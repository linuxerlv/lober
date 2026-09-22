// 本文件证伪 dbAuditLogger 的降级路径：审计写库失败时，Log 是否真的不让
// 调用方感知失败（不阻塞主流程），且是否真的在 slog 里留下了醒目的失败
// 记录（而不是静默吞掉）。对应任务描述「审计写库失败真的不会让人面操作
// 整体失败吗」这一条证伪要求——只读代码不足以确认这一行为，必须注入一个
// 总是报错的 AuditStore 并观察实际输出。
package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
)

// failingAuditStore 是一个 AppendAudit 总是报错的 contract.AuditStore 桩，
// 供本文件的失败注入测试使用。ListAudit 不会被 dbAuditLogger 调用，返回
// 零值即可。
type failingAuditStore struct {
	err error
}

func (f failingAuditStore) AppendAudit(ctx context.Context, record contract.AuditRecord) error {
	return f.err
}

func (f failingAuditStore) ListAudit(ctx context.Context, filter contract.AuditFilter) ([]contract.AuditRecord, error) {
	return nil, nil
}

var _ contract.AuditStore = failingAuditStore{}

// TestDbAuditLogger_StoreFailureDoesNotFailCaller 证伪「审计写库失败会不会
// 让调用方（人面操作）整体失败」：注入一个总是报错的 AuditStore，断言
// Log 仍然返回 nil——调用方（auditMiddleware、handleRejectHook）据此判断
// 审计故障绝不会升级为业务失败。
func TestDbAuditLogger_StoreFailureDoesNotFailCaller(t *testing.T) {
	store := failingAuditStore{err: errors.New("database is locked")}
	logger := newAuditLogger(store, fake.NewClock())

	err := logger.Log(context.Background(), "carol-author", "hook.reject", "hook-1", "reason=测试注入的写库失败")
	if err != nil {
		t.Fatalf("审计写库失败时 Log 仍应返回 nil（不让调用方感知失败），实际返回 %v", err)
	}
}

// TestDbAuditLogger_StoreFailureIsLoggedLoudly 证伪「审计写库失败是否被
// 静默吞掉」：断言失败时 slog 里出现一条 Level=ERROR 且携带
// audit_persist_failed 标记与原始错误文本的记录——静默吞掉等于审计形同
// 虚设，运维巡检日志级别时必须能看到「审计系统本身出了问题」。
func TestDbAuditLogger_StoreFailureIsLoggedLoudly(t *testing.T) {
	var buf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prevLogger)

	const dbErrText = "database is locked"
	store := failingAuditStore{err: errors.New(dbErrText)}
	logger := newAuditLogger(store, fake.NewClock())

	const (
		actor  = "dave-approver"
		action = "hook.reject"
		target = "hook-42"
	)
	if err := logger.Log(context.Background(), actor, action, target, "reason=xxx"); err != nil {
		t.Fatalf("Log 不应返回错误: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "audit_persist_failed") {
		t.Errorf("slog 输出里没有 audit_persist_failed 标记，实际输出：%q——\n"+
			"审计写库失败必须留下醒目的失败记录，不能静默吞掉", logged)
	}
	if !strings.Contains(logged, "level=ERROR") {
		t.Errorf("slog 输出里没有 level=ERROR，实际输出：%q——\n"+
			"审计故障必须用 Error 级别，与正常审计记录的日志级别区分开", logged)
	}
	if !strings.Contains(logged, dbErrText) {
		t.Errorf("slog 输出里没有原始错误文本 %q，实际输出：%q——\n"+
			"运维排障时唯一的线索就是这条日志，必须携带真实错误原因", dbErrText, logged)
	}
	for _, want := range []string{actor, action, target} {
		if !strings.Contains(logged, want) {
			t.Errorf("slog 输出里缺少字段 %q，实际输出：%q", want, logged)
		}
	}
}

// TestDbAuditLogger_SuccessDoesNotLogToSlog 确认成功路径不会误触发降级
// 日志——避免上一条测试只验证了失败路径存在这个标记，却没验证正常路径
// 保持沉默（否则 slog 会被大量无意义的「成功」记录淹没，运维无法靠
// audit_persist_failed 这个标记快速定位真正的故障）。
func TestDbAuditLogger_SuccessDoesNotLogToSlog(t *testing.T) {
	var buf bytes.Buffer
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prevLogger)

	store := failingAuditStore{err: nil} // 不报错，模拟落库成功
	logger := newAuditLogger(store, fake.NewClock())

	if err := logger.Log(context.Background(), "carol-author", "hook.reject", "hook-1", "reason=xxx"); err != nil {
		t.Fatalf("Log 不应返回错误: %v", err)
	}
	if logged := buf.String(); strings.Contains(logged, "audit_persist_failed") {
		t.Errorf("落库成功时不应写降级日志，实际输出：%q", logged)
	}
}
