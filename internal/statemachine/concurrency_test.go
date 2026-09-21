// 本文件测试三个并发场景（任务 10）。网关与扫描循环并发调用同一个状态
// 机，没有竞态检测的通过不算数。用屏障让 goroutine 尽量同时发起，断言
// 精确计数而不是「至少一个成功」。
package statemachine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// TestConcurrentStartBootExactlyOneWinsToken 验证多个 goroutine 对同一个
// pending 任务同时开始引导——恰好一个拿到 token。不是「最多三个」（那是
// 基于 attempt 上限的错误预期），是精确的一个：第一个成功后任务已是
// booting，其余的防御性检查会拦住。
func TestConcurrentStartBootExactlyOneWinsToken(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := pendingTaskWithArtifact(t, store, "task-concurrent-boot", "artifact-1")

	const goroutines = 10
	var barrier sync.WaitGroup
	barrier.Add(1)
	var ready sync.WaitGroup
	ready.Add(goroutines)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	results := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ready.Done()
			barrier.Wait() // 尽量同时发起，避免自然串行导致测不出竞态
			result, err := m.StartBoot(ctx, created.ID, model.ArchX86_64, model.FirmwareUEFI)
			if err == nil && result.State == model.TaskStateBooting && result.Token != "" {
				successCount.Add(1)
			}
			results[idx] = err
		}(i)
	}

	ready.Wait()
	barrier.Done()
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("并发 %d 次 StartBoot，成功拿到 booting+token 的次数 = %d, want 精确 1 —— "+
			"第一个成功后任务已是 booting，其余的防御性检查（第 1 步）必须拦住，"+
			"不是「最多三个」（那是基于 attempt 上限的错误预期）", goroutines, got)
	}

	final, err := store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if final.AttemptCount != 1 {
		t.Errorf("并发 StartBoot 之后 AttemptCount = %d, want 1 —— "+
			"只有恰好一个 goroutine 应该真正走到 attempt++ 那一步", final.AttemptCount)
	}
}

// TestConcurrentApplySignalDifferentSeqsAllProcessedMaxWatermark 验证
// 并发应用不同序号的信号——全部被处理，最终水位是最大那个。
func TestConcurrentApplySignalDifferentSeqsAllProcessedMaxWatermark(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	created := installingTaskAtPhase(t, store, "task-concurrent-signal", model.PhasePre, 0)

	signals := []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
	}

	var barrier sync.WaitGroup
	barrier.Add(1)
	var ready sync.WaitGroup
	ready.Add(len(signals))

	var wg sync.WaitGroup
	errs := make([]error, len(signals))

	for i, sig := range signals {
		wg.Add(1)
		go func(idx int, kind model.SignalKind) {
			defer wg.Done()
			entry, ok := model.LookupSignal(kind)
			if !ok {
				t.Errorf("model.LookupSignal(%q) 返回 ok=false，测试前提不成立", kind)
				return
			}
			ready.Done()
			barrier.Wait()
			_, err := m.ApplySignal(ctx, created.ID, kind, entry.Seq)
			errs[idx] = err
		}(i, sig)
	}

	ready.Wait()
	barrier.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("信号 %q 的 ApplySignal 返回了错误: %v", signals[i], err)
		}
	}

	final, err := store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	maxSeq := 0
	for _, sig := range signals {
		entry, _ := model.LookupSignal(sig)
		if entry.Seq > maxSeq {
			maxSeq = entry.Seq
		}
	}
	if final.LastSeq != maxSeq {
		t.Errorf("并发应用不同序号的信号之后 LastSeq = %d, want %d（最大序号）—— "+
			"全部信号都应该被处理（写事件），但最终水位必须是收到的最大序号",
			final.LastSeq, maxSeq)
	}

	events, err := store.ListEvents(ctx, created.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) != len(signals) {
		t.Errorf("并发应用 %d 个信号之后事件数 = %d, want %d —— 全部信号都应该被记录",
			len(signals), len(events), len(signals))
	}
}

// TestConcurrentScanAndSignalNoDoneThenFailedByTimeout 验证扫描循环与
// 信号并发——不出现「已 done 又被判超时失败」。
func TestConcurrentScanAndSignalNoDoneThenFailedByTimeout(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})

	budget, _ := model.PhaseBudget(model.PhaseInitializing)
	enteredAt := clock.Now()
	created := createTaskAtPhaseEnteredAt(t, store, "task-concurrent-scan-signal", model.TaskStateInitializing, model.PhaseInitializing, enteredAt, 1)
	if _, err := store.MutateTaskFields(ctx, created.ID, model.TaskMutation{LastSeq: intPtr(100)}); err != nil {
		t.Fatalf("夹具设置 LastSeq 失败: %v", err)
	}

	clock.Advance(budget + time.Second) // 让 initializing 阶段已经超时，扫描会把它判失败——除非 ready 先到

	var barrier sync.WaitGroup
	barrier.Add(1)
	var ready sync.WaitGroup
	ready.Add(2)

	var wg sync.WaitGroup
	var signalErr, scanErr error

	var signalOutcome contract.SignalOutcome

	wg.Add(2)
	go func() {
		defer wg.Done()
		ready.Done()
		barrier.Wait()
		signalOutcome, signalErr = m.ApplySignal(ctx, created.ID, model.SignalReady, 110)
	}()
	go func() {
		defer wg.Done()
		ready.Done()
		barrier.Wait()
		_, scanErr = m.ScanTimeouts(ctx, clock.Now())
	}()

	ready.Wait()
	barrier.Done()
	wg.Wait()

	if signalErr != nil {
		t.Errorf("ApplySignal(ready) 返回了错误: %v", signalErr)
	}
	if scanErr != nil {
		t.Errorf("ScanTimeouts 返回了错误: %v", scanErr)
	}

	final, err := store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if final.State != model.TaskStateDone && final.State != model.TaskStateFailed {
		t.Fatalf("并发 ready 信号与扫描之后 State = %q, want done 或 failed 之一", final.State)
	}

	// 两种最终结果都是可接受的（取决于谁先抢到 CAS）：ready 先到则最终
	// 是 done；扫描先到则最终是 failed。唯一真正不可接受的是「ready 的
	// ApplySignal 已经明确报告 StateChanged=true（也就是它确实把任务
	// 推进到了 done），但最终读到的状态却是 failed」——这才是「已 done
	// 又被判超时失败」，说明扫描侧的 CAS 覆盖了一个已经提交的终态。
	// 只看最终状态是 failed 不能区分「扫描赢得了合法的竞速」与
	// 「done 被覆盖」，必须结合 ApplySignal 自己报告的 StateChanged
	// 才能精确断言。
	if signalOutcome.StateChanged && final.State != model.TaskStateDone {
		t.Errorf("ApplySignal(ready) 报告 StateChanged=true（已把任务推进到 done），"+
			"但最终读到的状态是 %q，want %q —— 说明扫描侧的 CAS 覆盖了一个已经提交的"+
			"终态，这正是「已 done 又被判超时失败」这个 bug", final.State, model.TaskStateDone)
	}
}

func intPtr(v int) *int { return &v }
