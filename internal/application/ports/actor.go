package ports

import (
	"errors"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"slices"
)

var (
	ErrForbidden              = errors.New("没有创建任务的权限")
	ErrUnauthenticated        = errors.New("操作者身份无效")
	ErrPasswordChangeRequired = errors.New("需要先修改密码")
)

// Actor 由已验证身份的入口提供，不能从业务请求正文读取。
type Actor struct {
	Username                     string
	Role                         model.Role
	Disabled, MustChangePassword bool
}

// Authorize 先检查身份，再检查改密要求，最后使用统一权限表。
func (a Actor) Authorize(permission string) error {
	if a.Username == "" || a.Disabled || !a.Role.Valid() {
		return ErrUnauthenticated
	}
	if a.MustChangePassword {
		return ErrPasswordChangeRequired
	}
	if !slices.Contains(access.PermsOf(a.Role), permission) {
		return ErrForbidden
	}
	return nil
}
