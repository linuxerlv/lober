package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// countingScanner 是 timeoutScanner 的测试替身：记录被调用次数，可配置
// 每次调用是否返回错误。用 atomic 计数——测试要在 -race 下验证并发安全
// （runScanLoop 的 goroutine 与测试主 goroutine 并发读计数）。
type countingScanner struct {
	calls   atomic.Int64
	failAll bool
}

func (s *countingScanner) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	s.calls.Add(1)
	if s.failAll {
		return 0, errors.New("模拟的数据库瞬时不可用")
	}
	return 0, nil
}

// TestRunScanLoop_ExecutesImmediatelyOnStart 断言启动时立即执行一轮——
// 用一个很长的间隔验证计数仍变为 1（计划任务 3「测试要锁住的行为」）。
func TestRunScanLoop_ExecutesImmediatelyOnStart(t *testing.T) {
	scanner := &countingScanner{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := runScanLoop(ctx, scanner, time.Hour) // 间隔很长，若不立即执行则一直是 0
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for scanner.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即扫描——runScanLoop 应在起循环前先扫一轮")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := scanner.calls.Load(); got != 1 {
		t.Errorf("启动后短时间内的调用次数 = %d, want 恰好 1（长间隔下第二轮不该这么快触发）", got)
	}
}

// TestRunScanLoop_RepeatsOnInterval 断言循环按间隔重复执行。
func TestRunScanLoop_RepeatsOnInterval(t *testing.T) {
	scanner := &countingScanner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := runScanLoop(ctx, scanner, 20*time.Millisecond)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(3 * time.Second)
	for scanner.calls.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("3 秒内调用次数未达到 3，实际 = %d——循环未按间隔重复执行", scanner.calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestRunScanLoop_ContinuesAfterScanError 断言扫描出错后循环继续——用一个
// 总是返回错误的替身验证仍在累加调用次数（计划任务 3「测试要锁住的
// 行为」：数据库瞬时不可用之后，超时判定必须自己恢复）。
func TestRunScanLoop_ContinuesAfterScanError(t *testing.T) {
	scanner := &countingScanner{failAll: true}
	ctx, cancel := context.WithCancel(context.Background())

	done := runScanLoop(ctx, scanner, 15*time.Millisecond)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(3 * time.Second)
	for scanner.calls.Load() < 5 {
		select {
		case <-deadline:
			t.Fatalf("3 秒内调用次数未达到 5，实际 = %d——扫描出错后循环应继续累加调用次数，"+
				"而不是停摆（循环退出的后果是静默停摆到下次重启，没人会注意到）", scanner.calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestRunScanLoop_ContextCancelStopsLoopCleanly 断言上下文取消后函数确实
// 返回（不返回就是 goroutine 泄漏，计划任务 3「测试要锁住的行为」）。
func TestRunScanLoop_ContextCancelStopsLoopCleanly(t *testing.T) {
	scanner := &countingScanner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := runScanLoop(ctx, scanner, 10*time.Millisecond)

	// 等启动时的立即扫描完成，确保循环真的已经在跑。
	deadline := time.After(2 * time.Second)
	for scanner.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("循环未能在 2 秒内完成首轮扫描")
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

// TestRunScanLoop_NonPositiveIntervalFallsBackToDefault 断言非正数间隔时
// 循环仍能正常工作（兜底为 30 秒），不会忙等或直接崩溃。这不是计划任务 3
// 逐条列出的行为，但 runScanLoop 的实现里显式声明了这条兜底，遗漏测试
// 会让这行代码成为死代码或未经验证的猜测。
func TestRunScanLoop_NonPositiveIntervalFallsBackToDefault(t *testing.T) {
	scanner := &countingScanner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := runScanLoop(ctx, scanner, 0)
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.After(2 * time.Second)
	for scanner.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("2 秒内未观察到启动时的立即扫描")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// 30 秒的兜底间隔意味着短时间内不会有第二轮——只要没有 panic、且启动时
	// 的一轮確实发生，就足以证明非正数间隔被安全地处理了。
	if got := scanner.calls.Load(); got != 1 {
		t.Errorf("调用次数 = %d, want 1（兜底 30 秒间隔下短时间内不应有第二轮）", got)
	}
}
