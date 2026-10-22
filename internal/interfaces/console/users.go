package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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
}
type pageQuery struct {
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit"`
}

func (s *server) userRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/users", perm: PermUserManage, tag: "users", summary: "List users", query: pageQuery{}, resp: userPage{}, handle: s.listUsers},
		{method: "POST", path: "/console/v1/users", perm: PermUserManage, audit: "user.create", tag: "users", summary: "Create user", req: userCreateRequest{}, resp: userResponse{}, handle: s.createUser},
		{method: "PUT", path: "/console/v1/users/:username", perm: PermUserManage, audit: "user.update", tag: "users", summary: "Update user", req: userUpdateRequest{}, resp: userResponse{}, handle: s.updateUser},
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
	limit, ok := queryLimit(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (repository.Page[model.User], error) {
		return s.deps.Users.ListUsers(c.Request.Context(), repository.UserQuery{Cursor: c.Query("cursor"), Limit: limit})
	})
	if !ok {
		return
	}
	items := make([]userResponse, len(page.Items))
	for i, u := range page.Items {
		items[i] = toUserResponse(u)
	}
	Ok(c, userPage{items, page.Next})
}
func (s *server) createUser(c *gin.Context) {
	var req userCreateRequest
	if !Bind(c, &req) {
		return
	}
	if !model.ValidIdent(req.Username) || !req.Role.Valid() || req.Password == "" || !model.StorableText(req.Password) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	password, err := HashPassword(req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	u, ok := Write(c, func() (model.User, error) {
		return s.deps.Users.CreateUser(c.Request.Context(), model.User{Username: req.Username, DisplayName: req.DisplayName, Role: req.Role, Password: password, Disabled: req.Disabled, CreatedAt: s.clock()}, s.auditFrom(c, ""))
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
	username := c.Param("username")
	if req.Rev < 1 || !req.Role.Valid() || req.Username != "" && req.Username != username {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	if username == userOf(c).Username && req.Disabled {
		Fail(c, 409, nil, model.ConsoleSelfDisable)
		return
	}
	u, ok := Get(c, func() (model.User, error) { return s.deps.Users.GetUser(c.Request.Context(), username) })
	if !ok {
		return
	}
	if u.MustChangePassword && (req.Password == "" || CheckPassword(req.Password, u.Password)) {
		Fail(c, 409, nil, model.ConsolePasswordChangeRequired)
		return
	}
	u.Rev = req.Rev
	u.DisplayName = req.DisplayName
	u.Role = req.Role
	u.Disabled = req.Disabled
	passwordChanged := req.Password != ""
	if passwordChanged {
		password, err := HashPassword(req.Password)
		if err != nil {
			RespondError(c, err)
			return
		}
		u.Password = password
		u.MustChangePassword = false
	}
	s.credentials.Lock()
	defer s.credentials.Unlock()
	u, ok = Write(c, func() (model.User, error) { return s.deps.Users.UpdateUser(c.Request.Context(), u, s.auditFrom(c, "")) })
	if !ok {
		return
	}
	if passwordChanged || u.Disabled {
		s.sessions.dropUser(username)
	}
	Ok(c, toUserResponse(u))
}
func (s *server) disableUser(c *gin.Context) {
	username := c.Param("username")
	if username == userOf(c).Username {
		Fail(c, 409, nil, model.ConsoleSelfDisable)
		return
	}
	u, ok := Get(c, func() (model.User, error) { return s.deps.Users.GetUser(c.Request.Context(), username) })
	if !ok {
		return
	}
	u.Disabled = true
	s.credentials.Lock()
	defer s.credentials.Unlock()
	_, ok = Write(c, func() (model.User, error) { return s.deps.Users.UpdateUser(c.Request.Context(), u, s.auditFrom(c, "")) })
	if ok {
		s.sessions.dropUser(username)
		Ok(c, nil)
	}
}
