package pgstore

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"gorm.io/gorm"
)

func (s *Store) UpdateInventory(ctx context.Context, sn string, decide func(model.Host, bool) (repository.InventoryChange, error)) (model.Host, error) {
	if err := repository.CheckSN(sn); err != nil {
		return model.Host{}, err
	}
	var result model.Host
	err := s.write(ctx, func(tx *gorm.DB) error {
		rows, err := lockHosts(tx, []string{sn})
		if err != nil {
			return err
		}
		row, ok := rows[sn]
		if !ok {
			return hostNotFound(sn)
		}
		current, err := row.host()
		if err != nil {
			return err
		}
		open, err := openTasksOf(tx, []string{sn})
		if err != nil {
			return err
		}
		change, err := decide(current, open[sn])
		if err != nil {
			return err
		}
		result = current
		if change.Attrs == nil {
			return nil
		}
		if err := repository.CheckAudit(change.Audit); err != nil {
			return err
		}
		result.Attrs = change.Attrs
		result.Rev++
		if err := repository.CheckHosts([]model.Host{result}); err != nil {
			return err
		}
		row, err = toHostRow(result)
		if err != nil {
			return err
		}
		if err := tx.Model(&hostRow{SN: sn}).Select("attrs", "rev").Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, change.Audit, "host", sn)
	})
	if err != nil {
		return model.Host{}, translate(err, false, nil)
	}
	return result, nil
}
