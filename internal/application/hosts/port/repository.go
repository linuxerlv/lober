package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Hosts 只声明主机应用本阶段实际使用的方法。
type Hosts interface {
	GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error)
	ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error)
	CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error)
	UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error)
	DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error
	GetHost(ctx context.Context, sn string) (model.Host, error)
	ListHosts(ctx context.Context, q repository.HostQuery) (repository.Page[model.Host], error)
}
