// 本文件锁住一条所有家族共有的要求：iPXE 菜单必须以 #!ipxe 开头。
//
// **这是让每一台真机都装不上的最后一道缺口，而它的症状与前一个（缺 boot
// 命令）完全相同。** iPXE 只有在脚本首行是 `#!ipxe` 时才把内容当脚本执行；
// 缺了它，chain 拿到的是一坨无法识别的数据——不报错、不执行、直接返回，
// 机器回落到固件引导顺序继续 PXE 轮询。
//
// 实测（2026-09-25，QEMU BIOS 目标机接真实服务端）：目标机反复取到 434 字节
// 的完整菜单（含 kernel/initrd/boot 三行，URL 全部正确、手动 curl 那几个
// URL 都是 200、14.5MB 与 105MB 都能下全），但 vmlinuz 与 initrd.img 的 GET
// 一次都没从目标机发出过：
//
//	13:09:03 10.20.20.112 "/v1/boot?...&arch=i386&plat=pcbios" 200 434
//	13:12:40 10.20.20.112 "/v1/boot?...&arch=i386&plat=pcbios" 200 434
//
// 对照组很说明问题：no_task 那条路径（contract.NoTaskScript）一直带
// `#!ipxe`，所以「查不到任务」时机器能正确执行 sanboot 回落；唯独**成功
// 路径**的菜单没有这一行——也就是说这个系统此前只有失败路径能被 iPXE 执行。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestRenderMenu_AllFamiliesStartWithShebang 锁住六个家族的菜单首行都是
// #!ipxe。
func TestRenderMenu_AllFamiliesStartWithShebang(t *testing.T) {
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

		if !strings.HasPrefix(got, "#!ipxe\n") {
			first := got
			if i := strings.IndexByte(got, '\n'); i >= 0 {
				first = got[:i]
			}
			t.Errorf("%s: 菜单首行是 %q，必须是 #!ipxe——iPXE 只有首行是它才把"+
				"内容当脚本执行，缺了它 chain 拿到一坨无法识别的数据，不报错、"+
				"不执行、直接返回，机器回落 PXE 轮询。完整菜单:\n%s",
				tc.family, first, got)
		}
	}
}

// TestRenderMenu_ShebangIsFirstLineNotJustPresent 锁住它在**首行**，不是出现
// 在中间某处。
//
// iPXE 对这一行的要求是严格的首行匹配，写在第二行等于没写。单独测一条是
// 因为「包含」与「以之开头」这两种断言很容易写混，而写混的后果是测试绿着、
// 机器照样装不上。
func TestRenderMenu_ShebangIsFirstLineNotJustPresent(t *testing.T) {
	got, err := RenderMenu(menuCtxRHEL())
	if err != nil {
		t.Fatalf("RenderMenu 失败: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) == 0 || lines[0] != "#!ipxe" {
		t.Errorf("首行必须精确等于 #!ipxe（不能有前导空格或 BOM），实际首行 %q",
			lines[0])
	}
	// 不该重复出现——两行 #!ipxe 不会让 iPXE 报错，但说明有人在两个地方
	// 各加了一次，其中一处迟早会被删掉。
	if n := strings.Count(got, "#!ipxe"); n != 1 {
		t.Errorf("#!ipxe 出现了 %d 次，应恰好 1 次", n)
	}
}
