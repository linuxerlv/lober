package iso

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func TestCleanupDeletedRefusesLiveAndRemovesOwnedFiles(t *testing.T) {
	s, db := importSetup(t)
	i, err := db.CreateISO(t.Context(), model.ISO{Name: "delete managed media", Family: "windows", Series: "windows-server"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, i, "", isotest.Image(importFixture(i.Series)))
	if err := s.Import(t.Context(), i.ID); err != nil {
		t.Fatal(err)
	}
	i, err = db.GetISO(t.Context(), i.ID)
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := db.SourcesOf(t.Context(), i.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DeletionSnapshot{ISO: i, Sources: srcs}
	if err := s.CleanupDeleted(t.Context(), snapshot); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("live cleanup: %v", err)
	}
	paths := []string{s.finalPath(i.ID, ""), s.artifactRoot(i.Series, i.Version, i.Arch), filepath.Join(s.dirs.SMB, string(i.Series), i.Version, string(i.Arch))}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeleteISO(t.Context(), i.ID, i.Rev, importAudit()); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{".chunk-", ".retry-"} {
		p := filepath.Join(s.dirs.Staging, prefix+i.ID+"-residual")
		if err := os.WriteFile(p, []byte("residual"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	for range 2 {
		if err := s.CleanupDeleted(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range paths {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("owned path survived: %s: %v", p, err)
		}
	}
	next, err := db.CreateISO(t.Context(), model.ISO{Name: "replacement", Family: i.Family, Series: i.Series}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, next, "", isotest.Image(importFixture(next.Series)))
	if err := s.Import(t.Context(), next.ID); err != nil {
		t.Fatalf("replacement blocked after cleanup: %v", err)
	}
}

func TestCleanupDeletedCancelsActiveAcquisition(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	started := make(chan struct{})
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.FetchOne(t.Context(), item.ID, "") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch not started")
	}
	sources, err := s.store.SourcesOf(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.DeleteISO(t.Context(), item.ID, item.Rev, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupDeleted(t.Context(), DeletionSnapshot{ISO: item, Sources: sources}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("deleted fetch completed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup failed to cancel acquisition")
	}
	if _, err := os.Stat(s.stagingPath(item.ID, "")); !os.IsNotExist(err) {
		t.Fatalf("deleted partial remained: %v", err)
	}
}

func TestCleanupDeletedPreservesLocalOriginalAndForeignPublication(t *testing.T) {
	s, db := importSetup(t)
	s.dirs.LocalRoots = []string{s.dirs.Staging}
	s.transfer.dirs.LocalRoots = append([]string(nil), s.dirs.LocalRoots...)
	i, err := db.CreateISO(t.Context(), model.ISO{Name: "local ownership", Family: "linux", Series: "ubuntu"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	p := s.finalPath(i.ID, "") // Deliberately overlap managed/allowed directories.
	if err := os.WriteFile(p, isotest.Image(importFixture(i.Series)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), i, "path", p, "", 0, "", importAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), i.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Import(t.Context(), i.ID); err != nil {
		t.Fatal(err)
	}
	i, err = db.GetISO(t.Context(), i.ID)
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := db.SourcesOf(t.Context(), i.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := DeletionSnapshot{ISO: i, Sources: srcs}
	if err := db.DeleteISO(t.Context(), i.ID, i.Rev, importAudit()); err != nil {
		t.Fatal(err)
	}
	root := s.artifactRoot(i.Series, i.Version, i.Arch)
	if err := os.WriteFile(filepath.Join(root, ".lober-owner"), []byte("another ISO\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupDeleted(t.Context(), snapshot); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("foreign ownership: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("foreign publication removed", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lober-owner"), []byte(i.ID+"\n"+i.SHA256+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupDeleted(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("operator original removed", err)
	}
}
