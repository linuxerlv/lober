package store

import (
	"errors"
	"testing"
)

func TestRoleMenusValidation(t *testing.T) {
	for _, setting := range []RoleMenuSettings{
		{Role: "root", MenuIDs: []string{}},
		{Role: RoleViewer, Rev: -1},
		{Role: RoleViewer, MenuIDs: []string{"hosts", "hosts"}},
		{Role: RoleViewer, MenuIDs: []string{"../hosts"}},
	} {
		if err := CheckRoleMenus(setting); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", setting, err)
		}
	}
	for _, ids := range [][]string{nil, {}, {"hosts", "role-menus"}} {
		if err := CheckRoleMenus(RoleMenuSettings{Role: RoleViewer, MenuIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
}
