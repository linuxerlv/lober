package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/application/ports"
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

func TestHostImportRejectsOversizedStream(t *testing.T) {
	c, w := testContext(t)
	c.Request = httptest.NewRequest(http.MethodPost, "/console/v1/hosts/import", strings.NewReader(strings.Repeat("a", (16<<20)+1)))
	c.Request.ContentLength = -1
	registry := &bodyLimitImport{}
	s := &server{deps: Deps{HostRegistry: registry}}
	s.importHosts(c)
	if w.Code != http.StatusRequestEntityTooLarge || registry.calls != 0 {
		t.Fatalf("状态码=%d，导入调用次数=%d", w.Code, registry.calls)
	}
}
