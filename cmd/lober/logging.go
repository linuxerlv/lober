package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

type loggingStore struct {
	store.Store
	logger *slog.Logger
}

func loggingStoreOf(st *pgstore.Store, logger *slog.Logger) *loggingStore {
	return &loggingStore{Store: st, logger: logger}
}
func (l *loggingStore) log(op string, start time.Time, err error) {
	if err != nil {
		l.logger.Warn("仓储调用失败", "operation", op, "duration", time.Since(start))
		return
	}
	l.logger.Debug("仓储调用", "operation", op, "duration", time.Since(start))
}
func (l *loggingStore) CreateHost(ctx context.Context, h model.Host, a store.Audit) (model.Host, error) {
	start := time.Now()
	got, err := l.Store.CreateHost(ctx, h, a)
	l.log("host.create", start, err)
	return got, err
}
func (l *loggingStore) CreateTasks(ctx context.Context, tasks []model.Task, a store.Audit) ([]model.Task, error) {
	start := time.Now()
	got, err := l.Store.CreateTasks(ctx, tasks, a)
	l.log("task.create", start, err)
	return got, err
}
func (l *loggingStore) TransitionTask(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState, events []store.NewEvent) (model.Task, []store.Appended, error) {
	start := time.Now()
	got, appended, err := l.Store.TransitionTask(ctx, id, expect, to, events)
	l.log("task.transition", start, err)
	return got, appended, err
}
func (l *loggingStore) UpdateTaskExecution(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState, runtime *model.Execution, events []store.NewEvent, audit store.Audit) (model.Task, []store.Appended, error) {
	start := time.Now()
	got, appended, err := l.Store.UpdateTaskExecution(ctx, id, expect, to, runtime, events, audit)
	l.log("task.execution", start, err)
	return got, appended, err
}
