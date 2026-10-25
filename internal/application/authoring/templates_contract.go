package authoring

import (
	"context"
	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type TemplateListRequest struct {
	ListRequest
	Series catalog.Series
}
type DeriveRequest struct {
	From model.TemplateKey
	Name string
}
type TemplateRevision struct {
	Key model.TemplateKey
	Rev int64
}
type TemplateRejection struct {
	TemplateRevision
	Reason string
}
type TemplateView struct {
	Template model.Template
	Problems []model.Problem
}
type TemplatePage struct {
	Items []TemplateView
	Next  string
	Total *int64
}

// Templates 每次先检查操作者。草稿允许不完整，审批仍由原仓储在事务内判定。
type Templates interface {
	// Derive 遵守 docs/contracts/authoring-preview.md「32 条真实路由」第16项和「预览与写入边界」。
	// 写权限；先检查来源版本键，再从任意状态来源派生独立草稿，作者取可信身份，不审计。
	Derive(context.Context, ports.Actor, DeriveRequest) (TemplateView, error)
	// List 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；保留身份选择、分页与诊断顺序，游标默认100、页码默认20。
	List(context.Context, ports.Actor, TemplateListRequest) (TemplatePage, error)
	// Versions 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；先检查编号，返回版本升序的详情和诊断。
	Versions(context.Context, ports.Actor, string) ([]TemplateView, error)
	// Get 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；先检查版本键，再读单个版本并计算诊断。
	Get(context.Context, ports.Actor, model.TemplateKey) (TemplateView, error)
	// Create 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；作者取可信操作者，保存草稿后计算诊断，不增加审计。
	Create(context.Context, ports.Actor, model.Template) (TemplateView, error)
	// SaveDraft 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；版本键与Rev先于读取，非空名称不符拒绝；保留元数据，正文整份替换。
	SaveDraft(context.Context, ports.Actor, model.Template) (TemplateView, error)
	// NewVersion 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；先检查来源版本键，作者取可信操作者，已用版本号不重发。
	NewVersion(context.Context, ports.Actor, VersionSource) (TemplateView, error)
	// DeleteDraft 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；检查版本键和Rev后删除，无审计；引用与并发版本由仓储保护。
	DeleteDraft(context.Context, ports.Actor, TemplateRevision) error
	// Rename 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；改身份名称并同事务保存rename审计，成功不返回数据。
	Rename(context.Context, ports.Actor, RenameRequest) error
	// Submit 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；检查版本键和Rev，submit审计与状态一起保存，之后补读详情。
	Submit(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	// Approve 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；approver取可信操作者，approve审计与审批一起保存，之后补读。 审批和补读期间持选择写锁，仓储重查引用组件。
	Approve(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	// Reject 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；理由先于版本键和Rev检查，保留理由原文，同事务保存reject审计，之后补读。
	Reject(context.Context, ports.Actor, TemplateRejection) (TemplateView, error)
	// Deprecate 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；检查版本键和Rev，deprecate审计与废弃一起保存，之后补读。 废弃和补读期间持选择写锁。
	Deprecate(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	// Validate 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Templates对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；读取并返回完整诊断，不写记录、不增加审计或通知。
	Validate(context.Context, ports.Actor, model.TemplateKey) ([]model.Problem, error)
}
type TemplateDeps struct {
	Templates      port.Templates
	Hooks          port.HookReader
	Clock          ports.Clock
	SelectionGuard ports.SelectionGuard
}
