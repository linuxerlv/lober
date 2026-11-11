package port

import (
	"context"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type RenderRequest struct {
	Task   model.Task
	Kind   catalog.FileKind
	Slot   string
	Values map[catalog.SystemValue]string
}
type FrozenRenderer interface {
	// Render 的八项端口约定唯一连接 docs/contracts/machine-execution.md「冻结渲染端口」。
	Render(context.Context, RenderRequest) ([]byte, error)
}
type RenderFunc func(context.Context, RenderRequest) ([]byte, error)

// Render 的八项端口约定唯一连接 docs/contracts/machine-execution.md「冻结渲染端口」；只原样委托函数。
func (f RenderFunc) Render(ctx context.Context, r RenderRequest) ([]byte, error) { return f(ctx, r) }
