// 本文件实现在途调用的排空（P3-04 第一半任务 3），依据 SPEC §7.3 第二项。
//
// **这是 gRPC 加载器唯一 exec/http 完全不需要的机制。** 那两者每次调用是一个
// 独立的子进程或一个独立的 HTTP 请求，调用结束进程/连接就没了，没有「在途」
// 这个概念；gRPC 插件是一个长驻进程，替换它之前必须先让已经发出去的调用跑完。
//
// 形态（§7.3 给定）：一个带计数的门包住调用点——停止新请求进入、等在途调用
// 结束（带超时）、再停旧进程。
package grpc

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// errDraining 是排空开始后新调用被拒绝时返回的哨兵错误。
//
// 用哨兵而不是一句话：调用方（工厂装配出来的 provider 适配层）需要区分「插件
// 正在被替换，这次调用可以重试」与「插件真的坏了」——前者重试一下就好，后者
// 重试只是浪费时间。
var errDraining = errors.New("loader/grpc: 插件正在被替换，本次调用未发起（可稍后重试）")

// DrainTimeoutError 表示排空超时：等待期满时仍有在途调用没结束。
//
// **超时之后调用方仍然应当停掉旧进程**（计划任务 3 明确要求）：一个卡住的调用
// 不该无限期阻塞 reload，而 reload 往往正是在修一个影响装机的问题。因此本类型
// 不是「别停」的信号，而是「停了，但有调用被强制中断」的记录。
//
// InFlight 带出被中断的调用数：它决定那条日志该读成「偶发卡了一个」还是
// 「插件整体卡住了」——两者的后续处置完全不同。
type DrainTimeoutError struct {
	InFlight int
	Waited   time.Duration
}

func (e *DrainTimeoutError) Error() string {
	return fmt.Sprintf("loader/grpc: 排空等待 %v 后仍有 %d 个在途调用未结束，"+
		"旧插件进程仍将被停止——这些调用会被强制中断，"+
		"是排查「某个任务的 provider 调用为什么失败」时的线索", e.Waited, e.InFlight)
}

// drainGate 是带计数的门：记录在途调用数，支持一次性关门并等待清零。
//
// 用 sync.Cond 而不是 channel 或忙等：等待方要等的是「计数变成 0」这个条件，
// 而 Cond 正是为这种形态设计的——channel 方案要在每次 release 时判断是否该往
// 信号 channel 里写，那个判断本身要拿锁，反而更绕。
type drainGate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inFlight int

	// draining 一旦置真就**永不回到假**：这个门是一次性的。
	//
	// 不做可重开的门是刻意的：一次 reload 对应一个新的 drainGate（新插件实例
	// 带自己的门）。可重开的门会引入「排空完成后又被打开、而旧进程已经停了」
	// 这个状态——那时打进来的调用会发到一个已经死掉的进程上。
	draining bool
}

func newDrainGate() *drainGate {
	g := &drainGate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// enter 登记一次在途调用，返回必须被调用的 release 函数。
//
// **调用方必须用 defer 调 release**（计划任务 3 点명的易错处）：在发起调用前加、
// 在收到响应或出错后减，而错误路径与 panic 路径都必须减。漏减的后果是排空永远
// 等不到零，而 reload 永远超时——那种故障的症状（reload 总是超时）与真正的原因
// （某个错误分支忘了 release）之间毫无提示关系。
//
// 已开始排空时返回 errDraining，不登记。
func (g *drainGate) enter() (release func(), err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.draining {
		return nil, errDraining
	}
	g.inFlight++

	// release 幂等：重复调用不会把计数减到负数。幂等是因为调用点很可能同时有
	// defer release() 与某个错误分支里的显式 release()——那种重复在重构中很容易
	// 引入，而减到负数会让排空**提前**完成（计数从 -1 涨回 0），于是旧进程在
	// 还有调用在途时就被停了。
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.inFlight--
			if g.inFlight <= 0 {
				g.cond.Broadcast()
			}
		})
	}, nil
}

// isDraining 报告门是否已关。仅供测试与日志使用。
func (g *drainGate) isDraining() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.draining
}

// drain 关门并等待在途调用清零，最多等 timeout。
//
// 返回 nil 表示在途调用已全部结束；返回 *DrainTimeoutError 表示超时，
// **调用方仍应继续停掉旧进程**，并把这个错误记进日志。
//
// 超时的实现：起一个计时器 goroutine 去 Broadcast 唤醒等待，让 Wait 能退出。
// 不用 context.WithTimeout 包住 Cond.Wait 是因为 Cond 没有可取消的 Wait——
// 唤醒只能靠 Signal/Broadcast，所以计时器必须主动 Broadcast 一次。
func (g *drainGate) drain(timeout time.Duration) error {
	g.mu.Lock()
	g.draining = true
	g.mu.Unlock()

	deadline := time.Now().Add(timeout)

	// 计时器到点后 Broadcast，把可能还在 Wait 的本 goroutine 唤醒去重新判断
	// 时间。stop 用来在正常清零后收掉计时器，不泄漏。
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-t.C:
			g.mu.Lock()
			g.cond.Broadcast()
			g.mu.Unlock()
		case <-stop:
		}
	}()

	g.mu.Lock()
	defer g.mu.Unlock()
	for g.inFlight > 0 {
		if !time.Now().Before(deadline) {
			return &DrainTimeoutError{InFlight: g.inFlight, Waited: timeout}
		}
		g.cond.Wait()
	}
	return nil
}
