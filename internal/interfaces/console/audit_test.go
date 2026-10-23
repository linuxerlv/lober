package console

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

// Only ISO byte acquisition/import is substituted; audit persistence still
// uses the actual source repository transactions. Real IO is assembled in T14.
type auditedISOSource struct{ st repository.ISOSources }

func (f auditedISOSource) Register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a repository.Audit) error {
	src := model.Source{ISOID: item.ID, Role: role, Kind: model.SourceUpload, TotalSize: size, ExpectedSHA256: digest, State: model.SourcePending}
	if kind == "url" {
		src.Kind, src.URL = model.SourceRemote, addr
	}
	if kind == "path" {
		src.Kind, src.Path = model.SourceLocal, addr
	}
	_, err := f.st.SaveSource(ctx, src, a)
	return err
}
func (f auditedISOSource) Upload(context.Context, string, string, int64, io.Reader, repository.Audit) error {
	return nil
}
func (f auditedISOSource) step(ctx context.Context, id string, state model.SourceState, a repository.Audit) error {
	src, exists, err := f.st.GetSource(ctx, id, "")
	if err != nil {
		return err
	}
	if !exists {
		return repository.ErrNotFound
	}
	if state != "" {
		src.State = state
	}
	_, err = f.st.SaveSource(ctx, src, a)
	return err
}
func (f auditedISOSource) Cancel(ctx context.Context, id string, a repository.Audit) error {
	return f.step(ctx, id, model.SourceCancelled, a)
}
func (f auditedISOSource) Retry(ctx context.Context, id string, a repository.Audit) error {
	return f.step(ctx, id, model.SourcePending, a)
}
func (f auditedISOSource) Reimport(ctx context.Context, id string, a repository.Audit) error {
	return f.step(ctx, id, "", a)
}
func (f auditedISOSource) FreeSpace(context.Context) (int64, int64, error) {
	return 1 << 40, 1 << 40, nil
}

func Test每条正式审计路由都写入正确操作者和目标(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin := adminHTTP(t, s, st)
	reviewer := reviewerHTTP(t, s, st)
	setMedia(s, st, st, auditedISOSource{st}, &mockISODeletion{st: st})
	s.server.deps.TaskCtl = &taskControlFixture{st: st}
	seen := map[string]bool{}
	run := func(action, method, path, body, token, kind, id string) map[string]any {
		t.Helper()
		before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		var last int64
		if len(before.Items) > 0 {
			last = before.Items[0].ID
		}
		w := httpCall(s, method, path, body, token)
		if w.Code != 200 {
			t.Fatal(action, w.Code, w.Body.String())
		}
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if id == "" {
			if value, ok := env.Data["id"].(string); ok {
				id = value
			} else if tasks, ok := env.Data["tasks"].([]any); ok && len(tasks) > 0 {
				id = tasks[0].(map[string]any)["id"].(string)
			}
		}
		entries, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		actor := "admin"
		if token == reviewer {
			actor = "reviewer"
		}
		found := false
		for _, e := range entries.Items {
			if e.ID > last && e.Action == action && e.TargetKind == kind && e.TargetID == id && e.Actor == actor && e.At == fixedAt {
				found = true
			}
		}
		if !found {
			t.Fatal("missing audit", action, kind, id, actor)
		}
		requestPath, _, _ := strings.Cut(path, "?")
		matched := false
		for _, r := range s.Routes() {
			if r.Method != method || r.Audit != action {
				continue
			}
			pattern, actual := strings.Split(r.Path, "/"), strings.Split(requestPath, "/")
			if len(pattern) != len(actual) {
				continue
			}
			equal := true
			for i, part := range pattern {
				if !strings.HasPrefix(part, ":") && part != actual[i] {
					equal = false
					break
				}
			}
			if equal {
				seen[r.Method+" "+r.Path] = true
				matched = true
			}
		}
		if !matched {
			t.Fatal("audit request did not match declared route", method, path, action)
		}
		return env.Data
	}
	run("role.menus.update", "PUT", "/console/v1/roles/viewer/menus", `{"menuIds":["hosts"],"rev":0}`, admin, "role", "viewer")
	run("user.profile.update", "PUT", "/console/v1/user/profile", `{"displayName":"管理员","email":"admin@example.com","rev":1}`, admin, "user", "admin")
	run("user.create", "POST", "/console/v1/users", `{"username":"carol","role":"viewer","password":"carol password"}`, admin, "user", "carol")
	run("user.update", "PUT", "/console/v1/users/carol", `{"role":"operator","rev":1}`, admin, "user", "carol")
	run("user.disable", "DELETE", "/console/v1/users/carol", "", admin, "user", "carol")
	run("host.create", "POST", "/console/v1/hosts", `{"sn":"SN-AUDIT","arch":"x86_64","firmware":"uefi"}`, admin, "host", "SN-AUDIT")
	run("host.update", "PUT", "/console/v1/hosts/SN-AUDIT", `{"sn":"SN-AUDIT","arch":"x86_64","rev":1}`, admin, "host", "SN-AUDIT")
	run("host.delete", "DELETE", "/console/v1/hosts/SN-AUDIT?rev=2", "", admin, "host", "SN-AUDIT")
	run("host.import", "POST", "/console/v1/hosts/import", "sn,arch\nSN-IMPORT,x86_64\n", admin, "host", "SN-IMPORT")
	acquired := run("iso.create", "POST", "/console/v1/isos", `{"name":"audit-acquiring","family":"linux","series":"rocky","sources":[{"kind":"upload","size":3}]}`, admin, "iso", "")
	acquiredID := acquired["id"].(string)
	for _, op := range []string{"cancel", "retry", "reimport"} {
		run("iso."+op, "POST", "/console/v1/isos/"+acquiredID+"/"+op, "", admin, "iso", acquiredID)
	}
	missing, err := st.CreateISO(t.Context(), model.ISO{Name: "missing-source", Family: "linux", Series: "rocky"}, repository.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	run("iso.source.register", "POST", "/console/v1/isos/"+missing.ID+"/sources", `{"kind":"upload","size":3}`, admin, "iso", missing.ID)
	ready := readyISO(t, st, "audit-ready", "rocky")
	run("iso.deprecate", "POST", fmt.Sprintf("/console/v1/isos/%s/deprecate?rev=%d", ready.ID, ready.Rev), "", admin, "iso", ready.ID)
	ready, err = st.GetISO(t.Context(), ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	run("iso.delete", "DELETE", fmt.Sprintf("/console/v1/isos/%s?rev=%d", ready.ID, ready.Rev), "", admin, "iso", ready.ID)
	var approvedTemplate string
	for _, kind := range []string{"hooks", "templates"} {
		body := `{"name":"audit-hook","impls":[{"family":"linux","series":["rocky"],"kind":"fragment","body":"# example\n"}]}`
		singular := "hook"
		if kind == "templates" {
			singular = "template"
			body = `{"name":"audit-template","family":"linux","series":["rocky"],"body":"{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"}`
		}
		created := httpCall(s, "POST", "/console/v1/"+kind, body, admin)
		if created.Code != 200 {
			t.Fatal(created.Body.String())
		}
		var env struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(created.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		id := env.Data.ID
		run(singular+".submit", "POST", lifecyclePath(kind, id, "submit", 1), "", admin, singular, id+"@1")
		run(singular+".reject", "POST", lifecyclePath(kind, id, "reject", 2), `{"reason":"revise"}`, reviewer, singular, id+"@1")
		run(singular+".submit", "POST", lifecyclePath(kind, id, "submit", 3), "", admin, singular, id+"@1")
		run(singular+".approve", "POST", lifecyclePath(kind, id, "approve", 4), "", reviewer, singular, id+"@1")
		run(singular+".rename", "PUT", "/console/v1/"+kind+"/"+id+"/name", fmt.Sprintf(`{"name":%q}`, "renamed-"+singular), admin, singular, id)
		if kind == "templates" {
			approvedTemplate = id
		} else {
			run(singular+".deprecate", "POST", lifecyclePath(kind, id, "deprecate", 5), "", reviewer, singular, id+"@1")
		}
	}
	liveISO := readyISO(t, st, "audit-task-iso", "rocky")
	responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", `{"sn":"SN-TASK","arch":"x86_64","firmware":"uefi"}`, admin))
	created := run("task.create", "POST", "/console/v1/wizard/tasks", fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hosts":[{"sn":"SN-TASK"}]}`, liveISO.ID, approvedTemplate), admin, "task", "")
	taskID := created["tasks"].([]any)[0].(map[string]any)["id"].(string)
	for _, op := range []string{"cancel", "retry"} {
		run("task."+op, "POST", "/console/v1/tasks/"+taskID+"/"+op, "", admin, "task", taskID)
	}
	run("template.deprecate", "POST", lifecyclePath("templates", approvedTemplate, "deprecate", 5), "", reviewer, "template", approvedTemplate+"@1")
	run("user.password.change", "POST", "/console/v1/auth/password", `{"currentPassword":"admin password","newPassword":"replacement admin password"}`, admin, "user", "admin")
	for _, r := range s.Routes() {
		if r.Audit != "" && !seen[r.Method+" "+r.Path] {
			t.Error("audited route not exercised", r)
		}
	}
}

func Test审计分页过滤完整CSV及公式作为文本(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	hosts := make([]model.Host, 501)
	for i := range hosts {
		hosts[i] = model.Host{SN: fmt.Sprintf("SN-%03d", i)}
	}
	if _, err := st.ImportHosts(t.Context(), hosts, repository.Audit{Actor: "=1+2", Action: "host.import", Detail: " \t@SUM(1,2)\n下一行", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	page := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?action=host.import&limit=2", "", token))
	if len(page.Items) != 2 || page.Next == "" || page.Items[0].ID <= page.Items[1].ID {
		t.Fatal(page)
	}
	next := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?action=host.import&limit=2&cursor="+page.Next, "", token))
	if len(next.Items) != 2 || next.Items[0].ID >= page.Items[1].ID {
		t.Fatal(next)
	}
	for _, q := range []string{"target_kind=host&target_id=SN-000", "actor=" + url.QueryEscape("=1+2"), "from=" + fmt.Sprint(fixedAt) + "&to=" + fmt.Sprint(fixedAt)} {
		got := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?"+q, "", token))
		if len(got.Items) == 0 {
			t.Fatal(q)
		}
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	w := httpCall(s, "GET", "/console/v1/audit/export?action=host.import", "", token)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") || w.Header().Get("Content-Disposition") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 502 {
		t.Fatal(len(rows), err)
	}
	if strings.Join(rows[0], ",") != "id,at,actor,action,target_kind,target_id,detail" || rows[1][2] != "\t=1+2" || rows[1][6] != "\t \t@SUM(1,2)\n下一行" {
		t.Fatal(rows[:2])
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if len(before.Items) != len(after.Items) || before.Items[0].ID != after.Items[0].ID {
		t.Fatal("read-only audit access mutated logs")
	}
	for _, path := range []string{"/audit?limit=bad", "/audit?from=bad", "/audit?to=-1", "/audit?from=3&to=2", "/audit/export?from=bad"} {
		w := httpCall(s, "GET", "/console/v1"+path, "", token)
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	closed := pgtest.OpenStore(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.deps.Audit = closed
	for _, path := range []string{"/audit", "/audit/export"} {
		w := httpCall(s, "GET", "/console/v1"+path, "", token)
		if w.Code != 500 || strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func Test审计接口五角色权限(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	adminHTTP(t, s, st)
	for i, role := range model.Roles() {
		name := fmt.Sprintf("auditrole%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		for _, path := range []string{"/console/v1/audit", "/console/v1/audit/export"} {
			w := httpCall(s, "GET", path, "", token)
			want := 403
			if role == model.RoleAdmin {
				want = 200
			}
			if w.Code != want {
				t.Fatal(role, path, w.Code)
			}
		}
	}
}
func Test每个权限码都有正式路由用到(t *testing.T) {
	s := New(Deps{Clock: func() int64 { return fixedAt }})
	used := map[string]bool{}
	for _, r := range s.Routes() {
		used[r.Perm] = true
	}
	for _, p := range AllPerms() {
		if !used[p] {
			t.Error("unused permission", p)
		}
	}
}
func Test审计CSV文本转义(t *testing.T) {
	for _, raw := range []string{"=1+2", "+SUM(1,2)", "-1+2", "@SUM(1,2)", "\t=1+2", " \r\n=1+2", "＝1+2", "＋SUM(1,2)", "－1+2", "＠SUM(1,2)", "\u3000=1+2", "\ufeff=1+2", "\tplain", "\rplain", "\nplain", `=1+2";=1+2`, `=1+2'";,=1+2`} {
		if got := csvText(raw); got != "\t"+raw {
			t.Fatal(raw, got)
		}
		var data bytes.Buffer
		w := csv.NewWriter(&data)
		if err := w.Write([]string{csvText(raw)}); err != nil {
			t.Fatal(err)
		}
		w.Flush()
		if !strings.HasPrefix(data.String(), "\"\t") {
			t.Fatal("text prefix is outside quoted field", data.String())
		}
		rows, err := csv.NewReader(&data).ReadAll()
		// encoding/csv.Reader normalizes embedded CRLF to LF.
		if err != nil || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0] != "\t"+strings.ReplaceAll(raw, "\r\n", "\n") {
			t.Fatal("CSV field split or altered", rows, err)
		}
	}
	for _, raw := range []string{"", "普通文本", "123", "'already text", "a=b"} {
		if got := csvText(raw); got != raw {
			t.Fatal(raw, got)
		}
	}
}
