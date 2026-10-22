package repository

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
)

// Users persists accounts, with mutations and audit entries in one transaction.
// Sessions belong to the console. Identity and creation time are immutable.
type Users interface {
	CreateUser(context.Context, model.User, Audit) (model.User, error)
	UpdateUser(context.Context, model.User, Audit) (model.User, error)
	GetUser(context.Context, string) (model.User, error)
	ListUsers(context.Context, UserQuery) (Page[model.User], error)
}
type UserQuery struct {
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
	return nil
}
