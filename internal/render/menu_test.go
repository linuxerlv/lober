// 本文件测试 iPXE 菜单渲染（对应实现计划任务 7，依据 SPEC §4.5、§4.7）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// menuTestCtx 构造一个 Rocky 9 的最小 RenderContext，供菜单测试复用。
func menuTestCtx(family model.OSFamily, firmware model.FirmwareMode) model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "42"},
		Artifact: model.ArtifactSet{
			ID:        "as-rocky94",
			OSFamily:  family,
			OSVersion: "9.4",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "vmlinuz",
				Initrd: []string{"initrd.img"},
				Repo:   "repo/",
			},
		},
		Bases:          testBases,
		InstallToken:   "tok-menu-001",
		ActualFirmware: firmware,
	}
}

// TestMenu_RockyFourLines 锁住"Rocky 菜单的四行结构，且三个 URL 都指向
// 正确的基址"（计划任务 7 测试要求第一条）。
func TestMenu_RockyFourLines(t *testing.T) {
	ctx := menuTestCtx(model.OSFamilyRHEL, model.FirmwareUEFI)
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		// kernel 行可能因续行符 `\` 而在脚本文本中物理占用多行，但逻辑上
		// 仍是"kernel 语句" + "initrd 语句"两条——用语句数而非物理行数断言。
		joined := strings.Join(lines, "\n")
		kernelCount := strings.Count(joined, "kernel ")
		initrdCount := strings.Count(joined, "initrd ")
		if kernelCount != 1 || initrdCount != 1 {
			t.Fatalf("RenderMenu 输出应恰好含一条 kernel 语句与一条 initrd 语句，got:\n%s", out)
		}
	}

	if !strings.Contains(out, "kernel ") {
		t.Errorf("菜单应含 kernel 行，got:\n%s", out)
	}
	if !strings.Contains(out, "initrd ") {
		t.Errorf("菜单应含 initrd 行，got:\n%s", out)
	}
	if !strings.Contains(out, "inst.repo=") {
		t.Errorf("菜单应含 inst.repo= 参数，got:\n%s", out)
	}
	if !strings.Contains(out, "inst.ks=") {
		t.Errorf("菜单应含 inst.ks= 参数，got:\n%s", out)
	}

	artifactBase := ctx.Bases.ArtifactURL()
	gatewayBase := ctx.Bases.GatewayURL()
	if !strings.Contains(out, artifactBase) {
		t.Errorf("菜单中 kernel/initrd/inst.repo 应指向制品基址 %q，got:\n%s", artifactBase, out)
	}
	if !strings.Contains(out, gatewayBase) {
		t.Errorf("菜单中 inst.ks= 应指向网关基址 %q，got:\n%s", gatewayBase, out)
	}
}

// TestMenu_KsPointsToAnswerFile 验证 inst.ks= 的路径形态是
// /v1/answer/{task}/{token}/ks.cfg。
func TestMenu_KsPointsToAnswerFile(t *testing.T) {
	ctx := menuTestCtx(model.OSFamilyRHEL, model.FirmwareUEFI)
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	want := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, "ks.cfg")
	if !strings.Contains(out, want) {
		t.Errorf("菜单中 inst.ks= 应等于 %q，got:\n%s", want, out)
	}
}

// TestMenu_LinuxFamiliesRenderSuccessfully 锁住四个 Linux 家族的菜单渲染
// 都已接线（本文件的 menuTestCtx 是 Rocky 形态的通用 Linux 夹具，四家共用）。
//
// 这条测试替换了原先的 TestMenu_UnimplementedFamiliesReturnError —— 那条
// 断言 suse/windows/esxi 三个家族返回「未实现」错误，是分派接线之前的状态。
//
// Windows 与 ESXi **刻意不在这里覆盖**：它们对制品集有额外要求，用这个通用
// 夹具调用会得到正确的拒绝（Windows 要求 ArtifactSet.Firmware 与机器实际
// 上报的固件相符，ESXi 要求 Paths.Extra["mboot.efi"]），那不是「渲染失败」
// 而是「前提不满足时的硬拒绝」。它们的成功路径由 dispatch_test.go 用各自的
// 专用夹具覆盖，不在此重复。
//
// 为什么值得一条专门的测试：SUSE 的接线漏过一次。分派接线那一轨与写
// AutoYaST 渲染器的那一轨是同一波并行的，接线者检查时 autoyast.go 还不存在，
// 于是它在 renderer.go 留下注释「已检查 internal/render/ 下所有文件，没有
// 实现可接」并保留了未实现分支。而当时的分派测试不覆盖 SUSE 成功路径，
// 所以这个漏接没有变红 —— 直到 SUSE 的端到端脚本在 preview 阶段撞上它。
func TestMenu_LinuxFamiliesRenderSuccessfully(t *testing.T) {
	families := []model.OSFamily{
		model.OSFamilyRHEL, model.OSFamilyDebian,
		model.OSFamilyUbuntu, model.OSFamilySUSE,
	}
	for _, family := range families {
		t.Run(string(family), func(t *testing.T) {
			ctx := menuTestCtx(family, model.FirmwareUEFI)
			out, err := RenderMenu(ctx)
			if err != nil {
				t.Fatalf("RenderMenu(family=%s) 返回错误: %v", family, err)
			}
			if out == "" {
				t.Errorf("RenderMenu(family=%s) 产出为空", family)
			}
		})
	}
}

// TestMenu_SUSEUsesAutoYaSTNotKickstart 锁住 SUSE 的菜单用 autoyast= 而不是
// inst.ks=。SPEC 反复警示：两者都被口头叫「ks」，但 AutoYaST 是独立的 XML
// 格式，把 SUSE 路由进 kickstart 渲染器产出的文件安装器根本不认。
func TestMenu_SUSEUsesAutoYaSTNotKickstart(t *testing.T) {
	ctx := menuTestCtx(model.OSFamilySUSE, model.FirmwareUEFI)
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu(suse) 返回错误: %v", err)
	}
	if !strings.Contains(out, "autoyast=") {
		t.Errorf("SUSE 菜单应含 autoyast= 引导参数，got:\n%s", out)
	}
	if strings.Contains(out, "inst.ks=") {
		t.Errorf("SUSE 菜单不应含 inst.ks=（那是 RHEL 的 kickstart 参数），got:\n%s", out)
	}
	if !strings.Contains(out, "autoinst.xml") {
		t.Errorf("SUSE 的应答文件名应是 autoinst.xml，got:\n%s", out)
	}
}

// TestMenu_UnknownFamilyReturnsError 验证未知家族同样返回错误，不 panic。
func TestMenu_UnknownFamilyReturnsError(t *testing.T) {
	ctx := menuTestCtx(model.OSFamily("totally-unknown"), model.FirmwareUEFI)
	_, err := RenderMenu(ctx)
	if err == nil {
		t.Error("RenderMenu(未知家族) 应返回错误，got nil")
	}
}

// TestMenu_NoLiteralHTTPScheme 锁住"菜单里不含字面 http://"（计划任务 7
// 测试要求第三条）。用 https 基址渲染，确认 scheme 完全跟随配置。
func TestMenu_NoLiteralHTTPScheme(t *testing.T) {
	ctx := menuTestCtx(model.OSFamilyRHEL, model.FirmwareUEFI)
	ctx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	out, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 返回错误: %v", err)
	}
	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，菜单输出仍含字面 http://，说明 scheme 被硬编码，got:\n%s", out)
	}
}

// TestMenu_ErrIsDistinguishable 验证渲染错误携带足够信息可区分。
// 原先用 SUSE 做样本（那时它未实现），SUSE 已接线，改用一个真正未知的家族。
func TestMenu_ErrIsDistinguishable(t *testing.T) {
	const bogus = "totally-bogus-family"
	ctx := menuTestCtx(model.OSFamily(bogus), model.FirmwareUEFI)
	_, err := RenderMenu(ctx)
	if err == nil {
		t.Fatal("want non-nil error")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("错误信息应指出具体家族 %s，got: %v", bogus, err)
	}
}
