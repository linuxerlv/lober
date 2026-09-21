package model

import (
	"reflect"
	"testing"
)

// TestFailureReason_Reinstallable 逐一验证九种失败原因的可重装判定。
// 这是本模块唯一真正危险的逻辑（§3.4）：判错方向会直接导致生产机被误擦。
func TestFailureReason_Reinstallable(t *testing.T) {
	cases := []struct {
		reason FailureReason
		want   bool
	}{
		{FailureReasonBootTimeout, true},
		{FailureReasonAttemptExceeded, true},
		{FailureReasonInstallPhase, true},
		{FailureReasonHookRequired, true},
		{FailureReasonPrecheck, true},
		{FailureReasonCanceled, true},
		{FailureReasonRebootTimeout, false},
		{FailureReasonInitTimeout, false},
		{FailureReasonInitFailed, false},
	}
	for _, c := range cases {
		got := c.reason.Reinstallable()
		if got != c.want {
			t.Errorf("FailureReason(%q).Reinstallable() = %v, want %v —— "+
				"SPEC §3.4 明确规定该失败原因的重装许可，判反会导致生产机被误擦或运维被无谓阻拦",
				c.reason, got, c.want)
		}
	}
}

// TestFailureReason_UnknownDefaultsToNotReinstallable 锁住白名单查表的方向。
// 不锁住的话，将来新增失败原因时忘了归类，会默默变成「可重装」——
// 而那意味着可能擦掉一台已经装好的机器（SPEC §3.4，计划任务 2 设计要点）。
func TestFailureReason_UnknownDefaultsToNotReinstallable(t *testing.T) {
	unknown := FailureReason("some_future_reason_nobody_classified_yet")
	if unknown.Reinstallable() {
		t.Errorf("未知失败原因 %q 的 Reinstallable() 返回 true，want false —— "+
			"白名单查表的核心保障是「未列入的一律不可重装」，一旦此测试变绿，"+
			"任何忘了归类的新失败原因都会被当作允许重装，可能擦掉生产机（SPEC §3.4）",
			unknown)
	}
}

// TestTaskState_IsTerminal 验证终态与非终态的判定（§3.1）。
func TestTaskState_IsTerminal(t *testing.T) {
	terminal := []TaskState{TaskStateDone, TaskStateFailed}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("TaskState(%q).IsTerminal() = false, want true —— "+
				"SPEC §3.1 明确 done 与 failed 是唯二终态", s)
		}
	}

	nonTerminal := []TaskState{
		TaskStatePending, TaskStateBooting, TaskStateInstalling,
		TaskStateRebooting, TaskStateInitializing,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("TaskState(%q).IsTerminal() = true, want false —— "+
				"该状态仍在装机流程中，误判为终态会导致超时扫描或状态机把它当作已结束",
				s)
		}
	}
}

// TestTaskMutation_NoStateField 用编译期检查确认 TaskMutation 不含 State
// 字段。改运行时字段时携带状态必须报错，这是防止绕过 CAS 与「禁止重装」
// 判定的唯一屏障（计划任务 10 设计要点）；从类型层面直接不提供这个字段，
// 比运行期检查更彻底。
//
// 本测试用字面量初始化每个已知字段，若将来有人加了 State 字段，
// 这段字面量仍能编译通过——因此额外用 reflect 检查字段名单，
// 确保「不含 State」这条约束真正被测试锁住。
func TestTaskMutation_NoStateField(t *testing.T) {
	typ := reflect.TypeOf(TaskMutation{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name == "State" {
			t.Fatalf("TaskMutation 含 State 字段——它是 Store 改运行时字段的统一入参，" +
				"一旦允许携带 State，就出现了一条不走 CAS 的改状态路径，" +
				"迟早被用来绕过「禁止重装」的判定（SPEC §3.4，计划任务 2 设计要点）")
		}
	}
}
