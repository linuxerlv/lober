package memstore

import (
	"cmp"
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// CreateHook 见 store.Hooks。
func (s *Store) CreateHook(_ context.Context, h model.Hook) (model.Hook, error) {
	name, key, err := store.CheckNewHook(h)
	if err != nil {
		return model.Hook{}, err
	}
	h = cloneHook(h)
	h.ID, h.Version, h.Name, h.State, h.Rev = store.NewID(), 1, name, model.VersionDraft, 1
	err = s.write(func(t *tx) error {
		if other := nameHolder(t.hooks, key, ""); other != "" {
			return &store.DuplicateError{Field: "name", Existing: other}
		}
		t.hooks[h.ID] = ident{name: name, key: key, last: 1}
		t.putHook(h)
		return nil
	})
	if err != nil {
		return model.Hook{}, err
	}
	return cloneHook(h), nil
}

// SaveHookDraft 见 store.Hooks。保存替换的是内容（说明、参数、超时、是否必需与实现）；名字、状态、作者、审批人与内置
// 标记由仓储管，带来的与库里的不同报 ErrInvalid（store.CheckSameMeta）。
func (s *Store) SaveHookDraft(_ context.Context, h model.Hook) (model.Hook, error) {
	if err := store.CheckHookDraft(h); err != nil {
		return model.Hook{}, err
	}
	h = cloneHook(h)
	err := s.write(func(t *tx) error {
		old, err := t.hookAt(h.Key(), h.Rev)
		if err != nil {
			return err
		}
		what := "hook " + store.VersionID(h.ID, h.Version)
		if err := store.CheckDraft(what, store.HookMeta(old)); err != nil {
			return err
		}
		if err := store.CheckSameMeta(what, store.HookMeta(old), store.HookMeta(h)); err != nil {
			return err
		}
		h.Rev++
		t.putHook(h)
		return nil
	})
	if err != nil {
		return model.Hook{}, err
	}
	return cloneHook(h), nil
}

// NewHookVersion 见 store.Hooks。新版本的号由 allocVersion 分配：这个身份分配过的最大版本号加 1，删掉的草稿的号不再发。
func (s *Store) NewHookVersion(_ context.Context, id string, from int, author string) (model.Hook, error) {
	if err := store.CheckVersionKey(id, from); err != nil {
		return model.Hook{}, err
	}
	if err := store.CheckAuthor(author); err != nil {
		return model.Hook{}, err
	}
	var out model.Hook
	err := s.write(func(t *tx) error {
		src, next, err := newVersionSource("hook", t.hooks, id, t.hookVersions(id), from, hookInfo)
		if err != nil {
			return err
		}
		out = cloneHook(src) // 内容照抄来源；名字随身份
		out.Version, out.State, out.Author, out.Approver, out.Rev = next, model.VersionDraft, author, "", 1
		t.putHook(out)
		return nil
	})
	if err != nil {
		return model.Hook{}, err
	}
	return cloneHook(out), nil
}

// SubmitHook 见 store.Hooks。
func (s *Store) SubmitHook(_ context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(k, rev, a, model.VersionDraft, model.VersionReview, nil)
}

// ApproveHook 见 store.Hooks。重跑的判定看的是锁住之后读到的版本（store.ApproveHookProblems）。
func (s *Store) ApproveHook(_ context.Context, k model.HookKey, rev int64, approver string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckApprover(approver); err != nil {
		return err
	}
	return s.hookStep(k, rev, a, model.VersionReview, model.VersionApproved, func(h *model.Hook) error {
		if err := store.ApproveHookProblems(*h, approver); err != nil {
			return err
		}
		h.Approver = approver
		return nil
	})
}

// RejectHook 见 store.Hooks。
func (s *Store) RejectHook(_ context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(k, rev, a, model.VersionReview, model.VersionDraft, nil)
}

// DeprecateHook 见 store.Hooks。内置的版本也不能废弃：内置内容只读（C25）。
func (s *Store) DeprecateHook(_ context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(k, rev, a, model.VersionApproved, model.VersionDeprecated, nil)
}

// hookStep 把版本 k 从 from 转到 to，审计已由调用方查过。依次查键、Rev、内置与状态；apply 非空时在转换之前对读到的版本
// 跑它，返回错误就什么都不写（审批的重跑判定）。审计写在转换之后，写失败时整次丢掉。
func (s *Store) hookStep(k model.HookKey, rev int64, a store.Audit, from, to model.VersionState, apply func(h *model.Hook) error) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		h, err := t.hookAt(k, rev)
		if err != nil {
			return err
		}
		target := store.VersionID(k.ID, k.Version)
		if err := store.CheckStep("hook "+target, store.HookMeta(h), from); err != nil {
			return err
		}
		if apply != nil {
			if err := apply(&h); err != nil {
				return err
			}
		}
		h.State = to
		h.Rev++
		t.putHook(h)
		return t.addAudit(a, "hook", target)
	})
}

// RenameHook 见 store.Hooks。名字只在身份上，版本那几条记录没改，它们的 Rev 不变。
func (s *Store) RenameHook(_ context.Context, id, name string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	display, key, ps := store.NormalizeName(name, model.Subject{Kind: "hook", ID: id})
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return s.write(func(t *tx) error {
		if err := renameIdent(t.hooks, "hook", id, display, key); err != nil {
			return err
		}
		return t.addAudit(a, "hook", id)
	})
}

// DeleteHookDraft 见 store.Hooks。
func (s *Store) DeleteHookDraft(_ context.Context, k model.HookKey, rev int64) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		h, err := t.hookAt(k, rev)
		if err != nil {
			return err
		}
		what := "hook " + store.VersionID(k.ID, k.Version)
		if err := store.CheckDraft(what, store.HookMeta(h)); err != nil {
			return err
		}
		if by, ok := t.hookReferencedBy(k); ok {
			return errorf(store.ErrReferenced, "%s 被模板 %s 引用：先改掉那个模板的装配表，或删掉那个模板草稿", what, store.VersionID(by.ID, by.Version))
		}
		delete(t.hookVers, k)
		if len(t.hookVersions(k.ID)) == 0 { // 删的是最后一个版本：身份一起删掉，名字随之空出来
			delete(t.hooks, k.ID)
		}
		return nil
	})
}

// GetHook 见 store.Hooks。
func (s *Store) GetHook(_ context.Context, k model.HookKey) (model.Hook, error) {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return model.Hook{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.d.hook(k)
	if !ok {
		return model.Hook{}, hookNotFound(k)
	}
	return cloneHook(h), nil
}

// GetHooks 见 store.Hooks。键先全部查过格式，有一个不合就报 ErrInvalid，不当成找不到。
func (s *Store) GetHooks(_ context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error) {
	for _, k := range ks {
		if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[model.HookKey]model.Hook, len(ks))
	for _, k := range ks {
		if h, ok := s.d.hook(k); ok {
			out[k] = cloneHook(h)
		}
	}
	return out, nil
}

// ListHookVersions 见 store.Hooks。身份不存在报 ErrNotFound。
func (s *Store) ListHookVersions(_ context.Context, id string) ([]model.Hook, error) {
	if err := store.CheckID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.d.hooks[id]; !ok {
		return nil, errorf(store.ErrNotFound, "hook %s 不存在", id)
	}
	vs := s.d.hookVersions(id)
	for i := range vs {
		vs[i] = cloneHook(vs[i])
	}
	return vs, nil
}

// ListHooks 见 store.Hooks，排序、前缀与游标见 identPage。
func (s *Store) ListHooks(_ context.Context, q store.HookQuery) (store.Page[model.Hook], error) {
	limit, after, err := checkIdentQuery(q.Cursor, q.Limit, "hooks")
	if err != nil {
		return store.Page[model.Hook]{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pick := func(id string) (model.Hook, bool) {
		vs := s.d.hookVersions(id)
		if q.ApprovedOnly {
			return currentApproved(vs, hookInfo)
		}
		return vs[len(vs)-1], true // 身份至少有一个版本：删掉最后一个版本时身份一起删掉
	}
	keep := func(h model.Hook) bool { return len(q.States) == 0 || slices.Contains(q.States, h.State) }
	return identPage(s.d.hooks, q.NamePrefix, after, limit, "hooks", pick, keep, cloneHook), nil
}

// EnsureBuiltinHook 见 store.Hooks。比的是版本的全部字段，名字按身份上的，Rev 不比：内置的版本写入之后不会再变。
func (s *Store) EnsureBuiltinHook(_ context.Context, h model.Hook) error {
	key, err := store.CheckBuiltinHook(h)
	if err != nil {
		return err
	}
	id := store.VersionID(h.ID, h.Version)
	h = cloneHook(h)
	h.Rev = 1
	return s.write(func(t *tx) error {
		if old, ok := t.hook(h.Key()); ok {
			if !reflect.DeepEqual(old, h) {
				return errorf(store.ErrBuiltinMismatch, "内置的 hook %s 与库里的同一版本不同：改了内置内容就要升版本号", id)
			}
			return nil
		}
		if err := ensureBuiltinIdent(t.hooks, "hook", h.ID, h.Version, h.Name, key); err != nil {
			return err
		}
		t.putHook(h)
		return nil
	})
}

// 下面是 hook 版本表的读写。名字只存在身份上，版本里不存，读出时再填：改名不必改每个版本，返回的每个版本 Name 都是
// 身份现在的名字。

// hook 取版本 k，Name 填成身份现在的名字。
func (d *data) hook(k model.HookKey) (model.Hook, bool) {
	h, ok := d.hookVers[k]
	if ok {
		h.Name = d.hooks[k.ID].name
	}
	return h, ok
}

// hookAt 取 Rev 为 rev 的版本 k：不存在报 ErrNotFound，Rev 对不上报 ErrConflict。
func (d *data) hookAt(k model.HookKey, rev int64) (model.Hook, error) {
	h, ok := d.hook(k)
	switch {
	case !ok:
		return model.Hook{}, hookNotFound(k)
	case h.Rev != rev:
		return model.Hook{}, staleRev("hook "+store.VersionID(k.ID, k.Version), h.Rev, rev)
	}
	return h, nil
}

// putHook 存下版本 h（不存名字）。h 之后不能再被改：表里的记录只换不改（见包说明）。
func (d *data) putHook(h model.Hook) {
	h.Name = ""
	d.hookVers[h.Key()] = h
}

// hookVersions 是身份 id 的全部版本，按版本号升序，Name 已填。
func (d *data) hookVersions(id string) []model.Hook {
	var vs []model.Hook
	for k := range d.hookVers {
		if k.ID == id {
			h, _ := d.hook(k)
			vs = append(vs, h)
		}
	}
	slices.SortFunc(vs, func(a, b model.Hook) int { return cmp.Compare(a.Version, b.Version) })
	return vs
}

// hookReferencedBy 报告 hook 版本 k 是否被某个模板版本引用（包括草稿），是的话给出其中按键排在最前的一个，报错时
// 说法每次都一样。
func (d *data) hookReferencedBy(k model.HookKey) (model.TemplateKey, bool) {
	var by []model.TemplateKey
	for tk, v := range d.tplVers {
		if slices.Contains(v.refs, k) {
			by = append(by, tk)
		}
	}
	if len(by) == 0 {
		return model.TemplateKey{}, false
	}
	return slices.MinFunc(by, compareTemplateKeys), true
}

func hookNotFound(k model.HookKey) error {
	return errorf(store.ErrNotFound, "hook %s 不存在", store.VersionID(k.ID, k.Version))
}

func hookInfo(h model.Hook) (int, store.VersionMeta) { return h.Version, store.HookMeta(h) }

// cloneHook 深拷贝一个 hook 版本。nil 拷出来仍是 nil：写进去什么就读出什么。
func cloneHook(h model.Hook) model.Hook {
	h.Params = cloneDecls(h.Params)
	if h.Impls != nil {
		impls := make([]model.Impl, len(h.Impls))
		for i, impl := range h.Impls {
			impl.Series, impl.Timings = slices.Clone(impl.Series), slices.Clone(impl.Timings)
			impls[i] = impl
		}
		h.Impls = impls
	}
	return h
}

// 下面是 hook 与模板共用的部分：两边的身份、版本与名字规则一一对应（store.Templates 的说明）。入参与版本状态的检查是两份
// 实现共用的，在 store 包（check_authoring.go）；这里只有内存里的身份表怎么读写。

// ident 是 hook 或模板的一个身份。名字放在身份上，各版本共用（计划「已定的决定」），唯一约束按名字的 key。
type ident struct {
	name    string // 规范化之后的名字（store.NormalizeName 的 display）
	key     string // 名字的 key：display 的 ASCII 小写
	builtin bool   // 内置的身份只读（C25）
	last    int    // 分配过的最大版本号，只增不减（allocVersion）
}

// nameHolder 返回 idents 里除 self 之外占着名字 key 的身份 ID，没有时返回空串。
func nameHolder(idents map[string]ident, key, self string) string {
	for id, x := range idents {
		if id != self && x.key == key {
			return id
		}
	}
	return ""
}

// renameIdent 给身份 id 改名：不存在报 ErrNotFound，内置的报 ErrImmutable，名字被别的身份占了报 DuplicateError。只改
// 大小写时 key 还是自己的，不算撞上。只换名字，分配过的版本号照旧。
func renameIdent(idents map[string]ident, kind, id, name, key string) error {
	cur, ok := idents[id]
	switch {
	case !ok:
		return errorf(store.ErrNotFound, "%s %s 不存在", kind, id)
	case cur.builtin:
		return errorf(store.ErrImmutable, "%s %s 是内置的，不能改名", kind, id)
	}
	if other := nameHolder(idents, key, id); other != "" {
		return &store.DuplicateError{Field: "name", Existing: other}
	}
	cur.name, cur.key = name, key
	idents[id] = cur
	return nil
}

// newVersionSource 是开新版本之前的检查：身份 id 存在且不是内置的（C25），来源版本 from 存在，身份没有未审批的版本
// （每个身份同时最多一个草稿或待审批的版本）。vs 是身份的全部版本，按版本号升序；info 取一个版本的版本号与由仓储管的
// 字段。都过了才分配新版本的号（allocVersion），返回来源版本与这个号。
func newVersionSource[V any](kind string, idents map[string]ident, id string, vs []V, from int, info func(V) (int, store.VersionMeta)) (V, int, error) {
	var src V
	switch x, ok := idents[id]; {
	case !ok:
		return src, 0, errorf(store.ErrNotFound, "%s %s 不存在", kind, id)
	case x.builtin:
		return src, 0, errorf(store.ErrImmutable, "%s %s 是内置的，不能开新版本", kind, id)
	}
	found := false
	for _, v := range vs {
		if n, _ := info(v); n == from {
			src, found = v, true
		}
	}
	if !found {
		return src, 0, errorf(store.ErrNotFound, "%s %s 没有版本 %d", kind, id, from)
	}
	for _, v := range vs {
		if n, m := info(v); m.State == model.VersionDraft || m.State == model.VersionReview {
			return src, 0, errorf(store.ErrConflict, "%s %s 已有未审批的版本 %d：先审批、驳回或删掉它", kind, id, n)
		}
	}
	return src, allocVersion(idents, id), nil
}

// allocVersion 给身份 id 分配下一个版本号：它分配过的最大版本号加 1，并记回身份上。版本号只增不减，删掉的草稿的号
// 不再发：那个草稿的键还可能记在别处（调用方手里的旧副本、派生模板的 DerivedFrom、审计的对象），号再发出去，这些就
// 会对到另一份内容上，带着旧副本保存时连 Rev 都对得上。
func allocVersion(idents map[string]ident, id string) int {
	x := idents[id]
	x.last++
	idents[id] = x
	return x.last
}

// currentApproved 从一个身份的版本 vs 里取当前的已审批版本，没有就返回 false。规则借 model.CurrentApproved：只拿版本号
// 与状态问它，hook 与模板用的是同一份规则（store.HookQuery.ApprovedOnly）。
func currentApproved[V any](vs []V, info func(V) (int, store.VersionMeta)) (V, bool) {
	ts := make([]model.Template, len(vs))
	for i, v := range vs {
		n, m := info(v)
		ts[i] = model.Template{Version: n, State: m.State}
	}
	if cur, ok := model.CurrentApproved(ts); ok {
		for _, v := range vs {
			if n, _ := info(v); n == cur.Version {
				return v, true
			}
		}
	}
	var none V
	return none, false
}

// cursorKey 是 hook 与模板列表的排序键：名字的 key，再是 ID。
type cursorKey struct{ key, id string }

func (a cursorKey) compare(b cursorKey) int {
	return cmp.Or(strings.Compare(a.key, b.key), strings.Compare(a.id, b.id))
}

// checkIdentQuery 查列表的分页入参，解出游标：上一页最后一项的排序键，没有游标时为零值，排在任何一项之前。
func checkIdentQuery(cursor string, limit int, list string) (int, cursorKey, error) {
	n, err := store.CheckQuery(cursor, limit)
	if err != nil {
		return 0, cursorKey{}, err
	}
	var after cursorKey
	if cursor != "" {
		if err := store.DecodeCursor(cursor, list, &after.key, &after.id); err != nil {
			return 0, cursorKey{}, err
		}
	}
	return n, after, nil
}

// identPage 是 ListHooks 与 ListTemplates 共用的一页：名字的 key 以 prefix 开头的身份，各由 pick 选出一项，选不出或 keep
// 不要的跳过；按名字的 key、再按 ID 的字节序排，从 after 之后取 limit 条。prefix 按 store.NameKeyPrefix 转了再比：只转
// ASCII 字母，与「大小写不同也算同名」一致。
func identPage[V any](idents map[string]ident, prefix string, after cursorKey, limit int, list string,
	pick func(id string) (V, bool), keep func(V) bool, clone func(V) V) store.Page[V] {
	prefix = store.NameKeyPrefix(prefix)
	type item struct {
		at cursorKey
		v  V
	}
	var items []item
	for id, x := range idents {
		at := cursorKey{x.key, id}
		if !strings.HasPrefix(x.key, prefix) || at.compare(after) <= 0 {
			continue
		}
		if v, ok := pick(id); ok && keep(v) {
			items = append(items, item{at, v})
		}
	}
	slices.SortFunc(items, func(a, b item) int { return a.at.compare(b.at) })
	p := page(items, limit, func(it item) item { return it }, func(it item) string { return store.EncodeCursor(list, it.at.key, it.at.id) })
	out := store.Page[V]{Items: make([]V, len(p.Items)), Next: p.Next}
	for i, it := range p.Items {
		out.Items[i] = clone(it.v)
	}
	return out
}

// ensureBuiltinIdent 确认内置版本 version 所属的身份：不存在就建一个内置的身份，名字被别的身份占了报 DuplicateError；
// 已存在的要是内置的、名字也相同，否则程序自带的内容与库里的对不上（ErrBuiltinMismatch）：名字在身份上，各版本共用。
// 身份上分配过的最大版本号随装载的版本更新。
func ensureBuiltinIdent(idents map[string]ident, kind, id string, version int, name, key string) error {
	if cur, ok := idents[id]; ok {
		if !cur.builtin || cur.name != name {
			return errorf(store.ErrBuiltinMismatch, "%s %s 在库里名为 %q（内置：%v），程序自带的是内置的 %q", kind, id, cur.name, cur.builtin, name)
		}
		cur.last = max(cur.last, version)
		idents[id] = cur
		return nil
	}
	if other := nameHolder(idents, key, ""); other != "" {
		return &store.DuplicateError{Field: "name", Existing: other}
	}
	idents[id] = ident{name: name, key: key, builtin: true, last: version}
	return nil
}

// cloneDecls 深拷贝参数声明：选项与上下限各拷一份。nil 拷出来仍是 nil。
func cloneDecls(ds []model.ParamDecl) []model.ParamDecl {
	if ds == nil {
		return nil
	}
	out := make([]model.ParamDecl, len(ds))
	for i, d := range ds {
		d.Options, d.Min, d.Max = slices.Clone(d.Options), clonePtr(d.Min), clonePtr(d.Max)
		out[i] = d
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
