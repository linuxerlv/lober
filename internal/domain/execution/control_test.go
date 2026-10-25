package execution

import (
	"encoding/json"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func TestPureControlCopiesStateAndKeepsDiagnostics(t *testing.T) {
	task := taskFixture()
	before, _ := json.Marshal(task)
	next, changed := StartInstalling(task, 1200)
	if changed || next.Execution != task.Execution || InstallingNeeded(task) {
		t.Fatal("重复安装改变记录")
	}
	task.State = model.TaskBooting
	next, changed = StartInstalling(task, 1200)
	if !changed || !InstallingNeeded(task) || next.State != model.TaskInstalling || next.Execution.Stage != "packages" || next.Execution.Deadline != 4800 || task.State != model.TaskBooting {
		t.Fatal(next, changed)
	}
	task.State = model.TaskInstalling
	next, problems := Cancel(task, 1200)
	if len(problems) != 0 || next.State != model.TaskCancelled || next.Execution.Deadline != 0 || next.Execution == task.Execution {
		t.Fatal(next, problems)
	}
	after, _ := json.Marshal(task)
	if string(before) != string(after) {
		t.Fatal("取消修改输入")
	}
	next, problems = Retry(next, 1300)
	if len(problems) != 0 || next.State != model.TaskPending || next.Execution.Attempt != 2 || next.Execution.Deadline != 2200 || len(next.Execution.Signals) != 0 {
		t.Fatal(next, problems)
	}
	task.State = model.TaskFailed
	task.Execution.ReachedReboot = true
	before, _ = json.Marshal(task)
	rejected, problems := Retry(task, 1400)
	if len(problems) != 1 || problems[0].Code != "task_retry_after_reboot" || rejected.Execution != task.Execution {
		t.Fatal(problems)
	}
	_, problems = Cancel(task, 1400)
	if len(problems) != 1 || problems[0].Code != "task_control_state" {
		t.Fatal(problems)
	}
	after, _ = json.Marshal(task)
	if string(before) != string(after) {
		t.Fatal("拒绝修改输入")
	}
	for _, retry := range []bool{false, true} {
		empty := model.Task{State: model.TaskCancelled}
		if retry {
			next, problems = Retry(empty, 1500)
			if len(problems) != 0 || next.Execution.Attempt != 2 {
				t.Fatal(next, problems)
			}
		} else {
			empty.State = model.TaskPending
			next, problems = Cancel(empty, 1500)
			if len(problems) != 0 || next.Execution.Attempt != 1 || next.Execution.Deadline != 0 {
				t.Fatal(next, problems)
			}
		}
	}
}

func TestTimeoutAtEqualityDoesNotMutateInput(t *testing.T) {
	task := taskFixture()
	next, timedOut := Timeout(task, 6399)
	if timedOut || next.Execution != task.Execution {
		t.Fatal(next, timedOut)
	}
	next, timedOut = Timeout(task, 6400)
	if !timedOut || next.State != model.TaskFailed || next.Execution.Deadline != 0 || task.State != model.TaskInstalling || task.Execution.Deadline != 6400 {
		t.Fatal(next, timedOut)
	}
	for _, task := range []model.Task{{}, {Execution: &model.Execution{}}} {
		if _, timedOut := Timeout(task, 9999); timedOut {
			t.Fatal(task)
		}
	}
}
