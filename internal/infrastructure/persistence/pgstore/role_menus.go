package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"slices"

	"github.com/lober/lober/internal/domain/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type roleMenuRow struct {
	Role    string   `gorm:"primaryKey;check:chk_role_menu_settings_role,role IN ('viewer','operator','hook-author','hook-approver','admin')"`
	MenuIDs []string `gorm:"type:jsonb;serializer:json;not null"`
	Rev     int64    `gorm:"not null;check:chk_role_menu_settings_rev,rev >= 1"`
}

func (roleMenuRow) TableName() string { return "role_menu_settings" }
func (r roleMenuRow) settings() repository.RoleMenuSettings {
	return repository.RoleMenuSettings{Role: model.Role(r.Role), MenuIDs: append([]string{}, r.MenuIDs...), Rev: r.Rev}
}

func (s *Store) GetRoleMenus(ctx context.Context, role model.Role) (repository.RoleMenuSettings, bool, error) {
	if !role.Valid() {
		return repository.RoleMenuSettings{}, false, repository.ErrInvalid
	}
	var row roleMenuRow
	err := s.read(ctx).First(&row, "role = ?", string(role)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.RoleMenuSettings{}, false, nil
	}
	if err != nil {
		return repository.RoleMenuSettings{}, false, err
	}
	return row.settings(), true, nil
}

func (s *Store) SaveRoleMenus(ctx context.Context, setting repository.RoleMenuSettings, a repository.Audit) (repository.RoleMenuSettings, error) {
	if err := repository.CheckAudit(a); err != nil {
		return repository.RoleMenuSettings{}, err
	}
	if err := repository.CheckRoleMenus(setting); err != nil {
		return repository.RoleMenuSettings{}, err
	}
	row := roleMenuRow{Role: string(setting.Role), MenuIDs: append([]string{}, setting.MenuIDs...), Rev: setting.Rev + 1}
	err := s.write(ctx, func(tx *gorm.DB) error {
		var before []string
		if setting.Rev == 0 {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return repository.ErrConflict
			}
		} else {
			var current roleMenuRow
			if err := tx.Clauses(forUpdate).First(&current, "role = ?", row.Role).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return repository.ErrConflict
				}
				return err
			}
			if current.Rev != setting.Rev {
				return repository.ErrConflict
			}
			before = slices.Clone(current.MenuIDs)
			if err := tx.Model(&current).Select("menu_ids", "rev").Updates(&row).Error; err != nil {
				return err
			}
		}
		detail, err := json.Marshal(struct {
			Before []string `json:"before"`
			After  []string `json:"after"`
		}{before, row.MenuIDs})
		if err != nil {
			return err
		}
		a.Detail = string(detail)
		return addAudits(tx, a, "role", row.Role)
	})
	if err != nil {
		return repository.RoleMenuSettings{}, translate(err, false, nil)
	}
	return row.settings(), nil
}
