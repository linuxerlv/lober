package memstore

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// CreateHost 见 store.Hosts。
func (s *Store) CreateHost(_ context.Context, h model.Host, a store.Audit) (model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := store.CheckHosts([]model.Host{h}); err != nil {
		return model.Host{}, err
	}
	h = cloneHost(h)
	h.Rev = 1
	err := s.write(func(t *tx) error {
		if _, ok := t.hosts[h.SN]; ok {
			return &store.DuplicateError{Field: "sn", Existing: h.SN}
		}
		t.hosts[h.SN] = h
		return t.addAudit(a, "host", h.SN)
	})
	if err != nil {
		return model.Host{}, err
	}
	return cloneHost(h), nil
}

// UpdateHost 见 store.Hosts。
func (s *Store) UpdateHost(_ context.Context, h model.Host, a store.Audit) (model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := store.CheckHosts([]model.Host{h}); err != nil {
		return model.Host{}, err
	}
	h = cloneHost(h)
	err := s.write(func(t *tx) error {
		if _, err := t.hostAt(h.SN, h.Rev); err != nil {
			return err
		}
		h.Rev++
		t.hosts[h.SN] = h
		return t.addAudit(a, "host", h.SN)
	})
	if err != nil {
		return model.Host{}, err
	}
	return cloneHost(h), nil
}

// ImportHosts 见 store.Hosts。
func (s *Store) ImportHosts(_ context.Context, hs []model.Host, a store.Audit) ([]model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return nil, err
	}
	if err := store.CheckHosts(hs); err != nil {
		return nil, err
	}
	out := make([]model.Host, len(hs))
	err := s.write(func(t *tx) error {
		for i, h := range hs {
			h = cloneHost(h)
			h.Rev = t.hosts[h.SN].Rev + 1 // 已登记的不比 Rev；没登记的取到零值，新建的 Rev 因此为 1
			t.hosts[h.SN] = h
			out[i] = h
		}
		for _, h := range out {
			if err := t.addAudit(a, "host", h.SN); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = cloneHost(out[i])
	}
	return out, nil
}

// DeleteHost 见 store.Hosts。被任务引用的不能删，任务结束了也一样（referencedBy）。
func (s *Store) DeleteHost(_ context.Context, sn string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.write(func(t *tx) error {
		if _, err := t.hostAt(sn, rev); err != nil {
			return err
		}
		if by, ok := t.referencedBy(func(task model.Task) bool { return task.HostSN == sn }); ok {
			return errorf(store.ErrReferenced, "主机 %s 被任务 %s 引用：已建的任务要一直查得到它", sn, by)
		}
		delete(t.hosts, sn)
		return t.addAudit(a, "host", sn)
	})
}

// GetHost 见 store.Hosts。
func (s *Store) GetHost(_ context.Context, sn string) (model.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.d.hosts[sn]
	if !ok {
		return model.Host{}, hostNotFound(sn)
	}
	return cloneHost(h), nil
}

// GetHosts 见 store.Hosts。
func (s *Store) GetHosts(_ context.Context, sns []string) (map[string]model.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]model.Host, len(sns))
	for _, sn := range sns {
		if h, ok := s.d.hosts[sn]; ok {
			out[sn] = cloneHost(h)
		}
	}
	return out, nil
}

// ListHosts 见 store.Hosts。按 SN 的字节序排，游标是上一页最后一台的 SN。
func (s *Store) ListHosts(_ context.Context, q store.HostQuery) (store.Page[model.Host], error) {
	limit, err := store.CheckHostQuery(q)
	if err != nil {
		return store.Page[model.Host]{}, err
	}
	var after string
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "hosts", &after); err != nil {
			return store.Page[model.Host]{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []model.Host
	for _, h := range s.d.hosts {
		// AttrValue 不为空，没登记这个属性的主机取到空串，不会被选中
		if h.SN > after && strings.HasPrefix(h.SN, q.SNPrefix) && (q.AttrKey == "" || h.Attrs[q.AttrKey] == q.AttrValue) {
			items = append(items, h)
		}
	}
	slices.SortFunc(items, func(a, b model.Host) int { return strings.Compare(a.SN, b.SN) })
	return page(items, limit, cloneHost, func(h model.Host) string { return store.EncodeCursor("hosts", h.SN) }), nil
}

// hostAt 取 SN 为 sn、Rev 为 rev 的主机：没登记报 ErrNotFound，Rev 对不上报 ErrConflict。
func (d *data) hostAt(sn string, rev int64) (model.Host, error) {
	h, ok := d.hosts[sn]
	switch {
	case !ok:
		return model.Host{}, hostNotFound(sn)
	case h.Rev != rev:
		return model.Host{}, staleRev("主机 "+sn, h.Rev, rev)
	}
	return h, nil
}

func hostNotFound(sn string) error {
	return errorf(store.ErrNotFound, "主机 %q 没有登记", sn)
}

// cloneHost 深拷贝一台主机。nil 的属性拷出来仍是 nil：写进去什么就读出什么。
func cloneHost(h model.Host) model.Host {
	h.Attrs = maps.Clone(h.Attrs)
	return h
}
