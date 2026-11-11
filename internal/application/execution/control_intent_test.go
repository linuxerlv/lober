package execution_test

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"reflect"
	"strings"
	"testing"
)

type intentBackend struct {
	ctx      context.Context
	task     model.Task
	audit    repository.Audit
	calls    []string
	err      error
	problems []model.Problem
}

func (b *intentBackend) Cancel(c context.Context, t model.Task, a repository.Audit) error {
	b.ctx = c
	b.task = t
	b.audit = a
	b.calls = append(b.calls, "cancel")
	return b.err
}
func (b *intentBackend) Retry(c context.Context, t model.Task, a repository.Audit) error {
	b.ctx = c
	b.task = t
	b.audit = a
	b.calls = append(b.calls, "retry")
	return b.err
}
func (b *intentBackend) CanCancel(t model.Task) []model.Problem {
	b.task = t
	b.calls = append(b.calls, "can-cancel")
	return b.problems
}
func (b *intentBackend) CanRetry(t model.Task) []model.Problem {
	b.task = t
	b.calls = append(b.calls, "can-retry")
	return b.problems
}
func Test审计意图桥接不改变任务控制(t *testing.T) {
	b := &intentBackend{err: errors.New("保存失败"), problems: []model.Problem{{Message: "原原因"}}}
	ctl := execution.NewAuditedControl(b)
	task := model.Task{ID: "task-1", Rev: 7}
	a := ports.AuditIntent{Actor: "admin", Action: "task.cancel", Detail: "原因", At: 99}
	ctx := t.Context()
	for _, call := range []func(context.Context, model.Task, ports.AuditIntent) error{ctl.Cancel, ctl.Retry} {
		if err := call(ctx, task, a); err != b.err || b.ctx != ctx || !reflect.DeepEqual(b.task, task) || b.audit != (repository.Audit{Actor: a.Actor, Action: a.Action, Detail: a.Detail, At: a.At}) {
			t.Fatal("委托改变了输入或错误", err)
		}
	}
	if !reflect.DeepEqual(ctl.CanCancel(task), b.problems) || !reflect.DeepEqual(ctl.CanRetry(task), b.problems) || !reflect.DeepEqual(b.calls, []string{"cancel", "retry", "can-cancel", "can-retry"}) {
		t.Fatal("控制诊断未原样委托", b.calls)
	}
}

func Test审计意图桥接保留真实保存与冲突(t *testing.T) {
	st := pgtest.OpenStore(t)
	task := fixture(t, st)
	service, err := execution.New(st, []byte(strings.Repeat("k", 32)), func() int64 { return 1000 }, nil)
	must(t, err)
	ctl := execution.NewAuditedControl(service)
	intent := ports.AuditIntent{Actor: "operator", Action: "task.cancel", Detail: "人工取消", At: 1000}
	must(t, ctl.Cancel(t.Context(), task, intent))
	if err := ctl.Cancel(t.Context(), task, intent); !errors.Is(err, repository.ErrConflict) {
		t.Fatal("旧版本未冲突", err)
	}
	canceled, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if canceled.State != model.TaskCancelled || len(ctl.CanCancel(canceled)) == 0 || len(ctl.CanRetry(canceled)) != 0 {
		t.Fatal("原状态规则改变")
	}
	intent.Action = "task.retry"
	intent.Detail = "人工重试"
	must(t, ctl.Retry(t.Context(), canceled, intent))
	latest, err := st.GetTask(t.Context(), task.ID)
	must(t, err)
	if latest.State != model.TaskPending || latest.Execution.Attempt != 2 {
		t.Fatal("重试状态或次数改变")
	}
	page, err := st.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "task", TargetID: task.ID})
	must(t, err)
	found := 0
	for _, entry := range page.Items {
		if entry.Action == "task.cancel" || entry.Action == "task.retry" {
			found++
			if entry.Actor != "operator" || entry.At != 1000 || entry.Detail == "" {
				t.Fatal("审计事实丢失")
			}
		}
	}
	if found != 2 {
		t.Fatal("重复失败产生了审计", found)
	}
}
