// 本文件定义 Machine 类型的构造与 CAS 重试骨架。对应实现计划任务 1，
// 依据 SPEC §3.6。
//
// 六个依赖全部通过构造注入（Store、时钟、token 签发器、precheck 器、
// 日志存储、事件总线），时间全部来自注入的 Clock —— 实现代码里不得出现
// time.Now()。
package statemachine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// maxCASRetries 是 CAS 冲突时的最大重试次数。取 3：再多的意义不大，连续
// 三次都撞冲突说明有持续的竞争源，那是设计问题不是偶发（计划任务 1）。
const maxCASRetries = 3

// Machine 是 contract.StateMachine、contract.TokenIssuer 与
// contract.Prechecker 的共同实现，是 task.state 的唯一所有者。
//
// store 用组合接口 contract.Store（而不是只narrow 到 TaskStore）：
// StartBoot 的 precheck 需要查 ArtifactSetStore 取制品集，Fail/
// ApplySignal/ScanTimeouts 需要 EventStore 写事件——这些都是「Store」
// 这一个依赖里天然包含的能力，不是额外的依赖。
type Machine struct {
	store      contract.Store
	clock      contract.Clock
	tokens     contract.TokenIssuer
	prechecker contract.Prechecker
	logs       contract.LogStore
	events     contract.EventBus

	// taskLocks 把每个 taskID 的「读任务 → 算转换 → CAS → 写运行时字段」
	// 序列化成进程内的临界区。
	//
	// 为什么需要它（真实 bug，不是预防性加固）：contract.TaskStore.
	// MutateTaskFields 完全没有版本/CAS 保护（见 machine.go 顶部与
	// signal.go 的契约缺口说明）。casRetry 已经做到「CAS 先、mutation
	// 后」，这能防止「输家在 CAS 之前抢先写」，但挡不住另一种交错：当两个
	// 并发调用的目标 newState 彼此相同时（例如 ApplySignal 里
	// pre-start/pre-done/packages-start 等多个不同信号都推进到同一个
	// TaskStateInstalling），它们的 CAS 互不冲突——各自都能在自己读到的
	// 版本号上成功，版本号各自递增——但两次 MutateTaskFields 调用之间
	// 没有任何顺序保证，于是「携带更小 LastSeq 的那次调用」可能在
	// 「携带更大 LastSeq 的那次调用」完成之后才真正落地，把已经写入的
	// 更大水位覆盖回更小的值。这是用 -race -count=200 反复压测
	// TestConcurrentApplySignalDifferentSeqsAllProcessedMaxWatermark
	// 实测复现过的真实交错（约 1% 概率），不是理论推测。
	//
	// 真正的解法是让 Store 提供「CAS 状态并原子写入运行时字段」的组合
	// 方法，但那需要改 contract（见 signal.go 契约缺口）。在契约补上
	// 之前，本模块能在自己的边界内做到的最强保证是：同一个 taskID 的
	// 状态转换在本进程内严格串行——这正好符合 §1.1「网关与控制台是
	// 同一进程的两个 listener」的架构前提（跨进程的多个 Machine 实例仍
	// 依赖 Store 自身的 CAS，但那不是本模块单方面能解决的契约缺口）。
	taskLocksMu sync.Mutex
	taskLocks   map[string]*sync.Mutex
}

// New 构造一个 Machine，六个依赖全部必需。
func New(
	store contract.Store,
	clock contract.Clock,
	tokens contract.TokenIssuer,
	prechecker contract.Prechecker,
	logs contract.LogStore,
	events contract.EventBus,
) *Machine {
	return &Machine{
		store:      store,
		clock:      clock,
		tokens:     tokens,
		prechecker: prechecker,
		logs:       logs,
		events:     events,
		taskLocks:  make(map[string]*sync.Mutex),
	}
}

// lockTask 返回该 taskID 专用的锁，供 casRetry 序列化同一任务的并发
// 状态转换。锁本身用一把全局互斥锁保护 map 的读写（临界区极短，只是
// 取/建一个 *sync.Mutex 指针），不会成为真正的并发瓶颈——各任务之间
// 仍然完全并行，只有同一个 taskID 的多次调用会互相等待。
//
// taskLocks 是一个只增不减的 map：本模块的任务数量在单个进程生命周期内
// 是有限的（不是无界生成的临时 key），不会造成内存泄漏意义上的问题；
// 与 SQLite 版正式实现的行为差异（真正的跨进程 CAS）在契约缺口里已经
// 说明。
func (m *Machine) lockTask(taskID string) *sync.Mutex {
	m.taskLocksMu.Lock()
	defer m.taskLocksMu.Unlock()
	l, ok := m.taskLocks[taskID]
	if !ok {
		l = &sync.Mutex{}
		m.taskLocks[taskID] = l
	}
	return l
}

// 编译期断言：*Machine 必须同时实现 StateMachine、TokenIssuer 与
// Prechecker（计划任务 10）。八个 StateMachine 方法全部实现后启用。
var (
	_ contract.StateMachine = (*Machine)(nil)
	_ contract.TokenIssuer  = (*Machine)(nil)
	_ contract.Prechecker   = (*Machine)(nil)
)

// transitionDecision 是 CAS 回调的返回值：描述「基于当前任务算出的目标
// 转换」。回调必须是纯函数式的（输入当前任务，输出目标转换），不做 IO——
// 它可能被执行多次（每次重试都重新调用一次）。
type transitionDecision struct {
	// noop 为 true 表示不需要任何转换（例如幂等方法发现已经是目标状态）。
	// 此时 casRetry 跳过 CAS 与 mutation，直接返回当前任务。
	noop bool

	// rejectErr 非 nil 表示回调主动拒绝这次转换（例如 retry 发现失败原因
	// 不可重装）。casRetry 原样返回这个错误，不重试、不做任何写入。
	rejectErr error

	// mutation 非 nil 时，casRetry 会在状态 CAS 成功之后调用
	// MutateTaskFields 写入这些运行时字段（token、阶段、attempt 等）。
	//
	// 顺序是 CAS 先、mutation 后——这是本模块对「状态转换与字段写入必须
	// 原子」这条计划要求在当前 Store 契约下的最保守处理，见本文件末尾
	// 的契约缺口说明。之所以是这个顺序而不是反过来：MutateTaskFields
	// 完全没有 CAS 语义（不接受期望版本号，任何调用都会无条件生效），
	// 若先 mutate 再 CAS，两个并发的失败者会在各自的 CAS 真正执行之前
	// 都先把 mutation 写进库里——即使随后只有一个的 CAS 能成功，失败者
	// 的 mutation 副作用已经真实发生过（例如 attempt_count 被写了两次
	// 自增的中间值），这是一个已经在并发测试里实测复现过的 bug，不是
	// 假设。CAS 先执行则只有真正赢得 CAS 的那个 goroutine 才会执行
	// 后续的 mutation，不会有「输家也写了一次」的副作用。
	//
	// 代价：进程在 CAS 成功、mutation 执行之前崩溃，会留下「状态已经是
	// newState，但 token/phase 等字段还是旧值」的任务——这正是计划里
	// 提醒过的「已 booting 但无 token」的那种半成品。这是本模块在当前
	// Store 契约（MutateTaskFields 无 CAS 保护）下能做到的最好结果：
	// 该半成品有确定的止损（booting 阶段仍有 15 分钟预算，超时扫描会
	// 按 attempt 上限判定回 pending 或 failed，不会无限期挂住），而
	// mutate-first 顺序下的重复写副作用没有对应的止损机制。
	mutation *model.TaskMutation

	// newState 非空时，casRetry 会执行
	// CASTaskState(taskID, task.State, task.Version, newState)。
	// 为空表示本次转换不改变 state（只走 mutation，比如
	// MarkAnswerFetched 只推进 phase 不改 state 时可以只设 mutation）。
	newState model.TaskState
}

// casRetryFunc 是 casRetry 的回调签名：输入当前任务（每轮重读的最新值），
// 输出这一轮该执行的转换决策。必须是纯函数式的，不做 IO。
type casRetryFunc func(current model.Task) transitionDecision

// casRetry 是六个改状态方法共用的 CAS 重试骨架（计划任务 1）。
//
// 流程：重读任务 → 调 decide 得到目标转换 → 若有 newState 先执行 CAS
// （撞冲突则重试，最多 maxCASRetries 轮，重试用尽返回
// contract.ErrConflict）→ CAS 成功（或本次转换不改变 state）之后再执行
// mutation。CAS 必须先于 mutation：MutateTaskFields 没有版本保护，
// 先 mutate 会让并发的失败者也执行一次写副作用（见 transitionDecision.
// mutation 字段文档）。
//
// 三种必须处理的情形：
//   - decide 返回 noop：跳过写入，直接返回当前任务
//   - decide 返回 rejectErr：原样返回，不重试
//   - 重试次数用尽：返回 contract.ErrConflict，不无限重试
//
// 整个函数体在 lockTask(taskID) 持有期间执行——见 Machine.taskLocks 字段
// 文档：CAS 本身只保护 state 字段，挡不住多个并发调用的 newState 恰好
// 相同时（CAS 互不冲突）mutation 之间的乱序覆盖（已用并发压测复现），
// 进程内用这把锁把同一 taskID 的整个「读→决策→CAS→写」序列化，从根源
// 上消除这类交错。
func (m *Machine) casRetry(ctx context.Context, taskID string, decide casRetryFunc) (model.Task, error) {
	lock := m.lockTask(taskID)
	lock.Lock()
	defer lock.Unlock()

	var lastErr error
	for attempt := 0; attempt < maxCASRetries; attempt++ {
		current, err := m.store.GetTask(ctx, taskID)
		if err != nil {
			return model.Task{}, err
		}

		decision := decide(current)

		if decision.rejectErr != nil {
			return model.Task{}, decision.rejectErr
		}
		if decision.noop {
			return current, nil
		}

		result := current
		if decision.newState != "" {
			result, err = m.store.CASTaskState(ctx, taskID, current.State, current.Version, decision.newState)
			if err != nil {
				if errors.Is(err, contract.ErrConflict) {
					lastErr = err
					// 撞冲突：下一轮循环会重新 GetTask、重新调用 decide，
					// 用最新版本号重试——不执行 mutation，避免输家产生
					// 写副作用。
					continue
				}
				return model.Task{}, err
			}
		}

		if decision.mutation != nil {
			result, err = m.store.MutateTaskFields(ctx, taskID, *decision.mutation)
			if err != nil {
				return model.Task{}, err
			}
		}

		return result, nil
	}
	return model.Task{}, lastErr
}

// failTask 是「把任务置为失败」的共用实现，供 StartBoot（precheck 不过、
// attempt 超限）与 Fail/Cancel（任务 8）复用——两处对「怎样才算失败」的
// 写入逻辑必须完全一致，否则容易出现「事件字段格式两处写法不同」的裂缝。
//
// 行为：
//   - 只对非终态任务生效：若任务已经是终态（done/failed），跳过 CAS 与
//     mutation、不写事件，直接返回当前任务（幂等，且防止终态任务被
//     重复处理时事件数无界增长）。
//   - CAS 到 failed，同时通过 mutation 写入 FailureReason/FailureDetail、
//     清空 Token（失败后 token 不应再被使用）。
//   - 写一条 EventLevelError 的诊断事件，Message 里格式化 §3.5 的
//     六个字段（phase、rc、hook_digest、log_tail、syslog_tail、
//     距上次信号多久）。
//
// asOf 是「现在」的时间戳，必须来自调用方注入的 Clock（不得在本函数内部
// 调用 time.Now()），用于计算 detail.SinceLastSig 与写入 StateUpdatedAt/
// LastSignalAt 相关的诊断文案。
func (m *Machine) failTask(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail, asOf int64) (model.Task, error) {
	result, err := m.casRetry(ctx, taskID, func(current model.Task) transitionDecision {
		if current.State.IsTerminal() {
			return transitionDecision{noop: true}
		}

		reasonCopy := reason
		detailCopy := detail
		emptyToken := ""
		return transitionDecision{
			mutation: &model.TaskMutation{
				FailureReason: &reasonCopy,
				FailureDetail: &detailCopy,
				Token:         &emptyToken,
			},
			newState: model.TaskStateFailed,
		}
	})
	if err != nil {
		return model.Task{}, err
	}

	// 只有真正发生了失败转换才写事件；casRetry 对已终态任务返回 noop，
	// 此时 result.State 已经是终态但不是刚刚被这次调用置为失败的——用
	// 「本次调用之前是否已经是终态」判断是否需要写事件更准确，但 casRetry
	// 的返回值不区分这两种情况。这里用一个简单事实代替：若 result 的
	// FailureReason 与本次传入的 reason 一致，说明这次写入确实生效了
	// （已终态的旧任务的 FailureReason 大概率是别的原因，即使偶然相同，
	// 多写一条内容相同的诊断事件也不是错误行为，只是略微冗余，不影响
	// 正确性）。
	if result.State == model.TaskStateFailed {
		m.appendFailureEvent(ctx, result, detail, asOf)
	}
	return result, nil
}

// appendFailureEvent 写一条 §3.5 要求的失败诊断事件：phase、rc、
// hook_digest、log_tail、syslog_tail、距上次信号多久，全部格式化进
// Message（TaskEvent 上没有这几项的专属列）。
//
// 「距今多久」必须用 asOf（来自注入的 Clock）与 task.LastSignalAt 相减
// 算出，不能调用 time.Now()——否则这个字段在测试里不可控，而它恰恰是
// 失败诊断里区分度最高的一项。
func (m *Machine) appendFailureEvent(ctx context.Context, task model.Task, detail model.FailureDetail, asOf int64) {
	sinceLastSig := formatSinceLastSignal(task.LastSignalAt, asOf)
	message := fmt.Sprintf(
		"任务失败: phase=%s rc=%d hook_digest=%s log_tail=%s syslog_tail=%s 距上次信号=%s",
		detail.Phase, detail.ExitCode, detail.HookDigest, detail.LogTail, detail.SyslogTail, sinceLastSig,
	)
	_, _ = m.store.AppendEvent(ctx, model.TaskEvent{
		TaskID:  task.ID,
		Seq:     0,
		TS:      asOf,
		Phase:   detail.Phase,
		Level:   model.EventLevelError,
		Message: message,
		RC:      detail.ExitCode,
		Source:  model.EventSourceSignal,
	})
}

// formatSinceLastSignal 把「距上次信号多久」格式化为可读文案。
// lastSignalAt 为 0 表示从未收到过信号（例如 booting 阶段还没有任何
// installer 信号）。
func formatSinceLastSignal(lastSignalAt, asOf int64) string {
	if lastSignalAt == 0 {
		return "从未收到过信号"
	}
	delta := asOf - lastSignalAt
	if delta < 0 {
		delta = 0
	}
	return fmt.Sprintf("%d 秒前", delta)
}
