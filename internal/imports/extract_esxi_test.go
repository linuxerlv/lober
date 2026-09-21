// 本文件测试 ESXi 制品集的抽取（对应实现计划
// docs/superpowers/plans/2026-09-20-lober-phase3-02-esxi.md 任务 1，依据
// SPEC §0.3、§2.1、§4.6 第三项）。
package imports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// sampleESXiBootCfg 返回一份贴近真实 ESXi 8 boot.cfg 的原始模板文本（导入
// 时从 ISO 里原样取出、未经任何改写的版本）。四个模块足以覆盖"顺序保持"
// 与"完整性校验覆盖全部模块"两条行为，真实文件有几十个。
func sampleESXiBootCfg() string {
	return `bootstate=0
title=Loading ESXi installer
timeout=5
prefix=
kernel=/b.b00
kernelopt=runweasel ks=cdrom:/KS.CFG
modules=/jumpstrt.gz --- /useropts.gz --- /k.b00 --- /chain.b00
build=8.0.0-2000000
updated=0
`
}

// sampleESXiRoot 构造一份完整、可正常抽取的 ESXi ISO 内存文件系统：两个
// 识别特征文件（boot.cfg + mboot.efi）加 boot.cfg 里列出的全部四个模块。
func sampleESXiRoot() fstest.MapFS {
	return fstest.MapFS{
		"boot.cfg":    &fstest.MapFile{Data: []byte(sampleESXiBootCfg())},
		"mboot.efi":   &fstest.MapFile{Data: []byte("fake-mboot-efi-bytes")},
		"jumpstrt.gz": &fstest.MapFile{Data: []byte("fake-jumpstrt")},
		"useropts.gz": &fstest.MapFile{Data: []byte("fake-useropts")},
		"k.b00":       &fstest.MapFile{Data: []byte("fake-kernel-module")},
		"chain.b00":   &fstest.MapFile{Data: []byte("fake-chain-module")},
	}
}

func TestExtractESXi_Success(t *testing.T) {
	root := sampleESXiRoot()
	destDir := t.TempDir()

	paths, err := ExtractESXi(root, destDir, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("正常的 ESXi 制品集应抽取成功: %v", err)
	}
	if paths.Kernel != "" || len(paths.Initrd) != 0 || paths.Repo != "" {
		t.Errorf("ESXi 不使用 Kernel/Initrd/Repo 三个字段，应保持零值，got Kernel=%q Initrd=%v Repo=%q",
			paths.Kernel, paths.Initrd, paths.Repo)
	}
	for _, key := range []string{"mboot.efi", "boot.cfg", "jumpstrt.gz", "useropts.gz", "k.b00", "chain.b00"} {
		if _, ok := paths.Extra[key]; !ok {
			t.Errorf("paths.Extra 应含键 %q，got Extra=%v", key, paths.Extra)
		}
	}
}

func TestExtractESXi_FilesLandOnDisk(t *testing.T) {
	root := sampleESXiRoot()
	destDir := t.TempDir()

	if _, err := ExtractESXi(root, destDir, model.FirmwareUEFI); err != nil {
		t.Fatalf("不应报错: %v", err)
	}

	mustExist := []string{
		filepath.Join(destDir, "mboot.efi"),
		filepath.Join(destDir, "boot.cfg"),
		filepath.Join(destDir, "jumpstrt.gz"),
		filepath.Join(destDir, "useropts.gz"),
		filepath.Join(destDir, "k.b00"),
		filepath.Join(destDir, "chain.b00"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("抽取后应存在文件 %s，got err=%v", p, err)
		}
	}
}

// TestExtractESXi_BootCfgOnDiskIsRawUnrewritten 验证落盘的 boot.cfg 是
// 原始未改写版本——三处改写（prefix/kernelopt/modules 路径）是渲染期
// RenderESXiBootCfg 的职责，抽取阶段不应提前做掉，否则原始模板信息丢失，
// 且新 ESXi 版本的字段会在这一步而不是渲染期被破坏。
func TestExtractESXi_BootCfgOnDiskIsRawUnrewritten(t *testing.T) {
	root := sampleESXiRoot()
	destDir := t.TempDir()

	if _, err := ExtractESXi(root, destDir, model.FirmwareUEFI); err != nil {
		t.Fatalf("不应报错: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destDir, "boot.cfg"))
	if err != nil {
		t.Fatalf("读取落盘的 boot.cfg 失败: %v", err)
	}
	if string(got) != sampleESXiBootCfg() {
		t.Errorf("落盘的 boot.cfg 应与原始模板逐字节一致（不做任何改写），\ngot:\n%s\nwant:\n%s",
			got, sampleESXiBootCfg())
	}
}

// ---------------------------------------------------------------------------
// 测试要锁住的行为 1：两个特征文件同时存在时识别为 ESXi，只有一个时不识别
// （已由 identify.go 的 Identify 覆盖，见 identify_test.go 的
// TestIdentify_ESXi_UnsupportedButIdentified；本文件补一条从 ExtractESXi
// 自身角度的行为：缺 boot.cfg 或缺 mboot.efi 时抽取应失败并指出具体缺什么）
// ---------------------------------------------------------------------------

func TestExtractESXi_MissingBootCfgFails(t *testing.T) {
	root := fstest.MapFS{
		"mboot.efi": &fstest.MapFile{Data: []byte("fake-mboot-efi-bytes")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺 boot.cfg 时应失败，got nil")
	}
	if !strings.Contains(err.Error(), "boot.cfg") {
		t.Errorf("失败原因应提到 boot.cfg，got: %v", err)
	}
}

func TestExtractESXi_MissingMbootEFIFails(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg": &fstest.MapFile{Data: []byte(sampleESXiBootCfg())},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺 mboot.efi 时应失败，got nil")
	}
	if !strings.Contains(err.Error(), "mboot.efi") {
		t.Errorf("失败原因应提到 mboot.efi，got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试要锁住的行为 2：模块文件全部搬运且顺序保持
// ---------------------------------------------------------------------------

func TestExtractESXi_AllModulesCopied(t *testing.T) {
	root := sampleESXiRoot()
	paths, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	for _, module := range []string{"jumpstrt.gz", "useropts.gz", "k.b00", "chain.b00"} {
		if _, ok := paths.Extra[module]; !ok {
			t.Errorf("模块 %q 应出现在 paths.Extra 中，got Extra=%v", module, paths.Extra)
		}
	}
}

// TestExtractESXi_ModuleOrderPreservedInParsedList 验证解析 modules= 行时
// 顺序与原文一致——用一份模块名刻意不按字母序排列的 boot.cfg，确认
// parseESXiBootCfgModules 的返回顺序与输入完全一致，不被意外排序（该顺序
// 是引导要求，计划任务 1 明确警示不能重排；由于 ArtifactPaths.Extra 是
// map、天然不保序，真正体现"顺序保持"的地方是 boot.cfg 文本本身原样落盘——
// 见 TestExtractESXi_BootCfgOnDiskIsRawUnrewritten——与本函数解析结果的
// 顺序，本测试锁住后者）。
func TestExtractESXi_ModuleOrderPreservedInParsedList(t *testing.T) {
	bootCfg := "kernelopt=runweasel\nmodules=/zzz.gz --- /aaa.gz --- /mmm.gz\n"
	modules, err := parseESXiBootCfgModules(bootCfg)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := []string{"zzz.gz", "aaa.gz", "mmm.gz"}
	if len(modules) != len(want) {
		t.Fatalf("got %v, want %v", modules, want)
	}
	for i := range want {
		if modules[i] != want[i] {
			t.Errorf("模块顺序不符（第 %d 项）：got %q, want %q，全列表 got=%v want=%v",
				i, modules[i], want[i], modules, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 测试要锁住的行为 3：固件字段只接受 UEFI，其它值被拒
// ---------------------------------------------------------------------------

func TestExtractESXi_BIOSFirmwareRejected(t *testing.T) {
	root := sampleESXiRoot()
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareBIOS)
	if err == nil {
		t.Fatal("ESXi 制品集声明为 bios 固件时应被拒绝，got nil")
	}
	if !strings.Contains(err.Error(), "uefi") {
		t.Errorf("拒绝原因应提到只接受 uefi，got: %v", err)
	}
}

func TestExtractESXi_AnyFirmwareRejected(t *testing.T) {
	root := sampleESXiRoot()
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareAny)
	if err == nil {
		t.Fatal("ESXi 制品集声明为 any 固件时应被拒绝（ESXi 不是 Linux 四家那种通吃），got nil")
	}
}

func TestExtractESXi_UEFIFirmwareAccepted(t *testing.T) {
	root := sampleESXiRoot()
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err != nil {
		t.Errorf("声明为 uefi 时应被接受，got err=%v", err)
	}
}

// ---------------------------------------------------------------------------
// 测试要锁住的行为 4：缺模块文件时导入失败且原因具体
// ---------------------------------------------------------------------------

func TestExtractESXi_MissingModuleFileFailsWithSpecificReason(t *testing.T) {
	root := sampleESXiRoot()
	delete(root, "k.b00") // 移除一个模块文件，但 boot.cfg 仍然列着它

	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺一个 boot.cfg 列出的模块文件时应导入失败，got nil")
	}
	if !strings.Contains(err.Error(), "k.b00") {
		t.Errorf("失败原因应具体指出缺的是 k.b00（不能只说'缺文件'）——"+
			"ESXi 引导到某一步停住时错误信息不会指出缺哪个文件，所以导入期的这条原因"+
			"必须具体，got: %v", err)
	}
}

func TestExtractESXi_MissingMultipleModulesReportsFirst(t *testing.T) {
	root := sampleESXiRoot()
	delete(root, "jumpstrt.gz")
	delete(root, "chain.b00")

	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺多个模块文件时应导入失败，got nil")
	}
	// 至少应指出其中一个具体缺失的文件名（按 boot.cfg 中出现的顺序，
	// 第一个缺失的是 jumpstrt.gz）。
	if !strings.Contains(err.Error(), "jumpstrt.gz") {
		t.Errorf("失败原因应指出第一个缺失的模块 jumpstrt.gz，got: %v", err)
	}
}

func TestExtractESXi_EmptyModulesLineFails(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg":  &fstest.MapFile{Data: []byte("kernelopt=runweasel\nmodules=\n")},
		"mboot.efi": &fstest.MapFile{Data: []byte("fake")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("modules= 行为空时应失败，got nil")
	}
}

func TestExtractESXi_MissingModulesLineFails(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg":  &fstest.MapFile{Data: []byte("kernelopt=runweasel\n")},
		"mboot.efi": &fstest.MapFile{Data: []byte("fake")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("boot.cfg 中没有 modules= 行时应失败，got nil")
	}
}
