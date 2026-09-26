// 本文件锁住菜单给 installer 配了串口控制台。
//
// **没有它，装机过程全程不可观测。** 实测（2026-09-26，QEMU BIOS 目标机走
// 完整链路）：iPXE 的输出一直正常，直到
//
//	Probing EDD (edd=off to disable)... ok
//	<ESC>c<ESC>[?7l<ESC>[2J          ← 内核接管并清屏
//
// 此后串口一个字节都没有。anaconda 在跑，但它的输出全部送到 tty1（图形/虚拟
// 控制台），而 `-display none` 下那块屏幕没人看得到。于是它卡在装包前的某处
// 时，唯一能看到的只有「服务端没收到下一个信号」——既不知道它在等什么，也
// 不知道有没有报错。
//
// 这不是「调试不便」这一级的问题：真机排障同样如此。SPEC §0.1 把 iPXE 之前
// 的故障划为范围外、要求运维去 BMC 控制台看，而 BMC 的串口重定向拿到的正是
// ttyS0——内核不往那里写，BMC 上也是一片空白。
//
// 六个家族都需要，但各家的参数名不同（Linux 内核统一是 console=，而
// ESXi 的 mboot 与 WinPE 的 wimboot 各有自己的机制），本文件只覆盖四个 Linux
// 家族。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestMenu_LinuxFamiliesSetSerialConsole 锁住四个 Linux 家族的 kernel 行都带
// console=ttyS0。
func TestMenu_LinuxFamiliesSetSerialConsole(t *testing.T) {
	for _, tc := range []struct {
		family model.OSFamily
		ctx    func() model.RenderContext
	}{
		{model.OSFamilyRHEL, menuCtxRHEL},
		{model.OSFamilyDebian, menuCtxDebian},
		{model.OSFamilyUbuntu, menuCtxUbuntu},
		{model.OSFamilySUSE, menuCtxSUSE},
	} {
		got, err := RenderMenu(tc.ctx())
		if err != nil {
			t.Errorf("%s: RenderMenu 失败: %v", tc.family, err)
			continue
		}
		kernelLine := got
		if idx := strings.Index(got, "\ninitrd "); idx != -1 {
			kernelLine = got[:idx]
		}
		if !strings.Contains(kernelLine, "console=ttyS0") {
			t.Errorf("%s: kernel 行没有 console=ttyS0——installer 的输出会全部送到"+
				"tty1，串口与 BMC 上一片空白，卡住时无从诊断。kernel 行:\n%s",
				tc.family, kernelLine)
		}
	}
}

// TestMenu_SerialConsoleKeepsTTY0 锁住同时保留 tty0。
//
// `console=ttyS0 console=tty0` 两个都给时，内核把**最后一个**当主控制台
// （交互用），但**所有** console= 都会收到内核消息。顺序是 ttyS0 在前、tty0
// 在后：这样有物理显示器的真机仍然在屏幕上看到交互界面，而串口/BMC 侧同时
// 拿到完整日志。反过来写会让串口变成主控制台，物理机前的人看不到 installer
// 的提示。
func TestMenu_SerialConsoleKeepsTTY0(t *testing.T) {
	got, err := RenderMenu(menuCtxRHEL())
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	idxSerial := strings.Index(got, "console=ttyS0")
	idxTTY := strings.Index(got, "console=tty0")
	if idxTTY == -1 {
		t.Errorf("没有 console=tty0——只给串口会让有物理显示器的真机屏幕变黑，"+
			"机房里的人看不到 installer 的任何提示。菜单:\n%s", got)
	}
	if idxSerial != -1 && idxTTY != -1 && idxSerial > idxTTY {
		t.Error("console=ttyS0 应排在 console=tty0 之前：内核把最后一个 console= " +
			"当主控制台（交互用），顺序反了会让物理机前的人看不到 installer 界面")
	}
}

// TestMenu_KernelArgsCanOverrideConsole 锁住模板的 KernelArgs 排在默认控制台
// 参数之后。
//
// 内核对重复的 console= 取并集、最后一个为主控制台，所以模板想改主控制台
// （比如某机型的 BMC 走 ttyS1）只需在 KernelArgs 里再给一个——前提是它排在
// 默认值之后。这条锁住那个顺序。
func TestMenu_KernelArgsCanOverrideConsole(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.Template.KernelArgs = "console=ttyS1,115200n8"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	idxDefault := strings.Index(got, "console=ttyS0")
	idxOverride := strings.Index(got, "console=ttyS1,115200n8")
	if idxOverride == -1 {
		t.Fatalf("模板的 KernelArgs 没有出现在菜单里:\n%s", got)
	}
	if idxDefault != -1 && idxOverride < idxDefault {
		t.Error("模板的 KernelArgs 应排在默认 console= 之后，否则覆盖不生效" +
			"（内核取最后一个 console= 作主控制台）")
	}
}
