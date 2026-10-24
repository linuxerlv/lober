package execution

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// ControlBackend 保留原任务控制的四个方法和保存约定。
type ControlBackend interface {
	Cancel(context.Context, model.Task, repository.Audit) error
	Retry(context.Context, model.Task, repository.Audit) error
	CanCancel(model.Task) []model.Problem
	CanRetry(model.Task) []model.Problem
}

// AuditedControl 只把普通审计意图交给原任务控制，不改变状态规则。
type AuditedControl struct{ backend ControlBackend }

// NewAuditedControl 包装既有控制器；构造不访问存储或时钟。
func NewAuditedControl(backend ControlBackend) *AuditedControl {
	return &AuditedControl{backend: backend}
}

// Cancel 原样传递任务、上下文和四项审计事实；保存、通知、取消及重复请求规则由原控制器处理，错误原样返回。
func (c *AuditedControl) Cancel(ctx context.Context, t model.Task, a ports.AuditIntent) error {
	return c.backend.Cancel(ctx, t, repository.Audit{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At})
}

// Retry 原样传递任务、上下文和四项审计事实；保存、通知、取消及重复请求规则由原控制器处理，错误原样返回。
func (c *AuditedControl) Retry(ctx context.Context, t model.Task, a ports.AuditIntent) error {
	return c.backend.Retry(ctx, t, repository.Audit{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At})
}

// CanCancel 原样返回原控制器给出的原因，不保存或写审计。
func (c *AuditedControl) CanCancel(t model.Task) []model.Problem { return c.backend.CanCancel(t) }

// CanRetry 原样返回原控制器给出的原因，不保存或写审计。
func (c *AuditedControl) CanRetry(t model.Task) []model.Problem { return c.backend.CanRetry(t) }
