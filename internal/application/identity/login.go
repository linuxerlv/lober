package identity

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Service) Login(ctx context.Context, c LoginCommand) (SessionResult, error) {
	if c.Username == "" || c.Password == "" {
		return SessionResult{}, ErrBadRequest
	}
	u, err := s.users.GetUser(ctx, c.Username)
	if errors.Is(err, repository.ErrNotFound) {
		s.passwords.CheckDummy(c.Password)
		return SessionResult{}, ErrLoginFailed
	}
	if err != nil {
		return SessionResult{}, err
	}
	valid := s.passwords.Check(c.Password, u.Password)
	if !valid || u.Disabled || !u.Role.Valid() {
		return SessionResult{}, ErrLoginFailed
	}
	// 计算已结束；最终复核到发布之间不允许重置提交。
	s.credentials.RLock()
	defer s.credentials.RUnlock()
	current, err := s.users.GetUser(ctx, u.Username)
	if errors.Is(err, repository.ErrNotFound) || err == nil && (current.Password != u.Password || current.Disabled || !current.Role.Valid()) {
		return SessionResult{}, ErrLoginFailed
	}
	if err != nil {
		return SessionResult{}, err
	}
	user, err := s.currentUserView(ctx, current)
	if err != nil {
		return SessionResult{}, err
	}
	token := s.sessions.Put(current.Username)
	return SessionResult{Token: token, ExpiresAt: s.sessions.ExpiresAt(token), User: user}, nil
}
