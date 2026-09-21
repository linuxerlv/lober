// 本文件实现路由核对：遍历预期的六条路径，断言每条都能路由到非 404
// （用最小合法请求）。对应实现计划任务 8。
//
// 这条测试的价值是抓「注册路径时写错一个字符」——那种错误在集成前发现
// 不了，而集成时的表现是「机器拿不到应答文件」。
package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRouteAudit_AllSixRoutesReachable 锁住：六条路由全部可达（不是
// ServeMux 默认的 404，无论业务逻辑层面最终返回什么状态码）。
func TestRouteAudit_AllSixRoutesReachable(t *testing.T) {
	deps := newTestDeps()
	srv := NewServer(deps)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"boot.ipxe", http.MethodGet, "/boot.ipxe"},
		{"v1/boot", http.MethodGet, "/v1/boot"},
		{"v1/answer", http.MethodGet, "/v1/answer/t/tok/f"},
		{"v1/signal", http.MethodPost, "/v1/signal/t/tok/pre-start"},
		{"v1/facts write", http.MethodPost, "/v1/facts/t/tok"},
		{"v1/facts read", http.MethodGet, "/v1/facts/t/tok"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			// 六个用例用的都是无效 token/文件名，业务逻辑对此的正确响应是
			// 403（token 无效）——不会是 404，因为本实现里 404 只在
			// 「token 校验通过之后，文件名与该任务的应答文件名不一致」这一
			// 具体分支产生（见 answer.go），而这六个用例连 token 校验都
			// 过不了。因此这里若观察到 404，唯一合理的解释就是 ServeMux
			// 没有为这条路径注册任何处理函数（模式字符串写错），不是业务
			// 逻辑本身选择了 404。
			if rec.Code == http.StatusNotFound {
				t.Errorf("%s %s 返回 404，可能是路由未注册或路径字符串写错（业务逻辑在"+
					"token 无效时应回 403，不应该是 404）", c.method, c.path)
			}
		})
	}
}
