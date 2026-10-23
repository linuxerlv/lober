package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"log/slog"
	"os"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/hosts"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
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
	Machine    *machineBundle
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
func taskControlOf(m *machineBundle) console.TaskControl {
	if m.Service != nil {
		return m.Service
	}
	return taskControlStub{}
}
func wizardOf(st repository.Store, hub ports.Notifier, clock console.Clock, guard ports.SelectionGuard, ctl console.TaskControl) tasks.Wizard {
	manifest := selection.Manifest{}
	for _, t := range builtin.Templates() {
		manifest.Templates = append(manifest.Templates, model.TemplateKey{ID: t.ID, Version: t.Version})
	}
	for _, h := range builtin.Hooks() {
		manifest.Hooks = append(manifest.Hooks, model.HookKey{ID: h.ID, Version: h.Version})
	}
	return tasks.NewWizard(tasks.WizardDeps{Hosts: st, ISOs: st, Templates: st, Hooks: st, Tasks: st, Manifest: manifest, Clock: clock, Notifier: hub, SelectionGuard: guard, Diagnostics: ctl})
}
func hookAuthoringOf(st repository.Store, clock console.Clock) authoring.Hooks {
	return authoring.NewHooks(authoring.HookDeps{Hooks: st, Clock: clock})
}
func templateAuthoringOf(st repository.Store, clock console.Clock, guard ports.SelectionGuard) authoring.Templates {
	return authoring.NewTemplates(authoring.TemplateDeps{Templates: st, Hooks: st, Clock: clock, SelectionGuard: guard})
}
func previewRendererOf(cfg config) ports.FrozenPreview {
	return authoring.NewPreviewRenderer(iso.NewBootConfigReader(cfg.ISO.Artifacts), ports.PreviewEndpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr})
}
func authoringEditorOf(st repository.Store, renderer ports.FrozenPreview) authoring.Editor {
	return authoring.NewEditor(authoring.EditorDeps{Templates: st, Hooks: st, Hosts: st, ISOs: st, Renderer: renderer})
}
func hostRegistryOf(st repository.Store, clock console.Clock) hosts.Registry {
	return hosts.NewRegistry(hosts.Deps{Hosts: st, Clock: clock})
}
func mediaRegistryOf(st repository.Store, src *sourceSupervisor, j *deletionJournal, clock console.Clock) appmedia.Registry {
	return appmedia.NewRegistry(appmedia.Deps{ISOs: st, Sources: st, Control: src, Deletion: j, Clock: clock})
}
func consoleDepsOf(st repository.Store, mediaRegistry appmedia.Registry, hub ports.Notifier, cfg config, clock console.Clock, logger *slog.Logger, wizard tasks.Wizard, hooks authoring.Hooks, templates authoring.Templates, editor authoring.Editor, renderer ports.FrozenPreview, ctl console.TaskControl, hostRegistry hosts.Registry) console.Deps {
	return console.Deps{HostRegistry: hostRegistry, MediaRegistry: mediaRegistry, Tasks: st, Wizard: wizard, HookAuthoring: hooks, TemplateAuthoring: templates, AuthoringEditor: editor, Events: st, Audit: st, Users: st, RoleMenus: st, FrozenPreview: renderer, TaskCtl: ctl, Notifier: hub, Clock: clock, Logger: logger, SessionTTL: cfg.SessionTTLHours * 3600}
}
func appOf(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger, st repository.Store, consoleServer *console.Server, raw *iso.Sources, s *sourceSupervisor, j *deletionJournal, g *registrationGate, m *machineBundle) *app {
	return &app{Context: ctx, Config: cfg, Clock: clock, Logger: logger, Store: st, Console: consoleServer, Native: raw, Supervisor: s, Deletion: j, Gate: g, Machine: m}
}
