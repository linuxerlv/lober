// 本文件证伪「审计记录真的落到 audit_log 表了吗」——不满足于「Go 测试
// 通过」，而是绕开 AuditStore 接口本身，用另一条独立的原始 SQL 查询直接
// 读 audit_log 表，确认行真的在库里。对应任务描述明确要求的证伪方式：
// 「别只看 Go 测试通过——跑起来写一条，然后直接查那张表确认行在里面」。
package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// TestAppendAudit_RowIsActuallyInAuditLogTable 是本文件的核心证伪：调用
// AppendAudit（AuditStore 接口方法）写一条记录，然后完全不经过 ListAudit
// 或任何 contract 接口，直接对同一个 *sql.DB 发一条原始 SELECT，确认
// audit_log 表里真的多了一行、且列值与写入时一致。
//
// 这条测试要抓的具体风险：AppendAudit 内部的 INSERT 语句、事务提交、列名
// 拼写等任何一处出错，都可能导致「AppendAudit 返回 nil error 但实际没有
// 写进去」或「写进了错误的列」——只测 ListAudit 往返查不出这类问题，因为
// ListAudit 与 AppendAudit 共享同一段 SQL 拼写逻辑（auditColumns 常量），
// 两者一起出错时会互相掩盖。
func TestAppendAudit_RowIsActuallyInAuditLogTable(t *testing.T) {
	db := openTestDB(t)
	store := New(db)
	ctx := context.Background()

	const (
		ts     = int64(1735689600) // 2025-01-01T00:00:00Z，任意固定值
		actor  = "carol-author"
		action = "hook.reject"
		target = "hook-bypass-test"
		detail = "reason=绕开接口直接查表验证"
	)

	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: ts, Actor: actor, Action: action, Target: target, Detail: detail,
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	// 直接对同一个 *sql.DB 发原始查询，完全不经过 sqlite.Store 的任何方法
	// ——这是「绕开审计接口、直接读表」的字面实现。
	var (
		gotTS         int64
		gotActor      string
		gotAction     string
		gotTarget     string
		gotDetailJSON string
	)
	row := db.QueryRowContext(ctx,
		`SELECT ts, actor, action, target, detail_json FROM audit_log WHERE target = ?`, target)
	if err := row.Scan(&gotTS, &gotActor, &gotAction, &gotTarget, &gotDetailJSON); err != nil {
		t.Fatalf("直接查 audit_log 表未找到刚写入的行（err=%v）——"+
			"说明 AppendAudit 返回了 nil error 但实际没有真的落库", err)
	}

	if gotTS != ts {
		t.Errorf("表里的 ts = %d, want %d", gotTS, ts)
	}
	if gotActor != actor {
		t.Errorf("表里的 actor = %q, want %q", gotActor, actor)
	}
	if gotAction != action {
		t.Errorf("表里的 action = %q, want %q", gotAction, action)
	}
	if gotTarget != target {
		t.Errorf("表里的 target = %q, want %q", gotTarget, target)
	}
	// detail_json 是 JSON 编码后的字符串（见 audit.go 的文档），原文必须
	// 以某种可辨认的形式出现在其中——不要求精确匹配 JSON 编码格式，只要求
	// 原文内容没有丢失。
	if !strings.Contains(gotDetailJSON, "绕开接口直接查表验证") {
		t.Errorf("表里的 detail_json = %q，没有包含写入的原文", gotDetailJSON)
	}

	// 额外确认：表里恰好一行匹配该 target（没有重复写入、也没有漏写）。
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE target = ?`, target).Scan(&count); err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if count != 1 {
		t.Errorf("audit_log 表里 target=%q 的行数 = %d, want 1", target, count)
	}
}

// TestAppendAudit_DetailWithSpecialCharactersRoundTrips 证伪「detail 原文
// 完整往返」在直接查表层面同样成立——尤其是含中文、引号、换行的理由文本，
// 这是 JSON 列的典型故障点（编码/解码任一环节转义错误都可能在这类内容上
// 暴露，而纯 ASCII 内容不会触发）。本测试同样绕开 ListAudit，直接读表后
// 用 decodeJSON 还原，而不是依赖 ListAudit 内部的同一段解码逻辑——避免
// 编码与解码的错误互相掩盖。
func TestAppendAudit_DetailWithSpecialCharactersRoundTrips(t *testing.T) {
	db := openTestDB(t)
	store := New(db)
	ctx := context.Background()

	const target = "hook-special-chars-test"
	detail := "reason=接口调用没有做超时保护，容易把主机名单请求卡死\n" +
		`第二行含双引号 "quoted" 与反斜杠 \path\to\file`

	if err := store.AppendAudit(ctx, contract.AuditRecord{
		TS: 1735689600, Actor: "dave-approver", Action: "hook.reject", Target: target, Detail: detail,
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}

	var detailJSON string
	row := db.QueryRowContext(ctx, `SELECT detail_json FROM audit_log WHERE target = ?`, target)
	if err := row.Scan(&detailJSON); err != nil {
		t.Fatalf("直接查表未找到刚写入的行: %v", err)
	}

	var decoded string
	if err := decodeJSON(detailJSON, &decoded); err != nil {
		t.Fatalf("decodeJSON 失败: %v，原始 detail_json=%q", err, detailJSON)
	}
	if decoded != detail {
		t.Errorf("detail 往返后不一致：\n写入=%q\n读回=%q", detail, decoded)
	}
}
