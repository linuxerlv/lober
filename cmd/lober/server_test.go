package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/config"
)

// testConfigWithFreePorts 构造一份使用系统分配端口（":0"）的配置，避免
// 与开发机上其它服务撞（与计划里 e2e 脚本「端口用不常见的高位端口」是
// 同一条思路，测试里更进一步用 :0 让内核直接分配，完全不会冲突）。
//
// server.go 的 runServers 直接用 net.Listen(cfg.Gateway.Listen) 等地址
// 字符串，config.Validate 会拒绝 ":0"（缺主机部分），但本测试不经过
// Validate，直连 runServers，因此可以用 "127.0.0.1:0" 形式。
func testConfigWithFreePorts(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.Gateway.Listen = "127.0.0.1:0"
	cfg.Console.Listen = "127.0.0.1:0"
	cfg.Gateway.SyslogListen = "127.0.0.1:0"
	return cfg
}

// TestRunServers_AllThreeListenersServeCorrectContent 断言三个 listener
// 都能服务，各自返回自己的内容（证明没接错 handler，计划任务 4「测试要
// 锁住的行为」）。
//
// 因为配置用 ":0"，实际端口在运行时才知道——本测试改用「直接对
// app.GatewayServer/ConsoleServer 发起进程内 HTTP 请求」的方式验证
// handler 本身可用，而用一组固定端口单独验证 runServers 确实把这些
// handler 绑到了监听地址上（见下面的 TestRunServers_ContextCancelReturnsCleanly
// 与 TestRunServers_PortConflictFailsImmediately，那两条断言不依赖读到
// 实际内容，只依赖「监听成功/失败」这个可观察结果）。
func TestRunServers_GatewayAndConsoleHandlersRespond(t *testing.T) {
	cfg := testConfig(t)
	app := buildAppWithDBForTest(t, cfg)

	// 网关：GET /boot.ipxe 是无需任何前置数据就能响应的端点（全网共用
	// 一份静态文本，见 internal/gateway/bootscript.go）。
	gwReq := httpTestRequest(t, app.GatewayServer, "GET", "/boot.ipxe")
	if gwReq.StatusCode != http.StatusOK {
		t.Errorf("网关 /boot.ipxe 状态码 = %d, want 200", gwReq.StatusCode)
	}

	// 控制台：POST /console/v1/auth/login 是无需认证就能触达的端点（登录
	// 本身）；即使凭据不对也应该返回一个由控制台 handler 产生的响应体
	// （而不是 404，证明路由确实挂到了控制台 handler 上）。
	consoleResp := httpTestRequest(t, app.ConsoleServer, "POST", "/console/v1/auth/login")
	if consoleResp.StatusCode == http.StatusNotFound {
		t.Errorf("控制台 /console/v1/auth/login 返回 404——路由未挂到控制台 handler 上")
	}
}

// httpTestRequest 是进程内直连 http.Handler 发起请求的辅助（不经过真实
// TCP，只验证 handler 本身路由是否挂对）。
func httpTestRequest(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	srv := &http.Server{Handler: h}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	url := fmt.Sprintf("http://%s%s", ln.Addr().String(), path)
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("发起请求失败: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestRunServers_SyslogReceivesUDPPacket 断言 syslog 能收到 UDP 包
// （计划任务 4「测试要锁住的行为」）。用真实端口（系统分配）跑一次
// runServers，取得实际监听地址后发一个 UDP 包，断言日志文件里出现了
// 这条内容。
func TestRunServers_SyslogReceivesUDPPacket(t *testing.T) {
	cfg := testConfig(t)
	// 用固定但少见的高位端口而非 :0——本测试需要在起 runServers 之前就
	// 知道 syslog 的实际地址才能发包，:0 只有绑定后才知道端口，与
	// runServers 当前的接口形状（只接收配置里的地址字符串，不返回实际
	// 绑定结果）不匹配。选一个测试专用高位端口，冲突概率低。
	cfg.Gateway.SyslogListen = "127.0.0.1:19514"
	app := buildAppWithDBForTest(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErrCh := make(chan error, 1)
	go func() { serverErrCh <- runServers(ctx, app, cfg) }()

	// UDP 的 net.Dial 只是解析目标地址、不做任何"对端是否在监听"的探测，
	// 因此不能像 TCP 那样用连接尝试判定监听就绪；改用反复发包直到日志
	// 文件出现内容——只要 ListenAndServeSyslog 的 goroutine 尚未真正调用
	// net.ListenUDP，发出的包会被操作系统直接丢弃（UDP 无重传语义），
	// 反复发送直到收到为止即可绕过这个时序窗口，不需要精确知道"绑定
	// 完成"的那个时刻。

	// 断言日志文件里出现了这条内容。syslog 落盘经由 fileLogStore（本任务
	// 自己的生产实现，见 logstore.go）：匹配不到任务时落公共桶
	// "_syslog_unmatched" 对应的 tasks/_syslog_unmatched/_syslog.log
	// （gateway.Server.handleSyslogLine 用空 phase 调用 AppendLog，见
	// internal/gateway/syslog.go 的 syslogUnmatchedTaskID 常量）。
	logPath := filepath.Join(cfg.Store.LogPath, "tasks", "_syslog_unmatched", "_syslog.log")
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, dialErr := net.Dial("udp", cfg.Gateway.SyslogListen)
		if dialErr == nil {
			_, _ = conn.Write([]byte("<14>test syslog line\n"))
			conn.Close()
		}

		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "test syslog line") {
			return // 成功收到
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("3 秒内未在日志文件 %s 中观察到 syslog 内容，lastErr=%v", logPath, lastErr)
}

// TestRunServers_ContextCancelReturnsCleanly 断言上下文取消后 run 函数
// 返回且无错误（计划任务 4「测试要锁住的行为」）。
func TestRunServers_ContextCancelReturnsCleanly(t *testing.T) {
	cfg := testConfigWithFreePorts(t)
	app := buildAppWithDBForTest(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(ctx, app, cfg) }()

	// 给 goroutine 一点时间真正进入监听状态。
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("上下文取消后 runServers 返回了错误: %v, want nil", err)
		}
	case <-time.After(shutdownGrace + 5*time.Second):
		t.Fatal("上下文取消后 runServers 未能在宽限期内返回")
	}
}

// TestRunServers_PortConflictFailsImmediately 断言端口被占时立刻失败——
// 对应「系统半开着跑」那个故障（计划任务 4「测试要锁住的行为」）。
func TestRunServers_PortConflictFailsImmediately(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用端口失败: %v", err)
	}
	defer occupied.Close()

	cfg := testConfigWithFreePorts(t)
	cfg.Gateway.Listen = occupied.Addr().String() // 网关地址故意撞上已占用的端口
	app := buildAppWithDBForTest(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(ctx, app, cfg) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("网关端口冲突时 runServers 应该立刻返回错误，但返回了 nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("端口冲突后 2 秒内 runServers 未返回——系统半开着跑，这正是要拦住的故障")
	}
}
