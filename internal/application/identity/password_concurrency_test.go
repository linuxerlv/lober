package identity

import (
	"github.com/lober/lober/internal/application/identity/port"
	"sync/atomic"
)

// 首次完成核验后暂停，其他请求仍使用真实密码算法，不改共享依赖。
type onceCheckBarrier struct {
	port.Passwords
	entered, resume chan struct{}
	calls           atomic.Int32
}

func (g *onceCheckBarrier) Check(p, h string) bool {
	ok := g.Passwords.Check(p, h)
	if g.calls.Add(1) == 1 {
		close(g.entered)
		<-g.resume
	}
	return ok
}
