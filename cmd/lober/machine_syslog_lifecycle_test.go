package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lober/lober/internal/application/execution"
	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/events"
)

type syslogLifecycleReceiver struct{ received chan struct{} }

func (r syslogLifecycleReceiver) ReceiveInstallerLog(context.Context, executionport.InstallerLog) error {
	r.received <- struct{}{}
	return nil
}

type syslogEligibilityStore struct {
	mu      sync.Mutex
	task    model.Task
	entries []repository.NewEvent
}

func (s *syslogEligibilityStore) GetTask(_ context.Context, id string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.task.ID {
		return model.Task{}, repository.ErrNotFound
	}
	return s.task, nil
}

func (s *syslogEligibilityStore) AppendEvents(_ context.Context, _ string, entries []repository.NewEvent) ([]repository.Appended, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entries...)
	return []repository.Appended{{Seq: 1}}, nil
}

func TestTCPInstallerLogsRetainApplicationEligibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   model.TaskState
		attempt int
		want    int
	}{
		{"current", model.TaskInstalling, 1, 1},
		{"old-attempt", model.TaskInstalling, 2, 0},
		{"done", model.TaskDone, 1, 0},
		{"failed", model.TaskFailed, 1, 0},
		{"cancelled", model.TaskCancelled, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &syslogEligibilityStore{task: model.Task{ID: "task-network", State: tc.state, Execution: &model.Execution{Attempt: tc.attempt}}}
			collector := events.New(execution.NewInstallerLogs(store, nil), func() int64 { return assemblyAt })
			collector.ObserveIP("task-network", 1, "127.0.0.1:1", assemblyAt+60)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- collector.ServeTCP(ctx, listener, nil) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(3 * time.Second):
					t.Error("日志接收未退出")
				}
			}()
			conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := io.WriteString(conn, "installer message\n"); err != nil {
				t.Fatal(err)
			}
			conn.(*net.TCPConn).CloseWrite()
			if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
				t.Fatal("完整消息未处理并正常结束连接", err)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.entries) != tc.want {
				t.Fatal("TCP 绕过了应用对旧轮次或终态的检查", len(store.entries))
			}
		})
	}
}

func freeSyslogAddress(t *testing.T) string {
	t.Helper()
	packet, stream, err := listenSyslog("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := packet.LocalAddr().String()
	packet.Close()
	stream.Close()
	return address
}

func TestSyslogListenZeroSharesUDPAndTCPPort(t *testing.T) {
	packet, stream, err := listenSyslog("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	defer stream.Close()
	if packet.LocalAddr().String() != stream.Addr().String() {
		t.Fatal("端口 0 被分配成两个不同的日志地址")
	}
}

func TestSyslogTCPBindFailureClosesUDPAndHTTP(t *testing.T) {
	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer httpListener.Close()
	a := &app{Config: config{Machine: machineConfig{SyslogListen: blocked.Addr().String()}}, Machine: &machineBundle{Logs: events.New(nil, func() int64 { return assemblyAt })}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := serveListener(t.Context(), a, httpListener); err == nil {
		t.Fatal("TCP 已占用时仍启动成功")
	}
	packet, err := net.ListenPacket("udp", blocked.Addr().String())
	if err != nil {
		t.Fatal("TCP 启动失败后 UDP 仍被占用", err)
	}
	packet.Close()
	if conn, err := net.DialTimeout("tcp", httpListener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("日志启动失败后 HTTP 监听未关闭")
	}
}

func TestSyslogHTTPStartupFailureClosesBothProtocols(t *testing.T) {
	a, _ := assembled(t)
	address := freeSyslogAddress(t)
	a.Machine = &machineBundle{Logs: events.New(nil, func() int64 { return assemblyAt })}
	a.Config.Machine.SyslogListen = address
	a.Config.TLS = tlsConfig{Cert: filepath.Join(t.TempDir(), "missing.crt"), Key: filepath.Join(t.TempDir(), "missing.key")}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := serveListener(t.Context(), a, listener); err == nil {
		t.Fatal("证书缺失时仍启动成功")
	}
	packet, stream, err := listenSyslog(address)
	if err != nil {
		t.Fatal("HTTP 启动失败后日志监听未释放", err)
	}
	packet.Close()
	stream.Close()
	if conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("HTTP 启动失败后监听未关闭")
	}
}

func TestServeSyslogClosesActiveTCPOnCancelAndHTTPExit(t *testing.T) {
	for _, reason := range []string{"cancel", "http-listener-failure"} {
		t.Run(reason, func(t *testing.T) {
			a, _ := assembled(t)
			address := freeSyslogAddress(t)
			receiver := syslogLifecycleReceiver{received: make(chan struct{}, 2)}
			collector := events.New(receiver, func() int64 { return assemblyAt })
			// 本例仅检验网络清理。凭据流和保存行为另由真实 TCP 交付回归覆盖。
			collector.ObserveIP("task-lifecycle", 1, "127.0.0.1:1", assemblyAt+60)
			a.Machine = &machineBundle{Logs: collector}
			a.Config.Machine.SyslogListen = address
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serveListener(ctx, a, listener) }()
			var waited bool
			defer func() {
				cancel()
				if !waited {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("服务未清理退出")
					}
				}
			}()
			transport := http.DefaultTransport.(*http.Transport).Clone()
			defer transport.CloseIdleConnections()
			client := http.Client{Transport: transport, Timeout: 3 * time.Second}
			response, err := client.Get("http://" + listener.Addr().String() + "/console/v1/user/info")
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := io.WriteString(conn, "accepted\nunfinished"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-receiver.received:
			case <-time.After(3 * time.Second):
				t.Fatal("连接未进入实际日志接收")
			}
			// 不先关客户端；服务必须主动关仍在等待下一条消息的连接。
			if reason == "cancel" {
				cancel()
			} else {
				listener.Close()
			}
			select {
			case err := <-done:
				waited = true
				if (reason == "cancel") != (err == nil) {
					t.Error("服务退出结果错误", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("活跃 TCP 连接阻塞了服务退出")
			}
			conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := conn.Read(make([]byte, 1)); err == nil {
				t.Fatal("服务退出后 TCP 连接仍可读")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("服务退出后活跃 TCP 连接未关闭")
			}
			packet, stream, err := listenSyslog(address)
			if err != nil {
				t.Fatal("服务退出后日志地址仍占用", err)
			}
			packet.Close()
			stream.Close()
		})
	}
}

// 只为测试准备真实的同号 TCP/UDP。准备时若另一协议已被占用，关闭后换一对。
// 被测工厂首轮仍会真正调用 net.Listen，不能用人工错误冒充端口碰撞。
func reserveSyslogTestPair(t *testing.T) (net.PacketConn, net.Listener) {
	t.Helper()
	for i := 0; i < 8; i++ {
		stream, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		packet, err := net.ListenPacket("udp", stream.Addr().String())
		if err == nil {
			t.Cleanup(func() { packet.Close(); stream.Close() })
			return packet, stream
		}
		stream.Close()
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatal(err)
		}
	}
	t.Fatal("无法为回归准备同号的真实监听")
	return nil, nil
}

func requireSyslogPacketClosed(t *testing.T, packet net.PacketConn) {
	t.Helper()
	if err := packet.SetReadDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
		t.Fatal("失败轮次的 UDP 未关闭", err)
	}
}

func TestSyslogZeroPortRetriesRealTCPCollision(t *testing.T) {
	blockedUDP, blockedTCP := reserveSyslogTestPair(t)
	availableUDP, availableTCP := reserveSyslogTestPair(t)
	packetCalls, streamCalls := 0, 0
	packet, stream, err := listenSyslogWith("127.0.0.1:0", func(network, address string) (net.PacketConn, error) {
		packetCalls++
		if network != "udp" || address != "127.0.0.1:0" {
			t.Fatal("重选时改变了原地址或协议", network, address)
		}
		switch packetCalls {
		case 1:
			return blockedUDP, nil
		case 2:
			requireSyslogPacketClosed(t, blockedUDP)
			return availableUDP, nil
		default:
			t.Fatal("可用轮次成功后仍继续分配")
			return nil, errors.New("unexpected extra allocation")
		}
	}, func(network, address string) (net.Listener, error) {
		streamCalls++
		if network != "tcp" {
			t.Fatal("没有绑定 TCP")
		}
		if streamCalls == 1 {
			if address != blockedTCP.Addr().String() {
				t.Fatal("没有使用首轮 UDP 实际端口")
			}
			unexpected, collision := net.Listen(network, address)
			if unexpected != nil {
				unexpected.Close()
			}
			if !errors.Is(collision, syscall.EADDRINUSE) {
				t.Fatal("首轮没有发生真实 TCP 地址占用", collision)
			}
			t.Log("首轮真实 TCP 地址占用", collision)
			return nil, collision
		}
		if streamCalls != 2 || address != availableUDP.LocalAddr().String() {
			t.Fatal("重选后两种协议没有使用同一实际端口")
		}
		return availableTCP, nil
	})
	requireSyslogPacketClosed(t, blockedUDP)
	if err != nil {
		t.Fatal("随机端口遇真实 TCP 占用后未重新分配", err)
	}
	if packet == nil || stream == nil {
		t.Fatal("重选成功却没有返回两种真实监听")
	}
	defer packet.Close()
	defer stream.Close()
	if packetCalls != 2 || streamCalls != 2 || packet.LocalAddr().String() != stream.Addr().String() || packet.LocalAddr().String() == blockedTCP.Addr().String() {
		t.Fatal("重选后的同端口双协议结果错误", packetCalls, streamCalls)
	}
	// 返回的必须是仍可用的真实 UDP 和 TCP，不能只拼出相同地址。
	sender, err := net.Dial("udp", packet.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	if _, err := sender.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	packet.SetReadDeadline(time.Now().Add(time.Second))
	body := make([]byte, 16)
	if n, _, err := packet.ReadFrom(body); err != nil || string(body[:n]) != "ready" {
		t.Fatal("重选后的 UDP 不可用", err)
	}
	client, err := net.DialTimeout("tcp", stream.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("重选后的 TCP 不可用", err)
	}
	client.Close()
}

func TestSyslogZeroPortCollisionExhaustionReleasesUDP(t *testing.T) {
	firstUDP, blockedTCP := reserveSyslogTestPair(t)
	packets := []net.PacketConn{firstUDP}
	packetCalls, streamCalls := 0, 0
	packet, stream, err := listenSyslogWith("127.0.0.1:0", func(network, address string) (net.PacketConn, error) {
		packetCalls++
		if packetCalls > 8 {
			t.Fatal("随机端口碰撞重试超过总共 8 次")
		}
		if network != "udp" || address != "127.0.0.1:0" {
			t.Fatal("重试改变原监听配置")
		}
		if packetCalls == 1 {
			return firstUDP, nil
		}
		requireSyslogPacketClosed(t, packets[len(packets)-1])
		next, err := net.ListenPacket(network, blockedTCP.Addr().String())
		if err != nil {
			t.Fatal("重试前旧 UDP 仍占用端口", err)
		}
		t.Cleanup(func() { next.Close() })
		packets = append(packets, next)
		return next, nil
	}, func(network, address string) (net.Listener, error) {
		streamCalls++
		if network != "tcp" || address != blockedTCP.Addr().String() {
			t.Fatal("没有尝试本轮真实 UDP 地址")
		}
		unexpected, collision := net.Listen(network, address)
		if unexpected != nil {
			unexpected.Close()
		}
		if !errors.Is(collision, syscall.EADDRINUSE) {
			t.Fatal("耗尽回归没有产生真实 TCP 地址占用", collision)
		}
		return nil, collision
	})
	if packet != nil || stream != nil || !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatal("耗尽后隐藏错误或返回了监听资源", err)
	}
	for _, failed := range packets {
		requireSyslogPacketClosed(t, failed)
	}
	if packetCalls != 8 || streamCalls != 8 {
		t.Fatal("随机端口碰撞未在总共 8 次内有界重选", packetCalls, streamCalls)
	}
	probe, err := net.ListenPacket("udp", blockedTCP.Addr().String())
	if err != nil {
		t.Fatal("重试耗尽后 UDP 端口仍占用", err)
	}
	probe.Close()
}

func TestSyslogFixedPortCollisionIsNotRetried(t *testing.T) {
	reservedUDP, blockedTCP := reserveSyslogTestPair(t)
	address := blockedTCP.Addr().String()
	packetCalls, streamCalls := 0, 0
	packet, stream, err := listenSyslogWith(address, func(network, requested string) (net.PacketConn, error) {
		packetCalls++
		if packetCalls != 1 || network != "udp" || requested != address {
			t.Fatal("固定端口被静默重选")
		}
		return reservedUDP, nil
	}, func(network, requested string) (net.Listener, error) {
		streamCalls++
		return net.Listen(network, requested)
	})
	if packet != nil || stream != nil || !errors.Is(err, syscall.EADDRINUSE) || packetCalls != 1 || streamCalls != 1 {
		t.Fatal("固定端口碰撞未立即返回原错误", err, packetCalls, streamCalls)
	}
	requireSyslogPacketClosed(t, reservedUDP)
}

func TestSyslogZeroPortOtherErrorsAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage string
		err   error
	}{
		{"TCP-permission", "tcp", syscall.EACCES},
		{"TCP-unknown", "tcp", errors.New("stream failed")},
		{"UDP-address-in-use", "udp", syscall.EADDRINUSE},
		{"UDP-permission", "udp", syscall.EACCES},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reservedUDP, _ := reserveSyslogTestPair(t)
			packetCalls, streamCalls := 0, 0
			packet, stream, err := listenSyslogWith("127.0.0.1:0", func(network, address string) (net.PacketConn, error) {
				packetCalls++
				if packetCalls != 1 || network != "udp" || address != "127.0.0.1:0" {
					t.Fatal("非 TCP 地址占用错误触发了重试")
				}
				if tc.stage == "udp" {
					return nil, tc.err
				}
				return reservedUDP, nil
			}, func(network, address string) (net.Listener, error) {
				streamCalls++
				if streamCalls != 1 || network != "tcp" || address != reservedUDP.LocalAddr().String() {
					t.Fatal("非地址占用错误后再次绑定或改变地址")
				}
				return nil, tc.err
			})
			if packet != nil || stream != nil || !errors.Is(err, tc.err) || packetCalls != 1 {
				t.Fatal("非 TCP 地址占用错误未原样失败", err, packetCalls)
			}
			if tc.stage == "udp" {
				if streamCalls != 0 {
					t.Fatal("UDP 失败后仍调用 TCP")
				}
			} else {
				if streamCalls != 1 {
					t.Fatal("非地址占用错误被重试", streamCalls)
				}
				requireSyslogPacketClosed(t, reservedUDP)
			}
		})
	}
}
