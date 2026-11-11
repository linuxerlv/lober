// Package events collects bounded installer logs attributed by authenticated traffic.
package events

import (
	"context"
	"net"
	"net/netip"
	"sync"

	executionport "github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/model"
)

type lease struct {
	task    string
	attempt int
	expires int64
}
type Collector struct {
	receiver executionport.InstallerLogReceiver
	now      func() int64
	mu       sync.Mutex
	leases   map[netip.Addr]lease
}

func New(receiver executionport.InstallerLogReceiver, now func() int64) *Collector {
	return &Collector{receiver: receiver, now: now, leases: map[netip.Addr]lease{}}
}
func ip(address string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(host)
	return addr.Unmap(), err == nil
}

// ObserveIP must only be called by a gateway after a successful credential check.
// Forwarded headers are deliberately ignored; deployments need direct guest routing.
func (c *Collector) ObserveIP(task string, attempt int, address string, expires int64) {
	addr, ok := ip(address)
	if !ok || !model.ValidID(task) || attempt < 1 || expires <= c.now() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for addr, value := range c.leases {
		if value.expires <= c.now() {
			delete(c.leases, addr)
		}
	}
	if _, exists := c.leases[addr]; !exists && len(c.leases) >= 4096 {
		return
	}
	c.leases[addr] = lease{task: task, attempt: attempt, expires: expires}
}

func (c *Collector) Receive(ctx context.Context, address string, body []byte) error {
	addr, ok := ip(address)
	if !ok {
		return nil
	}
	c.mu.Lock()
	binding, found := c.leases[addr]
	c.mu.Unlock()
	at := c.now()
	if !found || binding.expires <= at {
		return nil
	}
	if len(body) > 65536 {
		body = body[:65536]
	}
	return c.receiver.ReceiveInstallerLog(ctx, executionport.InstallerLog{TaskID: binding.task, Attempt: binding.attempt, SourceIP: addr.String(), At: at, Body: model.SafeLogTail(body)})
}
