package execution_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
)

// 首次启动载荷只允许由本轮 ESXi 安装过程领取。
func TestESXiFirstbootStageRequiresActiveInstallCredential(t *testing.T) {
	f, _ := gatewayFixture(t)
	f.task.Snapshot.Family = "esxi"
	f.task.Snapshot.Series = "esxi"
	f.task.Snapshot.Format = "esxi-ks"
	f.task.State = model.TaskInstalling
	h := f.handler(t, port.RenderFunc(func(_ context.Context, _ port.RenderRequest) ([]byte, error) {
		return []byte("synthetic-stage"), nil
	}), nil)
	path := "/v1/answer/task-one/" + f.install + "/esxi-firstboot.sh"
	w := call(h, http.MethodGet, path, "")
	if w.Code != http.StatusOK || w.Body.String() != "synthetic-stage" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ESXi installer cannot receive the private stage: status %d", w.Code)
	}
	if w = call(h, http.MethodGet, "/v1/answer/task-one/"+f.final+"/esxi-firstboot.sh", ""); w.Code == http.StatusOK {
		t.Fatal("finalization credential accepted for stage download")
	}
	for _, state := range []model.TaskState{model.TaskInitializing, model.TaskDone, model.TaskCancelled, model.TaskFailed} {
		f.task.State = state
		if w = call(h, http.MethodGet, path, ""); w.Code == http.StatusOK || w.Body.String() == "synthetic-stage" {
			t.Fatalf("stage remains downloadable in state %s", state)
		}
	}
	f.task.State = model.TaskInstalling
	f.task.Snapshot.Format = "kickstart"
	if w = call(h, http.MethodGet, path, ""); w.Code == http.StatusOK {
		t.Fatal("stage available to a non-ESXi task")
	}
}
