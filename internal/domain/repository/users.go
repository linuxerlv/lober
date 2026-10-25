package repository

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
)

// Users persists accounts, with mutations and audit entries in one transaction.
// 会话归身份应用管理。账号标识和创建时间不能改。
type Users interface {
	// CreateUser 供可信身份应用/初始化保存已散列账号，先CheckAudit，再CheckUser；输入不裁剪。
	// 创建Rev=1，账号与user审计同事务；同用户名重复由唯一约束报冲突/重复，无自动重试。
	// 返回保存的账号（含内部散列，仅限可信调用者）；新建不涉及列表顺序，取消沿用doc.go。
	CreateUser(context.Context, model.User, Audit) (model.User, error)
	// UpdateUser 先CheckAudit、CheckUser和正Rev，再锁用户表及目标行，检查存在性、原Rev与最后启用管理员。
	// 整条替换允许的账号资料与安全字段，保留用户名/创建时间，Rev加1；账号与审计同事务。
	// 不存在报ErrNotFound，旧Rev报ErrConflict，移除最后管理员报ErrLastAdmin；不自动重试，取消沿用doc.go。
	UpdateUser(context.Context, model.User, Audit) (model.User, error)
	// GetUser 按原用户名读取内部账号，含散列，不供对外响应直接使用；不规范化或裁剪。
	// 不可存文本和缺失均ErrNotFound，其余读取错误原样返回；只读、重复与取消遵守doc.go。
	GetUser(context.Context, string) (model.User, error)
	// ListUsers 先CheckPageQuery，再解users游标；按用户名的字节序升序返回内部账号。
	// 游标默认100、页码默认20、最多500，空Items为非nil数组，末页Next为空；Total只在页码存在。
	// 不读取角色菜单，不排除内部散列；只读、重复、无提交复查与取消遵守doc.go。
	ListUsers(context.Context, UserQuery) (Page[model.User], error)
}
type UserQuery struct {
	Page   int // 大于 0 时按页码查询，0 时沿用游标。
	Cursor string
	Limit  int
}

// CheckUser checks data, not revision or disabled state. Hashing belongs to the
// caller; storage only requires a nonempty storable hash.
func CheckUser(u model.User) error {
	if u.CreatedAt <= 0 {
		return fmt.Errorf("%w: invalid user creation time", ErrInvalid)
	}
	var ps []model.Problem
	add := func(code model.Code, path string, args ...any) {
		ps = append(ps, model.NewProblem(code, model.Subject{Kind: model.UserSubjectKind, ID: u.Username, Path: path}, args...))
	}
	if !model.ValidIdent(u.Username) {
		add("user_field_invalid", "username", model.UserLabelUsername)
	}
	if !u.Role.Valid() {
		add("user_field_invalid", "role", model.UserLabelRole)
	}
	if u.Password == "" {
		add("user_field_invalid", "password", model.UserLabelPassword)
	}
	if !model.StorableText(u.DisplayName) {
		add("text_not_storable", "displayName")
	}
	if !model.StorableText(u.Password) {
		add("text_not_storable", "password")
	}
	if len(ps) > 0 {
		return &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return CheckProfile(u.DisplayName, u.Email, u.Avatar, u.Bio)
}
