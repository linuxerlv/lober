package access

import (
	"github.com/lober/lober/internal/domain/model"
	"reflect"
	"testing"
)

func Test菜单读取过滤越权项并保留目录顺序(t *testing.T) {
	for _, tc := range []struct {
		role         model.Role
		stored, want []string
	}{
		{model.RoleViewer, []string{"users", "tools", "tasks-group", "tasks", "unknown", "tasks"}, []string{"tasks", "tools"}},
		{model.RoleAdmin, []string{"tools", "tasks"}, []string{"tasks", "tools", "role-menus"}},
		{"unknown", []string{"tasks", "role-menus"}, []string{}},
	} {
		before := append([]string{}, tc.stored...)
		got := EffectiveMenuIDs(tc.role, tc.stored, true)
		if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(tc.stored, before) {
			t.Fatal(got)
		}
		if len(got) > 0 {
			got[0] = "changed"
			if !reflect.DeepEqual(EffectiveMenuIDs(tc.role, tc.stored, true), tc.want) {
				t.Fatal("结果共享切片")
			}
		}
	}
}

func Test概览页面能单独授权(t *testing.T) {
	for _, id := range []string{"overview-analytics", "overview-workspace"} {
		want := []string{id}
		got, ok := NormalizeMenuIDs(model.RoleViewer, want)
		if !ok || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(EffectiveMenuIDs(model.RoleViewer, got, true), want) {
			t.Fatalf("只开放 %s，实际得到 %v", id, got)
		}
	}
}
