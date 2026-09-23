// 本文件实现熔断器：连续失败达到阈值后标记加载器不可用，此后的调用直接
// 短路返回熔断错误，不再真正发起子进程/HTTP 调用（计划任务要点：「连续
// 失败 N 次标记 provider 不可用，之后按 §7.4 的失败语义处理」）。
//
// 熔断器本身不实现指数退避重试——那是 callWithRetry（protocol.go）的
// 职责，两者的关系是：熔断器包住"一次逻辑调用"（可能内部已经重试了
// 若干次），只要这次逻辑调用最终失败就记一次连续失败；熔断打开后，
// 后续调用不再执行 callWithRetry 里的任何一次尝试，直接返回熔断错误——
// 这正是「不再发起调用」的含义：省下的不是重试次数，是整次调用（含首次
// 尝试）。
//
// 熔断打开后不自动恢复（没有半开状态定时探测）：MVP 阶段的加载器失败
// 处理按 §7.4 的失败语义表交给调用方（阻塞类直接阻塞建任务，告警类只
// 告警），不需要熔断器自己尝试恢复——恢复的判断权交给运维（修好外部
// 系统或子进程脚本后重启进程，或未来 reload.go 提供的热替换配置）。
package loader

import "sync"

// circuitBreaker 是一个进程内、按加载器实例持有的连续失败计数器。
//
// threshold <= 0 表示禁用熔断（Allow 永远返回 true）——Config.BreakerThreshold
// 显式设为负数时对应这个语义（见 protocol.go 的 withDefaults 文档）。
type circuitBreaker struct {
	threshold int

	mu                 sync.Mutex
	consecutiveFailure int
	open               bool
}

// newCircuitBreaker 用给定阈值构造一个初始为「关闭」（允许调用）状态的
// 熔断器。
func newCircuitBreaker(threshold int) *circuitBreaker {
	return &circuitBreaker{threshold: threshold}
}

// Allow 判定当前是否允许发起一次新的调用。熔断已打开时返回 false，调用方
// （exec.go/http.go 的 Call 实现）必须在返回 false 时直接返回熔断错误，
// 不执行任何子进程/HTTP 操作。
func (b *circuitBreaker) Allow() bool {
	if b.threshold <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.open
}

// RecordResult 记录一次逻辑调用（可能已经历内部重试）的最终结果。err 非
// nil 视为一次失败，累加连续失败计数；计数达到阈值时打开熔断。err 为 nil
// 视为一次成功，清零连续失败计数——但**不**自动关闭已经打开的熔断（熔断
// 打开后不再发起调用，因此打开状态下不会再有成功的结果可供清零，这条
// 分支只在熔断尚未打开、单次调用恰好成功时生效，是正常的失败计数重置
// 路径，不是熔断恢复机制）。
func (b *circuitBreaker) RecordResult(err error) {
	if b.threshold <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil {
		b.consecutiveFailure = 0
		return
	}

	b.consecutiveFailure++
	if b.consecutiveFailure >= b.threshold {
		b.open = true
	}
}

// isOpen 仅供测试断言熔断是否已经打开，不暴露为可导出方法——调用方唯一
// 需要的判断入口是 Allow（决定要不要发起调用），直接暴露内部状态位会让
// 调用方绕过 Allow 自己做判断分支，增加两处状态不一致的风险。
func (b *circuitBreaker) isOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.open
}
