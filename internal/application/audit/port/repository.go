package port

import (
	"context"
	"github.com/lober/lober/internal/domain/repository"
)

// AuditLog 只能读取审计。业务写入仍由原仓储在同一事务保存审计。
type AuditLog interface {
	ListAudit(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error)
}
