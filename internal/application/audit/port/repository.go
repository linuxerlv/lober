package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/repository"
)

// AuditLog 只能读取审计。业务写入仍由原仓储在同一事务保存审计。
type AuditLog interface {
	// ListAudit 的八项保存约定唯一委托 repository.AuditLog.ListAudit
	// （internal/domain/repository/audit.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListAudit(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error)
}
