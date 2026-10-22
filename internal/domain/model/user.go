package model

import "slices"

type User struct {
	Username           string
	DisplayName        string
	Email              string
	Avatar             string
	Bio                string
	Role               Role
	Password           string
	Disabled           bool
	MustChangePassword bool
	CreatedAt          int64
	Rev                int64
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
