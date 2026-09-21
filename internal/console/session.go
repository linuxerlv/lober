// 本文件实现内存会话表：登录后签发的不透明 token 到用户身份的映射，
// 带固定 TTL 过期。对应实现计划任务 1，依据 SPEC §9.4、§12.5。
//
// 不做 JWT、不做滑动过期——理由见计划任务 1 的设计说明：需要即时吊销
// （运维离职、误操作后锁账号），JWT 要么带黑名单（等于还要存储）要么无法
// 吊销；固定过期比滑动过期更容易推理，装机会话通常在 TTL 内完成。
package console

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// sessionUser 是会话表里存的用户身份快照：登录时从配置的 Auth.Users 拷贝，
// 之后不再随配置变动而变化（会话生命周期内身份是固定的，配置热改不影响
// 已登录会话——这与「重启即全员登出」是同一个方向：会话状态只在内存，
// 不追踪外部配置的实时变化）。
type sessionUser struct {
	Username string
	RealName string
	Roles    []string
}

// sessionEntry 是会话表一行：用户身份加过期时间（Unix 秒）。
type sessionEntry struct {
	user      sessionUser
	expiresAt int64
}

// sessionStore 是内存会话表，一把互斥锁保护。
//
// 存在的理由：重启即全员登出是可接受的（计划任务 1）——装机系统不是 7×24
// 交互系统，重启是计划内的运维动作。
type sessionStore struct {
	mu    sync.Mutex
	byTok map[string]sessionEntry
}

func newSessionStore() *sessionStore {
	return &sessionStore{byTok: make(map[string]sessionEntry)}
}

// issueToken 生成一个不可预测的随机 token 并生成十六进制串。32 字节
// （256 位）随机性足以抵御猜测攻击，且十六进制串本身不含需要转义的字符，
// 可以直接放进 HTTP 头。
func issueToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// create 签发一个新会话，expiresAt 由调用方按配置的 TTL 算好传入（本类型
// 不持有时钟——Server 持有 contract.Clock，由 Server 算出过期时间，session
// 表本身保持对时间来源无感知，便于测试用固定时钟推进）。
func (s *sessionStore) create(user sessionUser, expiresAt int64) (string, error) {
	tok, err := issueToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byTok[tok] = sessionEntry{user: user, expiresAt: expiresAt}
	return tok, nil
}

// lookup 按 token 查会话。ok 为 false 表示 token 不存在或已过期——这两种
// 情况统一返回「未认证」，不额外区分（避免向调用方泄露 token 曾经存在过，
// 与 contract.Store 的 GetTaskByToken 对 token 查询的处理方向一致）。
//
// now 由调用方传入（Server 的 contract.Clock.Now()），使过期判定可在测试
// 里用可控时钟精确验证。
func (s *sessionStore) lookup(token string, now int64) (sessionUser, bool) {
	if token == "" {
		return sessionUser{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byTok[token]
	if !ok {
		return sessionUser{}, false
	}
	if entry.expiresAt < now {
		// 过期会话顺手清理掉，避免内存表无限增长（登录频率不高，这里没有
		// 并发热点问题）。
		delete(s.byTok, token)
		return sessionUser{}, false
	}
	return entry.user, true
}

// revoke 删除一个会话，使该 token 立即失效（登出）。删除不存在的 token
// 是无操作，不报错——登出端点的语义是「确保这个 token 不再有效」，token
// 本来就不存在时已经满足这个语义。
func (s *sessionStore) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byTok, token)
}
