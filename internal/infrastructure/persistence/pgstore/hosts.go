package pgstore

import (
	"context"
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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

// CreateHost 见 repository.Hosts。SN 已登记由主键报出来，不先查：先查再插，两次同时登记就都查不到。
func (s *Store) CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	if err := repository.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := repository.CheckHosts([]model.Host{h}); err != nil {
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

// UpdateHost 见 repository.Hosts。
func (s *Store) UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	if err := repository.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	if err := repository.CheckHosts([]model.Host{h}); err != nil {
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

// ImportHosts 见 repository.Hosts。按 hostLockOrder 的次序写，写一行就锁住一行：同时导入的两批、导入与批量建任务，都按同一个
// 次序给行加锁，不会互相等死。写完在同一个事务里读回各台的 Rev，这些行此时由本事务锁着，读到的就是本次写下的。返回值
// 与审计都按入参的顺序。
func (s *Store) ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error) {
	if err := repository.CheckAudit(a); err != nil {
		return nil, err
	}
	if err := repository.CheckHosts(hs); err != nil {
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
	sorted := hostLockOrder(rows, func(r hostRow) string { return r.SN })
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

// DeleteHost 见 repository.Hosts。被任务引用时由外键报出来（任务 9 加上 tasks 表之后），翻成 ErrReferenced。
func (s *Store) DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error {
	if err := repository.CheckAudit(a); err != nil {
		return err
	}
	if !storable(sn) {
		return hostNotFound(sn)
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		if _, err := lockHost(tx, sn, rev); err != nil {
			return err
		}
		if err := taskReference(tx, "host_sn = ?", sn); err != nil {
			return err
		}
		if err := tx.Delete(&hostRow{SN: sn}).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "host", sn)
	})
	return translate(err, true, nil)
}

// GetHost 见 repository.Hosts。
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

// GetHosts 见 repository.Hosts。
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

// ListHosts 见 repository.Hosts。按 SN 的字节序排，游标是上一页最后一台的 SN。SN 前缀用 starts_with 比，不用 LIKE：
// _、% 与反斜杠都按原样比，不必转义。
func (s *Store) ListHosts(ctx context.Context, q repository.HostQuery) (repository.Page[model.Host], error) {
	limit, err := repository.CheckHostQuery(q)
	if err != nil {
		return repository.Page[model.Host]{}, err
	}
	var after string
	if q.Cursor != "" {
		if err := repository.DecodeCursor(q.Cursor, "hosts", &after); err != nil {
			return repository.Page[model.Host]{}, err
		}
	}
	if !storable(q.SNPrefix, q.AttrKey, q.AttrValue) {
		return emptyPage[model.Host](q.Page), nil
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
	db, total, err := numberedQuery[hostRow](db, q.Page, limit)
	if err != nil {
		return repository.Page[model.Host]{}, err
	}
	var rows []hostRow
	if err := db.Order(`sn COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repository.Page[model.Host]{}, err
	}
	return page(rows, limit, hostRow.host, func(r hostRow) string { return repository.EncodeCursor("hosts", r.SN) }, total)
}

// hostLockOrder 是给多台主机加锁的次序：按 SN 的字节序升序，与 SQL 里的 ORDER BY sn COLLATE "C" 相同。凡在一个事务里给
// 多台主机加锁，都先按它排好、再按排好的顺序逐台加锁（ImportHosts 写主机；CreateTasks 经 lockHosts 加写锁）：任意两个
// 事务按同一个次序拿锁，等待就不会成环，不会互相等死（SQLSTATE 40P01）。各处自己排一遍，一处改了次序，只在 CSV 导入撞上
// 批量建任务时才出错。返回排好的副本，xs 不动；sn 取出一项的 SN。
func hostLockOrder[T any](xs []T, sn func(T) string) []T {
	out := slices.Clone(xs)
	slices.SortFunc(out, func(a, b T) int { return strings.Compare(sn(a), sn(b)) })
	return out
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
	return errorf(repository.ErrNotFound, "主机 %q 没有登记", sn)
}
