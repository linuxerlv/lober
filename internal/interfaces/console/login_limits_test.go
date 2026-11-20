package console

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
)

type limitedLoginRegistry struct {
	identity.Registry
	login func(context.Context, identity.LoginCommand) (identity.SessionResult, error)
}

func (r *limitedLoginRegistry) Login(ctx context.Context, command identity.LoginCommand) (identity.SessionResult, error) {
	return r.login(ctx, command)
}

func loginLimitFixture(t *testing.T, login func(context.Context, identity.LoginCommand) (identity.SessionResult, error)) (*gin.Engine, *atomic.Int64) {
	t.Helper()
	now := new(atomic.Int64)
	now.Store(fixedAt)
	s := &server{deps: Deps{Clock: now.Load, Identity: &limitedLoginRegistry{login: login}}, loginLimits: newLoginLimits(now.Load)}
	engine := gin.New()
	engine.POST("/console/v1/auth/login", s.login)
	return engine, now
}

func limitedLoginCall(engine http.Handler, peer, username, forwarded string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/console/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"fixture"}`, username)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = peer
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
		req.Header.Set("X-Real-IP", forwarded)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestLoginLimitsAccountWindowAndNormalization(t *testing.T) {
	var calls atomic.Int32
	engine, now := loginLimitFixture(t, func(context.Context, identity.LoginCommand) (identity.SessionResult, error) {
		calls.Add(1)
		return identity.SessionResult{}, identity.ErrLoginFailed
	})
	for i := 0; i < 10; i++ {
		if w := limitedLoginCall(engine, fmt.Sprintf("192.0.2.%d:1234", i+1), "alice", ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("未达到限制就拒绝：%d", w.Code)
		}
	}
	for _, name := range []string{" ALICE ", "Alice", "alice"} {
		w := limitedLoginCall(engine, "192.0.2.100:1234", name, "")
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
			t.Fatalf("同一账号绕过限制：%d，%s", w.Code, w.Header().Get("Retry-After"))
		}
	}
	if calls.Load() != 10 {
		t.Fatal("被限制的请求仍执行密码校验", calls.Load())
	}
	now.Add(59)
	if w := limitedLoginCall(engine, "192.0.2.100:1234", "alice", ""); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("窗口结束前提前放行", w.Code, w.Header())
	}
	now.Add(1)
	if w := limitedLoginCall(engine, "192.0.2.100:1234", "alice", ""); w.Code != 401 {
		t.Fatal("窗口结束后未恢复", w.Code)
	}
}

func TestLoginLimitsPeerIgnoresForwardedHeadersAndPorts(t *testing.T) {
	engine, _ := loginLimitFixture(t, func(context.Context, identity.LoginCommand) (identity.SessionResult, error) {
		return identity.SessionResult{}, nil
	})
	for i := 0; i < 30; i++ {
		if w := limitedLoginCall(engine, fmt.Sprintf("192.0.2.10:%d", 1000+i), fmt.Sprintf("user%d", i), fmt.Sprintf("198.51.100.%d", i+1)); w.Code != 200 {
			t.Fatal("正常请求被拒绝", i, w.Code)
		}
	}
	if w := limitedLoginCall(engine, "192.0.2.10:9999", "another", "203.0.113.99"); w.Code != 429 {
		t.Fatal("换端口或转发头绕过来源限制", w.Code)
	}
	if w := limitedLoginCall(engine, "192.0.2.11:9999", "another", ""); w.Code != 200 {
		t.Fatal("其他来源被一并限制", w.Code)
	}
}

func TestLoginLimitsGlobalWindow(t *testing.T) {
	engine, now := loginLimitFixture(t, func(context.Context, identity.LoginCommand) (identity.SessionResult, error) {
		return identity.SessionResult{}, nil
	})
	for i := 0; i < 120; i++ {
		if w := limitedLoginCall(engine, fmt.Sprintf("192.0.2.%d:1000", i+1), fmt.Sprintf("user%d", i), ""); w.Code != 200 {
			t.Fatal("全局额度尚未用完", i, w.Code)
		}
	}
	if w := limitedLoginCall(engine, "198.51.100.1:1000", "fresh", ""); w.Code != 429 {
		t.Fatal("换来源和账号绕过全局限制", w.Code)
	}
	now.Add(60)
	if w := limitedLoginCall(engine, "198.51.100.1:1000", "fresh", ""); w.Code != 200 {
		t.Fatal("全局窗口未恢复", w.Code)
	}
}

func TestLoginLimitsConcurrentChecksAndRelease(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	engine, _ := loginLimitFixture(t, func(ctx context.Context, _ identity.LoginCommand) (identity.SessionResult, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return identity.SessionResult{}, identity.ErrLoginFailed
		case <-ctx.Done():
			return identity.SessionResult{}, ctx.Err()
		}
	})
	done := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() { done <- limitedLoginCall(engine, "192.0.2.10:1000", "alice", "").Code }()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("密码校验没有开始")
		}
	}
	if w := limitedLoginCall(engine, "192.0.2.11:1000", "bob", ""); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("没有立即拒绝超过并发数的请求", w.Code, w.Header())
	}
	once.Do(func() { close(release) })
	for i := 0; i < 4; i++ {
		if status := <-done; status != 401 {
			t.Fatal("已有请求没有正常结束", status)
		}
	}
	if w := limitedLoginCall(engine, "192.0.2.11:1000", "bob", ""); w.Code != 401 {
		t.Fatal("密码校验结束后名额未归还", w.Code)
	}
}

func TestLoginLimitsInstalledOnRealRoute(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	s.server.deps.Identity = &limitedLoginRegistry{login: func(context.Context, identity.LoginCommand) (identity.SessionResult, error) {
		return identity.SessionResult{}, identity.ErrLoginFailed
	}}
	for i := 0; i < 10; i++ {
		if w := limitedLoginCall(s.Engine, "192.0.2.10:1000", "alice", ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := limitedLoginCall(s.Engine, "192.0.2.10:1000", "alice", ""); w.Code != 429 {
		t.Fatal("真实路由没有安装登录限制", w.Code)
	}
}
