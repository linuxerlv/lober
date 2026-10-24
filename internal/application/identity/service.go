package identity

import (
	"errors"
	"github.com/lober/lober/internal/application/identity/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/repository"
	"sync"
)

type Deps struct {
	Users     port.Users
	RoleMenus port.RoleMenus
	Passwords port.Passwords
	Sessions  *SessionBook
	Clock     ports.Clock
}
type Service struct {
	users       port.Users
	roleMenus   port.RoleMenus
	passwords   port.Passwords
	sessions    *SessionBook
	clock       ports.Clock
	credentials sync.RWMutex
}

var _ Access = (*Service)(nil)

// NewService 要求五项依赖均非nil，否则 panic；直接使用传入的同一个会话表。
// 构造不读取时钟或仓储；凭据锁由此实例独占，后续写入成功后才在写锁内撤销会话。
func NewService(d Deps) *Service {
	if d.Users == nil || d.RoleMenus == nil || d.Passwords == nil || d.Sessions == nil || d.Clock == nil {
		panic("身份服务缺少依赖")
	}
	return &Service{users: d.Users, roleMenus: d.RoleMenus, passwords: d.Passwords, sessions: d.Sessions, clock: d.Clock}
}

// 新口令校验先于外部计算；能力层的同类错误继续沿用原保存错误。
func (s *Service) hashPassword(password string) (string, error) {
	if !access.ValidPasswordText(password) {
		return "", repository.ErrInvalid
	}
	hash, err := s.passwords.Hash(password)
	if errors.Is(err, port.ErrInvalidPassword) {
		return "", repository.ErrInvalid
	}
	return hash, err
}
