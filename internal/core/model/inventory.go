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

	// Disks 是这台机器的磁盘清单，供建任务时的磁盘规划决策使用。
	//
	// **为什么挂在 Host 上而不是 Facts 里**：contract.FactStore 按 taskID 存
	// facts，而建任务时任务还不存在——那是个循环依赖。而磁盘规划必须在建任务
	// 时完成（决策要固化，SPEC §2.6），所以清单只能来自主机记录。
	//
	// 两个来源：
	//
	//   - 手工录入（POST /hosts 的 disks 字段）。SPEC §7.4 的「discovery 尚未
	//     覆盖到的机器」兜底路径，本字段当前唯一的写入方。
	//   - inspect 阶段的硬件采集（SPEC §11 Phase 2）。RenderInspectScript 已经
	//     写好但还没接进任何应答文件，接通后由它回填。
	//
	// 为空时，除 by-hook 之外的四个磁盘策略都会在建任务时以「磁盘清单为空」
	// 失败——那是刻意的：first/largest/by-model/raid1-ssd 都要求服务端已知盘，
	// 猜一个出来会让机器擦错盘（SPEC §10 失败模式 5）。
	Disks []HostDisk
}

// HostDisk 是主机磁盘清单里的一块盘。
//
// 字段与 contract.Disk 对应（磁盘规划器的输入类型），但刻意不复用那个类型：
// contract 层不该依赖 model，而 model.Host 又要能被 store 序列化，两者的
// 生命周期与演进节奏不同。转换在 console 的建任务路径上做。
type HostDisk struct {
	// ByIDPath 是持久化的设备标识（/dev/disk/by-id/... 或 WWN），**必填**。
	//
	// 不用内核名（/dev/sda）：那个名字在不同启动之间会漂移，写进分区指令可能
	// 擦错盘（SPEC §10 失败模式 5）。空值会让磁盘规划器选中一块没有路径的盘，
	// 然后渲染出空的 --ondisk=。
	ByIDPath string

	SizeBytes int64

	// Rotational 区分机械盘与固态盘，raid1-ssd 策略靠它选盘。
	Rotational bool

	Model string
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

	// MediaRoot 是**安装介质根**的相对路径，anaconda 的 inst.repo 指向它。
	//
	// 它与 Repo 是两个语义不同的路径，混用会让装机在取 stage2 时 404：
	//
	//	MediaRoot  安装介质根，其下有 images/install.img（installer 的 stage2
	//	           运行时镜像）与 .treeinfo。anaconda 的 inst.repo 要这个。
	//	Repo       包索引所在目录，其下有 repodata/repomd.xml。precheck 的制品
	//	           完整性闸门（RepoIndexPattern）与包管理器要这个。
	//
	// 真实 RHEL 9 系布局下两者**不同**：repodata 在 BaseOS/ 下、images/ 在
	// 介质根。老式扁平布局（RHEL 7 系与精简介质，repodata 直接在 ISO 根）下
	// 两者相同。
	//
	// 实测（2026-09-25）：只有 Repo 一个字段时，菜单把 inst.repo 指向
	// repo/BaseOS/，anaconda 在其下找不到 stage2：
	//
	//	".../repo/BaseOS//images/install.img"   404
	//	".../repo/BaseOS//LiveOS/squashfs.img"  404
	//	".../repo/BaseOS//.treeinfo"            404
	//
	// 任务停在 installing/pre 不再前进。
	//
	// **零值等价于 Repo**，这是兼容性要求：Phase 1/2 导入的制品集这一列为空，
	// 读的时候一律经 MediaRootOrRepo()，不要直接用。
	MediaRoot string
	Extra     map[string]string // esxi: mboot.efi / boot.cfg 模板；windows: wimboot / BCD / boot.sdi / boot.wim
}

// ArtifactIdentity 是一次导入从 ISO 元数据里判定出的制品集身份，供
// contract.ArtifactSetStore.SetArtifactSetContent 一次写入。
//
// **这个类型的存在理由是一个真实的链路断裂。** 在它出现之前，导入路径只写
// OSFamily 与 Paths，Arch 与 Firmware 永远保持零值——而 statemachine 的
// precheck 拿机器上报的 arch 与 ArtifactSet.Arch 直接比对（checkArch），
// 拿 MatchesFirmware 判固件，两道闸门对空值必然拒绝。于是真实导入产出的
// 制品集能进到 ready，却在每一次 /v1/boot 被拒。
//
// 这个缺口此前被七个 e2e 脚本完整掩盖：它们全都绕过导入，用 psql 直接
// UPDATE artifact_sets 把 arch 与 firmware 填成 x86_64/any（见
// scripts/e2e/lib-db.sh 的 db_artifact_ready），所以「导入管线已接上」这个
// 结论一直只在单元测试层成立。
type ArtifactIdentity struct {
	OSFamily OSFamily

	// OSVersion 是信息性字段，不参与选集与校验（选集看三元组：家族、架构、
	// 固件），失败诊断与控制台展示用。
	OSVersion string

	// Arch 参与 precheck 的第一道硬拒绝，必须非空。判定不出时导入应当
	// 直接失败，不留空值——见 imports.ErrArchUndetectable 的文档。
	Arch Arch

	// Firmware 参与 precheck 的第二道硬拒绝。取值规则见 ArtifactSet.Firmware
	// 的字段文档：Linux 四家 any，Windows 取实测位宽，ESXi 恒为 uefi。
	Firmware FirmwareMode
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

	// FailReason 是导入失败时的具体原因（本次契约变更新增，t01 记录的
	// 契约缺口：此前 State 只能置 failed，运维不知道是路径打不开、识别不出
	// 家族、抽取失败还是 manifest 生成失败，处置方式对每一种都不同）。
	// 非 failed 状态下本字段通常为空字符串——空字符串表示「没有失败原因」，
	// 不是一个需要特殊解析的哨兵值。写回走
	// contract.ArtifactSetStore.UpdateArtifactSetState 的新增第四个参数。
	FailReason string
}

// MediaRootOrRepo 返回安装介质根，MediaRoot 为空时回落到 Repo。
//
// 一律经这个方法读，不要直接用 MediaRoot：Phase 1/2 导入的制品集那一列是
// 空字符串，直接用会渲染出空的 inst.repo——机器会拿到 `inst.repo=` 然后在
// 找不到任何介质的情况下失败，而那个失败发生在目标机上、只能到 BMC 控制台看。
func (p ArtifactPaths) MediaRootOrRepo() string {
	if p.MediaRoot != "" {
		return p.MediaRoot
	}
	return p.Repo
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

	// ipxeBuildArchI386 是 **32 位 BIOS 构建的 iPXE** 报的 buildarch
	// （undionly.kpxe 就是这种构建，Debian 的 ipxe 包提供的正是它）。
	// 2026-09-25 在 QEMU BIOS 目标机上实测确认。
	ipxeBuildArchI386 = "i386"

	// ipxeBuildArchAArch64 是 arm64 构建的另一种拼写（iPXE 源码里两种都有）。
	ipxeBuildArchAArch64 = "aarch64"

	ipxePlatformPCBIOS = "pcbios"
	ipxePlatformEFI    = "efi"
)

// ParseIPXEBuildArch 把 iPXE 的 ${buildarch} 原始字符串映射成 Arch 枚举。
// 无法映射时返回 error——不能静默吞掉未知取值，那会让不兼容的制品被发给
// 机器（§4.6 的 arch 一致性校验依赖这个映射的准确性）。
func ParseIPXEBuildArch(raw string) (Arch, error) {
	switch raw {
	case ipxeBuildArchX86_64, ipxeBuildArchI386:
		// i386 与 x86_64 都映射到 ArchX86_64：buildarch 描述的是 **iPXE
		// 二进制自己的位宽**，不是 CPU 或目标 OS 的位宽。32 位 iPXE 能加载
		// 64 位内核——实机正是这么跑的（undionly.kpxe 引导 x86_64 的
		// vmlinuz，2026-09-25 实测）。
		//
		// 而 §4.6 第 1 条 arch 闸门要挡的是「x86_64 制品发给 arm64 机器，
		// kernel 直接加载失败」这种真实的不兼容。用 buildarch 区分
		// i386/x86_64 会把一个不存在的不兼容判成不兼容：机器拿到 no_task、
		// 在 PXE 里反复轮询，而服务端按 §4.2 的约定不写任何事件表记录，
		// 现场只能看到「机器转圈」，排查无从下手。
		return ArchX86_64, nil
	case ipxeBuildArchARM64, ipxeBuildArchAArch64:
		return ArchARM64, nil
	default:
		// 认不出的值仍然报错。放宽 i386 不等于放宽一切：真出现没见过的架构
		// （riscv64、loong64）时静默当成 x86_64，会让 kernel 在目标机上直接
		// 加载失败——那是一个服务端毫无记录的失败，只能到 BMC 控制台看。
		return "", fmt.Errorf("model: 无法识别的 iPXE buildarch 取值 %q"+
			"（已知取值：x86_64 / i386 均为 64 位 x86 平台，arm64 / aarch64 为 arm64；"+
			"新增架构时需同时确认 dnsmasq 的 option 93 映射，见 SPEC §1.3）", raw)
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
