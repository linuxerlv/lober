package identity

import (
	"context"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

var _ Accounts = (*Service)(nil)

func (s *Service) ListUsers(ctx context.Context, p Principal, q UserQuery) (UserPage, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage}); err != nil {
		return UserPage{}, err
	}
	if _, err := repository.CheckPageQuery(q.Cursor, q.Limit, q.Page); err != nil {
		return UserPage{}, err
	}
	page, err := s.users.ListUsers(ctx, repository.UserQuery{Page: q.Page, Limit: q.Limit, Cursor: q.Cursor})
	if err != nil {
		return UserPage{}, err
	}
	items := make([]UserView, len(page.Items))
	for i, u := range page.Items {
		items[i] = toUserView(u)
	}
	return UserPage{Items: items, Next: page.Next, Total: page.Total}, nil
}

func (s *Service) CreateUser(ctx context.Context, p Principal, cmd CreateUserCommand) (UserView, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage}); err != nil {
		return UserView{}, err
	}
	if !model.ValidIdent(cmd.Username) || !cmd.Role.Valid() || !access.ValidPasswordText(cmd.Password) {
		return UserView{}, ErrBadRequest
	}
	hash, err := s.hashPassword(cmd.Password)
	if err != nil {
		return UserView{}, err
	}
	u, err := s.users.CreateUser(ctx, model.User{Username: cmd.Username, DisplayName: cmd.DisplayName, Role: cmd.Role, Password: hash, Disabled: cmd.Disabled, CreatedAt: s.clock()}, s.accountAudit(p, "user.create"))
	if err != nil {
		return UserView{}, err
	}
	return toUserView(u), nil
}

func (s *Service) UpdateUser(ctx context.Context, p Principal, username string, cmd UpdateUserCommand) (UserView, error) {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage, Operation: LegacySelfUpdate, TargetUsername: username}); err != nil {
		return UserView{}, err
	}
	if cmd.Rev < 1 || !cmd.Role.Valid() || cmd.Username != "" && cmd.Username != username {
		return UserView{}, ErrBadRequest
	}
	if username == p.user.Username && cmd.Disabled {
		return UserView{}, ErrSelfDisable
	}
	u, err := s.users.GetUser(ctx, username)
	if err != nil {
		return UserView{}, err
	}
	if u.MustChangePassword && (cmd.Password == "" || s.passwords.Check(cmd.Password, u.Password)) {
		return UserView{}, ErrInitialPasswordUnchanged
	}
	u.Rev = cmd.Rev
	u.DisplayName = cmd.DisplayName
	u.Role = cmd.Role
	u.Disabled = cmd.Disabled
	passwordChanged := cmd.Password != ""
	if passwordChanged {
		hash, err := s.hashPassword(cmd.Password)
		if err != nil {
			return UserView{}, err
		}
		u.Password = hash
		u.MustChangePassword = false
	}
	s.credentials.Lock()
	defer s.credentials.Unlock()
	u, err = s.users.UpdateUser(ctx, u, s.accountAudit(p, "user.update"))
	if err != nil {
		return UserView{}, err
	}
	if passwordChanged || u.Disabled {
		s.sessions.DropUser(username)
	}
	return toUserView(u), nil
}

func (s *Service) DisableUser(ctx context.Context, p Principal, username string) error {
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage}); err != nil {
		return err
	}
	if username == p.user.Username {
		return ErrSelfDisable
	}
	u, err := s.users.GetUser(ctx, username)
	if err != nil {
		return err
	}
	u.Disabled = true
	s.credentials.Lock()
	defer s.credentials.Unlock()
	if _, err = s.users.UpdateUser(ctx, u, s.accountAudit(p, "user.disable")); err != nil {
		return err
	}
	s.sessions.DropUser(username)
	return nil
}

// 身份与动作来自可信快照和本方法；每次真正尝试保存才读取审计时钟。
func (s *Service) accountAudit(p Principal, action string) repository.Audit {
	return repository.Audit{Actor: p.Actor().Username, Action: action, At: s.clock()}
}
