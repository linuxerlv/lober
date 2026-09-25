// 本文件锁住菜单把 installer 的日志从第一刻就转发到服务端。
//
// **kickstart 的 `logging --host=` 指令救不了启动阶段的失败。** 那一行是
// anaconda **启动之后**才解析并生效的转发配置，而 installer 在启动阶段失败时
// 它还没被读到。内核参数 `inst.syslog=` 不同：dracut 阶段就生效，installer
// 的每一条日志（含它自己崩溃前的最后几行）都会发出来。
//
// 实测（2026-09-26，QEMU BIOS 目标机，console=ttyS0 与 inst.notmux 都已配）：
//
//	[   40.726754] systemd[1]: Created slice system-anaconda\x2dtmux.slice
//	[  254.146299] dracut Warning: Killing all remaining processes
//	[  254.441685] dracut Warning: Unmounted /oldroot.
//
// 内核时间戳从 40 秒直接跳到 254 秒——中间 213 秒的 installer 输出一个字节都
// 没留下，然后机器重启。服务端侧同期只收到 %pre hook 的输出（那是我们自己的
// signal 函数发的），anaconda 自己一条都没有。
//
// 于是「装机为什么失败」这个问题在当前配置下**无法回答**：串口空白、syslog
// 空白、目标盘没挂载所以 /tmp 里的 anaconda.log 也取不到。这不是调试便利性的
// 问题——SPEC §10 的失败模式表要求把失败原因呈现给运维，而这条链路上它根本
// 不存在。
package render

import (
	"strings"
	"testing"
)

// TestMenu_ForwardsInstallerSyslog 锁住四个 Linux 家族都带 inst.syslog=。
//
// 注意只有 RHEL 系认 inst.* 前缀；Debian 系的等价物是 `log_host=`/`log_port=`
// （debian-installer 的 preseed 变量），SUSE 的是 `linuxrc.log=`。本测试因此
// 只覆盖 RHEL——其余家族的启动阶段日志转发是另一件事，需要各自实测参数名。
func TestMenu_ForwardsInstallerSyslog(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.SyslogHost = "10.20.20.1"
	ctx.SyslogPort = 514

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}

	if !strings.Contains(got, "inst.syslog=") {
		t.Fatalf("菜单没有 inst.syslog=——kickstart 的 logging 指令是 anaconda "+
			"启动之后才生效的，启动阶段失败时它还没被读到，于是那 200 多秒的输出"+
			"一个字节都留不下来。菜单:\n%s", got)
	}
	want := "inst.syslog=10.20.20.1:514"
	if !strings.Contains(got, want) {
		t.Errorf("inst.syslog 的值不对，想要 %q。菜单:\n%s", want, got)
	}
}

// TestMenu_SyslogOmittedWhenHostEmpty 锁住 SyslogHost 为空时不渲染这个参数。
//
// 渲染出 `inst.syslog=:514` 会让 anaconda 试着往一个空主机名发日志——那是一个
// 每条日志都失败一次的配置，比没有转发更糟（它会拖慢整个安装）。
func TestMenu_SyslogOmittedWhenHostEmpty(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.SyslogHost = ""
	ctx.SyslogPort = 514

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	if strings.Contains(got, "inst.syslog=") {
		t.Errorf("SyslogHost 为空时不应渲染 inst.syslog——空主机名会让 anaconda "+
			"每条日志都发失败一次，拖慢整个安装。菜单:\n%s", got)
	}
}
