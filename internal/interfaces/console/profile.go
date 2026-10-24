package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
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
	u, ok := Write(c, func() (identity.UserView, error) {
		return s.deps.Identity.UpdateProfile(c.Request.Context(), principalOf(c), identity.ProfileCommand{DisplayName: req.DisplayName, Email: req.Email, Avatar: req.Avatar, Bio: req.Bio, Rev: req.Rev})
	})
	if ok {
		Ok(c, toUserResponse(u))
	}
}
