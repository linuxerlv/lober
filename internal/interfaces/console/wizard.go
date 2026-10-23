package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
)

type wizardHostItem struct {
	hostResponse
	Selectable bool          `json:"selectable"`
	Problems   []problemJSON `json:"problems"`
}
type wizardHostPage struct {
	Items []wizardHostItem `json:"items"`
	Next  string           `json:"next"`
	Total *int64           `json:"total,omitempty"`
}
type wizardISOItem struct {
	isoResponse
	Usable   bool          `json:"usable"`
	Problems []problemJSON `json:"problems"`
}
type wizardISOResult struct {
	Items []wizardISOItem `json:"items"`
	Next  string          `json:"next"`
	Total *int64          `json:"total,omitempty"`
}
type wizardTemplateItem struct {
	templateResponse
	Usable bool `json:"usable"`
}
type wizardTemplateResult struct {
	Items []wizardTemplateItem `json:"items"`
	Next  string               `json:"next"`
	Total *int64               `json:"total,omitempty"`
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
	selectionPageRequest
}
type wizardTemplateRequest struct {
	ISOID string `json:"isoID" binding:"required"`
	selectionPageRequest
}

type selectionPageRequest struct {
	Cursor   *string `json:"cursor,omitempty"`
	Limit    *int    `json:"limit,omitempty"`
	Page     *int    `json:"page,omitempty"`
	PageSize *int    `json:"page_size,omitempty"`
}

func selectionPagination(c *gin.Context, input selectionPageRequest) (page, limit int, cursor string, ok bool) {
	if input.Page != nil || input.PageSize != nil {
		page, limit = 1, 20
		if input.Page != nil {
			page = *input.Page
		}
		if input.PageSize != nil {
			limit = *input.PageSize
		}
		if page < 1 || limit < 1 || input.Cursor != nil || input.Limit != nil {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return 0, 0, "", false
		}
	} else {
		if input.Cursor != nil {
			cursor = *input.Cursor
		}
		if input.Limit != nil {
			limit = *input.Limit
		}
	}
	if !validPageNumbers(page, limit, cursor) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return 0, 0, "", false
	}
	return page, limit, cursor, true
}

type wizardHostQuery struct {
	Q         string `form:"q"`
	AttrKey   string `form:"attr_key"`
	AttrValue string `form:"attr_value"`
	Cursor    string `form:"cursor"`
	Limit     int    `form:"limit"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
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
	number, limit, ok := parseQueryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (tasks.HostPage, error) {
		return s.deps.Wizard.Hosts(c.Request.Context(), actorOf(c), tasks.HostRequest{PageRequest: tasks.PageRequest{Cursor: c.Query("cursor"), Limit: limit, Page: number}, Q: c.Query("q"), AttrKey: c.Query("attr_key"), AttrValue: c.Query("attr_value")})
	})
	if !ok {
		return
	}
	items := make([]wizardHostItem, len(page.Items))
	for i, item := range page.Items {
		items[i] = wizardHostItem{toHostResponse(item.Host), len(item.Problems) == 0, jsonProblems(item.Problems)}
	}
	Ok(c, wizardHostPage{Items: items, Next: page.Next, Total: page.Total})
}
func (s *server) wizardISOs(c *gin.Context) {
	var req wizardISORequest
	if !Bind(c, &req) {
		return
	}
	number, limit, cursor, ok := selectionPagination(c, req.selectionPageRequest)
	if !ok {
		return
	}
	page, ok := Get(c, func() (tasks.ISOPage, error) {
		return s.deps.Wizard.ISOs(c.Request.Context(), actorOf(c), tasks.ISORequest{PageRequest: tasks.PageRequest{Cursor: cursor, Limit: limit, Page: number}, HostSNs: req.HostSNs})
	})
	if !ok {
		return
	}
	items := make([]wizardISOItem, len(page.Items))
	for i, item := range page.Items {
		items[i] = wizardISOItem{toISOResponse(item.ISO), len(item.Problems) == 0, jsonProblems(item.Problems)}
	}
	Ok(c, wizardISOResult{Items: items, Next: page.Next, Total: page.Total})
}
func (s *server) wizardTemplates(c *gin.Context) {
	var req wizardTemplateRequest
	if !Bind(c, &req) {
		return
	}
	number, limit, cursor, ok := selectionPagination(c, req.selectionPageRequest)
	if !ok {
		return
	}
	page, ok := Get(c, func() (tasks.TemplatePage, error) {
		return s.deps.Wizard.Templates(c.Request.Context(), actorOf(c), tasks.TemplateRequest{PageRequest: tasks.PageRequest{Cursor: cursor, Limit: limit, Page: number}, ISOID: req.ISOID})
	})
	if !ok {
		return
	}
	items := make([]wizardTemplateItem, len(page.Items))
	for i, item := range page.Items {
		items[i] = wizardTemplateItem{toTemplateResponseWithProblems(item.Template, item.Problems), true}
	}
	Ok(c, wizardTemplateResult{Items: items, Next: page.Next, Total: page.Total})
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
func (r wizardSelection) applicationSelection() tasks.Selection {
	return tasks.Selection{ISOID: r.ISOID, TemplateID: r.TemplateID, TemplateVersion: r.TemplateVersion}
}
func (s *server) wizardParams(c *gin.Context) {
	var req wizardParamsRequest
	if !Bind(c, &req) {
		return
	}
	result, ok := Get(c, func() (tasks.ParamsResult, error) {
		return s.deps.Wizard.Params(c.Request.Context(), actorOf(c), tasks.ParamsRequest{Selection: req.applicationSelection(), HostSNs: req.HostSNs, Entered: req.Entered})
	})
	if !ok {
		return
	}
	hosts := make([]wizardParamsHost, len(result.Hosts))
	for i, h := range result.Hosts {
		hosts[i] = wizardParamsHost{h.SN, paramFields(h.Fields), jsonProblems(h.Problems)}
	}
	Ok(c, wizardParamsResult{hosts, jsonProblems(result.Common)})
}
func (s *server) wizardSubmit(c *gin.Context) {
	var req wizardSubmitRequest
	if !Bind(c, &req) {
		return
	}
	hosts := make([]tasks.SubmitHost, len(req.Hosts))
	for i, h := range req.Hosts {
		hosts[i] = tasks.SubmitHost{SN: h.SN, Entered: h.Entered}
	}
	created, ok := Get(c, func() ([]tasks.TaskChoice, error) {
		return s.deps.Wizard.Submit(c.Request.Context(), actorOf(c), tasks.SubmitRequest{Selection: req.applicationSelection(), Hosts: hosts})
	})
	if !ok {
		return
	}
	items := make([]taskItem, len(created))
	for i, item := range created {
		items[i] = toTaskChoiceItem(item)
	}
	Ok(c, wizardSubmitResult{items})
}
