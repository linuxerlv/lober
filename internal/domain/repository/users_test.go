package repository

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"strings"
	"testing"
)

func goodUser() model.User {
	return model.User{Username: "alice", DisplayName: "爱丽丝", Role: model.RoleOperator, Password: "stored-hash", CreatedAt: 1750000000}
}

func TestCheckUser每条前提各一个反例(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*model.User)
	}{
		{"empty name", func(u *model.User) { u.Username = "" }},
		{"uppercase", func(u *model.User) { u.Username = "Alice" }},
		{"hyphen", func(u *model.User) { u.Username = "al-ice" }},
		{"long name", func(u *model.User) { u.Username = strings.Repeat("a", 33) }},
		{"display NUL", func(u *model.User) { u.DisplayName = "a\x00b" }},
		{"display UTF8", func(u *model.User) { u.DisplayName = "a\xffb" }},
		{"role", func(u *model.User) { u.Role = "root" }},
		{"role case", func(u *model.User) { u.Role = "Admin" }},
		{"empty hash", func(u *model.User) { u.Password = "" }},
		{"hash NUL", func(u *model.User) { u.Password = "a\x00b" }},
		{"hash UTF8", func(u *model.User) { u.Password = "a\xffb" }},
		{"zero creation", func(u *model.User) { u.CreatedAt = 0 }},
		{"negative creation", func(u *model.User) { u.CreatedAt = -1 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u := goodUser()
			tt.change(&u)
			if err := CheckUser(u); !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestCheckUser合规则的通过(t *testing.T) {
	u := goodUser()
	u.Username = strings.Repeat("a", 32)
	u.DisplayName = strings.Repeat("用", 40)
	if err := CheckUser(u); err != nil {
		t.Fatal(err)
	}
}
func TestCheckUser不管禁用与Rev(t *testing.T) {
	u := goodUser()
	u.Disabled = true
	u.Rev = 7
	if err := CheckUser(u); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUser问题定位与字段规则一致(t *testing.T) {
	for _, tt := range []struct {
		path   string
		code   model.Code
		change func(*model.User)
	}{
		{"role", "user_field_invalid", func(u *model.User) { u.Role = "root" }},
		{"displayName", "text_not_storable", func(u *model.User) { u.DisplayName = "x\x00" }},
		{"password", "user_field_invalid", func(u *model.User) { u.Password = "" }},
	} {
		u := goodUser()
		tt.change(&u)
		var pe *ProblemsError
		if !errors.As(CheckUser(u), &pe) || len(pe.Problems) != 1 || pe.Problems[0].Code != tt.code || pe.Problems[0].Subject.Kind != "user" || pe.Problems[0].Subject.ID != "alice" || pe.Problems[0].Subject.Path != tt.path {
			t.Fatalf("wrong field detail: %+v", pe)
		}
	}
}
