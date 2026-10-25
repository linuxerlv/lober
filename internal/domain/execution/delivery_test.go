package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func frozenHook(body string) model.SnapshotInstance {
	sum := sha256.Sum256([]byte(body))
	return model.SnapshotInstance{Body: body, Digest: hex.EncodeToString(sum[:])}
}

func Test冻结组件重复摘要后续损坏仍拒绝(t *testing.T) {
	hook := frozenHook("frozen")
	bad := hook
	bad.Body = "damaged"
	snapshot := model.Snapshot{Slots: []model.SnapshotSlot{{Seq: 1, Exec: true, Instances: []model.SnapshotInstance{hook}}, {Seq: 2, Exec: true, Instances: []model.SnapshotInstance{bad}}}}
	if _, err := SelectFile(snapshot, "hooks/"+hook.Digest); err != ErrFrozenContent {
		t.Fatal("漏查后续损坏正文", err)
	}
	snapshot.Slots[1].Exec = false
	file, err := SelectFile(snapshot, "hooks/"+hook.Digest)
	if err != nil || !file.Hook || string(file.Body) != "frozen" || file.Kind != "" {
		t.Fatal(file, err)
	}
	snapshot.Slots[0].Instances = []model.SnapshotInstance{frozenHook("")}
	file, err = SelectFile(snapshot, "hooks/"+snapshot.Slots[0].Instances[0].Digest)
	if err != nil || !file.Hook || len(file.Body) != 0 {
		t.Fatal("空冻结正文", file, err)
	}
}

func TestFrozenFileSelectionKeepsExactNamesAndOrder(t *testing.T) {
	snapshot := model.Snapshot{Format: "kickstart", Slots: []model.SnapshotSlot{{Seq: 2, Name: "first", Exec: true}, {Seq: 2, Name: "second", Exec: true}, {Seq: 3, Name: "disabled"}}}
	answer, _ := catalog.DeliveryFileName(snapshot.Format, catalog.FileAnswer)
	for _, tc := range []struct {
		name string
		kind catalog.FileKind
		slot string
		err  error
	}{
		{answer, catalog.FileAnswer, "", nil}, {"scripts/2.sh", catalog.FileScript, "first", nil},
		{"scripts/02.sh", "", "", ErrFileNotFound}, {"scripts/3.sh", "", "", ErrFileNotFound},
		{"hooks/short", "", "", ErrFileNotFound}, {"unknown", "", "", ErrFileNotFound},
	} {
		got, err := SelectFile(snapshot, tc.name)
		if err != tc.err || got.Kind != tc.kind || got.Slot != tc.slot || got.Hook {
			t.Fatalf("%q: %+v %v", tc.name, got, err)
		}
	}
	snapshot.Format = "preseed"
	aux, _ := catalog.DeliveryFileName(snapshot.Format, catalog.FileAux)
	if aux != "" {
		got, err := SelectFile(snapshot, aux)
		if err != nil || got.Kind != catalog.FileAux {
			t.Fatal(got, err)
		}
	}
}

func TestDeliveryPredicatesKeepOriginalIndependentConditions(t *testing.T) {
	for _, state := range []model.TaskState{model.TaskPending, model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing, model.TaskDone, model.TaskFailed, model.TaskCancelled} {
		task := model.Task{State: state, Snapshot: model.Snapshot{Format: "preseed"}}
		if CanBoot(task) != (state == model.TaskPending || state == model.TaskBooting) || BootDeliverable(task) == state.Finished() || PreseedAvailable(task) != (state == model.TaskBooting || state == model.TaskInstalling) || !InstallerAnswerAvailable(task) {
			t.Fatal(state)
		}
		if InstallAccessAllowed(task, 0) || InstallAccessNeedsTime(task) {
			t.Fatal("无执行记录被授权", state)
		}
		task.Execution = &model.Execution{Deadline: 100}
		if !InstallAccessNeedsTime(task) || InstallAccessAllowed(task, 100) || InstallAccessAllowed(task, 99) != PreseedAvailable(task) {
			t.Fatal("期限边界", state)
		}
		task.Execution.Deadline = 0
		if InstallAccessNeedsTime(task) || InstallAccessAllowed(task, 999) != PreseedAvailable(task) {
			t.Fatal("无期限仍使用时钟", state)
		}
	}
	task := model.Task{State: model.TaskDone}
	for _, sig := range []Signal{{Phase: "booted"}, {Phase: "init-start"}, {Phase: "init-done"}, {Phase: "ready"}, {Timing: catalog.TimingFirstboot}} {
		if !FinalSignal(sig) || !SignalAccessAllowed(task, sig) {
			t.Fatal(sig)
		}
	}
	if FinalSignal(Signal{Phase: "installed"}) || SignalAccessAllowed(task, Signal{Phase: "installed"}) || InstallerAnswerAvailable(task) {
		t.Fatal("普通信号或格式放行")
	}
}
