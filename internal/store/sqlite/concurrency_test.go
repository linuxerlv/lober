// 本文件测试三个并发场景：同一任务同一转换的并发 CAS、同一任务不同序号
// 的并发事件写入、同一任务同一序号的并发事件写入。对应实现计划任务 11，
// 依据 SPEC §3.6。
//
// SQLite 在连接池上限 1 下会串行化写，这些测试更多是验证错误翻译在竞争
// 下仍然正确，而不是验证锁本身——但它们能抓到一类真实 bug：把
// SQLITE_BUSY 误翻译成 ErrConflict，那会让调用方以为是版本冲突而重读
// 重试，掩盖真正的连接池配置问题（计划任务 11「设计要点」）。
package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func openConcurrencyTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "concurrency.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

// TestConcurrency_CASExactlyOneWinner 验证多个 goroutine 同时 CAS 同一
// 任务的同一转换：恰好一个成功，其余全部得到 ErrConflict——不是「大部分
// 成功」，是精确的一个（计划任务 11「三个并发场景」第一条）。
func TestConcurrency_CASExactlyOneWinner(t *testing.T) {
	s := openConcurrencyTestStore(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, model.Task{
		ID: "task-cas-race", CreatedAt: 1, HostSN: "SN-cas-race", TemplateID: "tpl-1",
		TemplateVersion: 1, ArtifactSetID: "art-1", State: model.TaskStatePending,
		StateUpdatedAt: 1, CurrentPhase: model.PhasePending, PhaseEnteredAt: 1,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	const n = 20
	var (
		barrier   sync.WaitGroup
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		conflicts int
		otherErrs []error
	)
	barrier.Add(1)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			barrier.Wait() // 让全部 goroutine 尽量同时发起，而不是自然串行
			_, err := s.CASTaskState(ctx, task.ID, model.TaskStatePending, task.Version, model.TaskStateBooting)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, contract.ErrConflict):
				conflicts++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	barrier.Done()
	wg.Wait()

	if len(otherErrs) != 0 {
		t.Fatalf("出现了非 ErrConflict 的错误: %v —— SQLITE_BUSY 之类的连接池竞争错误"+
			"不应该被误翻译成别的东西，也不应该无端出现（连接池上限 1 下写操作本应"+
			"串行化排队，不应该报错）", otherErrs)
	}
	if succeeded != 1 {
		t.Errorf("并发 CAS 成功次数 = %d, want 精确的 1 —— SPEC §3.6 的乐观锁必须保证"+
			"恰好一个转换成功，「大部分成功」意味着版本冲突检测本身有漏洞，会让"+
			"状态机的扫描循环与信号请求同时把同一个任务推进两次", succeeded)
	}
	if conflicts != n-1 {
		t.Errorf("并发 CAS 得到 ErrConflict 的次数 = %d, want %d", conflicts, n-1)
	}
}

// TestConcurrency_DistinctSeqEventsAllSucceed 验证多个 goroutine 同时写
// 同一任务的不同事件序号：全部成功，事件数等于 goroutine 数（计划任务 11
// 「三个并发场景」第二条）。
func TestConcurrency_DistinctSeqEventsAllSucceed(t *testing.T) {
	s := openConcurrencyTestStore(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, model.Task{
		ID: "task-distinct-seq", CreatedAt: 1, HostSN: "SN-distinct-seq", TemplateID: "tpl-1",
		TemplateVersion: 1, ArtifactSetID: "art-1", State: model.TaskStatePending,
		StateUpdatedAt: 1, CurrentPhase: model.PhasePending, PhaseEnteredAt: 1,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	const n = 20
	var (
		barrier sync.WaitGroup
		wg      sync.WaitGroup
		mu      sync.Mutex
		errs    []error
	)
	barrier.Add(1)
	for i := 1; i <= n; i++ {
		seq := i * 10 // 与信号表的序号间隔风格一致，避免任何相邻性歧义
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			barrier.Wait()
			_, err := s.AppendEvent(ctx, model.TaskEvent{
				TaskID: task.ID, Seq: seq, TS: 1, Phase: model.PhasePending,
				Level: model.EventLevelInfo, Source: model.EventSourceSignal,
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(seq)
	}
	barrier.Done()
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("并发写入不同 seq 的事件出现错误: %v, want 全部成功 —— "+
			"这些 seq 互不相同，理应互不冲突", errs)
	}

	events, err := s.ListEvents(ctx, task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) != n {
		t.Errorf("事件数 = %d, want %d —— 每个 goroutine 写入一个不同的 seq，"+
			"总数应精确等于 goroutine 数，多了或少了都说明并发写入下有事件丢失或重复",
			len(events), n)
	}
}

// TestConcurrency_SameSeqEventsExactlyOneWinner 验证多个 goroutine 同时用
// 同一序号写事件：恰好一个成功，其余得到 ErrDuplicateSeq（计划任务 11
// 「三个并发场景」第三条）。
func TestConcurrency_SameSeqEventsExactlyOneWinner(t *testing.T) {
	s := openConcurrencyTestStore(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, model.Task{
		ID: "task-same-seq", CreatedAt: 1, HostSN: "SN-same-seq", TemplateID: "tpl-1",
		TemplateVersion: 1, ArtifactSetID: "art-1", State: model.TaskStatePending,
		StateUpdatedAt: 1, CurrentPhase: model.PhasePending, PhaseEnteredAt: 1,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	const n = 20
	const seq = 10
	var (
		barrier   sync.WaitGroup
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		duplicate int
		otherErrs []error
	)
	barrier.Add(1)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			barrier.Wait()
			_, err := s.AppendEvent(ctx, model.TaskEvent{
				TaskID: task.ID, Seq: seq, TS: 1, Phase: model.PhasePending,
				Level: model.EventLevelInfo, Source: model.EventSourceSignal,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, contract.ErrDuplicateSeq):
				duplicate++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	barrier.Done()
	wg.Wait()

	if len(otherErrs) != 0 {
		t.Fatalf("出现了非 ErrDuplicateSeq 的错误: %v", otherErrs)
	}
	if succeeded != 1 {
		t.Errorf("并发写同一 seq=%d 的事件，成功次数 = %d, want 精确的 1 —— "+
			"机器侧对同一次信号会重试最多三次，若这里有超过一个成功，说明"+
			"(task_id, seq) 唯一索引在竞争下失效，信号去重的全部依据落空", seq, succeeded)
	}
	if duplicate != n-1 {
		t.Errorf("并发写同一 seq 得到 ErrDuplicateSeq 的次数 = %d, want %d", duplicate, n-1)
	}
}
