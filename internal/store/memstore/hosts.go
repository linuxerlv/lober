package memstore

import (
	"context"
	"errors"
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
	if err := checkHosts([]model.Host{h}); err != nil {
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
	if err := checkHosts([]model.Host{h}); err != nil {
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
	if len(hs) == 0 {
		return nil, errorf(store.ErrInvalid, "一批至少要有一台主机")
	}
	if err := checkHosts(hs); err != nil {
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
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[model.Host]{}, err
	}
	if (q.AttrKey == "") != (q.AttrValue == "") {
		return store.Page[model.Host]{}, errorf(store.ErrInvalid, "AttrKey 与 AttrValue 要么都给，要么都不给")
	}
	var after string
	if q.Cursor != "" {
		if err := decodeCursor(q.Cursor, "hosts", &after); err != nil {
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
	return page(items, limit, cloneHost, func(h model.Host) string { return encodeCursor("hosts", h.SN) }), nil
}

// checkHosts 查要写入的一批主机，登记与修改是只有一台的批：SN 已经规范化，记录过 Host.Validate，批里 SN 不重复。
//
// 能用原因代码表达的问题都收进 ProblemsError(ErrInvalid)，与 model 的校验完全相同：按入参的顺序，每台先报批内重复
// （只在这个 SN 第一次出现时报一条），再报 NormalizeSN 与 Host.Validate 的问题。SN 合法却不是规范化之后的写法，
// 是调用方漏了规范化，model 的校验对它没有问题可报，报不带问题的 ErrInvalid。
func checkHosts(hs []model.Host) error {
	count := make(map[string]int, len(hs))
	for _, h := range hs {
		count[h.SN]++
	}
	var ps []model.Problem
	for _, h := range hs {
		if count[h.SN] > 1 {
			ps = append(ps, model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: h.SN}))
			count[h.SN] = 0
		}
		n, err := model.NormalizeSN(h.SN)
		var p model.Problem
		switch {
		case errors.As(err, &p):
			ps = append(ps, p)
		case n != h.SN:
			return errorf(store.ErrInvalid, "SN %q 不是规范化之后的写法 %q：调用方要先经 model.NormalizeSN", h.SN, n)
		}
		ps = append(ps, h.Validate()...)
	}
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return nil
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
