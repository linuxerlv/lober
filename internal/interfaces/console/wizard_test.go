package console

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/lober/lober/internal/application/audit"
	selectionguard "github.com/lober/lober/internal/infrastructure/selection"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

type wizardResultFixture struct {
	tasks.Wizard
	templates tasks.TemplatePage
	created   []tasks.TaskChoice
	err       error
}

func (f wizardResultFixture) Templates(context.Context, ports.Actor, tasks.TemplateRequest) (tasks.TemplatePage, error) {
	return f.templates, f.err
}
func (f wizardResultFixture) Submit(context.Context, ports.Actor, tasks.SubmitRequest) ([]tasks.TaskChoice, error) {
	return f.created, f.err
}

func Test向导只转换应用给出的诊断和完整任务(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	problem := model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: "SN-1"})
	d := s.server.deps
	d.TaskCtl = &taskControlFixture{st: st, block: []model.Problem{problem}}
	// 诊断在应用构造时注入，原任务控制入口的后置替换不参与向导。
	assembled := wizardDeps(d, st)
	s.server.deps.Wizard, s.server.deps.TemplateAuthoring, s.server.deps.TaskReader = assembled.Wizard, assembled.TemplateAuthoring, assembled.TaskReader
	result := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1","entered":{"tpl/hostname":"node-one"}}]`), token))
	if len(result.Tasks) != 1 || len(result.Tasks[0].CanCancel) != 1 || len(result.Tasks[0].CanRetry) != 1 || result.Tasks[0].CanCancel[0].Code != problem.Code || result.Tasks[0].Snapshot.Template.Params["hostname"].Items[0] != "node-one" {
		t.Fatal(result)
	}
	// 直接给已计算问题。网页不能重算后把这项抹掉，模板其余字段也必须保留。
	s.server.deps.Wizard = wizardResultFixture{templates: tasks.TemplatePage{Items: []tasks.TemplateChoice{{Template: tpl, Problems: []model.Problem{problem}}}, Next: "next"}}
	page := responseData[wizardTemplateResult](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token))
	if page.Next != "next" || len(page.Items) != 1 || len(page.Items[0].Problems) != 1 || page.Items[0].Problems[0].Code != problem.Code || page.Items[0].Body != tpl.Body || len(page.Items[0].Params) != len(tpl.Params) {
		t.Fatal(page)
	}
}

func Test向导应用错误沿用网页状态与问题明细(t *testing.T) {
	s, _, token, iso, tpl := wizardFixture(t)
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{tasks.ErrUnauthenticated, 401, model.ConsoleUnauthorized},
		{tasks.ErrPasswordChangeRequired, 403, model.ConsolePasswordChangeRequired},
		{tasks.ErrForbidden, 403, model.ConsoleForbidden},
		{repository.ErrTemplateNotCurrent, 409, model.ConsoleTemplateCurrent},
	} {
		s.server.deps.Wizard = wizardResultFixture{err: tc.err}
		w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token)
		var got envelope
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != tc.status || got.Message != tc.message || got.Data != nil {
			t.Fatal(w.Code, w.Body.String(), err)
		}
	}
	for _, p := range []model.Problem{model.NewProblem("arch_mismatch", model.Subject{Kind: model.KindHost, ID: "SN-1"}, "aarch64", "x86_64"), model.NewProblem("sn_invalid", model.Subject{Kind: model.KindHost}, "空编号")} {
		s.server.deps.Wizard = wizardResultFixture{err: &tasks.ProblemsError{Problems: []model.Problem{p}}}
		w := httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q}`, iso.ID), token)
		if w.Code != StatusOfProblems([]model.Problem{p}) || !strings.Contains(w.Body.String(), string(p.Code)) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

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
	if err := st.SubmitTemplate(t.Context(), key, 1, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", repository.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	tpl, err = st.GetTemplate(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	for _, sn := range []string{"SN-1", "SN-2", "SN-3"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"rack": "A01"}}, repository.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
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
	audit, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	if _, err := st.UpdateHost(t.Context(), host, repository.Audit{Actor: "seed", Action: "host.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	tasks, _ := st.ListTasks(t.Context(), repository.TaskQuery{})
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	if err := st.SubmitTemplate(t.Context(), key, 1, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", repository.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-2"}]`), token)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	tasks, _ := st.ListTasks(t.Context(), repository.TaskQuery{})
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	if err := st.DeprecateISO(t.Context(), retired.ID, retired.Rev, repository.Audit{Actor: "seed", Action: "iso.deprecate", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateISO(t.Context(), model.ISO{Name: "pending", Family: "linux", Series: "rocky"}, repository.Audit{Actor: "seed", Action: "iso.create", At: fixedAt}); err != nil {
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
	if err := st.SubmitTemplate(t.Context(), key, 1, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", repository.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
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
	tasks, _ := st.ListTasks(t.Context(), repository.TaskQuery{})
	if len(tasks.Items) != 2 {
		t.Fatal(tasks)
	}
	audit, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
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
	for i, role := range model.Roles() {
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
			allowed := role == model.RoleAdmin || role == model.RoleOperator
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
	s, st, token, iso, tpl := wizardFixture(t)
	body := fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["sn-1"],"entered":{"sn-1":{"tpl/hostname":"bad host"}}}`, iso.ID, tpl.ID)
	form := responseData[wizardParamsResult](t, httpCall(s, "POST", "/console/v1/wizard/params", body, token))
	if len(form.Hosts) != 1 || len(form.Hosts[0].Problems) == 0 || form.Hosts[0].Problems[0].Code != "param_value_invalid" {
		t.Fatal(form)
	}
	before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, entered := range []string{`{"MISSING":{"tpl/hostname":"node"}}`, `{"SN-1":{},"sn-1":{}}`} {
		body = fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["SN-1"],"entered":%s}`, iso.ID, tpl.ID, entered)
		w := httpCall(s, "POST", "/console/v1/wizard/params", body, token)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		tasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
		if err != nil || len(tasks.Items) != 0 {
			t.Fatal("拒绝的输入产生了任务", tasks, err)
		}
		after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
		if err != nil || len(after.Items) != len(before.Items) {
			t.Fatal("拒绝的输入产生了审计", after, err)
		}
	}
}

func Test向导参数输入顺序不串主机(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	body := fmt.Sprintf(`{"isoID":%q,"templateID":%q,"templateVersion":1,"hostSNs":["sn-2","SN-1"],"entered":{"SN-1":{"tpl/hostname":"node-one"},"sn-2":{"tpl/hostname":"node-two"}}}`, iso.ID, tpl.ID)
	form := responseData[wizardParamsResult](t, httpCall(s, "POST", "/console/v1/wizard/params", body, token))
	if len(form.Hosts) != 2 || form.Hosts[0].SN != "SN-2" || form.Hosts[1].SN != "SN-1" || len(form.Common) != 0 {
		t.Fatal(form)
	}
	for _, h := range form.Hosts {
		if len(h.Problems) != 0 {
			t.Fatal(h)
		}
	}
	result := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"sn-2","entered":{"tpl/hostname":"node-two"}},{"sn":"SN-1","entered":{"tpl/hostname":"node-one"}}]`), token))
	if len(result.Tasks) != 2 || result.Tasks[0].HostSN != "SN-2" || result.Tasks[1].HostSN != "SN-1" {
		t.Fatal(result)
	}
	for i, expected := range []struct{ sn, hostname string }{{"SN-2", "node-two"}, {"SN-1", "node-one"}} {
		got := result.Tasks[i]
		if got.Snapshot.Host.SN != expected.sn || len(got.Snapshot.Template.Params["hostname"].Items) != 1 || got.Snapshot.Template.Params["hostname"].Items[0] != expected.hostname {
			t.Fatal("响应快照串到了另一台主机", got)
		}
		back, err := st.GetTask(t.Context(), got.ID)
		if err != nil {
			t.Fatal(err)
		}
		if back.HostSN != expected.sn || back.Snapshot.Host.SN != expected.sn || len(back.Snapshot.Template.Params["hostname"].Items) != 1 || back.Snapshot.Template.Params["hostname"].Items[0] != expected.hostname {
			t.Fatal("保存的快照串到了另一台主机", back)
		}
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
	if err := st.SubmitTemplate(t.Context(), key, 1, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", repository.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
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
	pageTasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
	if err != nil || len(pageTasks.Items) != 0 {
		t.Fatal(pageTasks, err)
	}
}

func Test向导提交期间新版本审批必须等待任务提交(t *testing.T) {
	dsn := pgtest.Schema(t)
	st := pgtest.OpenAt(t, dsn)
	guard := &authoringWaitingGuard{SelectionGuard: selectionguard.NewGuard(), waiting: make(chan struct{}), entered: make(chan struct{})}
	s := New(wizardDepsGuard(Deps{Tasks: st, Events: st, AuditReader: audit.NewReader(st), Clock: func() int64 { return fixedAt }}, st, guard))
	_, _, token, iso, tpl := seedWizardFixture(t, s, st)
	next, err := st.NewTemplateVersion(t.Context(), tpl.ID, 1, "author")
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: next.ID, Version: next.Version}
	if err := st.SubmitTemplate(t.Context(), key, 1, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
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
	case <-guard.waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("审批没有到达共用选择锁")
	}
	select {
	case <-guard.entered:
		t.Fatal("任务持读锁时审批取得了写锁")
	case code := <-approved:
		t.Fatalf("审批越过任务提交: %d", code)
	default:
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
	tasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
	if err != nil || len(tasks.Items) != 1 || tasks.Items[0].TemplateVersion != 1 {
		t.Fatal(tasks, err)
	}
	newer, err := st.GetTemplate(t.Context(), key)
	if err != nil || newer.State != model.VersionApproved {
		t.Fatal(newer, err)
	}
}

// 两个入口各自持有服务和连接池，只共享数据库；数据库报告的等待关系决定放行顺序。
func Test任务提交与独立实例审批的契约(t *testing.T) {
	for _, c := range []struct {
		name, action, gate  string
		first               bool
		wantCode, wantTasks int
	}{
		{"新版本先审批，旧选择拒绝", "approve", "", true, 409, 0},
		{"已读旧选择，审批先于身份保护提交", "approve", "before", false, 409, 0},
		{"任务已锁版本，新版本审批等待提交", "approve", "tasks", false, 200, 1},
		{"同版本先废弃，旧选择拒绝", "deprecate", "", true, 409, 0},
		{"已读旧选择，废弃先于身份保护提交", "deprecate", "before", false, 409, 0},
		{"任务已锁版本，同版本废弃等待提交", "deprecate", "tasks", false, 200, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			dsn := pgtest.Schema(t)
			st := pgtest.OpenAt(t, dsn)
			other := pgtest.OpenAt(t, dsn)
			newServer := func(db *pgstore.Store) *Server {
				return New(wizardDeps(Deps{Tasks: db, Events: db, AuditReader: audit.NewReader(db), Clock: func() int64 { return fixedAt }}, db))
			}
			s, second := newServer(st), newServer(other)
			_, _, token, iso, tpl := seedWizardFixture(t, s, st)
			secondToken := loginHTTP(t, second, "admin", "admin password").Token
			version, rev := tpl.Version, tpl.Rev
			if c.action == "approve" {
				next, err := st.NewTemplateVersion(t.Context(), tpl.ID, tpl.Version, "author")
				if err != nil {
					t.Fatal(err)
				}
				if err := st.SubmitTemplate(t.Context(), model.TemplateKey{ID: next.ID, Version: next.Version}, next.Rev, repository.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
					t.Fatal(err)
				}
				version, rev = next.Version, next.Rev+1
			}
			before, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			change := func() int {
				return httpCall(second, "POST", fmt.Sprintf("/console/v1/templates/%s/versions/%d/%s?rev=%d", tpl.ID, version, c.action, rev), "", secondToken).Code
			}
			submit := func() int {
				w := httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token)
				if c.wantCode == 409 {
					var body envelope
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != 409 || body.Data != nil || body.Message != model.ConsoleTemplateCurrent {
						t.Errorf("选择过期响应应为原有409及空data：%s", w.Body.String())
					}
				}
				return w.Code
			}
			code := 0
			if c.first {
				if got := change(); got != 200 {
					t.Fatalf("状态变更失败：%d", got)
				}
				code = submit()
			} else if c.gate == "before" {
				reached, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				assembled := wizardDeps(s.server.deps, st, &wizardCurrentTaskGate{CurrentTaskCreator: st, reached: reached, release: release})
				s.server.deps.Wizard, s.server.deps.TemplateAuthoring = assembled.Wizard, assembled.TemplateAuthoring
				submitted := make(chan int, 1)
				go func() { submitted <- submit() }()
				select {
				case <-reached:
				case <-time.After(5 * time.Second):
					t.Fatal("任务未到达身份保护前的停顿点")
				}
				if got := change(); got != 200 {
					t.Fatalf("状态变更失败：%d", got)
				}
				t.Log("应用已读旧选择，独立实例状态变更先于身份保护提交")
				unblock()
				code = wizardAwaitCode(t, submitted)
			} else {
				conn, err := pgx.Connect(t.Context(), dsn)
				if err != nil {
					t.Fatal("无法连接数据库锁夹具")
				}
				defer conn.Close(context.Background())
				tx, err := conn.Begin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				if _, err := tx.Exec(t.Context(), `LOCK TABLE tasks IN SHARE MODE`); err != nil {
					t.Fatal(err)
				}
				submitted := make(chan int, 1)
				go func() { submitted <- submit() }()
				writer := wizardWaitForDatabaseGate(t, tx, "tasks", int(conn.PgConn().PID()))
				t.Logf("已确认任务连接 %d 在 tasks 等待夹具连接 %d", writer, conn.PgConn().PID())
				changed := make(chan int, 1)
				go func() { changed <- change() }()
				overtook, got := wizardChangeCompletedOrBlocked(t, tx, changed, writer)
				if overtook {
					if got != 200 {
						t.Fatalf("状态变更失败：%d", got)
					}
					t.Error("任务已锁住所选版本并开始插入，独立实例状态变更仍先提交")
				} else {
					t.Log("已确认状态变更正在等待任务事务")
				}
				if err := tx.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
				code = wizardAwaitCode(t, submitted)
				if !overtook && wizardAwaitCode(t, changed) != 200 {
					t.Fatal("放行后状态变更失败")
				}
			}
			tasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
			if err != nil {
				t.Fatal(err)
			}
			after, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			created := 0
			for _, a := range after.Items {
				if a.Action == "task.create" {
					created++
				}
			}
			t.Logf("提交返回 %d，任务 %d 条，任务审计 %d 条，全部新增审计 %d 条", code, len(tasks.Items), created, len(after.Items)-len(before.Items))
			if code != c.wantCode || len(tasks.Items) != c.wantTasks || created != c.wantTasks || len(after.Items)-len(before.Items) != c.wantTasks+1 {
				t.Errorf("应返回 %d、任务和任务审计各 %d 条、全部新增审计 %d 条", c.wantCode, c.wantTasks, c.wantTasks+1)
			}
			for _, task := range tasks.Items {
				if task.TemplateVersion != tpl.Version || task.Snapshot.Template.Version != tpl.Version {
					t.Errorf("任务没有固定所选版本：%+v", task)
				}
			}
			current, err := other.GetTemplate(t.Context(), model.TemplateKey{ID: tpl.ID, Version: version})
			if err != nil {
				t.Fatal(err)
			}
			wantState := model.VersionApproved
			if c.action == "deprecate" {
				wantState = model.VersionDeprecated
			}
			if current.State != wantState {
				t.Errorf("状态应为 %s，得到 %s", wantState, current.State)
			}
		})
	}
}

func wizardWaitForDatabaseGate(t *testing.T, tx pgx.Tx, table string, blocker int) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		var pid int
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT a.pid FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(a.pid)) AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation=to_regclass($1)) LIMIT 1),0)`, table, blocker).Scan(&pid)
		if err != nil {
			t.Fatalf("未能确认任务到达数据库锁：%v", err)
		}
		if pid != 0 {
			return pid
		}
		select {
		case <-ctx.Done():
			t.Fatal("任务未到达数据库锁")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func wizardChangeCompletedOrBlocked(t *testing.T, tx pgx.Tx, done <-chan int, writer int) (bool, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		select {
		case code := <-done:
			return true, code
		default:
		}
		var blocked bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.wait_event_type='Lock' AND $1=ANY(pg_blocking_pids(a.pid)) AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND NOT l.granted) AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.relation IN ('templates'::regclass,'template_versions'::regclass)))`, writer).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return false, 0
		}
		select {
		case code := <-done:
			return true, code
		case <-ctx.Done():
			t.Fatal("状态变更既未返回，也未观察到它等待任务事务")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func wizardAwaitCode(t *testing.T, done <-chan int) int {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("操作未完成")
		return 0
	}
}

// 只暂停真正的写入入口，恢复后仍使用真实数据库事务。
type wizardCurrentTaskGate struct {
	repository.CurrentTaskCreator
	reached chan<- struct{}
	release <-chan struct{}
}

func (g *wizardCurrentTaskGate) CreateCurrentTasks(ctx context.Context, tasks []model.Task, a repository.Audit) ([]model.Task, error) {
	close(g.reached)
	select {
	case <-g.release:
		return g.CurrentTaskCreator.CreateCurrentTasks(ctx, tasks, a)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func Test向导事务内一般准入仍返回422(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	assembled := wizardDeps(s.server.deps, st, &wizardCurrentTaskGate{CurrentTaskCreator: st, reached: reached, release: release})
	s.server.deps.Wizard, s.server.deps.TemplateAuthoring = assembled.Wizard, assembled.TemplateAuthoring
	submitted := make(chan int, 1)
	go func() {
		submitted <- httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token).Code
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("未到达事务前停顿点")
	}
	if err := st.DeprecateISO(t.Context(), iso.ID, iso.Rev, repository.Audit{Actor: "seed", Action: "iso.deprecate", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	unblock()
	if code := wizardAwaitCode(t, submitted); code != 422 {
		t.Fatalf("镜像废弃应为422，得到%d", code)
	}
	tasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
	if err != nil || len(tasks.Items) != 0 {
		t.Fatal(tasks, err)
	}
	audit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range audit.Items {
		if a.Action == "task.create" {
			t.Fatal("失败提交不应留下任务审计")
		}
	}
}
