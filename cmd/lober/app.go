package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"log/slog"
	"os"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/interfaces/console"
)

type app struct {
	Context    context.Context
	Config     config
	Clock      console.Clock
	Logger     *slog.Logger
	Store      repository.Store
	Console    *console.Server
	Native     *iso.Sources
	Supervisor *sourceSupervisor
	Deletion   *deletionJournal
	Gate       *registrationGate
}

func (a *app) Initialise() error {
	for _, h := range builtin.Hooks() {
		if err := ensureBuiltin(a.Context, func() error { return a.Store.EnsureBuiltinHook(a.Context, h) }); err != nil {
			return err
		}
	}
	for _, t := range builtin.Templates() {
		if err := ensureBuiltin(a.Context, func() error { return a.Store.EnsureBuiltinTemplate(a.Context, t) }); err != nil {
			return err
		}
	}
	users, err := a.Store.ListUsers(a.Context, repository.UserQuery{Limit: 1})
	if err != nil {
		return err
	}
	if len(users.Items) == 0 {
		if a.Config.SeedAdmin == "" {
			return errors.New("空库初始化必须显式配置 seedAdmin")
		}
		hash, err := console.HashPassword(a.Config.SeedAdmin)
		if err != nil {
			return err
		}
		_, err = a.Store.CreateUser(a.Context, model.User{Username: "admin", DisplayName: "管理员", Role: model.RoleAdmin, Password: hash, MustChangePassword: true, CreatedAt: a.Clock()}, repository.Audit{Actor: "bootstrap", Action: "user.create", At: a.Clock()})
		if err != nil {
			return err
		}
	}
	return nil
}
func ensureBuiltin(ctx context.Context, operation func() error) error {
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation()
		if err == nil || !errors.Is(err, repository.ErrConflict) {
			return err
		}
		if attempt == 3 {
			return err
		}
	}
	return repository.ErrConflict
}
func openStore(ctx context.Context, cfg config, logger *slog.Logger) (*pgstore.Store, func(), error) {
	st, err := pgstore.Open(ctx, cfg.PG.DSN, pgstore.WithMaxConns(cfg.PG.MaxConns))
	if err != nil {
		return nil, nil, errors.New("无法连接或初始化 PostgreSQL")
	}
	return st, func() {
		if err := st.Close(); err != nil {
			logger.Warn("PostgreSQL 连接池关闭失败")
		}
	}, nil
}
func nativeSourcesOf(st repository.Store, cfg config) (*iso.Sources, error) {
	for _, dir := range []string{cfg.ISO.Staging, cfg.ISO.Artifacts, cfg.ISO.SMB} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, err
		}
	}
	return iso.New(st, iso.Dirs{Staging: cfg.ISO.Staging, Artifacts: cfg.ISO.Artifacts, SMB: cfg.ISO.SMB, Tools: cfg.ISO.Tools, LocalRoots: cfg.ISO.LocalRoots}), nil
}
func gateOf() *registrationGate { return &registrationGate{} }
func supervisorOf(ctx context.Context, raw *iso.Sources, st repository.Store, j *deletionJournal, g *registrationGate, cfg config, logger *slog.Logger) (*sourceSupervisor, func()) {
	s := newSourceSupervisor(ctx, raw, st, j, g, cfg.ISO, logger)
	return s, s.Close
}
func consoleDepsOf(st repository.Store, src *sourceSupervisor, j *deletionJournal, hub ports.Notifier, cfg config, clock console.Clock, logger *slog.Logger) console.Deps {
	return console.Deps{Hosts: st, ISOs: st, Hooks: st, Templates: st, Tasks: st, CurrentTasks: st, Events: st, Audit: st, Users: st, Sources: st, ISOSource: src, ISODeletion: j, BootConfig: bootConfigReader{root: cfg.ISO.Artifacts}, PreviewEndpoints: console.PreviewEndpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr}, TaskCtl: taskControlStub{}, Notifier: hub, Clock: clock, Logger: logger, SessionTTL: cfg.SessionTTLHours * 3600}
}
func appOf(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger, st repository.Store, consoleServer *console.Server, raw *iso.Sources, s *sourceSupervisor, j *deletionJournal, g *registrationGate) *app {
	return &app{Context: ctx, Config: cfg, Clock: clock, Logger: logger, Store: st, Console: consoleServer, Native: raw, Supervisor: s, Deletion: j, Gate: g}
}
