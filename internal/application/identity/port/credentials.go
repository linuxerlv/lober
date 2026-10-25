package port

import "errors"

var ErrInvalidPassword = errors.New("口令不可保存")

// Passwords 只负责固定强度口令计算，不读写用户、会话或审计，也不接收取消状态。
// 各方法的八项纯能力边界唯一连接 docs/contracts/identity-audit.md「身份纯值与凭据能力」，保留下方强度与错误差异。
type Passwords interface {
	// Hash 只接受非空且可保存的文本，否则 ErrInvalidPassword；随机源故障原样返回。
	// 每次用新随机盐，结果保持固定格式，不缓存或降低强度。
	Hash(string) (string, error)
	// Check 的参数依次为明文、已存散列；坏编码直接 false，合法编码固定600000轮并常量时间比较。
	Check(string, string) bool
	// CheckDummy 对缺失账号的明文执行原固定假散列计算，无返回值和保存副作用。
	CheckDummy(string)
}

// Tokens 每次产生32字节随机数的64位小写十六进制；随机故障返回错误，不保存状态。
// NewToken 的八项纯能力约定唯一连接 docs/contracts/identity-audit.md「身份纯值与凭据能力」。
type Tokens interface{ NewToken() (string, error) }
