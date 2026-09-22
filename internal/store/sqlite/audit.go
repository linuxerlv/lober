// 本文件实现 contract.AuditStore：人面操作审计记录的落库与查询。对应
// M2 契约变更（见 docs/real-machine/05-审计持久化.md），依据 SPEC §9.4、
// internal/core/contract/store.go 的 AuditStore 文档。
//
// audit_log 表在 M1 就已建好（schema/001_init.sql 第 174 行），本文件是
// 该表第一次被 Go 代码写入或查询——此前唯一的生产实现
// （cmd/lober/auditlog.go）绕开了 Store 抽象，只把审计记录写进 log/slog，
// 这张表建了却从未被使用（本次修补的两层缺口之一，另一层是「只写不查」，
// 详见 contract.AuditStore 的类型文档）。
package sqlite

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
)

// auditColumns 是 audit_log 表除自增主键 id 之外的全部列，按 CREATE 语句
// 声明顺序排列（与 hookColumns/eventColumns 同一种风格）。
const auditColumns = `ts, actor, action, target, detail_json`

// AppendAudit 追加一条审计记录（append-only，与 EventStore.AppendEvent
// 同理——审计记录不可变，本方法不提供任何修改已写入记录的路径）。
//
// record.Detail 是自由文本（不是结构化数据），JSON 编码后写进
// detail_json 列——这是复用既有 JSON 列类型的手段，不代表 detail 本身有
// 内部结构（见 contract.AuditRecord.Detail 的文档）。encodeJSON 对一个
// 字符串值编码得到的是合法的 JSON 字符串字面量（如 "reason=xxx" 编码为
// "\"reason=xxx\""），往返不丢字符——这正是「detail 原文完整往返」这条
// conformance 断言要锁住的行为。
func (s *Store) AppendAudit(ctx context.Context, record contract.AuditRecord) error {
	detailJSON, err := encodeJSON(record.Detail)
	if err != nil {
		return fmt.Errorf("sqlite: 编码 audit detail_json 失败: %w", err)
	}

	query := fmt.Sprintf(`INSERT INTO audit_log (%s) VALUES (?, ?, ?, ?, ?)`, auditColumns)
	_, err = s.db.ExecContext(ctx, query, record.TS, record.Actor, record.Action, record.Target, detailJSON)
	if err != nil {
		return translateConstraintErr(err, "AppendAudit")
	}
	return nil
}

// ListAudit 按过滤条件列出审计记录，按 (ts DESC, id DESC) 排序——先按
// 写入时刻倒序，同一时刻多条记录再按自增 id 倒序（后写入的排前面），
// 保证同秒内的多条记录也有确定、可重复的顺序（时间戳精度是秒，同一秒
// 内并发写入并不罕见：审计中间件与 handler 内部的额外一条 Log 调用，
// 见 hook.go handleRejectHook，可能落在同一秒）。
//
// 查不到匹配记录时返回空切片而非错误（contract.AuditStore.ListAudit
// 文档明确要求）。
func (s *Store) ListAudit(ctx context.Context, filter contract.AuditFilter) ([]contract.AuditRecord, error) {
	query := `SELECT ` + auditColumns + ` FROM audit_log WHERE 1 = 1`
	var args []any

	if filter.Target != "" {
		query += ` AND target = ?`
		args = append(args, filter.Target)
	}
	if filter.SinceTS != 0 {
		query += ` AND ts >= ?`
		args = append(args, filter.SinceTS)
	}
	if filter.UntilTS != 0 {
		query += ` AND ts <= ?`
		args = append(args, filter.UntilTS)
	}
	query += ` ORDER BY ts DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translateConstraintErr(err, "ListAudit")
	}
	defer rows.Close()

	out := make([]contract.AuditRecord, 0)
	for rows.Next() {
		var (
			rec        contract.AuditRecord
			detailJSON string
		)
		if err := rows.Scan(&rec.TS, &rec.Actor, &rec.Action, &rec.Target, &detailJSON); err != nil {
			return nil, translateConstraintErr(err, "ListAudit")
		}
		if err := decodeJSON(detailJSON, &rec.Detail); err != nil {
			return nil, fmt.Errorf("sqlite: 解码 audit detail_json 失败: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListAudit")
	}
	return out, nil
}
