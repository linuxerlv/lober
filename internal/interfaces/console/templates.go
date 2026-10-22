package console

import (
	"context"
	"github.com/lober/lober/internal/application/authoring"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
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
func templateViewResponse(v authoring.TemplateView) templateResponse {
	return toTemplateResponseWithProblems(v.Template, v.Problems)
}
func templateViewResponses(vs []authoring.TemplateView) []templateResponse {
	out := make([]templateResponse, len(vs))
	for i, v := range vs {
		out[i] = templateViewResponse(v)
	}
	return out
}
func templateRevisionOf(c *gin.Context) authoring.TemplateRevision {
	rev, err := strconv.ParseInt(c.Query("rev"), 10, 64)
	if err != nil {
		rev = 0
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		version = 0
	}
	return authoring.TemplateRevision{Key: model.TemplateKey{ID: c.Param("id"), Version: version}, Rev: rev}
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
	q, ok := authoringQuery(c)
	if !ok {
		return
	}
	req := authoring.TemplateListRequest{ListRequest: authoring.ListRequest{PageRequest: authoring.PageRequest{Page: q.Page, Limit: q.Limit, Cursor: q.Cursor}, Prefix: q.NamePrefix, Approved: q.ApprovedOnly}, Series: catalog.Series(c.Query("series"))}
	if len(q.States) > 0 {
		req.State = q.States[0]
	}
	page, ok := Get(c, func() (authoring.TemplatePage, error) {
		return s.deps.TemplateAuthoring.List(c.Request.Context(), actorOf(c), req)
	})
	if ok {
		Ok(c, templatePage{templateViewResponses(page.Items), page.Next, page.Total})
	}
}
func (s *server) listTemplateVersions(c *gin.Context) {
	vs, ok := Get(c, func() ([]authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.Versions(c.Request.Context(), actorOf(c), c.Param("id"))
	})
	if ok {
		Ok(c, templateVersionPage{templateViewResponses(vs)})
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
	v, ok := Get(c, func() (authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.Get(c.Request.Context(), actorOf(c), k)
	})
	if ok {
		Ok(c, templateViewResponse(v))
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
	v, ok := Write(c, func() (authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.Create(c.Request.Context(), actorOf(c), templateOf(req))
	})
	if ok {
		Ok(c, templateViewResponse(v))
	}
}
func (s *server) deriveTemplate(c *gin.Context) {
	var req deriveRequest
	if !Bind(c, &req) {
		return
	}
	v, ok := Write(c, func() (authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.Derive(c.Request.Context(), actorOf(c), authoring.DeriveRequest{From: model.TemplateKey{ID: c.Param("id"), Version: req.From}, Name: req.Name})
	})
	if ok {
		Ok(c, templateViewResponse(v))
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
	t := templateOf(req.templateRequest)
	t.ID, t.Version, t.Rev = k.ID, k.Version, req.Rev
	v, ok := Write(c, func() (authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.SaveDraft(c.Request.Context(), actorOf(c), t)
	})
	if ok {
		Ok(c, templateViewResponse(v))
	}
}
func (s *server) newTemplateVersion(c *gin.Context) {
	from, ok := positiveInt(c, c.Query("from"))
	if !ok {
		return
	}
	v, ok := Write(c, func() (authoring.TemplateView, error) {
		return s.deps.TemplateAuthoring.NewVersion(c.Request.Context(), actorOf(c), authoring.VersionSource{ID: c.Param("id"), From: from})
	})
	if ok {
		Ok(c, templateViewResponse(v))
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
	if err := s.deps.TemplateAuthoring.DeleteDraft(c.Request.Context(), actorOf(c), authoring.TemplateRevision{Key: k, Rev: rev}); err != nil {
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
	if err := s.deps.TemplateAuthoring.Rename(c.Request.Context(), actorOf(c), authoring.RenameRequest{ID: c.Param("id"), Name: req.Name}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
