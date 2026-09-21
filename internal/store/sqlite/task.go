// 本文件实现 contract.TaskStore：任务的建、取、列、身份查找、CAS 状态
// 转换、运行时字段更新、超时扫描、token 查询。对应实现计划任务 3、4、5，
// 依据 SPEC §2.4、§3.3、§3.4、§3.6、§4.2、§4.3、§4.4、§4.7、§6.4。
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// taskColumns 是 tasks 表全部列，按 tasks 表 CREATE 语句里的声明顺序排列
// （schema/001_init.sql），供 INSERT 与 SELECT 共用同一份列名列表——两处
// 列表一旦不一致（比如 INSERT 漏写一列、SELECT 多读一列），symptom 会是
// 运行期的「列数不匹配」或「静默漏写某个字段」，用一份共享列表从根源上
// 消除这类漂移。
const taskColumns = `
	id, created_at, created_by,
	host_sn, host_mac, host_uuid,
	template_id, template_version, artifact_set_id, hook_digests_json,
	form_data_json, resolved_params_json,
	state, state_updated_at, version,
	current_phase, phase_entered_at,
	attempt_count, last_seq, last_signal_at,
	token, token_expires_at, finalize_token, finalize_expires_at,
	failure_reason, failure_detail_json
`

// taskRowScanner 是 *sql.Row 与 *sql.Rows 共有的 Scan 方法，供 scanTaskRow
// 同时服务 QueryRowContext（单行）与 QueryContext（多行遍历）两种调用点。
type taskRowScanner interface {
	Scan(dest ...any) error
}

// scanTaskRow 按 taskColumns 声明的顺序扫描一行到 model.Task。
//
// failure_detail_json 是 tasks 表唯一真正可为 NULL 的列（§3.5：
// *FailureDetail 为 nil 表示未设置失败详情，这是六个时间列之外、
// 唯一一个 Go 侧本身就用指针表达「可能不存在」的字段，与 scan.go
// 顶部注释里「时间列不需要 NULL」的结论并不矛盾——两者是不同的列）。
func scanTaskRow(row taskRowScanner) (model.Task, error) {
	var (
		tk               model.Task
		hookDigestsJSON  string
		formDataJSON     string
		resolvedJSON     string
		failureDetailRaw sql.NullString
	)

	err := row.Scan(
		&tk.ID, &tk.CreatedAt, &tk.CreatedBy,
		&tk.HostSN, &tk.HostMAC, &tk.HostUUID,
		&tk.TemplateID, &tk.TemplateVersion, &tk.ArtifactSetID, &hookDigestsJSON,
		&formDataJSON, &resolvedJSON,
		&tk.State, &tk.StateUpdatedAt, &tk.Version,
		&tk.CurrentPhase, &tk.PhaseEnteredAt,
		&tk.AttemptCount, &tk.LastSeq, &tk.LastSignalAt,
		&tk.Token, &tk.TokenExpiresAt, &tk.FinalizeToken, &tk.FinalizeExpiresAt,
		&tk.FailureReason, &failureDetailRaw,
	)
	if err != nil {
		return model.Task{}, err
	}

	if err := decodeJSON(hookDigestsJSON, &tk.HookDigests); err != nil {
		return model.Task{}, fmt.Errorf("sqlite: 解码任务 %q 的 hook_digests_json 失败: %w", tk.ID, err)
	}
	if err := decodeJSON(formDataJSON, &tk.FormData); err != nil {
		return model.Task{}, fmt.Errorf("sqlite: 解码任务 %q 的 form_data_json 失败: %w", tk.ID, err)
	}
	if err := decodeJSON(resolvedJSON, &tk.ResolvedParams); err != nil {
		return model.Task{}, fmt.Errorf("sqlite: 解码任务 %q 的 resolved_params_json 失败: %w", tk.ID, err)
	}
	if failureDetailRaw.Valid {
		var detail model.FailureDetail
		if err := decodeJSON(failureDetailRaw.String, &detail); err != nil {
			return model.Task{}, fmt.Errorf("sqlite: 解码任务 %q 的 failure_detail_json 失败: %w", tk.ID, err)
		}
		tk.FailureDetail = &detail
	}

	return tk, nil
}

// taskInsertArgs 按 taskColumns 的顺序构造一次 INSERT 的参数列表。
func taskInsertArgs(task model.Task) ([]any, error) {
	hookDigestsJSON, err := encodeJSON(nonNilStringSlice(task.HookDigests))
	if err != nil {
		return nil, fmt.Errorf("sqlite: 编码 hook_digests_json 失败: %w", err)
	}
	formDataJSON, err := encodeJSON(nonNilStringMap(task.FormData))
	if err != nil {
		return nil, fmt.Errorf("sqlite: 编码 form_data_json 失败: %w", err)
	}
	resolvedJSON, err := encodeJSON(nonNilStringMap(task.ResolvedParams))
	if err != nil {
		return nil, fmt.Errorf("sqlite: 编码 resolved_params_json 失败: %w", err)
	}

	var failureDetailArg any
	if task.FailureDetail != nil {
		detailJSON, err := encodeJSON(task.FailureDetail)
		if err != nil {
			return nil, fmt.Errorf("sqlite: 编码 failure_detail_json 失败: %w", err)
		}
		failureDetailArg = detailJSON
	} else {
		failureDetailArg = nil // 写入 SQL NULL——这是 tasks 表唯一可为 NULL 的列
	}

	return []any{
		task.ID, task.CreatedAt, task.CreatedBy,
		task.HostSN, task.HostMAC, task.HostUUID,
		task.TemplateID, task.TemplateVersion, task.ArtifactSetID, hookDigestsJSON,
		formDataJSON, resolvedJSON,
		task.State, task.StateUpdatedAt, task.Version,
		task.CurrentPhase, task.PhaseEnteredAt,
		task.AttemptCount, task.LastSeq, task.LastSignalAt,
		task.Token, task.TokenExpiresAt, task.FinalizeToken, task.FinalizeExpiresAt,
		task.FailureReason, failureDetailArg,
	}, nil
}

// nonNilStringSlice 把 nil 切片归一成空切片，供 JSON 编码产出 "[]" 而不是
// "null"（计划任务 2「编码时 nil 与空值都写成合法 JSON」，encodeJSON 本身
// 不做这层归一，因为哪种空形态合适因字段而异，由调用点决定）。
func nonNilStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nonNilStringMap 把 nil map 归一成空 map，供 JSON 编码产出 "{}" 而不是
// "null"。
func nonNilStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// CreateTask 落库一条新任务。若该主机（按 task.HostSN）已存在非终态任务，
// 撞 idx_tasks_sn_active 唯一索引，翻译成 contract.ErrActiveTaskExists
// （计划任务 3「实现流程」：不先 SELECT 再 INSERT，那有竞态窗口，让索引
// 做判定）。
func (s *Store) CreateTask(ctx context.Context, task model.Task) (model.Task, error) {
	args, err := taskInsertArgs(task)
	if err != nil {
		return model.Task{}, err
	}

	query := fmt.Sprintf(`INSERT INTO tasks (%s) VALUES (%s)`, taskColumns, placeholders(len(args)))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return model.Task{}, translateConstraintErr(err, "CreateTask")
	}
	return task, nil
}

// GetTask 按 id 查任务。查不到返回 contract.ErrNotFound。
func (s *Store) GetTask(ctx context.Context, taskID string) (model.Task, error) {
	query := fmt.Sprintf(`SELECT %s FROM tasks WHERE id = ?`, taskColumns)
	row := s.db.QueryRowContext(ctx, query, taskID)
	tk, err := scanTaskRow(row)
	if err != nil {
		return model.Task{}, translateConstraintErr(err, "GetTask")
	}
	return tk, nil
}

// pendingOnlyTerminalStates 不是本包用到的常量，此处特意不定义——
// 「只匹配 pending」直接写死在下面三条身份查找 SQL 的 WHERE 子句里，
// 不需要一个通用的终态集合（那是 CASTaskState/超时扫描关心的概念）。

// FindPendingTaskByIdentity 按主机身份查 pending 任务，判据是 SN → MAC →
// UUID 优先级回退（§4.3），空键跳过，只匹配 state=pending 的任务。
//
// 三条独立查询依次尝试（计划任务 3「实现流程」第 4 步）：非空的键才查，
// 全部为空直接返回 ErrNotFound，不拿空串去查——否则任何不报 SN 的机器
// 都会撞上第一个 SN 为空的任务（计划任务 3「身份查找的三条硬约束」）。
func (s *Store) FindPendingTaskByIdentity(ctx context.Context, sn, mac, uuid string) (model.Task, error) {
	type attempt struct {
		key    string
		column string
	}
	attempts := []attempt{
		{key: sn, column: "host_sn"},
		{key: mac, column: "host_mac"},
		{key: uuid, column: "host_uuid"},
	}

	for _, a := range attempts {
		if a.key == "" {
			continue
		}
		query := fmt.Sprintf(`SELECT %s FROM tasks WHERE state = ? AND %s = ?`, taskColumns, a.column)
		row := s.db.QueryRowContext(ctx, query, model.TaskStatePending, a.key)
		tk, err := scanTaskRow(row)
		if err == nil {
			return tk, nil
		}
		if translated := translateConstraintErr(err, "FindPendingTaskByIdentity"); translated != contract.ErrNotFound {
			// 非「查无此行」的错误（比如列不存在这类 bug）必须冒泡，
			// 不能被下一层身份键的尝试悄悄吞掉。
			return model.Task{}, translated
		}
		// 该键没查到，按优先级尝试下一个键（不是「转而比较别的键」，
		// 是「这一层给出的键没找到匹配，试下一层」）。
	}
	return model.Task{}, contract.ErrNotFound
}

// ListTasks 按过滤条件列出任务，按创建时间倒序（同值按 id）返回以保证
// 分页稳定（计划任务 3 设计要点）。不传 limit 时用契约默认上限，不返回
// 无界结果集。
func (s *Store) ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error) {
	const defaultLimit = 1000 // M1 契约定的默认上限（计划任务 3「实现流程」第 3 步）

	query := fmt.Sprintf(`SELECT %s FROM tasks WHERE 1=1`, taskColumns)
	var args []any
	if filter.State != "" {
		query += ` AND state = ?`
		args = append(args, filter.State)
	}
	if filter.HostSN != "" {
		query += ` AND host_sn = ?`
		args = append(args, filter.HostSN)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, defaultLimit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translateConstraintErr(err, "ListTasks")
	}
	defer rows.Close()

	out := make([]model.Task, 0)
	for rows.Next() {
		tk, err := scanTaskRow(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListTasks")
		}
		out = append(out, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListTasks")
	}
	return out, nil
}

// placeholders 返回 n 个以逗号分隔的 "?" 占位符，供动态构造的 INSERT
// 语句使用——taskColumns 的列数将来可能变化，占位符数量必须跟着列数走，
// 硬编码一串 "?" 容易在改列时漏改。
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = append(out, '?')
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// CAS 状态转换（计划任务 4，SPEC §3.6）
// ---------------------------------------------------------------------------

// CASTaskState 是唯一允许改变 task.state 的方法。一条 UPDATE，WHERE 子句
// 带三个条件（主键、期望状态、期望版本），SET 里版本自增；受影响行数为 0
// 判定冲突，返回 contract.ErrConflict（计划任务 4「实现流程」）。
//
// 区分「没找到任务」与「冲突」：0 行时额外查一次确认存在性（计划任务 4
// 设计要点：两次查询之间有窗口，但两个分支都返回错误，只是错误类型可能
// 不精确，选简单的这一种）。
//
// state_updated_at 只在状态真的变化（newState != expectedState）时才刷新
// ——CAS 的调用契约上总是在转换状态，但不排斥调用方传入
// expectedState==newState 这种退化情形，这里显式判断而不是无条件刷新，
// 保持「这个状态持续多久」这个字段的含义（计划任务 4 设计要点）。
func (s *Store) CASTaskState(ctx context.Context, taskID string, expectedState model.TaskState, expectedVersion int, newState model.TaskState) (model.Task, error) {
	now := currentUnixTime()

	var (
		res sql.Result
		err error
	)
	if newState != expectedState {
		res, err = s.db.ExecContext(ctx, `UPDATE tasks
			SET state = ?, version = version + 1, state_updated_at = ?
			WHERE id = ? AND state = ? AND version = ?`,
			newState, now, taskID, expectedState, expectedVersion)
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE tasks
			SET state = ?, version = version + 1
			WHERE id = ? AND state = ? AND version = ?`,
			newState, taskID, expectedState, expectedVersion)
	}
	if err != nil {
		return model.Task{}, translateConstraintErr(err, "CASTaskState")
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return model.Task{}, fmt.Errorf("sqlite: CASTaskState 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		if _, err := s.GetTask(ctx, taskID); err != nil {
			// GetTask 已经把 sql.ErrNoRows 翻译成 ErrNotFound，其它错误
			// 原样冒泡；两种情况都不该继续往下判定为 ErrConflict。
			return model.Task{}, err
		}
		return model.Task{}, contract.ErrConflict
	}

	return s.GetTask(ctx, taskID)
}

// currentUnixTime 返回真实墙钟当前时刻（Unix 秒）。CASTaskState 与
// MutateTaskFields 都不接受时钟参数（contract.TaskStore 的方法签名没有
// 提供），conformance 套件的 fixedNow 已锚定套件加载时的真实 time.Now()
// （见 conformance/store.go 顶部注释），因此实现读真实时钟与套件的时间
// 基准天然一致。
func currentUnixTime() int64 {
	return time.Now().Unix()
}

// MutateTaskFields 更新不涉及状态转换的运行时字段。model.TaskMutation
// 类型本身不含 State 字段，因此本方法天然不存在写 state 列的路径——这条
// 约束在类型层已经做了一半，本实现不主动新增任何能设置 state 列的调用
// 路径（计划任务 4「两条不能妥协的约束」第二条）。
//
// 从「可选字段更新集」构造 SET 子句：只为非 nil 的字段生成赋值，动态 SET
// 子句用参数占位符、绝不字符串拼接值（计划任务 4「设计要点」：参数来自
// HTTP 请求，拼接就是注入）。
func (s *Store) MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error) {
	var setClauses []string
	var args []any

	if mutation.CurrentPhase != nil {
		setClauses = append(setClauses, "current_phase = ?")
		args = append(args, *mutation.CurrentPhase)
	}
	if mutation.PhaseEnteredAt != nil {
		setClauses = append(setClauses, "phase_entered_at = ?")
		args = append(args, *mutation.PhaseEnteredAt)
	}
	if mutation.AttemptCount != nil {
		setClauses = append(setClauses, "attempt_count = ?")
		args = append(args, *mutation.AttemptCount)
	}
	if mutation.LastSeq != nil {
		setClauses = append(setClauses, "last_seq = ?")
		args = append(args, *mutation.LastSeq)
	}
	if mutation.LastSignalAt != nil {
		setClauses = append(setClauses, "last_signal_at = ?")
		args = append(args, *mutation.LastSignalAt)
	}
	if mutation.Token != nil {
		setClauses = append(setClauses, "token = ?")
		args = append(args, nullableToken(*mutation.Token))
	}
	if mutation.TokenExpiresAt != nil {
		setClauses = append(setClauses, "token_expires_at = ?")
		args = append(args, *mutation.TokenExpiresAt)
	}
	if mutation.FinalizeToken != nil {
		setClauses = append(setClauses, "finalize_token = ?")
		args = append(args, nullableToken(*mutation.FinalizeToken))
	}
	if mutation.FinalizeExpiresAt != nil {
		setClauses = append(setClauses, "finalize_expires_at = ?")
		args = append(args, *mutation.FinalizeExpiresAt)
	}
	if mutation.ResolvedParams != nil {
		encoded, err := encodeJSON(nonNilStringMap(*mutation.ResolvedParams))
		if err != nil {
			return model.Task{}, fmt.Errorf("sqlite: 编码 resolved_params_json 失败: %w", err)
		}
		setClauses = append(setClauses, "resolved_params_json = ?")
		args = append(args, encoded)
	}
	if mutation.FailureReason != nil {
		setClauses = append(setClauses, "failure_reason = ?")
		args = append(args, *mutation.FailureReason)
	}
	if mutation.FailureDetail != nil {
		encoded, err := encodeJSON(mutation.FailureDetail)
		if err != nil {
			return model.Task{}, fmt.Errorf("sqlite: 编码 failure_detail_json 失败: %w", err)
		}
		setClauses = append(setClauses, "failure_detail_json = ?")
		args = append(args, encoded)
	}

	if len(setClauses) == 0 {
		// 空 mutation：不发一条无 SET 子句的 UPDATE（那是语法错误），
		// 直接返回当前状态，语义上等同于「什么都没改」。
		return s.GetTask(ctx, taskID)
	}

	query := "UPDATE tasks SET " + joinSetClauses(setClauses) + " WHERE id = ?"
	args = append(args, taskID)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.Task{}, translateConstraintErr(err, "MutateTaskFields")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Task{}, fmt.Errorf("sqlite: MutateTaskFields 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		return model.Task{}, contract.ErrNotFound
	}

	return s.GetTask(ctx, taskID)
}

// joinSetClauses 把 SET 子句片段用 ", " 连接，供 MutateTaskFields 拼出
// 动态 UPDATE 语句。
func joinSetClauses(clauses []string) string {
	return strings.Join(clauses, ", ")
}

// ---------------------------------------------------------------------------
// 超时扫描（计划任务 5，SPEC §3.3）
// ---------------------------------------------------------------------------

// terminalTaskStates 是终态集合，供 SQL 里的 "state NOT IN (...)" 排除
// 终态任务用。与 model.TaskState.IsTerminal() 的判据保持同步——这里没有
// 直接调用 IsTerminal（它是一个按值判断的方法，不是可枚举的集合），而是
// 手写与其等价的两个值；ListTimedOutTasks 的测试
// （testTimeoutScanNeverReturnsPending 的姊妹断言，即 conformance 套件里
// 「确认扫描逻辑本身工作」的那部分）会在 model 新增终态值但这里漏改时
// 失败，因为多出来的终态会被误当非终态查出来。
var terminalTaskStates = []model.TaskState{model.TaskStateDone, model.TaskStateFailed}

// ListTimedOutTasks 返回超时扫描应判失败的任务：当前阶段的
// phase_entered_at + 该阶段预算 < asOf 的非终态任务。
//
// 取出全部非终态任务后在 Go 里用 model.PhaseBudget 过滤（计划任务 5
// 「超时扫描的实现方式」推荐项）：预算值只有一个来源（model 的预算表），
// 不在 SQL 里另存一份 CASE 表达式，避免两处定义漂移。pending 阶段
// PhaseBudget 返回 ok=false，天然被跳过——不需要在 SQL 或 Go 里单独
// 硬编码「排除 pending」，这条约束是 model.PhaseBudget 自身的契约
// （无预算的阶段就是没有「超时」这件事）。
func (s *Store) ListTimedOutTasks(ctx context.Context, asOf int64) ([]model.Task, error) {
	query := fmt.Sprintf(`SELECT %s FROM tasks WHERE state NOT IN (?, ?)`, taskColumns)
	rows, err := s.db.QueryContext(ctx, query, terminalTaskStates[0], terminalTaskStates[1])
	if err != nil {
		return nil, translateConstraintErr(err, "ListTimedOutTasks")
	}
	defer rows.Close()

	out := make([]model.Task, 0)
	for rows.Next() {
		tk, err := scanTaskRow(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListTimedOutTasks")
		}
		budget, ok := model.PhaseBudget(tk.CurrentPhase)
		if !ok {
			continue // 无预算阶段（当前只有 pending）：永不超时，跳过
		}
		if tk.PhaseEnteredAt+int64(budget.Seconds()) < asOf {
			out = append(out, tk)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListTimedOutTasks")
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// token 查询（计划任务 5，SPEC §4.4、§6.4）
// ---------------------------------------------------------------------------

// GetTaskByToken 按 token 值与用途查任务。token 不存在、已过期、或与
// 请求的 purpose 不符，统一返回 contract.ErrNotFound——不泄露「token 存在
// 但过期了」这一区分（计划任务 5「token 查询的四条要求」）。
//
// 空 token 直接返回 ErrNotFound，不拿空串去查——两个唯一索引的条件是
// 非空，但空串未必是 NULL，拿空串去查在语义上等同于「查一个不存在的
// token」，直接短路避免误配对到某一行 token 列恰好是空串的任务（不应有
// 这样的行，但短路本身不依赖这个前提）。
//
// 过期判据用真实 time.Now()：本方法签名不接受时钟参数，conformance 套件
// 的 fixedNow 已锚定套件加载时的真实时钟（见 conformance/store.go 顶部
// 注释），因此实现读真实时钟与套件的时间基准天然一致。
func (s *Store) GetTaskByToken(ctx context.Context, token string, purpose model.TokenPurpose) (model.Task, error) {
	if token == "" {
		return model.Task{}, contract.ErrNotFound
	}

	var column, expiresColumn string
	switch purpose {
	case model.TokenPurposeInstall:
		column, expiresColumn = "token", "token_expires_at"
	case model.TokenPurposeFinalize:
		column, expiresColumn = "finalize_token", "finalize_expires_at"
	default:
		return model.Task{}, contract.ErrNotFound
	}

	query := fmt.Sprintf(`SELECT %s FROM tasks WHERE %s = ?`, taskColumns, column)
	row := s.db.QueryRowContext(ctx, query, token)
	tk, err := scanTaskRow(row)
	if err != nil {
		return model.Task{}, translateConstraintErr(err, "GetTaskByToken")
	}

	var expiresAt int64
	if expiresColumn == "token_expires_at" {
		expiresAt = tk.TokenExpiresAt
	} else {
		expiresAt = tk.FinalizeExpiresAt
	}
	if expiresAt < currentUnixTime() {
		return model.Task{}, contract.ErrNotFound
	}
	return tk, nil
}
