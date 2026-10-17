package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lober/lober/internal/iso"
	"github.com/lober/lober/internal/store"
)

// Registration and snapshot capture share a gate. Source identity/path cannot
// change after registration, so fetch counters need not block deletion.
type registrationGate struct{ sync.RWMutex }
type deletionJournal struct {
	store  store.Store
	source *iso.Sources
	gate   *registrationGate
	dir    string
	logger *slog.Logger
	mu     sync.Mutex
}

func newDeletionJournal(st store.Store, src *iso.Sources, gate *registrationGate, cfg config, logger *slog.Logger) (*deletionJournal, error) {
	dir := filepath.Join(cfg.ISO.Staging, "deletions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &deletionJournal{store: st, source: src, gate: gate, dir: dir, logger: logger}, nil
}
func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (j *deletionJournal) persist(s iso.DeletionSnapshot) error {
	if err := store.CheckID(s.ISO.ID); err != nil {
		return err
	}
	if s.Sources == nil {
		return store.ErrInvalid
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := "." + s.ISO.ID + ".tmp"
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		if err = root.Remove(name); err != nil {
			return err
		}
		f, err = root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = root.Rename(name, s.ISO.ID+".json"); err != nil {
		return err
	}
	return syncDirectory(j.dir)
}
func (j *deletionJournal) discard(id string) error {
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(id + ".json"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(j.dir)
}
func (j *deletionJournal) Delete(ctx context.Context, id string, rev int64, a store.Audit) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.gate.Lock()
	item, err := j.store.GetISO(ctx, id)
	if err != nil {
		j.gate.Unlock()
		return err
	}
	if item.Rev != rev {
		j.gate.Unlock()
		return store.ErrConflict
	}
	sources, err := j.store.SourcesOf(ctx, id)
	if err != nil {
		j.gate.Unlock()
		return err
	}
	if sources == nil {
		sources = []store.Source{}
	}
	snapshot := iso.DeletionSnapshot{ISO: item, Sources: sources}
	if err = j.persist(snapshot); err != nil {
		j.gate.Unlock()
		return err
	}
	err = j.store.DeleteISO(ctx, id, rev, a)
	j.gate.Unlock()
	if err != nil {
		// A failed/ambiguous commit keeps its durable snapshot unless existence is
		// confirmed. Never remove bytes while the database still owns the record.
		if _, readErr := j.store.GetISO(ctx, id); readErr == nil {
			if discardErr := j.discard(id); discardErr != nil {
				j.logger.Warn("删除快照清理等待重试", "iso", id)
			}
		}
		return err
	}
	if err = j.source.CleanupDeleted(ctx, snapshot); err != nil {
		j.logger.Warn("ISO 字节清理等待重试", "iso", id)
		return nil
	}
	if err = j.discard(id); err != nil {
		j.logger.Warn("删除快照清理等待重试", "iso", id)
	}
	return nil
}
func (j *deletionJournal) RetryAll(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	root, err := os.OpenRoot(j.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if err := store.CheckID(id); err != nil {
			errs = append(errs, err)
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.Mode().IsRegular() {
			errs = append(errs, store.ErrInvalid)
			continue
		}
		f, err := root.Open(entry.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var snapshot iso.DeletionSnapshot
		decoder := json.NewDecoder(f)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&snapshot)
		var extra any
		if err == nil {
			if endErr := decoder.Decode(&extra); endErr != io.EOF {
				err = store.ErrInvalid
			}
		}
		f.Close()
		if err != nil || snapshot.ISO.ID != id || snapshot.Sources == nil {
			errs = append(errs, fmt.Errorf("invalid deletion snapshot %s", id))
			continue
		}
		_, err = j.store.GetISO(ctx, id)
		if err == nil {
			if err = j.discard(id); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			errs = append(errs, err)
			continue
		}
		if err = j.source.CleanupDeleted(ctx, snapshot); err != nil {
			errs = append(errs, err)
			continue
		}
		if err = j.discard(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
