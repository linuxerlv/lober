package port

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

// Control 将原任务和可信审计交给既有执行能力；保存、版本复查和提交后通知仍归后端。
type Control interface {
	TaskDiagnostics
	// Cancel 按原状态和版本取消；业务与审计同事务保存，失败不发布。
	Cancel(context.Context, model.Task, ports.AuditIntent) error
	// Retry 按原状态和版本重试；锁内复查、旧凭据撤销和通知仍由既有后端处理。
	Retry(context.Context, model.Task, ports.AuditIntent) error
}
