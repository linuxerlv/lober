// 本文件测试 Store 的八个 contract.TaskStore 方法，对应实现计划任务
// 4、5（本次与任务 3 合并交付）。用 pgtest.NewDB 拿独立 schema，
// AutoMigrate 已建好 tasks 表（含三个唯一索引）。
package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newTestStore 建一个独立 schema 的 Store，供各测试用例使用。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := pgtest.NewDB(t, Models()...)
	return New(db)
}

// baseTask 返回一个字段齐全、可直接落库的最小任务，调用方按需覆盖字段。
func baseTask(id string) model.Task {
	now := time.Now().Unix()
	return model.Task{
		ID:              id,
		CreatedAt:       now,
		CreatedBy:       "tester",
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

// ---------------------------------------------------------------------------
// CreateTask
// ---------------------------------------------------------------------------

// TestCreateTask_Normal 覆盖正常建任务：返回值与入参一致（除时间字段外，
// baseTask 本身已是 Unix 秒整数，不涉及精度损失）。
func TestCreateTask_Normal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := baseTask("task-normal")
	in.HostSN = "SN-normal"

	got, err := s.CreateTask(ctx, in)
	if err != nil {
		t.Fatalf("CreateTask() 失败: %v", err)
	}
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("CreateTask() 返回值与入参不符 (-want +got):\n%s", diff)
	}

	fetched, err := s.GetTask(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetTask() 失败: %v", err)
	}
	if diff := cmp.Diff(in, fetched); diff != "" {
		t.Errorf("GetTask() 读回结果与建任务时的入参不符 (-want +got):\n%s", diff)
	}
}

// TestCreateTask_SecondActiveTaskSameSNFails 覆盖同 SN 第二个非终态任务：
// 期望 contract.ErrActiveTaskExists。这是 idx_tasks_sn_active partial
// unique index 生效的第一道证据。
func TestCreateTask_SecondActiveTaskSameSNFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := baseTask("task-first")
	first.HostSN = "SN-dup"
	first.State = model.TaskStateBooting // 非终态

	if _, err := s.CreateTask(ctx, first); err != nil {
		t.Fatalf("第一个任务 CreateTask() 失败: %v", err)
	}

	second := baseTask("task-second")
	second.HostSN = "SN-dup"
	second.State = model.TaskStateInstalling // 同样非终态

	_, err := s.CreateTask(ctx, second)
	if !errors.Is(err, contract.ErrActiveTaskExists) {
		t.Fatalf("CreateTask() 第二个同 SN 非终态任务 = %v, want errors.Is(_, contract.ErrActiveTaskExists) = true", err)
	}
}

// TestCreateTask_SameSNAfterDoneSucceeds 覆盖同 SN 但前一个已 done：
// 期望成功。这是 partial index 的 WHERE 真生效的证据，也是 index tag
// 转义写错的第一个捕获点——若 WHERE 被 Go struct tag 语法或 GORM 解析
// 静默丢失，这条测试会失败（症状是「装完的机器再也不能重装」）。
func TestCreateTask_SameSNAfterDoneSucceeds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	done := baseTask("task-done")
	done.HostSN = "SN-reinstall"
	done.State = model.TaskStateDone // 终态，不占用 idx_tasks_sn_active

	if _, err := s.CreateTask(ctx, done); err != nil {
		t.Fatalf("第一个（done）任务 CreateTask() 失败: %v", err)
	}

	again := baseTask("task-reinstall")
	again.HostSN = "SN-reinstall"
	again.State = model.TaskStatePending

	if _, err := s.CreateTask(ctx, again); err != nil {
		t.Fatalf("同 SN 但前一个已 done 时 CreateTask() 应成功，实际报错: %v —— "+
			"若 idx_tasks_sn_active 的 WHERE 子句因反斜杠转义写错而丢失，"+
			"该索引会退化成无条件唯一索引，导致这里被误判为重复", err)
	}
}

// ---------------------------------------------------------------------------
// GetTask
// ---------------------------------------------------------------------------

// TestGetTask_NotFound 覆盖查不到的情形。
func TestGetTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetTask(ctx, "no-such-task")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("GetTask(不存在) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
	}
}

// ---------------------------------------------------------------------------
// FindPendingTaskByIdentity
// ---------------------------------------------------------------------------

func TestFindPendingTaskByIdentity(t *testing.T) {
	t.Run("只有SN命中", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		want := baseTask("task-sn-hit")
		want.HostSN = "SN-001"
		if _, err := s.CreateTask(ctx, want); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.FindPendingTaskByIdentity(ctx, "SN-001", "", "")
		if err != nil {
			t.Fatalf("FindPendingTaskByIdentity() 失败: %v", err)
		}
		if got.ID != want.ID {
			t.Errorf("FindPendingTaskByIdentity() 命中 ID = %q, want %q", got.ID, want.ID)
		}
	})

	t.Run("SN空而MAC命中", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		want := baseTask("task-mac-hit")
		want.HostMAC = "aa:bb:cc:dd:ee:ff"
		if _, err := s.CreateTask(ctx, want); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.FindPendingTaskByIdentity(ctx, "", "aa:bb:cc:dd:ee:ff", "")
		if err != nil {
			t.Fatalf("FindPendingTaskByIdentity() 失败: %v", err)
		}
		if got.ID != want.ID {
			t.Errorf("FindPendingTaskByIdentity() 命中 ID = %q, want %q", got.ID, want.ID)
		}
	})

	t.Run("三键全空返回NotFound", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		// 即便库里有 pending 任务且 host_sn 恰好为空，三键全空也必须
		// 直接返回 ErrNotFound，不拿空串去查。
		leftover := baseTask("task-empty-keys")
		if _, err := s.CreateTask(ctx, leftover); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		_, err := s.FindPendingTaskByIdentity(ctx, "", "", "")
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("FindPendingTaskByIdentity(三键全空) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
		}
	})

	// 本用例原先用 booting 当「非 pending」的样本，而 booting 现在是**接受**
	// 引导请求的状态之一（SPEC §3.3「booting | 15 min | 允许重试，attempt++」，
	// 见 contract.TaskStore.FindPendingTaskByIdentity 的文档）。改用真正不接受
	// 引导的四个状态，并加一条正向用例确认 booting 能被命中——两者合起来锁住
	// 「哪些状态接受引导」这条边界，比原先只测一个反例更严。
	t.Run("命中的任务状态不接受引导", func(t *testing.T) {
		for _, state := range []model.TaskState{
			model.TaskStateInstalling, model.TaskStateRebooting,
			model.TaskStateDone, model.TaskStateFailed,
		} {
			s := newTestStore(t)
			ctx := context.Background()

			notBootable := baseTask("task-" + string(state))
			notBootable.HostSN = "SN-" + string(state)
			notBootable.State = state
			if _, err := s.CreateTask(ctx, notBootable); err != nil {
				t.Fatalf("CreateTask() 失败: %v", err)
			}

			_, err := s.FindPendingTaskByIdentity(ctx, notBootable.HostSN, "", "")
			if !errors.Is(err, contract.ErrNotFound) {
				t.Errorf("状态 %s 不该接受引导请求，FindPendingTaskByIdentity = %v, "+
					"want errors.Is(_, contract.ErrNotFound) = true", state, err)
			}
		}
	})

	t.Run("命中booting任务_接受重试", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		booting := baseTask("task-booting-retry")
		booting.HostSN = "SN-booting-retry"
		booting.State = model.TaskStateBooting
		if _, err := s.CreateTask(ctx, booting); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.FindPendingTaskByIdentity(ctx, booting.HostSN, "", "")
		if err != nil {
			t.Fatalf("booting 任务必须能被查到——否则 MaxBootAttempts=3 的重试配额"+
				"一次都用不上（第一次引导就把任务推到 booting，机器重试时拿到 "+
				"no_task）。实际: %v", err)
		}
		if got.ID != booting.ID {
			t.Errorf("查到的任务 ID = %q, want %q", got.ID, booting.ID)
		}
	})

	t.Run("传入空SN不匹配host_sn也为空的任务", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		// host_sn 为空的 pending 任务先存在。
		emptyHostSN := baseTask("task-empty-host-sn")
		if _, err := s.CreateTask(ctx, emptyHostSN); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		// 调用方 SN 传空、但 MAC 命中另一个任务，验证不会误配对到
		// host_sn 为空的那个任务（因为 SN 参数本身为空，SN 这一层
		// 键直接跳过，不会拿空串去匹配 host_sn 为空的行）。
		macTask := baseTask("task-mac-only")
		macTask.HostMAC = "11:22:33:44:55:66"
		if _, err := s.CreateTask(ctx, macTask); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.FindPendingTaskByIdentity(ctx, "", "11:22:33:44:55:66", "")
		if err != nil {
			t.Fatalf("FindPendingTaskByIdentity() 失败: %v", err)
		}
		if got.ID != macTask.ID {
			t.Errorf("FindPendingTaskByIdentity() 命中 ID = %q, want %q（不应误配对到 host_sn 为空的任务）", got.ID, macTask.ID)
		}
	})
}

// ---------------------------------------------------------------------------
// GetTaskByToken
// ---------------------------------------------------------------------------

// TestGetTaskByToken 覆盖有效、已过期、purpose 不符、不存在四种情形——
// 四者的返回必须无法区分（都是 contract.ErrNotFound），不泄露 token
// 曾经存在过。
func TestGetTaskByToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	valid := baseTask("task-token-valid")
	valid.Token = "tok-valid"
	valid.TokenExpiresAt = future
	if _, err := s.CreateTask(ctx, valid); err != nil {
		t.Fatalf("CreateTask(valid) 失败: %v", err)
	}

	expired := baseTask("task-token-expired")
	expired.Token = "tok-expired"
	expired.TokenExpiresAt = past
	if _, err := s.CreateTask(ctx, expired); err != nil {
		t.Fatalf("CreateTask(expired) 失败: %v", err)
	}

	finalizeOnly := baseTask("task-token-finalize")
	finalizeOnly.FinalizeToken = "tok-finalize"
	finalizeOnly.FinalizeExpiresAt = future
	if _, err := s.CreateTask(ctx, finalizeOnly); err != nil {
		t.Fatalf("CreateTask(finalizeOnly) 失败: %v", err)
	}

	t.Run("有效", func(t *testing.T) {
		got, err := s.GetTaskByToken(ctx, "tok-valid", model.TokenPurposeInstall)
		if err != nil {
			t.Fatalf("GetTaskByToken(有效) 失败: %v", err)
		}
		if got.ID != valid.ID {
			t.Errorf("GetTaskByToken(有效) ID = %q, want %q", got.ID, valid.ID)
		}
	})

	t.Run("已过期", func(t *testing.T) {
		_, err := s.GetTaskByToken(ctx, "tok-expired", model.TokenPurposeInstall)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("GetTaskByToken(已过期) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
		}
	})

	t.Run("purpose不符", func(t *testing.T) {
		// tok-finalize 是 finalize token，用 install purpose 查应该
		// 查不到——install 用途查的是 token 列，不是 finalize_token 列。
		_, err := s.GetTaskByToken(ctx, "tok-finalize", model.TokenPurposeInstall)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("GetTaskByToken(purpose 不符) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		_, err := s.GetTaskByToken(ctx, "tok-never-issued", model.TokenPurposeInstall)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("GetTaskByToken(不存在) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
		}
	})
}

// ---------------------------------------------------------------------------
// CASTaskState
// ---------------------------------------------------------------------------

func TestCASTaskState(t *testing.T) {
	t.Run("正常转换且version自增", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-cas-normal")
		in.State = model.TaskStatePending
		in.Version = 1
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.CASTaskState(ctx, in.ID, model.TaskStatePending, 1, model.TaskStateBooting)
		if err != nil {
			t.Fatalf("CASTaskState() 失败: %v", err)
		}
		if got.State != model.TaskStateBooting {
			t.Errorf("CASTaskState() 后 State = %q, want %q", got.State, model.TaskStateBooting)
		}
		if got.Version != 2 {
			t.Errorf("CASTaskState() 后 Version = %d, want 2", got.Version)
		}
	})

	t.Run("version过期", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-cas-stale-version")
		in.State = model.TaskStatePending
		in.Version = 1
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		_, err := s.CASTaskState(ctx, in.ID, model.TaskStatePending, 99, model.TaskStateBooting)
		if !errors.Is(err, contract.ErrConflict) {
			t.Errorf("CASTaskState(version 过期) = %v, want errors.Is(_, contract.ErrConflict) = true", err)
		}
	})

	t.Run("from-state不符", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-cas-wrong-state")
		in.State = model.TaskStateBooting
		in.Version = 1
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		_, err := s.CASTaskState(ctx, in.ID, model.TaskStatePending, 1, model.TaskStateInstalling)
		if !errors.Is(err, contract.ErrConflict) {
			t.Errorf("CASTaskState(from-state 不符) = %v, want errors.Is(_, contract.ErrConflict) = true", err)
		}
	})

	t.Run("两者都不符", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-cas-both-wrong")
		in.State = model.TaskStateBooting
		in.Version = 1
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		_, err := s.CASTaskState(ctx, in.ID, model.TaskStatePending, 99, model.TaskStateInstalling)
		if !errors.Is(err, contract.ErrConflict) {
			t.Errorf("CASTaskState(两者都不符) = %v, want errors.Is(_, contract.ErrConflict) = true", err)
		}
	})
}

// ---------------------------------------------------------------------------
// MutateTaskFields
// ---------------------------------------------------------------------------

func TestMutateTaskFields(t *testing.T) {
	t.Run("改单字段时其余不变", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-mutate-single")
		in.AttemptCount = 1
		in.LastSeq = 5
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		newAttempt := 2
		got, err := s.MutateTaskFields(ctx, in.ID, model.TaskMutation{AttemptCount: &newAttempt})
		if err != nil {
			t.Fatalf("MutateTaskFields() 失败: %v", err)
		}
		if got.AttemptCount != 2 {
			t.Errorf("AttemptCount = %d, want 2", got.AttemptCount)
		}
		if got.LastSeq != 5 {
			t.Errorf("未改动的 LastSeq = %d, want 保持 5", got.LastSeq)
		}
		want := in
		want.AttemptCount = 2
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("改单字段后其余字段与预期不符 (-want +got):\n%s", diff)
		}
	})

	t.Run("四个可空时间列0到非0", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-mutate-zero-to-nonzero")
		// LastSignalAt/TokenExpiresAt/FinalizeExpiresAt 保持 0（未设置）。
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		newSignalAt := time.Now().Unix()
		newTokenExp := time.Now().Add(time.Hour).Unix()
		newFinalizeExp := time.Now().Add(2 * time.Hour).Unix()

		got, err := s.MutateTaskFields(ctx, in.ID, model.TaskMutation{
			LastSignalAt:      &newSignalAt,
			TokenExpiresAt:    &newTokenExp,
			FinalizeExpiresAt: &newFinalizeExp,
		})
		if err != nil {
			t.Fatalf("MutateTaskFields() 失败: %v", err)
		}
		if got.LastSignalAt != newSignalAt {
			t.Errorf("LastSignalAt = %d, want %d", got.LastSignalAt, newSignalAt)
		}
		if got.TokenExpiresAt != newTokenExp {
			t.Errorf("TokenExpiresAt = %d, want %d", got.TokenExpiresAt, newTokenExp)
		}
		if got.FinalizeExpiresAt != newFinalizeExp {
			t.Errorf("FinalizeExpiresAt = %d, want %d", got.FinalizeExpiresAt, newFinalizeExp)
		}
	})

	t.Run("非0到0验证写成了NULL", func(t *testing.T) {
		// 这是本任务最容易静默出错的一点（计划任务 5 风险提示）：GORM 的
		// Updates 默认忽略零值字段，若实现误用结构体形式的 Updates 而非
		// map 形式，这里会静默失败——字段看起来没变，但也不报错。
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-mutate-nonzero-to-zero")
		in.LastSignalAt = time.Now().Unix()
		in.TokenExpiresAt = time.Now().Add(time.Hour).Unix()
		in.FinalizeExpiresAt = time.Now().Add(2 * time.Hour).Unix()
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		var zero int64 = 0
		got, err := s.MutateTaskFields(ctx, in.ID, model.TaskMutation{
			LastSignalAt:      &zero,
			TokenExpiresAt:    &zero,
			FinalizeExpiresAt: &zero,
		})
		if err != nil {
			t.Fatalf("MutateTaskFields() 失败: %v", err)
		}
		if got.LastSignalAt != 0 {
			t.Errorf("LastSignalAt = %d, want 0（应已写成 SQL NULL 后读回 0）", got.LastSignalAt)
		}
		if got.TokenExpiresAt != 0 {
			t.Errorf("TokenExpiresAt = %d, want 0", got.TokenExpiresAt)
		}
		if got.FinalizeExpiresAt != 0 {
			t.Errorf("FinalizeExpiresAt = %d, want 0", got.FinalizeExpiresAt)
		}

		// 直接重新查一次，确保不是内存里的返回值凑巧对、而库里其实
		// 还是旧值（GetTask 走独立查询路径，能验证真正落库的状态）。
		reFetched, err := s.GetTask(ctx, in.ID)
		if err != nil {
			t.Fatalf("GetTask() 复查失败: %v", err)
		}
		if reFetched.LastSignalAt != 0 || reFetched.TokenExpiresAt != 0 || reFetched.FinalizeExpiresAt != 0 {
			t.Errorf("重新查询后三个可空时间列 = (%d, %d, %d), want 全部 0",
				reFetched.LastSignalAt, reFetched.TokenExpiresAt, reFetched.FinalizeExpiresAt)
		}
	})

	t.Run("空mutation不改变任何字段", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-mutate-empty")
		in.AttemptCount = 3
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.MutateTaskFields(ctx, in.ID, model.TaskMutation{})
		if err != nil {
			t.Fatalf("MutateTaskFields(空 mutation) 失败: %v", err)
		}
		if diff := cmp.Diff(in, got); diff != "" {
			t.Errorf("空 mutation 后任务被改变 (-want +got):\n%s", diff)
		}
	})

	t.Run("不存在的任务返回NotFound", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		newAttempt := 1
		_, err := s.MutateTaskFields(ctx, "no-such-task", model.TaskMutation{AttemptCount: &newAttempt})
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("MutateTaskFields(不存在的任务) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ListTimedOutTasks
// ---------------------------------------------------------------------------

func TestListTimedOutTasks(t *testing.T) {
	t.Run("超预算非终态被返回", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		longAgo := int64(1) // 任何预算都会判超时

		in := baseTask("task-timedout")
		in.State = model.TaskStateBooting
		in.CurrentPhase = model.PhaseBooting
		in.PhaseEnteredAt = longAgo
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.ListTimedOutTasks(ctx, time.Now().Unix())
		if err != nil {
			t.Fatalf("ListTimedOutTasks() 失败: %v", err)
		}
		if !containsTaskID(got, in.ID) {
			t.Errorf("ListTimedOutTasks() 未包含超预算的任务 %q", in.ID)
		}
	})

	t.Run("pending永不被返回", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		longAgo := int64(1)

		in := baseTask("task-pending-not-timedout")
		in.State = model.TaskStatePending
		in.CurrentPhase = model.PhasePending
		in.PhaseEnteredAt = longAgo
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.ListTimedOutTasks(ctx, time.Now().Unix())
		if err != nil {
			t.Fatalf("ListTimedOutTasks() 失败: %v", err)
		}
		if containsTaskID(got, in.ID) {
			t.Errorf("ListTimedOutTasks() 包含了 pending 任务 %q，pending 阶段无预算，永不应被判超时", in.ID)
		}
	})

	t.Run("未超预算不返回", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		in := baseTask("task-not-timedout-yet")
		in.State = model.TaskStateBooting
		in.CurrentPhase = model.PhaseBooting
		in.PhaseEnteredAt = time.Now().Unix() // 刚进入，远未到预算
		if _, err := s.CreateTask(ctx, in); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.ListTimedOutTasks(ctx, time.Now().Unix())
		if err != nil {
			t.Fatalf("ListTimedOutTasks() 失败: %v", err)
		}
		if containsTaskID(got, in.ID) {
			t.Errorf("ListTimedOutTasks() 包含了未超预算的任务 %q", in.ID)
		}
	})

	t.Run("终态任务永不被返回", func(t *testing.T) {
		s := newTestStore(t)
		ctx := context.Background()

		longAgo := int64(1)

		done := baseTask("task-done-not-timedout")
		done.State = model.TaskStateDone
		done.CurrentPhase = model.PhaseInitializing
		done.PhaseEnteredAt = longAgo
		if _, err := s.CreateTask(ctx, done); err != nil {
			t.Fatalf("CreateTask() 失败: %v", err)
		}

		got, err := s.ListTimedOutTasks(ctx, time.Now().Unix())
		if err != nil {
			t.Fatalf("ListTimedOutTasks() 失败: %v", err)
		}
		if containsTaskID(got, done.ID) {
			t.Errorf("ListTimedOutTasks() 包含了终态（done）任务 %q", done.ID)
		}
	})
}

func containsTaskID(tasks []model.Task, id string) bool {
	for _, tk := range tasks {
		if tk.ID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// ListTasks
// ---------------------------------------------------------------------------

// TestListTasks_FilterByStateAndHostSN 覆盖 TaskFilter 的两个字段过滤，
// 零值不过滤——补充计划文档未在"测试必须覆盖"清单里明列、但方法本身
// 存在的过滤语义，不测等于该方法的核心行为完全没被验证过。
func TestListTasks_FilterByStateAndHostSN(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	pendingA := baseTask("task-list-pending-a")
	pendingA.HostSN = "SN-A"
	pendingA.State = model.TaskStatePending
	if _, err := s.CreateTask(ctx, pendingA); err != nil {
		t.Fatalf("CreateTask() 失败: %v", err)
	}

	bootingB := baseTask("task-list-booting-b")
	bootingB.HostSN = "SN-B"
	bootingB.State = model.TaskStateBooting
	if _, err := s.CreateTask(ctx, bootingB); err != nil {
		t.Fatalf("CreateTask() 失败: %v", err)
	}

	t.Run("不过滤返回全部", func(t *testing.T) {
		got, err := s.ListTasks(ctx, contract.TaskFilter{})
		if err != nil {
			t.Fatalf("ListTasks() 失败: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("ListTasks(无过滤) 返回 %d 条, want 2", len(got))
		}
	})

	t.Run("按State过滤", func(t *testing.T) {
		got, err := s.ListTasks(ctx, contract.TaskFilter{State: model.TaskStatePending})
		if err != nil {
			t.Fatalf("ListTasks() 失败: %v", err)
		}
		if len(got) != 1 || got[0].ID != pendingA.ID {
			t.Errorf("ListTasks(State=pending) 返回 %v, want 只包含 %q", got, pendingA.ID)
		}
	})

	t.Run("按HostSN过滤", func(t *testing.T) {
		got, err := s.ListTasks(ctx, contract.TaskFilter{HostSN: "SN-B"})
		if err != nil {
			t.Fatalf("ListTasks() 失败: %v", err)
		}
		if len(got) != 1 || got[0].ID != bootingB.ID {
			t.Errorf("ListTasks(HostSN=SN-B) 返回 %v, want 只包含 %q", got, bootingB.ID)
		}
	})
}
