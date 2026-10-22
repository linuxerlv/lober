package authoring

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
)

// 探针只记录边界访问；保存内容与状态的测试使用真实仓储。
type hookProbe struct {
	repository.Hooks
	calls    int
	writes   int
	audit    repository.Audit
	approver string
	readErr  error
}

func (p *hookProbe) touched() error { p.calls++; return repository.ErrNotFound }
func (p *hookProbe) GetHook(context.Context, model.HookKey) (model.Hook, error) {
	p.calls++
	return model.Hook{}, p.readErr
}
func (p *hookProbe) GetHooks(context.Context, []model.HookKey) (map[model.HookKey]model.Hook, error) {
	return nil, p.touched()
}
func (p *hookProbe) ListHooks(context.Context, repository.HookQuery) (repository.Page[model.Hook], error) {
	return repository.Page[model.Hook]{}, p.touched()
}
func (p *hookProbe) ListHookVersions(context.Context, string) ([]model.Hook, error) {
	return nil, p.touched()
}
func (p *hookProbe) CreateHook(context.Context, model.Hook) (model.Hook, error) {
	return model.Hook{}, p.touched()
}
func (p *hookProbe) SaveHookDraft(context.Context, model.Hook) (model.Hook, error) {
	return model.Hook{}, p.touched()
}
func (p *hookProbe) NewHookVersion(context.Context, string, int, string) (model.Hook, error) {
	return model.Hook{}, p.touched()
}
func (p *hookProbe) DeleteHookDraft(context.Context, model.HookKey, int64) error { return p.touched() }
func (p *hookProbe) RenameHook(_ context.Context, _, _ string, a repository.Audit) error {
	p.audit = a
	return p.touched()
}
func (p *hookProbe) SubmitHook(_ context.Context, _ model.HookKey, _ int64, a repository.Audit) error {
	p.calls++
	p.writes++
	p.audit = a
	return nil
}
func (p *hookProbe) ApproveHook(_ context.Context, _ model.HookKey, _ int64, who string, a repository.Audit) error {
	p.approver = who
	p.audit = a
	return p.touched()
}
func (p *hookProbe) RejectHook(_ context.Context, _ model.HookKey, _ int64, a repository.Audit) error {
	p.audit = a
	return p.touched()
}
func (p *hookProbe) DeprecateHook(_ context.Context, _ model.HookKey, _ int64, a repository.Audit) error {
	p.audit = a
	return p.touched()
}

func Test组件应用拒绝无权调用且不碰仓储(t *testing.T) {
	key := model.HookKey{ID: "hook-1", Version: 1}
	type operation struct {
		name       string
		permission string
		call       func(Hooks, ports.Actor, bool) error
	}
	operations := []operation{
		{"list", "read", func(s Hooks, a ports.Actor, bad bool) error {
			r := ListRequest{}
			if bad {
				r.Page = -1
			}
			_, e := s.List(t.Context(), a, r)
			return e
		}},
		{"versions", "read", func(s Hooks, a ports.Actor, bad bool) error {
			id := key.ID
			if bad {
				id = ""
			}
			_, e := s.Versions(t.Context(), a, id)
			return e
		}},
		{"get", "read", func(s Hooks, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.Get(t.Context(), a, k)
			return e
		}},
		{"create", "write", func(s Hooks, a ports.Actor, bad bool) error {
			h := model.Hook{Name: "name"}
			if bad {
				h.Name = ""
			}
			_, e := s.Create(t.Context(), a, h)
			return e
		}},
		{"save", "write", func(s Hooks, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.SaveDraft(t.Context(), a, model.Hook{ID: k.ID, Version: k.Version, Rev: 1})
			return e
		}},
		{"copy", "write", func(s Hooks, a ports.Actor, bad bool) error {
			r := VersionSource{ID: key.ID, From: 1}
			if bad {
				r.From = 0
			}
			_, e := s.NewVersion(t.Context(), a, r)
			return e
		}},
		{"delete", "write", func(s Hooks, a ports.Actor, bad bool) error {
			r := HookRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			return s.DeleteDraft(t.Context(), a, r)
		}},
		{"rename", "write", func(s Hooks, a ports.Actor, bad bool) error {
			r := RenameRequest{ID: key.ID, Name: "name"}
			if bad {
				r.ID = ""
				r.Name = ""
			}
			return s.Rename(t.Context(), a, r)
		}},
		{"submit", "write", func(s Hooks, a ports.Actor, bad bool) error {
			r := HookRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Submit(t.Context(), a, r)
			return e
		}},
		{"approve", "approve", func(s Hooks, a ports.Actor, bad bool) error {
			r := HookRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Approve(t.Context(), a, r)
			return e
		}},
		{"reject", "approve", func(s Hooks, a ports.Actor, bad bool) error {
			r := HookRejection{HookRevision: HookRevision{Key: key, Rev: 1}, Reason: "fix"}
			if bad {
				r.Reason = " "
				r.Rev = 0
			}
			_, e := s.Reject(t.Context(), a, r)
			return e
		}},
		{"deprecate", "approve", func(s Hooks, a ports.Actor, bad bool) error {
			r := HookRevision{Key: key, Rev: 1}
			if bad {
				r.Rev = 0
			}
			_, e := s.Deprecate(t.Context(), a, r)
			return e
		}},
		{"validate", "write", func(s Hooks, a ports.Actor, bad bool) error {
			k := key
			if bad {
				k.Version = 0
			}
			_, e := s.Validate(t.Context(), a, k)
			return e
		}},
	}
	actors := []struct {
		name    string
		actor   ports.Actor
		invalid error
	}{
		{"viewer", ports.Actor{Username: "viewer", Role: model.RoleViewer}, nil},
		{"operator", ports.Actor{Username: "operator", Role: model.RoleOperator}, nil},
		{"author", ports.Actor{Username: "author", Role: model.RoleHookAuthor}, nil},
		{"approver", ports.Actor{Username: "approver", Role: model.RoleHookApprover}, nil},
		{"admin", ports.Actor{Username: "admin", Role: model.RoleAdmin}, nil},
		{"missing", ports.Actor{Role: model.RoleAdmin}, ports.ErrUnauthenticated},
		{"role", ports.Actor{Username: "who", Role: "bad", MustChangePassword: true}, ports.ErrUnauthenticated},
		{"disabled", ports.Actor{Username: "who", Role: model.RoleAdmin, Disabled: true, MustChangePassword: true}, ports.ErrUnauthenticated},
		{"password", ports.Actor{Username: "who", Role: model.RoleViewer, MustChangePassword: true}, ports.ErrPasswordChangeRequired},
	}
	for _, a := range actors {
		for _, op := range operations {
			for _, bad := range []bool{false, true} {
				name := a.name + "/" + op.name
				if bad {
					name += "/invalid"
				}
				t.Run(name, func(t *testing.T) {
					p := &hookProbe{}
					ticks := 0
					s := NewHooks(HookDeps{Hooks: p, Clock: func() int64 { ticks++; return 1790000000 }})
					want := a.invalid
					allowed := a.actor.Role == model.RoleAdmin || op.permission == "read" || op.permission == "write" && a.actor.Role == model.RoleHookAuthor || op.permission == "approve" && a.actor.Role == model.RoleHookApprover
					if want == nil && !allowed {
						want = ports.ErrForbidden
					}
					err := op.call(s, a.actor, bad)
					if want != nil {
						if !errors.Is(err, want) || p.calls != 0 || ticks != 0 {
							t.Fatalf("err=%v want=%v repo=%d clock=%d", err, want, p.calls, ticks)
						}
					} else {
						if errors.Is(err, ports.ErrUnauthenticated) || errors.Is(err, ports.ErrForbidden) || errors.Is(err, ports.ErrPasswordChangeRequired) {
							t.Fatal(err)
						}
						if !bad && p.calls == 0 {
							t.Fatal("有效调用未到仓储")
						}
						if op.permission == "read" || op.name == "create" || op.name == "save" || op.name == "copy" || op.name == "delete" || op.name == "validate" {
							if ticks != 0 {
								t.Fatal("无审计操作读取了时钟")
							}
						}
					}
				})
			}
		}
	}
}

func Test组件草稿保存保留身份元数据(t *testing.T) {
	st := pgtest.OpenStore(t)
	ticks := 0
	s := NewHooks(HookDeps{Hooks: st, Clock: func() int64 { ticks++; return 1790000000 }})
	a := ports.Actor{Username: "alice", Role: model.RoleHookAuthor}
	created, err := s.Create(t.Context(), a, model.Hook{Name: "draft", Author: "spoof", Description: "旧说明", TimeoutSec: 30, Required: true, Params: []model.ParamDecl{{Name: "disk", Type: "diskpath"}}, Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "# 旧正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Hook.Author != "alice" || created.Hook.State != model.VersionDraft {
		t.Fatal(created)
	}
	original := created.Hook
	in := model.Hook{ID: original.ID, Version: 1, Rev: original.Rev, Name: original.Name, Author: "spoof", Approver: "fake", Builtin: true, State: model.VersionApproved}
	saved, err := s.SaveDraft(t.Context(), a, in)
	if err != nil {
		t.Fatal(err)
	}
	h := saved.Hook
	if h.Author != "alice" || h.Approver != "" || h.Builtin || h.State != model.VersionDraft || h.Name != "draft" || h.Rev != 2 || h.Description != "" || h.TimeoutSec != 0 || h.Required || len(h.Impls) != 0 || len(h.Params) != 0 {
		t.Fatal(h)
	}
	if _, err = s.SaveDraft(t.Context(), a, in); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("旧修订=%v", err)
	}
	in.Rev = h.Rev
	in.Name = "renamed"
	if _, err = s.SaveDraft(t.Context(), a, in); !errors.Is(err, repository.ErrInvalid) {
		t.Fatalf("正文改名=%v", err)
	}
	if _, err = s.Create(t.Context(), a, model.Hook{Name: "forged", State: model.VersionApproved, Builtin: true}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatalf("伪造状态=%v", err)
	}
	if ticks != 0 {
		t.Fatal("草稿写入不应新增审计")
	}
	stored, err := st.GetHook(t.Context(), h.Key())
	if err != nil || !reflect.DeepEqual(stored, h) {
		t.Fatal(stored, err)
	}
}

type hookReadFailure struct {
	repository.Hooks
	failure       error
	reads, writes int
}

func (p *hookReadFailure) SubmitHook(ctx context.Context, k model.HookKey, rev int64, a repository.Audit) error {
	p.writes++
	return p.Hooks.SubmitHook(ctx, k, rev, a)
}
func (p *hookReadFailure) GetHook(context.Context, model.HookKey) (model.Hook, error) {
	p.reads++
	return model.Hook{}, p.failure
}
func Test组件状态写后补读失败不重试(t *testing.T) {
	st := pgtest.OpenStore(t)
	h, err := st.CreateHook(t.Context(), model.Hook{Name: "read-failure", Author: "author"})
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("补读失败")
	p := &hookReadFailure{Hooks: st, failure: failed}
	ticks := 0
	s := NewHooks(HookDeps{Hooks: p, Clock: func() int64 { ticks++; return 1790000000 }})
	got, err := s.Submit(t.Context(), ports.Actor{Username: "author", Role: model.RoleHookAuthor}, HookRevision{Key: h.Key(), Rev: h.Rev})
	if !errors.Is(err, failed) || p.writes != 1 || p.reads != 1 || ticks != 1 || !reflect.DeepEqual(got, HookView{}) {
		t.Fatalf("got=%+v err=%v writes=%d reads=%d ticks=%d", got, err, p.writes, p.reads, ticks)
	}
	stored, err := st.GetHook(t.Context(), h.Key())
	if err != nil || stored.State != model.VersionReview || stored.Rev != 2 {
		t.Fatal(stored, err)
	}
	audit, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 100})
	if err != nil || len(audit.Items) != 1 {
		t.Fatal(audit, err)
	}
	if audit.Items[0].Audit != (repository.Audit{Actor: "author", Action: "hook.submit", At: 1790000000}) {
		t.Fatal(audit.Items)
	}
}
func Test组件驳回先检查理由再检查版本和修订(t *testing.T) {
	p := &hookProbe{}
	s := NewHooks(HookDeps{Hooks: p, Clock: func() int64 { return 1790000000 }})
	a := ports.Actor{Username: "reviewer", Role: model.RoleHookApprover}
	for _, reason := range []string{" ", "bad\x00text"} {
		if _, err := s.Reject(t.Context(), a, HookRejection{Reason: reason}); !errors.Is(err, ErrRejectReason) || p.calls != 0 {
			t.Fatal(err, p.calls)
		}
	}
	reason := "  原样理由  "
	_, err := s.Reject(t.Context(), a, HookRejection{HookRevision: HookRevision{Key: model.HookKey{ID: "hook-1", Version: 1}, Rev: 1}, Reason: reason})
	if !errors.Is(err, repository.ErrNotFound) || p.audit.Detail != reason || p.audit.Actor != "reviewer" || p.audit.Action != "hook.reject" {
		t.Fatal(err, p.audit)
	}
	st := pgtest.OpenStore(t)
	real := NewHooks(HookDeps{Hooks: st, Clock: func() int64 { return 1790000000 }})
	h, err := real.Create(t.Context(), ports.Actor{Username: "author", Role: model.RoleHookAuthor}, model.Hook{Name: "reason"})
	if err != nil {
		t.Fatal(err)
	}
	h, err = real.Submit(t.Context(), ports.Actor{Username: "author", Role: model.RoleHookAuthor}, HookRevision{Key: h.Hook.Key(), Rev: h.Hook.Rev})
	if err != nil {
		t.Fatal(err)
	}
	_, err = real.Reject(t.Context(), a, HookRejection{HookRevision: HookRevision{Key: h.Hook.Key(), Rev: h.Hook.Rev}, Reason: strings.Repeat("x", 4097)})
	if !errors.Is(err, repository.ErrInvalid) || errors.Is(err, ErrRejectReason) {
		t.Fatal(err)
	}
}
func Test组件构造缺少必要能力立即拒绝(t *testing.T) {
	for _, d := range []HookDeps{{}, {Hooks: &hookProbe{}}, {Clock: func() int64 { return 1 }}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("缺少能力未拒绝")
				}
			}()
			NewHooks(d)
		}()
	}
}
