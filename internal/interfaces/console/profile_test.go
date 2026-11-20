package console

import (
	"encoding/json"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

func Test个人资料仅更新本人且不接受权限字段(t *testing.T) {
	for i, role := range model.Roles() {
		t.Run(string(role), func(t *testing.T) {
			s, st, _ := newHTTPServer(t)
			name := fmt.Sprintf("profile%d", i)
			u := seedAuthUser(t, st, name, role, "profile password")
			token := loginHTTP(t, s, name, "profile password").Token
			body := fmt.Sprintf(`{"displayName":"张三","email":"zhangsan@example.com","bio":"机房运维","avatar":"","rev":%d,"username":"another","role":"admin","disabled":true,"password":"injected"}`, u.Rev)
			w := httpCall(s, "PUT", "/console/v1/user/profile", body, token)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var env struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Data["displayName"] != "张三" || env.Data["email"] != "zhangsan@example.com" || env.Data["bio"] != "机房运维" {
				t.Fatal(env.Data)
			}
			back, err := st.GetUser(t.Context(), name)
			if err != nil || back.Username != name || back.Role != role || back.Disabled || back.Password != u.Password || back.Rev != u.Rev+1 {
				t.Fatal(back, err)
			}
			if w := httpCall(s, "PUT", "/console/v1/user/profile", body, token); w.Code != 409 {
				t.Fatal("stale profile accepted", w.Code)
			}
			w = httpCall(s, "GET", "/console/v1/user/info", "", token)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Data["email"] != "zhangsan@example.com" {
				t.Fatal("profile not persisted", env.Data)
			}
			audits, err := st.ListAudit(t.Context(), repository.AuditQuery{Action: "user.profile.update"})
			if err != nil || len(audits.Items) != 1 || audits.Items[0].Actor != name || audits.Items[0].TargetID != name {
				t.Fatal(audits, err)
			}
		})
	}
}

func Test个人资料验证及强制改密限制(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	u := seedAuthUser(t, st, "alice", model.RoleViewer, "old")
	token := loginHTTP(t, s, "alice", "old").Token
	for _, body := range []string{`{"rev":1,"email":"invalid"}`, `{"rev":1,"avatar":"https://remote/avatar.svg"}`, `{"rev":1,"avatar":"data:image/svg+xml;base64,PHN2Zz4="}`, `{"rev":0}`} {
		if w := httpCall(s, "PUT", "/console/v1/user/profile", body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := httpCall(s, "PUT", "/console/v1/user/profile", `{"rev":1}`, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	u.MustChangePassword = true
	if _, err := st.UpdateUser(t.Context(), u, repository.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if w := httpCall(s, "PUT", "/console/v1/user/profile", `{"rev":2}`, token); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func Test仅管理员可以给账号分配角色(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	target := seedAuthUser(t, st, "zhangsan", model.RoleViewer, "target")
	for i, role := range model.Roles() {
		name := fmt.Sprintf("assign%d", i)
		seedAuthUser(t, st, name, role, "actor")
		token := loginHTTP(t, s, name, "actor").Token
		w := httpCall(s, "PUT", "/console/v1/users/zhangsan", fmt.Sprintf(`{"role":"operator","rev":%d}`, target.Rev), token)
		want := 403
		if role == model.RoleAdmin {
			want = 200
		}
		if w.Code != want {
			t.Fatal(role, w.Code, w.Body.String())
		}
	}
	back, err := st.GetUser(t.Context(), "zhangsan")
	if err != nil || back.Role != model.RoleOperator {
		t.Fatal(back, err)
	}
}
