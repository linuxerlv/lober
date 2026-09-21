// 本文件测试任务六个端点（stream 除外，见 stream_test.go）。对应实现
// 计划任务 6「测试要锁住的行为」。
package console

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// seedTaskFixture 准备一台可建任务的主机 + active 模板 + ready 制品集，
// 返回 hID（本模块的 hostID() 函数计算即 SN）与 tplID。
func seedTaskFixture(t *testing.T, ts *testServer) (hID, tplID string) {
	t.Helper()
	ctx := t.Context()

	host := model.Host{SN: "TASK-SN-1", MAC: "aa:bb:cc:dd:ee:ff", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := ts.Store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("准备主机失败: %v", err)
	}

	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-task-1", Name: "x", OSFamily: model.OSFamilyRHEL, Firmware: model.FirmwareAny,
		State: model.ArtifactSetReady,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}

	tpl, err := ts.Store.CreateTemplate(ctx, model.Template{
		ID: "tpl-task-1", Name: "x", ArtifactSetID: set.ID, Version: 1, State: model.TemplateStateActive,
	})
	if err != nil {
		t.Fatalf("准备模板失败: %v", err)
	}
	return hostID(host), tpl.ID
}

// TestTask_CreatePending 锁住「建任务成功后状态是 pending」。
func TestTask_CreatePending(t *testing.T) {
	ts := newTestServer(t)
	hID, tplID := seedTaskFixture(t, ts)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": hID, "templateId": tplID, "formData": map[string]string{"hostname": "web-01"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建任务失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.State != string(model.TaskStatePending) {
		t.Errorf("建任务成功后 state 应为 pending，实际 %q", env.Data.State)
	}
	if env.Data.HostSN != "TASK-SN-1" || env.Data.HostMAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("hostSn/hostMac 应来自主机清单，实际 sn=%q mac=%q", env.Data.HostSN, env.Data.HostMAC)
	}

	events := ts.Bus.events()
	if len(events) != 1 || events[0] != contract.LifecycleEventTaskCreated {
		t.Errorf("应发布 task.created 事件一次，实际 %v", events)
	}
}

// TestTask_RequestBodySNFieldIgnored 锁住「请求体里带 SN 字段时被忽略或
// 拒绝（安全关键）」。
func TestTask_RequestBodySNFieldIgnored(t *testing.T) {
	ts := newTestServer(t)
	hID, tplID := seedTaskFixture(t, ts)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": hID, "templateId": tplID,
		"hostSn": "ATTACKER-INJECTED-SN", "sn": "ATTACKER-INJECTED-SN",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建任务失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.HostSN != "TASK-SN-1" {
		t.Errorf("hostSn 必须来自主机清单查出的值，不接受请求体里的注入值；实际 %q", env.Data.HostSN)
	}
}

// TestTask_ParamResolveFailureLeavesNoTaskInStore 锁住「参数决策失败时
// Store 里不得出现任何任务」——这是 §7.4 的执行点。
func TestTask_ParamResolveFailureLeavesNoTaskInStore(t *testing.T) {
	ts := newTestServer(t)
	hID, tplID := seedTaskFixture(t, ts)
	ts.Server.deps.ParamsResolver = &stubResolver{
		resolveFunc: func(ctx context.Context, formData, facts map[string]string) (map[string]string, error) {
			return nil, errors.New("模拟 IPAM 申请 IP 失败")
		},
	}
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": hID, "templateId": tplID,
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Fatalf("参数决策失败时建任务应失败，实际成功")
	}

	tasks, err := ts.Store.ListTasks(t.Context(), contract.TaskFilter{})
	if err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("参数决策失败时库里不应出现任何任务，实际 %d 条", len(tasks))
	}
}

// TestTask_FirmwareIncompatibleRejected 锁住「固件不兼容的组合在建任务时
// 被拒」。
func TestTask_FirmwareIncompatibleRejected(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	host := model.Host{SN: "TASK-SN-BIOS", MAC: "11:22:33:44:55:66", Arch: model.ArchX86_64, Firmware: model.FirmwareBIOS}
	if _, err := ts.Store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("准备主机失败: %v", err)
	}
	set, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "as-esxi", Name: "x", OSFamily: model.OSFamilyESXi, Firmware: model.FirmwareUEFI,
		State: model.ArtifactSetReady,
	})
	if err != nil {
		t.Fatalf("准备制品集失败: %v", err)
	}
	tpl, err := ts.Store.CreateTemplate(ctx, model.Template{
		ID: "tpl-esxi", Name: "x", ArtifactSetID: set.ID, Version: 1, State: model.TemplateStateActive,
	})
	if err != nil {
		t.Fatalf("准备模板失败: %v", err)
	}

	tok := ts.login("bob-operator")
	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": hostID(host), "templateId": tpl.ID,
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("BIOS 主机配 ESXi（仅支持 UEFI）应被拒，实际成功")
	}
}

// TestTask_ActiveTaskExistsRejected 锁住「同一主机已有非终态任务时回对应
// 错误码」。
func TestTask_ActiveTaskExistsRejected(t *testing.T) {
	ts := newTestServer(t)
	hID, tplID := seedTaskFixture(t, ts)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{"hostId": hID, "templateId": tplID})
	if rr.Code != http.StatusOK {
		t.Fatalf("第一次建任务应成功: %s", rr.Body.String())
	}

	rr = ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{"hostId": hID, "templateId": tplID})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeActiveTaskExists) {
		t.Errorf("第二次建任务应回 CodeActiveTaskExists(%d)，实际 code=%d", apierr.CodeActiveTaskExists, env.Code)
	}
}

// TestTask_RetryRejectsThreeNonReinstallableReasons 锁住「三种不可重装的
// 失败原因重试时回拒绝重装的错误码（逐一验证）」。
func TestTask_RetryRejectsThreeNonReinstallableReasons(t *testing.T) {
	reasons := []model.FailureReason{
		model.FailureReasonRebootTimeout, model.FailureReasonInitTimeout, model.FailureReasonInitFailed,
	}
	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			ts := newTestServer(t)
			ts.SM.retryFunc = func(ctx context.Context, taskID string) (model.Task, error) {
				return model.Task{}, contract.ErrNotReinstallable
			}
			tok := ts.login("bob-operator")

			rr := ts.doJSON("POST", "/console/v1/tasks/some-task/retry", tok, nil)
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code != int(apierr.CodeReinstallDenied) {
				t.Errorf("原因 %s 重试应回 CodeReinstallDenied(%d)，实际 code=%d", reason, apierr.CodeReinstallDenied, env.Code)
			}
		})
	}
}

// TestTask_RetrySucceedsForReinstallableReasons 锁住「六种可重装的失败
// 原因重试成功」。
func TestTask_RetrySucceedsForReinstallableReasons(t *testing.T) {
	reasons := []model.FailureReason{
		model.FailureReasonBootTimeout, model.FailureReasonAttemptExceeded, model.FailureReasonInstallPhase,
		model.FailureReasonHookRequired, model.FailureReasonPrecheck, model.FailureReasonCanceled,
	}
	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			ts := newTestServer(t)
			ts.SM.retryFunc = func(ctx context.Context, taskID string) (model.Task, error) {
				return model.Task{ID: taskID, State: model.TaskStatePending}, nil
			}
			tok := ts.login("bob-operator")

			rr := ts.doJSON("POST", "/console/v1/tasks/some-task/retry", tok, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("原因 %s 重试应成功: %d, body=%s", reason, rr.Code, rr.Body.String())
			}
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code != int(apierr.CodeOK) {
				t.Errorf("原因 %s 重试应成功，实际 code=%d", reason, env.Code)
			}
		})
	}
}

// TestTask_CancelReachesTerminalState 锁住「取消后任务进终态」。
func TestTask_CancelReachesTerminalState(t *testing.T) {
	ts := newTestServer(t)
	ts.SM.cancelFunc = func(ctx context.Context, taskID string) (model.Task, error) {
		return model.Task{ID: taskID, State: model.TaskStateFailed, FailureReason: model.FailureReasonCanceled}, nil
	}
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks/some-task/cancel", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("取消失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if !model.TaskState(env.Data.State).IsTerminal() {
		t.Errorf("取消后任务状态应为终态，实际 %q", env.Data.State)
	}
}

// TestTask_PermissionsRequired 锁住「建任务、重试、取消各自需要对应权限
// 码」。
func TestTask_PermissionsRequired(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"建任务", "POST", "/console/v1/tasks"},
		{"重试", "POST", "/console/v1/tasks/x/retry"},
		{"取消", "POST", "/console/v1/tasks/x/cancel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestServer(t)
			tok := ts.login("alice-viewer")
			rr := ts.doJSON(c.method, c.path, tok, map[string]any{})
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code != int(apierr.CodeForbidden) {
				t.Errorf("viewer 调 %s 应被拒 403，实际 code=%d", c.name, env.Code)
			}
		})
	}
}

// TestTask_ListAndEvents 覆盖列表按状态/SN 过滤与事件列表的基本路径。
func TestTask_ListAndEvents(t *testing.T) {
	ts := newTestServer(t)
	hID, tplID := seedTaskFixture(t, ts)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{"hostId": hID, "templateId": tplID})
	var created struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("GET", "/console/v1/tasks?state=pending", tok, nil)
	var listed struct {
		Data []taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &listed)
	if len(listed.Data) != 1 {
		t.Fatalf("按 state=pending 过滤应有 1 条，实际 %d", len(listed.Data))
	}

	rr = ts.doJSON("GET", "/console/v1/tasks/"+created.Data.ID+"/events", tok, nil)
	var events struct {
		Data []taskEventItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &events)
	if len(events.Data) != 1 {
		t.Errorf("建任务后应有 1 条首事件，实际 %d 条", len(events.Data))
	}
}
