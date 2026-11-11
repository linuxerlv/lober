package repository

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
)

// InventoryChange 是锁内判定的结果。Attrs 为 nil 时只读，非空时与审计一起保存。
type InventoryChange struct {
	Attrs map[string]string
	Audit Audit
}

// HostInventory 锁住主机，读取当前属性和忙闲状态，再由调用方决定是否写入。
// 回调不得重入仓储。建任务与重试遵循相同的主机锁顺序。
type HostInventory interface {
	UpdateInventory(context.Context, string, func(model.Host, bool) (InventoryChange, error)) (model.Host, error)
}
