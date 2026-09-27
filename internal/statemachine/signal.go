// 本文件实现应用信号 ApplySignal（任务 6）与签发 finalize token
// IssueFinalizeToken（任务 7）。依据 SPEC §3.6、§6.1、§6.4。
//
// 状态机由信号驱动。机器侧的网络不可靠、会重试、信号可能乱序到达，所以
// ApplySignal 必须对三种异常输入都正确：序号重复、阶段后退、任务已终态——
// 三者都是「记事件但不改状态」，不是错误。
package statemachine

import (
	"context"
	"errors"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// errUnknownSignal 是 ApplySignal 对不在 model.LookupSignal 表中的信号名
// 返回的错误——信号名来自 URL 路径段，不查表校验等于让机器指定自己的
// 阶段。不导出为 sentinel error：调用方（网关 handler）本应在更早的位置
// 拒绝，走到这里说明是调用方的职责疏漏，不需要用 errors.Is 精细识别。
var errUnknownSignal = errors.New("statemachine: 未知的信号名")

// ApplySignal 处理机器侧上报的信号（SPEC §6.1、§3.6）。
func (m *Machine) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	entry, ok := model.LookupSignal(signal)
	if !ok {
		return contract.SignalOutcome{}, errUnknownSignal
	}

	now := m.clock.Now()

	// 写事件的去重键用**信号表序号** entry.Seq，不是客户端传来的 seq。
	//
	// 两者是不同的东西，混用会让去重失效（2026-09-26 首次真实装机踩到）：
	// 机器侧的 seq 是每个阶段各自从 1 开始数的（installer 脚本里每段自己
	// 维护计数器），而事件表的唯一索引 idx_task_events_task_seq 建在
	// (task_id, seq) 上、跨阶段全局唯一。于是 packages-done(seq=1) 落库后，
	// post-start(seq=1) 撞 23505 被翻译成 ErrDuplicateSeq 并吞掉（那是给
	// 机器侧重试用的幂等路径），状态机判定重复投递、不推进状态——而网关
	// 照样返回 200。真实后果：3222 个包装完、rebooting 也发到了，任务却
	// 永远停在 installing，events 表一条记录都没有。
	//
	// entry.Seq 是 signalTable 里的全局单调序号（10/20/…/110，十的间隔留给
	// 将来插入中间信号），本方法下面判定水位「只进不退」用的正是它
	// （§3.6）。去重键与水位键取同一个值，语义才自洽：同一个信号重复投递
	// 才算重复，不同信号永远不撞。
	//
	// 客户端 seq 因此不再参与落库。它作为形参保留是因为契约文档端点 4 要求
	// 缺失或非法时拒绝请求（网关层已校验），值本身对状态机没有用途。
	//
	// hook-report 的表序号是 0：与阶段正交，一个阶段内可来任意多次，永不
	// 推进水位，也不参与去重（走非信号事件的自增负数序号路径）。
	eventSeq := entry.Seq

	_, appendErr := m.store.AppendEvent(ctx, model.TaskEvent{
		TaskID:  taskID,
		Seq:     eventSeq,
		TS:      now,
		Message: fmt.Sprintf("信号: %s", signal),
		Source:  model.EventSourceSignal,
	})
	duplicateSeq := errors.Is(appendErr, contract.ErrDuplicateSeq)
	if appendErr != nil && !duplicateSeq {
		return contract.SignalOutcome{}, appendErr
	}

	// 上次信号时间必须更新，即使信号未被接受改状态——这是 §3.5「距今
	// 多久」诊断字段的数据来源，刚收到信号说明机器活着。
	result, err := m.casRetry(ctx, taskID, func(current model.Task) transitionDecision {
		lastSignalAt := now

		if duplicateSeq {
			// 序号重复：只更新上次信号时间，不改状态、不改水位。
			return transitionDecision{
				mutation: &model.TaskMutation{LastSignalAt: &lastSignalAt},
			}
		}
		if current.State.IsTerminal() {
			// 终态任务不该被信号复活，只更新上次信号时间。
			return transitionDecision{
				mutation: &model.TaskMutation{LastSignalAt: &lastSignalAt},
			}
		}
		if entry.Seq == 0 {
			// hook-report：与阶段正交，不推进水位，只更新上次信号时间。
			return transitionDecision{
				mutation: &model.TaskMutation{LastSignalAt: &lastSignalAt},
			}
		}
		if entry.Seq <= current.LastSeq {
			// 阶段只能前进不能后退：延迟到达的早期信号不改变阶段，只
			// 更新上次信号时间。
			return transitionDecision{
				mutation: &model.TaskMutation{LastSignalAt: &lastSignalAt},
			}
		}

		// 正常推进：更新状态、阶段、水位、上次信号时间。
		lastSeq := entry.Seq
		phase := entry.Phase
		phaseEnteredAt := now
		return transitionDecision{
			mutation: &model.TaskMutation{
				CurrentPhase:   &phase,
				PhaseEnteredAt: &phaseEnteredAt,
				LastSeq:        &lastSeq,
				LastSignalAt:   &lastSignalAt,
			},
			newState: entry.State,
		}
	})
	if err != nil {
		return contract.SignalOutcome{}, err
	}

	stateChanged := !duplicateSeq && entry.Seq != 0 &&
		result.CurrentPhase == entry.Phase && result.LastSeq == entry.Seq

	if stateChanged && entry.State == model.TaskStateDone {
		// ready 信号推进到 done，发布「任务就绪」生命周期事件——§7.5：
		// 那时才写 DNS、注册 CMDB。
		m.events.Publish(contract.LifecycleEventTaskReady, result)
	} else if stateChanged {
		m.events.Publish(contract.LifecycleEventPhaseChanged, result)
	}

	return contract.SignalOutcome{
		Accepted:     true,
		StateChanged: stateChanged,
		Task:         result,
	}, nil
}

// IssueFinalizeToken 在渲染 %post 之前调用，签发 finalize token 并写入
// 任务（SPEC §6.4）。
//
// 幂等：已有未过期的 finalize token 时返回同一个。不幂等的后果是重复
// 渲染（比如 Ubuntu 取两个文件）会换掉 token，而先前写进目标系统的那个
// 就失效了——这不只是「快速连续调用两次」的场景，并发调用（两个请求几乎
// 同时到达）同样会踩到：若不加锁，两个 goroutine 都可能读到「尚未签发」，
// 各自生成不同的 token，只有最后一次 MutateTaskFields 真正落库，另一个
// 调用方拿到的 token 从未被持久化过，等它被写进目标系统时已经形同虚设
// （已用并发压测复现，见 concurrency_test.go）。
//
// 用 m.lockTask 而不是普通的 GetTask+MutateTaskFields：本方法不涉及状态
// 转换（不改 task.state），不需要 casRetry 的 CAS 骨架，但仍需要与
// casRetry 共享同一把「同 taskID 的读-决策-写序列化」的锁——否则本方法
// 与 casRetry 驱动的其它方法之间、或本方法自己的并发调用之间，仍然会
// 出现 MutateTaskFields 缺乏版本保护导致的乱序覆盖（见 machine.go
// Machine.taskLocks 字段文档）。
func (m *Machine) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	lock := m.lockTask(taskID)
	lock.Lock()
	defer lock.Unlock()

	now := m.clock.Now()

	current, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if current.FinalizeToken != "" && current.FinalizeExpiresAt > now {
		// 已有未过期的 finalize token：直接返回同一个，不重新签发。
		return current, nil
	}

	token, err := m.tokens.IssueToken(ctx)
	if err != nil {
		return model.Task{}, err
	}
	expiresAt := now + int64(model.FinalizeTokenTTL.Seconds())

	result, err := m.store.MutateTaskFields(ctx, taskID, model.TaskMutation{
		FinalizeToken:     &token,
		FinalizeExpiresAt: &expiresAt,
	})
	if err != nil {
		return model.Task{}, err
	}
	return result, nil
}
