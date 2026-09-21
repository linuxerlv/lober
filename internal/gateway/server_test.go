// 本文件测试 Server 的构造与路由注册。对应实现计划任务 1。
package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// newTestDeps 构造一份可用于测试的 Deps：Store 用 M1 的内存实现，时钟用
// M1 的可控时钟，状态机/渲染器/日志存储用本包的桩。
func newTestDeps() Deps {
	store := fake.NewStore()
	return Deps{
		TaskStore:        store,
		FactStore:        store,
		EventStore:       store,
		TemplateStore:    store,
		ArtifactSetStore: store,
		HostStore:        store,
		StateMachine:     newStateMachineStub(),
		Renderer:         newRendererStub(),
		LogStore:         newLogStoreStub(),
		Clock:            fake.NewClock(),
		Bases: model.BaseURLs{
			GatewayBase:  "http://gw.example.test",
			ArtifactBase: "http://artifacts.example.test",
		},
		SyslogHost: "10.0.0.1",
		SyslogPort: 514,
	}
}

// TestServerConstruct 锁住任务 1 要求的行为：桩能编译、Server 能构造。
// 路由表本身的可达性断言放在任务 8（见 route_audit_test.go）。
func TestServerConstruct(t *testing.T) {
	deps := newTestDeps()
	srv := NewServer(deps)
	if srv == nil {
		t.Fatal("NewServer 返回 nil —— Server 必须能用一份完整的 Deps 构造出来，" +
			"这是后续七个任务的地基")
	}

	// Server 必须实现 http.Handler，供 httptest.NewServer 与生产环境的
	// http.Server 直接使用。用编译期变量声明断言接口满足，而不是在运行时
	// 才发现类型不匹配。
	var _ http.Handler = srv
}

// TestServerConstruct_ImplementsHandler 用 httptest 验证 Server 确实能接入
// 一个真实的 HTTP 处理链路（不断言具体路由行为，那是任务 8 的范围；这里
// 只确认 ServeHTTP 不会在最基本的请求上 panic 或崩溃退出，例如请求一个
// 完全不存在的路径应该拿到标准库 ServeMux 的默认 404，而不是空指针
// panic）。
func TestServerConstruct_ImplementsHandler(t *testing.T) {
	deps := newTestDeps()
	srv := NewServer(deps)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/this-path-does-not-exist")
	if err != nil {
		t.Fatalf("请求不存在的路径失败: %v —— Server 应该能正常处理请求并返回响应，"+
			"而不是让连接本身失败", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /this-path-does-not-exist 状态码 = %d, want %d —— "+
			"未注册的路径应该落到 ServeMux 的默认 404，这确认路由表确实生效了",
			resp.StatusCode, http.StatusNotFound)
	}
}
