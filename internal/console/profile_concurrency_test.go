package console

import (
	"fmt"
	"github.com/lober/lober/internal/store"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type profileBodyGate struct {
	io.Reader
	once            sync.Once
	entered, resume chan struct{}
}

func (g *profileBodyGate) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.entered); <-g.resume })
	return g.Reader.Read(p)
}

func Test慢资料请求不能用猜测新修订号恢复旧权限或凭据(t *testing.T) {
	for _, kind := range []string{"reset", "disable", "demote"} {
		t.Run(kind, func(t *testing.T) {
			s, st, _ := newHTTPServer(t)
			admin := adminHTTP(t, s, st)
			victim := seedAuthUser(t, st, "victim", store.RoleAdmin, "old password")
			token := loginHTTP(t, s, "victim", "old password").Token
			gate := &profileBodyGate{Reader: strings.NewReader(fmt.Sprintf(`{"displayName":"stale","rev":%d}`, victim.Rev+1)), entered: make(chan struct{}), resume: make(chan struct{})}
			req := httptest.NewRequest("PUT", "/console/v1/user/profile", gate)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); s.Engine.ServeHTTP(w, req); done <- w }()
			select {
			case <-gate.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("profile did not reach body gate")
			}
			role := store.RoleAdmin
			if kind == "demote" {
				role = store.RoleViewer
			}
			password := ""
			if kind == "reset" {
				password = "replacement password"
			}
			w := httpCall(s, "PUT", "/console/v1/users/victim", fmt.Sprintf(`{"role":%q,"disabled":%t,"password":%q,"rev":%d}`, role, kind == "disable", password, victim.Rev), admin)
			close(gate.resume)
			if w.Code != 200 {
				t.Fatal("admin update failed", w.Code)
			}
			response := <-done
			if response.Code != 409 && response.Code != 401 {
				t.Fatal("stale profile update accepted", response.Code)
			}
			back, err := st.GetUser(t.Context(), "victim")
			if err != nil {
				t.Fatal(err)
			}
			if back.Role != role || back.Disabled != (kind == "disable") || back.Rev != victim.Rev+1 {
				t.Fatal("profile overwrote security fields")
			}
			if kind == "reset" && !CheckPassword(password, back.Password) {
				t.Fatal("profile restored old password")
			}
		})
	}
}
