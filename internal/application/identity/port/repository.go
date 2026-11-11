package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 登录先GetUser再验密码；启动初始化不要求网页身份，其他身份入口按各自既有顺序读取。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type Users interface {
	// CreateUser 的八项保存约定唯一委托 repository.Users.CreateUser
	// （internal/domain/repository/users.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	CreateUser(context.Context, model.User, repository.Audit) (model.User, error)
	// UpdateUser 的八项保存约定唯一委托 repository.Users.UpdateUser
	// （internal/domain/repository/users.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	UpdateUser(context.Context, model.User, repository.Audit) (model.User, error)
	// GetUser 的八项保存约定唯一委托 repository.Users.GetUser
	// （internal/domain/repository/users.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetUser(context.Context, string) (model.User, error)
	// ListUsers 的八项保存约定唯一委托 repository.Users.ListUsers
	// （internal/domain/repository/users.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListUsers(context.Context, repository.UserQuery) (repository.Page[model.User], error)
}

type RoleMenus interface {
	// GetRoleMenus 的八项保存约定唯一委托 repository.RoleMenus.GetRoleMenus
	// （internal/domain/repository/role_menus.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetRoleMenus(context.Context, model.Role) (repository.RoleMenuSettings, bool, error)
	// SaveRoleMenus 的八项保存约定唯一委托 repository.RoleMenus.SaveRoleMenus
	// （internal/domain/repository/role_menus.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	SaveRoleMenus(context.Context, repository.RoleMenuSettings, repository.Audit) (repository.RoleMenuSettings, error)
}
