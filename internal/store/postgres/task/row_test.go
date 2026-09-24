// 本文件测试 model.Task 与行结构体之间的双向映射，对应实现计划任务 3
// 的红灯步骤。纯 Go 单元测试，不需要数据库——映射测试抓的是列漏写与
// 类型错配，index tag 是否写对由任务 6 的 verifyIndexes 负责（见计划
// 任务 3「风险」段：两者不能互相替代）。
package task

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/model"
)

// TestTaskRowRoundTrip_AllFieldsSet 覆盖全字段填满的情形：model.Task →
// 行结构体 → model.Task 必须完全相等。
func TestTaskRowRoundTrip_AllFieldsSet(t *testing.T) {
	want := model.Task{
		ID:        "task-1",
		CreatedAt: 1700000000,
		CreatedBy: "admin",

		HostSN:   "SN001",
		HostMAC:  "aa:bb:cc:dd:ee:ff",
		HostUUID: "uuid-001",

		TemplateID:      "tpl-1",
		TemplateVersion: 2,
		ArtifactSetID:   "as-1",
		HookDigests:     []string{"digest-a", "digest-b"},

		FormData:       map[string]string{"hostname": "node01"},
		ResolvedParams: map[string]string{"biz_ip": "10.0.0.1"},

		State:          model.TaskStateBooting,
		StateUpdatedAt: 1700000100,
		Version:        3,

		CurrentPhase:   model.PhaseBooting,
		PhaseEnteredAt: 1700000200,

		AttemptCount: 1,
		LastSeq:      5,
		LastSignalAt: 1700000300,

		Token:             "tok-abc",
		TokenExpiresAt:    1700005000,
		FinalizeToken:     "ftok-xyz",
		FinalizeExpiresAt: 1700009000,

		FailureReason: model.FailureReasonInstallPhase,
		FailureDetail: &model.FailureDetail{
			Phase:        model.PhasePre,
			ExitCode:     1,
			HookDigest:   "digest-a",
			LogTail:      "log tail",
			SyslogTail:   "syslog tail",
			SinceLastSig: "5m",
		},
	}

	row := taskFromModel(want)
	got := row.toModel()

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("全字段填满的往返映射不相等 (-want +got):\n%s", diff)
	}
}

// TestTaskRowRoundTrip_NullableTimesZero 覆盖四个可空时间列为 0 的情形：
// model 侧的 0 必须映射成库侧的 NULL（*time.Time 为 nil），往返后仍是 0。
func TestTaskRowRoundTrip_NullableTimesZero(t *testing.T) {
	want := model.Task{
		ID:             "task-2",
		CreatedAt:      1700000000,
		State:          model.TaskStatePending,
		StateUpdatedAt: 1700000000,
		CurrentPhase:   model.PhasePending,
		PhaseEnteredAt: 1700000000,
		// LastSignalAt、TokenExpiresAt、FinalizeExpiresAt 保持零值。
	}

	row := taskFromModel(want)

	if row.LastSignalAt != nil {
		t.Errorf("LastSignalAt = %v, want nil（model 侧 0 必须映射为库侧 NULL）", row.LastSignalAt)
	}
	if row.TokenExpiresAt != nil {
		t.Errorf("TokenExpiresAt = %v, want nil", row.TokenExpiresAt)
	}
	if row.FinalizeExpiresAt != nil {
		t.Errorf("FinalizeExpiresAt = %v, want nil", row.FinalizeExpiresAt)
	}

	got := row.toModel()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("四个可空时间列为 0 时的往返映射不相等 (-want +got):\n%s", diff)
	}
}

// TestTaskRowRoundTrip_FailureDetailNil 覆盖 FailureDetail 为 nil 的情形：
// 往返后仍是 nil，不能被误变成指向零值 FailureDetail 的指针。
func TestTaskRowRoundTrip_FailureDetailNil(t *testing.T) {
	want := model.Task{
		ID:             "task-3",
		CreatedAt:      1700000000,
		State:          model.TaskStateDone,
		StateUpdatedAt: 1700000000,
		CurrentPhase:   model.PhaseDone,
		PhaseEnteredAt: 1700000000,
		FailureDetail:  nil,
	}

	row := taskFromModel(want)
	got := row.toModel()

	if got.FailureDetail != nil {
		t.Errorf("FailureDetail = %v, want nil", got.FailureDetail)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("FailureDetail 为 nil 时的往返映射不相等 (-want +got):\n%s", diff)
	}
}

// TestTaskRowRoundTrip_EmptyJSONMaps 覆盖 JSON 字段为空 map/slice（非 nil）
// 的情形：空 map/slice 必须能与 nil 区分，往返后仍是空而不是 nil。
func TestTaskRowRoundTrip_EmptyJSONMaps(t *testing.T) {
	want := model.Task{
		ID:             "task-4",
		CreatedAt:      1700000000,
		State:          model.TaskStatePending,
		StateUpdatedAt: 1700000000,
		CurrentPhase:   model.PhasePending,
		PhaseEnteredAt: 1700000000,

		HookDigests:    []string{},
		FormData:       map[string]string{},
		ResolvedParams: map[string]string{},
	}

	row := taskFromModel(want)
	got := row.toModel()

	if got.HookDigests == nil {
		t.Error("HookDigests 往返后变成了 nil，want 非 nil 的空 slice")
	}
	if got.FormData == nil {
		t.Error("FormData 往返后变成了 nil，want 非 nil 的空 map")
	}
	if got.ResolvedParams == nil {
		t.Error("ResolvedParams 往返后变成了 nil，want 非 nil 的空 map")
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("JSON 字段为空 map/slice 时的往返映射不相等 (-want +got):\n%s", diff)
	}
}

// TestTaskRow_TableName 锁住表名固定为 "tasks"，不依赖 GORM 复数推导。
func TestTaskRow_TableName(t *testing.T) {
	var row taskRow
	if got := row.TableName(); got != "tasks" {
		t.Errorf("TableName() = %q, want %q", got, "tasks")
	}
}

// TestModels_ReturnsTaskRowPointer 验证 Models() 返回的类型能被
// AutoMigrate 使用（*taskRow），供组合器收集。
func TestModels_ReturnsTaskRowPointer(t *testing.T) {
	models := Models()
	if len(models) != 1 {
		t.Fatalf("Models() 返回 %d 个元素, want 1", len(models))
	}
	if _, ok := models[0].(*taskRow); !ok {
		t.Errorf("Models()[0] 的类型是 %T, want *taskRow", models[0])
	}
}
