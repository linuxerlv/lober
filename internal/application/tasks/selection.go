package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
)

func (s Selection) valid() bool { return s.ISOID != "" && s.TemplateID != "" && s.TemplateVersion > 0 }
func normalizeSNs(raw []string) ([]string, error) {
	out := make([]string, len(raw))
	var ps []model.Problem
	for i, value := range raw {
		sn, err := model.NormalizeSN(value)
		if err != nil {
			var p model.Problem
			if errors.As(err, &p) {
				ps = append(ps, p)
			} else {
				return nil, err
			}
		} else {
			out[i] = sn
		}
	}
	if len(ps) > 0 {
		return nil, &ProblemsError{Problems: ps}
	}
	return out, nil
}
func (w *wizard) hostRecords(ctx context.Context, sns []string) ([]model.Host, []string, error) {
	records, err := w.deps.Hosts.GetHosts(ctx, sns)
	if err != nil {
		return nil, nil, err
	}
	hosts := make([]model.Host, len(sns))
	missing := []string{}
	for i, sn := range sns {
		h, exists := records[sn]
		if !exists {
			h = model.Host{SN: sn}
			missing = append(missing, sn)
		}
		hosts[i] = h
	}
	return hosts, missing, nil
}
func (w *wizard) templateHooks(ctx context.Context, templates []model.Template) (map[model.HookKey]model.Hook, error) {
	return w.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys(templates))
}

// selectionFacts 保留顺序：模板存在、当前审批版本、镜像存在、组件、内置发布。
// 编号和参数问题在这些事实之后检查，不能遮住已失效的选择。
func (w *wizard) selectionFacts(ctx context.Context, req Selection) (model.Template, model.ISO, map[model.HookKey]model.Hook, error) {
	t, err := w.deps.Templates.GetTemplate(ctx, model.TemplateKey{ID: req.TemplateID, Version: req.TemplateVersion})
	if err != nil {
		return model.Template{}, model.ISO{}, nil, err
	}
	versions, err := w.deps.Templates.ListTemplateVersions(ctx, t.ID)
	if err != nil {
		return model.Template{}, model.ISO{}, nil, err
	}
	current, exists := model.CurrentApproved(versions)
	if !exists || current.Version != t.Version {
		return model.Template{}, model.ISO{}, nil, repository.ErrTemplateNotCurrent
	}
	iso, err := w.deps.ISOs.GetISO(ctx, req.ISOID)
	if err != nil {
		return model.Template{}, model.ISO{}, nil, err
	}
	hooks, err := w.templateHooks(ctx, []model.Template{t})
	if err != nil {
		return model.Template{}, model.ISO{}, nil, err
	}
	if !selection.CurrentBuiltin(t, hooks, w.deps.Manifest) {
		return model.Template{}, model.ISO{}, nil, repository.ErrTemplateNotCurrent
	}
	return t, iso, hooks, nil
}
func (w *wizard) batch(ctx context.Context, t model.Template, iso model.ISO, hooks map[model.HookKey]model.Hook, hosts []model.Host, missing []string) (admission.BatchResult, error) {
	open := map[string]bool{}
	for _, h := range hosts {
		if _, done := open[h.SN]; done {
			continue
		}
		_, has, err := w.deps.Tasks.FindOpenTask(ctx, h.SN)
		if err != nil {
			return admission.BatchResult{}, err
		}
		open[h.SN] = has
	}
	result := admission.Batch(admission.BatchInput{Hosts: hosts, OpenTask: open, ISO: iso, Template: t, Hooks: hooks})
	seen := map[string]bool{}
	for _, sn := range missing {
		if !seen[sn] {
			result.ByHost[sn] = append(result.ByHost[sn], model.NewProblem("host_not_registered", model.Subject{Kind: model.KindHost, ID: sn}))
			seen[sn] = true
		}
	}
	return result, nil
}
func batchError(result admission.BatchResult) error {
	bad := len(result.Common) > 0
	for _, ps := range result.ByHost {
		bad = bad || len(ps) > 0
	}
	if bad {
		return &repository.BatchError{Result: result}
	}
	return nil
}
