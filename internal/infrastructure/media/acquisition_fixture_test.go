package iso

import (
	"context"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
)

// 只供原有真实库测试组装新来源能力与尚未迁移的导入能力。
type acquisitionFixture struct {
	*Sources
	acquisition *appmedia.Sources
	transfer    *Transfer
}

func newAcquisitionFixture(st repository.Store, d Dirs) *acquisitionFixture {
	locks, gate := NewSourceLocks(), NewRegistrationGate()
	transfer := NewTransfer(d)
	return &acquisitionFixture{Sources: New(st, d, locks, gate), transfer: transfer, acquisition: appmedia.NewSources(appmedia.SourceDeps{ISOs: st, Sources: st, Transfer: transfer, Locks: locks, Gate: gate})}
}
func (s *acquisitionFixture) Register(ctx context.Context, i model.ISO, k, a, r string, n int64, d string, audit repository.Audit) error {
	return s.acquisition.Register(ctx, i, k, a, r, n, d, audit)
}
func (s *acquisitionFixture) RegisterQueued(ctx context.Context, i model.ISO, k, a, r string, n int64, d string, audit repository.Audit) error {
	return s.acquisition.RegisterQueued(ctx, i, k, a, r, n, d, audit)
}
func (s *acquisitionFixture) Upload(ctx context.Context, id, r string, n int64, b io.Reader, a repository.Audit) error {
	return s.acquisition.Upload(ctx, id, r, n, b, a)
}
func (s *acquisitionFixture) Cancel(ctx context.Context, id string, a repository.Audit) error {
	return s.acquisition.Cancel(ctx, id, a)
}
func (s *acquisitionFixture) Retry(ctx context.Context, id string, a repository.Audit) error {
	return s.acquisition.Retry(ctx, id, a)
}
func (s *acquisitionFixture) RequestReimport(ctx context.Context, id string, a repository.Audit) error {
	return s.acquisition.RequestReimport(ctx, id, a)
}
func (s *acquisitionFixture) Reimport(ctx context.Context, id string, a repository.Audit) error {
	if err := s.RequestReimport(ctx, id, a); err != nil {
		return err
	}
	return s.Import(ctx, id)
}
func (s *acquisitionFixture) FetchOne(ctx context.Context, id, r string) error {
	return s.acquisition.FetchOne(ctx, id, r)
}
func (s *acquisitionFixture) Recover(ctx context.Context) ([]appmedia.ResumeWork, error) {
	return s.acquisition.Recover(ctx)
}
func (s *acquisitionFixture) FreeSpace(ctx context.Context) (int64, int64, error) {
	return s.acquisition.FreeSpace(ctx)
}
func (s *acquisitionFixture) Progress(ctx context.Context, id string) (appmedia.AcquisitionProgress, error) {
	return s.acquisition.Progress(ctx, id)
}
