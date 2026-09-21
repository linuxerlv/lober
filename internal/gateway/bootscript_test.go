// 本文件测试首跳脚本端点 GET /boot.ipxe。对应实现计划任务 3，依据
// docs/contracts/http-machine.md 端点 1、SPEC §4.1、§4.7。
package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// doBootScript 对给定基址构造一个 Server 并请求 GET /boot.ipxe，返回响应体
// 与 Content-Type。
func doBootScript(t *testing.T, gatewayBase string) (body, contentType string) {
	t.Helper()
	deps := newTestDeps()
	deps.Bases = model.BaseURLs{GatewayBase: gatewayBase, ArtifactBase: "http://artifacts.example.test"}
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodGet, "/boot.ipxe", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /boot.ipxe 状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return string(raw), resp.Header.Get("Content-Type")
}

// TestBootScript_ChainsToBootEndpoint 锁住：响应含 chain 命令且指向引导
// 决策端点。
func TestBootScript_ChainsToBootEndpoint(t *testing.T) {
	body, _ := doBootScript(t, "http://10.0.0.1")
	if !strings.Contains(body, "chain http://10.0.0.1/v1/boot?") {
		t.Errorf("响应体缺少指向 /v1/boot 的 chain 命令，got:\n%s", body)
	}
}

// TestBootScript_ContainsFiveIPXEVariablesVerbatim 锁住计划任务 3 最核心的
// 一条：五个 iPXE 变量要原样输出，不要被 Go 的模板引擎当成自己的占位符
// 处理。这五个变量是 §4.3 的身份查找键与 §4.6 一致性校验的输入，机器侧的
// iPXE 固件会在实际 chain 时把 ${serial} 等替换成真实值——如果网关侧提前
// 把它们解析掉或转义坏了，机器收到的 URL 就会带着字面的
// "${serial}"-被吃掉的残留或空字符串，第二跳请求直接查不到任何身份键。
func TestBootScript_ContainsFiveIPXEVariablesVerbatim(t *testing.T) {
	body, _ := doBootScript(t, "http://gw.example.test")

	vars := []string{
		"sn=${serial}",
		"mac=${net0/mac}",
		"uuid=${uuid}",
		"arch=${buildarch}",
		"plat=${platform}",
	}
	for _, v := range vars {
		if !strings.Contains(body, v) {
			t.Errorf("响应体缺少 iPXE 变量片段 %q（必须原样出现，不能被当作 Go 模板占位符"+
				"处理），got:\n%s", v, body)
		}
	}
}

// TestBootScript_BaseURLFromConfig 锁住：基址来自配置，换一个基址渲染，
// URL 跟着变。
func TestBootScript_BaseURLFromConfig(t *testing.T) {
	bodyA, _ := doBootScript(t, "http://gw-a.example.test")
	bodyB, _ := doBootScript(t, "http://gw-b.example.test")

	if !strings.Contains(bodyA, "http://gw-a.example.test/v1/boot") {
		t.Errorf("基址 A 未体现在响应体里，got:\n%s", bodyA)
	}
	if !strings.Contains(bodyB, "http://gw-b.example.test/v1/boot") {
		t.Errorf("基址 B 未体现在响应体里，got:\n%s", bodyB)
	}
	if strings.Contains(bodyB, "gw-a.example.test") {
		t.Errorf("基址 B 的响应体里混入了基址 A 的内容，说明基址被写死而非来自配置，got:\n%s", bodyB)
	}
}

// TestBootScript_NoLiteralHTTPScheme 锁住：响应不含字面 http://（scheme 来自
// 配置的基址）。这条断言的写法是：去掉基址本身贡献的那个 http://
// 之后，剩余文本里不应再出现 http://——网关代码自己不应该在别处硬编码
// scheme（SPEC §1.5：模板中不出现字面 http://，将来上 TLS 只改配置不改
// 代码）。
func TestBootScript_NoLiteralHTTPScheme(t *testing.T) {
	base := "http://gw.example.test"
	body, _ := doBootScript(t, base)

	remainder := strings.Replace(body, base, "", 1)
	if strings.Contains(remainder, "http://") {
		t.Errorf("去掉配置基址本身后，响应体仍含字面 http://，说明网关代码里硬编码了"+
			"scheme，而不是完全来自配置基址。got remainder:\n%s", remainder)
	}
}

// TestBootScript_ContentTypeIsPlainText 锁住 Content-Type 用纯文本。
func TestBootScript_ContentTypeIsPlainText(t *testing.T) {
	_, ct := doBootScript(t, "http://gw.example.test")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want 以 text/plain 开头", ct)
	}
}

// TestBootScript_StartsWithShebang 锁住响应以 #!ipxe shebang 开头——iPXE
// 固件靠这一行识别脚本类型。
func TestBootScript_StartsWithShebang(t *testing.T) {
	body, _ := doBootScript(t, "http://gw.example.test")
	if !strings.HasPrefix(body, "#!ipxe\n") {
		t.Errorf("响应体未以 #!ipxe 开头，got:\n%s", body)
	}
}

// TestBootScript_HasFallbackToShell 锁住：末尾加上失败回退到 shell（计划
// 任务 3「实现要点」：五个变量末尾加上失败回退到 shell）。
func TestBootScript_HasFallbackToShell(t *testing.T) {
	body, _ := doBootScript(t, "http://gw.example.test")
	if !strings.Contains(body, "|| shell") {
		t.Errorf("响应体缺少失败回退到 shell 的分支（|| shell），got:\n%s", body)
	}
}
