// 本文件测试应用信号 ApplySignal（任务 6）。锁住幂等的三种情形（序号重复、
// 阶段后退、终态任务）与 hook-report 的正交性。
package statemachine

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// installingTaskAtPhase 建一个处于 installing 状态、给定阶段、给定水位
// 的任务，供信号测试复用。
func installingTaskAtPhase(t *testing.T, store contract.Store, taskID string, phase model.Phase, lastSeq int) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), model.Task{
		ID:           taskID,
		State:        model.TaskStateInstalling,
		CurrentPhase: phase,
		LastSeq:      lastSeq,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	return created
}

// TestApplySignalNormalSequenceAdvancesPhase 验证正常序列推进状态与阶段。
func TestApplySignalNormalSequenceAdvancesPhase(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-signal-normal", model.PhasePre, 10)

	outcome, err := m.ApplySignal(ctx, "task-signal-normal", model.SignalPreDone, 20)
	if err != nil {
		t.Fatalf("ApplySignal(pre-done) 失败: %v", err)
	}
	if !outcome.Accepted {
		t.Errorf("outcome.Accepted = false, want true")
	}
	if !outcome.StateChanged {
		t.Errorf("outcome.StateChanged = false, want true —— 正常推进应该改变阶段")
	}
	if outcome.Task.CurrentPhase != model.PhasePre {
		t.Errorf("pre-done 之后 CurrentPhase = %q, want %q（pre-done 仍在 pre 阶段内）",
			outcome.Task.CurrentPhase, model.PhasePre)
	}
	if outcome.Task.LastSeq != 20 {
		t.Errorf("pre-done 之后 LastSeq = %d, want 20", outcome.Task.LastSeq)
	}
}

// TestApplySignalDuplicateSeqNotAcceptedNotError 验证重复序号返回「未
// 接受」而不是错误——机器侧重试三次，重复投递是常态。
func TestApplySignalDuplicateSeqNotAcceptedNotError(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-signal-dup", model.PhasePre, 0)

	first, err := m.ApplySignal(ctx, "task-signal-dup", model.SignalPreStart, 10)
	if err != nil {
		t.Fatalf("第一次 ApplySignal(pre-start) 失败: %v", err)
	}
	if !first.Accepted || !first.StateChanged {
		t.Fatalf("第一次 ApplySignal 结果异常: %+v", first)
	}

	second, err := m.ApplySignal(ctx, "task-signal-dup", model.SignalPreStart, 10)
	if err != nil {
		t.Fatalf("重复序号的 ApplySignal 返回了错误: %v，want nil error —— "+
			"机器侧会重试最多三次，重复投递是正常情况，不是错误路径", err)
	}
	if !second.Accepted {
		t.Errorf("重复序号 outcome.Accepted = false, want true —— 已记录过，应继续走成功路径")
	}
	if second.StateChanged {
		t.Errorf("重复序号 outcome.StateChanged = true, want false —— 重复投递不该再改一次状态")
	}
}

// TestApplySignalLateArrivingEarlySignalDoesNotChangePhase 验证延迟到达
// 的早期信号不改变阶段——用「先 post-done 再 packages-start」验证。
func TestApplySignalLateArrivingEarlySignalDoesNotChangePhase(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-signal-late", model.PhasePost, 0)

	postDone, err := m.ApplySignal(ctx, "task-signal-late", model.SignalPostDone, 60)
	if err != nil {
		t.Fatalf("ApplySignal(post-done) 失败: %v", err)
	}
	if postDone.Task.CurrentPhase != model.PhasePost {
		t.Fatalf("post-done 之后 CurrentPhase = %q, want %q", postDone.Task.CurrentPhase, model.PhasePost)
	}

	late, err := m.ApplySignal(ctx, "task-signal-late", model.SignalPackagesStart, 30)
	if err != nil {
		t.Fatalf("延迟到达的 packages-start 返回了错误: %v，want nil error", err)
	}
	if !late.Accepted {
		t.Errorf("延迟信号 outcome.Accepted = false, want true —— 仍要记事件")
	}
	if late.StateChanged {
		t.Errorf("延迟信号 outcome.StateChanged = true, want false —— " +
			"序号 30 不大于当前水位 60，阶段只能前进不能后退")
	}
	if late.Task.CurrentPhase != model.PhasePost {
		t.Errorf("延迟的 packages-start 之后 CurrentPhase = %q, want 保持 %q 不变 —— "+
			"否则已经 post-done 的任务会被打回 packages 阶段",
			late.Task.CurrentPhase, model.PhasePost)
	}
}

// TestApplySignalOnTerminalTaskOnlyLogsEvent 验证终态任务收到信号只记
// 事件——终态任务不该被信号复活。
func TestApplySignalOnTerminalTaskOnlyLogsEvent(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := installingTaskAtPhase(t, store, "task-signal-terminal", model.PhasePost, 60)
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStateInstalling, created.Version, model.TaskStateFailed); err != nil {
		t.Fatalf("夹具把任务推进到 failed 失败: %v", err)
	}

	outcome, err := m.ApplySignal(ctx, created.ID, model.SignalRebooting, 70)
	if err != nil {
		t.Fatalf("对终态任务 ApplySignal 返回了错误: %v，want nil error", err)
	}
	if !outcome.Accepted {
		t.Errorf("终态任务 outcome.Accepted = false, want true —— 仍要记事件")
	}
	if outcome.StateChanged {
		t.Errorf("终态任务 outcome.StateChanged = true, want false —— 终态任务不该被信号复活")
	}
	if outcome.Task.State != model.TaskStateFailed {
		t.Errorf("终态任务收到信号后 State = %q, want 保持 %q 不变", outcome.Task.State, model.TaskStateFailed)
	}
}

// TestApplySignalHookReportAcceptedButWatermarkUnchanged 验证 hook-report
// 来三次都被接受，但水位不变——它与阶段正交，一个阶段内有几个 hook 就
// 来几次。
func TestApplySignalHookReportAcceptedButWatermarkUnchanged(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-signal-hook", model.PhasePre, 10)

	for i := 0; i < 3; i++ {
		outcome, err := m.ApplySignal(ctx, "task-signal-hook", model.SignalHookReport, 0)
		if err != nil {
			t.Fatalf("第 %d 次 ApplySignal(hook-report) 失败: %v", i+1, err)
		}
		if !outcome.Accepted {
			t.Errorf("第 %d 次 hook-report outcome.Accepted = false, want true", i+1)
		}
		if outcome.Task.LastSeq != 10 {
			t.Errorf("第 %d 次 hook-report 之后 LastSeq = %d, want 保持 10 不变 —— "+
				"hook-report 永不推进水位", i+1, outcome.Task.LastSeq)
		}
	}
}

// TestApplySignalUnknownNameRejected 验证未知信号名被拒绝。
func TestApplySignalUnknownNameRejected(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-signal-unknown", model.PhasePre, 0)

	_, err := m.ApplySignal(ctx, "task-signal-unknown", model.SignalKind("bogus-signal"), 1)
	if err == nil {
		t.Fatalf("ApplySignal(未知信号名) 返回了 nil error，want 错误 —— " +
			"信号名来自 URL 路径段，不查表校验等于让机器指定自己的阶段")
	}
}

// TestApplySignalReadyReachesDoneAndPublishesLifecycleEvent 验证 ready
// 之后任务是 done，且生命周期事件被发布。
func TestApplySignalReadyReachesDoneAndPublishesLifecycleEvent(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()

	var publishedEvents []contract.LifecycleEvent
	bus := &recordingEventBus{onPublish: func(event contract.LifecycleEvent, task model.Task) {
		publishedEvents = append(publishedEvents, event)
	}}
	m := New(store, clock, &Machine{}, alwaysPassPrechecker{}, nil, bus)

	installingTaskAtPhase(t, store, "task-signal-ready", model.PhaseInitializing, 100)

	outcome, err := m.ApplySignal(ctx, "task-signal-ready", model.SignalReady, 110)
	if err != nil {
		t.Fatalf("ApplySignal(ready) 失败: %v", err)
	}
	if outcome.Task.State != model.TaskStateDone {
		t.Errorf("ready 之后 State = %q, want %q", outcome.Task.State, model.TaskStateDone)
	}

	found := false
	for _, e := range publishedEvents {
		if e == contract.LifecycleEventTaskReady {
			found = true
		}
	}
	if !found {
		t.Errorf("ready 信号处理完毕后未发布 %q 生命周期事件，实际发布了 %v —— "+
			"§7.5 要求 ready 之后才写 DNS、注册 CMDB，靠这个事件驱动",
			contract.LifecycleEventTaskReady, publishedEvents)
	}
}

// TestApplySignalUpdatesLastSignalAtEvenWhenNotAccepted 验证上次信号时间
// 在「未接受」的情形下也被更新——§3.5 的「距今多久」是失败诊断的关键
// 区分度，刚收到信号说明机器活着。
func TestApplySignalUpdatesLastSignalAtEvenWhenNotAccepted(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := installingTaskAtPhase(t, store, "task-signal-lastsignal", model.PhasePost, 60)
	if _, err := store.CASTaskState(ctx, created.ID, model.TaskStateInstalling, created.Version, model.TaskStateFailed); err != nil {
		t.Fatalf("夹具把任务推进到 failed 失败: %v", err)
	}

	clock.Advance(1)
	outcome, err := m.ApplySignal(ctx, created.ID, model.SignalRebooting, 70)
	if err != nil {
		t.Fatalf("ApplySignal 失败: %v", err)
	}
	if outcome.StateChanged {
		t.Fatalf("终态任务的信号意外改变了状态，测试前提不成立")
	}
	if outcome.Task.LastSignalAt != clock.Now() {
		t.Errorf("终态任务（未接受改状态）收到信号后 LastSignalAt = %d, want %d（现在）—— "+
			"即使信号未被接受改状态，上次信号时间也必须更新，"+
			"否则失败诊断里的「距今多久」会失真", outcome.Task.LastSignalAt, clock.Now())
	}
}

// ---------------------------------------------------------------------------
// finalize token（任务 7）
// ---------------------------------------------------------------------------

// TestFinalizeTokenIssuedWithTwentyFourHourExpiry 验证签发后任务上有
// token 且过期时间是现在加 24 小时。
func TestFinalizeTokenIssuedWithTwentyFourHourExpiry(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-finalize-issue", model.PhasePost, 60)

	result, err := m.IssueFinalizeToken(ctx, "task-finalize-issue")
	if err != nil {
		t.Fatalf("IssueFinalizeToken 失败: %v", err)
	}
	if result.FinalizeToken == "" {
		t.Errorf("签发后 FinalizeToken 为空，want 非空")
	}
	wantExpiry := clock.Now() + int64(model.FinalizeTokenTTL.Seconds())
	if result.FinalizeExpiresAt != wantExpiry {
		t.Errorf("签发后 FinalizeExpiresAt = %d, want %d（现在 + 24 小时）", result.FinalizeExpiresAt, wantExpiry)
	}
}

// TestFinalizeTokenRepeatedCallReturnsSameToken 验证重复调用返回同一个
// token——不幂等的后果是重复渲染（比如 Ubuntu 取两个文件）会换掉 token，
// 而先前写进目标系统的那个就失效了。
func TestFinalizeTokenRepeatedCallReturnsSameToken(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-finalize-repeat", model.PhasePost, 60)

	first, err := m.IssueFinalizeToken(ctx, "task-finalize-repeat")
	if err != nil {
		t.Fatalf("第一次 IssueFinalizeToken 失败: %v", err)
	}

	second, err := m.IssueFinalizeToken(ctx, "task-finalize-repeat")
	if err != nil {
		t.Fatalf("第二次 IssueFinalizeToken 失败: %v", err)
	}
	if second.FinalizeToken != first.FinalizeToken {
		t.Errorf("第二次 IssueFinalizeToken 返回了不同的 token（%q vs %q），want 相同 —— "+
			"重复渲染会换掉 token，而先前写进目标系统的那个就失效了",
			second.FinalizeToken, first.FinalizeToken)
	}
	if second.FinalizeExpiresAt != first.FinalizeExpiresAt {
		t.Errorf("第二次 IssueFinalizeToken 的过期时间变了（%d vs %d），want 相同",
			second.FinalizeExpiresAt, first.FinalizeExpiresAt)
	}
}

// TestFinalizeTokenExpiredIssuesNewOne 验证已过期时重复调用换发新的。
func TestFinalizeTokenExpiredIssuesNewOne(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	installingTaskAtPhase(t, store, "task-finalize-expired", model.PhasePost, 60)

	first, err := m.IssueFinalizeToken(ctx, "task-finalize-expired")
	if err != nil {
		t.Fatalf("第一次 IssueFinalizeToken 失败: %v", err)
	}

	clock.Advance(model.FinalizeTokenTTL + 1)

	second, err := m.IssueFinalizeToken(ctx, "task-finalize-expired")
	if err != nil {
		t.Fatalf("第二次 IssueFinalizeToken 失败: %v", err)
	}
	if second.FinalizeToken == first.FinalizeToken {
		t.Errorf("finalize token 已过期后重复调用仍返回了同一个 token，want 换发新的 —— " +
			"已过期的 token 不该被当作「仍在有效期内」继续复用")
	}
	wantExpiry := clock.Now() + int64(model.FinalizeTokenTTL.Seconds())
	if second.FinalizeExpiresAt != wantExpiry {
		t.Errorf("换发新 token 后 FinalizeExpiresAt = %d, want %d", second.FinalizeExpiresAt, wantExpiry)
	}
}

// recordingEventBus 是 contract.EventBus 的测试替身，把每次 Publish 转发
// 给注入的回调，供测试断言发布了哪些事件。
type recordingEventBus struct {
	onPublish func(event contract.LifecycleEvent, task model.Task)
}

func (b *recordingEventBus) Publish(event contract.LifecycleEvent, task model.Task) {
	if b.onPublish != nil {
		b.onPublish(event, task)
	}
}

func (b *recordingEventBus) Subscribe(event contract.LifecycleEvent, callback func(model.Task)) {}
