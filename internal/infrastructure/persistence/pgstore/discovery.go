package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type discoveryRow struct {
	MAC           string `gorm:"primaryKey"`
	SN            string `gorm:"not null"`
	Arch          string `gorm:"not null"`
	Firmware      string `gorm:"not null"`
	FirstSeenAt   int64  `gorm:"not null"`
	LastSeenAt    int64  `gorm:"not null"`
	Attempt       string `gorm:"not null"`
	ExpiresAt     int64  `gorm:"not null"`
	Reason        string `gorm:"not null"`
	ReportAt      int64  `gorm:"not null"`
	ReportAttempt string `gorm:"not null"`
	ReportDigest  string `gorm:"not null"`
	Report        string `gorm:"type:jsonb;not null"`

	SystemModel string `gorm:"not null;default:''"`

	RegisteredSN   *string  `gorm:"index:idx_discoveries_registered_sn"`
	RegisteredHost *hostRow `gorm:"foreignKey:RegisteredSN;references:SN;constraint:OnDelete:SET NULL"`
}

func (discoveryRow) TableName() string { return "host_discoveries" }

func lockDiscovery(tx *gorm.DB, mac string) (discoveryRow, error) {
	var row discoveryRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("mac = ?", mac).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = repository.ErrNotFound
	}
	return row, err
}

func (s *Store) ObserveDiscovery(ctx context.Context, a discovery.Attempt) (discovery.Attempt, error) {
	if discovery.MAC(a.MAC) != a.MAC || a.MAC == "" || !a.Arch.Valid() || !a.Firmware.Valid() {
		return discovery.Attempt{}, repository.ErrInvalid
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		seed := discoveryRow{MAC: a.MAC, FirstSeenAt: a.At, Report: "null"}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		row, err := lockDiscovery(tx, a.MAC)
		if err != nil {
			return err
		}
		// 已确认的网口正常引导时无需再运行采集镜像。
		if row.RegisteredSN != nil {
			a.ID = ""
			return nil
		}
		if a.SN == "" {
			a.SN = row.SN
		}
		if row.Attempt != "" && a.ID != "" && row.ExpiresAt > a.At && row.ReportAttempt != row.Attempt &&
			row.Arch == string(a.Arch) && row.Firmware == string(a.Firmware) && row.SN == a.SN {
			a.ID, a.ExpiresAt = row.Attempt, row.ExpiresAt
		}
		return tx.Model(&row).Updates(map[string]any{
			"sn": a.SN, "arch": a.Arch, "firmware": a.Firmware, "last_seen_at": max(row.LastSeenAt, a.At),
			"attempt": a.ID, "expires_at": a.ExpiresAt, "reason": a.Reason,
		}).Error
	})
	return a, err
}

func (s *Store) SaveDiscoveryReport(ctx context.Context, mac, id string, at int64, report discovery.Report) error {
	body, err := encodeJSON(report)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(digest[:])
	return s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockDiscovery(tx, mac)
		if errors.Is(err, repository.ErrNotFound) {
			return discovery.ErrToken
		}
		if err != nil {
			return err
		}
		if id == "" || row.Attempt != id || at >= row.ExpiresAt {
			return discovery.ErrToken
		}
		if row.ReportAttempt == id {
			if row.ReportDigest == hash {
				return nil
			}
			return discovery.ErrToken
		}
		return tx.Model(&row).Updates(map[string]any{"report": body, "report_at": at, "report_attempt": id, "report_digest": hash, "system_model": systemModel(report)}).Error
	})
}

func systemModel(report discovery.Report) string {
	for _, section := range report.Sections {
		if section.Key == "system" && len(section.Items) > 0 {
			fields := section.Items[0].Fields
			makeName, modelName := strings.TrimSpace(fields["厂商"]), strings.TrimSpace(fields["型号"])
			if makeName == modelName || strings.HasPrefix(modelName, makeName+" ") {
				return modelName
			}
			return strings.TrimSpace(makeName + " " + modelName)
		}
	}
	return ""
}

func (s *Store) ListDiscoveries(ctx context.Context, after, registeredSN string, limit int, at int64) ([]discovery.Record, error) {
	var rows []discoveryRow
	query := s.db.WithContext(ctx).Omit("Report")
	if registeredSN != "" {
		query = registeredReports(query, registeredSN)
		limit = 1
	} else {
		query = query.Where("mac > ?", after).Order("mac")
	}
	if err := query.Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]discovery.Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, row.summary(at))
	}
	return records, nil
}

func registeredReports(q *gorm.DB, sn string) *gorm.DB {
	return q.Where("registered_sn = ?", sn).Order("report_at DESC, last_seen_at DESC, mac")
}

// 同秒上报时优先采用已登记主机凭据提交的报告。
func (s *Store) LatestDiscoveryReport(ctx context.Context, sn string) (*discovery.Report, int64, error) {
	var row struct {
		Report   string
		ReportAt int64
	}
	result := s.db.WithContext(ctx).Raw(`SELECT report, report_at FROM (
		SELECT report, report_at, 1 AS priority, report_at AS seen, '' AS mac FROM host_hardware_reports WHERE sn = ?
		UNION ALL
		SELECT report, report_at, 0, last_seen_at, mac FROM host_discoveries WHERE registered_sn = ? AND report_at > 0
	) AS snapshots ORDER BY report_at DESC, priority DESC, seen DESC, mac LIMIT 1`, sn, sn).Scan(&row)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, 0, result.Error
	}
	var report discovery.Report
	if err := decodeJSON(row.Report, &report); err != nil {
		return nil, 0, err
	}
	return &report, row.ReportAt, nil
}

func (s *Store) GetDiscovery(ctx context.Context, mac string, at int64) (discovery.Record, error) {
	var row discoveryRow
	err := s.db.WithContext(ctx).Where("mac = ?", mac).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return discovery.Record{}, repository.ErrNotFound
	}
	if err != nil {
		return discovery.Record{}, err
	}
	record := row.summary(at)
	if row.ReportAt != 0 {
		if err := decodeJSON(row.Report, &record.Report); err != nil {
			return discovery.Record{}, err
		}
	}
	return record, nil
}

func (row discoveryRow) summary(at int64) discovery.Record {
	record := discovery.Record{MAC: row.MAC, SN: row.SN, Arch: catalog.Arch(row.Arch), Firmware: catalog.Firmware(row.Firmware),
		FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, Reason: row.Reason, ReportAt: row.ReportAt, SystemModel: row.SystemModel}
	if row.RegisteredSN != nil {
		record.RegisteredSN = *row.RegisteredSN
	}
	switch {
	case row.Attempt != "" && row.ReportAttempt == row.Attempt:
		record.Status = "received"
	case row.Attempt == "":
		record.Status = "unsupported"
	case at >= row.ExpiresAt:
		record.Status = "timed_out"
	default:
		record.Status = "waiting"
	}
	return record
}

func (s *Store) RegisteredDiscoverySN(ctx context.Context, mac string) (observed, registered string, err error) {
	var row discoveryRow
	err = s.db.WithContext(ctx).Select("sn", "registered_sn").Where("mac = ?", mac).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if row.RegisteredSN == nil {
		return "", "", nil
	}
	return row.SN, *row.RegisteredSN, nil
}

func (s *Store) RegisterDiscovery(ctx context.Context, mac, sn string, a repository.Audit) (model.Host, error) {
	if err := repository.CheckAudit(a); err != nil {
		return model.Host{}, err
	}
	var host model.Host
	err := s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockDiscovery(tx, mac)
		if err != nil {
			return err
		}
		if row.RegisteredSN != nil {
			if *row.RegisteredSN != sn {
				return repository.ErrConflict
			}
			var existing hostRow
			err := tx.Where("sn = ?", sn).Take(&existing).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrConflict
			}
			if err != nil {
				return err
			}
			host, err = existing.host()
			return err
		}
		host = model.Host{SN: sn, Arch: catalog.Arch(row.Arch), Firmware: catalog.Firmware(row.Firmware), Rev: 1}
		if err := repository.CheckHosts([]model.Host{host}); err != nil {
			return err
		}
		created, err := toHostRow(host)
		if err != nil {
			return err
		}
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Update("registered_sn", sn).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "host", sn)
	})
	if err != nil {
		return model.Host{}, translate(err, false, func(string) string { return sn })
	}
	return host, nil
}
