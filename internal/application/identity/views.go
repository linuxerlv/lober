package identity

import (
	"context"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
)

type RoleMenuView struct {
	Role                      model.Role
	AvailableMenuIDs, MenuIDs []string
	Rev                       int64
}

func toUserView(u model.User) UserView {
	return UserView{Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Avatar: u.Avatar, Bio: u.Bio, Role: u.Role, Permissions: access.PermsOf(u.Role), Disabled: u.Disabled, MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, Rev: u.Rev, MenuIDs: []string{}}
}

func (s *Service) effectiveRoleMenus(ctx context.Context, role model.Role) (RoleMenuView, error) {
	setting, found, err := s.roleMenus.GetRoleMenus(ctx, role)
	if err != nil {
		return RoleMenuView{}, err
	}
	out := RoleMenuView{Role: role, AvailableMenuIDs: access.AvailableMenuIDs(role), MenuIDs: access.EffectiveMenuIDs(role, setting.MenuIDs, found)}
	if found {
		out.Rev = setting.Rev
	}
	return out, nil
}

func (s *Service) currentUserView(ctx context.Context, u model.User) (UserView, error) {
	menus, err := s.effectiveRoleMenus(ctx, u.Role)
	if err != nil {
		return UserView{}, err
	}
	out := toUserView(u)
	out.MenuIDs = menus.MenuIDs
	out.MenuRev = menus.Rev
	return out, nil
}
