package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/audit"
)

type auditItem struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetID"`
}
type auditPage struct {
	Items []auditItem `json:"items"`
	Next  string      `json:"next"`
	Total *int64      `json:"total,omitempty"`
}
type auditQuery struct {
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
	TargetKind string `form:"target_kind"`
	TargetID   string `form:"target_id"`
	Actor      string `form:"actor"`
	Action     string `form:"action"`
	From       int64  `form:"from"`
	To         int64  `form:"to"`
	Cursor     string `form:"cursor"`
	Limit      int    `form:"limit"`
}
type auditExportQuery struct {
	TargetKind string `form:"target_kind"`
	TargetID   string `form:"target_id"`
	Actor      string `form:"actor"`
	Action     string `form:"action"`
	From       int64  `form:"from"`
	To         int64  `form:"to"`
}

func (s *server) auditRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/audit", perm: PermAuditRead, tag: "audit", summary: "Read audit entries with descending ID pagination", query: auditQuery{}, resp: auditPage{}, handle: s.listAudit},
		{method: "GET", path: "/console/v1/audit/export", perm: PermAuditRead, tag: "audit", summary: "Export every matching audit entry as formula-safe CSV", query: auditExportQuery{}, resp: "", responseMedia: "text/csv", raw: true, handle: s.exportAudit},
	}
}
func toAuditItem(e audit.Entry) auditItem {
	return auditItem{e.ID, e.At, e.Actor, e.Action, e.Detail, e.TargetKind, e.TargetID}
}
func auditFilters(c *gin.Context) (audit.Filter, bool) {
	from, ok := queryNonnegative(c, "from")
	if !ok {
		return audit.Filter{}, false
	}
	to, ok := queryNonnegative(c, "to")
	if !ok {
		return audit.Filter{}, false
	}
	return audit.Filter{TargetKind: c.Query("target_kind"), TargetID: c.Query("target_id"), Actor: c.Query("actor"), Action: c.Query("action"), From: from, To: to}, true
}
func (s *server) listAudit(c *gin.Context) {
	filter, ok := auditFilters(c)
	if !ok {
		return
	}
	number, limit, ok := parseQueryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (audit.Page, error) {
		return s.deps.AuditReader.List(c.Request.Context(), actorOf(c), audit.Query{Filter: filter, Limit: limit, Cursor: c.Query("cursor"), Page: number})
	})
	if !ok {
		return
	}
	items := make([]auditItem, len(page.Items))
	for i, e := range page.Items {
		items[i] = toAuditItem(e)
	}
	Ok(c, auditPage{items, page.Next, page.Total})
}
func (s *server) exportAudit(c *gin.Context) {
	filter, ok := auditFilters(c)
	if !ok {
		return
	}
	data, ok := Get(c, func() ([]byte, error) { return s.deps.AuditReader.ExportCSV(c.Request.Context(), actorOf(c), filter) })
	if !ok {
		return
	}
	c.Header("Content-Disposition", `attachment; filename="audit.csv"`)
	c.Data(200, "text/csv; charset=utf-8", data)
}
