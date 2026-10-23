package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

// Templates 是模板的身份与版本（C12–C17、C22–C26）。各方法与 Hooks 一一对应（CreateTemplate 对 CreateHook，依此
// 类推），没写注释的规则相同；另有四处不同：可以派生（DeriveTemplate）；审批时连带它引用的 hook 版本一起判定；ListTemplates 可以按系过滤；
// 保存模板的任何一个版本时，仓储同时记下这个版本引用了哪些 hook 版本（HookRefs），DeleteHookDraft 靠这份记录查
// hook 草稿有没有被引用。审批模板与建任务不读这份记录，对锁住的模板版本用 HookRefs 现算要加读锁的 hook 版本。
type Templates interface {
	CreateTemplate(ctx context.Context, t model.Template) (model.Template, error)
	SaveTemplateDraft(ctx context.Context, t model.Template) (model.Template, error)
	NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error)
	// DeriveTemplate 从来源版本 from 派生一个新身份（C24），不审计：来源可以是任何状态。复制族、系、正文、
	// 参数、取值来源与装配表，建新身份的版本 1 草稿，名字为 name（规则同 CreateTemplate），作者为 author，
	// DerivedFrom 记下来源。
	DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error)
	SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	// ApproveTemplate 同 ApproveHook，判定的是模板：事务里锁住它引用的 hook 版本，对最新数据重跑判定
	// （ApproveTemplateProblems）。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
	ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a Audit) error
	RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	RenameTemplate(ctx context.Context, id, name string, a Audit) error
	DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error
	GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error)
	ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error)
	ListTemplates(ctx context.Context, q TemplateQuery) (Page[model.Template], error)
	EnsureBuiltinTemplate(ctx context.Context, t model.Template) error
}
