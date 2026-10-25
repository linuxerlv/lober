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
	// Cancel 的完整约定唯一委托 execution.Service.Cancel（docs/contracts/machine-execution.md「控制与诊断」）。
	// 本口只把可信AuditIntent转换为repository.Audit，不增加权限或错误转换。
	Cancel(context.Context, model.Task, ports.AuditIntent) error
	// Retry 按原状态和版本重试；锁内复查、旧凭据撤销和通知仍由既有后端处理。
	// Retry 的完整约定唯一委托 execution.Service.Retry（docs/contracts/machine-execution.md「控制与诊断」）。
	// 本口不自动刷新版本或重试，不改变旧凭据撤销及通知位置。
	Retry(context.Context, model.Task, ports.AuditIntent) error
}
