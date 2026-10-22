package console

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lober/lober/internal/domain/repository"
)

// Pause after a real database read, retaining its actual old revision.
type pausedLoginUsers struct {
	repository.Users
	paused       atomic.Bool
	read, resume chan struct{}
}

func (p *pausedLoginUsers) GetUser(ctx context.Context, username string) (model.User, error) {
	u, err := p.Users.GetUser(ctx, username)
	if err == nil && username == "alice" && p.paused.CompareAndSwap(false, true) {
		close(p.read)
		select {
		case <-p.resume:
		case <-ctx.Done():
			return model.User{}, ctx.Err()
		}
	}
	return u, err
}

func Test并发改密不能让旧口令登录补发会话(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	u := seedAuthUser(t, st, "alice", model.RoleOperator, "old password")
	admin := seedAuthUser(t, st, "administrator", model.RoleAdmin, "admin password")
	token := loginHTTP(t, s, admin.Username, "admin password").Token
	p := &pausedLoginUsers{Users: st, read: make(chan struct{}), resume: make(chan struct{})}
	s.server.deps.Users = p
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- httpCall(s, "POST", "/console/v1/auth/login", `{"username":"alice","password":"old password"}`, "")
	}()
	<-p.read
	w := httpCall(s, "PUT", "/console/v1/users/alice", fmt.Sprintf(`{"displayName":"alice","role":"operator","password":"new password","rev":%d}`, u.Rev), token)
	close(p.resume)
	if w.Code != 200 {
		t.Fatal("password reset failed", w.Code, w.Body.String())
	}
	if stale := <-result; stale.Code != 401 {
		t.Fatal("old password published a post-reset session", stale.Code, stale.Body.String())
	}
	fresh := loginHTTP(t, s, "alice", "new password")
	if w := httpCall(s, "GET", "/console/v1/user/info", "", fresh.Token); w.Code != 200 {
		t.Fatal("new password session rejected", w.Code)
	}
}
