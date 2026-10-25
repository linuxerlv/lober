package identity

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

var _ Bootstrapper = (*Service)(nil)

func (s *Service) Initialise(ctx context.Context, seedAdmin string) error {
	users, err := s.users.ListUsers(ctx, repository.UserQuery{Limit: 1})
	if err != nil {
		return err
	}
	if len(users.Items) != 0 {
		return nil
	}
	if seedAdmin == "" {
		return errors.New("空库初始化必须显式配置 seedAdmin")
	}
	hash, err := s.hashPassword(seedAdmin)
	if err != nil {
		return err
	}
	_, err = s.users.CreateUser(ctx, model.User{Username: "admin", DisplayName: "管理员", Role: model.RoleAdmin, Password: hash, MustChangePassword: true, CreatedAt: s.clock()}, repository.Audit{Actor: "bootstrap", Action: "user.create", At: s.clock()})
	return err
}
