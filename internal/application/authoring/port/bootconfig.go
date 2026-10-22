package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
)

type BootConfig interface {
	ReadESXiBootConfig(context.Context, model.SnapshotISO) (string, error)
}
type HostReader interface {
	GetHost(ctx context.Context, sn string) (model.Host, error)
}
type ISOReader interface {
	GetISO(ctx context.Context, id string) (model.ISO, error)
}
