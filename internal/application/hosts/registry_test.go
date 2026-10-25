package hosts

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// 替身只记录应用交给仓储的参数，不复制仓储校验。
type hostRepository struct {
	calls *int
	host  model.Host
	sn    string
	rev   int64
	query repository.HostQuery
	audit repository.Audit
	ctx   context.Context
	err   error
	page  repository.Page[model.Host]
}

func (p *hostRepository) touch(ctx context.Context) { (*p.calls)++; p.ctx = ctx }
func (p *hostRepository) CreateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	p.touch(ctx)
	p.host = h
	p.audit = a
	h.Rev = 1
	return h, p.err
}
func (p *hostRepository) UpdateHost(ctx context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	p.touch(ctx)
	p.host = h
	p.audit = a
	h.Rev++
	return h, p.err
}
func (p *hostRepository) DeleteHost(ctx context.Context, sn string, rev int64, a repository.Audit) error {
	p.touch(ctx)
	p.sn = sn
	p.rev = rev
	p.audit = a
	return p.err
}
func (p *hostRepository) GetHost(ctx context.Context, sn string) (model.Host, error) {
	p.touch(ctx)
	p.sn = sn
	return model.Host{SN: sn, Rev: 7}, p.err
}
func (p *hostRepository) ListHosts(ctx context.Context, q repository.HostQuery) (repository.Page[model.Host], error) {
	p.touch(ctx)
	p.query = q
	return p.page, p.err
}

func Test主机应用权限先于输入(t *testing.T) {
	operations := []struct {
		name  string
		write bool
		call  func(Registry, ports.Actor, bool) error
	}{
		{"list", false, func(s Registry, a ports.Actor, bad bool) error {
			r := ListRequest{Limit: 20}
			if bad {
				r.Page = -1
			}
			_, e := s.List(t.Context(), a, r)
			return e
		}},
		{"get", false, func(s Registry, a ports.Actor, bad bool) error {
			sn := "sn-1"
			if bad {
				sn = "ſn-1"
			}
			_, e := s.Get(t.Context(), a, sn)
			return e
		}},
		{"create", true, func(s Registry, a ports.Actor, bad bool) error {
			h := model.Host{SN: "sn-1"}
			if bad {
				h.SN = "ſn-1"
			}
			_, e := s.Create(t.Context(), a, h)
			return e
		}},
		{"update", true, func(s Registry, a ports.Actor, bad bool) error {
			r := UpdateRequest{PathSN: "sn-1", Host: model.Host{Rev: 1}}
			if bad {
				r.PathSN = "ſn-1"
				r.Host.Rev = 0
			}
			_, e := s.Update(t.Context(), a, r)
			return e
		}},
		{"delete", true, func(s Registry, a ports.Actor, bad bool) error {
			r := DeleteRequest{SN: "sn-1", Rev: 1}
			if bad {
				r.SN = "ſn-1"
				r.Rev = 0
			}
			return s.Delete(t.Context(), a, r)
		}},
	}
	actors := []struct {
		name  string
		actor ports.Actor
		err   error
	}{
		{"空用户名", ports.Actor{Role: model.RoleAdmin, MustChangePassword: true}, ports.ErrUnauthenticated},
		{"无效角色", ports.Actor{Username: "user", Role: "invalid", MustChangePassword: true}, ports.ErrUnauthenticated},
		{"已禁用", ports.Actor{Username: "user", Role: model.RoleAdmin, Disabled: true, MustChangePassword: true}, ports.ErrUnauthenticated},
		{"强制改密", ports.Actor{Username: "user", Role: model.RoleViewer, MustChangePassword: true}, ports.ErrPasswordChangeRequired},
	}
	for _, role := range []model.Role{model.RoleAdmin, model.RoleOperator, model.RoleViewer, model.RoleHookAuthor, model.RoleHookApprover} {
		actors = append(actors, struct {
			name  string
			actor ports.Actor
			err   error
		}{string(role), ports.Actor{Username: "user", Role: role}, nil})
	}
	for _, op := range operations {
		for _, ac := range actors {
			for _, bad := range []bool{false, true} {
				t.Run(op.name+"/"+ac.name+map[bool]string{false: "/合法输入", true: "/坏输入"}[bad], func(t *testing.T) {
					repoCalls, clockCalls := 0, 0
					repo := &hostRepository{calls: &repoCalls}
					s := NewRegistry(Deps{Hosts: repo, Clock: func() int64 { clockCalls++; return 123 }})
					want := ac.err
					if want == nil && op.write && ac.actor.Role != model.RoleAdmin && ac.actor.Role != model.RoleOperator {
						want = ports.ErrForbidden
					}
					err := op.call(s, ac.actor, bad)
					if want != nil {
						if !errors.Is(err, want) {
							t.Fatalf("权限错误=%v，期望%v", err, want)
						}
						if repoCalls != 0 || clockCalls != 0 {
							t.Fatal(repoCalls, clockCalls)
						}
						return
					}
					if bad {
						if err == nil || repoCalls != 0 || clockCalls != 0 {
							t.Fatal(err, repoCalls, clockCalls)
						}
						return
					}
					wantClock := 0
					if op.write {
						wantClock = 1
					}
					if err != nil || repoCalls != 1 || clockCalls != wantClock {
						t.Fatal(err, repoCalls, clockCalls)
					}
				})
			}
		}
	}
}

func Test主机应用编号版本与整份替换(t *testing.T) {
	a := ports.Actor{Username: "operator", Role: model.RoleOperator}
	repoCalls, clockCalls := 0, 0
	repo := &hostRepository{calls: &repoCalls}
	s := NewRegistry(Deps{Hosts: repo, Clock: func() int64 { clockCalls++; return 123 }})
	for _, tc := range []struct {
		name    string
		request UpdateRequest
		problem bool
	}{
		{"先查版本", UpdateRequest{PathSN: "ſn-1", Host: model.Host{Rev: 0, SN: "OTHER"}}, false},
		{"再查路径", UpdateRequest{PathSN: "ſn-1", Host: model.Host{Rev: 1, SN: "OTHER"}}, true},
		{"再查正文", UpdateRequest{PathSN: "SN-1", Host: model.Host{Rev: 1, SN: "ſn-1"}}, true},
		{"不许改编号", UpdateRequest{PathSN: "SN-1", Host: model.Host{Rev: 1, SN: "OTHER"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Update(t.Context(), a, tc.request)
			var ps model.Problem
			if tc.problem {
				if !errors.As(err, &ps) || ps.Code != "sn_invalid" {
					t.Fatal(err)
				}
			} else if err != repository.ErrInvalid {
				t.Fatal(err)
			}
			if repoCalls != 0 || clockCalls != 0 {
				t.Fatal(repoCalls, clockCalls)
			}
		})
	}
	if err := s.Delete(t.Context(), a, DeleteRequest{SN: "ſn-1", Rev: 0}); err != repository.ErrInvalid {
		t.Fatal(err)
	}
	if repoCalls != 0 || clockCalls != 0 {
		t.Fatal(repoCalls, clockCalls)
	}
	for _, rev := range []int64{-9, 0, 77} {
		h, err := s.Create(t.Context(), a, model.Host{SN: " sn-1 ", Rev: rev, Attrs: map[string]string{"rack": " A01 "}})
		if err != nil || h.SN != "SN-1" || h.Rev != 1 || repo.host.Attrs["rack"] != " A01 " {
			t.Fatal(h, err)
		}
		if repo.audit != (repository.Audit{Actor: "operator", Action: "host.create", At: 123}) {
			t.Fatal(repo.audit)
		}
	}
	for _, sn := range []string{"", " sn-1 "} {
		h, err := s.Update(t.Context(), a, UpdateRequest{PathSN: " sn-1 ", Host: model.Host{SN: sn, Rev: 7}})
		if err != nil || h.Rev != 8 || !reflect.DeepEqual(repo.host, model.Host{SN: "SN-1", Rev: 7}) {
			t.Fatal(h, repo.host, err)
		}
		if repo.audit != (repository.Audit{Actor: "operator", Action: "host.update", At: 123}) {
			t.Fatal(repo.audit)
		}
	}
	if err := s.Delete(t.Context(), a, DeleteRequest{SN: " sn-1 ", Rev: 8}); err != nil || repo.sn != "SN-1" || repo.rev != 8 || repo.audit != (repository.Audit{Actor: "operator", Action: "host.delete", At: 123}) {
		t.Fatal(repo, err)
	}
	if repoCalls != 6 || clockCalls != 6 {
		t.Fatal(repoCalls, clockCalls)
	}
	if _, err := s.Get(t.Context(), a, " sn-1 "); err != nil || repo.sn != "SN-1" || clockCalls != 6 {
		t.Fatal(repo, err)
	}
	total := int64(3)
	repo.page = repository.Page[model.Host]{Items: []model.Host{{SN: "SN-10", Rev: 2}}, Next: "opaque", Total: &total}
	page, err := s.List(t.Context(), a, ListRequest{Prefix: "sn-1", AttrKey: "invalid-key", AttrValue: " A01 ", Cursor: "opaque", Limit: 2})
	if err != nil || !reflect.DeepEqual(page, HostPage{Items: []model.Host{{SN: "SN-10", Rev: 2}}, Next: "opaque", Total: &total}) || repo.query != (repository.HostQuery{SNPrefix: "sn-1", AttrKey: "invalid-key", AttrValue: " A01 ", Cursor: "opaque", Limit: 2}) || clockCalls != 6 {
		t.Fatal(page, repo.query, err)
	}
	for _, errWant := range []error{repository.ErrInvalid, repository.ErrNotFound, repository.ErrConflict, repository.ErrReferenced, errors.New("仓储内部错误")} {
		repo.err = errWant
		for _, call := range []func() error{
			func() error { _, e := s.List(t.Context(), a, ListRequest{}); return e },
			func() error { _, e := s.Get(t.Context(), a, "SN-1"); return e },
			func() error { _, e := s.Create(t.Context(), a, model.Host{SN: "SN-1", Arch: "bad"}); return e },
			func() error {
				_, e := s.Update(t.Context(), a, UpdateRequest{PathSN: "SN-1", Host: model.Host{Rev: 1, Arch: "bad"}})
				return e
			},
			func() error { return s.Delete(t.Context(), a, DeleteRequest{SN: "SN-1", Rev: 1}) },
		} {
			if got := call(); got != errWant {
				t.Fatalf("错误被改写：%v != %v", got, errWant)
			}
			if repo.ctx != t.Context() {
				t.Fatal("请求上下文被替换")
			}
		}
	}
}

func Test主机应用查询检查不访问仓储(t *testing.T) {
	repoCalls, clockCalls := 0, 0
	repo := &hostRepository{calls: &repoCalls}
	s := NewRegistry(Deps{Hosts: repo, Clock: func() int64 { clockCalls++; return 123 }})
	a := ports.Actor{Username: "viewer", Role: model.RoleViewer}
	for _, r := range []ListRequest{{Page: -1}, {Page: 1, Cursor: "opaque"}, {Limit: 501}, {AttrKey: "rack"}, {AttrValue: " A01 "}} {
		if _, err := s.List(t.Context(), a, r); !errors.Is(err, repository.ErrInvalid) {
			t.Fatal(r, err)
		}
		if repoCalls != 0 || clockCalls != 0 {
			t.Fatal(repoCalls, clockCalls)
		}
	}
}

func Test主机应用缺少依赖立即拒绝(t *testing.T) {
	for _, d := range []Deps{{}, {Hosts: &hostRepository{calls: new(int)}}, {Clock: func() int64 { return 1 }}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("缺少依赖没有拒绝")
				}
			}()
			NewRegistry(d)
		}()
	}
}
