package repository

import "context"

// AuditLog 是审计保存层的查询契约，由 application/audit 的只读能力消费。审计由要审计的写操作在同一个事务里写，没有单独写审计的方法。
type AuditLog interface {
	// ListAudit 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；只读，不写审计或通知，无提交复查。
	// 分页、筛选、顺序及Total遵守types.go对应Query与Page；先检查分页再解本对象游标。
	ListAudit(ctx context.Context, q AuditQuery) (Page[AuditEntry], error)
}
