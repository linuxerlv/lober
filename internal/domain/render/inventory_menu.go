package render

import (
	"strings"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// InventoryMenuRequest 是渲染采集菜单要的全部输入。采集时机器上还没有任务，所以这里没有
// Snapshot：只有网关基地址、制品基地址、主机 SN 与一次性令牌。
type InventoryMenuRequest struct {
	Gateway, Artifacts string
	SN, Token          string
	// Kernel 与 Initrd 是制品目录下的相对路径，取自部署配置。
	Kernel, Initrd string
}

// RenderInventoryMenu 产出采集镜像的 iPXE 引导行。
//
// 令牌走内核命令行（lober.inventory_token=），不走 URL 查询串：iPXE 的 kernel 行里放查询串
// 会被安装器或固件当成自己的参数，前面已经为 Ubuntu 的 ds= 踩过一次。采集脚本从 /proc/cmdline
// 读它。
//
// 三项都是必需的：缺任何一项直接报问题，不回落到空地址——引导到一半才发现地址是空的，
// 机器会卡在没有串口输出的地方。
func RenderInventoryMenu(r InventoryMenuRequest) ([]byte, []model.Problem, error) {
	subject := model.Subject{Kind: model.KindHost, ID: r.SN, Path: "inventory"}
	for _, field := range []struct{ name, value string }{
		{"gateway", r.Gateway}, {"artifacts", r.Artifacts}, {"kernel", r.Kernel}, {"initrd", r.Initrd},
	} {
		if strings.TrimSpace(field.value) == "" {
			p := model.NewProblem("render_inventory_missing", subject, field.name)
			return nil, []model.Problem{p}, nil
		}
	}

	kernel := catalog.ArtifactURL(r.Artifacts, r.Kernel)
	initrd := catalog.ArtifactURL(r.Artifacts, r.Initrd)
	// console= 两项都留：虚拟机的串口日志靠 ttyS0，物理机的显示器靠 tty0。缺哪个都会让
	// 采集中途出问题时无从查起。
	const console = "console=tty0 console=ttyS0,115200n8"

	var b strings.Builder
	b.WriteString("#!ipxe\n")
	b.WriteString("kernel " + kernel + " ip=dhcp \\\n")
	b.WriteString("  lober.gateway=" + r.Gateway + " \\\n")
	b.WriteString("  lober.sn=" + r.SN + " \\\n")
	b.WriteString("  lober.inventory_token=" + r.Token + " \\\n")
	b.WriteString("  " + console + "\n")
	b.WriteString("initrd " + initrd + "\n")
	b.WriteString("boot\n")
	return []byte(b.String()), nil, nil
}
