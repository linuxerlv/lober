package iso

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/store"
)

func TestRecoverFetchAndImportNoWorkStarted(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 4, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	todo, err := s.Recover(t.Context())
	if err != nil || len(todo) != 1 || todo[0].Kind != ResumeFetch || todo[0].ISOID != item.ID {
		t.Fatalf("%+v %v", todo, err)
	}
	if src := mustSource(t, s, item.ID, ""); src.State != store.SourcePending {
		t.Fatalf("Recover ran work: %+v", src)
	}
	if err := s.Cancel(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	todo, err = s.Recover(t.Context())
	if err != nil || len(todo) != 0 {
		t.Fatalf("cancelled recovered: %+v %v", todo, err)
	}
	if err := s.Retry(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	todo, err = s.Recover(t.Context())
	if err != nil || len(todo) != 1 || todo[0].Kind != ResumeImport {
		t.Fatalf("fetched not scheduled: %+v %v", todo, err)
	}
}

func TestRecoverDebianNeedsExtraAndReimportRejectsAcquiring(t *testing.T) {
	s, item := sourceFixture(t, "debian")
	if err := s.Register(t.Context(), item, "upload", "", "", 4, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	todo, err := s.Recover(t.Context())
	if err != nil || len(todo) != 0 {
		t.Fatalf("extra missing: %+v %v", todo, err)
	}
	if err := s.Reimport(t.Context(), item.ID, sourceAudit()); err != store.ErrConflict {
		t.Fatalf("reimport acquiring: %v", err)
	}
}
