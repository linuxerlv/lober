// 本文件测试 hookRow 与 model.Hook 的双向映射。对应实现计划任务 9（hook
// 聚合的前一半）。
//
// 往返测试只锁映射正确性（列漏写、类型错配），锁不住 index tag 写错——
// tag 只在 AutoMigrate 时生效，纯映射测试不碰数据库（计划任务 3 的风险
// 提示同样适用于本包）。
package hook

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/model"
)

// TestHookRow_Roundtrip_FullyPopulated 验证全字段填满的 Hook 经行结构体
// 往返后与原值完全相等。
func TestHookRow_Roundtrip_FullyPopulated(t *testing.T) {
	h := model.Hook{
		ID:           "hook-01",
		Name:         "设置主机名",
		Description:  "根据 form_data 设置主机名",
		Stage:        model.HookStagePost,
		OSFamilies:   []model.OSFamily{model.OSFamilyDebian, model.OSFamilyUbuntu},
		Interpreter:  "bash",
		Body:         "#!/bin/bash\nhostnamectl set-hostname \"$1\"\n",
		BodySHA256:   model.HookDigest("#!/bin/bash\nhostnamectl set-hostname \"$1\"\n"),
		ParamsSchema: `{"type":"object"}`,
		Requires: model.HookRequires{
			ContractVersion: ">=1",
			Facts:           []string{"biz_ip"},
			Capabilities:    []string{"network"},
		},
		Required:   true,
		TimeoutSec: 120,
		Version:    3,
		State:      model.HookStateActive,
		Author:     "alice",
		Approver:   "bob",
		ApprovedAt: 1700000000,
	}

	row := fromModel(h)
	got := row.toModel()

	if diff := cmp.Diff(h, got); diff != "" {
		t.Errorf("全字段往返后不一致 (-want +got):\n%s", diff)
	}
}

// TestHookRow_Roundtrip_ApprovedAtZero 验证 approved_at 为 0（未审批）时
// 往返后仍为 0——0 必须映射成 SQL NULL，不能落地成 1970-01-01 这种假数据
// （设计文档 §5）。
func TestHookRow_Roundtrip_ApprovedAtZero(t *testing.T) {
	h := model.Hook{
		ID:      "hook-02",
		Name:    "占位",
		Stage:   model.HookStagePre,
		Version: 1,
		State:   model.HookStateDraft,
	}

	row := fromModel(h)
	got := row.toModel()

	if diff := cmp.Diff(h, got); diff != "" {
		t.Errorf("ApprovedAt=0 往返后不一致 (-want +got):\n%s", diff)
	}
	if row.ApprovedAt != nil {
		t.Errorf("row.ApprovedAt = %v, want nil（0 必须映射成 SQL NULL）", row.ApprovedAt)
	}
}

// TestHookRow_Roundtrip_EmptyOSFamiliesAndRequires 验证 OSFamilies 为空
// slice（不限家族）、Requires 为零值时往返后仍与原值相等，且不会被错误
// 归并成 nil（空 slice 与 nil slice 是两种不同的业务事实：OSFamilies 为
// 空表示不限家族，参见 model.Hook.SupportsFamily 的文档）。
func TestHookRow_Roundtrip_EmptyOSFamiliesAndRequires(t *testing.T) {
	h := model.Hook{
		ID:         "hook-03",
		Name:       "不限家族",
		Stage:      model.HookStageFirstboot,
		OSFamilies: []model.OSFamily{},
		Requires:   model.HookRequires{},
		Version:    1,
		State:      model.HookStateDraft,
	}

	row := fromModel(h)
	got := row.toModel()

	if diff := cmp.Diff(h, got); diff != "" {
		t.Errorf("空 OSFamilies/Requires 往返后不一致 (-want +got):\n%s", diff)
	}
}

// TestHookRow_Roundtrip_NilOSFamilies 验证 OSFamilies 为 nil（未显式赋值）
// 时往返后仍为 nil——与上一条空 slice 的情形区分，两者都不应该出现在
// 同一个 nil/空 混淆的问题里（pgtype 包本身已经测过 Scanner/Valuer 层，
// 这里测的是 hookRow 这一层有没有不小心把 nil 归一成了空）。
func TestHookRow_Roundtrip_NilOSFamilies(t *testing.T) {
	h := model.Hook{
		ID:      "hook-04",
		Name:    "nil families",
		Stage:   model.HookStagePre,
		Version: 1,
		State:   model.HookStateDraft,
		// OSFamilies 与 Requires.Facts/Capabilities 均为零值 nil。
	}

	row := fromModel(h)
	got := row.toModel()

	if diff := cmp.Diff(h, got); diff != "" {
		t.Errorf("nil OSFamilies 往返后不一致 (-want +got):\n%s", diff)
	}
}

// TestModels_ReturnsHookRowPointer 验证 Models() 返回的切片能直接喂给
// AutoMigrate——元素必须是行结构体的指针，不是值（GORM 的 AutoMigrate
// 约定）。
func TestModels_ReturnsHookRowPointer(t *testing.T) {
	models := Models()
	if len(models) != 1 {
		t.Fatalf("len(Models()) = %d, want 1", len(models))
	}
	if _, ok := models[0].(*hookRow); !ok {
		t.Errorf("Models()[0] 类型 = %T, want *hookRow", models[0])
	}
}
