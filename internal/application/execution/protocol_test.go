package execution

import (
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
	"strings"
	"testing"
)

func TestSignalCredentialAndDuplicateOrderingRemainsCompatible(t *testing.T) {
	if ErrProtocol != executionrules.ErrProtocol {
		t.Fatal("错误哨兵不是同一对象")
	}
	r := &clockRepo{task: credentialTask()}
	calls := 0
	s, _ := New(r, []byte(strings.Repeat("k", 32)), func() int64 { calls++; return 1100 }, nil)
	r.task.State = "done"
	if err := s.Signal(t.Context(), r.task.ID, s.Token(r.task, Install), Signal{Phase: "installed"}); err != ErrCredential || calls != 1 {
		t.Fatal(err, calls)
	}
	calls = 0
	if err := s.Signal(t.Context(), r.task.ID, s.Token(r.task, Finalize), Signal{Phase: "ready"}); err != ErrProtocol || calls != 2 {
		t.Fatal(err, calls)
	}
	calls = 0
	if err := s.Signal(t.Context(), r.task.ID, "bad", Signal{Phase: "ready"}); err != ErrCredential || calls != 0 {
		t.Fatal(err, calls)
	}
}

func TestExecutionEventKeyCompatibility(t *testing.T) {
	sig := Signal{Phase: "hook-report", Slot: 1, Instance: 1, Digest: strings.Repeat("a", 64)}
	if got := attemptKey(credentialTask(), executionrules.SignalKey(sig)); got != "attempt-1-hook-report-1-1" {
		t.Fatal(got)
	}
}

func TestSlotEndReportsTheStageAfterCompletion(t *testing.T) {
	r := &clockRepo{task: credentialTask()}
	r.task.Snapshot.Slots = []model.SnapshotSlot{
		{Seq: 1, Exec: true, Timing: "pre"},
		{Seq: 2, Exec: true, Timing: "pre"},
	}
	s, err := New(r, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	token := s.Token(r.task, Install)
	for i, wantStage := range []string{"pre", "packages"} {
		for _, phase := range []model.SignalPhase{model.PhaseSlotStart, model.PhaseSlotEnd} {
			if err := s.Signal(t.Context(), r.task.ID, token, Signal{Phase: phase, Slot: i + 1, Timing: "pre"}); err != nil {
				t.Fatal(err)
			}
		}
		event := r.events[len(r.events)-1]
		if event.Kind != "slot.end" || event.Fields["stage"] != wantStage {
			t.Errorf("第 %d 项装前脚本结束后应回报阶段 %q，得到 %+v", i+1, wantStage, event)
		}
	}
}
