package machine

import (
	"net/http"
	"testing"
)

func TestPXEBootAcceptsMACWithoutBreakingLegacyGateway(t *testing.T) {
	_, h := gatewayFixture(t)
	w := call(h, http.MethodGet, "/v1/boot?sn=PXE-UNREGISTERED&arch=x86_64&firmware=bios&mac=52:54:00:42:41:21", "")
	if w.Code != http.StatusOK {
		t.Fatalf("携带启动网口的请求不应被旧入口拒绝: %d %s", w.Code, w.Body)
	}
}
