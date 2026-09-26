package imports

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// TestDetectIdentity_RHELFromTreeinfo 验证 RHEL 系从 .treeinfo 的
// [general] 段取 arch 与 version，firmware 取 any。
//
// 取值照抄真实 OpenCloudOS 9.6 minimal ISO 的 .treeinfo（实测内容，不是
// 编的）：arch = x86_64、version = 9.6、repository = BaseOS。
func TestDetectIdentity_RHELFromTreeinfo(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\narch = x86_64\nfamily = OpenCloudOS\nversion = 9.6\nrepository = BaseOS\n" +
				"[images-x86_64]\nkernel = images/pxeboot/vmlinuz\ninitrd = images/pxeboot/initrd.img\n")},
	}

	got, err := DetectIdentity(root, model.OSFamilyRHEL, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchX86_64 {
		t.Errorf("Arch = %q，想要 %q", got.Arch, model.ArchX86_64)
	}
	if got.OSVersion != "9.6" {
		t.Errorf("OSVersion = %q，想要 %q", got.OSVersion, "9.6")
	}
	// Linux 四家一套制品通吃两种固件（model.ArtifactSet.Firmware 字段文档）。
	if got.Firmware != model.FirmwareAny {
		t.Errorf("Firmware = %q，想要 %q", got.Firmware, model.FirmwareAny)
	}
	if got.OSFamily != model.OSFamilyRHEL {
		t.Errorf("OSFamily = %q，想要 %q", got.OSFamily, model.OSFamilyRHEL)
	}
}

// TestDetectIdentity_RHELARM64 验证 arch 是真的从文件里读的，不是硬编码
// 成 x86_64——把 .treeinfo 换成 aarch64 就该得到 arm64。
func TestDetectIdentity_RHELARM64(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\narch = aarch64\nversion = 9.6\n[images-aarch64]\nkernel = k\ninitrd = i\n")},
	}

	got, err := DetectIdentity(root, model.OSFamilyRHEL, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchARM64 {
		t.Errorf("Arch = %q，想要 %q（aarch64 是 RPM 系对 arm64 的叫法）", got.Arch, model.ArchARM64)
	}
}

// TestDetectIdentity_RHELUnknownArch 验证遇到认不出的 arch 值时**报错**，
// 不静默兜底成 x86_64。
//
// 兜底的后果：制品集声明 x86_64、机器实际是别的架构，precheck 的 arch 闸门
// 反而会放行，内核在目标机上直接加载失败——比导入时就拒绝难查得多。
func TestDetectIdentity_RHELUnknownArch(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\narch = ppc64le\nversion = 9.6\n[images-ppc64le]\nkernel = k\ninitrd = i\n")},
	}

	_, err := DetectIdentity(root, model.OSFamilyRHEL, model.FirmwareAny)
	if err == nil {
		t.Fatal("认不出的 arch 值必须报错，不能静默兜底成 x86_64")
	}
	if !strings.Contains(err.Error(), "ppc64le") {
		t.Errorf("错误信息里应带上认不出的那个值，便于诊断，实际: %v", err)
	}
}

// TestDetectIdentity_RHELMissingArch 验证 .treeinfo 里没有 arch 键时报错。
func TestDetectIdentity_RHELMissingArch(t *testing.T) {
	root := fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte(
			"[general]\nversion = 9.6\n[images-x86_64]\nkernel = k\ninitrd = i\n")},
	}

	if _, err := DetectIdentity(root, model.OSFamilyRHEL, model.FirmwareAny); err == nil {
		t.Fatal(".treeinfo 缺 arch 键时必须报错")
	}
}

// TestDetectIdentity_DebianFromBinaryDir 验证 Debian 系从
// dists/<suite>/main/binary-<arch>/ 的目录名取 arch。
func TestDetectIdentity_DebianFromBinaryDir(t *testing.T) {
	root := fstest.MapFS{
		"dists/stable/Release":                    &fstest.MapFile{Data: []byte("Codename: trixie\n")},
		"dists/stable/main/binary-amd64/Packages": &fstest.MapFile{Data: []byte("Package: bash\n")},
	}

	got, err := DetectIdentity(root, model.OSFamilyDebian, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchX86_64 {
		t.Errorf("Arch = %q，想要 %q（amd64 是 APT 系对 x86_64 的叫法）", got.Arch, model.ArchX86_64)
	}
	if got.Firmware != model.FirmwareAny {
		t.Errorf("Firmware = %q，想要 %q", got.Firmware, model.FirmwareAny)
	}
}

// TestDetectIdentity_DebianARM64 同样验证不是硬编码。
func TestDetectIdentity_DebianARM64(t *testing.T) {
	root := fstest.MapFS{
		"dists/stable/main/binary-arm64/Packages": &fstest.MapFile{Data: []byte("Package: bash\n")},
	}

	got, err := DetectIdentity(root, model.OSFamilyDebian, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchARM64 {
		t.Errorf("Arch = %q，想要 %q", got.Arch, model.ArchARM64)
	}
}

// TestDetectIdentity_ESXiForcesUEFI 验证 ESXi 固件恒为 uefi（§0.3：ESXi
// 仅 UEFI，BIOS 路径不在覆盖范围内），即便调用方声明的是别的值。
func TestDetectIdentity_ESXiForcesUEFI(t *testing.T) {
	root := fstest.MapFS{
		"EFI/BOOT/BOOTX64.EFI": &fstest.MapFile{Data: []byte("fake")},
		"boot.cfg":             &fstest.MapFile{Data: []byte("kernel=/b.b00\n")},
	}

	got, err := DetectIdentity(root, model.OSFamilyESXi, model.FirmwareBIOS)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Firmware != model.FirmwareUEFI {
		t.Errorf("Firmware = %q，想要 %q", got.Firmware, model.FirmwareUEFI)
	}
	if got.Arch != model.ArchX86_64 {
		t.Errorf("Arch = %q，想要 %q（ESXi 只发行 x86_64）", got.Arch, model.ArchX86_64)
	}
}

// TestFirmwareFor 验证固件维度的取值规则（model.ArtifactSet.Firmware 字段
// 文档定的那三条）。
//
// 直接测 firmwareFor 而不是经 DetectIdentity：Windows 的架构判定要读
// boot.wim 的 PE 头，那份判据还没实现（见
// TestDetectIdentity_UbuntuFromDiskInfo 验证 Ubuntu 从 .disk/info 取架构
// 与版本。
//
// 夹具内容照抄真实 Ubuntu Server 介质那一行的形态（架构作为独立词出现在
// 中间，不是结尾）。Ubuntu 是唯一必须读元数据的家族：casper/vmlinuz 路径
// 不带架构，抽取器对架构无感。
func TestDetectIdentity_UbuntuFromDiskInfo(t *testing.T) {
	root := fstest.MapFS{
		"casper/vmlinuz": &fstest.MapFile{Data: []byte("k")},
		".disk/info": &fstest.MapFile{Data: []byte(
			`Ubuntu-Server 24.04.1 LTS "Noble Numbat" - Release amd64 (20240827)`)},
	}

	got, err := DetectIdentity(root, model.OSFamilyUbuntu, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchX86_64 {
		t.Errorf("Arch = %q，想要 %q", got.Arch, model.ArchX86_64)
	}
	if got.OSVersion != "24.04.1" {
		t.Errorf("OSVersion = %q，想要 %q", got.OSVersion, "24.04.1")
	}
}

// TestDetectIdentity_UbuntuARM64 验证不是硬编码 x86_64。
func TestDetectIdentity_UbuntuARM64(t *testing.T) {
	root := fstest.MapFS{
		".disk/info": &fstest.MapFile{Data: []byte(
			`Ubuntu-Server 24.04.1 LTS "Noble Numbat" - Release arm64 (20240827)`)},
	}

	got, err := DetectIdentity(root, model.OSFamilyUbuntu, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchARM64 {
		t.Errorf("Arch = %q，想要 %q", got.Arch, model.ArchARM64)
	}
}

// TestDetectIdentity_SUSEUsesLoaderDirConstraint 验证 SUSE 取 x86_64，且这个
// 取值与 extract_suse.go 的 suseLoaderDir 常量是耦合的。
func TestDetectIdentity_SUSEUsesLoaderDirConstraint(t *testing.T) {
	// 守卫断言：SUSE 的架构推断完全依赖这个常量仍然硬编码着 x86_64。
	// 哪天 SUSE 抽取器支持了多架构，这条会先失败，提醒去改 identity.go
	// 而不是让它继续声称每份 SUSE 介质都是 x86_64。
	if suseLoaderDir != "boot/x86_64/loader" {
		t.Fatalf("suseLoaderDir = %q，已不再是硬编码的 x86_64 路径——"+
			"archFromSUSELoaderDir 的推断随之失效，需要改成从路径里解析架构",
			suseLoaderDir)
	}

	root := fstest.MapFS{"media.1/products": &fstest.MapFile{Data: []byte("/ openSUSE-Leap-15.6\n")}}
	got, err := DetectIdentity(root, model.OSFamilySUSE, model.FirmwareAny)
	if err != nil {
		t.Fatalf("DetectIdentity 失败: %v", err)
	}
	if got.Arch != model.ArchX86_64 {
		t.Errorf("Arch = %q，想要 %q", got.Arch, model.ArchX86_64)
	}
}

// TestDetectIdentity_UbuntuMissingArchFailsLoudly 验证 Ubuntu 的元数据缺架构
// 判据时**在导入期失败**，不产出 arch 为空的制品集。
//
// 为什么这条重要：arch 为空的制品集能顺利导入成 ready，但 precheck 的
// checkArch 拿 "x86_64" 与 "" 比对必然不等，于是**每一次装机**都会在
// /v1/boot 被拒，报的还是一句「机器上报架构 x86_64，任务制品集要求架构 ""」
// ——排查的人会去怀疑机器、怀疑 iPXE，而真正的缺口在几小时前的导入。
// 早失败一次，胜过每次装机都失败一次。
func TestDetectIdentity_UbuntuMissingArchFailsLoudly(t *testing.T) {
	for name, root := range map[string]fstest.MapFS{
		"没有 .disk/info": {"casper/vmlinuz": &fstest.MapFile{Data: []byte("k")}},
		// 这正是本仓库改动前 extract_ubuntu_test.go 夹具里的内容：不含架构词。
		"有 .disk/info 但不含架构词": {".disk/info": &fstest.MapFile{Data: []byte("Ubuntu 24.04 LTS")}},
	} {
		_, err := DetectIdentity(root, model.OSFamilyUbuntu, model.FirmwareAny)
		if err == nil {
			t.Errorf("%s: 判定不出 arch 时必须报错，不能产出 arch 为空的制品集", name)
			continue
		}
		if !errors.Is(err, ErrArchUndetectable) {
			t.Errorf("%s: 错误应可用 errors.Is(err, ErrArchUndetectable) 识别，实际: %v", name, err)
		}
	}
}

// TestDetectIdentity_UnknownFamilyFailsLoudly 验证不认识的家族报错。
func TestDetectIdentity_UnknownFamilyFailsLoudly(t *testing.T) {
	_, err := DetectIdentity(fstest.MapFS{}, model.OSFamily("plan9"), model.FirmwareAny)
	if err == nil {
		t.Fatal("不认识的家族必须报错")
	}
	if !errors.Is(err, ErrArchUndetectable) {
		t.Errorf("应可用 errors.Is 识别，实际: %v", err)
	}
}
