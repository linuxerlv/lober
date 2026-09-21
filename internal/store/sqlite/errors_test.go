// 本文件测试错误翻译：三种约束冲突情形各自翻译正确。对应实现计划任务 2，
// 依据 SPEC §2.5、§3.6。
//
// 用真实的约束冲突触发翻译函数，不手工构造 *sqlite.Error 值——计划任务 2
// 明确要求「用真实的约束冲突触发，不要手工构造错误值」，因为手工构造的
// 值无法验证「真实驱动在这种场景下确实会产出这个错误码/消息格式」这件事
// 本身。
package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// openTranslateTestDB 打开一个每个测试独立的文件数据库并应用迁移，供本
// 文件触发真实约束冲突用。用文件库而非 :memory:，与 store_test.go 的
// newConformanceStore 保持同样的理由（内存库在连接池上限 1 下行为不同）。
func openTranslateTestDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "translate.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

// insertMinimalTask 插入一行满足 tasks 表 NOT NULL 约束的最小任务记录，
// 供本文件的测试复用（与 migrate_test.go 的 insertMinimalTaskFunc 同理，
// 但本文件测的是 errors.go 而非 schema 本身，故不复用那个未导出函数——
// 它定义在同包的另一个 _test.go 文件里，理论上可以直接调用，这里为了让
// 每个测试文件的夹具自足、不因为另一个测试文件重命名或删除辅助函数而
// 意外遭殃，各自维护一份最小夹具）。
func insertMinimalTask(t *testing.T, s *Store, id, hostSN, state string) {
	t.Helper()
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO tasks (
		id, created_at, host_sn, template_id, template_version,
		artifact_set_id, state, state_updated_at, current_phase, phase_entered_at
	) VALUES (?, 0, ?, 'tpl-1', 1, 'art-1', ?, 0, 'pending', 0)`, id, hostSN, state)
	if err != nil {
		t.Fatalf("插入夹具任务失败: %v", err)
	}
}

// TestTranslate_ActiveTaskExists 验证撞 idx_tasks_sn_active 时翻译成
// ErrActiveTaskExists。
func TestTranslate_ActiveTaskExists(t *testing.T) {
	s := openTranslateTestDB(t)
	insertMinimalTask(t, s, "task-1", "SN-dup", "pending")

	_, rawErr := s.db.ExecContext(context.Background(), `INSERT INTO tasks (
		id, created_at, host_sn, template_id, template_version,
		artifact_set_id, state, state_updated_at, current_phase, phase_entered_at
	) VALUES ('task-2', 0, 'SN-dup', 'tpl-1', 1, 'art-1', 'booting', 0, 'pending', 0)`)
	if rawErr == nil {
		t.Fatalf("插入同 SN 的第二个非终态任务未报错——夹具本身没有触发预期的约束冲突，" +
			"翻译函数根本没有输入可测")
	}

	got := translateConstraintErr(rawErr, "CreateTask")
	if !errors.Is(got, contract.ErrActiveTaskExists) {
		t.Errorf("翻译撞 idx_tasks_sn_active 的错误 = %v, want errors.Is(got, ErrActiveTaskExists) —— "+
			"计划任务 2 要求按索引名（本实现按列名，见 errors.go 顶部长注释）匹配这类冲突，"+
			"若匹配失效，重复信号会被当成内部错误返回 500", got)
	}
}

// TestTranslate_DuplicateSeq 验证撞 idx_task_events_task_seq 时翻译成
// ErrDuplicateSeq。
func TestTranslate_DuplicateSeq(t *testing.T) {
	s := openTranslateTestDB(t)
	insertMinimalTask(t, s, "task-1", "SN-events", "pending")

	insertEvent := func() error {
		_, err := s.db.ExecContext(context.Background(), `INSERT INTO task_events
			(task_id, seq, ts, phase, level, source) VALUES ('task-1', 5, 0, 'pending', 'info', 'signal')`)
		return err
	}
	if err := insertEvent(); err != nil {
		t.Fatalf("插入第一条事件失败: %v", err)
	}
	rawErr := insertEvent()
	if rawErr == nil {
		t.Fatalf("重复插入同 (task_id, seq) 未报错——夹具本身没有触发预期的约束冲突")
	}

	got := translateConstraintErr(rawErr, "AppendEvent")
	if !errors.Is(got, contract.ErrDuplicateSeq) {
		t.Errorf("翻译撞 idx_task_events_task_seq 的错误 = %v, want errors.Is(got, ErrDuplicateSeq) —— "+
			"机器侧会重试三次，若这里匹配失效，重复投递会被当成异常而不是正常去重", got)
	}
}

// TestTranslate_NotFound 验证 sql.ErrNoRows 翻译成 ErrNotFound。
func TestTranslate_NotFound(t *testing.T) {
	s := openTranslateTestDB(t)

	row := s.db.QueryRowContext(context.Background(), `SELECT id FROM tasks WHERE id = 'does-not-exist'`)
	var id string
	rawErr := row.Scan(&id)
	if rawErr == nil {
		t.Fatalf("查询不存在的任务未报错")
	}

	got := translateConstraintErr(rawErr, "GetTask")
	if !errors.Is(got, contract.ErrNotFound) {
		t.Errorf("翻译 sql.ErrNoRows = %v, want errors.Is(got, ErrNotFound)", got)
	}
}

// TestTranslate_UnknownErrorIsWrappedNotSwallowed 验证无法识别的错误被
// 原样包装返回而不是吞掉——计划任务 2 设计要点明确要求，未知的数据库
// 错误应该冒泡成 500 并进服务端日志。用一条语法错误的 SQL 触发一个不属于
// 上面三种已知情形的错误。
func TestTranslate_UnknownErrorIsWrappedNotSwallowed(t *testing.T) {
	s := openTranslateTestDB(t)

	_, rawErr := s.db.ExecContext(context.Background(), `THIS IS NOT VALID SQL`)
	if rawErr == nil {
		t.Fatalf("语法错误的 SQL 未报错")
	}

	got := translateConstraintErr(rawErr, "探针")
	if errors.Is(got, contract.ErrNotFound) || errors.Is(got, contract.ErrConflict) ||
		errors.Is(got, contract.ErrDuplicateSeq) || errors.Is(got, contract.ErrActiveTaskExists) ||
		errors.Is(got, contract.ErrStillReferenced) {
		t.Errorf("无法识别的错误被误判成某个 sentinel error: %v, want 原样包装返回", got)
	}
	if !errors.Is(got, rawErr) {
		t.Errorf("无法识别的错误未被 %%w 包装原始错误: got=%v, raw=%v —— "+
			"未知错误必须能通过 errors.Is 追溯回原始驱动错误，否则调用方日志里只有"+
			"一句「未知错误」，排查时看不到底层原因", got, rawErr)
	}
}
