package iso

import (
	"context"
	"github.com/lober/lober/internal/application/media/port"
	"sync"
)

type SourceLocks struct {
	files  sync.Map
	active sync.Map
}

var _ port.SourceLocks = (*SourceLocks)(nil)

func NewSourceLocks() *SourceLocks { return &SourceLocks{} }
func (s *SourceLocks) Lock(id, role string) func() {
	v, _ := s.files.LoadOrStore(id+"/"+role, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

type trackedCancel struct{ cancel context.CancelFunc }

func (s *SourceLocks) Track(id, role string, cancel context.CancelFunc) func() {
	key := id + "/" + role
	v := &trackedCancel{cancel}
	s.active.Store(key, v)
	return func() { s.active.CompareAndDelete(key, v) }
}
func (s *SourceLocks) Cancel(id, role string) {
	if v, ok := s.active.Load(id + "/" + role); ok {
		v.(*trackedCancel).cancel()
	}
}

type RegistrationGate struct{ sync.RWMutex }

var _ port.RegistrationGate = (*RegistrationGate)(nil)

func NewRegistrationGate() *RegistrationGate { return &RegistrationGate{} }
