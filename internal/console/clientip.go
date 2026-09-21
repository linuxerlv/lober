// 本文件实现客户端 IP 取法：只在直连 peer 是 loopback 时信任 nginx 注入的
// 头部。对应实现计划任务 2，依据 §1.6（与 M5 网关一致的方向）。
//
// 审计日志记全回环地址就失去了追溯价值——而这个错误不会有任何报错，
// 是那种「测试全过、生产环境悄悄失真」的典型坑，所以判定逻辑必须显式写
// 出来并单独测试，不能散落在 handler 里顺手拼一下。
package console

import (
	"net"
	"net/http"
)

// defaultTrustedProxyPeer 判定 remoteAddr（形如 "host:port"）的主机部分是
// 否为 loopback 地址。解析失败时保守地返回 false——不信任头部好过错误地
// 信任一个解析不出来的地址。
func defaultTrustedProxyPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// 没有端口部分时 remoteAddr 本身可能就是纯 host（极少见，通常
		// http.Request.RemoteAddr 总是带端口），兜底再按纯地址解析一次。
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// clientIP 按「直连 peer 是 loopback 时才信任 X-Forwarded-For/X-Real-IP」
// 的规则取出审计要记录的客户端 IP。
//
// 不信任头部时直接返回 RemoteAddr 的主机部分——这一支路径对应「请求不是
// 经 nginx 转发而来」（例如测试直连、或部署配置有误导致裸机能直接打到
// Go 进程），此时头部内容不可信，必须用 TCP 连接本身的地址，不能读任何
// 请求头。
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if !s.trustedProxyPeer(r.RemoteAddr) {
		return host
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For 可能是逗号分隔的多级代理链，第一个是最初的
		// 客户端地址（nginx 单级代理场景下就是唯一值，但仍按通用约定取
		// 第一段，避免未来加了第二级代理时取错）。
		if idx := indexByte(xff, ','); idx >= 0 {
			return trimSpace(xff[:idx])
		}
		return trimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return trimSpace(xr)
	}
	return host
}

// indexByte 与 trimSpace 是避免额外 import strings 而写的两个极小工具
// 函数——本文件只需要这两个操作，没有必要为此引入整个 strings 包的
// 认知负担（读者一看函数名就知道在做什么，不需要跳到 strings 文档确认
// TrimSpace 对哪些字符生效）。
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
