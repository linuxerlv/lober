package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

type readStore struct {
	calls []string
	query repository.TaskQuery
	page  repository.Page[model.Task]
	task  model.Task
	err   error
}

func (s *readStore) ListTasks(_ context.Context, q repository.TaskQuery) (repository.Page[model.Task], error) {
	s.calls = append(s.calls, "list")
	s.query = q
	return s.page, s.err
}
func (s *readStore) GetTask(_ context.Context, id string) (model.Task, error) {
	s.calls = append(s.calls, "get:"+id)
	return s.task, s.err
}

type readRenderer struct {
	calls    int
	snapshot model.Snapshot
	firmware catalog.Firmware
	id       string
	result   PreviewResult
	err      error
}

func (r *readRenderer) RenderFrozen(_ context.Context, s model.Snapshot, f catalog.Firmware, id string) ([]ports.PreviewFile, []string, []model.Problem, error) {
	r.calls++
	r.snapshot = s
	r.firmware = f
	r.id = id
	return r.result.Files, r.result.Sample, r.result.Problems, r.err
}
func readFixture() (*readStore, *readRenderer, ReaderDeps) {
	s := &readStore{}
	r := &readRenderer{}
	return s, r, ReaderDeps{Tasks: s, Renderer: r}
}
func Test任务查询保留错误先后(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ListRequest
		want error
		code model.Code
	}{
		{"分页先于时间", ListRequest{PageRequest: PageRequest{Limit: 501}, CreatedFrom: "bad"}, repository.ErrInvalid, ""},
		{"起始先于结束", ListRequest{CreatedFrom: "bad", CreatedTo: "-1"}, ErrBadRequest, ""},
		{"范围先于编号", ListRequest{CreatedFrom: "3", CreatedTo: "2", HostSN: "非法"}, ErrBadRequest, ""},
		{"编号先于状态", ListRequest{HostSN: "非法", State: "bad"}, nil, "sn_invalid"},
		{"状态", ListRequest{State: "bad"}, ErrBadRequest, ""},
		{"时间拒绝空白", ListRequest{CreatedFrom: " 1"}, ErrBadRequest, ""},
		{"时间拒绝负数", ListRequest{CreatedTo: "-1"}, ErrBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, d := readFixture()
			_, err := NewReader(d).List(t.Context(), operator, tc.req)
			if tc.code != "" {
				var p model.Problem
				if !errors.As(err, &p) || p.Code != tc.code {
					t.Fatal(err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
			if len(s.calls) != 0 {
				t.Fatal(s.calls)
			}
		})
	}
}
func Test任务查询不能绕过权限(t *testing.T) {
	for _, a := range []ports.Actor{{}, {Username: "x", Role: model.RoleAdmin, Disabled: true}, {Username: "x", Role: model.RoleAdmin, MustChangePassword: true}, {Username: "x", Role: "invalid"}} {
		s, r, d := readFixture()
		reader := NewReader(d)
		want := ports.ErrUnauthenticated
		if a.MustChangePassword {
			want = ports.ErrPasswordChangeRequired
		}
		for _, err := range []error{func() error { _, e := reader.List(t.Context(), a, ListRequest{CreatedFrom: "bad"}); return e }(), func() error { _, e := reader.Get(t.Context(), a, "not-an-id"); return e }(), func() error {
			_, e := reader.Preview(t.Context(), a, "missing", PreviewRequest{Firmware: "bad"})
			return e
		}()} {
			if !errors.Is(err, want) {
				t.Fatal(a, err)
			}
		}
		if len(s.calls) != 0 || r.calls != 0 {
			t.Fatal(s.calls, r.calls)
		}
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleOperator, model.RoleHookAuthor, model.RoleHookApprover, model.RoleViewer} {
		s, _, d := readFixture()
		a := ports.Actor{Username: "x", Role: role}
		reader := NewReader(d)
		if _, e := reader.List(t.Context(), a, ListRequest{}); e != nil {
			t.Fatal(role, e)
		}
		if _, e := reader.Get(t.Context(), a, "not-an-id"); e != nil {
			t.Fatal(role, e)
		}
		if _, e := reader.Preview(t.Context(), a, "x", PreviewRequest{}); e != nil {
			t.Fatal(role, e)
		}
		if !reflect.DeepEqual(s.calls, []string{"list", "get:not-an-id", "get:x"}) {
			t.Fatal(role, s.calls)
		}
	}
}
func Test任务列表保留分页顺序与诊断(t *testing.T) {
	s, _, d := readFixture()
	total := int64(4)
	s.page = repository.Page[model.Task]{Items: []model.Task{{ID: "b"}, {ID: "a"}}, Next: "next", Total: &total}
	diag := &diagnostics{}
	d.Diagnostics = diag
	got, e := NewReader(d).List(t.Context(), operator, ListRequest{PageRequest: PageRequest{Page: 2, Limit: 2}, HostSN: " sn-1 ", State: "pending", CreatedFrom: "+1", CreatedTo: "20"})
	if e != nil || got.Next != "next" || got.Total != &total || len(got.Items) != 2 || got.Items[0].Task.ID != "b" || got.Items[1].Task.ID != "a" || len(got.Items[0].CanCancel) != 1 {
		t.Fatal(got, e)
	}
	if !reflect.DeepEqual(s.query, repository.TaskQuery{Page: 2, Limit: 2, HostSN: "SN-1", States: []model.TaskState{model.TaskPending}, CreatedFrom: 1, CreatedTo: 20}) || !reflect.DeepEqual(diag.calls, []string{"cancel:b", "retry:b", "cancel:a", "retry:a"}) {
		t.Fatal(s.query, diag.calls)
	}
	s.page = repository.Page[model.Task]{}
	got, e = NewReader(d).List(t.Context(), operator, ListRequest{})
	if e != nil || got.Items == nil || len(got.Items) != 0 {
		t.Fatal(got, e)
	}
	s.task = model.Task{ID: "stored", Snapshot: model.Snapshot{Template: model.SnapshotTemplate{Body: "frozen"}}}
	d.Diagnostics = nil
	one, e := NewReader(d).Get(t.Context(), operator, "raw-id")
	if e != nil || !reflect.DeepEqual(one.Task, s.task) || one.CanCancel == nil || one.CanRetry == nil {
		t.Fatal(one, e)
	}
	sentinel := errors.New("storage")
	s.err = sentinel
	if _, e = NewReader(d).Get(t.Context(), operator, "missing"); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	if _, e = NewReader(d).List(t.Context(), operator, ListRequest{}); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
}
func Test任务预览固件先于查任务(t *testing.T) {
	s, r, d := readFixture()
	s.err = repository.ErrNotFound
	_, e := NewReader(d).Preview(t.Context(), operator, "missing", PreviewRequest{Firmware: "bad"})
	if !errors.Is(e, ErrBadRequest) || len(s.calls) != 0 || r.calls != 0 {
		t.Fatal(e, s.calls, r.calls)
	}
}
func Test任务预览仅传冻结快照与样例标签(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		requested, saved, want catalog.Firmware
		sample                 []string
	}{{"请求覆盖", "bios", "uefi", "bios", []string{"credential"}}, {"快照回退", "", "bios", "bios", []string{"credential"}}, {"空值样例", "", "", "uefi", []string{"credential", "firmware"}}} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, d := readFixture()
			s.task = model.Task{ID: "actual-id", Snapshot: model.Snapshot{Host: model.SnapshotHost{SN: "FROZEN", Firmware: tc.saved}, Template: model.SnapshotTemplate{Body: "frozen"}}}
			r.result = PreviewResult{Files: []ports.PreviewFile{{Name: "answer", Body: "frozen"}}, Sample: []string{"credential"}}
			got, e := NewReader(d).Preview(t.Context(), operator, "lookup-id", PreviewRequest{Firmware: tc.requested})
			if e != nil || !reflect.DeepEqual(s.calls, []string{"get:lookup-id"}) || r.id != "actual-id" || r.firmware != tc.want || !reflect.DeepEqual(r.snapshot, s.task.Snapshot) || !reflect.DeepEqual(got.Sample, tc.sample) || len(got.Files) != 1 || !got.Files[0].Sample {
				t.Fatal(got, e, r)
			}
		})
	}
	s, r, d := readFixture()
	ps := []model.Problem{model.NewProblem("arch_mismatch", model.Subject{}, "x86_64", "aarch64"), model.NewProblem("host_has_open_task", model.Subject{})}
	r.result = PreviewResult{Problems: ps, Sample: []string{"renderer"}, Files: []ports.PreviewFile{{Name: "raw"}}}
	got, e := NewReader(d).Preview(t.Context(), operator, "x", PreviewRequest{})
	if e != nil || !reflect.DeepEqual(got.Problems, ps) || !reflect.DeepEqual(got.Sample, []string{"renderer"}) || got.Files[0].Sample {
		t.Fatal(got, e)
	}
	sentinel := errors.New("render")
	r.err = sentinel
	got, e = NewReader(d).Preview(t.Context(), operator, "x", PreviewRequest{})
	if !errors.Is(e, sentinel) || !reflect.DeepEqual(got, PreviewResult{}) {
		t.Fatal(got, e)
	}
	s.err = repository.ErrNotFound
	before := r.calls
	if _, e = NewReader(d).Preview(t.Context(), operator, "missing", PreviewRequest{}); !errors.Is(e, repository.ErrNotFound) || r.calls != before {
		t.Fatal(e, r.calls)
	}
}
func Test任务读取缺少必要能力立即拒绝(t *testing.T) {
	for _, d := range []ReaderDeps{{}, {Tasks: &readStore{}}, {Renderer: &readRenderer{}}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("缺少必要依赖未拒绝")
				}
			}()
			NewReader(d)
		}()
	}
}
