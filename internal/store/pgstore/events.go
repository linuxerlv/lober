package pgstore

import (
	"context"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// AppendEvents 见 store.Events。空的一批也要任务存在：调用方拿错了任务 ID，不能当成追加成功。
func (s *Store) AppendEvents(ctx context.Context, taskID string, es []store.NewEvent) ([]store.Appended, error) {
	if !model.ValidID(taskID) {
		return nil, badID(taskID)
	}
	if err := store.CheckEvents(es); err != nil {
		return nil, err
	}
	var out []store.Appended
	err := s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockTask(tx, taskID)
		if err != nil {
			return err
		}
		last := row.LastSeq
		if out, err = appendEvents(tx, &row, es); err != nil || row.LastSeq == last {
			return err // 一条都没新写（空的一批，或者 Key 全都已有），计数不用动
		}
		return tx.Model(&taskRow{ID: taskID}).Select("last_seq").Updates(&row).Error
	})
	if err != nil {
		return nil, translate(err, false, nil)
	}
	return out, nil
}

// ListEvents 见 store.Events。afterSeq 为负报 ErrInvalid；任务不存在报 ErrNotFound，同 AppendEvents。
func (s *Store) ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]store.Event, error) {
	if !model.ValidID(taskID) {
		return nil, badID(taskID)
	}
	n, err := store.CheckQuery("", limit)
	if err != nil {
		return nil, err
	}
	if afterSeq < 0 {
		return nil, errorf(store.ErrInvalid, "afterSeq 不能为负，收到 %d", afterSeq)
	}
	var rows []taskEventRow
	if err := s.read(ctx).Where("task_id = ? AND seq > ?", taskID, afterSeq).Order("seq").Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 { // 取到了事件，任务就一定在（外键）；一条都没取到时才另查
		var tasks int64
		if err := s.read(ctx).Model(&taskRow{}).Where("id = ?", taskID).Count(&tasks).Error; err != nil {
			return nil, err
		}
		if tasks == 0 {
			return nil, taskNotFound(taskID)
		}
	}
	out := make([]store.Event, len(rows)) // 没有事件时也不是 nil，第 5 层编成 JSON 是 []
	for i, r := range rows {
		if out[i], err = r.event(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// appendEvents 在事务 tx 里给锁住的任务 row 追加 es，事件已由调用方查过（store.CheckEvents）。Seq 接着 row.LastSeq 分配，
// row.LastSeq 随之加上去，由调用方连同别的列一起写回。Key 已存在的（包括同一批里排在前面的）跳过，报告为 Duplicate，Seq 是
// 原来那一条的。调用方锁着任务行，同一个任务的另一次追加插不进来，这里查到的已有 Key 就是全部；idx_task_events_key 兜底。
func appendEvents(tx *gorm.DB, row *taskRow, es []store.NewEvent) ([]store.Appended, error) {
	out := make([]store.Appended, len(es)) // 不带事件的一批也不是 nil
	if len(es) == 0 {
		return out, nil
	}
	seen, err := keySeqs(tx, row.ID, es)
	if err != nil {
		return nil, err
	}
	var rows []taskEventRow
	for i, e := range es {
		if seq, ok := seen[e.Key]; ok { // seen 里没有空的 Key：没有 Key 的不去重
			out[i] = store.Appended{Seq: seq, Duplicate: true}
			continue
		}
		row.LastSeq++
		r, err := toEventRow(row.ID, row.LastSeq, e)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
		out[i] = store.Appended{Seq: row.LastSeq}
		if e.Key != "" {
			seen[e.Key] = row.LastSeq
		}
	}
	if len(rows) == 0 { // Key 全都已有；GORM 不收空的一批
		return out, nil
	}
	return out, tx.Omit(clause.Associations).CreateInBatches(&rows, batchSize).Error
}

// keySeqs 在事务 tx 里查 es 带的 Key 中，任务 id 已经有了的那些，给出 Key → Seq。
func keySeqs(tx *gorm.DB, id string, es []store.NewEvent) (map[string]int64, error) {
	var keys []string
	for _, e := range es {
		if e.Key != "" {
			keys = append(keys, e.Key)
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	seen := make(map[string]int64, len(keys))
	for chunk := range slices.Chunk(keys, batchSize) {
		var got []taskEventRow
		if err := tx.Select("key", "seq").Where("task_id = ? AND key IN ?", id, chunk).Find(&got).Error; err != nil {
			return nil, err
		}
		for _, r := range got {
			seen[r.Key] = r.Seq
		}
	}
	return seen, nil
}
