package identity

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/repository"
	"slices"
)

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	username, ok := s.sessions.Lookup(token)
	if !ok {
		return Principal{}, ports.ErrUnauthenticated
	}
	u, err := s.users.GetUser(ctx, username)
	if errors.Is(err, repository.ErrNotFound) || err == nil && u.Disabled {
		s.sessions.Drop(token)
		return Principal{}, ports.ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	if !u.Role.Valid() {
		return Principal{}, ports.ErrUnauthenticated
	}
	return Principal{user: u, token: token, owner: s}, nil
}

func (s *Service) Authorize(p Principal, r AccessRequest) error {
	if p.owner != s || p.owner == nil {
		return ports.ErrUnauthenticated
	}
	decision := access.CheckRequest(access.Subject{Username: p.user.Username, Role: p.user.Role, Disabled: p.user.Disabled, MustChangePassword: p.user.MustChangePassword}, access.Request{Permission: r.Permission, Operation: r.Operation, TargetUsername: r.TargetUsername})
	switch decision {
	case access.Allow:
		return nil
	case access.Unauthenticated:
		return ports.ErrUnauthenticated
	case access.PasswordChangeRequired:
		return ports.ErrPasswordChangeRequired
	default:
		return ports.ErrForbidden
	}
}

func (s *Service) Logout(_ context.Context, p Principal) error {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermAnyUser, Operation: Logout}); err != nil {
		return err
	}
	s.sessions.Drop(p.token)
	return nil
}

func (s *Service) CurrentUser(ctx context.Context, p Principal) (UserView, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermAnyUser, Operation: UserInfo}); err != nil {
		return UserView{}, err
	}
	return s.currentUserView(ctx, p.user)
}

func (s *Service) StreamAuthorized(ctx context.Context, token, permission string) bool {
	username, ok := s.sessions.Lookup(token)
	if !ok {
		return false
	}
	u, err := s.users.GetUser(ctx, username)
	return err == nil && !u.Disabled && !u.MustChangePassword && u.Role.Valid() && slices.Contains(access.PermsOf(u.Role), permission)
}

// KnownPermission 只校验路由声明：16项实际权限及已登录标记 @any；它不授予权限。
func KnownPermission(permission string) bool {
	return permission == access.PermAnyUser || slices.Contains(access.AllPerms(), permission)
}
