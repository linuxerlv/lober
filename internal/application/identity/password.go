package identity

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Service) ChangePassword(ctx context.Context, p Principal, cmd PasswordCommand) error {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermAnyUser, Operation: ChangePassword}); err != nil {
		return err
	}
	if cmd.CurrentPassword == "" || cmd.NewPassword == "" {
		return ErrBadRequest
	}
	u := p.user
	if !s.passwords.Check(cmd.CurrentPassword, u.Password) {
		return ports.ErrForbidden
	}
	if cmd.NewPassword == cmd.CurrentPassword || !access.ValidPasswordText(cmd.NewPassword) {
		return ErrBadRequest
	}
	hash, err := s.hashPassword(cmd.NewPassword)
	if err != nil {
		return err
	}
	s.credentials.Lock()
	defer s.credentials.Unlock()
	current, err := s.users.GetUser(ctx, u.Username)
	if err != nil {
		return err
	}
	if current.Disabled || !current.Role.Valid() {
		return ports.ErrUnauthenticated
	}
	if current.Rev != u.Rev || current.Password != u.Password {
		return repository.ErrConflict
	}
	if name, ok := s.sessions.Lookup(p.token); !ok || name != u.Username {
		return ports.ErrUnauthenticated
	}
	current.Password = hash
	current.MustChangePassword = false
	if _, err = s.users.UpdateUser(ctx, current, s.accountAudit(p, "user.password.change")); err != nil {
		return err
	}
	s.sessions.DropUser(u.Username)
	return nil
}
