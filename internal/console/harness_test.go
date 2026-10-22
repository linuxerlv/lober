package console

import (
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const fixedAt int64 = 1790000000

func newHTTPServer(t *testing.T) (*Server, *pgstore.Store, *atomic.Int64) {
	t.Helper()
	st := pgtest.OpenStore(t)
	now := new(atomic.Int64)
	now.Store(fixedAt)
	s := New(Deps{Hosts: st, ISOs: st, Sources: st, Hooks: st, Templates: st, Tasks: st, CurrentTasks: st, Events: st, Audit: st, Users: st, Clock: now.Load, Notifier: NewHub(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return s, st, now
}
func seedHTTPUser(t *testing.T, s *Server, st store.Users, name string, role store.Role) (string, store.User) {
	t.Helper()
	u, err := st.CreateUser(t.Context(), store.User{Username: name, DisplayName: name, Role: role, Password: "private-hash", CreatedAt: fixedAt}, store.Audit{Actor: "test", Action: "user.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	return s.server.sessions.put(u.Username), u
}
func httpCall(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	return w
}
