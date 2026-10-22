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
	Derive(context.Context, ports.Actor, DeriveRequest) (TemplateView, error)
	List(context.Context, ports.Actor, TemplateListRequest) (TemplatePage, error)
	Versions(context.Context, ports.Actor, string) ([]TemplateView, error)
	Get(context.Context, ports.Actor, model.TemplateKey) (TemplateView, error)
	Create(context.Context, ports.Actor, model.Template) (TemplateView, error)
	SaveDraft(context.Context, ports.Actor, model.Template) (TemplateView, error)
	NewVersion(context.Context, ports.Actor, VersionSource) (TemplateView, error)
	DeleteDraft(context.Context, ports.Actor, TemplateRevision) error
	Rename(context.Context, ports.Actor, RenameRequest) error
	Submit(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	Approve(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	Reject(context.Context, ports.Actor, TemplateRejection) (TemplateView, error)
	Deprecate(context.Context, ports.Actor, TemplateRevision) (TemplateView, error)
	Validate(context.Context, ports.Actor, model.TemplateKey) ([]model.Problem, error)
}
type TemplateDeps struct {
	Templates      port.Templates
	Hooks          port.HookReader
	Clock          ports.Clock
	SelectionGuard ports.SelectionGuard
}
