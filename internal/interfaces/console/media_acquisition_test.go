package console

import (
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
)

type mediaTestWaker struct{}

func (mediaTestWaker) Wake() {}
func newRealMediaControl(st repository.Store, d iso.Dirs) port.SourceControl {
	locks, gate := iso.NewSourceLocks(), iso.NewRegistrationGate()
	return appmedia.NewControl(appmedia.NewSources(appmedia.SourceDeps{ISOs: st, Sources: st, Transfer: iso.NewTransfer(d), Locks: locks, Gate: gate}), mediaTestWaker{})
}
