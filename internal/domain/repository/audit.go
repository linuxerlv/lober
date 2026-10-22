package repository

import "context"

// AuditLog 是审计的查询。审计由要审计的写操作在同一个事务里写，没有单独写审计的方法。
type AuditLog interface {
	ListAudit(ctx context.Context, q AuditQuery) (Page[AuditEntry], error)
}
