package machine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/execution"
)

const invBody = `{"firmware":"bios","cpu":{"model":"Xeon","cores":4},"memory_mib":8192,
"disks":[{"name":"vda","bytes":8589934592},{"name":"vdb","bytes":9663676416}]}`

const invToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// 上报成功时网关返回 nil，入口回 204 不带正文。
func Test采集上报成功回204(t *testing.T) {
	f := &fixtureGateway{}
	w := httptest.NewRecorder()
	New(f).ServeHTTP(w, httptest.NewRequest("POST", "/v1/inventory/SN-1/"+invToken, strings.NewReader(invBody)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("状态 %d：%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("成功时不该有正文：%q", w.Body.String())
	}
	if f.inventory.SN != "SN-1" || f.inventory.Token != invToken || string(f.inventory.Body) != invBody {
		t.Fatalf("传给网关的入参不对：%+v", f.inventory)
	}
}

// 路由与方法的边界：段数不对、方法不对、带查询串、令牌不是安全词。
func Test采集上报路径与方法(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"GET 不许", "GET", "/v1/inventory/SN-1/" + invToken, 404},
		{"少一段", "POST", "/v1/inventory/SN-1", 404},
		{"多一段", "POST", "/v1/inventory/SN-1/" + invToken + "/extra", 404},
		{"带查询串", "POST", "/v1/inventory/SN-1/" + invToken + "?x=1", 400},
		{"令牌不是安全词", "POST", "/v1/inventory/SN-1/" + strings.Repeat("%21", 8), 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			New(&fixtureGateway{}).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(invBody)))
			if w.Code != tc.want {
				t.Fatalf("状态 %d，想要 %d：%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// 网关的错误分类逐条映射成状态码。分类本身由执行模块定义，入口只查 Kind。
func Test采集上报的错误映射(t *testing.T) {
	for _, tc := range []struct {
		kind execution.GatewayFailureKind
		want int
	}{
		// Unauthorized 在既有网关里映射成 403（文件下载被拒），采集上报沿用同一分类，
		// 所以这里也是 403。想区分 401 就要另起一个 Kind，不值得为一条响应多分一类。
		{execution.Unauthorized, 403},
		{execution.FileNotFound, 404},
		{execution.InventoryBusy, 409},
		{execution.ReportRejected, 422},
		{execution.NotSupported, 404},
		{execution.LookupUnavailable, 503},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			f := &fixtureGateway{err: &execution.GatewayError{Kind: tc.kind}}
			w := httptest.NewRecorder()
			New(f).ServeHTTP(w, httptest.NewRequest("POST", "/v1/inventory/SN-1/"+invToken, strings.NewReader(invBody)))
			if w.Code != tc.want {
				t.Fatalf("状态 %d，想要 %d", w.Code, tc.want)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("泄露了底层原因")
			}
		})
	}
}

// 正文超限在入口就挡下，不进网关。
func Test采集上报正文超限(t *testing.T) {
	f := &fixtureGateway{}
	w := httptest.NewRecorder()
	New(f).ServeHTTP(w, httptest.NewRequest("POST", "/v1/inventory/SN-1/"+invToken, strings.NewReader(strings.Repeat("x", maxInventoryBytes+1))))
	if w.Code != 400 {
		t.Fatalf("状态 %d", w.Code)
	}
	if f.inventory.SN != "" {
		t.Fatal("超限的正文不该进网关")
	}
}
