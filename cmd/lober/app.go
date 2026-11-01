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
	"github.com/lober/lober/internal/application/inventory"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	tasksport "github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/interfaces/console"
)

type app struct {
	Builtin    authoring.Bootstrapper
	Identity   identity.Bootstrapper
	Context    context.Context
	Config     config
	Clock      ports.Clock
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
	if err := a.Builtin.Initialise(a.Context); err != nil {
		return err
	}
	return a.Identity.Initialise(a.Context, a.Config.SeedAdmin)
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
func taskBackendOf(m *machineBundle) tasksport.Control {
	if m.Service != nil {
		return execution.NewAuditedControl(m.Service)
	}
	return tasks.NewUnavailableControl()
}
func wizardOf(st repository.Store, hub ports.Notifier, clock ports.Clock, guard ports.SelectionGuard, ctl tasksport.Control) tasks.Wizard {
	manifest := selection.Manifest{}
	for _, t := range builtin.Templates() {
		manifest.Templates = append(manifest.Templates, model.TemplateKey{ID: t.ID, Version: t.Version})
	}
	for _, h := range builtin.Hooks() {
		manifest.Hooks = append(manifest.Hooks, model.HookKey{ID: h.ID, Version: h.Version})
	}
	return tasks.NewWizard(tasks.WizardDeps{Hosts: st, ISOs: st, Templates: st, Hooks: st, Tasks: st, Manifest: manifest, Clock: clock, Notifier: hub, SelectionGuard: guard, Diagnostics: ctl})
}
func hookAuthoringOf(st repository.Store, clock ports.Clock) authoring.Hooks {
	return authoring.NewHooks(authoring.HookDeps{Hooks: st, Clock: clock})
}
func templateAuthoringOf(st repository.Store, clock ports.Clock, guard ports.SelectionGuard) authoring.Templates {
	return authoring.NewTemplates(authoring.TemplateDeps{Templates: st, Hooks: st, Clock: clock, SelectionGuard: guard})
}
func previewRendererOf(cfg config) ports.FrozenPreview {
	return authoring.NewPreviewRenderer(iso.NewBootConfigReader(cfg.ISO.Artifacts), ports.PreviewEndpoints{Gateway: cfg.Preview.Gateway, Artifacts: cfg.Preview.Artifacts, SMB: cfg.Preview.SMB, SyslogAddr: cfg.Preview.SyslogAddr})
}
func authoringEditorOf(st repository.Store, renderer ports.FrozenPreview) authoring.Editor {
	return authoring.NewEditor(authoring.EditorDeps{Templates: st, Hooks: st, Hosts: st, ISOs: st, Renderer: renderer})
}
func hostRegistryOf(st repository.Store, clock ports.Clock) hosts.Registry {
	return hosts.NewRegistry(hosts.Deps{Hosts: st, Clock: clock})
}
func mediaRegistryOf(st repository.Store, src port.SourceControl, j *appmedia.Deletion, clock ports.Clock) appmedia.Registry {
	return appmedia.NewRegistry(appmedia.Deps{ISOs: st, Sources: st, Control: src, Deletion: j, Clock: clock})
}
func consoleDepsOf(mediaRegistry appmedia.Registry, clock ports.Clock, logger *slog.Logger, wizard tasks.Wizard, hooks authoring.Hooks, templates authoring.Templates, editor authoring.Editor, controller tasks.Controller, hostRegistry hosts.Registry, identityService *identity.Service, auditReader audit.Reader, taskReader tasks.Reader, taskEvents tasks.EventReader) console.Deps {
	return console.Deps{TaskReader: taskReader, TaskEvents: taskEvents, HostRegistry: hostRegistry, MediaRegistry: mediaRegistry, Wizard: wizard, HookAuthoring: hooks, TemplateAuthoring: templates, AuthoringEditor: editor, AuditReader: auditReader, Identity: identityService, TaskController: controller, Clock: clock, Logger: logger}
}
func appOf(ctx context.Context, cfg config, clock ports.Clock, logger *slog.Logger, st repository.Store, consoleServer *console.Server, src *appmedia.Sources, ctl port.SourceControl, s *appmedia.Supervisor, j *appmedia.Deletion, m *machineBundle, identityService *identity.Service, builtinBootstrapper authoring.Bootstrapper) *app {
	return &app{Builtin: builtinBootstrapper, Identity: identityService, Context: ctx, Config: cfg, Clock: clock, Logger: logger, Store: st, Console: consoleServer, Sources: src, Control: ctl, Supervisor: s, Deletion: j, Machine: m}
}

func identityServiceOf(st repository.Store, cfg config, clock ports.Clock) *identity.Service {
	sessions := identity.NewSessionBook(cfg.SessionTTLHours*3600, clock, credentials.NewTokens())
	return identity.NewService(identity.Deps{Users: st, RoleMenus: st, Passwords: credentials.NewPasswords(), Sessions: sessions, Clock: clock})
}

func auditReaderOf(st repository.Store) audit.Reader { return audit.NewReader(st) }

// inventoryServiceOf 是采集应用的装配口。它的形状与 identityServiceOf 一致，身份与令牌
// 能力只在这里构造，其他位置不得自己再建一个随机源。
func inventoryServiceOf(st repository.Store, clock ports.Clock) *inventory.Service {
	return inventory.New(inventory.Deps{Hosts: st, Tokens: credentials.NewTokens(), Clock: clock, Tasks: st})
}

// taskReaderOf 显式共用原任务仓储、诊断与冻结渲染能力。
func taskReaderOf(st repository.Store, ctl tasksport.Control, renderer ports.FrozenPreview) tasks.Reader {
	return tasks.NewReader(tasks.ReaderDeps{Tasks: st, Diagnostics: ctl, Renderer: renderer})
}

func taskControllerOf(st repository.Store, backend tasksport.Control, clock ports.Clock, notifier ports.Notifier) tasks.Controller {
	return tasks.NewController(tasks.ControllerDeps{Tasks: st, Backend: backend, Clock: clock, Notifier: notifier})
}

// taskEventsOf 与执行、向导和控制入口共用唯一通知实例。
func taskEventsOf(st repository.Store, notifier ports.Notifier) tasks.EventReader {
	return tasks.NewEventReader(tasks.EventDeps{Tasks: st, Events: st, Notifier: notifier})
}

func builtinBootstrapperOf(st repository.Store) authoring.Bootstrapper {
	return authoring.NewBootstrapper(st, builtin.Hooks(), builtin.Templates())
}
