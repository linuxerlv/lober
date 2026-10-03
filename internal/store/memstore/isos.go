package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// CreateISO 见 store.ISOs。
func (s *Store) CreateISO(_ context.Context, iso model.ISO, a store.Audit) (model.ISO, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.ISO{}, err
	}
	if err := checkNewISO(iso); err != nil {
		return model.ISO{}, err
	}
	iso = model.ISO{ID: store.NewID(), Name: iso.Name, Family: iso.Family, Series: iso.Series, State: model.ISOAcquiring, Rev: 1}
	err := s.write(func(t *tx) error {
		t.isos[iso.ID] = iso
		return t.addAudit(a, "iso", iso.ID)
	})
	if err != nil {
		return model.ISO{}, err
	}
	return iso, nil
}

// UpdateISO 见 store.ISOs。已废弃的 ISO 报 ErrImmutable：废弃是要审计的决定，不审计的系统更新不能把它撤掉。
// 写进来的文本有一处存不下，不论置为什么状态都报 ErrInvalid（checkISOText）。
func (s *Store) UpdateISO(_ context.Context, iso model.ISO) (model.ISO, error) {
	switch {
	case !model.ValidID(iso.ID):
		return model.ISO{}, badID(iso.ID)
	case iso.State == model.ISODeprecated:
		return model.ISO{}, errorf(store.ErrInvalid, "系统更新不能把 ISO 置为已废弃：废弃要审计，走 DeprecateISO")
	case !iso.State.Valid():
		return model.ISO{}, errorf(store.ErrInvalid, "ISO 的状态 %q 不认识", iso.State)
	}
	iso = cloneISO(iso)
	err := s.write(func(t *tx) error {
		old, err := t.isoAt(iso.ID, iso.Rev)
		switch {
		case err != nil:
			return err
		case old.State == model.ISODeprecated:
			return errorf(store.ErrImmutable, "ISO %s 已废弃，不再接受系统更新", iso.ID)
		case iso.Family != old.Family || iso.Series != old.Series || iso.Name != old.Name:
			return errorf(store.ErrInvalid, "系统更新不能改 ISO %s 的族、系与名称", iso.ID)
		}
		if iso.State == model.ISOReady { // C5：导入完成的 ISO 必须通过完整性判定
			if ps := iso.Validate(); len(ps) > 0 {
				return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
			}
		}
		if err := checkISOText(iso); err != nil { // 不论置为什么状态：Validate 不看制品与 SHA256，别的状态也不跑它
			return err
		}
		if other := t.sha256Holder(iso); other != "" {
			return &store.DuplicateError{Field: "sha256", Existing: other}
		}
		iso.Rev++
		t.isos[iso.ID] = iso
		return nil
	})
	if err != nil {
		return model.ISO{}, err
	}
	return cloneISO(iso), nil
}

// DeprecateISO 见 store.ISOs。
func (s *Store) DeprecateISO(_ context.Context, id string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if !model.ValidID(id) {
		return badID(id)
	}
	return s.write(func(t *tx) error {
		iso, err := t.isoAt(id, rev)
		if err != nil {
			return err
		}
		if iso.State != model.ISOReady {
			return errorf(store.ErrConflict, "ISO %s 现为 %s，只有 ready 的能废弃", id, iso.State)
		}
		iso.State = model.ISODeprecated
		iso.Rev++
		t.isos[id] = iso
		return t.addAudit(a, "iso", id)
	})
}

// DeleteISO 见 store.ISOs。被任务引用时报 ErrReferenced 的检查随任务一起实现：在那之前建不出任务，也就没有引用。
func (s *Store) DeleteISO(_ context.Context, id string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if !model.ValidID(id) {
		return badID(id)
	}
	return s.write(func(t *tx) error {
		if _, err := t.isoAt(id, rev); err != nil {
			return err
		}
		delete(t.isos, id)
		return t.addAudit(a, "iso", id)
	})
}

// GetISO 见 store.ISOs。
func (s *Store) GetISO(_ context.Context, id string) (model.ISO, error) {
	if !model.ValidID(id) {
		return model.ISO{}, badID(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iso, ok := s.d.isos[id]
	if !ok {
		return model.ISO{}, isoNotFound(id)
	}
	return cloneISO(iso), nil
}

// ListISOs 见 store.ISOs。ISO 的名称不规范化、可以重复，没有名字的 key，所以按名称的字节序、再按 ID 排；游标是
// 上一页最后一个的名称与 ID。
func (s *Store) ListISOs(_ context.Context, q store.ISOQuery) (store.Page[model.ISO], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[model.ISO]{}, err
	}
	var after model.ISO // 只用 Name 与 ID；没有游标时两者为空，排在任何 ISO 之前
	if q.Cursor != "" {
		if err := decodeCursor(q.Cursor, "isos", &after.Name, &after.ID); err != nil {
			return store.Page[model.ISO]{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []model.ISO
	for _, iso := range s.d.isos {
		if compareISOs(iso, after) > 0 && isoMatches(q, iso) {
			items = append(items, iso)
		}
	}
	slices.SortFunc(items, compareISOs)
	return page(items, limit, cloneISO, func(iso model.ISO) string { return encodeCursor("isos", iso.Name, iso.ID) }), nil
}

// checkNewISO 查 CreateISO 的入参。其余字段由仓储与第 4 层的导入填，调用方填了，多半是把别的记录当成了新建的入参。
func checkNewISO(iso model.ISO) error {
	switch {
	case iso.ID != "" || iso.Version != "" || iso.Arch != "" || iso.State != "" || iso.SHA256 != "" ||
		iso.Artifacts != nil || iso.Offers != nil || iso.Rev != 0:
		return errorf(store.ErrInvalid, "新建 ISO 只填 Name、Family、Series，其余由仓储与导入填")
	case !iso.Family.Valid() || !iso.Series.Valid() || iso.Series.Family() != iso.Family:
		return errorf(store.ErrInvalid, "族 %q 与系 %q 对不上：两者都要是目录里的原样写法，系要属于族（C2）", iso.Family, iso.Series)
	case !model.StorableText(iso.Name):
		return errorf(store.ErrInvalid, "ISO 的名称存不下：不是合法的 UTF-8，或含 NUL")
	}
	return nil
}

// checkISOText 查系统更新写进来的文本都存得下（model.StorableText，spec §3 总则）：版本、架构、SHA256、制品的角色
// 与路径、提供的集合名、选项的值与显示名。PostgreSQL 的 text 与 jsonb 都不收 NUL，jsonb 还会把非法的 UTF-8 换成
// U+FFFD，读回来的就不是写进去的。名称、族与系不能经系统更新改，新建时已经查过。
func checkISOText(iso model.ISO) error {
	bad := func(what string) error {
		return errorf(store.ErrInvalid, "ISO %s 的%s存不下：不是合法的 UTF-8，或含 NUL", iso.ID, what)
	}
	switch {
	case !model.StorableText(iso.Version):
		return bad("版本")
	case !model.StorableText(string(iso.Arch)):
		return bad("架构")
	case !model.StorableText(iso.SHA256):
		return bad(" SHA256 ")
	}
	for role, path := range iso.Artifacts {
		if !model.StorableText(role) || !model.StorableText(path) {
			return bad("制品")
		}
	}
	for name, opts := range iso.Offers {
		if !model.StorableText(name) {
			return bad("提供的集合名")
		}
		for _, o := range opts {
			if !model.StorableText(o.Value) || !model.StorableText(o.Label) {
				return bad("提供的选项")
			}
		}
	}
	return nil
}

// sha256Holder 返回已经占着 iso 的 SHA256 的另一个 ISO 的 ID，没有时返回空串（C4）。条件与 pgstore 的部分唯一索引
// 相同：空的 SHA256 与导入失败的 ISO 都不占位，所以导入失败的 ISO 可以记下与别的 ISO 相同的内容，指向那一个
// （spec §5.1 第 3 步）。
func (d *data) sha256Holder(iso model.ISO) string {
	if !holdsSHA256(iso) {
		return ""
	}
	for id, other := range d.isos {
		if id != iso.ID && holdsSHA256(other) && other.SHA256 == iso.SHA256 {
			return id
		}
	}
	return ""
}

func holdsSHA256(iso model.ISO) bool {
	return iso.SHA256 != "" && iso.State != model.ISOFailed
}

// isoAt 取 ID 为 id、Rev 为 rev 的 ISO：不存在报 ErrNotFound，Rev 对不上报 ErrConflict。
func (d *data) isoAt(id string, rev int64) (model.ISO, error) {
	iso, ok := d.isos[id]
	switch {
	case !ok:
		return model.ISO{}, isoNotFound(id)
	case iso.Rev != rev:
		return model.ISO{}, staleRev("ISO "+id, iso.Rev, rev)
	}
	return iso, nil
}

func isoNotFound(id string) error {
	return errorf(store.ErrNotFound, "ISO %s 不存在", id)
}

func isoMatches(q store.ISOQuery, iso model.ISO) bool {
	return (q.Family == "" || iso.Family == q.Family) && (q.Series == "" || iso.Series == q.Series) &&
		(len(q.States) == 0 || slices.Contains(q.States, iso.State))
}

// compareISOs 是 ISO 的顺序：先按名称、再按 ID，都按字节序。
func compareISOs(a, b model.ISO) int {
	return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
}

// cloneISO 深拷贝一个 ISO：制品与提供的集合里的切片都另拷一份。nil 拷出来仍是 nil：写进去什么就读出什么。
func cloneISO(iso model.ISO) model.ISO {
	iso.Artifacts = maps.Clone(iso.Artifacts)
	if iso.Offers != nil {
		offers := make(map[string][]model.Option, len(iso.Offers))
		for name, opts := range iso.Offers {
			offers[name] = slices.Clone(opts)
		}
		iso.Offers = offers
	}
	return iso
}
