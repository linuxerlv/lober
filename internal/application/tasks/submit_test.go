package tasks

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test应用提交整批失败不写入不通知(t *testing.T) {
	for _, hosts := range [][]SubmitHost{
		{{SN: "SN-1"}, {SN: "SN-2", Entered: map[string]string{"tpl/hostname": "bad name"}}},
		{{SN: "SN-1"}, {SN: "sn-1"}}, {{SN: "SN-1"}, {SN: "MISSING"}},
	} {
		f, d, s, n := applicationFixture()
		_, err := NewWizard(d).Submit(t.Context(), operator, SubmitRequest{Selection: s, Hosts: hosts})
		var batch *repository.BatchError
		if !errors.As(err, &batch) || f.created != nil || len(n.published()) != 0 {
			t.Fatal(err, f.created, n.published())
		}
	}
}

type diagnostics struct{ calls []string }

func (d *diagnostics) CanCancel(t model.Task) []model.Problem {
	d.calls = append(d.calls, "cancel:"+t.ID)
	return []model.Problem{model.NewProblem("host_has_open_task", model.Subject{Kind: model.KindHost, ID: t.HostSN})}
}
func (d *diagnostics) CanRetry(t model.Task) []model.Problem {
	d.calls = append(d.calls, "retry:"+t.ID)
	return nil
}
func Test应用提交顺序审计时间与诊断(t *testing.T) {
	f, d, s, n := applicationFixture()
	at := int64(1790000000)
	d.Clock = func() int64 { at++; return at }
	diag := &diagnostics{}
	d.Diagnostics = diag
	got, err := NewWizard(d).Submit(t.Context(), operator, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: " sn-2 ", Entered: map[string]string{"tpl/hostname": "node-two"}}, {SN: "SN-1", Entered: map[string]string{"tpl/hostname": "node-one"}}}})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for i, want := range []struct{ sn, value string }{{"SN-2", "node-two"}, {"SN-1", "node-one"}} {
		task := got[i].Task
		if task.HostSN != want.sn || task.Snapshot.Template.Params["hostname"].Items[0] != want.value || task.CreatedBy != operator.Username || task.CreatedAt != 1790000001+int64(i) || len(got[i].CanCancel) != 1 {
			t.Fatal(got)
		}
	}
	if f.audit.Actor != operator.Username || f.audit.Action != "task.create" || f.audit.Detail != "" || f.audit.At != 1790000003 || !reflect.DeepEqual(n.published(), []string{"SN-2", "SN-1"}) {
		t.Fatal(f.audit, n.published())
	}
	if !reflect.DeepEqual(diag.calls, []string{"cancel:SN-2", "retry:SN-2", "cancel:SN-1", "retry:SN-1"}) {
		t.Fatal(diag.calls)
	}
}
func Test应用提交成功后才通知(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "成功", true: "事务失败"}[fail], func(t *testing.T) {
			f, d, s, n := applicationFixture()
			reached, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			f.create = func(ctx context.Context, ts []model.Task, a repository.Audit) ([]model.Task, error) {
				close(reached)
				<-release
				if fail {
					return nil, repository.ErrConflict
				}
				for i := range ts {
					ts[i].ID = ts[i].HostSN
				}
				return ts, nil
			}
			done := make(chan error, 1)
			go func() {
				_, e := NewWizard(d).Submit(t.Context(), operator, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: "SN-1"}, {SN: "SN-2"}}})
				done <- e
			}()
			select {
			case <-reached:
			case e := <-done:
				t.Fatal("未进入事务", e)
			case <-time.After(5 * time.Second):
				t.Fatal("未进入事务")
			}
			if len(n.published()) != 0 {
				t.Fatal("提交前发出通知")
			}
			release <- struct{}{}
			select {
			case e := <-done:
				if fail && !errors.Is(e, repository.ErrConflict) || !fail && e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("未完成")
			}
			want := 2
			if fail {
				want = 0
			}
			if len(n.published()) != want {
				t.Fatal(n.published())
			}
		})
	}
}
func Test应用提交取消不提前通知(t *testing.T) {
	f, d, s, n := applicationFixture()
	reached := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.create = func(ctx context.Context, ts []model.Task, a repository.Audit) ([]model.Task, error) {
		close(reached)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, e := NewWizard(d).Submit(ctx, operator, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: "SN-1"}}})
		done <- e
	}()
	select {
	case <-reached:
	case e := <-done:
		t.Fatal("未进入事务", e)
	case <-time.After(5 * time.Second):
		t.Fatal("未进入事务")
	}
	if len(n.published()) != 0 {
		t.Fatal(n.published())
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消未完成")
	}
	if len(n.published()) != 0 {
		t.Fatal(n.published())
	}
}

func Test应用事务已成功即使随后取消也通知(t *testing.T) {
	f, d, s, n := applicationFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.create = func(_ context.Context, ts []model.Task, _ repository.Audit) ([]model.Task, error) {
		ts[0].ID = "committed"
		cancel()
		return ts, nil
	}
	_, err := NewWizard(d).Submit(ctx, operator, SubmitRequest{Selection: s, Hosts: []SubmitHost{{SN: "SN-1"}}})
	if err != nil || !reflect.DeepEqual(n.published(), []string{"committed"}) {
		t.Fatal(err, n.published())
	}
}
