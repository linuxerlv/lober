// 本文件测试置失败 Fail、取消 Cancel 与重试 Retry（任务 8）。§3.4 是全
// 系统最重要的安全约束：三种不可重装的失败原因必须被 Retry 拒绝。
package statemachine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

func nonTerminalTaskWithState(t *testing.T, store contract.Store, taskID string, state model.TaskState) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:    taskID,
		State: state,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	return created
}

func failedTaskWithReason(t *testing.T, store contract.Store, taskID string, reason model.FailureReason) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:            taskID,
		State:         model.TaskStateFailed,
		FailureReason: reason,
		AttemptCount:  2,
		Token:         "stale-token",
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	return created
}

// ---------------------------------------------------------------------------
// Fail（§3.5 六字段诊断事件）
// ---------------------------------------------------------------------------

// TestFailWritesEventWithSixFieldsAndClockDerivedSinceLastSignal 验证置
// 失败写的事件含六个字段，且「距今多久」来自注入的时钟。
func TestFailWritesEventWithSixFieldsAndClockDerivedSinceLastSignal(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created, err := store.CreateTask(ctx, model.Task{
		ID:           "task-fail-fields",
		State:        model.TaskStateInstalling,
		CurrentPhase: model.PhasePackages,
		LastSignalAt: clock.Now(),
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	_ = created

	clock.Advance(30 * time.Second)

	result, err := m.Fail(ctx, "task-fail-fields", model.FailureReasonInstallPhase, model.FailureDetail{
		Phase:      model.PhasePackages,
		ExitCode:   1,
		HookDigest: "digest-abc",
		LogTail:    "安装包失败: 依赖冲突",
		SyslogTail: "anaconda: dependency error",
	})
	if err != nil {
		t.Fatalf("Fail 失败: %v", err)
	}
	if result.State != model.TaskStateFailed {
		t.Errorf("Fail 之后 State = %q, want %q", result.State, model.TaskStateFailed)
	}
	if result.FailureReason != model.FailureReasonInstallPhase {
		t.Errorf("Fail 之后 FailureReason = %q, want %q", result.FailureReason, model.FailureReasonInstallPhase)
	}

	events, err := store.ListEvents(ctx, "task-fail-fields", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	var diagnostic *model.TaskEvent
	for i := range events {
		if events[i].Level == model.EventLevelError {
			diagnostic = &events[i]
		}
	}
	if diagnostic == nil {
		t.Fatalf("未找到 EventLevelError 的诊断事件，events=%+v", events)
	}
	for _, want := range []string{
		string(model.PhasePackages), // phase
		"1",                         // rc
		"digest-abc",                // hook_digest
		"依赖冲突",                      // log_tail
		"dependency error",          // syslog_tail
		"30 秒前",                     // 距上次信号多久，来自注入的时钟
	} {
		if !strings.Contains(diagnostic.Message, want) {
			t.Errorf("诊断事件 Message = %q, want 包含 %q —— §3.5 要求失败事件"+
				"必须直接携带这六项信息，缺一项运维就要多猜一步", diagnostic.Message, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

// TestCancelPendingTaskNoLongerRespondsToBoot 验证取消后 pending 任务不再
// 能被身份查找命中——这是 §3.2 承认的残留风险的唯一缓解手段。
func TestCancelPendingTaskNoLongerRespondsToBoot(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created, err := store.CreateTask(ctx, model.Task{
		ID:      "task-cancel-pending",
		State:   model.TaskStatePending,
		HostSN:  "SN-cancel-pending",
		HostMAC: "aa:bb:cc:dd:ee:99",
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	// 取消前：身份查找应能命中。
	if _, err := store.FindPendingTaskByIdentity(ctx, created.HostSN, "", ""); err != nil {
		t.Fatalf("取消前按 SN 查 pending 任务失败: %v，want 命中", err)
	}

	result, err := m.Cancel(ctx, created.ID)
	if err != nil {
		t.Fatalf("Cancel 失败: %v", err)
	}
	if result.State != model.TaskStateFailed {
		t.Errorf("Cancel 之后 State = %q, want %q", result.State, model.TaskStateFailed)
	}
	if result.FailureReason != model.FailureReasonCanceled {
		t.Errorf("Cancel 之后 FailureReason = %q, want %q", result.FailureReason, model.FailureReasonCanceled)
	}

	_, err = store.FindPendingTaskByIdentity(ctx, created.HostSN, "", "")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("取消后按 SN 查 pending 任务，err = %v, want errors.Is(err, ErrNotFound) —— "+
			"pending 任务 cancel 后即不再响应引导，这是发现建错任务后唯一的补救手段", err)
	}
}

// ---------------------------------------------------------------------------
// Retry：禁止重装落在这里
// ---------------------------------------------------------------------------

// TestRetryRejectsThreeNonReinstallableReasons 验证三种不可重装原因逐一
// 被拒绝，返回的是专门的拒绝错误。
func TestRetryRejectsThreeNonReinstallableReasons(t *testing.T) {
	ctx := context.Background()
	nonReinstallable := []model.FailureReason{
		model.FailureReasonRebootTimeout,
		model.FailureReasonInitTimeout,
		model.FailureReasonInitFailed,
	}

	for _, reason := range nonReinstallable {
		store := fake.NewStore()
		clock := fake.NewClock()
		m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

		created := failedTaskWithReason(t, store, "task-retry-reject-"+string(reason), reason)

		_, err := m.Retry(ctx, created.ID)
		if !errors.Is(err, contract.ErrNotReinstallable) {
			t.Errorf("Retry(失败原因=%q) err = %v, want errors.Is(err, contract.ErrNotReinstallable) —— "+
				"这台机器已经装好且数据可能已在盘上，重装会擦掉；"+
				"拒绝必须在状态机里，不是靠 UI 隐藏按钮", reason, err)
		}

		got, getErr := store.GetTask(ctx, created.ID)
		if getErr != nil {
			t.Fatalf("GetTask 失败: %v", getErr)
		}
		if got.State != model.TaskStateFailed {
			t.Errorf("Retry 被拒绝后 State = %q, want 保持 %q 不变", got.State, model.TaskStateFailed)
		}
	}
}

// TestRetryAcceptsSixReinstallableReasons 验证六种可重装原因逐一被接受，
// 特别是 precheck——这条防止将来误扩大拒绝范围。
func TestRetryAcceptsSixReinstallableReasons(t *testing.T) {
	ctx := context.Background()
	reinstallable := []model.FailureReason{
		model.FailureReasonBootTimeout,
		model.FailureReasonAttemptExceeded,
		model.FailureReasonInstallPhase,
		model.FailureReasonHookRequired,
		model.FailureReasonPrecheck,
		model.FailureReasonCanceled,
	}

	for _, reason := range reinstallable {
		store := fake.NewStore()
		clock := fake.NewClock()
		m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

		created := failedTaskWithReason(t, store, "task-retry-accept-"+string(reason), reason)

		result, err := m.Retry(ctx, created.ID)
		if err != nil {
			t.Errorf("Retry(失败原因=%q) 返回了错误: %v, want nil —— "+
				"§8.2 只显式要求拒绝三类超时，这六种必须被接受，"+
				"否则运维会失去正当的恢复途径转而删任务重建，绕过所有审计", reason, err)
			continue
		}
		if result.State != model.TaskStatePending {
			t.Errorf("Retry(失败原因=%q) 之后 State = %q, want %q", reason, result.State, model.TaskStatePending)
		}
	}
}

// TestRetryResetsAttemptTokenAndState 验证 retry 后 attempt 归零、token
// 清空、状态回 pending。
func TestRetryResetsAttemptTokenAndState(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := failedTaskWithReason(t, store, "task-retry-reset", model.FailureReasonInstallPhase)

	result, err := m.Retry(ctx, created.ID)
	if err != nil {
		t.Fatalf("Retry 失败: %v", err)
	}
	if result.AttemptCount != 0 {
		t.Errorf("Retry 之后 AttemptCount = %d, want 0", result.AttemptCount)
	}
	if result.Token != "" {
		t.Errorf("Retry 之后 Token = %q, want 空", result.Token)
	}
	if result.State != model.TaskStatePending {
		t.Errorf("Retry 之后 State = %q, want %q", result.State, model.TaskStatePending)
	}
	if result.CurrentPhase != model.PhasePending {
		t.Errorf("Retry 之后 CurrentPhase = %q, want %q", result.CurrentPhase, model.PhasePending)
	}
}
