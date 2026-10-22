// Package port 定义向导实际需要的能力，不暴露其他模块的写入操作。
package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type Hosts interface {
	ListHosts(context.Context, repository.HostQuery) (repository.Page[model.Host], error)
	GetHosts(context.Context, []string) (map[string]model.Host, error)
}
type ISOs interface {
	GetISO(context.Context, string) (model.ISO, error)
	ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error)
}
type Templates interface {
	GetTemplate(context.Context, model.TemplateKey) (model.Template, error)
	ListTemplates(context.Context, repository.TemplateQuery) (repository.Page[model.Template], error)
	ListTemplateVersions(context.Context, string) ([]model.Template, error)
}
type Hooks interface {
	GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error)
}
type Tasks interface {
	FindOpenTask(context.Context, string) (model.Task, bool, error)
	// CreateCurrentTasks 在同一事务中确认当前模板并写任务及审计，成功才允许通知。
	CreateCurrentTasks(context.Context, []model.Task, repository.Audit) ([]model.Task, error)
}

// TaskDiagnostics 只提供已有的任务操作诊断，不在向导内实现执行规则。
type TaskDiagnostics interface {
	CanCancel(model.Task) []model.Problem
	CanRetry(model.Task) []model.Problem
}
