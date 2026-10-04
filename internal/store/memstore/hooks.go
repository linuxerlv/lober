package memstore

import (
	"cmp"
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// CreateHook 见 store.Hooks。
func (s *Store) CreateHook(_ context.Context, h model.Hook) (model.Hook, error) {
	if h.ID != "" || h.Version != 0 || h.State != "" || h.Approver != "" || h.Builtin || h.Rev != 0 {
		return model.Hook{}, errorf(store.ErrInvalid, "新建 hook 只带内容与作者：ID、版本、状态、审批人、内置标记与 Rev 由仓储填")
	}
	if err := checkAuthor(h.Author); err != nil {
		return model.Hook{}, err
	}
	name, key, ps := normalizeName(h.Name, model.Subject{Kind: "hook"}) // 还没有 ID
	if ps = append(ps, hookTextProblems(h, "")...); len(ps) > 0 {
		return model.Hook{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	h = cloneHook(h)
	h.ID, h.Version, h.Name, h.State, h.Rev = store.NewID(), 1, name, model.VersionDraft, 1
	err := s.write(func(t *tx) error {
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
// 标记由仓储管，带来的与库里的不同报 ErrInvalid（checkSameMeta）。
func (s *Store) SaveHookDraft(_ context.Context, h model.Hook) (model.Hook, error) {
	if err := checkKey(h.ID, h.Version); err != nil {
		return model.Hook{}, err
	}
	if ps := hookTextProblems(h, versionID(h.ID, h.Version)); len(ps) > 0 {
		return model.Hook{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	h = cloneHook(h)
	err := s.write(func(t *tx) error {
		old, err := t.hookAt(h.Key(), h.Rev)
		if err != nil {
			return err
		}
		what := "hook " + versionID(h.ID, h.Version)
		if err := checkDraft(what, hookMeta(old)); err != nil {
			return err
		}
		if err := checkSameMeta(what, hookMeta(old), hookMeta(h)); err != nil {
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
	if err := checkKey(id, from); err != nil {
		return model.Hook{}, err
	}
	if err := checkAuthor(author); err != nil {
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
	if err := checkApprover(approver); err != nil {
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
	if err := checkKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		h, err := t.hookAt(k, rev)
		if err != nil {
			return err
		}
		target := versionID(k.ID, k.Version)
		if err := checkStep("hook "+target, hookMeta(h), from); err != nil {
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
	if !model.ValidID(id) {
		return badID(id)
	}
	display, key, ps := normalizeName(name, model.Subject{Kind: "hook", ID: id})
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
	if err := checkKey(k.ID, k.Version); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		h, err := t.hookAt(k, rev)
		if err != nil {
			return err
		}
		what := "hook " + versionID(k.ID, k.Version)
		if err := checkDraft(what, hookMeta(h)); err != nil {
			return err
		}
		if by, ok := t.hookReferencedBy(k); ok {
			return errorf(store.ErrReferenced, "%s 被模板 %s 引用：先改掉那个模板的装配表，或删掉那个模板草稿", what, versionID(by.ID, by.Version))
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
	if err := checkKey(k.ID, k.Version); err != nil {
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
		if err := checkKey(k.ID, k.Version); err != nil {
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
	if !model.ValidID(id) {
		return nil, badID(id)
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
	id := versionID(h.ID, h.Version)
	key, ps, err := checkBuiltin(h.ID, h.Version, hookMeta(h), model.Subject{Kind: "hook", ID: id})
	if err != nil {
		return err
	}
	if ps = append(ps, hookTextProblems(h, id)...); len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
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
		return model.Hook{}, staleRev("hook "+versionID(k.ID, k.Version), h.Rev, rev)
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
	return errorf(store.ErrNotFound, "hook %s 不存在", versionID(k.ID, k.Version))
}

func hookMeta(h model.Hook) meta {
	return meta{name: h.Name, state: h.State, author: h.Author, approver: h.Approver, builtin: h.Builtin}
}

func hookInfo(h model.Hook) (int, meta) { return h.Version, hookMeta(h) }

// hookTextProblems 是 hook 版本 h 里存不下的文本（model.StorableText，spec §3 总则），每一处一条 text_not_storable：
// 说明落在 hook 整体上，参数声明与实现按位置，Subject 的写法同激活判定（model.Subject 的说明），id 是「ID@版本」，新建时
// 为空。PostgreSQL 的 text 与 jsonb 都不收 NUL，jsonb 还会把非法的 UTF-8 换成 U+FFFD，读回来的就不是写进去的，所以
// 族、系、种类这些照目录取值的字段也查：草稿里它们可以是任何存得下的串，激活时才查对不对。
func hookTextProblems(h model.Hook, id string) []model.Problem {
	whole := model.Subject{Kind: "hook", ID: id}
	var ps []model.Problem
	if !model.StorableText(h.Description) {
		ps = append(ps, notStorable(whole))
	}
	ps = append(ps, declTextProblems(h.Params, whole)...)
	for i, impl := range h.Impls {
		if !storable(impl.Body, string(impl.Family), string(impl.Kind), string(impl.Interpreter)) ||
			!storableAll(impl.Series) || !storableAll(impl.Timings) {
			ps = append(ps, notStorable(model.Subject{Kind: "impl", ID: id, Path: "impl/" + strconv.Itoa(i+1)}))
		}
	}
	return ps
}

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

// 下面是 hook 与模板共用的部分：两边的身份、版本与名字规则一一对应（store.Templates 的说明）。

// ident 是 hook 或模板的一个身份。名字放在身份上，各版本共用（计划「已定的决定」），唯一约束按名字的 key。
type ident struct {
	name    string // 规范化之后的名字（model.NormalizeName 的 display）
	key     string // 名字的 key：display 的 ASCII 小写
	builtin bool   // 内置的身份只读（C25）
	last    int    // 分配过的最大版本号，只增不减（allocVersion）
}

// meta 是版本里由仓储管的字段，hook 与模板同名同义。
type meta struct {
	name, author, approver string
	state                  model.VersionState
	builtin                bool
}

// versionID 是一个版本在审计对象、Subject 与报错里的写法：「ID@版本」，与第 1 层判定里的相同。
func versionID(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

// checkKey 查收到的版本键：ID 过 model.ValidID，版本号从 1 起。不合的报 ErrInvalid，不当成找不到。
func checkKey(id string, version int) error {
	switch {
	case !model.ValidID(id):
		return badID(id)
	case version < 1:
		return errorf(store.ErrInvalid, "版本号要从 1 起，收到 %d", version)
	}
	return nil
}

// checkAuthor 查新版本的作者：审批人不能是作者（C22），没有作者这条就无从查起；作者要写进库里，还得存得下。
func checkAuthor(author string) error {
	if author == "" || !model.StorableText(author) {
		return errorf(store.ErrInvalid, "作者要是非空的可存文本，收到 %q", author)
	}
	return nil
}

// checkApprover 查审批人存得下。空的或就是作者的，由重跑的判定报出（activation.Approver）。
func checkApprover(approver string) error {
	if !model.StorableText(approver) {
		return errorf(store.ErrInvalid, "审批人 %q 存不下：不是合法的 UTF-8，或含 NUL", approver)
	}
	return nil
}

// normalizeName 按 model.NormalizeName 规范化名字；不合法时返回它的问题，Subject 换成 subj。
func normalizeName(raw string, subj model.Subject) (display, key string, ps []model.Problem) {
	display, key, err := model.NormalizeName(raw)
	var p model.Problem
	if errors.As(err, &p) {
		p.Subject = subj
		return "", "", []model.Problem{p}
	}
	return display, key, nil
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

// checkStep 查版本 what 能不能从 from 转出：内置的只读（C25），状态不对是不合规矩的转换，调用方重读之后再判断。
func checkStep(what string, m meta, from model.VersionState) error {
	switch {
	case m.builtin:
		return errorf(store.ErrImmutable, "%s 是内置的，只读", what)
	case m.state != from:
		return errorf(store.ErrConflict, "%s 现为 %s，这一步要从 %s 转出", what, m.state, from)
	}
	return nil
}

// checkDraft 查版本 what 是不是能改、能删的草稿：内置的只读（C25），已提交、已审批与已废弃的都不能改（C23）。
func checkDraft(what string, m meta) error {
	switch {
	case m.builtin:
		return errorf(store.ErrImmutable, "%s 是内置的，只读", what)
	case m.state != model.VersionDraft:
		return errorf(store.ErrImmutable, "%s 现为 %s，只有草稿能改、能删", what, m.state)
	}
	return nil
}

// checkSameMeta 查保存时带来的、由仓储管的字段与库里的相同：名字改用 Rename，状态只经状态转换改，作者在开版本时定下，
// 审批人由审批写，内置标记只由装载内置内容写。不同的报 ErrInvalid，不悄悄丢掉。
func checkSameMeta(what string, old, in meta) error {
	switch {
	case in.name != old.name:
		return errorf(store.ErrInvalid, "%s 的名字是 %q，收到的是 %q：改名要另走 Rename", what, old.name, in.name)
	case in != old:
		return errorf(store.ErrInvalid, "%s 的状态、作者、审批人与内置标记只由仓储写，保存时要原样带回", what)
	}
	return nil
}

// newVersionSource 是开新版本之前的检查：身份 id 存在且不是内置的（C25），来源版本 from 存在，身份没有未审批的版本
// （每个身份同时最多一个草稿或待审批的版本）。vs 是身份的全部版本，按版本号升序；info 取一个版本的版本号与 meta。都过了
// 才分配新版本的号（allocVersion），返回来源版本与这个号。
func newVersionSource[V any](kind string, idents map[string]ident, id string, vs []V, from int, info func(V) (int, meta)) (V, int, error) {
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
		if n, m := info(v); m.state == model.VersionDraft || m.state == model.VersionReview {
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
func currentApproved[V any](vs []V, info func(V) (int, meta)) (V, bool) {
	ts := make([]model.Template, len(vs))
	for i, v := range vs {
		n, m := info(v)
		ts[i] = model.Template{Version: n, State: m.state}
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
		if err := decodeCursor(cursor, list, &after.key, &after.id); err != nil {
			return 0, cursorKey{}, err
		}
	}
	return n, after, nil
}

// identPage 是 ListHooks 与 ListTemplates 共用的一页：名字的 key 以 prefix 开头的身份，各由 pick 选出一项，选不出或 keep
// 不要的跳过；按名字的 key、再按 ID 的字节序排，从 after 之后取 limit 条。prefix 只把 ASCII 字母转成小写再比，所以与
// 「大小写不同也算同名」一致；名字只含可打印 ASCII，别的字符原样按字节比。
func identPage[V any](idents map[string]ident, prefix string, after cursorKey, limit int, list string,
	pick func(id string) (V, bool), keep func(V) bool, clone func(V) V) store.Page[V] {
	prefix = asciiLower(prefix)
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
	p := page(items, limit, func(it item) item { return it }, func(it item) string { return encodeCursor(list, it.at.key, it.at.id) })
	out := store.Page[V]{Items: make([]V, len(p.Items)), Next: p.Next}
	for i, it := range p.Items {
		out.Items[i] = clone(it.v)
	}
	return out
}

// asciiLower 只把 ASCII 的大写字母转成小写，名字的 key 就是这样来的（model.NormalizeName）。strings.ToLower 按 Unicode
// 的规则转，会把开尔文符号 K（U+212A）转成 k，前缀就会匹配上本不该匹配的名字。
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// checkBuiltin 查装载的内置版本：要是内置的已审批版本，键合格式，作者与审批人存得下，名字已经是规范的写法（再规范化
// 一次不变，库里存的与程序自带的才比得上）。不合这些的是程序的错，报 ErrInvalid。返回名字的 key，以及名字不合法时的
// 问题（Subject 为 subj），由调用方与文本的问题一起报。
func checkBuiltin(id string, version int, m meta, subj model.Subject) (string, []model.Problem, error) {
	if err := checkKey(id, version); err != nil {
		return "", nil, err
	}
	switch {
	case !m.builtin || m.state != model.VersionApproved:
		return "", nil, errorf(store.ErrInvalid, "装载的内置内容要是内置的已审批版本，收到内置为 %v、状态为 %s", m.builtin, m.state)
	case !storable(m.author, m.approver):
		return "", nil, errorf(store.ErrInvalid, "内置内容的作者或审批人存不下：不是合法的 UTF-8，或含 NUL")
	}
	display, key, ps := normalizeName(m.name, subj)
	if len(ps) == 0 && display != m.name {
		return "", nil, errorf(store.ErrInvalid, "内置内容的名字 %q 不是规范的写法 %q", m.name, display)
	}
	return key, ps, nil
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

func notStorable(s model.Subject) model.Problem {
	return model.NewProblem("text_not_storable", s)
}

// storable 报告 ss 是否都存得下（model.StorableText）。
func storable(ss ...string) bool {
	return !slices.ContainsFunc(ss, func(s string) bool { return !model.StorableText(s) })
}

// storableAll 同 storable，收的是 catalog 里那些以字符串为底的类型。
func storableAll[S ~string](ss []S) bool {
	return !slices.ContainsFunc(ss, func(s S) bool { return !model.StorableText(string(s)) })
}

// declTextProblems 是参数声明里存不下的文本，每个声明一条，Path 为 param/<序号>，其余同 whole。
func declTextProblems(decls []model.ParamDecl, whole model.Subject) []model.Problem {
	var ps []model.Problem
	for i, d := range decls {
		ok := storable(d.Name, d.Label, string(d.Type), d.Default, d.Offer)
		for _, o := range d.Options {
			ok = ok && storable(o.Value, o.Label)
		}
		if !ok {
			s := whole
			s.Path = "param/" + strconv.Itoa(i+1)
			ps = append(ps, notStorable(s))
		}
	}
	return ps
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
