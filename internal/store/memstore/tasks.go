package memstore

import (
	"cmp"
	"context"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// CreateTasks 见 store.Tasks。内存实现的写操作都串行，暂存里读到的就是最新的数据，第 2 步的加锁不用另做；第 3 步对这份数据
// 重跑准入（admit），全部通过才写任务，审计写在任务之后。
func (s *Store) CreateTasks(_ context.Context, ts []model.Task, a store.Audit) ([]model.Task, error) {
	if err := store.CheckAudit(a); err != nil {
		return nil, err
	}
	if err := store.CheckNewTasks(ts); err != nil {
		return nil, err
	}
	out := make([]model.Task, len(ts))
	for i, task := range ts {
		out[i] = cloneTask(task)
	}
	err := s.write(func(t *tx) error {
		if err := t.admit(out); err != nil {
			return err
		}
		for i := range out {
			out[i].ID, out[i].State, out[i].Rev = store.NewID(), model.TaskPending, 1
			t.tasks[out[i].ID] = out[i]
		}
		for _, task := range out {
			if err := t.addAudit(a, "task", task.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = cloneTask(out[i])
	}
	return out, nil
}

// admit 是批量建任务的第 3 步：用暂存里的最新数据组出准入判定的入参，交给 store.BatchProblems，没登记的 SN 由它补报
// host_not_registered。ISO 或模板版本不存在报 ErrNotFound：按键找不到，准入无从判起。同一批的 ISO 与模板版本都相同
// （CheckNewTasks 查过），看第一个任务就够了。
func (t *tx) admit(batch []model.Task) error {
	first := batch[0]
	iso, ok := t.isos[first.ISOID]
	if !ok {
		return isoNotFound(first.ISOID)
	}
	k := model.TemplateKey{ID: first.TemplateID, Version: first.TemplateVersion}
	tpl, ok := t.template(k)
	if !ok {
		return templateNotFound(k)
	}
	in := admission.BatchInput{OpenTask: map[string]bool{}, ISO: iso, Template: tpl, Hooks: map[model.HookKey]model.Hook{}}
	for _, hk := range store.HookRefs(tpl) {
		if h, ok := t.hook(hk); ok {
			in.Hooks[hk] = h
		}
	}
	var missing []string
	for _, task := range batch {
		h, ok := t.hosts[task.HostSN]
		if !ok {
			missing = append(missing, task.HostSN)
			continue
		}
		in.Hosts = append(in.Hosts, h)
		_, in.OpenTask[h.SN] = t.openTask(h.SN, "")
	}
	return store.BatchProblems(in, missing)
}

// GetTask 见 store.Tasks。
func (s *Store) GetTask(_ context.Context, id string) (model.Task, error) {
	if !model.ValidID(id) {
		return model.Task{}, badID(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.d.tasks[id]
	if !ok {
		return model.Task{}, taskNotFound(id)
	}
	return cloneTask(task), nil
}

// FindOpenTask 见 store.Tasks。SN 不是规范化之后的写法报 ErrInvalid：登记过的主机只有那种写法，拿别的写法去找，找不到也不
// 说明这台主机没有任务。
func (s *Store) FindOpenTask(_ context.Context, sn string) (model.Task, bool, error) {
	if n, err := model.NormalizeSN(sn); err != nil || n != sn {
		return model.Task{}, false, errorf(store.ErrInvalid, "SN %q 不是规范化之后的写法：调用方要先经 model.NormalizeSN", sn)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.d.openTask(sn, "")
	if !ok {
		return model.Task{}, false, nil
	}
	return cloneTask(task), true, nil
}

// ListTasks 见 store.Tasks。顺序见 compareTasks，游标是上一页最后一个的 CreatedAt 与 ID。
func (s *Store) ListTasks(_ context.Context, q store.TaskQuery) (store.Page[model.Task], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[model.Task]{}, err
	}
	after := model.Task{CreatedAt: math.MaxInt64} // 只用 CreatedAt 与 ID；没有游标时排在任何任务之前
	if q.Cursor != "" {
		if err := decodeCursor(q.Cursor, "tasks", &after.CreatedAt, &after.ID); err != nil {
			return store.Page[model.Task]{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []model.Task
	for _, task := range s.d.tasks {
		if compareTasks(task, after) > 0 && taskMatches(q, task) {
			items = append(items, task)
		}
	}
	slices.SortFunc(items, compareTasks)
	return page(items, limit, cloneTask, func(task model.Task) string { return encodeCursor("tasks", task.CreatedAt, task.ID) }), nil
}

// TransitionTask 见 store.Tasks。
func (s *Store) TransitionTask(_ context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent) (model.Task, []store.Appended, error) {
	return s.transition(id, expect, to, events, nil)
}

// TransitionTaskByUser 见 store.Tasks。
func (s *Store) TransitionTaskByUser(_ context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent, a store.Audit) (model.Task, []store.Appended, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Task{}, nil, err
	}
	return s.transition(id, expect, to, events, &a)
}

// transition 是两种推进共用的部分，a 为 nil 时不写审计（系统发起的推进）。先查 ID、目标状态与事件，再在暂存里查期望值与
// C31，然后写状态、追加事件，最后写审计，写失败时整次丢掉。一步推进合不合规矩不在这里判断，那是第 7 层状态机的事。
func (s *Store) transition(id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent, a *store.Audit) (model.Task, []store.Appended, error) {
	switch {
	case !model.ValidID(id):
		return model.Task{}, nil, badID(id)
	case !to.Valid():
		return model.Task{}, nil, errorf(store.ErrInvalid, "任务的目标状态 %q 不认识", to)
	}
	if err := store.CheckEvents(events); err != nil {
		return model.Task{}, nil, err
	}
	var out model.Task
	var appended []store.Appended
	err := s.write(func(t *tx) error {
		task, ok := t.tasks[id]
		switch {
		case !ok:
			return taskNotFound(id)
		case task.State != expect.State || task.Rev != expect.Rev:
			return errorf(store.ErrConflict, "任务 %s 现为 %s、Rev %d，期望的是 %s、Rev %d：重读之后再判断",
				id, task.State, task.Rev, expect.State, expect.Rev)
		}
		if _, open := t.openTask(task.HostSN, id); open && !to.Finished() {
			// C31：问题与准入判定报的相同（admission.HostSelectable）
			return &store.ProblemsError{Kind: store.ErrRejected, Problems: admission.HostSelectable(model.Host{SN: task.HostSN}, true)}
		}
		task.State, task.Rev = to, task.Rev+1
		t.tasks[id] = task
		out, appended = task, t.appendEvents(id, events)
		if a != nil {
			return t.addAudit(*a, "task", id)
		}
		return nil
	})
	if err != nil {
		return model.Task{}, nil, err
	}
	return cloneTask(out), appended, nil
}

// openTask 找主机 sn 未结束的任务，跳过 ID 为 except 的那一个（推进的正是它）。C31 保证最多一个。
func (d *data) openTask(sn, except string) (model.Task, bool) {
	for id, task := range d.tasks {
		if id != except && task.HostSN == sn && !task.State.Finished() {
			return task, true
		}
	}
	return model.Task{}, false
}

// referencedBy 报告有没有任务满足 refers，有的话给出其中 ID 最小的一个，报错时说法每次都一样。删主机与 ISO 时用它查引用
// （C26）：任务结束了也算，已建的任务要一直查得到它引用的对象；pgstore 由外键（RESTRICT）挡住。
func (d *data) referencedBy(refers func(model.Task) bool) (string, bool) {
	var ids []string
	for id, task := range d.tasks {
		if refers(task) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", false
	}
	return slices.Min(ids), true
}

// compareTasks 是任务的顺序：CreatedAt 倒序，再按 ID 的字节序。
func compareTasks(a, b model.Task) int {
	return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), strings.Compare(a.ID, b.ID))
}

// taskMatches 报告 task 是否满足 q 的各个过滤条件：主机完全相等，状态是给出的之一，CreatedFrom、CreatedTo 是含两端的时间
// 范围，零值表示不按它过滤。
func taskMatches(q store.TaskQuery, task model.Task) bool {
	return (q.HostSN == "" || task.HostSN == q.HostSN) && (len(q.States) == 0 || slices.Contains(q.States, task.State)) &&
		(q.CreatedFrom == 0 || task.CreatedAt >= q.CreatedFrom) && (q.CreatedTo == 0 || task.CreatedAt <= q.CreatedTo)
}

func taskNotFound(id string) error {
	return errorf(store.ErrNotFound, "任务 %s 不存在", id)
}

// cloneTask 深拷贝一个任务：快照里的 map 与切片逐层各拷一份。nil 拷出来仍是 nil，空的仍是空的：写进去什么就读出什么。
func cloneTask(task model.Task) model.Task {
	snap := &task.Snapshot
	snap.Host.Attrs = maps.Clone(snap.Host.Attrs)
	snap.ISO.Artifacts = maps.Clone(snap.ISO.Artifacts)
	snap.Template.Params = cloneValues(snap.Template.Params)
	if snap.Slots != nil {
		slots := make([]model.SnapshotSlot, len(snap.Slots))
		for i, slot := range snap.Slots {
			if slot.Instances != nil {
				insts := make([]model.SnapshotInstance, len(slot.Instances))
				for j, in := range slot.Instances {
					in.Params = cloneValues(in.Params)
					insts[j] = in
				}
				slot.Instances = insts
			}
			slots[i] = slot
		}
		snap.Slots = slots
	}
	return task
}

// cloneValues 深拷贝参数的最终取值：每个值的各项另拷一份。
func cloneValues(vs map[string]model.SnapshotValue) map[string]model.SnapshotValue {
	if vs == nil {
		return nil
	}
	out := make(map[string]model.SnapshotValue, len(vs))
	for name, v := range vs {
		v.Items = slices.Clone(v.Items)
		out[name] = v
	}
	return out
}
