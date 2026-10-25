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

// Authorize 供可信应用检查传入身份与原权限名，不规范化、不新增长度限制。
// 先检查身份，再检查改密要求，最后使用统一权限表；分别返回未认证、需改密、无权限，成功为nil。
// 只判断值，不查最新账号、会话或仓储；重复与并发只读结果相同，调用期间不得修改输入。
// 无写入、事务、版本冲突、提交复查、审计、通知、上下文或超时；不能代替逐请求认证。
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
