package iso

import (
	"net"
	"time"
)

// 分别限制每个等待阶段，不限制整个镜像下载需要多久。
type downloadTimeouts struct {
	lookup, connect, tlsHandshake, responseHeader, readIdle time.Duration
}

func defaultDownloadTimeouts() downloadTimeouts {
	return downloadTimeouts{
		lookup: 10 * time.Second, connect: 10 * time.Second,
		tlsHandshake: 10 * time.Second, responseHeader: 30 * time.Second,
		readIdle: 60 * time.Second,
	}
}

type idleTimeoutConn struct {
	net.Conn
	idle time.Duration
}

func (c idleTimeoutConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c idleTimeoutConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
