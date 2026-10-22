package console

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/scan"
)

type hookCandidateQuery struct {
	Slot     string `form:"slot" binding:"required"`
	Index    int    `form:"index"`
	Prefix   string `form:"prefix"`
	Cursor   string `form:"cursor"`
	Limit    int    `form:"limit"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
type hookCandidateResponse struct {
	Hook     hookResponse  `json:"hook"`
	Usable   bool          `json:"usable"`
	Problems []problemJSON `json:"problems"`
}
type hookCandidatePage struct {
	Items []hookCandidateResponse `json:"items"`
	Next  string                  `json:"next"`
	Total *int64                  `json:"total,omitempty"`
}
type sourceDiffResponse struct {
	Original *templateResponse `json:"original"`
	Current  *templateResponse `json:"current"`
	Changed  bool              `json:"changed"`
}

// Candidate checks run the same core activation as approval. Only diagnostics
// for the inserted instance (and its slot/template format prerequisites) affect
// its usability; unrelated incomplete draft content must not hide valid hooks.
func (s *server) hookCandidates(c *gin.Context) {
	key, ok := templateKeyOf(c)
	if !ok {
		return
	}
	tpl, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), key) })
	if !ok {
		return
	}
	slot := c.Query("slot")
	scanned, _ := scan.Scan(tpl.Body)
	if !slices.ContainsFunc(scanned.Slots, func(v scan.Slot) bool { return v.Name == slot }) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	index := len(tpl.Assembly[slot]) + 1
	if raw := c.Query("index"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > index {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		index = n
	}
	pageNumber, limit, ok := queryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (repository.Page[model.Hook], error) {
		return s.deps.Hooks.ListHooks(c.Request.Context(), repository.HookQuery{NamePrefix: c.Query("prefix"), Cursor: c.Query("cursor"), Limit: limit, Page: pageNumber, AllVersions: pageNumber > 0})
	})
	if !ok {
		return
	}
	hooks, ok := Get(c, func() (map[model.HookKey]model.Hook, error) {
		return s.templateHooks(c.Request.Context(), []model.Template{tpl})
	})
	if !ok {
		return
	}
	// The cursor pages identities; each identity exposes all pinned versions so
	// a new draft does not hide its still-approved predecessor from the editor.
	candidates := page.Items
	if pageNumber == 0 {
		candidates = []model.Hook{}
		for _, identity := range page.Items {
			versions, ok := Get(c, func() ([]model.Hook, error) {
				return s.deps.Hooks.ListHookVersions(c.Request.Context(), identity.ID)
			})
			if !ok {
				return
			}
			candidates = append(candidates, versions...)
		}
	}
	items := make([]hookCandidateResponse, 0, len(candidates))
	for _, hook := range candidates {
		candidate := tpl
		candidate.Assembly = maps.Clone(tpl.Assembly)
		if candidate.Assembly == nil {
			candidate.Assembly = map[string][]model.Instance{}
		}
		instances := slices.Clone(tpl.Assembly[slot])
		inst := model.Instance{Bindings: map[string]model.Binding{}}
		if index <= len(instances) {
			inst = instances[index-1]
			inst.Bindings = map[string]model.Binding{}
			for _, param := range hook.Params {
				if binding, exists := instances[index-1].Bindings[param.Name]; exists {
					inst.Bindings[param.Name] = binding
				}
			}
		}
		inst.HookID, inst.HookVersion = hook.ID, hook.Version
		if index > len(instances) {
			instances = append(instances, inst)
		} else {
			instances[index-1] = inst
		}
		candidate.Assembly[slot] = instances
		refs := maps.Clone(hooks)
		if refs == nil {
			refs = map[model.HookKey]model.Hook{}
		}
		refs[hook.Key()] = hook
		path := model.InstancePath(slot, index)
		var problems []model.Problem
		usable := true
		for _, problem := range activation.Template(candidate, refs) {
			subject := problem.Subject
			relevant := subject.Kind == model.KindSlot && (subject.Path == path || strings.HasPrefix(subject.Path, path+"/") || subject.Path == slot)
			relevant = relevant || problem.Code == "template_series_invalid" || problem.Code == "template_mixed_formats"
			if !relevant {
				continue
			}
			problems = append(problems, problem)
			// A draft can reference a draft hook; approval will still require it to be approved.
			if problem.Code != "hook_ref_unapproved" {
				usable = false
			}
		}
		items = append(items, hookCandidateResponse{toHookResponse(hook), usable, jsonProblems(problems)})
	}
	Ok(c, hookCandidatePage{Items: items, Next: page.Next, Total: page.Total})
}

func (s *server) sourceDiff(c *gin.Context) {
	key, ok := templateKeyOf(c)
	if !ok {
		return
	}
	tpl, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), key) })
	if !ok {
		return
	}
	if tpl.DerivedFrom == nil {
		Ok(c, sourceDiffResponse{})
		return
	}
	original, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), *tpl.DerivedFrom)
	})
	if !ok {
		return
	}
	versions, ok := Get(c, func() ([]model.Template, error) {
		return s.deps.Templates.ListTemplateVersions(c.Request.Context(), tpl.DerivedFrom.ID)
	})
	if !ok {
		return
	}
	current := original
	for _, version := range versions {
		if version.Version > current.Version {
			current = version
		}
	}
	responses, ok := Get(c, func() ([]templateResponse, error) {
		return s.templateResponses(c.Request.Context(), []model.Template{original, current})
	})
	if !ok {
		return
	}
	Ok(c, sourceDiffResponse{&responses[0], &responses[1], current.Version != original.Version})
}
