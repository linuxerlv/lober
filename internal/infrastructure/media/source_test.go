package iso

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/catalog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func sourceAudit() repository.Audit {
	return repository.Audit{Actor: "source-test", Action: "iso.source", At: 1}
}
func sourceFixture(t *testing.T, series string) (*acquisitionFixture, model.ISO) {
	t.Helper()
	db := pgtest.Open(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "source", Family: "linux", Series: catalog.Series(series)}, sourceAudit())
	if err != nil {
		t.Fatal(err)
	}
	dirs := Dirs{Staging: t.TempDir(), Artifacts: t.TempDir(), SMB: t.TempDir(), Tools: t.TempDir(), LocalRoots: []string{t.TempDir()}}
	return newAcquisitionFixture(db, dirs), item
}

func mustSource(t *testing.T, s *acquisitionFixture, id, role string) model.Source {
	t.Helper()
	src, ok, err := s.store.GetSource(t.Context(), id, role)
	if err != nil || !ok {
		t.Fatalf("source exists=%v: %v", ok, err)
	}
	return src
}
func mustSourceSave(t *testing.T, s *acquisitionFixture, src model.Source) model.Source {
	t.Helper()
	src, err := s.store.SaveSource(t.Context(), src, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	return src
}
func sourceDigest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func TestLocalPathSegmentsAndSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{root, filepath.Join(root, "a", "b")} {
		if err := CheckLocalPath([]string{root}, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{root + "2", filepath.Join(root, "..", "outside")} {
		if CheckLocalPath([]string{root}, p) == nil {
			t.Fatalf("accepted outside path %s", p)
		}
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err == nil {
		if CheckLocalPath([]string{root}, filepath.Join(link, "file")) == nil {
			t.Fatal("symlink escapes root")
		}
	}
}

func TestLocalFetchOriginalAndDigest(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	b := []byte("installation medium")
	p := filepath.Join(s.dirs.LocalRoots[0], "medium.iso")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), item, "path", p, "", 0, sourceDigest(b), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	if src.State != model.SourceFetched || src.Fetched != int64(len(b)) {
		t.Fatalf("%+v", src)
	}
	got, err := os.ReadFile(s.sourcePath(src))
	if err != nil || string(got) != string(b) {
		t.Fatalf("original path: %q %v", got, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("local medium removed:", err)
	}
}

func TestSourceDigestMismatch(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 3, sourceDigest([]byte("bad")), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abc"), repository.Audit{})
	if err == nil {
		t.Fatal("digest mismatch accepted")
	}
	var problem *repository.ProblemsError
	if !errors.As(err, &problem) || len(problem.Problems) != 1 || problem.Problems[0].Subject != (model.Subject{Kind: "iso", ID: item.ID, Path: "source"}) {
		t.Fatalf("source problem contract: %v", err)
	}
	if src := mustSource(t, s, item.ID, ""); src.State != model.SourceFailed {
		t.Fatalf("%+v", src)
	}
	if _, err := os.Stat(s.sourcePath(mustSource(t, s, item.ID, ""))); !os.IsNotExist(err) {
		t.Fatalf("published mismatching content: %v", err)
	}
}

func TestSourceRegisterChecksPersistedStateRoleAudit(t *testing.T) {
	s, item := sourceFixture(t, "debian")
	if err := s.Register(t.Context(), item, "upload", "", "../bad", 1, "", sourceAudit()); err == nil {
		t.Fatal("unsafe role accepted")
	}
	if err := s.Register(t.Context(), item, "upload", "", "", 1, "", repository.Audit{}); err != repository.ErrAuditRequired {
		t.Fatalf("audit: %v", err)
	}
	for _, role := range []string{"", "netboot"} {
		if err := s.Register(t.Context(), item, "upload", "", role, 1, "", sourceAudit()); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := s.store.SourcesOf(t.Context(), item.ID)
	if err != nil || len(sources) != 2 || sources[0].Role != "" || sources[1].Role != "netboot" {
		t.Fatalf("%+v %v", sources, err)
	}
	item.State = model.ISOImporting
	if _, err := s.store.UpdateISO(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), item, "upload", "", "", 1, "", sourceAudit()); err != repository.ErrConflict {
		t.Fatalf("state: %v", err)
	}
}

func TestSourceFreeSpaceAndMissingDirectory(t *testing.T) {
	s, _ := sourceFixture(t, "rocky")
	stage, art, err := s.FreeSpace(t.Context())
	if err != nil || stage <= 0 || art <= 0 {
		t.Fatalf("%d %d %v", stage, art, err)
	}
	s.transfer.dirs.Artifacts = filepath.Join(t.TempDir(), "absent")
	if _, _, err := s.FreeSpace(t.Context()); err == nil || !strings.Contains(err.Error(), s.transfer.dirs.Artifacts) {
		t.Fatalf("missing artifacts: %v", err)
	}
}
