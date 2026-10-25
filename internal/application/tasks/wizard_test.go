package tasks

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type wizardStore struct {
	calls         []string
	hosts         map[string]model.Host
	iso           model.ISO
	template      model.Template
	versions      []model.Template
	hooks         map[model.HookKey]model.Hook
	open          map[string]bool
	hostPage      repository.Page[model.Host]
	isoPage       repository.Page[model.ISO]
	templatePage  repository.Page[model.Template]
	candidatePage repository.Page[repository.TemplateCandidateFacts]
	hostQuery     repository.HostQuery
	isoQuery      repository.ISOQuery
	templateQuery repository.TemplateQuery
	fail          map[string]error
	create        func(context.Context, []model.Task, repository.Audit) ([]model.Task, error)
	created       []model.Task
	audit         repository.Audit
}

func (f *wizardStore) called(name string) error { f.calls = append(f.calls, name); return f.fail[name] }
func (f *wizardStore) ListHosts(_ context.Context, q repository.HostQuery) (repository.Page[model.Host], error) {
	f.hostQuery = q
	return f.hostPage, f.called("hosts")
}
func (f *wizardStore) GetHosts(_ context.Context, sns []string) (map[string]model.Host, error) {
	return f.hosts, f.called("get-hosts")
}
func (f *wizardStore) GetISO(context.Context, string) (model.ISO, error) {
	return f.iso, f.called("iso")
}
func (f *wizardStore) ListISOs(_ context.Context, q repository.ISOQuery) (repository.Page[model.ISO], error) {
	f.isoQuery = q
	return f.isoPage, f.called("isos")
}
func (f *wizardStore) GetTemplate(context.Context, model.TemplateKey) (model.Template, error) {
	return f.template, f.called("template")
}
func (f *wizardStore) ListTemplates(_ context.Context, q repository.TemplateQuery) (repository.Page[model.Template], error) {
	f.templateQuery = q
	return f.templatePage, f.called("templates")
}
func (f *wizardStore) ListTemplateCandidates(_ context.Context, q repository.TemplateQuery) (repository.Page[repository.TemplateCandidateFacts], error) {
	f.templateQuery = q
	return f.candidatePage, f.called("candidates")
}
func (f *wizardStore) ListTemplateVersions(context.Context, string) ([]model.Template, error) {
	return f.versions, f.called("versions")
}
func (f *wizardStore) GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error) {
	return f.hooks, f.called("hooks")
}
func (f *wizardStore) FindOpenTask(_ context.Context, sn string) (model.Task, bool, error) {
	return model.Task{}, f.open[sn], f.called("open:" + sn)
}
func (f *wizardStore) CreateCurrentTasks(ctx context.Context, ts []model.Task, a repository.Audit) ([]model.Task, error) {
	if err := f.called("create-current"); err != nil {
		return nil, err
	}
	if f.create != nil {
		return f.create(ctx, ts, a)
	}
	f.created = ts
	f.audit = a
	for i := range ts {
		ts[i].ID = ts[i].HostSN
		ts[i].State = model.TaskPending
		ts[i].Rev = 1
	}
	return ts, nil
}

type testGuard struct{ sync.RWMutex }

func (g *testGuard) ReadLock() func()  { g.RLock(); return g.RUnlock }
func (g *testGuard) WriteLock() func() { g.Lock(); return g.Unlock }

type testNotifier struct {
	sync.Mutex
	ids []string
}

func (n *testNotifier) Publish(id string)                          { n.Lock(); defer n.Unlock(); n.ids = append(n.ids, id) }
func (n *testNotifier) Subscribe(string) (<-chan struct{}, func()) { panic("向导不订阅") }
func (n *testNotifier) published() []string                        { n.Lock(); defer n.Unlock(); return slices.Clone(n.ids) }

var operator = ports.Actor{Username: "operator", Role: model.RoleOperator}

func applicationFixture() (*wizardStore, WizardDeps, Selection, *testNotifier) {
	// 沿用已有领域及网页测试中的 Rocky 模板正文，只构造数据，不重写规则。
	tpl := model.Template{ID: "template", Version: 1, Name: "wizard", State: model.VersionApproved, Author: "author", Approver: "reviewer", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# {{param:hostname}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n", Params: []model.ParamDecl{{Name: "hostname", Label: "主机名", Type: "hostname", Default: "node"}}}
	iso := model.ISO{ID: "iso", Name: "Rocky", Family: "linux", Series: "rocky", Version: "9", Arch: "x86_64", State: model.ISOReady}
	f := &wizardStore{hosts: map[string]model.Host{}, iso: iso, template: tpl, versions: []model.Template{tpl}, hooks: map[model.HookKey]model.Hook{}, open: map[string]bool{}, fail: map[string]error{}}
	for _, sn := range []string{"SN-1", "SN-2"} {
		f.hosts[sn] = model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi"}
		f.hostPage.Items = append(f.hostPage.Items, f.hosts[sn])
	}
	f.isoPage.Items = []model.ISO{iso}
	f.templatePage.Items = []model.Template{tpl}
	f.candidatePage.Items = []repository.TemplateCandidateFacts{{Template: tpl, Hooks: f.hooks}}
	n := &testNotifier{}
	d := WizardDeps{Hosts: f, ISOs: f, Templates: f, Hooks: f, Tasks: f, Clock: func() int64 { return 1790000000 }, Notifier: n, SelectionGuard: &testGuard{}}
	return f, d, Selection{ISOID: iso.ID, TemplateID: tpl.ID, TemplateVersion: 1}, n
}
func Test应用向导不能绕过权限(t *testing.T) {
	for _, tc := range []struct {
		name  string
		actor ports.Actor
		want  error
	}{
		{"空身份", ports.Actor{}, ErrUnauthenticated},
		{"无账号", ports.Actor{Role: model.RoleAdmin}, ErrUnauthenticated},
		{"停用", ports.Actor{Username: "u", Role: model.RoleAdmin, Disabled: true, MustChangePassword: true}, ErrUnauthenticated},
		{"非法角色", ports.Actor{Username: "u", Role: "other"}, ErrUnauthenticated},
		{"先改密", ports.Actor{Username: "u", Role: model.RoleViewer, MustChangePassword: true}, ErrPasswordChangeRequired},
		{"查看", ports.Actor{Username: "u", Role: model.RoleViewer}, ErrForbidden},
		{"编写", ports.Actor{Username: "u", Role: model.RoleHookAuthor}, ErrForbidden},
		{"审批", ports.Actor{Username: "u", Role: model.RoleHookApprover}, ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, d, s, _ := applicationFixture()
			w := NewWizard(d)
			for _, call := range []func() error{
				func() error { _, e := w.Hosts(t.Context(), tc.actor, HostRequest{}); return e },
				func() error { _, e := w.ISOs(t.Context(), tc.actor, ISORequest{HostSNs: []string{"SN-1"}}); return e },
				func() error { _, e := w.Templates(t.Context(), tc.actor, TemplateRequest{ISOID: s.ISOID}); return e },
				func() error {
					_, e := w.Params(t.Context(), tc.actor, ParamsRequest{Selection: s, HostSNs: []string{"SN-1"}})
					return e
				},
				func() error {
					_, e := w.Submit(t.Context(), tc.actor, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: "SN-1"}}})
					return e
				},
			} {
				if e := call(); !errors.Is(e, tc.want) {
					t.Fatalf("错误 %v，期待 %v", e, tc.want)
				}
			}
			if len(f.calls) != 0 {
				t.Fatal("拒绝身份仍读取仓储", f.calls)
			}
		})
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleOperator} {
		f, d, s, _ := applicationFixture()
		w, actor := NewWizard(d), ports.Actor{Username: "ok", Role: role}
		_, a := w.Hosts(t.Context(), actor, HostRequest{})
		_, b := w.ISOs(t.Context(), actor, ISORequest{HostSNs: []string{"SN-1"}})
		_, c := w.Templates(t.Context(), actor, TemplateRequest{ISOID: s.ISOID})
		_, e := w.Params(t.Context(), actor, ParamsRequest{Selection: s, HostSNs: []string{"SN-1"}})
		_, g := w.Submit(t.Context(), actor, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: "SN-1"}}})
		for _, err := range []error{a, b, c, e, g} {
			if err != nil {
				t.Fatal(role, err)
			}
		}
		if !slices.Contains(f.calls, "create-current") {
			t.Fatal(role, f.calls)
		}
	}
}
func Test应用参数顺序缺失和重复编号(t *testing.T) {
	f, d, s, _ := applicationFixture()
	w := NewWizard(d)
	got, err := w.Params(t.Context(), operator, ParamsRequest{Selection: s, HostSNs: []string{" sn-2 ", "SN-1", "MISSING", "sn-2"}, Entered: map[string]map[string]string{"sn-2": {"tpl/hostname": "node-two"}, "SN-1": {"tpl/hostname": "node-one"}}})
	if err != nil || len(got.Hosts) != 4 {
		t.Fatal(got, err)
	}
	for i, sn := range []string{"SN-2", "SN-1", "MISSING", "SN-2"} {
		if got.Hosts[i].SN != sn {
			t.Fatal(got)
		}
	}
	if len(got.Hosts[2].Fields) != 0 || !hasProblem(got.Hosts[2].Problems, "host_not_registered") || !hasProblem(got.Hosts[0].Problems, "host_duplicate_in_batch") {
		t.Fatal(got)
	}
	if len(got.Hosts[1].Problems) != 0 || len(got.Hosts[0].Fields) != 1 {
		t.Fatal(got)
	}
	if n := len(slices.DeleteFunc(slices.Clone(f.calls), func(v string) bool { return v != "open:SN-2" })); n != 1 {
		t.Fatal(f.calls)
	}
}
func hasProblem(ps []model.Problem, code model.Code) bool {
	for _, p := range ps {
		if p.Code == code {
			return true
		}
	}
	return false
}
func Test应用拒绝未知和重复输入行(t *testing.T) {
	for _, entered := range []map[string]map[string]string{{"OTHER": {}}, {"SN-1": {}, "sn-1": {}}, {"": {}}} {
		f, d, s, n := applicationFixture()
		_, err := NewWizard(d).Params(t.Context(), operator, ParamsRequest{Selection: s, HostSNs: []string{"SN-1"}, Entered: entered})
		if !errors.Is(err, repository.ErrInvalid) || slices.Contains(f.calls, "get-hosts") || len(n.published()) != 0 {
			t.Fatal(err, f.calls)
		}
	}
}
func Test应用当前审批与内置版本(t *testing.T) {
	for _, tc := range []string{"新草稿", "新审批", "模板内置过期", "模板内置当前", "组件内置过期"} {
		t.Run(tc, func(t *testing.T) {
			f, d, s, _ := applicationFixture()
			wantErr := false
			switch tc {
			case "新草稿", "新审批":
				next := f.template
				next.Version = 2
				next.State = model.VersionDraft
				if tc == "新审批" {
					next.State = model.VersionApproved
					wantErr = true
				}
				f.versions = append(f.versions, next)
			case "模板内置过期", "模板内置当前":
				f.template.Builtin = true
				wantErr = true
				if tc == "模板内置当前" {
					d.Manifest.Templates = []model.TemplateKey{{ID: f.template.ID, Version: 1}}
					wantErr = false
				}
			case "组件内置过期":
				f.template.Assembly = map[string][]model.Instance{"x": {{HookID: "hook", HookVersion: 1}}}
				f.hooks[model.HookKey{ID: "hook", Version: 1}] = model.Hook{ID: "hook", Version: 1, Builtin: true}
				wantErr = true
			}
			_, err := NewWizard(d).Params(t.Context(), operator, ParamsRequest{Selection: s, HostSNs: []string{"SN-1"}})
			if wantErr && !errors.Is(err, repository.ErrTemplateNotCurrent) || !wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}
func Test应用候选分页与错误先后(t *testing.T) {
	f, d, s, _ := applicationFixture()
	f.hostPage.Next = "host-next"
	f.isoPage.Next = "iso-next"
	f.templatePage.Next = "template-next"
	f.open["SN-2"] = true
	w := NewWizard(d)
	hp, e := w.Hosts(t.Context(), operator, HostRequest{Q: " sn-1 ", AttrKey: "rack", AttrValue: "a", PageRequest: PageRequest{Limit: 2, Cursor: "before"}})
	if e != nil || hp.Next != "host-next" || !hasProblem(hp.Items[1].Problems, "host_has_open_task") || f.hostQuery.SNPrefix != "SN-1" {
		t.Fatal(hp, e, f.hostQuery)
	}
	ip, e := w.ISOs(t.Context(), operator, ISORequest{HostSNs: []string{"SN-2", "SN-1"}})
	if e != nil || ip.Next != "iso-next" || !reflect.DeepEqual(f.isoQuery.States, []model.ISOState{model.ISOReady}) {
		t.Fatal(ip, e)
	}
	f.templatePage.Items[0].Builtin = true
	tp, e := w.Templates(t.Context(), operator, TemplateRequest{ISOID: s.ISOID})
	if e != nil || len(tp.Items) != 0 || tp.Next != "template-next" || !f.templateQuery.ApprovedOnly {
		t.Fatal(tp, e)
	}
	// 模板失效先于编号错误；镜像不存在先于非法页大小。
	f.versions = nil
	_, e = w.Params(t.Context(), operator, ParamsRequest{Selection: s, HostSNs: []string{""}})
	if !errors.Is(e, repository.ErrTemplateNotCurrent) {
		t.Fatal(e)
	}
	f.fail["iso"] = repository.ErrNotFound
	_, e = w.Templates(t.Context(), operator, TemplateRequest{ISOID: s.ISOID, PageRequest: PageRequest{Limit: -1}})
	if !errors.Is(e, repository.ErrNotFound) {
		t.Fatal(e)
	}
}
func Test应用空批与普通问题(t *testing.T) {
	_, d, s, _ := applicationFixture()
	w := NewWizard(d)
	_, a := w.ISOs(t.Context(), operator, ISORequest{})
	_, b := w.Params(t.Context(), operator, ParamsRequest{Selection: s})
	_, c := w.Submit(t.Context(), operator, SubmitRequest{Selection: s})
	for _, err := range []error{a, b, c} {
		if !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
	_, err := w.ISOs(t.Context(), operator, ISORequest{HostSNs: []string{"", "NONE"}})
	var ps *ProblemsError
	if !errors.As(err, &ps) || len(ps.Problems) != 2 {
		t.Fatal(err)
	}
}

func Test应用候选重复缺失保留问题顺序(t *testing.T) {
	f, d, _, _ := applicationFixture()
	_, err := NewWizard(d).ISOs(t.Context(), operator, ISORequest{HostSNs: []string{"missing", "MISSING"}, PageRequest: PageRequest{Limit: -1}})
	var batch *repository.BatchError
	if !errors.As(err, &batch) || len(batch.Result.ByHost["MISSING"]) != 3 || slices.Contains(f.calls, "isos") {
		t.Fatal(err, f.calls)
	}
	for i, want := range []model.Code{"host_duplicate_in_batch", "host_not_registered", "host_not_registered"} {
		if batch.Result.ByHost["MISSING"][i].Code != want {
			t.Fatal(batch)
		}
	}
}

func Test应用页码候选传递页码总数与过滤事实(t *testing.T) {
	f, d, selection, _ := applicationFixture()
	total := int64(7)
	f.hostPage.Total, f.isoPage.Total, f.candidatePage.Total = &total, &total, &total
	d.Manifest.Templates = []model.TemplateKey{{ID: "builtin", Version: 2}}
	d.Manifest.Hooks = []model.HookKey{{ID: "hook", Version: 3}}
	w := NewWizard(d)
	page := PageRequest{Page: 2, Limit: 1}
	hosts, err := w.Hosts(t.Context(), operator, HostRequest{PageRequest: page})
	if err != nil || hosts.Total == nil || *hosts.Total != total || f.hostQuery.Page != 2 || f.hostQuery.Limit != 1 {
		t.Fatal(hosts, f.hostQuery, err)
	}
	isos, err := w.ISOs(t.Context(), operator, ISORequest{PageRequest: page, HostSNs: []string{"SN-1"}})
	if err != nil || isos.Total == nil || *isos.Total != total || f.isoQuery.Page != 2 || f.isoQuery.Limit != 1 {
		t.Fatal(isos, f.isoQuery, err)
	}
	templates, err := w.Templates(t.Context(), operator, TemplateRequest{PageRequest: page, ISOID: selection.ISOID})
	facts := f.templateQuery.Selection
	if err != nil || templates.Total == nil || *templates.Total != total || f.templateQuery.Page != 2 || f.templateQuery.Limit != 1 || facts == nil {
		t.Fatal(templates, f.templateQuery, err)
	}
	if !reflect.DeepEqual(facts.ISO, f.iso) || !reflect.DeepEqual(facts.BuiltinTemplates, d.Manifest.Templates) || !reflect.DeepEqual(facts.BuiltinHooks, d.Manifest.Hooks) {
		t.Fatal(facts)
	}
}

func Test应用页码直接用同次事实且保留游标二次读取(t *testing.T) {
	for _, numbered := range []bool{false, true} {
		t.Run(map[bool]string{true: "页码", false: "游标"}[numbered], func(t *testing.T) {
			f, d, s, _ := applicationFixture()
			page := 0
			if numbered {
				page = 1
			}
			first := f.template
			first.Assembly = map[string][]model.Instance{"pre": {{HookID: "one", HookVersion: 1}}}
			second := first
			second.ID = "other"
			second.Assembly = map[string][]model.Instance{"pre": {{HookID: "two", HookVersion: 1}}}
			one := model.Hook{ID: "one", Version: 1, State: model.VersionApproved}
			two := model.Hook{ID: "two", Version: 1, State: model.VersionApproved}
			f.candidatePage.Items = []repository.TemplateCandidateFacts{{Template: first, Hooks: map[model.HookKey]model.Hook{one.Key(): one}}, {Template: second, Hooks: map[model.HookKey]model.Hook{two.Key(): two}}}
			got, err := NewWizard(d).Templates(t.Context(), operator, TemplateRequest{ISOID: s.ISOID, PageRequest: PageRequest{Page: page}})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"iso", "templates", "hooks"}
			if numbered {
				want = []string{"iso", "candidates"}
				if len(got.Items) != 2 || len(got.Items[0].Hooks) != 2 || len(got.Items[1].Hooks) != 2 {
					t.Fatal(got)
				}
			}
			if !reflect.DeepEqual(f.calls, want) {
				t.Fatal(f.calls, want)
			}
			// 读取失败仍向外报告；页码的组件读取失败现在由同次facts能力报告。
			f.calls = nil
			failure := errors.New("读取事实失败")
			failureAt := "hooks"
			if numbered {
				failureAt = "candidates"
			}
			f.fail[failureAt] = failure
			_, err = NewWizard(d).Templates(t.Context(), operator, TemplateRequest{ISOID: s.ISOID, PageRequest: PageRequest{Page: page}})
			if !errors.Is(err, failure) {
				t.Fatal(err)
			}
		})
	}
}
