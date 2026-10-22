package console

import (
	"context"
	"errors"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

type menuDefinition struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ParentID   string `json:"parentId"`
	Permission string `json:"permission"`
	Required   bool   `json:"required"`
}

var menuCatalog = []menuDefinition{
	{ID: "tasks-group", Title: "任务"},
	{ID: "tasks", Title: "任务列表", ParentID: "tasks-group", Permission: PermTaskRead},
	{ID: "hosts-group", Title: "主机清单"},
	{ID: "hosts", Title: "主机清单", ParentID: "hosts-group", Permission: PermHostRead},
	{ID: "templates-group", Title: "模板"},
	{ID: "templates", Title: "模板", ParentID: "templates-group", Permission: PermTemplateRead},
	{ID: "hooks-group", Title: "hook"},
	{ID: "hooks", Title: "hook 管理与审批", ParentID: "hooks-group", Permission: PermHookRead},
	{ID: "isos-group", Title: "制品集"},
	{ID: "isos", Title: "制品集导入", ParentID: "isos-group", Permission: PermISORead},
	{ID: "tools-group", Title: "工具"},
	{ID: "tools", Title: "磁盘策略 dry-run", ParentID: "tools-group", Permission: PermTemplateRead},
	{ID: "reports-group", Title: "报表与审计"},
	{ID: "reports", Title: "报表与审计", ParentID: "reports-group", Permission: PermAuditRead},
	{ID: "access", Title: "权限管理"},
	{ID: "users", Title: "账号管理", ParentID: "access", Permission: PermUserManage},
	{ID: "role-menus", Title: "角色与菜单", ParentID: "access", Permission: PermUserManage, Required: true},
}

type roleMenuResponse struct {
	Role             store.Role `json:"role"`
	AvailableMenuIDs []string   `json:"availableMenuIds"`
	MenuIDs          []string   `json:"menuIds"`
	Rev              int64      `json:"rev"`
}
type roleMenuListResponse struct {
	Menus []menuDefinition   `json:"menus"`
	Roles []roleMenuResponse `json:"roles"`
}
type roleMenuRequest struct {
	MenuIDs []string `json:"menuIds" binding:"required"`
	Rev     *int64   `json:"rev" binding:"required"`
}

func (s *server) menuRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/roles/menus", perm: PermUserManage, tag: "access", summary: "Read role menu configuration", resp: roleMenuListResponse{}, handle: s.listRoleMenus},
		{method: "PUT", path: "/console/v1/roles/:role/menus", perm: PermUserManage, audit: "role.menus.update", tag: "access", summary: "Save role menus with revision checking", req: roleMenuRequest{}, resp: roleMenuResponse{}, handle: s.saveRoleMenus},
	}
}

func availableMenus(role store.Role) []string {
	out := []string{}
	permissions := PermsOf(role)
	for _, m := range menuCatalog {
		// Folders carry hierarchy only; only second-level pages can be granted.
		if m.ParentID != "" && slices.Contains(permissions, m.Permission) {
			out = append(out, m.ID)
		}
	}
	return out
}

func (s *server) roleMenus(ctx context.Context, role store.Role) (roleMenuResponse, error) {
	if s.deps.RoleMenus == nil {
		return roleMenuResponse{}, errors.New("role menu storage unavailable")
	}
	setting, found, err := s.deps.RoleMenus.GetRoleMenus(ctx, role)
	if err != nil {
		return roleMenuResponse{}, err
	}
	allowed := availableMenus(role)
	out := roleMenuResponse{Role: role, AvailableMenuIDs: allowed, MenuIDs: append([]string{}, allowed...)}
	if found {
		out.MenuIDs = []string{}
		out.Rev = setting.Rev
		// Reapply current permissions if a future release changes the catalog.
		for _, m := range menuCatalog {
			if slices.Contains(allowed, m.ID) && (slices.Contains(setting.MenuIDs, m.ID) || m.Required) {
				out.MenuIDs = append(out.MenuIDs, m.ID)
			}
		}
	}
	return out, nil
}

func (s *server) listRoleMenus(c *gin.Context) {
	out := roleMenuListResponse{Menus: slices.Clone(menuCatalog), Roles: []roleMenuResponse{}}
	for _, role := range store.Roles() {
		item, err := s.roleMenus(c.Request.Context(), role)
		if err != nil {
			RespondError(c, err)
			return
		}
		out.Roles = append(out.Roles, item)
	}
	Ok(c, out)
}

func (s *server) saveRoleMenus(c *gin.Context) {
	role := store.Role(c.Param("role"))
	var req roleMenuRequest
	if !Bind(c, &req) {
		return
	}
	if !role.Valid() || req.Rev == nil || req.MenuIDs == nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	setting := store.RoleMenuSettings{Role: role, MenuIDs: req.MenuIDs, Rev: *req.Rev}
	if store.CheckRoleMenus(setting) != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	allowed := availableMenus(role)
	for _, id := range req.MenuIDs {
		if !slices.Contains(allowed, id) {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
	}
	setting.MenuIDs = []string{}
	for _, m := range menuCatalog {
		if m.Required && slices.Contains(allowed, m.ID) && !slices.Contains(req.MenuIDs, m.ID) {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		if slices.Contains(req.MenuIDs, m.ID) {
			setting.MenuIDs = append(setting.MenuIDs, m.ID)
		}
	}
	if s.deps.RoleMenus == nil {
		Fail(c, 500, nil, model.ConsoleInternal)
		return
	}
	saved, ok := Write(c, func() (store.RoleMenuSettings, error) {
		return s.deps.RoleMenus.SaveRoleMenus(c.Request.Context(), setting, s.auditFrom(c, ""))
	})
	if ok {
		Ok(c, roleMenuResponse{Role: role, AvailableMenuIDs: allowed, MenuIDs: saved.MenuIDs, Rev: saved.Rev})
	}
}

func (s *server) currentUserResponse(ctx context.Context, u store.User) (userResponse, error) {
	menus, err := s.roleMenus(ctx, u.Role)
	if err != nil {
		return userResponse{}, err
	}
	out := toUserResponse(u)
	out.MenuIDs = menus.MenuIDs
	out.MenuRev = menus.Rev
	return out, nil
}
