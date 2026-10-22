package pgstore

import (
	"context"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/store"
)

// addAudits 在事务 tx 里给每个对象写一条同样的审计，按 ids 的顺序，ID 也按这个顺序递增。调用方先写动作本身、后调用
// 它：审计写失败（包括测试的失败注入）时，整个事务连同动作一起回滚（spec §6.2 第 8 条）。
func addAudits(tx *gorm.DB, a store.Audit, kind string, ids ...string) error {
	rows := make([]auditRow, len(ids))
	for i, id := range ids {
		rows[i] = newAuditRow(a, kind, id)
	}
	return tx.CreateInBatches(&rows, batchSize).Error
}

// ListAudit 见 store.AuditLog。新的在前；游标是上一页最后一条的 ID，后面的页只取比它小的，翻页中途新写的审计不会
// 插进来。
func (s *Store) ListAudit(ctx context.Context, q store.AuditQuery) (store.Page[store.AuditEntry], error) {
	limit, err := store.CheckPageQuery(q.Cursor, q.Limit, q.Page)
	if err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	var before int64
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "audit", &before); err != nil {
			return store.Page[store.AuditEntry]{}, err
		}
	}
	if !storable(q.TargetKind, q.TargetID, q.Actor, q.Action) {
		return emptyPage[store.AuditEntry](q.Page), nil
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where("id < ?", before)
	}
	// 各过滤条件都是完全相等：库的排序规则是确定性的，text 的 = 按字节比，大小写不同就不相等
	for _, f := range []struct{ col, v string }{
		{"target_kind", q.TargetKind}, {"target_id", q.TargetID}, {"actor", q.Actor}, {"action", q.Action},
	} {
		if f.v != "" {
			db = db.Where(f.col+" = ?", f.v)
		}
	}
	if q.From != 0 {
		db = db.Where("at >= ?", q.From)
	}
	if q.To != 0 {
		db = db.Where("at <= ?", q.To)
	}
	db, total, err := numberedQuery[auditRow](db, q.Page, limit)
	if err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	var rows []auditRow
	if err := db.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	return page(rows, limit, auditRow.entry, func(r auditRow) string { return store.EncodeCursor("audit", r.ID) }, total)
}
