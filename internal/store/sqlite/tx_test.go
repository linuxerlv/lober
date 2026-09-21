// 本文件测试事务运行：回调失败时两张表都没有新行、嵌套调用返回错误而不
// 是卡住。对应实现计划任务 10，依据 SPEC §3.6。
package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func openTxTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "tx.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

// TestTx_CallbackErrorRollsBackBothTables 验证回调返回错误时整个事务
// 回滚：任务表与事件表都不应留下新行，不是「只回滚了一张」（计划任务 10
// 「测试要锁住的行为」第一条）。这正是建任务需要事务的理由——同时写任务
// 与首条事件，两者必须原子。
func TestTx_CallbackErrorRollsBackBothTables(t *testing.T) {
	s := openTxTestStore(t)
	ctx := context.Background()

	sentinelErr := errors.New("模拟回调内部失败")

	task := model.Task{
		ID: "task-tx-rollback", CreatedAt: 1, HostSN: "SN-tx", TemplateID: "tpl-1",
		TemplateVersion: 1, ArtifactSetID: "art-1", State: model.TaskStatePending,
		StateUpdatedAt: 1, CurrentPhase: model.PhasePending, PhaseEnteredAt: 1,
	}

	err := s.RunInTx(ctx, func(ctx context.Context, store contract.Store) error {
		if _, err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("事务内 CreateTask 失败: %v —— 夹具本身出问题", err)
		}
		if _, err := store.AppendEvent(ctx, model.TaskEvent{
			TaskID: task.ID, Seq: 10, TS: 1, Phase: model.PhasePending,
			Level: model.EventLevelInfo, Source: model.EventSourceSignal,
		}); err != nil {
			t.Fatalf("事务内 AppendEvent 失败: %v —— 夹具本身出问题", err)
		}
		return sentinelErr
	})

	if !errors.Is(err, sentinelErr) {
		t.Fatalf("RunInTx 返回 %v, want errors.Is(err, sentinelErr) —— "+
			"回调返回的错误必须原样冒泡，调用方需要知道具体是哪个业务错误导致了回滚", err)
	}

	if _, err := s.GetTask(ctx, task.ID); !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("回调失败后 GetTask(%q) 的 err = %v, want errors.Is(err, ErrNotFound) —— "+
			"事务回滚必须撤销任务表的写入，不能只回滚事件表", task.ID, err)
	}

	events, err := s.ListEvents(ctx, task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("回调失败后 ListEvents(%q) 返回 %d 条事件, want 0 —— "+
			"事务回滚必须撤销事件表的写入，不能只回滚任务表", task.ID, len(events))
	}
}

// TestTx_CallbackSuccessCommitsBothTables 验证回调成功时两张表的写入都
// 持久化——这是「回滚测试」的对照组，确保不是「RunInTx 本身什么都没做」
// 导致回滚测试碰巧通过。
func TestTx_CallbackSuccessCommitsBothTables(t *testing.T) {
	s := openTxTestStore(t)
	ctx := context.Background()

	task := model.Task{
		ID: "task-tx-commit", CreatedAt: 1, HostSN: "SN-tx-commit", TemplateID: "tpl-1",
		TemplateVersion: 1, ArtifactSetID: "art-1", State: model.TaskStatePending,
		StateUpdatedAt: 1, CurrentPhase: model.PhasePending, PhaseEnteredAt: 1,
	}

	err := s.RunInTx(ctx, func(ctx context.Context, store contract.Store) error {
		if _, err := store.CreateTask(ctx, task); err != nil {
			return err
		}
		_, err := store.AppendEvent(ctx, model.TaskEvent{
			TaskID: task.ID, Seq: 10, TS: 1, Phase: model.PhasePending,
			Level: model.EventLevelInfo, Source: model.EventSourceSignal,
		})
		return err
	})
	if err != nil {
		t.Fatalf("RunInTx 失败: %v, want 成功", err)
	}

	if _, err := s.GetTask(ctx, task.ID); err != nil {
		t.Errorf("提交后 GetTask(%q) 失败: %v, want 成功——写入应已持久化", task.ID, err)
	}
	events, err := s.ListEvents(ctx, task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("提交后 ListEvents(%q) 返回 %d 条事件, want 1", task.ID, len(events))
	}
}

// TestTx_NestedCallReturnsErrorNotHang 验证嵌套调用返回错误而不是卡住——
// 本测试设超时，否则测试本身会挂死（计划任务 10「测试要锁住的行为」
// 第二条）。SQLite 单写者且连接池上限 1，嵌套开事务会等待自己持有的连接
// 释放，若 RunInTx 没有拒绝嵌套，这条测试会在超时后失败，而不是死等到
// go test 自身的默认超时（那样报出来的是「测试整体超时」，看不出具体是
// 哪个调用挂住了）。
func TestTx_NestedCallReturnsErrorNotHang(t *testing.T) {
	s := openTxTestStore(t)

	done := make(chan error, 1)
	go func() {
		ctx := context.Background()
		done <- s.RunInTx(ctx, func(ctx context.Context, store contract.Store) error {
			txRunner, ok := store.(contract.TxRunner)
			if !ok {
				t.Errorf("事务内的 store 未实现 contract.TxRunner——嵌套调用测试本身无法进行")
				return nil
			}
			return txRunner.RunInTx(ctx, func(ctx context.Context, store contract.Store) error {
				t.Errorf("嵌套的 RunInTx 回调被执行了——嵌套调用应该在开启事务前就被拒绝")
				return nil
			})
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("嵌套调用 RunInTx 返回 nil, want 非 nil 的错误")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunInTx 嵌套调用在 5 秒内未返回——这正是计划警告的死锁：" +
			"嵌套开事务等待自己持有的连接释放，表现为整个进程卡住无响应")
	}
}
