package repository

import (
	"context"

	"github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
)

// Discoveries 保存未登记主机的报到和报告。确认登记时，主机和审计记录一起保存。
type Discoveries interface {
	ObserveDiscovery(context.Context, discovery.Attempt) (discovery.Attempt, error)
	SaveDiscoveryReport(ctx context.Context, mac, attempt string, at int64, report discovery.Report) error
	ListDiscoveries(ctx context.Context, after string, limit int, at int64) ([]discovery.Record, error)

	GetDiscovery(ctx context.Context, mac string, at int64) (discovery.Record, error)
	RegisteredDiscoverySN(ctx context.Context, mac string) (string, error)
	RegisterDiscovery(ctx context.Context, mac, sn string, a Audit) (model.Host, error)
}
