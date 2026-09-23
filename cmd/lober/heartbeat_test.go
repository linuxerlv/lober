package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// fakeHeartbeatTaskLister 是 heartbeatTaskLister 的测试替身：按预置的
// state -> []model.Task 映射回放 ListTasks，同时用 atomic 计数记录每个
// state 被查询的次数，供并发场景（-race）下安全读取。
type fakeHeartbeatTaskLister struct {
	byState map[model.TaskState][]model.Task
	calls   atomic.Int64
}

func (f *fakeHeartbeatTaskLister) ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error) {
	f.calls.Add(1)
	return f.byState[filter.State], nil
}

// fakeHeartbeatProber 是 contract.HeartbeatProviderV1 的测试替身：记录每次
// Probe 收到的目标，返回预置结果或按目标自动生成「全部存活」的结果。
type fakeHeartbeatProber struct {
	mu          chan struct{} // 用带缓冲的 channel 做一个简单的并发安全记录点
	lastTargets []contract.ProbeTarget
	probeCalls  atomic.Int64
	results     []contract.ProbeResult // 非空时直接返回这个固定结果，忽略 targets
}

func newFakeHeartbeatProber() *fakeHeartbeatProber {
	return &fakeHeartbeatProber{mu: make(chan struct{}, 1)}
}

func (f *fakeHeartbeatProber) Probe(ctx context.Context, targets []contract.ProbeTarget) []contract.ProbeResult {
	f.probeCalls.Add(1)
	f.mu <- struct{}{}
	f.lastTargets = targets
	<-f.mu
	if f.results != nil {
		return f.results
	}
	out := make([]contract.ProbeResult, len(targets))
	for i, t := range targets {
		out[i] = contract.ProbeResult{TaskID: t.TaskID, Reached: true, Detail: "fake: 存活"}
	}
	return out
}

func (f *fakeHeartbeatProber) Ping(ctx context.Context) error { return nil }

var _ contract.HeartbeatProviderV1 = (*fakeHeartbeatProber)(nil)

// taskWithBizIP 构造一个带已知探测地址的任务。键用裸键 "biz_ip"——与
// resolve.go 的 Resolve 现在写回 ResolvedParams 时使用的键名一致（也是
// heartbeat.go 现在读取的键名），不再是 fact 命名空间常量
// model.FactBizIP（LOBER_BIZ_IP 属于注入给机器侧 hook 的 Facts 命名
// 空间，与 ResolvedParams 是两个不同的键空间）。
func taskWithBizIP(id string, state model.TaskState, ip string) model.Task {
	return model.Task{
		ID:             id,
		State:          state,
		ResolvedParams: map[string]string{"biz_ip": ip},
	}
}

// TestRunHeartbeatLoop_ExecutesImmediatelyOnStart 断言启动时立即探测一轮，
// 与超时扫描循环（scanner.go）同样的纪律：进程刚起来时不该空等一个间隔
// 才开始探测。
func TestRunHeartbeatLoop_ExecutesImmediatelyOnStart(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateInstalling: {taskWithBizIP("t1", model.TaskStateInstalling, "127.0.0.1")},
	}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runHeartbeatLoop(ctx, lister, prober, time.Hour) // 间隔很长，不立即执行则一直是 0
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for prober.probeCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即探测——runHeartbeatLoop 应在起循环前先探测一轮")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := prober.probeCalls.Load(); got != 1 {
		t.Errorf("启动后短时间内 Probe 调用次数 = %d, want 恰好 1（长间隔下第二轮不该这么快触发）", got)
	}
}

// TestRunHeartbeatLoop_RepeatsOnInterval 断言循环按间隔重复探测。
func TestRunHeartbeatLoop_RepeatsOnInterval(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateBooting: {taskWithBizIP("t1", model.TaskStateBooting, "127.0.0.1")},
	}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())

	done := runHeartbeatLoop(ctx, lister, prober, 20*time.Millisecond)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(3 * time.Second)
	for prober.probeCalls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("3 秒内 Probe 调用次数未达到 3，实际 = %d——循环未按间隔重复执行", prober.probeCalls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestRunHeartbeatLoop_ContextCancelStopsLoopCleanly 断言上下文取消后函数
// 确实返回（不返回就是 goroutine 泄漏），与超时扫描循环同样的纪律。
func TestRunHeartbeatLoop_ContextCancelStopsLoopCleanly(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateBooting: {taskWithBizIP("t1", model.TaskStateBooting, "127.0.0.1")},
	}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())

	done := runHeartbeatLoop(ctx, lister, prober, 10*time.Millisecond)

	deadline := time.After(2 * time.Second)
	for prober.probeCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("循环未能在 2 秒内完成首轮探测")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
		// 循环确实退出，goroutine 未泄漏。
	case <-time.After(2 * time.Second):
		t.Fatal("上下文取消后 2 秒内 done channel 未关闭——循环可能未正确退出（goroutine 泄漏）")
	}
}

// TestRunHeartbeatLoop_NonPositiveIntervalFallsBackToDefault 断言非正数间隔
// 时循环仍能正常工作（兜底默认间隔），不会忙等或直接崩溃。
func TestRunHeartbeatLoop_NonPositiveIntervalFallsBackToDefault(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateBooting: {taskWithBizIP("t1", model.TaskStateBooting, "127.0.0.1")},
	}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())

	done := runHeartbeatLoop(ctx, lister, prober, 0)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for prober.probeCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即探测")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// 兜底间隔（60 秒）意味着短时间内不会有第二轮。
	if got := prober.probeCalls.Load(); got != 1 {
		t.Errorf("Probe 调用次数 = %d, want 1（兜底间隔下短时间内不应有第二轮）", got)
	}
}

// TestRunHeartbeatLoop_OnlyQueriesProbeableStates 断言每一轮只查询
// {booting, installing, rebooting, initializing} 四个状态——不查询 pending
// （还没到需要探活的阶段）、done/failed（终态，探测无意义）。
func TestRunHeartbeatLoop_OnlyQueriesProbeableStates(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runHeartbeatLoop(ctx, lister, prober, time.Hour)
	defer func() {
		cancel()
		<-done
	}()

	// 就绪信号用 lister.calls 而不是 prober.probeCalls：本测试的 byState
	// 全空，没有任何可探测任务，按「无可探测任务时不调用 Probe」这条纪律
	// （见 TestRunHeartbeatLoop_NoProbeableTasksSkipsProbeCall），
	// probeCalls 在本场景下永远是 0，用它做就绪门会导致本测试必然超时——
	// 这是等待条件选错了信号，不是被测行为的问题。
	wantStates := map[model.TaskState]bool{
		model.TaskStateBooting:      true,
		model.TaskStateInstalling:   true,
		model.TaskStateRebooting:    true,
		model.TaskStateInitializing: true,
	}
	deadline := time.After(2 * time.Second)
	for lister.calls.Load() < int64(len(wantStates)) {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即探测（ListTasks 调用次数不足）")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if got := lister.calls.Load(); got != int64(len(wantStates)) {
		t.Fatalf("ListTasks 调用次数 = %d, want %d（应恰好查询四个可探测状态各一次）",
			got, len(wantStates))
	}
}

// TestRunHeartbeatLoop_ProbesOnlyTasksWithKnownAddress 断言没有可探测地址
// （ResolvedParams 缺 LOBER_BIZ_IP）的任务不会被塞进探测目标列表——喂一个
// 空字符串 IP 给探测器没有意义，且会污染探测结果的可观测性。
func TestRunHeartbeatLoop_ProbesOnlyTasksWithKnownAddress(t *testing.T) {
	withIP := taskWithBizIP("has-ip", model.TaskStateInstalling, "127.0.0.1")
	withoutIP := model.Task{ID: "no-ip", State: model.TaskStateInstalling, ResolvedParams: map[string]string{}}

	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateInstalling: {withIP, withoutIP},
	}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runHeartbeatLoop(ctx, lister, prober, time.Hour)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for prober.probeCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即探测")
		case <-time.After(10 * time.Millisecond):
		}
	}

	prober.mu <- struct{}{}
	targets := prober.lastTargets
	<-prober.mu

	if len(targets) != 1 {
		t.Fatalf("探测目标数 = %d, want 1（缺地址的任务不该进入探测目标列表），got %+v", len(targets), targets)
	}
	if targets[0].TaskID != "has-ip" {
		t.Errorf("探测目标 TaskID = %q, want %q", targets[0].TaskID, "has-ip")
	}
}

// TestRunHeartbeatLoop_ProbeFailureDoesNotPanicOrStopLoop 断言探测失败
// （Reached=false）不会让循环崩溃或停摆——探测失败只记录，不改任务状态
// （§7.4），也不该让下一轮探测停止执行。
func TestRunHeartbeatLoop_ProbeFailureDoesNotPanicOrStopLoop(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{
		model.TaskStateInstalling: {taskWithBizIP("t1", model.TaskStateInstalling, "127.0.0.1")},
	}}
	prober := newFakeHeartbeatProber()
	prober.results = []contract.ProbeResult{
		{TaskID: "t1", Reached: false, Detail: "模拟探测失败"},
	}
	ctx, cancel := context.WithCancel(context.Background())

	done := runHeartbeatLoop(ctx, lister, prober, 15*time.Millisecond)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(3 * time.Second)
	for prober.probeCalls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("3 秒内 Probe 调用次数未达到 3，实际 = %d——探测失败后循环应继续累加调用次数，"+
				"而不是停摆", prober.probeCalls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestRunHeartbeatLoop_NoProbeableTasksSkipsProbeCall 断言当前没有任何
// 可探测任务时不调用 Probe（空目标列表没有必要发起一次空调用），但仍应
// 完成 ListTasks 查询本身（用于确认「确实查过、只是没查到」而不是「循环
// 卡死没跑」）。
func TestRunHeartbeatLoop_NoProbeableTasksSkipsProbeCall(t *testing.T) {
	lister := &fakeHeartbeatTaskLister{byState: map[model.TaskState][]model.Task{}}
	prober := newFakeHeartbeatProber()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runHeartbeatLoop(ctx, lister, prober, time.Hour)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for lister.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即查询")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// 给一点缓冲时间确认没有 Probe 调用发生（本测试的核心断言）。
	time.Sleep(50 * time.Millisecond)
	if got := prober.probeCalls.Load(); got != 0 {
		t.Errorf("Probe 调用次数 = %d, want 0（无可探测任务时不该调用 Probe）", got)
	}
}
