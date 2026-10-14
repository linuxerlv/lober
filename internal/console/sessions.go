package console

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"sync"
)

type session struct {
	username string
	expires  int64
}
type sessions struct {
	mu      sync.Mutex
	byToken map[string]session
	ttl     int64
	now     Clock
}

func newSessions(ttl int64, now Clock) *sessions {
	if ttl == 0 {
		ttl = 12 * 3600
	}
	if ttl < 0 || now == nil {
		panic("invalid session dependencies")
	}
	return &sessions{byToken: make(map[string]session), ttl: ttl, now: now}
}
func (s *sessions) put(username string) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(buf)
	now := s.now()
	expiry := int64(math.MaxInt64)
	if now >= 0 && s.ttl <= math.MaxInt64-now {
		expiry = now + s.ttl
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, item := range s.byToken {
		if item.expires <= now {
			delete(s.byToken, tok)
		}
	}
	s.byToken[token] = session{username, expiry}
	return token
}
func (s *sessions) lookup(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.byToken[token]
	if !ok {
		return "", false
	}
	if item.expires <= s.now() {
		delete(s.byToken, token)
		return "", false
	}
	return item.username, true
}
func (s *sessions) drop(token string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.byToken, token) }
