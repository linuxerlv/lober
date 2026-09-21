// 本文件实现 contract.TxRunner：事务运行，拒绝嵌套。对应实现计划任务 10，
// 依据 SPEC §3.6。
//
// 建任务时要同时写任务与首条事件，两者必须原子（计划任务 10「为什么需要
// 这一步」）。事务内的 Store 是同一个类型但持有 *sql.Tx 而非 *sql.DB
// ——两者都满足 querier 接口，各方法针对 querier 编程，因此不必为事务
// 版本复制一遍所有方法（计划任务 10「实现方式」）。
package sqlite

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/core/contract"
)

// errNestedTx 是 RunInTx 拒绝嵌套调用时返回的错误。不导出为 contract 层
// 的 sentinel error——嵌套调用是调用方的编程错误，不是像 ErrConflict 那样
// 的正常业务分支，调用方不需要用 errors.Is 判定它、只需要知道调用失败了
// （与 internal/core/fake 的同名错误保持一致的设计判断）。
var errNestedTx = errors.New("sqlite: RunInTx 不支持嵌套调用")

// RunInTx 在单个数据库事务里执行 fn。回调返回 nil 则提交，返回错误则
// 回滚并原样返回该错误（计划任务 10「事务的实现流程」）。
//
// 拒绝嵌套调用（fn 内部再次调用 RunInTx）：SQLite 是单写者，且 M1 把
// 连接池上限设为 1。嵌套开事务会等待自己持有的连接释放——死锁，且表现
// 为整个进程卡住无响应（计划任务 10「为什么必须拒绝嵌套」）。判定依据是
// s.rawDB 是否为 nil：只有顶层（非事务内）的 Store 持有非 nil 的 rawDB
// （见 store.go），事务内的 Store（本方法下方构造的 txStore）不设置
// rawDB，因此在事务内调用 RunInTx 时 s.rawDB 为 nil，天然被这条判断拦住
// ——不需要额外的运行期标记位。
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context, store contract.Store) error) error {
	if s.rawDB == nil {
		return errNestedTx
	}

	tx, err := s.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后的 Rollback 调用本身会返回 sql.ErrTxDone，这是预期行为，不是需要处理的错误

	txStore := &Store{db: tx} // rawDB 留空（nil），标记「已在事务内」

	if err := fn(ctx, txStore); err != nil {
		return err
	}
	return tx.Commit()
}
