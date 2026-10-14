package console

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"log/slog"
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
	token, u := seedHTTPUser(t, s, st, "alice", store.RoleAdmin)
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/probe", perm: PermUserManage, tag: "probe", summary: "Protected probe", handle: func(c *gin.Context) { Ok(c, "allowed") }}})
	if w := httpCall(s, "GET", "/console/v1/probe", "", token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	u.Role = store.RoleViewer
	var err error
	u, err = st.UpdateUser(t.Context(), u, store.Audit{Actor: "test", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	if w := httpCall(s, "GET", "/console/v1/probe", "", token); w.Code != 403 {
		t.Fatal(w.Body.String())
	}
	u.Disabled = true
	_, err = st.UpdateUser(t.Context(), u, store.Audit{Actor: "test", Action: "user.update", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	if w := httpCall(s, "GET", "/console/v1/user/info", "", token); w.Code != 401 {
		t.Fatal(w.Body.String())
	}
}
func Test审计取当前用户路由动作与注入时钟(t *testing.T) {
	s, st, now := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "alice", store.RoleAdmin)
	now.Store(fixedAt + 42)
	var got store.Audit
	s.server.installRoutes(s, []route{{method: "POST", path: "/console/v1/probe", perm: PermUserManage, audit: "probe.write", tag: "probe", summary: "Audited probe", handle: func(c *gin.Context) { got = s.server.auditFrom(c, "reason"); Ok(c, nil) }}})
	if w := httpCall(s, "POST", "/console/v1/probe", "", token); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got.Actor != "alice" || got.Action != "probe.write" || got.Detail != "reason" || got.At != fixedAt+42 {
		t.Fatal(got)
	}
	if !strings.Contains(httpCall(s, "GET", "/console/v1/user/info", "", token).Body.String(), "alice") {
		t.Fatal("user info")
	}
}
func TestPanic兜底回500且不泄露令牌(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token, _ := seedHTTPUser(t, s, st, "alice", store.RoleViewer)
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
	token, _ := seedHTTPUser(t, s, st, "alice", store.RoleViewer)
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/tasks/:id/stream", perm: PermTaskRead, tag: "probe", summary: "Stream probe", stream: true, handle: func(c *gin.Context) { Ok(c, true) }}})
	s.server.installRoutes(s, []route{{method: "GET", path: "/console/v1/probe/stream", perm: PermAnyUser, tag: "probe", summary: "Not an SSE route", handle: func(c *gin.Context) { Ok(c, true) }}})
	if w := httpCall(s, "GET", "/console/v1/user/info?access_token="+token, "", ""); w.Code != 401 {
		t.Fatal("query credential accepted outside stream")
	}
	if w := httpCall(s, "GET", "/console/v1/probe/stream?access_token="+token, "", ""); w.Code != 401 {
		t.Fatal("path suffix bypassed descriptor")
	}
	if w := httpCall(s, "GET", "/console/v1/tasks/id/stream?access_token="+token, "", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := httpCall(s, "GET", "/console/v1/tasks/id/stream?access_token="+token, "", "wrong"); w.Code != 401 {
		t.Fatal("header bypassed")
	}
}
func Test会话期限与退出登录(t *testing.T) {
	now := fixedAt
	s := newSessions(10, func() int64 { return now })
	token := s.put("alice")
	if len(token) != 64 {
		t.Fatal(token)
	}
	if name, ok := s.lookup(token); !ok || name != "alice" {
		t.Fatal("missing session")
	}
	now += 10
	if _, ok := s.lookup(token); ok {
		t.Fatal("expired")
	}
	token = s.put("alice")
	s.drop(token)
	if _, ok := s.lookup(token); ok {
		t.Fatal("logout")
	}
}
func Test路由权限全部登记(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	known := map[string]bool{PermAnyUser: true, "": true}
	for _, p := range AllPerms() {
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
