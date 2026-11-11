package catalog

import (
	"slices"
	"testing"
)

// platformArchIDs 与 platformFirmwareIDs 遍历实现里的表：公开接口没有列出全部架构或
// 全部固件的函数，「一个不多」只能这样查，断言仍走公开接口。
func platformArchIDs() []Arch {
	var out []Arch
	for _, r := range platformArchTable {
		out = append(out, r.id)
	}
	return out
}

func platformFirmwareIDs() []Firmware {
	var out []Firmware
	for _, r := range platformFirmwareTable {
		out = append(out, r.id)
	}
	return out
}

func Test架构与固件的取值与显示名(t *testing.T) {
	// 标识会写进主机登记、ISO 记录和数据库，发布后不能改名，所以期望值写死在测试里。
	// 固件的顺序也钉住：它就是 Firmwares 返回的顺序。
	arches := []struct {
		id    Arch
		label string
	}{
		{"x86_64", "x86_64"},
		{"aarch64", "aarch64"},
	}
	var wantArches []Arch
	for _, c := range arches {
		wantArches = append(wantArches, c.id)
		if !c.id.Valid() || c.id.Label() != c.label {
			t.Errorf("架构 %q：Valid() = %v、Label() = %q，想要 true、%q", c.id, c.id.Valid(), c.id.Label(), c.label)
		}
	}
	if got := platformArchIDs(); !slices.Equal(got, wantArches) {
		t.Errorf("目录里的架构 = %q，想要恰好 %q", got, wantArches)
	}

	firmwares := []struct {
		id    Firmware
		label string
	}{
		{"bios", "BIOS"},
		{"uefi", "UEFI"},
	}
	var wantFirmwares []Firmware
	for _, c := range firmwares {
		wantFirmwares = append(wantFirmwares, c.id)
		if !c.id.Valid() || c.id.Label() != c.label {
			t.Errorf("固件 %q：Valid() = %v、Label() = %q，想要 true、%q", c.id, c.id.Valid(), c.id.Label(), c.label)
		}
	}
	if got := platformFirmwareIDs(); !slices.Equal(got, wantFirmwares) {
		t.Errorf("目录里的固件 = %q，想要恰好 %q", got, wantFirmwares)
	}
}

func Test未知或大小写不同的架构与固件无效(t *testing.T) {
	// 规则同族与系：只认原样写法。iPXE 的上报值（i386、arm64、pcbios、efi）也不是系统的
	// 取值，要先换算；原样存进主机登记，和 ISO 比对时就永远对不上。
	for _, a := range []Arch{"", "X86_64", "x86-64", "amd64", "x64", "i386", "arm64", "AArch64", " aarch64", "aarch64\n"} {
		if a.Valid() || a.Label() != "" {
			t.Errorf("架构 %q：Valid() = %v、Label() = %q，想要 false、空", a, a.Valid(), a.Label())
		}
	}
	for _, f := range []Firmware{"", "BIOS", "UEFI", "Uefi", "pcbios", "efi", "legacy", " uefi", "bios\n"} {
		if f.Valid() || f.Label() != "" {
			t.Errorf("固件 %q：Valid() = %v、Label() = %q，想要 false、空", f, f.Valid(), f.Label())
		}
	}
}

func TestIPXE构建架构换算(t *testing.T) {
	known := []struct {
		buildarch string
		want      Arch
	}{
		{"x86_64", "x86_64"},
		// BIOS 下的 iPXE 是 32 位构建，在 x86_64 机器上也报 i386（d014d50）。
		{"i386", "x86_64"},
		{"arm64", "aarch64"},
	}
	for _, c := range known {
		if got, ok := ArchFromIPXE(c.buildarch); !ok || got != c.want {
			t.Errorf("ArchFromIPXE(%q) = %q、%v，想要 %q、true", c.buildarch, got, ok, c.want)
		}
	}
	// 只认 iPXE 原样上报的写法，换算不了的由网关拒绝引导：系统自己的 aarch64 不是 iPXE
	// 的取值；arm32、riscv64、loong64 是系统不支持的 iPXE 构建；大小写不同、带空白的也不认。
	unknown := []string{
		"", "X86_64", "I386", "ARM64", "Arm64", "aarch64", "amd64", "i686",
		"arm32", "riscv64", "loong64", " x86_64", "i386\n", "arm64 ",
	}
	for _, in := range unknown {
		if got, ok := ArchFromIPXE(in); ok || got != "" {
			t.Errorf("ArchFromIPXE(%q) = %q、%v，想要空、false", in, got, ok)
		}
	}
}

func TestIPXE平台换算(t *testing.T) {
	known := []struct {
		platform string
		want     Firmware
	}{
		{"pcbios", "bios"},
		{"efi", "uefi"},
	}
	for _, c := range known {
		if got, ok := FirmwareFromIPXE(c.platform); !ok || got != c.want {
			t.Errorf("FirmwareFromIPXE(%q) = %q、%v，想要 %q、true", c.platform, got, ok, c.want)
		}
	}
	// 系统自己的 bios、uefi 不是 iPXE 的取值；linux、sbi 是系统不支持的 iPXE 平台。
	unknown := []string{"", "PCBIOS", "EFI", "Efi", "bios", "uefi", "BIOS", "UEFI", "linux", "sbi", " efi", "pcbios\n"}
	for _, in := range unknown {
		if got, ok := FirmwareFromIPXE(in); ok || got != "" {
			t.Errorf("FirmwareFromIPXE(%q) = %q、%v，想要空、false", in, got, ok)
		}
	}
}

func Test各系支持的固件(t *testing.T) {
	// ESXi 只走 UEFI（spec §2.1），其余的系两种固件都能装。逐个系查而不抽查：
	// 哪个系登记错了，建任务时就会放过或误拒一整批主机。
	for _, s := range seriesTableIDs() {
		want := []Firmware{"bios", "uefi"}
		if s == "esxi" {
			want = []Firmware{"uefi"}
		}
		if got := s.Firmwares(); !slices.Equal(got, want) {
			t.Errorf("系 %q 的 Firmwares() = %q，想要 %q", s, got, want)
		}
	}
	for _, s := range []Series{"", "ESXi", "esxi-ks", "esx", "centos", "Rocky", "linux"} {
		if got := s.Firmwares(); len(got) != 0 {
			t.Errorf("未知的系 %q 的 Firmwares() = %q，想要空", s, got)
		}
	}
}

func Test固件要求只登记已知的系与固件(t *testing.T) {
	// 要求表里的系写错了，那条要求就静默失效，该系回到两种固件都放行；同一个系登记两次，
	// 后一条不起作用；固件写错或留空，该系就一台主机也装不了。
	seen := map[Series]bool{}
	for _, r := range platformFirmwareLimitTable {
		if !r.series.Valid() || seen[r.series] {
			t.Errorf("固件要求登记了未知或重复的系 %q", r.series)
		}
		seen[r.series] = true
		if len(r.firmwares) == 0 {
			t.Errorf("系 %q 的固件要求是空的", r.series)
		}
		for _, f := range r.firmwares {
			if !f.Valid() {
				t.Errorf("系 %q 的固件要求里有未知的固件 %q", r.series, f)
			}
		}
	}
}

func Test改动返回的固件列表不影响下一次查询(t *testing.T) {
	// 目录是内置的静态事实（C1），调用方拿去改写的切片不能连带改掉目录。受限与不受限的系各查一个。
	for _, s := range []Series{"rocky", "esxi"} {
		got := s.Firmwares()
		if len(got) == 0 {
			t.Errorf("系 %q 的 Firmwares() 为空，验证不了副本", s)
			continue
		}
		want := slices.Clone(got)
		for i := range got {
			got[i] = "legacy"
		}
		if again := s.Firmwares(); !slices.Equal(again, want) {
			t.Errorf("改过上一次的结果后，系 %q 的 Firmwares() = %q，想要 %q", s, again, want)
		}
	}
}

func Test平台兼容判定(t *testing.T) {
	cases := []struct {
		name         string
		series       Series
		isoArch      Arch
		hostArch     Arch
		hostFW       Firmware
		archOK, fwOK bool
	}{
		{"esxi 配 bios 的主机", "esxi", "x86_64", "x86_64", "bios", true, false},
		{"esxi 配 uefi 的主机", "esxi", "x86_64", "x86_64", "uefi", true, true},
		{"x86_64 的 ISO 配 aarch64 的主机", "rocky", "x86_64", "aarch64", "uefi", false, true},
		{"aarch64 的 ISO 配 x86_64 的主机", "ubuntu", "aarch64", "x86_64", "bios", false, true},
		// 两项分开给出，准入判定才能把架构与固件两个原因都报出来。
		{"架构与固件都不符", "esxi", "x86_64", "aarch64", "bios", false, false},
		{"只登记了架构", "esxi", "x86_64", "x86_64", "", true, true},
		{"只登记了架构且不符", "rocky", "aarch64", "x86_64", "", false, true},
		{"只登记了固件", "windows-server", "x86_64", "", "bios", true, true},
		{"只登记了固件且不符", "esxi", "x86_64", "", "bios", true, false},
		// 只有空串算没登记：登记了却不合法的值比不上任何 ISO，不能当作没登记放过去。
		{"主机架构写成 iPXE 的上报值", "rocky", "x86_64", "i386", "", false, true},
		{"主机架构大小写不同", "rocky", "x86_64", "X86_64", "", false, true},
		{"主机固件写成 iPXE 的上报值", "rocky", "x86_64", "", "pcbios", true, false},
		{"主机固件大小写不同", "rocky", "x86_64", "", "UEFI", true, false},
		// 系或 ISO 架构未知时没有可比的对象，主机两项都没登记也不通过。
		{"未知的系", "centos", "x86_64", "x86_64", "bios", false, false},
		{"未知的系且主机两项都没登记", "centos", "x86_64", "", "", false, false},
		{"系为空", "", "x86_64", "", "", false, false},
		{"系大小写不同", "ESXi", "x86_64", "x86_64", "uefi", false, false},
		{"ISO 架构为空", "rocky", "", "", "", false, false},
		{"ISO 架构为空而主机登记了", "rocky", "", "x86_64", "bios", false, false},
		{"ISO 架构未知且与主机相同", "rocky", "amd64", "amd64", "bios", false, false},
		{"ISO 架构写成 iPXE 的上报值", "debian", "i386", "", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			archOK, fwOK := PlatformOK(c.series, c.isoArch, c.hostArch, c.hostFW)
			if archOK != c.archOK || fwOK != c.fwOK {
				t.Errorf("PlatformOK(%q, %q, %q, %q) = %v、%v，想要 %v、%v",
					c.series, c.isoArch, c.hostArch, c.hostFW, archOK, fwOK, c.archOK, c.fwOK)
			}
		})
	}
}

func TestLinux与Windows的系两种固件都通过(t *testing.T) {
	for _, f := range []Family{"linux", "windows"} {
		for _, s := range SeriesOf(f) {
			for _, a := range []Arch{"x86_64", "aarch64"} {
				for _, fw := range []Firmware{"bios", "uefi"} {
					if archOK, fwOK := PlatformOK(s, a, a, fw); !archOK || !fwOK {
						t.Errorf("PlatformOK(%q, %q, %q, %q) = %v、%v，想要 true、true", s, a, a, fw, archOK, fwOK)
					}
				}
			}
		}
	}
}

func Test主机没登记架构与固件时两项都通过(t *testing.T) {
	// C28：建任务时只校验主机登记了的项。每个系都如此，包括限制了固件的 esxi。
	for _, s := range seriesTableIDs() {
		for _, a := range []Arch{"x86_64", "aarch64"} {
			if archOK, fwOK := PlatformOK(s, a, "", ""); !archOK || !fwOK {
				t.Errorf("PlatformOK(%q, %q, 空, 空) = %v、%v，想要 true、true", s, a, archOK, fwOK)
			}
		}
	}
}

func Test回归d014d50_BIOS机器的iPXE报i386(t *testing.T) {
	// 旧系统里，BIOS 机器的 iPXE 报 i386，被判和 x86_64 的 ISO 不兼容。
	// 网关先换算再判定，这台机器就该通过。
	arch, archKnown := ArchFromIPXE("i386")
	fw, fwKnown := FirmwareFromIPXE("pcbios")
	if !archKnown || !fwKnown {
		t.Fatalf("i386 换算成 %q、%v，pcbios 换算成 %q、%v，想要都能换算", arch, archKnown, fw, fwKnown)
	}
	if archOK, fwOK := PlatformOK("rocky", "x86_64", arch, fw); !archOK || !fwOK {
		t.Errorf("rocky 的 x86_64 ISO 配这台机器 = %v、%v，想要 true、true", archOK, fwOK)
	}
	// 换算只纠正架构的误判，不顺带放过固件：同一台机器装 esxi 仍然不行。
	if archOK, fwOK := PlatformOK("esxi", "x86_64", arch, fw); !archOK || fwOK {
		t.Errorf("esxi 的 x86_64 ISO 配这台机器 = %v、%v，想要 true、false", archOK, fwOK)
	}
}
