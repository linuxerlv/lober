package pgstore_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// Test契约事件 是事件这一组：任务的追加式日志。Seq 按任务从 1 起连续分配，Key 用来去重（第 7 层收到重发的信号时据此只落
// 一条），推进时带的事件与 AppendEvents 共用同一套规则。事件不审计。并发见 Test契约并发。
func Test契约事件(t *testing.T) {
	runCases(t, []testCase{
		{"Seq 每个任务从 1 起连续，不同任务互不影响，读回来与写进去的相同，不写审计", eventSeq},
		{"同一个 Key 再追加报告 Duplicate，Seq 是原来那条的，不新增；同一批里重复的也一样", eventDuplicate},
		{"同一个 Key 在不同任务里可以各用一次", eventKeyPerTask},
		{"一次追加多于一批（batchSize）：分批写入与分批查已有的 Key，第二批的照样写上，原样重发照样认出", eventManyBatches},
		{"推进时带的事件与 AppendEvents 共用 Seq 与 Key；结束了的任务照样能追加", eventWithTransition},
		{"事件不合法报 ErrInvalid，整批不写", eventInvalid},
		{"任务不存在报 ErrNotFound，ID 不合格式报 ErrInvalid", eventMissingTask},
		{"空的一批什么都不写，结果是长度为 0 的切片", eventEmpty},
		{"ListEvents 只给 Seq 大于 afterSeq 的，limit 的规则同分页", eventList},
		{"Fields 原样存下：nil 与空 map 读回来不变，返回值与入参都是拷贝", eventFields},
	})
}

// keyed 是一条带 Key 的事件。
func keyed(key string) store.NewEvent {
	return store.NewEvent{At: at0, Kind: "signal.recv", Key: key}
}

// mustAppend 给任务 id 追加 es，返回每一条的结果。
func mustAppend(t *testing.T, s *pgstore.Store, id string, es ...store.NewEvent) []store.Appended {
	t.Helper()
	got, err := s.AppendEvents(t.Context(), id, es)
	must(t, err, "给任务 "+id+" 追加事件")
	return got
}

// mustTask 建一个任务，主机是 SN-1。
func mustTask(t *testing.T, s *pgstore.Store) model.Task {
	t.Helper()
	return newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1")...)[0]
}

func eventSeq(t *testing.T, s *pgstore.Store) {
	ts := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1", "SN-2")...)
	a, b := ts[0].ID, ts[1].ID
	audits := auditEntries(t, s)
	ev := func(i int) store.NewEvent {
		return store.NewEvent{At: at0 + int64(i), Kind: "slot.exit", Fields: map[string]string{"n": strconv.Itoa(i)}}
	}
	wantEqual(t, mustAppend(t, s, a, ev(1), ev(2), ev(3)), []store.Appended{{Seq: 1}, {Seq: 2}, {Seq: 3}}, "第一批")
	wantEqual(t, mustAppend(t, s, b, ev(4), ev(5)), []store.Appended{{Seq: 1}, {Seq: 2}}, "另一个任务从 1 起")
	wantEqual(t, mustAppend(t, s, a, ev(6)), []store.Appended{{Seq: 4}}, "接着这个任务的上一批")
	wantEqual(t, allEvents(t, s, a), []store.Event{
		{TaskID: a, Seq: 1, NewEvent: ev(1)}, {TaskID: a, Seq: 2, NewEvent: ev(2)},
		{TaskID: a, Seq: 3, NewEvent: ev(3)}, {TaskID: a, Seq: 4, NewEvent: ev(6)},
	}, "读回来的事件")
	wantEqual(t, allEvents(t, s, b), []store.Event{{TaskID: b, Seq: 1, NewEvent: ev(4)}, {TaskID: b, Seq: 2, NewEvent: ev(5)}}, "另一个任务的事件")
	wantItems(t, auditEntries(t, s), audits, "事件不写审计")
}

// 第 7 层收到重发的信号时，同一个 Key 只落一条：已存在的那条跳过，报告为 Duplicate，Seq 是原来那条的，内容不同也不覆盖；
// 同一批里新的照常写。同一批里 Key 重复，后一条按「Key 已存在」处理。没有 Key 的不去重；Key 区分大小写。
func eventDuplicate(t *testing.T, s *pgstore.Store) {
	id := mustTask(t, s).ID
	first := store.NewEvent{At: at0, Kind: "signal.recv", Key: "sig-1", Fields: map[string]string{"slot": "分区"}}
	wantEqual(t, mustAppend(t, s, id, first, anEvent("slot.exit", ""), keyed("sig-2")), []store.Appended{{Seq: 1}, {Seq: 2}, {Seq: 3}}, "第一批")
	again := first
	again.At, again.Fields = at0+9, map[string]string{"slot": "重发"}
	wantEqual(t, mustAppend(t, s, id, again, keyed("sig-3")), []store.Appended{{Seq: 1, Duplicate: true}, {Seq: 4}}, "重发的信号")
	wantEqual(t, mustAppend(t, s, id, keyed("sig-4"), keyed("sig-4"), keyed("sig-2")),
		[]store.Appended{{Seq: 5}, {Seq: 5, Duplicate: true}, {Seq: 3, Duplicate: true}}, "同一批里 Key 重复")
	wantEqual(t, mustAppend(t, s, id, anEvent("slot.exit", ""), anEvent("slot.exit", "")), []store.Appended{{Seq: 6}, {Seq: 7}}, "没有 Key 的")
	wantEqual(t, mustAppend(t, s, id, keyed("SIG-1")), []store.Appended{{Seq: 8}}, "Key 区分大小写")
	got := allEvents(t, s, id)
	if len(got) != 8 {
		t.Fatalf("应有 8 条事件，得到 %d 条", len(got))
	}
	wantEqual(t, got[0].NewEvent, first, "Key 重复时原来那条不变")
}

func eventKeyPerTask(t *testing.T, s *pgstore.Store) {
	ts := newFixture(t, s).mustCreate(t, s, mustTaskHosts(t, s, "SN-1", "SN-2")...)
	for _, task := range ts {
		wantEqual(t, mustAppend(t, s, task.ID, keyed("sig-1")), []store.Appended{{Seq: 1}}, "任务 "+task.ID+" 第一次用 sig-1")
	}
	wantEqual(t, mustAppend(t, s, ts[0].ID, keyed("sig-1")), []store.Appended{{Seq: 1, Duplicate: true}}, "同一个任务再用一次")
}

// 一次追加的条数不设上限（store.CheckEvents 只查每一条），第 7 层一次补报的信号可能多于一条语句写得下的一批（batchSize）：
// 写入按入参的次序分批，查已有的 Key 按 Key 的字节序分批，这里多出的一条在两边都落在第二批（sig-1000 排在最后）。Seq 照样
// 从 1 起连续；原样重发一次，每一条都报告为 Duplicate、Seq 是原来那条的，一条都不新增，计数也不动。
func eventManyBatches(t *testing.T, s *pgstore.Store) {
	id := mustTask(t, s).ID
	n := pgstore.BatchSizeForTest + 1
	in := make([]store.NewEvent, n)
	first, again := make([]store.Appended, n), make([]store.Appended, n)
	want := make([]store.Event, n)
	for i := range n {
		seq := int64(i + 1)
		in[i] = store.NewEvent{At: at0 + seq, Kind: "signal.recv", Key: fmt.Sprintf("sig-%04d", i)}
		first[i], again[i] = store.Appended{Seq: seq}, store.Appended{Seq: seq, Duplicate: true}
		want[i] = store.Event{TaskID: id, Seq: seq, NewEvent: in[i]}
	}
	wantEqual(t, mustAppend(t, s, id, in...), first, "第一次追加")
	wantEqual(t, allEvents(t, s, id), want, "读回来的事件")
	wantEqual(t, mustAppend(t, s, id, in...), again, "原样重发")
	wantSeqCounter(t, s, id, int64(n), "原样重发之后")
	wantEqual(t, allEvents(t, s, id), want, "原样重发之后读回来的事件")
	wantEqual(t, mustAppend(t, s, id, keyed("sig-new")), []store.Appended{{Seq: int64(n + 1)}}, "重发之后追加新的")
}

// 推进时带的事件与 AppendEvents 是同一份日志：Seq 接着分配，Key 互相去重。带的事件里有已存在的 Key，照样推进，那条报告为
// Duplicate。结束了的任务照样能追加：迟到的信号只记为事件（spec §5.4 第 5 步）。
func eventWithTransition(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	id := mustTask(t, s).ID
	_, appended, err := s.TransitionTask(ctx, id, store.TaskExpect{State: model.TaskPending, Rev: 1}, model.TaskBooting,
		[]store.NewEvent{keyed("boot-1"), anEvent("task.boot", "")})
	must(t, err, "推进")
	wantEqual(t, appended, []store.Appended{{Seq: 1}, {Seq: 2}}, "推进时带的事件")
	wantEqual(t, mustAppend(t, s, id, keyed("boot-1"), keyed("sig-1")), []store.Appended{{Seq: 1, Duplicate: true}, {Seq: 3}}, "之后追加的")
	moved, appended, err := s.TransitionTaskByUser(ctx, id, store.TaskExpect{State: model.TaskBooting, Rev: 2}, model.TaskCancelled,
		[]store.NewEvent{keyed("sig-1"), keyed("cancel-1")}, anAudit("task.cancel"))
	must(t, err, "带着已存在的 Key 推进")
	wantEqual(t, appended, []store.Appended{{Seq: 3, Duplicate: true}, {Seq: 4}}, "带着已存在的 Key 推进时的事件")
	wantEqual(t, moved.State, model.TaskCancelled, "带着已存在的 Key 推进之后的状态")
	wantEqual(t, mustAppend(t, s, id, keyed("late-1")), []store.Appended{{Seq: 5}}, "给已结束的任务追加")
}

// 一批里有一条不合规则（store.CheckEvents），整批都不写：同一批里合规则的那几条也不写。
func eventInvalid(t *testing.T, s *pgstore.Store) {
	id := mustTask(t, s).ID
	mustAppend(t, s, id, keyed("sig-1"))
	before := taskSnapshot(t, s)
	at := func(e store.NewEvent) store.NewEvent { e.At = at0; return e }
	for _, c := range []struct {
		name string
		e    store.NewEvent
	}{
		{"没有时间", store.NewEvent{Kind: "slot.exit"}},
		{"时间为负", store.NewEvent{At: -1, Kind: "slot.exit"}},
		{"Kind 为空", at(store.NewEvent{})},
		{"Kind 不到两段", at(store.NewEvent{Kind: "exit"})},
		{"Kind 有大写", at(store.NewEvent{Kind: "Slot.exit"})},
		{"Key 不是安全词", at(store.NewEvent{Kind: "slot.exit", Key: "sig 1"})},
		{"Key 超过 128 字节", at(store.NewEvent{Kind: "slot.exit", Key: strings.Repeat("k", 129)})},
		{"字段名不是标识符", at(store.NewEvent{Kind: "slot.exit", Fields: map[string]string{"Exit": "0"}})},
		{"字段值含 NUL", at(store.NewEvent{Kind: "slot.log", Fields: map[string]string{"log": "a\x00b"}})},
		{"字段值不是 UTF-8", at(store.NewEvent{Kind: "slot.log", Fields: map[string]string{"log": "\xff"}})},
		{"字段合计超过 64 KiB", at(store.NewEvent{Kind: "slot.log", Fields: map[string]string{"log": strings.Repeat("x", 64<<10)}})},
	} {
		_, err := s.AppendEvents(t.Context(), id, []store.NewEvent{keyed("sig-2"), c.e})
		wantIs(t, err, store.ErrInvalid, c.name)
	}
	wantTasksUnchanged(t, s, before, "事件不合法时")
	// 键与值合计恰好 64 KiB 的能写
	full := at(store.NewEvent{Kind: "slot.log", Fields: map[string]string{"log": strings.Repeat("x", 64<<10-len("log"))}})
	wantEqual(t, mustAppend(t, s, id, full), []store.Appended{{Seq: 2}}, "合计恰好 64 KiB")
}

// 任务不存在报 ErrNotFound，空的一批也一样：调用方拿错了任务 ID，不能当成追加成功。
func eventMissingTask(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	mustTask(t, s) // 有任务，只是不是要找的那个
	before := taskSnapshot(t, s)
	for _, es := range [][]store.NewEvent{{keyed("sig-1")}, nil} {
		_, err := s.AppendEvents(ctx, "nosuchtask", es)
		wantIs(t, err, store.ErrNotFound, fmt.Sprintf("给不存在的任务追加 %d 条", len(es)))
	}
	_, err := s.ListEvents(ctx, "nosuchtask", 0, 0)
	wantIs(t, err, store.ErrNotFound, "列出不存在的任务的事件")
	for _, id := range []string{"", "Bad", "a@1", "-a", "a.b", "a/b"} {
		_, err := s.AppendEvents(ctx, id, []store.NewEvent{keyed("sig-1")})
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("AppendEvents(%q)", id))
		_, err = s.ListEvents(ctx, id, 0, 0)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("ListEvents(%q)", id))
	}
	wantTasksUnchanged(t, s, before, "找不到任务时")
}

// 结果是长度为 0 的切片，不是 nil（见包说明）：不带事件的追加与推进、没有事件可列的 ListEvents 都是。
func eventEmpty(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	id := mustTask(t, s).ID
	before := taskSnapshot(t, s)
	for _, es := range [][]store.NewEvent{nil, {}} {
		got, err := s.AppendEvents(ctx, id, es)
		must(t, err, "追加空的一批")
		if got == nil || len(got) != 0 {
			t.Fatalf("追加空的一批：结果应是长度为 0 的切片，得到 %#v", got)
		}
	}
	wantTasksUnchanged(t, s, before, "追加空的一批")
	es, err := s.ListEvents(ctx, id, 0, 0)
	must(t, err, "列出事件")
	if es == nil || len(es) != 0 {
		t.Fatalf("没有事件时应是长度为 0 的切片，得到 %#v", es)
	}
}

// ListEvents 是第 5 层按事件序号补读用的：只给 Seq 大于 afterSeq 的，按 Seq 升序，最多 limit 条。limit 为 0 时取 100，
// 超过 500 或为负报 ErrInvalid；afterSeq 为负也报 ErrInvalid。
func eventList(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	id := mustTask(t, s).ID
	var in []store.NewEvent
	for i := range 7 {
		in = append(in, store.NewEvent{At: at0 + int64(i), Kind: "slot.exit", Key: fmt.Sprintf("sig-%d", i)})
	}
	mustAppend(t, s, id, in...)
	all := allEvents(t, s, id)
	for _, c := range []struct {
		after int64
		limit int
		want  []store.Event
	}{
		{0, 3, all[0:3]}, {3, 3, all[3:6]}, {6, 3, all[6:]}, {7, 3, nil}, {100, 0, nil}, {2, 0, all[2:]}, {0, 7, all},
	} {
		got, err := s.ListEvents(ctx, id, c.after, c.limit)
		must(t, err, fmt.Sprintf("afterSeq %d、limit %d", c.after, c.limit))
		if got == nil {
			t.Fatalf("afterSeq %d、limit %d：结果是 nil，没有事件时应是长度为 0 的切片", c.after, c.limit)
		}
		wantItems(t, got, c.want, fmt.Sprintf("afterSeq %d、limit %d", c.after, c.limit))
	}
	var more []store.NewEvent
	for range 94 {
		more = append(more, anEvent("slot.log", ""))
	}
	mustAppend(t, s, id, more...) // 共 101 条
	first, err := s.ListEvents(ctx, id, 0, 0)
	must(t, err, "limit 为 0")
	rest, err := s.ListEvents(ctx, id, first[len(first)-1].Seq, 0)
	must(t, err, "limit 为 0 的第二次")
	if len(first) != 100 || len(rest) != 1 || rest[0].Seq != 101 {
		t.Fatalf("limit 为 0 时应取 100 条：第一次 %d 条，第二次 %d 条", len(first), len(rest))
	}
	whole, err := s.ListEvents(ctx, id, 0, 500)
	must(t, err, "limit 为 500")
	wantEqual(t, len(whole), 101, "limit 为 500 时取到的条数")
	for _, c := range []struct {
		after int64
		limit int
	}{{0, 501}, {0, -1}, {-1, 0}} {
		_, err := s.ListEvents(ctx, id, c.after, c.limit)
		wantIs(t, err, store.ErrInvalid, fmt.Sprintf("afterSeq %d、limit %d", c.after, c.limit))
	}
}

// Fields 写进去什么就读出什么：nil 读回来是 nil，空 map 读回来是空 map；改了入参或返回值里的 Fields，仓储里的不变。
func eventFields(t *testing.T, s *pgstore.Store) {
	ctx := t.Context()
	id := mustTask(t, s).ID
	events := func() []store.NewEvent {
		return []store.NewEvent{
			{At: at0, Kind: "slot.exit"},
			{At: at0, Kind: "slot.exit", Fields: map[string]string{}},
			{At: at0, Kind: "slot.log", Fields: map[string]string{"log": "第一行\n\t\"引号\" \\ 反斜杠\x01", "exit": "0"}},
		}
	}
	var want []store.Event
	for i, e := range events() {
		want = append(want, store.Event{TaskID: id, Seq: int64(i + 1), NewEvent: e})
	}
	in := events()
	mustAppend(t, s, id, in...)
	in[1].Fields["extra"] = "多了"
	in[2].Fields["log"] = "改了"
	wantEqual(t, allEvents(t, s, id), want, "改了入参之后")
	got := allEvents(t, s, id)
	got[1].Fields["extra"] = "多了"
	got[2].Fields["log"] = "改了"
	wantEqual(t, allEvents(t, s, id), want, "改了读到的返回值之后")

	tr := []store.NewEvent{{At: at0, Kind: "task.boot", Fields: map[string]string{"by": "gateway"}}}
	_, _, err := s.TransitionTask(ctx, id, store.TaskExpect{State: model.TaskPending, Rev: 1}, model.TaskBooting, tr)
	must(t, err, "带着事件推进")
	tr[0].Fields["by"] = "改了"
	wantEqual(t, allEvents(t, s, id)[3].Fields, map[string]string{"by": "gateway"}, "改了推进的入参之后")
}
