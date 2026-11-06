package pgstore

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func (s *Store) UpdateTaskExecution(ctx context.Context, id string, expect repository.TaskExpect, to model.TaskState,
	runtime *model.Execution, events []repository.NewEvent, audit repository.Audit) (model.Task, []repository.Appended, error) {
	if err := repository.CheckTransition(id, to, events); err != nil {
		return model.Task{}, nil, err
	}
	audited := audit != (repository.Audit{})
	if audited {
		if err := repository.CheckAudit(audit); err != nil {
			return model.Task{}, nil, err
		}
	}
	if runtime == nil || runtime.Attempt < 1 || runtime.BootCount < 0 || runtime.BootCount > 3 ||
		runtime.IssuedAt <= 0 || runtime.PhaseAt < runtime.IssuedAt || runtime.Deadline < 0 || len(runtime.Signals) > 16384 {
		return model.Task{}, nil, repository.ErrInvalid
	}
	for key, value := range runtime.Signals {
		if len(key) > 128 || len(value) > 128 || !model.IsSafeWord(key) || !model.IsSafeWord(value) {
			return model.Task{}, nil, repository.ErrInvalid
		}
	}
	encoded, err := encodeJSON(runtime)
	if err != nil || len(encoded) > 4<<20 {
		return model.Task{}, nil, repository.ErrInvalid
	}
	var entry *repository.Audit
	if audited {
		entry = &audit
	}
	return s.persistTransition(ctx, id, expect, to, encoded, events, entry)
}
