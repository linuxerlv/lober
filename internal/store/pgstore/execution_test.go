package pgstore_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

func TestExecutionPersistsWithTaskAndRejectsStaleWriter(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	f := newFixture(t, s)
	task := f.mustCreate(t, s, mustTaskHosts(t, s, "EXECUTION-PERSIST")...)[0]
	runtime := &model.Execution{Attempt: 1, BootCount: 1, IssuedAt: at0, PhaseAt: at0, Deadline: at0 + 600,
		Signals: map[string]string{"first": "verified"}}
	event := store.NewEvent{At: at0, Kind: "machine.boot", Key: "boot-one"}
	next, appended, err := s.UpdateTaskExecution(t.Context(), task.ID, store.TaskExpect{State: task.State, Rev: task.Rev},
		model.TaskBooting, runtime, []store.NewEvent{event}, store.Audit{})
	if err != nil || len(appended) != 1 || next.Rev != task.Rev+1 || !reflect.DeepEqual(next.Execution, runtime) {
		t.Fatalf("runtime transaction failed: %v", err)
	}
	runtime.BootCount = 2
	_, _, err = s.UpdateTaskExecution(t.Context(), task.ID, store.TaskExpect{State: task.State, Rev: task.Rev},
		model.TaskFailed, runtime, nil, store.Audit{})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale writer accepted", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := pgtest.OpenAt(t, dsn)
	restored := mustGetTask(t, reopened, task.ID)
	if restored.State != model.TaskBooting || restored.Execution.BootCount != 1 || restored.Execution.Signals["first"] != "verified" {
		t.Fatal("durable execution state changed after reconnect")
	}
}

func TestExecutionAuditFailureRollsBackRuntimeStateAndEvents(t *testing.T) {
	s := pgtest.OpenStore(t)
	f := newFixture(t, s)
	task := f.mustCreate(t, s, mustTaskHosts(t, s, "EXECUTION-ROLLBACK")...)[0]
	_, _, err := s.UpdateTaskExecution(t.Context(), task.ID, store.TaskExpect{State: task.State, Rev: task.Rev},
		model.TaskBooting, &model.Execution{Attempt: 1, IssuedAt: at0, PhaseAt: at0, Deadline: at0 + 600},
		[]store.NewEvent{{At: at0, Kind: "machine.boot"}}, store.Audit{Actor: pgtest.FaultActor, Action: "task.retry", At: at0})
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	if got := mustGetTask(t, s, task.ID); !reflect.DeepEqual(got, task) {
		t.Fatal("failed audit changed task/runtime")
	}
	events, err := s.ListEvents(t.Context(), task.ID, 0, 100)
	if err != nil || len(events) != 0 {
		t.Fatal("failed audit wrote execution events", err)
	}
}
