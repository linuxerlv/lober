// 本文件测试超时扫描 ScanTimeouts（任务 9）与自动重试 AutoRetryFailedTasks
// （Phase 4 新增）。锁住 booting 超时的重试窗口决断、pending 永不触碰、
// 不可重装分类的正确使用，以及自动重试三层判定（开关/白名单/独立上限）
// 里最危险的一条：三种不可重装原因永不被自动重试。
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

// erroringMutateStore 包装 contract.Store，对指定 taskID 的
// MutateTaskFields 调用强制返回一个真实错误（不是 noop、不是
// contract.ErrConflict）。TestScanTimeoutsOneTaskFailureDoesNotAbortRound
// 验证的是「任务被并发抢先终态化」这种会走 noop 分支的撞车——
// handleTimedOutTask 在这种情况下从未真正返回非 nil error，ScanTimeouts
// 循环里的 continue 分支因此从未被一条会返回真实错误的路径覆盖过。本
// 类型让 TestScanTimeoutsRealStoreErrorDoesNotAbortRound 能构造一条真正
// 命中 continue 分支的场景。
type erroringMutateStore struct {
	contract.Store
	failTaskID string
}

var errScanFalsifyInjected = errors.New("scan_test: 注入的 Store 错误")

func (s *erroringMutateStore) MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error) {
	if taskID == s.failTaskID {
		return model.Task{}, errScanFalsifyInjected
	}
	return s.Store.MutateTaskFields(ctx, taskID, mutation)
}

// TestScanTimeoutsRealStoreErrorDoesNotAbortRound 验证 handleTimedOutTask
// 真正返回错误（不是并发终态化导致的 noop）时，ScanTimeouts 仍然继续
// 处理其余任务——这是计划任务 9「单个任务撞 CAS 冲突不该让其余任务本轮
// 不被处理」字面要求的行为，与
// TestScanTimeoutsOneTaskFailureDoesNotAbortRound 互补：那条测试验证的
// 撞车场景走的是 noop（不报错）分支，从未真正让 handleTimedOutTask 返回
// 非 nil error，本测试用真实的 Store 错误补上这个分支。
func TestScanTimeoutsRealStoreErrorDoesNotAbortRound(t *testing.T) {
	ctx := context.Background()
	backing := fake.NewStore()
	clock := fake.NewClock()

	store := &erroringMutateStore{Store: backing, failTaskID: "task-scan-store-error"}
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	preBudget, _ := model.PhaseBudget(model.PhasePre)
	enteredAt := clock.Now()

	createTaskAtPhaseEnteredAt(t, backing, "task-scan-store-error", model.TaskStateInstalling, model.PhasePre, enteredAt, 1)
	normal := createTaskAtPhaseEnteredAt(t, backing, "task-scan-store-error-normal", model.TaskStateInstalling, model.PhasePre, enteredAt, 1)

	clock.Advance(preBudget + time.Second)

	failedCount, err := m.ScanTimeouts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ScanTimeouts 返回了错误: %v，want nil —— 单个任务撞到真实 Store 错误不该让整轮失败", err)
	}
	if failedCount != 1 {
		t.Errorf("一个任务处置时撞到真实 Store 错误、一个正常超时，failedCount = %d, want 1 —— "+
			"单个任务的处置异常不该让其余任务本轮不被处理", failedCount)
	}

	got, err := backing.GetTask(ctx, normal.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("正常超时的任务 State = %q, want %q —— "+
			"另一个任务撞到真实 Store 错误不该影响它被正常处理", got.State, model.TaskStateFailed)
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

// createFailedTaskWithReason 建一个直接处于 failed 状态、带指定失败原因
// 的任务，供自动重试测试构造前提，不经过 failTask/ScanTimeouts 等状态机
// 方法——自动重试测试只关心 AutoRetryFailedTasks 对给定失败原因的任务
// 做出的判定，不关心它是怎么走到 failed 的。attemptCount 默认给 1（表示
// 「还没打满装机 token 上限」，让「可重装」这一层判定不会被
// attempt_exceeded 这个无关维度干扰——AutoRetryFailedTasks 的白名单判据
// 是 FailureReason，不是 AttemptCount）。
func createFailedTaskWithReason(t *testing.T, store contract.Store, taskID string, artifactID string, reason model.FailureReason) model.Task {
	t.Helper()
	if _, err := store.CreateArtifactSet(context.Background(), model.ArtifactSet{
		ID:       artifactID,
		OSFamily: model.OSFamilyRHEL,
		Arch:     model.ArchX86_64,
		Firmware: model.FirmwareAny,
		State:    model.ArtifactSetReady,
	}); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:            taskID,
		ArtifactSetID: artifactID,
		State:         model.TaskStateFailed,
		CurrentPhase:  model.PhasePackages,
		AttemptCount:  1,
		FailureReason: reason,
	})
	if err != nil {
		t.Fatalf("CreateTask(%s, reason=%s) 失败: %v", taskID, reason, err)
	}
	return created
}

// enabledAutoRetryConfig 是测试里反复使用的「开关开启、独立上限为 1」
// 配置——计划任务 4 建议的默认建议值，也是本文件测试的统一基线。
var enabledAutoRetryConfig = AutoRetryConfig{Enabled: true, MaxAutoRetries: 1}

// TestAutoRetry_DisabledByDefaultDoesNotRetry 验证计划任务 4「测试要锁住
// 的行为」第一条：开关默认关闭时不自动重试——即使任务的失败原因完全在
// 白名单内（boot_timeout），cfg.Enabled 为零值 false 时也不应该发生任何
// 重试。这是自动重试功能最外层的闸门：§3.2 已经说明的残留风险（机器一旦
// 网络启动就会装机）要求运维必须显式同意才能打开这个行为。
func TestAutoRetry_DisabledByDefaultDoesNotRetry(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := createFailedTaskWithReason(t, store, "task-autoretry-disabled", "artifact-autoretry-1", model.FailureReasonBootTimeout)

	var zeroValueConfig AutoRetryConfig // 零值：Enabled=false, MaxAutoRetries=0
	retried, err := m.AutoRetryFailedTasks(ctx, zeroValueConfig)
	if err != nil {
		t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
	}
	if retried != 0 {
		t.Errorf("零值配置（默认关闭）时 AutoRetryFailedTasks 返回 retried=%d, want 0 —— "+
			"开关默认必须是关闭，自动重装是需要运维明确同意的行为（计划任务 4）", retried)
	}

	got, getErr := store.GetTask(ctx, created.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("开关关闭时任务 State = %q, want 保持 %q 不变", got.State, model.TaskStateFailed)
	}
}

// TestAutoRetry_ThreeNonReinstallableReasonsNeverRetried 是本任务最重要
// 的测试：逐个验证三种不可重装的失败原因（reboot_timeout、init_timeout、
// init_failed）即使在开关开启的情况下也永不被自动重试——这三种失败意味
// 着机器已经装好、盘上可能已有数据，自动重装会擦掉它们（§3.4）。用一个
// 表驱动的子测试逐个验证，任何一个失败都必须单独可见（这条是全系统最
// 危险的逻辑，缺一个反例都不能算通过）。
func TestAutoRetry_ThreeNonReinstallableReasonsNeverRetried(t *testing.T) {
	nonReinstallable := []model.FailureReason{
		model.FailureReasonRebootTimeout,
		model.FailureReasonInitTimeout,
		model.FailureReasonInitFailed,
	}

	for _, reason := range nonReinstallable {
		t.Run(string(reason), func(t *testing.T) {
			ctx := context.Background()
			store := fake.NewStore()
			clock := fake.NewClock()
			m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

			created := createFailedTaskWithReason(t, store, "task-noreinstall-"+string(reason),
				"artifact-noreinstall-"+string(reason), reason)

			retried, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
			if err != nil {
				t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
			}
			if retried != 0 {
				t.Fatalf("failure_reason=%s（不可重装）即使开关开启，AutoRetryFailedTasks 仍返回 "+
					"retried=%d, want 0——这三种失败意味着机器已经装好、盘上可能已有数据，"+
					"自动重装会擦掉它们（SPEC §3.4），这是全系统最危险的逻辑", reason, retried)
			}

			got, getErr := store.GetTask(ctx, created.ID)
			if getErr != nil {
				t.Fatalf("GetTask 失败: %v", getErr)
			}
			if got.State != model.TaskStateFailed {
				t.Errorf("failure_reason=%s：任务 State = %q, want 保持 %q（原样留在 failed，"+
					"绝不能被重置回 pending）", reason, got.State, model.TaskStateFailed)
			}
			if got.Token != "" {
				t.Errorf("failure_reason=%s：任务 Token = %q, want 空——不应该为不可重装的失败"+
					"签发新的装机 token", reason, got.Token)
			}
		})
	}
}

// TestAutoRetry_WhitelistedReasonsAreRetried 验证白名单内的两种失败原因
// （boot_timeout、install_phase）在开关开启时会被自动重试：任务被
// Retry 重置回 pending，AttemptCount 清零、Token 清空、FailureReason
// 清空——这些断言复用 Retry 本身已经验证过的行为（lifecycle_test.go），
// 这里只确认 AutoRetryFailedTasks 确实调用到了它。
func TestAutoRetry_WhitelistedReasonsAreRetried(t *testing.T) {
	whitelisted := []model.FailureReason{
		model.FailureReasonBootTimeout,
		model.FailureReasonInstallPhase,
	}

	for _, reason := range whitelisted {
		t.Run(string(reason), func(t *testing.T) {
			ctx := context.Background()
			store := fake.NewStore()
			clock := fake.NewClock()
			m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

			created := createFailedTaskWithReason(t, store, "task-whitelist-"+string(reason),
				"artifact-whitelist-"+string(reason), reason)

			retried, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
			if err != nil {
				t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
			}
			if retried != 1 {
				t.Fatalf("failure_reason=%s（白名单内）AutoRetryFailedTasks 返回 retried=%d, want 1",
					reason, retried)
			}

			got, getErr := store.GetTask(ctx, created.ID)
			if getErr != nil {
				t.Fatalf("GetTask 失败: %v", getErr)
			}
			if got.State != model.TaskStatePending {
				t.Errorf("failure_reason=%s：任务 State = %q, want %q（自动重试应把任务重置回"+
					" pending，与手工 Retry 的效果一致）", reason, got.State, model.TaskStatePending)
			}
			if got.AttemptCount != 0 {
				t.Errorf("failure_reason=%s：AttemptCount = %d, want 0", reason, got.AttemptCount)
			}

			assertEventContainsMarker(t, store, created.ID, autoRetryEventMarker)
		})
	}
}

// TestAutoRetry_NonWhitelistedReinstallableReasonsNotRetried 验证第二层
// 判定的关键反例：precheck、hook_required、canceled 三个原因即使
// Reinstallable() 为 true（它们确实在可重装白名单内，见 model.task.go），
// 也不在「值得自动重试」这个更窄的白名单里——precheck 是「重试必然同样
// 失败」（§4.6），hook_required 是脚本或环境的确定性问题，canceled 是
// 人工深思熟虑的决定，系统都不应该自作主张重试。
func TestAutoRetry_NonWhitelistedReinstallableReasonsNotRetried(t *testing.T) {
	reinstallableButNotWorthAutoRetry := []model.FailureReason{
		model.FailureReasonPrecheck,
		model.FailureReasonHookRequired,
		model.FailureReasonCanceled,
		model.FailureReasonAttemptExceeded,
	}

	for _, reason := range reinstallableButNotWorthAutoRetry {
		t.Run(string(reason), func(t *testing.T) {
			if !reason.Reinstallable() {
				t.Fatalf("测试前提错误：%s 应该是 Reinstallable()==true，"+
					"否则这条测试和「三种不可重装」那条测试断言的是同一件事，"+
					"没有单独验证「可重装但不在自动重试白名单」这层区别", reason)
			}

			ctx := context.Background()
			store := fake.NewStore()
			clock := fake.NewClock()
			m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

			created := createFailedTaskWithReason(t, store, "task-notworth-"+string(reason),
				"artifact-notworth-"+string(reason), reason)

			retried, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
			if err != nil {
				t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
			}
			if retried != 0 {
				t.Errorf("failure_reason=%s（可重装但不在自动重试白名单）AutoRetryFailedTasks "+
					"返回 retried=%d, want 0", reason, retried)
			}

			got, getErr := store.GetTask(ctx, created.ID)
			if getErr != nil {
				t.Fatalf("GetTask 失败: %v", getErr)
			}
			if got.State != model.TaskStateFailed {
				t.Errorf("failure_reason=%s：任务 State = %q, want 保持 %q 不变", reason, got.State, model.TaskStateFailed)
			}
		})
	}
}

// TestAutoRetry_IndependentMaxCapStopsFurtherRetries 验证独立上限：
// MaxAutoRetries=1 时，同一任务第一次自动重试成功后又失败（同一个白名单
// 失败原因），第二次扫描不应该再次自动重试——必须人工介入。这条上限独立
// 于 AttemptCount（装机 token 取用次数上限，§9.1 第三道闸门），是本任务
// 新增的一条完全不同维度的计数。
func TestAutoRetry_IndependentMaxCapStopsFurtherRetries(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	taskID := "task-autoretry-cap"
	createFailedTaskWithReason(t, store, taskID, "artifact-autoretry-cap", model.FailureReasonBootTimeout)

	// 第一轮扫描：应该自动重试一次。
	retried, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
	if err != nil {
		t.Fatalf("第一轮 AutoRetryFailedTasks 返回了错误: %v", err)
	}
	if retried != 1 {
		t.Fatalf("第一轮 AutoRetryFailedTasks 返回 retried=%d, want 1", retried)
	}

	// 模拟「重试后又失败了一次」：把任务重新置为 failed、同一个白名单
	// 失败原因（不经过真实的装机流程，直接操纵 store——本测试只关心
	// AutoRetryFailedTasks 对「已经用尽自动重试次数」的任务的判定，不
	// 关心它是怎么再次失败的）。
	afterFirstRetry, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if _, err := store.CASTaskState(ctx, taskID, model.TaskStatePending, afterFirstRetry.Version, model.TaskStateFailed); err != nil {
		t.Fatalf("夹具把任务重新置为 failed 失败: %v", err)
	}
	failureReason := model.FailureReasonBootTimeout
	if _, err := store.MutateTaskFields(ctx, taskID, model.TaskMutation{FailureReason: &failureReason}); err != nil {
		t.Fatalf("夹具写 FailureReason 失败: %v", err)
	}

	// 第二轮扫描：MaxAutoRetries=1 已经用尽（上一轮的标记事件仍在），
	// 不应该再次自动重试。
	retried2, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
	if err != nil {
		t.Fatalf("第二轮 AutoRetryFailedTasks 返回了错误: %v", err)
	}
	if retried2 != 0 {
		t.Errorf("第二轮 AutoRetryFailedTasks 返回 retried=%d, want 0——独立上限 MaxAutoRetries=1"+
			"已经用尽，必须人工介入，不能无限自动重装", retried2)
	}

	got, getErr := store.GetTask(ctx, taskID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("用尽自动重试上限之后任务 State = %q, want 保持 %q 不变", got.State, model.TaskStateFailed)
	}
}

// TestAutoRetry_WritesMarkerEventForAutoOperator 验证计划任务 4「自动
// 重试要写审计记录，操作人记为系统」这条要求在本模块契约缺口下的落地
// 方式（见 appendAutoRetryMarkerEvent 文档）：写入的事件 Source 是
// model.EventSourceProvider（不是任何一个代表「人面操作」的来源），
// Message 含固定标记 autoRetryEventMarker，足以与手工点击 Retry（不会
// 写这条标记事件）区分开。
func TestAutoRetry_WritesMarkerEventForAutoOperator(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	taskID := "task-autoretry-marker"
	createFailedTaskWithReason(t, store, taskID, "artifact-autoretry-marker", model.FailureReasonBootTimeout)

	if _, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig); err != nil {
		t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
	}

	events, err := store.ListEvents(ctx, taskID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	found := false
	for _, e := range events {
		if strings.Contains(e.Message, autoRetryEventMarker) {
			found = true
			if e.Source != model.EventSourceProvider {
				t.Errorf("自动重试标记事件 Source = %q, want %q——操作人是系统而非某个用户，"+
					"必须能与人面手工操作的事件来源区分", e.Source, model.EventSourceProvider)
			}
		}
	}
	if !found {
		t.Fatalf("未找到含标记 %q 的事件，events=%+v", autoRetryEventMarker, events)
	}
}

// TestAutoRetry_HandlesMixOfTasksIndependently 验证一轮扫描同时面对多个
// 不同失败原因的任务时，各自独立判定，互不影响——不可重装的那个不会因为
// 白名单的那个先被处理就受到任何影响，返回的 retried 计数精确等于本轮真
// 正被重试的任务数。
func TestAutoRetry_HandlesMixOfTasksIndependently(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	createFailedTaskWithReason(t, store, "task-mix-retryable-1", "artifact-mix-1", model.FailureReasonBootTimeout)
	createFailedTaskWithReason(t, store, "task-mix-retryable-2", "artifact-mix-2", model.FailureReasonInstallPhase)
	createFailedTaskWithReason(t, store, "task-mix-blocked-1", "artifact-mix-3", model.FailureReasonRebootTimeout)
	createFailedTaskWithReason(t, store, "task-mix-blocked-2", "artifact-mix-4", model.FailureReasonInitFailed)
	createFailedTaskWithReason(t, store, "task-mix-blocked-3", "artifact-mix-5", model.FailureReasonPrecheck)

	retried, err := m.AutoRetryFailedTasks(ctx, enabledAutoRetryConfig)
	if err != nil {
		t.Fatalf("AutoRetryFailedTasks 返回了错误: %v", err)
	}
	if retried != 2 {
		t.Errorf("五个失败任务中只有两个在白名单内，AutoRetryFailedTasks 返回 retried=%d, want 2", retried)
	}

	blocked := []string{"task-mix-blocked-1", "task-mix-blocked-2", "task-mix-blocked-3"}
	for _, id := range blocked {
		got, err := store.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask(%s) 失败: %v", id, err)
		}
		if got.State != model.TaskStateFailed {
			t.Errorf("任务 %s 不应被自动重试，State = %q, want 保持 %q", id, got.State, model.TaskStateFailed)
		}
	}

	retryable := []string{"task-mix-retryable-1", "task-mix-retryable-2"}
	for _, id := range retryable {
		got, err := store.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("GetTask(%s) 失败: %v", id, err)
		}
		if got.State != model.TaskStatePending {
			t.Errorf("任务 %s 应该被自动重试并回到 pending，State = %q, want %q", id, got.State, model.TaskStatePending)
		}
	}
}
