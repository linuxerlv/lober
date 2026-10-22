package pgstore

import (
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"gorm.io/gorm"
)

// Called while the referenced parent is locked, matching CreateTasks lock order.
func taskReference(tx *gorm.DB, condition string, value string) error {
	var rows []taskRow
	if err := tx.Select("id").Where(condition, value).Order(`id COLLATE "C"`).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 0 {
		return &repository.ReferencedError{References: []model.Subject{{Kind: repository.TargetTask, ID: rows[0].ID}}}
	}
	return nil
}
