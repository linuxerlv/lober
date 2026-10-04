package memstore

import (
	"context"
	"maps"
	"slices"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// AppendEvents 见 store.Events。空的一批也要任务存在：调用方拿错了任务 ID，不能当成追加成功。
func (s *Store) AppendEvents(_ context.Context, taskID string, es []store.NewEvent) ([]store.Appended, error) {
	if !model.ValidID(taskID) {
		return nil, badID(taskID)
	}
	if err := store.CheckEvents(es); err != nil {
		return nil, err
	}
	var out []store.Appended
	err := s.write(func(t *tx) error {
		if _, ok := t.tasks[taskID]; !ok {
			return taskNotFound(taskID)
		}
		out = t.appendEvents(taskID, es)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListEvents 见 store.Events。afterSeq 为负报 ErrInvalid；任务不存在报 ErrNotFound，同 AppendEvents。
func (s *Store) ListEvents(_ context.Context, taskID string, afterSeq int64, limit int) ([]store.Event, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.d.tasks[taskID]; !ok {
		return nil, taskNotFound(taskID)
	}
	log := s.d.events[taskID]
	log = log[int(min(afterSeq, int64(len(log)))):] // Seq 就是下标加 1
	log = log[:min(n, len(log))]
	out := make([]store.Event, len(log)) // 没有事件时也不是 nil，第 5 层编成 JSON 是 []
	for i, e := range log {
		out[i] = e
		out[i].Fields = maps.Clone(e.Fields)
	}
	return out, nil
}

// appendEvents 在暂存里给任务 id 追加 es，事件已由调用方查过。Seq 接着这个任务已有的分配；Key 已存在的（包括同一批里排在
// 前面的）跳过，报告为 Duplicate，Seq 是原来那一条的。先截掉已提交那块多余的容量，追加时就另起一块：暂存丢掉时，已提交的
// 那块不受影响。
func (t *tx) appendEvents(id string, es []store.NewEvent) []store.Appended {
	out := make([]store.Appended, len(es)) // 不带事件的一批也不是 nil
	if len(es) == 0 {
		return out
	}
	log := slices.Clip(t.events[id])
	for i, e := range es {
		if seq, ok := keySeq(log, e.Key); ok {
			out[i] = store.Appended{Seq: seq, Duplicate: true}
			continue
		}
		e.Fields = maps.Clone(e.Fields)
		log = append(log, store.Event{TaskID: id, Seq: int64(len(log)) + 1, NewEvent: e})
		out[i] = store.Appended{Seq: int64(len(log))}
	}
	t.events[id] = log
	return out
}

// keySeq 找 log 里 Key 为 key 的那一条，返回它的 Seq。空的 Key 不去重。
func keySeq(log []store.Event, key string) (int64, bool) {
	if key == "" {
		return 0, false
	}
	for _, e := range log {
		if e.Key == key {
			return e.Seq, true
		}
	}
	return 0, false
}
