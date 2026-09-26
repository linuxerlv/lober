// 本文件测试 Windows 制品集的双固件导入（对应实现计划 P3-01 任务 1，依据
// SPEC §0.3、§2.1、§4.5、§4.6，附录 A.4-A.6）。
//
// 全部用内存文件系统与假的 WinPE 架构探测器构造，不依赖真实 ISO 或真实
// WIM 文件解析（计划任务 1 明确要求"内存文件系统测试，不依赖真 ISO"）。
package imports

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// sampleWindowsRoot 构造一个含 sources/install.wim 特征文件的最小 Windows
// 安装源视图（对应 SPEC §2.1 识别特征文件表：Windows → sources/install.wim）。
func sampleWindowsRoot() fstest.MapFS {
	return fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("fake-install-wim-bytes")},
		"setup.exe":           &fstest.MapFile{Data: []byte("fake-setup-exe")},
	}
}

// windowsExternalFiles 在磁盘上写出 wimboot 与三个 WinPE 制品（BCD、
// boot.sdi、boot.wim），返回它们的绝对路径。这四个文件按计划任务 1 明确
// 要求"不在 ISO 里，要单独获取并放进制品根目录"——因此测试用本地临时目录
// 模拟"运维已经把这四个文件放在某处"的前置状态，而不是从 sampleWindowsRoot
// 里读。
func windowsExternalFiles(t *testing.T, winpeBinaryContent []byte) WindowsExternalSources {
	t.Helper()
	dir := t.TempDir()

	wimbootPath := filepath.Join(dir, "wimboot")
	bcdPath := filepath.Join(dir, "BCD")
	bootSDIPath := filepath.Join(dir, "boot.sdi")
	bootWIMPath := filepath.Join(dir, "boot.wim")

	writeFile(t, wimbootPath, []byte("fake-wimboot-binary"))
	writeFile(t, bcdPath, []byte("fake-bcd-bytes"))
	writeFile(t, bootSDIPath, []byte("fake-boot-sdi-bytes"))
	writeFile(t, bootWIMPath, winpeBinaryContent)

	return WindowsExternalSources{
		WimbootPath: wimbootPath,
		BCDPath:     bcdPath,
		BootSDIPath: bootSDIPath,
		BootWIMPath: bootWIMPath,
	}
}

// fakeWimArchDetector 是 WimArchDetector 的测试替身：按预设的固定返回值
// 响应，不解析任何真实字节内容（计划任务 1："读镜像的架构标识，与制品集
// 声明的固件比对"——真实的 WIM 头解析不在本模块的契约范围内，见本文件
// 末尾的契约缺口说明）。
type fakeWimArchDetector struct {
	firmware model.FirmwareMode
	err      error
}

func (f fakeWimArchDetector) DetectFirmware(bootWIMPath string) (model.FirmwareMode, error) {
	return f.firmware, f.err
}

// ---------------------------------------------------------------------------
// TestExtractWindows：四类文件齐全时导入成功
// ---------------------------------------------------------------------------

func TestExtractWindows_AllFourArtifactsPresent_Succeeds(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	paths, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err != nil {
		t.Fatalf("四类文件齐全且位宽与声明固件一致时应导入成功: %v", err)
	}

	if paths.Extra == nil {
		t.Fatalf("ArtifactPaths.Extra 不应为 nil")
	}
	for _, key := range []string{"wimboot", "BCD", "boot.sdi", "boot.wim"} {
		if _, ok := paths.Extra[key]; !ok {
			t.Errorf("ArtifactPaths.Extra 缺少键 %q，got %v", key, paths.Extra)
		}
	}
}

func TestExtractWindows_FilesLandOnDisk(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	paths, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}

	for _, key := range []string{"wimboot", "BCD", "boot.sdi", "boot.wim"} {
		rel, ok := paths.Extra[key]
		if !ok {
			t.Fatalf("缺少键 %q", key)
		}
		full := filepath.Join(destDir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("抽取后应存在文件 %s（键 %q），got err=%v", full, key, err)
		}
	}
}

// ---------------------------------------------------------------------------
// TestExtractWindows：缺任一文件则失败且原因具体
// ---------------------------------------------------------------------------

func TestExtractWindows_MissingWimboot_FailsWithSpecificReason(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	ext.WimbootPath = "" // 模拟 wimboot 缺失
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("wimboot 缺失时应返回错误——计划任务 1 明确要求" +
			"「导入 Windows 制品集时若 wimboot 不存在，要明确报错而不是等引导时 404」")
	}
	if !strings.Contains(err.Error(), "wimboot") {
		t.Errorf("错误信息应具体指出缺失的是 wimboot，got: %v", err)
	}
}

func TestExtractWindows_MissingBCD_FailsWithSpecificReason(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	ext.BCDPath = ""
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("BCD 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "BCD") {
		t.Errorf("错误信息应具体指出缺失的是 BCD，got: %v", err)
	}
}

func TestExtractWindows_MissingBootSDI_FailsWithSpecificReason(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	ext.BootSDIPath = ""
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("boot.sdi 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "boot.sdi") {
		t.Errorf("错误信息应具体指出缺失的是 boot.sdi，got: %v", err)
	}
}

func TestExtractWindows_MissingBootWIM_FailsWithSpecificReason(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	ext.BootWIMPath = ""
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("boot.wim 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "boot.wim") {
		t.Errorf("错误信息应具体指出缺失的是 boot.wim，got: %v", err)
	}
}

func TestExtractWindows_MissingInstallWIMInISO_FailsWithSpecificReason(t *testing.T) {
	root := fstest.MapFS{"README.txt": &fstest.MapFile{Data: []byte("not a real windows iso")}}
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("ISO 内容中缺少 sources/install.wim 时应返回错误")
	}
	if !strings.Contains(err.Error(), "install.wim") {
		t.Errorf("错误信息应具体指出缺失的是 sources/install.wim，got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExtractWindows：位宽与声明固件不符时导入失败
// ---------------------------------------------------------------------------

func TestExtractWindows_FirmwareMismatch_FailsImport(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("bios-winpe-bytes-but-declared-uefi"))
	destDir := t.TempDir()
	// 声明的固件是 uefi，但探测器判定这个 boot.wim 实际是 32 位/BIOS 的。
	detector := fakeWimArchDetector{firmware: model.FirmwareBIOS}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("声明 uefi 却导入了 bios 位宽的 WinPE 应导入失败——" +
			"计划任务 1 明确要求「声明 UEFI 却导入了 32 位的 WinPE 会在引导时失败，" +
			"而那时运维已经在机房了」，必须在导入时拦住")
	}
	if !strings.Contains(err.Error(), "uefi") && !strings.Contains(err.Error(), "bios") {
		t.Errorf("错误信息应指出声明值与实际探测值的具体不一致，got: %v", err)
	}
}

func TestExtractWindows_FirmwareMatch_BIOS_Succeeds(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("bios-winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareBIOS}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareBIOS, detector)
	if err != nil {
		t.Fatalf("声明 bios 且探测结果也是 bios 时应导入成功: %v", err)
	}
}

func TestExtractWindows_DeclaredFirmwareAny_Rejected(t *testing.T) {
	// §2.1 表格明确：Windows 的 firmware 取具体值（uefi 或 bios），不是
	// any——any 只用于 Linux 四家「一套制品通吃」的情况。
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareAny, detector)
	if err == nil {
		t.Fatal("Windows 制品集声明固件为 any 应被拒绝——" +
			"SPEC §2.1：Windows 需按固件导两个具体制品集，any 是 Linux 四家专属语义")
	}
}

func TestExtractWindows_ArchDetectorError_PropagatesAsImportFailure(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("corrupt-winpe-bytes"))
	destDir := t.TempDir()
	detector := fakeWimArchDetector{err: errors.New("无法解析 WIM 头部")}

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, detector)
	if err == nil {
		t.Fatal("架构探测器返回错误时导入应整体失败，不能假设一个默认固件继续")
	}
}

// TestExtractWindows_NilDetector_ReturnsErrorNotPanic 是复核阶段发现并修复的
// 回归测试：detector 参数为 nil 时，原实现会在调用
// detector.DetectFirmware(...) 处直接 panic（nil interface 上调方法）——这与
// 本函数其它每一处前置校验的风格矛盾：所有已知的缺失/不一致输入
// （declaredFirmware 非法、四个文件缺任一、位宽不符）都返回一条具体的 error，
// 只有"调用方忘了注入 detector"这一种输入方式会让整个导入 job 直接崩溃，而
// 不是走 job.go 的 fail() 分支置为 failed 状态。对运维而言两者的现场完全
// 不同：一个是制品集变成 failed 且有原因，另一个是导入 job 的 goroutine
// panic（若调用方没有 recover，会拖垫整个进程）。修复：在触达 detector 之前
// 显式校验 nil 并返回 error。
func TestExtractWindows_NilDetector_ReturnsErrorNotPanic(t *testing.T) {
	root := sampleWindowsRoot()
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	destDir := t.TempDir()

	_, err := ExtractWindows(root, ext, destDir, model.FirmwareUEFI, nil)
	if err == nil {
		t.Fatal("detector 为 nil 时应返回错误，不应假装成功")
	}
	if !strings.Contains(err.Error(), "detector") {
		t.Errorf("错误信息应具体指出问题出在 detector 参数，got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExtractWindows：同一版本导两个制品集（不同固件）互不冲突
// ---------------------------------------------------------------------------

func TestExtractWindows_SameVersionTwoFirmwares_NoConflict(t *testing.T) {
	root := sampleWindowsRoot()

	uefiExt := windowsExternalFiles(t, []byte("uefi-winpe"))
	uefiDest := t.TempDir()
	uefiDetector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	uefiPaths, err := ExtractWindows(root, uefiExt, uefiDest, model.FirmwareUEFI, uefiDetector)
	if err != nil {
		t.Fatalf("UEFI 制品集导入失败: %v", err)
	}

	biosExt := windowsExternalFiles(t, []byte("bios-winpe"))
	biosDest := t.TempDir()
	biosDetector := fakeWimArchDetector{firmware: model.FirmwareBIOS}
	biosPaths, err := ExtractWindows(root, biosExt, biosDest, model.FirmwareBIOS, biosDetector)
	if err != nil {
		t.Fatalf("BIOS 制品集导入失败: %v", err)
	}

	// 两次导入各自落盘在独立的 destDir 下，互不覆盖、互不冲突。
	if uefiDest == biosDest {
		t.Fatal("测试设置错误：两次导入应使用不同的 destDir")
	}
	uefiBootWIM := filepath.Join(uefiDest, uefiPaths.Extra["boot.wim"])
	biosBootWIM := filepath.Join(biosDest, biosPaths.Extra["boot.wim"])
	if _, err := os.Stat(uefiBootWIM); err != nil {
		t.Errorf("UEFI 制品集的 boot.wim 应存在: %v", err)
	}
	if _, err := os.Stat(biosBootWIM); err != nil {
		t.Errorf("BIOS 制品集的 boot.wim 应存在: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExtractWindows：识别出 Windows 家族（复用 Identify 的输出，间接锁住
// 「导入流程能识别 Windows」这条行为）。本轮（n2 轨）解除了 Windows 的 MVP
// 封锁，Identify 现在对 Windows 特征文件返回 (windows, nil)，不再是
// ErrUnsupportedOSFamily——本用例替换了旧版断言（旧版锁的正是本轮要解除
// 的封锁行为）。
// ---------------------------------------------------------------------------

func TestExtractWindows_IdentifyRecognizesWindowsFamily(t *testing.T) {
	root := sampleWindowsRoot()
	family, err := Identify(root)
	if err != nil {
		t.Fatalf("Windows 已解除 MVP 封锁，识别 Windows 特征文件不应报错，got family=%s err=%v", family, err)
	}
	if family != model.OSFamilyWindows {
		t.Fatalf("识别出的家族字段应为 windows，got %s", family)
	}
}
