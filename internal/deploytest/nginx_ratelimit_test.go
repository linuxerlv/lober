// 本文件锁住 nginx 配置不给制品下载套引导端点的限流桶。
//
// **/artifacts/ 的流量特征与引导端点完全不同**，套同一个桶会让装包阶段整体
// 失败：
//
//	引导端点   每台机器每次装机个位数请求（/boot.ipxe、/v1/boot、/v1/answer）
//	制品下载   一次装机几百个 RPM，dnf 并发拉取，瞬时速率远超 30r/s
//
// 实测（2026-09-26，QEMU BIOS 目标机，前面二十一个缺口都已修掉）：anaconda 通过
// 存储校验、开始装包，前 41 个包 200，之后**全部 503**：
//
//	.../Packages/dmidecode-3.5-7.oc9.x86_64.rpm"   503 190
//	.../Packages/dnf-4.16.2-5.oc9.noarch.rpm"      503 190
//	.../Packages/dnf-data-4.16.2-5.oc9.noarch.rpm" 503 190
//
// 三分钟后机器重新 PXE 引导。目标盘停在 9.2MB——334 个包里只装进去 41 个。
//
// SPEC §9.3 对同一位置有过一条同方向的结论：「离线仓库必须保持开放，签了会让
// 装机在装包阶段整体失败」——那说的是 secure_link 签名，而限流是同一个道理，
// 只是这一条当时没写进去。
//
// 本测试读 deploy/nginx/lober.conf 的文本做断言，不起 nginx：那份文件是部署
// 产物，CI 里没有 nginx 可跑，而「哪个 location 套了哪个桶」是纯文本事实。
package deploytest

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const nginxConf = "../../deploy/nginx/lober.conf"

// TestArtifactsLocationHasNoBootRateLimit 锁住 /artifacts/ 不套 lober_boot 桶。
func TestArtifactsLocationHasNoBootRateLimit(t *testing.T) {
	blocks := parseLocations(t)

	for _, name := range []string{"/artifacts/", "/artifacts/secure/", "/repo/"} {
		body, ok := blocks[name]
		if !ok {
			t.Errorf("找不到 location %s——配置结构变了，本测试的断言可能已失效", name)
			continue
		}
		if strings.Contains(body, "zone=lober_boot") {
			t.Errorf("location %s 套了 lober_boot 限流桶（rate=30r/s）——制品下载"+
				"是一次几百个 RPM 的并发拉取，与引导端点的个位数请求完全不同量级。"+
				"实测：前 41 个包 200，之后全部 503，334 个包只装进 41 个。\n%s",
				name, body)
		}
	}
}

// TestBootEndpointsKeepRateLimit 回归保护：引导端点仍然限流。
//
// 去掉制品下载的限流不等于全都不限。引导端点保留 lober_boot 桶的理由在
// lober-http-snippet.conf 的注释里：一批机器高频发信号会把另一批机器的引导
// 请求挤掉限流队列。
func TestBootEndpointsKeepRateLimit(t *testing.T) {
	raw := readConf(t)
	if !strings.Contains(raw, "zone=lober_boot") {
		t.Error("整份配置里没有 lober_boot 限流——引导端点仍需要它（见 " +
			"lober-http-snippet.conf 关于两个桶为什么分开的说明）")
	}
	if !strings.Contains(raw, "zone=lober_signal") {
		t.Error("整份配置里没有 lober_signal 限流——信号端点仍需要它")
	}
}

// TestArtifactsLocationKeepsOpen 锁住制品 location 既不限流也不签名。
//
// 两件事同一个方向：SPEC §9.3 要求离线仓库保持开放。secure_link 那条已经被
// /artifacts/secure/ 这个独立 location 隔开了，限流这条是本次补上的。
func TestArtifactsLocationKeepsOpen(t *testing.T) {
	blocks := parseLocations(t)
	body, ok := blocks["/artifacts/"]
	if !ok {
		t.Fatal("找不到 location /artifacts/")
	}
	// 只看**指令行**，不看注释：/artifacts/ 的注释里会提到 secure_link（解释
	// 为什么签名版本单独放一个 location），那不是配置生效的内容。
	for _, line := range strings.Split(body, "\n") {
		code := strings.TrimSpace(line)
		if code == "" || strings.HasPrefix(code, "#") {
			continue
		}
		if strings.HasPrefix(code, "secure_link") {
			t.Errorf("location /artifacts/ 有 secure_link 指令——SPEC §9.3：离线仓库"+
				"必须保持开放，签了会让装机在装包阶段整体失败。签名版本在 "+
				"/artifacts/secure/ 那个独立 location 里。该行: %s", code)
		}
	}
	if !strings.Contains(body, "alias") {
		t.Error("location /artifacts/ 应用 alias 直接发盘，不经 proxy_pass")
	}
}

func readConf(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(nginxConf)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", nginxConf, err)
	}
	return string(b)
}

// parseLocations 把配置切成 location → 块体。
//
// 只做到「按 location 行切分、到下一个 location 或 server 边界为止」这个粒度：
// 够用来断言某个指令属于哪个 location，而不需要真的实现 nginx 的配置解析器。
func parseLocations(t *testing.T) map[string]string {
	t.Helper()
	raw := readConf(t)
	re := regexp.MustCompile(`(?m)^\s*location\s+(\S+)\s*\{`)
	matches := re.FindAllStringSubmatchIndex(raw, -1)

	out := make(map[string]string, len(matches))
	for i, m := range matches {
		name := raw[m[2]:m[3]]
		end := len(raw)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[name] = raw[m[0]:end]
	}
	return out
}
