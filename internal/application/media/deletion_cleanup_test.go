package media

import (
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func Test清理拒绝仍有记录且不触碰字节(t *testing.T) {
	db := pgtest.Open(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "live", Family: "linux", Series: "rocky"}, repository.Audit{Actor: "operator", Action: "iso.create", At: 1})
	if err != nil {
		t.Fatal(err)
	}
	dirs := iso.Dirs{Staging: t.TempDir(), Artifacts: t.TempDir(), SMB: t.TempDir()}
	path := filepath.Join(dirs.Staging, item.ID)
	if err := os.WriteFile(path, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	journal, err := iso.NewDeletionJournal(dirs.Staging)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeletion(DeletionDeps{ISOs: db, Sources: db, Journal: journal, Files: iso.NewDeletedFiles(dirs), Locks: iso.NewSourceLocks(), Gate: iso.NewRegistrationGate(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := d.cleanup(t.Context(), port.DeletionSnapshot{ISO: item, Sources: []model.Source{}}); !errors.Is(err, repository.ErrConflict) {
		t.Fatal("仍有记录却允许清理", err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "sentinel" {
		t.Fatal(err)
	}
}
