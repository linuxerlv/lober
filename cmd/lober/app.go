package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"log/slog"
	"os"
	"time"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/interfaces/console"
)

type app struct {
	Identity   identity.Bootstrapper
	Context    context.Context
	Config     config
	Clock      console.Clock
	Logger     *slog.Logger
	Store      repository.Store
	Console    *console.Server
	Sources    *appmedia.Sources
	Control    port.SourceControl
	Supervisor *appmedia.Supervisor
	Deletion   *appmedia.Deletion
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
	return a.Identity.Initialise(a.Context, a.Config.SeedAdmin)
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
func mediaDirsOf(cfg config) (iso.Dirs, error) {
	for _, dir := range []string{cfg.ISO.Staging, cfg.ISO.Artifacts, cfg.ISO.SMB} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return iso.Dirs{}, err
		}
	}
	return iso.Dirs{Staging: cfg.ISO.Staging, Artifacts: cfg.ISO.Artifacts, SMB: cfg.ISO.SMB, Tools: cfg.ISO.Tools, LocalRoots: cfg.ISO.LocalRoots}, nil
}
func gateOf() *iso.RegistrationGate   { return iso.NewRegistrationGate() }
func sourceLocksOf() *iso.SourceLocks { return iso.NewSourceLocks() }
func acquisitionOf(st repository.Store, dirs iso.Dirs, locks *iso.SourceLocks, gate *iso.RegistrationGate) *appmedia.Sources {
	return appmedia.NewSources(appmedia.SourceDeps{ISOs: st, Sources: st, Transfer: iso.NewTransfer(dirs), Import: iso.NewImportFiles(dirs), Locks: locks, Gate: gate})
}
func deletionJournalOf(dirs iso.Dirs) (*iso.DeletionJournal, error) {
	return iso.NewDeletionJournal(dirs.Staging)
}
func deletionOf(st repository.Store, dirs iso.Dirs, journal *iso.DeletionJournal, locks *iso.SourceLocks, gate *iso.RegistrationGate, logger *slog.Logger) *appmedia.Deletion {
	return appmedia.NewDeletion(appmedia.DeletionDeps{ISOs: st, Sources: st, Journal: journal, Files: iso.NewDeletedFiles(dirs), Locks: locks, Gate: gate, Logger: logger})
}
func supervisorOf(ctx context.Context, src *appmedia.Sources, st repository.Store, deletion *appmedia.Deletion, cfg config, logger *slog.Logger) (*appmedia.Supervisor, func()) {
	s := appmedia.NewSupervisor(ctx, appmedia.SupervisorDeps{Work: src, Sources: st, Deletion: deletion, Logger: logger, Workers: cfg.ISO.Workers, PollInterval: time.Duration(cfg.ISO.PollMS) * time.Millisecond})
	return s, s.Close
}
func mediaControlOf(src *appmedia.Sources, sup *appmedia.Supervisor) port.SourceControl {
	return appmedia.NewControl(src, sup)
}
func taskControlOf(m *machineBundle) console.TaskControl {
	if m.Service != nil {
		return execution.NewAuditedControl(m.Service)
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
func mediaRegistryOf(st repository.Store, src port.SourceControl, j *appmedia.Deletion, clock console.Clock) appmedia.Registry {
	return appmedia.NewRegistry(appmedia.Deps{ISOs: st, Sources: st, Control: src, Deletion: j, Clock: clock})
}
func consoleDepsOf(st repository.Store, mediaRegistry appmedia.Registry, hub ports.Notifier, cfg config, clock console.Clock, logger *slog.Logger, wizard tasks.Wizard, hooks authoring.Hooks, templates authoring.Templates, editor authoring.Editor, renderer ports.FrozenPreview, ctl console.TaskControl, hostRegistry hosts.Registry, identityService *identity.Service, auditReader audit.Reader, taskReader tasks.Reader) console.Deps {
	return console.Deps{TaskReader: taskReader, HostRegistry: hostRegistry, MediaRegistry: mediaRegistry, Tasks: st, Wizard: wizard, HookAuthoring: hooks, TemplateAuthoring: templates, AuthoringEditor: editor, Events: st, AuditReader: auditReader, Identity: identityService, FrozenPreview: renderer, TaskCtl: ctl, Notifier: hub, Clock: clock, Logger: logger}
}
func appOf(ctx context.Context, cfg config, clock console.Clock, logger *slog.Logger, st repository.Store, consoleServer *console.Server, src *appmedia.Sources, ctl port.SourceControl, s *appmedia.Supervisor, j *appmedia.Deletion, m *machineBundle, identityService *identity.Service) *app {
	return &app{Identity: identityService, Context: ctx, Config: cfg, Clock: clock, Logger: logger, Store: st, Console: consoleServer, Sources: src, Control: ctl, Supervisor: s, Deletion: j, Machine: m}
}

func identityServiceOf(st repository.Store, cfg config, clock console.Clock) *identity.Service {
	sessions := identity.NewSessionBook(cfg.SessionTTLHours*3600, clock, credentials.NewTokens())
	return identity.NewService(identity.Deps{Users: st, RoleMenus: st, Passwords: credentials.NewPasswords(), Sessions: sessions, Clock: clock})
}

func auditReaderOf(st repository.Store) audit.Reader { return audit.NewReader(st) }

// taskReaderOf 显式共用原任务仓储、诊断与冻结渲染能力。
func taskReaderOf(st repository.Store, ctl console.TaskControl, renderer ports.FrozenPreview) tasks.Reader {
	return tasks.NewReader(tasks.ReaderDeps{Tasks: st, Diagnostics: ctl, Renderer: renderer})
}
