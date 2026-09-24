// 本文件实现 contract.TxRunner：事务运行，拒绝嵌套。对应实现计划任务
// 10，依据设计文档 §4。
//
// 建任务时要同时写 tasks 与 task_events 两张表，两者必须原子——这是
// RunInTx 存在的理由。八个聚合包共享同一个事务句柄（GORM 的
// db.Transaction 回调参数 *gorm.DB），因此跨聚合写入天然在同一个事务里，
// 不需要额外的协调。
//
// 拒绝嵌套调用的理由与旧 SQLite 实现不同（sqlite/tx.go 的注释写的是
// 「单写者 + 连接池上限 1，嵌套会等待自己持有的连接，死锁」）。PG 下
// 连接池放开后不会死锁，但后果更隐蔽：嵌套调用会静默开出一个独立事务，
// 外层回滚时内层已经提交，原子性无声失效且不报任何错——这比死锁更危险，
// 死锁至少会让人立刻发现进程卡住，静默的原子性失效可能要等到数据不一致
// 被业务发现才暴露。判定依据从「rawDB == nil」换成显式的 inTx 标记位：
// GORM 的事务句柄仍是 *gorm.DB，与顶层连接同一个类型，无法像
// *sql.DB/*sql.Tx 那样靠类型区分，必须有一个运行期状态记录「当前这个
// Store 是不是已经在事务里」。
package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
)

// errNestedTx 是 RunInTx 拒绝嵌套调用时返回的错误。不导出为 contract
// 层的 sentinel error——嵌套调用是调用方的编程错误，不是像 ErrConflict
// 那样需要 errors.Is 判定后走不同分支的正常业务分支，调用方只需要知道
// 调用失败了。这与 internal/core/fake 里同名判断的设计保持一致（计划
// 任务 10「实现方式」明确要求参照该判断）。
var errNestedTx = errors.New("postgres: RunInTx 不支持嵌套调用（PG 下嵌套会静默开出独立事务，外层回滚时内层已提交，原子性无声失效）")

// RunInTx 在单个数据库事务里执行 fn。回调返回 nil 则提交，返回错误则
// 回滚并原样返回该错误。
//
// 拒绝嵌套：若 s.inTx 已经是 true，说明当前 Store 本身就是某次 RunInTx
// 内部传给回调的那个事务内 Store，回调却又调用了一次 RunInTx——直接
// 拒绝，不去尝试用 GORM 的 SAVEPOINT 语义兜底。GORM 对「在已有事务的
// *gorm.DB 上再调用 Transaction」确实会用 SAVEPOINT 而不是开新的独立
// 事务，但本文件头注释里「静默开出独立事务」的风险描述针对的是错用
// 一个全新顶层连接发起嵌套事务的场景（比如回调里意外拿到顶层 db 而不是
// 传入的事务句柄）——为了不依赖调用方总是正确传递事务句柄这个假设，
// 一律拒绝比区分「这次嵌套到底安全不安全」更简单也更不容易在未来被
// 意外破坏。
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context, store contract.Store) error) error {
	if s.inTx {
		return errNestedTx
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := newWithTxFlag(tx, true)
		return fn(ctx, txStore)
	})
}
