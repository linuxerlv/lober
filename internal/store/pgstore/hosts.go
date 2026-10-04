package pgstore

import (
	"context"
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hostColumns 是修改主机时写的列：整条替换，改成零值的也照写（不带 Select 的 Updates 会跳过零值）。
var hostColumns = []string{"arch", "firmware", "attrs", "rev"}

// upsertHost 是导入的写法：没登记的新建；已登记的整条替换，Rev 在库里加 1，不比调用方带来的。在一条语句里判断
// 登没登记，同时登记同一个 SN 的另一次写入插不进中间。
var upsertHost = clause.OnConflict{
	Columns: []clause.Column{{Name: "sn"}},
	DoUpdates: append(clause.AssignmentColumns([]string{"arch", "firmware", "attrs"}),
		clause.Assignment{Column: clause.Column{Name: "rev"}, Value: gorm.Expr("hosts.rev + 1")}),
}

// CreateHost 见 store.Hosts。SN 已登记由主键报出来，不先查：先查再插，两次同时登记就都查不到。
func (s *Store) CreateHost(ctx context.Context, h model.Host, a store.Audit) (model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := checkHosts([]model.Host{h}); err != nil {
		return model.Host{}, err
	}
	h.Rev = 1
	row, err := toHostRow(h)
	if err != nil {
		return model.Host{}, err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "host", h.SN)
	})
	if err != nil {
		return model.Host{}, translate(err, false, func(string) string { return h.SN }) // 撞上的就是这个 SN
	}
	return row.host()
}

// UpdateHost 见 store.Hosts。
func (s *Store) UpdateHost(ctx context.Context, h model.Host, a store.Audit) (model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := checkHosts([]model.Host{h}); err != nil {
		return model.Host{}, err
	}
	row, err := toHostRow(h)
	if err != nil {
		return model.Host{}, err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		cur, err := lockHost(tx, h.SN, h.Rev)
		if err != nil {
			return err
		}
		row.Rev = cur.Rev + 1
		if err := tx.Model(&hostRow{SN: h.SN}).Select(hostColumns).Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "host", h.SN)
	})
	if err != nil {
		return model.Host{}, translate(err, false, nil)
	}
	return row.host()
}

// ImportHosts 见 store.Hosts。按 SN 的顺序写：同时导入的两批按同一个顺序给行加锁，不会互相等死。写完在同一个事务里
// 读回各台的 Rev，这些行此时由本事务锁着，读到的就是本次写下的。返回值与审计都按入参的顺序。
func (s *Store) ImportHosts(ctx context.Context, hs []model.Host, a store.Audit) ([]model.Host, error) {
	if err := store.CheckAudit(a); err != nil {
		return nil, err
	}
	if len(hs) == 0 {
		return nil, errorf(store.ErrInvalid, "一批至少要有一台主机")
	}
	if err := checkHosts(hs); err != nil {
		return nil, err
	}
	rows := make([]hostRow, len(hs))
	sns := make([]string, len(hs))
	for i, h := range hs {
		h.Rev = 1 // 新建的 Rev 为 1；已登记的由 upsertHost 在库里加 1，不用这个值
		row, err := toHostRow(h)
		if err != nil {
			return nil, err
		}
		rows[i], sns[i] = row, h.SN
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(x, y hostRow) int { return strings.Compare(x.SN, y.SN) })
	err := s.write(ctx, func(tx *gorm.DB) error {
		for chunk := range slices.Chunk(sorted, batchSize) {
			if err := tx.Clauses(upsertHost).Create(&chunk).Error; err != nil {
				return err
			}
		}
		revs := make(map[string]int64, len(hs))
		for chunk := range slices.Chunk(sns, batchSize) {
			var got []hostRow
			if err := tx.Select("sn", "rev").Where("sn IN ?", chunk).Find(&got).Error; err != nil {
				return err
			}
			for _, r := range got {
				revs[r.SN] = r.Rev
			}
		}
		for i := range rows {
			rows[i].Rev = revs[rows[i].SN]
		}
		return addAudits(tx, a, "host", sns...)
	})
	if err != nil {
		return nil, translate(err, false, nil)
	}
	out := make([]model.Host, len(rows))
	for i, r := range rows {
		if out[i], err = r.host(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DeleteHost 见 store.Hosts。被任务引用时由外键报出来（任务 9 加上 tasks 表之后），翻成 ErrReferenced。
func (s *Store) DeleteHost(ctx context.Context, sn string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if !storable(sn) {
		return hostNotFound(sn)
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		if _, err := lockHost(tx, sn, rev); err != nil {
			return err
		}
		if err := tx.Delete(&hostRow{SN: sn}).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "host", sn)
	})
	return translate(err, true, nil)
}

// GetHost 见 store.Hosts。
func (s *Store) GetHost(ctx context.Context, sn string) (model.Host, error) {
	if !storable(sn) {
		return model.Host{}, hostNotFound(sn)
	}
	var row hostRow
	err := s.read(ctx).Where("sn = ?", sn).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return model.Host{}, hostNotFound(sn)
	case err != nil:
		return model.Host{}, err
	}
	return row.host()
}

// GetHosts 见 store.Hosts。
func (s *Store) GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error) {
	keys := slices.DeleteFunc(slices.Clone(sns), func(sn string) bool { return !storable(sn) })
	out := make(map[string]model.Host, len(keys))
	for chunk := range slices.Chunk(keys, batchSize) {
		var rows []hostRow
		if err := s.read(ctx).Where("sn IN ?", chunk).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			h, err := r.host()
			if err != nil {
				return nil, err
			}
			out[r.SN] = h
		}
	}
	return out, nil
}

// ListHosts 见 store.Hosts。按 SN 的字节序排，游标是上一页最后一台的 SN。SN 前缀用 starts_with 比，不用 LIKE：
// _、% 与反斜杠都按原样比，不必转义。
func (s *Store) ListHosts(ctx context.Context, q store.HostQuery) (store.Page[model.Host], error) {
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
	if !storable(q.SNPrefix, q.AttrKey, q.AttrValue) {
		return emptyPage[model.Host](), nil
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`sn COLLATE "C" > ?`, after)
	}
	if q.SNPrefix != "" {
		db = db.Where("starts_with(sn, ?)", q.SNPrefix)
	}
	if q.AttrKey != "" {
		// 没登记这个属性的主机取到 NULL，不会被选中
		db = db.Where("attrs ->> ? = ?", q.AttrKey, q.AttrValue)
	}
	var rows []hostRow
	if err := db.Order(`sn COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[model.Host]{}, err
	}
	return page(rows, limit, hostRow.host, func(r hostRow) string { return encodeCursor("hosts", r.SN) })
}

// checkHosts 查要写入的一批主机，登记与修改是只有一台的批：SN 已经规范化，记录过 Host.Validate，批里 SN 不重复。
//
// 能用原因代码表达的问题都收进 ProblemsError(ErrInvalid)，与 model 的校验完全相同：按入参的顺序，每台先报批内重复
// （只在这个 SN 第一次出现时报一条），再报 NormalizeSN 与 Host.Validate 的问题。SN 合法却不是规范化之后的写法，
// 是调用方漏了规范化，model 的校验对它没有问题可报，报不带问题的 ErrInvalid。规则与内存实现的同名函数相同，由
// 契约测试守着两边一致。
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

// lockHost 在事务 tx 里给 SN 为 sn 的主机加写锁并取出它：没登记报 ErrNotFound，Rev 不是 rev 报 ErrConflict。
func lockHost(tx *gorm.DB, sn string, rev int64) (hostRow, error) {
	var row hostRow
	err := tx.Clauses(forUpdate).Where("sn = ?", sn).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return hostRow{}, hostNotFound(sn)
	case err != nil:
		return hostRow{}, err
	case row.Rev != rev:
		return hostRow{}, staleRev("主机 "+sn, row.Rev, rev)
	}
	return row, nil
}

func hostNotFound(sn string) error {
	return errorf(store.ErrNotFound, "主机 %q 没有登记", sn)
}
