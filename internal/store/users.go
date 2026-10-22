package store

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/core/model"
	"slices"
)

// Users persists accounts, with mutations and audit entries in one transaction.
// Sessions belong to the console. Identity and creation time are immutable.
type Users interface {
	CreateUser(context.Context, User, Audit) (User, error)
	UpdateUser(context.Context, User, Audit) (User, error)
	GetUser(context.Context, string) (User, error)
	ListUsers(context.Context, UserQuery) (Page[User], error)
}
type User struct {
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	Email              string `json:"email"`
	Avatar             string `json:"avatar"`
	Bio                string `json:"bio"`
	Role               Role   `json:"role"`
	Password           string `json:"-"`
	Disabled           bool   `json:"disabled"`
	MustChangePassword bool   `json:"passwordChangeRequired"`
	CreatedAt          int64  `json:"createdAt"`
	Rev                int64  `json:"rev"`
}
type Role string

const (
	RoleViewer       Role = "viewer"
	RoleOperator     Role = "operator"
	RoleHookAuthor   Role = "hook-author"
	RoleHookApprover Role = "hook-approver"
	RoleAdmin        Role = "admin"
	UserSubjectKind       = "user"
)

var roles = []Role{RoleViewer, RoleOperator, RoleHookAuthor, RoleHookApprover, RoleAdmin}

func Roles() []Role        { return slices.Clone(roles) }
func (r Role) Valid() bool { return slices.Contains(roles, r) }

type UserQuery struct {
	Page   int // Zero preserves cursor pagination; positive values select a numbered page.
	Cursor string
	Limit  int
}

// CheckUser checks data, not revision or disabled state. Hashing belongs to the
// caller; storage only requires a nonempty storable hash.
func CheckUser(u User) error {
	if u.CreatedAt <= 0 {
		return fmt.Errorf("%w: invalid user creation time", ErrInvalid)
	}
	var ps []model.Problem
	add := func(code model.Code, path string, args ...any) {
		ps = append(ps, model.NewProblem(code, model.Subject{Kind: UserSubjectKind, ID: u.Username, Path: path}, args...))
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
