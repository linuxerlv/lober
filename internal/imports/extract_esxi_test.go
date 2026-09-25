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

// sampleESXiRoot 构造一份完整、可正常抽取的 ESXi ISO 内存文件系统。
//
// 三组文件，缺一组抽取就该失败：
//   - boot.cfg 与 **efi/boot/bootx64.efi**（UEFI 引导器的真实路径，真实 ESXi
//     ISO 里没有 mboot.efi——落盘名才是 mboot.efi，见 esxiUEFILoaderSrcPath）
//   - boot.cfg 的 **kernel= 指向的文件**（独立于 modules=，此前夹具漏了它，
//     而抽取器也漏了检查，两个漏洞互相掩盖）
//   - modules= 列出的全部模块
func sampleESXiRoot() fstest.MapFS {
	return fstest.MapFS{
		"boot.cfg":             &fstest.MapFile{Data: []byte(sampleESXiBootCfg())},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("fake-mboot-efi-bytes")},
		"b.b00":                &fstest.MapFile{Data: []byte("fake-kernel")},
		"jumpstrt.gz":          &fstest.MapFile{Data: []byte("fake-jumpstrt")},
		"useropts.gz":          &fstest.MapFile{Data: []byte("fake-useropts")},
		"k.b00":                &fstest.MapFile{Data: []byte("fake-kernel-module")},
		"chain.b00":            &fstest.MapFile{Data: []byte("fake-chain-module")},
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
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("fake-mboot-efi-bytes")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺 boot.cfg 时应失败，got nil")
	}
	if !strings.Contains(err.Error(), "boot.cfg") {
		t.Errorf("失败原因应提到 boot.cfg，got: %v", err)
	}
}

// TestExtractESXi_MissingMbootEFIFails 验证缺 UEFI 引导器时导入失败。
//
// 断言的路径是 **efi/boot/bootx64.efi**（ISO 内的真实位置）而不是 mboot.efi
// （那是落盘名）。夹具与断言都改过一次：真实 ESXi ISO 里没有 mboot.efi，见
// esxiUEFILoaderSrcPath 的文档与实测依据。
func TestExtractESXi_MissingMbootEFIFails(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg": &fstest.MapFile{Data: []byte(sampleESXiBootCfg())},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("缺 UEFI 引导器时应失败，got nil")
	}
	if !strings.Contains(err.Error(), esxiUEFILoaderSrcPath) {
		t.Errorf("失败原因应提到 %s（ISO 内的真实路径），got: %v", esxiUEFILoaderSrcPath, err)
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
		"boot.cfg":             &fstest.MapFile{Data: []byte("kernelopt=runweasel\nmodules=\n")},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("fake")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("modules= 行为空时应失败，got nil")
	}
}

func TestExtractESXi_MissingModulesLineFails(t *testing.T) {
	root := fstest.MapFS{
		"boot.cfg":             &fstest.MapFile{Data: []byte("kernelopt=runweasel\n")},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("fake")},
	}
	_, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err == nil {
		t.Fatal("boot.cfg 中没有 modules= 行时应失败，got nil")
	}
}

// realESXiRoot 构造一份与真实 ESXi 7.0U3 ISO 同构的最小根目录。
//
// 关键结构（实测 VMware-ESXi-7.0U3v-24723872-x86_64.iso）：
//   - UEFI 引导器在 efi/boot/bootx64.efi，**根目录没有 mboot.efi**
//   - boot.cfg 的 kernel= 是**单独一行**，不在 modules= 里
//   - mboot.c32 是 BIOS 路径的 syslinux 模块，与 UEFI 无关
func realESXiRoot() fstest.MapFS {
	bootCfg := `bootstate=0
title=Loading ESXi installer
timeout=5
prefix=
kernel=/b.b00
kernelopt=runweasel cdromBoot
modules=/jumpstrt.gz --- /useropts.gz --- /k.b00
build=7.0.3-0.145.24723872
updated=0
`
	return fstest.MapFS{
		"boot.cfg":             &fstest.MapFile{Data: []byte(bootCfg)},
		"b.b00":                &fstest.MapFile{Data: []byte("kernel")},
		"jumpstrt.gz":          &fstest.MapFile{Data: []byte("m1")},
		"useropts.gz":          &fstest.MapFile{Data: []byte("m2")},
		"k.b00":                &fstest.MapFile{Data: []byte("m3")},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("uefi-loader")},
		"efi/boot/boot.cfg":    &fstest.MapFile{Data: []byte(bootCfg)},
		"mboot.c32":            &fstest.MapFile{Data: []byte("bios-comboot")},
	}
}

// TestExtractESXi_TakesUEFILoaderFromEFIBoot 验证 UEFI 引导器从
// efi/boot/bootx64.efi 取，落盘名仍是 mboot.efi。
//
// **真实 ESXi ISO 里没有 mboot.efi。** UEFI 引导器按 UEFI 规范的可移动介质默认
// 路径放在 efi/boot/bootx64.efi（固件自动发现那个路径）；根目录的 mboot.c32 是
// BIOS 路径的 syslinux COMBOOT 模块，与 UEFI 无关。
//
// SPEC §0.3/§4.5 写的「mboot.efi」是 **PXE 部署时的惯例名字**——VMware 的 PXE
// 文档让你把 bootx64.efi 复制出来用那个名字。因此落盘名保持 mboot.efi（渲染器
// 与菜单形态不用改），只有**查找路径**要改。
//
// 这与 Debian 那个「ISO 内叫 vmlinuz、落盘叫 linux」是同一个模式：查找名与落盘
// 名分开，两者都不是随便选的。
func TestExtractESXi_TakesUEFILoaderFromEFIBoot(t *testing.T) {
	destDir := t.TempDir()
	paths, err := ExtractESXi(realESXiRoot(), destDir, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("ExtractESXi 失败: %v —— 真实 ESXi ISO 的 UEFI 引导器在 "+
			"efi/boot/bootx64.efi，根目录没有 mboot.efi", err)
	}

	// 落盘名必须是 mboot.efi（SPEC §4.5 的菜单形态与渲染器都按这个名字）。
	if got, ok := paths.Extra["mboot.efi"]; !ok || got != "mboot.efi" {
		t.Errorf("Extra[\"mboot.efi\"] = %q（存在=%v）, want \"mboot.efi\" —— "+
			"落盘名保持 mboot.efi，只有 ISO 内的查找路径不同", got, ok)
	}
	if _, err := os.Stat(filepath.Join(destDir, "mboot.efi")); err != nil {
		t.Errorf("mboot.efi 未落盘: %v", err)
	}
}

// TestExtractESXi_ExtractsKernelFromBootCfg 验证 boot.cfg 的 kernel= 行指向的
// 文件也被抽取。
//
// **它此前被漏掉了。** required 列表是「mboot.efi + boot.cfg + modules」，而
// 真实 boot.cfg 里 `kernel=/b.b00` 是**单独一行**，b.b00 不在 modules= 里
// （实测那份镜像的 modules 从 /jumpstrt.gz 开始，整行没有 b.b00）。
//
// 漏掉内核的后果：引导时 mboot 去取 kernel= 指向的文件，404。而那个错误发生在
// 机器已经上电、已经走到引导器的时候——正是 ExtractESXi 的注释自己说的「引导到
// 某一步会停住且错误信息不会指出缺哪个文件」。
func TestExtractESXi_ExtractsKernelFromBootCfg(t *testing.T) {
	destDir := t.TempDir()
	paths, err := ExtractESXi(realESXiRoot(), destDir, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("ExtractESXi 失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "b.b00")); err != nil {
		t.Errorf("boot.cfg 的 kernel= 指向的 b.b00 未被抽取: %v —— "+
			"kernel 是 boot.cfg 里独立于 modules= 的一行，漏掉它会让引导器在取内核时 404，"+
			"而那时机器已经上电了", err)
	}
	if got, ok := paths.Extra["b.b00"]; !ok || got != "b.b00" {
		t.Errorf("Extra[\"b.b00\"] = %q（存在=%v）, want \"b.b00\"", got, ok)
	}
}

// TestExtractESXi_RejectsMissingUEFILoader 验证 UEFI 引导器缺失时导入失败。
//
// 修「从新位置取」不能变成「取不到就跳过」——缺引导器的制品集是装不了的，
// 必须在导入阶段就拒绝。
func TestExtractESXi_RejectsMissingUEFILoader(t *testing.T) {
	root := realESXiRoot()
	delete(root, "efi/boot/bootx64.efi")

	if _, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI); err == nil {
		t.Error("UEFI 引导器缺失时导入仍然成功 —— 缺引导器的制品集装不了，" +
			"必须在导入阶段拒绝而不是留到引导时 404")
	}
}
