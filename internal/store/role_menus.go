package store

import (
	"context"
	"math"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// RoleMenuSettings uses Rev=0 only when creating the first explicit setting.
// An explicit empty MenuIDs slice differs from an absent setting.
type RoleMenuSettings struct {
	Role    Role
	MenuIDs []string
	Rev     int64
}

type RoleMenus interface {
	GetRoleMenus(context.Context, Role) (RoleMenuSettings, bool, error)
	SaveRoleMenus(context.Context, RoleMenuSettings, Audit) (RoleMenuSettings, error)
}

func CheckRoleMenus(s RoleMenuSettings) error {
	if !s.Role.Valid() || s.Rev < 0 || s.Rev == math.MaxInt64 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(s.MenuIDs))
	for _, id := range s.MenuIDs {
		if !model.ValidIdent(strings.ReplaceAll(id, "-", "_")) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
