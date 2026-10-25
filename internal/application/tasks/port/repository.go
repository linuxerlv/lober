// Package port 定义向导实际需要的能力，不暴露其他模块的写入操作。
package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type Hosts interface {
	// ListHosts 的八项保存约定唯一委托 repository.Hosts.ListHosts
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListHosts(context.Context, repository.HostQuery) (repository.Page[model.Host], error)
	// GetHosts 的八项保存约定唯一委托 repository.Hosts.GetHosts
	// （internal/domain/repository/hosts.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHosts(context.Context, []string) (map[string]model.Host, error)
}
type ISOs interface {
	// GetISO 的八项保存约定唯一委托 repository.ISOs.GetISO
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetISO(context.Context, string) (model.ISO, error)
	// ListISOs 的八项保存约定唯一委托 repository.ISOs.ListISOs
	// （internal/domain/repository/isos.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error)
}
type Templates interface {
	// GetTemplate 的八项保存约定唯一委托 repository.Templates.GetTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetTemplate(context.Context, model.TemplateKey) (model.Template, error)
	// ListTemplates 的八项保存约定唯一委托 repository.Templates.ListTemplates
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTemplates(context.Context, repository.TemplateQuery) (repository.Page[model.Template], error)
	// ListTemplateCandidates 的八项约定唯一委托 repository.Templates.ListTemplateCandidates
	// （internal/domain/repository/templates.go）；继承单次事实一致边界，无额外事务、权限或重试。
	ListTemplateCandidates(context.Context, repository.TemplateQuery) (repository.Page[repository.TemplateCandidateFacts], error)
	// ListTemplateVersions 的八项保存约定唯一委托 repository.Templates.ListTemplateVersions
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTemplateVersions(context.Context, string) ([]model.Template, error)
}
type Hooks interface {
	// GetHooks 的八项保存约定唯一委托 repository.Hooks.GetHooks
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error)
}
type Tasks interface {
	// FindOpenTask 的八项保存约定唯一委托 repository.Tasks.FindOpenTask
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	FindOpenTask(context.Context, string) (model.Task, bool, error)
	// CreateCurrentTasks 在同一事务中确认当前模板并写任务及审计，成功才允许通知。
	// CreateCurrentTasks 的八项保存约定唯一委托 repository.CurrentTaskCreator.CreateCurrentTasks
	// （internal/domain/repository/current_tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	CreateCurrentTasks(context.Context, []model.Task, repository.Audit) ([]model.Task, error)
}

// TaskDiagnostics 只提供已有的任务操作诊断，不在向导内实现执行规则。
type TaskDiagnostics interface {
	// CanCancel 的八项约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」。
	// 只检查传入快照并保留问题次序；不代表真正提交时仍能取消。
	CanCancel(model.Task) []model.Problem
	// CanRetry 的八项约定唯一连接 docs/contracts/machine-execution.md「控制与诊断」。
	// 先检查已重启限制，再检查失败/取消状态；不代表真正提交时仍能重试。
	CanRetry(model.Task) []model.Problem
}
