package repository

import "context"

// AuditLog 是审计保存层的查询契约，由 application/audit 的只读能力消费。审计由要审计的写操作在同一个事务里写，没有单独写审计的方法。
type AuditLog interface {
	ListAudit(ctx context.Context, q AuditQuery) (Page[AuditEntry], error)
}
