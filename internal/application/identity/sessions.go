package identity

import (
	"github.com/lober/lober/internal/application/identity/port"
	"github.com/lober/lober/internal/application/ports"
	"math"
	"sync"
)

type session struct {
	username string
	expires  int64
}

// SessionBook 只保存本实例的内存会话，不做持久化、审计或请求取消检查。
// 所有表读写由互斥锁保护；Clock 和 Tokens 由组装点提供，令牌生成在表锁外。
type SessionBook struct {
	mu      sync.Mutex
	byToken map[string]session
	ttl     int64
	now     ports.Clock
	tokens  port.Tokens
}

// NewSessionBook 的 ttl 单位是秒；0取43200，负值或缺少依赖 panic。
// 构造不调用时钟或令牌能力，每次构造得到独立空表。
func NewSessionBook(ttl int64, now ports.Clock, tokens port.Tokens) *SessionBook {
	if ttl == 0 {
		ttl = 12 * 3600
	}
	if ttl < 0 || now == nil || tokens == nil {
		panic("invalid session dependencies")
	}
	return &SessionBook{byToken: make(map[string]session), ttl: ttl, now: now, tokens: tokens}
}

// Put 先生成令牌，再读时钟，最后在锁内清理到期记录并发布；重复调用独立发令牌。
// 随机错误或输出不是64位小写十六进制时 panic；负时钟和加法溢出饱和到 MaxInt64。
func (s *SessionBook) Put(username string) string {
	token, err := s.tokens.NewToken()
	if err != nil {
		panic(err)
	}
	if len(token) != 64 {
		panic("无效会话令牌")
	}
	for _, c := range token {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			panic("无效会话令牌")
		}
	}
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

// Lookup 缺失令牌直接返回 false，不读时钟；存在才判断 expires<=now 并删除到期项。
// 查找及删除在同一表锁内；再次查询已删除令牌不额外读时钟。
func (s *SessionBook) Lookup(token string) (string, bool) {
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

// Drop 在表锁内只删除指定令牌；重复删除无额外结果。
func (s *SessionBook) Drop(token string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.byToken, token) }

// ExpiresAt 在表锁内读期限，缺失返回0；不读时钟或顺带清理。
func (s *SessionBook) ExpiresAt(token string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byToken[token].expires
}

// DropUser 在表锁内删除此用户全部令牌；重复调用无事发生，不影响其他用户。
func (s *SessionBook) DropUser(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, item := range s.byToken {
		if item.username == username {
			delete(s.byToken, token)
		}
	}
}
