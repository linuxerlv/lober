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
	if err := store.CheckNewISO(iso); err != nil {
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

// UpdateISO 见 store.ISOs。入参先过 store.CheckISOUpdate；读到现有的记录之后，再过 store.CheckISOUpdateFrom：已废弃的报
// ErrImmutable，改了族、系与名称报 ErrInvalid，置为 ready 的要过完整性判定，写进来的文本都要存得下。
func (s *Store) UpdateISO(_ context.Context, iso model.ISO) (model.ISO, error) {
	if err := store.CheckISOUpdate(iso); err != nil {
		return model.ISO{}, err
	}
	iso = cloneISO(iso)
	err := s.write(func(t *tx) error {
		old, err := t.isoAt(iso.ID, iso.Rev)
		if err != nil {
			return err
		}
		if err := store.CheckISOUpdateFrom(old, iso); err != nil {
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
	if err := store.CheckID(id); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		iso, err := t.isoAt(id, rev)
		if err != nil {
			return err
		}
		if err := store.CheckISODeprecation(id, iso.State); err != nil {
			return err
		}
		iso.State = model.ISODeprecated
		iso.Rev++
		t.isos[id] = iso
		return t.addAudit(a, "iso", id)
	})
}

// DeleteISO 见 store.ISOs。被任务引用的只能废弃、不能删，任务结束了也一样（referencedBy）。
func (s *Store) DeleteISO(_ context.Context, id string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		if _, err := t.isoAt(id, rev); err != nil {
			return err
		}
		if by, ok := t.referencedBy(func(task model.Task) bool { return task.ISOID == id }); ok {
			return errorf(store.ErrReferenced, "ISO %s 被任务 %s 引用：只能废弃，已建的任务要一直查得到它", id, by)
		}
		delete(t.isos, id)
		return t.addAudit(a, "iso", id)
	})
}

// GetISO 见 store.ISOs。
func (s *Store) GetISO(_ context.Context, id string) (model.ISO, error) {
	if err := store.CheckID(id); err != nil {
		return model.ISO{}, err
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
		if err := store.DecodeCursor(q.Cursor, "isos", &after.Name, &after.ID); err != nil {
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
	return page(items, limit, cloneISO, func(iso model.ISO) string { return store.EncodeCursor("isos", iso.Name, iso.ID) }), nil
}

// sha256Holder 返回已经占着 iso 的 SHA256 的另一个 ISO 的 ID，没有时返回空串（C4）。谁占着它见 store.HoldsSHA256：空的
// SHA256 与导入失败的 ISO 都不占位，与 pgstore 的部分唯一索引同一个条件。
func (d *data) sha256Holder(iso model.ISO) string {
	if !store.HoldsSHA256(iso) {
		return ""
	}
	for id, other := range d.isos {
		if id != iso.ID && store.HoldsSHA256(other) && other.SHA256 == iso.SHA256 {
			return id
		}
	}
	return ""
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
