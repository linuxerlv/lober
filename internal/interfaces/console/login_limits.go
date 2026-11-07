package console

import (
	"crypto/sha256"
	"net"
	"strings"
	"sync"
)

type loginWindow struct {
	start int64
	count int
}
type loginLimits struct {
	mu              sync.Mutex
	now             func() int64
	global          loginWindow
	peers, accounts map[[32]byte]loginWindow
	active          int
}

func newLoginLimits(now func() int64) *loginLimits {
	return &loginLimits{now: now, peers: make(map[[32]byte]loginWindow), accounts: make(map[[32]byte]loginWindow)}
}

func loginRemaining(w loginWindow, now int64, limit int) int64 {
	if w.count == 0 || now >= w.start+60 {
		return 0
	}
	if w.count >= limit {
		return max(int64(1), w.start+60-now)
	}
	return 0
}

func loginIncrement(w loginWindow, now int64) loginWindow {
	if w.count == 0 || now >= w.start+60 {
		return loginWindow{start: now, count: 1}
	}
	w.count++
	return w
}

func loginKey(value string) [32]byte { return sha256.Sum256([]byte(value)) }

func (l *loginLimits) peer(remote string) int64 {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	key := loginKey(host)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, w := range l.peers {
		if now >= w.start+60 {
			delete(l.peers, k)
		}
	}
	for k, w := range l.accounts {
		if now >= w.start+60 {
			delete(l.accounts, k)
		}
	}
	if wait := max(loginRemaining(l.global, now, 120), loginRemaining(l.peers[key], now, 30)); wait > 0 {
		return wait
	}
	if _, ok := l.peers[key]; !ok && len(l.peers) >= 4096 {
		return 60
	}
	l.global = loginIncrement(l.global, now)
	l.peers[key] = loginIncrement(l.peers[key], now)
	return 0
}

func (l *loginLimits) acquire(username string) (func(), int64) {
	key := loginKey(strings.ToLower(strings.TrimSpace(username)))
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if wait := loginRemaining(l.accounts[key], now, 10); wait > 0 {
		return nil, wait
	}
	if _, ok := l.accounts[key]; !ok && len(l.accounts) >= 4096 {
		return nil, 60
	}
	if l.active >= 4 {
		return nil, 1
	}
	l.accounts[key] = loginIncrement(l.accounts[key], now)
	l.active++
	return func() { l.mu.Lock(); l.active--; l.mu.Unlock() }, 0
}
