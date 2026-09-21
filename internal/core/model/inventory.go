// 本文件定义清单类型与固件兼容矩阵：Host、OSFamily、FirmwareMode、Arch、
// ArtifactSet 及相关解析函数。对应实现计划任务 6，依据 SPEC §0.3、§2.1、
// §4.3、§4.5、§4.6。
package model

import "fmt"

// OSFamily 是六个目标 OS 家族（§0.3）。
type OSFamily string

const (
	OSFamilyRHEL    OSFamily = "rhel"
	OSFamilyDebian  OSFamily = "debian"
	OSFamilyUbuntu  OSFamily = "ubuntu"
	OSFamilySUSE    OSFamily = "suse"
	OSFamilyWindows OSFamily = "windows"
	OSFamilyESXi    OSFamily = "esxi"
)

// FirmwareMode 是固件模式。any 只有制品集会取——它表示「这套制品 BIOS 与
// UEFI 通吃」，Host 上报的固件模式永远是 bios 或 uefi 之一，不会是 any。
type FirmwareMode string

const (
	FirmwareBIOS FirmwareMode = "bios"
	FirmwareUEFI FirmwareMode = "uefi"
	FirmwareAny  FirmwareMode = "any"
)

// Arch 是两个目标架构。
type Arch string

const (
	ArchX86_64 Arch = "x86_64"
	ArchARM64  Arch = "arm64"
)

// firmwareCompat 是固件兼容矩阵：家族 × 固件模式 → 是否支持（§0.3）。
//
// Linux 四家 BIOS 与 UEFI 都支持（一套制品通吃）；Windows 两种都支持但需
// 按固件导两套制品；esxi 只支持 UEFI —— BIOS 路径需要 syslinux 的
// mboot.c32 加 iPXE 的 COMBOOT 支持，MVP 明确排除（附录 A.14）。
var firmwareCompat = map[OSFamily]map[FirmwareMode]bool{
	OSFamilyRHEL:    {FirmwareBIOS: true, FirmwareUEFI: true},
	OSFamilyDebian:  {FirmwareBIOS: true, FirmwareUEFI: true},
	OSFamilyUbuntu:  {FirmwareBIOS: true, FirmwareUEFI: true},
	OSFamilySUSE:    {FirmwareBIOS: true, FirmwareUEFI: true},
	OSFamilyWindows: {FirmwareBIOS: true, FirmwareUEFI: true},
	OSFamilyESXi:    {FirmwareBIOS: false, FirmwareUEFI: true},
}

// FirmwareCompatible 判定某 OS 家族是否支持某固件模式（§0.3、§4.6）。
//
// 未知家族返回 false——不被静默接受。firmwareMode 必须是 bios 或 uefi
// 之一（Host 的实际上报值），传入 any 没有意义，同样返回 false。
func FirmwareCompatible(family OSFamily, firmware FirmwareMode) bool {
	modes, ok := firmwareCompat[family]
	if !ok {
		return false
	}
	return modes[firmware]
}

// Host 是已发现主机清单的一行（§4.3）。
type Host struct {
	// 三个查找键，按 SN → MAC → UUID 优先级回退匹配。
	SN   string
	MAC  string
	UUID string

	// Arch 与 Firmware 是 discovery 阶段记录的固件与架构，用于建任务时
	// 过滤可选 OS 列表；引导时仍要按机器实际上报值再校验一遍（§4.3 末条、
	// §4.6）——两处用途不同，不能互相替代。
	Arch     Arch
	Firmware FirmwareMode

	Rack       string // 机架位，建任务确认框的必显字段之一（§3.2）
	LastSeenAt int64  // 上次被发现时间，Unix 秒；同为必显字段，供运维核对是否为目标机
}

// ArtifactSetState 是制品集的生命周期状态。
type ArtifactSetState string

const (
	ArtifactSetImporting  ArtifactSetState = "importing"
	ArtifactSetReady      ArtifactSetState = "ready"
	ArtifactSetFailed     ArtifactSetState = "failed"
	ArtifactSetDeprecated ArtifactSetState = "deprecated"
)

// ArtifactPaths 是制品集内各文件的相对路径（§2.1）。所有路径相对于制品
// 根目录、不含 scheme 与主机名——拼 URL 是渲染器的事（§1.5）。
type ArtifactPaths struct {
	Kernel string
	Initrd []string
	Repo   string
	Extra  map[string]string // esxi: mboot.efi / boot.cfg 模板；windows: wimboot / BCD / boot.sdi / boot.wim
}

// ArtifactSet 是一次 ISO 导入产出的制品集（§2.1）。
type ArtifactSet struct {
	ID   string
	Name string

	OSFamily  OSFamily
	OSVersion string
	Arch      Arch

	// Firmware 承载固件维度：Linux 四家取 any（一套制品通吃），Windows 取
	// 具体值（同一版本导两个制品集），ESXi 只有 uefi。把固件做成字段而非
	// paths.extra 里的分键，选集逻辑就变成对四元组的一次查表。
	Firmware FirmwareMode

	SourceISOSHA256 string
	State           ArtifactSetState
	ManifestSHA256  string
	Paths           ArtifactPaths
}

// MatchesFirmware 判定该制品集能否发给某固件模式的机器。any 对两种固件
// 都返回真；指定固件只匹配自己（§2.1、计划任务 6 设计要点）。
func (a ArtifactSet) MatchesFirmware(firmware FirmwareMode) bool {
	if a.Firmware == FirmwareAny {
		return true
	}
	return a.Firmware == firmware
}

// iPXE 上报的 ${buildarch} 与 ${platform} 原始字符串值。
//
// 取值待实机核实（附录 B：iPXE 变量拼写与可用性需对照 ipxe.org/cfg 逐架构
// 实测）。此处采用 iPXE 项目公开文档与 SPEC §4.5 引导脚本示例中出现的
// 拼写（buildarch=x86_64、platform=pcbios/efi）作为已知取值；解析函数集中
// 在这一处，将来发现新取值时只需在这里补一个分支。
const (
	ipxeBuildArchX86_64 = "x86_64"
	ipxeBuildArchARM64  = "arm64"

	ipxePlatformPCBIOS = "pcbios"
	ipxePlatformEFI    = "efi"
)

// ParseIPXEBuildArch 把 iPXE 的 ${buildarch} 原始字符串映射成 Arch 枚举。
// 无法映射时返回 error——不能静默吞掉未知取值，那会让不兼容的制品被发给
// 机器（§4.6 的 arch 一致性校验依赖这个映射的准确性）。
func ParseIPXEBuildArch(raw string) (Arch, error) {
	switch raw {
	case ipxeBuildArchX86_64:
		return ArchX86_64, nil
	case ipxeBuildArchARM64:
		return ArchARM64, nil
	default:
		return "", fmt.Errorf("model: 无法识别的 iPXE buildarch 取值 %q（取值待实机核实，见 SPEC 附录 B）", raw)
	}
}

// ParseIPXEPlatform 把 iPXE 的 ${platform} 原始字符串映射成 FirmwareMode
// 枚举。无法映射时返回 error（§4.6 的固件模式兼容校验依赖这个映射的准确性）。
func ParseIPXEPlatform(raw string) (FirmwareMode, error) {
	switch raw {
	case ipxePlatformPCBIOS:
		return FirmwareBIOS, nil
	case ipxePlatformEFI:
		return FirmwareUEFI, nil
	default:
		return "", fmt.Errorf("model: 无法识别的 iPXE platform 取值 %q（取值待实机核实，见 SPEC 附录 B）", raw)
	}
}
