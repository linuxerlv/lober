// 本文件测试 RunInTx：跨聚合写入的原子性与嵌套拒绝。对应实现计划任务
// 10，依据设计文档 §4。
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newTestStore 建一个独立 schema 的顶层 Store，供本文件各测试用例使用。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := pgtest.NewDB(t, allModelsForTest()...)
	return New(db)
}

// baseTask 返回一个字段齐全、可直接落库的最小任务。与 task 包
// store_test.go 里的同名函数保持同一套字段选择（本文件测的是跨聚合
// 事务，不是 task 聚合本身的语义，字段值本身不重要，重要的是「够
// 落库」）。
func baseTask(id, hostSN string) model.Task {
	now := time.Now().Unix()
	return model.Task{
		ID:              id,
		CreatedAt:       now,
		CreatedBy:       "tester",
		HostSN:          hostSN,
		TemplateID:      "tpl-1",
		TemplateVersion: 1,
		ArtifactSetID:   "art-1",
		State:           model.TaskStatePending,
		StateUpdatedAt:  now,
		Version:         1,
		CurrentPhase:    model.PhasePending,
		PhaseEnteredAt:  now,
	}
}

// baseEvent 返回一条与给定任务关联、可直接落库的最小事件。
func baseEvent(taskID string, seq int) model.TaskEvent {
	return model.TaskEvent{
		TaskID:  taskID,
		Seq:     seq,
		TS:      time.Now().Unix(),
		Phase:   model.PhasePending,
		Level:   model.EventLevelInfo,
		Message: "created",
		Source:  model.EventSourceProvider,
	}
}

// TestRunInTx_CommitVisibleOutside 覆盖回调返回 nil 的提交路径：事务内
// 跨两个聚合（task + event）的写入，事务提交后在事务外的顶层 Store 上
// 都能读到——这正是 RunInTx 存在的理由：建任务要同时写 tasks 与
// task_events，两者必须原子。
func TestRunInTx_CommitVisibleOutside(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	taskID := "task-commit"
	task := baseTask(taskID, "SN-commit")

	err := s.RunInTx(ctx, func(ctx context.Context, tx contract.Store) error {
		if _, err := tx.CreateTask(ctx, task); err != nil {
			return err
		}
		if _, err := tx.AppendEvent(ctx, baseEvent(taskID, 1)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx() 返回错误: %v", err)
	}

	if _, err := s.GetTask(ctx, taskID); err != nil {
		t.Errorf("提交后事务外应能读到任务，GetTask() 返回: %v", err)
	}
	events, err := s.ListEvents(ctx, taskID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents() 失败: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("提交后事务外应能读到 1 条事件，实际读到 %d 条", len(events))
	}
}

// TestRunInTx_CallbackErrorRollsBackBothAggregates 覆盖回调返回错误的
// 回滚路径：跨两个聚合的写入（task + event）在回调返回错误后必须整个
// 回滚，两者都不存在——不能出现「task 提交了但 event 没提交」这种
// 半成品状态，那正是设计文档 §4 强调的「原子性」要保证的东西。
func TestRunInTx_CallbackErrorRollsBackBothAggregates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	taskID := "task-rollback"
	task := baseTask(taskID, "SN-rollback")
	wantErr := errors.New("模拟回调失败")

	err := s.RunInTx(ctx, func(ctx context.Context, tx contract.Store) error {
		if _, err := tx.CreateTask(ctx, task); err != nil {
			return err
		}
		if _, err := tx.AppendEvent(ctx, baseEvent(taskID, 1)); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunInTx() 应该原样返回回调的错误，实际: %v", err)
	}

	if _, err := s.GetTask(ctx, taskID); !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("回滚后任务不应存在，GetTask() 应返回 ErrNotFound，实际: %v", err)
	}
	events, err := s.ListEvents(ctx, taskID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents() 失败: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("回滚后事件不应存在，实际读到 %d 条", len(events))
	}
}

// TestRunInTx_NestedCallReturnsErrorWithoutCommittingAnything 覆盖嵌套
// 调用：回调内部再调一次 RunInTx，必须返回错误，且外层事务里已经做的
// 写入不会被提交——嵌套调用本身是编程错误，拒绝时不能顺手把外层已经
// 完成的部分写入留下。
func TestRunInTx_NestedCallReturnsErrorWithoutCommittingAnything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	taskID := "task-nested"
	task := baseTask(taskID, "SN-nested")

	var nestedErr error
	err := s.RunInTx(ctx, func(ctx context.Context, tx contract.Store) error {
		if _, err := tx.CreateTask(ctx, task); err != nil {
			return err
		}

		txRunner, ok := tx.(contract.TxRunner)
		if !ok {
			t.Fatal("事务内的 Store 应同时满足 contract.TxRunner，才能验证嵌套调用被拒绝")
		}
		nestedErr = txRunner.RunInTx(ctx, func(ctx context.Context, inner contract.Store) error {
			t.Fatal("嵌套的 RunInTx 不应该执行回调本身")
			return nil
		})
		return nestedErr
	})

	if nestedErr == nil {
		t.Fatal("嵌套调用 RunInTx 应该返回错误，实际返回 nil")
	}
	if err == nil {
		t.Fatal("外层 RunInTx 应该因为回调返回了嵌套错误而回滚，实际返回 nil")
	}

	if _, err := s.GetTask(ctx, taskID); !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("外层因嵌套调用被拒绝而回滚，任务不应存在，GetTask() 应返回 ErrNotFound，实际: %v", err)
	}
}

// TestRunInTx_NestedRejectionIsNotContractSentinel 确认嵌套拒绝的错误
// 不是任何 contract 层 sentinel——嵌套是调用方的编程错误，不是像
// ErrConflict 那样需要业务分支处理的正常情况（计划任务 10「实现方式」
// 明确要求）。
func TestRunInTx_NestedRejectionIsNotContractSentinel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var nestedErr error
	_ = s.RunInTx(ctx, func(ctx context.Context, tx contract.Store) error {
		txRunner := tx.(contract.TxRunner)
		nestedErr = txRunner.RunInTx(ctx, func(ctx context.Context, inner contract.Store) error {
			return nil
		})
		return nestedErr
	})

	sentinels := []error{
		contract.ErrNotFound,
		contract.ErrConflict,
		contract.ErrDuplicateSeq,
		contract.ErrActiveTaskExists,
		contract.ErrStillReferenced,
	}
	for _, sentinel := range sentinels {
		if errors.Is(nestedErr, sentinel) {
			t.Errorf("嵌套拒绝的错误不应匹配 contract sentinel %v，实际 errors.Is 命中了", sentinel)
		}
	}
}

// TestRunInTx_TopLevelStoreIsNotInTx 确认顶层 Store（New 直接构造的）
// 不会被误判为「已经在事务里」——inTx 标记位默认应是 false，否则
// 顶层 Store 上第一次调用 RunInTx 就会被自己误当成嵌套调用拒绝掉。
func TestRunInTx_TopLevelStoreIsNotInTx(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.RunInTx(ctx, func(ctx context.Context, tx contract.Store) error {
		return nil
	})
	if err != nil {
		t.Fatalf("顶层 Store 第一次调用 RunInTx 不应该被当成嵌套调用拒绝，实际返回: %v", err)
	}
}
