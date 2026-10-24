package console

import (
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/application/media"
	mediaport "github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	tasksport "github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/infrastructure/credentials"
	"github.com/lober/lober/internal/infrastructure/notification"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	selectionguard "github.com/lober/lober/internal/infrastructure/selection"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const fixedAt int64 = 1790000000

func newHTTPServer(t *testing.T) (*Server, *pgstore.Store, *atomic.Int64) {
	t.Helper()
	st := pgtest.OpenStore(t)
	now := new(atomic.Int64)
	now.Store(fixedAt)
	s := New(wizardDeps(testDeps{Tasks: st, Events: st, AuditReader: audit.NewReader(st), Clock: now.Load, Notifier: notification.NewHub(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, st))
	return s, st, now
}

type wizardTaskRepository struct {
	repository.Tasks
	repository.CurrentTaskCreator
}

// 测试也显式组装，保证向导、模板编写和通知订阅使用相同实例。
type authoringTestStore interface {
	repository.Hosts
	repository.Users
	repository.RoleMenus
	repository.ISOs
	repository.ISOSources
	repository.Hooks
	repository.Templates
	repository.CurrentTaskCreator
}

func wizardDeps(d testDeps, store authoringTestStore, override ...repository.CurrentTaskCreator) Deps {
	return wizardDepsGuard(d, store, selectionguard.NewGuard(), override...)
}
func wizardDepsGuard(d testDeps, store authoringTestStore, guard ports.SelectionGuard, override ...repository.CurrentTaskCreator) Deps {
	return wizardDepsGuardControl(d, store, guard, tasks.NewUnavailableControl(), override...)
}
func wizardDepsGuardControl(input testDeps, store authoringTestStore, guard ports.SelectionGuard, backend tasksport.Control, override ...repository.CurrentTaskCreator) Deps {
	d := Deps{Clock: input.Clock, Logger: input.Logger, AuditReader: input.AuditReader, Identity: input.Identity}
	cap := &testCapabilities{Tasks: input.Tasks, Events: input.Events, Notifier: input.Notifier, FrozenPreview: input.FrozenPreview}

	if d.Identity == nil {
		repositories := &identityTestRepositories{Users: store, RoleMenus: store}
		sessions := identity.NewSessionBook(0, d.Clock, credentials.NewTokens())
		service := identity.NewService(identity.Deps{Users: repositories, RoleMenus: repositories, Passwords: credentials.NewPasswords(), Sessions: sessions, Clock: d.Clock})
		d.Identity = &identityTestFixture{Registry: service, sessions: sessions, repositories: repositories}
	}
	var current repository.CurrentTaskCreator = store
	if len(override) > 0 {
		current = override[0]
	}
	if cap.Notifier == nil {
		cap.Notifier = notification.NewHub()
	}
	manifest := selection.Manifest{}
	for _, t := range builtin.Templates() {
		manifest.Templates = append(manifest.Templates, model.TemplateKey{ID: t.ID, Version: t.Version})
	}
	for _, h := range builtin.Hooks() {
		manifest.Hooks = append(manifest.Hooks, model.HookKey{ID: h.ID, Version: h.Version})
	}
	d.TemplateAuthoring = authoring.NewTemplates(authoring.TemplateDeps{Templates: store, Hooks: store, Clock: d.Clock, SelectionGuard: guard})
	if cap.FrozenPreview == nil {
		cap.FrozenPreview = authoring.NewPreviewRenderer(nil, ports.PreviewEndpoints{})
	}
	d.AuthoringEditor = authoring.NewEditor(authoring.EditorDeps{Templates: store, Hooks: store, Hosts: store, ISOs: store, Renderer: cap.FrozenPreview})
	d.MediaRegistry = media.NewRegistry(media.Deps{ISOs: store, Sources: store, Control: &mockISOSource{calls: map[string][]sourceCall{}}, Deletion: &mockISODeletion{st: store}, Clock: d.Clock})
	d.HostRegistry = hosts.NewRegistry(hosts.Deps{Hosts: store, Clock: d.Clock})
	d.HookAuthoring = authoring.NewHooks(authoring.HookDeps{Hooks: store, Clock: d.Clock})
	d.TaskController = tasks.NewController(tasks.ControllerDeps{Tasks: cap.Tasks, Backend: backend, Clock: d.Clock, Notifier: cap.Notifier})
	d.TaskReader = tasks.NewReader(tasks.ReaderDeps{Tasks: cap.Tasks, Diagnostics: backend, Renderer: cap.FrozenPreview})
	d.TaskEvents = tasks.NewEventReader(tasks.EventDeps{Tasks: cap.Tasks, Events: cap.Events, Notifier: cap.Notifier})
	d.Identity.(*identityTestFixture).capabilities = cap
	d.Wizard = tasks.NewWizard(tasks.WizardDeps{Hosts: store, ISOs: store, Hooks: store, Templates: store, Tasks: wizardTaskRepository{cap.Tasks, current}, Manifest: manifest, Clock: d.Clock, Notifier: cap.Notifier, SelectionGuard: guard, Diagnostics: backend})
	return d
}
func seedHTTPUser(t *testing.T, s *Server, st repository.Users, name string, role model.Role) (string, model.User) {
	t.Helper()
	u, err := st.CreateUser(t.Context(), model.User{Username: name, DisplayName: name, Role: role, Password: "private-hash", CreatedAt: fixedAt}, repository.Audit{Actor: "test", Action: "user.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	return identityFixture(s).sessions.Put(u.Username), u
}
func httpCall(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	return w
}

func setMedia(s *Server, isos mediaport.ISOs, sources mediaport.Sources, control mediaport.SourceControl, deletion mediaport.Deletion) {
	s.server.deps.MediaRegistry = media.NewRegistry(media.Deps{ISOs: isos, Sources: sources, Control: control, Deletion: deletion, Clock: s.server.deps.Clock})
}

// 会话和可替换仓储只由测试夹具持有，生产网页只能访问 Registry。
type identityTestRepositories struct {
	repository.Users
	repository.RoleMenus
}
type identityTestFixture struct {
	identity.Registry
	sessions     *identity.SessionBook
	repositories *identityTestRepositories
	capabilities *testCapabilities
}

func identityFixture(s *Server) *identityTestFixture {
	return s.server.deps.Identity.(*identityTestFixture)
}

// 替换旧测试诊断或渲染器后，明确重接读取应用，避免仍持有旧实例。
func rewireTaskReader(s *Server, backendOverride ...tasksport.Control) {
	cap := testCaps(s)
	var backend tasksport.Control = tasks.NewUnavailableControl()
	if len(backendOverride) > 0 {
		backend = backendOverride[0]
	}
	s.server.deps.TaskReader = tasks.NewReader(tasks.ReaderDeps{Tasks: cap.Tasks, Diagnostics: backend, Renderer: cap.FrozenPreview})
}
func setTaskControl(s *Server, backend tasksport.Control) {
	d := s.server.deps
	cap := testCaps(s)
	s.server.deps.TaskController = tasks.NewController(tasks.ControllerDeps{Tasks: cap.Tasks, Backend: backend, Clock: d.Clock, Notifier: cap.Notifier})
	rewireTaskReader(s, backend)
}

// 测试替换能力只保存在身份夹具里，生产依赖只含应用入口。
type testCapabilities struct {
	Tasks         repository.Tasks
	Events        repository.Events
	Notifier      ports.Notifier
	FrozenPreview ports.FrozenPreview
}
type testDeps struct {
	Tasks         repository.Tasks
	Events        repository.Events
	Notifier      ports.Notifier
	FrozenPreview ports.FrozenPreview
	Clock         ports.Clock
	Logger        *slog.Logger
	AuditReader   audit.Reader
	Identity      identity.Registry
}

func testCaps(s *Server) *testCapabilities { return identityFixture(s).capabilities }
func setTaskNotifier(s *Server, n ports.Notifier) {
	cap := testCaps(s)
	cap.Notifier = n
	s.server.deps.TaskEvents = tasks.NewEventReader(tasks.EventDeps{Tasks: cap.Tasks, Events: cap.Events, Notifier: n})
}

type unusedTaskReader struct{ tasks.Reader }
type unusedTaskController struct{ tasks.Controller }
type unusedTaskEvents struct{ tasks.EventReader }

func emptyConsoleDeps(clock ports.Clock) Deps {
	return Deps{Clock: clock, TaskReader: unusedTaskReader{}, TaskController: unusedTaskController{}, TaskEvents: unusedTaskEvents{}}
}

func taskTestDeps(s *Server) testDeps {
	d := s.server.deps
	c := testCaps(s)
	return testDeps{Tasks: c.Tasks, Events: c.Events, Notifier: c.Notifier, FrozenPreview: c.FrozenPreview, Clock: d.Clock, Identity: d.Identity, AuditReader: d.AuditReader, Logger: d.Logger}
}
