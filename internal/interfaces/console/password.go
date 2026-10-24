package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
)

type passwordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

func (s *server) changePassword(c *gin.Context) {
	var req passwordRequest
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.Identity.ChangePassword(c.Request.Context(), principalOf(c), identity.PasswordCommand{CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
