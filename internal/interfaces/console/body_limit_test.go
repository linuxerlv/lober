package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

type bodyLimitLogin struct {
	identity.Registry
	calls int
}

func (f *bodyLimitLogin) Login(context.Context, identity.LoginCommand) (identity.SessionResult, error) {
	f.calls++
	return identity.SessionResult{}, nil
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	const valid = `{"username":"operator","password":"fixture"}`
	for _, tc := range []struct {
		name, body string
		stream     bool
		status     int
		calls      int
	}{
		{"exact", valid + strings.Repeat(" ", (16<<10)-len(valid)), false, 200, 1},
		{"one over", valid + strings.Repeat(" ", (16<<10)-len(valid)+1), true, 413, 0},
		{"declared", `{"username":"operator","password":"fixture","unused":"` + strings.Repeat("a", 2<<20) + `"}`, false, 413, 0},
		{"malformed", valid + `{}`, true, 400, 0},
		{"empty", "", false, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(t)
			c.Request = httptest.NewRequest(http.MethodPost, "/console/v1/auth/login", strings.NewReader(tc.body))
			if tc.stream {
				c.Request.ContentLength = -1
			}
			login := &bodyLimitLogin{}
			s := &server{deps: Deps{Identity: login}}
			s.login(c)
			if w.Code != tc.status || login.calls != tc.calls {
				t.Fatalf("状态码=%d，登录调用次数=%d；预期 %d、%d", w.Code, login.calls, tc.status, tc.calls)
			}
		})
	}
}

type bodyLimitImport struct {
	hosts.Registry
	calls int
}

func (f *bodyLimitImport) Import(context.Context, ports.Actor, hosts.ImportRequest) (hosts.ImportResult, error) {
	f.calls++
	return hosts.ImportResult{}, nil
}

func TestHostImportBodyBoundary(t *testing.T) {
	for _, size := range []int{16 << 20, (16 << 20) + 1} {
		c, w := testContext(t)
		c.Request = httptest.NewRequest(http.MethodPost, "/console/v1/hosts/import", strings.NewReader(strings.Repeat("a", size)))
		c.Request.ContentLength = -1
		registry := &bodyLimitImport{}
		s := &server{deps: Deps{HostRegistry: registry}}
		s.importHosts(c)
		if size == 16<<20 && (w.Code != http.StatusOK || registry.calls != 1) ||
			size > 16<<20 && (w.Code != http.StatusRequestEntityTooLarge || registry.calls != 0) {
			t.Fatalf("正文大小=%d，状态码=%d，导入调用次数=%d", size, w.Code, registry.calls)
		}
	}
}

func TestOrdinaryJSONBodyBoundary(t *testing.T) {
	const valid = `{"name":"fixture"}`
	for _, size := range []int{4 << 20, (4 << 20) + 1} {
		c, w := testContext(t)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(valid+strings.Repeat(" ", size-len(valid))))
		c.Request.ContentLength = -1
		var req map[string]string
		if ok := Bind(c, &req); size == 4<<20 && (!ok || req["name"] != "fixture") ||
			size > 4<<20 && (ok || w.Code != http.StatusRequestEntityTooLarge) {
			t.Fatalf("正文大小=%d，状态码=%d，解析成功=%t", size, w.Code, ok)
		}
	}
}

func TestLegacyLongPasswordCanBeRotated(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	payload := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	old := strings.Repeat("a", MaxLoginBodyLimit-len(payload(loginRequest{Username: "legacy"})))
	seedAuthUser(t, st, "legacy", model.RoleViewer, old)
	login := func(pw string) *httptest.ResponseRecorder {
		return httpCall(s, http.MethodPost, "/console/v1/auth/login", payload(loginRequest{Username: "legacy", Password: pw}), "")
	}
	if w := login(old); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("默认上限没有生效", w.Code)
	}
	s.server.deps.LoginBodyLimit = MaxLoginBodyLimit
	token := responseData[sessionResponse](t, login(old)).Token
	change := func(pw string) *httptest.ResponseRecorder {
		return httpCall(s, http.MethodPost, "/console/v1/auth/password", payload(passwordRequest{CurrentPassword: old, NewPassword: pw}), token)
	}
	if w := change(strings.Repeat("b", 2049)); w.Code != http.StatusBadRequest {
		t.Fatal("新长密码没有被拒绝", w.Code)
	}
	// JSON 编码会把每个字符转义成 6 字节。边界密码仍应能用默认上限登录。
	newPassword := strings.Repeat("<", 2048)
	if w := change(newPassword); w.Code != http.StatusOK {
		t.Fatal("短密码迁移失败，或上次拒绝更改了原密码", w.Code)
	}
	s.server.deps.LoginBodyLimit = 0
	if w := login(newPassword); w.Code != http.StatusOK {
		t.Fatal("改密后未能用默认上限登录", w.Code)
	}
}
