// 本文件测试在途调用的排空（P3-04 第一半任务 3），依据 SPEC §7.3 第二项。
//
// 这是 gRPC 加载器**唯一** exec/http 完全不需要的机制——后两者每次调用是独立
// 进程或独立 HTTP 请求，没有「在途」的概念。
package grpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestDrainGate_RejectsNewCallsWhileDraining 验证排空期间新调用被拒绝。
func TestDrainGate_RejectsNewCallsWhileDraining(t *testing.T) {
	g := newDrainGate()

	// 先占一个在途调用，让排空不会立刻完成。
	release, err := g.enter()
	if err != nil {
		t.Fatalf("enter 失败: %v", err)
	}

	drainDone := make(chan error, 1)
	go func() { drainDone <- g.drain(time.Second) }()

	// 给排空一点时间进入「已关门」状态。
	waitFor(t, func() bool { return g.isDraining() })

	if _, err := g.enter(); !errors.Is(err, errDraining) {
		t.Errorf("排空期间 enter() = %v, want errDraining —— 不关门的话新调用会"+
			"源源不断进来，排空永远等不到零，reload 永远超时", err)
	}

	release()
	if err := <-drainDone; err != nil {
		t.Errorf("在途调用结束后 drain 应成功，got %v", err)
	}
}

// TestDrainGate_WaitsForInFlightBeforeCompleting 验证在途调用完成后排空才返回
// ——即「在途调用完成后旧进程才停」。
func TestDrainGate_WaitsForInFlightBeforeCompleting(t *testing.T) {
	g := newDrainGate()

	release, err := g.enter()
	if err != nil {
		t.Fatalf("enter 失败: %v", err)
	}

	drained := make(chan error, 1)
	go func() { drained <- g.drain(2 * time.Second) }()

	select {
	case <-drained:
		t.Fatal("在途调用还没结束，drain 就返回了 —— 那意味着旧进程会在一个" +
			"正在进行的调用中途被停掉，该调用的结果不可预测")
	case <-time.After(80 * time.Millisecond):
		// 预期：还在等。
	}

	release()
	select {
	case err := <-drained:
		if err != nil {
			t.Errorf("drain 返回 %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("在途调用已结束但 drain 没返回 —— 计数没有减到零，说明 release 漏减了")
	}
}

// TestDrainGate_TimeoutStillProceeds 验证超时后排空仍然返回（让调用方去停旧
// 进程），且返回一个可识别的超时错误供记日志。
//
// **超时后仍然停旧进程**（计划任务 3 明确要求）：一个卡住的调用不该无限期阻塞
// reload——而 reload 往往正是在修一个影响装机的问题。但要能记一条日志说明有调用
// 被强制中断，那是排查「为什么某个任务的 provider 调用失败」的线索。
func TestDrainGate_TimeoutStillProceeds(t *testing.T) {
	g := newDrainGate()

	// 一个永不结束的在途调用。
	if _, err := g.enter(); err != nil {
		t.Fatalf("enter 失败: %v", err)
	}

	start := time.Now()
	err := g.drain(120 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("在途调用卡住时 drain 返回 nil —— 调用方无从知道有调用被强制中断，" +
			"而那条信息是排查「某个任务的 provider 调用为什么失败」的线索")
	}
	var timeoutErr *DrainTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("drain 返回 %T: %v, want *DrainTimeoutError —— 超时与其它失败要能"+
			"区分开：超时意味着「已经停了旧进程但有调用被中断」，调用方据此记日志", err, err)
	}
	if timeoutErr.InFlight != 1 {
		t.Errorf("DrainTimeoutError.InFlight = %d, want 1 —— 被中断的调用数要带出来，"+
			"它决定这条日志是「偶发一个」还是「插件整体卡住」", timeoutErr.InFlight)
	}
	if elapsed > time.Second {
		t.Errorf("drain 耗时 %v，远超给定的 120ms 超时 —— 超时必须真的生效", elapsed)
	}
}

// TestDrainGate_ReleaseOnErrorPath 验证调用出错时计数也正确减少。
//
// **漏减的后果是排空永远等不到零，而 reload 永远超时**（计划任务 3 原话）。
// 这条用「总是出错的调用」验证：release 必须由延迟执行保证，不能只写在成功
// 分支里。
func TestDrainGate_ReleaseOnErrorPath(t *testing.T) {
	g := newDrainGate()

	// 模拟 20 次全部出错的调用。每次都走「enter → defer release → 返回错误」。
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.enter()
			if err != nil {
				return
			}
			defer release()
			_ = errors.New("这次调用失败了")
		}()
	}
	wg.Wait()

	// 全部结束后排空应当立刻完成。给一个很短的超时：若有漏减，这里会超时失败。
	if err := g.drain(300 * time.Millisecond); err != nil {
		t.Errorf("全部调用（都以错误结束）之后 drain 失败: %v —— "+
			"计数在错误路径上漏减了；漏减的后果是排空永远等不到零，reload 永远超时", err)
	}
}

// TestDrainGate_ConcurrentEnterAndDrain 在 -race 下压一遍并发进出与排空。
func TestDrainGate_ConcurrentEnterAndDrain(t *testing.T) {
	g := newDrainGate()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.enter()
			if err != nil {
				return // 已关门，正常
			}
			defer release()
			time.Sleep(time.Millisecond)
		}()
	}

	time.Sleep(5 * time.Millisecond)
	drainErr := g.drain(2 * time.Second)
	wg.Wait()

	if drainErr != nil {
		var timeoutErr *DrainTimeoutError
		if !errors.As(drainErr, &timeoutErr) {
			t.Errorf("drain 返回了非超时错误: %v", drainErr)
		}
	}
	// 排空之后门必须是关着的，不会自己打开。
	if _, err := g.enter(); !errors.Is(err, errDraining) {
		t.Errorf("排空完成后 enter() = %v, want errDraining —— 门一旦关上就不该"+
			"自己打开，否则旧进程停掉之后还会有调用打进来", err)
	}
}

// waitFor 轮询等待 cond 成立，最多 2 秒。用轮询而不是 sleep 一个固定时长：
// 后者在慢机器上会偶发失败，而这类偶发失败最消耗排查时间。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待条件成立超时")
}

// 保证 context 被 import（drain 的签名不用 ctx，但同包其它文件用）。
var _ = context.Background
