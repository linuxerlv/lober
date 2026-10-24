package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type Users interface {
	CreateUser(context.Context, model.User, repository.Audit) (model.User, error)
	UpdateUser(context.Context, model.User, repository.Audit) (model.User, error)
	GetUser(context.Context, string) (model.User, error)
	ListUsers(context.Context, repository.UserQuery) (repository.Page[model.User], error)
}

type RoleMenus interface {
	GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error)
	SaveRoleMenus(context.Context, repository.RoleMenuSettings, repository.Audit) (repository.RoleMenuSettings, error)
}
