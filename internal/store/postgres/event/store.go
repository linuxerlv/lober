// 本文件实现 contract.EventStore：事件追加与去重、按序号查询。对应实现
// 计划任务 7 的前一半（event 聚合），依据 SPEC §2.5、§3.6、§12.8。
package event

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// Store 实现 contract.EventStore。
type Store struct {
	db *gorm.DB
}

// New 构造 Store，与其他聚合包同一形态（task.New 等）：统一收 *gorm.DB，
// 顶层调用方传业务连接，组合器的 RunInTx 在事务句柄上重新构造。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// AppendEvent 追加一条事件。
//
// event.Seq > 0 表示信号来源事件：直接 INSERT，撞 (task_id, seq) 唯一索引
// idx_task_events_task_seq 时应翻译成 contract.ErrDuplicateSeq——这不是
// 错误路径，是机器侧重试造成的正常重复投递（契约文档明确要求 handler 把
// 它当作"已记录过，继续走成功路径"处理，不是异常）。
//
// 阻塞：这条翻译目前依赖 pgerr.Translate 尚未实现的 23505/ConstraintName
// 分支（见 pgerr/translate.go 文件头注释：github.com/jackc/pgx/v5 不在
// go.mod/go.sum 里，硬性约束禁止本包自行 go get）。本方法已经按最终形态
// 调用 pgerr.Translate，一旦该依赖补齐、pgerr 补上那个分支，本方法不需要
// 改一行代码就能拿到正确行为；但在依赖补齐前，撞唯一索引时实际拿到的是
// pgerr 兜底分支包出的通用 %w 错误，不是 contract.ErrDuplicateSeq——
// store_test.go 的 TestAppendEvent_DuplicateSeq_ReturnsErrDuplicateSeq
// 会如实反映这一点（FAIL），不通过伪造判定让它"看起来"通过。
//
// event.Seq <= 0 表示非信号来源的事件（扫描循环判超时、人工操作），由
// allocateAndInsertNonSignalEvent 分配一个不参与去重的序号，调用方传入的
// Seq 值在此分支被忽略。
func (s *Store) AppendEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	if event.Seq > 0 {
		r := fromModel(event)
		if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
			return model.TaskEvent{}, pgerr.Translate(err, "AppendEvent")
		}
		return toModel(r), nil
	}

	return s.appendNonSignalEvent(ctx, event)
}

// appendNonSignalEvent 处理 event.Seq <= 0 的分支：在一个事务里读取该任务
// 当前最小 seq 再减一，写入分配后的负数序号。
//
// 分配算法与 sqlite/event.go 的 allocateAndInsertNonSignalEvent 保持同样
// 语义（计划任务 7 风险提示要求）：COALESCE(MIN(seq), 0) - 1——该任务还
// 没有任何事件时 MIN(seq) 是 SQL NULL，COALESCE 归零后再减一得到 -1，作为
// 第一个非信号序号。递减而非递增：信号序号天然是正数且递增，两个序号
// 空间永不相交，都保持"新事件序号在数轴上更远离 0"这一单调性质。
//
// 用显式事务保证"取值"与"写入"之间不被并发的另一次非信号事件写入插队，
// 否则两次并发调用会算出同一个候选负数序号而互相撞在同一个唯一索引上。
func (s *Store) appendNonSignalEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	var assigned model.TaskEvent

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var newSeq int
		sqlRow := tx.Raw(`SELECT COALESCE(MIN(seq), 0) - 1 FROM task_events WHERE task_id = ?`, event.TaskID).Row()
		if err := sqlRow.Scan(&newSeq); err != nil {
			return fmt.Errorf("分配非信号事件序号失败: %w", err)
		}

		r := fromModel(event)
		r.Seq = newSeq
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		assigned = toModel(r)
		return nil
	})
	if err != nil {
		return model.TaskEvent{}, pgerr.Translate(err, "AppendEvent")
	}
	return assigned, nil
}

// ListEvents 按任务与过滤条件列出事件，按 id 升序返回（自增主键 id 反映
// 插入顺序，不是按 seq 排序——负序号的非信号事件也要出现在正确的时间
// 位置，与旧实现 sqlite/event.go 的 ListEvents 排序依据一致）。
//
// filter.AfterSeq 非零时只返回 Seq 大于该值的事件，供 SSE 用
// Last-Event-ID 续传（§12.8）。查不到匹配记录返回空切片而非 nil。
func (s *Store) ListEvents(ctx context.Context, taskID string, filter contract.EventFilter) ([]model.TaskEvent, error) {
	q := s.db.WithContext(ctx).Where("task_id = ?", taskID)
	if filter.AfterSeq != 0 {
		q = q.Where("seq > ?", filter.AfterSeq)
	}

	var rows []row
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListEvents")
	}

	out := make([]model.TaskEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, toModel(r))
	}
	return out, nil
}

// 编译期确认 *Store 满足 contract.EventStore，接口加方法时这里先报错，
// 比运行时才发现方法集不匹配更早暴露问题。
var _ contract.EventStore = (*Store)(nil)
