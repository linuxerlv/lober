package provider

import (
	"context"
	"testing"
	"time"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
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
