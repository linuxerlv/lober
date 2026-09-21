package provider

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
)

// listenLocal 在 127.0.0.1 上开一个监听端口，返回端口号与关闭函数，供
// tcp 探测测试验证「探活成功」的场景。
func listenLocal(t *testing.T) (port int, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("测试准备阶段监听本地端口失败: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return addr.Port, func() { _ = ln.Close() }
}

// closedLocalPort 返回一个大概率没有监听者的本地端口：先监听再立即关闭，
// 拿到一个刚刚释放、通常还未被其它进程复用的端口号，用于验证「拨号失败」
// 场景，不依赖某个魔法端口号总是空闲这一假设。
func closedLocalPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("测试准备阶段监听本地端口失败: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	return addr.Port
}

func TestHeartbeatTCP_ReachableTarget(t *testing.T) {
	port, closeFn := listenLocal(t)
	defer closeFn()

	h := newTCPHeartbeat(port)
	results := h.Probe(context.Background(), []contract.ProbeTarget{
		{TaskID: "task-1", IP: "127.0.0.1"},
	})

	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果，got %d", len(results))
	}
	if !results[0].Reached {
		t.Fatalf("本地监听端口应探测成功，got Reached=false, Detail=%s", results[0].Detail)
	}
}

func TestHeartbeatTCP_UnreachableTarget(t *testing.T) {
	closedPort := closedLocalPort(t)

	h := newTCPHeartbeat(closedPort)
	results := h.Probe(context.Background(), []contract.ProbeTarget{
		{TaskID: "task-1", IP: "127.0.0.1"},
	})

	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果，got %d", len(results))
	}
	if results[0].Reached {
		t.Fatal("关闭的端口应探测失败，got Reached=true")
	}
	if results[0].Detail == "" {
		t.Fatal("探测失败时 Detail 不应为空")
	}
}

func TestHeartbeatTCP_SingleTargetFailureDoesNotAffectOthers(t *testing.T) {
	openPort, closeFn := listenLocal(t)
	defer closeFn()
	closedPort := closedLocalPort(t)

	// 用两个不同端口分别代表「可达」与「不可达」，但探测器只有一个端口
	// 配置——这里改用两个探测器分别模拟，验证的是「批量探测里一个目标
	// 失败不影响其它目标各自独立的结果」，而不是要求同一探测器同时支持
	// 两个不同端口（那不是本探测器的设计——它对全部目标使用同一端口，
	// 见 tcpHeartbeat 的文档注释）。这里用同一个不可达 IP 混入一个可达 IP
	// 的方式验证，而不是混用端口。
	_ = closedPort

	h := newTCPHeartbeat(openPort)
	results := h.Probe(context.Background(), []contract.ProbeTarget{
		{TaskID: "reachable", IP: "127.0.0.1"},
		{TaskID: "unreachable", IP: "203.0.113.1"}, // TEST-NET-3，保留地址，必定不可达且不会实际发出流量到公网
	})

	if len(results) != 2 {
		t.Fatalf("应返回 2 条结果，got %d", len(results))
	}

	byTaskID := make(map[string]contract.ProbeResult)
	for _, r := range results {
		byTaskID[r.TaskID] = r
	}

	if !byTaskID["reachable"].Reached {
		t.Fatalf("可达目标的结果不应被不可达目标的探测影响，got %+v", byTaskID["reachable"])
	}
	if byTaskID["unreachable"].Reached {
		t.Fatal("不可达目标应探测失败")
	}
}

func TestHeartbeatICMP_NoPrivilegeGracefulDegradation(t *testing.T) {
	h := newICMPHeartbeat()

	// 本测试要能在无权限环境下通过：无论当前测试进程是否具备 ICMP 特权
	// （CI/开发机通常没有），Probe 都不应 panic，且必须返回与目标数量
	// 相等的结果切片。若环境恰好有特权（结果为 Reached=true）也算通过——
	// 本测试验证的是「不 panic、不让整批失败」这条降级路径的存在，
	// 不是强制要求当前环境一定无权限。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("icmp 探测在无权限时绝不能 panic，got panic: %v", r)
		}
	}()

	results := h.Probe(context.Background(), []contract.ProbeTarget{
		{TaskID: "task-1", IP: "127.0.0.1"},
	})

	if len(results) != 1 {
		t.Fatalf("应返回 1 条结果，got %d", len(results))
	}
	if !results[0].Reached {
		if results[0].Detail == "" {
			t.Fatal("探测失败时 Detail 必须非空，便于运维区分「权限问题」与「目标真的不可达」")
		}
	}
}

func TestHeartbeatICMP_PingDoesNotPanicWithoutPrivilege(t *testing.T) {
	h := newICMPHeartbeat()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Ping 在无权限时绝不能 panic，got panic: %v", r)
		}
	}()
	// 不断言 err 的具体值——有无权限取决于运行环境，这里只验证不 panic。
	_ = h.Ping(context.Background())
}

func TestHeartbeat_EmptyTargetsReturnsEmptyNoError(t *testing.T) {
	h := newTCPHeartbeat(22)
	results := h.Probe(context.Background(), nil)
	if len(results) != 0 {
		t.Fatalf("空目标列表应返回空结果，got %d 条", len(results))
	}
}

func TestHeartbeat_ConcurrencyCapDoesNotExhaustResources(t *testing.T) {
	port, closeFn := listenLocal(t)
	defer closeFn()

	const targetCount = 200
	targets := make([]contract.ProbeTarget, targetCount)
	for i := 0; i < targetCount; i++ {
		targets[i] = contract.ProbeTarget{TaskID: "task-" + strconv.Itoa(i), IP: "127.0.0.1"}
	}

	h := newTCPHeartbeat(port)

	done := make(chan []contract.ProbeResult, 1)
	go func() {
		done <- h.Probe(context.Background(), targets)
	}()

	select {
	case results := <-done:
		if len(results) != targetCount {
			t.Fatalf("大量目标探测应返回与目标数相等的结果，got %d，want %d", len(results), targetCount)
		}
		reachedCount := 0
		for _, r := range results {
			if r.Reached {
				reachedCount++
			}
		}
		if reachedCount != targetCount {
			t.Fatalf("并发上限生效时全部目标最终仍应探测成功（只是分批），got %d/%d 成功",
				reachedCount, targetCount)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("200 个目标的批量探测超过 30 秒未完成——并发上限可能配置不当或存在死锁")
	}
}
