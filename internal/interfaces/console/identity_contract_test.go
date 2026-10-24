package console

import (
	"encoding/json"
	"fmt"
	"github.com/lober/lober/internal/domain/access"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test身份十三入口契约与强制改密矩阵(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	// 写入口故意缺少必填项：400 表示已通过身份检查，403 表示被权限挡住。
	cases := []struct {
		method, path, perm, audit, tag string
		allowedStatus                  int
		forcedAllowed                  bool
	}{
		{"POST", "/console/v1/auth/login", "", "", "auth", 400, true},
		{"POST", "/console/v1/auth/logout", "@any", "", "auth", 200, true},
		{"POST", "/console/v1/auth/password", "@any", "user.password.change", "auth", 400, true},
		{"GET", "/console/v1/user/info", "@any", "", "auth", 200, true},
		{"PUT", "/console/v1/user/profile", "@any", "user.profile.update", "auth", 400, false},
		{"GET", "/console/v1/users", "user:manage", "", "users", 200, false},
		{"POST", "/console/v1/users", "user:manage", "user.create", "users", 400, false},
		{"PUT", "/console/v1/users/:username", "user:manage", "user.update", "users", 400, true},
		{"DELETE", "/console/v1/users/:username", "user:manage", "user.disable", "users", 409, false},
		{"GET", "/console/v1/roles/menus", "user:manage", "", "access", 200, false},
		{"PUT", "/console/v1/roles/:role/menus", "user:manage", "role.menus.update", "access", 400, false},
		{"GET", "/console/v1/audit", "audit:read", "", "audit", 200, false},
		{"GET", "/console/v1/audit/export", "audit:read", "", "audit", 200, false},
	}
	var routes []RouteInfo
	for _, r := range s.Routes() {
		if slices.Contains([]string{"auth", "users", "access", "audit"}, r.Tag) {
			routes = append(routes, r)
		}
	}
	if len(routes) != 13 {
		t.Fatalf("身份与审计入口数 = %d，预期 13", len(routes))
	}
	for _, tc := range cases {
		want := RouteInfo{tc.method, tc.path, tc.perm, tc.audit, tc.tag}
		if !slices.Contains(routes, want) {
			t.Errorf("入口描述改变：%+v", want)
		}
		path := strings.ReplaceAll(strings.ReplaceAll(tc.path, ":username", "missing"), ":role", "viewer")
		wantStatus := 401
		if tc.perm == "" {
			wantStatus = 400
		}
		if w := httpCall(s, tc.method, path, "{}", ""); w.Code != wantStatus {
			t.Fatalf("未登录 %s %s：%d，预期 %d", tc.method, path, w.Code, wantStatus)
		}
	}
	roles := []model.Role{model.RoleViewer, model.RoleOperator, model.RoleHookAuthor, model.RoleHookApprover, model.RoleAdmin}
	if !slices.Equal(model.Roles(), roles) || len(access.AllPerms()) != 16 {
		t.Fatal("角色顺序或权限数量改变")
	}
	for i, role := range roles {
		name := fmt.Sprintf("identity%d", i)
		u := seedAuthUser(t, st, name, role, "identity password")
		for _, forced := range []bool{false, true} {
			if forced {
				u.MustChangePassword = true
				var err error
				u, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: fixedAt})
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/强制改密=%t/%s%s", role, forced, tc.method, tc.path), func(t *testing.T) {
					path := strings.ReplaceAll(strings.ReplaceAll(tc.path, ":username", name), ":role", "viewer")
					token := identityFixture(s).sessions.Put(name)
					want := tc.allowedStatus
					message := ""
					if forced && !tc.forcedAllowed {
						want, message = 403, model.ConsolePasswordChangeRequired
					} else if tc.perm != "" && tc.perm != "@any" && role != model.RoleAdmin {
						want, message = 403, model.ConsoleForbidden
					}
					w := httpCall(s, tc.method, path, "{}", token)
					if w.Code != want {
						t.Fatalf("状态 %d，预期 %d", w.Code, want)
					}
					if message != "" {
						var env struct {
							Message string `json:"message"`
						}
						if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Message != message {
							t.Fatalf("错误顺序改变：%s", w.Body.String())
						}
					}
				})
			}
		}
		// 旧用户更新入口仍让强制改密管理员更新本人，普通用户不能借此管理账号。
		token := identityFixture(s).sessions.Put(name)
		other := httpCall(s, "PUT", "/console/v1/users/missing", "{}", token)
		var denied struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(other.Body.Bytes(), &denied); err != nil || other.Code != 403 || denied.Message != model.ConsolePasswordChangeRequired {
			t.Fatal("强制改密允许了更新别人，或先检查了正文")
		}
		body := fmt.Sprintf(`{"role":%q,"password":"replacement identity password","rev":%d}`, role, u.Rev)
		w := httpCall(s, "PUT", "/console/v1/users/"+name, body, token)
		want := 403
		if role == model.RoleAdmin {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s 本人改密：%d，预期 %d", role, w.Code, want)
		}
		if role == model.RoleAdmin {
			out := responseData[userResponse](t, w)
			if out.MustChangePassword || out.MenuIDs == nil || len(out.MenuIDs) != 0 || out.MenuRev != 0 {
				t.Fatal("用户更新返回值改变")
			}
			if w := httpCall(s, "GET", "/console/v1/user/info", "", token); w.Code != 401 {
				t.Fatal("改密后旧会话仍有效")
			}
		}
	}
	// 不创建长连接：不存在的任务返回404，证明查询令牌只在事件流通过认证。
	token, _ := seedHTTPUser(t, s, st, "querytoken", model.RoleViewer)
	for _, tc := range []struct {
		path, header string
		status       int
	}{
		{"/console/v1/user/info?access_token=" + token, "", 401},
		{"/console/v1/tasks/missing/stream?access_token=" + token, "", 404},
		{"/console/v1/tasks/missing/stream?access_token=" + token, "Basic invalid", 401},
		{"/console/v1/tasks/missing/stream?access_token=invalid", "Bearer " + token, 404},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("令牌来源检查：%d，预期 %d", w.Code, tc.status)
		}
	}
}
