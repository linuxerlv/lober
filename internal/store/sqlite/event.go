// 本文件实现 contract.EventStore：事件追加与去重、按序号查询。对应实现
// 计划任务 6，依据 SPEC §2.5、§3.6、§12.8。
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// eventColumns 是 task_events 表除自增主键 id 之外的全部列，按 CREATE
// 语句声明顺序排列。id 单独处理——AppendEvent 需要区分「INSERT 时不指定
// id」与「SELECT 时要读出 id 用于排序」，混进同一份列表反而不直观。
const eventColumns = `task_id, seq, ts, phase, level, message, rc, hook_digest, source`

// scanTaskEvent 从一行（含 id 列）扫描出 model.TaskEvent。ListEvents 需要
// id 决定排序（按插入顺序而非 seq——负序号的非信号事件也要出现在正确的
// 时间位置，计划任务 6「实现流程」第 3 步），AppendEvent 的返回值不需要
// id，故用两个独立的扫描路径而不是让 TaskEvent 额外带一个不属于 contract
// 的 id 字段。
type eventRowScanner interface {
	Scan(dest ...any) error
}

func scanTaskEventWithID(row eventRowScanner) (model.TaskEvent, int64, error) {
	var e model.TaskEvent
	var id int64
	err := row.Scan(&id, &e.TaskID, &e.Seq, &e.TS, &e.Phase, &e.Level, &e.Message, &e.RC, &e.HookDigest, &e.Source)
	if err != nil {
		return model.TaskEvent{}, 0, err
	}
	return e, id, nil
}

// AppendEvent 追加一条事件。
//
// event.Seq 为正数时代表信号来源事件：直接 INSERT，撞
// idx_task_events_task_seq 唯一索引时翻译成 contract.ErrDuplicateSeq
// （计划任务 6「实现流程」第 1 步）——这不是错误路径，是机器侧重试造成的
// 正常重复投递，handler 应把它当作「已记录过，继续走成功路径」处理。
//
// event.Seq 非正数（零或负）表示非信号来源的事件：本方法在一个事务里取
// 该任务当前最小 seq 再减一，分配一个不参与去重、递减的负数序号——递减
// 而非递增：信号序号天然是正数且递增，两个序号空间永不相交，且都保持
// 「新事件序号在数轴上更远离 0」这一单调性质（计划任务 6「设计要点」）。
// 用显式事务保证「取值」与「写入」之间不被并发的另一次非信号事件写入
// 插队，否则两次并发调用会算出同一个候选负数序号而互相冲突。
func (s *Store) AppendEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	if event.Seq > 0 {
		query := fmt.Sprintf(`INSERT INTO task_events (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, eventColumns)
		_, err := s.db.ExecContext(ctx, query,
			event.TaskID, event.Seq, event.TS, event.Phase, event.Level, event.Message, event.RC, event.HookDigest, event.Source)
		if err != nil {
			return model.TaskEvent{}, translateConstraintErr(err, "AppendEvent")
		}
		return event, nil
	}

	return s.appendNonSignalEvent(ctx, event)
}

// appendNonSignalEvent 处理 event.Seq 非正数的分支：在一个事务里读取当前
// 最小 seq 再减一，写入分配后的负数序号。调用方传入的 Seq 值在此分支被
// 忽略（contract.EventStore.AppendEvent 文档明确要求）。
//
// 若 s 本身已经在事务内（rawDB 为 nil，见 tx.go），不能再开一层事务——
// 直接复用当前连接上的写路径：SQLite 单写者模型下，同一事务内的写操作
// 天然串行，不存在「取值」与「写入」之间被别的写者插队的风险，这条风险
// 只存在于「s 是顶层 Store、rawDB 非 nil」的情形。
func (s *Store) appendNonSignalEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	if s.rawDB == nil {
		// 已经在事务内：直接在当前连接（*sql.Tx）上取值+写入，不开新事务。
		return allocateAndInsertNonSignalEvent(ctx, s.db, event)
	}

	tx, err := s.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return model.TaskEvent{}, fmt.Errorf("sqlite: AppendEvent 开启事务失败: %w", err)
	}
	defer tx.Rollback()

	assigned, err := allocateAndInsertNonSignalEvent(ctx, tx, event)
	if err != nil {
		return model.TaskEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.TaskEvent{}, fmt.Errorf("sqlite: AppendEvent 提交事务失败: %w", err)
	}
	return assigned, nil
}

// allocateAndInsertNonSignalEvent 在给定的 querier 上取该任务当前最小 seq
// 再减一，然后插入。COALESCE(MIN(seq), 0) - 1：该任务还没有任何事件时
// MIN(seq) 是 SQL NULL，COALESCE 归零后再减一得到 -1，作为第一个非信号
// 序号——比 0 更负，天然不会与将来任何正数信号序号相等，也不与「seq=0」
// 这个 AppendEvent 输入里代表「非信号」的占位值混淆（占位值 0 本身从不
// 落盘，落盘的一定是分配后的负数）。
func allocateAndInsertNonSignalEvent(ctx context.Context, q querier, event model.TaskEvent) (model.TaskEvent, error) {
	var newSeq int
	row := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0) - 1 FROM task_events WHERE task_id = ?`, event.TaskID)
	if err := row.Scan(&newSeq); err != nil {
		return model.TaskEvent{}, fmt.Errorf("sqlite: 分配非信号事件序号失败: %w", err)
	}

	assigned := event
	assigned.Seq = newSeq

	query := fmt.Sprintf(`INSERT INTO task_events (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, eventColumns)
	if _, err := q.ExecContext(ctx, query,
		assigned.TaskID, assigned.Seq, assigned.TS, assigned.Phase, assigned.Level, assigned.Message, assigned.RC, assigned.HookDigest, assigned.Source); err != nil {
		return model.TaskEvent{}, fmt.Errorf("sqlite: 写入非信号事件失败: %w", err)
	}
	return assigned, nil
}

// ListEvents 按任务与过滤条件列出事件，按插入顺序（自增主键 id）排序而
// 非按 seq——负序号的非信号事件也要出现在正确的时间位置（计划任务 6
// 「实现流程」第 3 步）。filter.AfterSeq 用于 SSE 的 Last-Event-ID 续传
// （§12.8）。
func (s *Store) ListEvents(ctx context.Context, taskID string, filter contract.EventFilter) ([]model.TaskEvent, error) {
	query := `SELECT id, ` + eventColumns + ` FROM task_events WHERE task_id = ?`
	args := []any{taskID}
	if filter.AfterSeq != 0 {
		query += ` AND seq > ?`
		args = append(args, filter.AfterSeq)
	}
	query += ` ORDER BY id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translateConstraintErr(err, "ListEvents")
	}
	defer rows.Close()

	out := make([]model.TaskEvent, 0)
	for rows.Next() {
		e, _, err := scanTaskEventWithID(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListEvents")
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListEvents")
	}
	return out, nil
}

// 编译期确认 *sql.Tx 满足 querier 接口（allocateAndInsertNonSignalEvent
// 以 querier 而不是 *sql.Tx 为参数类型，便于非事务路径的 s.db 直接传入）。
var _ querier = (*sql.Tx)(nil)
