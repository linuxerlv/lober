package repository

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func TestRoleMenusValidation(t *testing.T) {
	for _, setting := range []RoleMenuSettings{
		{Role: "root", MenuIDs: []string{}},
		{Role: model.RoleViewer, Rev: -1},
		{Role: model.RoleViewer, MenuIDs: []string{"hosts", "hosts"}},
		{Role: model.RoleViewer, MenuIDs: []string{"../hosts"}},
	} {
		if err := CheckRoleMenus(setting); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", setting, err)
		}
	}
	for _, ids := range [][]string{nil, {}, {"hosts", "role-menus"}} {
		if err := CheckRoleMenus(RoleMenuSettings{Role: model.RoleViewer, MenuIDs: ids}); err != nil {
			t.Fatal(err)
		}
	}
}
