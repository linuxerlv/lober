package console

import (
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"github.com/lober/lober/internal/infrastructure/builtin"
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
	s := New(wizardDeps(Deps{Hosts: st, ISOs: st, Sources: st, Tasks: st, Events: st, Audit: st, Users: st, RoleMenus: st, Clock: now.Load, Notifier: NewHub(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, st))
	return s, st, now
}

type wizardTaskRepository struct {
	repository.Tasks
	repository.CurrentTaskCreator
}

// 测试也显式组装，保证向导、模板编写和通知订阅使用相同实例。
type authoringTestStore interface {
	repository.Hooks
	repository.Templates
	repository.CurrentTaskCreator
}

func wizardDeps(d Deps, store authoringTestStore, override ...repository.CurrentTaskCreator) Deps {
	return wizardDepsGuard(d, store, selectionguard.NewGuard(), override...)
}
func wizardDepsGuard(d Deps, store authoringTestStore, guard ports.SelectionGuard, override ...repository.CurrentTaskCreator) Deps {
	var current repository.CurrentTaskCreator = store
	if len(override) > 0 {
		current = override[0]
	}
	if d.Notifier == nil {
		d.Notifier = NewHub()
	}
	manifest := selection.Manifest{}
	for _, t := range builtin.Templates() {
		manifest.Templates = append(manifest.Templates, model.TemplateKey{ID: t.ID, Version: t.Version})
	}
	for _, h := range builtin.Hooks() {
		manifest.Hooks = append(manifest.Hooks, model.HookKey{ID: h.ID, Version: h.Version})
	}
	d.TemplateAuthoring = authoring.NewTemplates(authoring.TemplateDeps{Templates: store, Hooks: store, Clock: d.Clock, SelectionGuard: guard})
	if d.FrozenPreview == nil {
		d.FrozenPreview = authoring.NewPreviewRenderer(nil, ports.PreviewEndpoints{})
	}
	d.AuthoringEditor = authoring.NewEditor(authoring.EditorDeps{Templates: store, Hooks: store, Hosts: d.Hosts, ISOs: d.ISOs, Renderer: d.FrozenPreview})
	d.HookAuthoring = authoring.NewHooks(authoring.HookDeps{Hooks: store, Clock: d.Clock})
	d.Wizard = tasks.NewWizard(tasks.WizardDeps{Hosts: d.Hosts, ISOs: d.ISOs, Hooks: store, Templates: store, Tasks: wizardTaskRepository{d.Tasks, current}, Manifest: manifest, Clock: d.Clock, Notifier: d.Notifier, SelectionGuard: guard, Diagnostics: d.TaskCtl})
	return d
}
func seedHTTPUser(t *testing.T, s *Server, st repository.Users, name string, role model.Role) (string, model.User) {
	t.Helper()
	u, err := st.CreateUser(t.Context(), model.User{Username: name, DisplayName: name, Role: role, Password: "private-hash", CreatedAt: fixedAt}, repository.Audit{Actor: "test", Action: "user.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	return s.server.sessions.put(u.Username), u
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
