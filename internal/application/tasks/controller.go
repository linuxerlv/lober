package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
)

type controller struct{ deps ControllerDeps }

// NewController 要求显式提供所有能力，不在请求时寻找替代实现。
func NewController(d ControllerDeps) Controller {
	if d.Tasks == nil || d.Backend == nil || d.Clock == nil || d.Notifier == nil {
		panic("任务控制缺少必要能力")
	}
	return &controller{deps: d}
}
func (c *controller) control(ctx context.Context, actor ports.Actor, id string, retry bool) (TaskChoice, error) {
	permission, action := access.PermTaskCancel, "task.cancel"
	if retry {
		permission, action = access.PermTaskRetry, "task.retry"
	}
	if err := actor.Authorize(permission); err != nil {
		return TaskChoice{}, err
	}
	t, err := c.deps.Tasks.GetTask(ctx, id)
	if err != nil {
		return TaskChoice{}, err
	}
	audit := ports.AuditIntent{Actor: actor.Username, Action: action, At: c.deps.Clock()}
	if retry {
		err = c.deps.Backend.Retry(ctx, t, audit)
	} else {
		err = c.deps.Backend.Cancel(ctx, t, audit)
	}
	if err != nil {
		return TaskChoice{}, err
	}
	back, err := c.deps.Tasks.GetTask(ctx, t.ID)
	if err != nil {
		return TaskChoice{}, err
	}
	c.deps.Notifier.Publish(back.ID)
	return taskChoice(back, c.deps.Backend), nil
}
func (c *controller) Cancel(ctx context.Context, a ports.Actor, id string) (TaskChoice, error) {
	return c.control(ctx, a, id, false)
}
func (c *controller) Retry(ctx context.Context, a ports.Actor, id string) (TaskChoice, error) {
	return c.control(ctx, a, id, true)
}

type unavailableControl struct{}

// NewUnavailableControl 用于没有启用机器服务的组装，不执行或伪造状态变化。
func NewUnavailableControl() port.Control { return unavailableControl{} }
func (unavailableControl) Cancel(context.Context, model.Task, ports.AuditIntent) error {
	return ErrControlUnavailable
}
func (unavailableControl) Retry(context.Context, model.Task, ports.AuditIntent) error {
	return ErrControlUnavailable
}
func (unavailableControl) CanCancel(model.Task) []model.Problem { return nil }
func (unavailableControl) CanRetry(model.Task) []model.Problem  { return nil }
