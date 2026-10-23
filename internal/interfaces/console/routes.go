package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"

	"slices"
	"strings"
)

type route struct {
	method, path, perm, audit, tag, summary string
	req, resp, query                        any
	headers                                 any
	respVariants                            []any
	errorData                               any
	handle                                  gin.HandlerFunc
	stream, raw                             bool
	requestMedia, responseMedia             string
}
type RouteInfo struct{ Method, Path, Perm, Audit, Tag string }

func (s *Server) Routes() []RouteInfo { return slices.Clone(s.routes) }
func (s *server) allRoutes() []route {
	return slices.Concat(s.authRoutes(), s.userRoutes(), s.menuRoutes(), s.hostRoutes(), s.isoRoutes(), s.hookRoutes(), s.templateRoutes(), s.wizardRoutes(), s.taskRoutes(), s.auditRoutes(), []route{
		{method: "GET", path: "/openapi.json", perm: PermAnyUser, tag: "schema", summary: "OpenAPI contract", resp: map[string]any{}, raw: true, handle: func(c *gin.Context) { c.JSON(200, s.owner.OpenAPI()) }},
	})
}
func (s *server) installRoutes(out *Server, rs []route) {
	for _, rt := range rs {
		if rt.method == "" || rt.path == "" || rt.tag == "" || rt.summary == "" || rt.handle == nil {
			panic("incomplete route descriptor")
		}
		if rt.perm == "" && (rt.path != "/console/v1/auth/login" || rt.method != "POST") {
			panic("only login is public")
		}
		if rt.perm != "" && rt.perm != PermAnyUser && !slices.Contains(AllPerms(), rt.perm) {
			panic("unregistered permission")
		}
		key := rt.method + " " + rt.path
		if _, ok := s.index[key]; ok {
			panic("duplicate route")
		}
		s.index[key] = rt
		s.routes = append(s.routes, rt)
		out.routes = append(out.routes, RouteInfo{rt.method, rt.path, rt.perm, rt.audit, rt.tag})
		handlers := []gin.HandlerFunc{rt.handle}
		if rt.perm != "" {
			handlers = []gin.HandlerFunc{s.authenticate(), s.authorize(rt.perm), rt.handle}
		}
		out.Engine.Handle(rt.method, rt.path, handlers...)
	}
	slices.SortFunc(out.routes, func(a, b RouteInfo) int { return strings.Compare(a.Path+" "+a.Method, b.Path+" "+b.Method) })
}

type userResponse struct {
	Username           string     `json:"username"`
	DisplayName        string     `json:"displayName"`
	Email              string     `json:"email"`
	Avatar             string     `json:"avatar"`
	Bio                string     `json:"bio"`
	Role               model.Role `json:"role"`
	Permissions        []string   `json:"permissions"`
	Disabled           bool       `json:"disabled"`
	CreatedAt          int64      `json:"createdAt"`
	Rev                int64      `json:"rev"`
	MustChangePassword bool       `json:"passwordChangeRequired"`
	MenuIDs            []string   `json:"menuIds"`
	MenuRev            int64      `json:"menuRev"`
}

func toUserResponse(u model.User) userResponse {
	return userResponse{Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Avatar: u.Avatar, Bio: u.Bio, Role: u.Role, Permissions: PermsOf(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt, Rev: u.Rev, MustChangePassword: u.MustChangePassword, MenuIDs: []string{}}
}
func (s *server) userInfo(c *gin.Context) {
	u, ok := Get(c, func() (userResponse, error) { return s.currentUserResponse(c.Request.Context(), *userOf(c)) })
	if ok {
		Ok(c, u)
	}
}
