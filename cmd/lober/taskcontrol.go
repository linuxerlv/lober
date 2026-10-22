package main

import (
	"context"
	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Layer 7 supplies the safety rules and execution state machine.
type taskControlStub struct{}

func (taskControlStub) Cancel(context.Context, model.Task, repository.Audit) error {
	return console.ErrTaskControlUnavailable
}
func (taskControlStub) Retry(context.Context, model.Task, repository.Audit) error {
	return console.ErrTaskControlUnavailable
}
func (taskControlStub) CanCancel(model.Task) []model.Problem { return nil }
func (taskControlStub) CanRetry(model.Task) []model.Problem  { return nil }
