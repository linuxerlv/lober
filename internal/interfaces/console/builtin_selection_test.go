package console

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
)

func Test内置选择拒绝旧发布但不改变冻结任务(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	tpl.ID, tpl.Name, tpl.Version = "builtin-retired", "lober:retired", 1
	tpl.Builtin, tpl.Author, tpl.Approver, tpl.Rev = true, "", "", 0
	if err := st.EnsureBuiltinTemplate(t.Context(), tpl); err != nil {
		t.Fatal(err)
	}
	host, err := st.GetHost(t.Context(), "SN-1")
	if err != nil {
		t.Fatal(err)
	}
	frozen, problems := snapshot.Build(params.Input{Template: tpl, ISO: iso, Host: host})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	created, err := st.CreateTasks(t.Context(), []model.Task{{HostSN: host.SN, ISOID: iso.ID, TemplateID: tpl.ID, TemplateVersion: 1, Snapshot: frozen, CreatedBy: "admin", CreatedAt: fixedAt}}, repository.Audit{Actor: "admin", Action: "task.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	path := "/console/v1/tasks/" + created[0].ID
	before := responseData[taskItem](t, httpCall(s, "GET", path, "", token))
	tpl.Version, tpl.Body = 2, tpl.Body+"# 新发布\n"
	if err := st.EnsureBuiltinTemplate(t.Context(), tpl); err != nil {
		t.Fatal(err)
	}
	old := tpl
	old.Version = 1
	w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, old, `[{"sn":"SN-2"}]`), token)
	if w.Code != 409 {
		t.Fatalf("旧发布应拒绝，得到 %d %s", w.Code, w.Body.String())
	}
	back, err := st.GetTask(t.Context(), created[0].ID)
	if err != nil || !reflect.DeepEqual(back.Snapshot, frozen) || back.TemplateVersion != 1 {
		t.Fatal("已存任务快照被改变", err)
	}
	after := responseData[taskItem](t, httpCall(s, "GET", path, "", token))
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("旧任务网页响应被新发布改变")
	}
}
