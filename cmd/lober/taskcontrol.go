package main

import (
	"context"
	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Layer 7 supplies the safety rules and execution state machine.
type taskControlStub struct{}

func (taskControlStub) Cancel(context.Context, model.Task, store.Audit) error {
	return console.ErrTaskControlUnavailable
}
func (taskControlStub) Retry(context.Context, model.Task, store.Audit) error {
	return console.ErrTaskControlUnavailable
}
func (taskControlStub) CanCancel(model.Task) []model.Problem { return nil }
func (taskControlStub) CanRetry(model.Task) []model.Problem  { return nil }
