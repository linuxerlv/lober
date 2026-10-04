package pgstore

import "github.com/lober/lober/internal/store"

// auditRow 是 audit_log 表的一行。审计只追加，不更新；也没有外键，对象删掉之后照样查得到它的审计。
type auditRow struct {
	ID         int64  `gorm:"primaryKey"` // bigserial：按写入的顺序递增，回滚的写入会占掉几个号
	Actor      string `gorm:"not null;index:idx_audit_log_actor"`
	Action     string `gorm:"not null"`
	Detail     string `gorm:"not null"`
	At         int64  `gorm:"not null;index:idx_audit_log_at;check:at > 0"`
	TargetKind string `gorm:"not null;index:idx_audit_log_target,priority:1"`
	TargetID   string `gorm:"not null;index:idx_audit_log_target,priority:2"`
}

func (auditRow) TableName() string { return "audit_log" }

func newAuditRow(a store.Audit, kind, id string) auditRow {
	return auditRow{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At, TargetKind: kind, TargetID: id}
}

// entry 把一行转回 store.AuditEntry。它不会失败，签名与别的行类型一致，好交给 page。
func (r auditRow) entry() (store.AuditEntry, error) {
	a := store.Audit{Actor: r.Actor, Action: r.Action, Detail: r.Detail, At: r.At}
	return store.AuditEntry{ID: r.ID, Audit: a, TargetKind: r.TargetKind, TargetID: r.TargetID}, nil
}
