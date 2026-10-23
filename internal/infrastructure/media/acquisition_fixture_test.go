package iso

import (
	"context"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"log/slog"
	"path/filepath"
)

// 原有真实库测试复用同一组来源能力、导入能力和协调锁。
type acquisitionFixture struct {
	store       repository.Store
	dirs        Dirs
	locks       port.SourceLocks
	gate        port.RegistrationGate
	acquisition *appmedia.Sources
	transfer    *Transfer
}

func newAcquisitionFixture(st repository.Store, d Dirs) *acquisitionFixture {
	locks, gate := NewSourceLocks(), NewRegistrationGate()
	transfer := NewTransfer(d)
	return &acquisitionFixture{store: st, dirs: d, locks: locks, gate: gate, transfer: transfer, acquisition: appmedia.NewSources(appmedia.SourceDeps{ISOs: st, Sources: st, Transfer: transfer, Import: NewImportFiles(d), Locks: locks, Gate: gate})}
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

func (s *acquisitionFixture) Import(ctx context.Context, id string) error {
	service := appmedia.NewSources(appmedia.SourceDeps{ISOs: s.store, Sources: s.store, Transfer: s.transfer, Import: NewImportFiles(s.dirs), Locks: s.locks, Gate: s.gate})
	return service.Import(ctx, id)
}

func (s *acquisitionFixture) artifactRoot(series catalog.Series, version string, arch catalog.Arch) string {
	return filepath.Join(s.dirs.Artifacts, string(series), version, string(arch))
}
func (s *acquisitionFixture) stagingPath(id, role string) string {
	return s.finalPath(id, role) + ".part"
}
func (s *acquisitionFixture) sourcePath(src model.Source) string {
	if src.Kind == model.SourceLocal {
		return src.Path
	}
	return s.finalPath(src.ISOID, src.Role)
}

func (s *acquisitionFixture) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
}
func (s *acquisitionFixture) CleanupDeleted(ctx context.Context, snapshot port.DeletionSnapshot) error {
	journal, err := NewDeletionJournal(s.dirs.Staging)
	if err != nil {
		return err
	}
	if err = journal.Persist(ctx, snapshot); err != nil {
		return err
	}
	service := appmedia.NewDeletion(appmedia.DeletionDeps{ISOs: s.store, Sources: s.store, Journal: journal, Files: NewDeletedFiles(s.dirs), Locks: s.locks, Gate: s.gate, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return service.RetryAll(ctx)
}
