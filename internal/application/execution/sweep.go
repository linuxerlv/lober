package execution

import (
	"context"
	"errors"

	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Sweeper 只负责扫超时任务，每次调用都从仓储读持久状态。
type Sweeper struct{ core }

// Sweep reads durable state on every invocation, including after process restart.
// Sweep 的八项应用约定唯一连接 docs/contracts/machine-execution.md「信号与超时扫描」的本方法说明
// 及「当前职责与共同保存边界」；不扩展权限、重试、审计或取消保证。
func (s *Sweeper) Sweep(ctx context.Context) error {
	q := repository.TaskQuery{States: []model.TaskState{model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing}, Limit: 100}
	for {
		page, err := s.repo.ListTasks(ctx, q)
		if err != nil {
			return err
		}
		for _, task := range page.Items {
			at := s.clock.Now()
			next, timedOut := executionrules.Timeout(task, at)
			if !timedOut {
				continue
			}
			_, err := s.commit(ctx, task, next, repository.NewEvent{At: at, Kind: "machine.timeout", Key: attemptKey(task, "timeout"), Fields: map[string]string{"state": string(task.State), "stage": string(task.Execution.Stage)}}, repository.Audit{})
			if err != nil && !errors.Is(err, repository.ErrConflict) {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		q.Cursor = page.Next
	}
}
