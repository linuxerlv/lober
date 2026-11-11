// Package selection 负责进程内的模板选择锁。
package selection

import (
	"github.com/lober/lober/internal/application/ports"
	"sync"
)

type guard struct{ sync.RWMutex }

func NewGuard() ports.SelectionGuard { return &guard{} }
func (g *guard) ReadLock() func()    { g.RLock(); return g.RUnlock }
func (g *guard) WriteLock() func()   { g.Lock(); return g.Unlock }
