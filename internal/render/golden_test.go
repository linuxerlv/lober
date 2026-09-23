// 本文件是 kickstart 组装的 golden file 测试（对应实现计划任务 6，依据
// SPEC §5.1、§5.6）。
//
// golden 测试的价值全在于"有人看了那个 diff"——更新 golden 时必须人工审阅
// diff，不能盲目接受 -update 的输出。运行 `go test ./internal/render/ -run
// TestGolden -update` 只会重写 testdata/*.golden，之后必须用 `git diff`
// 逐字符查看变化是否符合预期，再决定是否提交；不能把 -update 当成"让测试
// 变绿"的捷径。
package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// update 控制"重新生成期望文件"（计划任务 6："用一个 -update flag 控制
// 重新生成期望文件"）。
var update = flag.Bool("update", false, "重新生成 testdata/*.golden（必须人工审阅 git diff 后才能提交，不能盲目接受）")

// fixedInstallToken / fixedFinalizeToken / fixedSN 等是渲染上下文的固定假
// 数据（计划任务 6："渲染上下文的构造要用固定的假数据（固定 token、固定
// 时间、固定 SN），否则 golden 每次都变"）。
const (
	fixedTaskID        = "42"
	fixedInstallToken  = "install-tok-fixed-0000000000000000"
	fixedFinalizeToken = "finalize-tok-fixed-0000000000000000"
	fixedSN            = "SN-FIXED-0001"
	fixedHostname      = "web-01"
	fixedBizIP         = "10.20.0.15"
	fixedDiskByID      = "/dev/disk/by-id/wwn-0x5000c500fixed0001"
)

// goldenBases 是 golden 测试统一使用的固定基址。
var goldenBases = model.BaseURLs{
	GatewayBase:  "http://gw.internal",
	ArtifactBase: "http://ng.internal",
}

// minimalKickstartCtx 构造"无 hook 的最小 kickstart"渲染上下文。
func minimalKickstartCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            fixedTaskID,
			HostSN:        fixedSN,
			FinalizeToken: fixedFinalizeToken,
		},
		Template: model.Template{
			ID:             "tpl-rocky9-minimal",
			AnswerTemplate: "%packages\n@core\n%end\n",
		},
		Artifact: model.ArtifactSet{
			ID:        "as-rocky94",
			OSFamily:  model.OSFamilyRHEL,
			OSVersion: "9.4",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "vmlinuz",
				Initrd: []string{"initrd.img"},
				Repo:   "repo/",
			},
		},
		Facts: map[string]string{
			model.FactTaskID:       fixedTaskID,
			model.FactSN:           fixedSN,
			model.FactHostname:     fixedHostname,
			model.FactBizIP:        fixedBizIP,
			model.FactDiskPrimary:  fixedDiskByID,
			model.FactOSFamily:     string(model.OSFamilyRHEL),
			model.FactArtifactBase: goldenBases.ArtifactURL(),
			model.FactGatewayBase:  goldenBases.GatewayURL(),
		},
		Bases:         goldenBases,
		InstallToken:  fixedInstallToken,
		FinalizeToken: fixedFinalizeToken,
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
}

// fullKickstartCtx 构造"带三阶段 hook 的完整 kickstart"渲染上下文，在
// minimalKickstartCtx 基础上加三个 installer 阶段的 hook 与一个 firstboot
// hook。
func fullKickstartCtx() model.RenderContext {
	ctx := minimalKickstartCtx()
	ctx.Template.ID = "tpl-rocky9-full"
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb2222nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: false},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// rockyMenuCtx 构造 Rocky 的 iPXE 菜单渲染上下文。
func rockyMenuCtx() model.RenderContext {
	ctx := minimalKickstartCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	return ctx
}

// goldenPath 返回 testdata 下某个 golden 文件的绝对路径。
func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

// assertGolden 比较 got 与 testdata/name 的内容；*update 为真时改为写入
// got 到该文件（供人工用 git diff 审阅）。
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)

	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("写入 golden 文件 %s 失败: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 文件 %s 失败: %v —— 若这是新增的 golden 文件，"+
			"先跑一次 `go test ./internal/render/ -run TestGolden -update` 生成它，"+
			"然后人工审阅内容后再提交", path, err)
	}
	if got != string(want) {
		t.Errorf("渲染输出与 golden 文件 %s 不一致（逐字节比较）。\n"+
			"若这是预期内的变更，运行 `go test ./internal/render/ -run TestGolden -update` "+
			"重新生成，然后必须用 git diff 人工审阅变化再提交——golden 测试的价值全在于"+
			"「有人看了那个 diff」。\n实际输出:\n%s", path, got)
	}
}

// TestGolden_FirstbootScaffoldWrappedInPostSection 锁住一个曾经真实出现过
// 的组装 bug：FirstbootScaffold 的产出必须落在自己的 %post ... %end 段落
// 里，不能作为不属于任何段落的裸露文本出现在 kickstart 文件末尾。
//
// 这个 bug 不会被任何更细粒度的单元测试抓到——RenderStageSection 与
// FirstbootScaffold 各自的测试都只断言"自己的产出里有什么"，没有测试断言
// "AssembleKickstart 把它们拼起来之后，firstboot 那部分是否仍然处在某个
// 合法段落内"。这正是本计划反复强调"golden file 让『输出变了』这件事可见"
// 的原因——人工审阅完整装配结果时一眼就能看出裸露文本，逐条单元断言看
// 不出来。
func TestGolden_FirstbootScaffoldWrappedInPostSection(t *testing.T) {
	out, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}

	marker := "mkdir -p /var/lib/lober"
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("输出中找不到 firstboot 脚手架的起始标记 %q，got:\n%s", marker, out)
	}

	// 从起始标记往前找最近的段落头（%pre / %post / %post --nochroot），
	// 它必须是 %post（不能是 %pre，也不能"找不到"，那意味着这段文本前面
	// 根本没有开启任何段落）。
	before := out[:idx]
	lastPost := strings.LastIndex(before, "%post")
	lastPre := strings.LastIndex(before, "%pre")
	if lastPost == -1 {
		t.Fatalf("firstboot 脚手架之前没有任何 %%post 段落头，说明这段文本"+
			"游离在所有段落之外——anaconda 会把它当成语法错误或直接忽略，"+
			"firstboot 机制会静默整体失效。got:\n%s", out)
	}
	if lastPost < lastPre {
		t.Fatalf("firstboot 脚手架前最近的段落头是 %%pre 而不是 %%post"+
			"（lastPost=%d, lastPre=%d），说明它被错误地归到了 pre 阶段的段落里", lastPost, lastPre)
	}

	// 从起始标记往后找最近的 %end，它必须存在，且中间不能再出现下一个
	// 段落头（否则说明 firstboot 脚手架没有被独立的 %end 正确闭合，而是被
	// 后续内容意外截断或合并）。
	after := out[idx:]
	endIdx := strings.Index(after, "%end")
	if endIdx == -1 {
		t.Fatalf("firstboot 脚手架之后找不到 %%end 闭合段落，got:\n%s", out)
	}
}

// TestGolden_MinimalKickstart 锁住无 hook 的最小 kickstart 输出（计划任务
// 6"至少三份"golden 之一）。
func TestGolden_MinimalKickstart(t *testing.T) {
	out, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}
	assertGolden(t, "kickstart_minimal.golden", out)
}

// TestGolden_FullKickstart 锁住带三阶段 hook 的完整 kickstart 输出。
func TestGolden_FullKickstart(t *testing.T) {
	out, err := AssembleKickstart(fullKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}
	assertGolden(t, "kickstart_full.golden", out)
}

// TestGolden_RockyMenu 锁住 Rocky 的 iPXE 菜单输出。
func TestGolden_RockyMenu(t *testing.T) {
	out, err := RenderMenu(rockyMenuCtx())
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	assertGolden(t, "menu_rocky.golden", out)
}

// TestGolden_NoLiteralHTTPScheme 锁住"输出里不含字面 http://"
// （计划任务 6 测试要求，SPEC §1.5：URL 全从基址来）——单独一条断言，覆盖
// 完整 kickstart 与菜单两种产出。
//
// 这条约束的对象是"scheme 是否硬编码在渲染器源码里"，不是"运行期输出的
// 字符串里永远不能出现 http:// 这几个字符"——golden 测试固定用的基址本身
// 就是 http://（MVP 明文 HTTP 场景），那种情况下输出理所当然含 http://，
// 这是配置决定的，不是代码写死的。因此本测试把基址换成 https://
// 重新渲染，断言换了配置之后输出里不再残留任何硬编码的 http://，这才是
// "不写死 scheme"这条要求的可执行版本（与 clientlib_test.go /
// menu_test.go 里对应测试的设计保持一致）。
func TestGolden_NoLiteralHTTPScheme(t *testing.T) {
	httpsBases := model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}

	httpsKsCtx := fullKickstartCtx()
	httpsKsCtx.Bases = httpsBases
	// Facts 里的 LOBER_ARTIFACT_BASE/LOBER_GATEWAY_BASE 是"某个更早的时间点
	// 把 ctx.Bases 拼好后存进 facts"的产物（真实系统里两者永远来自同一份
	// 配置，不会出现 Bases 已经是 https:// 而 Facts 里还残留旧 http://
	// 快照的情况）。测试里手动换 Bases 时必须同步更新这两个 fact 值，否则
	// 会造成"渲染器把 scheme 硬编码进了 fact"的假阳性——真正的问题只在于
	// 测试 fixture 的构造顺序，不在于 InjectFacts 或 AssembleKickstart 本身
	// 的实现。
	httpsKsCtx.Facts = cloneFacts(httpsKsCtx.Facts)
	httpsKsCtx.Facts[model.FactArtifactBase] = httpsBases.ArtifactURL()
	httpsKsCtx.Facts[model.FactGatewayBase] = httpsBases.GatewayURL()

	ksOut, err := AssembleKickstart(httpsKsCtx)
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}
	if strings.Contains(ksOut, "http://") {
		t.Errorf("Bases 配置为 https:// 时，完整 kickstart 输出仍含字面 http://，"+
			"说明某处 scheme 被硬编码，got:\n%s", ksOut)
	}

	httpsMenuCtx := rockyMenuCtx()
	httpsMenuCtx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	menuOut, err := RenderMenu(httpsMenuCtx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	if strings.Contains(menuOut, "http://") {
		t.Errorf("Bases 配置为 https:// 时，iPXE 菜单输出仍含字面 http://，"+
			"说明某处 scheme 被硬编码，got:\n%s", menuOut)
	}
}

// cloneFacts 返回 facts 的一份浅拷贝，供测试在不影响共享 fixture 的前提下
// 修改个别键值。
func cloneFacts(facts map[string]string) map[string]string {
	out := make(map[string]string, len(facts))
	for k, v := range facts {
		out[k] = v
	}
	return out
}
