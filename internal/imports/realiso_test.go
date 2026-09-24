// 本文件用**真实 ISO 文件**验证导入路径，由环境变量 LOBER_TEST_REAL_ISO 开关。
//
// 为什么需要它：其余全部导入测试用的都是 fstest.MapFS 或手工造的目录树，
// 而 e2e 脚本一律绕过导入（直接写库把制品集推进到 ready）。于是
// **纯 Go ISO 9660 解析器从未处理过一份真 ISO** ——它在真实镜像上能不能走通
// （长文件名的 Rock Ridge/Joliet 扩展、目录记录跨扇区、大目录树、以及那些
// 只在真镜像里出现的边界）完全没有证据。
//
// 这一层是**所有家族真机装机的共同前置**：装机链路的第一步就是把 ISO 导成制品集，
// 导不出来后面一步都走不了。因此它值得一个能随时对着真镜像跑的测试，而不是
// 等到 VM 里装机失败再回头查。
//
// 跑法：
//
//	LOBER_TEST_REAL_ISO=/d/OpenCloudOS-9.6-x86_64-minimal.iso \
//	  /d/golang/bin/go test ./internal/imports/ -run TestRealISO -v
//
// 未设该变量时**跳过**而不是失败——与 store 测试要求 LOBER_TEST_PG_DSN 的做法
// 刻意不同：那边没有数据库就等于整个 store 层零覆盖（skip 会让 make check 在
// 零覆盖下变绿，所以必须 FAIL）；这边只是缺一份几 GB 的本地镜像，其余导入测试
// 仍然覆盖着解析与抽取的逻辑分支，skip 不会制造「看起来测了其实没测」的假象。
package imports

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// realISOPath 返回真实 ISO 的路径，未配置时跳过当前测试。
func realISOPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("LOBER_TEST_REAL_ISO")
	if p == "" {
		t.Skip("未设 LOBER_TEST_REAL_ISO，跳过真实 ISO 验证（见本文件头注释）")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("LOBER_TEST_REAL_ISO 指向的文件不可访问: %v", err)
	}
	return p
}

// TestRealISO_OpenAndIdentify 验证真实 ISO 能被打开、目录树能遍历、家族能识别。
//
// 三步分开断言而不是直接跑完整导入：任何一步失败时，错误信息要能直接指出是
// 「解析器读不了这个镜像」「目录树是空的」还是「特征文件在但识别逻辑没认出来」
// ——三者的处置完全不同。
func TestRealISO_OpenAndIdentify(t *testing.T) {
	isoPath := realISOPath(t)
	ctx := context.Background()

	root, err := NewMountExtractor("").Open(ctx, isoPath)
	if err != nil {
		t.Fatalf("打开真实 ISO 失败: %v —— 纯 Go ISO 9660 解析器在这份真镜像上"+
			"走不通，而导入是所有家族装机链路的第一步，这里失败则后面一步都走不了", err)
	}

	// 顶层目录必须非空。空目录树意味着解析器把根目录记录读错了——那种情况下
	// 后续的特征文件查找会全部落空，症状是「识别不出 OS 家族」，而真正的原因
	// 在更下面一层。
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		t.Fatalf("读 ISO 根目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("ISO 根目录为空 —— 解析器把根目录记录读错了，后续特征文件查找会全部落空")
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("ISO 根目录 %d 项: %v", len(entries), names)

	family, err := Identify(root)
	if err != nil {
		t.Fatalf("识别 OS 家族失败: %v —— 根目录项见上方日志，"+
			"对照 SPEC §2.1 的识别特征文件表核对是哪个特征文件没找到", err)
	}
	t.Logf("识别出的 OS 家族: %s", family)
}

// TestRealISO_JobRunProducesUsableArtifact 用**真实生产入口** Job.Run 跑一次
// 完整导入，并验证抽出来的路径在盘上真实存在。
//
// 走 Job.Run 而不是直接调某个 ExtractXxx：那条路径才是 HTTP 端点实际会走的
// （打开 ISO → 识别家族 → 按家族分派 → 抽取 → 生成 manifest → 写回 ready），
// 中间任何一环断掉都会让真机装机在第一步失败。逐个调 ExtractXxx 只能验证
// 抽取函数本身，验不到分派与写回。
//
// 「路径在盘上真实存在」这一条是与既有测试的关键差异：MapFS 测试验证的是
// 「抽取函数返回了正确的路径字符串」，而这里验证的是「那些路径指向的文件真的
// 被复制出来了」。两者之间差着一整个文件复制环节，而那个环节出错的症状是
// 装机时 404。
func TestRealISO_JobRunProducesUsableArtifact(t *testing.T) {
	isoPath := realISOPath(t)
	ctx := context.Background()
	destDir := t.TempDir()

	store := newFakeArtifactSetStore()
	job := NewJob(store, NewMountExtractor(""))

	if err := job.Run(ctx, "real-iso-set", isoPath, destDir); err != nil {
		t.Fatalf("Job.Run 失败: %v", err)
	}

	// Job.Run 内部对失败是「写回 failed 状态并返回 nil」（见 job.fail），
	// 因此**必须核对状态**，不能只看 Run 的返回值——只看返回值会让一次失败的
	// 导入看起来像成功，这正是 e2e 脚本当年绕过导入时没有察觉的那类问题。
	if store.state != model.ArtifactSetReady {
		t.Fatalf("导入后制品集状态 = %q, want ready；失败原因 = %q",
			store.state, store.failReason)
	}
	if store.manifestSHA256 == "" {
		t.Error("导入成功但 manifest sha256 为空")
	}

	paths := store.paths
	t.Logf("识别家族=%s 抽取结果: Kernel=%q Initrd=%v Repo=%q Extra=%v",
		store.osFamily, paths.Kernel, paths.Initrd, paths.Repo, paths.Extra)

	// 逐项确认落盘。Repo 带结尾斜杠是目录，只确认目录存在（其下的索引入口由
	// statemachine 的 precheck 按家族判据校验，不在本测试范围）。
	check := func(rel string, wantDir bool) {
		t.Helper()
		if rel == "" {
			return
		}
		full := filepath.Join(destDir, filepath.FromSlash(rel))
		info, statErr := os.Stat(full)
		if statErr != nil {
			t.Errorf("抽取声明的路径 %q 在盘上不存在: %v —— 抽取函数返回了路径"+
				"但文件没被复制出来，症状是装机时 404", rel, statErr)
			return
		}
		if wantDir && !info.IsDir() {
			t.Errorf("%q 应当是目录", rel)
		}
		if !wantDir && info.IsDir() {
			t.Errorf("%q 应当是文件", rel)
		}
		if !wantDir && info.Size() == 0 {
			t.Errorf("%q 是 0 字节 —— 复制动作发生了但内容没写进去", rel)
		}
	}

	check(paths.Kernel, false)
	for _, ir := range paths.Initrd {
		check(ir, false)
	}
	if paths.Repo != "" {
		check(paths.Repo[:len(paths.Repo)-1], true) // 去掉结尾斜杠
	}
	for _, v := range paths.Extra {
		check(v, false)
	}
}
