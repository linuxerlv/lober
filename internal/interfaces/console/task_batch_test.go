package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test装机批次可回查统计和筛选(t *testing.T) {
	s, st, token, task, _ := taskFixture(t)
	_, _, err := st.TransitionTask(t.Context(), task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskFailed, nil)
	if err != nil {
		t.Fatal(err)
	}

	path := "/console/v1/task-batches/" + task.BatchID
	type summary struct {
		Total           int64
		Pending         int64
		Failed          int64
		Active          int64
		CreatedBy       string
		CreatedAt       int64
		ISOName         string
		TemplateName    string
		TemplateVersion int
	}
	want := summary{Total: 2, Pending: 1, Failed: 1, CreatedBy: task.CreatedBy, CreatedAt: task.CreatedAt, ISOName: task.Snapshot.ISO.Name, TemplateName: task.Snapshot.Template.Name, TemplateVersion: task.TemplateVersion}
	for range 2 {
		got := responseData[summary](t, httpCall(s, "GET", path, "", token))
		if got != want {
			t.Fatalf("批次统计 = %+v，想要 %+v", got, want)
		}
		page := responseData[taskPage](t, httpCall(s, "GET", "/console/v1/tasks?batch_id="+task.BatchID+"&state=failed&page=1&page_size=1", "", token))
		if page.Total == nil || *page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != task.ID {
			t.Fatalf("失败任务 = %+v", page)
		}
	}
	for _, tc := range []struct {
		path, token string
		status      int
	}{
		{path, "", http.StatusUnauthorized},
		{"/console/v1/task-batches/missing-batch", token, http.StatusNotFound},
		{"/console/v1/task-batches/-invalid", token, http.StatusBadRequest},
	} {
		if w := httpCall(s, "GET", tc.path, "", tc.token); w.Code != tc.status {
			t.Fatalf("%s 状态码 = %d，想要 %d。%s", tc.path, w.Code, tc.status, w.Body.String())
		}
	}
}

func Test装机批次浏览器刷新(t *testing.T) {
	web := os.Getenv("LOBER_TEST_BATCH_WEB")
	if web == "" {
		t.Skip("设置 LOBER_TEST_BATCH_WEB 为已构建的 web 目录，并准备 Playwright Chromium")
	}
	s, st, _, task, _ := taskFixture(t)
	_, _, err := st.TransitionTask(t.Context(), task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskFailed, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(WithFrontend(s.Engine))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	browser := exec.CommandContext(ctx, "node", "scripts/check-task-batch-browser.mjs", server.URL, task.BatchID, task.ID)
	browser.Dir = web
	output, err := browser.CombinedOutput()
	if err != nil {
		t.Fatalf("浏览器验证失败：%v\n%s", err, output)
	}
	t.Log(string(output))
}
