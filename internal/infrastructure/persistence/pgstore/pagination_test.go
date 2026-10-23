package pgstore_test

import (
	"errors"
	"fmt"
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"slices"
	"testing"
)

func checkNumberedPage[T any](t *testing.T, p repository.Page[T], err error, count int, total int64) {
	t.Helper()
	must(t, err, "numbered list")
	if len(p.Items) != count || p.Total == nil || *p.Total != total {
		t.Fatalf("want %d rows total %d; got %+v", count, total, p)
	}
}

func TestNumberedPaginationStore(t *testing.T) {
	s := pgtest.OpenStore(t)
	for i := 0; i < 5; i++ {
		mustCreateHost(t, s, fullHost(fmt.Sprintf("PAGE-%d", i)))
		_, err := s.CreateUser(t.Context(), aUser(fmt.Sprintf("page%d", i)), anAudit("user.create"))
		must(t, err, "create user")
		mustCreateISO(t, s, rockyISO("same-name"))
	}
	hp, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 2, Limit: 2, SNPrefix: "PAGE-"})
	checkNumberedPage(t, hp, err, 2, 5)
	if hp.Items[0].SN != "PAGE-2" || hp.Next == "" {
		t.Fatalf("wrong offset or next: %+v", hp)
	}
	empty, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 9, Limit: 2, SNPrefix: "PAGE-"})
	checkNumberedPage(t, empty, err, 0, 5)
	missing, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 1, Limit: 2, SNPrefix: "missing"})
	checkNumberedPage(t, missing, err, 0, 0)
	invalid, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 1, SNPrefix: "\x00"})
	checkNumberedPage(t, invalid, err, 0, 0)
	users, err := s.ListUsers(t.Context(), repository.UserQuery{Page: 2, Limit: 2})
	checkNumberedPage(t, users, err, 2, 5)
	if users.Items[0].Username != "page2" {
		t.Fatal(users)
	}
	isos, err := s.ListISOs(t.Context(), repository.ISOQuery{Page: 2, Limit: 2})
	checkNumberedPage(t, isos, err, 2, 5)
	allISO, err := s.ListISOs(t.Context(), repository.ISOQuery{})
	must(t, err, "legacy isos")
	if !slices.EqualFunc(isos.Items, allISO.Items[2:4], func(a, b model.ISO) bool { return a.ID == b.ID }) || allISO.Total != nil {
		t.Fatal("ISO ties or legacy total")
	}
	audit, err := s.ListAudit(t.Context(), repository.AuditQuery{Page: 2, Limit: 2, Action: "host.create"})
	checkNumberedPage(t, audit, err, 2, 5)
	if audit.Items[0].TargetID != "PAGE-2" {
		t.Fatal(audit)
	}
	f := newFixture(t, s)
	f.mustCreate(t, s, mustTaskHosts(t, s, "TASK-A", "TASK-B", "TASK-C", "TASK-D", "TASK-E")...)
	tasks, err := s.ListTasks(t.Context(), repository.TaskQuery{Page: 2, Limit: 2, States: []model.TaskState{model.TaskPending}})
	checkNumberedPage(t, tasks, err, 2, 5)
	allTasks, err := s.ListTasks(t.Context(), repository.TaskQuery{})
	must(t, err, "legacy tasks")
	if !slices.EqualFunc(tasks.Items, allTasks.Items[2:4], func(a, b model.Task) bool { return a.ID == b.ID }) {
		t.Fatal("task timestamp ties unstable")
	}
	filtered, err := s.ListTasks(t.Context(), repository.TaskQuery{Page: 1, Limit: 2, HostSN: "TASK-C"})
	checkNumberedPage(t, filtered, err, 1, 1)
	// Numbered default size is 20, while legacy queries keep their old default.
	for i := 5; i < 25; i++ {
		mustCreateHost(t, s, fullHost(fmt.Sprintf("PAGE-%02d", i)))
	}
	defaults, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 1, SNPrefix: "PAGE-"})
	checkNumberedPage(t, defaults, err, 20, 25)
	secondDefault, err := s.ListHosts(t.Context(), repository.HostQuery{Page: 2, SNPrefix: "PAGE-"})
	checkNumberedPage(t, secondDefault, err, 5, 25)
	legacy, err := s.ListHosts(t.Context(), repository.HostQuery{SNPrefix: "PAGE-"})
	must(t, err, "legacy default")
	if len(legacy.Items) != 25 || legacy.Total != nil {
		t.Fatal("legacy default changed", legacy)
	}

}

func TestNumberedPaginationVersionSelection(t *testing.T) {
	s := pgtest.OpenStore(t)
	for i := 0; i < 3; i++ {
		h := hookOps.mustCreate(t, s, fmt.Sprintf("page-%d", i))
		h = hookOps.to(t, s, h, model.VersionApproved)
		_, err := s.NewHookVersion(t.Context(), h.ID, h.Version, "alice")
		must(t, err, "hook newer draft")
		tpl := templateOps.mustCreate(t, s, fmt.Sprintf("page-%d", i))
		tpl = templateOps.to(t, s, tpl, model.VersionApproved)
		_, err = s.NewTemplateVersion(t.Context(), tpl.ID, tpl.Version, "alice")
		must(t, err, "template newer draft")
	}
	for _, approved := range []bool{false, true} {
		hooks, err := s.ListHooks(t.Context(), repository.HookQuery{Page: 2, Limit: 1, ApprovedOnly: approved})
		checkNumberedPage(t, hooks, err, 1, 3)
		templates, err := s.ListTemplates(t.Context(), repository.TemplateQuery{Page: 2, Limit: 1, ApprovedOnly: approved, Series: "rocky"})
		checkNumberedPage(t, templates, err, 1, 3)
		wantVersion := 2
		if approved {
			wantVersion = 1
		}
		if hooks.Items[0].Name != "page-1" || hooks.Items[0].Version != wantVersion || templates.Items[0].Name != "page-1" || templates.Items[0].Version != wantVersion {
			t.Fatal(hooks, templates)
		}
	}
	hs, err := s.ListHooks(t.Context(), repository.HookQuery{Page: 1, States: []model.VersionState{model.VersionApproved}})
	checkNumberedPage(t, hs, err, 0, 0)
	ts, err := s.ListTemplates(t.Context(), repository.TemplateQuery{Page: 1, States: []model.VersionState{model.VersionApproved}})
	checkNumberedPage(t, ts, err, 0, 0)
	for n := 1; n <= 6; n++ {
		versions, err := s.ListHooks(t.Context(), repository.HookQuery{AllVersions: true, Page: n, Limit: 1, NamePrefix: "page-"})
		checkNumberedPage(t, versions, err, 1, 6)
		if versions.Items[0].Name != fmt.Sprintf("page-%d", (n-1)/2) || versions.Items[0].Version != (n-1)%2+1 {
			t.Fatal("all version ordering", versions)
		}
	}
	// All-version cursors include the version tie breaker, and return no numbered total.
	var cursor string
	for n := 0; n < 6; n++ {
		versions, err := s.ListHooks(t.Context(), repository.HookQuery{AllVersions: true, Cursor: cursor, Limit: 1, NamePrefix: "page-"})
		must(t, err, "all-version cursor")
		if len(versions.Items) != 1 || versions.Total != nil || versions.Items[0].Name != fmt.Sprintf("page-%d", n/2) || versions.Items[0].Version != n%2+1 {
			t.Fatal(versions)
		}
		cursor = versions.Next
		if (n < 5) != (cursor != "") {
			t.Fatal("wrong all-version next", n, cursor)
		}
	}
	approved, err := s.ListHooks(t.Context(), repository.HookQuery{AllVersions: true, Page: 2, Limit: 1, ApprovedOnly: true})
	checkNumberedPage(t, approved, err, 1, 3)
	noSeries, err := s.ListTemplates(t.Context(), repository.TemplateQuery{Page: 2, Limit: 1, Series: "ubuntu"})
	checkNumberedPage(t, noSeries, err, 0, 0)

}

func TestNumberedPaginationStoreRejectsInvalidQueries(t *testing.T) {
	s := pgtest.OpenStore(t)
	for _, number := range []int{-1, int(^uint(0) >> 1)} {
		queries := []func() error{
			func() error {
				_, e := s.ListHosts(t.Context(), repository.HostQuery{Page: number, Limit: 500})
				return e
			},
			func() error { _, e := s.ListISOs(t.Context(), repository.ISOQuery{Page: number, Limit: 500}); return e },
			func() error {
				_, e := s.ListHooks(t.Context(), repository.HookQuery{Page: number, Limit: 500})
				return e
			},
			func() error {
				_, e := s.ListTemplates(t.Context(), repository.TemplateQuery{Page: number, Limit: 500})
				return e
			},
			func() error {
				_, e := s.ListTasks(t.Context(), repository.TaskQuery{Page: number, Limit: 500})
				return e
			},
			func() error {
				_, e := s.ListAudit(t.Context(), repository.AuditQuery{Page: number, Limit: 500})
				return e
			},
			func() error {
				_, e := s.ListUsers(t.Context(), repository.UserQuery{Page: number, Limit: 500})
				return e
			},
		}
		for i, query := range queries {
			if e := query(); !errors.Is(e, repository.ErrInvalid) {
				t.Fatalf("query %d Page %d accepted: %v", i, number, e)
			}
		}
	}
}

func TestNumberedTemplateEligibilityBeforePagination(t *testing.T) {
	s := pgtest.OpenStore(t)
	iso := mustReadyISO(t, s, rockyISO("selection"), 1)
	plain := templateOps.mustCreate(t, s, "z-plain")
	plain = templateOps.to(t, s, plain, model.VersionApproved)
	_, err := s.NewTemplateVersion(t.Context(), plain.ID, plain.Version, "alice")
	must(t, err, "newer draft must not hide approved selection")
	offered := sampleTemplate("a-missing-offer")
	offered.Params = append(offered.Params, model.ParamDecl{Name: "edition", Type: "enum", Offer: "editions"})
	templateOps.to(t, s, templateOps.mustCreateFrom(t, s, offered), model.VersionApproved)
	hook := sampleHook("selection-hook")
	hook.Params = append(hook.Params, model.ParamDecl{Name: "edition", Type: "enum", Offer: "editions"})
	offeredHook, err := s.CreateHook(t.Context(), hook)
	must(t, err, "create offered hook")
	offeredHook = hookOps.to(t, s, offeredHook, model.VersionApproved)
	templateOps.to(t, s, templateOps.mustCreateFrom(t, s, referencing(fullTemplate("b-missing-hook-offer"), offeredHook.Key())), model.VersionApproved)
	deprecated := hookOps.to(t, s, hookOps.mustCreate(t, s, "deprecated-hook"), model.VersionApproved)
	templateOps.to(t, s, templateOps.mustCreateFrom(t, s, referencing(fullTemplate("c-deprecated-hook"), deprecated.Key())), model.VersionApproved)
	must(t, s.DeprecateHook(t.Context(), deprecated.Key(), deprecated.Rev, anAudit("hook.deprecate")), "deprecate referenced hook")
	builtinHook := hookOps.builtin("builtin-selection-hook", 1, "lober:SelectionHook")
	builtinHook.Params = sampleHook("ignored").Params
	must(t, s.EnsureBuiltinHook(t.Context(), builtinHook), "old builtin hook")
	builtinHook.Version = 2
	must(t, s.EnsureBuiltinHook(t.Context(), builtinHook), "current builtin hook")
	templateOps.to(t, s, templateOps.mustCreateFrom(t, s, referencing(fullTemplate("d-old-builtin-hook"), model.HookKey{ID: builtinHook.ID, Version: 1})), model.VersionApproved)
	templateOps.to(t, s, templateOps.mustCreateFrom(t, s, referencing(fullTemplate("y-current-builtin-hook"), builtinHook.Key())), model.VersionApproved)
	oldBuiltin := templateOps.builtin("builtin-selection-old", 1, "lober:OldSelection")
	oldBuiltin.Params = nil
	oldBuiltin.Assembly = nil
	must(t, s.EnsureBuiltinTemplate(t.Context(), oldBuiltin), "old builtin template")
	currentBuiltin := oldBuiltin
	currentBuiltin.ID = "builtin-selection-current"
	currentBuiltin.Name = "lober:CurrentSelection"
	must(t, s.EnsureBuiltinTemplate(t.Context(), currentBuiltin), "current builtin template")
	selection := &repository.TemplateSelection{ISO: iso, BuiltinTemplates: []model.TemplateKey{{ID: currentBuiltin.ID, Version: 1}}, BuiltinHooks: []model.HookKey{builtinHook.Key()}}
	for _, offers := range []map[string][]model.Option{nil, {"editions": {}}, {"editions": {{Value: "server", Label: "Server"}}}} {
		selection.ISO.Offers = offers
		all, err := s.ListTemplates(t.Context(), repository.TemplateQuery{ApprovedOnly: true, Series: iso.Series})
		must(t, err, "all templates for behavioral reference")
		want := []model.Template{}
		for _, candidate := range all.Items {
			hooks, err := s.GetHooks(t.Context(), repository.HookRefs(candidate))
			must(t, err, "reference hooks")
			builtinOK := !candidate.Builtin || candidate.ID == currentBuiltin.ID && candidate.Version == currentBuiltin.Version
			for _, h := range hooks {
				if h.Builtin && (h.ID != builtinHook.ID || h.Version != builtinHook.Version) {
					builtinOK = false
				}
			}
			if builtinOK && len(admission.TemplateUsable(candidate, hooks, selection.ISO)) == 0 {
				want = append(want, candidate)
			}
		}
		for n := 1; n <= len(want)+1; n++ {
			got, err := s.ListTemplates(t.Context(), repository.TemplateQuery{Page: n, Limit: 1, Selection: selection})
			count := 1
			if n > len(want) {
				count = 0
			}
			checkNumberedPage(t, got, err, count, int64(len(want)))
			if count == 1 && got.Items[0].ID != want[n-1].ID {
				t.Fatal("eligibility page disagrees with domain rules", got, want[n-1])
			}
		}
	}
}
