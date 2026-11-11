package identity

import (
	"context"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Service) UpdateProfile(ctx context.Context, p Principal, cmd ProfileCommand) (UserView, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermAnyUser}); err != nil {
		return UserView{}, err
	}
	if cmd.Rev < 1 {
		return UserView{}, ErrBadRequest
	}
	if err := repository.CheckProfile(cmd.DisplayName, cmd.Email, cmd.Avatar, cmd.Bio); err != nil {
		return UserView{}, err
	}
	// 请求的版本只能对应认证时的快照，不能拿猜测的新版本重贴旧权限或密码。
	u := p.user
	if cmd.Rev != u.Rev {
		return UserView{}, repository.ErrConflict
	}
	u.DisplayName = cmd.DisplayName
	u.Email = cmd.Email
	u.Avatar = cmd.Avatar
	u.Bio = cmd.Bio
	u, err := s.users.UpdateUser(ctx, u, s.accountAudit(p, "user.profile.update"))
	if err != nil {
		return UserView{}, err
	}
	return s.currentUserView(ctx, u)
}
