package console

import (
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
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
}
type versionPage struct {
	Items []hookResponse `json:"items"`
}
type hookListQuery struct {
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

func ProblemsOf(h model.Hook) []model.Problem { return activation.Hook(h) }
func toHookResponse(h model.Hook) hookResponse {
	return hookResponse{ID: h.ID, Version: h.Version, Name: h.Name, Description: h.Description, State: h.State, Author: h.Author, Approver: h.Approver, Builtin: h.Builtin, TimeoutSec: h.TimeoutSec, Required: h.Required, Params: declsToDTO(h.Params), Impls: implsToDTO(h.Impls), Problems: jsonProblems(ProblemsOf(h)), Rev: h.Rev}
}
func hookResponses(hs []model.Hook) []hookResponse {
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
func authoringQuery(c *gin.Context) (store.HookQuery, bool) {
	limit, ok := queryLimit(c)
	if !ok {
		return store.HookQuery{}, false
	}
	approved, ok := queryBool(c, "approved")
	if !ok {
		return store.HookQuery{}, false
	}
	q := store.HookQuery{NamePrefix: c.Query("prefix"), ApprovedOnly: approved, Cursor: c.Query("cursor"), Limit: limit}
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
	page, ok := Get(c, func() (store.Page[model.Hook], error) { return s.deps.Hooks.ListHooks(c.Request.Context(), q) })
	if ok {
		Ok(c, hookPage{hookResponses(page.Items), page.Next})
	}
}
func (s *server) listHookVersions(c *gin.Context) {
	hs, ok := Get(c, func() ([]model.Hook, error) { return s.deps.Hooks.ListHookVersions(c.Request.Context(), c.Param("id")) })
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
func hookKeyOf(c *gin.Context) (model.HookKey, bool) {
	version, ok := positiveInt(c, c.Param("version"))
	if !ok {
		return model.HookKey{}, false
	}
	k := model.HookKey{ID: c.Param("id"), Version: version}
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		RespondError(c, err)
		return k, false
	}
	return k, true
}
func (s *server) getHook(c *gin.Context) {
	k, ok := hookKeyOf(c)
	if !ok {
		return
	}
	h, ok := Get(c, func() (model.Hook, error) { return s.deps.Hooks.GetHook(c.Request.Context(), k) })
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
	h := hookOf(req)
	h.Author = userOf(c).Username
	created, ok := Write(c, func() (model.Hook, error) { return s.deps.Hooks.CreateHook(c.Request.Context(), h) })
	if ok {
		Ok(c, toHookResponse(created))
	}
}
func (s *server) saveHookDraft(c *gin.Context) {
	k, ok := hookKeyOf(c)
	if !ok {
		return
	}
	var req hookDraftRequest
	if !Bind(c, &req) {
		return
	}
	current, ok := Get(c, func() (model.Hook, error) { return s.deps.Hooks.GetHook(c.Request.Context(), k) })
	if !ok {
		return
	}
	if req.Name != "" && req.Name != current.Name {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	h := hookOf(req.hookRequest)
	h.ID, h.Version, h.Rev, h.Name = current.ID, current.Version, req.Rev, current.Name
	h.Author, h.Approver, h.State, h.Builtin = current.Author, current.Approver, current.State, current.Builtin
	saved, ok := Write(c, func() (model.Hook, error) { return s.deps.Hooks.SaveHookDraft(c.Request.Context(), h) })
	if ok {
		Ok(c, toHookResponse(saved))
	}
}
func (s *server) newHookVersion(c *gin.Context) {
	from, ok := positiveInt(c, c.Query("from"))
	if !ok {
		return
	}
	h, ok := Write(c, func() (model.Hook, error) {
		return s.deps.Hooks.NewHookVersion(c.Request.Context(), c.Param("id"), from, userOf(c).Username)
	})
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func (s *server) deleteHookDraft(c *gin.Context) {
	k, ok := hookKeyOf(c)
	if !ok {
		return
	}
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	if err := s.deps.Hooks.DeleteHookDraft(c.Request.Context(), k, rev); err != nil {
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
	if err := s.deps.Hooks.RenameHook(c.Request.Context(), c.Param("id"), req.Name, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
