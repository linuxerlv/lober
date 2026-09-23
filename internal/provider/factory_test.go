package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/provider/loader"
)

// validProviderConfig 返回一份全部字段合法的 config.Provider，供工厂测试
// 的「合法配置」基线场景使用。
func validProviderConfig() config.Provider {
	return config.Provider{
		IPAM:              "static",
		DNS:               "noop",
		Resolver:          "rule-based",
		DiskPolicy:        model.DiskPolicyLargest,
		HeartbeatMode:     "tcp",
		HeartbeatInterval: 60 * time.Second,
	}
}

func TestFactory_ValidConfigConstructsAllSevenProviders(t *testing.T) {
	store := fake.NewStore()
	providers, err := NewProviders(validProviderConfig(), store)
	if err != nil {
		t.Fatalf("合法配置应成功构造，got err=%v", err)
	}

	if providers.IPAM == nil {
		t.Error("IPAM 不应为 nil")
	}
	if providers.DNS == nil {
		t.Error("DNS 不应为 nil")
	}
	if providers.Inventory == nil {
		t.Error("Inventory 不应为 nil")
	}
	if providers.Notify == nil {
		t.Error("Notify 不应为 nil")
	}
	if providers.Heartbeat == nil {
		t.Error("Heartbeat 不应为 nil")
	}
	if providers.TemplateResolver == nil {
		t.Error("TemplateResolver 不应为 nil")
	}
	if providers.DiskPlanner == nil {
		t.Error("DiskPlanner 不应为 nil")
	}
}

func TestFactory_UnknownIPAMReturnsError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.IPAM = "no-such-ipam"
	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("未知的 IPAM 选择项必须返回错误，不能回落到默认实现——" +
			"配置里写错一个 provider 名字应该在启动时被发现")
	}
}

func TestFactory_UnknownDNSReturnsError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.DNS = "no-such-dns"
	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("未知的 DNS 选择项必须返回错误")
	}
}

func TestFactory_UnknownResolverReturnsError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.Resolver = "no-such-resolver"
	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("未知的 Resolver 选择项必须返回错误")
	}
}

func TestFactory_UnknownDiskPolicyReturnsError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.DiskPolicy = model.DiskPolicy("no-such-policy")
	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("未知的磁盘策略必须返回错误")
	}
}

func TestFactory_UnknownHeartbeatModeReturnsError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.HeartbeatMode = "no-such-mode"
	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("未知的心跳模式必须返回错误")
	}
}

func TestFactory_HeartbeatModeOff_ProbeReturnsEmptyNotNilError(t *testing.T) {
	cfg := validProviderConfig()
	cfg.HeartbeatMode = "off"
	store := fake.NewStore()

	providers, err := NewProviders(cfg, store)
	if err != nil {
		t.Fatalf("心跳模式 off 应是合法配置: %v", err)
	}
	if providers.Heartbeat == nil {
		t.Fatal("心跳模式 off 时 Heartbeat 字段仍应是非 nil 的可用实现")
	}

	results := providers.Heartbeat.Probe(context.Background(), nil)
	if len(results) != 0 {
		t.Fatalf("off 模式探测应返回空结果，got %d 条", len(results))
	}
	if err := providers.Heartbeat.Ping(context.Background()); err != nil {
		t.Fatalf("off 模式 Ping 不应报错: %v", err)
	}
}

// ---------------------------------------------------------------------------
// exec/http 加载器接线（本次任务，SPEC §7.2）
// ---------------------------------------------------------------------------

// TestFactory_IPAMExec_ConstructsWorkingAdapter 验证 provider.ipam="exec"
// 加上合法的 ipam_loader 配置，工厂能构造出一个真正可用的 IPAM 实现——
// 不只是「类型对了」，而是端到端地调用一次 Allocate，确认背后的子进程
// 真的被调用且结果被正确解码。
//
// 子进程用本测试二进制的重新执行（与 loader 包 exec_test.go 同样的
// TestHelperProcess 模式）——factory_test.go 是 provider 包（不是
// loader 包）的测试文件，两个包各自的测试二进制是独立的可执行文件，
// 不能共享 loader 包里定义的 TestHelperProcess，因此本文件需要一份
// 自己的最小实现。
func TestFactory_IPAMExec_ConstructsWorkingAdapter(t *testing.T) {
	helperMode := "provider-factory-echo"
	withFactoryTestHelperEnv(t, helperMode)

	exePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() 失败: %v", err)
	}

	cfg := validProviderConfig()
	cfg.IPAM = "exec"
	cfg.IPAMLoader = config.LoaderConfig{
		Type:    "exec",
		Command: exePath,
		Args:    []string{"-test.run=^TestFactoryHelperProcess$"},
	}

	store := fake.NewStore()
	providers, err := NewProviders(cfg, store)
	if err != nil {
		t.Fatalf("NewProviders() 失败: %v", err)
	}
	if providers.IPAM == nil {
		t.Fatal("provider.ipam=exec 时 Providers.IPAM 不应为 nil")
	}

	result, err := providers.IPAM.Allocate(context.Background(), contract.AllocRequest{Hostname: "node-99"})
	if err != nil {
		t.Fatalf("Allocate() 失败: %v", err)
	}
	if result.IP != "10.9.9.9" {
		t.Errorf("Allocate() 结果 = %+v，want IP=10.9.9.9（子进程 echo 模式应返回固定值）", result)
	}
}

// TestFactory_DNSHTTP_ConstructsWorkingAdapter 验证 provider.dns="http"
// 加上合法的 dns_loader 配置，工厂构造出的 DNS 实现能真正发出一次 HTTP
// 请求并正确处理响应。
func TestFactory_DNSHTTP_ConstructsWorkingAdapter(t *testing.T) {
	requestReceived := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		_ = json.NewEncoder(w).Encode(loader.Response{})
	}))
	defer srv.Close()

	cfg := validProviderConfig()
	cfg.DNS = "http"
	cfg.DNSLoader = config.LoaderConfig{Type: "http", URL: srv.URL}

	store := fake.NewStore()
	providers, err := NewProviders(cfg, store)
	if err != nil {
		t.Fatalf("NewProviders() 失败: %v", err)
	}
	if providers.DNS == nil {
		t.Fatal("provider.dns=http 时 Providers.DNS 不应为 nil")
	}

	if err := providers.DNS.Register(context.Background(), "host1.example.com", "10.0.1.5"); err != nil {
		t.Fatalf("Register() 失败: %v", err)
	}
	if !requestReceived {
		t.Error("DNS Register 未真正发出 HTTP 请求——工厂构造出的实现可能没有正确接线到 http 加载器")
	}
}

// TestFactory_HeartbeatHTTP_ConstructsWorkingAdapter 是心跳模式的 http
// 对照组。
func TestFactory_HeartbeatHTTP_ConstructsWorkingAdapter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := []contract.ProbeResult{{TaskID: "t1", Reached: true, Detail: "ok"}}
		resultJSON, _ := json.Marshal(results)
		_ = json.NewEncoder(w).Encode(loader.Response{Result: resultJSON})
	}))
	defer srv.Close()

	cfg := validProviderConfig()
	cfg.HeartbeatMode = "http"
	cfg.HeartbeatLoader = config.LoaderConfig{Type: "http", URL: srv.URL}

	store := fake.NewStore()
	providers, err := NewProviders(cfg, store)
	if err != nil {
		t.Fatalf("NewProviders() 失败: %v", err)
	}
	if providers.Heartbeat == nil {
		t.Fatal("provider.heartbeat_mode=http 时 Providers.Heartbeat 不应为 nil")
	}

	results := providers.Heartbeat.Probe(context.Background(), []contract.ProbeTarget{{TaskID: "t1", IP: "10.0.1.5"}})
	if len(results) != 1 || !results[0].Reached {
		t.Errorf("Probe() 结果 = %+v，want 1 条 Reached=true 的结果", results)
	}
}

// TestFactory_IPAMExec_MissingCommandRejectedAtConstruction 验证工厂层
// 也会拒绝「选了 exec 但没给 command」——即使 config.Validate 已经在更
// 早的启动阶段拦住了这种配置（config_test.go 已覆盖该路径），本测试
// 确认 loader.NewExecLoader 自身的构造期校验作为第二道防线同样生效，
// 两层校验都不能删掉任何一层（config.Validate 可能被跳过调用，例如
// 测试代码或未来某个路径直接构造 config.Provider 值传给 NewProviders
// 而不经过 Parse+Validate 的完整流程）。
func TestFactory_IPAMExec_MissingCommandRejectedAtConstruction(t *testing.T) {
	cfg := validProviderConfig()
	cfg.IPAM = "exec"
	cfg.IPAMLoader = config.LoaderConfig{Type: "exec", Command: ""}

	store := fake.NewStore()
	_, err := NewProviders(cfg, store)
	if err == nil {
		t.Fatal("NewProviders() 应该拒绝 ipam=exec 但 command 为空的配置")
	}
}

// withFactoryTestHelperEnv 设置本文件 TestFactoryHelperProcess 依赖的
// 环境变量。
func withFactoryTestHelperEnv(t *testing.T, mode string) {
	t.Helper()
	if err := os.Setenv(factoryTestHelperEnv, mode); err != nil {
		t.Fatalf("设置环境变量失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(factoryTestHelperEnv) })
}

// factoryTestHelperEnv 标记本次重新执行是作为 TestFactoryHelperProcess
// 的子进程角色运行（与 loader 包 exec_test.go 的 helperProcessEnv 是
// 同一种模式，两个包各自独立的测试二进制需要各自的环境变量名与判定
// 函数，不能跨包共享）。
const factoryTestHelperEnv = "LOBER_PROVIDER_FACTORY_TEST_HELPER_MODE"

// TestFactoryHelperProcess 是被重新拉起的子进程实际执行的程序，不是
// 一个真正的测试用例（模式与 loader 包 exec_test.go 的 TestHelperProcess
// 相同）。目前只有一种模式（echo 固定的 AllocResult），够用即可——本
// 文件的测试目标是验证「工厂正确接线」，不是重新验证 exec 加载器本身
// 的各种失败路径（那些已经在 loader 包的 exec_test.go 里覆盖）。
func TestFactoryHelperProcess(t *testing.T) {
	mode := os.Getenv(factoryTestHelperEnv)
	if mode == "" {
		return
	}

	switch mode {
	case "provider-factory-echo":
		var req loader.Request
		if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
			fmt.Fprintf(os.Stderr, "解析 stdin 失败: %v", err)
			os.Exit(1)
		}
		result := contract.AllocResult{IP: "10.9.9.9", Gateway: "10.9.9.1", Mask: "255.255.255.0"}
		resultJSON, _ := json.Marshal(result)
		_ = json.NewEncoder(os.Stdout).Encode(loader.Response{Result: resultJSON})
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "未知的 helper 模式: %s", mode)
		os.Exit(2)
	}
}
