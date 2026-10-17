package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/store"
)

type profileRequest struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Avatar      string `json:"avatar"`
	Bio         string `json:"bio"`
	Rev         int64  `json:"rev" binding:"required,gt=0"`
}

func (s *server) updateProfile(c *gin.Context) {
	var req profileRequest
	if !Bind(c, &req) {
		return
	}
	if err := store.CheckProfile(req.DisplayName, req.Email, req.Avatar, req.Bio); err != nil {
		RespondError(c, err)
		return
	}
	// Fresh authenticated identity plus CAS: unrelated credential/role edits can
	// never be overwritten by a stale profile form or mass-assigned JSON fields.
	u := *userOf(c)
	u.Rev = req.Rev
	u.DisplayName = req.DisplayName
	u.Email = req.Email
	u.Avatar = req.Avatar
	u.Bio = req.Bio
	u, ok := Write(c, func() (store.User, error) { return s.deps.Users.UpdateUser(c.Request.Context(), u, s.auditFrom(c, "")) })
	if !ok {
		return
	}
	result, ok := Get(c, func() (userResponse, error) { return s.currentUserResponse(c.Request.Context(), u) })
	if ok {
		Ok(c, result)
	}
}
