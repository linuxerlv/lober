package iso

import (
	"errors"
	"os"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/iso/isotest"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func TestRequestReimportPersistsRecoverableWorkBeforeExtraction(t *testing.T) {
	s, db := importSetup(t)
	create := func(name string) model.ISO {
		t.Helper()
		item, err := db.CreateISO(t.Context(), model.ISO{Name: name, Family: "linux", Series: "rocky"}, importAudit())
		if err != nil {
			t.Fatal(err)
		}
		importUpload(t, s, item, "", isotest.Image(importFixture("rocky")))
		item.State = model.ISOFailed
		item, err = db.UpdateISO(t.Context(), item)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	item := create("queued-reimport")
	audit := repository.Audit{Actor: "operator", Action: "iso.reimport", At: 1790000000}
	if err := s.RequestReimport(t.Context(), item.ID, audit); err != nil {
		t.Fatal(err)
	}
	queued, err := db.GetISO(t.Context(), item.ID)
	if err != nil || queued.State != model.ISOAcquiring || queued.Rev != item.Rev+1 {
		t.Fatal(queued, err)
	}
	if _, err := os.Stat(s.artifactRoot("rocky", "9.6", "x86_64")); !os.IsNotExist(err) {
		t.Fatal("request performed extraction", err)
	}
	restarted := New(db, s.dirs)
	work, err := restarted.Recover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range work {
		if w.ISOID == item.ID && w.Kind == ResumeImport {
			found = true
		}
	}
	if !found {
		t.Fatal("durable request missing after restart", work)
	}
	entries, err := db.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: item.ID, Action: "iso.reimport"})
	if err != nil || len(entries.Items) != 1 || entries.Items[0].Actor != "operator" {
		t.Fatal(entries, err)
	}
	if err := restarted.Import(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := db.GetISO(t.Context(), item.ID)
	if err != nil || ready.State != model.ISOReady {
		t.Fatal(ready, err)
	}
	rejected := create("request-audit-failure")
	srcBefore := mustSource(t, s, rejected.ID, "")
	audit.Actor = pgtest.FaultActor
	if err := s.RequestReimport(t.Context(), rejected.ID, audit); err == nil {
		t.Fatal("failed audit accepted")
	}
	back, err := db.GetISO(t.Context(), rejected.ID)
	srcAfter := mustSource(t, s, rejected.ID, "")
	if err != nil || back.State != model.ISOFailed || back.Rev != rejected.Rev || srcBefore.Rev != srcAfter.Rev {
		t.Fatal("failed audit changed work", back, err)
	}
	audit.Actor = "operator"
	if err := s.RequestReimport(t.Context(), ready.ID, audit); !errors.Is(err, repository.ErrConflict) {
		t.Fatal("ready reimport accepted", err)
	}
}
