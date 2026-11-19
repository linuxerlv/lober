package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

type controlGetter func(context.Context, string) (model.Task, error)

func (f controlGetter) GetTask(c context.Context, id string) (model.Task, error) { return f(c, id) }

type controlBackend struct {
	calls  *[]string
	got    model.Task
	audit  ports.AuditIntent
	err    error
	commit func()
}

func (b *controlBackend) run(action string, t model.Task, a ports.AuditIntent) (model.Task, error) {
	*b.calls = append(*b.calls, action)
	b.got = t
	b.audit = a
	if b.err != nil {
		return model.Task{}, b.err
	}
	if b.commit != nil {
		b.commit()
	}
	t.Rev++
	t.State = model.TaskCancelled
	if action == "retry" {
		t.State = model.TaskPending
	}
	return t, nil
}
func (b *controlBackend) Cancel(_ context.Context, t model.Task, a ports.AuditIntent) (model.Task, error) {
	return b.run("cancel", t, a)
}
func (b *controlBackend) Retry(_ context.Context, t model.Task, a ports.AuditIntent) (model.Task, error) {
	return b.run("retry", t, a)
}
func (b *controlBackend) CanCancel(t model.Task) []model.Problem {
	*b.calls = append(*b.calls, "canCancel:"+t.ID)
	return []model.Problem{model.NewProblem("host_has_open_task", model.Subject{})}
}
func (b *controlBackend) CanRetry(t model.Task) []model.Problem {
	*b.calls = append(*b.calls, "canRetry:"+t.ID)
	return nil
}

func controlFixture() (ControllerDeps, *controlBackend, *[]string) {
	calls := []string{}
	b := &controlBackend{calls: &calls}
	d := ControllerDeps{Tasks: controlGetter(func(_ context.Context, id string) (model.Task, error) {
		calls = append(calls, "get:"+id)
		return model.Task{ID: "stored", Rev: 7, State: model.TaskFailed}, nil
	}), Backend: b, Clock: func() int64 { calls = append(calls, "clock"); return 42 }}
	return d, b, &calls
}
func Test任务控制拒绝越权且不读取(t *testing.T) {
	for _, a := range []ports.Actor{{}, {Username: "x", Role: model.RoleAdmin, Disabled: true}, {Username: "x", Role: model.RoleAdmin, MustChangePassword: true}, {Username: "x", Role: model.RoleViewer}, {Username: "x", Role: model.RoleHookAuthor}, {Username: "x", Role: model.RoleHookApprover}} {
		d, _, calls := controlFixture()
		c := NewController(d)
		want := ports.ErrForbidden
		if a.Username == "" || a.Disabled {
			want = ports.ErrUnauthenticated
		}
		if a.MustChangePassword {
			want = ports.ErrPasswordChangeRequired
		}
		for _, op := range []func(context.Context, ports.Actor, string, int64) (TaskChoice, error){c.Cancel, c.Retry} {
			if _, err := op(t.Context(), a, "raw", 7); !errors.Is(err, want) {
				t.Fatal(a, err)
			}
		}
		if len(*calls) != 0 {
			t.Fatal(*calls)
		}
	}
}
func Test任务控制不存在先于不可用(t *testing.T) {
	d, _, calls := controlFixture()
	d.Backend = NewUnavailableControl()
	d.Tasks = controlGetter(func(context.Context, string) (model.Task, error) {
		*calls = append(*calls, "get")
		return model.Task{}, repository.ErrNotFound
	})
	if _, err := NewController(d).Cancel(t.Context(), operator, "missing", 7); !errors.Is(err, repository.ErrNotFound) || !reflect.DeepEqual(*calls, []string{"get"}) {
		t.Fatal(err, *calls)
	}
	u := NewUnavailableControl()
	if u.CanCancel(model.Task{}) != nil || u.CanRetry(model.Task{}) != nil {
		t.Fatal("不可用后端伪造诊断")
	}
	for _, op := range []func(context.Context, model.Task, ports.AuditIntent) (model.Task, error){u.Cancel, u.Retry} {
		if _, err := op(t.Context(), model.Task{}, ports.AuditIntent{}); !errors.Is(err, ErrControlUnavailable) {
			t.Fatal(err)
		}
	}
}
func Test任务控制保留版本审计与顺序(t *testing.T) {
	for _, action := range []string{"cancel", "retry"} {
		for _, role := range []model.Role{model.RoleAdmin, model.RoleOperator} {
			d, b, calls := controlFixture()
			c := NewController(d)
			op := c.Cancel
			wantState := model.TaskCancelled
			if action == "retry" {
				op = c.Retry
				wantState = model.TaskPending
			}
			got, err := op(t.Context(), ports.Actor{Username: "trusted", Role: role}, "raw", 7)
			want := []string{"get:raw", "clock", action, "canCancel:stored", "canRetry:stored"}
			if err != nil || !reflect.DeepEqual(*calls, want) || b.got.Rev != 7 || b.got.State != model.TaskFailed || b.audit != (ports.AuditIntent{Actor: "trusted", Action: "task." + action, At: 42}) || got.Task.ID != "stored" || got.Task.Rev != 8 || got.Task.State != wantState || len(got.CanCancel) != 1 {
				t.Fatal(err, *calls, b.got, b.audit, got)
			}
		}
	}
}
func Test任务控制成功后直接返回已提交的任务(t *testing.T) {
	for _, retry := range []bool{false, true} {
		d, b, _ := controlFixture()
		reads, backendPublishes := 0, 0
		readErr := errors.New("read failed")
		d.Tasks = controlGetter(func(context.Context, string) (model.Task, error) {
			reads++
			if reads == 2 {
				return model.Task{}, readErr
			}
			return model.Task{ID: "stored", Rev: 7}, nil
		})
		b.commit = func() { backendPublishes++ }
		c := NewController(d)
		op := c.Cancel
		if retry {
			op = c.Retry
		}
		result, err := op(t.Context(), operator, "raw", 7)
		if err != nil || result.Task.Rev != 8 || reads != 1 || backendPublishes != 1 {
			t.Fatal(err, result, reads, backendPublishes)
		}
	}
}
func Test任务控制后端失败不重读不补发(t *testing.T) {
	for _, sentinel := range []error{repository.ErrConflict, ErrControlUnavailable, errors.New("audit failed"), &repository.ProblemsError{Kind: repository.ErrRejected, Problems: []model.Problem{model.NewProblem("host_has_open_task", model.Subject{})}}} {
		for _, retry := range []bool{false, true} {
			d, b, calls := controlFixture()
			b.err = sentinel
			c := NewController(d)
			op := c.Cancel
			action := "cancel"
			if retry {
				op = c.Retry
				action = "retry"
			}
			if _, err := op(t.Context(), operator, "raw", 7); err != sentinel || !reflect.DeepEqual(*calls, []string{"get:raw", "clock", action}) {
				t.Fatal(err, *calls)
			}
		}
	}
}
func Test任务控制提交后上下文取消仍返回结果和诊断(t *testing.T) {
	for _, retry := range []bool{false, true} {
		d, b, calls := controlFixture()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		backendPublishes := 0
		b.commit = func() { backendPublishes++; cancel() }
		c := NewController(d)
		op := c.Cancel
		if retry {
			op = c.Retry
		}
		if got, err := op(ctx, operator, "raw", 7); err != nil || got.Task.Rev != 8 || len(got.CanCancel) != 1 || backendPublishes != 1 || len(*calls) != 5 {
			t.Fatal(err, backendPublishes, *calls)
		}
	}
}
func Test任务控制缺少依赖立即拒绝(t *testing.T) {
	for _, field := range []string{"Tasks", "Backend", "Clock"} {
		d, _, _ := controlFixture()
		reflect.ValueOf(&d).Elem().FieldByName(field).SetZero()
		func() {
			defer func() {
				if recover() == nil {
					t.Error(field)
				}
			}()
			NewController(d)
		}()
	}
}
