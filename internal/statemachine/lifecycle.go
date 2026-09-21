// 本文件实现置失败 Fail、取消 Cancel 与重试 Retry（任务 8）。依据 SPEC
// §3.4、§3.5、§8.2。
//
// §3.4 是全系统最重要的安全约束：拒绝必须在状态机里，不是靠 UI 隐藏
// 按钮——UI 可以被绕过（直接调 API），状态机不能。
package statemachine

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Fail 把任务置为失败，写入 model.FailureDetail 描述的 §3.5 六个诊断
// 字段。
func (m *Machine) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	return m.failTask(ctx, taskID, reason, detail, m.clock.Now())
}

// Cancel 是人工取消：任意非终态 → failed（FailureReasonCanceled）。
// pending 任务取消后即不再响应引导（SPEC §3.2 的缓解手段）。
func (m *Machine) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	return m.failTask(ctx, taskID, model.FailureReasonCanceled, model.FailureDetail{}, m.clock.Now())
}

// Retry 重置 attempt_count 为 0、清空 token → pending（SPEC §8.2）。
//
// 对三种不可重装的失败原因必须拒绝：reboot_timeout、init_timeout、
// init_failed——判据是 reason.Reinstallable()（model 层已把这三个排除在
// 白名单外）。不满足时返回 contract.ErrNotReinstallable。
func (m *Machine) Retry(ctx context.Context, taskID string) (model.Task, error) {
	return m.casRetry(ctx, taskID, func(current model.Task) transitionDecision {
		if current.State != model.TaskStateFailed {
			return transitionDecision{rejectErr: errRetryRequiresFailed}
		}
		if !current.FailureReason.Reinstallable() {
			return transitionDecision{rejectErr: contract.ErrNotReinstallable}
		}

		zeroAttempt := 0
		emptyToken := ""
		zeroExpiresAt := int64(0)
		emptyReason := model.FailureReason("")
		pendingPhase := model.PhasePending
		now := m.clock.Now()

		// 契约缺口：model.TaskMutation.FailureDetail 是「指针的指针」——
		// 外层指针 nil 表示「不改该字段」，因此没有办法通过 MutateTaskFields
		// 把已经存在的 FailureDetail 显式清成 nil（传 nil 会被当作「跳过」，
		// 而不是「设为 nil」）。最保守的处理：不在这里清空 FailureDetail，
		// 只清空 FailureReason——诊断信息展示层应以 FailureReason=="" 判断
		// 「当前没有生效中的失败」，不应单独依赖 FailureDetail 是否为 nil。
		// 真正的解法是把 TaskMutation.FailureDetail 改成一个「是否要清空」
		// 与「新值」分离的形式（例如加一个 ClearFailureDetail bool 字段），
		// 但这需要改 contract 与两个 Store 实现，不是本模块能单方面做的。
		return transitionDecision{
			mutation: &model.TaskMutation{
				AttemptCount:   &zeroAttempt,
				Token:          &emptyToken,
				TokenExpiresAt: &zeroExpiresAt,
				FailureReason:  &emptyReason,
				CurrentPhase:   &pendingPhase,
				PhaseEnteredAt: &now,
			},
			newState: model.TaskStatePending,
		}
	})
}

// errRetryRequiresFailed 是 Retry 对非 failed 任务的拒绝错误：只有失败
// 的任务才谈得上「重试」。不导出为 sentinel error——调用方不需要用
// errors.Is 精细识别这条，与 contract.ErrNotReinstallable（需要精细识别
// 以回正确 HTTP 状态码）不同。
var errRetryRequiresFailed = errors.New("statemachine: 只有 failed 状态的任务才能 retry")
