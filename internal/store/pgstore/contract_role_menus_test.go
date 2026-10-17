package pgstore_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func Test契约角色菜单(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	if _, found, err := s.GetRoleMenus(t.Context(), store.RoleViewer); err != nil || found {
		t.Fatalf("missing: %v %v", found, err)
	}
	input := store.RoleMenuSettings{Role: store.RoleViewer, MenuIDs: []string{}}
	if _, err := s.SaveRoleMenus(t.Context(), input, store.Audit{}); !errors.Is(err, store.ErrAuditRequired) {
		t.Fatal(err)
	}
	saved, err := s.SaveRoleMenus(t.Context(), input, anAudit("role.menus.update"))
	if err != nil || saved.Rev != 1 || saved.MenuIDs == nil {
		t.Fatalf("save: %+v %v", saved, err)
	}
	reopened := pgtest.OpenAt(t, dsn)
	got, found, err := reopened.GetRoleMenus(t.Context(), store.RoleViewer)
	if err != nil || !found || !reflect.DeepEqual(got, saved) {
		t.Fatalf("persist: %+v %v %v", got, found, err)
	}
	if _, err := s.SaveRoleMenus(t.Context(), input, anAudit("role.menus.update")); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	saved.MenuIDs = []string{"hosts"}
	saved, err = s.SaveRoleMenus(t.Context(), saved, anAudit("role.menus.update"))
	if err != nil || saved.Rev != 2 {
		t.Fatalf("update: %+v %v", saved, err)
	}
}

func Test角色菜单审计失败回滚(t *testing.T) {
	s := pgtest.OpenStore(t)
	a := anAudit("role.menus.update")
	a.Actor = pgtest.FaultActor
	in := store.RoleMenuSettings{Role: store.RoleViewer, MenuIDs: []string{"hosts"}}
	if _, err := s.SaveRoleMenus(t.Context(), in, a); err == nil {
		t.Fatal("audit failure accepted")
	}
	if _, found, err := s.GetRoleMenus(t.Context(), in.Role); err != nil || found {
		t.Fatalf("failed create persisted: %v %v", found, err)
	}
	saved, err := s.SaveRoleMenus(t.Context(), in, anAudit("role.menus.update"))
	if err != nil {
		t.Fatal(err)
	}
	in = saved
	in.MenuIDs = []string{}
	if _, err := s.SaveRoleMenus(t.Context(), in, a); err == nil {
		t.Fatal("audit failure accepted")
	}
	got, found, err := s.GetRoleMenus(t.Context(), in.Role)
	if err != nil || !found || !reflect.DeepEqual(got, saved) {
		t.Fatalf("failed update persisted: %+v %v", got, err)
	}
	audit, err := s.ListAudit(t.Context(), store.AuditQuery{})
	if err != nil || len(audit.Items) != 1 || audit.Items[0].TargetKind != "role" || audit.Items[0].TargetID != "viewer" {
		t.Fatalf("audit: %+v %v", audit, err)
	}
}

func Test角色菜单并发首次保存与更新(t *testing.T) {
	s := pgtest.OpenStore(t)
	for _, rev := range []int64{0, 1} {
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, ids := range [][]string{{"hosts"}, {"templates"}} {
			wg.Add(1)
			go func(ids []string) {
				defer wg.Done()
				<-start
				_, err := s.SaveRoleMenus(t.Context(), store.RoleMenuSettings{Role: store.RoleViewer, MenuIDs: ids, Rev: rev}, anAudit("role.menus.update"))
				results <- err
			}(ids)
		}
		close(start)
		wg.Wait()
		close(results)
		ok, conflicts := 0, 0
		for err := range results {
			if err == nil {
				ok++
			} else if errors.Is(err, store.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if ok != 1 || conflicts != 1 {
			t.Fatalf("rev %d: %d success, %d conflicts", rev, ok, conflicts)
		}
	}
	audit, err := s.ListAudit(t.Context(), store.AuditQuery{})
	if err != nil || len(audit.Items) != 2 {
		t.Fatalf("audit: %+v %v", audit, err)
	}
}
