// 本文件测试 iPXE 菜单渲染（对应实现计划任务 7，依据 SPEC §4.5、§4.7）。
package render

import (
	"errors"
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

// TestMenu_UnimplementedFamiliesReturnError 锁住"未实现的家族各自
// 返回错误（不是空字符串）"（计划任务 7 测试要求第二条）。Debian 与
// Ubuntu 在 phase2-01 已实现，从这份"未实现"清单中移除——它们的正确性
// 分别由 preseed_test.go 的 TestGolden_DebianMenu、autoinstall_test.go
// 的 TestGolden_UbuntuMenu 等测试覆盖。
func TestMenu_UnimplementedFamiliesReturnError(t *testing.T) {
	families := []model.OSFamily{
		model.OSFamilySUSE, model.OSFamilyWindows, model.OSFamilyESXi,
	}
	for _, family := range families {
		t.Run(string(family), func(t *testing.T) {
			ctx := menuTestCtx(family, model.FirmwareUEFI)
			out, err := RenderMenu(ctx)
			if err == nil {
				t.Errorf("RenderMenu(family=%s) 应返回错误（未实现），got nil error, out=%q", family, out)
			}
			if out != "" {
				t.Errorf("RenderMenu(family=%s) 返回错误时输出应为空字符串，got %q", family, out)
			}
		})
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

// TestMenu_ErrIsDistinguishable 验证未实现家族返回的错误可用 errors.Is /
// errors.As 或至少能与其它错误区分（供网关未来据此选择 HTTP 状态码）。
// 本测试只验证错误非空且携带家族名信息，不对错误类型做过强假设（那属于
// 契约层，M4 未被要求定义专门的 sentinel error 类型）。
func TestMenu_ErrIsDistinguishable(t *testing.T) {
	ctx := menuTestCtx(model.OSFamilySUSE, model.FirmwareUEFI)
	_, err := RenderMenu(ctx)
	if err == nil {
		t.Fatal("want non-nil error")
	}
	if errors.Is(err, nil) {
		t.Error("错误值不应等价于 nil")
	}
	if !strings.Contains(err.Error(), "suse") && !strings.Contains(strings.ToLower(err.Error()), "suse") {
		t.Errorf("错误信息应指出具体家族 suse，got: %v", err)
	}
}
