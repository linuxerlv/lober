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

// HookReader 只读取组件，不能装载内置版本或写入其他对象。
type HookReader interface {
	// GetHook 的八项保存约定唯一委托 repository.Hooks.GetHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHook(ctx context.Context, k model.HookKey) (model.Hook, error)
	// GetHooks 的八项保存约定唯一委托 repository.Hooks.GetHooks
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetHooks(ctx context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error)
	// ListHookVersions 的八项保存约定唯一委托 repository.Hooks.ListHookVersions
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListHookVersions(ctx context.Context, id string) ([]model.Hook, error)
	// ListHooks 的八项保存约定唯一委托 repository.Hooks.ListHooks
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListHooks(ctx context.Context, q repository.HookQuery) (repository.Page[model.Hook], error)
}
type Hooks interface {
	HookReader
	// CreateHook 的八项保存约定唯一委托 repository.Hooks.CreateHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	CreateHook(ctx context.Context, h model.Hook) (model.Hook, error)
	// SaveHookDraft 的八项保存约定唯一委托 repository.Hooks.SaveHookDraft
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error)
	// NewHookVersion 的八项保存约定唯一委托 repository.Hooks.NewHookVersion
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	NewHookVersion(ctx context.Context, id string, from int, author string) (model.Hook, error)
	// SubmitHook 的八项保存约定唯一委托 repository.Hooks.SubmitHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	SubmitHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	// ApproveHook 的八项保存约定唯一委托 repository.Hooks.ApproveHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ApproveHook(ctx context.Context, k model.HookKey, rev int64, approver string, a repository.Audit) error
	// RejectHook 的八项保存约定唯一委托 repository.Hooks.RejectHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	RejectHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	// DeprecateHook 的八项保存约定唯一委托 repository.Hooks.DeprecateHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeprecateHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	// RenameHook 的八项保存约定唯一委托 repository.Hooks.RenameHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	RenameHook(ctx context.Context, id, name string, a repository.Audit) error
	// DeleteHookDraft 的八项保存约定唯一委托 repository.Hooks.DeleteHookDraft
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error
}

// TemplateReader 只读取模板身份和版本。
type TemplateReader interface {
	// GetTemplate 的八项保存约定唯一委托 repository.Templates.GetTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error)
	// ListTemplateVersions 的八项保存约定唯一委托 repository.Templates.ListTemplateVersions
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error)
	// ListTemplates 的八项保存约定唯一委托 repository.Templates.ListTemplates
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTemplates(ctx context.Context, q repository.TemplateQuery) (repository.Page[model.Template], error)
}
type Templates interface {
	TemplateReader
	// CreateTemplate 的八项保存约定唯一委托 repository.Templates.CreateTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	CreateTemplate(ctx context.Context, t model.Template) (model.Template, error)
	// SaveTemplateDraft 的八项保存约定唯一委托 repository.Templates.SaveTemplateDraft
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	SaveTemplateDraft(ctx context.Context, t model.Template) (model.Template, error)
	// NewTemplateVersion 的八项保存约定唯一委托 repository.Templates.NewTemplateVersion
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error)
	// DeriveTemplate 从来源版本 from 派生一个新身份（C24），不审计：来源可以是任何状态。复制族、系、正文、
	// 参数、取值来源与装配表，建新身份的版本 1 草稿，名字为 name（规则同 CreateTemplate），作者为 author，
	// DerivedFrom 记下来源。
	// DeriveTemplate 的八项保存约定唯一委托 repository.Templates.DeriveTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error)
	// SubmitTemplate 的八项保存约定唯一委托 repository.Templates.SubmitTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	// ApproveTemplate 同 ApproveHook，判定的是模板：事务里锁住它引用的 hook 版本，对最新数据重跑判定
	// （ApproveTemplateProblems）。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
	// ApproveTemplate 的八项保存约定唯一委托 repository.Templates.ApproveTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a repository.Audit) error
	// RejectTemplate 的八项保存约定唯一委托 repository.Templates.RejectTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	// DeprecateTemplate 的八项保存约定唯一委托 repository.Templates.DeprecateTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	// RenameTemplate 的八项保存约定唯一委托 repository.Templates.RenameTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	RenameTemplate(ctx context.Context, id, name string, a repository.Audit) error
	// DeleteTemplateDraft 的八项保存约定唯一委托 repository.Templates.DeleteTemplateDraft
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error
}

// BuiltinRepository 只装载内置内容；每个对象的原子性与并发复查由仓储负责。
type BuiltinRepository interface {
	// EnsureBuiltinHook 的八项保存约定唯一委托 repository.Hooks.EnsureBuiltinHook
	// （internal/domain/repository/hooks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	EnsureBuiltinHook(context.Context, model.Hook) error
	// EnsureBuiltinTemplate 的八项保存约定唯一委托 repository.Templates.EnsureBuiltinTemplate
	// （internal/domain/repository/templates.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	EnsureBuiltinTemplate(context.Context, model.Template) error
}
