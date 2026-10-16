package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type roleMenuRow struct {
	Role    string   `gorm:"primaryKey;check:chk_role_menu_settings_role,role IN ('viewer','operator','hook-author','hook-approver','admin')"`
	MenuIDs []string `gorm:"type:jsonb;serializer:json;not null"`
	Rev     int64    `gorm:"not null;check:chk_role_menu_settings_rev,rev >= 1"`
}

func (roleMenuRow) TableName() string { return "role_menu_settings" }
func (r roleMenuRow) settings() store.RoleMenuSettings {
	return store.RoleMenuSettings{Role: store.Role(r.Role), MenuIDs: append([]string{}, r.MenuIDs...), Rev: r.Rev}
}

func (s *Store) GetRoleMenus(ctx context.Context, role store.Role) (store.RoleMenuSettings, bool, error) {
	if !role.Valid() {
		return store.RoleMenuSettings{}, false, store.ErrInvalid
	}
	var row roleMenuRow
	err := s.read(ctx).First(&row, "role = ?", string(role)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.RoleMenuSettings{}, false, nil
	}
	if err != nil {
		return store.RoleMenuSettings{}, false, err
	}
	return row.settings(), true, nil
}

func (s *Store) SaveRoleMenus(ctx context.Context, setting store.RoleMenuSettings, a store.Audit) (store.RoleMenuSettings, error) {
	if err := store.CheckAudit(a); err != nil {
		return store.RoleMenuSettings{}, err
	}
	if err := store.CheckRoleMenus(setting); err != nil {
		return store.RoleMenuSettings{}, err
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
				return store.ErrConflict
			}
		} else {
			var current roleMenuRow
			if err := tx.Clauses(forUpdate).First(&current, "role = ?", row.Role).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return store.ErrConflict
				}
				return err
			}
			if current.Rev != setting.Rev {
				return store.ErrConflict
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
		return store.RoleMenuSettings{}, translate(err, false, nil)
	}
	return row.settings(), nil
}
