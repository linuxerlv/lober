package model

import (
	"testing"
	"time"
)

// TestPhaseBudget_PendingHasNoBudget 锁住 pending 查不到预算这条行为。
// 不锁住的话，将来某次「顺手补个默认超时」的改动会静默把所有长期挂着的任务
// 判成失败（SPEC §3.3、§0.1：机器何时上电不在本系统控制范围内）。
func TestPhaseBudget_PendingHasNoBudget(t *testing.T) {
	if _, ok := PhaseBudget(PhasePending); ok {
		t.Errorf("PhaseBudget(pending) 返回 ok=true, want false —— " +
			"pending 阶段必须查不到预算，用「键不存在」表达「永不超时」，" +
			"否则运维上午建任务下午才上电的正常流程会被误判超时（SPEC §3.3）")
	}
	if _, ok := PhaseTimeoutReason(PhasePending); ok {
		t.Errorf("PhaseTimeoutReason(pending) 返回 ok=true, want false —— " +
			"没有预算就不该有对应的超时失败原因，两者必须同步缺失")
	}
}

// TestPhaseBudget_ExactValues 精确匹配六个阶段的预算值（SPEC §3.3）。
func TestPhaseBudget_ExactValues(t *testing.T) {
	cases := []struct {
		phase Phase
		want  time.Duration
	}{
		{PhaseBooting, 15 * time.Minute},
		{PhasePre, 10 * time.Minute},
		{PhasePackages, 60 * time.Minute},
		{PhasePost, 20 * time.Minute},
		{PhaseRebooting, 8 * time.Minute},
		{PhaseInitializing, 15 * time.Minute},
	}
	for _, c := range cases {
		got, ok := PhaseBudget(c.phase)
		if !ok {
			t.Fatalf("PhaseBudget(%q) 查不到预算，want %v —— SPEC §3.3 明确规定该阶段的预算值", c.phase, c.want)
		}
		if got != c.want {
			t.Errorf("PhaseBudget(%q) = %v, want %v —— "+
				"预算值抄错会让某个阶段过早或过晚报超时，与 SPEC §3.3 的表不符",
				c.phase, got, c.want)
		}
	}
}

// TestPhaseTimeoutReason_ExactMapping 精确匹配六个阶段到失败原因的映射
// （SPEC §3.3、§3.4）。
func TestPhaseTimeoutReason_ExactMapping(t *testing.T) {
	cases := []struct {
		phase Phase
		want  FailureReason
	}{
		{PhaseBooting, FailureReasonBootTimeout},
		{PhasePre, FailureReasonInstallPhase},
		{PhasePackages, FailureReasonInstallPhase},
		{PhasePost, FailureReasonInstallPhase},
		{PhaseRebooting, FailureReasonRebootTimeout},
		{PhaseInitializing, FailureReasonInitTimeout},
	}
	for _, c := range cases {
		got, ok := PhaseTimeoutReason(c.phase)
		if !ok {
			t.Fatalf("PhaseTimeoutReason(%q) 查不到失败原因，want %q", c.phase, c.want)
		}
		if got != c.want {
			t.Errorf("PhaseTimeoutReason(%q) = %q, want %q —— "+
				"超时原因映射错了会导致运维看到的失败原因与实际阶段不符",
				c.phase, got, c.want)
		}
	}
}

// TestPhaseTimeoutReason_RebootAndInitAreNotReinstallable 把预算表与
// FailureReason 的可重装判定连起来断言：rebooting 与 initializing 的超时
// 原因必须落在不可重装那一组。
//
// 这是防止「改了超时原因映射却没想到重装后果」的唯一屏障——两张表分别看都
// 可能各自正确，但组合起来才是真正的安全约束（SPEC §3.3、§3.4）。
func TestPhaseTimeoutReason_RebootAndInitAreNotReinstallable(t *testing.T) {
	dangerousPhases := []Phase{PhaseRebooting, PhaseInitializing}
	for _, phase := range dangerousPhases {
		reason, ok := PhaseTimeoutReason(phase)
		if !ok {
			t.Fatalf("PhaseTimeoutReason(%q) 查不到失败原因", phase)
		}
		if reason.Reinstallable() {
			t.Errorf("阶段 %q 超时后的失败原因 %q 的 Reinstallable() = true, want false —— "+
				"该阶段的机器已经装好且盘上可能已有数据，若超时原因被误改成可重装的值，"+
				"会导致状态机允许重装这台机器、擦掉生产数据（SPEC §3.4）",
				phase, reason)
		}
	}
}

// TestBudgetedPhases_MatchesBudgetTable 验证「有预算的阶段清单」与预算表
// 本身完全对应——供 Store 构造超时扫描查询使用，若两者不同步，扫描会漏扫
// 或多扫某个阶段。
func TestBudgetedPhases_MatchesBudgetTable(t *testing.T) {
	got := BudgetedPhases()
	if len(got) != 6 {
		t.Fatalf("BudgetedPhases() 返回 %d 个阶段, want 6", len(got))
	}
	for _, phase := range got {
		if _, ok := PhaseBudget(phase); !ok {
			t.Errorf("BudgetedPhases() 包含 %q，但 PhaseBudget(%q) 查不到预算——两者必须同步", phase, phase)
		}
	}
	for _, phase := range []Phase{PhaseBooting, PhasePre, PhasePackages, PhasePost, PhaseRebooting, PhaseInitializing} {
		found := false
		for _, p := range got {
			if p == phase {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("BudgetedPhases() 缺少阶段 %q —— 超时扫描会漏扫这个阶段", phase)
		}
	}
	if _, ok := PhaseBudget(PhasePending); ok {
		t.Fatalf("PhaseBudget(pending) 意外返回 ok=true，测试前提被破坏")
	}
	for _, phase := range got {
		if phase == PhasePending {
			t.Errorf("BudgetedPhases() 包含 pending —— pending 必须永远不出现在有预算的阶段清单中（SPEC §3.3）")
		}
	}
}
