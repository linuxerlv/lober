package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"slices"
)

type wizard struct{ deps WizardDeps }

// NewWizard 只接收显式注入的能力，不回退到旧网页仓储入口。
func NewWizard(d WizardDeps) Wizard {
	if d.Hosts == nil || d.ISOs == nil || d.Templates == nil || d.Hooks == nil || d.Tasks == nil || d.Clock == nil || d.Notifier == nil || d.SelectionGuard == nil {
		panic("任务向导缺少必要能力")
	}
	return &wizard{deps: d}
}
func authorize(actor ports.Actor) error {
	if actor.Username == "" || actor.Disabled || !actor.Role.Valid() {
		return ErrUnauthenticated
	}
	if actor.MustChangePassword {
		return ErrPasswordChangeRequired
	}
	if !slices.Contains(access.PermsOf(actor.Role), access.PermTaskCreate) {
		return ErrForbidden
	}
	return nil
}
func (w *wizard) Hosts(ctx context.Context, actor ports.Actor, req HostRequest) (HostPage, error) {
	if err := authorize(actor); err != nil {
		return HostPage{}, err
	}
	if _, err := repository.CheckPageQuery(req.Cursor, req.Limit, req.Page); err != nil {
		return HostPage{}, err
	}
	prefix := req.Q
	if normalized, err := model.NormalizeSN(prefix); err == nil {
		prefix = normalized
	}
	page, err := w.deps.Hosts.ListHosts(ctx, repository.HostQuery{SNPrefix: prefix, AttrKey: req.AttrKey, AttrValue: req.AttrValue, Cursor: req.Cursor, Limit: req.Limit, Page: req.Page})
	if err != nil {
		return HostPage{}, err
	}
	items := make([]HostChoice, len(page.Items))
	for i, h := range page.Items {
		_, open, err := w.deps.Tasks.FindOpenTask(ctx, h.SN)
		if err != nil {
			return HostPage{}, err
		}
		items[i] = HostChoice{Host: h, Problems: admission.HostSelectable(h, open)}
	}
	return HostPage{Items: items, Next: page.Next, Total: page.Total}, nil
}
func (w *wizard) ISOs(ctx context.Context, actor ports.Actor, req ISORequest) (ISOPage, error) {
	if err := authorize(actor); err != nil {
		return ISOPage{}, err
	}
	if len(req.HostSNs) == 0 {
		return ISOPage{}, repository.ErrInvalid
	}
	sns, err := normalizeSNs(req.HostSNs)
	if err != nil {
		return ISOPage{}, err
	}
	hosts, missing, err := w.hostRecords(ctx, sns)
	if err != nil {
		return ISOPage{}, err
	}
	byHost := map[string][]model.Problem{}
	seen := map[string]bool{}
	for _, h := range hosts {
		if seen[h.SN] {
			byHost[h.SN] = []model.Problem{model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: model.KindHost, ID: h.SN})}
		}
		seen[h.SN] = true
	}
	// 候选阶段沿用每个缺失输入项的问题；参数和提交阶段只补一次。
	for _, sn := range missing {
		byHost[sn] = append(byHost[sn], model.NewProblem("host_not_registered", model.Subject{Kind: model.KindHost, ID: sn}))
	}
	if err := batchError(admission.BatchResult{Common: []model.Problem{}, ByHost: byHost}); err != nil {
		return ISOPage{}, err
	}
	if _, err := repository.CheckPageQuery(req.Cursor, req.Limit, req.Page); err != nil {
		return ISOPage{}, err
	}
	page, err := w.deps.ISOs.ListISOs(ctx, repository.ISOQuery{States: []model.ISOState{model.ISOReady}, Cursor: req.Cursor, Limit: req.Limit, Page: req.Page})
	if err != nil {
		return ISOPage{}, err
	}
	items := make([]ISOChoice, len(page.Items))
	for i, item := range page.Items {
		items[i] = ISOChoice{ISO: item, Problems: admission.ISOUsable(item, hosts)}
	}
	return ISOPage{Items: items, Next: page.Next, Total: page.Total}, nil
}
func (w *wizard) Templates(ctx context.Context, actor ports.Actor, req TemplateRequest) (TemplatePage, error) {
	if err := authorize(actor); err != nil {
		return TemplatePage{}, err
	}
	if req.ISOID == "" {
		return TemplatePage{}, repository.ErrInvalid
	}
	iso, err := w.deps.ISOs.GetISO(ctx, req.ISOID)
	if err != nil {
		return TemplatePage{}, err
	}
	if ps := iso.Unusable(); len(ps) > 0 {
		return TemplatePage{}, &ProblemsError{Problems: ps}
	}
	if _, err := repository.CheckPageQuery(req.Cursor, req.Limit, req.Page); err != nil {
		return TemplatePage{}, err
	}
	query := repository.TemplateQuery{ApprovedOnly: true, Series: iso.Series, Cursor: req.Cursor, Limit: req.Limit, Page: req.Page}
	// 页码模式先筛可用候选，再计算总数和截取页面；游标模式保留原顺序。
	if req.Page > 0 {
		query.Selection = &repository.TemplateSelection{ISO: iso, BuiltinTemplates: w.deps.Manifest.Templates, BuiltinHooks: w.deps.Manifest.Hooks}
	}
	page, err := w.deps.Templates.ListTemplates(ctx, query)
	if err != nil {
		return TemplatePage{}, err
	}
	hooks, err := w.templateHooks(ctx, page.Items)
	if err != nil {
		return TemplatePage{}, err
	}
	items := make([]TemplateChoice, 0, len(page.Items))
	for _, t := range page.Items {
		if req.Page == 0 && (!selection.CurrentBuiltin(t, hooks, w.deps.Manifest) || len(admission.TemplateUsable(t, hooks, iso)) != 0) {
			continue
		}
		items = append(items, TemplateChoice{Template: t, Hooks: hooks, Problems: activation.Template(t, hooks)})
	}
	return TemplatePage{Items: items, Next: page.Next, Total: page.Total}, nil
}
