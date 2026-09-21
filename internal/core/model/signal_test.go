package model

import "testing"

// TestSignal_FlowSequenceStrictlyIncreasing 验证十一个流程信号的序号严格
// 递增。这是「只进不退」判据的全部基础（SPEC §3.6）：延迟到达的低序号信号
// 不能把已经推进到高序号状态的任务打回去，前提是序号本身单调。
func TestSignal_FlowSequenceStrictlyIncreasing(t *testing.T) {
	order := []SignalKind{
		SignalPreStart, SignalPreDone,
		SignalPackagesStart, SignalPackagesDone,
		SignalPostStart, SignalPostDone,
		SignalRebooting, SignalBooted,
		SignalInitStart, SignalInitDone, SignalReady,
	}
	prev := -1
	for _, kind := range order {
		entry, ok := LookupSignal(kind)
		if !ok {
			t.Fatalf("信号 %q 查不到表项——十一个流程信号必须全部在表中", kind)
		}
		if entry.Seq <= prev {
			t.Errorf("信号 %q 的序号 %d 未严格大于前一个信号的序号 %d —— "+
				"SPEC §3.6 要求信号序号严格递增，一旦出现相等或倒退，"+
				"「延迟到达的旧信号不能打回新状态」这条判据就会失效",
				kind, entry.Seq, prev)
		}
		prev = entry.Seq
	}
}

// TestSignal_HookReportIsZeroAndDoesNotAdvance 验证 hook-report 的序号为 0
// 且不推进状态与阶段（§6.1）：它与阶段正交，一个阶段内可来任意多次，
// 永不推进水位。
func TestSignal_HookReportIsZeroAndDoesNotAdvance(t *testing.T) {
	entry, ok := LookupSignal(SignalHookReport)
	if !ok {
		t.Fatalf("信号 hook-report 查不到表项")
	}
	if entry.Seq != 0 {
		t.Errorf("hook-report 的序号 = %d, want 0 —— "+
			"它必须永远不大于任何真实水位，否则会被误判为「推进了状态」", entry.Seq)
	}
	var zeroState TaskState
	var zeroPhase Phase
	if entry.State != zeroState || entry.Phase != zeroPhase {
		t.Errorf("hook-report 携带了 State=%q Phase=%q，want 均为零值 —— "+
			"hook-report 与阶段正交，不应推进任务的状态或阶段（SPEC §6.1）",
			entry.State, entry.Phase)
	}
}

// TestSignal_KeyTransitions 验证关键信号的状态跳转（§3.1、§6.1）。
func TestSignal_KeyTransitions(t *testing.T) {
	cases := []struct {
		kind      SignalKind
		wantState TaskState
	}{
		{SignalRebooting, TaskStateRebooting},
		{SignalBooted, TaskStateInitializing},
		{SignalReady, TaskStateDone},
	}
	for _, c := range cases {
		entry, ok := LookupSignal(c.kind)
		if !ok {
			t.Fatalf("信号 %q 查不到表项", c.kind)
		}
		if entry.State != c.wantState {
			t.Errorf("信号 %q 推进到状态 %q, want %q —— "+
				"这三条跳转是状态机的骨架（SPEC §3.1），错一条整条流程就断了",
				c.kind, entry.State, c.wantState)
		}
	}
}

// TestSignal_UnknownRejected 验证未知信号名被拒绝。信号名来自 URL 路径段，
// 不查表校验就等于让机器指定自己的阶段（SPEC §6.1）。
func TestSignal_UnknownRejected(t *testing.T) {
	unknown := SignalKind("totally-made-up-signal")
	if _, ok := LookupSignal(unknown); ok {
		t.Errorf("未知信号 %q 被 LookupSignal 接受，want 拒绝（ok=false）—— "+
			"信号名是机器可控的 URL 路径段，不查表校验等于让机器自行指定阶段，"+
			"是一个协议级的安全洞（SPEC §6.1）", unknown)
	}
}

// TestSignal_FinalizeTokenScope 验证 finalize token 有权发送的信号集合恰好是
// booted / init-start / init-done / ready / hook-report 五个（SPEC §6.4）。
func TestSignal_FinalizeTokenScope(t *testing.T) {
	allowed := []SignalKind{
		SignalBooted, SignalInitStart, SignalInitDone, SignalReady, SignalHookReport,
	}
	for _, kind := range allowed {
		if !FinalizeCanSend(kind) {
			t.Errorf("FinalizeCanSend(%q) = false, want true —— "+
				"SPEC §6.4 明确 finalize token 有权发送重启后阶段的这几个信号，"+
				"缺失会导致重启后的正常流程被误拒", kind)
		}
	}

	denied := []SignalKind{
		SignalPreStart, SignalPreDone, SignalPackagesStart, SignalPackagesDone,
		SignalPostStart, SignalPostDone, SignalRebooting,
	}
	for _, kind := range denied {
		if FinalizeCanSend(kind) {
			t.Errorf("FinalizeCanSend(%q) = true, want false —— "+
				"这些是装机阶段的信号，finalize token 权限过宽会让重启后的凭据"+
				"能干装机 token 的事，违反 SPEC §6.4 的权限隔离设计", kind)
		}
	}
}
