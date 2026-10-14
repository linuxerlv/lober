package console

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// ErrTaskControlUnavailable is the explicit layer-7 assembly placeholder.
// Other errors retain their normal mapping and are never echoed to clients.
var ErrTaskControlUnavailable = errors.New("task control is not implemented")

// TaskControl delegates lifecycle decisions to the future execution layer.
type TaskControl interface {
	Cancel(context.Context, model.Task, store.Audit) error
	Retry(context.Context, model.Task, store.Audit) error
	CanCancel(model.Task) []model.Problem
	CanRetry(model.Task) []model.Problem
}

func Problems(ps []model.Problem) error {
	if len(ps) == 0 {
		return nil
	}
	return &store.ProblemsError{Kind: store.ErrRejected, Problems: ps}
}
