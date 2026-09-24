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

// DeliveryChannel 是制品集的交付通道（SPEC §11 Phase 3、P3-03 任务 1）。
//
// 两条通道**并存，不是替代关系**：
//
//   - installer 驱动（本系统 Phase 1 起就有的那条）是**兜底**，覆盖新 OS
//     版本（镜像还没封装好）、特殊机型的驱动问题（要在 installer 的 shell 里
//     现场调）、以及装机失败要停下来看现场（§2.3 的失败处置策略）。
//   - 镜像直写是**主力**，覆盖批量装机与「装出来的系统逐台一致」。
//
// 镜像直写做不到上面那三件事，所以兜底通道不能废——这是 SPEC §11 Phase 3
// 明确写下的分工，不是实现上的将就。
//
// 做成 ArtifactSet 上的一个维度而不是另建一套模型：后者会让任务、模板、
// 制品集这三层各自分叉（P3-03 任务 1 原话）。加上这一维之后选集逻辑从四元组
// 变成五元组（家族、版本、架构、固件、通道）。
type DeliveryChannel string

const (
	// ChannelInstaller 是 installer 驱动通道。**它同时是零值的语义**，见
	// Normalize 与 IsInstaller。
	ChannelInstaller DeliveryChannel = "installer"

	// ChannelImageWrite 是镜像直写通道。
	ChannelImageWrite DeliveryChannel = "image-write"
)

// Normalize 把零值折叠成 ChannelInstaller，其余取值原样返回。
//
// **零值等价于 installer 驱动是一条兼容性硬要求**（P3-03 任务 1）：Phase 1 与
// Phase 2 导入的制品集在库里这一列是空字符串，零值若不等价于 installer，那些
// 制品集升级后会变成「通道未知」、选集一个都匹配不上——后果是升级完成的瞬间
// 所有装机任务开始失败，而原因不会出现在任何日志里（查询本身没报错，只是查不到）。
//
// 未知取值**不**折叠成 installer：静默折叠会让一个拼错的通道名被当成兜底通道
// 使用，而运维以为自己选的是镜像直写。拼写校验归 Valid()，本方法只管零值这
// 一件事。
func (c DeliveryChannel) Normalize() DeliveryChannel {
	if c == "" {
		return ChannelInstaller
	}
	return c
}

// IsInstaller 判定是否为 installer 驱动通道（零值算是）。
func (c DeliveryChannel) IsInstaller() bool {
	return c.Normalize() == ChannelInstaller
}

// IsImageWrite 判定是否为镜像直写通道。
func (c DeliveryChannel) IsImageWrite() bool {
	return c.Normalize() == ChannelImageWrite
}

// deliveryChannels 是 DeliveryChannel 的白名单。取值来自用户输入（模板上选
// 哪条通道），必须白名单校验——放过一个拼错的取值会让建任务走到一条不存在的
// 通道上，而那个错误要到渲染阶段才暴露。
var deliveryChannels = map[DeliveryChannel]bool{
	ChannelInstaller:  true,
	ChannelImageWrite: true,
}

// Valid 判定该通道取值是否合法。空字符串合法（等价于 installer，见 Normalize）。
func (c DeliveryChannel) Valid() bool {
	return deliveryChannels[c.Normalize()]
}

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

// repoIndexPattern 是仓库索引入口矩阵：家族 → 「这份仓库对 installer 真正
// 可用」的判据文件（相对仓库目录的 glob）。
//
// 为什么需要按家族分流：各家包管理器读的索引入口完全不同。
//
//   - RHEL 与 SUSE 都是 RPM-MD 格式（dnf/yum 与 zypper 共用同一套仓库元
//     数据），入口是 repodata/repomd.xml。
//   - Debian 与 Ubuntu 都是 APT，入口是 dists/<suite>/Release。**suite 名
//     随版本变**（bookworm、trixie、noble、sid……），用户可能导入任何一版，
//     所以这里是 glob 而不是写死某一个 suite 名。
//
// 这张表修的是一个曾在真机上无条件必然触发的阻断性 bug：判据原先是散落在
// 三处的同一条常量 repodata/repomd.xml（RPM-MD 专属），而 ExtractDebian
// 产出的 repo/ 下是 dists/ 与 pool/ 两棵 APT 树，从不会有 repodata/ 这个
// 目录。制品完整性检查挡在签发装机 token 之前（§4.6），于是**任何** Debian
// 任务在机器真正引导时都会被判定为「制品文件不存在」而拒绝，与该制品集导入
// 得多完整、多正确无关。见 docs/real-machine/12-e2e-debian.md。
//
// 规则放在 model 而不是各调用方本地：它是 OS 家族的固有属性（跟
// firmwareCompat 同一性质），而消费它的有三处——
// internal/statemachine/precheck.go 的引导时校验、cmd/lober/preflight.go 的
// 启动自检、cmd/lober/verifier.go 的 verify 端点。**那条常量此前就是被复制
// 了三份**，一处修好另两处照旧，正是 bug 能长期存在的原因。
//
// Windows 与 ESXi 刻意不在表内：两者的 ArtifactPaths.Repo 恒为空串（只产出
// Extra 里的一组文件），调用方对空路径直接跳过，走不到这张表。
//
// Ubuntu 在表内，尽管 ExtractUbuntu 目前产出的 Repo 也是空串（Ubuntu 的
// squashfs 介质没有可浏览的仓库树，见 imports/extract_ubuntu.go）——配一条
// 是零成本的前瞻：Ubuntu 本身是 APT 系，server 介质上确实带 dists/ 树，将来
// 若让抽取器填上 Repo，有条目就走对的判据，没条目则会撞上调用方「无法确认」
// 的硬拒绝，白白多一轮排查。
var repoIndexPattern = map[OSFamily]string{
	OSFamilyRHEL:   "repodata/repomd.xml",
	OSFamilySUSE:   "repodata/repomd.xml",
	OSFamilyDebian: "dists/*/Release",
	OSFamilyUbuntu: "dists/*/Release",
}

// RepoIndexPattern 返回某 OS 家族的仓库索引入口判据（相对仓库目录的 glob）。
//
// ok 为 false 表示本表没有该家族的条目。调用方**不应**据此放行——「无法确认
// 仓库可用」与「确认仓库不可用」在 §4.6 那一层应当同样处置（§9.1 默认拒绝），
// 区分两者只是为了给运维不同的错误文案：前者是「漏配判据」，后者是「仓库真的
// 缺索引」。与 FirmwareCompatible 对未知家族的处置方向一致：不静默接受。
func RepoIndexPattern(family OSFamily) (pattern string, ok bool) {
	pattern, ok = repoIndexPattern[family]
	return pattern, ok
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

	// Channel 是交付通道（P3-03 任务 1），把选集从四元组扩成五元组。
	//
	// **零值等价于 ChannelInstaller**，这是兼容性要求：Phase 1 与 Phase 2
	// 导入的制品集这一列为空，不能因为加了这个字段就全部选不中。读的时候
	// 一律经 Channel.Normalize() 或 MatchesChannel，不要直接用 == 比较。
	Channel DeliveryChannel

	// Image 是镜像直写通道专有的三项（镜像文件、校验值与签名、分区布局）。
	// installer 驱动通道的制品集此字段为 nil。
	//
	// 用指针而不是内嵌值：nil 与「零值结构体」在语义上不同，前者是「这条
	// 通道不需要这些字段」，后者会让「镜像 sha256 是空字符串」这种非法状态
	// 看起来像是合法数据。导入侧对镜像直写通道校验三项齐备，缺任一则导入
	// 失败（P3-03 任务 1 的测试要求）。
	Image *ImageArtifact

	SourceISOSHA256 string
	State           ArtifactSetState
	ManifestSHA256  string
	Paths           ArtifactPaths

	// FailReason 是导入失败时的具体原因（本次契约变更新增，t01 记录的
	// 契约缺口：此前 State 只能置 failed，运维不知道是路径打不开、识别不出
	// 家族、抽取失败还是 manifest 生成失败，处置方式对每一种都不同）。
	// 非 failed 状态下本字段通常为空字符串——空字符串表示「没有失败原因」，
	// 不是一个需要特殊解析的哨兵值。写回走
	// contract.ArtifactSetStore.UpdateArtifactSetState 的新增第四个参数。
	FailReason string
}

// ImageArtifact 是镜像直写通道的制品三项（P3-03 任务 1）。字段与
// pipeline/README.md 第 2 节定义的「四件套」交付物逐一对应——那份文档是外网
// 封装侧与内网写盘侧之间的唯一接口，两边字段名必须对得上。
type ImageArtifact struct {
	// ImagePath 是压缩后镜像文件相对制品集根目录的路径（四件套里的
	// image.raw.zst）。格式是 raw 块设备镜像加 zstd 压缩：raw 的字节序列本身
	// 就是目标盘的最终内容，不需要额外一层格式解析；qcow2 之类在写盘前要先
	// qemu-img convert，那一步必然落地临时文件，与「不落地」的要求冲突。
	ImagePath string

	// SHA256 是**压缩后**镜像文件的校验值，导入时校验「传输到内网的文件没有
	// 被篡改」。
	//
	// 与 UncompressedSHA256 是两个不同阶段、不同目的的校验，**即使都叫
	// sha256 也不能共用一个值**（pipeline/README.md 第 2.2 节）：压缩算法
	// 版本不同会让压缩字节不同而解压内容相同，共用一个值会在那种场景下误报，
	// 或者反过来在真正的传输损坏时因为比对了错误的值而漏检。
	SHA256 string

	// UncompressedSHA256 是**解压后**原始镜像的校验值，供写盘端回读校验
	// （P3-03 任务 4 第 3 步）比对「写到盘上的字节没有出错」。
	UncompressedSHA256 string

	// SizeBytes / UncompressedSizeBytes 分别是压缩后与解压后的字节数。后者
	// 用来判断目标盘容量是否足够，以及分区扩展的起始边界。
	SizeBytes             int64
	UncompressedSizeBytes int64

	// PartitionLayout 是镜像内部的分区结构，写盘完成后按它扩展分区。
	//
	// 这一项的存在理由：镜像是固定大小的，而目标机器的盘大小不一；写完要扩展
	// 根分区到整盘，而怎么扩展取决于镜像内部的分区结构——**这个信息只有封装时
	// 知道，所以要随镜像一起交付**（P3-03 任务 1）。
	PartitionLayout PartitionLayout

	// SignaturePath 是清单签名文件的相对路径（manifest.yaml.sig）。
	//
	// 签的是**清单**不是镜像本身（§9.6、P3-03 任务 3 第 1 条）：镜像有几 GB，
	// 签它要完整读一遍；清单里已经有镜像的 sha256，签清单等于间接签了镜像，
	// 而清单只有几 KB。
	SignaturePath string

	// ManifestPath 是清单文件本身的相对路径（manifest.yaml），签名校验时与
	// SignaturePath 配对使用。
	ManifestPath string
}

// PartitionLayout 描述镜像内部的分区结构。
//
// pipeline/manifest/manifest.schema.json 刻意只占位了顶层字段名，把具体子字段
// 留给消费者定义（「提前设计扩展分区算法要用的具体数据形状容易设计错，应由
// 消费者驱动」）——本类型就是那个定义。
type PartitionLayout struct {
	// TableType 是分区表类型，取值 gpt 或 mbr。mkosi 产出的镜像固定是 gpt
	// （legacy-free，只支持 EFI 引导，见 pipeline/README.md 第 1 节的核实
	// 结论），但仍显式记录，不假设消费者知道这条约束。
	TableType string

	// Partitions 是分区列表，按镜像内的分区号升序。
	Partitions []Partition
}

// Partition 是分区布局里的一个分区。
type Partition struct {
	// Number 是分区号（1 起），对应 /dev/<disk>p<N> 或 <disk><N>。
	Number int

	// Filesystem 是文件系统类型（ext4 / xfs / vfat 等）。扩展分区时要用它
	// 决定调用哪个增长工具（resize2fs vs xfs_growfs），两者不通用。
	Filesystem string

	// Role 标识这个分区的用途。只有 RootPartition 那一个会被扩展到整盘，
	// 其余原样保留——盲目扩展 ESP 或 swap 会破坏镜像。
	Role string
}

// 分区角色的取值。
const (
	// PartitionRoleRoot 是要被扩展到整盘的根分区。一份布局里应当**恰好有
	// 一个**——零个意味着无从扩展，多个意味着不知道该扩哪个，两种都是封装侧
	// 的错误，导入时就该拒绝而不是在装机到一半时才发现。
	PartitionRoleRoot = "root"

	// PartitionRoleESP 是 EFI 系统分区，固定大小，不扩展。
	PartitionRoleESP = "esp"

	// PartitionRoleSwap 是交换分区，不扩展。
	PartitionRoleSwap = "swap"

	// PartitionRoleOther 是其余不需要特殊处理的分区。
	PartitionRoleOther = "other"
)

// RootPartition 返回布局里的根分区。ok 为假表示没有恰好一个根分区——调用方
// 应当据此拒绝，而不是猜一个。
func (l PartitionLayout) RootPartition() (p Partition, ok bool) {
	var found Partition
	count := 0
	for _, part := range l.Partitions {
		if part.Role == PartitionRoleRoot {
			found = part
			count++
		}
	}
	if count != 1 {
		return Partition{}, false
	}
	return found, true
}

// MatchesChannel 判定该制品集是否属于指定通道。
//
// want 为零值时视为**不按通道过滤**，返回真——这是四元组查询向后兼容的判据：
// Phase 1/2 的调用方不传这一维，行为必须与加字段之前完全一致。
//
// 比较两侧都先 Normalize：库里的历史制品集这一列是空字符串，而调用方多半传
// 显式的 ChannelInstaller，不归一化就会漏掉全部历史数据。
func (a ArtifactSet) MatchesChannel(want DeliveryChannel) bool {
	if want == "" {
		return true
	}
	return a.Channel.Normalize() == want.Normalize()
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
