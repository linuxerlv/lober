package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// ISOs 只提供镜像模块使用的六项记录能力，写入和审计仍由仓储共同提交。
type ISOs interface {
	CreateISO(context.Context, model.ISO, repository.Audit) (model.ISO, error)
	UpdateISO(context.Context, model.ISO) (model.ISO, error)
	DeprecateISO(context.Context, string, int64, repository.Audit) error
	DeleteISO(context.Context, string, int64, repository.Audit) error
	GetISO(context.Context, string) (model.ISO, error)
	ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error)
}

// Sources 不包含当前流程未使用的单条删除能力。
type Sources interface {
	SaveSource(context.Context, model.Source, repository.Audit) (model.Source, error)
	SaveSourceAndAcquire(context.Context, model.Source, int64, repository.Audit) (model.Source, error)
	GetSource(context.Context, string, string) (model.Source, bool, error)
	SourcesOf(context.Context, string) ([]model.Source, error)
	ListUnfinishedSources(context.Context) ([]model.Source, error)
	SaveRange(context.Context, string, string, int64, int64) error
	RangesOf(context.Context, string, string) ([]model.Range, error)
}
