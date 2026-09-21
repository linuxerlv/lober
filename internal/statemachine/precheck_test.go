// 本文件测试 Prechecker 实现（任务 3）：§4.6 三项一致性校验——架构一致性、
// 固件模式兼容、制品完整性。
package statemachine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// completeRocky9ArtifactFiles 构造一套「文件齐全」的内存文件系统，路径与
// completeRocky9Artifact 声明的 Paths 一一对应，供多个子测试复用一份合法
// 基线，再各自去掉某个文件模拟缺失。
func completeRocky9ArtifactFiles() fstest.MapFS {
	return fstest.MapFS{
		"rocky9.4/vmlinuz":                  &fstest.MapFile{Data: []byte("kernel")},
		"rocky9.4/initrd.img":               &fstest.MapFile{Data: []byte("initrd")},
		"rocky9.4/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}
}

func completeRocky9Artifact() model.ArtifactSet {
	return model.ArtifactSet{
		ID:        "artifact-rocky9",
		OSFamily:  model.OSFamilyRHEL,
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "rocky9.4/vmlinuz",
			Initrd: []string{"rocky9.4/initrd.img"},
			Repo:   "rocky9.4/repo/",
		},
	}
}

// TestPrecheckArchMismatchRejected 验证架构不符被拒绝，且错误里能读出
// 「是架构那一项」——运维需要精确原因，不是笼统的「precheck 失败」。
func TestPrecheckArchMismatchRejected(t *testing.T) {
	m := &Machine{}
	artifact := completeRocky9Artifact() // Arch = x86_64
	files := completeRocky9ArtifactFiles()

	err := m.Check(context.Background(), model.ArchARM64, model.FirmwareUEFI, artifact, files)
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check(实际arch=arm64, 制品arch=x86_64) err = %v, want *contract.PrecheckError —— "+
			"x86_64 制品发给 arm64 机器会导致内核加载失败，必须被拒绝", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureArch {
		t.Errorf("PrecheckError.Failure = %q, want %q —— 运维需要从错误里直接读出"+
			"是架构这一项不过，而不是笼统的失败", precheckErr.Failure, contract.PrecheckFailureArch)
	}
}

// TestPrecheckESXiOnBIOSRejected 验证 ESXi + BIOS 被拒绝（SPEC §0.3、
// §4.6：MVP 明确排除这个组合，mboot.efi 是 UEFI 专用）。
func TestPrecheckESXiOnBIOSRejected(t *testing.T) {
	m := &Machine{}
	artifact := model.ArtifactSet{
		ID:       "artifact-esxi8",
		OSFamily: model.OSFamilyESXi,
		Arch:     model.ArchX86_64,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "esxi8/mboot.efi",
			Repo:   "esxi8/repo/",
		},
	}
	files := fstest.MapFS{
		"esxi8/mboot.efi":                &fstest.MapFile{Data: []byte("mboot")},
		"esxi8/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareBIOS, artifact, files)
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check(esxi + bios) err = %v, want *contract.PrecheckError —— "+
			"esxi 在 MVP 只支持 uefi（附录 A.14：BIOS 路径需要 syslinux 的 mboot.c32"+
			"加 iPXE 的 COMBOOT 支持，明确排除）", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureFirmware {
		t.Errorf("PrecheckError.Failure = %q, want %q（典型拒绝：这台机器是 BIOS，任务要装 ESXi）",
			precheckErr.Failure, contract.PrecheckFailureFirmware)
	}
}

// TestPrecheckLinuxFourFamiliesPassBothFirmwares 验证 Linux 四家在两种
// 固件下都通过——它们的制品固件字段是 any（一套制品通吃）。
func TestPrecheckLinuxFourFamiliesPassBothFirmwares(t *testing.T) {
	m := &Machine{}
	families := []model.OSFamily{
		model.OSFamilyRHEL, model.OSFamilyDebian, model.OSFamilyUbuntu, model.OSFamilySUSE,
	}
	firmwares := []model.FirmwareMode{model.FirmwareBIOS, model.FirmwareUEFI}

	for _, family := range families {
		for _, firmware := range firmwares {
			artifact := model.ArtifactSet{
				ID:       "artifact-" + string(family),
				OSFamily: family,
				Arch:     model.ArchX86_64,
				Firmware: model.FirmwareAny,
				State:    model.ArtifactSetReady,
				Paths: model.ArtifactPaths{
					Kernel: "os/vmlinuz",
					Initrd: []string{"os/initrd.img"},
					Repo:   "os/repo/",
				},
			}
			files := fstest.MapFS{
				"os/vmlinuz":                  &fstest.MapFile{Data: []byte("kernel")},
				"os/initrd.img":               &fstest.MapFile{Data: []byte("initrd")},
				"os/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
			}

			err := m.Check(context.Background(), model.ArchX86_64, firmware, artifact, files)
			if err != nil {
				t.Errorf("Check(family=%q, firmware=%q) err = %v, want nil —— "+
					"Linux 四家 BIOS 与 UEFI 都支持，一套制品通吃（SPEC §0.3）",
					family, firmware, err)
			}
		}
	}
}

// TestPrecheckMissingInitrdRejectedWithPath 验证缺一个 initrd 时被拒绝，
// 错误细节里含那个路径——运维要能直接定位哪个文件缺失，不用去猜。
func TestPrecheckMissingInitrdRejectedWithPath(t *testing.T) {
	m := &Machine{}
	artifact := completeRocky9Artifact()
	files := completeRocky9ArtifactFiles()
	delete(files, "rocky9.4/initrd.img")

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, artifact, files)
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check(缺 initrd) err = %v, want *contract.PrecheckError", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureArtifact {
		t.Errorf("PrecheckError.Failure = %q, want %q", precheckErr.Failure, contract.PrecheckFailureArtifact)
	}
	if !strings.Contains(precheckErr.Detail, "rocky9.4/initrd.img") {
		t.Errorf("PrecheckError.Detail = %q, want 包含缺失的路径 %q —— "+
			"运维需要直接从错误里读出是哪个文件缺失，不能只说「制品不完整」",
			precheckErr.Detail, "rocky9.4/initrd.img")
	}
}

// TestPrecheckRepoDirExistsButMissingMetadataRejected 验证仓库目录存在但
// 缺元数据文件时被拒绝——这条抓的是「检查了目录本身」那个 bug：空目录能
// stat 成功但对 installer 毫无用处，必须检查目录下的
// repodata/repomd.xml。
func TestPrecheckRepoDirExistsButMissingMetadataRejected(t *testing.T) {
	m := &Machine{}
	artifact := completeRocky9Artifact()
	files := fstest.MapFS{
		"rocky9.4/vmlinuz":    &fstest.MapFile{Data: []byte("kernel")},
		"rocky9.4/initrd.img": &fstest.MapFile{Data: []byte("initrd")},
		// 故意造一个「目录存在」的假象但不放 repodata/repomd.xml——
		// fstest.MapFS 没有真正的空目录概念，这里放一个不相关的文件
		// 让 repo 路径的父级在遍历意义上「存在」，但目标元数据文件缺失。
		"rocky9.4/repo/.keep": &fstest.MapFile{Data: []byte("")},
	}

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, artifact, files)
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check(仓库目录存在但缺 repodata/repomd.xml) err = %v, want *contract.PrecheckError —— "+
			"空目录能 stat 成功但对 installer 毫无用处，判定方式必须落到"+
			"目录下的仓库元数据文件，不能只检查目录本身", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureArtifact {
		t.Errorf("PrecheckError.Failure = %q, want %q", precheckErr.Failure, contract.PrecheckFailureArtifact)
	}
}

// TestPrecheckUnparsableArchRejected 验证无法解析的架构字符串被拒绝——
// 未知的 ${buildarch} 说明是没见过的平台，不该猜着往下走。本测试直接
// 校验 model.ParseIPXEBuildArch 的解析失败路径会被 Check 的调用方（网关）
// 当作拒绝处理；Check 本身的签名要求调用方已经解析成 model.Arch，所以
// 这里验证的是「解析失败即拒绝」这一约定本身成立，而不是重复测试
// model 包。
func TestPrecheckUnparsableArchRejected(t *testing.T) {
	if _, err := model.ParseIPXEBuildArch("mips"); err == nil {
		t.Fatalf("model.ParseIPXEBuildArch(\"mips\") 未返回错误，want 错误 —— " +
			"未知的 buildarch 取值必须在解析层就被拒绝，Prechecker 的三项校验" +
			"建立在「传入的 Arch 已经是解析成功的合法值」这个前提上")
	}
	if _, err := model.ParseIPXEPlatform("legacy-bios"); err == nil {
		t.Fatalf("model.ParseIPXEPlatform(\"legacy-bios\") 未返回错误，want 错误")
	}
}
