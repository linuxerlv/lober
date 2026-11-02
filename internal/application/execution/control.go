package execution

import (
	"context"

	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Control 只负责用户发起的取消与重试，按用户看到的那份修订号保存。
type Control struct{ core }

var _ ControlBackend = (*Control)(nil)

// CanCancel 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (*Control) CanCancel(t model.Task) []model.Problem { return executionrules.CanCancel(t) }

// CanRetry 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (*Control) CanRetry(t model.Task) []model.Problem { return executionrules.CanRetry(t) }

// User controls use the exact revision the user reviewed. Conflicts require refresh.
func (c *Control) control(ctx context.Context, task model.Task, audit repository.Audit, retry bool) error {
	if err := repository.CheckAudit(audit); err != nil {
		return err
	}
	latest, err := c.repo.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if latest.Rev != task.Rev || latest.State != task.State {
		return repository.ErrConflict
	}
	problems := executionrules.CanCancel(latest)
	if retry {
		problems = executionrules.CanRetry(latest)
	}
	if len(problems) > 0 {
		return &repository.ProblemsError{Kind: repository.ErrRejected, Problems: problems}
	}
	at := c.clock.Now()
	var next model.Task
	kind := "cancelled"
	if retry {
		next, _ = executionrules.Retry(latest, at)
		kind = "retry"
	} else {
		next, _ = executionrules.Cancel(latest, at)
	}
	_, err = c.commit(ctx, latest, next, repository.NewEvent{At: at, Kind: "machine." + kind, Key: attemptKey(next, kind)}, audit)
	return err
}

// Cancel 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (c *Control) Cancel(ctx context.Context, t model.Task, a repository.Audit) error {
	return c.control(ctx, t, a, false)
}

// Retry 的八项应用约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (c *Control) Retry(ctx context.Context, t model.Task, a repository.Audit) error {
	return c.control(ctx, t, a, true)
}
