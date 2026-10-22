package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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
	if err := repository.CheckProfile(req.DisplayName, req.Email, req.Avatar, req.Bio); err != nil {
		RespondError(c, err)
		return
	}
	// Keep the revision attached to the authenticated snapshot. A slow request
	// must not relabel old credentials/roles with a guessed newer revision.
	u := *userOf(c)
	if req.Rev != u.Rev {
		RespondError(c, repository.ErrConflict)
		return
	}
	u.DisplayName = req.DisplayName
	u.Email = req.Email
	u.Avatar = req.Avatar
	u.Bio = req.Bio
	u, ok := Write(c, func() (model.User, error) { return s.deps.Users.UpdateUser(c.Request.Context(), u, s.auditFrom(c, "")) })
	if !ok {
		return
	}
	result, ok := Get(c, func() (userResponse, error) { return s.currentUserResponse(c.Request.Context(), u) })
	if ok {
		Ok(c, result)
	}
}
