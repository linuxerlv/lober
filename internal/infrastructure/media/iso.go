package iso

import (
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/repository"
)

type Dirs struct {
	Staging, Artifacts, SMB, Tools string
	LocalRoots                     []string
}
type Sources struct {
	store repository.Store
	dirs  Dirs
	locks port.SourceLocks
	gate  port.RegistrationGate
}

func New(st repository.Store, d Dirs, locks port.SourceLocks, gate port.RegistrationGate) *Sources {
	d.LocalRoots = append([]string(nil), d.LocalRoots...)
	return &Sources{store: st, dirs: d, locks: locks, gate: gate}
}
func (s *Sources) lockSource(id, role string) func() { return s.locks.Lock(id, role) }
