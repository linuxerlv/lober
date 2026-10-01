package catalog

import "slices"

// Arch 是系统的 CPU 架构，取 x86_64 或 aarch64。ISO 导入时识别出它，主机可以登记它；
// iPXE 上报的是自己的构建架构，要经 ArchFromIPXE 换算成它才能比对。
type Arch string

// Firmware 是主机的固件，取 bios 或 uefi。iPXE 上报的平台要经 FirmwareFromIPXE 换算成它。
type Firmware string

// 架构与固件的取值，连同 iPXE 上报值到系统取值的对应关系，都只在下面几张表里定义一次：
// 旧系统出过 BIOS 机器的 iPXE 报 i386、被判不兼容的事（d014d50）。标识会写进主机登记、
// ISO 记录和数据库，发布后不能改名；显示名只给界面看。

type platformArchRow struct {
	id    Arch
	label string
	ipxe  []string // iPXE 的 ${buildarch} 里换算成本架构的取值
}

type platformFirmwareRow struct {
	id    Firmware
	label string
	ipxe  string // iPXE 的 ${platform} 里换算成本固件的取值
}

// i386 也换算成 x86_64 是刻意的：BIOS 下的 iPXE 是 32 位构建，在 x86_64 机器上也报 i386。
// 代价是认不出真正的 32 位机器，系统本来就不支持它们。
var platformArchTable = []platformArchRow{
	{"x86_64", "x86_64", []string{"x86_64", "i386"}},
	{"aarch64", "aarch64", []string{"arm64"}},
}

// platformFirmwareTable 的顺序就是 Series.Firmwares 返回的顺序。
var platformFirmwareTable = []platformFirmwareRow{
	{"bios", "BIOS", "pcbios"},
	{"uefi", "UEFI", "efi"},
}

// platformFirmwareLimitTable 只登记对固件有要求的系，没登记的系两种固件都能装。
// 目前只有 ESXi：它只走 UEFI（spec §2.1）。
var platformFirmwareLimitTable = []struct {
	series    Series
	firmwares []Firmware
}{
	{"esxi", []Firmware{"uefi"}},
}

// Valid 报告 a 是不是系统的架构，规则同 Family.Valid：区分大小写、不做规范化。
// iPXE 的上报值（i386、arm64）不算，要先经 ArchFromIPXE 换算。
func (a Arch) Valid() bool {
	_, ok := platformArchRowOf(a)
	return ok
}

// Label 是架构的界面显示名；未知的架构返回空串。
func (a Arch) Label() string {
	r, _ := platformArchRowOf(a)
	return r.label
}

// Valid 报告 f 是不是系统的固件，规则同 Arch.Valid；iPXE 的上报值（pcbios、efi）不算。
func (f Firmware) Valid() bool {
	_, ok := platformFirmwareRowOf(f)
	return ok
}

// Label 是固件的界面显示名；未知的固件返回空串。
func (f Firmware) Label() string {
	r, _ := platformFirmwareRowOf(f)
	return r.label
}

// ArchFromIPXE 把 iPXE 上报的构建架构换算成系统的架构，网关引导时用它。只认 iPXE 原样的
// 写法；换算不了时返回 false，由网关拒绝这次引导。
func ArchFromIPXE(buildarch string) (Arch, bool) {
	for _, r := range platformArchTable {
		if slices.Contains(r.ipxe, buildarch) {
			return r.id, true
		}
	}
	return "", false
}

// FirmwareFromIPXE 把 iPXE 上报的平台换算成系统的固件，规则同 ArchFromIPXE。
func FirmwareFromIPXE(platform string) (Firmware, bool) {
	for _, r := range platformFirmwareTable {
		if r.ipxe == platform {
			return r.id, true
		}
	}
	return "", false
}

// Firmwares 返回系 s 能装的固件，顺序同 platformFirmwareTable，每次都是新切片；
// 未知的系返回空。
func (s Series) Firmwares() []Firmware {
	if !s.Valid() {
		return nil
	}
	limit, limited := platformFirmwareLimitOf(s)
	var out []Firmware
	for _, r := range platformFirmwareTable {
		if !limited || slices.Contains(limit, r.id) {
			out = append(out, r.id)
		}
	}
	return out
}

// PlatformOK 判断系 s、架构为 isoArch 的 ISO 能不能装到这台主机上，架构与固件分开给出，
// 调用方好分别报原因。建任务时的准入判定和引导时的网关都调它，C28 的两次校验就是同一套规则。
//
// 主机的架构或固件为空时，那一项算通过：建任务时主机可能没登记，C28 是登记了才校验；
// 引导时网关传入的是换算后的实际值，不会为空。非空却不合法的值比不上任何 ISO，算不通过。
// 系或 ISO 架构未知时没有可比的对象，两项都不通过。
func PlatformOK(s Series, isoArch, hostArch Arch, hostFW Firmware) (archOK, fwOK bool) {
	if !s.Valid() || !isoArch.Valid() {
		return false, false
	}
	archOK = hostArch == "" || hostArch == isoArch
	fwOK = hostFW == "" || slices.Contains(s.Firmwares(), hostFW)
	return archOK, fwOK
}

func platformArchRowOf(a Arch) (platformArchRow, bool) {
	for _, r := range platformArchTable {
		if r.id == a {
			return r, true
		}
	}
	return platformArchRow{}, false
}

func platformFirmwareRowOf(f Firmware) (platformFirmwareRow, bool) {
	for _, r := range platformFirmwareTable {
		if r.id == f {
			return r, true
		}
	}
	return platformFirmwareRow{}, false
}

func platformFirmwareLimitOf(s Series) ([]Firmware, bool) {
	for _, r := range platformFirmwareLimitTable {
		if r.series == s {
			return r.firmwares, true
		}
	}
	return nil, false
}
