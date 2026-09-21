// 本文件实现 contract.TxRunner：事务运行，拒绝嵌套。对应实现计划任务 10，
// 依据 SPEC §3.6。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/contract"
)

func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context, store contract.Store) error) error {
	panic("sqlite: RunInTx 尚未实现（计划任务 10）")
}
