package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/domain/model"
)

type menuDefinition struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ParentID   string `json:"parentId"`
	Permission string `json:"permission"`
	Required   bool   `json:"required"`
}

type roleMenuResponse struct {
	Role             model.Role `json:"role"`
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

func (s *server) listRoleMenus(c *gin.Context) {
	result, ok := Get(c, func() (identity.MenuList, error) {
		return s.deps.Identity.ListRoleMenus(c.Request.Context(), principalOf(c))
	})
	if !ok {
		return
	}
	out := roleMenuListResponse{Menus: make([]menuDefinition, len(result.Menus)), Roles: make([]roleMenuResponse, len(result.Roles))}
	for i, m := range result.Menus {
		out.Menus[i] = menuDefinition{ID: m.ID, Title: m.Title, ParentID: m.ParentID, Permission: m.Permission, Required: m.Required}
	}
	for i, m := range result.Roles {
		out.Roles[i] = toRoleMenuResponse(m)
	}
	Ok(c, out)
}
func toRoleMenuResponse(m identity.RoleMenuView) roleMenuResponse {
	return roleMenuResponse{Role: m.Role, AvailableMenuIDs: m.AvailableMenuIDs, MenuIDs: m.MenuIDs, Rev: m.Rev}
}
func (s *server) saveRoleMenus(c *gin.Context) {
	var req roleMenuRequest
	if !Bind(c, &req) {
		return
	}
	result, ok := Write(c, func() (identity.RoleMenuView, error) {
		return s.deps.Identity.SaveRoleMenus(c.Request.Context(), principalOf(c), model.Role(c.Param("role")), identity.SaveMenusCommand{MenuIDs: req.MenuIDs, Rev: *req.Rev})
	})
	if ok {
		Ok(c, toRoleMenuResponse(result))
	}
}
