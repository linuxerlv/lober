package memstore

import (
	"context"
	"math"

	"github.com/lober/lober/internal/store"
)

// ListAudit 见 store.AuditLog。新的在前；游标是上一页最后一条的 ID，后面的页只取比它小的，翻页中途新写的审计
// 不会插进来。
func (s *Store) ListAudit(_ context.Context, q store.AuditQuery) (store.Page[store.AuditEntry], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	before := int64(math.MaxInt64)
	if q.Cursor != "" {
		if err := decodeCursor(q.Cursor, "audit", &before); err != nil {
			return store.Page[store.AuditEntry]{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []store.AuditEntry
	for i := len(s.d.audits) - 1; i >= 0 && len(items) <= limit; i-- { // 多取一条，page 才知道有没有下一页
		if e := s.d.audits[i]; e.ID < before && auditMatches(q, e) {
			items = append(items, e)
		}
	}
	keep := func(e store.AuditEntry) store.AuditEntry { return e } // 条目只有值字段，复制一份就是深拷贝
	return page(items, limit, keep, func(e store.AuditEntry) string { return encodeCursor("audit", e.ID) }), nil
}

// auditMatches 报告 e 是否满足 q 的各个过滤条件：都是完全相等，From、To 是含两端的时间范围，零值表示不按它过滤。
func auditMatches(q store.AuditQuery, e store.AuditEntry) bool {
	return (q.TargetKind == "" || e.TargetKind == q.TargetKind) &&
		(q.TargetID == "" || e.TargetID == q.TargetID) &&
		(q.Actor == "" || e.Actor == q.Actor) &&
		(q.Action == "" || e.Action == q.Action) &&
		(q.From == 0 || e.At >= q.From) &&
		(q.To == 0 || e.At <= q.To)
}
