package store

import (
	"encoding/json"
	"errors"
	"github.com/lober/lober/internal/core/model"
	"slices"
	"strings"
	"testing"
)

func goodUser() User {
	return User{Username: "alice", DisplayName: "爱丽丝", Role: RoleOperator, Password: "stored-hash", CreatedAt: 1750000000}
}

func TestCheckUser每条前提各一个反例(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*User)
	}{
		{"empty name", func(u *User) { u.Username = "" }},
		{"uppercase", func(u *User) { u.Username = "Alice" }},
		{"hyphen", func(u *User) { u.Username = "al-ice" }},
		{"long name", func(u *User) { u.Username = strings.Repeat("a", 33) }},
		{"display NUL", func(u *User) { u.DisplayName = "a\x00b" }},
		{"display UTF8", func(u *User) { u.DisplayName = "a\xffb" }},
		{"role", func(u *User) { u.Role = "root" }},
		{"role case", func(u *User) { u.Role = "Admin" }},
		{"empty hash", func(u *User) { u.Password = "" }},
		{"hash NUL", func(u *User) { u.Password = "a\x00b" }},
		{"hash UTF8", func(u *User) { u.Password = "a\xffb" }},
		{"zero creation", func(u *User) { u.CreatedAt = 0 }},
		{"negative creation", func(u *User) { u.CreatedAt = -1 }},
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
func TestRole的取值与Valid(t *testing.T) {
	want := []Role{"viewer", "operator", "hook-author", "hook-approver", "admin"}
	if !slices.Equal(Roles(), want) {
		t.Fatal(Roles())
	}
	for _, r := range want {
		if !r.Valid() {
			t.Fatal(r)
		}
	}
	for _, r := range []Role{"", "root", "Admin", "viewer "} {
		if r.Valid() {
			t.Fatal(r)
		}
	}
}
func TestRoles返回副本(t *testing.T) {
	got := Roles()
	got[0] = "changed"
	if Roles()[0] != "viewer" {
		t.Fatal("shared roles")
	}
}
func Test用户口令散列不进入JSON(t *testing.T) {
	encoded, err := json.Marshal(goodUser())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "stored-hash") || strings.Contains(string(encoded), "Password") {
		t.Fatal("password escaped boundary")
	}
}

func TestCheckUser问题定位与字段规则一致(t *testing.T) {
	for _, tt := range []struct {
		path   string
		code   model.Code
		change func(*User)
	}{
		{"role", "user_field_invalid", func(u *User) { u.Role = "root" }},
		{"displayName", "text_not_storable", func(u *User) { u.DisplayName = "x\x00" }},
		{"password", "user_field_invalid", func(u *User) { u.Password = "" }},
	} {
		u := goodUser()
		tt.change(&u)
		var pe *ProblemsError
		if !errors.As(CheckUser(u), &pe) || len(pe.Problems) != 1 || pe.Problems[0].Code != tt.code || pe.Problems[0].Subject.Kind != "user" || pe.Problems[0].Subject.ID != "alice" || pe.Problems[0].Subject.Path != tt.path {
			t.Fatalf("wrong field detail: %+v", pe)
		}
	}
}
