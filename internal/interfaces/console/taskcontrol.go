package console

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// ErrTaskControlUnavailable is the explicit layer-7 assembly placeholder.
// Other errors retain their normal mapping and are never echoed to clients.
var ErrTaskControlUnavailable = errors.New("task control is not implemented")

// TaskControl delegates lifecycle decisions to the future execution layer.
type TaskControl interface {
	Cancel(context.Context, model.Task, ports.AuditIntent) error
	Retry(context.Context, model.Task, ports.AuditIntent) error
	CanCancel(model.Task) []model.Problem
	CanRetry(model.Task) []model.Problem
}

func Problems(ps []model.Problem) error {
	if len(ps) == 0 {
		return nil
	}
	return &repository.ProblemsError{Kind: repository.ErrRejected, Problems: ps}
}
