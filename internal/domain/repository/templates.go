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
	// CreateTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 输入格式与文本检查唯一沿用check_authoring.go的CheckNewTemplate；身份、版本及引用同事务。
	CreateTemplate(ctx context.Context, t model.Template) (model.Template, error)
	// SaveTemplateDraft 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 输入唯一沿用CheckTemplateDraft；锁内草稿与元数据检查后还检查CheckSameDerivedFrom，内容与引用同事务替换。
	SaveTemplateDraft(ctx context.Context, t model.Template) (model.Template, error)
	// NewTemplateVersion 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error)
	// DeriveTemplate 从来源版本 from 派生一个新身份（C24），不审计：来源可以是任何状态。复制族、系、正文、
	// 参数、取值来源与装配表，建新身份的版本 1 草稿，名字为 name（规则同 CreateTemplate），作者为 author，
	// DerivedFrom 记下来源。
	// 先CheckVersionKey、CheckAuthor及NormalizeName，再读来源和新建；来源与新身份没有共同锁保护的快照承诺。
	// 新身份、版本1与引用同事务，返回独立草稿；不审计或通知，同名竞争由唯一约束处理。
	DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error)
	// SubmitTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 保存顺序继承SubmitHook；模板状态变化先锁身份再锁版本。
	SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	// ApproveTemplate 同 ApproveHook，判定的是模板：事务里锁住它引用的 hook 版本，对最新数据重跑判定
	// （ApproveTemplateProblems）。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
	// 检查顺序继承ApproveHook；模板状态变化先锁身份再锁版本，随后按键顺序锁引用组件，复查仍在同事务。
	ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a Audit) error
	// RejectTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 保存顺序继承RejectHook；模板状态变化先锁身份再锁版本。
	RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	// DeprecateTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 保存顺序继承DeprecateHook；模板状态变化先锁身份再锁版本。
	DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a Audit) error
	// RenameTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	RenameTemplate(ctx context.Context, id, name string, a Audit) error
	// DeleteTemplateDraft 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error
	// GetTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；只读，不写审计或通知，无提交复查。
	// 先检查版本键再查存在性，缺失ErrNotFound；成功返回独立版本。
	GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error)
	// ListTemplateVersions 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；只读，不写审计或通知，无提交复查。
	ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error)
	// ListTemplates 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；只读，不写审计或通知，无提交复查。
	// 分页、筛选、顺序及Total遵守types.go对应Query与Page；先检查分页再解本对象游标。
	ListTemplates(ctx context.Context, q TemplateQuery) (Page[model.Template], error)
	// EnsureBuiltinTemplate 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；对应Hooks方法的保存约定按本接口上方说明继承。
	// 输入唯一沿用CheckBuiltinTemplate；不能带DerivedFrom，身份、版本及引用同事务。
	EnsureBuiltinTemplate(ctx context.Context, t model.Template) error
}
