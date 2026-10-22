package authoring

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/scan"
)

type editor struct{ deps EditorDeps }

func NewEditor(d EditorDeps) Editor {
	if d.Templates == nil || d.Hooks == nil {
		panic("编辑辅助缺少必要能力")
	}
	return &editor{deps: d}
}
func (s *editor) template(ctx context.Context, a ports.Actor, k model.TemplateKey) (model.Template, error) {
	if err := a.Authorize(access.PermTemplateRead); err != nil {
		return model.Template{}, err
	}
	if err := repository.CheckVersionKey(k.ID, k.Version); err != nil {
		return model.Template{}, err
	}
	return s.deps.Templates.GetTemplate(ctx, k)
}
func (s *editor) Slots(ctx context.Context, a ports.Actor, k model.TemplateKey) (SlotResult, error) {
	tpl, err := s.template(ctx, a, k)
	if err != nil {
		return SlotResult{}, err
	}
	result, problems := scan.Scan(tpl.Body)
	for i := range problems {
		problems[i].Subject.Kind = model.KindTemplate
		problems[i].Subject.ID = k.String()
	}
	// 保留第一次出现的位置，重复位置由原有规则报告。
	for _, p := range activation.Template(tpl, nil) {
		if p.Code == "slot_duplicate" {
			problems = append(problems, p)
		}
	}
	slots := make([]SlotInfo, 0, len(result.Slots))
	seen := map[string]bool{}
	for _, seg := range result.Segments {
		if seg.Kind != scan.KindSlot || seen[seg.Slot.Name] {
			continue
		}
		seen[seg.Slot.Name] = true
		slots = append(slots, SlotInfo{seg.Slot.Name, seg.Slot.Exec, seg.Slot.Timing, seg.Line, seg.Col})
	}
	return SlotResult{Slots: slots, Problems: problems}, nil
}
func (s *editor) Candidates(ctx context.Context, a ports.Actor, r CandidateRequest) (CandidatePage, error) {
	tpl, err := s.template(ctx, a, r.Key)
	if err != nil {
		return CandidatePage{}, err
	}
	scanned, _ := scan.Scan(tpl.Body)
	if !slices.ContainsFunc(scanned.Slots, func(v scan.Slot) bool { return v.Name == r.Slot }) {
		return CandidatePage{}, repository.ErrInvalid
	}
	index := len(tpl.Assembly[r.Slot]) + 1
	if r.Index != nil {
		if *r.Index < 1 || *r.Index > index {
			return CandidatePage{}, repository.ErrInvalid
		}
		index = *r.Index
	}
	if _, err := repository.CheckPageQuery(r.Cursor, r.Limit, r.Page); err != nil {
		return CandidatePage{}, err
	}
	page, err := s.deps.Hooks.ListHooks(ctx, repository.HookQuery{NamePrefix: r.Prefix, Cursor: r.Cursor, Limit: r.Limit, Page: r.Page, AllVersions: r.Page > 0})
	if err != nil {
		return CandidatePage{}, err
	}
	hooks, err := s.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys([]model.Template{tpl}))
	if err != nil {
		return CandidatePage{}, err
	}
	candidates := page.Items
	// 游标先翻身份，再展开该身份的全部版本。页码已经按版本翻页。
	if r.Page == 0 {
		candidates = []model.Hook{}
		for _, identity := range page.Items {
			versions, err := s.deps.Hooks.ListHookVersions(ctx, identity.ID)
			if err != nil {
				return CandidatePage{}, err
			}
			candidates = append(candidates, versions...)
		}
	}
	items := make([]Candidate, 0, len(candidates))
	for _, h := range candidates {
		candidate := candidateTemplate(tpl, r.Slot, index, h)
		refs := maps.Clone(hooks)
		if refs == nil {
			refs = map[model.HookKey]model.Hook{}
		}
		refs[h.Key()] = h
		problems := candidateProblems(activation.Template(candidate, refs), r.Slot, index)
		usable := true
		for _, p := range problems {
			if p.Code != "hook_ref_unapproved" {
				usable = false
			}
		}
		items = append(items, Candidate{Hook: hookView(h), Usable: usable, Problems: problems})
	}
	return CandidatePage{Items: items, Next: page.Next, Total: page.Total}, nil
}
func candidateTemplate(tpl model.Template, slot string, index int, h model.Hook) model.Template {
	candidate := cloneTemplate(tpl)
	if candidate.Assembly == nil {
		candidate.Assembly = map[string][]model.Instance{}
	}
	instances := candidate.Assembly[slot]
	inst := model.Instance{Bindings: map[string]model.Binding{}}
	if index <= len(instances) {
		inst = instances[index-1]
		inst.Bindings = map[string]model.Binding{}
		for _, p := range h.Params {
			if binding, ok := instances[index-1].Bindings[p.Name]; ok {
				inst.Bindings[p.Name] = binding
			}
		}
	}
	inst.HookID, inst.HookVersion = h.ID, h.Version
	if index > len(instances) {
		instances = append(instances, inst)
	} else {
		instances[index-1] = inst
	}
	candidate.Assembly[slot] = instances
	return candidate
}
func candidateProblems(all []model.Problem, slot string, index int) []model.Problem {
	path := model.InstancePath(slot, index)
	var out []model.Problem
	for _, p := range all {
		subject := p.Subject
		relevant := subject.Kind == model.KindSlot && (subject.Path == path || strings.HasPrefix(subject.Path, path+"/") || subject.Path == slot)
		if relevant || p.Code == "template_series_invalid" || p.Code == "template_mixed_formats" {
			out = append(out, p)
		}
	}
	return out
}
func (s *editor) SourceDiff(ctx context.Context, a ports.Actor, k model.TemplateKey) (SourceDiffResult, error) {
	tpl, err := s.template(ctx, a, k)
	if err != nil {
		return SourceDiffResult{}, err
	}
	if tpl.DerivedFrom == nil {
		return SourceDiffResult{}, nil
	}
	original, err := s.deps.Templates.GetTemplate(ctx, *tpl.DerivedFrom)
	if err != nil {
		return SourceDiffResult{}, err
	}
	versions, err := s.deps.Templates.ListTemplateVersions(ctx, tpl.DerivedFrom.ID)
	if err != nil {
		return SourceDiffResult{}, err
	}
	current := original
	for _, v := range versions {
		if v.Version > current.Version {
			current = v
		}
	}
	hooks, err := s.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys([]model.Template{original, current}))
	if err != nil {
		return SourceDiffResult{}, err
	}
	return SourceDiffResult{Original: &TemplateView{Template: cloneTemplate(original), Problems: activation.Template(original, hooks)}, Current: &TemplateView{Template: cloneTemplate(current), Problems: activation.Template(current, hooks)}, Changed: current.Version != original.Version}, nil
}
