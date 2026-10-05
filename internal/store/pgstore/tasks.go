package pgstore

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// finishedStates 是结束了的任务的状态（model.TaskState.Finished），与 idx_tasks_open 的谓词一致：查「这台主机有没有未结束的
// 任务」时用它。
var finishedStates = []string{string(model.TaskDone), string(model.TaskFailed), string(model.TaskCancelled)}

// forShare 是读锁：别的事务照样能读、能同样加读锁，但改不了、删不掉这一行，要等本事务结束。
var forShare = clause.Locking{Strength: "SHARE"}

// CreateTasks 见 store.Tasks。快照里的串都存得下，由 store.CheckNewTasks 查过：jsonb 不收 NUL，encoding/json 还会把非法的
// UTF-8 换成 U+FFFD。任务逐个插入：撞上 idx_tasks_open 时，才知道撞上的是哪一台。
func (s *Store) CreateTasks(ctx context.Context, ts []model.Task, a store.Audit) ([]model.Task, error) {
	if err := store.CheckAudit(a); err != nil {
		return nil, err
	}
	if err := store.CheckNewTasks(ts); err != nil {
		return nil, err
	}
	rows := make([]taskRow, len(ts))
	ids := make([]string, len(ts))
	for i, t := range ts {
		t.ID, t.State, t.Rev = store.NewID(), model.TaskPending, 1
		row, err := toTaskRow(t)
		if err != nil {
			return nil, err
		}
		rows[i], ids[i] = row, t.ID
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		in, err := admit(tx, ts)
		if err != nil {
			return err
		}
		for _, r := range rows {
			err := tx.Omit(clause.Associations).Create(&r).Error
			if err == nil {
				continue
			}
			// 第 4 步的兜底：失败的任务正在重试、还没提交时，准入看不到它（推进不锁主机），插入要等它提交才撞上。结果按准入
			// 看得到它时的给，与第 3 步报的同形
			if openTaskTaken(err) {
				in.OpenTask[r.HostSN] = true
				if be := store.BatchProblems(in, nil); be != nil {
					return be
				}
			}
			return err
		}
		return addAudits(tx, a, "task", ids...)
	})
	if err != nil {
		return nil, translate(err, false, nil)
	}
	out := make([]model.Task, len(rows))
	for i, r := range rows {
		if out[i], err = r.task(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// admit 是批量建任务的第 2、3 步：在事务 tx 里按固定的顺序加锁，再用锁住之后读到的数据重跑准入（store.BatchProblems）。
// 主机按 SN 的字节序加写锁（lockHosts）；ISO、模板版本与它引用的 hook 版本加读锁，废弃与删除都要等这一批提交。ISO 或模板
// 版本不存在报 ErrNotFound：按键找不到，准入无从判起。同一批的 ISO 与模板版本都相同（CheckNewTasks 查过），看第一个任务
// 就够了。通过时返回判定的入参，插入时撞上 idx_tasks_open 还要拿它重判。
func admit(tx *gorm.DB, batch []model.Task) (admission.BatchInput, error) {
	first := batch[0]
	sns := make([]string, len(batch))
	for i, t := range batch {
		sns[i] = t.HostSN
	}
	hosts, err := lockHosts(tx, sns)
	if err != nil {
		return admission.BatchInput{}, err
	}
	in := admission.BatchInput{OpenTask: map[string]bool{}, Hooks: map[model.HookKey]model.Hook{}}
	var iso isoRow
	switch err := tx.Clauses(forShare).Where("id = ?", first.ISOID).Take(&iso).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return admission.BatchInput{}, isoNotFound(first.ISOID)
	case err != nil:
		return admission.BatchInput{}, err
	}
	if in.ISO, err = iso.iso(); err != nil {
		return admission.BatchInput{}, err
	}
	// 读锁只加在版本行上，连上的身份行不锁，理由同 lockTemplateVersion
	k := model.TemplateKey{ID: first.TemplateID, Version: first.TemplateVersion}
	tpl, ok, err := templateVersionAt(tx.Clauses(lockOf("SHARE", templateTables.versions)), k)
	switch {
	case err != nil:
		return admission.BatchInput{}, err
	case !ok:
		return admission.BatchInput{}, templateNotFound(k)
	}
	if in.Template, err = tpl.template(); err != nil {
		return admission.BatchInput{}, err
	}
	// 引用的 hook 版本按 HookRefs 的顺序加锁，与审批模板时相同；不存在的不放进入参，由准入当作没废弃
	for chunk := range slices.Chunk(store.HookRefs(in.Template), batchSize) {
		rows, err := hookVersionsIn(tx, chunk, lockOf("SHARE", hookTables.versions))
		if err != nil {
			return admission.BatchInput{}, err
		}
		for _, r := range rows {
			h, err := r.hook()
			if err != nil {
				return admission.BatchInput{}, err
			}
			in.Hooks[h.Key()] = h
		}
	}
	open, err := openTasksOf(tx, sns)
	if err != nil {
		return admission.BatchInput{}, err
	}
	var missing []string // 从没登记过，或者在向导里选中之后被删了
	for _, t := range batch {
		row, ok := hosts[t.HostSN]
		if !ok {
			missing = append(missing, t.HostSN)
			continue
		}
		h, err := row.host()
		if err != nil {
			return admission.BatchInput{}, err
		}
		in.Hosts, in.OpenTask[h.SN] = append(in.Hosts, h), open[h.SN]
	}
	return in, store.BatchProblems(in, missing)
}

// lockHosts 在事务 tx 里给 sns 这几台主机加写锁并取出，没登记的不在结果里。按 SN 的字节序加锁（LockRows 在排序之后，行按
// ORDER BY 的顺序上锁），与 ImportHosts 写主机的顺序相同：两批同时提交时，等待不会成环。分块也按这个顺序取。
func lockHosts(tx *gorm.DB, sns []string) (map[string]hostRow, error) {
	sorted := slices.Sorted(slices.Values(sns))
	out := make(map[string]hostRow, len(sorted))
	for chunk := range slices.Chunk(sorted, batchSize) {
		var rows []hostRow
		if err := tx.Clauses(forUpdate).Where("sn IN ?", chunk).Order(`sn COLLATE "C"`).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.SN] = r
		}
	}
	return out, nil
}

// openTasksOf 在事务 tx 里查 sns 这几台主机里哪些已有未结束的任务。调用方锁着这几台主机，同时给它们建任务的另一批要等本
// 事务结束，提交了的这里都看得到；失败任务的重试不锁主机，没提交的看不到，由 idx_tasks_open 兜底。
func openTasksOf(tx *gorm.DB, sns []string) (map[string]bool, error) {
	open := map[string]bool{}
	for chunk := range slices.Chunk(sns, batchSize) {
		var got []string
		err := tx.Model(&taskRow{}).Where("host_sn IN ? AND state NOT IN ?", chunk, finishedStates).Pluck("host_sn", &got).Error
		if err != nil {
			return nil, err
		}
		for _, sn := range got {
			open[sn] = true
		}
	}
	return open, nil
}

// GetTask 见 store.Tasks。
func (s *Store) GetTask(ctx context.Context, id string) (model.Task, error) {
	if err := store.CheckID(id); err != nil {
		return model.Task{}, err
	}
	var row taskRow
	switch err := s.read(ctx).Where("id = ?", id).Take(&row).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return model.Task{}, taskNotFound(id)
	case err != nil:
		return model.Task{}, err
	}
	return row.task()
}

// FindOpenTask 见 store.Tasks。SN 不是规范化之后的写法报 ErrInvalid（store.CheckSN）。
func (s *Store) FindOpenTask(ctx context.Context, sn string) (model.Task, bool, error) {
	if err := store.CheckSN(sn); err != nil {
		return model.Task{}, false, err
	}
	var row taskRow
	switch err := s.read(ctx).Where("host_sn = ? AND state NOT IN ?", sn, finishedStates).Take(&row).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return model.Task{}, false, nil
	case err != nil:
		return model.Task{}, false, err
	}
	t, err := row.task()
	if err != nil {
		return model.Task{}, false, err
	}
	return t, true, nil
}

// ListTasks 见 store.Tasks。按 CreatedAt 倒序、再按 ID 的字节序排，游标是上一页最后一个的这两样。
func (s *Store) ListTasks(ctx context.Context, q store.TaskQuery) (store.Page[model.Task], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[model.Task]{}, err
	}
	var afterAt int64
	var afterID string
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "tasks", &afterAt, &afterID); err != nil {
			return store.Page[model.Task]{}, err
		}
	}
	// 存不下的状态谁都不是，去掉；给了状态却一个都不剩，就什么都匹配不上
	var states []string
	for _, st := range q.States {
		if storable(string(st)) {
			states = append(states, string(st))
		}
	}
	if !storable(q.HostSN) || len(q.States) > 0 && len(states) == 0 {
		return emptyPage[model.Task](), nil
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`(created_at < ? OR (created_at = ? AND id COLLATE "C" > ?))`, afterAt, afterAt, afterID)
	}
	if q.HostSN != "" {
		db = db.Where("host_sn = ?", q.HostSN)
	}
	if len(states) > 0 {
		db = db.Where("state IN ?", states)
	}
	if q.CreatedFrom != 0 {
		db = db.Where("created_at >= ?", q.CreatedFrom)
	}
	if q.CreatedTo != 0 {
		db = db.Where("created_at <= ?", q.CreatedTo)
	}
	var rows []taskRow
	if err := db.Order(`created_at DESC, id COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[model.Task]{}, err
	}
	return page(rows, limit, taskRow.task, func(r taskRow) string { return store.EncodeCursor("tasks", r.CreatedAt, r.ID) })
}

// TransitionTask 见 store.Tasks。
func (s *Store) TransitionTask(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent) (model.Task, []store.Appended, error) {
	return s.transition(ctx, id, expect, to, events, nil)
}

// TransitionTaskByUser 见 store.Tasks。
func (s *Store) TransitionTaskByUser(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent, a store.Audit) (model.Task, []store.Appended, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Task{}, nil, err
	}
	return s.transition(ctx, id, expect, to, events, &a)
}

// transition 是两种推进共用的部分，a 为 nil 时不写审计（系统发起的推进）。先查 ID、目标状态与事件（store.CheckTransition）；
// 再给任务行加写锁、比对期望值，追加事件，用一条 UPDATE 写下状态、Rev 与事件的计数，最后写审计。一步推进合不合规矩不在这里
// 判断，那是第 7 层状态机的事。
//
// C31 不先查：推进不锁主机，先查的话，别人刚建、还没提交的任务查不到，两边就都落了库。推进到未结束的状态时由 idx_tasks_open
// 挡住，撞上时整个推进回滚，问题与准入判定报的相同（store.OpenTaskProblems）。
func (s *Store) transition(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent, a *store.Audit) (model.Task, []store.Appended, error) {
	if err := store.CheckTransition(id, to, events); err != nil {
		return model.Task{}, nil, err
	}
	var row taskRow
	var appended []store.Appended
	err := s.write(ctx, func(tx *gorm.DB) error {
		var err error
		if row, err = lockTask(tx, id); err != nil {
			return err
		}
		if row.State != string(expect.State) || row.Rev != expect.Rev {
			return errorf(store.ErrConflict, "任务 %s 现为 %s、Rev %d，期望的是 %s、Rev %d：重读之后再判断",
				id, row.State, row.Rev, expect.State, expect.Rev)
		}
		if appended, err = appendEvents(tx, &row, events); err != nil {
			return err
		}
		row.State, row.Rev = string(to), row.Rev+1
		if err := tx.Model(&taskRow{ID: id}).Select("state", "rev", "last_seq").Updates(&row).Error; err != nil {
			if openTaskTaken(err) {
				return store.OpenTaskProblems(row.HostSN)
			}
			return err
		}
		if a != nil {
			return addAudits(tx, *a, "task", id)
		}
		return nil
	})
	if err != nil {
		return model.Task{}, nil, translate(err, false, nil)
	}
	t, err := row.task()
	if err != nil {
		return model.Task{}, nil, err
	}
	return t, appended, nil
}

// lockTask 在事务 tx 里给任务 id 加写锁并取出它，不存在报 ErrNotFound。推进与追加事件都先锁它：同一个任务上的这两种写入因此
// 排队，比对期望值、分配事件的 Seq 时都看得到前一次写下的。
func lockTask(tx *gorm.DB, id string) (taskRow, error) {
	var row taskRow
	switch err := tx.Clauses(forUpdate).Where("id = ?", id).Take(&row).Error; {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return taskRow{}, taskNotFound(id)
	case err != nil:
		return taskRow{}, err
	}
	return row, nil
}

// openTaskTaken 报告 err 是不是撞上了 idx_tasks_open：这台主机已有另一个未结束的任务（C31）。
func openTaskTaken(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == codeUnique && pe.ConstraintName == openTaskIndex
}

func taskNotFound(id string) error {
	return errorf(store.ErrNotFound, "任务 %s 不存在", id)
}
