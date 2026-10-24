package identity

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
)

var (
	ErrLoginFailed              = errors.New("账号或密码错误")
	ErrBadRequest               = errors.New("请求参数错误")
	ErrSelfDisable              = errors.New("不能禁用本人")
	ErrInitialPasswordUnchanged = errors.New("必须更换初始密码")
)

// Principal 只由本实例认证产生；请求正文不能提供或改写它。
type Principal struct {
	user  model.User
	token string
	owner *Service
}

// Actor 返回普通身份值，不含密码和令牌；零值快照得到零值 Actor。
// 它不读仓储或时钟，不改变快照，也不触发审计。
func (p Principal) Actor() ports.Actor {
	if p.owner == nil {
		return ports.Actor{}
	}
	return ports.Actor{Username: p.user.Username, Role: p.user.Role, Disabled: p.user.Disabled, MustChangePassword: p.user.MustChangePassword}
}

type LoginCommand struct{ Username, Password string }
type SessionResult struct {
	Token     string
	ExpiresAt int64
	User      UserView
}
type UserView struct {
	Username, DisplayName, Email, Avatar, Bio string
	Role                                      model.Role
	Permissions, MenuIDs                      []string
	Disabled, MustChangePassword              bool
	CreatedAt, Rev, MenuRev                   int64
}
type Operation = access.Operation

const (
	General          = access.General
	UserInfo         = access.UserInfo
	Logout           = access.Logout
	ChangePassword   = access.ChangePassword
	LegacySelfUpdate = access.LegacySelfUpdate
)

type AccessRequest struct {
	Permission     string
	Operation      Operation
	TargetUsername string
}

// Access 处理本实例认证；Principal 只在当前请求内使用，不能跨请求缓存。
// 以下方法不写数据库、不写审计；只有登录、退出及到期/异常认证会改变内存会话。
// context 仅传给原仓储读取，不额外插入取消检查，也不把仓储故障改成未认证。
type Access interface {
	// Login 不清理输入；空字段先报 BadRequest，账号缺失或凭据失效统一报 LoginFailed。
	// 先真/假散列校验，再持读锁重读凭据、读取菜单和发布；菜单失败不发令牌。
	// 重复成功产生独立会话；密码计算在锁外，context 传给各次读取。
	Login(context.Context, LoginCommand) (SessionResult, error)
	// Logout 先检查快照归属和退出权限，再只删本令牌；本方法重复删除无额外错误。
	// 后续请求仍须重新认证；不读取仓储、时钟或取消状态。
	Logout(context.Context, Principal) error
	// CurrentUser 先检查归属和本人信息权限，再取本次有效菜单，不再次读取用户。
	// 用户字段来自认证快照，菜单错误原样返回；重复读取可看到菜单新配置。
	CurrentUser(context.Context, Principal) (UserView, error)
	// Authenticate 先查令牌，再读最新用户；缺失/到期令牌不访问仓储。
	// 缺失或禁用账号删除该令牌；无效角色只拒绝，仓储故障保留原错误。
	// 每次返回本实例拥有的值快照；不持凭据协调锁，context 传给 GetUser。
	Authenticate(context.Context, string) (Principal, error)
	// Authorize 先归属和身份，再改密要求，最后权限；未知操作拒绝。
	// 旧本人更新先过改密检查再查管理权限，普通用户保持 Forbidden。
	// 只判断快照，不读时钟、会话或仓储；重复判断无副作用。
	Authorize(Principal, AccessRequest) error
	// StreamAuthorized 每次先查原令牌，再读账号，检查禁用、改密、角色和指定权限。
	// 账号异常返回 false，不额外删除令牌；context 传给读取，事件发送留在入口。
	StreamAuthorized(context.Context, string, string) bool
}
