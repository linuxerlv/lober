// 本文件实现 contract.AuditLogger 的生产版本。对应 SPEC §9.4（人面操作
// 审计：建任务、hook 增删改、hook 激活审批、provider 配置变更等敏感操作
// 留痕）。
//
// # 本次契约变更前的状态（第一层缺口，已修补）
//
// 此前唯一的生产实现只把审计记录写进 log/slog（标准错误，交给 journald
// 收），从不写 internal/store/postgres/audit/row.go 的 GORM model tag
// 建好的 audit_log 表——那张表建了却从未被使用。本文件现在改为落库
// （委托给 contract.AuditStore，实现见
// internal/store/postgres/audit/store.go），slog 降级为兜底（见下方
// dbAuditLogger 的文档）。取舍理由与影响范围详见 docs/real-machine/05-审计持久化.md。
package main

import (
	"context"
	"log/slog"

	"github.com/lober/lober/internal/core/contract"
)

// dbAuditLogger 是 contract.AuditLogger 的生产实现：优先落库
// （AuditStore.AppendAudit），失败时降级写一条醒目的 slog 错误作为兜底，
// 但不让调用方感知到失败（Log 始终返回 nil）。
//
// 取舍理由（与 SPEC §6.3「遥测失败不杀装机」是同一类判断，这里的说法是
// 「审计失败不该让运维改不了配置」）：审计中间件
// （internal/console/audit.go）与 hook.go 的 handleRejectHook 都是在业务
// 操作已经成功之后才调用 Log——如果本方法把落库失败原样返回给调用方、
// 调用方又选择把这个错误当作整体失败处理，就会出现「hook 审批已经生效，
// 但因为审计表暂时写不进去，运维看到一个 500」这种荒谬的结果：审计是
// 事后追责的辅助能力，不应该有资格阻塞它本该记录的那个操作。
//
// 但失败不能被静默吞掉：本方法在降级路径写一条 Level=Error 的 slog 记录
// （而不是 Info，与正常审计记录的日志级别区分开），让运维在日常巡检
// 日志级别时能看到「审计系统本身出了问题」——这与把错误直接吞掉（等同于
// 什么都没发生）不同，是「不让审计故障升级成业务故障，但也不让审计故障
// 无声无息」之间的中间地带。
type dbAuditLogger struct {
	store contract.AuditStore
	clock contract.Clock
}

// newAuditLogger 构造一个落库的审计记录器，内部持有一个 AuditStore 用于
// AppendAudit，以及一个 Clock 用于给 AuditRecord.TS 赋值（AuditLogger.Log
// 的签名不接受时间参数，落盘时间由实现自行决定，见 contract.AuditRecord
// 的文档）。
func newAuditLogger(store contract.AuditStore, clock contract.Clock) contract.AuditLogger {
	return dbAuditLogger{store: store, clock: clock}
}

func (l dbAuditLogger) Log(ctx context.Context, actor, action, target, detail string) error {
	err := l.store.AppendAudit(ctx, contract.AuditRecord{
		TS:     l.clock.Now(),
		Actor:  actor,
		Action: action,
		Target: target,
		Detail: detail,
	})
	if err != nil {
		// 降级路径：落库失败不影响调用方（见类型文档的取舍理由），但必须
		// 在 slog 里留下醒目的失败记录——Level=Error 且显式标注
		// audit_persist_failed，与正常审计记录（若将来仍需要旁路一份到
		// slog）在日志级别与字段上都能被运维的日志系统单独筛出来。
		slog.Default().Error("audit_persist_failed",
			slog.String("actor", actor),
			slog.String("action", action),
			slog.String("target", target),
			slog.String("detail", detail),
			slog.String("error", err.Error()),
		)
		return nil
	}
	return nil
}

var _ contract.AuditLogger = dbAuditLogger{}
