package console

import (
	"github.com/lober/lober/internal/store"
	"slices"
	"testing"
)

func Test每个角色的权限码(t *testing.T) {
	read := []string{"host:read", "iso:read", "hook:read", "template:read", "task:read"}
	for _, tt := range []struct {
		role  store.Role
		extra []string
	}{
		{store.RoleViewer, nil}, {store.RoleOperator, []string{"host:write", "task:create", "task:cancel", "task:retry"}},
		{store.RoleHookAuthor, []string{"hook:write", "template:write"}}, {store.RoleHookApprover, []string{"hook:approve", "template:approve"}},
	} {
		want := append(slices.Clone(read), tt.extra...)
		if !slices.Equal(PermsOf(tt.role), want) {
			t.Fatal(tt.role, PermsOf(tt.role))
		}
	}
}
func TestAdmin拿全部权限码(t *testing.T) {
	if len(AllPerms()) != 16 || !slices.Equal(PermsOf(store.RoleAdmin), AllPerms()) {
		t.Fatal(AllPerms())
	}
}
func Test未知角色没有权限(t *testing.T) {
	for _, r := range []store.Role{"", "root", "Admin", "viewer "} {
		if len(PermsOf(r)) != 0 {
			t.Fatal(r)
		}
	}
}
func TestPermsOf返回副本(t *testing.T) {
	a := PermsOf(store.RoleAdmin)
	a[0] = "changed"
	if AllPerms()[0] != "host:read" {
		t.Fatal("shared")
	}
	a = PermsOf(store.RoleViewer)
	a[0] = "changed"
	if PermsOf(store.RoleViewer)[0] != "host:read" {
		t.Fatal("shared")
	}
}
