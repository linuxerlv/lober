package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/domain/model"
	"strconv"
)

type userUpdateRequest struct {
	Username    string     `json:"username,omitempty"`
	DisplayName string     `json:"displayName"`
	Role        model.Role `json:"role" binding:"required"`
	Password    string     `json:"password,omitempty"`
	Disabled    bool       `json:"disabled"`
	Rev         int64      `json:"rev" binding:"required,gt=0"`
}
type userCreateRequest struct {
	Username    string     `json:"username" binding:"required"`
	DisplayName string     `json:"displayName"`
	Role        model.Role `json:"role" binding:"required"`
	Password    string     `json:"password" binding:"required"`
	Disabled    bool       `json:"disabled"`
}
type userPage struct {
	Items []userResponse `json:"items"`
	Next  string         `json:"next"`
	Total *int64         `json:"total,omitempty"`
}
type pageQuery struct {
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	Cursor   string `form:"cursor"`
	Limit    int    `form:"limit"`
}

func (s *server) userRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/users", perm: PermUserManage, tag: "users", summary: "List users", query: pageQuery{}, resp: userPage{}, handle: s.listUsers},
		{method: "POST", path: "/console/v1/users", perm: PermUserManage, audit: "user.create", tag: "users", summary: "Create user", req: userCreateRequest{}, resp: userResponse{}, handle: s.createUser},
		{method: "PUT", path: "/console/v1/users/:username", identityOperation: identity.LegacySelfUpdate, perm: PermUserManage, audit: "user.update", tag: "users", summary: "Update user", req: userUpdateRequest{}, resp: userResponse{}, handle: s.updateUser},
		{method: "DELETE", path: "/console/v1/users/:username", perm: PermUserManage, audit: "user.disable", tag: "users", summary: "Disable user", handle: s.disableUser},
	}
}
func queryLimit(c *gin.Context) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 || limit > 500 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return 0, false
	}
	return limit, true
}
func (s *server) listUsers(c *gin.Context) {
	number, limit, ok := queryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (identity.UserPage, error) {
		return s.deps.Identity.ListUsers(c.Request.Context(), principalOf(c), identity.UserQuery{Cursor: c.Query("cursor"), Limit: limit, Page: number})
	})
	if !ok {
		return
	}
	items := make([]userResponse, len(page.Items))
	for i, u := range page.Items {
		items[i] = toUserResponse(u)
	}
	Ok(c, userPage{items, page.Next, page.Total})
}
func (s *server) createUser(c *gin.Context) {
	var req userCreateRequest
	if !Bind(c, &req) {
		return
	}
	u, ok := Write(c, func() (identity.UserView, error) {
		return s.deps.Identity.CreateUser(c.Request.Context(), principalOf(c), identity.CreateUserCommand{Username: req.Username, DisplayName: req.DisplayName, Role: req.Role, Password: req.Password, Disabled: req.Disabled})
	})
	if ok {
		Ok(c, toUserResponse(u))
	}
}
func (s *server) updateUser(c *gin.Context) {
	var req userUpdateRequest
	if !Bind(c, &req) {
		return
	}
	u, ok := Write(c, func() (identity.UserView, error) {
		return s.deps.Identity.UpdateUser(c.Request.Context(), principalOf(c), c.Param("username"), identity.UpdateUserCommand{Username: req.Username, DisplayName: req.DisplayName, Role: req.Role, Password: req.Password, Disabled: req.Disabled, Rev: req.Rev})
	})
	if ok {
		Ok(c, toUserResponse(u))
	}
}
func (s *server) disableUser(c *gin.Context) {
	if err := s.deps.Identity.DisableUser(c.Request.Context(), principalOf(c), c.Param("username")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
