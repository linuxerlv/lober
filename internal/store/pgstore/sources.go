package pgstore

import (
	"cmp"
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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
func (r sourceRow) source() model.Source {
	return model.Source{ISOID: r.ISOID, Role: r.Role, Kind: model.SourceKind(r.Kind), URL: r.URL, Path: r.Path, TotalSize: r.TotalSize, ExpectedSHA256: r.ExpectedSHA256, Fetched: r.Fetched, State: model.SourceState(r.State), Error: r.Error, Rev: r.Rev}
}
func sourceToRow(s model.Source) sourceRow {
	return sourceRow{ISOID: s.ISOID, Role: s.Role, Kind: string(s.Kind), URL: s.URL, Path: s.Path, TotalSize: s.TotalSize, ExpectedSHA256: s.ExpectedSHA256, Fetched: s.Fetched, State: string(s.State), Error: s.Error, Rev: s.Rev}
}
func optionalAudit(a repository.Audit) error {
	if a == (repository.Audit{}) {
		return nil
	}
	return repository.CheckAudit(a)
}
func addSourceAudit(tx *gorm.DB, a repository.Audit, id string) error {
	if a == (repository.Audit{}) {
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

func (s *Store) SaveSource(ctx context.Context, src model.Source, a repository.Audit) (model.Source, error) {
	return s.saveSource(ctx, src, a, 0)
}

func (s *Store) SaveSourceAndAcquire(ctx context.Context, src model.Source, isoRev int64, a repository.Audit) (model.Source, error) {
	if isoRev < 1 {
		return model.Source{}, repository.ErrInvalid
	}
	if err := repository.CheckAudit(a); err != nil {
		return model.Source{}, err
	}
	return s.saveSource(ctx, src, a, isoRev)
}

func (s *Store) saveSource(ctx context.Context, src model.Source, a repository.Audit, acquireRev int64) (model.Source, error) {
	if err := repository.CheckSource(src); err != nil {
		return model.Source{}, err
	}
	if err := optionalAudit(a); err != nil {
		return model.Source{}, err
	}
	var next sourceRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		// 所有来源写入先锁 ISO；新键并发登记也串行，不靠不存在的行锁。
		var parent isoRow
		if err := tx.Clauses(forUpdate).Where("id = ?", src.ISOID).Take(&parent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if model.ISOState(parent.State) == model.ISODeprecated {
			return repository.ErrImmutable
		}
		if acquireRev != 0 {
			if parent.Rev != acquireRev || model.ISOState(parent.State) != model.ISOFailed {
				return repository.ErrConflict
			}
			sources, err := listSources(tx.Where("iso_id = ?", src.ISOID))
			if err != nil {
				return err
			}
			mainFetched, extraFetched := false, false
			extra, needsExtra := catalog.ExtraSourceOf(catalog.Series(parent.Series))
			for _, other := range sources {
				if other.Role == "" && other.State == model.SourceFetched {
					mainFetched = true
				}
				if needsExtra && other.Role == extra.Role && other.State == model.SourceFetched {
					extraFetched = true
				}
				if src.Rev != 0 && other.State != model.SourceFetched {
					return repository.ErrConflict
				}
			}
			if !mainFetched {
				return repository.ErrConflict
			}
			if src.Rev == 0 {
				if !needsExtra || src.Role != extra.Role {
					return repository.ErrInvalid
				}
			} else if src.Role != "" || src.State != model.SourceFetched || needsExtra && !extraFetched {
				return repository.ErrConflict
			}
		}
		old, err := lockedSource(tx, src.ISOID, src.Role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if src.Rev != 0 || src.State != model.SourcePending || src.Fetched != 0 || src.Error != "" {
				return repository.ErrConflict
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
				return repository.ErrConflict
			}
			if cur.Kind != src.Kind || cur.URL != src.URL || cur.Path != src.Path || cur.ExpectedSHA256 != src.ExpectedSHA256 {
				return repository.ErrInvalid
			}
			if err := repository.CheckSourceTransition(cur.State, src.State); err != nil {
				return err
			}
			retry := cur.State != model.SourcePending && cur.State != model.SourceFetching && src.State == model.SourcePending
			if retry {
				src.Fetched, src.Error = 0, ""
				if err := sourceQuery(tx, src.ISOID, src.Role).Delete(&rangeRow{}).Error; err != nil {
					return err
				}
			} else if cur.Fetched != src.Fetched && cur.State != model.SourceFetching {
				return repository.ErrConflict
			}
			if cur.TotalSize != src.TotalSize && cur.State != model.SourceFetching {
				return repository.ErrConflict
			}
			src.Rev = cur.Rev + 1
			next = sourceToRow(src)
			if err := sourceQuery(tx.Model(&sourceRow{}), src.ISOID, src.Role).Select("kind", "url", "path", "total_size", "expected_sha256", "fetched", "state", "error", "rev").Updates(&next).Error; err != nil {
				return err
			}
		}
		if acquireRev != 0 {
			if err := tx.Model(&isoRow{}).Where("id = ?", src.ISOID).Updates(map[string]any{"state": string(model.ISOAcquiring), "rev": parent.Rev + 1}).Error; err != nil {
				return err
			}
		}
		return addSourceAudit(tx, a, src.ISOID)
	})
	if err != nil {
		return model.Source{}, translate(err, false, nil)
	}
	return next.source(), nil
}
func (s *Store) GetSource(ctx context.Context, id, role string) (model.Source, bool, error) {
	if err := repository.CheckSourceKey(id, role); err != nil {
		return model.Source{}, false, err
	}
	var r sourceRow
	err := sourceQuery(s.read(ctx), id, role).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Source{}, false, nil
	}
	return r.source(), err == nil, err
}
func (s *Store) SourcesOf(ctx context.Context, id string) ([]model.Source, error) {
	if err := repository.CheckID(id); err != nil {
		return nil, err
	}
	return listSources(s.read(ctx).Where("iso_id = ?", id).Order(`role COLLATE "C"`))
}
func (s *Store) ListUnfinishedSources(ctx context.Context) ([]model.Source, error) {
	return listSources(s.read(ctx).Where("state IN ?", []string{"pending", "fetching"}).Order(`iso_id COLLATE "C", role COLLATE "C"`))
}
func listSources(db *gorm.DB) ([]model.Source, error) {
	var rows []sourceRow
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]model.Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.source())
	}
	return out, nil
}
func (s *Store) DeleteSource(ctx context.Context, id, role string, a repository.Audit) error {
	if err := repository.CheckSourceKey(id, role); err != nil {
		return err
	}
	if err := optionalAudit(a); err != nil {
		return err
	}
	return translate(s.write(ctx, func(tx *gorm.DB) error {
		_, err := lockedSource(tx, id, role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return repository.ErrNotFound
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
	if err := repository.CheckSourceKey(id, role); err != nil {
		return err
	}
	if err := repository.CheckRange(start, length); err != nil {
		return err
	}
	return translate(s.write(ctx, func(tx *gorm.DB) error {
		src, err := lockedSource(tx, id, role)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return repository.ErrNotFound
		}
		if err != nil {
			return err
		}
		if src.State != "fetching" {
			return repository.ErrConflict
		}
		if src.TotalSize > 0 && start+length > src.TotalSize {
			return repository.ErrInvalid
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
func (s *Store) RangesOf(ctx context.Context, id, role string) ([]model.Range, error) {
	if err := repository.CheckSourceKey(id, role); err != nil {
		return nil, err
	}
	return readRanges(s.read(ctx), id, role)
}
func readRanges(db *gorm.DB, id, role string) ([]model.Range, error) {
	var rows []rangeRow
	if err := sourceQuery(db, id, role).Order("start").Find(&rows).Error; err != nil {
		return nil, err
	}
	ranges := make([]model.Range, 0, len(rows))
	for _, r := range rows {
		ranges = append(ranges, model.Range{Start: r.Start, Length: r.Length})
	}
	slices.SortFunc(ranges, func(a, b model.Range) int { return cmp.Compare(a.Start, b.Start) })
	out := make([]model.Range, 0, len(ranges))
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
