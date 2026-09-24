// 本文件实现 contract.FactStore：facts 的合并写入与读取。对应实现计划
// 任务 7 后半段，依据 SPEC §5.5。
package fact

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// Store 实现 contract.FactStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// MergeFacts 把给定键值对合并写入该任务的 fact 集合：已存在的键被覆盖，
// 不存在的键被新增，未出现在本次调用中的既有键保持不变（SPEC §5.5）。
//
// 用 clause.OnConflict 做 UPSERT，不先删后插——先删后插会有一个「该任务
// 的 facts 暂时不存在」的窗口，且这正是 facts KV 存在的理由被破坏的地方：
// 后一个 hook 的写入不能抹掉前一个 hook 已经落库的结果，而先删后插在
// 删除到插入之间的这段时间里，任何并发读到的都是「facts 暂时清空」这个
// 从未真实存在过的状态。task_facts 的主键是 (task_id, key)，UPSERT 天然
// 表达「已存在则覆盖、不存在则新增」，不需要经过删除这一步。
//
// 空 map 输入是合法调用，语义是「本次不合并任何键」——循环体一次都不
// 执行，不报错也不清空既有 facts（不做「空 map 表示清空全部」的特殊
// 解释，那是 replace 语义，不是本方法承诺的 merge 语义）。
func (s *Store) MergeFacts(ctx context.Context, taskID string, facts map[string]string) error {
	if len(facts) == 0 {
		return nil
	}

	now := time.Now().UTC()
	rows := make([]factRow, 0, len(facts))
	for k, v := range facts {
		rows = append(rows, factRow{
			TaskID:    taskID,
			Key:       k,
			Value:     v,
			UpdatedAt: now,
		})
	}

	// ON CONFLICT (task_id, key) DO UPDATE SET value = excluded.value,
	// updated_at = excluded.updated_at —— 与旧实现
	// internal/store/sqlite/fact.go（已移除）的 ON CONFLICT 语句同一语义，只是
	// 用 GORM 的 clause 表达、且一次调用批量写入本次调用的全部键（不逐键
	// 单独发一条语句），减少往返次数。
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&rows).Error
	if err != nil {
		return pgerr.Translate(err, "MergeFacts")
	}
	return nil
}

// GetFacts 返回该任务当前的全部 facts（早先写入的合并结果）。任务不存在
// 或尚无任何 fact 时返回空 map 而不是错误或 nil——facts 是纯粹的 KV
// 存储，没有「facts 集合不存在」这个错误态，只有「facts 集合是空的」
// （SPEC §5.5，与旧实现 internal/store/sqlite/fact.go（已移除）的 GetFacts 同一
// 语义）。
func (s *Store) GetFacts(ctx context.Context, taskID string) (map[string]string, error) {
	var rows []factRow
	if err := s.db.WithContext(ctx).Where("task_id = ?", taskID).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "GetFacts")
	}

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}
