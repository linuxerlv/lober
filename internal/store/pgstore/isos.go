package pgstore

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// isoColumns 是系统更新写的列：族、系与名称不能经它改，其余整条替换，改回零值的也照写。
var isoColumns = []string{"version", "arch", "state", "sha256", "artifacts", "offers", "rev"}

// CreateISO 见 store.ISOs。
func (s *Store) CreateISO(ctx context.Context, iso model.ISO, a store.Audit) (model.ISO, error) {
	if err := store.CheckAudit(a); err != nil {
		return model.ISO{}, err
	}
	if err := store.CheckNewISO(iso); err != nil {
		return model.ISO{}, err
	}
	iso = model.ISO{ID: store.NewID(), Name: iso.Name, Family: iso.Family, Series: iso.Series, State: model.ISOAcquiring, Rev: 1}
	row, err := toISORow(iso)
	if err != nil {
		return model.ISO{}, err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "iso", iso.ID)
	})
	if err != nil {
		return model.ISO{}, translate(err, false, nil)
	}
	return row.iso()
}

// UpdateISO 见 store.ISOs。入参先过 store.CheckISOUpdate；锁住现有的记录之后，再过 store.CheckISOUpdateFrom：已废弃的报
// ErrImmutable，改了族、系与名称报 ErrInvalid，置为 ready 的要过完整性判定，写进来的文本都要存得下。
//
// SHA256 先在事务里查有没有别的 ISO 占着（C4），好报出对方的 ID；两次更新同时写同一份内容时，部分唯一索引
// idx_isos_sha256 兜底，撞上之后在事务外查出对方。
func (s *Store) UpdateISO(ctx context.Context, iso model.ISO) (model.ISO, error) {
	if err := store.CheckISOUpdate(iso); err != nil {
		return model.ISO{}, err
	}
	var row isoRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		cur, err := lockISO(tx, iso.ID, iso.Rev)
		if err != nil {
			return err
		}
		old, err := cur.iso()
		if err != nil {
			return err
		}
		if err := store.CheckISOUpdateFrom(old, iso); err != nil {
			return err
		}
		other, err := sha256Holder(tx, iso)
		switch {
		case err != nil:
			return err
		case other != "":
			return &store.DuplicateError{Field: "sha256", Existing: other}
		}
		next := iso
		next.Rev = cur.Rev + 1
		if row, err = toISORow(next); err != nil {
			return err
		}
		return tx.Model(&isoRow{ID: iso.ID}).Select(isoColumns).Updates(&row).Error
	})
	if err != nil {
		return model.ISO{}, translate(err, false, func(string) string {
			other, _ := sha256Holder(s.read(ctx), iso) // 查不到就原样返回撞上的数据库错误
			return other
		})
	}
	return row.iso()
}

// DeprecateISO 见 store.ISOs。
func (s *Store) DeprecateISO(ctx context.Context, id string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		cur, err := lockISO(tx, id, rev)
		if err != nil {
			return err
		}
		if err := store.CheckISODeprecation(id, model.ISOState(cur.State)); err != nil {
			return err
		}
		cur.State, cur.Rev = string(model.ISODeprecated), cur.Rev+1
		if err := tx.Model(&isoRow{ID: id}).Select("state", "rev").Updates(&cur).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "iso", id)
	})
	return translate(err, false, nil)
}

// DeleteISO 见 store.ISOs。被任务引用时由外键报出来（任务 9 加上 tasks 表之后），翻成 ErrReferenced。
func (s *Store) DeleteISO(ctx context.Context, id string, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		if _, err := lockISO(tx, id, rev); err != nil {
			return err
		}
		if err := tx.Delete(&isoRow{ID: id}).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "iso", id)
	})
	return translate(err, true, nil)
}

// GetISO 见 store.ISOs。
func (s *Store) GetISO(ctx context.Context, id string) (model.ISO, error) {
	if err := store.CheckID(id); err != nil {
		return model.ISO{}, err
	}
	var row isoRow
	err := s.read(ctx).Where("id = ?", id).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return model.ISO{}, isoNotFound(id)
	case err != nil:
		return model.ISO{}, err
	}
	return row.iso()
}

// ListISOs 见 store.ISOs。ISO 的名称不规范化、可以重复，没有名字的 key，所以按名称的字节序、再按 ID 排；游标是上一页
// 最后一个的名称与 ID。
func (s *Store) ListISOs(ctx context.Context, q store.ISOQuery) (store.Page[model.ISO], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[model.ISO]{}, err
	}
	var afterName, afterID string
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "isos", &afterName, &afterID); err != nil {
			return store.Page[model.ISO]{}, err
		}
	}
	// 存不下的状态谁都不是，去掉；给了状态却一个都不剩，就什么都匹配不上
	var states []string
	for _, st := range q.States {
		if storable(string(st)) {
			states = append(states, string(st))
		}
	}
	if !storable(string(q.Family), string(q.Series)) || len(q.States) > 0 && len(states) == 0 {
		return emptyPage[model.ISO](), nil
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`(name COLLATE "C" > ? OR (name COLLATE "C" = ? AND id COLLATE "C" > ?))`, afterName, afterName, afterID)
	}
	if q.Family != "" {
		db = db.Where("family = ?", string(q.Family))
	}
	if q.Series != "" {
		db = db.Where("series = ?", string(q.Series))
	}
	if len(states) > 0 {
		db = db.Where("state IN ?", states)
	}
	var rows []isoRow
	if err := db.Order(`name COLLATE "C", id COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[model.ISO]{}, err
	}
	return page(rows, limit, isoRow.iso, func(r isoRow) string { return store.EncodeCursor("isos", r.Name, r.ID) })
}

// sha256Holder 在 db 里查已经占着 iso 的 SHA256 的另一个 ISO，返回它的 ID，没有时返回空串（C4）。谁占着它见
// store.HoldsSHA256：空的 SHA256 与导入失败的 ISO 都不占位，查询的条件与部分唯一索引 idx_isos_sha256 的谓词都是它。
func sha256Holder(db *gorm.DB, iso model.ISO) (string, error) {
	if !store.HoldsSHA256(iso) {
		return "", nil
	}
	var ids []string
	err := db.Model(&isoRow{}).Where("sha256 = ? AND state <> ? AND id <> ?", iso.SHA256, string(model.ISOFailed), iso.ID).
		Limit(1).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// lockISO 在事务 tx 里给 ID 为 id 的 ISO 加写锁并取出它：不存在报 ErrNotFound，Rev 不是 rev 报 ErrConflict。
func lockISO(tx *gorm.DB, id string, rev int64) (isoRow, error) {
	var row isoRow
	err := tx.Clauses(forUpdate).Where("id = ?", id).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return isoRow{}, isoNotFound(id)
	case err != nil:
		return isoRow{}, err
	case row.Rev != rev:
		return isoRow{}, staleRev("ISO "+id, row.Rev, rev)
	}
	return row, nil
}

func isoNotFound(id string) error {
	return errorf(store.ErrNotFound, "ISO %s 不存在", id)
}
