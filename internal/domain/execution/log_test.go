package execution

import (
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func TestInstallerLogRequiresLiveMatchingAttempt(t *testing.T) {
	task := taskFixture()
	if !CanReceiveInstallerLog(task, 1) || CanReceiveInstallerLog(task, 2) {
		t.Fatal("尝试号判定改变")
	}
	task.State = model.TaskDone
	if CanReceiveInstallerLog(task, 1) {
		t.Fatal("终态接收日志")
	}
	task.State = model.TaskInstalling
	task.Execution = nil
	if CanReceiveInstallerLog(task, 1) {
		t.Fatal("无执行记录接收日志")
	}
}
