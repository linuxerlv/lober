package pgstore

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// ImportHosts 与 CreateTasks 给主机加锁的次序是同一个（hostLockOrder）：CSV 导入撞上批量建任务、两边的主机有重叠时，两个事务
// 按同一个次序拿锁，等待不会成环，不会互相等死（SQLSTATE 40P01，第 5 层拿到只能报内部错误）。两边各排一遍的话，一边改了次序，
// 只测同一个方法自己跟自己并发的用例（Test同时导入同一组主机不会互相等死、concurrentBatches）照样是绿的。
//
// 分几轮，每轮几次导入与几批建任务同时开始：导入的入参各自打乱顺序，建任务的几批各取一段与相邻的批重叠一半的主机，顺序也打乱。
// 按最终结果断言，不依赖调度的先后：导入全部成功；建任务的每批要么成功，要么报 BatchError、问题只有 host_has_open_task；每台
// 主机至多一个未结束的任务，正是成功那一批给它建的；每台主机的 Rev 是导入的次数加 1。这个测试本身就在测并发，不走 runCases，
// 保持串行。
func Test同时导入主机与建任务不会互相等死(t *testing.T) {
	s := openContract(t)
	sqlDB, err := s.db.DB()
	if err != nil {
		t.Fatalf("取连接池：%v", err)
	}
	sqlDB.SetMaxIdleConns(sqlDB.Stats().MaxOpenConnections) // 连接留着不关，每轮开始时不必现建，几个事务才真的同时加锁
	const n, rounds, importers, creators = 40, 5, 4, 4
	f := newFixture(t, s)
	hs := make([]model.Host, n)
	for i := range hs {
		hs[i] = taskHost(fmt.Sprintf("SN-%03d", i)) // 导入的与登记的内容相同：重跑准入只会因为未结束的任务拦下
	}
	mustImport(t, s, hs)
	r := rand.New(rand.NewPCG(1, 2)) // 种子固定，每次打乱得都一样
	shuffled := func(in []model.Host) []model.Host {
		out := slices.Clone(in)
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}

	for round := range rounds {
		imports := make([][]model.Host, importers)
		for i := range imports {
			imports[i] = shuffled(hs)
		}
		batches := make([][]model.Task, creators)
		for i := range batches {
			part := make([]model.Host, n/2) // 第 i 批从第 i*n/creators 台起取一半：相邻的两批重叠一半
			for j := range part {
				part[j] = hs[(i*n/creators+j)%n]
			}
			batches[i] = f.newTasks(t, shuffled(part)...)
		}
		importErrs := make([]error, importers)
		created := make([][]model.Task, creators)
		createErrs := make([]error, creators)
		together(importers+creators, func(i int) {
			if i < importers {
				_, importErrs[i] = s.ImportHosts(t.Context(), imports[i], anAudit("host.import"))
				return
			}
			created[i-importers], createErrs[i-importers] = s.CreateTasks(t.Context(), batches[i-importers], anAudit("task.create"))
		})

		for i, err := range importErrs {
			must(t, err, fmt.Sprintf("第 %d 轮的第 %d 次导入（加锁的次序不一致时是 SQLSTATE 40P01）", round+1, i+1))
		}
		open := map[string]string{} // SN → 成功的那一批给它建的任务
		var tasks []model.Task
		for i, err := range createErrs {
			if err == nil {
				for _, task := range created[i] {
					if prev, dup := open[task.HostSN]; dup {
						t.Fatalf("第 %d 轮：%s 同时有了 %s 与 %s 两个未结束的任务（C31）", round+1, task.HostSN, prev, task.ID)
					}
					open[task.HostSN] = task.ID
				}
				tasks = append(tasks, created[i]...)
				continue
			}
			var be *store.BatchError
			if !errors.As(err, &be) || !errors.Is(err, store.ErrRejected) {
				t.Fatalf("第 %d 轮的第 %d 批建任务应成功或报 BatchError（加锁的次序不一致时是 SQLSTATE 40P01），得到 %v", round+1, i+1, err)
			}
			ps := batchProblems(be)
			if len(ps) == 0 || slices.ContainsFunc(ps, func(p model.Problem) bool { return p.Code != "host_has_open_task" }) {
				t.Fatalf("第 %d 轮的第 %d 批的问题应当只有 host_has_open_task，至少一条，得到 %v", round+1, i+1, ps)
			}
		}
		if len(tasks) == 0 {
			t.Fatalf("第 %d 轮一批任务都没建成：这一轮开始时没有未结束的任务，先拿到锁的那一批应当成功", round+1)
		}
		for _, h := range hs {
			got, found, err := s.FindOpenTask(t.Context(), h.SN)
			must(t, err, "找 "+h.SN+" 未结束的任务")
			if want, ok := open[h.SN]; found != ok || got.ID != want {
				t.Fatalf("第 %d 轮：%s 未结束的任务应是 %q（空表示没有），得到 %v、%q", round+1, h.SN, want, found, got.ID)
			}
		}
		for _, task := range tasks {
			mustTransition(t, s, task, model.TaskDone) // 结束了，下一轮才能再给这几台建任务
		}
	}

	got, err := s.GetHosts(t.Context(), snsOf(hs))
	must(t, err, "读回主机")
	for _, h := range hs {
		if want := int64(1 + rounds*importers); got[h.SN].Rev != want {
			t.Fatalf("%s 的 Rev 是 %d，应为导入的次数加 1，即 %d", h.SN, got[h.SN].Rev, want)
		}
	}
}
