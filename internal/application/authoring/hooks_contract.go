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
	List(context.Context, ports.Actor, ListRequest) (HookPage, error)
	Versions(context.Context, ports.Actor, string) ([]HookView, error)
	Get(context.Context, ports.Actor, model.HookKey) (HookView, error)
	Create(context.Context, ports.Actor, model.Hook) (HookView, error)
	SaveDraft(context.Context, ports.Actor, model.Hook) (HookView, error)
	NewVersion(context.Context, ports.Actor, VersionSource) (HookView, error)
	DeleteDraft(context.Context, ports.Actor, HookRevision) error
	Rename(context.Context, ports.Actor, RenameRequest) error
	Submit(context.Context, ports.Actor, HookRevision) (HookView, error)
	Approve(context.Context, ports.Actor, HookRevision) (HookView, error)
	Reject(context.Context, ports.Actor, HookRejection) (HookView, error)
	Deprecate(context.Context, ports.Actor, HookRevision) (HookView, error)
	Validate(context.Context, ports.Actor, model.HookKey) ([]model.Problem, error)
}
type HookDeps struct {
	Hooks port.Hooks
	Clock ports.Clock
}
