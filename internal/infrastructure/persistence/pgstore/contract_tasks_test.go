package pgstore_test

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	coresnapshot "github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
)

// Test契约任务 是任务这一组（C27–C31、C33）：批量建任务的前提、在事务里重跑准入、条件推进与查询，以及要审计的两个方法。
// 主机与 ISO 被任务引用之后不能删（C26），那两个用例写在 contract_hosts_test.go、contract_isos_test.go，要先建任务，在这一组
// 跑。事件见 Test契约事件，并发见 Test契约并发。
func Test契约任务(t *testing.T) {
	runCases(t, []testCase{
		{"建任务：ID 合规且是安全词，State 为 pending，Rev 为 1，读回来与写进去的相同", taskCreate},
		{"建任务：仓储不解释快照，nil 与空的 map、切片都原样存下", taskCreateSnapshot},
		{"建任务：主机的任务结束之后，可以再给它建", taskCreateAfterFinished},
		{"建任务：普通仓储仍接受旧的已审批版本", taskCreateOlderApproved},
		{"当前选择：旧选择整批拒绝且不写审计", taskCurrentStale},
		{"当前选择：一般准入失败仍报整批错误", taskCurrentRejected},
		{"当前选择：返回顺序与默认字段不变", taskCurrentCreate},
		{"建任务：入参的每条前提各一个反例，都报 ErrInvalid，什么都不写", taskCreateInvalid},
		{"建任务：批里 SN 重复报 host_duplicate_in_batch", taskCreateDuplicate},
		{"建任务：ISO 或模板版本不存在报 ErrNotFound", taskCreateMissing},
		{"建任务：一批的主机多于一批（batchSize）：分批加锁与查未结束的任务，第二批的照样逐台报出、照样建上", taskCreateManyBatches},
	})
	runCases(t, rejectedCases())
	runCases(t, []testCase{
		{"引用：主机被任务引用之后删除报 ErrReferenced，任务结束了也一样", hostDeleteReferenced},
		{"引用：ISO 被任务引用之后删除报 ErrReferenced，废弃了、任务结束了也一样", isoDeleteReferenced},
		{"推进：Rev 加 1，状态与事件一起提交；仓储不判断推进合不合规矩；不写审计", taskTransition},
		{"推进：Rev 或期望的状态对不上报 ErrConflict，什么都不改", taskTransitionConflict},
		{"推进：目标状态或事件不合法报 ErrInvalid，整个推进不生效", taskTransitionInvalid},
		{"推进：任务不存在报 ErrNotFound，ID 不合格式报 ErrInvalid", taskTransitionMissing},
		{"推进：回到未结束的状态时，这台主机已有别的未结束任务，报 host_has_open_task", taskTransitionOpenTask},
		{"查询：GetTask 找不到报 ErrNotFound，ID 不合格式报 ErrInvalid", taskGet},
		{"查询：FindOpenTask 在有、没有、任务刚结束时都对", taskFindOpen},
		{"查询：ListTasks 按 CreatedAt 倒序、再按 ID，翻页与一次全取相同", taskListPaging},
		{"查询：ListTasks 按主机、状态、时间范围过滤", taskListFilters},
		{"查询：ListTasks 的查询不合规则报 ErrInvalid", taskListInvalid},
		{"查询：ListTasks 的 Limit 为 0 时每页 100 条", taskListDefaultLimit},
		{"返回值与入参都是拷贝：快照是深拷贝", taskCopies},
	})
	runAudited(t, taskAuditedOps())
}

func taskCurrentStale(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	batch := f.newTasks(t, mustTaskHosts(t, s, "SN-2", "SN-1")...)
	next, err := s.NewTemplateVersion(t.Context(), f.tpl.ID, f.tpl.Version, "author")
	must(t, err, "创建新版本")
	templateOps.to(t, s, next, model.VersionApproved)
	before := taskSnapshot(t, s)
	_, err = s.CreateCurrentTasks(t.Context(), batch, anAudit("task.create"))
	wantIs(t, err, repository.ErrTemplateNotCurrent, "选择已失效")
	wantIs(t, err, repository.ErrConflict, "保留冲突类别")
	wantTasksUnchanged(t, s, before, "旧选择被拒绝后")
}

func taskCurrentRejected(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	f.mustCreate(t, s, hs[1])
	before := taskSnapshot(t, s)
	_, err := s.CreateCurrentTasks(t.Context(), f.newTasks(t, hs...), anAudit("task.create"))
	var be *repository.BatchError
	if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) {
		t.Fatalf("主机被占用应报整批准入错误，得到 %v", err)
	}
	wantTasksUnchanged(t, s, before, "一般准入拒绝后")
}

func taskCurrentCreate(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	in := f.newTasks(t, mustTaskHosts(t, s, "SN-2", "SN-1")...)
	got, err := s.CreateCurrentTasks(t.Context(), in, anAudit("task.create"))
	must(t, err, "按当前选择建任务")
	wantEqual(t, len(got), 2, "任务数")
	for i, task := range got {
		want := in[i]
		want.ID, want.State, want.Rev = task.ID, model.TaskPending, 1
		if !model.ValidID(task.ID) {
			t.Fatal("任务编号不合法")
		}
		wantEqual(t, task, want, "返回顺序与默认字段")
		wantEqual(t, mustGetTask(t, s, task.ID), want, "落库结果")
	}
}

// 当前版本要求属于向导；普通仓储调用仍可明确指定旧的已审批版本。
func taskCreateOlderApproved(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	next, err := s.NewTemplateVersion(t.Context(), f.tpl.ID, f.tpl.Version, "author")
	must(t, err, "创建较新版本")
	templateOps.to(t, s, next, model.VersionApproved)
	before := len(auditEntries(t, s))
	created := f.mustCreate(t, s, mustTaskHosts(t, s, "SN-OLD")...)
	if len(created) != 1 || created[0].TemplateVersion != f.tpl.Version || created[0].Snapshot.Template.Version != f.tpl.Version {
		t.Fatalf("应保留明确选择的旧版本，得到 %+v", created)
	}
	wantEqual(t, len(taskPages(t, s, repository.TaskQuery{Limit: 500})[0]), 1, "只有一个任务")
	wantEqual(t, len(auditEntries(t, s))-before, 2, "登记主机与创建任务各一条审计")
}

// fixture 是建任务要用的一套记录：一个 ready 的 Rocky ISO、一个已审批的 kickstart 模板，以及它引用的已审批 hook。
type fixture struct {
	iso   model.ISO
	tpl   model.Template
	hooks map[model.HookKey]model.Hook // tpl 引用的 hook 版本
}

func newFixture(t *testing.T, s *pgstore.Store) fixture {
	t.Helper()
	lvm := hookOps.to(t, s, hookOps.mustCreate(t, s, "LVM"), model.VersionApproved)
	tpl := templateOps.mustCreateFrom(t, s, referencing(fullTemplate("Rocky-Base"), lvm.Key()))
	return fixture{
		iso:   mustReadyISO(t, s, rockyISO("Rocky 9.6"), 1),
		tpl:   templateOps.to(t, s, tpl, model.VersionApproved),
		hooks: map[model.HookKey]model.Hook{lvm.Key(): lvm},
	}
}

// taskHost 是建任务用的一台主机：模板参数 hostname 取自它的属性。
func taskHost(sn string) model.Host {
	h := fullHost(sn)
	h.Attrs["hostname"] = "node-1"
	return h
}

// mustTaskHosts 登记 sns 这几台主机（taskHost），按 sns 的顺序返回登记之后的记录。
func mustTaskHosts(t *testing.T, s *pgstore.Store, sns ...string) []model.Host {
	t.Helper()
	var hs []model.Host
	for _, sn := range sns {
		hs = append(hs, mustCreateHost(t, s, taskHost(sn)))
	}
	return hs
}

// newTasks 是给 hs 这几台主机各建一个任务的入参，顺序同 hs，主机不必已登记。快照由 snapshot.Build 按 f 构建，与第 5 层
// 建任务时走的是同一条路。
func (f fixture) newTasks(t *testing.T, hs ...model.Host) []model.Task {
	t.Helper()
	var ts []model.Task
	for _, h := range hs {
		snap, ps := coresnapshot.Build(params.Input{Template: f.tpl, Hooks: f.hooks, ISO: f.iso, Host: h})
		if len(ps) > 0 {
			t.Fatalf("构建 %s 的快照：%v", h.SN, ps)
		}
		ts = append(ts, model.Task{HostSN: h.SN, ISOID: f.iso.ID, TemplateID: f.tpl.ID, TemplateVersion: f.tpl.Version,
			Snapshot: snap, CreatedBy: "alice", CreatedAt: at0})
	}
	return ts
}

// mustCreate 给 hs 这几台主机建一批任务，返回建出的任务。
func (f fixture) mustCreate(t *testing.T, s *pgstore.Store, hs ...model.Host) []model.Task {
	t.Helper()
	got, err := s.CreateTasks(t.Context(), f.newTasks(t, hs...), anAudit("task.create"))
	must(t, err, fmt.Sprintf("给 %v 建任务", snsOf(hs)))
	return got
}

func mustGetTask(t *testing.T, s *pgstore.Store, id string) model.Task {
	t.Helper()
	got, err := s.GetTask(t.Context(), id)
	must(t, err, "读任务 "+id)
	return got
}

// mustTransition 把任务 task 从它现在的状态推进到 to，不带事件，返回推进之后的任务。
func mustTransition(t *testing.T, s *pgstore.Store, task model.Task, to model.TaskState) model.Task {
	t.Helper()
	got, _, err := s.TransitionTask(t.Context(), task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, to, nil)
	must(t, err, fmt.Sprintf("把任务 %s 从 %s 推进到 %s", task.ID, task.State, to))
	return got
}

// anEvent 是一条合规则的事件，key 可空。
func anEvent(kind, key string) repository.NewEvent {
	return repository.NewEvent{At: at0, Kind: kind, Key: key, Fields: map[string]string{"slot": "分区", "exit": "0"}}
}

// sortedTasks 返回按任务列表的顺序排好的 ts 的副本：CreatedAt 倒序，再按 ID 的字节序。
func sortedTasks(ts []model.Task) []model.Task {
	out := slices.Clone(ts)
	slices.SortFunc(out, func(a, b model.Task) int {
		return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	return out
}

// taskPages 用查询 q 从头翻到尾，返回每一页；游标由它填。
func taskPages(t *testing.T, s *pgstore.Store, q repository.TaskQuery) [][]model.Task {
	t.Helper()
	return allPages(t, func(cursor string) (repository.Page[model.Task], error) {
		q.Cursor = cursor
		return s.ListTasks(t.Context(), q)
	})
}

// allEvents 是任务 id 的全部事件，按 Seq 从上一次读到的最后一条续读到尾。没有事件的一次也要是长度为 0 的切片（见包说明）。
func allEvents(t *testing.T, s *pgstore.Store, id string) []repository.Event {
	t.Helper()
	var all []repository.Event
	for range maxPages {
		var after int64
		if len(all) > 0 {
			after = all[len(all)-1].Seq
		}
		es, err := s.ListEvents(t.Context(), id, after, 500)
		must(t, err, "列出任务 "+id+" 的事件")
		if es == nil {
			t.Fatalf("列出任务 %s 的事件：结果是 nil，没有事件时应是长度为 0 的切片", id)
		}
		if all = append(all, es...); len(es) < 500 {
			return all
		}
	}
	t.Fatalf("列出任务 %s 的事件：续读了 %d 次还没到头", id, maxPages)
	return nil
}

// taskContents 是经接口看得见的主机、ISO、任务、事件与审计，用来断言一次被拒绝或失败的写入什么都没改。
type taskContents struct {
	contents
	Tasks  []model.Task
	Events map[string][]repository.Event // 任务 ID → 它的全部事件
}

func taskSnapshot(t *testing.T, s *pgstore.Store) taskContents {
	t.Helper()
	c := taskContents{contents: snapshot(t, s), Events: map[string][]repository.Event{}}
	c.Tasks = slices.Concat(taskPages(t, s, repository.TaskQuery{Limit: 500})...)
	for _, task := range c.Tasks {
		c.Events[task.ID] = allEvents(t, s, task.ID)
	}
	return c
}

// wantTasksUnchanged 要求仓储里的记录与 before 相同：动作没有落库，审计也没有写。
func wantTasksUnchanged(t *testing.T, s *pgstore.Store, before taskContents, what string) {
	t.Helper()
	if after := taskSnapshot(t, s); !reflect.DeepEqual(after, before) {
		t.Fatalf("%s仓储不应有变化：\n之前 %+v\n之后 %+v", what, before, after)
	}
}

// wantSeqCounter 直接查任务 id 的事件计数 last_seq（它不是 model.Task 的字段，wantTasksUnchanged 看不到）：要等于 want，
// 并且事件恰好是 Seq 1 到 want。推进被拒、整个回滚之后，计数要留在推进之前；要是推进把它带走了（比如改成在事务外分配），
// 之后追加的事件就会留下空号，违反「每个任务从 1 起连续」（S6-1）。
func wantSeqCounter(t *testing.T, s *pgstore.Store, id string, want int64, what string) {
	t.Helper()
	var last, n, top int64
	err := s.DBForTest().WithContext(t.Context()).Raw(`SELECT last_seq, (SELECT count(*) FROM task_events WHERE task_id = ?), `+
		`(SELECT coalesce(max(seq), 0) FROM task_events WHERE task_id = ?) FROM tasks WHERE id = ?`, id, id, id).Row().Scan(&last, &n, &top)
	must(t, err, "查任务 "+id+" 的事件计数")
	if last != want || n != want || top != want {
		t.Fatalf("%s：任务 %s 的 last_seq 是 %d，有 %d 条事件、最大的 Seq 是 %d，三者都应为 %d", what, id, last, n, top, want)
	}
}

// taskAuditedOps 是任务里要审计的两个方法，由 runAudited 查（spec §6.2 第 8 条）。runAudited 的快照只看 hook、模板与审计，
// 所以每次调用都套上 unchangedOnError：调用失败时，任务、事件、主机与 ISO 也都没变。
func taskAuditedOps() []auditedOp {
	return []auditedOp{
		{"CreateTasks", func(t *testing.T, s *pgstore.Store) auditedCall {
			batch := newFixture(t, s).newTasks(t, mustTaskHosts(t, s, "SN-2", "SN-1")...)
			return unchangedOnError(t, s, func(a repository.Audit) ([]target, error) {
				got, err := s.CreateTasks(t.Context(), batch, a)
				var targets []target // 每个任务一条，按入参的顺序
				for _, task := range got {
					targets = append(targets, target{"task", task.ID})
				}
				return targets, err
			})
		}},
		{"CreateCurrentTasks", func(t *testing.T, s *pgstore.Store) auditedCall {
			batch := newFixture(t, s).newTasks(t, mustTaskHosts(t, s, "SN-2", "SN-1")...)
			return unchangedOnError(t, s, func(a repository.Audit) ([]target, error) {
				got, err := s.CreateCurrentTasks(t.Context(), batch, a)
				var targets []target // 每个任务一条，按入参的顺序
				for _, task := range got {
					targets = append(targets, target{"task", task.ID})
				}
				return targets, err
			})
		}},
		{"TransitionTaskByUser", func(t *testing.T, s *pgstore.Store) auditedCall {
			task := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
			expect := repository.TaskExpect{State: task.State, Rev: task.Rev}
			events := []repository.NewEvent{anEvent("task.cancel", "cancel-1")}
			return unchangedOnError(t, s, func(a repository.Audit) ([]target, error) {
				_, _, err := s.TransitionTaskByUser(t.Context(), task.ID, expect, model.TaskCancelled, events, a)
				return []target{{"task", task.ID}}, err
			})
		}},
	}
}

// unchangedOnError 给 call 套上一层：调用失败时，任务、事件、主机与 ISO 都要与调用之前相同。
func unchangedOnError(t *testing.T, s *pgstore.Store, call auditedCall) auditedCall {
	return func(a repository.Audit) ([]target, error) {
		before := taskSnapshot(t, s)
		targets, err := call(a)
		if err != nil {
			wantTasksUnchanged(t, s, before, "调用失败时")
		}
		return targets, err
	}
}

// batchProblems 把 BatchError 里的问题排成一列：先 Common，再按 SN 的顺序逐台。
func batchProblems(be *repository.BatchError) []model.Problem {
	ps := slices.Clone(be.Result.Common)
	for _, sn := range slices.Sorted(maps.Keys(be.Result.ByHost)) {
		ps = append(ps, be.Result.ByHost[sn]...)
	}
	return ps
}

func taskCreate(t *testing.T, s *pgstore.Store) {
	in := newFixture(t, s).newTasks(t, mustTaskHosts(t, s, "SN-2", "SN-1", "SN-3")...)
	in[1].CreatedAt, in[2].CreatedBy = at0+5, "张三" // 同一批里各任务的时间与建的人照各自的存
	got, err := s.CreateTasks(t.Context(), in, anAudit("task.create"))
	must(t, err, "建任务")
	if len(got) != len(in) {
		t.Fatalf("建了 %d 个任务，返回了 %d 个", len(in), len(got))
	}
	seen := map[string]bool{}
	for i, task := range got {
		if !model.ValidID(task.ID) || !model.IsSafeWord(task.ID) || seen[task.ID] {
			t.Fatalf("第 %d 个任务的 ID %q 应当合 model.ValidID、是安全词，并且同一批里互不相同", i+1, task.ID)
		}
		seen[task.ID] = true
		want := in[i] // 返回值的顺序同入参
		want.ID, want.State, want.Rev = task.ID, model.TaskPending, 1
		wantEqual(t, task, want, fmt.Sprintf("第 %d 个任务的返回值", i+1))
		wantEqual(t, mustGetTask(t, s, task.ID), want, fmt.Sprintf("读回来的第 %d 个任务", i+1))
	}
}

// 仓储不解释快照，写进去什么就读出什么（C29）：pgstore 把它存成 jsonb，nil 与空的 map、切片要分得开，嵌套的每一层都要在。
func taskCreateSnapshot(t *testing.T, s *pgstore.Store) {
	in := newFixture(t, s).newTasks(t, mustTaskHosts(t, s, "SN-1", "SN-2")...)
	a := &in[0].Snapshot
	a.Host.Attrs, a.ISO.Artifacts, a.Template.Params, a.Slots = nil, nil, nil, nil
	b := &in[1].Snapshot
	b.Host.Attrs, b.ISO.Artifacts, b.Template.Params = map[string]string{}, map[string]string{}, map[string]model.SnapshotValue{}
	b.Slots = []model.SnapshotSlot{
		{Seq: 1, Name: "分区"},
		{Seq: 2, Name: "装后配置", Exec: true, Timing: "post-target", Instances: []model.SnapshotInstance{}},
		{Seq: 3, Name: "首次开机", Exec: true, Timing: "firstboot", Instances: []model.SnapshotInstance{
			{HookID: "nic", HookVersion: 2, HookName: "NIC", Kind: model.ImplScript, Body: "#!/bin/sh\necho \"引号\" \\ 制表\t\x01\n",
				Interpreter: "sh", TimeoutSec: 900, Required: true, Digest: sha256Of(9), Params: map[string]model.SnapshotValue{
					"dns":   {Type: "ip", List: true, Items: []string{"10.0.0.53", "10.0.1.53"}},
					"empty": {Type: "text", List: true, Items: []string{}},
					"none":  {Type: "text"},
				}},
			{HookID: "empty", HookVersion: 1, Params: map[string]model.SnapshotValue{}},
			{HookID: "none", HookVersion: 1},
		}},
	}
	got, err := s.CreateTasks(t.Context(), in, anAudit("task.create"))
	must(t, err, "建任务")
	for i, task := range got {
		want := in[i]
		want.ID, want.State, want.Rev = task.ID, model.TaskPending, 1
		wantEqual(t, task, want, fmt.Sprintf("第 %d 个任务的返回值", i+1))
		wantEqual(t, mustGetTask(t, s, task.ID), want, fmt.Sprintf("读回来的第 %d 个任务", i+1))
	}
}

// C31 只挡未结束的任务：任务结束了（完成、失败或取消），就能再给这台主机建一个。
func taskCreateAfterFinished(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1")
	ended := 0
	for _, end := range model.TaskStates() { // model 认的结束状态逐个试，不另抄（S9-1）
		if !end.Finished() {
			continue
		}
		mustTransition(t, s, f.mustCreate(t, s, hs...)[0], end)
		ended++
	}
	f.mustCreate(t, s, hs...)
	wantEqual(t, len(slices.Concat(taskPages(t, s, repository.TaskQuery{HostSN: "SN-1"})...)), ended+1, "这台主机的任务数")
}

// 批量建任务的第 1 步：入参本身不合前提就报 ErrInvalid，什么都不写。每条前提一个反例，规则见 repository.CheckNewTasks。快照里
// 有存不下的文本也算（spec §3 总则）：pgstore 把快照存成 jsonb，固化下来的就不是写进去的那份（C29）。串藏在嵌套的结构、
// 切片与 map 的键里也要查出来，几种位置各一例。
func taskCreateInvalid(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	other := mustReadyISO(t, s, rockyISO("Other"), 2)
	before := taskSnapshot(t, s)
	at := func(i int, mod func(task *model.Task)) func([]model.Task) []model.Task { // 只改第 i 个
		return func(ts []model.Task) []model.Task { mod(&ts[i]); return ts }
	}
	all := func(mod func(task *model.Task)) func([]model.Task) []model.Task { // 每个都改
		return func(ts []model.Task) []model.Task {
			for i := range ts {
				mod(&ts[i])
			}
			return ts
		}
	}
	for _, c := range []struct {
		name string
		mod  func(ts []model.Task) []model.Task
	}{
		{"一个任务都没有", func([]model.Task) []model.Task { return nil }},
		{"空的一批", func([]model.Task) []model.Task { return []model.Task{} }},
		{"ID 不为空", at(1, func(task *model.Task) { task.ID = "abc" })},
		{"State 不为空", at(0, func(task *model.Task) { task.State = model.TaskPending })},
		{"Rev 不为 0", at(1, func(task *model.Task) { task.Rev = 1 })},
		{"CreatedAt 为 0", at(0, func(task *model.Task) { task.CreatedAt = 0 })},
		{"CreatedAt 为负", at(1, func(task *model.Task) { task.CreatedAt = -1 })},
		{"CreatedBy 为空", at(0, func(task *model.Task) { task.CreatedBy = "" })},
		{"CreatedBy 存不下", at(1, func(task *model.Task) { task.CreatedBy = "a\x00" })},
		{"快照格式不对", at(0, func(task *model.Task) { task.Snapshot.Schema++ })},
		{"HostSN 与快照不一致", at(0, func(task *model.Task) { task.Snapshot.Host.SN = "SN-2" })},
		{"ISOID 与快照不一致", at(1, func(task *model.Task) { task.Snapshot.ISO.ID = other.ID })},
		{"TemplateID 与快照不一致", at(0, func(task *model.Task) { task.Snapshot.Template.ID = "other" })},
		{"TemplateVersion 与快照不一致", at(1, func(task *model.Task) { task.Snapshot.Template.Version++ })},
		{"快照的正文含 NUL", at(0, func(task *model.Task) { task.Snapshot.Template.Body = "a\x00b" })},
		{"快照里属性的值不是 UTF-8", at(1, func(task *model.Task) { task.Snapshot.Host.Attrs = map[string]string{"rack": "\xff"} })},
		{"快照里属性的键不是 UTF-8", at(0, func(task *model.Task) { task.Snapshot.Host.Attrs = map[string]string{"\xff": "A1"} })},
		{"快照里实例参数的取值含 NUL", at(1, func(task *model.Task) {
			task.Snapshot.Slots = []model.SnapshotSlot{{Seq: 1, Name: "分区", Instances: []model.SnapshotInstance{{HookID: "lvm", HookVersion: 1,
				Params: map[string]model.SnapshotValue{"disk": {Type: "text", List: true, Items: []string{"sda", "a\x00"}}}}}}}
		})},
		{"HostSN 没有规范化", at(0, func(task *model.Task) { task.HostSN, task.Snapshot.Host.SN = "sn-1", "sn-1" })},
		{"ISOID 不合 ID 的格式", all(func(task *model.Task) { task.ISOID, task.Snapshot.ISO.ID = "ISO 1", "ISO 1" })},
		{"TemplateID 不合 ID 的格式", all(func(task *model.Task) { task.TemplateID, task.Snapshot.Template.ID = "tpl@1", "tpl@1" })},
		{"TemplateVersion 不到 1", all(func(task *model.Task) { task.TemplateVersion, task.Snapshot.Template.Version = 0, 0 })},
		{"同一批的 ISO 不同", at(1, func(task *model.Task) { task.ISOID, task.Snapshot.ISO.ID = other.ID, other.ID })},
		{"同一批的模板版本不同", at(1, func(task *model.Task) { task.TemplateVersion, task.Snapshot.Template.Version = 2, 2 })},
	} {
		_, err := s.CreateTasks(t.Context(), c.mod(f.newTasks(t, hs...)), anAudit("task.create"))
		wantIs(t, err, repository.ErrInvalid, c.name)
	}
	wantTasksUnchanged(t, s, before, "入参不合前提时")
}

func taskCreateDuplicate(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	before := taskSnapshot(t, s)
	dup := func(sn string) model.Problem {
		return model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: sn})
	}
	// 每个重复的 SN 报一条，按它第一次出现的顺序
	_, err := s.CreateTasks(t.Context(), f.newTasks(t, hs[1], hs[0], hs[1], hs[0], hs[1]), anAudit("task.create"))
	wantProblems(t, err, repository.ErrInvalid, []model.Problem{dup("SN-2"), dup("SN-1")}, "批里 SN 重复")
	wantTasksUnchanged(t, s, before, "批里 SN 重复时")
}

// 一次给多于一批（batchSize）的主机建任务：给主机加锁（lockHosts，按加锁的次序分批）与查已有的未结束任务（openTasksOf，
// 按入参的顺序分批）都分批做，这里两处都走到第二批。排在最后的两台按 SN 与按入参都落在第二批，它们已有未结束的任务：整批
// 被拦下，两台逐台报出 host_has_open_task（C30），只靠插入时撞上 idx_tasks_open 兜底是报不全的；两个任务结束之后，整批都
// 建上，每台一条审计。
func taskCreateManyBatches(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	n := pgstore.BatchSizeForTest + 2
	var in []model.Host
	for i := range n {
		in = append(in, taskHost(fmt.Sprintf("SN-%05d", i)))
	}
	hs := mustImport(t, s, in)
	busy := f.mustCreate(t, s, hs[n-2:]...)
	batch := f.newTasks(t, hs...)
	audits := len(auditEntries(t, s))
	_, err := s.CreateTasks(ctx, batch, anAudit("task.create"))
	var be *repository.BatchError
	if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) {
		t.Fatalf("有两台已有未结束的任务，应报 BatchError(ErrRejected)，得到 %v", err)
	}
	want := slices.Concat(admission.HostSelectable(hs[n-2], true), admission.HostSelectable(hs[n-1], true))
	wantEqual(t, batchProblems(be), want, "第二批里的两台都要报出")
	for _, task := range busy {
		mustTransition(t, s, task, model.TaskDone)
	}
	got, err := s.CreateTasks(ctx, batch, anAudit("task.create"))
	must(t, err, "那两台的任务结束之后再建")
	if len(got) != n {
		t.Fatalf("给 %d 台建任务，返回了 %d 个", n, len(got))
	}
	for i, task := range got {
		if task.HostSN != hs[i].SN || task.State != model.TaskPending {
			t.Fatalf("第 %d 个任务是 %s 的、状态 %s，应是 %s 的、pending", i+1, task.HostSN, task.State, hs[i].SN)
		}
	}
	if n := len(slices.Concat(taskPages(t, s, repository.TaskQuery{States: []model.TaskState{model.TaskPending}, Limit: 500})...)); n != len(got) {
		t.Fatalf("未结束的任务应恰好是这一批的 %d 个，得到 %d 个", len(got), n)
	}
	if added := len(auditEntries(t, s)) - audits; added != n { // 被拦下的那一次不写审计，系统推进也不写
		t.Fatalf("建上 %d 个任务应写 %d 条审计，写了 %d 条", n, n, added)
	}
}

// 向导里选中之后，ISO 被删了，或者模板草稿被删了：按键找不到，报 ErrNotFound，什么都不写。
func taskCreateMissing(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1")
	gone := f
	gone.iso = mustReadyISO(t, s, rockyISO("Gone"), 2)
	noISO := gone.newTasks(t, hs...)
	must(t, s.DeleteISO(ctx, gone.iso.ID, gone.iso.Rev, anAudit("iso.delete")), "删除 ISO")
	gone = f
	gone.tpl = templateOps.newVersionOf(t, s, f.tpl)
	noTemplate := gone.newTasks(t, hs...)
	must(t, s.DeleteTemplateDraft(ctx, model.TemplateKey{ID: gone.tpl.ID, Version: gone.tpl.Version}, gone.tpl.Rev), "删除模板草稿")
	before := taskSnapshot(t, s)
	_, err := s.CreateTasks(ctx, noISO, anAudit("task.create"))
	wantIs(t, err, repository.ErrNotFound, "ISO 不存在")
	_, err = s.CreateTasks(ctx, noTemplate, anAudit("task.create"))
	wantIs(t, err, repository.ErrNotFound, "模板版本不存在")
	wantTasksUnchanged(t, s, before, "找不到 ISO 或模板版本时")
}

// rejectedCases 是在事务里重跑准入被拦下的各种情形（C5、C22、C26、C28、C30、C31），每种一个用例：prepare 按 fixture 给
// SN-1、SN-2 备好一批，再改仓储里的数据，模拟向导筛过之后、提交之前别人做的改动。codes 是期望的问题代码，用来确认反例
// 造对了，比对见 wantRejected。
func rejectedCases() []testCase {
	type prepare func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task
	rejected := func(name string, codes []model.Code, prep prepare) testCase {
		return testCase{"建任务：重跑准入：" + name, func(t *testing.T, s *pgstore.Store) {
			f := newFixture(t, s)
			wantRejected(t, s, prep(t, s, f, mustTaskHosts(t, s, "SN-1", "SN-2")), codes)
		}}
	}
	deprecateISO := func(t *testing.T, s *pgstore.Store, f fixture) {
		iso := mustGetISO(t, s, f.iso.ID)
		must(t, s.DeprecateISO(t.Context(), iso.ID, iso.Rev, anAudit("iso.deprecate")), "废弃 ISO")
	}
	return []testCase{
		rejected("ISO 已废弃", []model.Code{"iso_not_ready"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs...)
			deprecateISO(t, s, f)
			return batch
		}),
		rejected("ISO 导入失败", []model.Code{"iso_import_failed"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs...)
			iso := mustGetISO(t, s, f.iso.ID)
			iso.State = model.ISOFailed
			mustUpdateISO(t, s, iso)
			return batch
		}),
		rejected("模板版本是草稿", []model.Code{"template_not_approved"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			f.tpl = templateOps.newVersionOf(t, s, f.tpl)
			return f.newTasks(t, hs...)
		}),
		rejected("模板版本已废弃", []model.Code{"template_not_approved"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs...)
			templateOps.to(t, s, f.tpl, model.VersionDeprecated)
			return batch
		}),
		rejected("模板引用的 hook 版本已废弃", []model.Code{"template_blocked_by_deprecated"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs...)
			for _, h := range f.hooks {
				hookOps.to(t, s, h, model.VersionDeprecated)
			}
			return batch
		}),
		rejected("主机没登记，或者已经被删了", []model.Code{"host_not_registered", "host_not_registered"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs[0], taskHost("SN-9"), hs[1])
			must(t, s.DeleteHost(t.Context(), "SN-2", hs[1].Rev, anAudit("host.delete")), "删除 SN-2")
			return batch
		}),
		rejected("主机已有未结束的任务", []model.Code{"host_has_open_task"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			f.mustCreate(t, s, hs[0])
			return f.newTasks(t, hs[1], hs[0])
		}),
		// 逐台列出原因（C30）：只报出第一台撞上的不够
		rejected("两台主机都有未结束的任务时逐台报出", []model.Code{"host_has_open_task", "host_has_open_task"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			f.mustCreate(t, s, hs...)
			return f.newTasks(t, hs[1], hs[0])
		}),
		rejected("主机登记的架构改了，与 ISO 的不符", []model.Code{"arch_mismatch"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs...)
			h := hs[1]
			h.Arch = "aarch64"
			_, err := s.UpdateHost(t.Context(), h, anAudit("host.update"))
			must(t, err, "改 SN-2 的架构")
			return batch
		}),
		rejected("ISO 与主机都有问题时一起报出", []model.Code{"iso_not_ready", "host_not_registered"}, func(t *testing.T, s *pgstore.Store, f fixture, hs []model.Host) []model.Task {
			batch := f.newTasks(t, hs[0], taskHost("SN-9"))
			deprecateISO(t, s, f)
			return batch
		}),
	}
}

// wantRejected 建 batch，要求在事务里重跑准入被拦下：BatchError 与对仓储现在的数据直接调用 repository.BatchProblems 的结果相同
// （判定规则只有第 1 层那一份，仓储要做的是把锁住之后读到的数据交给它），问题的代码依次是 codes（batchProblems 的顺序），
// 并且什么都没写，审计也没写。
func wantRejected(t *testing.T, s *pgstore.Store, batch []model.Task, codes []model.Code) {
	t.Helper()
	want := expectedBatch(t, s, batch)
	wantCodes(t, batchProblems(want), codes)
	before := taskSnapshot(t, s)
	_, err := s.CreateTasks(t.Context(), batch, anAudit("task.create"))
	var be *repository.BatchError
	if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) {
		t.Fatalf("应报 BatchError(ErrRejected)，得到 %v", err)
	}
	wantEqual(t, be.Result, want.Result, "重跑准入的结果")
	wantTasksUnchanged(t, s, before, "重跑准入没通过时")
}

// expectedBatch 是 batch 对仓储现在的数据跑 repository.BatchProblems 的结果：主机、它们有没有未结束的任务、ISO、模板版本与它引用
// 的 hook 版本都经接口读出，没登记的 SN 按批里的顺序列出。用例造的一批应当过不了准入。
func expectedBatch(t *testing.T, s *pgstore.Store, batch []model.Task) *repository.BatchError {
	t.Helper()
	ctx := t.Context()
	in := admission.BatchInput{OpenTask: map[string]bool{}}
	var missing []string
	for _, task := range batch {
		h, err := s.GetHost(ctx, task.HostSN)
		if errors.Is(err, repository.ErrNotFound) {
			missing = append(missing, task.HostSN)
			continue
		}
		must(t, err, "读主机 "+task.HostSN)
		_, open, err := s.FindOpenTask(ctx, task.HostSN)
		must(t, err, "找 "+task.HostSN+" 未结束的任务")
		in.Hosts, in.OpenTask[h.SN] = append(in.Hosts, h), open
	}
	in.ISO = mustGetISO(t, s, batch[0].ISOID)
	in.Template = templateOps.mustGet(t, s, batch[0].TemplateID, batch[0].TemplateVersion)
	hooks, err := s.GetHooks(ctx, repository.HookRefs(in.Template))
	must(t, err, "读模板引用的 hook 版本")
	in.Hooks = hooks
	var be *repository.BatchError
	if err := repository.BatchProblems(in, missing); !errors.As(err, &be) {
		t.Fatalf("用例造的一批应当过不了准入，BatchProblems 得到 %v", err)
	}
	return be
}

func taskTransition(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	task := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
	audits := auditEntries(t, s)
	events := []repository.NewEvent{anEvent("task.boot", "boot-1"), {At: at0 + 1, Kind: "slot.exit"}}
	got, appended, err := s.TransitionTask(ctx, task.ID, repository.TaskExpect{State: model.TaskPending, Rev: 1}, model.TaskBooting, events)
	must(t, err, "推进")
	want := task
	want.State, want.Rev = model.TaskBooting, 2
	wantEqual(t, got, want, "推进的返回值")
	wantEqual(t, mustGetTask(t, s, task.ID), want, "推进之后读回来的")
	wantEqual(t, appended, []repository.Appended{{Seq: 1}, {Seq: 2}}, "推进时追加的事件")
	wantEqual(t, allEvents(t, s, task.ID), []repository.Event{
		{TaskID: task.ID, Seq: 1, NewEvent: anEvent("task.boot", "boot-1")},
		{TaskID: task.ID, Seq: 2, NewEvent: repository.NewEvent{At: at0 + 1, Kind: "slot.exit"}},
	}, "事件与状态一起写下")
	// 一步推进合不合规矩由第 7 层的状态机判断（C33），仓储照写：推进到同一个状态、退回更早的状态、从结束的状态再改
	for _, to := range []model.TaskState{model.TaskBooting, model.TaskPending, model.TaskDone, model.TaskFailed} {
		next, appended, err := s.TransitionTask(ctx, want.ID, repository.TaskExpect{State: want.State, Rev: want.Rev}, to, nil)
		must(t, err, fmt.Sprintf("从 %s 推进到 %s", want.State, to))
		want.State, want.Rev = to, want.Rev+1
		wantEqual(t, next, want, "推进到 "+string(to)+" 的返回值")
		if appended == nil || len(appended) != 0 {
			t.Fatalf("不带事件的推进：结果应是长度为 0 的切片，得到 %#v", appended)
		}
	}
	wantEqual(t, mustGetTask(t, s, task.ID), want, "几次推进之后读回来的")
	wantItems(t, auditEntries(t, s), audits, "系统发起的推进不写审计")
}

// 条件推进（C33，spec §6.2 第 4 条）：当前的状态与 Rev 都等于期望值才写入，否则报 ErrConflict，调用方重读之后再判断。
func taskTransitionConflict(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	stale := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
	cur := mustTransition(t, s, stale, model.TaskBooting)
	before := taskSnapshot(t, s)
	for _, e := range []repository.TaskExpect{
		{State: stale.State, Rev: stale.Rev}, // 读过之后被别人推进了
		{State: cur.State, Rev: stale.Rev},   // 状态对上了，Rev 没对上
		{State: stale.State, Rev: cur.Rev},   // Rev 对上了，状态没对上
		{State: cur.State, Rev: cur.Rev + 1},
		{State: cur.State},
	} {
		_, _, err := s.TransitionTask(ctx, cur.ID, e, model.TaskInstalling, []repository.NewEvent{anEvent("task.install", "install-1")})
		wantIs(t, err, repository.ErrConflict, fmt.Sprintf("系统推进，期望 %+v", e))
		_, _, err = s.TransitionTaskByUser(ctx, cur.ID, e, model.TaskCancelled, nil, anAudit("task.cancel"))
		wantIs(t, err, repository.ErrConflict, fmt.Sprintf("用户推进，期望 %+v", e))
	}
	wantTasksUnchanged(t, s, before, "期望对不上时")
	wantSeqCounter(t, s, cur.ID, 0, "期望对不上之后")
}

// 目标状态不合法、带的事件有一条不合规则（规则同 AppendEvents），都报 ErrInvalid：整个推进不生效，状态不变，合规则的那几条
// 事件也不写。
func taskTransitionInvalid(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	task := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
	expect := repository.TaskExpect{State: task.State, Rev: task.Rev}
	before := taskSnapshot(t, s)
	for _, to := range []model.TaskState{"", "Pending", "started"} {
		_, _, err := s.TransitionTask(ctx, task.ID, expect, to, nil)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("系统推进到 %q", to))
		_, _, err = s.TransitionTaskByUser(ctx, task.ID, expect, to, nil, anAudit("task.cancel"))
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("用户推进到 %q", to))
	}
	for _, bad := range []repository.NewEvent{
		{Kind: "task.boot"},                      // 没有时间
		{At: at0, Kind: "boot"},                  // Kind 不到两段
		{At: at0, Kind: "task.boot", Key: "a b"}, // Key 不是安全词
		{At: at0, Kind: "task.boot", Fields: map[string]string{"Exit": "0"}},
		{At: at0, Kind: "task.boot", Fields: map[string]string{"log": "a\x00b"}},
	} {
		es := []repository.NewEvent{anEvent("task.boot", "boot-1"), bad}
		_, _, err := s.TransitionTask(ctx, task.ID, expect, model.TaskBooting, es)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("系统推进，带了事件 %+v", bad))
		_, _, err = s.TransitionTaskByUser(ctx, task.ID, expect, model.TaskCancelled, es, anAudit("task.cancel"))
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("用户推进，带了事件 %+v", bad))
	}
	wantTasksUnchanged(t, s, before, "推进不合规则时")
}

func taskTransitionMissing(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...) // 有任务，只是不是要推进的那个
	before := taskSnapshot(t, s)
	expect := repository.TaskExpect{State: model.TaskPending, Rev: 1}
	_, _, err := s.TransitionTask(ctx, "nosuchtask", expect, model.TaskBooting, nil)
	wantIs(t, err, repository.ErrNotFound, "系统推进不存在的任务")
	_, _, err = s.TransitionTaskByUser(ctx, "nosuchtask", expect, model.TaskCancelled, nil, anAudit("task.cancel"))
	wantIs(t, err, repository.ErrNotFound, "用户推进不存在的任务")
	for _, id := range []string{"", "Bad", "a@1", "-a", "a.b", "a/b"} {
		_, _, err := s.TransitionTask(ctx, id, expect, model.TaskBooting, nil)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("系统推进 %q", id))
		_, _, err = s.TransitionTaskByUser(ctx, id, expect, model.TaskCancelled, nil, anAudit("task.cancel"))
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("用户推进 %q", id))
	}
	wantTasksUnchanged(t, s, before, "找不到任务时")
}

// C31：一台主机同一时刻最多一个未结束的任务。失败的任务重试回 pending 时，期间又给这台主机建了新任务，就会撞上；系统发起的
// 推进同样挡住，问题与准入判定报的相同。推进到结束的状态不受这条约束；新任务结束之后，就能推进回未结束的状态。
func taskTransitionOpenTask(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1")
	failed := mustTransition(t, s, f.mustCreate(t, s, hs...)[0], model.TaskFailed)
	newer := f.mustCreate(t, s, hs...)[0] // 失败的任务已结束，可以再建
	before := taskSnapshot(t, s)
	want := admission.HostSelectable(hs[0], true)
	expect := repository.TaskExpect{State: failed.State, Rev: failed.Rev}
	_, _, err := s.TransitionTaskByUser(ctx, failed.ID, expect, model.TaskPending, []repository.NewEvent{anEvent("task.retry", "")}, anAudit("task.retry"))
	wantProblems(t, err, repository.ErrRejected, want, "重试")
	for _, to := range []model.TaskState{model.TaskPending, model.TaskBooting, model.TaskInstalling} {
		_, _, err := s.TransitionTask(ctx, failed.ID, expect, to, nil)
		wantProblems(t, err, repository.ErrRejected, want, "系统推进到 "+string(to))
	}
	wantTasksUnchanged(t, s, before, "撞上未结束的任务时")
	// 重试带着一条事件：它与计数在同一个事务里写，撞上 C31 时一起回滚，计数不被推进带走
	wantSeqCounter(t, s, failed.ID, 0, "重试撞上未结束的任务之后")

	cancelled, _, err := s.TransitionTaskByUser(ctx, failed.ID, expect, model.TaskCancelled, nil, anAudit("task.cancel"))
	must(t, err, "另有未结束的任务时，推进到结束的状态")
	mustTransition(t, s, newer, model.TaskDone)
	if got := mustTransition(t, s, cancelled, model.TaskPending); got.State != model.TaskPending {
		t.Fatalf("新任务结束之后应能推进回 pending，得到 %s", got.State)
	}
}

func taskGet(t *testing.T, s *pgstore.Store) {
	task := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
	wantEqual(t, mustGetTask(t, s, task.ID), task, "读回来的")
	_, err := s.GetTask(t.Context(), "nosuchtask")
	wantIs(t, err, repository.ErrNotFound, "读不存在的任务")
	for _, id := range []string{"", "Bad", "a@1", "-a", "a.b", "a/b"} {
		_, err := s.GetTask(t.Context(), id)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("GetTask(%q)", id))
	}
}

// FindOpenTask 给第 7 层引导时按 SN 找任务（C32）。SN 没规范化是调用方漏了规范化，报 ErrInvalid：当成「没有任务」，机器
// 就会从本地盘启动，这台主机的任务却一直等着。
func taskFindOpen(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2")
	find := func(sn string, want model.Task, wantFound bool, what string) {
		t.Helper()
		got, found, err := s.FindOpenTask(ctx, sn)
		must(t, err, what)
		if found != wantFound {
			t.Fatalf("%s：找到与否应为 %v，得到 %v", what, wantFound, found)
		}
		wantEqual(t, got, want, what)
	}
	find("SN-1", model.Task{}, false, "还没有任务")
	find("SN-9", model.Task{}, false, "没登记的主机")
	for _, end := range model.TaskStates() { // model 认的结束状态逐个试，不另抄（S9-1）
		if !end.Finished() {
			continue
		}
		task := f.mustCreate(t, s, hs[0])[0]
		find("SN-1", task, true, "刚建好的任务")
		find("SN-2", model.Task{}, false, "别的主机")
		task = mustTransition(t, s, task, model.TaskInstalling)
		find("SN-1", task, true, "推进之后，给出推进之后的")
		mustTransition(t, s, task, end)
		find("SN-1", model.Task{}, false, "任务刚结束为 "+string(end))
	}
	// 存不下的 SN（含 NUL，或不是合法的 UTF-8）也不是规范化之后的写法：照样报 ErrInvalid，不当成「没有任务」，也不报数据库的
	// 编码错误（store 的包说明）
	for _, sn := range []string{"sn-1", " SN-1", "SN-1\n", "", "SN-1\x00", "SN-1\xff"} {
		_, _, err := s.FindOpenTask(ctx, sn)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("FindOpenTask(%q)", sn))
	}
}

func taskListPaging(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	var want []model.Task
	// 有几个时间相同：同一时刻建的再按 ID 排，页的边界会落在它们中间
	for i, at := range []int64{300, 100, 300, 200, 300, 100, 50} {
		in := f.newTasks(t, mustTaskHosts(t, s, fmt.Sprintf("SN-%d", i))...)
		in[0].CreatedAt = at
		got, err := s.CreateTasks(t.Context(), in, anAudit("task.create"))
		must(t, err, "建任务")
		want = append(want, got...)
	}
	want = sortedTasks(want)
	whole, err := s.ListTasks(t.Context(), repository.TaskQuery{Limit: len(want)})
	must(t, err, "一次全取")
	wantItems(t, whole.Items, want, "一次全取")
	if whole.Next != "" {
		t.Fatalf("恰好取完时 Next 应为空，得到 %q", whole.Next)
	}
	for _, limit := range []int{2, 3} {
		wantPaged(t, taskPages(t, s, repository.TaskQuery{Limit: limit}), limit, want, fmt.Sprintf("每页 %d 条", limit))
	}
}

func taskListFilters(t *testing.T, s *pgstore.Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2", "SN-3")
	create := func(h model.Host, at int64) model.Task {
		in := f.newTasks(t, h)
		in[0].CreatedAt = at
		got, err := s.CreateTasks(t.Context(), in, anAudit("task.create"))
		must(t, err, "建任务")
		return got[0]
	}
	a := mustTransition(t, s, create(hs[0], 100), model.TaskFailed)
	b := mustTransition(t, s, create(hs[0], 200), model.TaskDone)
	c := create(hs[0], 300)
	d := mustTransition(t, s, create(hs[1], 200), model.TaskInstalling)
	e := create(hs[2], 400)
	for _, q := range []struct {
		name string
		q    repository.TaskQuery
		want []model.Task
	}{
		{"不过滤", repository.TaskQuery{}, []model.Task{a, b, c, d, e}},
		{"按主机", repository.TaskQuery{HostSN: "SN-1"}, []model.Task{a, b, c}},
		{"主机要完全相等", repository.TaskQuery{HostSN: "SN"}, nil},
		{"按一个状态", repository.TaskQuery{States: []model.TaskState{model.TaskPending}}, []model.Task{c, e}},
		{"按几个状态", repository.TaskQuery{States: []model.TaskState{model.TaskFailed, model.TaskDone, model.TaskBooting}}, []model.Task{a, b}},
		{"时间范围含两端", repository.TaskQuery{CreatedFrom: 200, CreatedTo: 300}, []model.Task{b, c, d}},
		{"只给起点", repository.TaskQuery{CreatedFrom: 300}, []model.Task{c, e}},
		{"只给终点", repository.TaskQuery{CreatedTo: 100}, []model.Task{a}},
		{"几个条件同时给", repository.TaskQuery{HostSN: "SN-1", States: []model.TaskState{model.TaskPending, model.TaskFailed}, CreatedTo: 250}, []model.Task{a}},
		{"没有匹配的", repository.TaskQuery{HostSN: "SN-9"}, nil},
		// 空的切片与 nil 一样不过滤：第 5 层把 JSON 的空数组解出来就是它，当成「状态在空集合里」就一条都不给了
		{"状态给空切片等于不过滤", repository.TaskQuery{States: []model.TaskState{}}, []model.Task{a, b, c, d, e}},
	} {
		q.q.Limit = 1 // 一条一页：翻页时过滤也要对
		wantItems(t, slices.Concat(taskPages(t, s, q.q)...), sortedTasks(q.want), q.name)
	}
	// 存不下的过滤条件谁都不是（store 的包说明）：当作没有，得到空的一页，不报数据库的编码错误
	for _, bad := range []string{"\x00", "\xff"} {
		for _, q := range []repository.TaskQuery{
			{HostSN: "SN-1" + bad}, {States: []model.TaskState{model.TaskState("pending" + bad)}},
			{HostSN: "SN-1", States: []model.TaskState{model.TaskState(bad)}},
		} {
			wantItems(t, slices.Concat(taskPages(t, s, q)...), nil, fmt.Sprintf("ListTasks(%#v)", q))
		}
	}
}

func taskListDefaultLimit(t *testing.T, s *pgstore.Store) {
	var in []model.Host
	for i := range defaultPageSize + 1 {
		in = append(in, taskHost(fmt.Sprintf("SN-%03d", i)))
	}
	newFixture(t, s).mustCreate(t, s, mustImport(t, s, in)...) // 一批建出 101 个任务
	wantDefaultLimit(t, slices.Concat(taskPages(t, s, repository.TaskQuery{Limit: 500})...), func(cursor string) (repository.Page[model.Task], error) {
		return s.ListTasks(t.Context(), repository.TaskQuery{Cursor: cursor})
	})
}

func taskListInvalid(t *testing.T, s *pgstore.Store) {
	newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)
	// 排序键为 null 的游标是伪造的：收下的话 created_at < 0，翻出空的一页、Next 为空，调用方以为翻到头了
	for _, q := range []repository.TaskQuery{{Limit: 501}, {Limit: -1}, {Cursor: "\xff"}, {Cursor: "这不是游标"},
		{Cursor: repository.EncodeCursor("tasks", nil, nil)}} {
		_, err := s.ListTasks(t.Context(), q)
		wantIs(t, err, repository.ErrInvalid, fmt.Sprintf("Limit %d、Cursor %q", q.Limit, q.Cursor))
	}
}

// taskCopies：改了入参或返回值里快照的 map、切片与它们里面的元素，仓储里的数据都不变（包说明里的约定）。
func taskCopies(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1")
	mutate := func(task model.Task) {
		snap := &task.Snapshot
		snap.Host.Attrs["hostname"] = "改了"
		snap.ISO.Artifacts["kernel"] = "改了"
		snap.Template.Params["hostname"].Items[0] = "改了"
		snap.Template.Params["extra"] = model.SnapshotValue{}
		snap.Slots[0].Name = "改了"
		inst := &snap.Slots[0].Instances[0]
		inst.Body = "改了"
		inst.Params["disk"].Items[0] = "改了"
	}
	in := f.newTasks(t, hs...)
	got, err := s.CreateTasks(ctx, in, anAudit("task.create"))
	must(t, err, "建任务")
	want := f.newTasks(t, hs...)[0] // 同样的入参再构建一份：快照构建对同样的输入给出同样的结果
	want.ID, want.State, want.Rev = got[0].ID, model.TaskPending, 1
	mutate(in[0])
	mutate(got[0])
	wantEqual(t, mustGetTask(t, s, want.ID), want, "改了建任务的入参与返回值之后")

	mutate(mustGetTask(t, s, want.ID))
	open, _, err := s.FindOpenTask(ctx, "SN-1")
	must(t, err, "找未结束的任务")
	mutate(open)
	p, err := s.ListTasks(ctx, repository.TaskQuery{})
	must(t, err, "列出")
	mutate(p.Items[0])
	moved, _, err := s.TransitionTask(ctx, want.ID, repository.TaskExpect{State: model.TaskPending, Rev: 1}, model.TaskBooting, nil)
	must(t, err, "推进")
	mutate(moved)
	want.State, want.Rev = model.TaskBooting, 2
	wantEqual(t, mustGetTask(t, s, want.ID), want, "改了读到的与推进的返回值之后")
}
