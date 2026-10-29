package events

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	executionport "github.com/lober/lober/internal/application/execution/port"
)

type tcpLogReceiver struct {
	logs chan executionport.InstallerLog
}

func (r *tcpLogReceiver) ReceiveInstallerLog(_ context.Context, in executionport.InstallerLog) error {
	in.Body = bytes.Clone(in.Body)
	r.logs <- in
	return nil
}

func startTCPLogs(t *testing.T, collector *Collector, idle time.Duration) (string, context.CancelFunc, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- collector.serveTCP(ctx, listener, idle, nil) }()
	var once sync.Once
	wait := func() {
		once.Do(func() {
			select {
			case err := <-done:
				if err != nil {
					t.Error("TCP 接收退出失败", err)
				}
			case <-time.After(3 * time.Second):
				t.Error("TCP 接收未退出")
			}
		})
	}
	t.Cleanup(func() { cancel(); wait() })
	return listener.Addr().String(), cancel, wait
}

func writeTCPLogs(t *testing.T, conn net.Conn, body string) {
	t.Helper()
	if _, err := io.WriteString(conn, body); err != nil {
		t.Fatal("TCP 日志写入失败", err)
	}
}

func dialTCPLogs(t *testing.T, address string) *net.TCPConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn.(*net.TCPConn)
}

func expectTCPClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("连接关闭后仍收到数据")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("TCP 连接未由服务关闭")
	}
}

func TestTCPLogsDiscardUnterminatedAndOversizeFrames(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"disconnect-before-newline", []byte("not a complete frame")},
		{"over-limit-before-newline", append(bytes.Repeat([]byte("x"), 65537), '\n')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 2)}
			collector := New(receiver, func() int64 { return 1000 })
			collector.ObserveIP("task-one", 1, "127.0.0.1:1", 2000)
			address, _, _ := startTCPLogs(t, collector, time.Second)
			conn := dialTCPLogs(t, address)
			// 超长连接可能在写完前被拒绝；读取关闭才是本例的断言。
			conn.Write(tc.body)
			conn.CloseWrite()
			expectTCPClosed(t, conn)
			select {
			case <-receiver.logs:
				t.Fatal("未结束或超长的消息被截成日志收录")
			default:
			}
		})
	}
}

func TestTCPLogsAcceptMaximumFrameAndKeepBinaryBytes(t *testing.T) {
	receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 2)}
	collector := New(receiver, func() int64 { return 1000 })
	collector.ObserveIP("task-one", 3, "127.0.0.1:1", 2000)
	address, _, _ := startTCPLogs(t, collector, time.Second)
	conn := dialTCPLogs(t, address)
	body := append(bytes.Repeat([]byte("x"), 65530), []byte("\x00TAIL!")...)
	if _, err := conn.Write(append(body, '\n')); err != nil {
		t.Fatal(err)
	}
	conn.CloseWrite()
	expectTCPClosed(t, conn)
	select {
	case got := <-receiver.logs:
		want := append(bytes.Repeat([]byte("x"), 4090), []byte("\x00TAIL!")...)
		if !bytes.Equal(got.Body, want) || got.TaskID != "task-one" || got.Attempt != 3 || got.SourceIP != "127.0.0.1" {
			t.Fatal("合法上限消息或实际归属改变")
		}
	default:
		t.Fatal("65536 字节消息被拒绝")
	}
}

func TestTCPLogsCheckCurrentLeaseForEveryFrame(t *testing.T) {
	receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 4)}
	var now atomic.Int64
	now.Store(1000)
	collector := New(receiver, now.Load)
	address, _, _ := startTCPLogs(t, collector, time.Second)
	// 没有凭据登记的真实来源不收录。
	unbound := dialTCPLogs(t, address)
	writeTCPLogs(t, unbound, "unbound\n")
	unbound.CloseWrite()
	expectTCPClosed(t, unbound)
	collector.ObserveIP("task-one", 1, "127.0.0.1:1", 1100)
	conn := dialTCPLogs(t, address)
	writeTCPLogs(t, conn, "current\n")
	select {
	case got := <-receiver.logs:
		if string(got.Body) != "current" || got.TaskID != "task-one" || got.Attempt != 1 {
			t.Fatal("未登记的消息被收录或本轮归属错误")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("有效租约消息未收录")
	}
	// 同一个仍打开的连接也必须重查租约，不能把归属缓存到连接上。
	now.Store(1100)
	writeTCPLogs(t, conn, "expired\n")
	conn.CloseWrite()
	expectTCPClosed(t, conn)
	select {
	case <-receiver.logs:
		t.Fatal("到期等号的消息仍收录")
	default:
	}
	collector.ObserveIP("task-two", 2, "127.0.0.1:1", 2000)
	renewed := dialTCPLogs(t, address)
	writeTCPLogs(t, renewed, "renewed\n")
	renewed.CloseWrite()
	expectTCPClosed(t, renewed)
	select {
	case got := <-receiver.logs:
		if string(got.Body) != "renewed" || got.TaskID != "task-two" || got.Attempt != 2 {
			t.Fatal("租约更新后仍使用旧归属")
		}
	default:
		t.Fatal("新轮次消息未收录")
	}
}

func TestTCPLogsBoundConnectionsAndReleaseClosedSlots(t *testing.T) {
	receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 128)}
	collector := New(receiver, func() int64 { return 1000 })
	collector.ObserveIP("task-one", 1, "127.0.0.1:1", 2000)
	address, _, _ := startTCPLogs(t, collector, time.Minute)
	var held []*net.TCPConn
	// 每个连接发送完整消息并等到收录，确认确已占用服务端连接名额。
	for i := 0; i < 64; i++ {
		conn := dialTCPLogs(t, address)
		held = append(held, conn)
		if _, err := io.WriteString(conn, "held\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-receiver.logs:
		case <-time.After(time.Second):
			t.Fatal("上限以内的连接未接收")
		}
	}
	excess := dialTCPLogs(t, address)
	expectTCPClosed(t, excess)
	held[0].CloseWrite()
	expectTCPClosed(t, held[0])
	replacement := dialTCPLogs(t, address)
	if _, err := io.WriteString(replacement, "replacement\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-receiver.logs:
		if string(got.Body) != "replacement" {
			t.Fatal("连接上限外的消息被收录")
		}
	case <-time.After(time.Second):
		t.Fatal("连接释放后仍不能接收")
	}
}

func TestTCPLogsCloseIdlePartialFrame(t *testing.T) {
	receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 2)}
	collector := New(receiver, func() int64 { return 1000 })
	collector.ObserveIP("task-one", 1, "127.0.0.1:1", 2000)
	address, _, _ := startTCPLogs(t, collector, time.Second)
	conn := dialTCPLogs(t, address)
	writeTCPLogs(t, conn, "unfinished")
	expectTCPClosed(t, conn)
	select {
	case <-receiver.logs:
		t.Fatal("空闲超时前的半条消息被收录")
	default:
	}
}

func TestTCPLogsCancelClosesActiveConnectionAndListener(t *testing.T) {
	receiver := &tcpLogReceiver{logs: make(chan executionport.InstallerLog, 2)}
	collector := New(receiver, func() int64 { return 1000 })
	collector.ObserveIP("task-one", 1, "127.0.0.1:1", 2000)
	address, cancel, wait := startTCPLogs(t, collector, time.Minute)
	conn := dialTCPLogs(t, address)
	writeTCPLogs(t, conn, "accepted\npartial")
	select {
	case <-receiver.logs:
	case <-time.After(time.Second):
		t.Fatal("连接未进入实际接收")
	}
	cancel()
	expectTCPClosed(t, conn)
	wait()
	if reopened, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		reopened.Close()
		t.Fatal("取消后监听仍接受连接")
	}
}
