package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
	"testing"
)

type unsupportedStartupStore struct {
	repository.Store
	pages   []repository.Page[model.Task]
	queries []repository.TaskQuery
	err     error
}

func (s *unsupportedStartupStore) ListTasks(_ context.Context, q repository.TaskQuery) (repository.Page[model.Task], error) {
	s.queries = append(s.queries, q)
	if s.err != nil {
		return repository.Page[model.Task]{}, s.err
	}
	p := s.pages[0]
	s.pages = s.pages[1:]
	return p, nil
}

func TestStartupRejectsUnsupportedUnfinishedTasksBeforeInitialisation(t *testing.T) {
	for _, state := range []model.TaskState{model.TaskPending, model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing} {
		t.Run(string(state), func(t *testing.T) {
			store := &unsupportedStartupStore{pages: []repository.Page[model.Task]{{Next: "next"}, {Items: []model.Task{{ID: "old-task", HostSN: "CUSTOMER-HOST", State: state, Snapshot: model.Snapshot{Family: "linux", Series: "openeuler", Format: "kickstart"}}}}}}
			calls := []string{}
			a := &app{Context: t.Context(), Store: store, Builtin: &startupBuiltin{calls: &calls}, Identity: &startupIdentity{calls: &calls}}
			err := a.Initialise()
			if err == nil || !strings.Contains(err.Error(), "old-task") || !strings.Contains(err.Error(), "CUSTOMER-HOST") || !strings.Contains(err.Error(), "openeuler") {
				t.Fatalf("应列出旧任务并阻止启动：%v", err)
			}
			if len(calls) != 0 {
				t.Fatal("检查前已进行初始化", calls)
			}
			if len(store.queries) != 2 || store.queries[1].Cursor != "next" {
				t.Fatal("没有检查后续页", store.queries)
			}
		})
	}
}
func TestStartupPreservesTaskReadFailure(t *testing.T) {
	failure := errors.New("任务读取失败")
	calls := []string{}
	a := &app{Context: t.Context(), Store: &unsupportedStartupStore{err: failure}, Builtin: &startupBuiltin{calls: &calls}, Identity: &startupIdentity{calls: &calls}}
	if err := a.Initialise(); !errors.Is(err, failure) || len(calls) != 0 {
		t.Fatal("读取失败仍启动", err, calls)
	}
}

func TestStartupAllowsSupportedTasksAndCompletedHistory(t *testing.T) {
	for _, series := range []string{"rhel", "almalinux", "rocky", "ubuntu", "debian", "windows-server", "windows-desktop", "esxi"} {
		t.Run(series, func(t *testing.T) {
			current := model.Snapshot{Series: catalog.Series(series)}
			current.Family = current.Series.Family()
			current.Format = current.Series.Format()
			store := &unsupportedStartupStore{pages: []repository.Page[model.Task]{{Items: []model.Task{{ID: "current", State: model.TaskPending, Snapshot: current}, {ID: "old-completed", State: model.TaskDone, Snapshot: model.Snapshot{Family: "linux", Series: "sles16", Format: "agama"}}}}}}
			calls := []string{}
			a := &app{Context: t.Context(), Store: store, Builtin: &startupBuiltin{calls: &calls}, Identity: &startupIdentity{calls: &calls}}
			if err := a.Initialise(); err != nil {
				t.Fatal("保留系统或已结束旧任务阻止启动", err)
			}
			if len(calls) != 2 {
				t.Fatal("没有正常初始化", calls)
			}
		})
	}
}
