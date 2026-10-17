package iso

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func TestUploadOutOfOrderDuplicateMissingAndAutomaticFinish(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	b := []byte("abcdefghijkl")
	if err := s.Register(t.Context(), item, "upload", "", "", int64(len(b)), sourceDigest(b), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []struct {
		offset int64
		data   string
	}{{4, "efgh"}, {4, "efgh"}, {6, "ghij"}} {
		if err := s.Upload(t.Context(), item.ID, "", chunk.offset, strings.NewReader(chunk.data), store.Audit{}); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := s.Progress(t.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := progress.Sources[0]; got.Fetched != 6 || !reflect.DeepEqual(got.Missing, []store.Range{{Start: 0, Length: 4}, {Start: 10, Length: 2}}) {
		t.Fatalf("%+v", got)
	}
	// Refresh reconstructs progress from PostgreSQL, not instance memory.
	s = New(s.store, s.dirs)
	for _, chunk := range []struct {
		offset int64
		data   string
	}{{10, "kl"}, {0, "abcd"}} {
		if err := s.Upload(t.Context(), item.ID, "", chunk.offset, strings.NewReader(chunk.data), store.Audit{}); err != nil {
			t.Fatal(err)
		}
	}
	src := mustSource(t, s, item.ID, "")
	if src.State != store.SourceFetched || src.Fetched != 12 {
		t.Fatalf("%+v", src)
	}
	got, err := os.ReadFile(s.sourcePath(src))
	if err != nil || !bytes.Equal(got, b) {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := os.Stat(s.stagingPath(item.ID, "")); !os.IsNotExist(err) {
		t.Fatalf("part remains: %v", err)
	}
}

func TestUploadRejectedDoesNotOverwriteConfirmedBytes(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 8, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []struct {
		offset int64
		data   string
	}{{-1, "x"}, {7, "overflow"}, {1, "X"}, {0, "abcdXXXXXXXX"}, {4, ""}} {
		if err := s.Upload(t.Context(), item.ID, "", chunk.offset, strings.NewReader(chunk.data), store.Audit{}); err == nil {
			t.Fatalf("accepted %+v", chunk)
		}
		src := mustSource(t, s, item.ID, "")
		if src.Fetched != 4 {
			t.Fatalf("rejected chunk changes progress: %+v", src)
		}
		got, err := os.ReadFile(s.stagingPath(item.ID, ""))
		if err != nil || string(got) != "abcd" {
			t.Fatalf("confirmed bytes changed: %q %v", got, err)
		}
	}
}

func TestCancelRetryAllSourcesAndFetchedPreserved(t *testing.T) {
	s, item := sourceFixture(t, "debian")
	for _, role := range []string{"", "netboot"} {
		if err := s.Register(t.Context(), item, "upload", "", role, 4, "", sourceAudit()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("ab"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"", "netboot"} {
		if src := mustSource(t, s, item.ID, role); src.State != store.SourceCancelled {
			t.Fatalf("%+v", src)
		}
	}
	if current, err := s.store.GetISO(t.Context(), item.ID); err != nil || current.State != model.ISOAcquiring {
		t.Fatalf("ISO cancellation: %+v %v", current, err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 2, strings.NewReader("cd"), store.Audit{}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("upload after cancel: %v", err)
	}
	if err := s.Retry(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"", "netboot"} {
		src := mustSource(t, s, item.ID, role)
		ranges, err := s.store.RangesOf(t.Context(), item.ID, role)
		if src.State != store.SourcePending || src.Fetched != 0 || err != nil || len(ranges) != 0 {
			t.Fatalf("retry: %+v %+v %v", src, ranges, err)
		}
	}
	if _, err := os.Stat(s.stagingPath(item.ID, "")); !os.IsNotExist(err) {
		t.Fatalf("retry preserves partial: %v", err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Retry(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if src := mustSource(t, s, item.ID, ""); src.State != store.SourceFetched {
		t.Fatalf("retry resets fetched: %+v", src)
	}
}

func TestUploadCancelledContextDoesNotWrite(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 4, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Upload(ctx, item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if src := mustSource(t, s, item.ID, ""); src.Fetched != 0 {
		t.Fatalf("cancel changed progress: %+v", src)
	}
}

func TestUploadPublicationCrashRestoresFinalAsPartial(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 4, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("ab"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	// Model a process dying between rename and SaveSource(fetched).
	if err := os.WriteFile(s.stagingPath(item.ID, ""), []byte("abcd"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveRange(t.Context(), item.ID, "", 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(s.stagingPath(item.ID, ""), s.sourcePath(mustSource(t, s, item.ID, ""))); err != nil {
		t.Fatal(err)
	}
	s = New(s.store, s.dirs)
	if err := s.Upload(t.Context(), item.ID, "", 2, strings.NewReader("cd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if src := mustSource(t, s, item.ID, ""); src.State != store.SourceFetched {
		t.Fatalf("%+v", src)
	}
	if got, err := os.ReadFile(s.sourcePath(mustSource(t, s, item.ID, ""))); err != nil || string(got) != "abcd" {
		t.Fatalf("publication recovery content: %q %v", got, err)
	}
}

func TestUploadTruncatedConfirmedFileRejectsOverlap(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 8, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(s.stagingPath(item.ID, ""), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("XY"), store.Audit{}); err == nil {
		t.Fatal("overlap accepted despite missing confirmed bytes")
	}
}

func TestUploadFinalChunkReplayedAfterLostAcknowledgement(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 4, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 2, strings.NewReader("cd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 2, strings.NewReader("XX"), store.Audit{}); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if got, err := os.ReadFile(s.sourcePath(mustSource(t, s, item.ID, ""))); err != nil || string(got) != "abcd" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRetryAuditFailurePreservesPartialFileAndRanges(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	if err := s.Register(t.Context(), item, "upload", "", "", 8, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, strings.NewReader("abcd"), store.Audit{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	a := sourceAudit()
	a.Actor = pgtest.FaultActor
	if err := s.Retry(t.Context(), item.ID, a); err == nil {
		t.Fatal("audit injection did not fail")
	}
	if src := mustSource(t, s, item.ID, ""); src.State != store.SourceCancelled || src.Fetched != 4 {
		t.Fatalf("audit rollback: %+v", src)
	}
	if got, err := os.ReadFile(s.stagingPath(item.ID, "")); err != nil || string(got) != "abcd" {
		t.Fatalf("retry lost confirmed bytes despite rollback: %q %v", got, err)
	}
}
