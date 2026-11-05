package authoring

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

func (p *templateProbe) NewTemplateVersion(_ context.Context, _ string, _ int, _ string) (model.Template, error) {
	p.writes++
	return p.item, p.writeErr
}
func (p *templateProbe) SubmitTemplate(ctx context.Context, _ model.TemplateKey, _ int64, a repository.Audit) error {
	return p.write(ctx, a)
}

func TestHistoricalTemplateRemainsReadableButCannotBeReused(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos", "removed"} {
		for _, op := range []string{"get", "create", "save", "copy", "derive", "submit", "approve"} {
			t.Run(string(series)+"/"+op, func(t *testing.T) {
				item := model.Template{ID: "old-template", Version: 1, Rev: 1, Name: "old", Family: "linux", Series: []catalog.Series{"rocky", series}, State: model.VersionDraft, Author: "author"}
				p := &templateProbe{item: item}
				events := []string{}
				s := NewTemplates(TemplateDeps{Templates: p, Hooks: templateHookProbe{}, Clock: func() int64 { return 100 }, SelectionGuard: templateGuardProbe{&events}})
				actor := ports.Actor{Username: "operator", Role: model.RoleAdmin}
				revision := TemplateRevision{Key: model.TemplateKey{ID: item.ID, Version: 1}, Rev: 1}
				var err error
				switch op {
				case "get":
					_, err = s.Get(t.Context(), actor, revision.Key)
				case "create":
					item.ID = ""
					item.Version = 0
					item.Rev = 0
					item.State = ""
					_, err = s.Create(t.Context(), actor, item)
				case "save":
					_, err = s.SaveDraft(t.Context(), actor, item)
				case "copy":
					_, err = s.NewVersion(t.Context(), actor, VersionSource{ID: item.ID, From: 1})
				case "derive":
					_, err = s.Derive(t.Context(), actor, DeriveRequest{From: revision.Key, Name: "new"})
				case "submit":
					_, err = s.Submit(t.Context(), actor, revision)
				case "approve":
					_, err = s.Approve(t.Context(), actor, revision)
				}
				if op == "get" {
					if err != nil {
						t.Fatal("历史记录不可读", err)
					}
					return
				}
				if !errors.Is(err, repository.ErrRejected) || p.writes != 0 {
					t.Fatalf("旧模板入口 %s 未在写入前拒绝：%v，写入 %d 次", op, err, p.writes)
				}
			})
		}
	}
}

type scopeHookStore struct {
	repository.Hooks
	item   model.Hook
	writes int
}

func (p *scopeHookStore) GetHook(context.Context, model.HookKey) (model.Hook, error) {
	return p.item, nil
}
func (p *scopeHookStore) CreateHook(_ context.Context, h model.Hook) (model.Hook, error) {
	p.writes++
	return h, nil
}
func (p *scopeHookStore) SaveHookDraft(_ context.Context, h model.Hook) (model.Hook, error) {
	p.writes++
	return h, nil
}
func (p *scopeHookStore) NewHookVersion(context.Context, string, int, string) (model.Hook, error) {
	p.writes++
	return p.item, nil
}
func (p *scopeHookStore) SubmitHook(context.Context, model.HookKey, int64, repository.Audit) error {
	p.writes++
	return nil
}
func (p *scopeHookStore) ApproveHook(context.Context, model.HookKey, int64, string, repository.Audit) error {
	p.writes++
	return nil
}
func TestHistoricalHookRemainsReadableButCannotBeReused(t *testing.T) {
	for _, op := range []string{"get", "create", "save", "copy", "submit", "approve"} {
		t.Run(op, func(t *testing.T) {
			item := model.Hook{ID: "old-hook", Name: "old", Version: 1, Rev: 1, State: model.VersionDraft, Author: "author", Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky", "openeuler"}, Kind: model.ImplScript}}}
			p := &scopeHookStore{item: item}
			s := NewHooks(HookDeps{Hooks: p, Clock: func() int64 { return 100 }})
			actor := ports.Actor{Username: "operator", Role: model.RoleAdmin}
			revision := HookRevision{Key: item.Key(), Rev: 1}
			var err error
			switch op {
			case "get":
				_, err = s.Get(t.Context(), actor, item.Key())
			case "create":
				_, err = s.Create(t.Context(), actor, item)
			case "save":
				_, err = s.SaveDraft(t.Context(), actor, item)
			case "copy":
				_, err = s.NewVersion(t.Context(), actor, VersionSource{ID: item.ID, From: 1})
			case "submit":
				_, err = s.Submit(t.Context(), actor, revision)
			case "approve":
				_, err = s.Approve(t.Context(), actor, revision)
			}
			if op == "get" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, repository.ErrRejected) || p.writes != 0 {
				t.Fatal("旧组件仍可继续使用", op, err, p.writes)
			}
		})
	}
}
