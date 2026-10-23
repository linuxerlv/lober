package authoring

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"maps"
	"slices"
	"strings"
)

type templates struct{ deps TemplateDeps }

func NewTemplates(d TemplateDeps) Templates {
	if d.Templates == nil || d.Hooks == nil || d.Clock == nil || d.SelectionGuard == nil {
		panic("模板编写缺少必要能力")
	}
	return &templates{deps: d}
}
func (s *templates) views(ctx context.Context, ts []model.Template) ([]TemplateView, error) {
	hooks, err := s.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys(ts))
	if err != nil {
		return nil, err
	}
	out := make([]TemplateView, len(ts))
	for i, t := range ts {
		out[i] = TemplateView{Template: cloneTemplate(t), Problems: activation.Template(t, hooks)}
	}
	return out, nil
}
func (s *templates) view(ctx context.Context, t model.Template) (TemplateView, error) {
	vs, err := s.views(ctx, []model.Template{t})
	if err != nil {
		return TemplateView{}, err
	}
	return vs[0], nil
}
func (s *templates) List(ctx context.Context, a ports.Actor, r TemplateListRequest) (TemplatePage, error) {
	if err := a.Authorize(access.PermTemplateRead); err != nil {
		return TemplatePage{}, err
	}
	if _, err := repository.CheckPageQuery(r.Cursor, r.Limit, r.Page); err != nil {
		return TemplatePage{}, err
	}
	if r.State != "" && !r.State.Valid() {
		return TemplatePage{}, repository.ErrInvalid
	}
	if r.Series != "" && !r.Series.Valid() {
		return TemplatePage{}, repository.ErrInvalid
	}
	q := repository.TemplateQuery{Series: r.Series, NamePrefix: r.Prefix, ApprovedOnly: r.Approved, Page: r.Page, Cursor: r.Cursor, Limit: r.Limit}
	if r.State != "" {
		q.States = []model.VersionState{r.State}
	}
	page, err := s.deps.Templates.ListTemplates(ctx, q)
	if err != nil {
		return TemplatePage{}, err
	}
	items, err := s.views(ctx, page.Items)
	if err != nil {
		return TemplatePage{}, err
	}
	return TemplatePage{Items: items, Next: page.Next, Total: page.Total}, nil
}
func (s *templates) Versions(ctx context.Context, a ports.Actor, id string) ([]TemplateView, error) {
	if err := a.Authorize(access.PermTemplateRead); err != nil {
		return nil, err
	}
	if err := repository.CheckID(id); err != nil {
		return nil, err
	}
	hs, err := s.deps.Templates.ListTemplateVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, hs)
}
func (s *templates) Get(ctx context.Context, a ports.Actor, k model.TemplateKey) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateRead); err != nil {
		return TemplateView{}, err
	}
	return s.get(ctx, k)
}
func (s *templates) get(ctx context.Context, k model.TemplateKey) (TemplateView, error) {
	if err := repository.CheckVersionKey(k.ID, k.Version); err != nil {
		return TemplateView{}, err
	}
	h, err := s.deps.Templates.GetTemplate(ctx, k)
	if err != nil {
		return TemplateView{}, err
	}
	return s.view(ctx, h)
}
func (s *templates) Create(ctx context.Context, a ports.Actor, h model.Template) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return TemplateView{}, err
	}
	h.Author = a.Username
	saved, err := s.deps.Templates.CreateTemplate(ctx, h)
	if err != nil {
		return TemplateView{}, err
	}
	return s.view(ctx, saved)
}
func (s *templates) SaveDraft(ctx context.Context, a ports.Actor, h model.Template) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return TemplateView{}, err
	}
	if err := checkTemplateRevision(TemplateRevision{Key: model.TemplateKey{ID: h.ID, Version: h.Version}, Rev: h.Rev}); err != nil {
		return TemplateView{}, err
	}
	current, err := s.deps.Templates.GetTemplate(ctx, model.TemplateKey{ID: h.ID, Version: h.Version})
	if err != nil {
		return TemplateView{}, err
	}
	if h.Name != "" && h.Name != current.Name {
		return TemplateView{}, repository.ErrInvalid
	}
	h.Name = current.Name
	h.Author, h.Approver, h.State, h.Builtin, h.DerivedFrom = current.Author, current.Approver, current.State, current.Builtin, current.DerivedFrom
	saved, err := s.deps.Templates.SaveTemplateDraft(ctx, h)
	if err != nil {
		return TemplateView{}, err
	}
	return s.view(ctx, saved)
}
func (s *templates) NewVersion(ctx context.Context, a ports.Actor, r VersionSource) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return TemplateView{}, err
	}
	if err := repository.CheckVersionKey(r.ID, r.From); err != nil {
		return TemplateView{}, err
	}
	h, err := s.deps.Templates.NewTemplateVersion(ctx, r.ID, r.From, a.Username)
	if err != nil {
		return TemplateView{}, err
	}
	return s.view(ctx, h)
}
func checkTemplateRevision(r TemplateRevision) error {
	if err := repository.CheckVersionKey(r.Key.ID, r.Key.Version); err != nil {
		return err
	}
	if r.Rev <= 0 {
		return repository.ErrInvalid
	}
	return nil
}
func (s *templates) DeleteDraft(ctx context.Context, a ports.Actor, r TemplateRevision) error {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return err
	}
	if err := checkTemplateRevision(r); err != nil {
		return err
	}
	return s.deps.Templates.DeleteTemplateDraft(ctx, r.Key, r.Rev)
}
func (s *templates) audit(a ports.Actor, action, detail string) repository.Audit {
	return repository.Audit{Actor: a.Username, Action: action, Detail: detail, At: s.deps.Clock()}
}
func (s *templates) Rename(ctx context.Context, a ports.Actor, r RenameRequest) error {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return err
	}
	return s.deps.Templates.RenameTemplate(ctx, r.ID, r.Name, s.audit(a, "template.rename", ""))
}
func (s *templates) Submit(ctx context.Context, a ports.Actor, r TemplateRevision) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return TemplateView{}, err
	}
	if err := checkTemplateRevision(r); err != nil {
		return TemplateView{}, err
	}
	if err := s.deps.Templates.SubmitTemplate(ctx, r.Key, r.Rev, s.audit(a, "template.submit", "")); err != nil {
		return TemplateView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *templates) Approve(ctx context.Context, a ports.Actor, r TemplateRevision) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateApprove); err != nil {
		return TemplateView{}, err
	}
	if err := checkTemplateRevision(r); err != nil {
		return TemplateView{}, err
	}
	release := s.deps.SelectionGuard.WriteLock()
	defer release()
	if err := s.deps.Templates.ApproveTemplate(ctx, r.Key, r.Rev, a.Username, s.audit(a, "template.approve", "")); err != nil {
		return TemplateView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *templates) Reject(ctx context.Context, a ports.Actor, r TemplateRejection) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateApprove); err != nil {
		return TemplateView{}, err
	}
	if strings.TrimSpace(r.Reason) == "" || !model.StorableText(r.Reason) {
		return TemplateView{}, ErrRejectReason
	}
	if err := checkTemplateRevision(r.TemplateRevision); err != nil {
		return TemplateView{}, err
	}
	if err := s.deps.Templates.RejectTemplate(ctx, r.Key, r.Rev, s.audit(a, "template.reject", r.Reason)); err != nil {
		return TemplateView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *templates) Deprecate(ctx context.Context, a ports.Actor, r TemplateRevision) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateApprove); err != nil {
		return TemplateView{}, err
	}
	if err := checkTemplateRevision(r); err != nil {
		return TemplateView{}, err
	}
	release := s.deps.SelectionGuard.WriteLock()
	defer release()
	if err := s.deps.Templates.DeprecateTemplate(ctx, r.Key, r.Rev, s.audit(a, "template.deprecate", "")); err != nil {
		return TemplateView{}, err
	}
	return s.get(ctx, r.Key)
}
func (s *templates) Validate(ctx context.Context, a ports.Actor, k model.TemplateKey) ([]model.Problem, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return nil, err
	}
	view, err := s.get(ctx, k)
	if err != nil {
		return nil, err
	}
	return view.Problems, nil
}

func (s *templates) Derive(ctx context.Context, a ports.Actor, r DeriveRequest) (TemplateView, error) {
	if err := a.Authorize(access.PermTemplateWrite); err != nil {
		return TemplateView{}, err
	}
	if err := repository.CheckVersionKey(r.From.ID, r.From.Version); err != nil {
		return TemplateView{}, err
	}
	t, err := s.deps.Templates.DeriveTemplate(ctx, r.From, r.Name, a.Username)
	if err != nil {
		return TemplateView{}, err
	}
	return s.view(ctx, t)
}

// 返回值独占可变内容，调用者转换或编辑响应不会改到仓储原值。
func cloneTemplate(t model.Template) model.Template {
	if t.DerivedFrom != nil {
		v := *t.DerivedFrom
		t.DerivedFrom = &v
	}
	t.Series = slices.Clone(t.Series)
	t.Params = slices.Clone(t.Params)
	for i := range t.Params {
		p := &t.Params[i]
		p.Options = slices.Clone(p.Options)
		if p.Min != nil {
			v := *p.Min
			p.Min = &v
		}
		if p.Max != nil {
			v := *p.Max
			p.Max = &v
		}
	}
	t.Bindings = maps.Clone(t.Bindings)
	if t.Assembly != nil {
		out := make(map[string][]model.Instance, len(t.Assembly))
		for k, instances := range t.Assembly {
			vs := slices.Clone(instances)
			for i := range vs {
				v := &vs[i]
				v.Bindings = maps.Clone(v.Bindings)
				if v.Required != nil {
					x := *v.Required
					v.Required = &x
				}
				if v.TimeoutSec != nil {
					x := *v.TimeoutSec
					v.TimeoutSec = &x
				}
			}
			out[k] = vs
		}
		t.Assembly = out
	}
	return t
}
