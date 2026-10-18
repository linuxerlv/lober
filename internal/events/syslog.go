// Package events collects bounded installer logs attributed by authenticated traffic.
package events

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/netip"
	"sync"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

type Repository interface {
	GetTask(context.Context, string) (model.Task, error)
	AppendEvents(context.Context, string, []store.NewEvent) ([]store.Appended, error)
}
type Notifier interface{ Publish(string) }
type lease struct {
	task    string
	attempt int
	expires int64
}
type Collector struct {
	repo     Repository
	now      func() int64
	notifier Notifier
	mu       sync.Mutex
	leases   map[netip.Addr]lease
}

func New(repo Repository, now func() int64, notifier Notifier) *Collector {
	return &Collector{repo: repo, now: now, notifier: notifier, leases: map[netip.Addr]lease{}}
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
	task, err := c.repo.GetTask(ctx, binding.task)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if task.State.Finished() || task.Execution == nil || task.Execution.Attempt != binding.attempt {
		return nil
	}
	if len(body) > 65536 {
		body = body[:65536]
	}
	safe := model.SafeLogTail(body)
	_, err = c.repo.AppendEvents(ctx, task.ID, []store.NewEvent{{At: at, Kind: "installer.syslog", Fields: map[string]string{"source_ip": addr.String(), "log_base64": base64.StdEncoding.EncodeToString(safe)}}})
	if err == nil && c.notifier != nil {
		c.notifier.Publish(task.ID)
	}
	return err
}
