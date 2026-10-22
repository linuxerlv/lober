package console

import (
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type hookRequest struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	TimeoutSec  int            `json:"timeoutSec,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Params      []paramDeclDTO `json:"params,omitempty"`
	Impls       []implDTO      `json:"impls,omitempty"`
}
type hookDraftRequest struct {
	hookRequest
	Rev int64 `json:"rev" binding:"required,gt=0"`
}
type hookResponse struct {
	ID          string             `json:"id"`
	Version     int                `json:"version"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	State       model.VersionState `json:"state"`
	Author      string             `json:"author"`
	Approver    string             `json:"approver"`
	Builtin     bool               `json:"builtin"`
	TimeoutSec  int                `json:"timeoutSec"`
	Required    bool               `json:"required"`
	Params      []paramDeclDTO     `json:"params"`
	Impls       []implDTO          `json:"impls"`
	Problems    []problemJSON      `json:"problems"`
	Rev         int64              `json:"rev"`
}
type hookPage struct {
	Items []hookResponse `json:"items"`
	Next  string         `json:"next"`
	Total *int64         `json:"total,omitempty"`
}
type versionPage struct {
	Items []hookResponse `json:"items"`
}
type hookListQuery struct {
	Page     int                `form:"page"`
	PageSize int                `form:"page_size"`
	Prefix   string             `form:"prefix"`
	State    model.VersionState `form:"state"`
	Approved bool               `form:"approved"`
	Cursor   string             `form:"cursor"`
	Limit    int                `form:"limit"`
}
type fromVersionQuery struct {
	From int `form:"from" binding:"required,gt=0"`
}
type nameRequest struct {
	Name string `json:"name"`
}

func toHookResponse(view authoring.HookView) hookResponse {
	h := view.Hook
	return hookResponse{ID: h.ID, Version: h.Version, Name: h.Name, Description: h.Description, State: h.State, Author: h.Author, Approver: h.Approver, Builtin: h.Builtin, TimeoutSec: h.TimeoutSec, Required: h.Required, Params: declsToDTO(h.Params), Impls: implsToDTO(h.Impls), Problems: jsonProblems(view.Problems), Rev: h.Rev}
}
func hookResponses(hs []authoring.HookView) []hookResponse {
	out := make([]hookResponse, len(hs))
	for i, h := range hs {
		out[i] = toHookResponse(h)
	}
	return out
}
func (s *server) hookRoutes() []route {
	return slices.Concat(s.hookBaseRoutes(), s.hookLifecycleRoutes())
}
func (s *server) hookBaseRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/hooks", perm: PermHookRead, tag: "hooks", summary: "List hook identities", query: hookListQuery{}, resp: hookPage{}, handle: s.listHooks},
		{method: "POST", path: "/console/v1/hooks", perm: PermHookWrite, tag: "hooks", summary: "Create hook draft", req: hookRequest{}, resp: hookResponse{}, handle: s.createHook},
		{method: "GET", path: "/console/v1/hooks/:id/versions", perm: PermHookRead, tag: "hooks", summary: "List hook versions", resp: versionPage{}, handle: s.listHookVersions},
		{method: "POST", path: "/console/v1/hooks/:id/versions", perm: PermHookWrite, tag: "hooks", summary: "Copy a hook version into a new draft", query: fromVersionQuery{}, resp: hookResponse{}, handle: s.newHookVersion},
		{method: "GET", path: "/console/v1/hooks/:id/versions/:version", perm: PermHookRead, tag: "hooks", summary: "Get hook version and diagnostics", resp: hookResponse{}, handle: s.getHook},
		{method: "PUT", path: "/console/v1/hooks/:id/versions/:version", perm: PermHookWrite, tag: "hooks", summary: "Replace hook draft content", req: hookDraftRequest{}, resp: hookResponse{}, handle: s.saveHookDraft},
		{method: "DELETE", path: "/console/v1/hooks/:id/versions/:version", perm: PermHookWrite, tag: "hooks", summary: "Delete hook draft", query: revisionQuery{}, handle: s.deleteHookDraft},
		{method: "PUT", path: "/console/v1/hooks/:id/name", perm: PermHookWrite, audit: "hook.rename", tag: "hooks", summary: "Rename hook identity", req: nameRequest{}, handle: s.renameHook},
	}
}
func queryBool(c *gin.Context, key string) (bool, bool) {
	raw, exists := c.GetQuery(key)
	if !exists {
		return false, true
	}
	if raw != "true" && raw != "false" {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false, false
	}
	return raw == "true", true
}
func authoringQuery(c *gin.Context) (repository.HookQuery, bool) {
	number, limit, ok := queryPagination(c)
	if !ok {
		return repository.HookQuery{}, false
	}
	approved, ok := queryBool(c, "approved")
	if !ok {
		return repository.HookQuery{}, false
	}
	q := repository.HookQuery{NamePrefix: c.Query("prefix"), ApprovedOnly: approved, Cursor: c.Query("cursor"), Limit: limit, Page: number}
	if state := c.Query("state"); state != "" {
		if !model.VersionState(state).Valid() {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return q, false
		}
		q.States = []model.VersionState{model.VersionState(state)}
	}
	return q, true
}
func (s *server) listHooks(c *gin.Context) {
	q, ok := authoringQuery(c)
	if !ok {
		return
	}
	req := authoring.ListRequest{PageRequest: authoring.PageRequest{Page: q.Page, Limit: q.Limit, Cursor: q.Cursor}, Prefix: q.NamePrefix, Approved: q.ApprovedOnly}
	if len(q.States) > 0 {
		req.State = q.States[0]
	}
	page, ok := Get(c, func() (authoring.HookPage, error) {
		return s.deps.HookAuthoring.List(c.Request.Context(), actorOf(c), req)
	})
	if ok {
		Ok(c, hookPage{hookResponses(page.Items), page.Next, page.Total})
	}
}
func (s *server) listHookVersions(c *gin.Context) {
	hs, ok := Get(c, func() ([]authoring.HookView, error) {
		return s.deps.HookAuthoring.Versions(c.Request.Context(), actorOf(c), c.Param("id"))
	})
	if ok {
		Ok(c, versionPage{hookResponses(hs)})
	}
}
func positiveInt(c *gin.Context, raw string) (int, bool) {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return 0, false
	}
	return n, true
}

// 无法解析的数字交给应用入口，保留各操作自己的检查顺序。
func hookKeyOf(c *gin.Context) model.HookKey {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil {
		version = 0
	}
	return model.HookKey{ID: c.Param("id"), Version: version}
}
func hookRevisionOf(c *gin.Context) authoring.HookRevision {
	rev, err := strconv.ParseInt(c.Query("rev"), 10, 64)
	if err != nil {
		rev = 0
	}
	return authoring.HookRevision{Key: hookKeyOf(c), Rev: rev}
}
func (s *server) getHook(c *gin.Context) {
	h, ok := Get(c, func() (authoring.HookView, error) {
		return s.deps.HookAuthoring.Get(c.Request.Context(), actorOf(c), hookKeyOf(c))
	})
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func hookOf(req hookRequest) model.Hook {
	return model.Hook{Name: req.Name, Description: req.Description, TimeoutSec: req.TimeoutSec, Required: req.Required, Params: declsFromDTO(req.Params), Impls: implsFromDTO(req.Impls)}
}
func (s *server) createHook(c *gin.Context) {
	var req hookRequest
	if !Bind(c, &req) {
		return
	}
	h, ok := Write(c, func() (authoring.HookView, error) {
		return s.deps.HookAuthoring.Create(c.Request.Context(), actorOf(c), hookOf(req))
	})
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func (s *server) saveHookDraft(c *gin.Context) {
	// 保存原本先检查版本，再解析正文；应用仍独立检查同一版本键。
	k := hookKeyOf(c)
	if k.Version <= 0 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	var req hookDraftRequest
	if !Bind(c, &req) {
		return
	}
	h := hookOf(req.hookRequest)
	h.ID, h.Version, h.Rev = k.ID, k.Version, req.Rev
	saved, ok := Write(c, func() (authoring.HookView, error) {
		return s.deps.HookAuthoring.SaveDraft(c.Request.Context(), actorOf(c), h)
	})
	if ok {
		Ok(c, toHookResponse(saved))
	}
}
func (s *server) newHookVersion(c *gin.Context) {
	from, err := strconv.Atoi(c.Query("from"))
	if err != nil {
		from = 0
	}
	h, ok := Write(c, func() (authoring.HookView, error) {
		return s.deps.HookAuthoring.NewVersion(c.Request.Context(), actorOf(c), authoring.VersionSource{ID: c.Param("id"), From: from})
	})
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func (s *server) deleteHookDraft(c *gin.Context) {
	if err := s.deps.HookAuthoring.DeleteDraft(c.Request.Context(), actorOf(c), hookRevisionOf(c)); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) renameHook(c *gin.Context) {
	var req nameRequest
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.HookAuthoring.Rename(c.Request.Context(), actorOf(c), authoring.RenameRequest{ID: c.Param("id"), Name: req.Name}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
