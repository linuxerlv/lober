// 本文件实现超时扫描 ScanTimeouts（任务 9）。依据 SPEC §3.3、§3.4。
//
// 分阶段超时能在数分钟内报出卡在哪个阶段。这是「机器卡住了」的唯一发现
// 途径——机器不会主动报告自己挂了。
package statemachine

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/model"
)

// rebootingTimeoutMarker 与 initializingTimeoutMarker 是 rebooting/
// initializing 超时诊断事件里必须出现的标记文案，用来与「装机失败」明确
// 区分——两者处理方式完全不同（前者不能重装，见 SPEC §3.4）。
const (
	rebootingTimeoutMarker    = "装好但未起来"
	initializingTimeoutMarker = "已就绪但初始化未完成"
)

// ScanTimeouts 由 30 秒周期循环调用，扫描并处理所有超时任务
// （model.PhaseBudget 定义的预算，pending 阶段永不超时）。返回本轮被判
// 失败的任务数（不含回 pending 的）。
func (m *Machine) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	candidates, err := m.store.ListTimedOutTasks(ctx, asOf)
	if err != nil {
		return 0, err
	}

	failedCount := 0
	for _, candidate := range candidates {
		failed, err := m.handleTimedOutTask(ctx, candidate, asOf)
		if err != nil {
			// 单个任务处置失败不中断整轮：记日志继续，不返回错误——
			// 一个任务撞 CAS 冲突不该让其余任务本轮不被处理。本模块
			// 没有被注入 logger 依赖（六个依赖里没有这一项），因此这里
			// 只能静默跳过；见本文件末尾契约缺口说明。
			continue
		}
		if failed {
			failedCount++
		}
	}
	return failedCount, nil
}

// handleTimedOutTask 处置单个超时候选任务，返回该任务是否被判 failed
// （true）或回到 pending / 因已被并发操作终态化而跳过（false）。
func (m *Machine) handleTimedOutTask(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	if candidate.CurrentPhase == model.PhaseBooting {
		return m.handleBootingTimeout(ctx, candidate, asOf)
	}
	return m.handleOtherPhaseTimeout(ctx, candidate, asOf)
}

// handleBootingTimeout 处置 booting 阶段超时：SPEC §3.3 与 §3.4 两处措辞
// 不同（前者写「允许重试，attempt++」，后者把「booting 超时」列为失败
// 态），本模块采用的决断：attempt_count < 上限则回 pending（机器可自行
// 再来一次，且回 pending 时不增加 attempt——attempt 是在实际签发 token
// 时增加的，这里再加一次会让每次超时消耗两个额度）；已达上限则置
// failed（原因是 attempt 超限）。
func (m *Machine) handleBootingTimeout(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	if candidate.AttemptCount < model.MaxBootAttempts {
		result, err := m.casRetry(ctx, candidate.ID, func(current model.Task) transitionDecision {
			if current.State.IsTerminal() || current.CurrentPhase != model.PhaseBooting {
				// 扫描到候选之后、真正处置之前，任务已被并发操作
				// 终态化或推进出 booting 阶段——跳过，不算失败。
				return transitionDecision{noop: true}
			}
			pendingPhase := model.PhasePending
			return transitionDecision{
				mutation: &model.TaskMutation{
					CurrentPhase:   &pendingPhase,
					PhaseEnteredAt: &asOf,
				},
				newState: model.TaskStatePending,
			}
		})
		if err != nil {
			return false, err
		}
		_ = result
		return false, nil
	}

	if _, err := m.failTask(ctx, candidate.ID, model.FailureReasonAttemptExceeded, model.FailureDetail{
		Phase: model.PhaseBooting,
	}, asOf); err != nil {
		return false, err
	}
	return true, nil
}

// handleOtherPhaseTimeout 处置 booting 之外各阶段的超时：按 M1 的超时
// 原因映射置 failed。rebooting 与 initializing 的失败原因落在不可重装
// 那一组（M1 已有测试锁住这条映射，本模块只需正确使用它）。
func (m *Machine) handleOtherPhaseTimeout(ctx context.Context, candidate model.Task, asOf int64) (bool, error) {
	reason, ok := model.PhaseTimeoutReason(candidate.CurrentPhase)
	if !ok {
		// model.PhaseBudget 已经保证只有有预算的阶段才会出现在
		// ListTimedOutTasks 的结果里，因此这里理论上不会发生；防御性地
		// 跳过而不是 panic。
		return false, nil
	}

	budget, _ := model.PhaseBudget(candidate.CurrentPhase)
	actualElapsed := asOf - candidate.PhaseEnteredAt

	logTail := fmt.Sprintf("阶段 %s 超时: 预算=%s 实际已过=%d秒", candidate.CurrentPhase, budget, actualElapsed)
	switch candidate.CurrentPhase {
	case model.PhaseRebooting:
		logTail = fmt.Sprintf("[%s] %s", rebootingTimeoutMarker, logTail)
	case model.PhaseInitializing:
		logTail = fmt.Sprintf("[%s] %s", initializingTimeoutMarker, logTail)
	}

	if _, err := m.failTask(ctx, candidate.ID, reason, model.FailureDetail{
		Phase:   candidate.CurrentPhase,
		LogTail: logTail,
	}, asOf); err != nil {
		return false, err
	}
	return true, nil
}
