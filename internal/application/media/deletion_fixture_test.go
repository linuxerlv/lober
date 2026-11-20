package media_test

import (
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

type deletionFixture struct {
	Store    repository.Store
	Sources  *appmedia.Sources
	Control  port.SourceControl
	Deletion *appmedia.Deletion
	Journal  *iso.DeletionJournal
	Locks    *iso.SourceLocks
	Gate     *iso.RegistrationGate
	Dirs     iso.Dirs
}
type deletionConfig struct {
	ISO iso.Dirs
	PG  struct{ DSN string }
}
type silentWake struct{}

func (silentWake) Wake() {}
func assembledDeletion(t *testing.T) (*deletionFixture, deletionConfig) {
	t.Helper()
	var cfg deletionConfig
	cfg.PG.DSN = pgtest.Schema(t)
	db := pgtest.OpenAt(t, cfg.PG.DSN)
	root := t.TempDir()
	cfg.ISO = iso.Dirs{Staging: filepath.Join(root, "staging"), Artifacts: filepath.Join(root, "artifacts"), SMB: filepath.Join(root, "smb"), Tools: filepath.Join(root, "tools"), LocalRoots: []string{root}}
	for _, d := range []string{cfg.ISO.Staging, cfg.ISO.Artifacts, cfg.ISO.SMB} {
		if err := os.MkdirAll(d, 0750); err != nil {
			t.Fatal(err)
		}
	}
	a := &deletionFixture{Store: db, Dirs: cfg.ISO, Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate()}
	var err error
	a.Journal, err = iso.NewDeletionJournal(cfg.ISO.Staging)
	if err != nil {
		t.Fatal(err)
	}
	a.Sources = appmedia.NewSources(appmedia.SourceDeps{ISOs: db, Sources: db, Transfer: iso.NewTransfer(cfg.ISO), Import: iso.NewImportFiles(cfg.ISO), Locks: a.Locks, Gate: a.Gate})
	a.Control = appmedia.NewControl(a.Sources, silentWake{})
	a.Deletion = a.newDeletion(a.Journal)
	return a, cfg
}
func (a *deletionFixture) newDeletion(j port.DeletionJournal) *appmedia.Deletion {
	return appmedia.NewDeletion(appmedia.DeletionDeps{ISOs: a.Store, Sources: a.Store, Journal: j, Files: iso.NewDeletedFiles(a.Dirs), Locks: a.Locks, Gate: a.Gate, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}
func assemblyAudit(action string) repository.Audit {
	return repository.Audit{Actor: "operator", Action: action, At: 1790000000}
}
func rockyImage() []byte { return isotest.Image(importFixture("rocky")) }
