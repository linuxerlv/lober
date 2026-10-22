package authoring

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

type templateGuardProbe struct{ events *[]string }

func (g templateGuardProbe) ReadLock() func() {
	*g.events = append(*g.events, "read-lock")
	return func() { *g.events = append(*g.events, "read-unlock") }
}
func (g templateGuardProbe) WriteLock() func() {
	*g.events = append(*g.events, "write-lock")
	return func() { *g.events = append(*g.events, "write-unlock") }
}

type templateProbe struct {
	port.Templates
	events            *[]string
	item              model.Template
	writeErr, readErr error
	writes            int
	audit             repository.Audit
	approver          string
}

func (p *templateProbe) GetTemplate(ctx context.Context, _ model.TemplateKey) (model.Template, error) {
	if p.events != nil {
		*p.events = append(*p.events, "reload")
	}
	if ctx.Err() != nil {
		return model.Template{}, ctx.Err()
	}
	return p.item, p.readErr
}
func (p *templateProbe) ApproveTemplate(ctx context.Context, _ model.TemplateKey, _ int64, who string, a repository.Audit) error {
	p.approver = who
	return p.write(ctx, a)
}
func (p *templateProbe) DeprecateTemplate(ctx context.Context, _ model.TemplateKey, _ int64, a repository.Audit) error {
	return p.write(ctx, a)
}
func (p *templateProbe) write(ctx context.Context, a repository.Audit) error {
	p.writes++
	p.audit = a
	if p.events != nil {
		*p.events = append(*p.events, "write")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return p.writeErr
}
func (p *templateProbe) SaveTemplateDraft(_ context.Context, v model.Template) (model.Template, error) {
	p.writes++
	p.item = v
	return v, p.writeErr
}
func (p *templateProbe) CreateTemplate(_ context.Context, v model.Template) (model.Template, error) {
	p.writes++
	p.item = v
	return v, p.writeErr
}
func (p *templateProbe) DeriveTemplate(_ context.Context, k model.TemplateKey, name, author string) (model.Template, error) {
	p.writes++
	v := p.item
	v.DerivedFrom = &k
	v.Name = name
	v.Author = author
	v.State = model.VersionDraft
	v.Builtin = false
	v.Approver = ""
	return v, p.writeErr
}

type templateHookProbe struct {
	port.HookReader
	events *[]string
	err    error
}

func (p templateHookProbe) GetHooks(ctx context.Context, _ []model.HookKey) (map[model.HookKey]model.Hook, error) {
	if p.events != nil {
		*p.events = append(*p.events, "hooks")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return map[model.HookKey]model.Hook{}, p.err
}
func Test模板应用审批使用同一个选择锁(t *testing.T) {
	for _, op := range []string{"approve", "deprecate"} {
		for _, failure := range []string{"", "write", "reload", "hooks", "cancel", "reload-cancel", "hooks-cancel"} {
			t.Run(op+"/"+failure, func(t *testing.T) {
				events := []string{}
				failed := errors.New("失败")
				p := &templateProbe{events: &events, item: model.Template{ID: "template-1", Version: 1}}
				hp := templateHookProbe{events: &events}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				switch failure {
				case "write":
					p.writeErr = failed
				case "reload":
					p.readErr = failed
				case "hooks":
					hp.err = failed
				case "reload-cancel":
					p.readErr = context.Canceled
				case "hooks-cancel":
					hp.err = context.Canceled
				case "cancel":
					cancel()
				}
				guard := templateGuardProbe{&events}
				s := NewTemplates(TemplateDeps{Templates: p, Hooks: hp, Clock: func() int64 { return 1790000000 }, SelectionGuard: guard})
				release := guard.ReadLock()
				release()
				r := TemplateRevision{Key: model.TemplateKey{ID: p.item.ID, Version: p.item.Version}, Rev: 2}
				a := ports.Actor{Username: "reviewer", Role: model.RoleHookApprover}
				var err error
				if op == "approve" {
					_, err = s.Approve(ctx, a, r)
				} else {
					_, err = s.Deprecate(ctx, a, r)
				}
				want := []string{"read-lock", "read-unlock", "write-lock", "write"}
				if failure != "write" && failure != "cancel" {
					want = append(want, "reload")
					if failure != "reload" && failure != "reload-cancel" {
						want = append(want, "hooks")
					}
				}
				want = append(want, "write-unlock")
				if !reflect.DeepEqual(events, want) {
					t.Fatalf("顺序=%v want=%v", events, want)
				}
				wantErr := failed
				if failure == "cancel" || failure == "reload-cancel" || failure == "hooks-cancel" {
					wantErr = context.Canceled
				}
				if failure == "" {
					wantErr = nil
				}
				if !errors.Is(err, wantErr) || p.writes != 1 {
					t.Fatal(err, p.writes)
				}
				if p.audit.Actor != "reviewer" || p.audit.Action != "template."+op || p.audit.At != 1790000000 {
					t.Fatal(p.audit)
				}
			})
		}
	}
}
func Test模板应用派生和保存只覆盖内容(t *testing.T) {
	events := []string{}
	key := model.TemplateKey{ID: "source-1", Version: 2}
	a := ports.Actor{Username: "alice", Role: model.RoleHookAuthor}
	for _, state := range []model.VersionState{model.VersionDraft, model.VersionReview, model.VersionApproved, model.VersionDeprecated} {
		p := &templateProbe{item: model.Template{ID: "template-1", Version: 1, Rev: 4, Name: "original", Author: "old", Approver: "reviewer", Builtin: true, State: state, DerivedFrom: &key}}
		s := NewTemplates(TemplateDeps{Templates: p, Hooks: templateHookProbe{}, Clock: func() int64 { t.Fatal("草稿操作不应读时钟"); return 0 }, SelectionGuard: templateGuardProbe{&events}})
		got, err := s.Derive(t.Context(), a, DeriveRequest{From: model.TemplateKey{ID: p.item.ID, Version: p.item.Version}, Name: "derived"})
		if err != nil || got.Template.State != model.VersionDraft || got.Template.Author != "alice" || got.Template.Builtin || got.Template.Approver != "" || *got.Template.DerivedFrom != (model.TemplateKey{ID: p.item.ID, Version: p.item.Version}) {
			t.Fatal(got, err)
		}
		in := model.Template{ID: p.item.ID, Version: 1, Rev: 4, Author: "fake", Approver: "fake", Body: "new", DerivedFrom: &model.TemplateKey{ID: "fake", Version: 1}}
		got, err = s.SaveDraft(t.Context(), a, in)
		if err != nil {
			t.Fatal(err)
		}
		v := got.Template
		if v.Name != "original" || v.Author != "old" || v.Approver != "reviewer" || !v.Builtin || v.State != state || *v.DerivedFrom != key || v.Body != "new" {
			t.Fatal(v)
		}
		in.Name = "wrong"
		if _, err = s.SaveDraft(t.Context(), a, in); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
func Test模板写后引用读取失败不重试(t *testing.T) {
	for _, save := range []bool{false, true} {
		p := &templateProbe{item: model.Template{ID: "template-1", Version: 1, Rev: 1, Name: "draft"}}
		failed := errors.New("引用读取失败")
		events := []string{}
		s := NewTemplates(TemplateDeps{Templates: p, Hooks: templateHookProbe{err: failed}, Clock: func() int64 { return 1 }, SelectionGuard: templateGuardProbe{&events}})
		a := ports.Actor{Username: "alice", Role: model.RoleHookAuthor}
		var err error
		if save {
			_, err = s.SaveDraft(t.Context(), a, p.item)
		} else {
			_, err = s.Create(t.Context(), a, p.item)
		}
		if !errors.Is(err, failed) || p.writes != 1 {
			t.Fatal(err, p.writes)
		}
	}
}

func Test模板应用返回内容不共用深层引用(t *testing.T) {
	min, max := int64(1), int64(9)
	timeout, required := 7, true
	from := model.TemplateKey{ID: "source-1", Version: 1}
	original := model.Template{ID: "template-1", Version: 1, DerivedFrom: &from, Series: []catalog.Series{"rocky"}, Params: []model.ParamDecl{{Name: "choice", Type: "enum", Options: []model.Option{{Value: "a", Label: "A"}}, Min: &min, Max: &max}}, Bindings: map[string]model.Binding{"choice": {Value: "a"}}, Assembly: map[string][]model.Instance{"disk": {{HookID: "hook-1", HookVersion: 1, TimeoutSec: &timeout, Required: &required, Bindings: map[string]model.Binding{"disk": {Value: "old"}}}}}}
	p := &templateProbe{item: original}
	events := []string{}
	s := NewTemplates(TemplateDeps{Templates: p, Hooks: templateHookProbe{}, Clock: func() int64 { return 1 }, SelectionGuard: templateGuardProbe{&events}})
	v, err := s.Get(t.Context(), ports.Actor{Username: "viewer", Role: model.RoleViewer}, model.TemplateKey{ID: original.ID, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	v.Template.DerivedFrom.Version = 9
	v.Template.Series[0] = "ubuntu"
	v.Template.Params[0].Options[0].Label = "changed"
	*v.Template.Params[0].Min = 2
	*v.Template.Params[0].Max = 3
	v.Template.Bindings["choice"] = model.Binding{Value: "b"}
	*v.Template.Assembly["disk"][0].TimeoutSec = 99
	*v.Template.Assembly["disk"][0].Required = false
	v.Template.Assembly["disk"][0].Bindings["disk"] = model.Binding{Value: "new"}
	if original.DerivedFrom.Version != 1 || original.Series[0] != "rocky" || original.Params[0].Options[0].Label != "A" || min != 1 || max != 9 || original.Bindings["choice"].Value != "a" || timeout != 7 || !required || original.Assembly["disk"][0].Bindings["disk"].Value != "old" {
		t.Fatal("响应修改了原值", original)
	}
}
func Test模板应用构造缺少能力立即拒绝(t *testing.T) {
	events := []string{}
	valid := TemplateDeps{Templates: &templateProbe{}, Hooks: templateHookProbe{}, Clock: func() int64 { return 1 }, SelectionGuard: templateGuardProbe{&events}}
	for i := 0; i < 4; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			d := valid
			switch i {
			case 0:
				d.Templates = nil
			case 1:
				d.Hooks = nil
			case 2:
				d.Clock = nil
			case 3:
				d.SelectionGuard = nil
			}
			defer func() {
				if recover() == nil {
					t.Fatal("缺少必要能力未拒绝")
				}
			}()
			NewTemplates(d)
		})
	}
}

type templateAuthProbe struct {
	port.Templates
	calls int
}

func (p *templateAuthProbe) touch() error { p.calls++; return repository.ErrNotFound }
func (p *templateAuthProbe) GetTemplate(context.Context, model.TemplateKey) (model.Template, error) {
	return model.Template{}, p.touch()
}
func (p *templateAuthProbe) GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error) {
	return nil, p.touch()
}
func (p *templateAuthProbe) GetHook(context.Context, model.HookKey) (model.Hook, error) {
	return model.Hook{}, p.touch()
}
func (p *templateAuthProbe) ListHooks(context.Context, repository.HookQuery) (repository.Page[model.Hook], error) {
	return repository.Page[model.Hook]{}, p.touch()
}
func (p *templateAuthProbe) ListHookVersions(context.Context, string) ([]model.Hook, error) {
	return nil, p.touch()
}
func (p *templateAuthProbe) ListTemplates(context.Context, repository.TemplateQuery) (repository.Page[model.Template], error) {
	return repository.Page[model.Template]{}, p.touch()
}
func (p *templateAuthProbe) ListTemplateVersions(context.Context, string) ([]model.Template, error) {
	return nil, p.touch()
}
func (p *templateAuthProbe) CreateTemplate(context.Context, model.Template) (model.Template, error) {
	return model.Template{}, p.touch()
}
func (p *templateAuthProbe) SaveTemplateDraft(context.Context, model.Template) (model.Template, error) {
	return model.Template{}, p.touch()
}
func (p *templateAuthProbe) NewTemplateVersion(context.Context, string, int, string) (model.Template, error) {
	return model.Template{}, p.touch()
}
func (p *templateAuthProbe) DeriveTemplate(context.Context, model.TemplateKey, string, string) (model.Template, error) {
	return model.Template{}, p.touch()
}
func (p *templateAuthProbe) DeleteTemplateDraft(context.Context, model.TemplateKey, int64) error {
	return p.touch()
}
func (p *templateAuthProbe) RenameTemplate(context.Context, string, string, repository.Audit) error {
	return p.touch()
}
func (p *templateAuthProbe) SubmitTemplate(context.Context, model.TemplateKey, int64, repository.Audit) error {
	return p.touch()
}
func (p *templateAuthProbe) ApproveTemplate(context.Context, model.TemplateKey, int64, string, repository.Audit) error {
	return p.touch()
}
func (p *templateAuthProbe) RejectTemplate(context.Context, model.TemplateKey, int64, repository.Audit) error {
	return p.touch()
}
func (p *templateAuthProbe) DeprecateTemplate(context.Context, model.TemplateKey, int64, repository.Audit) error {
	return p.touch()
}
func Test模板应用拒绝无权调用且不碰仓储(t *testing.T) {
	key := model.TemplateKey{ID: "template-1", Version: 1}
	type operation struct {
		name       string
		permission string
		call       func(Templates, ports.Actor, bool) error
	}
	operations := []operation{
		{"derive", "write", func(s Templates, a ports.Actor, bad bool) error {
			r := DeriveRequest{From: key, Name: "copy"}
			if bad {
				r.From.Version = 0
			}
			_, e := s.Derive(t.Context(), a, r)
			return e
		}},
		{"list", "read", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateListRequest{}
			if bad {
				r.Page = -1
			}
			_, e := s.List(t.Context(), a, r)
			return e
		}},
		{"versions", "read", func(s Templates, a ports.Actor, bad bool) error {
			id := key.ID
			if bad {
				id = ""
			}
			_, e := s.Versions(t.Context(), a, id)
			return e
		}},
		{"get", "read", func(s Templates, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.Get(t.Context(), a, k)
			return e
		}},
		{"create", "write", func(s Templates, a ports.Actor, bad bool) error {
			h := model.Template{Name: "name"}
			if bad {
				h.Name = ""
			}
			_, e := s.Create(t.Context(), a, h)
			return e
		}},
		{"save", "write", func(s Templates, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.SaveDraft(t.Context(), a, model.Template{ID: k.ID, Version: k.Version, Rev: 1})
			return e
		}},
		{"copy", "write", func(s Templates, a ports.Actor, bad bool) error {
			r := VersionSource{ID: key.ID, From: 1}
			if bad {
				r.From = 0
			}
			_, e := s.NewVersion(t.Context(), a, r)
			return e
		}},
		{"delete", "write", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			return s.DeleteDraft(t.Context(), a, r)
		}},
		{"rename", "write", func(s Templates, a ports.Actor, bad bool) error {
			r := RenameRequest{ID: key.ID, Name: "name"}
			if bad {
				r.ID = ""
				r.Name = ""
			}
			return s.Rename(t.Context(), a, r)
		}},
		{"submit", "write", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Submit(t.Context(), a, r)
			return e
		}},
		{"approve", "approve", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Approve(t.Context(), a, r)
			return e
		}},
		{"reject", "approve", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateRejection{TemplateRevision: TemplateRevision{Key: key, Rev: 1}, Reason: "fix"}
			if bad {
				r.Reason = " "
				r.Rev = 0
			}
			_, e := s.Reject(t.Context(), a, r)
			return e
		}},
		{"deprecate", "approve", func(s Templates, a ports.Actor, bad bool) error {
			r := TemplateRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Deprecate(t.Context(), a, r)
			return e
		}},
		{"validate", "write", func(s Templates, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.Validate(t.Context(), a, k)
			return e
		}},
	}
	actors := []struct {
		name    string
		actor   ports.Actor
		invalid error
	}{
		{"viewer", ports.Actor{Username: "viewer", Role: model.RoleViewer}, nil},
		{"operator", ports.Actor{Username: "operator", Role: model.RoleOperator}, nil},
		{"author", ports.Actor{Username: "author", Role: model.RoleHookAuthor}, nil},
		{"approver", ports.Actor{Username: "approver", Role: model.RoleHookApprover}, nil},
		{"admin", ports.Actor{Username: "admin", Role: model.RoleAdmin}, nil},
		{"missing", ports.Actor{Role: model.RoleAdmin}, ports.ErrUnauthenticated},
		{"role", ports.Actor{Username: "who", Role: "bad", MustChangePassword: true}, ports.ErrUnauthenticated},
		{"disabled", ports.Actor{Username: "who", Role: model.RoleAdmin, Disabled: true, MustChangePassword: true}, ports.ErrUnauthenticated},
		{"password", ports.Actor{Username: "who", Role: model.RoleViewer, MustChangePassword: true}, ports.ErrPasswordChangeRequired},
	}
	for _, a := range actors {
		for _, op := range operations {
			for _, bad := range []bool{false, true} {
				name := a.name + "/" + op.name
				if bad {
					name += "/invalid"
				}
				t.Run(name, func(t *testing.T) {
					p := &templateAuthProbe{}
					ticks := 0
					s := NewTemplates(TemplateDeps{Templates: p, Hooks: p, SelectionGuard: templateGuardProbe{new([]string)}, Clock: func() int64 { ticks++; return 1790000000 }})
					want := a.invalid
					allowed := a.actor.Role == model.RoleAdmin || op.permission == "read" || op.permission == "write" && a.actor.Role == model.RoleHookAuthor || op.permission == "approve" && a.actor.Role == model.RoleHookApprover
					if want == nil && !allowed {
						want = ports.ErrForbidden
					}
					err := op.call(s, a.actor, bad)
					if want != nil {
						if !errors.Is(err, want) || p.calls != 0 || ticks != 0 {
							t.Fatalf("err=%v want=%v repo=%d clock=%d", err, want, p.calls, ticks)
						}
					} else {
						if errors.Is(err, ports.ErrUnauthenticated) || errors.Is(err, ports.ErrForbidden) || errors.Is(err, ports.ErrPasswordChangeRequired) {
							t.Fatal(err)
						}
						if !bad && p.calls == 0 {
							t.Fatal("有效调用未到仓储")
						}
						if op.permission == "read" || op.name == "create" || op.name == "save" || op.name == "copy" || op.name == "derive" || op.name == "delete" || op.name == "validate" {
							if ticks != 0 {
								t.Fatal("无审计操作读取了时钟")
							}
						}
					}
				})
			}
		}
	}
}
