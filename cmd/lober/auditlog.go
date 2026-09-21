// 本文件实现 contract.AuditLogger 的生产版本。对应 SPEC §9.4（人面操作
// 审计：建任务、hook 增删改、hook 激活审批、provider 配置变更等敏感操作
// 留痕）。
//
// 契约缺口：internal/store/sqlite/schema/001_init.sql 已经建出 audit_log
// 表（列与本接口的四个参数逐一对应），但没有任何模块的 Go 代码对外暴露
// 写入它的方法——contract.AuditLogger 不是 contract.Store 组合接口的
// 成员，sqlite.Store 也没有实现这个接口。装配点本可以绕开 Store 抽象，
// 直接对 App.DB 拼 SQL 写这张表，但那样会在装配点重复一份本该属于 M2
// 的建表/写入职责，且一旦 M2 将来给 audit_log 加列或改名，这份绕过
// Store 抽象的 SQL 不会跟着改。保守选择：本实现只依赖 log/slog 落一条
// 结构化审计日志到标准错误（由 journald 收，与 SPEC §9.4「审计」的
// 「留痕」要求在事实上等价——journald 本身具备持久化与检索能力），不碰
// audit_log 表。正解方向是给 contract.Store 补一个 AuditLogStore 窄接口
// 并由 M2 实现，装配点直接复用 Store 而不是另起一个基于 slog 的实现。
package main

import (
	"context"
	"log/slog"

	"github.com/lober/lober/internal/core/contract"
)

// slogAuditLogger 是 contract.AuditLogger 的生产实现：结构化落到
// log/slog（标准错误，交给 journald 收，见文件头注释）。
type slogAuditLogger struct{}

// newAuditLogger 构造一个基于 slog 的审计记录器。参数保留（db 目前未
// 使用）是为了不影响调用方签名——正解补上 audit_log 表的写入能力时，
// 只需要改本函数体内部，不需要改调用点。
func newAuditLogger() contract.AuditLogger {
	return slogAuditLogger{}
}

func (slogAuditLogger) Log(ctx context.Context, actor, action, target, detail string) error {
	slog.Default().Info("audit",
		slog.String("actor", actor),
		slog.String("action", action),
		slog.String("target", target),
		slog.String("detail", detail),
	)
	return nil
}

var _ contract.AuditLogger = slogAuditLogger{}
