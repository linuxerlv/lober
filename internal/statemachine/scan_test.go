// 本文件测试超时扫描 ScanTimeouts（任务 9）。锁住 booting 超时的重试窗口
// 决断、pending 永不触碰、不可重装分类的正确使用。
package statemachine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// createTaskAtPhaseEnteredAt 建一个指定状态、阶段、阶段进入时间的任务。
func createTaskAtPhaseEnteredAt(t *testing.T, store contract.Store, taskID string, state model.TaskState, phase model.Phase, phaseEnteredAt int64, attemptCount int) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:             taskID,
		State:          state,
		CurrentPhase:   phase,
		PhaseEnteredAt: phaseEnteredAt,
		AttemptCount:   attemptCount,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	return created
}

// TestScanTimeoutsNeverTouchesPendingTasks 验证 pending 任务永不出现在
// 处置结果里——一年之后再扫也不动它。
func TestScanTimeoutsNeverTouchesPendingTasks(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := createTaskAtPhaseEnteredAt(t, store, "task-scan-pending", model.TaskStatePending, model.PhasePending, clock.Now(), 0)

	oneYearLater := clock.Now() + int64(365*24*3600)
	failedCount, err := m.ScanTimeouts(ctx, oneYearLater)
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 0 {
		t.Errorf("一年之后再扫，failedCount = %d, want 0 —— 不该有任何任务被处理", failedCount)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStatePending {
		t.Errorf("pending 任务扫描后 State = %q, want 保持 %q 不变 —— "+
			"pending 阶段无预算，那台机器什么时候上电不在系统控制范围内",
			got.State, model.TaskStatePending)
	}
}

// TestScanTimeoutsBootingUnderAttemptLimitReturnsToPending 验证 booting
// 超时且 attempt 未达上限 → 回 pending，且 attempt 未增加。
func TestScanTimeoutsBootingUnderAttemptLimitReturnsToPending(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	budget, _ := model.PhaseBudget(model.PhaseBooting)
	enteredAt := clock.Now()
	created := createTaskAtPhaseEnteredAt(t, store, "task-scan-booting-retry", model.TaskStateBooting, model.PhaseBooting, enteredAt, 1)

	clock.Advance(budget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 0 {
		t.Errorf("booting 超时且 attempt 未达上限，failedCount = %d, want 0 —— "+
			"回 pending 不是判失败", failedCount)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStatePending {
		t.Errorf("booting 超时（attempt=1，未达上限）之后 State = %q, want %q", got.State, model.TaskStatePending)
	}
	if got.AttemptCount != 1 {
		t.Errorf("booting 超时回 pending 之后 AttemptCount = %d, want 保持 1 不变 —— "+
			"attempt 是在实际签发 token 时增加的，这里再加一次会让每次超时消耗两个额度",
			got.AttemptCount)
	}
}

// TestScanTimeoutsBootingAtAttemptLimitFails 验证 booting 超时且 attempt
// 已达上限 → failed，原因是 attempt 超限。
func TestScanTimeoutsBootingAtAttemptLimitFails(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	budget, _ := model.PhaseBudget(model.PhaseBooting)
	enteredAt := clock.Now()
	created := createTaskAtPhaseEnteredAt(t, store, "task-scan-booting-exceeded", model.TaskStateBooting, model.PhaseBooting, enteredAt, model.MaxBootAttempts)

	clock.Advance(budget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 1 {
		t.Errorf("booting 超时且 attempt 已达上限，failedCount = %d, want 1", failedCount)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("booting 超时（attempt 已达上限）之后 State = %q, want %q", got.State, model.TaskStateFailed)
	}
	if got.FailureReason != model.FailureReasonAttemptExceeded {
		t.Errorf("booting 超时（attempt 已达上限）之后 FailureReason = %q, want %q",
			got.FailureReason, model.FailureReasonAttemptExceeded)
	}
}

// TestScanTimeoutsInstallPhasesFail 验证三个安装阶段超时 → failed，原因
// 是安装阶段失败。
func TestScanTimeoutsInstallPhasesFail(t *testing.T) {
	installPhases := []model.Phase{model.PhasePre, model.PhasePackages, model.PhasePost}

	for _, phase := range installPhases {
		ctx := context.Background()
		store := fake.NewStore()
		clock := fake.NewClock()
		m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

		budget, ok := model.PhaseBudget(phase)
		if !ok {
			t.Fatalf("model.PhaseBudget(%q) 返回 ok=false，测试前提不成立", phase)
		}
		enteredAt := clock.Now()
		created := createTaskAtPhaseEnteredAt(t, store, "task-scan-install-"+string(phase), model.TaskStateInstalling, phase, enteredAt, 1)

		clock.Advance(budget + time.Second)

		failedCount, err := m.ScanTimeouts(ctx, clock.Now())
		if err != nil {
			t.Fatalf("ScanTimeouts(phase=%q) 失败: %v", phase, err)
		}
		if failedCount != 1 {
			t.Errorf("phase=%q 超时，failedCount = %d, want 1", phase, failedCount)
		}

		got, getErr := store.GetTask(ctx, created.ID)
		if getErr != nil {
			t.Fatalf("GetTask 失败: %v", getErr)
		}
		if got.State != model.TaskStateFailed {
			t.Errorf("phase=%q 超时之后 State = %q, want %q", phase, got.State, model.TaskStateFailed)
		}
		if got.FailureReason != model.FailureReasonInstallPhase {
			t.Errorf("phase=%q 超时之后 FailureReason = %q, want %q",
				phase, got.FailureReason, model.FailureReasonInstallPhase)
		}
	}
}

// TestScanTimeoutsRebootingFailsNotReinstallableWithMarker 验证 rebooting
// 超时 → 失败原因不可重装，且事件文案含「装好但未起来」的标记。
func TestScanTimeoutsRebootingFailsNotReinstallableWithMarker(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	budget, _ := model.PhaseBudget(model.PhaseRebooting)
	enteredAt := clock.Now()
	created := createTaskAtPhaseEnteredAt(t, store, "task-scan-rebooting", model.TaskStateRebooting, model.PhaseRebooting, enteredAt, 1)

	clock.Advance(budget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 1 {
		t.Errorf("rebooting 超时，failedCount = %d, want 1", failedCount)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.FailureReason != model.FailureReasonRebootTimeout {
		t.Errorf("rebooting 超时之后 FailureReason = %q, want %q", got.FailureReason, model.FailureReasonRebootTimeout)
	}
	if got.FailureReason.Reinstallable() {
		t.Errorf("rebooting 超时的失败原因 %q 被判定为可重装，want 不可重装 —— "+
			"系统已装好但起不来，重装会擦掉盘上数据", got.FailureReason)
	}

	assertEventContainsMarker(t, store, created.ID, "装好但未起来")
}

// TestScanTimeoutsInitializingFailsNotReinstallable 验证 initializing
// 超时 → 失败原因不可重装。
func TestScanTimeoutsInitializingFailsNotReinstallable(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	budget, _ := model.PhaseBudget(model.PhaseInitializing)
	enteredAt := clock.Now()
	created := createTaskAtPhaseEnteredAt(t, store, "task-scan-initializing", model.TaskStateInitializing, model.PhaseInitializing, enteredAt, 1)

	clock.Advance(budget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 1 {
		t.Errorf("initializing 超时，failedCount = %d, want 1", failedCount)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.FailureReason != model.FailureReasonInitTimeout {
		t.Errorf("initializing 超时之后 FailureReason = %q, want %q", got.FailureReason, model.FailureReasonInitTimeout)
	}
	if got.FailureReason.Reinstallable() {
		t.Errorf("initializing 超时的失败原因 %q 被判定为可重装，want 不可重装", got.FailureReason)
	}

	assertEventContainsMarker(t, store, created.ID, "已就绪但初始化未完成")
}

// TestScanTimeoutsReturnValueExcludesReturnedToPending 验证返回值等于
// 判失败数，不含回 pending 的。
func TestScanTimeoutsReturnValueExcludesReturnedToPending(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	bootingBudget, _ := model.PhaseBudget(model.PhaseBooting)
	enteredAt := clock.Now()

	// 一个 booting 超时但 attempt 未达上限（会回 pending，不计入
	// failedCount），一个 pre 阶段超时（会判 failed，计入 failedCount）。
	createTaskAtPhaseEnteredAt(t, store, "task-scan-mixed-retry", model.TaskStateBooting, model.PhaseBooting, enteredAt, 1)
	preBudget, _ := model.PhaseBudget(model.PhasePre)
	createTaskAtPhaseEnteredAt(t, store, "task-scan-mixed-fail", model.TaskStateInstalling, model.PhasePre, enteredAt, 1)

	longestBudget := bootingBudget
	if preBudget > longestBudget {
		longestBudget = preBudget
	}
	clock.Advance(longestBudget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 失败: %v", err)
	}
	if failedCount != 1 {
		t.Errorf("混合场景（一个回 pending，一个判 failed）failedCount = %d, want 1 —— "+
			"契约要求返回值只计真正落到 failed 的任务数，不计回 pending 的", failedCount)
	}
}

// TestScanTimeoutsOneTaskFailureDoesNotAbortRound 验证一个任务处置失败时
// 其余仍被处理——单个任务撞 CAS 冲突不该让其余任务本轮不被处理。
func TestScanTimeoutsOneTaskFailureDoesNotAbortRound(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	preBudget, _ := model.PhaseBudget(model.PhasePre)
	enteredAt := clock.Now()

	// 造两个都该判 failed 的任务；其中一个在扫描期间被别的并发操作
	// 抢先推进到终态（模拟撞车）——扫描到它时应该发现它已经是终态，
	// 跳过它（不算失败也不报错），继续处理另一个。
	racedAway := createTaskAtPhaseEnteredAt(t, store, "task-scan-raced", model.TaskStateInstalling, model.PhasePre, enteredAt, 1)
	normal := createTaskAtPhaseEnteredAt(t, store, "task-scan-normal", model.TaskStateInstalling, model.PhasePre, enteredAt, 1)

	clock.Advance(preBudget + time.Second)

	if _, err := store.CASTaskState(ctx, racedAway.ID, model.TaskStateInstalling, racedAway.Version, model.TaskStateDone); err != nil {
		t.Fatalf("夹具抢先把任务推进到 done 失败: %v", err)
	}

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 返回了错误: %v，want nil —— 单个任务的处置异常不该让整轮失败", err)
	}
	if failedCount != 1 {
		t.Errorf("一个任务被抢先终态化、一个正常超时，failedCount = %d, want 1", failedCount)
	}

	got, err := store.GetTask(ctx, normal.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("正常超时的任务 State = %q, want %q —— "+
			"另一个任务处置失败不该影响它被正常处理", got.State, model.TaskStateFailed)
	}
}

// assertEventContainsMarker 断言该任务的事件里存在一条 Message 包含给定
// 标记字符串的事件。
func assertEventContainsMarker(t *testing.T, store contract.Store, taskID, marker string) {
	t.Helper()
	events, err := store.ListEvents(context.Background(), taskID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	for _, e := range events {
		if strings.Contains(e.Message, marker) {
			return
		}
	}
	t.Errorf("任务 %q 的事件里未找到包含标记 %q 的事件，events=%+v —— "+
		"告警文案必须与「装机失败」明确区分，两者处理方式完全不同（前者不能重装）",
		taskID, marker, events)
}
