package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"math"
	"testing"
)

type fixture struct {
	repository.ISOs
	repository.ISOSources
	calls, reads, ranges, registered int
	source                           model.Source
	changing                         bool
	audit                            []repository.Audit
}

func (f *fixture) GetISO(context.Context, string) (model.ISO, error) {
	f.calls++
	return model.ISO{ID: "image", Family: "linux", Series: "debian", Rev: 1}, nil
}
func (f *fixture) ListISOs(context.Context, repository.ISOQuery) (repository.Page[model.ISO], error) {
	f.calls++
	return repository.Page[model.ISO]{}, nil
}
func (f *fixture) CreateISO(_ context.Context, i model.ISO, a repository.Audit) (model.ISO, error) {
	f.calls++
	f.audit = append(f.audit, a)
	i.ID = "image"
	return i, nil
}
func (f *fixture) FreeSpace(context.Context) (int64, int64, error) {
	f.calls++
	return math.MaxInt64, math.MaxInt64, nil
}
func (f *fixture) Register(_ context.Context, _ model.ISO, _, _, _ string, _ int64, _ string, a repository.Audit) error {
	f.calls++
	f.registered++
	f.audit = append(f.audit, a)
	if f.registered == 2 {
		return repository.ErrConflict
	}
	return nil
}
func (f *fixture) SourcesOf(context.Context, string) ([]model.Source, error) {
	f.calls++
	return []model.Source{f.source}, nil
}
func (f *fixture) GetSource(context.Context, string, string) (model.Source, bool, error) {
	f.calls++
	if f.changing {
		f.source.Rev++
	}
	return f.source, true, nil
}
func (f *fixture) RangesOf(context.Context, string, string) ([]model.Range, error) {
	f.calls++
	f.ranges++
	return []model.Range{{Start: 4, Length: 6}}, nil
}
func (f *fixture) Upload(_ context.Context, _, _ string, _ int64, r io.Reader, a repository.Audit) error {
	f.calls++
	f.audit = append(f.audit, a)
	_, err := io.Copy(io.Discard, r)
	return err
}
func (f *fixture) Cancel(context.Context, string, repository.Audit) error   { f.calls++; return nil }
func (f *fixture) Retry(context.Context, string, repository.Audit) error    { f.calls++; return nil }
func (f *fixture) Reimport(context.Context, string, repository.Audit) error { f.calls++; return nil }
func (f *fixture) Delete(context.Context, string, int64, repository.Audit) error {
	f.calls++
	return nil
}
func (f *fixture) Read([]byte) (int, error) { f.reads++; return 0, io.EOF }
func setup(f *fixture) Registry {
	return NewRegistry(Deps{ISOs: f, Sources: f, Control: f, Deletion: f, Clock: func() int64 { f.calls++; return 123 }})
}

var admin = ports.Actor{Username: "admin", Role: model.Role("admin")}

func operations(r Registry, f *fixture) []func(ports.Actor) error {
	return []func(ports.Actor) error{
		func(a ports.Actor) error { _, e := r.List(context.Background(), a, ListRequest{}); return e }, func(a ports.Actor) error { _, e := r.Get(context.Background(), a, "image"); return e }, func(a ports.Actor) error { _, e := r.Progress(context.Background(), a, "image"); return e },
		func(a ports.Actor) error { _, e := r.Create(context.Background(), a, CreateRequest{}); return e }, func(a ports.Actor) error { return r.RegisterSource(context.Background(), a, RegisterRequest{}) }, func(a ports.Actor) error { return r.Upload(context.Background(), a, UploadRequest{Body: f}) }, func(a ports.Actor) error { return r.Cancel(context.Background(), a, "image") }, func(a ports.Actor) error { return r.Retry(context.Background(), a, "image") }, func(a ports.Actor) error { return r.Reimport(context.Background(), a, "image") }, func(a ports.Actor) error { return r.Deprecate(context.Background(), a, RevisionRequest{}) }, func(a ports.Actor) error { return r.Delete(context.Background(), a, RevisionRequest{}) }}
}
func Test拒绝身份不读取任何能力(t *testing.T) {
	for _, a := range []ports.Actor{{}, {Username: "bad", Role: "bad"}, {Username: "admin", Role: "admin", Disabled: true}, {Username: "admin", Role: "admin", MustChangePassword: true}} {
		f := &fixture{}
		for _, op := range operations(setup(f), f) {
			if op(a) == nil {
				t.Fatal("应拒绝")
			}
		}
		if f.calls+f.reads != 0 {
			t.Fatal(f)
		}
	}
}
func Test五角色读写权限(t *testing.T) {
	for _, role := range []model.Role{"admin", "operator", "hook-author", "hook-approver", "viewer"} {
		f := &fixture{}
		r := setup(f)
		a := ports.Actor{Username: "user", Role: role}
		for i, op := range operations(r, f) {
			if i < 3 {
				if e := op(a); e != nil {
					t.Fatal(role, i, e)
				}
			} else if role != "admin" {
				before := f.calls
				if !errors.Is(op(a), ports.ErrForbidden) || before != f.calls || f.reads != 0 {
					t.Fatal(role, i)
				}
			}
		}
	}
}
func Test创建后登记失败仍返回编号(t *testing.T) {
	f := &fixture{}
	result, err := setup(f).Create(t.Context(), admin, CreateRequest{Name: "image", Family: "linux", Series: "debian", Sources: []SourceSpec{{Kind: "upload", Size: 1}, {Kind: "upload", Role: "netboot", Size: 1}}})
	if !errors.Is(err, repository.ErrConflict) || !result.Created || result.ISO.ID != "image" || len(f.audit) != 3 {
		t.Fatal(result, err, f.audit)
	}
	for _, a := range f.audit {
		if a.Action != "iso.create" || a.Detail != "" || a.At != 123 {
			t.Fatal(a)
		}
	}
}
func Test余量累加溢出先拒绝(t *testing.T) {
	f := &fixture{}
	_, err := setup(f).Create(t.Context(), admin, CreateRequest{Name: "image", Family: "linux", Series: "debian", Sources: []SourceSpec{{Kind: "upload", Size: math.MaxInt64}, {Kind: "upload", Role: "netboot", Size: 1}}})
	if !errors.Is(err, repository.ErrInvalid) || f.calls != 0 {
		t.Fatal(err, f.calls)
	}
}
