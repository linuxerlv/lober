package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"log/slog"
	"sync"
)

type DeletionDeps struct {
	ISOs    port.ISOs
	Sources port.Sources
	Journal port.DeletionJournal
	Files   port.DeletedFiles
	Locks   port.SourceLocks
	Gate    port.RegistrationGate
	Logger  *slog.Logger
}
type Deletion struct {
	isos    port.ISOs
	sources port.Sources
	journal port.DeletionJournal
	files   port.DeletedFiles
	locks   port.SourceLocks
	gate    port.RegistrationGate
	logger  *slog.Logger
	mu      sync.Mutex
}

func NewDeletion(d DeletionDeps) *Deletion {
	if d.ISOs == nil || d.Sources == nil || d.Journal == nil || d.Files == nil || d.Locks == nil || d.Gate == nil || d.Logger == nil {
		panic("media deletion requires all dependencies")
	}
	return &Deletion{isos: d.ISOs, sources: d.Sources, journal: d.Journal, files: d.Files, locks: d.Locks, gate: d.Gate, logger: d.Logger}
}
func (j *Deletion) Delete(ctx context.Context, id string, rev int64, a repository.Audit) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.gate.Lock()
	item, err := j.isos.GetISO(ctx, id)
	if err != nil {
		j.gate.Unlock()
		return err
	}
	if item.Rev != rev {
		j.gate.Unlock()
		return repository.ErrConflict
	}
	sources, err := j.sources.SourcesOf(ctx, id)
	if err != nil {
		j.gate.Unlock()
		return err
	}
	if sources == nil {
		sources = []model.Source{}
	}
	snapshot := port.DeletionSnapshot{ISO: item, Sources: sources}
	if err = j.journal.Persist(ctx, snapshot); err != nil {
		j.gate.Unlock()
		return err
	}
	err = j.isos.DeleteISO(ctx, id, rev, a)
	j.gate.Unlock()
	if err != nil {
		// 删除失败或提交结果不明时，只有确认记录仍在才撤销快照。
		if _, readErr := j.isos.GetISO(ctx, id); readErr == nil {
			if discardErr := j.journal.Discard(ctx, id); discardErr != nil {
				j.logger.Warn("删除快照清理等待重试", "iso", id)
			}
		}
		return err
	}
	if err = j.cleanup(ctx, snapshot); err != nil {
		j.logger.Warn("ISO 字节清理等待重试", "iso", id)
		return nil
	}
	if err = j.journal.Discard(ctx, id); err != nil {
		j.logger.Warn("删除快照清理等待重试", "iso", id)
	}
	return nil
}

func (j *Deletion) RetryAll(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	snapshots, listErr := j.journal.List(ctx)
	errs := []error{listErr}
	for _, snapshot := range snapshots {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := mediarules.CheckDeletion(snapshot.ISO, snapshot.Sources); err != nil {
			errs = append(errs, err)
			continue
		}
		_, err := j.isos.GetISO(ctx, snapshot.ISO.ID)
		if err == nil {
			errs = append(errs, j.journal.Discard(ctx, snapshot.ISO.ID))
			continue
		}
		if !errors.Is(err, repository.ErrNotFound) {
			errs = append(errs, err)
			continue
		}
		if err := j.cleanup(ctx, snapshot); err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, j.journal.Discard(ctx, snapshot.ISO.ID))
	}
	return errors.Join(errs...)
}
func (j *Deletion) cleanup(ctx context.Context, snapshot port.DeletionSnapshot) error {
	if err := mediarules.CheckDeletion(snapshot.ISO, snapshot.Sources); err != nil {
		return err
	}
	if err := j.confirmDeleted(ctx, snapshot.ISO.ID); err != nil {
		return err
	}
	unlock := j.locks.Lock(snapshot.ISO.ID, "@import")
	defer unlock()
	for _, source := range snapshot.Sources {
		j.locks.Cancel(snapshot.ISO.ID, source.Role)
		unlock := j.locks.Lock(snapshot.ISO.ID, source.Role)
		defer unlock()
	}
	if err := j.confirmDeleted(ctx, snapshot.ISO.ID); err != nil {
		return err
	}
	err := j.files.Cleanup(ctx, snapshot)
	var fault *port.Fault
	if errors.As(err, &fault) && fault.Kind == port.PublicationConflict {
		return errors.Join(repository.ErrConflict, err)
	}
	return err
}
func (j *Deletion) confirmDeleted(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := j.isos.GetISO(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return repository.ErrConflict
}
