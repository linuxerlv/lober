package memstore

import (
	"cmp"
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 模板与 hook 一一对应，身份、名字与状态转换的规则用 hooks.go 里共用的那几个函数；这里只写模板自己的：派生、审批时
// 连同引用的 hook 版本一起判定、按系过滤，以及保存时一起记下引用了哪些 hook 版本。

// tplVersion 是存下来的一个模板版本，连同它引用的 hook 版本（store.HookRefs）。保存模板的任何一个版本时一起写，删 hook
// 草稿时靠它查引用。
type tplVersion struct {
	t    model.Template // 名字只在身份上，这里的 Name 为空
	refs []model.HookKey
}

// CreateTemplate 见 store.Templates。派生要记来源，只能走 DeriveTemplate，所以入参不能带 DerivedFrom。
func (s *Store) CreateTemplate(_ context.Context, tpl model.Template) (model.Template, error) {
	if tpl.ID != "" || tpl.Version != 0 || tpl.State != "" || tpl.Approver != "" || tpl.Builtin || tpl.DerivedFrom != nil || tpl.Rev != 0 {
		return model.Template{}, errorf(store.ErrInvalid, "新建模板只带内容与作者：ID、版本、状态、审批人、内置标记、派生来源与 Rev 由仓储填，派生走 DeriveTemplate")
	}
	if err := checkAuthor(tpl.Author); err != nil {
		return model.Template{}, err
	}
	name, key, ps := normalizeName(tpl.Name, model.Subject{Kind: "template"}) // 还没有 ID
	if ps = append(ps, templateTextProblems(tpl, "")...); len(ps) > 0 {
		return model.Template{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	tpl = cloneTemplate(tpl)
	tpl.ID, tpl.Version, tpl.Name, tpl.State, tpl.Rev = store.NewID(), 1, name, model.VersionDraft, 1
	err := s.write(func(t *tx) error {
		if other := nameHolder(t.tpls, key, ""); other != "" {
			return &store.DuplicateError{Field: "name", Existing: other}
		}
		t.tpls[tpl.ID] = ident{name: name, key: key, last: 1}
		t.putTemplate(tpl)
		return nil
	})
	if err != nil {
		return model.Template{}, err
	}
	return cloneTemplate(tpl), nil
}

// SaveTemplateDraft 见 store.Templates。规则同 SaveHookDraft；派生来源只由派生写，保存时改了也报 ErrInvalid。
func (s *Store) SaveTemplateDraft(_ context.Context, tpl model.Template) (model.Template, error) {
	if err := checkKey(tpl.ID, tpl.Version); err != nil {
		return model.Template{}, err
	}
	if ps := templateTextProblems(tpl, versionID(tpl.ID, tpl.Version)); len(ps) > 0 {
		return model.Template{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	tpl = cloneTemplate(tpl)
	err := s.write(func(t *tx) error {
		old, err := t.templateAt(tplKey(tpl), tpl.Rev)
		if err != nil {
			return err
		}
		what := "模板 " + versionID(tpl.ID, tpl.Version)
		if err := checkDraft(what, tplMeta(old)); err != nil {
			return err
		}
		if err := checkSameMeta(what, tplMeta(old), tplMeta(tpl)); err != nil {
			return err
		}
		if !reflect.DeepEqual(tpl.DerivedFrom, old.DerivedFrom) {
			return errorf(store.ErrInvalid, "%s 的派生来源只由派生写，保存时要原样带回", what)
		}
		tpl.Rev++
		t.putTemplate(tpl)
		return nil
	})
	if err != nil {
		return model.Template{}, err
	}
	return cloneTemplate(tpl), nil
}

// NewTemplateVersion 见 store.Templates。规则同 NewHookVersion；派生来源跟着内容照抄：来源出了新版本时，派生模板的每个
// 版本都要能比出差异（C24）。
func (s *Store) NewTemplateVersion(_ context.Context, id string, from int, author string) (model.Template, error) {
	if err := checkKey(id, from); err != nil {
		return model.Template{}, err
	}
	if err := checkAuthor(author); err != nil {
		return model.Template{}, err
	}
	var out model.Template
	err := s.write(func(t *tx) error {
		src, next, err := newVersionSource("模板", t.tpls, id, t.templateVersions(id), from, tplInfo)
		if err != nil {
			return err
		}
		out = cloneTemplate(src)
		out.Version, out.State, out.Author, out.Approver, out.Rev = next, model.VersionDraft, author, "", 1
		t.putTemplate(out)
		return nil
	})
	if err != nil {
		return model.Template{}, err
	}
	return cloneTemplate(out), nil
}

// DeriveTemplate 见 store.Templates。复制的是来源版本的族、系、正文、参数、取值来源与装配表；名字、作者与派生来源是
// 新身份自己的。
func (s *Store) DeriveTemplate(_ context.Context, from model.TemplateKey, name, author string) (model.Template, error) {
	if err := checkKey(from.ID, from.Version); err != nil {
		return model.Template{}, err
	}
	if err := checkAuthor(author); err != nil {
		return model.Template{}, err
	}
	display, key, ps := normalizeName(name, model.Subject{Kind: "template"}) // 新身份还没有 ID
	if len(ps) > 0 {
		return model.Template{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	var out model.Template
	err := s.write(func(t *tx) error {
		src, ok := t.template(from)
		if !ok {
			return templateNotFound(from)
		}
		if other := nameHolder(t.tpls, key, ""); other != "" {
			return &store.DuplicateError{Field: "name", Existing: other}
		}
		out = cloneTemplate(model.Template{
			ID: store.NewID(), Version: 1, Name: display, State: model.VersionDraft, Author: author, DerivedFrom: &from,
			Family: src.Family, Series: src.Series, Body: src.Body, Params: src.Params, Bindings: src.Bindings, Assembly: src.Assembly,
			Rev: 1,
		})
		t.tpls[out.ID] = ident{name: display, key: key, last: 1}
		t.putTemplate(out)
		return nil
	})
	if err != nil {
		return model.Template{}, err
	}
	return cloneTemplate(out), nil
}

// SubmitTemplate 见 store.Templates。
func (s *Store) SubmitTemplate(_ context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(k, rev, a, model.VersionDraft, model.VersionReview, nil)
}

// ApproveTemplate 见 store.Templates。重跑的判定看的是同一个事务里读到的模板版本与它引用的 hook 版本：内存实现的写操作
// 都串行，读到的就是最新的；引用的版本不存在的，不放进判定的入参，由判定报 hook_ref_missing。
func (s *Store) ApproveTemplate(_ context.Context, k model.TemplateKey, rev int64, approver string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := checkApprover(approver); err != nil {
		return err
	}
	return s.templateStep(k, rev, a, model.VersionReview, model.VersionApproved, func(t *tx, tpl *model.Template) error {
		hooks := map[model.HookKey]model.Hook{}
		for _, hk := range store.HookRefs(*tpl) {
			if h, ok := t.hook(hk); ok {
				hooks[hk] = h
			}
		}
		if err := store.ApproveTemplateProblems(*tpl, hooks, approver); err != nil {
			return err
		}
		tpl.Approver = approver
		return nil
	})
}

// RejectTemplate 见 store.Templates。
func (s *Store) RejectTemplate(_ context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(k, rev, a, model.VersionReview, model.VersionDraft, nil)
}

// DeprecateTemplate 见 store.Templates。内置的版本也不能废弃：内置内容只读（C25）。
func (s *Store) DeprecateTemplate(_ context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(k, rev, a, model.VersionApproved, model.VersionDeprecated, nil)
}

// templateStep 同 hookStep，转的是模板版本 k；apply 还拿得到这次写入的暂存，审批要从里面取引用的 hook 版本。
func (s *Store) templateStep(k model.TemplateKey, rev int64, a store.Audit, from, to model.VersionState, apply func(t *tx, tpl *model.Template) error) error {
	if err := checkKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		tpl, err := t.templateAt(k, rev)
		if err != nil {
			return err
		}
		target := versionID(k.ID, k.Version)
		if err := checkStep("模板 "+target, tplMeta(tpl), from); err != nil {
			return err
		}
		if apply != nil {
			if err := apply(t, &tpl); err != nil {
				return err
			}
		}
		tpl.State = to
		tpl.Rev++
		t.putTemplate(tpl)
		return t.addAudit(a, "template", target)
	})
}

// RenameTemplate 见 store.Templates。
func (s *Store) RenameTemplate(_ context.Context, id, name string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if !model.ValidID(id) {
		return badID(id)
	}
	display, key, ps := normalizeName(name, model.Subject{Kind: "template", ID: id})
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return s.write(func(t *tx) error {
		if err := renameIdent(t.tpls, "模板", id, display, key); err != nil {
			return err
		}
		return t.addAudit(a, "template", id)
	})
}

// DeleteTemplateDraft 见 store.Templates。任务只引用已审批的版本，没有挡着删模板草稿的引用；派生模板的 DerivedFrom 与
// 审计仍可能记着这个草稿的键，它的版本号不再发（allocVersion），这些键不会对到别的内容上。它记着的 hook 引用随它一起
// 删掉。
func (s *Store) DeleteTemplateDraft(_ context.Context, k model.TemplateKey, rev int64) error {
	if err := checkKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		tpl, err := t.templateAt(k, rev)
		if err != nil {
			return err
		}
		if err := checkDraft("模板 "+versionID(k.ID, k.Version), tplMeta(tpl)); err != nil {
			return err
		}
		delete(t.tplVers, k)
		if len(t.templateVersions(k.ID)) == 0 { // 删的是最后一个版本：身份一起删掉，名字随之空出来
			delete(t.tpls, k.ID)
		}
		return nil
	})
}

// GetTemplate 见 store.Templates。
func (s *Store) GetTemplate(_ context.Context, k model.TemplateKey) (model.Template, error) {
	if err := checkKey(k.ID, k.Version); err != nil {
		return model.Template{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tpl, ok := s.d.template(k)
	if !ok {
		return model.Template{}, templateNotFound(k)
	}
	return cloneTemplate(tpl), nil
}

// ListTemplateVersions 见 store.Templates。身份不存在报 ErrNotFound。
func (s *Store) ListTemplateVersions(_ context.Context, id string) ([]model.Template, error) {
	if !model.ValidID(id) {
		return nil, badID(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.d.tpls[id]; !ok {
		return nil, errorf(store.ErrNotFound, "模板 %s 不存在", id)
	}
	vs := s.d.templateVersions(id)
	for i := range vs {
		vs[i] = cloneTemplate(vs[i])
	}
	return vs, nil
}

// ListTemplates 见 store.Templates。按系过滤看的是选出的那一项，与状态的过滤一样。
func (s *Store) ListTemplates(_ context.Context, q store.TemplateQuery) (store.Page[model.Template], error) {
	limit, after, err := checkIdentQuery(q.Cursor, q.Limit, "templates")
	if err != nil {
		return store.Page[model.Template]{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pick := func(id string) (model.Template, bool) {
		vs := s.d.templateVersions(id)
		if q.ApprovedOnly {
			return model.CurrentApproved(vs)
		}
		return vs[len(vs)-1], true // 身份至少有一个版本：删掉最后一个版本时身份一起删掉
	}
	keep := func(tpl model.Template) bool {
		return (len(q.States) == 0 || slices.Contains(q.States, tpl.State)) && (q.Series == "" || slices.Contains(tpl.Series, q.Series))
	}
	return identPage(s.d.tpls, q.NamePrefix, after, limit, "templates", pick, keep, cloneTemplate), nil
}

// EnsureBuiltinTemplate 见 store.Templates，规则同 EnsureBuiltinHook。内置模板不是派生来的，带了派生来源报 ErrInvalid。
func (s *Store) EnsureBuiltinTemplate(_ context.Context, tpl model.Template) error {
	id := versionID(tpl.ID, tpl.Version)
	key, ps, err := checkBuiltin(tpl.ID, tpl.Version, tplMeta(tpl), model.Subject{Kind: "template", ID: id})
	if err != nil {
		return err
	}
	if tpl.DerivedFrom != nil {
		return errorf(store.ErrInvalid, "内置模板 %s 不是派生来的，不能带派生来源", id)
	}
	if ps = append(ps, templateTextProblems(tpl, id)...); len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	tpl = cloneTemplate(tpl)
	tpl.Rev = 1
	return s.write(func(t *tx) error {
		if old, ok := t.template(tplKey(tpl)); ok {
			if !reflect.DeepEqual(old, tpl) {
				return errorf(store.ErrBuiltinMismatch, "内置的模板 %s 与库里的同一版本不同：改了内置内容就要升版本号", id)
			}
			return nil
		}
		if err := ensureBuiltinIdent(t.tpls, "模板", tpl.ID, tpl.Version, tpl.Name, key); err != nil {
			return err
		}
		t.putTemplate(tpl)
		return nil
	})
}

// 下面是模板版本表的读写，规则同 hook 的那几个函数。

func (d *data) template(k model.TemplateKey) (model.Template, bool) {
	v, ok := d.tplVers[k]
	if ok {
		v.t.Name = d.tpls[k.ID].name
	}
	return v.t, ok
}

func (d *data) templateAt(k model.TemplateKey, rev int64) (model.Template, error) {
	tpl, ok := d.template(k)
	switch {
	case !ok:
		return model.Template{}, templateNotFound(k)
	case tpl.Rev != rev:
		return model.Template{}, staleRev("模板 "+versionID(k.ID, k.Version), tpl.Rev, rev)
	}
	return tpl, nil
}

// putTemplate 存下版本 tpl（不存名字），连同它引用的 hook 版本。
func (d *data) putTemplate(tpl model.Template) {
	refs := store.HookRefs(tpl)
	tpl.Name = ""
	d.tplVers[tplKey(tpl)] = tplVersion{t: tpl, refs: refs}
}

func (d *data) templateVersions(id string) []model.Template {
	var vs []model.Template
	for k := range d.tplVers {
		if k.ID == id {
			tpl, _ := d.template(k)
			vs = append(vs, tpl)
		}
	}
	slices.SortFunc(vs, func(a, b model.Template) int { return cmp.Compare(a.Version, b.Version) })
	return vs
}

func tplKey(tpl model.Template) model.TemplateKey {
	return model.TemplateKey{ID: tpl.ID, Version: tpl.Version}
}

func compareTemplateKeys(a, b model.TemplateKey) int {
	return cmp.Or(strings.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
}

func templateNotFound(k model.TemplateKey) error {
	return errorf(store.ErrNotFound, "模板 %s 不存在", versionID(k.ID, k.Version))
}

func tplMeta(tpl model.Template) meta {
	return meta{name: tpl.Name, state: tpl.State, author: tpl.Author, approver: tpl.Approver, builtin: tpl.Builtin}
}

func tplInfo(tpl model.Template) (int, meta) { return tpl.Version, tplMeta(tpl) }

// templateTextProblems 是模板版本 tpl 里存不下的文本，规则同 hookTextProblems：正文、族与系落在模板整体上；参数声明按
// 位置；模板参数的取值来源按参数名，Path 为 tpl/<参数名>；装配表按插槽名，插槽名本身落在插槽上，实例按顺序落在
// <插槽名>/<序号>，实例的取值来源落在 <插槽名>/<序号>/<参数名>。map 按键排过序，同一份模板每次报得一样。
func templateTextProblems(tpl model.Template, id string) []model.Problem {
	whole := model.Subject{Kind: "template", ID: id}
	var ps []model.Problem
	if !storable(tpl.Body, string(tpl.Family)) || !storableAll(tpl.Series) {
		ps = append(ps, notStorable(whole))
	}
	ps = append(ps, declTextProblems(tpl.Params, whole)...)
	for _, name := range slices.Sorted(maps.Keys(tpl.Bindings)) {
		if !bindingStorable(name, tpl.Bindings[name]) {
			s := whole
			s.Path = model.TemplateFieldKey(name)
			ps = append(ps, notStorable(s))
		}
	}
	for _, slot := range slices.Sorted(maps.Keys(tpl.Assembly)) {
		at := func(path string) model.Subject { return model.Subject{Kind: "slot", ID: id, Path: path} }
		if !model.StorableText(slot) {
			ps = append(ps, notStorable(at(slot)))
		}
		for i, in := range tpl.Assembly[slot] {
			if !model.StorableText(in.HookID) {
				ps = append(ps, notStorable(at(model.InstancePath(slot, i+1))))
			}
			for _, name := range slices.Sorted(maps.Keys(in.Bindings)) {
				if !bindingStorable(name, in.Bindings[name]) {
					ps = append(ps, notStorable(at(model.InstanceFieldKey(slot, i+1, name))))
				}
			}
		}
	}
	return ps
}

func bindingStorable(name string, b model.Binding) bool {
	return storable(name, string(b.Source), b.Value, b.Attr)
}

// cloneTemplate 深拷贝一个模板版本：派生来源、系、参数、取值来源与装配表（实例里的指针与取值来源）各拷一份。nil 拷出来
// 仍是 nil，空的仍是空的：写进去什么就读出什么。
func cloneTemplate(tpl model.Template) model.Template {
	tpl.DerivedFrom = clonePtr(tpl.DerivedFrom)
	tpl.Series = slices.Clone(tpl.Series)
	tpl.Params = cloneDecls(tpl.Params)
	tpl.Bindings = maps.Clone(tpl.Bindings)
	if tpl.Assembly != nil {
		assembly := make(map[string][]model.Instance, len(tpl.Assembly))
		for slot, insts := range tpl.Assembly {
			if insts != nil {
				c := make([]model.Instance, len(insts))
				for i, in := range insts {
					in.TimeoutSec, in.Required, in.Bindings = clonePtr(in.TimeoutSec), clonePtr(in.Required), maps.Clone(in.Bindings)
					c[i] = in
				}
				insts = c
			}
			assembly[slot] = insts
		}
		tpl.Assembly = assembly
	}
	return tpl
}
