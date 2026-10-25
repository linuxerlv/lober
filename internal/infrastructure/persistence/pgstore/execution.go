package pgstore

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"gorm.io/gorm"
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
	var row taskRow
	var appended []repository.Appended
	err = s.write(ctx, func(tx *gorm.DB) error {
		var err error
		row, err = lockTask(tx, id)
		if err != nil {
			return err
		}
		if row.State != string(expect.State) || row.Rev != expect.Rev {
			return repository.ErrConflict
		}
		appended, err = appendEvents(tx, &row, events)
		if err != nil {
			return err
		}
		row.State, row.Rev, row.Execution = string(to), row.Rev+1, encoded
		if err := tx.Model(&taskRow{ID: id}).Select("state", "rev", "last_seq", "execution").Updates(&row).Error; err != nil {
			if openTaskTaken(err) {
				return repository.OpenTaskProblems(row.HostSN)
			}
			return err
		}
		if audited {
			return addAudits(tx, audit, "task", id)
		}
		return nil
	})
	if err != nil {
		return model.Task{}, nil, translate(err, false, nil)
	}
	task, err := row.task()
	return task, appended, err
}
