package console

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/builtin"
	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/snapshot"
	"github.com/lober/lober/internal/store"
)

type wizardHostItem struct {
	hostResponse
	Selectable bool          `json:"selectable"`
	Problems   []problemJSON `json:"problems"`
}
type wizardHostPage struct {
	Items []wizardHostItem `json:"items"`
	Next  string           `json:"next"`
}
type wizardISOItem struct {
	isoResponse
	Usable   bool          `json:"usable"`
	Problems []problemJSON `json:"problems"`
}
type wizardISOResult struct {
	Items []wizardISOItem `json:"items"`
	Next  string          `json:"next"`
}
type wizardTemplateItem struct {
	templateResponse
	Usable bool `json:"usable"`
}
type wizardTemplateResult struct {
	Items []wizardTemplateItem `json:"items"`
	Next  string               `json:"next"`
}
type paramFieldDTO struct {
	Key      params.FieldKey  `json:"key"`
	Label    string           `json:"label"`
	Type     model.ParamType  `json:"type"`
	List     bool             `json:"list"`
	Required bool             `json:"required"`
	Locked   bool             `json:"locked"`
	Prefill  string           `json:"prefill"`
	Options  []optionResponse `json:"options"`
}
type wizardParamsHost struct {
	SN       string          `json:"sn"`
	Fields   []paramFieldDTO `json:"fields"`
	Problems []problemJSON   `json:"problems"`
}
type wizardParamsResult struct {
	Hosts  []wizardParamsHost `json:"hosts"`
	Common []problemJSON      `json:"common"`
}
type wizardSelection struct {
	ISOID           string `json:"isoID" binding:"required"`
	TemplateID      string `json:"templateID" binding:"required"`
	TemplateVersion int    `json:"templateVersion" binding:"required,gt=0"`
}
type wizardSubmitHost struct {
	SN      string            `json:"sn"`
	Entered map[string]string `json:"entered,omitempty"`
}
type wizardSubmitRequest struct {
	wizardSelection
	Hosts []wizardSubmitHost `json:"hosts" binding:"required,min=1"`
}
type wizardSubmitResult struct {
	Tasks []taskItem `json:"tasks"`
}
type wizardParamsRequest struct {
	wizardSelection
	HostSNs []string                     `json:"hostSNs" binding:"required,min=1"`
	Entered map[string]map[string]string `json:"entered,omitempty"`
}
type wizardISORequest struct {
	HostSNs []string `json:"hostSNs" binding:"required,min=1"`
	Cursor  string   `json:"cursor,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}
type wizardTemplateRequest struct {
	ISOID  string `json:"isoID" binding:"required"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type wizardHostQuery struct {
	Q         string `form:"q"`
	AttrKey   string `form:"attr_key"`
	AttrValue string `form:"attr_value"`
	Cursor    string `form:"cursor"`
	Limit     int    `form:"limit"`
}

func (s *server) wizardRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/wizard/hosts", perm: PermTaskCreate, tag: "wizard", summary: "Choose registered hosts and show open-task blockers", query: wizardHostQuery{}, resp: wizardHostPage{}, handle: s.wizardHosts},
		{method: "POST", path: "/console/v1/wizard/isos", perm: PermTaskCreate, tag: "wizard", summary: "Choose ready ISO with per-host compatibility", req: wizardISORequest{}, resp: wizardISOResult{}, handle: s.wizardISOs},
		{method: "POST", path: "/console/v1/wizard/templates", perm: PermTaskCreate, tag: "wizard", summary: "Choose currently approved usable template versions", req: wizardTemplateRequest{}, resp: wizardTemplateResult{}, handle: s.wizardTemplates},
		{method: "POST", path: "/console/v1/wizard/params", perm: PermTaskCreate, tag: "wizard", summary: "Evaluate per-host parameter forms", req: wizardParamsRequest{}, resp: wizardParamsResult{}, handle: s.wizardParams},
		{method: "POST", path: "/console/v1/wizard/tasks", perm: PermTaskCreate, audit: "task.create", tag: "wizard", summary: "Revalidate and atomically create the entire task batch", req: wizardSubmitRequest{}, resp: wizardSubmitResult{}, handle: s.wizardSubmit},
	}
}
func (s *server) wizardHosts(c *gin.Context) {
	limit, ok := queryLimit(c)
	if !ok {
		return
	}
	prefix := c.Query("q")
	if normalized, err := model.NormalizeSN(prefix); err == nil {
		prefix = normalized
	}
	page, ok := Get(c, func() (store.Page[model.Host], error) {
		return s.deps.Hosts.ListHosts(c.Request.Context(), store.HostQuery{SNPrefix: prefix, AttrKey: c.Query("attr_key"), AttrValue: c.Query("attr_value"), Cursor: c.Query("cursor"), Limit: limit})
	})
	if !ok {
		return
	}
	items := make([]wizardHostItem, len(page.Items))
	for i, h := range page.Items {
		_, open, err := s.deps.Tasks.FindOpenTask(c.Request.Context(), h.SN)
		if err != nil {
			RespondError(c, err)
			return
		}
		ps := admission.HostSelectable(h, open)
		items[i] = wizardHostItem{toHostResponse(h), len(ps) == 0, jsonProblems(ps)}
	}
	Ok(c, wizardHostPage{items, page.Next})
}
func wizardSNs(c *gin.Context, raw []string) ([]string, bool) {
	out := make([]string, len(raw))
	var ps []model.Problem
	for i, value := range raw {
		sn, err := model.NormalizeSN(value)
		if err != nil {
			var p model.Problem
			if errors.As(err, &p) {
				ps = append(ps, p)
			} else {
				RespondError(c, err)
				return nil, false
			}
		} else {
			out[i] = sn
		}
	}
	if !Judge(c, ps) {
		return nil, false
	}
	return out, true
}
func (s *server) wizardHostRecords(c *gin.Context, sns []string) ([]model.Host, []string, bool) {
	records, ok := Get(c, func() (map[string]model.Host, error) { return s.deps.Hosts.GetHosts(c.Request.Context(), sns) })
	if !ok {
		return nil, nil, false
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
	return hosts, missing, true
}
func missingWizardHosts(c *gin.Context, hosts []model.Host, missing []string) bool {
	byHost := map[string][]model.Problem{}
	seen := map[string]bool{}
	for _, h := range hosts {
		if seen[h.SN] {
			byHost[h.SN] = []model.Problem{model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: model.KindHost, ID: h.SN})}
		}
		seen[h.SN] = true
	}
	for _, sn := range missing {
		byHost[sn] = append(byHost[sn], model.NewProblem("host_not_registered", model.Subject{Kind: model.KindHost, ID: sn}))
	}
	return respondWizardBatch(c, admission.BatchResult{Common: []model.Problem{}, ByHost: byHost})
}
func respondWizardBatch(c *gin.Context, result admission.BatchResult) bool {
	bad := len(result.Common) > 0
	for _, ps := range result.ByHost {
		bad = bad || len(ps) > 0
	}
	if !bad {
		return true
	}
	RespondError(c, &store.BatchError{Result: result})
	return false
}
func (s *server) wizardISOs(c *gin.Context) {
	var req wizardISORequest
	if !Bind(c, &req) {
		return
	}
	sns, ok := wizardSNs(c, req.HostSNs)
	if !ok {
		return
	}
	hosts, missing, ok := s.wizardHostRecords(c, sns)
	if !ok || !missingWizardHosts(c, hosts, missing) {
		return
	}
	page, ok := Get(c, func() (store.Page[model.ISO], error) {
		return s.deps.ISOs.ListISOs(c.Request.Context(), store.ISOQuery{States: []model.ISOState{model.ISOReady}, Cursor: req.Cursor, Limit: req.Limit})
	})
	if !ok {
		return
	}
	items := make([]wizardISOItem, len(page.Items))
	for i, item := range page.Items {
		ps := admission.ISOUsable(item, hosts)
		items[i] = wizardISOItem{toISOResponse(item), len(ps) == 0, jsonProblems(ps)}
	}
	Ok(c, wizardISOResult{items, page.Next})
}
func (s *server) wizardTemplates(c *gin.Context) {
	var req wizardTemplateRequest
	if !Bind(c, &req) {
		return
	}
	iso, ok := Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), req.ISOID) })
	if !ok || !Judge(c, iso.Unusable()) {
		return
	}
	page, ok := Get(c, func() (store.Page[model.Template], error) {
		return s.deps.Templates.ListTemplates(c.Request.Context(), store.TemplateQuery{ApprovedOnly: true, Series: iso.Series, Cursor: req.Cursor, Limit: req.Limit})
	})
	if !ok {
		return
	}
	hooks, ok := Get(c, func() (map[model.HookKey]model.Hook, error) { return s.templateHooks(c.Request.Context(), page.Items) })
	if !ok {
		return
	}
	items := make([]wizardTemplateItem, 0, len(page.Items))
	for _, t := range page.Items {
		if !currentBuiltinSelection(t, hooks) {
			continue
		}
		ps := admission.TemplateUsable(t, hooks, iso)
		if len(ps) != 0 {
			continue
		}
		items = append(items, wizardTemplateItem{toTemplateResponse(t, hooks), true})
	}
	Ok(c, wizardTemplateResult{items, page.Next})
}

// The shipped manifest is immutable program data; persisted obsolete builtin
// versions remain available to existing frozen tasks but are not wizard choices.
func currentBuiltinSelection(t model.Template, hooks map[model.HookKey]model.Hook) bool {
	if t.Builtin {
		found := false
		for _, shipped := range builtin.Templates() {
			found = found || t.ID == shipped.ID && t.Version == shipped.Version
		}
		if !found {
			return false
		}
	}
	shippedHooks := map[model.HookKey]bool{}
	for _, h := range builtin.Hooks() {
		shippedHooks[model.HookKey{ID: h.ID, Version: h.Version}] = true
	}
	for _, key := range store.HookRefs(t) {
		if h, exists := hooks[key]; exists && h.Builtin && !shippedHooks[key] {
			return false
		}
	}
	return true
}
func (s *server) wizardSelectionFacts(c *gin.Context, req wizardSelection) (model.Template, model.ISO, map[model.HookKey]model.Hook, bool) {
	key := model.TemplateKey{ID: req.TemplateID, Version: req.TemplateVersion}
	t, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), key) })
	if !ok {
		return model.Template{}, model.ISO{}, nil, false
	}
	versions, ok := Get(c, func() ([]model.Template, error) {
		return s.deps.Templates.ListTemplateVersions(c.Request.Context(), t.ID)
	})
	if !ok {
		return model.Template{}, model.ISO{}, nil, false
	}
	current, exists := model.CurrentApproved(versions)
	if !exists || current.Version != t.Version {
		Fail(c, 409, nil, model.ConsoleTemplateCurrent)
		return model.Template{}, model.ISO{}, nil, false
	}
	iso, ok := Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), req.ISOID) })
	if !ok {
		return model.Template{}, model.ISO{}, nil, false
	}
	hooks, ok := Get(c, func() (map[model.HookKey]model.Hook, error) {
		return s.templateHooks(c.Request.Context(), []model.Template{t})
	})
	if ok && !currentBuiltinSelection(t, hooks) {
		Fail(c, 409, nil, model.ConsoleTemplateCurrent)
		return model.Template{}, model.ISO{}, nil, false
	}
	return t, iso, hooks, ok
}
func (s *server) wizardBatch(c *gin.Context, t model.Template, iso model.ISO, hooks map[model.HookKey]model.Hook, hosts []model.Host, missing []string) (admission.BatchResult, bool) {
	open := map[string]bool{}
	for _, h := range hosts {
		if _, done := open[h.SN]; done {
			continue
		}
		_, has, err := s.deps.Tasks.FindOpenTask(c.Request.Context(), h.SN)
		if err != nil {
			RespondError(c, err)
			return admission.BatchResult{}, false
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
	return result, true
}
func paramFields(fields []params.Field) []paramFieldDTO {
	out := make([]paramFieldDTO, len(fields))
	for i, f := range fields {
		options := make([]optionResponse, len(f.Options))
		for j, o := range f.Options {
			options[j] = optionResponse(o)
		}
		out[i] = paramFieldDTO{f.Key, f.Label, f.Type, f.List, f.Required, f.Locked, f.Prefill, options}
	}
	return out
}
func wizardEntered(c *gin.Context, sns []string, raw map[string]map[string]string) (map[string]map[string]string, bool) {
	selected := make(map[string]bool, len(sns))
	for _, sn := range sns {
		selected[sn] = true
	}
	out := make(map[string]map[string]string, len(raw))
	for key, values := range raw {
		sn, err := model.NormalizeSN(key)
		_, duplicate := out[sn]
		if err != nil || !selected[sn] || duplicate {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return nil, false
		}
		out[sn] = values
	}
	return out, true
}
func (s *server) wizardParams(c *gin.Context) {
	var req wizardParamsRequest
	if !Bind(c, &req) {
		return
	}
	s.templateSelection.RLock()
	defer s.templateSelection.RUnlock()
	t, iso, hooks, ok := s.wizardSelectionFacts(c, req.wizardSelection)
	if !ok {
		return
	}
	sns, ok := wizardSNs(c, req.HostSNs)
	if !ok {
		return
	}
	entered, ok := wizardEntered(c, sns, req.Entered)
	if !ok {
		return
	}
	hosts, missing, ok := s.wizardHostRecords(c, sns)
	if !ok {
		return
	}
	result, ok := s.wizardBatch(c, t, iso, hooks, hosts, missing)
	if !ok {
		return
	}
	absent := map[string]bool{}
	for _, sn := range missing {
		absent[sn] = true
	}
	out := make([]wizardParamsHost, len(hosts))
	for i, h := range hosts {
		fields := []paramFieldDTO{}
		ps := append([]model.Problem{}, result.ByHost[h.SN]...)
		if !absent[h.SN] && len(result.Common) == 0 {
			res := params.Evaluate(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredOf(entered[h.SN])})
			fields = paramFields(res.Fields)
			ps = append(ps, res.Problems...)
		}
		out[i] = wizardParamsHost{h.SN, fields, jsonProblems(ps)}
	}
	Ok(c, wizardParamsResult{out, jsonProblems(result.Common)})
}
func (s *server) wizardSubmit(c *gin.Context) {
	var req wizardSubmitRequest
	if !Bind(c, &req) {
		return
	}
	// Current-approved is a console-owned rule. Serialize console approvals and
	// deprecations with this fresh DB read through the atomic task transaction.
	s.templateSelection.RLock()
	defer s.templateSelection.RUnlock()
	t, iso, hooks, ok := s.wizardSelectionFacts(c, req.wizardSelection)
	if !ok {
		return
	}
	raw := make([]string, len(req.Hosts))
	for i, h := range req.Hosts {
		raw[i] = h.SN
	}
	sns, ok := wizardSNs(c, raw)
	if !ok {
		return
	}
	hosts, missing, ok := s.wizardHostRecords(c, sns)
	if !ok {
		return
	}
	result, ok := s.wizardBatch(c, t, iso, hooks, hosts, missing)
	if !ok {
		return
	}
	absent := map[string]bool{}
	for _, sn := range missing {
		absent[sn] = true
	}
	tasks := make([]model.Task, 0, len(hosts))
	seen := map[string]bool{}
	for i, h := range hosts {
		if seen[h.SN] || absent[h.SN] || len(result.Common) > 0 {
			continue
		}
		seen[h.SN] = true
		frozen, ps := snapshot.Build(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredOf(req.Hosts[i].Entered)})
		result.ByHost[h.SN] = append(result.ByHost[h.SN], ps...)
		if len(ps) == 0 {
			tasks = append(tasks, model.Task{HostSN: h.SN, ISOID: iso.ID, TemplateID: t.ID, TemplateVersion: t.Version, Snapshot: frozen, CreatedBy: userOf(c).Username, CreatedAt: s.clock()})
		}
	}
	if !respondWizardBatch(c, result) {
		return
	}
	created, ok := Write(c, func() ([]model.Task, error) {
		return s.deps.Tasks.CreateTasks(c.Request.Context(), tasks, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	items := make([]taskItem, len(created))
	for i, task := range created {
		items[i] = s.taskResponse(task)
		s.hub.Publish(task.ID)
	}
	Ok(c, wizardSubmitResult{items})
}
