package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// HookReader 只读取组件，不能装载内置版本或写入其他对象。
type HookReader interface {
	GetHook(ctx context.Context, k model.HookKey) (model.Hook, error)
	GetHooks(ctx context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error)
	ListHookVersions(ctx context.Context, id string) ([]model.Hook, error)
	ListHooks(ctx context.Context, q repository.HookQuery) (repository.Page[model.Hook], error)
}
type Hooks interface {
	HookReader
	CreateHook(ctx context.Context, h model.Hook) (model.Hook, error)
	SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error)
	NewHookVersion(ctx context.Context, id string, from int, author string) (model.Hook, error)
	SubmitHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	ApproveHook(ctx context.Context, k model.HookKey, rev int64, approver string, a repository.Audit) error
	RejectHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	DeprecateHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error
	RenameHook(ctx context.Context, id, name string, a repository.Audit) error
	DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error
}

// TemplateReader 只读取模板身份和版本。
type TemplateReader interface {
	GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error)
	ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error)
	ListTemplates(ctx context.Context, q repository.TemplateQuery) (repository.Page[model.Template], error)
}
type Templates interface {
	TemplateReader
	CreateTemplate(ctx context.Context, t model.Template) (model.Template, error)
	SaveTemplateDraft(ctx context.Context, t model.Template) (model.Template, error)
	NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error)
	// DeriveTemplate 从来源版本 from 派生一个新身份（C24），不审计：来源可以是任何状态。复制族、系、正文、
	// 参数、取值来源与装配表，建新身份的版本 1 草稿，名字为 name（规则同 CreateTemplate），作者为 author，
	// DerivedFrom 记下来源。
	DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error)
	SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	// ApproveTemplate 同 ApproveHook，判定的是模板：事务里锁住它引用的 hook 版本，对最新数据重跑判定
	// （ApproveTemplateProblems）。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
	ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a repository.Audit) error
	RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error
	RenameTemplate(ctx context.Context, id, name string, a repository.Audit) error
	DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error
}

// BuiltinRepository 只装载内置内容；每个对象的原子性与并发复查由仓储负责。
type BuiltinRepository interface {
	EnsureBuiltinHook(context.Context, model.Hook) error
	EnsureBuiltinTemplate(context.Context, model.Template) error
}
