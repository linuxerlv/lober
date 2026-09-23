// 本文件测试 secure_link 签名 URL 的签发与校验往返。对应 SPEC §9.3、
// t11-securelink 任务「测试要锁住」四条：签名往返、过期生效、IP 绑定生效、
// 密钥不同则签名不同。
//
// verifySecureLinkIndependent 是本文件独立实现的 nginx 校验逻辑镜像
// （不调用 siglink.go 的 secureLinkMD5/nginxSecureLinkBase64，自己重新
// 写一遍同一个公式）——这是本机没有真实 nginx 时唯一能证明「Go 签发的
// 签名与 nginx 会计算出的签名一致」的办法（本任务 verifyOutcome 会说明
// 这一点与其边界）。若这份独立实现与 siglink.go 的实现出现分歧，说明
// 至少有一方理解 nginx 公式的方式错了，两者代码互相印证比单一实现的
// 单元测试更能发现「我自己想岔了但测试用同一份错误逻辑生成期望值」这类
// 问题。
package gateway

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// verifySecureLinkIndependent 独立重新实现 nginx secure_link 模块的校验
// 算法：给定请求路径 uri、客户端 IP、密钥，以及从请求 query 里取出的
// md5/expires 两个参数，判断这次访问是否应该被 nginx 放行。
//
// 返回值三态对应 nginx 配置里 secure_link 变量的三种可能取值（SPEC §9.3
// 示例）："" 表示签名校验失败（拒绝，403）、"0" 表示签名对但已过期
// （410）、非空非 "0" 表示通过。本函数用两个 bool 返回值表达这三态
// （valid=false 对应 ""；valid=true 且 expired=true 对应 "0"；valid=true
// 且 expired=false 对应通过）。
func verifySecureLinkIndependent(uri, remoteAddr, secret string, md5Param string, expiresParam string, now int64) (valid bool, expired bool) {
	expiresAt, err := strconv.ParseInt(expiresParam, 10, 64)
	if err != nil {
		return false, false
	}

	input := fmt.Sprintf("%d%s%s %s", expiresAt, uri, remoteAddr, secret)
	sum := md5.Sum([]byte(input))
	encoded := base64.StdEncoding.EncodeToString(sum[:])
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	encoded = strings.TrimRight(encoded, "=")

	if encoded != md5Param {
		return false, false
	}
	if expiresAt < now {
		return true, true
	}
	return true, false
}

// signedURLParts 从 SignedArtifactURL 的产出里拆出校验函数需要的三个值：
// 路径（不含 query）、md5 参数、expires 参数。
func signedURLParts(t *testing.T, signedURL string) (path, md5Param, expiresParam string) {
	t.Helper()
	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatalf("解析签名 URL 失败: %v, url=%q", err, signedURL)
	}
	return parsed.Path, parsed.Query().Get("md5"), parsed.Query().Get("expires")
}

// TestSignedArtifactURL_RewritesToSecurePrefix 锁住路径前缀重写：
// /artifacts/ 必须换成 /artifacts/secure/，这是 deploy/nginx/lober.conf
// 新增 location 匹配的前缀，两者必须逐字节一致。
func TestSignedArtifactURL_RewritesToSecurePrefix(t *testing.T) {
	unsigned := "http://artifacts.example.test/artifacts/rocky94/vmlinuz"
	signed, ok := SignedArtifactURL(unsigned, "test-secret", "10.10.0.5", 1000)
	if !ok {
		t.Fatalf("SignedArtifactURL 返回 ok=false，want true（输入符合预期格式）")
	}
	path, _, _ := signedURLParts(t, signed)
	if path != "/artifacts/secure/rocky94/vmlinuz" {
		t.Errorf("签名 URL 路径 = %q, want %q", path, "/artifacts/secure/rocky94/vmlinuz")
	}
}

// TestSignedArtifactURL_UnexpectedPrefix_ReturnsFalse 锁住防御性分支：
// 不含 /artifacts/ 前缀的输入不应被签名，返回 ok=false 且原样返回，供
// 调用方跳过替换而不是签发畸形 URL。
func TestSignedArtifactURL_UnexpectedPrefix_ReturnsFalse(t *testing.T) {
	unsigned := "http://gw.example.test/v1/boot"
	signed, ok := SignedArtifactURL(unsigned, "test-secret", "10.10.0.5", 1000)
	if ok {
		t.Fatalf("SignedArtifactURL 返回 ok=true，want false（输入不含 /artifacts/ 前缀）")
	}
	if signed != unsigned {
		t.Errorf("ok=false 时应原样返回输入 URL，got %q, want %q", signed, unsigned)
	}
}

// TestSecureLink_RoundTrip_ValidatesWithIndependentImplementation 锁住
// 「测试要锁住」第一条：Go 签发 → 用独立实现的校验逻辑验证通过。这是
// 本机没有真实 nginx 时唯一能核实两侧公式一致的手段。
func TestSecureLink_RoundTrip_ValidatesWithIndependentImplementation(t *testing.T) {
	unsigned := "http://artifacts.example.test/artifacts/rocky94/vmlinuz"
	const secret = "shared-secret-value"
	const clientIP = "10.10.0.42"
	const expiresAt = int64(2000000000) // 远未来时间戳，避免测试本身受「当前时间」影响

	signed, ok := SignedArtifactURL(unsigned, SecureLinkSecret(secret), clientIP, expiresAt)
	if !ok {
		t.Fatalf("SignedArtifactURL 返回 ok=false")
	}

	path, md5Param, expiresParam := signedURLParts(t, signed)
	valid, expired := verifySecureLinkIndependent(path, clientIP, secret, md5Param, expiresParam, 0)
	if !valid {
		t.Fatalf("独立实现校验签名失败——两侧公式不一致，签名 URL: %q", signed)
	}
	if expired {
		t.Errorf("独立实现判定已过期，want 未过期（expiresAt 是远未来时间戳）")
	}
}

// TestSecureLink_ExpiredURL_FailsIndependentValidation 锁住「测试要锁住」
// 第二条：过期时间生效——过期的 URL 校验失败（判定为 expired=true）。
func TestSecureLink_ExpiredURL_FailsIndependentValidation(t *testing.T) {
	unsigned := "http://artifacts.example.test/artifacts/rocky94/vmlinuz"
	const secret = "shared-secret-value"
	const clientIP = "10.10.0.42"
	const expiresAt = int64(1000) // 早已过去的时间戳

	signed, ok := SignedArtifactURL(unsigned, SecureLinkSecret(secret), clientIP, expiresAt)
	if !ok {
		t.Fatalf("SignedArtifactURL 返回 ok=false")
	}

	path, md5Param, expiresParam := signedURLParts(t, signed)
	// now 用一个远大于 expiresAt 的值模拟「现在已经超过过期时间」。
	valid, expired := verifySecureLinkIndependent(path, clientIP, secret, md5Param, expiresParam, 2000000000)
	if !valid {
		t.Fatalf("签名本身应该校验通过（md5 没变），want valid=true, expired=true；got valid=false")
	}
	if !expired {
		t.Errorf("expiresAt=%d 早于 now=2000000000，want expired=true", expiresAt)
	}
}

// TestSecureLink_DifferentClientIP_FailsValidation 锁住「测试要锁住」
// 第三条：IP 绑定生效——换一个 IP 校验失败。对应 SPEC §9.3「$remote_addr
// 进签名可让 URL 绑定到具体机器，防止 token 横向传播」。
func TestSecureLink_DifferentClientIP_FailsValidation(t *testing.T) {
	unsigned := "http://artifacts.example.test/artifacts/rocky94/vmlinuz"
	const secret = "shared-secret-value"
	const signedForIP = "10.10.0.42"
	const differentIP = "10.10.0.99"
	const expiresAt = int64(2000000000)

	signed, ok := SignedArtifactURL(unsigned, SecureLinkSecret(secret), signedForIP, expiresAt)
	if !ok {
		t.Fatalf("SignedArtifactURL 返回 ok=false")
	}

	path, md5Param, expiresParam := signedURLParts(t, signed)
	valid, _ := verifySecureLinkIndependent(path, differentIP, secret, md5Param, expiresParam, 0)
	if valid {
		t.Errorf("用签发时不同的 IP（%q 而非 %q）校验，want 失败，got 通过——"+
			"这意味着签名没有真正绑定客户端 IP，token 可以被横向传播", differentIP, signedForIP)
	}
}

// TestSecureLink_DifferentSecret_ProducesDifferentSignature 锁住「测试要
// 锁住」第四条：密钥不同则签名不同（防止配置了空密钥还能用——本测试同时
// 覆盖空密钥场景：空密钥产出的签名必须与非空密钥的签名不同，否则空密钥
// 与「有真实密钥」在签名结果上无法区分，等于没有授权）。
func TestSecureLink_DifferentSecret_ProducesDifferentSignature(t *testing.T) {
	unsigned := "http://artifacts.example.test/artifacts/rocky94/vmlinuz"
	const clientIP = "10.10.0.42"
	const expiresAt = int64(2000000000)

	signedA, okA := SignedArtifactURL(unsigned, "secret-a", clientIP, expiresAt)
	signedB, okB := SignedArtifactURL(unsigned, "secret-b", clientIP, expiresAt)
	signedEmpty, okEmpty := SignedArtifactURL(unsigned, "", clientIP, expiresAt)
	if !okA || !okB || !okEmpty {
		t.Fatalf("SignedArtifactURL 返回 ok=false（okA=%v okB=%v okEmpty=%v）", okA, okB, okEmpty)
	}

	_, md5A, _ := signedURLParts(t, signedA)
	_, md5B, _ := signedURLParts(t, signedB)
	_, md5Empty, _ := signedURLParts(t, signedEmpty)

	if md5A == md5B {
		t.Errorf("secret-a 与 secret-b 产出相同签名 %q，want 不同——密钥必须真正参与签名计算", md5A)
	}
	if md5A == md5Empty {
		t.Errorf("非空密钥与空密钥产出相同签名 %q，want 不同——否则空密钥配置等于没有授权仍能签出"+
			"看似有效的 URL，config.Validate 拒绝空密钥启动（config_test.go）是这条测试的另一半防线", md5A)
	}
}

// TestNginxSecureLinkBase64_MatchesDocumentedTransform 单独锁住 base64
// 变体编码本身（不经过完整签名流程），直接对照 nginx 文档描述的转换规则：
// 标准 base64 编码后 + 换 -、/ 换 _、去掉末尾 =。用一个已知会产生 '+'、
// '/' 与填充字符的输入验证三种替换全部生效，不是只覆盖了恰好不需要
// 替换的输入。
func TestNginxSecureLinkBase64_MatchesDocumentedTransform(t *testing.T) {
	// 选取一段已知标准 base64 编码后同时含 '+'、'/' 与 '=' 填充的字节：
	// 0xfb 0xff 0xbf 三字节的标准 base64 是 "+/+/"，正好覆盖前两种替换；
	// 补一个奇数长度输入（1 字节）触发 "=" 填充覆盖第三种替换。
	got := nginxSecureLinkBase64([]byte{0xfb, 0xff, 0xbf})
	want := "-_-_"
	if got != want {
		t.Errorf("nginxSecureLinkBase64([0xfb,0xff,0xbf]) = %q, want %q（标准 base64 应为 %q，"+
			"+ 换 - 且 / 换 _ 之后应得到本期望值）", got, want, "+/+/")
	}

	gotPadded := nginxSecureLinkBase64([]byte{0x00})
	if strings.Contains(gotPadded, "=") {
		t.Errorf("nginxSecureLinkBase64([0x00]) = %q，含未去除的 '=' 填充字符", gotPadded)
	}
}
