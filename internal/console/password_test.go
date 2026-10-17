package console

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func Test当前用户改密及全部会话撤销(t *testing.T) {
	for i, role := range store.Roles() {
		t.Run(string(role), func(t *testing.T) {
			s, st, _ := newHTTPServer(t)
			name := fmt.Sprintf("user%d", i)
			seedAuthUser(t, st, name, role, "old password")
			first := loginHTTP(t, s, name, "old password").Token
			second := loginHTTP(t, s, name, "old password").Token
			for _, body := range []string{`{"currentPassword":"wrong","newPassword":"new password"}`, `{"currentPassword":"old password","newPassword":""}`, `{"currentPassword":"old password","newPassword":"old password"}`} {
				w := httpCall(s, "POST", "/console/v1/auth/password", body, first)
				if w.Code != 400 && w.Code != 403 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if w := httpCall(s, "GET", "/console/v1/user/info", "", first); w.Code != 200 {
				t.Fatal("failed change revoked session", w.Code)
			}
			w := httpCall(s, "POST", "/console/v1/auth/password", `{"currentPassword":"old password","newPassword":"new password","username":"admin","role":"admin"}`, first)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, token := range []string{first, second} {
				if w := httpCall(s, "GET", "/console/v1/user/info", "", token); w.Code != 401 {
					t.Fatal("old session survived", w.Code)
				}
			}
			back, err := st.GetUser(t.Context(), name)
			if err != nil || back.Role != role || back.MustChangePassword {
				t.Fatal(back, err)
			}
			loginHTTP(t, s, name, "new password")
			if w := httpCall(s, "POST", "/console/v1/auth/login", fmt.Sprintf(`{"username":%q,"password":"old password"}`, name), ""); w.Code != 401 {
				t.Fatal("old password accepted")
			}
			entries, err := st.ListAudit(t.Context(), store.AuditQuery{Action: "user.password.change"})
			if err != nil || len(entries.Items) != 1 || entries.Items[0].Actor != name || strings.Contains(entries.Items[0].Detail, "password") {
				t.Fatalf("audit: %+v %v", entries, err)
			}
		})
	}
}

func Test强制改密只允许会话与本人改密(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	u := seedAuthUser(t, st, "alice", store.RoleViewer, "initial")
	u.MustChangePassword = true
	if _, err := st.UpdateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	token := loginHTTP(t, s, "alice", "initial").Token
	for _, path := range []string{"/console/v1/hosts", "/console/v1/roles/menus"} {
		if w := httpCall(s, "GET", path, "", token); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if w := httpCall(s, "POST", "/console/v1/auth/password", `{"currentPassword":"initial","newPassword":"replacement"}`, token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	fresh := loginHTTP(t, s, "alice", "replacement")
	if fresh.User.MustChangePassword {
		t.Fatal("flag remained")
	}
	if w := httpCall(s, "POST", "/console/v1/auth/password", `{}`, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

type failingPasswordAudit struct{ store.Users }

func (p failingPasswordAudit) UpdateUser(ctx context.Context, u store.User, a store.Audit) (store.User, error) {
	a.Actor = pgtest.FaultActor
	return p.Users.UpdateUser(ctx, u, a)
}
func Test改密审计失败不改变凭据与会话(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	seedAuthUser(t, st, "alice", store.RoleViewer, "old")
	token := loginHTTP(t, s, "alice", "old").Token
	s.server.deps.Users = failingPasswordAudit{st}
	if w := httpCall(s, "POST", "/console/v1/auth/password", `{"currentPassword":"old","newPassword":"new"}`, token); w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", token); w.Code != 200 {
		t.Fatal("failed change revoked session")
	}
	loginHTTP(t, s, "alice", "old")
}

func Test并发本人改密不能覆盖管理员重置(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	u := seedAuthUser(t, st, "alice", store.RoleViewer, "old")
	token := loginHTTP(t, s, "alice", "old").Token
	admin := adminHTTP(t, s, st)
	paused := &pausedLoginUsers{Users: st, read: make(chan struct{}), resume: make(chan struct{})}
	s.server.deps.Users = paused
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- httpCall(s, "POST", "/console/v1/auth/password", `{"currentPassword":"old","newPassword":"stale"}`, token)
	}()
	select {
	case <-paused.read:
	case <-time.After(2 * time.Second):
		t.Fatal("password route did not authenticate")
	}
	w := httpCall(s, "PUT", "/console/v1/users/alice", fmt.Sprintf(`{"role":"operator","password":"admin replacement","rev":%d}`, u.Rev), admin)
	close(paused.resume)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := <-result; w.Code != 401 && w.Code != 409 {
		t.Fatal("stale password change accepted", w.Code, w.Body.String())
	}
	fresh := loginHTTP(t, s, "alice", "admin replacement")
	if fresh.User.Role != store.RoleOperator {
		t.Fatal("role overwritten")
	}
}
