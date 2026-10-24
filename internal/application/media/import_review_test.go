package media_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

type mutateImportContext struct {
	context.Context
	mu     sync.Mutex
	when   func() bool
	mutate func()
	done   bool
}

func (c *mutateImportContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.done && c.when() {
		c.done = true
		c.mutate()
	}
	return c.Context.Err()
}

func (c *mutateImportContext) didMutate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

func TestImportRechecksEverySourceAfterAllCopies(t *testing.T) {
	for _, series := range []catalog.Series{"debian", "windows-server"} {
		t.Run(string(series), func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "changed during import", Family: series.Family(), Series: series}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			files := importFixture(series)
			files["sources/late.txt"] = []byte("original-source")
			importUpload(t, s, i, "", isotest.Image(files))
			role, beforeText, afterText := "", "original-source", "modified-source"
			trigger := filepath.Join(s.dirs.SMB, string(series), "10.0", "x86_64") + ".tmp-" + i.ID
			if series == "debian" {
				role = "netboot"
				importUpload(t, s, i, role, isotest.Image(map[string][]byte{
					".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"),
					"linux":      []byte("original-source"), "initrd.gz": []byte("netboot-initrd"),
				}))
				trigger = s.artifactRoot(series, "13.6.0", "x86_64") + ".tmp-" + i.ID
			}
			p := s.sourcePath(mustSource(t, s, i.ID, role))
			ctx := &mutateImportContext{Context: t.Context(), when: func() bool { _, e := os.Stat(trigger); return e == nil }, mutate: func() {
				raw, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				at := bytes.Index(raw, []byte(beforeText))
				if at < 0 {
					t.Fatal("fixture payload missing")
				}
				f, e := os.OpenFile(p, os.O_WRONLY, 0)
				if e != nil {
					t.Fatal(e)
				}
				_, e = f.WriteAt([]byte(afterText), int64(at))
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
			}}
			err = s.Import(ctx, i.ID)
			if !ctx.didMutate() {
				t.Fatal("mutation did not run")
			}
			var problem model.Problem
			if !errors.As(err, &problem) || problem.Code != "iso_content_mismatch" {
				t.Fatalf("changed media accepted: %v", err)
			}
			got, e := db.GetISO(t.Context(), i.ID)
			if e != nil || got.State != model.ISOFailed {
				t.Fatal(got, e)
			}
			if _, e := os.Stat(s.artifactRoot(got.Series, got.Version, got.Arch)); !os.IsNotExist(e) {
				t.Fatalf("published artifacts survived: %v", e)
			}
			if _, e := os.Stat(filepath.Join(s.dirs.SMB, string(got.Series), got.Version, string(got.Arch))); !os.IsNotExist(e) {
				t.Fatalf("published SMB survived: %v", e)
			}
		})
	}
}

type lostImportAckStore struct {
	repository.Store
	lost, unreadable bool
}

func (s *lostImportAckStore) UpdateISO(ctx context.Context, i model.ISO) (model.ISO, error) {
	got, err := s.Store.UpdateISO(ctx, i)
	if err == nil && i.State == model.ISOReady {
		s.lost = true
		return model.ISO{}, errors.New("lost commit acknowledgment")
	}
	return got, err
}
func (s *lostImportAckStore) GetISO(ctx context.Context, id string) (model.ISO, error) {
	if s.lost && s.unreadable {
		return model.ISO{}, errors.New("database unavailable")
	}
	return s.Store.GetISO(ctx, id)
}
func TestImportPreservesPublicationAfterLostCommitAcknowledgment(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(map[bool]string{false: "reconciled", true: "uncertain"}[unreadable], func(t *testing.T) {
			s, db := importSetup(t)
			i, err := db.CreateISO(t.Context(), model.ISO{Name: "lost commit ack", Family: "windows", Series: "windows-server"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			importUpload(t, s, i, "", isotest.Image(importFixture(i.Series)))
			s.store = &lostImportAckStore{Store: db, unreadable: unreadable}
			err = s.Import(t.Context(), i.ID)
			if (err != nil) != unreadable {
				t.Fatalf("reconciliation result: %v", err)
			}
			got, err := db.GetISO(t.Context(), i.ID)
			if err != nil || got.State != model.ISOReady {
				t.Fatal(got, err)
			}
			for _, p := range []string{s.artifactRoot(got.Series, got.Version, got.Arch), filepath.Join(s.dirs.SMB, string(got.Series), got.Version, string(got.Arch))} {
				if _, err := os.Stat(p); err != nil {
					t.Fatalf("committed publication removed: %v", err)
				}
			}
		})
	}
}

func TestImportMissingExtraCanRegisterAndReimport(t *testing.T) {
	s, db := importSetup(t)
	i, err := db.CreateISO(t.Context(), model.ISO{Name: "recover missing netboot", Family: "linux", Series: "debian"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, i, "", isotest.Image(importFixture("debian")))
	if err := s.Import(t.Context(), i.ID); err == nil {
		t.Fatal("missing extra accepted")
	}
	importUpload(t, s, i, "netboot", isotest.Image(map[string][]byte{
		".disk/info": []byte("Debian GNU/Linux 13.6.0 amd64 NETBOOT"), "linux": []byte("kernel"), "initrd.gz": []byte("initrd"),
	}))
	if err := s.Reimport(t.Context(), i.ID, importAudit()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetISO(t.Context(), i.ID)
	if err != nil || got.State != model.ISOReady {
		t.Fatal(got, err)
	}
	if src := mustSource(t, s, i.ID, ""); src.Error != "" {
		t.Fatalf("stale import failure retained: %q", src.Error)
	}
}

func TestImportChangedFetchedSizeFailsDurably(t *testing.T) {
	s, db := importSetup(t)
	i, err := db.CreateISO(t.Context(), model.ISO{Name: "truncated media", Family: "linux", Series: "ubuntu"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	importUpload(t, s, i, "", isotest.Image(importFixture(i.Series)))
	if err := os.Truncate(s.sourcePath(mustSource(t, s, i.ID, "")), 123); err != nil {
		t.Fatal(err)
	}
	err = s.Import(t.Context(), i.ID)
	var p model.Problem
	if !errors.As(err, &p) || p.Code != "iso_content_mismatch" {
		t.Fatalf("unexpected failure: %v", err)
	}
	got, err := db.GetISO(t.Context(), i.ID)
	if err != nil || got.State != model.ISOFailed || mustSource(t, s, i.ID, "").Error == "" {
		t.Fatal(got, err)
	}
}
