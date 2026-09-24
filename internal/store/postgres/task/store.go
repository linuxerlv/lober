// 本文件实现 contract.TaskStore 的八个方法，对应实现计划任务 3、4、5
// （三个任务合并交付）。签名与语义完全照抄
// internal/core/contract/store.go:87-141，参考语义抄
// internal/store/sqlite/task.go（已移除）的既有实现（SQL 方言不同，业务判断
// 一致）。
package task

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// Store 实现 contract.TaskStore，持有一个 *gorm.DB（可能是顶层连接，也
// 可能是 RunInTx 传入的事务句柄——两者是同一个类型，不需要额外接口）。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// 编译期断言：Store 满足 contract.TaskStore。
var _ contract.TaskStore = (*Store)(nil)

// CreateTask 落库一条新任务。若该主机已存在非终态任务，撞
// idx_tasks_sn_active 唯一索引，经 pgerr.Translate 翻译成
// contract.ErrActiveTaskExists（计划任务 4：不先 SELECT 再 INSERT，
// 那有竞态窗口，让索引做判定）。
func (s *Store) CreateTask(ctx context.Context, task model.Task) (model.Task, error) {
	row := taskFromModel(task)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return model.Task{}, pgerr.Translate(err, "CreateTask")
	}
	return row.toModel(), nil
}

// GetTask 按 id 查任务。查不到返回 contract.ErrNotFound。
func (s *Store) GetTask(ctx context.Context, taskID string) (model.Task, error) {
	var row taskRow
	if err := s.db.WithContext(ctx).Where("id = ?", taskID).First(&row).Error; err != nil {
		return model.Task{}, pgerr.Translate(err, "GetTask")
	}
	return row.toModel(), nil
}

// FindPendingTaskByIdentity 按主机身份查 pending 任务，判据是 SN → MAC →
// UUID 优先级回退（§4.3），空键跳过，只匹配 state=pending 的任务。
//
// 三条独立查询依次尝试：非空的键才查，全部为空直接返回 ErrNotFound，
// 不拿空串去查——否则任何不报 SN 的机器都会撞上第一个 host_sn 也为空的
// pending 任务（计划任务 3「身份查找的三条硬约束」，与 sqlite 版实现
// 同一语义）。
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
		var row taskRow
		err := s.db.WithContext(ctx).
			Where("state = ? AND "+a.column+" = ?", model.TaskStatePending, a.key).
			First(&row).Error
		if err == nil {
			return row.toModel(), nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			// 非「查无此行」的错误（比如列不存在这类 bug）必须冒泡，
			// 不能被下一层身份键的尝试悄悄吞掉。
			return model.Task{}, pgerr.Translate(err, "FindPendingTaskByIdentity")
		}
		// 该键没查到，按优先级尝试下一个键。
	}
	return model.Task{}, contract.ErrNotFound
}

// ListTasks 按过滤条件列出任务，按创建时间倒序（同值按 id 倒序）返回，
// 保证分页稳定，语义与 sqlite 版一致。零值过滤字段不参与 WHERE。
func (s *Store) ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error) {
	const defaultLimit = 1000 // M1 契约定的默认上限，与 sqlite 版一致

	q := s.db.WithContext(ctx).Model(&taskRow{})
	if filter.State != "" {
		q = q.Where("state = ?", filter.State)
	}
	if filter.HostSN != "" {
		q = q.Where("host_sn = ?", filter.HostSN)
	}
	// 失败原因过滤是失败分布报表「下钻」那一步的支撑（P4-04 任务 3）。
	if filter.FailureReason != "" {
		q = q.Where("failure_reason = ?", filter.FailureReason)
	}
	// 创建时间是**闭区间**，与 audit 的 ts 区间语义一致——两处是运维在两个
	// 界面上看同一个时间窗口，包含性不同会让边界记录在一处出现、另一处消失
	// （contract.TaskFilter 的字段文档说明了为什么必须相同）。
	//
	// 必须先转成 time.Time 再进 WHERE：created_at 是 timestamptz 列（设计
	// 文档 §5 的类型映射），而 filter 里是 Unix 秒。直接把 int64 交给 pgx
	// 会报「unable to encode 1790245501 into binary format for timestamptz
	// (OID 1184): cannot find encode plan」——pgx 不做这个隐式转换，也不该做。
	// 上面三个条件之所以不需要转，是因为 state/host_sn/failure_reason 都是
	// TEXT 列。用 .UTC() 与写入侧保持同一时区约定（Unix 秒本身无时区，
	// timestamptz 存绝对时刻，往返不丢信息）。
	if filter.CreatedSinceTS != 0 {
		q = q.Where("created_at >= ?", time.Unix(filter.CreatedSinceTS, 0).UTC())
	}
	if filter.CreatedUntilTS != 0 {
		q = q.Where("created_at <= ?", time.Unix(filter.CreatedUntilTS, 0).UTC())
	}

	// Limit 零值取 M1 的默认上限（保持本方法一直以来的行为）；显式传入的
	// 值原样生效，允许超过 1000——CSV 导出需要一次取远超 1000 行，那是调用
	// 方知情的选择（contract.TaskFilter.Limit 文档）。
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	var rows []taskRow
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListTasks")
	}

	out := make([]model.Task, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

// CASTaskState 是唯一允许改变 task.state 的方法。条件是 id AND state AND
// version 三者同时成立，不是只比版本号；成功时 version+1 并返回更新后的
// 任务；RowsAffected == 0 时判定为冲突还是不存在，先额外查一次确认存在性
// （与 sqlite 版同一策略：两次查询之间有竞态窗口，但两个分支都返回错误，
// 只是错误类型可能不精确，选简单的这一种）。不用 optimisticlock 插件
// （设计文档 §2：该插件的模型与本项目 (state, version) 双条件 CAS 不
// 匹配）。
//
// state_updated_at 只在状态真的变化时才刷新，保持「这个状态持续多久」
// 这个字段的含义（与 sqlite 版一致）。
func (s *Store) CASTaskState(ctx context.Context, taskID string, expectedState model.TaskState, expectedVersion int, newState model.TaskState) (model.Task, error) {
	now := time.Now().UTC()

	updates := map[string]any{
		"state":   newState,
		"version": gorm.Expr("version + 1"),
	}
	if newState != expectedState {
		updates["state_updated_at"] = now
	}

	result := s.db.WithContext(ctx).Model(&taskRow{}).
		Where("id = ? AND state = ? AND version = ?", taskID, expectedState, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return model.Task{}, pgerr.Translate(result.Error, "CASTaskState")
	}

	if result.RowsAffected == 0 {
		if _, err := s.GetTask(ctx, taskID); err != nil {
			// GetTask 已经把「查无此行」翻译成 ErrNotFound，其它错误
			// 原样冒泡；两种情况都不该继续往下判定为 ErrConflict。
			return model.Task{}, err
		}
		return model.Task{}, contract.ErrConflict
	}

	return s.GetTask(ctx, taskID)
}

// MutateTaskFields 更新不涉及状态转换的运行时字段。model.TaskMutation
// 每个字段是指针，nil 表示不改；类型本身不含 State 字段，本方法不新增
// 任何能写 state 列的路径。
//
// 用 map 形式的 Updates 而不是传结构体：GORM 的 Updates 对结构体入参会
// 忽略零值字段，这在「把 *time.Time 从有值改回 nil（即 model 侧 0）」
// 时会导致该列被跳过而不是写成 SQL NULL——这是本方法最容易静默出错的
// 地方（计划任务 5 风险提示）。map 形式的 Updates 对显式列出的键一律
// 生成 SET，包括值为 nil 的键，因此可空时间列的「清空」能真正落地成
// NULL。
func (s *Store) MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error) {
	updates := map[string]any{}

	if mutation.CurrentPhase != nil {
		updates["current_phase"] = *mutation.CurrentPhase
	}
	if mutation.PhaseEnteredAt != nil {
		updates["phase_entered_at"] = time.Unix(*mutation.PhaseEnteredAt, 0).UTC()
	}
	if mutation.AttemptCount != nil {
		updates["attempt_count"] = *mutation.AttemptCount
	}
	if mutation.LastSeq != nil {
		updates["last_seq"] = *mutation.LastSeq
	}
	if mutation.LastSignalAt != nil {
		// pgtype.NullTimeFromUnix 把 0 映射为 nil（SQL NULL）——map 形式
		// 的 Updates 对显式列出的键一律写 SET，即便值是 nil 指针，因此
		// 「非0→0」这条测试要求的 NULL 写入在这里真的会发生。
		updates["last_signal_at"] = pgtype.NullTimeFromUnix(*mutation.LastSignalAt)
	}
	if mutation.Token != nil {
		updates["token"] = *mutation.Token
	}
	if mutation.TokenExpiresAt != nil {
		updates["token_expires_at"] = pgtype.NullTimeFromUnix(*mutation.TokenExpiresAt)
	}
	if mutation.FinalizeToken != nil {
		updates["finalize_token"] = *mutation.FinalizeToken
	}
	if mutation.FinalizeExpiresAt != nil {
		updates["finalize_expires_at"] = pgtype.NullTimeFromUnix(*mutation.FinalizeExpiresAt)
	}
	if mutation.ResolvedParams != nil {
		updates["resolved_params_json"] = pgtype.StringMap(*mutation.ResolvedParams)
	}
	if mutation.FailureReason != nil {
		updates["failure_reason"] = *mutation.FailureReason
	}
	if mutation.FailureDetail != nil {
		updates["failure_detail_json"] = pgtype.JSON[*model.FailureDetail]{Val: mutation.FailureDetail}
	}

	if len(updates) == 0 {
		// 空 mutation：不发一条空 SET 的 UPDATE，直接返回当前状态，
		// 语义上等同于「什么都没改」（与 sqlite 版一致）。
		return s.GetTask(ctx, taskID)
	}

	result := s.db.WithContext(ctx).Model(&taskRow{}).Where("id = ?", taskID).Updates(updates)
	if result.Error != nil {
		return model.Task{}, pgerr.Translate(result.Error, "MutateTaskFields")
	}
	if result.RowsAffected == 0 {
		return model.Task{}, contract.ErrNotFound
	}

	return s.GetTask(ctx, taskID)
}

// ListTimedOutTasks 返回超时扫描应判失败的任务：当前阶段的
// phase_entered_at + 该阶段预算 < asOf 的非终态任务。
//
// 查询范围以 model.BudgetedPhases() 为准（排除无预算的阶段，即
// pending），不遍历全部 Phase 取值再跳过 pending——后者在 Phase 新增
// 取值时容易漏改（契约文档明确要求）。SQL 侧只按 current_phase IN (...)
// 缩小范围，具体的「是否超时」判断（阶段各自的 budget 不同）仍在 Go
// 侧完成，终态判据用 model.TaskState.IsTerminal() 统一来源，不在 SQL
// 里另写一份 state NOT IN (...)。
func (s *Store) ListTimedOutTasks(ctx context.Context, asOf int64) ([]model.Task, error) {
	budgeted := model.BudgetedPhases()
	if len(budgeted) == 0 {
		return []model.Task{}, nil
	}

	var rows []taskRow
	if err := s.db.WithContext(ctx).Where("current_phase IN ?", budgeted).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListTimedOutTasks")
	}

	out := make([]model.Task, 0)
	for _, row := range rows {
		tk := row.toModel()
		if tk.State.IsTerminal() {
			continue // 终态任务不参与超时扫描
		}
		budget, ok := model.PhaseBudget(tk.CurrentPhase)
		if !ok {
			continue // 无预算阶段：永不超时，跳过（理论上不会出现，因为
			// SQL 已经按 BudgetedPhases() 过滤过；保留判断是双重防线）
		}
		if tk.PhaseEnteredAt+int64(budget.Seconds()) < asOf {
			out = append(out, tk)
		}
	}
	return out, nil
}

// GetTaskByToken 按 token 值与用途查任务。token 不存在、已过期、或与
// 请求的 purpose 不符，统一返回 contract.ErrNotFound——四种情形的返回
// 必须无法区分，不泄露 token 曾经存在过（§4.4）。
func (s *Store) GetTaskByToken(ctx context.Context, token string, purpose model.TokenPurpose) (model.Task, error) {
	if token == "" {
		return model.Task{}, contract.ErrNotFound
	}

	var column string
	switch purpose {
	case model.TokenPurposeInstall:
		column = "token"
	case model.TokenPurposeFinalize:
		column = "finalize_token"
	default:
		return model.Task{}, contract.ErrNotFound
	}

	var row taskRow
	err := s.db.WithContext(ctx).Where(column+" = ?", token).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Task{}, contract.ErrNotFound
		}
		return model.Task{}, pgerr.Translate(err, "GetTaskByToken")
	}

	tk := row.toModel()
	var expiresAt int64
	if purpose == model.TokenPurposeInstall {
		expiresAt = tk.TokenExpiresAt
	} else {
		expiresAt = tk.FinalizeExpiresAt
	}
	if expiresAt < time.Now().Unix() {
		return model.Task{}, contract.ErrNotFound
	}
	return tk, nil
}
