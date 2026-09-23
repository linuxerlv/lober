// 本文件测试 HTTPLoader：200 正常解码、非 2xx 报错、超时、启动校验、
// 熔断。用 httptest.Server 起一个真实的本地 HTTP 服务器，不 mock
// http.RoundTripper——后者容易在「Transport 该怎么被调用」这层细节上
// 测试实现而不是行为，httptest.Server 则是端到端地验证「发出去的请求
// 长什么样、收回来的响应怎么被处理」。
package loader

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPLoader_DecodesResult 验证 200 响应时结果被正确解码。
func TestHTTPLoader_DecodesResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("服务端解析请求体失败: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Method != "ipam.allocate" {
			t.Errorf("服务端收到 Method = %q, want %q", req.Method, "ipam.allocate")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{Result: req.Params})
	}))
	defer srv.Close()

	l, err := NewHTTPLoader(HTTPConfig{URL: srv.URL}, Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewHTTPLoader() 失败: %v", err)
	}

	type payload struct {
		Hostname string `json:"hostname"`
	}
	var result payload
	err = l.Call(context.Background(), "ipam.allocate", payload{Hostname: "node-02"}, &result)
	if err != nil {
		t.Fatalf("Call() 失败: %v", err)
	}
	if result.Hostname != "node-02" {
		t.Errorf("result.Hostname = %q, want %q", result.Hostname, "node-02")
	}
}

// TestHTTPLoader_NonSuccessStatus 验证非 2xx 状态码算失败。
func TestHTTPLoader_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream broke"))
	}))
	defer srv.Close()

	l, err := NewHTTPLoader(HTTPConfig{URL: srv.URL}, Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewHTTPLoader() 失败: %v", err)
	}

	err = l.Call(context.Background(), "notify.emit", map[string]string{}, nil)
	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil —— 500 状态码应该算失败")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Call() 错误 = %v，want 包含状态码 500", err)
	}
}

// TestHTTPLoader_BusinessError 验证 200 但 Response.Error 非空时算失败。
func TestHTTPLoader_BusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{Error: "目标系统拒绝了本次注册"})
	}))
	defer srv.Close()

	l, err := NewHTTPLoader(HTTPConfig{URL: srv.URL}, Config{BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewHTTPLoader() 失败: %v", err)
	}

	err = l.Call(context.Background(), "dns.register", map[string]string{}, nil)
	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil")
	}
	if !strings.Contains(err.Error(), "目标系统拒绝了本次注册") {
		t.Errorf("Call() 错误 = %v，未包含服务端报告的业务错误文案", err)
	}
}

// TestHTTPLoader_Timeout 验证服务端响应慢于配置超时时，Call 报超时并
// 提前返回（不等服务端真正响应）。
func TestHTTPLoader_Timeout(t *testing.T) {
	blockUntil := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockUntil // 一直不响应，直到测试主动放行
	}))
	// 必须先放行 handler 再 Close server：httptest.Server.Close() 会等待
	// 所有活跃连接的 handler 返回才结束（内部用 sync.WaitGroup），若不先
	// close(blockUntil)，handler 永远卡在 <-blockUntil，Close() 会一直
	// 阻塞到 net/http/httptest 内置的兜底超时才强行放弹并打日志——这不是
	// http.go 的 bug，是本测试早前用 t.Cleanup 注册放行、却用 defer 注册
	// Close 导致的执行顺序错误（defer 在函数返回前执行，t.Cleanup 在
	// 之后才执行，Close 因此先于放行发生）。改成显式的先关 channel、
	// 再 defer Close，顺序保证正确。
	defer func() {
		close(blockUntil)
		srv.Close()
	}()

	l, err := NewHTTPLoader(HTTPConfig{URL: srv.URL},
		Config{Timeout: 100 * time.Millisecond, BreakerThreshold: -1})
	if err != nil {
		t.Fatalf("NewHTTPLoader() 失败: %v", err)
	}

	start := time.Now()
	err = l.Call(context.Background(), "notify.emit", map[string]string{}, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Call() 返回 nil, want 非 nil —— 超时应该报错")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("Call() 错误 = %v，未包裹 ErrTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Call() 耗时 %v，远超配置的 100ms 超时", elapsed)
	}
}

// TestNewHTTPLoader_RejectsEmptyURL 验证配置里选了 http 但没给 url 时，
// 构造函数直接拒绝。
func TestNewHTTPLoader_RejectsEmptyURL(t *testing.T) {
	_, err := NewHTTPLoader(HTTPConfig{URL: ""}, Config{})
	if err == nil {
		t.Fatal("NewHTTPLoader() 返回 nil error, want 非 nil —— 空 url 必须在构造期被拒绝")
	}
}

// TestHTTPLoader_CircuitBreakerStopsCallingServer 验证熔断打开后，
// 后续调用不再真正发起 HTTP 请求——用一个计数 handler 直接验证服务端
// 收到的请求数没有超过阈值。
func TestHTTPLoader_CircuitBreakerStopsCallingServer(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	l, err := NewHTTPLoader(HTTPConfig{URL: srv.URL},
		Config{MaxRetries: 0, RetryBackoff: time.Millisecond, BreakerThreshold: 2})
	if err != nil {
		t.Fatalf("NewHTTPLoader() 失败: %v", err)
	}

	for i := 0; i < 4; i++ {
		_ = l.Call(context.Background(), "notify.emit", map[string]string{}, nil)
	}

	if requestCount != 2 {
		t.Errorf("服务端收到 %d 次请求, want 2（阈值 2，前 2 次真实失败后熔断，"+
			"后续 2 次调用不应该再打到服务端）", requestCount)
	}
}
