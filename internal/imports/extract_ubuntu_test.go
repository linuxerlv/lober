// 本文件测试 Ubuntu 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。
package imports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// ubuntuFakeRoot 构造一个最小的 Ubuntu ISO 内容视图：casper 目录下的内核、
// initrd 与 squashfs 镜像（计划任务 5："Ubuntu 的内核与 initrd 在 casper
// 目录下"，"仓库不是 deb 仓库而是一个 squashfs 镜像"），加 .disk/info
// 识别特征文件。
func ubuntuFakeRoot() fstest.MapFS {
	return fstest.MapFS{
		"casper/vmlinuz":             &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/initrd":              &fstest.MapFile{Data: []byte("fake-initrd")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("fake-squashfs-image")},
		".disk/info":                 &fstest.MapFile{Data: []byte(`Ubuntu-Server 24.04.1 LTS "Noble Numbat" - Release amd64 (20240827)`)},
	}
}

// TestExtractUbuntu_ProducesCorrectPathStructure 锁住"两个家族的抽取各自
// 产出正确的路径结构"（计划任务 5 测试要求第一条）。
func TestExtractUbuntu_ProducesCorrectPathStructure(t *testing.T) {
	root := ubuntuFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	if paths.Kernel != "vmlinuz" {
		t.Errorf("Ubuntu 内核固定输出文件名应为 vmlinuz（SPEC §4.5），got %q", paths.Kernel)
	}
	if len(paths.Initrd) != 1 || paths.Initrd[0] != "initrd" {
		t.Errorf("Ubuntu initrd 固定输出文件名应为 initrd（SPEC §4.5），got %v", paths.Initrd)
	}

	for _, rel := range []string{"vmlinuz", "initrd"} {
		if _, err := os.Stat(filepath.Join(destDir, rel)); err != nil {
			t.Errorf("落盘文件 %q 应存在: %v", rel, err)
		}
	}
}

// TestExtractUbuntu_SquashfsRecordedInExtra 锁住"Ubuntu 的 squashfs 镜像
// 被记入额外文件且被完整性校验覆盖"（计划任务 5 测试要求第二条）。
//
// Extra 的键是 squashfs 文件的原始文件名（不是固定常量）——因为不同
// Ubuntu 版本/变体的 squashfs 文件名不同，键名必须跟着文件名走才能让
// 多段场景（TestExtractUbuntu_MultiPartSquashfsAllRecordedInExtra）里
// 的每一段都有一个不冲突的键。
//
// "被完整性校验覆盖"这半句由 internal/statemachine/precheck.go 的
// artifactCheckPaths 函数保证（它遍历 ArtifactPaths.Extra 的全部值做
// 存在性检查，是通用逻辑，不需要为 Ubuntu 新增任何代码），本测试只锁住
// "被记入额外文件"这一半——squashfs 的相对路径必须出现在返回的
// ArtifactPaths.Extra 里。
func TestExtractUbuntu_SquashfsRecordedInExtra(t *testing.T) {
	root := ubuntuFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	const wantKey = "filesystem.squashfs"
	squashfsPath, ok := paths.Extra[wantKey]
	if !ok {
		t.Fatalf("ArtifactPaths.Extra 应含键 %q，got Extra=%v", wantKey, paths.Extra)
	}
	if _, err := os.Stat(filepath.Join(destDir, squashfsPath)); err != nil {
		t.Errorf("Extra 记录的 squashfs 相对路径 %q 在磁盘上应真实存在: %v", squashfsPath, err)
	}
}

// TestExtractUbuntu_MultiPartSquashfsAllRecordedInExtra 锁住"真实 Ubuntu
// Server 22.04/24.04 live ISO 的多段 squashfs 结构全部被抽取并记录"。
//
// 背景（不是本仓库假设，是核实过的真实行为）：Ubuntu Server 自 22.04 起
// casper/ 下不再是单一的 filesystem.squashfs，而是"基础层 + 覆盖层"两段：
// ubuntu-server-minimal.squashfs（基础）与
// ubuntu-server-minimal.ubuntu-server.squashfs（覆盖，含 WiFi 驱动等
// Server 专属内容）。只抽取第一段、漏掉第二段，产出的制品集装出来的机器
// 会缺功能组件——这正是 Cubic 工具曾经踩过的真实故障
// （github.com/muquit/cubic-ubuntu-server-fix 的 README 明确记录了这个
// bug）。旧版 Ubuntu／某些变体只有一个 squashfs 文件（ubuntuFakeRoot 覆盖
// 的场景），因此抽取逻辑不能硬编码固定文件名，必须扫描 casper/ 下全部
// *.squashfs 文件，一个不漏地搬运并逐一记入 Extra，让运维即使换了
// Ubuntu 版本/变体也不会静默漏装某一段。
func TestExtractUbuntu_MultiPartSquashfsAllRecordedInExtra(t *testing.T) {
	root := fstest.MapFS{
		"casper/vmlinuz": &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/initrd":  &fstest.MapFile{Data: []byte("fake-initrd")},
		"casper/ubuntu-server-minimal.squashfs": &fstest.MapFile{
			Data: []byte("fake-base-layer"),
		},
		"casper/ubuntu-server-minimal.ubuntu-server.squashfs": &fstest.MapFile{
			Data: []byte("fake-overlay-layer"),
		},
		".disk/info": &fstest.MapFile{Data: []byte(`Ubuntu-Server 24.04.1 LTS "Noble Numbat" - Release amd64 (20240827)`)},
	}
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	wantFiles := []string{
		"ubuntu-server-minimal.squashfs",
		"ubuntu-server-minimal.ubuntu-server.squashfs",
	}
	if len(paths.Extra) != len(wantFiles) {
		t.Fatalf("两段 squashfs 都应被记入 Extra，got Extra=%v", paths.Extra)
	}
	for _, name := range wantFiles {
		rel, ok := paths.Extra[name]
		if !ok {
			t.Errorf("Extra 应含键 %q（对应 casper/%s），got Extra=%v", name, name, paths.Extra)
			continue
		}
		if _, err := os.Stat(filepath.Join(destDir, rel)); err != nil {
			t.Errorf("Extra[%q] 记录的相对路径 %q 在磁盘上应真实存在: %v", name, rel, err)
		}
	}
}

// TestExtractUbuntu_MissingSquashfsReturnsError 锁住"缺关键文件时导入
// 置失败且原因具体"（计划任务 5 测试要求第四条），专门针对 Ubuntu 特有的
// squashfs 镜像缺失场景（计划任务 5 明确点名："Ubuntu 少了 squashfs 镜像
// 就装不了，而那个缺失在 Phase 1 的校验逻辑里不会被发现"——本测试锁住的
// 是抽取阶段就该拒绝，不是等完整性校验才发现）。这里 casper/ 目录本身
// 存在（含 vmlinuz、initrd），只是其下没有任何 *.squashfs 文件——与下面
// TestExtractUbuntu_MissingCasperDirReturnsError（casper/ 整个目录都不
// 存在）是两条独立场景：findUbuntuCasperSquashfsFiles 内部先
// fs.ReadDir("casper")，目录不存在与目录存在但为空对该调用的返回值不同
// （前者是 error，后者是空切片+nil error），任何一条路径出错都可能只
// 影响其中一种场景。
func TestExtractUbuntu_MissingSquashfsReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"casper/vmlinuz": &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/initrd":  &fstest.MapFile{Data: []byte("fake-initrd")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("缺 squashfs 镜像时 ExtractUbuntu 应返回错误")
	}
	if !strings.Contains(err.Error(), "squashfs") {
		t.Errorf("缺 squashfs 镜像时错误信息应提到 squashfs，让运维一眼看出"+
			"是这一类文件缺失而不必去猜，got: %v", err)
	}
}

// TestExtractUbuntu_MissingCasperDirReturnsError 锁住"casper/ 目录整体
// 不存在时返回明确错误"——这与"casper/ 存在但内容不全"是不同的故障面：
// 一份完全不是 Ubuntu live ISO 结构的目录树（比如识别误判，或者调用方
// 传错了根路径）应该在查找 squashfs 这一步就得到清晰的失败信息，而不是
// 一个"文件不存在"式的裸系统错误让人误以为是别的原因。
func TestExtractUbuntu_MissingCasperDirReturnsError(t *testing.T) {
	root := fstest.MapFS{
		".disk/info": &fstest.MapFile{Data: []byte("not actually ubuntu")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("casper/ 目录整体不存在时 ExtractUbuntu 应返回错误")
	}
}

// TestExtractUbuntu_MissingKernelReturnsError 缺内核场景。
func TestExtractUbuntu_MissingKernelReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"casper/initrd":              &fstest.MapFile{Data: []byte("fake-initrd")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("fake-squashfs")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("缺内核文件时 ExtractUbuntu 应返回错误")
	}
	if !strings.Contains(err.Error(), "vmlinuz") {
		t.Errorf("缺内核文件时错误信息应指名具体文件（casper/vmlinuz），"+
			"这样运维才能一眼看出问题所在而不必去猜，got: %v", err)
	}
}

// TestExtractUbuntu_MissingInitrdReturnsError 缺 initrd 场景（任务要求
// "缺 casper/vmlinuz、缺 initrd 各自报明确错误"——与缺内核是两条独立的
// 测试，因为 findUbuntuCasperFile 对每个文件名分别调用，任何一次改动都
// 可能只影响其中一条路径而不影响另一条，两条都要单独锁住）。
func TestExtractUbuntu_MissingInitrdReturnsError(t *testing.T) {
	root := fstest.MapFS{
		"casper/vmlinuz":             &fstest.MapFile{Data: []byte("fake-kernel")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("fake-squashfs")},
	}
	destDir := t.TempDir()

	_, err := ExtractUbuntu(root, destDir)
	if err == nil {
		t.Fatal("缺 initrd 文件时 ExtractUbuntu 应返回错误")
	}
	if !strings.Contains(err.Error(), "initrd") {
		t.Errorf("缺 initrd 文件时错误信息应指名具体文件（casper/initrd），"+
			"got: %v", err)
	}
}

// TestExtractUbuntu_PathsAreRelativeNoSchemeNoHost 锁住"路径是相对路径、
// 不含 scheme 与主机名"（任务测试要求第三条）——ArtifactPaths 里的所有
// 字段按 SPEC §2.1 的约定必须是相对于制品根目录的相对路径，拼 URL 是
// 渲染器的事（model/inventory.go 里 ArtifactPaths 的文档注释）。这条测试
// 独立于"落盘文件是否存在"，专门防止未来有人往 Kernel/Initrd/Extra 里
// 塞一个绝对路径或者已经拼好协议头的 URL 字符串。
func TestExtractUbuntu_PathsAreRelativeNoSchemeNoHost(t *testing.T) {
	root := ubuntuFakeRoot()
	destDir := t.TempDir()

	paths, err := ExtractUbuntu(root, destDir)
	if err != nil {
		t.Fatalf("ExtractUbuntu 返回错误: %v", err)
	}

	candidates := []string{paths.Kernel}
	candidates = append(candidates, paths.Initrd...)
	for _, v := range paths.Extra {
		candidates = append(candidates, v)
	}

	for _, p := range candidates {
		if p == "" {
			continue
		}
		if strings.Contains(p, "://") {
			t.Errorf("路径 %q 不应含 scheme（拼 URL 是渲染器的事，抽取器只产出"+
				"相对路径，见 model.ArtifactPaths 的文档注释）", p)
		}
		if filepath.IsAbs(p) {
			t.Errorf("路径 %q 不应是绝对路径", p)
		}
		if strings.HasPrefix(p, "/") {
			t.Errorf("路径 %q 不应以 / 开头（会被误解读为绝对路径或误拼出"+
				"双斜杠 URL）", p)
		}
	}
}

// TestIdentifyOrder_UbuntuBeforeDebian 锁住"同时含 Debian 与 Ubuntu 特征
// 时识别为 Ubuntu"（计划任务 5 测试要求第三条，与 identify_test.go 的
// TestIdentify_AmbiguousUbuntuAndDebianResolvesToUbuntu 是同一条不变量，
// 这里加入更接近真实 Ubuntu ISO 的完整特征文件集合后再验证一遍——确认
// 加入 casper/squashfs 等真实特征后判定顺序仍正确，不仅仅是 identify.go
// 单元测试里的极简 fixture）。
func TestIdentifyOrder_UbuntuBeforeDebian(t *testing.T) {
	root := ubuntuFakeRoot()
	// 手动补上 dists/ 目录，模拟"同时含 Debian 与 Ubuntu 特征"的真实歧义
	// 场景（计划任务 5 原文点名："Ubuntu 的 ISO 也含 dists/"）。
	root["dists/stable/Release"] = &fstest.MapFile{Data: []byte("")}

	family, err := Identify(root)
	if err != nil {
		t.Fatalf("Identify 返回错误: %v", err)
	}
	if family != model.OSFamilyUbuntu {
		t.Fatalf("同时含真实 Ubuntu 特征集合与 dists/ 时应识别为 Ubuntu，got %s", family)
	}
}
