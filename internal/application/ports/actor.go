package ports

import "github.com/lober/lober/internal/domain/model"

// Actor 由已验证身份的入口提供，不能从业务请求正文读取。
type Actor struct {
	Username                     string
	Role                         model.Role
	Disabled, MustChangePassword bool
}
