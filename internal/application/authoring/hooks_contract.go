// Package authoring 编排组件和模板的编写操作，入口负责转换网页字段。
package authoring

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

var ErrRejectReason = errors.New("驳回理由不能为空或包含不可存文本")

type PageRequest struct {
	Page, Limit int
	Cursor      string
}
type ListRequest struct {
	PageRequest
	Prefix   string
	State    model.VersionState
	Approved bool
}
type VersionSource struct {
	ID   string
	From int
}
type RenameRequest struct{ ID, Name string }
type HookRevision struct {
	Key model.HookKey
	Rev int64
}
type HookRejection struct {
	HookRevision
	Reason string
}
type HookView struct {
	Hook     model.Hook
	Problems []model.Problem
}
type HookPage struct {
	Items []HookView
	Next  string
	Total *int64
}

// Hooks 每次先检查操作者。草稿允许不完整，审批仍由原仓储在事务内判定。
type Hooks interface {
	// List 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；保留身份选择、分页与诊断顺序，游标默认100、页码默认20。
	List(context.Context, ports.Actor, ListRequest) (HookPage, error)
	// Versions 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；先检查编号，返回版本升序的详情和诊断。
	Versions(context.Context, ports.Actor, string) ([]HookView, error)
	// Get 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 读权限；先检查版本键，再读单个版本并计算诊断。
	Get(context.Context, ports.Actor, model.HookKey) (HookView, error)
	// Create 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；作者取可信操作者，保存草稿后计算诊断，不增加审计。
	Create(context.Context, ports.Actor, model.Hook) (HookView, error)
	// SaveDraft 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；版本键与Rev先于读取，非空名称不符拒绝；保留元数据，正文整份替换。
	SaveDraft(context.Context, ports.Actor, model.Hook) (HookView, error)
	// NewVersion 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；先检查来源版本键，作者取可信操作者，已用版本号不重发。
	NewVersion(context.Context, ports.Actor, VersionSource) (HookView, error)
	// DeleteDraft 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；检查版本键和Rev后删除，无审计；引用与并发版本由仓储保护。
	DeleteDraft(context.Context, ports.Actor, HookRevision) error
	// Rename 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；改身份名称并同事务保存rename审计，成功不返回数据。
	Rename(context.Context, ports.Actor, RenameRequest) error
	// Submit 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；检查版本键和Rev，submit审计与状态一起保存，之后补读详情。
	Submit(context.Context, ports.Actor, HookRevision) (HookView, error)
	// Approve 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；approver取可信操作者，approve审计与审批一起保存，之后补读。
	Approve(context.Context, ports.Actor, HookRevision) (HookView, error)
	// Reject 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；理由先于版本键和Rev检查，保留理由原文，同事务保存reject审计，之后补读。
	Reject(context.Context, ports.Actor, HookRejection) (HookView, error)
	// Deprecate 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 审批权限；检查版本键和Rev，deprecate审计与废弃一起保存，之后补读。
	Deprecate(context.Context, ports.Actor, HookRevision) (HookView, error)
	// Validate 的应用约定唯一连接 docs/contracts/authoring-preview.md「32 条真实路由」Hooks对应行，
	// 并共用「共同约定」「问题与先后顺序」「页型与来源不能互相替代」「预览与写入边界」。
	// 写权限；读取并返回完整诊断，不写记录、不增加审计或通知。
	Validate(context.Context, ports.Actor, model.HookKey) ([]model.Problem, error)
}
type HookDeps struct {
	Hooks port.Hooks
	Clock ports.Clock
}
