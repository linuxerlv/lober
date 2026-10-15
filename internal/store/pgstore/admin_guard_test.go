package pgstore_test

import (
	"errors"
	"testing"

	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func Test最后启用管理员不能禁用或降级(t *testing.T) {
	s := pgtest.OpenStore(t)
	in := aUser("administrator")
	in.Role = store.RoleAdmin
	u, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	if err != nil {
		t.Fatal(err)
	}
	for _, disable := range []bool{false, true} {
		changed := u
		if disable {
			changed.Disabled = true
		} else {
			changed.Role = store.RoleViewer
		}
		if _, err := s.UpdateUser(t.Context(), changed, anAudit("user.remove.admin")); !errors.Is(err, store.ErrConflict) {
			t.Fatal("last admin removed", err)
		}
	}
	back, err := s.GetUser(t.Context(), u.Username)
	if err != nil || back.Rev != u.Rev || back.Disabled || back.Role != store.RoleAdmin {
		t.Fatal(back, err)
	}
	entries, err := s.ListAudit(t.Context(), store.AuditQuery{Action: "user.remove.admin"})
	if err != nil || len(entries.Items) != 0 {
		t.Fatal("rejected removal wrote audit", entries, err)
	}
}

func Test并发管理员移除必须保留一位(t *testing.T) {
	s := pgtest.OpenStore(t)
	users := make([]store.User, 2)
	for i, name := range []string{"admin_a", "admin_b"} {
		u := aUser(name)
		u.Role = store.RoleAdmin
		var err error
		users[i], err = s.CreateUser(t.Context(), u, anAudit("user.create"))
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, u := range users {
		go func(u store.User) {
			<-start
			u.Disabled = true
			_, err := s.UpdateUser(t.Context(), u, anAudit("user.cross.disable"))
			results <- err
		}(u)
	}
	close(start)
	succeeded := 0
	for range 2 {
		if err := <-results; err == nil {
			succeeded++
		} else if !errors.Is(err, store.ErrConflict) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatal("cross disable did not preserve administrator", succeeded)
	}
	page, err := s.ListUsers(t.Context(), store.UserQuery{})
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, u := range page.Items {
		if u.Role == store.RoleAdmin && !u.Disabled {
			enabled++
		}
	}
	if enabled != 1 {
		t.Fatal("wrong surviving administrator count", enabled)
	}
}
