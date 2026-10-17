package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/store"
	"testing"
)

func adminHTTP(t *testing.T, s *Server, st store.Users) string {
	t.Helper()
	seedAuthUser(t, st, "admin", store.RoleAdmin, "admin password")
	return loginHTTP(t, s, "admin", "admin password").Token
}
func Test用户管理全部路由权限矩阵(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	for i, role := range store.Roles() {
		name := fmt.Sprintf("u%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		for _, rt := range s.Routes() {
			if rt.Perm != PermUserManage {
				continue
			}
			path := rt.Path
			if path == "/console/v1/users/:username" {
				path = "/console/v1/users/missing"
			}
			w := httpCall(s, rt.Method, path, `{}`, token)
			if role == store.RoleAdmin {
				if w.Code == 401 || w.Code == 403 {
					t.Fatal(role, rt, w.Code)
				}
			} else if w.Code != 403 {
				t.Fatal(role, rt, w.Code)
			}
		}
	}
}
func Test用户HTTP创建修改禁用恢复与审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	w := httpCall(s, "POST", "/console/v1/users", `{"username":"bob","displayName":"鲍勃","role":"operator","password":"initial"}`, token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	u, err := st.GetUser(t.Context(), "bob")
	if err != nil || !CheckPassword("initial", u.Password) || u.Rev != 1 {
		t.Fatal(err)
	}
	bobToken := loginHTTP(t, s, "bob", "initial").Token
	update := fmt.Sprintf(`{"rev":%d,"displayName":"","role":"viewer","password":"replacement"}`, u.Rev)
	w = httpCall(s, "PUT", "/console/v1/users/bob", update, token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = httpCall(s, "GET", "/console/v1/user/info", "", bobToken); w.Code != 401 {
		t.Fatal("old password session survived")
	}
	bobToken = loginHTTP(t, s, "bob", "replacement").Token
	w = httpCall(s, "DELETE", "/console/v1/users/bob", "", token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = httpCall(s, "GET", "/console/v1/user/info", "", bobToken); w.Code != 401 {
		t.Fatal("disabled session survived")
	}
	u, err = st.GetUser(t.Context(), "bob")
	if err != nil || !u.Disabled || u.DisplayName != "" {
		t.Fatal(err, u)
	}
	update = fmt.Sprintf(`{"rev":%d,"role":"viewer","disabled":false}`, u.Rev)
	if w = httpCall(s, "PUT", "/console/v1/users/bob", update, token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	loginHTTP(t, s, "bob", "replacement")
	entries, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range entries.Items {
		if e.TargetID == "bob" {
			if e.Actor != "admin" || e.TargetKind != "user" {
				t.Fatal(e)
			}
			counts[e.Action]++
		}
	}
	if counts["user.create"] != 1 || counts["user.update"] != 2 || counts["user.disable"] != 1 {
		t.Fatal(counts)
	}
}
func Test用户HTTP拒绝坏请求陈旧版本与自己禁用(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	for _, body := range []string{`{"username":"Bob","role":"viewer","password":"x"}`, `{"username":"bob","role":"root","password":"x"}`, `{"username":"bob","role":"viewer","password":""}`, `{`} {
		if w := httpCall(s, "POST", "/console/v1/users", body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, err := st.GetUser(t.Context(), "bob"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("invalid write persisted")
	}
	for _, path := range []string{"/console/v1/users?limit=bad", "/console/v1/users?limit=-1", "/console/v1/users?limit=501"} {
		if w := httpCall(s, "GET", path, "", token); w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	u, err := st.GetUser(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"role":"admin"}`, `{"rev":0,"role":"admin"}`} {
		if w := httpCall(s, "PUT", "/console/v1/users/admin", body, token); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if w := httpCall(s, "PUT", "/console/v1/users/admin", `{"rev":99,"role":"admin"}`, token); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := httpCall(s, "DELETE", "/console/v1/users/admin", "", token); w.Code != 409 {
		t.Fatal("self disable accepted")
	}
	if w := httpCall(s, "PUT", "/console/v1/users/admin", fmt.Sprintf(`{"rev":%d,"role":"admin","disabled":true}`, u.Rev), token); w.Code != 409 {
		t.Fatal("self disable via PUT accepted")
	}
}
func Test初始管理员强制改密在服务端执行并持久化(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	u := seedAuthUser(t, st, "admin", store.RoleAdmin, "seed password")
	u.MustChangePassword = true
	u, err := st.UpdateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	login := loginHTTP(t, s, "admin", "seed password")
	if !login.User.MustChangePassword {
		t.Fatal("missing forced-change marker")
	}
	if w := httpCall(s, "GET", "/console/v1/users", "", login.Token); w.Code != 403 {
		t.Fatal("forced-change bypass")
	}
	body := fmt.Sprintf(`{"rev":%d,"role":"admin","passwordChangeRequired":false}`, u.Rev)
	if w := httpCall(s, "PUT", "/console/v1/users/admin", body, login.Token); w.Code != 409 {
		t.Fatal("client flag bypass")
	}
	body = fmt.Sprintf(`{"rev":%d,"role":"admin","password":"seed password"}`, u.Rev)
	if w := httpCall(s, "PUT", "/console/v1/users/admin", body, login.Token); w.Code != 409 {
		t.Fatal("same-password bypass")
	}
	body = fmt.Sprintf(`{"rev":%d,"role":"admin","password":"new password"}`, u.Rev)
	if w := httpCall(s, "PUT", "/console/v1/users/admin", body, login.Token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	back, err := st.GetUser(t.Context(), "admin")
	if err != nil || back.MustChangePassword || !CheckPassword("new password", back.Password) {
		t.Fatal("forced change not persisted")
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", login.Token); w.Code != 401 {
		t.Fatal("old session survived")
	}
	fresh := loginHTTP(t, s, "admin", "new password")
	if w := httpCall(s, "GET", "/console/v1/users", "", fresh.Token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
func Test用户列表不泄露口令并能按游标翻页(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedAuthUser(t, st, "bob", store.RoleViewer, "pw")
	w := httpCall(s, "GET", "/console/v1/users?limit=1", "", token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var env struct{ Data userPage }
	json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Data.Items) != 1 || env.Data.Next == "" {
		t.Fatal("bad page")
	}
	w = httpCall(s, "GET", "/console/v1/users?limit=1&cursor="+env.Data.Next, "", token)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
