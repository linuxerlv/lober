package pgstore

import (
	"cmp"
	"context"
	"errors"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
)

type sourceRow struct {
	ISOID          string `gorm:"primaryKey"`
	Role           string `gorm:"primaryKey"`
	Kind           string `gorm:"not null;check:kind IN ('remote', 'upload', 'local')"`
	URL            string `gorm:"not null"`
	Path           string `gorm:"not null"`
	TotalSize      int64  `gorm:"not null"`
	ExpectedSHA256 string `gorm:"not null"`
	Fetched        int64  `gorm:"not null;check:fetched >= 0"`
	State          string `gorm:"not null;check:state IN ('pending', 'fetching', 'fetched', 'failed', 'cancelled')"`
	Error          string `gorm:"not null"`
	Rev            int64  `gorm:"not null;check:rev >= 1"`
	ISO            isoRow `gorm:"foreignKey:ISOID;references:ID;constraint:OnDelete:CASCADE"`
}

func (sourceRow) TableName() string { return "iso_sources" }

type rangeRow struct {
	SourceISOID string    `gorm:"column:iso_id;primaryKey"`
	SourceRole  string    `gorm:"column:role;primaryKey"`
	Start       int64     `gorm:"primaryKey;autoIncrement:false;check:start >= 0"`
	Length      int64     `gorm:"not null;check:length > 0"`
	Source      sourceRow `gorm:"foreignKey:SourceISOID,SourceRole;references:ISOID,Role;constraint:OnDelete:CASCADE"`
}

func (rangeRow) TableName() string { return "iso_upload_ranges" }
func (r sourceRow) source() store.Source {
	return store.Source{ISOID: r.ISOID, Role: r.Role, Kind: store.SourceKind(r.Kind), URL: r.URL, Path: r.Path, TotalSize: r.TotalSize, ExpectedSHA256: r.ExpectedSHA256, Fetched: r.Fetched, State: store.SourceState(r.State), Error: r.Error, Rev: r.Rev}
}
func sourceToRow(s store.Source) sourceRow {
	return sourceRow{ISOID: s.ISOID, Role: s.Role, Kind: string(s.Kind), URL: s.URL, Path: s.Path, TotalSize: s.TotalSize, ExpectedSHA256: s.ExpectedSHA256, Fetched: s.Fetched, State: string(s.State), Error: s.Error, Rev: s.Rev}
}
func optionalAudit(a store.Audit) error {
	if a == (store.Audit{}) {
		return nil
	}
	return store.CheckAudit(a)
}
func addSourceAudit(tx *gorm.DB, a store.Audit, id string) error {
	if a == (store.Audit{}) {
		return nil
	}
	return addAudits(tx, a, "iso", id)
}
func sourceQuery(db *gorm.DB, id, role string) *gorm.DB {
	return db.Where("iso_id = ? AND role = ?", id, role)
}
func lockedSource(tx *gorm.DB, id, role string) (sourceRow, error) {
	var r sourceRow
	err := sourceQuery(tx.Clauses(forUpdate), id, role).Take(&r).Error
	return r, err
}

func (s *Store) SaveSource(ctx context.Context, src store.Source, a store.Audit) (store.Source, error) {
	if err := store.CheckSource(src); err != nil {
		return store.Source{}, err
	}
	if err := optionalAudit(a); err != nil {
		return store.Source{}, err
	}
	var next sourceRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		// 所有来源写入先锁 ISO；新键并发登记也串行，不靠不存在的行锁。
		var parent isoRow
		if err := tx.Clauses(forUpdate).Where("id = ?", src.ISOID).Take(&parent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return store.ErrNotFound
			}
			return err
		}
		if model.ISOState(parent.State) == model.ISODeprecated {
			return store.ErrImmutable
		}
		old, err := lockedSource(tx, src.ISOID, src.Role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if src.Rev != 0 || src.State != store.SourcePending || src.Fetched != 0 || src.Error != "" {
				return store.ErrConflict
			}
			src.Rev = 1
			next = sourceToRow(src)
			if err := tx.Omit("ISO").Create(&next).Error; err != nil {
				return err
			}
		} else {
			if err != nil {
				return err
			}
			cur := old.source()
			if cur.Rev != src.Rev {
				return store.ErrConflict
			}
			if cur.Kind != src.Kind || cur.URL != src.URL || cur.Path != src.Path || cur.ExpectedSHA256 != src.ExpectedSHA256 {
				return store.ErrInvalid
			}
			if err := store.CheckSourceTransition(cur.State, src.State); err != nil {
				return err
			}
			retry := cur.State != store.SourcePending && cur.State != store.SourceFetching && src.State == store.SourcePending
			if retry {
				src.Fetched, src.Error = 0, ""
				if err := sourceQuery(tx, src.ISOID, src.Role).Delete(&rangeRow{}).Error; err != nil {
					return err
				}
			} else if cur.Fetched != src.Fetched && cur.State != store.SourceFetching {
				return store.ErrConflict
			}
			if cur.TotalSize != src.TotalSize && cur.State != store.SourceFetching {
				return store.ErrConflict
			}
			src.Rev = cur.Rev + 1
			next = sourceToRow(src)
			if err := sourceQuery(tx.Model(&sourceRow{}), src.ISOID, src.Role).Select("kind", "url", "path", "total_size", "expected_sha256", "fetched", "state", "error", "rev").Updates(&next).Error; err != nil {
				return err
			}
		}
		return addSourceAudit(tx, a, src.ISOID)
	})
	if err != nil {
		return store.Source{}, translate(err, false, nil)
	}
	return next.source(), nil
}
func (s *Store) GetSource(ctx context.Context, id, role string) (store.Source, bool, error) {
	if err := store.CheckSourceKey(id, role); err != nil {
		return store.Source{}, false, err
	}
	var r sourceRow
	err := sourceQuery(s.read(ctx), id, role).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.Source{}, false, nil
	}
	return r.source(), err == nil, err
}
func (s *Store) SourcesOf(ctx context.Context, id string) ([]store.Source, error) {
	if err := store.CheckID(id); err != nil {
		return nil, err
	}
	return listSources(s.read(ctx).Where("iso_id = ?", id).Order(`role COLLATE "C"`))
}
func (s *Store) ListUnfinishedSources(ctx context.Context) ([]store.Source, error) {
	return listSources(s.read(ctx).Where("state IN ?", []string{"pending", "fetching"}).Order(`iso_id COLLATE "C", role COLLATE "C"`))
}
func listSources(db *gorm.DB) ([]store.Source, error) {
	var rows []sourceRow
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]store.Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.source())
	}
	return out, nil
}
func (s *Store) DeleteSource(ctx context.Context, id, role string, a store.Audit) error {
	if err := store.CheckSourceKey(id, role); err != nil {
		return err
	}
	if err := optionalAudit(a); err != nil {
		return err
	}
	return translate(s.write(ctx, func(tx *gorm.DB) error {
		_, err := lockedSource(tx, id, role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := sourceQuery(tx, id, role).Delete(&sourceRow{}).Error; err != nil {
			return err
		}
		return addSourceAudit(tx, a, id)
	}), true, nil)
}
func (s *Store) SaveRange(ctx context.Context, id, role string, start, length int64) error {
	if err := store.CheckSourceKey(id, role); err != nil {
		return err
	}
	if err := store.CheckRange(start, length); err != nil {
		return err
	}
	return translate(s.write(ctx, func(tx *gorm.DB) error {
		src, err := lockedSource(tx, id, role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		if src.State != "fetching" {
			return store.ErrConflict
		}
		if src.TotalSize > 0 && start+length > src.TotalSize {
			return store.ErrInvalid
		}
		r := rangeRow{SourceISOID: id, SourceRole: role, Start: start, Length: length}
		// 重传较短的分片不能撤销之前确认的后半段。
		if err := tx.Omit("Source").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "iso_id"}, {Name: "role"}, {Name: "start"}}, DoUpdates: clause.Assignments(map[string]any{"length": gorm.Expr("GREATEST(iso_upload_ranges.length, EXCLUDED.length)")})}).Create(&r).Error; err != nil {
			return err
		}
		ranges, err := readRanges(tx, id, role)
		if err != nil {
			return err
		}
		var total int64
		for _, r := range ranges {
			total += r.Length
		}
		return sourceQuery(tx.Model(&sourceRow{}), id, role).Updates(map[string]any{"fetched": total, "rev": src.Rev + 1}).Error
	}), false, nil)
}
func (s *Store) RangesOf(ctx context.Context, id, role string) ([]store.Range, error) {
	if err := store.CheckSourceKey(id, role); err != nil {
		return nil, err
	}
	return readRanges(s.read(ctx), id, role)
}
func readRanges(db *gorm.DB, id, role string) ([]store.Range, error) {
	var rows []rangeRow
	if err := sourceQuery(db, id, role).Order("start").Find(&rows).Error; err != nil {
		return nil, err
	}
	ranges := make([]store.Range, 0, len(rows))
	for _, r := range rows {
		ranges = append(ranges, store.Range{Start: r.Start, Length: r.Length})
	}
	slices.SortFunc(ranges, func(a, b store.Range) int { return cmp.Compare(a.Start, b.Start) })
	out := make([]store.Range, 0, len(ranges))
	for _, r := range ranges {
		if len(out) > 0 && r.Start <= out[len(out)-1].Start+out[len(out)-1].Length {
			last := &out[len(out)-1]
			last.Length = max(last.Start+last.Length, r.Start+r.Length) - last.Start
		} else {
			out = append(out, r)
		}
	}
	return out, nil
}
