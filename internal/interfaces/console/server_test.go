package console

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func Test没有令牌一律401且未知路径404(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	if len(s.Routes()) == 0 {
		t.Fatal("empty route table")
	}
	for _, rt := range s.Routes() {
		if rt.Perm != "" {
			if w := httpCall(s, rt.Method, rt.Path, "", ""); w.Code != 401 {
				t.Fatal(rt, w.Code, w.Body.String())
			}
		}
	}
	if w := httpCall(s, "GET", "/console/v1/nope", "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func Test权限使用仓储里的当前角色与禁用状态(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, u := seedHTTPUser(t, s, st, "alice", model.RoleAdmin)
	seedHTTPUser(t, s, st, "backup", model.RoleAdmin)
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/probe", perm: PermUserManage, tag: "probe", summary: "Protected probe", handle: func(c *gin.Context) { Ok(c, "allowed") }}})
	if w := httpCall(s, "GET", "/console/v1/probe", "", token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	u.Role = model.RoleViewer
	var err error
	u, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "test", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	if w := httpCall(s, "GET", "/console/v1/probe", "", token); w.Code != 403 {
		t.Fatal(w.Body.String())
	}
	u.Disabled = true
	_, err = st.UpdateUser(t.Context(), u, repository.Audit{Actor: "test", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", token); w.Code != 401 {
		t.Fatal(w.Body.String())
	}
}
func Test审计取当前用户路由动作与注入时钟(t *testing.T) {
	s, st, _, task, now := taskFixture(t)
	token, _ := seedHTTPUser(t, s, st, "alice", model.RoleAdmin)
	now.Store(fixedAt + 42)
	backend := &taskControlFixture{st: st}
	setTaskControl(s, backend)
	if w := httpCall(s, "POST", "/console/v1/tasks/"+task.ID+"/cancel", `{"actor":"forged","action":"forged","detail":"reason","at":1}`, token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(backend.calls) != 1 {
		t.Fatal(backend.calls)
	}
	got := backend.calls[0]
	if got.Actor != "alice" || got.Action != "task.cancel" || got.Detail != "" || got.At != fixedAt+42 {
		t.Fatal(got)
	}
	if !strings.Contains(httpCall(s, "GET", "/console/v1/user/info", "", token).Body.String(), "alice") {
		t.Fatal("user info")
	}
}
func TestPanic兜底回500且不泄露令牌(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "alice", model.RoleViewer)
	var log bytes.Buffer
	s.server.logger = slog.New(slog.NewTextHandler(&log, nil))
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/panic", perm: PermAnyUser, tag: "probe", summary: "Panic probe", handle: func(c *gin.Context) { panic("secret-value") }}})
	w := httpCall(s, "GET", "/console/v1/panic?access_token="+token, "", token)
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret-value") || strings.Contains(log.String(), token) || strings.Contains(log.String(), "secret-value") {
		t.Fatal("panic secret escaped")
	}
}
func Test查询令牌只允许SSE且Authorization优先(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "alice", model.RoleViewer)
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/probe/sse/:id", perm: PermTaskRead, tag: "probe", summary: "Stream probe", stream: true, handle: func(c *gin.Context) { Ok(c, true) }}})
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/probe/stream", perm: PermAnyUser, tag: "probe", summary: "Not an SSE route", handle: func(c *gin.Context) { Ok(c, true) }}})
	if w := httpCall(s, "GET", "/console/v1/user/info?access_token="+token, "", ""); w.Code != 401 {
		t.Fatal("query credential accepted outside stream")
	}
	if w := httpCall(s, "GET", "/console/v1/probe/stream?access_token="+token, "", ""); w.Code != 401 {
		t.Fatal("path suffix bypassed descriptor")
	}
	if w := httpCall(s, "GET", "/console/v1/probe/sse/id?access_token="+token, "", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := httpCall(s, "GET", "/console/v1/probe/sse/id?access_token="+token, "", "wrong"); w.Code != 401 {
		t.Fatal("header bypassed")
	}
}
func Test会话期限与退出登录(t *testing.T) {
	now := fixedAt
	s := identity.NewSessionBook(10, func() int64 { return now }, credentials.NewTokens())
	token := s.Put("alice")
	if len(token) != 64 {
		t.Fatal(token)
	}
	if name, ok := s.Lookup(token); !ok || name != "alice" {
		t.Fatal("missing session")
	}
	now += 10
	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired")
	}
	token = s.Put("alice")
	s.Drop(token)
	if _, ok := s.Lookup(token); ok {
		t.Fatal("logout")
	}
}
func Test路由权限全部登记(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	known := map[string]bool{PermAnyUser: true, "": true}
	for _, p := range access.AllPerms() {
		known[p] = true
	}
	for _, r := range s.Routes() {
		if !known[r.Perm] {
			t.Fatal(r)
		}
		if r.Perm == "" && r.Path != "/console/v1/auth/login" {
			t.Fatal("unintended public API")
		}
	}
	if model.ConsoleUnauthorized == "" {
		t.Fatal("empty auth explanation")
	}
}

func Test错误Authorization不能回退查询令牌(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "reader", model.RoleViewer)
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/bearer-probe", perm: PermTaskRead, tag: "probe", summary: "Stream probe", stream: true, handle: func(c *gin.Context) { Ok(c, true) }}})
	for _, header := range []string{"Basic value", "Bearer", "Bearer one two"} {
		req := httptest.NewRequest("GET", "/console/v1/bearer-probe?access_token="+token, nil)
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal("错误请求头使用了查询令牌", header, w.Code)
		}
	}
}
func Test只有路由说明时可构造但身份请求明确失败(t *testing.T) {
	s := New(emptyConsoleDeps(func() int64 { return fixedAt }))
	if len(s.Routes()) == 0 || s.OpenAPI() == nil {
		t.Fatal("路由说明不可用")
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", ""); w.Code != 500 {
		t.Fatal("缺身份依赖未明确失败", w.Code)
	}
}
