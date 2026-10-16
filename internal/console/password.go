package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
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
	u := *userOf(c)
	if !CheckPassword(req.CurrentPassword, u.Password) {
		Fail(c, 403, nil, model.ConsoleForbidden)
		return
	}
	if req.NewPassword == req.CurrentPassword || !model.StorableText(req.NewPassword) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		RespondError(c, err)
		return
	}
	s.credentials.Lock()
	defer s.credentials.Unlock()
	current, err := s.deps.Users.GetUser(c.Request.Context(), u.Username)
	if err != nil {
		RespondError(c, err)
		return
	}
	if current.Disabled || !current.Role.Valid() {
		Fail(c, 401, nil, model.ConsoleUnauthorized)
		return
	}
	if current.Rev != u.Rev || current.Password != u.Password {
		RespondError(c, store.ErrConflict)
		return
	}
	if name, ok := s.sessions.lookup(bearerToken(c)); !ok || name != u.Username {
		Fail(c, 401, nil, model.ConsoleUnauthorized)
		return
	}
	current.Password = hash
	current.MustChangePassword = false
	_, ok := Write(c, func() (store.User, error) {
		return s.deps.Users.UpdateUser(c.Request.Context(), current, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	s.sessions.dropUser(u.Username)
	Ok(c, nil)
}
