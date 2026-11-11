package hosts

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"reflect"
	"testing"
)

func (p *hostRepository) GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error) {
	p.touch(ctx)
	return nil, p.err
}
func (p *hostRepository) ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error) {
	p.touch(ctx)
	return hs, p.err
}

type importRepository struct {
	*hostRepository
	readCalls, writeCalls int
	sns                   []string
	batch                 []model.Host
	result                []model.Host
	known                 map[string]model.Host
	readErr, writeErr     error
}

func (p *importRepository) GetHosts(ctx context.Context, sns []string) (map[string]model.Host, error) {
	p.readCalls++
	p.ctx = ctx
	p.sns = sns
	return p.known, p.readErr
}
func (p *importRepository) ImportHosts(ctx context.Context, hs []model.Host, a repository.Audit) ([]model.Host, error) {
	p.writeCalls++
	p.ctx = ctx
	p.batch = hs
	p.audit = a
	if p.result != nil {
		return p.result, p.writeErr
	}
	return hs, p.writeErr
}
func importFixture() (*importRepository, Registry, *int) {
	p := &importRepository{hostRepository: &hostRepository{calls: new(int)}}
	n := new(int)
	return p, NewRegistry(Deps{Hosts: p, Clock: func() int64 { *n++; return 123 }}), n
}

var importActor = ports.Actor{Username: "operator", Role: model.RoleOperator}

func Test主机导入授权先于CSV(t *testing.T) {
	cases := []struct {
		actor ports.Actor
		want  error
	}{
		{ports.Actor{}, ports.ErrUnauthenticated},
		{ports.Actor{Username: "bad", Role: "invalid"}, ports.ErrUnauthenticated},
		{ports.Actor{Username: "disabled", Role: model.RoleAdmin, Disabled: true}, ports.ErrUnauthenticated},
		{ports.Actor{Username: "viewer", Role: model.RoleViewer}, ports.ErrForbidden},
		{ports.Actor{Username: "author", Role: model.RoleHookAuthor}, ports.ErrForbidden},
		{ports.Actor{Username: "approver", Role: model.RoleHookApprover}, ports.ErrForbidden},
		{ports.Actor{Username: "operator", Role: model.RoleOperator, MustChangePassword: true}, ports.ErrPasswordChangeRequired},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			p, s, n := importFixture()
			_, err := s.Import(t.Context(), tc.actor, ImportRequest{CSV: []byte("broken"), DryRun: dry})
			if !errors.Is(err, tc.want) || p.readCalls != 0 || p.writeCalls != 0 || *n != 0 {
				t.Fatal(err, tc.want, p, *n)
			}
		}
	}
}
func Test主机导入文件错误不读库(t *testing.T) {
	p, s, n := importFixture()
	_, err := s.Import(t.Context(), importActor, ImportRequest{CSV: []byte("sn,arch\nSN-1,x86_64\nSN-2\n")})
	var file *CSVFileError
	if !errors.As(err, &file) || file.Message == "" || p.readCalls != 0 || p.writeCalls != 0 || *n != 0 {
		t.Fatal(err, p, *n)
	}
}
func Test主机导入读取先于预演和拒绝(t *testing.T) {
	for _, dry := range []bool{true, false} {
		for _, csv := range []string{"sn,arch\nSN-1,wrong\n", "sn\nSN-1\n"} {
			p, s, n := importFixture()
			want := errors.New("read failed")
			p.readErr = want
			_, err := s.Import(t.Context(), importActor, ImportRequest{CSV: []byte(csv), DryRun: dry})
			if err != want || p.readCalls != 1 || p.writeCalls != 0 || *n != 0 || p.ctx != t.Context() {
				t.Fatal(err, p, *n)
			}
			if csv == "sn,arch\nSN-1,wrong\n" && len(p.sns) != 0 {
				t.Fatal(p.sns)
			}
		}
	}
}
func Test主机导入计划与整批可信审计(t *testing.T) {
	p, s, n := importFixture()
	p.known = map[string]model.Host{"SN-1": {SN: "SN-1", Rev: 3}}
	csv := []byte("sn,rack\nSN-2, A01 \nSN-1,new\n")
	r, err := s.Import(t.Context(), importActor, ImportRequest{CSV: csv, DryRun: true})
	if err != nil || r.Plan.Rows[0].Action != "create" || r.Plan.Rows[1].Action != "update" || p.readCalls != 1 || p.writeCalls != 0 || *n != 0 || !reflect.DeepEqual(p.sns, []string{"SN-2", "SN-1"}) {
		t.Fatal(r, err, p, *n)
	}
	p.result = []model.Host{{SN: "SN-2", Rev: 1, Attrs: map[string]string{"rack": " A01 "}}, {SN: "SN-1", Rev: 4, Attrs: map[string]string{"rack": "new"}}}
	r, err = s.Import(t.Context(), importActor, ImportRequest{CSV: csv})
	if err != nil || p.readCalls != 2 || p.writeCalls != 1 || *n != 1 || p.audit != (repository.Audit{Actor: "operator", Action: "host.import", At: 123}) || p.ctx != t.Context() || !reflect.DeepEqual(r.Hosts, p.result) || r.Hosts[0].SN != "SN-2" || r.Hosts[1].SN != "SN-1" || p.batch[0].SN != "SN-2" || p.batch[1].SN != "SN-1" || r.Hosts[0].Attrs["rack"] != " A01 " {
		t.Fatal(r, err, p, *n)
	}
	p.writeErr = errors.New("write failed")
	if _, err = s.Import(t.Context(), importActor, ImportRequest{CSV: csv}); err != p.writeErr {
		t.Fatal(err)
	}
}
func Test主机导入拒绝保持完整计划(t *testing.T) {
	p, s, n := importFixture()
	csv := []byte("sn,arch\nSN-1,wrong\nSN-2,x86_64\n")
	preview, err := s.Import(t.Context(), importActor, ImportRequest{CSV: csv, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Import(t.Context(), importActor, ImportRequest{CSV: csv})
	var rejected *ImportRejectedError
	if !errors.As(err, &rejected) || !reflect.DeepEqual(preview.Plan, rejected.Plan) || rejected.Message != preview.Plan.Rows[0].Problems[0].Message || p.readCalls != 2 || p.writeCalls != 0 || *n != 0 {
		t.Fatal(err, p, *n)
	}
}
