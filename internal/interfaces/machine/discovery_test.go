package machine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
)

type discoveryFixture struct{ boots, reports int }

func (f *discoveryFixture) Boot(_ context.Context, _, _ string, _ catalog.Arch, _ catalog.Firmware) ([]byte, error) {
	f.boots++
	return []byte("#!ipxe\n# discovery\n"), nil
}
func (f *discoveryFixture) Submit(_ context.Context, _, _ string, _ []byte) error {
	f.reports++
	return nil
}

func TestDiscoveryHTTP(t *testing.T) {
	f := new(discoveryFixture)
	g := new(fixtureGateway)
	h := NewWithDiscovery(g, f, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")})
	call := func(method, path, agent, token, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("User-Agent", agent)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(w, r)
		return w
	}
	boot := "/v1/boot?sn=SN-1&arch=x86_64&firmware=uefi"
	mac := "52:54:00:12:34:56"
	if w := call("GET", boot+"&mac="+mac, "iPXE", "", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "discovery") || f.boots != 1 {
		t.Fatalf("报到失败: %d %s", w.Code, w.Body)
	}
	if w := call("GET", boot, "iPXE", "", ""); w.Code != http.StatusOK || f.boots != 1 {
		t.Fatalf("旧引导请求访问了发现: %d", w.Code)
	}
	call("GET", boot+"&mac="+mac, "debian-installer", "", "")
	if f.boots != 1 {
		t.Fatal("安装器应答访问了发现")
	}
	g.result = execution.BootResult{Body: []byte("install"), Found: true}
	if w := call("GET", boot+"&mac="+mac, "iPXE", "", ""); w.Body.String() != "install" || f.boots != 1 {
		t.Fatalf("装机优先: %s", w.Body)
	}
	g.result = execution.BootResult{}
	blocked := httptest.NewRequest("GET", boot+"&mac="+mac, nil)
	blocked.RemoteAddr = "198.51.100.12:4567"
	blocked.Header.Set("X-Forwarded-For", "192.0.2.5")
	h.ServeHTTP(httptest.NewRecorder(), blocked)
	if f.boots != 1 {
		t.Fatal("装机网以外的来源不能发起发现")
	}
	path := "/v1/discovery/" + mac
	blockedReport := httptest.NewRequest("POST", path, strings.NewReader("{}"))
	blockedReport.RemoteAddr = blocked.RemoteAddr
	blockedReport.Header.Set("Authorization", "Bearer key")
	denied := httptest.NewRecorder()
	h.ServeHTTP(denied, blockedReport)
	if denied.Code != http.StatusNotFound || f.reports != 0 {
		t.Fatalf("装机网以外的来源不能上报: %d", denied.Code)
	}
	if w := call("POST", path, "", "", "{}"); w.Code != http.StatusUnauthorized {
		t.Fatalf("缺凭据: %d", w.Code)
	}
	if w := call("POST", path, "", "key", "{}"); w.Code != http.StatusNoContent || f.reports != 1 {
		t.Fatalf("上报失败: %d", w.Code)
	}
}

func (f *discoveryFixture) RegisteredSerial(context.Context, string) (string, error) {
	return "CONFIRMED", nil
}

func TestDiscoveryConfirmedSerialUsedForMachineLookup(t *testing.T) {
	f := new(discoveryFixture)
	g := &fixtureGateway{result: execution.BootResult{Body: []byte("install"), Found: true}}
	h := NewWithDiscovery(g, f, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")})
	const path = "/v1/boot?sn=WRONG&arch=x86_64&firmware=uefi&mac=52:54:00:12:34:56"
	for _, agent := range []string{"iPXE", "debian-installer"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("User-Agent", agent)
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if g.boot.SN != "CONFIRMED" || g.installer.SN != "CONFIRMED" {
		t.Fatalf("确认登记后的串号未用于引导: %q %q", g.boot.SN, g.installer.SN)
	}
}
