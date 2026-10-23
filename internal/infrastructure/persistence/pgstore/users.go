package pgstore

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"gorm.io/gorm"
)

type userRow struct {
	Username           string `gorm:"primaryKey"`
	DisplayName        string `gorm:"not null"`
	Email              string `gorm:"not null;default:''"`
	Avatar             string `gorm:"not null;default:''"`
	Bio                string `gorm:"not null;default:''"`
	Role               string `gorm:"not null;check:chk_users_role,role IN ('viewer','operator','hook-author','hook-approver','admin')"`
	Password           string `gorm:"not null"`
	Disabled           bool   `gorm:"not null"`
	MustChangePassword bool   `gorm:"not null;default:false"`
	CreatedAt          int64  `gorm:"not null;check:chk_users_created_at,created_at > 0"`
	Rev                int64  `gorm:"not null;check:chk_users_rev,rev >= 1"`
}

func (userRow) TableName() string { return "users" }
func toUserRow(u model.User) userRow {
	return userRow{Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Avatar: u.Avatar, Bio: u.Bio, Role: string(u.Role), Password: u.Password, Disabled: u.Disabled, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, Rev: u.Rev}
}
func (r userRow) user() model.User {
	return model.User{Username: r.Username, DisplayName: r.DisplayName, Email: r.Email, Avatar: r.Avatar, Bio: r.Bio, Role: model.Role(r.Role), Password: r.Password, Disabled: r.Disabled, MustChangePassword: r.MustChangePassword, CreatedAt: r.CreatedAt, Rev: r.Rev}
}

var userColumns = []string{"display_name", "email", "avatar", "bio", "role", "password", "disabled", "must_change_password", "rev"}

func (s *Store) CreateUser(ctx context.Context, u model.User, a repository.Audit) (model.User, error) {
	if err := repository.CheckAudit(a); err != nil {
		return model.User{}, err
	}
	if err := repository.CheckUser(u); err != nil {
		return model.User{}, err
	}
	u.Rev = 1
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, model.UserSubjectKind, u.Username)
	})
	if err != nil {
		return model.User{}, translate(err, false, func(string) string { return u.Username })
	}
	return row.user(), nil
}
func (s *Store) UpdateUser(ctx context.Context, u model.User, a repository.Audit) (model.User, error) {
	if err := repository.CheckAudit(a); err != nil {
		return model.User{}, err
	}
	if err := repository.CheckUser(u); err != nil {
		return model.User{}, err
	}
	if u.Rev < 1 {
		return model.User{}, repository.ErrInvalid
	}
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		var current userRow
		// Acquire before row locks. This also serializes concurrent removals
		// of distinct administrators and conflicts with account creation.
		if err := tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if err := tx.Clauses(forUpdate).First(&current, "username = ?", u.Username).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Rev != u.Rev {
			return staleRev("user", current.Rev, u.Rev)
		}
		if current.Role == string(model.RoleAdmin) && !current.Disabled && (u.Role != model.RoleAdmin || u.Disabled) {
			var enabled int64
			if err := tx.Model(&userRow{}).Where("role = ? AND disabled = false", string(model.RoleAdmin)).Count(&enabled).Error; err != nil {
				return err
			}
			if enabled <= 1 {
				return repository.ErrLastAdmin
			}
		}
		row.CreatedAt = current.CreatedAt
		row.Rev = current.Rev + 1
		if err := tx.Model(&userRow{Username: u.Username}).Select(userColumns).Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, model.UserSubjectKind, u.Username)
	})
	if err != nil {
		return model.User{}, translate(err, false, nil)
	}
	return row.user(), nil
}
func (s *Store) GetUser(ctx context.Context, username string) (model.User, error) {
	if !storable(username) {
		return model.User{}, repository.ErrNotFound
	}
	var row userRow
	if err := s.read(ctx).First(&row, "username = ?", username).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, repository.ErrNotFound
		}
		return model.User{}, err
	}
	return row.user(), nil
}
func (s *Store) ListUsers(ctx context.Context, q repository.UserQuery) (repository.Page[model.User], error) {
	limit, err := repository.CheckPageQuery(q.Cursor, q.Limit, q.Page)
	if err != nil {
		return repository.Page[model.User]{}, err
	}
	var after string
	if q.Cursor != "" {
		if err := repository.DecodeCursor(q.Cursor, "users", &after); err != nil {
			return repository.Page[model.User]{}, err
		}
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`username COLLATE "C" > ?`, after)
	}
	db, total, err := numberedQuery[userRow](db, q.Page, limit)
	if err != nil {
		return repository.Page[model.User]{}, err
	}
	var rows []userRow
	if err := db.Order(`username COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return repository.Page[model.User]{}, err
	}
	return page(rows, limit, func(r userRow) (model.User, error) { return r.user(), nil }, func(r userRow) string { return repository.EncodeCursor("users", r.Username) }, total)
}
