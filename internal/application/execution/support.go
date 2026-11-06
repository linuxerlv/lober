package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func taskSupportProblem(task model.Task) *model.Problem {
	if task.Snapshot.SystemSupported() {
		return nil
	}
	p := model.NewProblem("task_system_unsupported", model.Subject{Kind: "task", ID: task.ID}, task.Snapshot.Series, task.Snapshot.Format)
	return &p
}
func checkTaskSupport(task model.Task) error {
	if p := taskSupportProblem(task); p != nil {
		return &repository.ProblemsError{Kind: repository.ErrRejected, Problems: []model.Problem{*p}}
	}
	return nil
}

// CheckUnfinishedTasks 在任何初始化和恢复之前检查旧任务；只读取，不替客户机更改任务状态。
func CheckUnfinishedTasks(ctx context.Context, repo interface {
	ListTasks(context.Context, repository.TaskQuery) (repository.Page[model.Task], error)
}) error {
	var blocked []error
	q := repository.TaskQuery{Limit: 100}
	for _, state := range model.TaskStates() {
		if !state.Finished() {
			q.States = append(q.States, state)
		}
	}
	for {
		page, err := repo.ListTasks(ctx, q)
		if err != nil {
			return err
		}
		for _, task := range page.Items {
			if !task.State.Finished() && !task.Snapshot.SystemSupported() {
				blocked = append(blocked, fmt.Errorf("任务 %s，主机 %s，系统 %s，格式 %s，状态 %s", task.ID, task.HostSN, task.Snapshot.Series, task.Snapshot.Format, task.State))
			}
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	if len(blocked) > 0 {
		return fmt.Errorf("不能启动：以下未完成任务使用已停止支持的系统。请先在旧版本中确认客户机已安全结束安装，并通过正常任务接口处理：%w", errors.Join(blocked...))
	}
	return nil
}
