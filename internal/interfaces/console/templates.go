package console

import (
	"context"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type templateRequest struct {
	Name     string                   `json:"name,omitempty"`
	Family   catalog.Family           `json:"family,omitempty"`
	Series   []catalog.Series         `json:"series,omitempty"`
	Body     string                   `json:"body,omitempty"`
	Params   []paramDeclDTO           `json:"params,omitempty"`
	Bindings map[string]bindingDTO    `json:"bindings,omitempty"`
	Assembly map[string][]instanceDTO `json:"assembly,omitempty"`
}
type templateDraftRequest struct {
	templateRequest
	Rev int64 `json:"rev" binding:"required,gt=0"`
}
type templateResponse struct {
	ID          string                   `json:"id"`
	Version     int                      `json:"version"`
	Name        string                   `json:"name"`
	State       model.VersionState       `json:"state"`
	Author      string                   `json:"author"`
	Approver    string                   `json:"approver"`
	Builtin     bool                     `json:"builtin"`
	DerivedFrom *templateKeyResponse     `json:"derivedFrom"`
	Family      catalog.Family           `json:"family"`
	Series      []catalog.Series         `json:"series"`
	Body        string                   `json:"body"`
	Params      []paramDeclDTO           `json:"params"`
	Bindings    map[string]bindingDTO    `json:"bindings"`
	Assembly    map[string][]instanceDTO `json:"assembly"`
	Problems    []problemJSON            `json:"problems"`
	Rev         int64                    `json:"rev"`
}
type templatePage struct {
	Items []templateResponse `json:"items"`
	Next  string             `json:"next"`
	Total *int64             `json:"total,omitempty"`
}
type templateVersionPage struct {
	Items []templateResponse `json:"items"`
}
type deriveRequest struct {
	From int    `json:"from" binding:"required,gt=0"`
	Name string `json:"name"`
}
type templateListQuery struct {
	Page     int                `form:"page"`
	PageSize int                `form:"page_size"`
	Prefix   string             `form:"prefix"`
	State    model.VersionState `form:"state"`
	Approved bool               `form:"approved"`
	Cursor   string             `form:"cursor"`
	Limit    int                `form:"limit"`
	Series   catalog.Series     `form:"series"`
}

func toTemplateResponse(t model.Template, hooks map[model.HookKey]model.Hook) templateResponse {
	return toTemplateResponseWithProblems(t, activation.Template(t, hooks))
}
func toTemplateResponseWithProblems(t model.Template, problems []model.Problem) templateResponse {
	var from *templateKeyResponse
	if t.DerivedFrom != nil {
		from = &templateKeyResponse{t.DerivedFrom.ID, t.DerivedFrom.Version}
	}
	return templateResponse{ID: t.ID, Version: t.Version, Name: t.Name, State: t.State, Author: t.Author, Approver: t.Approver, Builtin: t.Builtin, DerivedFrom: from, Family: t.Family, Series: append([]catalog.Series{}, t.Series...), Body: t.Body, Params: declsToDTO(t.Params), Bindings: bindingsToDTO(t.Bindings), Assembly: assemblyToDTO(t.Assembly), Problems: jsonProblems(problems), Rev: t.Rev}
}

func (s *server) templateHooks(ctx context.Context, templates []model.Template) (map[model.HookKey]model.Hook, error) {
	return s.deps.Hooks.GetHooks(ctx, repository.TemplateHookKeys(templates))
}
func (s *server) templateResponses(ctx context.Context, templates []model.Template) ([]templateResponse, error) {
	hooks, err := s.templateHooks(ctx, templates)
	if err != nil {
		return nil, err
	}
	items := make([]templateResponse, len(templates))
	for i, t := range templates {
		items[i] = toTemplateResponse(t, hooks)
	}
	return items, nil
}
func (s *server) respondTemplate(c *gin.Context, t model.Template) {
	items, ok := Get(c, func() ([]templateResponse, error) {
		return s.templateResponses(c.Request.Context(), []model.Template{t})
	})
	if ok {
		Ok(c, items[0])
	}
}
func (s *server) templateRoutes() []route {
	return slices.Concat(s.templateBaseRoutes(), s.templateLifecycleRoutes())
}
func (s *server) templateBaseRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/templates", perm: PermTemplateRead, tag: "templates", summary: "List template identities", query: templateListQuery{}, resp: templatePage{}, handle: s.listTemplates},
		{method: "POST", path: "/console/v1/templates", perm: PermTemplateWrite, tag: "templates", summary: "Create template draft", req: templateRequest{}, resp: templateResponse{}, handle: s.createTemplate},
		{method: "POST", path: "/console/v1/templates/:id/derive", perm: PermTemplateWrite, tag: "templates", summary: "Derive independent template draft", req: deriveRequest{}, resp: templateResponse{}, handle: s.deriveTemplate},
		{method: "GET", path: "/console/v1/templates/:id/versions", perm: PermTemplateRead, tag: "templates", summary: "List template versions", resp: templateVersionPage{}, handle: s.listTemplateVersions},
		{method: "POST", path: "/console/v1/templates/:id/versions", perm: PermTemplateWrite, tag: "templates", summary: "Copy a template version into a new draft", query: fromVersionQuery{}, resp: templateResponse{}, handle: s.newTemplateVersion},
		{method: "GET", path: "/console/v1/templates/:id/versions/:version", perm: PermTemplateRead, tag: "templates", summary: "Get template version and diagnostics", resp: templateResponse{}, handle: s.getTemplate},
		{method: "PUT", path: "/console/v1/templates/:id/versions/:version", perm: PermTemplateWrite, tag: "templates", summary: "Replace template draft content", req: templateDraftRequest{}, resp: templateResponse{}, handle: s.saveTemplateDraft},
		{method: "DELETE", path: "/console/v1/templates/:id/versions/:version", perm: PermTemplateWrite, tag: "templates", summary: "Delete template draft", query: revisionQuery{}, handle: s.deleteTemplateDraft},
		{method: "PUT", path: "/console/v1/templates/:id/name", perm: PermTemplateWrite, audit: "template.rename", tag: "templates", summary: "Rename template identity", req: nameRequest{}, handle: s.renameTemplate},
		{method: "POST", path: "/console/v1/templates/:id/versions/:version/preview", perm: PermTemplateRead, tag: "templates", summary: "Render complete machine files from a frozen preview snapshot", req: previewRequest{}, resp: previewResponse{}, handle: s.previewTemplate},
		{method: "POST", path: "/console/v1/templates/:id/versions/:version/preview-params", perm: PermTemplateRead, tag: "templates", summary: "Evaluate preview parameter fields for draft or approved template context", req: previewRequest{}, resp: previewParamsResponse{}, handle: s.previewParams},
		{method: "GET", path: "/console/v1/templates/:id/versions/:version/slots", perm: PermTemplateRead, tag: "templates", summary: "Scan editor slots and diagnostic positions", resp: slotList{}, handle: s.slotsOf},
		{method: "GET", path: "/console/v1/templates/:id/versions/:version/hook-candidates", perm: PermTemplateRead, tag: "templates", summary: "Check hook candidates with core activation", query: hookCandidateQuery{}, resp: hookCandidatePage{}, handle: s.hookCandidates},
		{method: "GET", path: "/console/v1/templates/:id/versions/:version/source-diff", perm: PermTemplateRead, tag: "templates", summary: "Compare pinned and newest source template versions", resp: sourceDiffResponse{}, handle: s.sourceDiff},
	}
}
func (s *server) listTemplates(c *gin.Context) {
	common, ok := authoringQuery(c)
	if !ok {
		return
	}
	series := catalog.Series(c.Query("series"))
	if series != "" && !series.Valid() {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	q := repository.TemplateQuery{NamePrefix: common.NamePrefix, States: common.States, ApprovedOnly: common.ApprovedOnly, Cursor: common.Cursor, Limit: common.Limit, Page: common.Page, Series: series}
	page, ok := Get(c, func() (repository.Page[model.Template], error) {
		return s.deps.Templates.ListTemplates(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items, ok := Get(c, func() ([]templateResponse, error) { return s.templateResponses(c.Request.Context(), page.Items) })
	if ok {
		Ok(c, templatePage{items, page.Next, page.Total})
	}
}
func (s *server) listTemplateVersions(c *gin.Context) {
	ts, ok := Get(c, func() ([]model.Template, error) {
		return s.deps.Templates.ListTemplateVersions(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	items, ok := Get(c, func() ([]templateResponse, error) { return s.templateResponses(c.Request.Context(), ts) })
	if ok {
		Ok(c, templateVersionPage{items})
	}
}
func templateKeyOf(c *gin.Context) (model.TemplateKey, bool) {
	version, ok := positiveInt(c, c.Param("version"))
	if !ok {
		return model.TemplateKey{}, false
	}
	k := model.TemplateKey{ID: c.Param("id"), Version: version}
	if err := repository.CheckVersionKey(k.ID, k.Version); err != nil {
		RespondError(c, err)
		return k, false
	}
	return k, true
}
func (s *server) getTemplate(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	t, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if ok {
		s.respondTemplate(c, t)
	}
}
func templateOf(req templateRequest) model.Template {
	return model.Template{Name: req.Name, Family: req.Family, Series: req.Series, Body: req.Body, Params: declsFromDTO(req.Params), Bindings: bindingsFromDTO(req.Bindings), Assembly: assemblyFromDTO(req.Assembly)}
}
func (s *server) createTemplate(c *gin.Context) {
	var req templateRequest
	if !Bind(c, &req) {
		return
	}
	t := templateOf(req)
	t.Author = userOf(c).Username
	created, ok := Write(c, func() (model.Template, error) { return s.deps.Templates.CreateTemplate(c.Request.Context(), t) })
	if ok {
		s.respondTemplate(c, created)
	}
}
func (s *server) deriveTemplate(c *gin.Context) {
	var req deriveRequest
	if !Bind(c, &req) {
		return
	}
	created, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.DeriveTemplate(c.Request.Context(), model.TemplateKey{ID: c.Param("id"), Version: req.From}, req.Name, userOf(c).Username)
	})
	if ok {
		s.respondTemplate(c, created)
	}
}
func (s *server) saveTemplateDraft(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	var req templateDraftRequest
	if !Bind(c, &req) {
		return
	}
	current, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if !ok {
		return
	}
	if req.Name != "" && req.Name != current.Name {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	t := templateOf(req.templateRequest)
	t.ID, t.Version, t.Rev, t.Name = current.ID, current.Version, req.Rev, current.Name
	t.Author, t.Approver, t.State, t.Builtin, t.DerivedFrom = current.Author, current.Approver, current.State, current.Builtin, current.DerivedFrom
	saved, ok := Write(c, func() (model.Template, error) { return s.deps.Templates.SaveTemplateDraft(c.Request.Context(), t) })
	if ok {
		s.respondTemplate(c, saved)
	}
}
func (s *server) newTemplateVersion(c *gin.Context) {
	from, ok := positiveInt(c, c.Query("from"))
	if !ok {
		return
	}
	t, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.NewTemplateVersion(c.Request.Context(), c.Param("id"), from, userOf(c).Username)
	})
	if ok {
		s.respondTemplate(c, t)
	}
}
func (s *server) deleteTemplateDraft(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	if err := s.deps.Templates.DeleteTemplateDraft(c.Request.Context(), k, rev); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) renameTemplate(c *gin.Context) {
	var req nameRequest
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.Templates.RenameTemplate(c.Request.Context(), c.Param("id"), req.Name, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
