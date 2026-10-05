package pgstore

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Store 是 PostgreSQL 仓储，用 Open 构造，可以被多个 goroutine 同时使用。
type Store struct {
	db *gorm.DB
}

var _ store.Store = (*Store)(nil)

// models 是本层的全部行类型，AutoMigrate 按它们建表；它们的表与 schema.go 的 tables 一一对应。
var models = []any{
	&hostRow{}, &isoRow{},
	&hookRow{}, &hookVersionRow{}, &templateRow{}, &templateVersionRow{}, &templateHookRefRow{},
	&taskRow{}, &taskEventRow{},
	&auditRow{},
}

// batchSize 是一条语句最多写或查的行数：PostgreSQL 一条语句最多 65535 个参数，CSV 导入一次可能有几千台主机。
const batchSize = 1000

// Open 连上 dsn 指的库，对本层的全部行类型跑 AutoMigrate，再做表结构断言（见包说明）。断言不过就关闭连接，返回的
// 错误逐条写出差在哪里、该怎么改。连接池的大小用 database/sql 的默认值，由装配点按部署调整。
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 logger.Discard, // 调用日志是第 5 层的切面，这里不另记
		SkipDefaultTransaction: true,           // 写操作都显式开事务，单条语句不必再包一层
		DisableAutomaticPing:   true,           // 下面带着 ctx 自己 ping
	})
	if err != nil {
		return nil, fmt.Errorf("pgstore：连接数据库：%w", err)
	}
	s := &Store{db: db}
	if err := s.start(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// start 确认连得上，建表，再断言表结构。
func (s *Store) start(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("pgstore：取连接池：%w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("pgstore：连接数据库：%w", err)
	}
	if err := s.db.WithContext(ctx).AutoMigrate(models...); err != nil {
		return fmt.Errorf("pgstore：建表：%w", err)
	}
	ms, err := checkSchema(ctx, s.db, tables)
	if err != nil {
		return err
	}
	if len(ms) > 0 {
		return fmt.Errorf("pgstore：表结构与期望值有 %d 处不一致，拒绝启动：\n%s", len(ms), describeMismatches(ms))
	}
	return nil
}

// Close 见 store.Store：关闭连接池。
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// write 在一个事务里执行 fn：fn 返回 nil 才提交，否则回滚，一次写要么全做、要么全不做（spec §6.2 第 8 条）。返回时
// 事务已经结束（第 9 条）。
func (s *Store) write(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(fn)
}

// read 是不开事务的查询。
func (s *Store) read(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx)
}

// forUpdate 给读到的行加写锁，直到事务结束：比对 Rev 与写入之间，别的事务改不了这一行。
var forUpdate = clause.Locking{Strength: "UPDATE"}

// storable 报告这些串是不是都存得下（model.StorableText）。存不下的键不可能登记过，按它查找、过滤都当作没有，与内存
// 实现一致；不先挡下，PostgreSQL 会报编码错误。
func storable(ss ...string) bool {
	for _, s := range ss {
		if !model.StorableText(s) {
			return false
		}
	}
	return true
}

// page 从多取了一条的 rows 里取一页，条目用 conv 转成 model 值。多取一条才知道还有没有下一页：恰好取完时 Next 为空，
// 不多给一个空页。Items 不为 nil，第 5 层编成 JSON 是 []。
func page[R, T any](rows []R, limit int, conv func(R) (T, error), cursorOf func(R) string) (store.Page[T], error) {
	var p store.Page[T]
	if len(rows) > limit {
		rows = rows[:limit]
		p.Next = cursorOf(rows[limit-1])
	}
	p.Items = make([]T, 0, len(rows))
	for _, r := range rows {
		it, err := conv(r)
		if err != nil {
			return store.Page[T]{}, err
		}
		p.Items = append(p.Items, it)
	}
	return p, nil
}

// emptyPage 是没有条目的一页：过滤条件存不下时，什么都匹配不上。
func emptyPage[T any]() store.Page[T] {
	return store.Page[T]{Items: []T{}}
}

// 游标由 store.EncodeCursor 编出、store.DecodeCursor 解开，与内存实现用同一份：排序键是上一页最后一条的。解出的串都
// 存得下，拿去查询不会让 PostgreSQL 报编码错误。
