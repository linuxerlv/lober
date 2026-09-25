// 本文件锁住一条所有家族共有的要求：iPXE 菜单必须以 boot 命令结尾。
//
// **这是一个让每一台真机都装不上、而服务端侧看起来一切正常的缺口。**
// iPXE 的 kernel / initrd 命令只是把文件加载进内存并设置引导参数，真正
// 把控制权交给内核的是 boot。没有它，iPXE 加载完镜像就走到脚本末尾，
// chain 返回后落到 `|| shell`（/boot.ipxe 的写法）或直接退出，机器回到
// 固件引导顺序继续 PXE 轮询。
//
// 现场症状是「机器在 PXE 里反复转圈」——而 SPEC §0.1 恰好把这类现象划为
// 范围外（判据在固件与 DHCP 里，本系统零可观测性），于是排查方向会被带到
// BIOS 设置、DHCP 配置、网线上去。同时服务端侧的一切指标都正常：任务已
// 进 booting、nginx access log 里菜单请求 200、字节数也对。
//
// 实测（2026-09-25，QEMU BIOS 目标机走真实引导链）：目标机成功取到
// /boot.ipxe 与 /v1/boot（429 字节的菜单，不是 76 字节的 no_task），此后
// 再无任何制品请求——vmlinuz 与 initrd.img 的 GET 一次都没出现过。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestRenderMenu_AllFamiliesEndWithBoot 锁住每个已接线家族的菜单都以 boot
// 结尾。
//
// 用 RenderMenu 这个统一入口而不是各家族的内部函数：boot 命令是「菜单」这
// 个产物的整体要求，不是某一家的细节，从分派入口测能保证将来新增家族时
// 这条约束自动适用。
func TestRenderMenu_AllFamiliesEndWithBoot(t *testing.T) {
	for _, tc := range []struct {
		family model.OSFamily
		ctx    func() model.RenderContext
	}{
		{model.OSFamilyRHEL, menuCtxRHEL},
		{model.OSFamilyDebian, menuCtxDebian},
		{model.OSFamilyUbuntu, menuCtxUbuntu},
		{model.OSFamilySUSE, menuCtxSUSE},
		{model.OSFamilyWindows, menuCtxWindows},
		{model.OSFamilyESXi, menuCtxESXi},
	} {
		got, err := RenderMenu(tc.ctx())
		if err != nil {
			t.Errorf("%s: RenderMenu 失败: %v", tc.family, err)
			continue
		}

		trimmed := strings.TrimRight(got, "\n")
		lines := strings.Split(trimmed, "\n")
		last := strings.TrimSpace(lines[len(lines)-1])
		if last != "boot" {
			t.Errorf("%s: 菜单最后一行是 %q，必须是 boot——没有它 iPXE 只加载"+
				"镜像却不执行，机器会退回固件引导顺序继续 PXE 轮询。完整菜单:\n%s",
				tc.family, last, got)
		}
	}
}

// TestRenderMenu_BootComesAfterInitrd 锁住 boot 出现在 initrd 之后。
//
// iPXE 是顺序执行的：boot 之前必须已经 kernel + initrd 都加载完，否则它会
// 拿一个不完整的引导上下文去跳转。这条与上一条不重复——上一条只管「有没有
// boot」，这条管「位置对不对」。
func TestRenderMenu_BootComesAfterInitrd(t *testing.T) {
	for _, tc := range []struct {
		family model.OSFamily
		ctx    func() model.RenderContext
	}{
		{model.OSFamilyRHEL, menuCtxRHEL},
		{model.OSFamilyDebian, menuCtxDebian},
		{model.OSFamilyUbuntu, menuCtxUbuntu},
		{model.OSFamilySUSE, menuCtxSUSE},
		{model.OSFamilyWindows, menuCtxWindows},
		{model.OSFamilyESXi, menuCtxESXi},
	} {
		got, err := RenderMenu(tc.ctx())
		if err != nil {
			t.Errorf("%s: RenderMenu 失败: %v", tc.family, err)
			continue
		}
		// ESXi 没有 initrd 行：它的引导器是 mboot.efi，用 `-c <boot.cfg URL>`
		// 传配置，模块列表写在 boot.cfg 里而不是菜单里。位置约束对它无从谈起，
		// 「boot 必须存在」已由上一个测试覆盖。
		if tc.family == model.OSFamilyESXi {
			continue
		}
		idxInitrd := strings.LastIndex(got, "initrd ")
		idxBoot := strings.LastIndex(got, "boot")
		if idxInitrd == -1 {
			t.Errorf("%s: 菜单里没有 initrd 行:\n%s", tc.family, got)
			continue
		}
		if idxBoot < idxInitrd {
			t.Errorf("%s: boot 出现在 initrd 之前——iPXE 顺序执行，那会拿一个"+
				"不完整的引导上下文跳转。完整菜单:\n%s", tc.family, got)
		}
	}
}

// menuCtxRHEL / menuCtxDebian / menuCtxUbuntu 造各家族最小可渲染上下文。
func menuCtxBase(family model.OSFamily, kernel string, initrd []string, repo string) model.RenderContext {
	return model.RenderContext{
		Task:         model.Task{ID: "task-menu-1"},
		InstallToken: "tok-menu-1",
		Artifact: model.ArtifactSet{
			ID: "as-menu-1", OSFamily: family, Arch: model.ArchX86_64,
			Firmware: model.FirmwareAny,
			Paths:    model.ArtifactPaths{Kernel: kernel, Initrd: initrd, Repo: repo},
		},
		Bases: model.BaseURLs{
			GatewayBase:  "http://gw.internal",
			ArtifactBase: "http://ng.internal/artifacts",
		},
		ActualFirmware: model.FirmwareBIOS,
	}
}

func menuCtxRHEL() model.RenderContext {
	return menuCtxBase(model.OSFamilyRHEL, "vmlinuz", []string{"initrd.img"}, "repo/BaseOS/")
}

func menuCtxDebian() model.RenderContext {
	return menuCtxBase(model.OSFamilyDebian, "linux", []string{"initrd.gz"}, "repo/")
}

func menuCtxUbuntu() model.RenderContext {
	return menuCtxBase(model.OSFamilyUbuntu, "vmlinuz", []string{"initrd"}, "repo/")
}

func menuCtxSUSE() model.RenderContext {
	return menuCtxBase(model.OSFamilySUSE, "boot/x86_64/loader/linux",
		[]string{"boot/x86_64/loader/initrd"}, "repo/")
}

// menuCtxWindows 的固件必须与制品集一致：RenderWindowsMenu 自己做固件硬
// 校验（§4.6 第二项，winpe.go:49），不一致会返回错误而不是菜单。
func menuCtxWindows() model.RenderContext {
	ctx := menuCtxBase(model.OSFamilyWindows, "", nil, "")
	ctx.Artifact.Firmware = model.FirmwareBIOS
	ctx.ActualFirmware = model.FirmwareBIOS
	ctx.Artifact.Paths.Extra = map[string]string{
		"wimboot": "wimboot", "BCD": "BCD",
		"boot.sdi": "boot.sdi", "boot.wim": "boot.wim",
	}
	return ctx
}

// menuCtxESXi 必须是 UEFI：ESXi 仅支持 UEFI（§0.3），RenderESXiMenuLine 对
// BIOS 返回 ErrESXiRequiresUEFI。
func menuCtxESXi() model.RenderContext {
	ctx := menuCtxBase(model.OSFamilyESXi, "", nil, "")
	ctx.Artifact.Firmware = model.FirmwareUEFI
	ctx.ActualFirmware = model.FirmwareUEFI
	ctx.Artifact.Paths.Extra = map[string]string{"mboot.efi": "mboot.efi"}
	return ctx
}
