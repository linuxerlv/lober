// 本文件测试制品授权（SPEC §9.3 Phase 2 项）在 GET /v1/boot 命中路径上的
// 端到端接线：SecureLinkEnabled 打开后，响应菜单里的大制品 URL 应带
// secure_link 签名查询参数；关闭时（默认）行为与本次改动之前完全一致。
package gateway

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// realisticMenuRendererStub 返回一个 RenderMenu 产出「贴近真实」菜单文本
// 的渲染器桩：kernel/initrd 两行都用 ctx.Bases.ArtifactURL(ctx.Artifact.
// Paths.Kernel/Initrd[0]) 拼出——这与 internal/render/menu.go 的
// renderRockyMenu 对 kernel/initrd 两个 URL 的构造方式完全一致（本包不
// import render 包，不能直接复用它，但可以用同样的纯函数调用方式复刻
// 出同一份未签名 URL 字符串，这正是 signLargeArtifactURLs 依赖的那条
// 性质）。
//
// newRendererStub()（stub_test.go）产出的是硬编码 "vmlinuz" 字面量，不
// 从 ctx.Artifact.Paths 读取，不足以验证「网关能找到并替换实际制品路径
// 对应的 URL」这条端到端行为，因此本文件需要一个更贴近真实的桩。
func realisticMenuRendererStub() *rendererStub {
	stub := newRendererStub()
	stub.renderMenuFn = func(ctx model.RenderContext) (string, error) {
		kernelURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Kernel)
		initrdURL := ctx.Bases.ArtifactURL(ctx.Artifact.Paths.Initrd[0])
		return "#!ipxe\nkernel " + kernelURL + " inst.text\ninitrd " + initrdURL + "\nboot\n", nil
	}
	return stub
}

// newSecureLinkGrantFixture 复用 newGrantFixture 的前置数据，但把制品集
// 的 Kernel/Initrd 路径补齐（newGrantFixture 原本的 artifact 没有设置
// Paths，因为它服务于不关心具体制品路径的其它测试），并换上
// realisticMenuRendererStub。
func newSecureLinkGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	fx := newGrantFixture(t)

	artifact, err := fx.deps.ArtifactSetStore.GetArtifactSet(t.Context(), "art-1")
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	artifact.Paths.Kernel = "rocky94/vmlinuz"
	artifact.Paths.Initrd = []string{"rocky94/initrd.img"}
	store, ok := fx.deps.ArtifactSetStore.(*fake.Store)
	if !ok {
		t.Fatalf("ArtifactSetStore 不是 *fake.Store，无法回写 Paths（类型: %T）", fx.deps.ArtifactSetStore)
	}
	if _, err := store.SetArtifactSetContent(t.Context(), artifact.ID, model.ArtifactIdentity{OSFamily: artifact.OSFamily, Arch: model.ArchX86_64, Firmware: model.FirmwareAny}, artifact.Paths); err != nil {
		t.Fatalf("回写 ArtifactSet.Paths 失败: %v", err)
	}

	fx.deps.Renderer = realisticMenuRendererStub()
	return fx
}

// TestBootGrant_SecureLinkDisabled_MenuUnsigned 锁住默认行为（向后兼容）：
// SecureLinkEnabled 为 false（零值，即未在 Deps 里设置）时，响应菜单里的
// URL 不带 md5/expires 查询参数，与本次改动之前完全一致。
func TestBootGrant_SecureLinkDisabled_MenuUnsigned(t *testing.T) {
	fx := newSecureLinkGrantFixture(t)
	// 不设置 fx.deps.SecureLinkEnabled——保持零值 false。

	body, status := doBoot(t, fx.deps, grantQuery())
	if status != 200 {
		t.Fatalf("状态码 = %d, want 200", status)
	}
	if strings.Contains(body, "md5=") || strings.Contains(body, "expires=") {
		t.Errorf("SecureLinkEnabled=false 时响应体仍含签名查询参数，want 完全不含：\n%s", body)
	}
	if !strings.Contains(body, "http://artifacts.example.test/artifacts/rocky94/vmlinuz") {
		t.Errorf("响应体应包含未改写的原始 kernel URL，got:\n%s", body)
	}
}

// TestBootGrant_SecureLinkEnabled_MenuKernelAndInitrdSigned 锁住核心接线
// 行为：打开后，kernel 与 initrd 两个 URL 都应被改写成
// /artifacts/secure/ 前缀并带 md5/expires 参数，且这个签名能被独立实现
// 的校验逻辑（siglink_test.go 的 verifySecureLinkIndependent）验证通过
// ——不只是「格式看起来对」，而是「真的是用配置的密钥、这次请求的客户端
// IP 算出来的正确签名」。
func TestBootGrant_SecureLinkEnabled_MenuKernelAndInitrdSigned(t *testing.T) {
	fx := newSecureLinkGrantFixture(t)
	fx.deps.SecureLinkEnabled = true
	fx.deps.SecureLinkSecret = "integration-test-secret"
	fx.deps.SecureLinkTTL = 30 * time.Minute

	body, status := doBoot(t, fx.deps, grantQuery())
	if status != 200 {
		t.Fatalf("状态码 = %d, want 200", status)
	}

	if !strings.Contains(body, "/artifacts/secure/rocky94/vmlinuz") {
		t.Fatalf("响应体未包含改写后的 kernel URL 前缀 /artifacts/secure/，got:\n%s", body)
	}
	if !strings.Contains(body, "/artifacts/secure/rocky94/initrd.img") {
		t.Fatalf("响应体未包含改写后的 initrd URL 前缀 /artifacts/secure/，got:\n%s", body)
	}
	if strings.Contains(body, "/artifacts/rocky94/vmlinuz ") {
		t.Errorf("响应体仍含未改写的原始 kernel URL（应已被替换为签名版本），got:\n%s", body)
	}

	kernelURL, md5Param, expiresParam := extractSignedURLParams(t, body, "/artifacts/secure/rocky94/vmlinuz")
	if kernelURL == "" {
		t.Fatalf("未能从响应体中提取签名 kernel URL，got:\n%s", body)
	}

	// clientIP(r) 在没有 X-Real-IP 头部、httptest 默认 RemoteAddr 的情况下
	// 会返回 httptest.NewRequest 默认使用的 peer 地址——doBoot 内部用的是
	// httptest.NewRequest 默认值（192.0.2.1:1234，Go 标准库文档记录的
	// 测试专用地址段），本测试直接复用同一个值而不是重新发起真实请求。
	const expectedClientIP = "192.0.2.1"
	valid, expired := verifySecureLinkIndependent(kernelURL, expectedClientIP, "integration-test-secret", md5Param, expiresParam, 0)
	if !valid {
		t.Errorf("独立校验逻辑判定 kernel URL 签名不合法——网关签发的签名与期望的密钥/客户端IP/公式不一致，"+
			"md5=%q expires=%q path=%q", md5Param, expiresParam, kernelURL)
	}
	if expired {
		t.Errorf("kernel URL 签名被判定已过期，want 未过期（TTL=30分钟，签发时刻应远早于过期）")
	}
}

// extractSignedURLParams 从 body 里找到以 prefix 开头的 URL 子串，解析出
// 路径与 md5/expires 两个查询参数。查找方式：定位 prefix 的位置，向后扫到
// 第一个空白字符为止（菜单文本用空格或换行分隔字段，这与
// realisticMenuRendererStub 产出的具体格式一致）。
func extractSignedURLParams(t *testing.T, body, prefix string) (path, md5Param, expiresParam string) {
	t.Helper()
	idx := strings.Index(body, prefix)
	if idx < 0 {
		return "", "", ""
	}
	rest := body[idx:]
	end := strings.IndexAny(rest, " \n\t")
	if end < 0 {
		end = len(rest)
	}
	urlStr := rest[:end]

	parsed, err := url.Parse(urlStr)
	if err != nil {
		t.Fatalf("解析提取出的 URL 失败: %v, url=%q", err, urlStr)
	}
	return parsed.Path, parsed.Query().Get("md5"), parsed.Query().Get("expires")
}

// TestBootGrant_SecureLinkEnabled_ExpiresIsFutureUnixTimestamp 锁住
// expires 参数的取值来源：应是 s.deps.Clock.Now() + TTL 秒数，不是某个
// 硬编码常量——用一份可控时钟（fake.NewClock）验证 expires 与
// 「起始时间 + TTL」在数值上吻合。
func TestBootGrant_SecureLinkEnabled_ExpiresIsFutureUnixTimestamp(t *testing.T) {
	fx := newSecureLinkGrantFixture(t)
	clock := fake.NewClock()
	clock.SetTime(time.Unix(1_700_000_000, 0).UTC())
	fx.deps.Clock = clock
	fx.deps.SecureLinkEnabled = true
	fx.deps.SecureLinkSecret = "integration-test-secret"
	fx.deps.SecureLinkTTL = 30 * time.Minute

	body, status := doBoot(t, fx.deps, grantQuery())
	if status != 200 {
		t.Fatalf("状态码 = %d, want 200", status)
	}

	_, _, expiresParam := extractSignedURLParams(t, body, "/artifacts/secure/rocky94/vmlinuz")
	expiresAt, err := strconv.ParseInt(expiresParam, 10, 64)
	if err != nil {
		t.Fatalf("expires 参数不是合法整数: %q, err=%v", expiresParam, err)
	}

	want := int64(1_700_000_000 + 30*60)
	if expiresAt != want {
		t.Errorf("expires = %d, want %d（Clock.Now()=1700000000 + TTL 30 分钟）", expiresAt, want)
	}
}
