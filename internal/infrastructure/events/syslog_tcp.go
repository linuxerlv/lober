package events

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const (
	tcpMaxConnections  = 64
	tcpMaxMessageBytes = 65536
	tcpIdleTimeout     = 30 * time.Second
)

// ServeTCP 接收原生换行日志，返回前关闭监听和全部连接并等待接收结束。
// 地址只取真实连接；每条完整消息都重新交给 Receive 检查当前租约。
func (c *Collector) ServeTCP(ctx context.Context, listener net.Listener, report func(error)) error {
	return c.serveTCP(ctx, listener, tcpIdleTimeout, report)
}

func (c *Collector) serveTCP(ctx context.Context, listener net.Listener, idle time.Duration, report func(error)) error {
	runCtx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	connections := make(map[net.Conn]struct{})
	var workers sync.WaitGroup
	closed := make(chan struct{})
	go func() {
		<-runCtx.Done()
		listener.Close()
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		close(closed)
	}()
	defer func() {
		cancel()
		<-closed
		workers.Wait()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		if runCtx.Err() != nil {
			mu.Unlock()
			conn.Close()
			return nil
		}
		if len(connections) >= tcpMaxConnections {
			mu.Unlock()
			conn.Close()
			continue
		}
		connections[conn] = struct{}{}
		workers.Add(1)
		mu.Unlock()
		go func() {
			defer func() {
				mu.Lock()
				delete(connections, conn)
				mu.Unlock()
				conn.Close()
				workers.Done()
			}()
			// 多留一个换行字节。超过上限时关闭整条连接，不收录截断片段。
			reader := bufio.NewReaderSize(tcpIdleReader{conn, idle}, tcpMaxMessageBytes+1)
			for {
				frame, err := reader.ReadSlice('\n')
				if err != nil {
					return
				}
				if runCtx.Err() != nil {
					return
				}
				if err := c.receiveTCPFrame(runCtx, conn.RemoteAddr().String(), frame[:len(frame)-1]); err != nil && runCtx.Err() == nil && report != nil {
					report(err)
				}
			}
		}()
	}
}

// 每次真正读取网络数据前续上空闲期限；缓冲区中的多条消息仍逐条处理。
type tcpIdleReader struct {
	conn net.Conn
	idle time.Duration
}

func (r tcpIdleReader) Read(body []byte) (int, error) {
	if err := r.conn.SetReadDeadline(time.Now().Add(r.idle)); err != nil {
		return 0, err
	}
	return r.conn.Read(body)
}

func (c *Collector) receiveTCPFrame(ctx context.Context, address string, body []byte) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("installer syslog receiver panic")
		}
	}()
	return c.Receive(ctx, address, body)
}
