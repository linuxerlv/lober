// 本文件测试 CAS 重试骨架（任务 1）。锁住：首次成功只调回调一次、CAS
// 冲突后重读重试、noop 跳过 CAS、拒绝原样返回不重试。
package statemachine

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

func newTestMachine(store contract.Store, clock contract.Clock) *Machine {
	return New(store, clock, nil, nil, nil, nil)
}

func mustCreateTaskFor(t *testing.T, store *fake.Store, tk model.Task) model.Task {
	t.Helper()
	created, err := store.CreateTask(context.Background(), tk)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v —— 夹具本身出问题，后续断言无意义", err)
	}
	return created
}

// TestCASRetryFirstAttemptSucceeds 验证首次成功时回调只被调用一次——
// 如果实现在成功之后仍多余地重读或重调回调，这条会通过调用计数把它暴露。
func TestCASRetryFirstAttemptSucceeds(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newTestMachine(store, clock)

	created := mustCreateTaskFor(t, store, model.Task{
		ID:      "task-1",
		State:   model.TaskStatePending,
		Version: 0,
	})

	calls := 0
	result, err := m.casRetry(ctx, created.ID, func(current model.Task) transitionDecision {
		calls++
		return transitionDecision{newState: model.TaskStateBooting}
	})
	if err != nil {
		t.Fatalf("casRetry 首次成功路径返回了错误: %v，want nil", err)
	}
	if calls != 1 {
		t.Errorf("回调被调用了 %d 次，want 1 —— 首次成功不应该多余地重读或重试", calls)
	}
	if result.State != model.TaskStateBooting {
		t.Errorf("casRetry 返回的任务 State = %q, want %q", result.State, model.TaskStateBooting)
	}
}

// TestCASRetrySucceedsAfterConflict 验证注入一个「第一次 CAS 前被别人
// 改掉」的场景后，casRetry 重读并重试能够成功——这是 CAS 重试骨架存在
// 的核心理由：扫描循环与信号请求撞车时，重读一次通常就能成功。
func TestCASRetrySucceedsAfterConflict(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newTestMachine(store, clock)

	created := mustCreateTaskFor(t, store, model.Task{
		ID:      "task-2",
		State:   model.TaskStatePending,
		Version: 0,
	})

	calls := 0
	result, err := m.casRetry(ctx, created.ID, func(current model.Task) transitionDecision {
		calls++
		if calls == 1 {
			// 模拟「回调读到任务后，CAS 之前，被另一个并发请求改掉」：
			// 在第一次回调内部偷偷改任务，让即将执行的 CAS（用第一次读
			// 到的 version）必然冲突。
			if _, err := store.CASTaskState(ctx, created.ID, model.TaskStatePending, current.Version, model.TaskStateBooting); err != nil {
				t.Fatalf("夹具注入冲突失败: %v", err)
			}
		}
		// 每轮都基于「当前」状态算目标转换：第一轮基于 pending 算出的
		// 目标是 booting（会冲突）；第二轮重读后任务已经是 booting，
		// 应基于 booting 算出下一个目标 installing。
		switch current.State {
		case model.TaskStatePending:
			return transitionDecision{newState: model.TaskStateBooting}
		case model.TaskStateBooting:
			return transitionDecision{newState: model.TaskStateInstalling}
		default:
			t.Fatalf("回调收到了未预期的状态 %q", current.State)
			return transitionDecision{}
		}
	})
	if err != nil {
		t.Fatalf("casRetry 在冲突后重试仍失败: %v，want 重读后成功", err)
	}
	if calls != 2 {
		t.Errorf("回调被调用了 %d 次，want 2（第一次撞冲突，第二次重读后成功）", calls)
	}
	if result.State != model.TaskStateInstalling {
		t.Errorf("casRetry 最终返回的 State = %q, want %q —— 说明重试用的是重读后的最新版本号",
			result.State, model.TaskStateInstalling)
	}
}

// TestCASRetryNoopSkipsCAS 验证回调返回「不需要转换」时不产生 CAS——
// 幂等方法（如 MarkAnswerFetched 发现已是目标状态）走这条路径。
func TestCASRetryNoopSkipsCAS(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newTestMachine(store, clock)

	created := mustCreateTaskFor(t, store, model.Task{
		ID:      "task-3",
		State:   model.TaskStateInstalling,
		Version: 5,
	})

	result, err := m.casRetry(ctx, created.ID, func(current model.Task) transitionDecision {
		return transitionDecision{noop: true}
	})
	if err != nil {
		t.Fatalf("casRetry noop 路径返回了错误: %v，want nil", err)
	}
	if result.Version != 5 {
		t.Errorf("noop 之后 Version = %d, want 保持 5 不变 —— noop 必须跳过 CAS，"+
			"否则每次幂等调用都会无意义地自增版本号", result.Version)
	}
	if result.State != model.TaskStateInstalling {
		t.Errorf("noop 之后 State = %q, want 保持不变", result.State)
	}
}

// TestCASRetryRejectReturnsErrorWithoutRetry 验证回调返回错误时错误原样
// 返回且不重试——retry 发现失败原因不可重装时必须走这条路径，不能被
// CAS 重试骨架当成冲突吞掉重试。
func TestCASRetryRejectReturnsErrorWithoutRetry(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newTestMachine(store, clock)

	created := mustCreateTaskFor(t, store, model.Task{
		ID:      "task-4",
		State:   model.TaskStateFailed,
		Version: 1,
	})

	sentinel := errors.New("统一测试用拒绝错误")
	calls := 0
	_, err := m.casRetry(ctx, created.ID, func(current model.Task) transitionDecision {
		calls++
		return transitionDecision{rejectErr: sentinel}
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("casRetry 拒绝路径返回 err = %v, want errors.Is(err, sentinel) —— "+
			"回调主动拒绝时错误必须原样返回，调用方要能用 errors.Is 识别具体的拒绝原因", err)
	}
	if calls != 1 {
		t.Errorf("回调被调用了 %d 次，want 1 —— 拒绝不是冲突，不应该被重试", calls)
	}
}

// TestCASRetryExhaustsRetriesReturnsConflict 验证重试次数用尽后返回
// contract.ErrConflict，不无限重试——真有持续竞争源（或 bug）时不能挂死
// 请求。
func TestCASRetryExhaustsRetriesReturnsConflict(t *testing.T) {
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newTestMachine(store, clock)

	created := mustCreateTaskFor(t, store, model.Task{
		ID:      "task-5",
		State:   model.TaskStatePending,
		Version: 0,
	})

	calls := 0
	_, err := m.casRetry(ctx, created.ID, func(current model.Task) transitionDecision {
		calls++
		// 每一轮都在回调内部偷偷用一次真实 CAS 把任务的 version 往前推
		// 一格（在 pending 与 booting 之间来回切换，状态本身不重要，
		// 重要的是 version 每轮都变），让即将执行的「基于本轮读到的
		// version」的 CAS 永远撞上已经过期的版本号，制造持续冲突。
		fresh, getErr := store.GetTask(ctx, created.ID)
		if getErr != nil {
			t.Fatalf("夹具内部 GetTask 失败: %v", getErr)
		}
		next := model.TaskStateBooting
		if fresh.State == model.TaskStateBooting {
			next = model.TaskStatePending
		}
		if _, err := store.CASTaskState(ctx, created.ID, fresh.State, fresh.Version, next); err != nil {
			t.Fatalf("夹具内部注入冲突用的 CAS 失败: %v", err)
		}
		return transitionDecision{newState: model.TaskStateInstalling}
	})
	if !errors.Is(err, contract.ErrConflict) {
		t.Errorf("持续冲突下 casRetry 返回 err = %v, want errors.Is(err, contract.ErrConflict)", err)
	}
	if calls != maxCASRetries {
		t.Errorf("回调被调用了 %d 次，want 恰好 %d 次（重试上限）—— 不应该无限重试", calls, maxCASRetries)
	}
}
