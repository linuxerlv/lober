// 本文件测试 row.go 的行结构体与 model.ArtifactSet 之间的双向映射，对应
// 实现计划任务 8（artifact 三分之一）。
//
// 映射测试是纯内存操作，不需要数据库连接——row 与 model 之间的转换函数
// 本身不触碰 *gorm.DB，验证往返相等即可（与计划任务 3 对 task 包定的
// 测试形态一致：往返测试抓列漏写与类型错配，抓不住 index tag 写错，
// 但本包没有 index tag，不存在这个缺口）。
package artifact

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/model"
)

// TestRowRoundTrip_FullyPopulated 覆盖全字段填满的情况，含非空的 Initrd
// 切片与 Extra map——这两类字段最容易在序列化时掉字段。
func TestRowRoundTrip_FullyPopulated(t *testing.T) {
	want := model.ArtifactSet{
		// Channel 显式写成 installer：fromModel 写入时归一化（零值折叠成
		// installer，库里不出现两种表示同一件事的值），因此往返回来必然是这个
		// 值，夹具里留零值会让 cmp.Diff 报一处预期内的差异。
		Channel:         model.ChannelInstaller,
		ID:              "artifact-rocky9",
		Name:            "rocky9.4",
		OSFamily:        model.OSFamilyRHEL,
		OSVersion:       "9.4",
		Arch:            model.ArchX86_64,
		Firmware:        model.FirmwareAny,
		SourceISOSHA256: "abc123",
		State:           model.ArtifactSetReady,
		ManifestSHA256:  "manifest-sha",
		Paths: model.ArtifactPaths{
			Kernel: "rocky9.4/vmlinuz",
			Initrd: []string{"rocky9.4/initrd.img", "rocky9.4/initrd-extra.img"},
			Repo:   "rocky9.4/repo/",
			Extra: map[string]string{
				"boot_cfg": "rocky9.4/boot.cfg",
				"wimboot":  "rocky9.4/wimboot",
			},
		},
		FailReason: "",
	}

	row := fromModel(want)
	got := row.toModel()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("往返后与原值不符（-want +got）:\n%s", diff)
	}
}

// TestRowRoundTrip_EmptyJSONFields 覆盖 JSON 字段为空 map/slice（不是
// nil）的情况——nil 与空集合在 JSON 层是不同的值，必须能区分，否则
// 「没填」与「填了空表单」会被混淆。
func TestRowRoundTrip_EmptyJSONFields(t *testing.T) {
	want := model.ArtifactSet{
		// Channel 显式写成 installer：fromModel 写入时归一化（零值折叠成
		// installer，库里不出现两种表示同一件事的值），因此往返回来必然是这个
		// 值，夹具里留零值会让 cmp.Diff 报一处预期内的差异。
		Channel:   model.ChannelInstaller,
		ID:        "artifact-empty",
		Name:      "empty-set",
		OSFamily:  model.OSFamilyDebian,
		OSVersion: "12",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetImporting,
		Paths: model.ArtifactPaths{
			Initrd: []string{},
			Extra:  map[string]string{},
		},
	}

	row := fromModel(want)
	got := row.toModel()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("往返后与原值不符（-want +got）:\n%s", diff)
	}
}

// TestRowRoundTrip_NilJSONFields 覆盖 Paths 内 Initrd/Extra 为 nil 的
// 情况——CreateArtifactSet 登记时通常只知道名字与本地路径，Paths 此刻
// 是零值，Initrd/Extra 都是 nil。
func TestRowRoundTrip_NilJSONFields(t *testing.T) {
	want := model.ArtifactSet{
		// Channel 显式写成 installer：fromModel 写入时归一化（零值折叠成
		// installer，库里不出现两种表示同一件事的值），因此往返回来必然是这个
		// 值，夹具里留零值会让 cmp.Diff 报一处预期内的差异。
		Channel:   model.ChannelInstaller,
		ID:        "artifact-nil-json",
		Name:      "nil-json-set",
		OSFamily:  model.OSFamilyWindows,
		OSVersion: "2022",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareUEFI,
		State:     model.ArtifactSetImporting,
		Paths:     model.ArtifactPaths{}, // Initrd、Extra 均为 nil
	}

	row := fromModel(want)
	got := row.toModel()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("往返后与原值不符（-want +got）:\n%s", diff)
	}
	if got.Paths.Initrd != nil {
		t.Errorf("got.Paths.Initrd = %#v, want nil —— 零值 Paths 不应在往返后长出一个空切片", got.Paths.Initrd)
	}
	if got.Paths.Extra != nil {
		t.Errorf("got.Paths.Extra = %#v, want nil —— 零值 Paths 不应在往返后长出一个空 map", got.Paths.Extra)
	}
}

// TestRowRoundTrip_FailReasonWithSpecialChars 覆盖失败原因含中文、引号、
// 换行——FailReason 是自由文本，落库编码不能因为这些字符出错或截断。
func TestRowRoundTrip_FailReasonWithSpecialChars(t *testing.T) {
	want := model.ArtifactSet{
		// Channel 显式写成 installer：fromModel 写入时归一化（零值折叠成
		// installer，库里不出现两种表示同一件事的值），因此往返回来必然是这个
		// 值，夹具里留零值会让 cmp.Diff 报一处预期内的差异。
		Channel:    model.ChannelInstaller,
		ID:         "artifact-fail-reason",
		Name:       "failed-set",
		OSFamily:   model.OSFamilyESXi,
		OSVersion:  "8.0",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		State:      model.ArtifactSetFailed,
		FailReason: "识别失败：无法解析 boot.cfg\n第二行，含\"引号\"与'单引号'",
	}

	row := fromModel(want)
	got := row.toModel()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("往返后与原值不符（-want +got）:\n%s", diff)
	}
}

// TestModels 验证 Models() 返回的类型能被 AutoMigrate 收集——只断言长度
// 与类型，不连数据库（连库验证是 store_test.go 的职责）。
func TestModels(t *testing.T) {
	models := Models()
	if len(models) != 1 {
		t.Fatalf("Models() 返回 %d 个类型, want 1", len(models))
	}
	if _, ok := models[0].(*artifactSetRow); !ok {
		t.Errorf("Models()[0] 的类型是 %T, want *artifactSetRow", models[0])
	}
}
