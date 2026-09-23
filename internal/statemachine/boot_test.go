// 本文件测试开始引导 StartBoot（任务 4）与标记应答文件已取
// MarkAnswerFetched（任务 5）。锁住 §4.2 步骤顺序这条硬约束，以及 Phase 4
// 新增的并发准入检查（§10 失败模式 11）接入点。
package statemachine

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// alwaysPassPrechecker 是一个总是通过的 Prechecker 测试替身。
type alwaysPassPrechecker struct{}

func (alwaysPassPrechecker) Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error {
	return nil
}

// alwaysFailPrechecker 是一个总是拒绝的 Prechecker 测试替身，用于验证
// 「precheck 不过时 attempt 不增加」这条顺序约束。
type alwaysFailPrechecker struct{}

func (alwaysFailPrechecker) Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error {
	return &contract.PrecheckError{Failure: contract.PrecheckFailureArch, Detail: "测试用固定拒绝"}
}

func newBootTestMachine(store contract.Store, clock contract.Clock, prechecker contract.Prechecker) *Machine {
	return New(store, clock, &Machine{}, prechecker, nil, noopEventBus{})
}

// noopEventBus 是 contract.EventBus 的测试替身：Publish 什么都不做，
// Subscribe 也什么都不做。
type noopEventBus struct{}

func (noopEventBus) Publish(event contract.LifecycleEvent, task model.Task)             {}
func (noopEventBus) Subscribe(event contract.LifecycleEvent, callback func(model.Task)) {}

func mustCreateArtifact(t *testing.T, store contract.Store, set model.ArtifactSet) model.ArtifactSet {
	t.Helper()
	created, err := store.CreateArtifactSet(context.Background(), set)
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	return created
}

func pendingTaskWithArtifact(t *testing.T, store contract.Store, taskID, artifactID string) model.Task {
	t.Helper()
	mustCreateArtifact(t, store, model.ArtifactSet{
		ID:       artifactID,
		OSFamily: model.OSFamilyRHEL,
		Arch:     model.ArchX86_64,
		Firmware: model.FirmwareAny,
		State:    model.ArtifactSetReady,
	})
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:            taskID,
		ArtifactSetID: artifactID,
		State:         model.TaskStatePending,
		CurrentPhase:  model.PhasePending,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	return created
}

// TestBeginBootPrecheckFailureDoesNotIncrementAttempt 验证五步顺序里最
// 关键的一条：precheck 不过时 attempt 没有增加。用一个总是失败的
// precheck 验证——重试必然同样失败，消耗 attempt 只会让运维少三次真正
// 的重试机会。
func TestBeginBootPrecheckFailureDoesNotIncrementAttempt(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysFailPrechecker{})

	pendingTaskWithArtifact(t, store, "task-precheck-fail", "artifact-1")

	result, err := m.StartBoot(ctx, "task-precheck-fail", model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("StartBoot 在 precheck 拒绝时返回了错误: %v，want nil error —— "+
			"precheck 不通过是 StartBoot 正确识别并处理的业务结果（置 failed），"+
			"不是调用本身失败；调用方（网关）靠返回的 Task.State 判断该渲染"+
			"booting 菜单还是 sanboot 失败响应", err)
	}
	if result.State != model.TaskStateFailed {
		t.Errorf("StartBoot 返回值 State = %q, want %q", result.State, model.TaskStateFailed)
	}

	got, getErr := store.GetTask(ctx, "task-precheck-fail")
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.AttemptCount != 0 {
		t.Errorf("precheck 拒绝后 AttemptCount = %d, want 0 —— "+
			"precheck 失败不是可重试的偶发故障，重试必然同样失败，"+
			"消耗 attempt 只会让运维少三次真正的重试机会", got.AttemptCount)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("precheck 拒绝后 State = %q, want %q", got.State, model.TaskStateFailed)
	}
	if got.FailureReason != model.FailureReasonPrecheck {
		t.Errorf("precheck 拒绝后 FailureReason = %q, want %q", got.FailureReason, model.FailureReasonPrecheck)
	}
	if got.Token != "" {
		t.Errorf("precheck 拒绝后 Token = %q, want 空——不应该签发 token", got.Token)
	}
}

// TestBeginBootAttemptExceededFailsWithoutToken 验证 attempt 达到上限时
// 置 failed 且不签 token——上限判定要在签 token 之前，否则第四次也拿到了
// token。
func TestBeginBootAttemptExceededFailsWithoutToken(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	pendingTaskWithArtifact(t, store, "task-attempt-exceeded", "artifact-1")

	// 连续调用三次，把 attempt 推到上限（MaxBootAttempts=3）；每次成功后
	// 任务会变成 booting，为了让下一次调用仍能命中「pending 检查」通过，
	// 这里在每次成功后手工把状态拨回 pending（模拟机器还没真正走到
	// installer，又来问了一次菜单——这正是 booting 超时未达上限回 pending
	// 之后会发生的场景，也是「同一任务窗口内菜单最多取 3 次」的真实触发
	// 路径）。
	for i := 0; i < model.MaxBootAttempts; i++ {
		result, err := m.StartBoot(ctx, "task-attempt-exceeded", model.ArchX86_64, model.FirmwareUEFI)
		if err != nil {
			t.Fatalf("第 %d 次 StartBoot 失败: %v，want 成功（此时 attempt 应为 %d，未超限）", i+1, err, i+1)
		}
		if result.AttemptCount != i+1 {
			t.Fatalf("第 %d 次 StartBoot 后 AttemptCount = %d, want %d", i+1, result.AttemptCount, i+1)
		}
		if _, err := store.CASTaskState(ctx, "task-attempt-exceeded", model.TaskStateBooting, result.Version, model.TaskStatePending); err != nil {
			t.Fatalf("夹具把任务拨回 pending 失败: %v", err)
		}
	}

	// 第四次：attempt 即将变成 4，超过上限 3，应该置 failed 且不签 token。
	fourth, err := m.StartBoot(ctx, "task-attempt-exceeded", model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("第 4 次 StartBoot 返回了错误: %v，want nil error —— "+
			"attempt 超限同样是 StartBoot 正确识别并处理的业务结果", err)
	}
	if fourth.State != model.TaskStateFailed {
		t.Errorf("第 4 次 StartBoot 返回值 State = %q, want %q", fourth.State, model.TaskStateFailed)
	}

	got, getErr := store.GetTask(ctx, "task-attempt-exceeded")
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("attempt 超限后 State = %q, want %q", got.State, model.TaskStateFailed)
	}
	if got.FailureReason != model.FailureReasonAttemptExceeded {
		t.Errorf("attempt 超限后 FailureReason = %q, want %q", got.FailureReason, model.FailureReasonAttemptExceeded)
	}
	if got.Token != "" {
		t.Errorf("attempt 超限后 Token = %q, want 空——上限判定必须在签 token 之前", got.Token)
	}
}

// TestBeginBootSuccessPathIssuesTokenAndBooting 验证成功路径：状态变
// booting、token 非空、过期时间是现在加 90 分钟（用可控时钟验证精确值）。
func TestBeginBootSuccessPathIssuesTokenAndBooting(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	pendingTaskWithArtifact(t, store, "task-success", "artifact-1")

	result, err := m.StartBoot(ctx, "task-success", model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("StartBoot 成功路径返回了错误: %v", err)
	}
	if result.State != model.TaskStateBooting {
		t.Errorf("成功后 State = %q, want %q", result.State, model.TaskStateBooting)
	}
	if result.Token == "" {
		t.Errorf("成功后 Token 为空，want 非空")
	}
	wantExpiry := clock.Now() + int64(model.InstallTokenTTL.Seconds())
	if result.TokenExpiresAt != wantExpiry {
		t.Errorf("成功后 TokenExpiresAt = %d, want %d（现在 + 90 分钟）", result.TokenExpiresAt, wantExpiry)
	}
	if result.AttemptCount != 1 {
		t.Errorf("成功后 AttemptCount = %d, want 1", result.AttemptCount)
	}
}

// TestBeginBootOnAlreadyBootingDoesNotIncrementAttempt 验证对已经是
// booting 的任务再调一次不会累加 attempt——第 1 步的防御性检查：调用方
// （M5）会先查 pending 任务再调这个方法，但两步之间有窗口，并发的第二个
// 请求可能已经把任务推进到 booting。
func TestBeginBootOnAlreadyBootingDoesNotIncrementAttempt(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	pendingTaskWithArtifact(t, store, "task-already-booting", "artifact-1")

	first, err := m.StartBoot(ctx, "task-already-booting", model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("第一次 StartBoot 失败: %v", err)
	}
	if first.AttemptCount != 1 {
		t.Fatalf("第一次成功后 AttemptCount = %d, want 1", first.AttemptCount)
	}

	_, err = m.StartBoot(ctx, "task-already-booting", model.ArchX86_64, model.FirmwareUEFI)
	if err == nil {
		t.Fatalf("对已经是 booting 的任务再次 StartBoot 返回了 nil error，want 错误——" +
			"任务已不是 pending，不该重新处理")
	}

	got, getErr := store.GetTask(ctx, "task-already-booting")
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.AttemptCount != 1 {
		t.Errorf("对已 booting 任务重复调用后 AttemptCount = %d, want 保持 1 不变 —— "+
			"不检查的后果是对已终态/非pending任务重复累加 attempt，"+
			"那个计数会无界增长，而 §9.1 第三道闸门依赖它", got.AttemptCount)
	}
}

// TestBeginBootOnTerminalTaskReturnsErrorWithoutEvent 验证对终态任务调用
// 返回错误且不写任何事件——这条对应「无界增长」bug：扫描循环刚把任务判成
// failed，网关的旧请求还在处理中，两者之间有窗口。
func TestBeginBootOnTerminalTaskReturnsErrorWithoutEvent(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := pendingTaskWithArtifact(t, store, "task-terminal", "artifact-1")
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateFailed); err != nil {
		t.Fatalf("夹具把任务推进到 failed 失败: %v", err)
	}

	beforeEvents, err := store.ListEvents(ctx, created.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}

	_, err = m.StartBoot(ctx, created.ID, model.ArchX86_64, model.FirmwareUEFI)
	if err == nil {
		t.Fatalf("对已终态任务 StartBoot 返回了 nil error，want 错误")
	}

	afterEvents, err := store.ListEvents(ctx, created.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(afterEvents) != len(beforeEvents) {
		t.Errorf("对终态任务调用 StartBoot 后事件数从 %d 变为 %d，want 不变 —— "+
			"终态任务不该被重复处理，包括不该写入任何新事件",
			len(beforeEvents), len(afterEvents))
	}
}

// ---------------------------------------------------------------------------
// MarkAnswerFetched（任务 5）
// ---------------------------------------------------------------------------

// TestMarkAnswerFetchedBootingToInstalling 验证 booting → installing 的
// 正常转换，且阶段进入时间被重置——这样 pre 的 10 分钟预算从 installer
// 真正起来时开始算，而不是从签发 token 时。
func TestMarkAnswerFetchedBootingToInstalling(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := pendingTaskWithArtifact(t, store, "task-mark-answer", "artifact-1")
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateBooting); err != nil {
		t.Fatalf("夹具把任务推进到 booting 失败: %v", err)
	}

	clock.Advance(5 * time.Minute) // 推进 5 分钟，验证阶段进入时间确实被重置为「现在」而不是残留旧值

	result, err := m.MarkAnswerFetched(ctx, created.ID)
	if err != nil {
		t.Fatalf("MarkAnswerFetched 失败: %v", err)
	}
	if result.State != model.TaskStateInstalling {
		t.Errorf("MarkAnswerFetched 之后 State = %q, want %q", result.State, model.TaskStateInstalling)
	}
	if result.CurrentPhase != model.PhasePre {
		t.Errorf("MarkAnswerFetched 之后 CurrentPhase = %q, want %q", result.CurrentPhase, model.PhasePre)
	}
	if result.PhaseEnteredAt != clock.Now() {
		t.Errorf("MarkAnswerFetched 之后 PhaseEnteredAt = %d, want %d（现在）—— "+
			"pre 阶段的 10 分钟预算必须从 installer 真正起来时开始算，"+
			"不是从签发 token 时", result.PhaseEnteredAt, clock.Now())
	}
}

// TestMarkAnswerFetchedIdempotentOnRepeatedCall 验证重复调用返回成功且
// 不改变状态——对应 cloud-init 取两个文件（user-data 与 meta-data）的
// 场景，第二次调用不应报错。
func TestMarkAnswerFetchedIdempotentOnRepeatedCall(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := pendingTaskWithArtifact(t, store, "task-mark-answer-idempotent", "artifact-1")
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, model.TaskStateBooting); err != nil {
		t.Fatalf("夹具把任务推进到 booting 失败: %v", err)
	}

	first, err := m.MarkAnswerFetched(ctx, created.ID)
	if err != nil {
		t.Fatalf("第一次 MarkAnswerFetched 失败: %v", err)
	}

	second, err := m.MarkAnswerFetched(ctx, created.ID)
	if err != nil {
		t.Fatalf("第二次 MarkAnswerFetched 返回了错误: %v，want nil —— "+
			"Ubuntu 的 cloud-init 会取 user-data 与 meta-data 两个文件，"+
			"第二次报错会导致应答文件请求返回 500，cloud-init 拿不到"+
			"meta-data 就不会继续，装机停住", err)
	}
	if second.State != first.State || second.CurrentPhase != first.CurrentPhase {
		t.Errorf("第二次 MarkAnswerFetched 之后状态发生了变化: State=%q Phase=%q, "+
			"want 与第一次相同 State=%q Phase=%q —— 第二次调用不应再次触发状态变化之外的副作用",
			second.State, second.CurrentPhase, first.State, first.CurrentPhase)
	}
}

// TestMarkAnswerFetchedOnPendingRejected 验证对 pending 任务调用返回
// 错误——installer 不可能在签 token 之前起来。
func TestMarkAnswerFetchedOnPendingRejected(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := pendingTaskWithArtifact(t, store, "task-mark-answer-pending", "artifact-1")

	_, err := m.MarkAnswerFetched(ctx, created.ID)
	if err == nil {
		t.Fatalf("MarkAnswerFetched 对 pending 任务返回了 nil error，want 错误 —— " +
			"installer 不可能在签 token 之前起来")
	}
}

// TestStartBootAdmissionRejectsQueuesWithoutFailOrAttempt 验证并发准入
// 检查接入 StartBoot 之后最关键的一条行为（Phase 4 §10 失败模式 11）：
// 并发上限打满时，StartBoot 返回错误（网关据此回退到既有的 no_task 载荷，
// 机器继续 PXE 轮询），但任务本身原样留在 pending——不消耗 attempt、
// 不置 failed、不签发 token。这是与 precheck 失败/attempt 超限两种既有
// 拒绝路径的关键区别：那两种是「重试必然同样失败」，这一种是「稍后重试
// 就会成功」，处理后果必须相反。
func TestStartBootAdmissionRejectsQueuesWithoutFailOrAttempt(t *testing.T) {
	defer SetGlobalAdmissionLimit(defaultAdmissionLimit)

	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	// 先占满上限：造一台已经在 booting 的任务，把全局上限压到 1——本次
	// StartBoot 面对的任务应该被判定为「已达上限」而拒绝。
	occupying := pendingTaskWithArtifact(t, store, "task-admission-occupying", "artifact-1")
	occupyingResult, err := m.StartBoot(ctx, occupying.ID, model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("占位任务的 StartBoot 失败: %v（此时上限仍是默认值，应该成功）", err)
	}
	if occupyingResult.State != model.TaskStateBooting {
		t.Fatalf("占位任务 State = %q, want %q", occupyingResult.State, model.TaskStateBooting)
	}
	SetGlobalAdmissionLimit(1)

	pending := pendingTaskWithArtifact(t, store, "task-admission-queued", "artifact-2")

	_, err = m.StartBoot(ctx, pending.ID, model.ArchX86_64, model.FirmwareUEFI)
	if err == nil {
		t.Fatalf("并发已达上限（占位任务在 booting，上限=1）时 StartBoot 返回了 nil error，" +
			"want 非 nil——超限必须让调用方（网关）知道这次引导不该继续，" +
			"而不是签发 token 让第 21 台机器一起挤占带宽")
	}
	if !errors.Is(err, errAdmissionQueued) {
		t.Errorf("StartBoot 在准入拒绝时返回的错误 = %v，want 能被 errors.Is(err, errAdmissionQueued) 识别", err)
	}

	got, getErr := store.GetTask(ctx, pending.ID)
	if getErr != nil {
		t.Fatalf("GetTask 失败: %v", getErr)
	}
	if got.State != model.TaskStatePending {
		t.Errorf("准入拒绝之后任务 State = %q, want %q——超限的任务必须原样留在 pending，"+
			"不能被置 failed（那会让运维看到一堆「因为并发限流」而失败的任务，"+
			"而这批任务本该在几分钟后自然装上）", got.State, model.TaskStatePending)
	}
	if got.AttemptCount != 0 {
		t.Errorf("准入拒绝之后 AttemptCount = %d, want 0——超限不是失败，不能消耗 attempt"+
			"（消耗的后果是三轮排队之后任务被判 attempt_exceeded，而它本该只是等一会儿）",
			got.AttemptCount)
	}
	if got.Token != "" {
		t.Errorf("准入拒绝之后 Token = %q, want 空——不应该签发装机 token", got.Token)
	}
	if got.Version != pending.Version {
		t.Errorf("准入拒绝之后 Version = %d, want %d（未变）——准入检查是纯读判定，"+
			"不应该触发任何一次 CAS 或 mutation", got.Version, pending.Version)
	}
}

// TestStartBootAdmissionUnderLimitProceedsNormally 是上一条的对照组：
// 未达上限时，准入检查完全不影响既有成功路径（token 签发、attempt 递增、
// 状态转换 booting）——本任务对 StartBoot 只做新增，不改既有成功路径的
// 任何字段。
func TestStartBootAdmissionUnderLimitProceedsNormally(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	pendingTaskWithArtifact(t, store, "task-admission-under-limit", "artifact-1")

	result, err := m.StartBoot(ctx, "task-admission-under-limit", model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("未达并发上限时 StartBoot 返回了错误: %v，want nil error", err)
	}
	if result.State != model.TaskStateBooting {
		t.Errorf("未达并发上限时 State = %q, want %q", result.State, model.TaskStateBooting)
	}
	if result.Token == "" {
		t.Errorf("未达并发上限时 Token 为空，want 非空——准入检查不应阻断正常的成功路径")
	}
	if result.AttemptCount != 1 {
		t.Errorf("未达并发上限时 AttemptCount = %d, want 1", result.AttemptCount)
	}
}

// TestStartBootAdmissionLimitZeroMeansUnlimited 验证全局上限设为 0 时
// StartBoot 的准入检查等价于不生效（向后兼容未显式配置并发上限的部署，
// 与 AdmissionController.SetLimit(0) 的语义一致）。
func TestStartBootAdmissionLimitZeroMeansUnlimited(t *testing.T) {
	defer SetGlobalAdmissionLimit(defaultAdmissionLimit)

	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	SetGlobalAdmissionLimit(0)

	// 先占满一台，验证 0 表示不限时后续任务仍能正常拿到 token——若实现
	// 错误地把 0 当作「上限为 0 台」而不是「不限」，第二台会被误拒。
	occupying := pendingTaskWithArtifact(t, store, "task-unlimited-occupying", "artifact-1")
	if _, err := m.StartBoot(ctx, occupying.ID, model.ArchX86_64, model.FirmwareUEFI); err != nil {
		t.Fatalf("占位任务的 StartBoot 失败: %v", err)
	}

	pending := pendingTaskWithArtifact(t, store, "task-unlimited-second", "artifact-2")
	result, err := m.StartBoot(ctx, pending.ID, model.ArchX86_64, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("全局上限为 0（不限）时第二台 StartBoot 返回了错误: %v，want nil error", err)
	}
	if result.State != model.TaskStateBooting {
		t.Errorf("全局上限为 0 时第二台 State = %q, want %q", result.State, model.TaskStateBooting)
	}
}

var (
	_ contract.Prechecker = alwaysPassPrechecker{}
	_ contract.Prechecker = alwaysFailPrechecker{}
)
