package identity

import (
	"context"

	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

var _ Registry = (*Service)(nil)

func (s *Service) ListRoleMenus(ctx context.Context, p Principal) (MenuList, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage}); err != nil {
		return MenuList{}, err
	}
	catalog := access.MenuCatalog()
	out := MenuList{Menus: make([]MenuDefinition, len(catalog)), Roles: []RoleMenuView{}}
	for i, m := range catalog {
		out.Menus[i] = MenuDefinition{ID: m.ID, Title: m.Title, ParentID: m.ParentID, Permission: m.Permission, Required: m.Required}
	}
	for _, role := range model.Roles() {
		item, err := s.effectiveRoleMenus(ctx, role)
		if err != nil {
			return MenuList{}, err
		}
		out.Roles = append(out.Roles, item)
	}
	return out, nil
}

func (s *Service) SaveRoleMenus(ctx context.Context, p Principal, role model.Role, cmd SaveMenusCommand) (RoleMenuView, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage}); err != nil {
		return RoleMenuView{}, err
	}
	setting := repository.RoleMenuSettings{Role: role, MenuIDs: cmd.MenuIDs, Rev: cmd.Rev}
	if cmd.MenuIDs == nil || repository.CheckRoleMenus(setting) != nil {
		return RoleMenuView{}, ErrBadRequest
	}
	ids, ok := access.NormalizeMenuIDs(role, cmd.MenuIDs)
	if !ok {
		return RoleMenuView{}, ErrBadRequest
	}
	setting.MenuIDs = ids
	saved, err := s.roleMenus.SaveRoleMenus(ctx, setting, s.accountAudit(p, "role.menus.update"))
	if err != nil {
		return RoleMenuView{}, err
	}
	return RoleMenuView{Role: role, AvailableMenuIDs: access.AvailableMenuIDs(role), MenuIDs: saved.MenuIDs, Rev: saved.Rev}, nil
}
