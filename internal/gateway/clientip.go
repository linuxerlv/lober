// 本文件实现客户端 IP 提取：只在直连 peer 是 loopback 时才信任 X-Real-IP
// 头部。对应实现计划任务 2，依据 SPEC §1.6、§9.2。
//
// 为什么需要这一步：nginx 反代让直连 peer 变成回环地址（§1.6），这会静默
// 破坏三件事：审计日志记全回环、按客户端限速失效、将来的制品签名对不上
// （nginx 校验时用真实客户端 IP，Go 签发时看到回环）。所以要从 nginx 注入
// 的头部取真实 IP——但必须加一个条件判断：仅当直连 peer 是 loopback 时才
// 信任那个头部，其余情况一律用真实 peer。
//
// 不加这个判断的后果：网关端点本身无认证，任何人可以伪造那个头部冒充
// 其它机器请求装机脚本——装机 VLAN 上一台失陷主机就能用别的机器的身份取
// 应答文件。
//
// clientIP 判定不作为任何端点的认证依据（docs/contracts/http-machine.md
// 认证矩阵小结明确：本文档所有认证判定只依赖 token 或 SN/MAC，不依赖客户端
// IP，SPEC §5.7 说明 booted 之后信号源 IP 会变，服务端不得用源 IP 做校验）。
// 它目前唯一的消费方是 syslog 接收器的「按源 IP 归档到对应任务」逻辑
// （UDP 场景没有 HTTP 请求与 X-Real-IP 头部，syslog.go 直接用 UDP 包的源
// 地址，不经过本函数）——本函数服务于机器面 HTTP 端点未来可能需要的审计
// 与限速用途。
package gateway

import (
	"net"
	"net/http"
)

// clientIP 从 HTTP 请求提取客户端 IP：仅当直连 peer（r.RemoteAddr）解析为
// loopback 地址（含 IPv6 的 ::1）且 X-Real-IP 头部非空、能解析为合法 IP 时，
// 才使用头部值；其余一切情况（peer 非 loopback、无头部、头部内容不是合法
// IP）都返回真实 peer 的 IP 部分。
//
// 返回值是字符串形式的 IP（不带端口），供调用方做字符串比较或再次
// net.ParseIP。r.RemoteAddr 本身格式不合法（极罕见，标准库通常保证它是
// host:port）时原样返回 r.RemoteAddr，不 panic。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// r.RemoteAddr 不是 host:port 形式——标准库的 net/http 通常保证这个
		// 格式，走到这里说明输入本身异常，原样返回而不是伪造一个值。
		return r.RemoteAddr
	}

	peerIP := net.ParseIP(host)
	if peerIP == nil || !peerIP.IsLoopback() {
		// 直连 peer 不是 loopback：不信任任何头部，无论头部是否存在。
		return host
	}

	headerVal := r.Header.Get("X-Real-IP")
	if headerVal == "" {
		return host
	}

	// 头部值本身也要解析校验：伪造者可能塞进非 IP 的内容，那会污染审计
	// 日志。校验不过时回落到真实 peer（此处即 loopback 地址本身）。
	if net.ParseIP(headerVal) == nil {
		return host
	}
	return headerVal
}
