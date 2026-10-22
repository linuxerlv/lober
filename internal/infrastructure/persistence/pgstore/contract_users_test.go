package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"reflect"
	"slices"
	"testing"

	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

func aUser(name string) model.User {
	return model.User{Username: name, DisplayName: "显示名", Role: model.RoleOperator, Password: "persisted-hash", CreatedAt: at0}
}

func Test契约用户(t *testing.T) {
	runCases(t, []testCase{
		{"create readback and audit", userCreateReadBack},
		{"zero fields and restore", userUpdateToZero},
		{"identity immutable", userUpdateKeepsUsername},
		{"revision missing and notfound", userUpdateConflict},
		{"invalid writes unchanged", userCreateInvalid},
		{"duplicate username", userCreateDuplicate},
		{"byte order pagination", userListPaging},
		{"default limit", userListDefaultLimit},
		{"disabled remains readable", userDisabledReadBack},
		{"audit rollback", userAuditRollback},
		{"concurrent revision", userConcurrentUpdates},
		{"concurrent create", userConcurrentCreate},
		{"invalid query and cancelled context", userQueryInvalid},
	})
}
func userCreateReadBack(t *testing.T, s *pgstore.Store) {
	in := aUser("alice")
	got, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "create")
	in.Rev = 1
	wantEqual(t, got, in, "returned user")
	back, err := s.GetUser(t.Context(), "alice")
	must(t, err, "read")
	wantEqual(t, back, in, "readback")
	entries := auditEntries(t, s)
	if len(entries) != 1 || entries[0].TargetKind != "user" || entries[0].TargetID != "alice" || entries[0].Actor != "alice" || entries[0].Action != "user.create" {
		t.Fatal(entries)
	}
}
func userUpdateToZero(t *testing.T, s *pgstore.Store) {
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "create")
	u.DisplayName = ""
	u.Role = model.RoleViewer
	u.Password = "replacement-hash"
	u.Disabled = true
	u.CreatedAt++
	got, err := s.UpdateUser(t.Context(), u, anAudit("user.update"))
	must(t, err, "disable")
	want := u
	want.CreatedAt = at0
	want.Rev = 2
	wantEqual(t, got, want, "updated fields")
	got.Disabled = false
	restored, err := s.UpdateUser(t.Context(), got, anAudit("user.update"))
	must(t, err, "restore")
	got.Rev = 3
	wantEqual(t, restored, got, "restored false")
}
func userUpdateKeepsUsername(t *testing.T, s *pgstore.Store) {
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "create")
	before := snapshot(t, s)
	u.Username = "bob"
	_, err = s.UpdateUser(t.Context(), u, anAudit("user.update"))
	wantIs(t, err, repository.ErrNotFound, "rename")
	wantUnchanged(t, s, before, "rename")
}
func userUpdateConflict(t *testing.T, s *pgstore.Store) {
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "create")
	before := snapshot(t, s)
	u.Rev = 99
	_, err = s.UpdateUser(t.Context(), u, anAudit("user.update"))
	wantIs(t, err, repository.ErrConflict, "stale")
	wantUnchanged(t, s, before, "stale")
	u.Rev = 0
	_, err = s.UpdateUser(t.Context(), u, anAudit("user.update"))
	wantIs(t, err, repository.ErrInvalid, "zero revision")
	u = aUser("nobody")
	u.Rev = 1
	_, err = s.UpdateUser(t.Context(), u, anAudit("user.update"))
	wantIs(t, err, repository.ErrNotFound, "missing")
}
func userCreateInvalid(t *testing.T, s *pgstore.Store) {
	for _, modify := range []func(*model.User){func(u *model.User) { u.Username = "Bad" }, func(u *model.User) { u.Role = "root" }, func(u *model.User) { u.DisplayName = "bad\x00" }, func(u *model.User) { u.Password = "" }, func(u *model.User) { u.CreatedAt = 0 }} {
		u := aUser("alice")
		modify(&u)
		before := snapshot(t, s)
		_, err := s.CreateUser(t.Context(), u, anAudit("user.create"))
		wantIs(t, err, repository.ErrInvalid, "invalid")
		wantUnchanged(t, s, before, "invalid")
	}
	before := snapshot(t, s)
	_, err := s.CreateUser(t.Context(), aUser("alice"), repository.Audit{})
	wantIs(t, err, repository.ErrAuditRequired, "missing audit")
	wantUnchanged(t, s, before, "missing audit")
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "seed")
	before = snapshot(t, s)
	u.Disabled = true
	_, err = s.UpdateUser(t.Context(), u, repository.Audit{})
	wantIs(t, err, repository.ErrAuditRequired, "update audit")
	wantUnchanged(t, s, before, "update audit")
}
func userCreateDuplicate(t *testing.T, s *pgstore.Store) {
	_, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "first")
	before := snapshot(t, s)
	_, err = s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	wantDuplicate(t, err, "username", "alice", "duplicate")
	wantUnchanged(t, s, before, "duplicate")
}
func userPages(t *testing.T, s *pgstore.Store, q repository.UserQuery) [][]model.User {
	t.Helper()
	return pagesFrom(t, q.Cursor, func(cursor string) (repository.Page[model.User], error) {
		q.Cursor = cursor
		return s.ListUsers(t.Context(), q)
	})
}
func userListPaging(t *testing.T, s *pgstore.Store) {
	for _, name := range []string{"bob", "a_", "a0", "alice", "carol"} {
		_, err := s.CreateUser(t.Context(), aUser(name), anAudit("user.create"))
		must(t, err, "seed")
	}
	var names []string
	for _, u := range slices.Concat(userPages(t, s, repository.UserQuery{Limit: 2})...) {
		names = append(names, u.Username)
	}
	if !reflect.DeepEqual(names, []string{"a0", "a_", "alice", "bob", "carol"}) {
		t.Fatal(names)
	}
	all, err := s.ListUsers(t.Context(), repository.UserQuery{Limit: 500})
	must(t, err, "all")
	wantEqual(t, slices.Concat(userPages(t, s, repository.UserQuery{Limit: 2})...), all.Items, "page equivalence")
}
func userListDefaultLimit(t *testing.T, s *pgstore.Store) {
	for i := 0; i < 101; i++ {
		_, err := s.CreateUser(t.Context(), aUser(fmt.Sprintf("u%03d", i)), anAudit("user.create"))
		must(t, err, "seed")
	}
	first, err := s.ListUsers(t.Context(), repository.UserQuery{})
	must(t, err, "first")
	if len(first.Items) != 100 || first.Next == "" {
		t.Fatal(len(first.Items), first.Next)
	}
	last, err := s.ListUsers(t.Context(), repository.UserQuery{Cursor: first.Next})
	must(t, err, "last")
	if len(last.Items) != 1 || last.Next != "" {
		t.Fatal(last)
	}
}
func userDisabledReadBack(t *testing.T, s *pgstore.Store) {
	u := aUser("alice")
	u.Disabled = true
	created, err := s.CreateUser(t.Context(), u, anAudit("user.create"))
	must(t, err, "create")
	got, err := s.GetUser(t.Context(), u.Username)
	must(t, err, "read")
	wantEqual(t, got, created, "disabled read")
}
func userAuditRollback(t *testing.T, s *pgstore.Store) {
	a := anAudit("user.create")
	a.Actor = pgtest.FaultActor
	before := snapshot(t, s)
	_, err := s.CreateUser(t.Context(), aUser("alice"), a)
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	wantUnchanged(t, s, before, "create rollback")
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "seed")
	before = snapshot(t, s)
	u.Disabled = true
	a.Action = "user.update"
	_, err = s.UpdateUser(t.Context(), u, a)
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	wantUnchanged(t, s, before, "update rollback")
}
func userConcurrentUpdates(t *testing.T, s *pgstore.Store) {
	u, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
	must(t, err, "seed")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, label := range []string{"first", "second"} {
		go func() {
			<-start
			copy := u
			copy.DisplayName = label
			_, err := s.UpdateUser(t.Context(), copy, anAudit("user.update"))
			results <- err
		}()
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, repository.ErrConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	got, err := s.GetUser(t.Context(), u.Username)
	must(t, err, "read")
	if success != 1 || conflict != 1 || got.Rev != 2 || len(auditEntries(t, s)) != 2 {
		t.Fatal(success, conflict, got)
	}
}
func userQueryInvalid(t *testing.T, s *pgstore.Store) {
	for _, q := range []repository.UserQuery{{Limit: -1}, {Limit: 501}, {Cursor: "not-a-cursor"}, {Cursor: repository.EncodeCursor("hosts", "alice")}, {Cursor: repository.EncodeCursor("users", "a\x00")}} {
		_, err := s.ListUsers(t.Context(), q)
		wantIs(t, err, repository.ErrInvalid, "query")
	}
	page, err := s.ListUsers(t.Context(), repository.UserQuery{})
	must(t, err, "empty")
	if page.Items == nil {
		t.Fatal("nil page")
	}
	_, err = s.GetUser(t.Context(), "bad\x00")
	wantIs(t, err, repository.ErrNotFound, "unstorable key")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.ListUsers(ctx, repository.UserQuery{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = s.GetUser(ctx, "alice")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = s.CreateUser(ctx, aUser("alice"), anAudit("user.create"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func userConcurrentCreate(t *testing.T, s *pgstore.Store) {
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := s.CreateUser(t.Context(), aUser("alice"), anAudit("user.create"))
			results <- err
		}()
	}
	close(start)
	success, duplicate := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, repository.ErrDuplicate):
			duplicate++
		default:
			t.Fatal(err)
		}
	}
	page, err := s.ListUsers(t.Context(), repository.UserQuery{})
	must(t, err, "read")
	if success != 1 || duplicate != 1 || len(page.Items) != 1 || len(auditEntries(t, s)) != 1 {
		t.Fatal(success, duplicate, page)
	}
}
