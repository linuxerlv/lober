// 本文件锁住 RHEL 菜单用 inst.cmdline 而不是 inst.text。
//
// 两者都是「非图形」，但**交互性完全不同**：
//
//	inst.text     文本 TUI。仍然是交互式的——anaconda 起 tmux、画界面、
//	              等键盘输入。kickstart 没覆盖到的问题它会停下来问。
//	inst.cmdline  非交互。输出直接往控制台打，遇到未应答的问题立刻失败退出，
//	              不等任何人。
//
// 全自动装机必须用后者。实测（2026-09-26，QEMU BIOS 目标机，串口已配好
// console=ttyS0 所以看得见）：inst.text 下 installer 起到
//
//	Created slice system-anaconda\x2dtmux.slice - Slice /system/anaconda-tmux.
//	Starting anaconda-pre.service - pre-anaconda logging service...
//	Starting plymouth-quit-wait.service - Hold until boot process finishes up...
//
// 就停住了，内核时间戳冻结在 39 秒，此后七分钟无任何输出。anaconda 在等一个
// 可交互的终端，而 `-display none` 的虚拟机（以及真实机房里没插显示器的服务器）
// 给不了它。
//
// SPEC §4.5 的示例写的是 inst.text，那是示例本身的问题——它没有在真机上跑过。
package render

import (
	"strings"
	"testing"
)

// TestRockyMenu_UsesCmdlineNotText 锁住用 inst.cmdline。
func TestRockyMenu_UsesCmdlineNotText(t *testing.T) {
	got, err := RenderMenu(menuCtxRHEL())
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}

	if !strings.Contains(got, "inst.cmdline") {
		t.Errorf("菜单没有 inst.cmdline——全自动装机需要非交互模式，"+
			"缺了它 anaconda 会等一个可交互终端（实测卡在 anaconda-tmux.slice "+
			"之后七分钟无输出）。菜单:\n%s", got)
	}
	if strings.Contains(got, "inst.text") {
		t.Errorf("菜单里还有 inst.text——它是交互式 TUI（起 tmux、画界面、"+
			"等键盘），与 inst.cmdline 冲突。菜单:\n%s", got)
	}
}

// TestRockyMenu_NoPlymouth 锁住关掉 plymouth。
//
// plymouth 是引导动画，它的 plymouth-quit-wait.service 语义是「等引导过程
// 结束」——无图形环境下那个条件不会满足，服务一直 Starting。同一次实测里它
// 与 anaconda-pre 一起挂着。
//
// 装机不需要引导动画，关掉它少一类挂起原因，也让串口输出干净（plymouth 会
// 抢控制台）。
func TestRockyMenu_NoPlymouth(t *testing.T) {
	got, err := RenderMenu(menuCtxRHEL())
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	if !strings.Contains(got, "plymouth.enable=0") {
		t.Errorf("菜单没有 plymouth.enable=0——plymouth-quit-wait.service 在无"+
			"图形环境下会一直等（实测与 anaconda-pre 一起挂着），且 plymouth 会"+
			"抢控制台让串口输出变乱。菜单:\n%s", got)
	}
}

// TestRockyMenu_CmdlineBeforeKernelArgs 锁住模板的 KernelArgs 仍排在最后。
//
// 运维想换回 inst.text 调试某台机器时，KernelArgs 里给 inst.text 应当能生效
// ——anaconda 对冲突的显示模式参数取最后一个。
func TestRockyMenu_CmdlineBeforeKernelArgs(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.Template.KernelArgs = "inst.text"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	idxCmdline := strings.Index(got, "inst.cmdline")
	idxText := strings.Index(got, "inst.text")
	if idxText == -1 {
		t.Fatalf("模板的 KernelArgs 没出现在菜单里:\n%s", got)
	}
	if idxCmdline > idxText {
		t.Error("inst.cmdline 应排在模板 KernelArgs 之前，否则运维想临时换回 " +
			"inst.text 调试时覆盖不生效（anaconda 对冲突的显示模式取最后一个）")
	}
}
