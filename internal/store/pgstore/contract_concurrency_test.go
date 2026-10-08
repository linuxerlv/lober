package pgstore

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Test契约并发 是并发这一组（C30、C31、C33，spec §6.2 第 4 条）：多个 goroutine 同时调用，只按最终结果断言（恰好一个成功、
// Seq 连续不重不缺），不依赖调度的先后。这一组考验的是加锁与约束：用例失败时，先查是不是没加锁、加锁的顺序不一致，
// 或者在事务外读了数据，不要靠重试或加大超时让它变绿。
func Test契约并发(t *testing.T) {
	runCases(t, []testCase{
		{"同一台主机的几批任务同时提交，恰好一批成功，其余报 host_has_open_task", concurrentBatches},
		{"不同主机的几批任务同时提交，全都成功", concurrentDisjointBatches},
		{"同一个任务用同一个期望值同时推进，恰好一次成功，只有它带的事件落库", concurrentTransitions},
		{"同一个任务同时追加事件，Seq 连续、不重、不缺，一批的几条相邻", concurrentAppends},
		{"同一个 Key 同时追加，只落一条", concurrentSameKey},
	})
}

// workers 是每个并发用例同时调用的 goroutine 数。
const workers = 8

// together 让 n 个 goroutine 一起放行、同时跑 fn(i)，等它们全部返回：尽量让调用撞在一起。fn 里不能调用 t.Fatal 这类只能在
// 测试本身的 goroutine 里调用的方法，结果由调用方收集之后再断言。
func together(n int, fn func(i int)) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			fn(i)
		})
	}
	close(start)
	wg.Wait()
}

// 几批都是这三台主机，顺序各不相同：拿锁的顺序要是跟着入参走，两批就会互相等死。恰好一批成功（C31）；其余的报
// BatchError，里面只有 host_has_open_task。在事务里重跑准入时每台都报，靠部分唯一索引兜底时可能只报撞上的那一台，所以
// 不要求每台都报。
func concurrentBatches(t *testing.T, s *Store) {
	f := newFixture(t, s)
	hs := mustTaskHosts(t, s, "SN-1", "SN-2", "SN-3")
	batches := make([][]model.Task, workers)
	for i := range batches {
		order := append(slices.Clone(hs[i%len(hs):]), hs[:i%len(hs)]...)
		if i%2 == 1 {
			slices.Reverse(order)
		}
		batches[i] = f.newTasks(t, order...)
	}
	audits := auditEntries(t, s)
	results := make([][]model.Task, workers)
	errs := make([]error, workers)
	together(workers, func(i int) { results[i], errs[i] = s.CreateTasks(t.Context(), batches[i], anAudit("task.create")) })
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("第 %d 批与第 %d 批都成功了：同一台主机有了两个未结束的任务（C31）", winner+1, i+1)
			}
			winner = i
			continue
		}
		var be *store.BatchError
		if !errors.As(err, &be) || !errors.Is(err, store.ErrRejected) {
			t.Fatalf("第 %d 批应报 BatchError(ErrRejected)，得到 %v", i+1, err)
		}
		ps := batchProblems(be)
		if len(ps) == 0 || slices.ContainsFunc(ps, func(p model.Problem) bool { return p.Code != "host_has_open_task" }) {
			t.Fatalf("第 %d 批的问题应当只有 host_has_open_task，至少一条，得到 %v", i+1, ps)
		}
	}
	if winner < 0 {
		t.Fatal("一批都没成功")
	}
	wantItems(t, slices.Concat(taskPages(t, s, store.TaskQuery{Limit: 500})...), sortedTasks(results[winner]), "建出的任务只有成功的那一批")
	for _, task := range results[winner] {
		got, found, err := s.FindOpenTask(t.Context(), task.HostSN)
		must(t, err, "找 "+task.HostSN+" 未结束的任务")
		if !found || got.ID != task.ID {
			t.Fatalf("%s 未结束的任务应是成功那一批里的 %s，得到 %v、%s", task.HostSN, task.ID, found, got.ID)
		}
	}
	if n := len(auditEntries(t, s)) - len(audits); n != len(hs) {
		t.Fatalf("应只为成功的那一批写 %d 条审计，写了 %d 条", len(hs), n)
	}
}

// 加锁只挡同一台主机：几批的主机互不相同时，同时提交都要成功，不能把别的主机的批也挡下来。
func concurrentDisjointBatches(t *testing.T, s *Store) {
	f := newFixture(t, s)
	batches := make([][]model.Task, workers)
	for i := range batches {
		batches[i] = f.newTasks(t, mustTaskHosts(t, s, fmt.Sprintf("SN-%d", i))...)
	}
	errs := make([]error, workers)
	together(workers, func(i int) { _, errs[i] = s.CreateTasks(t.Context(), batches[i], anAudit("task.create")) })
	for i, err := range errs {
		must(t, err, fmt.Sprintf("第 %d 批", i+1))
	}
	wantEqual(t, len(slices.Concat(taskPages(t, s, store.TaskQuery{Limit: 500})...)), workers, "建出的任务数")
}

// 条件推进（C33）：同一个期望值只有一次生效，其余报 ErrConflict。事件与状态在同一个事务里写，所以只有成功那一次带的事件落库。
func concurrentTransitions(t *testing.T, s *Store) {
	task := mustTask(t, s)
	expect := store.TaskExpect{State: task.State, Rev: task.Rev}
	errs := make([]error, workers)
	together(workers, func(i int) {
		events := []store.NewEvent{{At: at0, Kind: "task.boot", Key: "boot-" + strconv.Itoa(i)}}
		_, _, errs[i] = s.TransitionTask(t.Context(), task.ID, expect, model.TaskBooting, events)
	})
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("第 %d 次与第 %d 次推进都成功了：同一个期望值只能生效一次", winner+1, i+1)
			}
			winner = i
			continue
		}
		wantIs(t, err, store.ErrConflict, fmt.Sprintf("第 %d 次推进", i+1))
	}
	if winner < 0 {
		t.Fatal("一次都没成功")
	}
	want := task
	want.State, want.Rev = model.TaskBooting, task.Rev+1
	wantEqual(t, mustGetTask(t, s, task.ID), want, "推进之后")
	wantEqual(t, allEvents(t, s, task.ID), []store.Event{{TaskID: task.ID, Seq: 1,
		NewEvent: store.NewEvent{At: at0, Kind: "task.boot", Key: "boot-" + strconv.Itoa(winner)}}}, "落库的事件")
}

// 同一个任务同时追加：每个 goroutine 追加 rounds 批、每批两条。全部 Seq 从 1 起连续、不重、不缺，一批的两条相邻（一批在
// 一个事务里分配），并且每个 Seq 落库的正是追加结果里报的那一条。
func concurrentAppends(t *testing.T, s *Store) {
	const rounds = 20
	id := mustTask(t, s).ID
	got := make([][]store.Appended, workers)
	errs := make([]error, workers)
	together(workers, func(i int) {
		for r := range rounds {
			es := []store.NewEvent{
				{At: at0, Kind: "slot.log", Fields: map[string]string{"by": strconv.Itoa(i), "n": strconv.Itoa(2 * r)}},
				{At: at0, Kind: "slot.log", Fields: map[string]string{"by": strconv.Itoa(i), "n": strconv.Itoa(2*r + 1)}},
			}
			a, err := s.AppendEvents(t.Context(), id, es)
			if err != nil {
				errs[i] = err
				return
			}
			got[i] = append(got[i], a...)
		}
	})
	owner := map[int64]string{} // Seq → 追加它的 goroutine 与序号
	for i, as := range got {
		must(t, errs[i], fmt.Sprintf("第 %d 个 goroutine 追加", i+1))
		if len(as) != 2*rounds {
			t.Fatalf("第 %d 个 goroutine 追加了 %d 条，结果有 %d 条", i+1, 2*rounds, len(as))
		}
		for j, a := range as {
			switch _, dup := owner[a.Seq]; {
			case a.Duplicate || dup:
				t.Fatalf("第 %d 个 goroutine 的第 %d 条得到 %+v：没有 Key 的事件不去重，Seq 也不能发两次", i+1, j+1, a)
			case j%2 == 1 && a.Seq != as[j-1].Seq+1:
				t.Fatalf("第 %d 个 goroutine 的一批两条得到 Seq %d 与 %d，应当相邻", i+1, as[j-1].Seq, a.Seq)
			}
			owner[a.Seq] = fmt.Sprintf("%d/%d", i, j)
		}
	}
	evs := allEvents(t, s, id)
	if len(evs) != workers*2*rounds {
		t.Fatalf("应落库 %d 条事件，得到 %d 条", workers*2*rounds, len(evs))
	}
	for k, e := range evs {
		if e.Seq != int64(k+1) {
			t.Fatalf("Seq 应从 1 起连续，第 %d 条的 Seq 是 %d", k+1, e.Seq)
		}
		if who := e.Fields["by"] + "/" + e.Fields["n"]; who != owner[e.Seq] {
			t.Fatalf("Seq %d 落库的是 %s 追加的，追加的结果说是 %s", e.Seq, who, owner[e.Seq])
		}
	}
}

// 同一个信号同时重发了几次：只落一条，恰好一次报告为新写的，其余报告 Duplicate，Seq 都是那一条的。
func concurrentSameKey(t *testing.T, s *Store) {
	id := mustTask(t, s).ID
	got := make([][]store.Appended, workers)
	errs := make([]error, workers)
	together(workers, func(i int) {
		es := []store.NewEvent{{At: at0, Kind: "signal.recv", Key: "sig-1", Fields: map[string]string{"by": strconv.Itoa(i)}}}
		got[i], errs[i] = s.AppendEvents(t.Context(), id, es)
	})
	winner := -1
	for i, as := range got {
		must(t, errs[i], fmt.Sprintf("第 %d 次追加", i+1))
		if len(as) != 1 || as[0].Seq != 1 {
			t.Fatalf("第 %d 次追加得到 %+v，应只有一条，Seq 都是那一条的 1", i+1, as)
		}
		if !as[0].Duplicate {
			if winner >= 0 {
				t.Fatalf("第 %d 次与第 %d 次都报告为新写的：同一个 Key 只能落一条", winner+1, i+1)
			}
			winner = i
		}
	}
	if winner < 0 {
		t.Fatal("没有一次报告为新写的")
	}
	wantEqual(t, allEvents(t, s, id), []store.Event{{TaskID: id, Seq: 1, NewEvent: store.NewEvent{
		At: at0, Kind: "signal.recv", Key: "sig-1", Fields: map[string]string{"by": strconv.Itoa(winner)},
	}}}, "落库的事件")
}
