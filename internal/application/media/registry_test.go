package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"math"
	"strings"
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

// 记录应用交给写能力的真实参数，避免只用“没有权限错误”冒充写入成功。
type adminWriteFixture struct {
	fixture
	writes        []string
	id            string
	rev           int64
	created       model.ISO
	registeredISO model.ISO
	spec          SourceSpec
	bytes         string
	offset        int64
	writeAudit    repository.Audit
}

func (f *adminWriteFixture) CreateISO(ctx context.Context, i model.ISO, a repository.Audit) (model.ISO, error) {
	f.writes = append(f.writes, "create")
	f.created = i
	f.writeAudit = a
	return f.fixture.CreateISO(ctx, i, a)
}
func (f *adminWriteFixture) Register(_ context.Context, i model.ISO, k, a, r string, n int64, d string, audit repository.Audit) error {
	f.writes = append(f.writes, "register")
	f.registeredISO = i
	f.spec = SourceSpec{Kind: k, Addr: a, Role: r, Size: n, SHA256: d}
	f.writeAudit = audit
	return nil
}
func (f *adminWriteFixture) Upload(_ context.Context, id, r string, n int64, b io.Reader, a repository.Audit) error {
	f.writes = append(f.writes, "upload")
	f.id = id
	f.spec.Role = r
	f.offset = n
	f.writeAudit = a
	data, err := io.ReadAll(b)
	f.bytes = string(data)
	return err
}
func (f *adminWriteFixture) record(op, id string, rev int64, a repository.Audit) error {
	f.writes = append(f.writes, op)
	f.id = id
	f.rev = rev
	f.writeAudit = a
	return nil
}
func (f *adminWriteFixture) Cancel(_ context.Context, id string, a repository.Audit) error {
	return f.record("cancel", id, 0, a)
}
func (f *adminWriteFixture) Retry(_ context.Context, id string, a repository.Audit) error {
	return f.record("retry", id, 0, a)
}
func (f *adminWriteFixture) Reimport(_ context.Context, id string, a repository.Audit) error {
	return f.record("reimport", id, 0, a)
}
func (f *adminWriteFixture) DeprecateISO(_ context.Context, id string, rev int64, a repository.Audit) error {
	return f.record("deprecate", id, rev, a)
}
func (f *adminWriteFixture) Delete(_ context.Context, id string, rev int64, a repository.Audit) error {
	return f.record("delete", id, rev, a)
}

func Test镜像管理员八个写入口传递有效输入(t *testing.T) {
	for _, op := range []string{"create", "register", "upload", "cancel", "retry", "reimport", "deprecate", "delete"} {
		t.Run(op, func(t *testing.T) {
			f := &adminWriteFixture{fixture: fixture{source: model.Source{ISOID: "image", Kind: model.SourceUpload, TotalSize: 4, Rev: 1}}}
			r := NewRegistry(Deps{ISOs: f, Sources: f, Control: f, Deletion: f, Clock: func() int64 { return 123 }})
			var err error
			action := "iso." + op
			want := op
			switch op {
			case "create":
				var result CreateResult
				result, err = r.Create(t.Context(), admin, CreateRequest{Name: "Debian", Family: "linux", Series: "debian", Sources: []SourceSpec{{Kind: "upload", Size: 4}}})
				if err == nil && (!result.Created || result.ISO.ID != "image" || result.ISO.Name != "Debian" || f.created.Family != "linux" || f.created.Series != "debian" || f.registeredISO.ID != "image" || f.spec.Kind != "upload" || f.spec.Size != 4) {
					t.Fatalf("创建事实不完整: result=%+v fixture=%+v", result, f)
				}
				want = "create,register"
			case "register":
				err = r.RegisterSource(t.Context(), admin, RegisterRequest{ID: "image", Source: SourceSpec{Kind: "upload", Role: "netboot", Size: 4}})
				if err == nil && (f.registeredISO.ID != "image" || f.spec.Kind != "upload" || f.spec.Role != "netboot" || f.spec.Size != 4) {
					t.Fatalf("登记参数丢失: %+v", f)
				}
				action = "iso.source.register"
			case "upload":
				err = r.Upload(t.Context(), admin, UploadRequest{ID: "image", Start: 1, End: 3, Total: 4, ContentLength: 3, Body: strings.NewReader("abc")})
				if err == nil && (f.id != "image" || f.offset != 1 || f.bytes != "abc" || f.writeAudit != (repository.Audit{})) {
					t.Fatalf("上传参数或零审计丢失: %+v", f)
				}
			case "cancel":
				err = r.Cancel(t.Context(), admin, "image")
			case "retry":
				err = r.Retry(t.Context(), admin, "image")
			case "reimport":
				err = r.Reimport(t.Context(), admin, "image")
			case "deprecate":
				err = r.Deprecate(t.Context(), admin, RevisionRequest{ID: "image", Rev: 7})
			case "delete":
				err = r.Delete(t.Context(), admin, RevisionRequest{ID: "image", Rev: 7})
			}
			if err != nil || strings.Join(f.writes, ",") != want {
				t.Fatalf("err=%v writes=%v want=%s", err, f.writes, want)
			}
			if op != "upload" && f.writeAudit != (repository.Audit{Actor: admin.Username, Action: action, At: 123}) {
				t.Fatalf("审计丢失: %+v", f.writeAudit)
			}
			if op == "cancel" || op == "retry" || op == "reimport" || op == "deprecate" || op == "delete" {
				if f.id != "image" {
					t.Fatal(f.id)
				}
			}
			if (op == "deprecate" || op == "delete") && f.rev != 7 {
				t.Fatal(f.rev)
			}
		})
	}
}
