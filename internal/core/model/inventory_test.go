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
