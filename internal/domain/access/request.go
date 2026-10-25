package access

import (
	"github.com/lober/lober/internal/domain/model"
	"slices"
)

type Operation int

const (
	General Operation = iota
	UserInfo
	Logout
	ChangePassword
	LegacySelfUpdate
)

type Decision int

const (
	Allow Decision = iota
	Unauthenticated
	PasswordChangeRequired
	Forbidden
)

type Subject struct {
	Username                     string
	Role                         model.Role
	Disabled, MustChangePassword bool
}
type Request struct {
	Permission     string
	Operation      Operation
	TargetUsername string
}

// CheckRequest 只判断值：身份优先，其次改密要求，再看权限；未知操作拒绝。
// 本人旧更新例外不授予管理权限；纯函数不读仓储、时钟或取消状态。
func CheckRequest(s Subject, r Request) Decision {
	if s.Username == "" || s.Disabled || !s.Role.Valid() {
		return Unauthenticated
	}
	if r.Operation < General || r.Operation > LegacySelfUpdate {
		return Forbidden
	}
	if s.MustChangePassword {
		// 旧本人更新先通过改密检查，再由管理权限拒绝普通用户，保留原错误顺序。
		allowed := r.Operation == UserInfo || r.Operation == Logout || r.Operation == ChangePassword || r.Operation == LegacySelfUpdate && r.TargetUsername == s.Username
		if !allowed {
			return PasswordChangeRequired
		}
	}
	if r.Permission != PermAnyUser && !slices.Contains(PermsOf(s.Role), r.Permission) {
		return Forbidden
	}
	return Allow
}
