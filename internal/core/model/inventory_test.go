package model

import "testing"

// TestFirmwareCompatible_NineCombinations 逐一验证「家族 × 固件」组合的
// 兼容判定，特别是 esxi + bios 必须为假（SPEC §0.3）。
func TestFirmwareCompatible_NineCombinations(t *testing.T) {
	cases := []struct {
		family   OSFamily
		firmware FirmwareMode
		want     bool
	}{
		{OSFamilyRHEL, FirmwareBIOS, true},
		{OSFamilyRHEL, FirmwareUEFI, true},
		{OSFamilyDebian, FirmwareBIOS, true},
		{OSFamilyDebian, FirmwareUEFI, true},
		{OSFamilyUbuntu, FirmwareBIOS, true},
		{OSFamilyUbuntu, FirmwareUEFI, true},
		{OSFamilySUSE, FirmwareBIOS, true},
		{OSFamilySUSE, FirmwareUEFI, true},
		{OSFamilyWindows, FirmwareBIOS, true},
		{OSFamilyWindows, FirmwareUEFI, true},
		{OSFamilyESXi, FirmwareUEFI, true},
		{OSFamilyESXi, FirmwareBIOS, false}, // 最危险的一条：MVP 不覆盖 BIOS+ESXi
	}
	for _, c := range cases {
		got := FirmwareCompatible(c.family, c.firmware)
		if got != c.want {
			t.Errorf("FirmwareCompatible(%q, %q) = %v, want %v —— "+
				"SPEC §0.3 的固件兼容矩阵是硬约束，esxi+bios 组合若被误判为兼容，"+
				"会让网关把根本无法引导的制品发给 BIOS 机器",
				c.family, c.firmware, got, c.want)
		}
	}
}

// TestFirmwareCompatible_UnknownFamilyRejected 验证未知 OS 家族不被静默
// 接受（SPEC §0.3、计划任务 6 测试要求）。
func TestFirmwareCompatible_UnknownFamilyRejected(t *testing.T) {
	unknown := OSFamily("plan9")
	if FirmwareCompatible(unknown, FirmwareUEFI) {
		t.Errorf("FirmwareCompatible(%q, uefi) = true, want false —— "+
			"未知家族必须被拒绝，静默接受会让配置错误的 artifact_set 被当作合法组合", unknown)
	}
}

// TestDeliveryChannel_ZeroValueIsInstaller 验证通道字段的零值等价于
// installer 驱动通道。
//
// 这是 P3-03 任务 1 的**兼容性要求**：Phase 1 与 Phase 2 导入的全部制品集
// 都没有这个字段，反序列化出来是空字符串。零值必须等价于 installer 驱动，
// 否则那些已有制品集会突然变成「通道未知」，选集逻辑一个都匹配不上——
// 而那意味着升级之后所有装机任务立刻全部失败，且原因不在任何日志里。
func TestDeliveryChannel_ZeroValueIsInstaller(t *testing.T) {
	var zero DeliveryChannel
	if !zero.IsInstaller() {
		t.Errorf("DeliveryChannel(\"\").IsInstaller() = false, want true —— " +
			"零值必须等价于 installer 驱动通道，否则 Phase 1/2 导入的制品集" +
			"（库里那一列是空字符串）会在升级后全部选不中，所有装机任务立刻失败")
	}
	if zero.IsImageWrite() {
		t.Errorf("DeliveryChannel(\"\").IsImageWrite() = true, want false")
	}

	// 显式取值各自成立。
	if !ChannelInstaller.IsInstaller() || ChannelInstaller.IsImageWrite() {
		t.Errorf("ChannelInstaller 的两个判定不自洽")
	}
	if !ChannelImageWrite.IsImageWrite() || ChannelImageWrite.IsInstaller() {
		t.Errorf("ChannelImageWrite 的两个判定不自洽")
	}
}

// TestDeliveryChannel_Normalize 验证归一化把零值折叠成 installer，供存储层
// 与选集逻辑用同一个值比较。
//
// 有了 Normalize，「空字符串」与「installer」这两种表示同一件事的写法不会
// 在比较时分叉——否则 `set.Channel == ChannelInstaller` 这种直觉写法会漏掉
// 全部历史制品集，而 `set.Channel.IsInstaller()` 又要求每个调用点都记得用
// 方法而不是等号。
func TestDeliveryChannel_Normalize(t *testing.T) {
	cases := []struct {
		in   DeliveryChannel
		want DeliveryChannel
	}{
		{"", ChannelInstaller},
		{ChannelInstaller, ChannelInstaller},
		{ChannelImageWrite, ChannelImageWrite},
		// 未知取值原样返回，不静默折叠成 installer：静默折叠会让一个拼错的
		// 通道名被当成兜底通道用，而运维以为自己选的是镜像直写。校验由
		// Valid() 负责，Normalize 只管零值这一件事。
		{DeliveryChannel("typo"), DeliveryChannel("typo")},
	}
	for _, c := range cases {
		if got := c.in.Normalize(); got != c.want {
			t.Errorf("DeliveryChannel(%q).Normalize() = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDeliveryChannel_Valid 验证白名单校验：两个合法取值加零值通过，其余拒绝。
func TestDeliveryChannel_Valid(t *testing.T) {
	for _, ch := range []DeliveryChannel{"", ChannelInstaller, ChannelImageWrite} {
		if !ch.Valid() {
			t.Errorf("DeliveryChannel(%q).Valid() = false, want true", ch)
		}
	}
	for _, ch := range []DeliveryChannel{"image", "installer-driven", "IMAGE_WRITE", "dd"} {
		if ch.Valid() {
			t.Errorf("DeliveryChannel(%q).Valid() = true, want false —— "+
				"通道取值来自用户输入（模板上选），必须白名单校验；"+
				"放过一个拼错的取值会让建任务走到一个不存在的通道上", ch)
		}
	}
}

// TestArtifactSet_MatchesChannel 验证五元组选集的第五维：同一 OS 版本的两条
// 通道制品集能被分别选中，且不带通道条件的查询（历史四元组调用方）仍然工作。
//
// P3-03 任务 1 的选集要求：现在是五元组（家族、版本、架构、固件、通道），
// 模板要能指定用哪条通道——同一个 OS 版本可能两条通道都有制品。
func TestArtifactSet_MatchesChannel(t *testing.T) {
	// 历史制品集：Channel 字段是零值（Phase 1/2 导入的那些）。
	legacy := ArtifactSet{Firmware: FirmwareAny}
	imageWrite := ArtifactSet{Firmware: FirmwareAny, Channel: ChannelImageWrite}

	if !legacy.MatchesChannel(ChannelInstaller) {
		t.Errorf("历史制品集（Channel 为零值）匹配 installer 通道失败 —— " +
			"向后兼容要求它们被当作 installer 驱动通道")
	}
	if legacy.MatchesChannel(ChannelImageWrite) {
		t.Errorf("历史制品集匹配上了 image-write 通道 —— 那会让「选镜像直写」" +
			"错误命中一份没有镜像文件的 installer 制品集，装机走到一半才发现")
	}
	if !imageWrite.MatchesChannel(ChannelImageWrite) {
		t.Errorf("镜像直写制品集匹配 image-write 通道失败")
	}
	if imageWrite.MatchesChannel(ChannelInstaller) {
		t.Errorf("镜像直写制品集匹配上了 installer 通道")
	}

	// 不带通道条件（传零值）时视为「不按通道过滤」——历史四元组调用方不传
	// 这一维，行为必须与加字段之前一致。
	if !legacy.MatchesChannel("") || !imageWrite.MatchesChannel("") {
		t.Errorf("传零值通道时应视为不过滤，两条通道的制品集都该匹配 —— " +
			"这是四元组查询向后兼容的判据")
	}
}

// TestRepoIndexPattern_PerFamilyPackageManagerFormat 验证仓库索引入口按
// 家族给出各自包管理器的格式，而不是一条通吃的常量。
//
// 这条锁的是一个曾在真机上无条件必然触发的阻断性 bug：判据曾被硬编码成
// repodata/repomd.xml（RPM-MD 专属），而 Debian/Ubuntu 的仓库树是 APT 的
// dists/<suite>/，从不会有那个文件。制品完整性检查挡在签发装机 token 之前
// （§4.6），于是**任何** Debian 任务在机器真正引导时都会被判定为「制品文件
// 不存在」而拒绝。见 docs/real-machine/12-e2e-debian.md。
//
// Debian/Ubuntu 那条必须是 glob：suite 名随版本变（bookworm、trixie、
// noble、sid……），写死某一版就是把这个 bug 换个形式留下来。
func TestRepoIndexPattern_PerFamilyPackageManagerFormat(t *testing.T) {
	cases := []struct {
		family  OSFamily
		want    string
		wantOK  bool
		because string
	}{
		{OSFamilyRHEL, "repodata/repomd.xml", true, "dnf/yum 读 RPM-MD 元数据入口"},
		{OSFamilySUSE, "repodata/repomd.xml", true, "zypper 与 RHEL 共用 RPM-MD 格式"},
		{OSFamilyDebian, "dists/*/Release", true, "APT 读 dists/<suite>/Release，suite 名随版本变"},
		{OSFamilyUbuntu, "dists/*/Release", true, "Ubuntu 同为 APT 系"},
		{OSFamilyWindows, "", false, "Windows 制品无仓库树，Repo 恒为空串"},
		{OSFamilyESXi, "", false, "ESXi 制品只产出 Extra 里的一组文件"},
	}
	for _, c := range cases {
		got, ok := RepoIndexPattern(c.family)
		if ok != c.wantOK || got != c.want {
			t.Errorf("RepoIndexPattern(%q) = (%q, %v), want (%q, %v) —— %s",
				c.family, got, ok, c.want, c.wantOK, c.because)
		}
	}
}

// TestRepoIndexPattern_UnknownFamilyNotOK 验证未知家族返回 ok=false，让调用
// 方能把「无法确认仓库可用」与「确认仓库不可用」区分开——两者在 §4.6 那一层
// 都应当拒绝，但错误文案不同，运维需要知道是「配置漏了判据」还是「仓库真的
// 缺索引」。与 FirmwareCompatible 对未知家族的处置方向一致：不静默接受。
func TestRepoIndexPattern_UnknownFamilyNotOK(t *testing.T) {
	if got, ok := RepoIndexPattern(OSFamily("plan9")); ok {
		t.Errorf("RepoIndexPattern(\"plan9\") = (%q, true), want ok=false —— "+
			"未知家族不能静默给出某个判据，那会让一份来源不明的仓库树被当作合法", got)
	}
}

// TestArtifactSet_MatchesFirmware 验证制品集的固件匹配：any 匹配两者、
// 指定固件只匹配自己（计划任务 6 设计要点）。
func TestArtifactSet_MatchesFirmware(t *testing.T) {
	anySet := ArtifactSet{Firmware: FirmwareAny}
	if !anySet.MatchesFirmware(FirmwareBIOS) {
		t.Error("firmware=any 的制品集 MatchesFirmware(bios) = false, want true —— any 必须对两种固件都放宽匹配")
	}
	if !anySet.MatchesFirmware(FirmwareUEFI) {
		t.Error("firmware=any 的制品集 MatchesFirmware(uefi) = false, want true —— any 必须对两种固件都放宽匹配")
	}

	uefiSet := ArtifactSet{Firmware: FirmwareUEFI}
	if !uefiSet.MatchesFirmware(FirmwareUEFI) {
		t.Error("firmware=uefi 的制品集 MatchesFirmware(uefi) = false, want true")
	}
	if uefiSet.MatchesFirmware(FirmwareBIOS) {
		t.Error("firmware=uefi 的制品集 MatchesFirmware(bios) = true, want false —— " +
			"指定固件的制品集不应匹配另一种固件，否则 Windows 的两套 WinPE 制品会被互相错发")
	}
}

// TestParseIPXEBuildArch 验证 ${buildarch} 的解析与对无法映射值的拒绝
// （SPEC §4.1、§4.6，附录 B）。
func TestParseIPXEBuildArch(t *testing.T) {
	cases := []struct {
		raw  string
		want Arch
	}{
		{"x86_64", ArchX86_64},
		{"arm64", ArchARM64},
	}
	for _, c := range cases {
		got, err := ParseIPXEBuildArch(c.raw)
		if err != nil {
			t.Errorf("ParseIPXEBuildArch(%q) 返回 error: %v, want %q", c.raw, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("ParseIPXEBuildArch(%q) = %q, want %q", c.raw, got, c.want)
		}
	}

	if _, err := ParseIPXEBuildArch("mips"); err == nil {
		t.Error("ParseIPXEBuildArch(\"mips\") 未返回 error, want error —— " +
			"无法映射的取值必须报错而不是静默放行，否则 §4.6 的 arch 一致性校验会失去意义")
	}
}

// TestParseIPXEPlatform 验证 ${platform} 的解析与对无法映射值的拒绝。
func TestParseIPXEPlatform(t *testing.T) {
	cases := []struct {
		raw  string
		want FirmwareMode
	}{
		{"pcbios", FirmwareBIOS},
		{"efi", FirmwareUEFI},
	}
	for _, c := range cases {
		got, err := ParseIPXEPlatform(c.raw)
		if err != nil {
			t.Errorf("ParseIPXEPlatform(%q) 返回 error: %v, want %q", c.raw, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("ParseIPXEPlatform(%q) = %q, want %q", c.raw, got, c.want)
		}
	}

	if _, err := ParseIPXEPlatform("arm_uboot"); err == nil {
		t.Error("ParseIPXEPlatform(\"arm_uboot\") 未返回 error, want error —— " +
			"无法映射的取值必须报错，否则 §4.6 的固件模式校验会把未知取值当作某个已知值处理")
	}
}

// TestParseIPXEBuildArch_RealWorldValues 锁住 iPXE ${buildarch} 的真实取值。
//
// **i386 是实机核实出来的取值，不是推测。** 2026-09-25 在 QEMU BIOS 目标机
// 上实测：dnsmasq 按 option 93 = 0 发 undionly.kpxe（Debian ipxe 包提供的
// 32 位 BIOS 构建），那个 iPXE 实例的 ${buildarch} 报 `i386`，于是
// /v1/boot 拿它去比对 x86_64 的制品集，返回 no_task——机器在 PXE 里转圈，
// 服务端不留任何记录（§4.2 约定 no_task 不写事件表）。
//
// 原实现只认 x86_64 与 arm64，错误信息里写着「取值待实机核实，见 SPEC
// 附录 B」——现在核实了。
//
// 为什么 i386 应当映射成 ArchX86_64 而不是新增一个架构：SPEC §4.6 第 1 条
// 要的语义是「x86_64 制品别发给 arm64 机器，kernel 直接加载失败」，而 32 位
// iPXE **能**加载 64 位内核（实机正是这么跑的：undionly.kpxe 引导
// x86_64 的 vmlinuz）。buildarch 描述的是 iPXE 二进制自己的位宽，不是 CPU
// 或目标 OS 的位宽，用它区分 x86_64/i386 会把一个不存在的不兼容判成不兼容。
func TestParseIPXEBuildArch_RealWorldValues(t *testing.T) {
	for raw, want := range map[string]Arch{
		// 64 位 BIOS/UEFI 构建（snponly.efi、ipxe.efi）。
		"x86_64": ArchX86_64,
		// 32 位 BIOS 构建（undionly.kpxe）——实机报的就是这个值。
		"i386":  ArchX86_64,
		"arm64": ArchARM64,
		// iPXE 源码里 arm64 构建也用 aarch64 这个别名。
		"aarch64": ArchARM64,
	} {
		got, err := ParseIPXEBuildArch(raw)
		if err != nil {
			t.Errorf("ParseIPXEBuildArch(%q) 返回错误: %v——这是 iPXE 真实会报的值", raw, err)
			continue
		}
		if got != want {
			t.Errorf("ParseIPXEBuildArch(%q) = %q，想要 %q", raw, got, want)
		}
	}
}

// TestParseIPXEBuildArch_UnknownStillRejected 锁住「认不出的值仍然报错」。
//
// 放宽 i386 不等于放宽一切：真出现一个没见过的架构（比如 riscv64）时，
// 静默当成 x86_64 会让 kernel 在目标机上直接加载失败，那是一个服务端毫无
// 记录的失败，只能到 BMC 控制台看。
func TestParseIPXEBuildArch_UnknownStillRejected(t *testing.T) {
	for _, raw := range []string{"riscv64", "loong64", "", "X86_64", "amd64"} {
		if _, err := ParseIPXEBuildArch(raw); err == nil {
			t.Errorf("ParseIPXEBuildArch(%q) 应报错——认不出的架构不能静默当成 x86_64", raw)
		}
	}
}
