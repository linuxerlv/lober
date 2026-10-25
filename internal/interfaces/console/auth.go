package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
)

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}
type sessionResponse struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expiresAt"`
	User      userResponse `json:"user"`
}

func (s *server) authRoutes() []route {
	return []route{
		{method: "POST", path: "/console/v1/auth/login", tag: "auth", summary: "Sign in", req: loginRequest{}, resp: sessionResponse{}, handle: s.login},
		{method: "POST", path: "/console/v1/auth/logout", identityOperation: identity.Logout, perm: PermAnyUser, tag: "auth", summary: "Sign out", handle: s.logout},
		{method: "POST", path: "/console/v1/auth/password", identityOperation: identity.ChangePassword, perm: PermAnyUser, audit: "user.password.change", tag: "auth", summary: "Change own password and revoke all sessions", req: passwordRequest{}, handle: s.changePassword},
		{method: "GET", path: "/console/v1/user/info", identityOperation: identity.UserInfo, perm: PermAnyUser, tag: "auth", summary: "Current user and permissions", resp: userResponse{}, handle: s.userInfo},
		{method: "PUT", path: "/console/v1/user/profile", perm: PermAnyUser, audit: "user.profile.update", tag: "auth", summary: "Update own profile", req: profileRequest{}, resp: userResponse{}, handle: s.updateProfile},
	}
}
func (s *server) login(c *gin.Context) {
	var req loginRequest
	if !Bind(c, &req) {
		return
	}
	result, ok := Get(c, func() (identity.SessionResult, error) {
		return s.deps.Identity.Login(c.Request.Context(), identity.LoginCommand{Username: req.Username, Password: req.Password})
	})
	if ok {
		Ok(c, sessionResponse{Token: result.Token, ExpiresAt: result.ExpiresAt, User: toUserResponse(result.User)})
	}
}
func (s *server) logout(c *gin.Context) {
	if err := s.deps.Identity.Logout(c.Request.Context(), principalOf(c)); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
