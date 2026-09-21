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

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// maxCASRetries 是 CAS 冲突时的最大重试次数。取 3：再多的意义不大，连续
// 三次都撞冲突说明有持续的竞争源，那是设计问题不是偶发（计划任务 1）。
const maxCASRetries = 3

// Machine 是 contract.StateMachine、contract.TokenIssuer 与
// contract.Prechecker 的共同实现，是 task.state 的唯一所有者。
type Machine struct {
	store      contract.TaskStore
	clock      contract.Clock
	tokens     contract.TokenIssuer
	prechecker contract.Prechecker
	logs       contract.LogStore
	events     contract.EventBus
}

// New 构造一个 Machine，六个依赖全部必需。
func New(
	store contract.TaskStore,
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
	}
}

// 编译期断言：*Machine 必须同时实现 StateMachine、TokenIssuer 与
// Prechecker（计划任务 10 会解开；此刻先注释掉，等八个方法全部实现后再
// 启用，否则在实现过程中每次编译都会因未完成的方法报错）。
//
// var (
// 	_ contract.StateMachine = (*Machine)(nil)
// 	_ contract.TokenIssuer  = (*Machine)(nil)
// 	_ contract.Prechecker   = (*Machine)(nil)
// )

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

	// mutation 非 nil 时，casRetry 会先调用 MutateTaskFields 写入这些
	// 运行时字段（token、阶段、attempt 等），再执行状态 CAS。
	//
	// 顺序是 mutation 先、CAS 后：这是本模块对「状态转换与字段写入必须
	// 原子」这条计划要求在当前 Store 契约下的最保守处理 —— 见本文件末尾
	// 的契约缺口说明。mutation 先写的好处是，如果进程在两次调用之间崩溃，
	// 任务仍停留在旧状态（只是携带了一个尚未生效的新 token/phase），比
	// 「状态已经推进但字段没写」这种半成品更安全：旧状态下任务不会被误认
	// 为已经进入新阶段，且遗留的 token/字段会在下一次同样的转换尝试时被
	// 覆盖。
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
// 流程：重读任务 → 调 decide 得到目标转换 → 执行（或跳过）写入 → 撞 CAS
// 冲突则重试，最多 maxCASRetries 轮；重试用尽返回 contract.ErrConflict。
//
// 三种必须处理的情形：
//   - decide 返回 noop：跳过写入，直接返回当前任务
//   - decide 返回 rejectErr：原样返回，不重试
//   - 重试次数用尽：返回 contract.ErrConflict，不无限重试
func (m *Machine) casRetry(ctx context.Context, taskID string, decide casRetryFunc) (model.Task, error) {
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
		if decision.mutation != nil {
			result, err = m.store.MutateTaskFields(ctx, taskID, *decision.mutation)
			if err != nil {
				return model.Task{}, err
			}
		}

		if decision.newState == "" {
			// 本次转换不改变 state（只有 mutation），mutation 本身不走
			// CAS 条件，因此不存在「冲突」这一说——直接返回。
			return result, nil
		}

		result, err = m.store.CASTaskState(ctx, taskID, current.State, current.Version, decision.newState)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, contract.ErrConflict) {
			return model.Task{}, err
		}
		lastErr = err
		// 撞冲突：下一轮循环会重新 GetTask，用最新版本号重试。
	}
	return model.Task{}, lastErr
}
