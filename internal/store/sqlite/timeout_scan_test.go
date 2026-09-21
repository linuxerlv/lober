// 本文件补充 ListTimedOutTasks 对终态任务的排除测试。conformance 套件的
// testTimeoutScanNeverReturnsPending 只验证了 pending 被排除、booting 被
// 包含，没有覆盖 done/failed 这两个真正的终态——本文件补上这条断言，锁住
// 「终态任务永不出现在超时扫描结果里」这条行为，与 task.go 里
// ListTimedOutTasks 改为调用 model.TaskState.IsTerminal() 过滤（而非在 SQL
// 里手写 state NOT IN (...)）的修复配套。
package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

func openTimeoutScanTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "timeout-scan.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

// TestListTimedOutTasks_ExcludesTerminalStates 验证 done 与 failed 两个
// 终态任务即使阶段预算早已用尽，也绝不出现在 ListTimedOutTasks 的结果里
// ——终态任务已经不再需要超时判定，若扫描循环把已完成或已失败的任务当作
// 「又超时了」重新处理一遍，会产生错误的重复失败事件或状态转换尝试。
func TestListTimedOutTasks_ExcludesTerminalStates(t *testing.T) {
	s := openTimeoutScanTestStore(t)
	ctx := context.Background()

	longAgo := int64(1) // 阶段进入时间设在很早以前，任何预算都会判超时

	doneTask, err := s.CreateTask(ctx, model.Task{
		ID: "task-done-not-timedout", CreatedAt: longAgo, HostSN: "SN-done",
		TemplateID: "tpl-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: model.TaskStateDone, StateUpdatedAt: longAgo,
		CurrentPhase: model.PhaseInitializing, PhaseEnteredAt: longAgo,
	})
	if err != nil {
		t.Fatalf("CreateTask(done) 失败: %v —— 夹具准备阶段失败", err)
	}

	failedTask, err := s.CreateTask(ctx, model.Task{
		ID: "task-failed-not-timedout", CreatedAt: longAgo, HostSN: "SN-failed",
		TemplateID: "tpl-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: model.TaskStateFailed, StateUpdatedAt: longAgo,
		CurrentPhase: model.PhasePackages, PhaseEnteredAt: longAgo,
	})
	if err != nil {
		t.Fatalf("CreateTask(failed) 失败: %v —— 夹具准备阶段失败", err)
	}

	// 同时建一个真的该超时的非终态任务，确保扫描逻辑本身仍然工作——避免
	// 「扫描返回空列表」被误判成「终态被正确排除」。
	timedOut, err := s.CreateTask(ctx, model.Task{
		ID: "task-booting-timedout", CreatedAt: longAgo, HostSN: "SN-booting",
		TemplateID: "tpl-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: model.TaskStateBooting, StateUpdatedAt: longAgo,
		CurrentPhase: model.PhaseBooting, PhaseEnteredAt: longAgo,
	})
	if err != nil {
		t.Fatalf("CreateTask(booting) 失败: %v —— 夹具准备阶段失败", err)
	}

	oneYearLater := longAgo + int64(365*24*3600)
	got, err := s.ListTimedOutTasks(ctx, oneYearLater)
	if err != nil {
		t.Fatalf("ListTimedOutTasks 失败: %v", err)
	}

	for _, tk := range got {
		if tk.ID == doneTask.ID {
			t.Errorf("ListTimedOutTasks 返回了已 done 的任务 %q —— 终态任务不该再被超时扫描判定", tk.ID)
		}
		if tk.ID == failedTask.ID {
			t.Errorf("ListTimedOutTasks 返回了已 failed 的任务 %q —— 终态任务不该再被超时扫描判定", tk.ID)
		}
	}

	foundTimedOut := false
	for _, tk := range got {
		if tk.ID == timedOut.ID {
			foundTimedOut = true
		}
	}
	if !foundTimedOut {
		t.Errorf("ListTimedOutTasks 没有返回本该超时的 booting 任务 %q —— "+
			"扫描逻辑本身可能整体失效，上面「终态被排除」的断言就没有意义", timedOut.ID)
	}
}
