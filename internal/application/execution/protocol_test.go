package execution

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func taskFixture() model.Task {
	return model.Task{ID: "task-one", HostSN: "SN-ONE", State: model.TaskInstalling, Rev: 2,
		Execution: &model.Execution{Attempt: 1, BootCount: 1, IssuedAt: 1000, PhaseAt: 1000, Deadline: 6400, Signals: map[string]string{}},
		Snapshot: model.Snapshot{Family: "linux", Series: "rocky", Format: "kickstart", ISO: model.SnapshotISO{Arch: "x86_64"}, Slots: []model.SnapshotSlot{
			{Seq: 1, Name: "setup", Exec: true, Timing: catalog.TimingPostTarget, Instances: []model.SnapshotInstance{{Required: true, Digest: strings.Repeat("a", 64)}}},
			{Seq: 2, Name: "init", Exec: true, Timing: catalog.TimingFirstboot, Instances: []model.SnapshotInstance{{Required: true, Digest: strings.Repeat("b", 64)}, {Required: false, Digest: strings.Repeat("c", 64)}}},
		}}}
}

func TestCredentialsBindTaskAttemptScopeAndExpiry(t *testing.T) {
	s, err := New(nil, []byte(strings.Repeat("k", 32)), func() int64 { return 1100 }, nil)
	if err != nil {
		t.Fatal(err)
	}
	task := taskFixture()
	install, final := s.Token(task, Install), s.Token(task, Finalize)
	if install == final {
		t.Fatal("scope credentials coincide")
	}
	if err := s.Authorize(task, install, Install); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(task, final, Install); err == nil {
		t.Fatal("finalize credential can download installation files")
	}
	task.ID = "other"
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("cross-task token accepted")
	}
	task.ID = "task-one"
	task.Execution.Attempt++
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("old attempt accepted")
	}
	task.Execution.Attempt = 1
	s.now = func() int64 { return 6400 }
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("expired install accepted")
	}
	if s.Authorize(task, final, Finalize) != nil {
		t.Fatal("finalize expires with install")
	}
	s.now = func() int64 { return 87400 }
	if s.Authorize(task, final, Finalize) == nil {
		t.Fatal("expired finalize accepted")
	}
	s.now = func() int64 { return 1100 }
	task.State = model.TaskRebooting
	if s.Authorize(task, install, Install) == nil {
		t.Fatal("post-reboot file access accepted")
	}
	if _, err := New(nil, []byte("weak"), s.now, nil); err == nil {
		t.Fatal("short signing key accepted")
	}
}

func apply(t *testing.T, task model.Task, sig Signal) model.Task {
	t.Helper()
	next, duplicate, err := advance(task, sig, 1100)
	if err != nil || duplicate {
		t.Fatalf("advance %s: duplicate=%v err=%v", sig.Phase, duplicate, err)
	}
	return next
}
func slotSignal(phase string, slot, instance int, digest string, rc int) Signal {
	timing := catalog.TimingPostTarget
	if slot == 2 {
		timing = catalog.TimingFirstboot
	}
	return Signal{Phase: phase, Slot: slot, Timing: timing, Instance: instance, Digest: digest, RC: rc}
}
func completeSlot(t *testing.T, task model.Task, slot int) model.Task {
	task = apply(t, task, slotSignal("slot-start", slot, 0, "", 0))
	for i, hook := range task.Snapshot.Slots[slot-1].Instances {
		task = apply(t, task, slotSignal("hook-start", slot, i+1, hook.Digest, 0))
		task = apply(t, task, slotSignal("hook-report", slot, i+1, hook.Digest, 0))
	}
	return apply(t, task, slotSignal("slot-end", slot, 0, "", 0))
}
func TestProtocolRequiresFrozenHooksAndExactDuplicateReports(t *testing.T) {
	task := taskFixture()
	bad := slotSignal("hook-report", 1, 1, strings.Repeat("f", 64), 0)
	if _, _, err := advance(task, bad, 1100); err == nil {
		t.Fatal("unknown hook digest accepted")
	}
	if _, _, err := advance(task, Signal{Phase: "ready"}, 1100); err == nil {
		t.Fatal("ready bypasses required progress")
	}
	task = apply(t, task, slotSignal("slot-start", 1, 0, "", 0))
	task = apply(t, task, slotSignal("hook-start", 1, 1, strings.Repeat("a", 64), 0))
	report := slotSignal("hook-report", 1, 1, strings.Repeat("a", 64), 0)
	report.Body = []byte{'o', 0, 'k'}
	task = apply(t, task, report)
	next, duplicate, err := advance(task, report, 1101)
	if err != nil || !duplicate || next.State != task.State {
		t.Fatal("exact retransmission is not idempotent", err)
	}
	report.RC = 1
	if _, _, err := advance(task, report, 1102); err == nil {
		t.Fatal("contradictory duplicate accepted")
	}
	if task.Execution.Signals[signalKey(report)] == "" {
		t.Fatal("signal not durable")
	}
}
func TestProtocolTracksInstallRebootInitializationAndDone(t *testing.T) {
	task := completeSlot(t, taskFixture(), 1)
	task = apply(t, task, Signal{Phase: "installed"})
	if task.State != model.TaskRebooting || !task.Execution.ReachedReboot {
		t.Fatal("missing irreversible reboot marker")
	}
	if _, _, err := advance(task, slotSignal("slot-start", 1, 0, "", 0), 1102); err == nil {
		t.Fatal("installer re-entry accepted")
	}
	task = apply(t, task, Signal{Phase: "booted"})
	task = apply(t, task, Signal{Phase: "init-start"})
	if task.State != model.TaskInitializing {
		t.Fatal("not initializing")
	}
	if _, _, err := advance(task, Signal{Phase: "init-done"}, 1102); err == nil {
		t.Fatal("init-done bypasses hooks")
	}
	task = completeSlot(t, task, 2)
	task = apply(t, task, Signal{Phase: "init-done"})
	task = apply(t, task, Signal{Phase: "ready"})
	if task.State != model.TaskDone {
		t.Fatal("not done")
	}
	if _, _, err := advance(task, Signal{Phase: "booted", Body: []byte("changed")}, 1102); err == nil {
		t.Fatal("terminal task revived")
	}
	if len(CanRetry(task)) == 0 {
		t.Fatal("done retry allowed")
	}
	task.State = model.TaskFailed
	if len(CanRetry(task)) == 0 {
		t.Fatal("post-reboot failed retry allowed")
	}
}
func TestRequiredFailureAndOptionalFailure(t *testing.T) {
	task := taskFixture()
	task = apply(t, task, slotSignal("slot-start", 1, 0, "", 0))
	task = apply(t, task, slotSignal("hook-start", 1, 1, strings.Repeat("a", 64), 0))
	task = apply(t, task, slotSignal("hook-report", 1, 1, strings.Repeat("a", 64), 7))
	if task.State != model.TaskFailed {
		t.Fatal("required failure ignored")
	}
	if len(CanRetry(task)) != 0 {
		t.Fatal("pre-reboot failed retry rejected")
	}
	task = taskFixture()
	task.State = model.TaskInitializing
	task.Execution.ReachedReboot = true
	task = apply(t, task, Signal{Phase: "init-start"})
	task = apply(t, task, slotSignal("slot-start", 2, 0, "", 0))
	task = apply(t, task, slotSignal("hook-start", 2, 1, strings.Repeat("b", 64), 0))
	task = apply(t, task, slotSignal("hook-report", 2, 1, strings.Repeat("b", 64), 0))
	task = apply(t, task, slotSignal("hook-start", 2, 2, strings.Repeat("c", 64), 0))
	task = apply(t, task, slotSignal("hook-report", 2, 2, strings.Repeat("c", 64), 9))
	if task.State != model.TaskInitializing {
		t.Fatal("optional failure terminated task")
	}
}

func TestBootCapAndActualPlatform(t *testing.T) {
	task := taskFixture()
	task.State = model.TaskPending
	task.Execution = nil
	next, err := boot(task, "x86_64", "bios", 1000)
	if err != nil || next.State != model.TaskBooting || next.Execution.BootCount != 1 {
		t.Fatal("first boot", err)
	}
	next, err = boot(next, "x86_64", "bios", 1001)
	if err != nil {
		t.Fatal(err)
	}
	next, err = boot(next, "x86_64", "bios", 1002)
	if err != nil {
		t.Fatal(err)
	}
	next, err = boot(next, "x86_64", "bios", 1003)
	if err != nil || next.State != model.TaskFailed || next.Execution.BootCount != 3 {
		t.Fatal("boot loop not stopped", err)
	}
	mismatch, err := boot(task, "aarch64", "uefi", 1000)
	if err != nil || mismatch.State != model.TaskFailed {
		t.Fatal("platform mismatch accepted", err)
	}
	task.State = model.TaskInstalling
	if _, err := boot(task, "x86_64", "bios", 1000); err == nil {
		t.Fatal("installation restart allowed")
	}
}

func TestLateBootCannotRefreshExpiredAttempt(t *testing.T) {
	task := taskFixture()
	task.State = model.TaskBooting
	task.Execution.Deadline = 1600
	next, err := boot(task, "x86_64", "bios", 1600)
	if err != nil || next.State != model.TaskFailed {
		t.Fatal("expired boot issued another installation attempt", err)
	}
	task.State = model.TaskPending
	task.Execution = runtime(1000, 2)
	next, err = boot(task, "x86_64", "bios", 10000)
	if err != nil || next.Execution.IssuedAt != 10000 || next.Execution.Attempt != 2 {
		t.Fatal("waiting retry did not start fresh credential lifetime", err)
	}
}

func TestSameTimingSlotsMayReportOutOfOrder(t *testing.T) {
	task := taskFixture()
	task.Snapshot.Slots = append(task.Snapshot.Slots, model.SnapshotSlot{Seq: 3, Name: "independent", Exec: true, Timing: catalog.TimingPostTarget})
	task = apply(t, task, Signal{Phase: "slot-start", Slot: 3, Timing: catalog.TimingPostTarget})
	if task.State != model.TaskInstalling {
		t.Fatal("same timing order changed task state")
	}
}

func TestPhaseBudgetsAndLateSignalsCannotRefreshDeadline(t *testing.T) {
	task := taskFixture()
	task.Snapshot.Slots = append(task.Snapshot.Slots, model.SnapshotSlot{Seq: 3, Name: "early", Exec: true, Timing: catalog.TimingPre})
	task = apply(t, task, Signal{Phase: "slot-start", Slot: 3, Timing: catalog.TimingPre})
	if task.Execution.Deadline != 1100+600 || task.Execution.Stage != "pre" {
		t.Fatal("pre budget missing")
	}
	task = apply(t, task, Signal{Phase: "slot-end", Slot: 3, Timing: catalog.TimingPre})
	if task.Execution.Deadline != 1100+3600 || task.Execution.Stage != "packages" {
		t.Fatal("package budget missing")
	}
	task = apply(t, task, slotSignal("slot-start", 1, 0, "", 0))
	if task.Execution.Deadline != 1100+1200 || task.Execution.Stage != "post" {
		t.Fatal("post budget missing")
	}
	task = apply(t, task, slotSignal("hook-start", 1, 1, strings.Repeat("a", 64), 0))
	task = apply(t, task, slotSignal("hook-report", 1, 1, strings.Repeat("a", 64), 0))
	task = apply(t, task, slotSignal("slot-end", 1, 0, "", 0))
	task = apply(t, task, Signal{Phase: "installed"})
	if task.Execution.Deadline != 1100+480 || task.Execution.Stage != "rebooting" {
		t.Fatal("reboot budget is not eight minutes")
	}
	task = apply(t, task, Signal{Phase: "booted"})
	if task.Execution.Deadline != 1100+900 || task.Execution.Stage != "initializing" {
		t.Fatal("initialization budget is not fifteen minutes")
	}
}
