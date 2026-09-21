// 本文件测试客户端 IP 提取：只在直连 peer 是 loopback 时才信任 X-Real-IP
// 头部。对应实现计划任务 2，依据 SPEC §1.6、§9.2。
package gateway

import (
	"net/http"
	"testing"
)

// newRequestWithPeer 构造一个带指定 RemoteAddr（直连 peer，形如
// "1.2.3.4:5678"）与可选 X-Real-IP 头部的请求，供 clientIP 测试使用。
func newRequestWithPeer(t *testing.T, remoteAddr, xRealIP string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.RemoteAddr = remoteAddr
	if xRealIP != "" {
		req.Header.Set("X-Real-IP", xRealIP)
	}
	return req
}

// TestClientIP_LoopbackPeerWithHeader_UsesHeader 锁住：loopback peer 加头部
// → 用头部值。这是 nginx 反代场景的正常路径（§1.6：nginx 让直连 peer 变成
// 回环地址，必须从头部取真实 IP）。
func TestClientIP_LoopbackPeerWithHeader_UsesHeader(t *testing.T) {
	req := newRequestWithPeer(t, "127.0.0.1:12345", "192.0.2.10")
	got := clientIP(req)
	if got != "192.0.2.10" {
		t.Errorf("clientIP() = %q, want %q —— 直连 peer 是 loopback（nginx 反代典型场景）时，"+
			"必须信任 X-Real-IP 头部里的真实客户端 IP，否则审计日志会全部记成回环地址",
			got, "192.0.2.10")
	}
}

// TestClientIP_NonLoopbackPeerWithHeader_IgnoresHeader 是安全关键那条：
// 非 loopback peer 加头部 → 用真实 peer，绝不能信任头部。
//
// 不加这个判断的后果：网关端点本身无认证，任何人可以伪造 X-Real-IP 头部
// 冒充其它机器请求装机脚本——装机 VLAN 上一台失陷主机就能用别的机器的
// 身份取应答文件（计划任务 2「不加这个判断的后果」）。
func TestClientIP_NonLoopbackPeerWithHeader_IgnoresHeader(t *testing.T) {
	req := newRequestWithPeer(t, "10.0.5.7:54321", "192.0.2.10")
	got := clientIP(req)
	if got != "10.0.5.7" {
		t.Errorf("clientIP() = %q, want %q（真实 peer）—— 直连 peer 不是 loopback 时，"+
			"绝不能信任 X-Real-IP 头部：伪造者可以在装机 VLAN 上塞入任意 IP，"+
			"冒充其它机器的身份骗取装机脚本，这是本函数存在的最核心安全边界",
			got, "10.0.5.7")
	}
}

// TestClientIP_LoopbackPeerWithoutHeader_UsesPeer 锁住：loopback peer 无
// 头部 → 用真实 peer（没有头部可信任，只能用直连地址本身）。
func TestClientIP_LoopbackPeerWithoutHeader_UsesPeer(t *testing.T) {
	req := newRequestWithPeer(t, "127.0.0.1:12345", "")
	got := clientIP(req)
	if got != "127.0.0.1" {
		t.Errorf("clientIP() = %q, want %q —— 没有 X-Real-IP 头部时，"+
			"只能用直连 peer 本身作为客户端 IP", got, "127.0.0.1")
	}
}

// TestClientIP_IPv6LoopbackPeerWithHeader_UsesHeader 锁住：IPv6 回环
// （::1）也被识别为 loopback。不能只比较字符串 "127.0.0.1"——nginx 若监听
// IPv6 回环，直连 peer 会是 "::1"，这条判定同样要生效。
func TestClientIP_IPv6LoopbackPeerWithHeader_UsesHeader(t *testing.T) {
	req := newRequestWithPeer(t, "[::1]:12345", "192.0.2.10")
	got := clientIP(req)
	if got != "192.0.2.10" {
		t.Errorf("clientIP() = %q, want %q —— IPv6 回环 ::1 必须被识别为 loopback，"+
			"否则 nginx 监听 IPv6 回环时头部判定会失效", got, "192.0.2.10")
	}
}

// TestClientIP_IPv6LoopbackPeerWithoutHeader_UsesPeer 补充覆盖 IPv6 回环
// 无头部时的行为，与 IPv4 对称。
func TestClientIP_IPv6LoopbackPeerWithoutHeader_UsesPeer(t *testing.T) {
	req := newRequestWithPeer(t, "[::1]:12345", "")
	got := clientIP(req)
	if got != "::1" {
		t.Errorf("clientIP() = %q, want %q", got, "::1")
	}
}

// TestClientIP_HeaderNonIPContent_FallsBackToPeer 锁住：头部含非 IP 内容时
// 回落到真实 peer——头部值本身也要解析校验，伪造者可能塞进非 IP 的内容，
// 那会污染审计日志（计划任务 2「实现要点」最后一条）。
func TestClientIP_HeaderNonIPContent_FallsBackToPeer(t *testing.T) {
	req := newRequestWithPeer(t, "127.0.0.1:12345", "not-an-ip; DROP TABLE tasks")
	got := clientIP(req)
	if got != "127.0.0.1" {
		t.Errorf("clientIP() = %q, want %q（真实 peer）—— 头部值不是合法 IP 时必须回落到"+
			"真实 peer，而不是把非 IP 内容原样当作客户端 IP 使用，否则审计日志会被污染",
			got, "127.0.0.1")
	}
}
