// 本文件锁住 storeBackedWimArchDetector（imports.WimArchDetector 的生产
// 实现）的行为。对应本轮 m2 轨任务：给 imports.Job.ConfigureWindows 提供
// 生产可用的 WimArchDetector，依据 SPEC §0.3、§2.1、§4.6，以及
// internal/imports/extract_windows.go 文件末尾的契约缺口说明第 2 条
// （「真实 WIM 格式解析需要专门的库，不在本模块允许的依赖白名单内」）。
//
// 设计决策（保守版本，见 wimarch.go 文件头注释的完整论证）：本探测器不
// 解析 boot.wim 的二进制内容，而是信任运维登记 artifact_set 时填写的
// model.ArtifactSet.Firmware 字段——这个字段本身就是 SPEC §2.1 要求的
// 「同一 Windows 版本按固件导两个 artifact_set」的显式声明。
//
// 已知契约缺口（写入 findings 供编排方确认）：因为 DetectFirmware 读到
// 的正是 ExtractWindows 稍后要拿来比对的同一个 declaredFirmware，本探测
// 器目前无法提供独立于声明值的第二个信息源，ExtractWindows 里
// 「actualFirmware != declaredFirmware」这条一致性校验在本实现下永远为
// false（恒等）——真实的位宽不一致（比如运维手动放错了文件）不会被这层
// 校验拦住。真实 WIM 头部解析是本文件明确未覆盖的已知缺口。
package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// fakeArtifactSetGetter 是 storeBackedWimArchDetector 依赖的窄接口
// （见 wimarch.go 的 artifactSetGetter）的测试替身：按 id 查表返回预设的
// model.ArtifactSet，不接入真实 SQLite。
type fakeArtifactSetGetter struct {
	sets map[string]model.ArtifactSet
	err  error
}

func (f fakeArtifactSetGetter) GetArtifactSet(_ context.Context, id string) (model.ArtifactSet, error) {
	if f.err != nil {
		return model.ArtifactSet{}, f.err
	}
	set, ok := f.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return set, nil
}

// TestStoreBackedWimArchDetector_UEFI声明值被如实返回 锁住「保守版本」
// 的核心行为：给定一个位于 <base>/<artifact_set_id>/boot.wim 路径下的
// 文件，探测器应查出该 artifact_set 声明的固件并原样返回——本用例覆盖
// uefi 这一具体取值。
func TestStoreBackedWimArchDetector_UEFI声明值被如实返回(t *testing.T) {
	getter := fakeArtifactSetGetter{sets: map[string]model.ArtifactSet{
		"win2022-uefi": {ID: "win2022-uefi", Firmware: model.FirmwareUEFI},
	}}
	detector := newStoreBackedWimArchDetector(getter)

	bootWIMPath := filepath.Join("C:", "windows-sources", "win2022-uefi", "boot.wim")
	got, err := detector.DetectFirmware(bootWIMPath)
	if err != nil {
		t.Fatalf("DetectFirmware 返回错误: %v；声明值存在且合法时不应报错", err)
	}
	if got != model.FirmwareUEFI {
		t.Errorf("DetectFirmware = %q, want %q（应如实返回该 artifact_set 声明的固件）",
			got, model.FirmwareUEFI)
	}
}

// TestStoreBackedWimArchDetector_BIOS声明值被如实返回 是上一条的对照组，
// 覆盖 bios 取值——两种固件各自命中是任务明确要求的测试点。
func TestStoreBackedWimArchDetector_BIOS声明值被如实返回(t *testing.T) {
	getter := fakeArtifactSetGetter{sets: map[string]model.ArtifactSet{
		"win2022-bios": {ID: "win2022-bios", Firmware: model.FirmwareBIOS},
	}}
	detector := newStoreBackedWimArchDetector(getter)

	bootWIMPath := filepath.Join("/srv/windows-sources", "win2022-bios", "boot.wim")
	got, err := detector.DetectFirmware(bootWIMPath)
	if err != nil {
		t.Fatalf("DetectFirmware 返回错误: %v", err)
	}
	if got != model.FirmwareBIOS {
		t.Errorf("DetectFirmware = %q, want %q", got, model.FirmwareBIOS)
	}
}

// TestStoreBackedWimArchDetector_固件声明为any时被拒绝 锁住 SPEC §2.1
// 明确的硬约束：Windows 的固件字段必须取具体值（uefi/bios），any 是
// Linux 四家「一套制品通吃」的专属语义。若某个 artifact_set 错误地把
// Windows 制品集声明成 any，探测器必须拒绝而不是把 any 原样返回给
// ExtractWindows（那样会导致后续 declaredFirmware != model.FirmwareUEFI
// && != model.FirmwareBIOS 的校验在 ExtractWindows 里才第二次触发，
// 但本探测器自己也应该在这一层就明确拒绝，不依赖调用方的第二道校验兜底）。
func TestStoreBackedWimArchDetector_固件声明为any时被拒绝(t *testing.T) {
	getter := fakeArtifactSetGetter{sets: map[string]model.ArtifactSet{
		"win2022-any": {ID: "win2022-any", Firmware: model.FirmwareAny},
	}}
	detector := newStoreBackedWimArchDetector(getter)

	bootWIMPath := filepath.Join("/srv/windows-sources", "win2022-any", "boot.wim")
	_, err := detector.DetectFirmware(bootWIMPath)
	if err == nil {
		t.Fatal("固件声明为 any 时 DetectFirmware 应返回错误" +
			"（SPEC §2.1：Windows 必须取具体固件值，any 是 Linux 四家专属语义）")
	}
}

// TestStoreBackedWimArchDetector_制品集不存在时报错 锁住「找不到就报错，
// 不能假设一个默认固件继续」这条设计（与 extract_windows.go
// WimArchDetector 接口文档的要求一致：探测失败必须返回 error，调用方据此
// 整体拒绝导入）。
func TestStoreBackedWimArchDetector_制品集不存在时报错(t *testing.T) {
	getter := fakeArtifactSetGetter{sets: map[string]model.ArtifactSet{}}
	detector := newStoreBackedWimArchDetector(getter)

	bootWIMPath := filepath.Join("/srv/windows-sources", "does-not-exist", "boot.wim")
	_, err := detector.DetectFirmware(bootWIMPath)
	if err == nil {
		t.Fatal("artifact_set 查不到时 DetectFirmware 应返回错误，不能假设默认固件继续")
	}
}

// TestStoreBackedWimArchDetector_底层Store出错时报错透传 确认底层
// GetArtifactSet 返回的错误（例如数据库连接问题）会被 DetectFirmware
// 原样透传（wrap 后仍可用 errors.Is/errors.As 或至少文本可读），不会被
// 静默吞掉或替换成一个误导性的默认值。
func TestStoreBackedWimArchDetector_底层Store出错时报错透传(t *testing.T) {
	underlying := errors.New("模拟数据库连接失败")
	getter := fakeArtifactSetGetter{err: underlying}
	detector := newStoreBackedWimArchDetector(getter)

	bootWIMPath := filepath.Join("/srv/windows-sources", "whatever", "boot.wim")
	_, err := detector.DetectFirmware(bootWIMPath)
	if err == nil {
		t.Fatal("底层 store 出错时 DetectFirmware 应报错")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("DetectFirmware 的错误未包裹底层错误 %v，got: %v"+
			"（运维排障需要能看到根因，不能只看到一句"+
			"「探测失败」而不知道是不是数据库本身出了问题）", underlying, err)
	}
}

// TestStoreBackedWimArchDetector_路径不含子目录时报错 锁住探测器解析
// artifact_set_id 的方式与 fileWindowsSourcesResolver 的目录约定
// （<base>/<artifact_set_id>/boot.wim）绑定：若传入的路径不含这一层
// 子目录（例如直接是 "boot.wim" 或 "/boot.wim"），无法反解出
// artifact_set_id，必须报错而不是猜一个空字符串去查表。
func TestStoreBackedWimArchDetector_路径不含子目录时报错(t *testing.T) {
	getter := fakeArtifactSetGetter{sets: map[string]model.ArtifactSet{}}
	detector := newStoreBackedWimArchDetector(getter)

	_, err := detector.DetectFirmware("boot.wim")
	if err == nil {
		t.Fatal("路径不含 <artifact_set_id> 这一层子目录时应报错，" +
			"不能反解出一个错误的空 ID 继续查表")
	}
}
