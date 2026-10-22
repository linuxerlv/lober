package console

import (
	"github.com/lober/lober/internal/domain/model"

	"slices"
	"testing"
)

func Test每个角色的权限码(t *testing.T) {
	read := []string{"host:read", "iso:read", "hook:read", "template:read", "task:read"}
	for _, tt := range []struct {
		role  model.Role
		extra []string
	}{
		{model.RoleViewer, nil}, {model.RoleOperator, []string{"host:write", "task:create", "task:cancel", "task:retry"}},
		{model.RoleHookAuthor, []string{"hook:write", "template:write"}}, {model.RoleHookApprover, []string{"hook:approve", "template:approve"}},
	} {
		want := append(slices.Clone(read), tt.extra...)
		if !slices.Equal(PermsOf(tt.role), want) {
			t.Fatal(tt.role, PermsOf(tt.role))
		}
	}
}
func TestAdmin拿全部权限码(t *testing.T) {
	if len(AllPerms()) != 16 || !slices.Equal(PermsOf(model.RoleAdmin), AllPerms()) {
		t.Fatal(AllPerms())
	}
}
func Test未知角色没有权限(t *testing.T) {
	for _, r := range []model.Role{"", "root", "Admin", "viewer "} {
		if len(PermsOf(r)) != 0 {
			t.Fatal(r)
		}
	}
}
func TestPermsOf返回副本(t *testing.T) {
	a := PermsOf(model.RoleAdmin)
	a[0] = "changed"
	if AllPerms()[0] != "host:read" {
		t.Fatal("shared")
	}
	a = PermsOf(model.RoleViewer)
	a[0] = "changed"
	if PermsOf(model.RoleViewer)[0] != "host:read" {
		t.Fatal("shared")
	}
}
