package execution

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

type Repository interface {
	GetTask(context.Context, string) (model.Task, error)
	FindOpenTask(context.Context, string) (model.Task, bool, error)
	ListTasks(context.Context, store.TaskQuery) (store.Page[model.Task], error)
	UpdateTaskExecution(context.Context, string, store.TaskExpect, model.TaskState, *model.Execution, []store.NewEvent, store.Audit) (model.Task, []store.Appended, error)
}
type Notifier interface{ Publish(string) }
type Service struct {
	repo     Repository
	key      []byte
	now      func() int64
	notifier Notifier
}

func New(repo Repository, key []byte, now func() int64, notifier Notifier) (*Service, error) {
	if len(key) < 32 || now == nil {
		return nil, errors.New("invalid execution configuration")
	}
	return &Service{repo: repo, key: append([]byte(nil), key...), now: now, notifier: notifier}, nil
}
func (s *Service) commit(ctx context.Context, before, after model.Task, event store.NewEvent, audit store.Audit) (model.Task, error) {
	next, _, err := s.repo.UpdateTaskExecution(ctx, before.ID, store.TaskExpect{State: before.State, Rev: before.Rev}, after.State, after.Execution, []store.NewEvent{event}, audit)
	if err == nil && s.notifier != nil {
		s.notifier.Publish(next.ID)
	}
	return next, err
}
func (s *Service) Boot(ctx context.Context, sn string, arch catalog.Arch, firmware catalog.Firmware) (model.Task, bool, error) {
	normalized, p := model.NormalizeSN(sn)
	if p != nil {
		return model.Task{}, false, nil
	}
	for range 8 {
		task, found, err := s.repo.FindOpenTask(ctx, normalized)
		if err != nil || !found {
			return task, found, err
		}
		// An installed or running installer must never be booted into installation again.
		if task.State != model.TaskPending && task.State != model.TaskBooting {
			return model.Task{}, false, nil
		}
		at := s.now()
		next, err := boot(task, arch, firmware, at)
		if err != nil {
			return model.Task{}, false, err
		}
		next, err = s.commit(ctx, task, next, store.NewEvent{At: at, Kind: "machine.boot", Key: attemptKey(next, "boot-"+strconv.Itoa(next.Execution.BootCount)) + "-" + string(next.State), Fields: map[string]string{"arch": string(arch), "firmware": string(firmware)}}, store.Audit{})
		if errors.Is(err, store.ErrConflict) {
			return model.Task{}, false, nil
		}
		return next, err == nil && !next.State.Finished(), err
	}
	return model.Task{}, false, store.ErrConflict
}

func (s *Service) GetAuthorized(ctx context.Context, id, token string, finalize bool) (model.Task, error) {
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return model.Task{}, err
	}
	scope := Install
	if finalize {
		scope = Finalize
	}
	if err := s.Authorize(task, token, scope); err != nil {
		return model.Task{}, err
	}
	return task, nil
}
func (s *Service) Answer(ctx context.Context, id, token string) (model.Task, error) {
	for range 8 {
		task, err := s.GetAuthorized(ctx, id, token, false)
		if err != nil {
			return model.Task{}, err
		}
		if task.State == model.TaskInstalling {
			return task, nil
		}
		next := clone(task)
		at := s.now()
		next.State = model.TaskInstalling
		stage(next.Execution, "packages", at, 3600)
		next, err = s.commit(ctx, task, next, store.NewEvent{At: at, Kind: "machine.installing", Key: attemptKey(next, "installing")}, store.Audit{})
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		return next, err
	}
	return model.Task{}, store.ErrConflict
}
func finalSignal(sig Signal) bool {
	return sig.Timing == catalog.TimingFirstboot || sig.Phase == "booted" || sig.Phase == "init-start" || sig.Phase == "init-done" || sig.Phase == "ready"
}
func (s *Service) Signal(ctx context.Context, id, token string, sig Signal) error {
	for range 8 {
		task, err := s.repo.GetTask(ctx, id)
		if err != nil {
			return err
		}
		scope := Install
		if finalSignal(sig) {
			scope = Finalize
		}
		if err := s.authorizeCredential(task, token, scope); err != nil {
			return err
		}
		if scope == Install && task.State.Finished() {
			return ErrCredential
		}
		at := s.now()
		next, duplicate, err := advance(task, sig, at)
		if err != nil || duplicate {
			return err
		}
		event := store.NewEvent{At: at, Kind: "machine." + sig.Phase, Key: attemptKey(next, signalKey(sig)), Fields: map[string]string{"slot": strconv.Itoa(sig.Slot), "timing": string(sig.Timing), "instance": strconv.Itoa(sig.Instance), "digest": sig.Digest, "rc": strconv.Itoa(sig.RC), "log_base64": base64.StdEncoding.EncodeToString(model.SafeLogTail(sig.Body))}}
		// Event kinds use dot identifiers; phases are canonical HTTP hyphen words.
		switch sig.Phase {
		case "slot-start":
			event.Kind = "slot.start"
		case "slot-end":
			event.Kind = "slot.end"
		case "hook-start":
			event.Kind = "hook.start"
		case "hook-report":
			event.Kind = "hook.report"
		case "init-start":
			event.Kind = "machine.init_start"
		case "init-done":
			event.Kind = "machine.init_done"
		}
		_, err = s.commit(ctx, task, next, event, store.Audit{})
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		return err
	}
	return store.ErrConflict
}

func (*Service) CanCancel(t model.Task) []model.Problem { return CanCancel(t) }
func (*Service) CanRetry(t model.Task) []model.Problem  { return CanRetry(t) }

// User controls use the exact revision the user reviewed. Conflicts require refresh.
func (s *Service) control(ctx context.Context, task model.Task, audit store.Audit, retry bool) error {
	if err := store.CheckAudit(audit); err != nil {
		return err
	}
	latest, err := s.repo.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if latest.Rev != task.Rev || latest.State != task.State {
		return store.ErrConflict
	}
	problems := CanCancel(latest)
	if retry {
		problems = CanRetry(latest)
	}
	if len(problems) > 0 {
		return &store.ProblemsError{Kind: store.ErrRejected, Problems: problems}
	}
	next := clone(latest)
	at := s.now()
	kind := "cancelled"
	if next.Execution == nil {
		next.Execution = runtime(at, 1)
	}
	if retry {
		attempt := next.Execution.Attempt + 1
		next.Execution = runtime(at, attempt)
		next.State = model.TaskPending
		kind = "retry"
	} else {
		next.State = model.TaskCancelled
		next.Execution.Deadline = 0
	}
	_, err = s.commit(ctx, latest, next, store.NewEvent{At: at, Kind: "machine." + kind, Key: attemptKey(next, kind)}, audit)
	return err
}
func (s *Service) Cancel(ctx context.Context, t model.Task, a store.Audit) error {
	return s.control(ctx, t, a, false)
}
func (s *Service) Retry(ctx context.Context, t model.Task, a store.Audit) error {
	return s.control(ctx, t, a, true)
}

// Sweep reads durable state on every invocation, including after process restart.
func (s *Service) Sweep(ctx context.Context) error {
	q := store.TaskQuery{States: []model.TaskState{model.TaskBooting, model.TaskInstalling, model.TaskRebooting, model.TaskInitializing}, Limit: 100}
	for {
		page, err := s.repo.ListTasks(ctx, q)
		if err != nil {
			return err
		}
		for _, task := range page.Items {
			at := s.now()
			if task.Execution == nil || task.Execution.Deadline == 0 || at < task.Execution.Deadline {
				continue
			}
			next := clone(task)
			next.State = model.TaskFailed
			next.Execution.Deadline = 0
			_, err := s.commit(ctx, task, next, store.NewEvent{At: at, Kind: "machine.timeout", Key: attemptKey(task, "timeout"), Fields: map[string]string{"state": string(task.State), "stage": task.Execution.Stage}}, store.Audit{})
			if err != nil && !errors.Is(err, store.ErrConflict) {
				return err
			}
		}
		if page.Next == "" {
			return nil
		}
		q.Cursor = page.Next
	}
}
