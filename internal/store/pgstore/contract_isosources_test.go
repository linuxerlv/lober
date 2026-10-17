package pgstore_test

import (
	"errors"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestISOSources(t *testing.T) {
	s := pgtest.OpenStore(t)
	ctx := t.Context()
	iso := mustCreateISO(t, s, rockyISO("source test"))
	src := store.Source{ISOID: iso.ID, Kind: store.SourceUpload, State: store.SourcePending, TotalSize: 10}
	saved, err := s.SaveSource(ctx, src, anAudit("iso.source"))
	must(t, err, "register")
	page, err := s.ListAudit(ctx, store.AuditQuery{TargetKind: "iso", TargetID: iso.ID})
	must(t, err, "audits")
	if len(page.Items) != 2 || page.Items[0].Action != "iso.source" {
		t.Fatal(page)
	}
	if saved.Rev != 1 {
		t.Fatal(saved)
	}
	fetched := saved
	fetched.Fetched = 1
	_, err = s.SaveSource(ctx, fetched, store.Audit{})
	wantIs(t, err, store.ErrConflict, "pending progress")
	saved.State = store.SourceFetching
	saved, err = s.SaveSource(ctx, saved, store.Audit{})
	must(t, err, "start")
	for _, r := range []store.Range{{Start: 5, Length: 3}, {Start: 0, Length: 3}, {Start: 2, Length: 4}, {Start: 5, Length: 1}} {
		must(t, s.SaveRange(ctx, iso.ID, "", r.Start, r.Length), "range")
	}
	ranges, err := s.RangesOf(ctx, iso.ID, "")
	must(t, err, "ranges")
	if !reflect.DeepEqual(ranges, []store.Range{{Start: 0, Length: 8}}) {
		t.Fatal(ranges)
	}
	cur, ok, err := s.GetSource(ctx, iso.ID, "")
	must(t, err, "get")
	if !ok || cur.Fetched != 8 {
		t.Fatal(cur)
	}
	_, err = s.SaveSource(ctx, saved, store.Audit{})
	wantIs(t, err, store.ErrConflict, "stale progress rev")
	cur.State = store.SourceCancelled
	cur, err = s.SaveSource(ctx, cur, anAudit("iso.cancel"))
	must(t, err, "cancel")
	stale := cur
	stale.State = store.SourceFetching
	stale.Fetched = 9
	_, err = s.SaveSource(ctx, stale, store.Audit{})
	wantIs(t, err, store.ErrConflict, "progress cannot undo cancellation")
	wantIs(t, s.SaveRange(ctx, iso.ID, "", 8, 2), store.ErrConflict, "range after cancel")
	unfinished, err := s.ListUnfinishedSources(ctx)
	must(t, err, "unfinished")
	if len(unfinished) != 0 {
		t.Fatal(unfinished)
	}
	cur.State = store.SourcePending
	cur, err = s.SaveSource(ctx, cur, anAudit("iso.retry"))
	must(t, err, "retry")
	if cur.Fetched != 0 || cur.Error != "" {
		t.Fatal(cur)
	}
	ranges, err = s.RangesOf(ctx, iso.ID, "")
	must(t, err, "retry ranges")
	if len(ranges) != 0 {
		t.Fatal(ranges)
	}
	for _, c := range []store.Source{
		{ISOID: iso.ID, Role: "netboot", Kind: store.SourceRemote, URL: "https://example.test/boot", State: store.SourcePending},
		{ISOID: iso.ID, Role: "local", Kind: store.SourceLocal, Path: "/srv/iso/image.iso", State: store.SourcePending},
	} {
		_, err = s.SaveSource(ctx, c, anAudit("iso.source"))
		must(t, err, "extra source")
	}
	all, err := s.SourcesOf(ctx, iso.ID)
	must(t, err, "sources")
	if len(all) != 3 || all[0].Role != "" || all[1].Role != "local" || all[2].Role != "netboot" {
		t.Fatal(all)
	}
	must(t, s.DeleteSource(ctx, iso.ID, "local", store.Audit{}), "delete source")
	must(t, s.DeleteSource(ctx, iso.ID, "", store.Audit{}), "delete main source only")
	all, err = s.SourcesOf(ctx, iso.ID)
	must(t, err, "main deleted")
	if len(all) != 1 || all[0].Role != "netboot" {
		t.Fatal(all)
	}
	must(t, s.DeleteISO(ctx, iso.ID, iso.Rev, anAudit("iso.delete")), "cascade")
	all, err = s.SourcesOf(ctx, iso.ID)
	must(t, err, "after delete")
	if len(all) != 0 {
		t.Fatal(all)
	}
}

func TestSourceAuditRollback(t *testing.T) {
	s := pgtest.OpenStore(t)
	ctx := t.Context()
	iso := mustCreateISO(t, s, rockyISO("rollback"))
	src := store.Source{ISOID: iso.ID, Kind: store.SourceUpload, State: store.SourcePending, TotalSize: 10}
	a := anAudit("iso.source")
	a.Actor = pgtest.FaultActor
	if _, err := s.SaveSource(ctx, src, a); err == nil {
		t.Fatal("fault should roll back registration")
	}
	_, ok, err := s.GetSource(ctx, iso.ID, "")
	must(t, err, "get")
	if ok {
		t.Fatal("registration survived failed audit")
	}
	src, err = s.SaveSource(ctx, src, anAudit("iso.source"))
	must(t, err, "register")
	src.State = store.SourceFetching
	src, err = s.SaveSource(ctx, src, store.Audit{})
	must(t, err, "start")
	must(t, s.SaveRange(ctx, iso.ID, "", 0, 5), "range")
	src, _, err = s.GetSource(ctx, iso.ID, "")
	must(t, err, "get")
	src.State = store.SourceFailed
	src, err = s.SaveSource(ctx, src, store.Audit{})
	must(t, err, "fail")
	src.State = store.SourcePending
	if _, err = s.SaveSource(ctx, src, a); err == nil {
		t.Fatal("fault should roll back retry")
	}
	cur, _, err := s.GetSource(ctx, iso.ID, "")
	must(t, err, "get")
	if cur.State != store.SourceFailed || cur.Fetched != 5 {
		t.Fatal(cur)
	}
	ranges, err := s.RangesOf(ctx, iso.ID, "")
	must(t, err, "ranges")
	if len(ranges) != 1 || ranges[0].Length != 5 {
		t.Fatal(ranges)
	}
	if err = s.DeleteSource(ctx, iso.ID, "", a); err == nil {
		t.Fatal("fault should roll back delete")
	}
	_, ok, err = s.GetSource(ctx, iso.ID, "")
	must(t, err, "get")
	if !ok {
		t.Fatal("delete survived failed audit")
	}
}

func TestSourceValidation(t *testing.T) {
	base := store.Source{ISOID: store.NewID(), Kind: store.SourceUpload, State: store.SourcePending, TotalSize: 20}
	for _, mutate := range []func(*store.Source){
		func(s *store.Source) { s.Kind = "unknown" }, func(s *store.Source) { s.URL = "https://example.test" },
		func(s *store.Source) { s.Path = "/tmp/image" }, func(s *store.Source) { s.Kind = store.SourceRemote },
		func(s *store.Source) { s.Kind = store.SourceLocal }, func(s *store.Source) { s.Role = "../escape" },
		func(s *store.Source) { s.State = "bad" }, func(s *store.Source) { s.TotalSize = -1 },
		func(s *store.Source) { s.Fetched = -1 }, func(s *store.Source) { s.Fetched = 21 },
		func(s *store.Source) { s.ExpectedSHA256 = strings.Repeat("A", 64) }, func(s *store.Source) { s.Error = "\x00" },
	} {
		c := base
		mutate(&c)
		if err := store.CheckSource(c); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, r := range []store.Range{{Start: -1, Length: 1}, {Start: 0, Length: 0}, {Start: 1, Length: math.MaxInt64}} {
		if err := store.CheckRange(r.Start, r.Length); !errors.Is(err, store.ErrInvalid) {
			t.Error(r, err)
		}
	}
	states := []store.SourceState{store.SourcePending, store.SourceFetching, store.SourceFetched, store.SourceFailed, store.SourceCancelled}
	for _, from := range states {
		for _, to := range states {
			allowed := from == to || from == store.SourcePending && (to == store.SourceFetching || to == store.SourceFailed || to == store.SourceCancelled) || from == store.SourceFetching && (to == store.SourceFetched || to == store.SourceFailed || to == store.SourceCancelled) || from != store.SourcePending && from != store.SourceFetching && to == store.SourcePending
			err := store.CheckSourceTransition(from, to)
			if (err == nil) != allowed {
				t.Errorf("%s->%s %v", from, to, err)
			}
		}
	}
}
