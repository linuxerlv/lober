// 本文件锁住「booting 状态的任务接受重复引导请求」这条 SPEC 明文要求。
//
// **缺了它，MaxBootAttempts = 3 这个配额一次都用不上。** SPEC 对重试有三处
// 明文规定：
//
//	§3.3 阶段预算表   | booting | 15 min | 允许重试，attempt++ |
//	§9.1 第三道闸门   同一任务窗口内菜单最多取 3 次，超过即 failed
//	§4.4             「一次性失效会直接掐死正常重试」
//
// 而 FindPendingTaskByIdentity 只匹配 state=pending，StartBoot 又有两道
// pending 守卫，于是第一次引导把任务推到 booting 之后，同一台机器再来就查不到
// 任务、拿到 no_task。
//
// PXE 引导重试是常态而非异常：取 kernel 时网络抖动、initrd 校验失败、机器在
// BIOS 里被人手动重启、iPXE 自己的 retry——任何一种都会让机器回来再问一次。
// 实测（2026-09-25，QEMU BIOS 目标机）：第一次引导成功取到菜单，任务进
// booting；机器重试时拿到
//
//	http://gw.vmlab.internal/v1/boot... ok
//	lober: no task for
//	Booting from SAN device 0x80
//	Boot from SAN device 0x80 failed: Input/output error
//
// 现场症状同样是「机器在 PXE 里转圈」，而服务端侧任务显示 booting、看起来
// 一切正常，只是永远不会前进。
package statemachine

import (
	"context"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// newRetryFixture 造一台 pending 任务与一个 StartBoot 可用的状态机。
func newRetryFixture(t *testing.T, id string) (context.Context, *fake.Store, *fake.Clock, *Machine, string) {
	t.Helper()
	ctx := context.Background()
	store := fake.NewStore()
	clock := fake.NewClock()
	m := newBootTestMachine(store, clock, alwaysPassPrechecker{})
	task := pendingTaskWithArtifact(t, store, id, "artifact-retry-"+id)
	return ctx, store, clock, m, task.ID
}

// advancePastRetryInterval 把时钟推过最短重试间隔——模拟机器真的跑了一圈
// （下 kernel、加载失败、重启、重走 DHCP）之后回来，而不是同一次引导的并发
// 重复请求。见 minBootRetryInterval 的文档。
func advancePastRetryInterval(clock *fake.Clock) {
	clock.Advance(minBootRetryInterval + time.Second)
}

// TestStartBoot_BootingTaskAcceptsRetry 锁住 booting 任务能再次引导，且
// attempt 递增。
func TestStartBoot_BootingTaskAcceptsRetry(t *testing.T) {
	ctx, _, clock, m, taskID := newRetryFixture(t, "task-retry-1")

	first, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS)
	if err != nil {
		t.Fatalf("第一次引导应成功: %v", err)
	}
	if first.State != model.TaskStateBooting {
		t.Fatalf("第一次引导后 state = %q，想要 booting", first.State)
	}
	if first.AttemptCount != 1 {
		t.Fatalf("第一次引导后 attempt = %d，想要 1", first.AttemptCount)
	}

	// 推过最短重试间隔：模拟机器真的跑了一圈回来，而非并发重复请求。
	advancePastRetryInterval(clock)

	second, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS)
	if err != nil {
		t.Fatalf("booting 任务应接受重试（SPEC §3.3「允许重试，attempt++」），"+
			"实际报错: %v——这会让 MaxBootAttempts=3 的配额一次都用不上", err)
	}
	if second.AttemptCount != 2 {
		t.Errorf("重试后 attempt = %d，想要 2", second.AttemptCount)
	}
	if second.State != model.TaskStateBooting {
		t.Errorf("重试后 state = %q，想要仍是 booting", second.State)
	}
	// 每次重试都签新 token：旧 token 可能已经泄漏在失败现场的日志里
	// （iPXE 会把完整 URL 打在屏幕上），而它带着取应答文件的权限。
	if second.Token == first.Token {
		t.Error("重试应签发新 token——旧 token 可能已随失败现场的 iPXE 屏显泄漏，" +
			"而它带着取应答文件的权限")
	}
}

// TestStartBoot_RetryExhaustsAtMaxAttempts 锁住第 4 次引导被拒并置 failed。
//
// 这是 §9.1 第三道闸门：配额用尽后不再签 token，任务置
// failed(attempt_exceeded)，机器拿到 sanboot 回落本地盘。没有这个上限，一台
// 反复引导失败的机器会无限次取到应答文件。
func TestStartBoot_RetryExhaustsAtMaxAttempts(t *testing.T) {
	ctx, store, clock, m, taskID := newRetryFixture(t, "task-retry-exhaust")

	for i := 1; i <= model.MaxBootAttempts; i++ {
		if i > 1 {
			advancePastRetryInterval(clock)
		}
		got, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS)
		if err != nil {
			t.Fatalf("第 %d 次引导应成功（上限是 %d）: %v", i, model.MaxBootAttempts, err)
		}
		if got.AttemptCount != i {
			t.Fatalf("第 %d 次引导后 attempt = %d", i, got.AttemptCount)
		}
	}

	advancePastRetryInterval(clock)
	final, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS)
	if err != nil {
		// 置 failed 的路径可能返回错误也可能返回任务，两种都接受，
		// 关键是最终状态必须是 failed。
		got, getErr := store.GetTask(ctx, taskID)
		if getErr != nil {
			t.Fatalf("取任务失败: %v", getErr)
		}
		final = got
	}
	if final.State != model.TaskStateFailed {
		t.Errorf("第 %d 次引导后 state = %q，想要 failed——超过 MaxBootAttempts "+
			"必须置 failed，否则一台反复失败的机器会无限次取到应答文件",
			model.MaxBootAttempts+1, final.State)
	}
	if final.FailureReason != model.FailureReasonAttemptExceeded {
		t.Errorf("失败原因 = %q，想要 %q", final.FailureReason, model.FailureReasonAttemptExceeded)
	}
}

// TestStartBoot_TerminalTaskStillRejected 锁住终态任务仍然被拒。
//
// 放宽 booting 不等于放宽一切：done 的任务再接受引导会把一台已交付的生产机
// 重新擦盘装机——这是本系统最严重的一种误操作，§9.1 的闸门存在的首要理由。
func TestStartBoot_TerminalTaskStillRejected(t *testing.T) {
	for _, state := range []model.TaskState{model.TaskStateDone, model.TaskStateFailed} {
		ctx, store, _, m, taskID := newRetryFixture(t, "task-terminal-"+string(state))
		cur, err := store.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("取任务失败: %v", err)
		}
		if _, err := store.CASTaskState(ctx, taskID, model.TaskStatePending, cur.Version, state); err != nil {
			t.Fatalf("把任务推到 %s 失败: %v", state, err)
		}

		if _, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS); err == nil {
			t.Errorf("%s 状态的任务必须拒绝引导请求——done 的任务再接受引导会把"+
				"一台已交付的生产机重新擦盘装机", state)
		}
	}
}

// TestStartBoot_ConcurrentDuplicateDeduped 锁住「同一次引导的并发重复请求只
// 签一个 token」。
//
// 这条与 TestStartBoot_BootingTaskAcceptsRetry 是一对：两者外观相同（都是对
// booting 任务再发引导请求），语义完全相反，判据是时间间隔。不去重的后果是
// 一次引导消耗多个 attempt 配额、签发多个 token（旧 token 并不失效，暴露面
// 随之变大）。
//
// iPXE 在某些固件上会对同一个 URL 发两次，网络重传也会造成同样效果。
func TestStartBoot_ConcurrentDuplicateDeduped(t *testing.T) {
	ctx, store, _, m, taskID := newRetryFixture(t, "task-retry-dup")

	first, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS)
	if err != nil {
		t.Fatalf("第一次引导应成功: %v", err)
	}

	// 不推进时钟：这就是同一次引导的并发重复。
	if _, err := m.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareBIOS); err == nil {
		t.Error("同一时刻的重复引导请求必须被拒——否则一次引导会消耗多个 attempt " +
			"配额并签发多个 token，而旧 token 并不失效")
	}

	got, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("取任务失败: %v", err)
	}
	if got.AttemptCount != 1 {
		t.Errorf("并发重复后 attempt = %d，想要 1（只算一次引导）", got.AttemptCount)
	}
	if got.Token != first.Token {
		t.Error("并发重复不应换 token——真正在跑的那次引导还在用它取应答文件")
	}
}
