// 本文件测试 esxi_dispatch.go 新增的适配点：NewESXiFamilyExtractor 与
// ESXiRawBootCfgArtifactKey（对应任务分配文档「解除 ESXi 的导入封锁」）。
//
// ExtractESXi 本身的行为（固件校验、缺文件报错、顺序保持等）已由
// extract_esxi_test.go 完整覆盖，本文件只锁住「适配层」自身的两件事：
// 闭包正确转发参数、导出的键名常量取值正确。
package imports

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/model"
)

// TestNewESXiFamilyExtractor_MatchesFamilyExtractorSignature 编译期验证
// NewESXiFamilyExtractor 的返回值确实能赋给 familyExtractor 类型的变量
// （job.go 定义的私有类型，同包可见）——这正是本适配函数存在的唯一理由：
// 让 ESXi 的抽取函数进得了 familyExtractors 这张表。
func TestNewESXiFamilyExtractor_MatchesFamilyExtractorSignature(t *testing.T) {
	var fe familyExtractor = NewESXiFamilyExtractor(model.FirmwareUEFI)
	if fe == nil {
		t.Fatal("NewESXiFamilyExtractor 返回值不应为 nil")
	}
}

// TestNewESXiFamilyExtractor_UEFISucceeds 验证闭包在 declaredFirmware=uefi
// 时，行为与直接调用 ExtractESXi(root, destDir, model.FirmwareUEFI) 一致。
func TestNewESXiFamilyExtractor_UEFISucceeds(t *testing.T) {
	root := sampleESXiRoot()
	destDir := t.TempDir()

	extract := NewESXiFamilyExtractor(model.FirmwareUEFI)
	paths, err := extract(root, destDir)
	if err != nil {
		t.Fatalf("声明为 uefi 时适配层应转发成功结果，got err=%v", err)
	}
	if _, ok := paths.Extra["mboot.efi"]; !ok {
		t.Errorf("适配层应转发 ExtractESXi 的产出，paths.Extra 应含 mboot.efi，got=%v", paths.Extra)
	}
}

// TestNewESXiFamilyExtractor_BIOSRejected 验证闭包把 declaredFirmware=bios
// 正确传递给了 ExtractESXi，而不是被适配层意外覆盖或忽略——固化在
// 构造时的固件值必须在每次调用闭包时都生效，不能被后续调用改变。
func TestNewESXiFamilyExtractor_BIOSRejected(t *testing.T) {
	root := sampleESXiRoot()

	extract := NewESXiFamilyExtractor(model.FirmwareBIOS)
	_, err := extract(root, t.TempDir())
	if err == nil {
		t.Fatal("声明为 bios 时适配层应转发 ExtractESXi 的拒绝结果，got nil")
	}
	if !strings.Contains(err.Error(), "uefi") {
		t.Errorf("拒绝原因应提到只接受 uefi（与 ExtractESXi 一致），got: %v", err)
	}
}

// TestNewESXiFamilyExtractor_FirmwareFixedAtConstruction 验证同一个闭包
// 反复调用时，声明的固件值不会漂移——多次调用应得到一致的结果，证明
// declaredFirmware 是在构造时被闭包捕获的值，不是每次调用重新读取的
// 某个可变状态。
func TestNewESXiFamilyExtractor_FirmwareFixedAtConstruction(t *testing.T) {
	extract := NewESXiFamilyExtractor(model.FirmwareUEFI)

	for i := 0; i < 3; i++ {
		root := sampleESXiRoot()
		if _, err := extract(root, t.TempDir()); err != nil {
			t.Fatalf("第 %d 次调用应仍然成功（固件值应保持 uefi 不漂移），got err=%v", i, err)
		}
	}
}

// TestESXiRawBootCfgArtifactKey_MatchesExtractESXiOutput 验证导出常量
// ESXiRawBootCfgArtifactKey 确实是 ExtractESXi 产出的 paths.Extra 里
// 存放原始 boot.cfg 的那个键——n1 轨的 RenderESXiBootCfg 需要靠这个常量
// （而不是硬编码字符串）找到抽取阶段落盘的原始模板。
func TestESXiRawBootCfgArtifactKey_MatchesExtractESXiOutput(t *testing.T) {
	root := sampleESXiRoot()
	paths, err := ExtractESXi(root, t.TempDir(), model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, ok := paths.Extra[ESXiRawBootCfgArtifactKey]; !ok {
		t.Errorf("paths.Extra 应含键 ESXiRawBootCfgArtifactKey=%q，got Extra=%v",
			ESXiRawBootCfgArtifactKey, paths.Extra)
	}
	if ESXiRawBootCfgArtifactKey != "boot.cfg" {
		t.Errorf("ESXiRawBootCfgArtifactKey 应等于字面量 \"boot.cfg\"，got %q", ESXiRawBootCfgArtifactKey)
	}
}

// 编译期占位：确认 fstest.MapFS 类型在本文件可用（sampleESXiRoot 定义在
// extract_esxi_test.go，同包共享，此处不重复定义，仅确认引入的包与其
// 用法一致，避免未使用 import 的编译错误在维护时被误删）。
var _ fstest.MapFS = fstest.MapFS{}
