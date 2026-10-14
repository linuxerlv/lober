package pgstore

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
)

type userRow struct {
	Username    string `gorm:"primaryKey"`
	DisplayName string `gorm:"not null"`
	Role        string `gorm:"not null;check:chk_users_role,role IN ('viewer','operator','hook-author','hook-approver','admin')"`
	Password    string `gorm:"not null"`
	Disabled    bool   `gorm:"not null"`
	CreatedAt   int64  `gorm:"not null;check:chk_users_created_at,created_at > 0"`
	Rev         int64  `gorm:"not null;check:chk_users_rev,rev >= 1"`
}

func (userRow) TableName() string { return "users" }
func toUserRow(u store.User) userRow {
	return userRow{Username: u.Username, DisplayName: u.DisplayName, Role: string(u.Role), Password: u.Password, Disabled: u.Disabled, CreatedAt: u.CreatedAt, Rev: u.Rev}
}
func (r userRow) user() store.User {
	return store.User{Username: r.Username, DisplayName: r.DisplayName, Role: store.Role(r.Role), Password: r.Password, Disabled: r.Disabled, CreatedAt: r.CreatedAt, Rev: r.Rev}
}

var userColumns = []string{"display_name", "role", "password", "disabled", "rev"}

func (s *Store) CreateUser(ctx context.Context, u store.User, a store.Audit) (store.User, error) {
	if err := store.CheckAudit(a); err != nil {
		return store.User{}, err
	}
	if err := store.CheckUser(u); err != nil {
		return store.User{}, err
	}
	u.Rev = 1
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, store.UserSubjectKind, u.Username)
	})
	if err != nil {
		return store.User{}, translate(err, false, func(string) string { return u.Username })
	}
	return row.user(), nil
}
func (s *Store) UpdateUser(ctx context.Context, u store.User, a store.Audit) (store.User, error) {
	if err := store.CheckAudit(a); err != nil {
		return store.User{}, err
	}
	if err := store.CheckUser(u); err != nil {
		return store.User{}, err
	}
	if u.Rev < 1 {
		return store.User{}, store.ErrInvalid
	}
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		var current userRow
		if err := tx.Clauses(forUpdate).First(&current, "username = ?", u.Username).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return store.ErrNotFound
			}
			return err
		}
		if current.Rev != u.Rev {
			return staleRev("user", current.Rev, u.Rev)
		}
		row.CreatedAt = current.CreatedAt
		row.Rev = current.Rev + 1
		if err := tx.Model(&userRow{Username: u.Username}).Select(userColumns).Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, store.UserSubjectKind, u.Username)
	})
	if err != nil {
		return store.User{}, translate(err, false, nil)
	}
	return row.user(), nil
}
func (s *Store) GetUser(ctx context.Context, username string) (store.User, error) {
	if !storable(username) {
		return store.User{}, store.ErrNotFound
	}
	var row userRow
	if err := s.read(ctx).First(&row, "username = ?", username).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.User{}, store.ErrNotFound
		}
		return store.User{}, err
	}
	return row.user(), nil
}
func (s *Store) ListUsers(ctx context.Context, q store.UserQuery) (store.Page[store.User], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[store.User]{}, err
	}
	var after string
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "users", &after); err != nil {
			return store.Page[store.User]{}, err
		}
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`username COLLATE "C" > ?`, after)
	}
	var rows []userRow
	if err := db.Order(`username COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[store.User]{}, err
	}
	return page(rows, limit, func(r userRow) (store.User, error) { return r.user(), nil }, func(r userRow) string { return store.EncodeCursor("users", r.Username) })
}
