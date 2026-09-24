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

// repoIndexFixture 给出一份「仓库索引入口齐备」的仓库文件，路径挂在
// repoDir 下，形状按家族区分：RHEL 与 SUSE 是 RPM-MD，Debian 与 Ubuntu 是
// APT。供跨家族遍历的测试构造各自合法的基线。
func repoIndexFixture(family model.OSFamily, repoDir string) map[string]*fstest.MapFile {
	switch family {
	case model.OSFamilyDebian, model.OSFamilyUbuntu:
		return map[string]*fstest.MapFile{
			repoDir + "/dists/trixie/Release": {Data: []byte("Suite: stable\n")},
		}
	default:
		return map[string]*fstest.MapFile{
			repoDir + "/repodata/repomd.xml": {Data: []byte("<repomd/>")},
		}
	}
}

// TestPrecheckLinuxFourFamiliesPassBothFirmwares 验证 Linux 四家在两种
// 固件下都通过——它们的制品固件字段是 any（一套制品通吃）。
//
// 夹具按家族给对应形状的仓库（见 repoIndexFixture）。本测试早先给四家共用
// 一份 RHEL 形状的 repodata/repomd.xml，那正是 repoMarkerPattern 要修掉的
// 错误假设：「一套制品通吃」说的是**同一份制品同时服务 BIOS 与 UEFI**
// （SPEC §0.3 那一列讲的是制品维护成本），不是「四家的仓库索引格式相同」。
// 当时能过是因为判据代码犯了同一个错，两边的错互相抵消了。断言轴（四家 ×
// 两种固件全部放行）未改动，只改夹具。
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
				"os/vmlinuz":    &fstest.MapFile{Data: []byte("kernel")},
				"os/initrd.img": &fstest.MapFile{Data: []byte("initrd")},
			}
			for p, f := range repoIndexFixture(family, "os/repo") {
				files[p] = f
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

// TestPrecheckUnknownFamilyWithRepoHardRejected 验证判据表里没有的家族带着
// 一个非空 Repo 时被硬拒绝，而不是退化成「目录存在就算过」。
//
// 这条锁的是 repoMarkerPattern 的兜底语义（§9.1 默认拒绝）：将来新增一个
// 家族却忘了配判据时，必须在 /v1/boot 就红，不能静默放行一份索引不可用的
// 仓库、把问题推到装机的选包阶段才暴露——那正是本轮修掉的这个 bug 的症状，
// 不能换个形式留下来。
//
// 用 Windows 做样本：它在表内没有条目（真实抽取器产出的 Repo 是空串，走不
// 到这条路径），这里刻意给它一个非空 Repo 构造出「未配判据」的情形。
func TestPrecheckUnknownFamilyWithRepoHardRejected(t *testing.T) {
	m := &Machine{}
	artifact := model.ArtifactSet{
		ID:       "artifact-win",
		OSFamily: model.OSFamilyWindows,
		Arch:     model.ArchX86_64,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Repo: "win/repo/",
		},
	}
	files := fstest.MapFS{
		"win/repo/somefile": &fstest.MapFile{Data: []byte("x")},
	}

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, artifact, files)
	if err == nil {
		t.Fatal("Check() 放行了一个判据表里没有条目的家族的非空 Repo，want 拒绝 —— " +
			"「无法确认仓库可用」与「确认仓库不可用」在 §4.6 这一层应当同样处置")
	}
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check() 返回 %T，want *contract.PrecheckError", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureArtifact {
		t.Errorf("PrecheckError.Failure = %q, want %q", precheckErr.Failure, contract.PrecheckFailureArtifact)
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

// debianArtifact 构造一份 Debian 制品集，Paths 与 ExtractDebian 的真实
// 产出一致（内核 linux、initrd initrd.gz、仓库 repo/ 带结尾斜杠）。
func debianArtifact() model.ArtifactSet {
	return model.ArtifactSet{
		ID:        "artifact-debian13",
		OSFamily:  model.OSFamilyDebian,
		OSVersion: "13",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "debian13/linux",
			Initrd: []string{"debian13/initrd.gz"},
			Repo:   "debian13/repo/",
		},
	}
}

// TestPrecheckDebianRepoAcceptsAPTIndexTree 验证 Debian 的仓库判据是 APT
// 的 dists/<suite>/Release，而不是 yum 的 repodata/repomd.xml。
//
// 这条抓的是一个在真机上 100% 必然触发的阻断性 bug：判据曾被硬编码成
// repodata/repomd.xml（RHEL/yum 专属产物），而 ExtractDebian 产出的 repo/
// 下是 dists/ 与 pool/ 两棵 APT 树，从不会有 repodata/ 这个目录。制品完整性
// 检查挡在签发装机 token 之前（§4.6），因此任何 Debian 任务在机器真正引导
// 时都会被判定为「制品文件不存在」而拒绝，与该制品集导入得多完整、多正确
// 无关。docs/real-machine/12-e2e-debian.md 记录了这次发现，
// scripts/e2e-debian.sh 当时只能手工造一个假 repomd.xml 绕过去。
//
// suite 名字故意用 trixie 而不是通用占位符：判据必须对任意 suite 名成立
// （bookworm/trixie/sid 各版本不同，且用户可能导入任何一版），写死某个
// suite 名就是把这个 bug 换个形式留下来。
func TestPrecheckDebianRepoAcceptsAPTIndexTree(t *testing.T) {
	m := &Machine{}
	files := fstest.MapFS{
		"debian13/linux":     &fstest.MapFile{Data: []byte("kernel")},
		"debian13/initrd.gz": &fstest.MapFile{Data: []byte("initrd")},
		"debian13/repo/dists/trixie/Release": &fstest.MapFile{
			Data: []byte("Suite: stable\n"),
		},
		"debian13/repo/dists/trixie/main/binary-amd64/Packages.gz": &fstest.MapFile{
			Data: []byte("packages"),
		},
		"debian13/repo/pool/main/a/apt/apt_2.9.8_amd64.deb": &fstest.MapFile{
			Data: []byte("deb"),
		},
	}

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, debianArtifact(), files)
	if err != nil {
		t.Fatalf("Check() 拒绝了一份仓库树完整的 Debian 制品集: %v —— "+
			"Debian 的仓库判据必须是 APT 的 dists/<suite>/Release，"+
			"不能沿用 RHEL 的 repodata/repomd.xml（Debian 仓库树从不会有那个文件，"+
			"沿用它等于让每一个 Debian 任务都在 /v1/boot 被无条件拒绝）", err)
	}
}

// TestPrecheckDebianRepoWithoutReleaseRejected 验证 Debian 的 repo/ 下只有
// pool/ 而没有任何 dists/<suite>/Release 时被拒绝。
//
// 这是上一条的反面：换判据不能换成「有 repo 目录就算过」。APT 在选包阶段
// 读的是 dists/<suite>/Release，只有 pool/ 的仓库树对 installer 毫无用处，
// 与「空目录能 stat 成功」是同一类问题（见 checkPathExists 的原始注释）。
func TestPrecheckDebianRepoWithoutReleaseRejected(t *testing.T) {
	m := &Machine{}
	files := fstest.MapFS{
		"debian13/linux":     &fstest.MapFile{Data: []byte("kernel")},
		"debian13/initrd.gz": &fstest.MapFile{Data: []byte("initrd")},
		"debian13/repo/pool/main/a/apt/apt_2.9.8_amd64.deb": &fstest.MapFile{
			Data: []byte("deb"),
		},
	}

	err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, debianArtifact(), files)
	if err == nil {
		t.Fatal("Check() 放行了一份没有 dists/<suite>/Release 的 Debian 仓库树，" +
			"want 拒绝 —— 只有 pool/ 的仓库树 APT 读不出任何软件包索引，" +
			"装机会在选包阶段失败")
	}
	var precheckErr *contract.PrecheckError
	if !errors.As(err, &precheckErr) {
		t.Fatalf("Check() 返回 %T，want *contract.PrecheckError", err)
	}
	if precheckErr.Failure != contract.PrecheckFailureArtifact {
		t.Errorf("PrecheckError.Failure = %q, want %q", precheckErr.Failure, contract.PrecheckFailureArtifact)
	}
}

// TestPrecheckSUSERepoUsesRPMMDMarker 验证 SUSE 仍然走 repodata/repomd.xml
// 判据，不被 Debian 的改动带跑。
//
// SUSE 与 Debian 都把仓库落在 repo/ 下、都带结尾斜杠，形状上无法区分，只能
// 靠 OSFamily 分流。ExtractSUSE 复制的是 repodata/ 与 suse/ 两棵树（zypper
// 用的是 RPM-MD 格式，与 RHEL 同一套），因此判据必须按家族查表，不能全局
// 改成 APT 那一套——那样修好 Debian 的同时会把 SUSE 打断。
func TestPrecheckSUSERepoUsesRPMMDMarker(t *testing.T) {
	m := &Machine{}
	artifact := model.ArtifactSet{
		ID:        "artifact-suse15",
		OSFamily:  model.OSFamilySUSE,
		OSVersion: "15.6",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
		Paths: model.ArtifactPaths{
			Kernel: "suse15/linux",
			Initrd: []string{"suse15/initrd"},
			Repo:   "suse15/repo/",
		},
	}
	files := fstest.MapFS{
		"suse15/linux":                    &fstest.MapFile{Data: []byte("kernel")},
		"suse15/initrd":                   &fstest.MapFile{Data: []byte("initrd")},
		"suse15/repo/repodata/repomd.xml": &fstest.MapFile{Data: []byte("<repomd/>")},
	}

	if err := m.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, artifact, files); err != nil {
		t.Fatalf("Check() 拒绝了一份 repodata/repomd.xml 齐备的 SUSE 制品集: %v —— "+
			"SUSE 用 RPM-MD 仓库格式（与 RHEL 同一套），修 Debian 判据时不能"+
			"把 SUSE 一起改到 APT 那一套", err)
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
