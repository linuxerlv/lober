package main

import (
	"testing"

	"github.com/lober/lober/internal/domain/repository"
)

func Test空库必须显式初始口令已有用户不再要求(t *testing.T) {
	a, cfg := assembled(t)
	// A second empty schema tests actual config loading, rather than bypassing
	// the loader that previously substituted a known password.
	empty := assemblyConfig(t)
	t.Setenv("LOBER_PG_DSN", empty.PG.DSN)
	loaded, err := loadConfig(configFile(t, "listen: '127.0.0.1:8080'\n"))
	if err != nil {
		t.Fatal(err)
	}
	loaded.ISO = empty.ISO
	b, cleanup, err := newApp(t.Context(), loaded, a.Clock, a.Logger)
	if err != nil {
		t.Fatal("assembly failed")
	}
	defer cleanup()
	if err := b.Initialise(); err == nil {
		t.Fatal("missing bootstrap secret enabled a known account")
	}
	users, err := b.Store.ListUsers(t.Context(), repository.UserQuery{})
	if err != nil || len(users.Items) != 0 {
		t.Fatal("failed bootstrap persisted user", len(users.Items), err)
	}
	b.Config.SeedAdmin = cfg.SeedAdmin
	if err := b.Initialise(); err != nil {
		t.Fatal(err)
	}
	b.Config.SeedAdmin = ""
	if err := b.Initialise(); err != nil {
		t.Fatal("populated database still required bootstrap secret", err)
	}
}
