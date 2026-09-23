// 本文件是分派覆盖测试（t08-dispatch 一轨的任务，对应
// internal/render/menu.go 与 renderer.go 的分派 switch）。
//
// 目的：锁住"六个家族的分派各自命中正确的实现"，防止未来有人在分派处
// 加了 case 却漏了真正接线（正是上一轮 Windows/ESXi 发生过的问题：
// unattend.go/winpe.go/esxi.go 的实现与测试都在，但分派处仍返回"未实现"，
// 那些代码一行都跑不到——本文件的存在就是为了让这类回归在 CI 里可见）。
package render

import (
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// 六个家族各自的最小 RenderContext 构造
// ---------------------------------------------------------------------------

// dispatchRHELCtx / dispatchDebianCtx / dispatchUbuntuCtx 复用现有 golden
// 测试已经验证过的最小上下文构造方式（menuTestCtx／minimalKickstartCtx），
// 不重新发明一套 fixture——分派测试只关心"调用到了正确的函数"，家族自身
// 的渲染细节已由各自的 golden/单元测试覆盖。

func dispatchRHELCtx() model.RenderContext {
	ctx := minimalKickstartCtx()
	ctx.ActualFirmware = model.FirmwareUEFI
	return ctx
}

func dispatchDebianCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "d1", FinalizeToken: "finalize-tok-debian"},
		Artifact: model.ArtifactSet{
			ID:        "as-debian12",
			OSFamily:  model.OSFamilyDebian,
			OSVersion: "12",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "vmlinuz",
				Initrd: []string{"initrd.gz"},
				Repo:   "repo/",
			},
		},
		Facts: map[string]string{
			model.FactDiskPrimary: "/dev/disk/by-id/wwn-debian-dispatch",
		},
		Bases:         testBases,
		InstallToken:  "tok-dispatch-debian",
		FinalizeToken: "finalize-tok-debian",
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
}

func dispatchUbuntuCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{ID: "u1", FinalizeToken: "finalize-tok-ubuntu"},
		Artifact: model.ArtifactSet{
			ID:        "as-ubuntu2404",
			OSFamily:  model.OSFamilyUbuntu,
			OSVersion: "24.04",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "vmlinuz",
				Initrd: []string{"initrd"},
				Extra:  map[string]string{"squashfs": "casper/filesystem.squashfs"},
			},
		},
		Facts: map[string]string{
			model.FactDiskPrimary: "/dev/disk/by-id/wwn-ubuntu-dispatch",
		},
		Bases:         testBases,
		InstallToken:  "tok-dispatch-ubuntu",
		FinalizeToken: "finalize-tok-ubuntu",
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
}

// dispatchWindowsCtx 构造 Windows 家族的最小 RenderContext，firmware 取
// uefi——Paths.Extra 携带 RenderWindowsMenu 需要的四个键
// （winpe.go 的 windowsExtraKeys），Task.ResolvedParams 携带 RenderUnattend
// 需要的 hostname（guard 校验器要求非空表单值合法，见 unattend.go）。
func dispatchWindowsCtx(firmware model.FirmwareMode) model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID: "w1",
			ResolvedParams: map[string]string{
				"hostname": "win-dispatch-01",
			},
		},
		Artifact: model.ArtifactSet{
			ID:        "as-win2022",
			OSFamily:  model.OSFamilyWindows,
			OSVersion: "2022",
			Arch:      model.ArchX86_64,
			Firmware:  firmware,
			Paths: model.ArtifactPaths{
				Extra: map[string]string{
					"wimboot":  "win2022-" + string(firmware) + "/wimboot",
					"BCD":      "win2022-" + string(firmware) + "/BCD",
					"boot.sdi": "win2022-" + string(firmware) + "/boot.sdi",
					"boot.wim": "win2022-" + string(firmware) + "/boot.wim",
				},
			},
		},
		Facts: map[string]string{
			model.FactDiskPrimary: "0",
			model.FactHostname:    "win-dispatch-01",
		},
		Bases:          testBases,
		InstallToken:   "tok-dispatch-windows",
		FinalizeToken:  "finalize-tok-windows",
		ActualFirmware: firmware,
	}
}

// dispatchESXiCtx 构造 ESXi 家族的最小 RenderContext，firmware 取
// uefi——复用 esxi_test.go 的 esxiKickstartCtx 结构（同一份 Extra 键约定：
// mboot.efi/boot.cfg）。
func dispatchESXiCtx(firmware model.FirmwareMode) model.RenderContext {
	ctx := esxiKickstartCtx()
	ctx.Artifact.Firmware = firmware
	ctx.ActualFirmware = firmware
	return ctx
}

// dispatchSUSECtx 构造 SUSE 家族的最小 RenderContext——即使 SUSE 目前落
// "未实现"分支，仍需要一个合法 fixture 供 TestDispatch_UnimplementedFamilyStillErrors
// 复用（该测试锁住"未实现家族仍返回错误"这条防线不会被将来的改动破坏）。
func dispatchSUSECtx() model.RenderContext {
	return model.RenderContext{
		Task:     model.Task{ID: "s1"},
		Artifact: model.ArtifactSet{OSFamily: model.OSFamilySUSE, OSVersion: "15.5", Arch: model.ArchX86_64},
		Bases:    testBases,
	}
}

// ---------------------------------------------------------------------------
// 六个家族的分派各自命中正确实现（RenderMenu）
// ---------------------------------------------------------------------------

// TestDispatch_RenderMenu_ImplementedFamiliesSucceed 断言 RHEL/Debian/
// Ubuntu/Windows/ESXi 五个已实现家族的 RenderMenu 都不返回"未实现"错误，
// 且产出非空——这是本轮任务的核心断言："那些代码一行都跑不到"这类回归
// 会在这里直接失败（out 为空或 err 命中 unimplementedFamilyError）。
func TestDispatch_RenderMenu_ImplementedFamiliesSucceed(t *testing.T) {
	cases := []struct {
		name string
		ctx  model.RenderContext
	}{
		{"rhel", dispatchRHELCtx()},
		{"debian", dispatchDebianCtx()},
		{"ubuntu", dispatchUbuntuCtx()},
		{"windows", dispatchWindowsCtx(model.FirmwareUEFI)},
		{"esxi", dispatchESXiCtx(model.FirmwareUEFI)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RenderMenu(c.ctx)
			if err != nil {
				t.Fatalf("RenderMenu(family=%s) 返回错误: %v", c.name, err)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("RenderMenu(family=%s) 产出为空", c.name)
			}
			if strings.Contains(strings.ToLower(out), "mvp 范围内未实现") {
				t.Errorf("RenderMenu(family=%s) 产出中含'未实现'字样，说明分派未真正接线，got:\n%s",
					c.name, out)
			}
		})
	}
}

// TestDispatch_RenderAnswerFile_ImplementedFamiliesSucceed 断言五个已实现
// 家族的 RenderAnswerFile 都不返回"未实现"错误，且产出非空。fileName 各自
// 传入该家族在 model.AnswerFileName 中登记的值——这也顺带验证了
// validateAnswerFileName 对新接线家族的放行没有问题。
func TestDispatch_RenderAnswerFile_ImplementedFamiliesSucceed(t *testing.T) {
	r := &Renderer{}
	cases := []struct {
		name string
		ctx  model.RenderContext
	}{
		{"rhel", dispatchRHELCtx()},
		{"debian", dispatchDebianCtx()},
		{"ubuntu", dispatchUbuntuCtx()},
		{"windows", dispatchWindowsCtx(model.FirmwareUEFI)},
		{"esxi", dispatchESXiCtx(model.FirmwareUEFI)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fileName, ok := c.ctx.AnswerFileName()
			if !ok {
				t.Fatalf("AnswerFileName(family=%s) 返回 ok=false", c.name)
			}
			out, err := r.RenderAnswerFile(c.ctx, fileName)
			if err != nil {
				t.Fatalf("RenderAnswerFile(family=%s, file=%s) 返回错误: %v", c.name, fileName, err)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("RenderAnswerFile(family=%s) 产出为空", c.name)
			}
			if strings.Contains(strings.ToLower(out), "mvp 范围内未实现") {
				t.Errorf("RenderAnswerFile(family=%s) 产出中含'未实现'字样，说明分派未真正接线，got:\n%s",
					c.name, out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 硬约束 1：BIOS + ESXi 被拒绝
// ---------------------------------------------------------------------------

// TestDispatch_ESXiRejectsBIOS 锁住"ESXi 只支持 UEFI"（SPEC §0.3、附录
// A.14）：机器实际固件是 BIOS 时，RenderMenu 必须拒绝，不产出一个注定
// 失败的菜单，且错误信息要说清楚"这台机器是 BIOS，任务要装 ESXi"。
func TestDispatch_ESXiRejectsBIOS(t *testing.T) {
	ctx := dispatchESXiCtx(model.FirmwareUEFI)
	ctx.ActualFirmware = model.FirmwareBIOS // 机器实际上报 BIOS，任务仍 pin 着 ESXi
	out, err := RenderMenu(ctx)
	if err == nil {
		t.Fatalf("RenderMenu(esxi, 实际固件=bios) 应返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("RenderMenu(esxi, 实际固件=bios) 返回错误时输出应为空，got %q", out)
	}
	if !errors.Is(err, ErrESXiRequiresUEFI) {
		t.Errorf("错误应可用 errors.Is(err, ErrESXiRequiresUEFI) 判定，got: %v", err)
	}
	// 错误信息要可读：说清"机器实际固件"与"ESXi 只支持 UEFI"两个事实，
	// 不能只是一句不带上下文的 sentinel 文本。
	msg := err.Error()
	if !strings.Contains(msg, "uefi") {
		t.Errorf("错误信息应提及 uefi 要求，got: %v", err)
	}
	if !strings.Contains(msg, string(model.FirmwareBIOS)) {
		t.Errorf("错误信息应提及机器实际上报的固件模式 %q，got: %v", model.FirmwareBIOS, err)
	}
}

// ---------------------------------------------------------------------------
// 硬约束 2：Windows 按固件选对应的制品键
// ---------------------------------------------------------------------------

// TestDispatch_WindowsMenu_UEFIAndBIOSSelectOwnArtifactKeys 验证 Windows
// 的 UEFI 与 BIOS 两条路径各自选到自己那套制品键（不是共用一套），且
// RenderMenu 按 ctx.ActualFirmware 校验，与任务 pin 住的 Firmware 不符时
// 硬拒绝（§4.6 第二项）。
func TestDispatch_WindowsMenu_UEFIAndBIOSSelectOwnArtifactKeys(t *testing.T) {
	uefiCtx := dispatchWindowsCtx(model.FirmwareUEFI)
	uefiOut, err := RenderMenu(uefiCtx)
	if err != nil {
		t.Fatalf("RenderMenu(windows, uefi) 返回错误: %v", err)
	}
	wantUEFIWimboot := uefiCtx.SetArtifactURL(uefiCtx.Artifact.Paths.Extra["wimboot"])
	if !strings.Contains(uefiOut, wantUEFIWimboot) {
		t.Errorf("windows uefi 菜单应指向 uefi 制品键 %q，got:\n%s", wantUEFIWimboot, uefiOut)
	}

	biosCtx := dispatchWindowsCtx(model.FirmwareBIOS)
	biosOut, err := RenderMenu(biosCtx)
	if err != nil {
		t.Fatalf("RenderMenu(windows, bios) 返回错误: %v", err)
	}
	wantBIOSWimboot := biosCtx.SetArtifactURL(biosCtx.Artifact.Paths.Extra["wimboot"])
	if !strings.Contains(biosOut, wantBIOSWimboot) {
		t.Errorf("windows bios 菜单应指向 bios 制品键 %q，got:\n%s", wantBIOSWimboot, biosOut)
	}

	// 两套产出引用的 wimboot 制品 URL 必须不同——否则说明固件维度没有真正
	// 生效，两个固件都读到了同一套制品键。
	if wantUEFIWimboot == wantBIOSWimboot {
		t.Fatalf("测试 fixture 本身没有区分两套固件的制品键，测试无效")
	}
	if strings.Contains(uefiOut, wantBIOSWimboot) || strings.Contains(biosOut, wantUEFIWimboot) {
		t.Errorf("windows 的 uefi/bios 菜单产出交叉引用了对方的制品键，"+
			"uefi out:\n%s\nbios out:\n%s", uefiOut, biosOut)
	}

	// 实际固件与任务 pin 住的固件不符时必须硬拒绝（§4.6），不得静默选另
	// 一套制品集。
	mismatched := dispatchWindowsCtx(model.FirmwareUEFI)
	mismatched.ActualFirmware = model.FirmwareBIOS
	if _, err := RenderMenu(mismatched); err == nil {
		t.Error("RenderMenu(windows, 任务pin=uefi, 实际=bios) 应返回错误，got nil")
	}
}

// ---------------------------------------------------------------------------
// 硬约束 3：SUSE 绝不能路由进 kickstart
// ---------------------------------------------------------------------------

// TestDispatch_SUSENotRoutedToKickstart 锁住"SUSE 不会被送进 RHEL 的
// kickstart 分支"这条防线——即使 AutoYaST 的实现尚未接入（本 worktree 内
// 不存在该实现，见 contractGaps），SUSE 的分派结果也绝不能等价于 RHEL 的
// kickstart 产出，且不应含 kickstart 的语法特征（%packages）。SUSE 当前
// 落"未实现"分支返回错误，产出为空，天然满足"不是 kickstart"，但本测试
// 显式断言这一点，防止未来接线 AutoYaST 时不小心复用了 AssembleKickstart。
func TestDispatch_SUSENotRoutedToKickstart(t *testing.T) {
	ctx := dispatchSUSECtx()
	out, err := RenderMenu(ctx)
	if err == nil && strings.Contains(out, "%packages") {
		t.Fatalf("SUSE 的菜单产出含 kickstart 特征 %%packages，说明被错误路由到了"+
			"RHEL 的 kickstart 分支，got:\n%s", out)
	}

	r := &Renderer{}
	answerOut, answerErr := r.RenderAnswerFile(ctx, "autoinst.xml")
	if answerErr == nil && strings.Contains(answerOut, "%packages") {
		t.Fatalf("SUSE 的应答文件产出含 kickstart 特征 %%packages，说明被错误路由到了"+
			"RHEL 的 kickstart 分支，got:\n%s", answerOut)
	}
	// 若未来某个改动让这条路径意外返回了 AssembleKickstart 的产出（哪怕
	// 不含 %packages 字面串），至少要求"不是 unimplementedFamilyError 时
	// 产出不能是空字符串却仍报错"这种自相矛盾的状态——即错误与非空输出
	// 不能同时出现。
	if answerErr != nil && answerOut != "" {
		t.Errorf("RenderAnswerFile(suse) 返回错误时输出应为空字符串，got %q", answerOut)
	}
}

// ---------------------------------------------------------------------------
// 未知家族仍然报明确错误（default 分支未被挤掉）
// ---------------------------------------------------------------------------

// TestDispatch_UnknownFamilyStillErrors 验证加了新 case 之后，default 分支
// （未知家族）仍然存在且报错，不会因为 switch 增加了分支就意外落进某个
// 具体家族的 case（Go 的 switch 不会有这种问题，但这是显式的回归防线：
// 若未来重构成 if/else 链，同样的错误可能悄悄发生）。
func TestDispatch_UnknownFamilyStillErrors(t *testing.T) {
	unknown := model.OSFamily("totally-unknown-family")

	menuCtx := model.RenderContext{
		Task:     model.Task{ID: "x1"},
		Artifact: model.ArtifactSet{OSFamily: unknown},
		Bases:    testBases,
	}
	if _, err := RenderMenu(menuCtx); err == nil {
		t.Error("RenderMenu(未知家族) 应返回错误，got nil")
	}

	r := &Renderer{}
	if _, err := r.RenderAnswerFile(menuCtx, "whatever"); err == nil {
		t.Error("RenderAnswerFile(未知家族) 应返回错误，got nil")
	}
}

// TestDispatch_UnimplementedFamilyStillErrors 验证 SUSE（唯一仍未实现的
// 家族）在两个入口都返回非空错误、空字符串输出——本轮接线 Windows/ESXi
// 之后，SUSE 的"未实现"防线必须继续生效，不能被误接线。
func TestDispatch_UnimplementedFamilyStillErrors(t *testing.T) {
	ctx := dispatchSUSECtx()

	out, err := RenderMenu(ctx)
	if err == nil {
		t.Errorf("RenderMenu(suse) 应返回错误（未实现），got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("RenderMenu(suse) 返回错误时输出应为空字符串，got %q", out)
	}

	r := &Renderer{}
	answerOut, answerErr := r.RenderAnswerFile(ctx, "autoinst.xml")
	if answerErr == nil {
		t.Errorf("RenderAnswerFile(suse) 应返回错误（未实现），got nil, out=%q", answerOut)
	}
	if answerOut != "" {
		t.Errorf("RenderAnswerFile(suse) 返回错误时输出应为空字符串，got %q", answerOut)
	}
}

// ---------------------------------------------------------------------------
// 既有四个家族的 golden file 不得变化
// ---------------------------------------------------------------------------
//
// 本文件不重复 golden_test.go 的断言逻辑——"跑一次 `go test
// ./internal/render/ -run Golden` 确认不变"是本轮验证步骤的一部分（记录在
// StructuredOutput 的 verifyCommands 里），不是本文件需要额外编写的测试
// 用例。golden_test.go 的既有测试（TestGolden_MinimalKickstart 等）在本
// 文件引入的分派改动下已经原样运行且通过（本轮验证已确认），这里不重复
// 造一份"再跑一次 golden 断言"的测试，那只是同一份断言的复制。
