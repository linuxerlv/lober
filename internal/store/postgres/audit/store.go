// 本文件实现 contract.AuditStore：人面操作审计记录的落库与查询，对应
// 实现计划任务 8 的 audit 三分之一。语义抄自旧实现
// internal/store/sqlite/audit.go（已移除），接口签名抄自
// internal/core/contract/store.go 的 AuditStore。
package audit

import (
	"context"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/store/postgres/pgerr"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// Store 实现 contract.AuditStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。db 应已 SET search_path 指向目标
// schema（生产装配点与 pgtest.NewDB 都负责这一步，本包不重复处理）。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Models 返回本包需要 AutoMigrate 的行结构体，供 pgtest.NewDB(t,
// audit.Models()...) 与生产装配点复用同一份迁移清单。
func Models() []any {
	return []any{&row{}}
}

// AppendAudit 追加一条审计记录（append-only——没有更新或删除方法，
// 审计记录本身不可变，见 contract.AuditStore 的类型文档）。
func (s *Store) AppendAudit(ctx context.Context, record contract.AuditRecord) error {
	r := row{
		TS:         record.TS,
		Actor:      record.Actor,
		Action:     record.Action,
		Target:     record.Target,
		DetailJSON: pgtype.JSON[string]{Val: record.Detail},
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return pgerr.Translate(err, "AppendAudit")
	}
	return nil
}

// ListAudit 按过滤条件列出审计记录，按 (ts DESC, id DESC) 排序——先按
// 写入时刻倒序，同一时刻多条记录再按自增 id 倒序（后写入的排前面），
// 与旧实现 internal/store/sqlite/audit.go（已移除）的排序约定一致：时间戳精度是
// 秒，同一秒内并发写入并不罕见，光按 ts 排序在同秒记录之间顺序不确定，
// 有下游断言依赖"同一秒内后写入的排前面"这一具体顺序。
//
// filter.Target 精确匹配（不做子串）；SinceTS/UntilTS 非零时限定闭区间；
// 两者都非零且 SinceTS > UntilTS 时，闭区间 [SinceTS, UntilTS] 本身为空，
// WHERE 条件天然查不到任何行——不需要额外的分支判断去抢在查询之前返回
// 空结果，闭区间语义本身就产出了"空结果而不是报错"这个要求的行为。
//
// 查不到匹配记录时返回空切片而非 ErrNotFound：没有审计记录是正常状态
// （比如一个刚创建、从未被驳回过的 hook），调用方不需要为这条查询处理
// 错误分支（contract.AuditStore.ListAudit 文档明确要求）。
func (s *Store) ListAudit(ctx context.Context, filter contract.AuditFilter) ([]contract.AuditRecord, error) {
	query := s.db.WithContext(ctx).Model(&row{})

	if filter.Target != "" {
		query = query.Where("target = ?", filter.Target)
	}
	if filter.SinceTS != 0 {
		query = query.Where("ts >= ?", filter.SinceTS)
	}
	if filter.UntilTS != 0 {
		query = query.Where("ts <= ?", filter.UntilTS)
	}
	query = query.Order("ts DESC").Order("id DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	var rows []row
	if err := query.Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListAudit")
	}

	out := make([]contract.AuditRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.AuditRecord{
			TS:     r.TS,
			Actor:  r.Actor,
			Action: r.Action,
			Target: r.Target,
			Detail: r.DetailJSON.Val,
		})
	}
	return out, nil
}

// 编译期确认 *Store 实现 contract.AuditStore——签名有任何偏离都在
// 编译阶段暴露，不必等到运行测试才发现。
var _ contract.AuditStore = (*Store)(nil)
