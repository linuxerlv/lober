// 本文件锁住 BIOS 固件下的 biosboot 分区。
//
// **BIOS 机器要从 GPT 磁盘启动必须有一个 1MiB 的 biosboot 分区**，GRUB 把
// core.img 写在那里（MBR 的 446 字节装不下它，GPT 又没有 MBR 后面那段空隙）。
// 缺了它 anaconda 的存储校验直接失败。
//
// 这是本轮实测中最后一个、也是最难定位的缺口——因为它的症状指向别处：
//
//	ValidationReport(error_messages=["Your BIOS-based system needs a special
//	partition to boot from a GPT disk label. To continue, please create a
//	1MiB 'biosboot' type partition on the vda disk."])
//
// 校验失败后 anaconda 把这个错误挂在 SummaryHub（安装摘要屏）上等人处理：
//
//	Input is required by ScreenData(SummaryHub,None,False) screen
//
// inst.text 下它就停在那里；inst.cmdline 下它直接退出重启。两种模式的表象
// 完全不同（一个挂着、一个重启），而服务端侧看到的都只是「packages 阶段超时」
// ——目标盘 192KB、包 0 个、repodata 索引全部 200。
//
// 拿到这条错误信息花了六轮尝试：串口（只有内核消息）、kickstart 的 logging
// 指令（anaconda 启动前不生效）、内核 inst.syslog（同样没输出）、
// systemd.journald.forward_to_console（格式变了但没有 anaconda 的行）、
// 最小化 kickstart 排除渲染器（同样卡住，证明不是我们的内容问题）、最后靠
// inst.sshd 登进 installer 读 /tmp/anaconda.log 才看到。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestKickstart_BIOSNeedsBiosbootPartition 锁住 BIOS 固件下渲染出 biosboot。
func TestKickstart_BIOSNeedsBiosbootPartition(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyLargest
	ctx.ActualFirmware = model.FirmwareBIOS
	ctx.Facts[model.FactDiskPrimary] = "/dev/vda"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}

	if !strings.Contains(got, "biosboot") {
		t.Fatalf("BIOS 固件下没有 biosboot 分区——GRUB 的 core.img 在 GPT 盘上"+
			"没有地方放，anaconda 的存储校验会直接失败（实测错误原文：\"Your "+
			"BIOS-based system needs a special partition to boot from a GPT disk "+
			"label\"），然后挂在安装摘要屏等人处理。kickstart:\n%s", got[:700])
	}
	// 1MiB 是 anaconda 明确要求的大小。
	if !strings.Contains(got, "--size=1") {
		t.Errorf("biosboot 分区应是 1MiB（anaconda 的错误信息明确要求 1MiB）。"+
			"kickstart:\n%s", got[:700])
	}
}

// TestKickstart_UEFINeedsESPNotBiosboot 锁住 UEFI 下是 ESP 而不是 biosboot。
//
// 两种固件要的是不同的东西：UEFI 要 /boot/efi 的 EFI 系统分区，BIOS 要
// biosboot。给错了或两个都给，anaconda 会报另一类校验错误。
func TestKickstart_UEFINeedsESPNotBiosboot(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyLargest
	ctx.ActualFirmware = model.FirmwareUEFI
	ctx.Facts[model.FactDiskPrimary] = "/dev/vda"

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	if !strings.Contains(got, "/boot/efi") {
		t.Errorf("UEFI 固件下应有 /boot/efi 分区:\n%s", got[:700])
	}
	if strings.Contains(got, "biosboot") {
		t.Errorf("UEFI 固件下不应有 biosboot（那是 BIOS 从 GPT 盘启动才需要的）:\n%s",
			got[:700])
	}
}

// TestKickstart_ByHookDocumentsBiosbootRequirement 锁住 by-hook 的说明注释里
// 写明了这条要求。
//
// by-hook 下分区指令由运维写的 hook 产出，服务端管不着——但**必须告诉他们要
// 建 biosboot**，否则每个人都要自己踩一遍这个六轮才定位到的坑。注释写在
// kickstart 正文里，anaconda 会把它存成 /root/anaconda-ks.cfg，是失败现场唯一
// 能直接拿到的证据。
func TestKickstart_ByHookDocumentsBiosbootRequirement(t *testing.T) {
	ctx := minimalKickstartCtx()
	ctx.Template.DiskPolicy = model.DiskPolicyByHook
	ctx.ActualFirmware = model.FirmwareBIOS
	delete(ctx.Facts, model.FactDiskPrimary)

	got, err := AssembleKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleKickstart 失败: %v", err)
	}
	if !strings.Contains(got, "biosboot") {
		t.Errorf("by-hook + BIOS 的 %%include 说明里应提到 biosboot 要求——否则"+
			"每个写 hook 的人都要自己踩一遍这个坑。kickstart:\n%s", got[:900])
	}
}
