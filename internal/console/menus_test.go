package console

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/store"
)

func Test菜单目录与五角色默认配置(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	for _, role := range store.Roles() {
		token, _ := seedHTTPUser(t, s, st, strings.ReplaceAll(string(role), "-", "_"), role)
		var env struct {
			Data struct {
				MenuIDs []string `json:"menuIds"`
				MenuRev int64    `json:"menuRev"`
			} `json:"data"`
		}
		w := httpCall(s, "GET", "/console/v1/user/info", "", token)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &env) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		want := []string{"tasks", "hosts", "templates", "hooks", "isos", "tools"}
		if role == store.RoleAdmin {
			want = append(want, "reports", "role-menus")
		}
		if !reflect.DeepEqual(env.Data.MenuIDs, want) || env.Data.MenuRev != 0 {
			t.Fatalf("%s: %+v", role, env.Data)
		}
	}
}

func Test角色菜单HTTP权限与验证(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	var admin string
	for i, role := range store.Roles() {
		name := []string{"viewer", "operator", "author", "approver", "admin"}[i]
		token, _ := seedHTTPUser(t, s, st, name, role)
		want := 403
		if role == store.RoleAdmin {
			want = 200
			admin = token
		}
		if w := httpCall(s, "GET", "/console/v1/roles/menus", "", token); w.Code != want {
			t.Fatalf("%s %d", role, w.Code)
		}
		if role != store.RoleAdmin {
			if w := httpCall(s, "PUT", "/console/v1/roles/viewer/menus", `{"menuIds":[],"rev":0}`, token); w.Code != 403 {
				t.Fatal(role, w.Code)
			}
		}
	}
	for _, method := range []string{"GET", "PUT"} {
		path := "/console/v1/roles/menus"
		if method == "PUT" {
			path = "/console/v1/roles/viewer/menus"
		}
		if w := httpCall(s, method, path, `{}`, ""); w.Code != 401 {
			t.Fatal(method, w.Code)
		}
	}
	for _, tc := range []struct{ role, body string }{
		{"root", `{"menuIds":[],"rev":0}`},
		{"viewer", `{"menuIds":["unknown"],"rev":0}`},
		{"viewer", `{"menuIds":["hosts","hosts"],"rev":0}`},
		{"viewer", `{"menuIds":["reports"],"rev":0}`},
		{"viewer", `{"menuIds":[],"rev":-1}`},
		{"viewer", `{"rev":0}`},
		{"viewer", `{"menuIds":null,"rev":0}`},
		{"viewer", `{"menuIds":[]}`},
		{"admin", `{"menuIds":[],"rev":0}`},
	} {
		if w := httpCall(s, "PUT", "/console/v1/roles/"+tc.role+"/menus", tc.body, admin); w.Code != 400 {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
}

func Test角色菜单修改进入当前会话(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	admin, _ := seedHTTPUser(t, s, st, "admin", store.RoleAdmin)
	viewer, _ := seedHTTPUser(t, s, st, "alice", store.RoleViewer)
	save := func(body string, want int) {
		t.Helper()
		w := httpCall(s, "PUT", "/console/v1/roles/viewer/menus", body, admin)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	save(`{"menuIds":["templates","hosts"],"rev":0}`, 200)
	save(`{"menuIds":[],"rev":0}`, 409)
	check := func(want []string, rev int64) {
		t.Helper()
		var env struct {
			Data struct {
				MenuIDs []string `json:"menuIds"`
				MenuRev int64    `json:"menuRev"`
			} `json:"data"`
		}
		w := httpCall(s, "GET", "/console/v1/user/info", "", viewer)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &env) != nil || !reflect.DeepEqual(env.Data.MenuIDs, want) || env.Data.MenuRev != rev {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	check([]string{"hosts", "templates"}, 1)
	save(`{"menuIds":[],"rev":1}`, 200)
	check([]string{}, 2)
	audit, err := st.ListAudit(t.Context(), store.AuditQuery{Action: "role.menus.update"})
	if err != nil || len(audit.Items) != 2 || audit.Items[0].Actor != "admin" || audit.Items[0].TargetKind != "role" {
		t.Fatalf("audit %+v %v", audit, err)
	}
}
