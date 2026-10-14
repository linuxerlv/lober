package pgstore

import (
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
)

// Called while the referenced parent is locked, matching CreateTasks lock order.
func taskReference(tx *gorm.DB, condition string, value string) error {
	var rows []taskRow
	if err := tx.Select("id").Where(condition, value).Order(`id COLLATE "C"`).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 0 {
		return &store.ReferencedError{References: []model.Subject{{Kind: store.TargetTask, ID: rows[0].ID}}}
	}
	return nil
}
