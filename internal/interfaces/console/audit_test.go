package console

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/domain/access"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

type auditLaterPageFailure struct {
	repository.AuditLog
	queries   []repository.AuditQuery
	firstSize int
	next      string
}

func (f *auditLaterPageFailure) ListAudit(ctx context.Context, q repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
	f.queries = append(f.queries, q)
	if len(f.queries) > 1 {
		return repository.Page[repository.AuditEntry]{}, errors.New("测试：后页读取失败")
	}
	page, err := f.AuditLog.ListAudit(ctx, q)
	f.firstSize, f.next = len(page.Items), page.Next
	return page, err
}

func Test审计导出后页失败不发送残缺下载(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "auditfailure", model.RoleAdmin)
	hosts := make([]model.Host, 501)
	for i := range hosts {
		hosts[i] = model.Host{SN: fmt.Sprintf("LATE-%03d", i)}
	}
	if _, err := st.ImportHosts(t.Context(), hosts, repository.Audit{Actor: "=1+2", Action: "host.import", Detail: "原文", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	failing := &auditLaterPageFailure{AuditLog: st}
	s.server.deps.AuditReader = audit.NewReader(failing)
	path := fmt.Sprintf("/console/v1/audit/export?action=host.import&actor=%%3D1%%2B2&target_kind=host&from=%d&to=%d", fixedAt, fixedAt)
	w := httpCall(s, "GET", path, "", token)
	if failing.firstSize != 500 || failing.next == "" || len(failing.queries) != 2 {
		t.Fatalf("未实际读满第一页后失败：条数 %d，调用 %d", failing.firstSize, len(failing.queries))
	}
	want := repository.AuditQuery{Action: "host.import", Actor: "=1+2", TargetKind: "host", From: fixedAt, To: fixedAt, Limit: 500}
	if failing.queries[0] != want {
		t.Fatal("第一页过滤条件或页大小改变")
	}
	want.Cursor = failing.next
	if failing.queries[1] != want {
		t.Fatal("后页游标或过滤条件改变")
	}
	if w.Code != 500 || w.Header().Get("Content-Disposition") != "" || strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatal("后页失败发送了残缺下载", w.Code)
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Code != 500 || env.Message != model.ConsoleInternal {
		t.Fatal("失败响应不是完整的错误消息")
	}
	if strings.Contains(w.Body.String(), "id,at,actor") || strings.Contains(w.Body.String(), "LATE-") {
		t.Fatal("错误响应夹带已缓冲的CSV")
	}
}

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
	expected, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: "host.import", Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	tail, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: "host.import", Limit: 500, Cursor: expected.Next})
	if err != nil || len(expected.Items) != 500 || len(tail.Items) != 1 {
		t.Fatalf("真实数据未跨页: %d %d %v", len(expected.Items), len(tail.Items), err)
	}
	expected.Items = append(expected.Items, tail.Items...)
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
	for i, e := range expected.Items {
		want := []string{strconv.FormatInt(e.ID, 10), strconv.FormatInt(e.At, 10), "\t=1+2", "host.import", "host", e.TargetID, "\t \t@SUM(1,2)\n下一行"}
		if !reflect.DeepEqual(rows[i+1], want) {
			t.Fatalf("第 %d 行字段或顺序不符: %q", i, rows[i+1])
		}
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
	s.server.deps.AuditReader = audit.NewReader(closed)
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
	for _, p := range access.AllPerms() {
		if !used[p] {
			t.Error("unused permission", p)
		}
	}
}

func Test审计旧游标不倒灌并发新增及精确筛选(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "auditcursor", model.RoleAdmin)
	a := repository.Audit{Actor: "CaseActor", Action: "host.import", Detail: "原文", At: fixedAt}
	if _, err := st.ImportHosts(t.Context(), []model.Host{{SN: "OLD-1"}, {SN: "OLD-2"}, {SN: "OLD-3"}}, a); err != nil {
		t.Fatal(err)
	}
	first := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?action=host.import&limit=2", "", token))
	if len(first.Items) != 2 || first.Next == "" {
		t.Fatal(first)
	}
	done := make(chan error, 1)
	go func() { _, err := st.ImportHosts(t.Context(), []model.Host{{SN: "NEW"}}, a); done <- err }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	next := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?action=host.import&limit=2&cursor="+url.QueryEscape(first.Next), "", token))
	if len(next.Items) != 1 || next.Next != "" || next.Items[0].TargetID != "OLD-1" || next.Items[0].ID >= first.Items[1].ID {
		t.Fatal(next)
	}
	for _, q := range []string{"actor=caseactor", "actor=%00", "target_kind=Host", "action=HOST.import", "from=" + fmt.Sprint(fixedAt+1), "to=" + fmt.Sprint(fixedAt-1)} {
		page := responseData[auditPage](t, httpCall(s, "GET", "/console/v1/audit?page=1&page_size=20&"+q, "", token))
		if len(page.Items) != 0 || page.Total == nil || *page.Total != 0 {
			t.Fatalf("应为空页: %s %+v", q, page)
		}
	}
}
