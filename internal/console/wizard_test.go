package console

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/builtin"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func wizardFixture(t *testing.T) (*Server, *pgstore.Store, string, model.ISO, model.Template) {
	t.Helper()
	s, st, _ := newHTTPServer(t)
	return seedWizardFixture(t, s, st)
}
func seedWizardFixture(t *testing.T, s *Server, st *pgstore.Store) (*Server, *pgstore.Store, string, model.ISO, model.Template) {
	t.Helper()
	token := adminHTTP(t, s, st)
	iso := readyISO(t, st, "Rocky", "rocky")
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "wizard", Author: "author", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# {{param:hostname}} {{param:locked}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n", Params: []model.ParamDecl{{Name: "hostname", Label: "主机名", Type: "hostname", Default: "node"}, {Name: "locked", Label: "固定项", Type: "word", Default: "fixed"}}, Bindings: map[string]model.Binding{"locked": {Source: model.BindingDefault, Editable: false}}})
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: tpl.ID, Version: 1}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	tpl, err = st.GetTemplate(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	for _, sn := range []string{"SN-1", "SN-2", "SN-3"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"rack": "A01"}}, store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatal(err)
		}
	}
	return s, st, token, iso, tpl
}
func wizardBody(iso model.ISO, tpl model.Template, hosts string) string {
	return fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":%d,"hosts":%s,"createdBy":"spoof","createdAt":1}`, iso.ID, tpl.ID, tpl.Version, hosts)
}
func Test向导完整五步与快照审计(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	hosts := responseData[wizardHostPage](t, httpCall(s, "GET", "/console/v1/wizard/hosts?q=sn&attr_key=rack&attr_value=A01&limit=2", "", token))
	if len(hosts.Items) != 2 || hosts.Items[0].SN != "SN-1" || !hosts.Items[0].Selectable || hosts.Next == "" {
		t.Fatal(hosts)
	}
	candidates := responseData[wizardISOResult](t, httpCall(s, "POST", "/console/v1/wizard/isos", `{"hostSNs":["sn-1","SN-2"]}`, token))
	if len(candidates.Items) != 1 || candidates.Items[0].ID != iso.ID || !candidates.Items[0].Usable {
		t.Fatal(candidates)
	}
	templates := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	if len(templates.Items) != 1 || templates.Items[0].ID != tpl.ID || templates.Items[0].Version != 1 {
		t.Fatal(templates)
	}
	form := responseData[wizardParamsResult](t, httpCall(s, "POST", "/console/v1/wizard/params", fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["SN-1","SN-2"]}`, iso.ID, tpl.ID), token))
	if len(form.Hosts) != 2 || len(form.Hosts[0].Fields) != 2 || form.Hosts[0].Fields[0].Key != "tpl/hostname" || form.Hosts[0].Fields[0].Prefill != "node" || form.Hosts[0].Fields[0].Locked || !form.Hosts[0].Fields[1].Locked || form.Hosts[0].Fields[1].Prefill != "fixed" {
		t.Fatal(form)
	}
	result := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"sn-1","entered":{"tpl/hostname":"node-1"}},{"sn":"SN-2","entered":{"tpl/hostname":"node-2"}}]`), token))
	if len(result.Tasks) != 2 || result.Tasks[0].HostSN != "SN-1" || result.Tasks[0].State != model.TaskPending || result.Tasks[0].CreatedBy != "admin" || result.Tasks[0].CreatedAt != fixedAt {
		t.Fatal(result)
	}
	back, err := st.GetTask(t.Context(), result.Tasks[0].ID)
	if err != nil || back.Snapshot.Template.Params["hostname"].Items[0] != "node-1" || back.Snapshot.Host.Attrs["rack"] != "A01" {
		t.Fatal(back, err)
	}
	bytes, _ := json.Marshal(result)
	for _, field := range []string{`"snapshot"`, `"attrs"`, `"params"`, `"items"`, `"createdBy"`} {
		if !strings.Contains(string(bytes), field) {
			t.Fatal(string(bytes))
		}
	}
	if strings.Contains(string(bytes), `"Snapshot"`) {
		t.Fatal(string(bytes))
	}
	audit, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	count := 0
	for _, a := range audit.Items {
		if a.Action == "task.create" {
			count++
			if a.Actor != "admin" || a.At != fixedAt {
				t.Fatal(a)
			}
		}
	}
	if count != 2 {
		t.Fatal(count)
	}
	hosts = responseData[wizardHostPage](t, httpCall(s, "GET", "/console/v1/wizard/hosts?q=SN-1", "", token))
	if len(hosts.Items) != 1 || hosts.Items[0].Selectable || hosts.Items[0].Problems[0].Code != "host_has_open_task" {
		t.Fatal(hosts)
	}
}
func Test向导批量收集开放任务平台参数与缺失主机问题(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token))
	host, err := st.GetHost(t.Context(), "SN-2")
	if err != nil {
		t.Fatal(err)
	}
	host.Arch = "aarch64"
	if _, err := st.UpdateHost(t.Context(), host, store.Audit{Actor: "seed", Action: "host.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1","entered":{"tpl/hostname":"bad host"}},{"sn":"SN-2","entered":{"tpl/locked":"override"}},{"sn":"SN-3","entered":{"tpl/hostname":"also bad"}},{"sn":"MISSING"}]`), token)
	if w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	var failure struct {
		Data batchDetail `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{"SN-1": {"host_has_open_task", "param_value_invalid"}, "SN-2": {"arch_mismatch", "param_locked"}, "SN-3": {"param_value_invalid"}, "MISSING": {"host_not_registered"}}
	for sn, codes := range expected {
		for _, code := range codes {
			found := false
			for _, p := range failure.Data.ByHost[sn] {
				found = found || string(p.Code) == code
			}
			if !found {
				t.Fatal(sn, code, failure.Data)
			}
		}
	}
	tasks, _ := st.ListTasks(t.Context(), store.TaskQuery{})
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(tasks.Items) != 1 || len(after.Items) != len(before.Items) {
		t.Fatal("partial batch committed")
	}
	for _, body := range []string{wizardBody(iso, tpl, `[{"sn":"SN-3"},{"sn":"sn-3"}]`), wizardBody(iso, tpl, `[{"sn":"MISSING"}]`)} {
		w := httpCall(s, "POST", "/console/v1/wizard/tasks", body, token)
		if w.Code != 422 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := httpCall(s, "POST", "/console/v1/wizard/isos", `{"hostSNs":["MISSING"]}`, token); w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func Test向导当前已审批版本不会被新草稿取代(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	next, err := st.NewTemplateVersion(t.Context(), tpl.ID, 1, "author")
	if err != nil {
		t.Fatal(err)
	}
	result := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	if len(result.Items) != 1 || result.Items[0].Version != 1 {
		t.Fatal(result)
	}
	responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token))
	key := model.TemplateKey{ID: next.ID, Version: next.Version}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-2"}]`), token)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	tasks, _ := st.ListTasks(t.Context(), store.TaskQuery{})
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(tasks.Items) != 1 || len(after.Items) != len(before.Items) {
		t.Fatal("stale selection committed")
	}
}
func Test向导候选分页准备状态与缺失集合(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	arm := readyISO(t, st, "ARM", "rocky")
	arm.Arch = "aarch64"
	if _, err := st.UpdateISO(t.Context(), arm); err != nil {
		t.Fatal(err)
	}
	retired := readyISO(t, st, "retired", "rocky")
	if err := st.DeprecateISO(t.Context(), retired.ID, retired.Rev, store.Audit{Actor: "seed", Action: "iso.deprecate", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateISO(t.Context(), model.ISO{Name: "pending", Family: "linux", Series: "rocky"}, store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	first := responseData[wizardISOResult](t, httpCall(s, "POST", "/console/v1/wizard/isos", `{"hostSNs":["SN-1"],"limit":1}`, token))
	if len(first.Items) != 1 || first.Items[0].ID != arm.ID || first.Items[0].Usable || first.Next == "" || first.Items[0].Problems[0].Subject.ID != "SN-1" {
		t.Fatal(first)
	}
	second := responseData[wizardISOResult](t, httpCall(s, "POST", "/console/v1/wizard/isos", fmt.Sprintf(`{"hostSNs":["SN-1"],"limit":1,"cursor":%q}`, first.Next), token))
	if len(second.Items) != 1 || second.Items[0].ID != iso.ID || !second.Items[0].Usable {
		t.Fatal(second)
	}
	// Missing offered collection makes an otherwise approved template disappear from candidates.
	offered, err := st.CreateTemplate(t.Context(), model.Template{Name: "offered", Author: "author", Family: "linux", Series: []catalog.Series{"rocky"}, Body: tpl.Body, Params: append(tpl.Params, model.ParamDecl{Name: "edition", Type: "enum", Offer: "editions"}), Bindings: tpl.Bindings})
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: offered.ID, Version: 1}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	candidates := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	for _, item := range candidates.Items {
		if item.ID == offered.ID {
			t.Fatal("unusable offer template listed")
		}
	}
}
func Test向导并发创建一批成功另一批全拒绝(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	body := wizardBody(iso, tpl, `[{"sn":"SN-1"},{"sn":"SN-2"}]`)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- httpCall(s, "POST", "/console/v1/wizard/tasks", body, token).Code }()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for code := range codes {
		count[code]++
	}
	if count[200] != 1 || count[422] != 1 {
		t.Fatal(count)
	}
	tasks, _ := st.ListTasks(t.Context(), store.TaskQuery{})
	if len(tasks.Items) != 2 {
		t.Fatal(tasks)
	}
	audit, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	creates := 0
	for _, a := range audit.Items {
		if a.Action == "task.create" {
			creates++
		}
	}
	if creates != 2 {
		t.Fatal(creates)
	}
}
func Test向导五路权限及空批非法字段(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	for _, body := range []string{`{}`, wizardBody(iso, tpl, `[]`), wizardBody(iso, tpl, `[{"sn":""}]`)} {
		w := httpCall(s, "POST", "/console/v1/wizard/tasks", body, token)
		if w.Code != 400 && w.Code != 422 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for i, role := range store.Roles() {
		name := fmt.Sprintf("wizard%d", i)
		seedAuthUser(t, st, name, role, "pw")
		actor := loginHTTP(t, s, name, "pw").Token
		count := 0
		for _, r := range s.Routes() {
			if !strings.HasPrefix(r.Path, "/console/v1/wizard/") {
				continue
			}
			count++
			w := httpCall(s, r.Method, r.Path, `{}`, actor)
			allowed := role == store.RoleAdmin || role == store.RoleOperator
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, r, w.Code)
			}
		}
		if count != 5 {
			t.Fatal(count)
		}
	}
}

func Test向导参数输入按规范SN定位并拒绝未知行(t *testing.T) {
	s, _, token, iso, tpl := wizardFixture(t)
	body := fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["sn-1"],"entered":{"sn-1":{"tpl/hostname":"bad host"}}}`, iso.ID, tpl.ID)
	form := responseData[wizardParamsResult](t, httpCall(s, "POST", "/console/v1/wizard/params", body, token))
	if len(form.Hosts) != 1 || len(form.Hosts[0].Problems) == 0 || form.Hosts[0].Problems[0].Code != "param_value_invalid" {
		t.Fatal(form)
	}
	body = fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["SN-1"],"entered":{"MISSING":{"tpl/hostname":"node"}}}`, iso.ID, tpl.ID)
	w := httpCall(s, "POST", "/console/v1/wizard/params", body, token)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	body = fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["SN-1"],"entered":{"SN-1":{},"sn-1":{}}}`, iso.ID, tpl.ID)
	w = httpCall(s, "POST", "/console/v1/wizard/params", body, token)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func Test向导只接受程序当前携带的内置身份及版本(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	seedBuiltinContent(t, st)
	retired := tpl
	retired.ID, retired.Name, retired.Version = "builtin-retired", "lober:retired", 1
	retired.Builtin, retired.Author, retired.Approver, retired.Rev = true, "", "", 0
	if err := st.EnsureBuiltinTemplate(t.Context(), retired); err != nil {
		t.Fatal(err)
	}
	h := model.Hook{ID: "builtin-packages", Version: 1, Name: "lober:packages", Builtin: true, State: model.VersionApproved, Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "# retired\n"}}}
	if err := st.EnsureBuiltinHook(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	custom := tpl
	custom.ID, custom.Name, custom.Version, custom.Rev, custom.State, custom.Approver = "", "old-hook-custom", 0, 0, "", ""
	custom.Body += "{{slot:retired}}\n"
	custom.Assembly = map[string][]model.Instance{"retired": {{HookID: h.ID, HookVersion: h.Version}}}
	custom, err := st.CreateTemplate(t.Context(), custom)
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: custom.ID, Version: 1}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	page := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	currentSeen := false
	for _, candidate := range page.Items {
		if candidate.ID == retired.ID || candidate.ID == custom.ID {
			t.Fatalf("obsolete builtin candidate: %s", candidate.ID)
		}
		for _, shipped := range builtin.Templates() {
			if candidate.ID == shipped.ID && candidate.Version == shipped.Version && shipped.ID == "builtin-kickstart" {
				currentSeen = true
			}
		}
	}
	if !currentSeen {
		t.Fatal("current builtin missing", page)
	}
	for _, rejected := range []model.Template{retired, custom} {
		w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, rejected, `[{"sn":"SN-1"}]`), token)
		if w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	pageTasks, err := st.ListTasks(t.Context(), store.TaskQuery{})
	if err != nil || len(pageTasks.Items) != 0 {
		t.Fatal(pageTasks, err)
	}
}

func Test向导提交期间新版本审批必须等待任务提交(t *testing.T) {
	dsn := pgtest.Schema(t)
	st := pgtest.OpenAt(t, dsn)
	s := New(Deps{Hosts: st, ISOs: st, Hooks: st, Templates: st, Tasks: st, Events: st, Audit: st, Users: st, Clock: func() int64 { return fixedAt }})
	_, _, token, iso, tpl := seedWizardFixture(t, s, st)
	next, err := st.NewTemplateVersion(t.Context(), tpl.ID, 1, "author")
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: next.ID, Version: next.Version}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal("cannot open real lock fixture")
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), `SELECT sn FROM hosts WHERE sn='SN-1' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	submitted := make(chan int, 1)
	go func() {
		submitted <- httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token).Code
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := tx.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation='hosts'::regclass))`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task writer did not reach real host lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	approved := make(chan int, 1)
	go func() {
		approved <- httpCall(s, "POST", fmt.Sprintf("/console/v1/templates/%s/versions/2/approve?rev=2", tpl.ID), "", token).Code
	}()
	select {
	case code := <-approved:
		t.Fatalf("approval overtook in-flight current-version task creation: %d", code)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-submitted:
		if code != 200 {
			t.Fatal("submit", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task writer did not finish")
	}
	select {
	case code := <-approved:
		if code != 200 {
			t.Fatal("approve", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approver did not finish")
	}
	tasks, err := st.ListTasks(t.Context(), store.TaskQuery{})
	if err != nil || len(tasks.Items) != 1 || tasks.Items[0].TemplateVersion != 1 {
		t.Fatal(tasks, err)
	}
	newer, err := st.GetTemplate(t.Context(), key)
	if err != nil || newer.State != model.VersionApproved {
		t.Fatal(newer, err)
	}
}
