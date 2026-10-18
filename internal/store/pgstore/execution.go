package pgstore

import (
	"context"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"gorm.io/gorm"
)

func (s *Store) UpdateTaskExecution(ctx context.Context, id string, expect store.TaskExpect, to model.TaskState,
	runtime *model.Execution, events []store.NewEvent, audit store.Audit) (model.Task, []store.Appended, error) {
	if err := store.CheckTransition(id, to, events); err != nil {
		return model.Task{}, nil, err
	}
	audited := audit != (store.Audit{})
	if audited {
		if err := store.CheckAudit(audit); err != nil {
			return model.Task{}, nil, err
		}
	}
	if runtime == nil || runtime.Attempt < 1 || runtime.BootCount < 0 || runtime.BootCount > 3 ||
		runtime.IssuedAt <= 0 || runtime.PhaseAt < runtime.IssuedAt || runtime.Deadline < 0 || len(runtime.Signals) > 16384 {
		return model.Task{}, nil, store.ErrInvalid
	}
	for key, value := range runtime.Signals {
		if len(key) > 128 || len(value) > 128 || !model.IsSafeWord(key) || !model.IsSafeWord(value) {
			return model.Task{}, nil, store.ErrInvalid
		}
	}
	encoded, err := encodeJSON(runtime)
	if err != nil || len(encoded) > 4<<20 {
		return model.Task{}, nil, store.ErrInvalid
	}
	var row taskRow
	var appended []store.Appended
	err = s.write(ctx, func(tx *gorm.DB) error {
		var err error
		row, err = lockTask(tx, id)
		if err != nil {
			return err
		}
		if row.State != string(expect.State) || row.Rev != expect.Rev {
			return store.ErrConflict
		}
		appended, err = appendEvents(tx, &row, events)
		if err != nil {
			return err
		}
		row.State, row.Rev, row.Execution = string(to), row.Rev+1, encoded
		if err := tx.Model(&taskRow{ID: id}).Select("state", "rev", "last_seq", "execution").Updates(&row).Error; err != nil {
			if openTaskTaken(err) {
				return store.OpenTaskProblems(row.HostSN)
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
