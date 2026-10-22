package authoring

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type editorReader struct {
	port.TemplateReader
	port.HookReader
	templates    map[model.TemplateKey]model.Template
	versions     []model.Template
	hooks        map[model.HookKey]model.Hook
	page         repository.Page[model.Hook]
	hookVersions []model.Hook
	query        repository.HookQuery
	reads        int
}

func (r *editorReader) GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error) {
	r.reads++
	if ctx.Err() != nil {
		return model.Template{}, ctx.Err()
	}
	v, ok := r.templates[k]
	if !ok {
		return v, repository.ErrNotFound
	}
	return v, nil
}
func (r *editorReader) ListTemplateVersions(context.Context, string) ([]model.Template, error) {
	return r.versions, nil
}
func (r *editorReader) GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error) {
	return r.hooks, nil
}
func (r *editorReader) ListHooks(_ context.Context, q repository.HookQuery) (repository.Page[model.Hook], error) {
	r.query = q
	return r.page, nil
}
func (r *editorReader) ListHookVersions(context.Context, string) ([]model.Hook, error) {
	return r.hookVersions, nil
}
func editorActor() ports.Actor { return ports.Actor{Username: "reader", Role: model.RoleAdmin} }
func editorFixture() (*editorReader, model.TemplateKey) {
	k := model.TemplateKey{ID: "template-1", Version: 1}
	tpl := model.Template{ID: k.ID, Version: k.Version, Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n{{param:undeclared}}", Assembly: map[string][]model.Instance{"disk": {{HookID: "missing", HookVersion: 1}, {HookID: "old", HookVersion: 1, Bindings: map[string]model.Binding{"keep": {Source: "fixed", Value: "value"}, "drop": {Source: "fixed", Value: "value"}}}}}}
	return &editorReader{templates: map[model.TemplateKey]model.Template{k: tpl}, hooks: map[model.HookKey]model.Hook{}}, k
}
func Test编辑辅助直调也检查模板读取权限(t *testing.T) {
	r, k := editorFixture()
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	for _, a := range []ports.Actor{{}, {Username: "x", Role: "invalid"}, {Username: "x", Role: model.RoleAdmin, Disabled: true}, {Username: "x", Role: model.RoleAdmin, MustChangePassword: true}} {
		want := ports.ErrUnauthenticated
		if a.MustChangePassword {
			want = ports.ErrPasswordChangeRequired
		}
		_, e1 := s.Slots(t.Context(), a, k)
		_, e2 := s.Candidates(t.Context(), a, CandidateRequest{Key: k})
		_, e3 := s.SourceDiff(t.Context(), a, k)
		for _, err := range []error{e1, e2, e3} {
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		}
	}
	if r.reads != 0 {
		t.Fatal("未授权却读取了模板")
	}
	for _, role := range []model.Role{model.RoleViewer, model.RoleOperator, model.RoleHookAuthor, model.RoleHookApprover, model.RoleAdmin} {
		a := ports.Actor{Username: "reader", Role: role}
		if _, err := s.Slots(t.Context(), a, k); err != nil {
			t.Fatal(role, err)
		}
		if _, err := s.Candidates(t.Context(), a, CandidateRequest{Key: k, Slot: "disk"}); err != nil {
			t.Fatal(role, err)
		}
		if _, err := s.SourceDiff(t.Context(), a, k); err != nil {
			t.Fatal(role, err)
		}
	}

}
func Test候选只采纳当前替换位置的问题(t *testing.T) {
	r, k := editorFixture()
	untouched, _ := editorFixture()
	original := untouched.templates[k]
	h := model.Hook{ID: "candidate", Version: 1, State: model.VersionDraft, Params: []model.ParamDecl{{Name: "keep", Type: "word"}}, Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "# test"}}}
	r.page.Items = []model.Hook{h}
	r.hookVersions = []model.Hook{h}
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	index := 2
	got, err := s.Candidates(t.Context(), editorActor(), CandidateRequest{Key: k, Slot: "disk", Index: &index})
	if err != nil || len(got.Items) != 1 {
		t.Fatal(got, err)
	}
	c := got.Items[0]
	if !c.Usable || c.Hook.Hook.Key() != h.Key() || len(c.Problems) != 1 || c.Problems[0].Code != "hook_ref_unapproved" || c.Problems[0].Subject.Path != "disk/2" {
		t.Fatal(c)
	}
	if !reflect.DeepEqual(r.templates[k], original) || len(r.hooks) != 0 {
		t.Fatal("候选查询改了原装配或引用表")
	}
	// 同名绑定必须保留，因此旧的文字值换成整数声明后仍要报错。
	h.Params = []model.ParamDecl{{Name: "keep", Type: "int"}}
	r.hookVersions = []model.Hook{h}
	got, err = s.Candidates(t.Context(), editorActor(), CandidateRequest{Key: k, Slot: "disk", Index: &index})
	if err != nil || len(got.Items) != 1 || got.Items[0].Usable {
		t.Fatal(got, err)
	}
	bindingProblem := false
	for _, p := range got.Items[0].Problems {
		bindingProblem = bindingProblem || (p.Code == "param_value_invalid" && p.Subject.Path == "disk/2/keep")
	}
	if !bindingProblem {
		t.Fatal(got)
	}
	h.Params = []model.ParamDecl{{Name: "keep", Type: "word"}}
	h.Impls[0].Series = []catalog.Series{"ubuntu"}
	r.hookVersions = []model.Hook{h}
	got, err = s.Candidates(t.Context(), editorActor(), CandidateRequest{Key: k, Slot: "disk", Index: &index})
	if err != nil || got.Items[0].Usable {
		t.Fatal(got, err)
	}
	found := false
	for _, p := range got.Items[0].Problems {
		if p.Subject.Path != "disk/2" {
			t.Fatal(p)
		}
		found = found || p.Code == "impl_missing_for_series"
	}
	if !found {
		t.Fatal(got)
	}
}
func Test来源差异选择最新草稿但不改派生模板(t *testing.T) {
	r, k := editorFixture()
	source := model.Template{ID: "source", Version: 1, Body: "original"}
	latest := source
	latest.Version = 3
	latest.Body = "draft"
	latest.State = model.VersionDraft
	tpl := r.templates[k]
	from := model.TemplateKey{ID: "source", Version: 1}
	tpl.DerivedFrom = &from
	r.templates[k] = tpl
	r.templates[from] = source
	r.versions = []model.Template{latest, source}
	untouched, _ := editorFixture()
	before := untouched.templates[k]
	before.DerivedFrom = &model.TemplateKey{ID: "source", Version: 1}
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	got, err := s.SourceDiff(t.Context(), editorActor(), k)
	if err != nil || !got.Changed || got.Original.Template.Version != 1 || got.Current.Template.Version != 3 || got.Current.Template.Body != "draft" {
		t.Fatal(got, err)
	}
	got.Original.Template.Body = "changed"
	got.Current.Template.Series = append(got.Current.Template.Series, "ubuntu")
	if !reflect.DeepEqual(r.templates[k], before) || r.templates[from].Body != "original" {
		t.Fatal("来源查询改变了依赖对象")
	}
	tpl.DerivedFrom = nil
	r.templates[k] = tpl
	got, err = s.SourceDiff(t.Context(), editorActor(), k)
	if err != nil || got.Original != nil || got.Current != nil || got.Changed {
		t.Fatal(got, err)
	}
}
func Test编辑插槽保留首次位置和重复诊断(t *testing.T) {
	r, k := editorFixture()
	tpl := r.templates[k]
	tpl.Body = "x {{slot:disk}}\n{{slot:other}}\n{{slot:disk}}"
	r.templates[k] = tpl
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	got, err := s.Slots(t.Context(), editorActor(), k)
	if err != nil || len(got.Slots) != 2 || got.Slots[0].Name != "disk" || got.Slots[0].Line != 1 || got.Slots[0].Col != 3 || got.Slots[1].Name != "other" || got.Slots[1].Line != 2 {
		t.Fatal(got, err)
	}
	found := false
	for _, p := range got.Problems {
		if p.Code == "slot_duplicate" {
			found = true
			if p.Subject.Line != 3 {
				t.Fatal(p)
			}
		}
	}
	if !found {
		t.Fatal(got)
	}
	tpl.Body = ""
	r.templates[k] = tpl
	got, err = s.Slots(t.Context(), editorActor(), k)
	if err != nil || got.Slots == nil || len(got.Slots) != 0 {
		t.Fatal(got, err)
	}
}
func Test候选按模板插槽序号分页依次检查(t *testing.T) {
	r, k := editorFixture()
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	zero := 0
	cases := []struct {
		req  CandidateRequest
		want error
	}{
		{CandidateRequest{Key: model.TemplateKey{ID: "missing", Version: 1}, Slot: "bad", Index: &zero, PageRequest: PageRequest{Page: -1}}, repository.ErrNotFound},
		{CandidateRequest{Key: k, Slot: "bad"}, repository.ErrInvalid},
		{CandidateRequest{Key: k, Slot: "disk", Index: &zero}, repository.ErrInvalid},
		{CandidateRequest{Key: k, Slot: "disk", PageRequest: PageRequest{Page: -1}}, repository.ErrInvalid},
		{CandidateRequest{Key: k, Slot: "disk", PageRequest: PageRequest{Page: int(^uint(0) >> 1), Limit: 500}}, repository.ErrInvalid},
	}
	for _, tc := range cases {
		_, err := s.Candidates(t.Context(), editorActor(), tc.req)
		if !errors.Is(err, tc.want) {
			t.Fatal(tc.req, err)
		}
	}
}
func Test候选两种分页分别处理身份和版本(t *testing.T) {
	r, k := editorFixture()
	one := model.Hook{ID: "one", Version: 1}
	two := one
	two.Version = 2
	r.page = repository.Page[model.Hook]{Items: []model.Hook{two}, Next: "next"}
	r.hookVersions = []model.Hook{one, two}
	s := NewEditor(EditorDeps{Templates: r, Hooks: r})
	got, err := s.Candidates(t.Context(), editorActor(), CandidateRequest{Key: k, Slot: "disk", PageRequest: PageRequest{Limit: 1}})
	if err != nil || len(got.Items) != 2 || got.Items[0].Hook.Hook.Version != 1 || got.Items[1].Hook.Hook.Version != 2 || got.Next != "next" || r.query.AllVersions {
		t.Fatal(got, err, r.query)
	}
	total := int64(2)
	r.page.Total = &total
	got, err = s.Candidates(t.Context(), editorActor(), CandidateRequest{Key: k, Slot: "disk", PageRequest: PageRequest{Page: 2, Limit: 1}})
	if err != nil || len(got.Items) != 1 || got.Items[0].Hook.Hook.Version != 2 || got.Total == nil || *got.Total != 2 || !r.query.AllVersions || r.query.Page != 2 {
		t.Fatal(got, err, r.query)
	}
}

func Test编辑辅助缺少依赖立即拒绝构造(t *testing.T) {
	r, _ := editorFixture()
	for _, d := range []EditorDeps{{}, {Templates: r}, {Hooks: r}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("缺少依赖却构造成功")
				}
			}()
			NewEditor(d)
		}()
	}
}
