package execution

import "github.com/lober/lober/internal/domain/model"

// CanBoot 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func CanBoot(t model.Task) bool {
	return t.State == model.TaskPending || t.State == model.TaskBooting
}

// InstallingNeeded 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func InstallingNeeded(t model.Task) bool { return t.State != model.TaskInstalling }

// StartInstalling 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
// 调用者须先通过原安装凭据检查，保证执行记录非空。
func StartInstalling(t model.Task, at int64) (model.Task, bool) {
	if !InstallingNeeded(t) {
		return t, false
	}
	t = clone(t)
	t.State = model.TaskInstalling
	stage(t.Execution, model.StagePackages, at, packagesBudget)
	return t, true
}

// CanCancel 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func CanCancel(t model.Task) []model.Problem {
	if !t.State.Finished() {
		return nil
	}
	return []model.Problem{model.NewProblem("task_control_state", model.Subject{Kind: "task", ID: t.ID}, "取消")}
}

// CanRetry 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func CanRetry(t model.Task) []model.Problem {
	if t.Execution != nil && t.Execution.ReachedReboot {
		return []model.Problem{model.NewProblem("task_retry_after_reboot", model.Subject{Kind: "task", ID: t.ID})}
	}
	if t.State == model.TaskFailed || t.State == model.TaskCancelled {
		return nil
	}
	return []model.Problem{model.NewProblem("task_control_state", model.Subject{Kind: "task", ID: t.ID}, "重试")}
}

// Cancel 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func Cancel(t model.Task, at int64) (model.Task, []model.Problem) {
	if problems := CanCancel(t); len(problems) > 0 {
		return t, problems
	}
	t = clone(t)
	if t.Execution == nil {
		t.Execution = runtime(at, 1)
	}
	t.State = model.TaskCancelled
	t.Execution.Deadline = 0
	return t, nil
}

// Retry 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func Retry(t model.Task, at int64) (model.Task, []model.Problem) {
	if problems := CanRetry(t); len(problems) > 0 {
		return t, problems
	}
	t = clone(t)
	if t.Execution == nil {
		t.Execution = runtime(at, 1)
	}
	t.Execution = runtime(at, t.Execution.Attempt+1)
	t.State = model.TaskPending
	return t, nil
}

// Timeout 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
func Timeout(t model.Task, at int64) (model.Task, bool) {
	if t.Execution == nil || t.Execution.Deadline == 0 || at < t.Execution.Deadline {
		return t, false
	}
	t = clone(t)
	t.State = model.TaskFailed
	t.Execution.Deadline = 0
	return t, true
}
