// 本文件定义 Store 类型：SQLite 实现 contract.Store 与 contract.TxRunner
// 的持有者与构造函数。对应实现计划任务 1，依据 SPEC §2.6、§2.7、§3.6。
//
// Store 只做赋值，不做初始化——开库与迁移是调用方的事（走 Open，见
// migrate.go）；本包只在这个已经打开、已经迁移过的连接上实现查询与写入。
package sqlite

import (
	"context"
	"database/sql"

	"github.com/lober/lober/internal/core/contract"
)

// querier 是 database/sql 里 *sql.DB 与 *sql.Tx 共有的最小方法集合，本包
// 的全部查询/写入方法都针对这个接口编程而不是直接依赖 *sql.DB——事务内的
// Store（tx.go）持有 *sql.Tx 而不是 *sql.DB，两者都满足 querier，因此事务
// 版本不需要为每个方法复制一份实现（计划任务 10「实现方式」）。
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store 持有一个数据库连接（*sql.DB 或事务内的 *sql.Tx，见 tx.go），实现
// contract.Store 的全部七个窄接口与 contract.TxRunner。
//
// 字段命名为 db 而非 conn：db 是 querier 接口值，事务版本会把同一个字段名
// 指向 *sql.Tx，调用方看到的方法签名不因是否在事务内而改变。
type Store struct {
	db querier

	// rawDB 只在非事务版本上非 nil，供 RunInTx 开启新事务用——querier 接口
	// 本身没有 BeginTx 方法（*sql.Tx 没有这个方法，接口不能要求它），事务
	// 运行必须从最初的 *sql.DB 开始。
	rawDB *sql.DB
}

// New 用一个已经打开且已完成迁移的 *sql.DB 构造 Store。调用方负责调用
// Open（migrate.go）完成开库与迁移；本构造函数只做字段赋值。
func New(db *sql.DB) *Store {
	return &Store{db: db, rawDB: db}
}

// 编译期接口断言：*Store 必须同时满足组合 Store 接口与事务运行接口。
//
// 放在文件末尾而不是随手写在某个函数里——它的作用是「接口有新方法时这里
// 先报错」，位置固定才容易找到（计划任务 10「接口断言」）。
//
// 骨架阶段（计划任务 1）就解开而不是留到任务 10：Go 的接口满足是编译期
// 静态检查，没有「方法暂缺时断言先跳过、实现齐了再生效」的中间态——七个
// 窄接口的全部方法此刻已在各文件里以 panic 桩的形式声明齐全（签名与
// contract 完全一致，方法体 panic），所以断言现在就能通过编译；后续任务
// 逐个把桩换成真实实现时，这条断言全程保持「立即生效」，不需要额外解开。
var (
	_ contract.Store    = (*Store)(nil)
	_ contract.TxRunner = (*Store)(nil)
)
