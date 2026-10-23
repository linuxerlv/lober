package authoring

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
)

type hooks struct{ deps HookDeps }

func NewHooks(d HookDeps) Hooks {
	if d.Hooks == nil || d.Clock == nil {
		panic("组件编写缺少必要能力")
	}
	return &hooks{deps: d}
}
func hookView(h model.Hook) HookView { return HookView{Hook: h, Problems: activation.Hook(h)} }
func hookViews(hs []model.Hook) []HookView {
	out := make([]HookView, len(hs))
	for i, h := range hs {
		out[i] = hookView(h)
	}
	return out
}
func (s *hooks) List(ctx context.Context, a ports.Actor, r ListRequest) (HookPage, error) {
	if err := a.Authorize(access.PermHookRead); err != nil {
		return HookPage{}, err
	}
	if _, err := repository.CheckPageQuery(r.Cursor, r.Limit, r.Page); err != nil {
		return HookPage{}, err
	}
	if r.State != "" && !r.State.Valid() {
		return HookPage{}, repository.ErrInvalid
	}
	q := repository.HookQuery{NamePrefix: r.Prefix, ApprovedOnly: r.Approved, Page: r.Page, Cursor: r.Cursor, Limit: r.Limit}
	if r.State != "" {
		q.States = []model.VersionState{r.State}
	}
	page, err := s.deps.Hooks.ListHooks(ctx, q)
	if err != nil {
		return HookPage{}, err
	}
	return HookPage{Items: hookViews(page.Items), Next: page.Next, Total: page.Total}, nil
}
func (s *hooks) Versions(ctx context.Context, a ports.Actor, id string) ([]HookView, error) {
	if err := a.Authorize(access.PermHookRead); err != nil {
		return nil, err
	}
	if err := repository.CheckID(id); err != nil {
		return nil, err
	}
	hs, err := s.deps.Hooks.ListHookVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	return hookViews(hs), nil
}
func (s *hooks) Get(ctx context.Context, a ports.Actor, k model.HookKey) (HookView, error) {
	if err := a.Authorize(access.PermHookRead); err != nil {
		return HookView{}, err
	}
	return s.get(ctx, k)
}
func (s *hooks) get(ctx context.Context, k model.HookKey) (HookView, error) {
	if err := repository.CheckVersionKey(k.ID, k.Version); err != nil {
		return HookView{}, err
	}
	h, err := s.deps.Hooks.GetHook(ctx, k)
	if err != nil {
		return HookView{}, err
	}
	return hookView(h), nil
}
func (s *hooks) Create(ctx context.Context, a ports.Actor, h model.Hook) (HookView, error) {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return HookView{}, err
	}
	h.Author = a.Username
	saved, err := s.deps.Hooks.CreateHook(ctx, h)
	if err != nil {
		return HookView{}, err
	}
	return hookView(saved), nil
}
func (s *hooks) SaveDraft(ctx context.Context, a ports.Actor, h model.Hook) (HookView, error) {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return HookView{}, err
	}
	if err := checkHookRevision(HookRevision{Key: h.Key(), Rev: h.Rev}); err != nil {
		return HookView{}, err
	}
	current, err := s.deps.Hooks.GetHook(ctx, h.Key())
	if err != nil {
		return HookView{}, err
	}
	if h.Name != "" && h.Name != current.Name {
		return HookView{}, repository.ErrInvalid
	}
	h.Name = current.Name
	h.Author, h.Approver, h.State, h.Builtin = current.Author, current.Approver, current.State, current.Builtin
	saved, err := s.deps.Hooks.SaveHookDraft(ctx, h)
	if err != nil {
		return HookView{}, err
	}
	return hookView(saved), nil
}
func (s *hooks) NewVersion(ctx context.Context, a ports.Actor, r VersionSource) (HookView, error) {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return HookView{}, err
	}
	if err := repository.CheckVersionKey(r.ID, r.From); err != nil {
		return HookView{}, err
	}
	h, err := s.deps.Hooks.NewHookVersion(ctx, r.ID, r.From, a.Username)
	if err != nil {
		return HookView{}, err
	}
	return hookView(h), nil
}
func checkHookRevision(r HookRevision) error {
	if err := repository.CheckVersionKey(r.Key.ID, r.Key.Version); err != nil {
		return err
	}
	if r.Rev <= 0 {
		return repository.ErrInvalid
	}
	return nil
}
func (s *hooks) DeleteDraft(ctx context.Context, a ports.Actor, r HookRevision) error {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return err
	}
	if err := checkHookRevision(r); err != nil {
		return err
	}
	return s.deps.Hooks.DeleteHookDraft(ctx, r.Key, r.Rev)
}
func (s *hooks) audit(a ports.Actor, action, detail string) repository.Audit {
	return repository.Audit{Actor: a.Username, Action: action, Detail: detail, At: s.deps.Clock()}
}
func (s *hooks) Rename(ctx context.Context, a ports.Actor, r RenameRequest) error {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return err
	}
	return s.deps.Hooks.RenameHook(ctx, r.ID, r.Name, s.audit(a, "hook.rename", ""))
}
func (s *hooks) Submit(ctx context.Context, a ports.Actor, r HookRevision) (HookView, error) {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return HookView{}, err
	}
	if err := checkHookRevision(r); err != nil {
		return HookView{}, err
	}
	if err := s.deps.Hooks.SubmitHook(ctx, r.Key, r.Rev, s.audit(a, "hook.submit", "")); err != nil {
		return HookView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *hooks) Approve(ctx context.Context, a ports.Actor, r HookRevision) (HookView, error) {
	if err := a.Authorize(access.PermHookApprove); err != nil {
		return HookView{}, err
	}
	if err := checkHookRevision(r); err != nil {
		return HookView{}, err
	}
	if err := s.deps.Hooks.ApproveHook(ctx, r.Key, r.Rev, a.Username, s.audit(a, "hook.approve", "")); err != nil {
		return HookView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *hooks) Reject(ctx context.Context, a ports.Actor, r HookRejection) (HookView, error) {
	if err := a.Authorize(access.PermHookApprove); err != nil {
		return HookView{}, err
	}
	if strings.TrimSpace(r.Reason) == "" || !model.StorableText(r.Reason) {
		return HookView{}, ErrRejectReason
	}
	if err := checkHookRevision(r.HookRevision); err != nil {
		return HookView{}, err
	}
	if err := s.deps.Hooks.RejectHook(ctx, r.Key, r.Rev, s.audit(a, "hook.reject", r.Reason)); err != nil {
		return HookView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *hooks) Deprecate(ctx context.Context, a ports.Actor, r HookRevision) (HookView, error) {
	if err := a.Authorize(access.PermHookApprove); err != nil {
		return HookView{}, err
	}
	if err := checkHookRevision(r); err != nil {
		return HookView{}, err
	}
	if err := s.deps.Hooks.DeprecateHook(ctx, r.Key, r.Rev, s.audit(a, "hook.deprecate", "")); err != nil {
		return HookView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *hooks) Validate(ctx context.Context, a ports.Actor, k model.HookKey) ([]model.Problem, error) {
	if err := a.Authorize(access.PermHookWrite); err != nil {
		return nil, err
	}
	view, err := s.get(ctx, k)
	if err != nil {
		return nil, err
	}
	return view.Problems, nil
}
