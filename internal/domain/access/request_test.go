package access

import (
	"github.com/lober/lober/internal/domain/model"
	"reflect"
	"testing"
)

func Test请求先检查身份再改密再权限(t *testing.T) {
	for _, role := range model.Roles() {
		for _, forced := range []bool{false, true} {
			subject := Subject{Username: "alice", Role: role, MustChangePassword: forced}
			for _, op := range []Operation{General, UserInfo, Logout, ChangePassword, LegacySelfUpdate, 99} {
				req := Request{Permission: PermAnyUser, Operation: op, TargetUsername: "alice"}
				want := Allow
				if op == LegacySelfUpdate {
					req.Permission = PermUserManage
					if role != model.RoleAdmin {
						want = Forbidden
					}
				}
				if op == 99 {
					want = Forbidden
				} else if forced && op == General {
					want = PasswordChangeRequired
				}
				if got := CheckRequest(subject, req); got != want {
					t.Fatal(role, forced, op, got, want)
				}
			}
		}
	}
	if CheckRequest(Subject{Username: "alice", Role: model.RoleViewer, MustChangePassword: true}, Request{Permission: PermUserManage}) != PasswordChangeRequired {
		t.Fatal("权限抢在改密前")
	}
	if CheckRequest(Subject{Username: "alice", Role: model.RoleViewer}, Request{Permission: PermUserManage}) != Forbidden {
		t.Fatal("普通用户获得管理权限")
	}
	for _, s := range []Subject{{}, {Username: "alice", Role: "unknown"}, {Username: "alice", Role: model.RoleAdmin, Disabled: true}} {
		if CheckRequest(s, Request{Permission: PermAnyUser}) != Unauthenticated {
			t.Fatal("坏身份通过")
		}
	}
}
func Test菜单默认显式空必需页面与副本(t *testing.T) {
	if got := EffectiveMenuIDs(model.RoleViewer, nil, false); !reflect.DeepEqual(got, []string{"tasks", "hosts", "templates", "hooks", "isos", "tools"}) {
		t.Fatal(got)
	}
	if got := EffectiveMenuIDs(model.RoleViewer, []string{}, true); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
	if got := EffectiveMenuIDs(model.RoleAdmin, []string{}, true); !reflect.DeepEqual(got, []string{"role-menus"}) {
		t.Fatal(got)
	}
	if _, ok := NormalizeMenuIDs(model.RoleAdmin, []string{"tasks"}); ok {
		t.Fatal("遗漏必需页面")
	}
	for _, ids := range [][]string{{"users"}, {"tasks-group"}, {"unknown"}} {
		if _, ok := NormalizeMenuIDs(model.RoleViewer, ids); ok {
			t.Fatal("授予不可用菜单", ids)
		}
	}
	if got, ok := NormalizeMenuIDs(model.RoleViewer, []string{"tools", "tasks"}); !ok || !reflect.DeepEqual(got, []string{"tasks", "tools"}) {
		t.Fatal(got, ok)
	}
	catalog := MenuCatalog()
	catalog[0].ID = "bad"
	if MenuCatalog()[0].ID != "tasks-group" {
		t.Fatal("目录可被外部改写")
	}
	ids := AvailableMenuIDs(model.RoleViewer)
	ids[0] = "bad"
	if AvailableMenuIDs(model.RoleViewer)[0] != "tasks" {
		t.Fatal("可用菜单共享切片")
	}
	perms := AllPerms()
	perms[0] = "bad"
	if AllPerms()[0] != "host:read" || PermsOf(model.RoleAdmin)[0] != "host:read" {
		t.Fatal("权限共享切片")
	}
}
func Test新口令文本不增加政策(t *testing.T) {
	for _, s := range []string{"", "x\x00", "x\xff"} {
		if ValidPasswordText(s) {
			t.Fatal("非法口令通过")
		}
	}
	for _, s := range []string{"x", " ", "口令"} {
		if !ValidPasswordText(s) {
			t.Fatal("添加了口令要求")
		}
	}
}
