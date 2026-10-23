package console

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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
func toAuditItem(e repository.AuditEntry) auditItem {
	return auditItem{e.ID, e.At, e.Actor, e.Action, e.Detail, e.TargetKind, e.TargetID}
}
func auditFilters(c *gin.Context) (repository.AuditQuery, bool) {
	from, ok := queryNonnegative(c, "from")
	if !ok {
		return repository.AuditQuery{}, false
	}
	to, ok := queryNonnegative(c, "to")
	if !ok {
		return repository.AuditQuery{}, false
	}
	if to != 0 && from > to {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return repository.AuditQuery{}, false
	}
	return repository.AuditQuery{TargetKind: c.Query("target_kind"), TargetID: c.Query("target_id"), Actor: c.Query("actor"), Action: c.Query("action"), From: from, To: to}, true
}
func (s *server) listAudit(c *gin.Context) {
	q, ok := auditFilters(c)
	if !ok {
		return
	}
	number, limit, ok := queryPagination(c)
	if !ok {
		return
	}
	q.Limit, q.Cursor, q.Page = limit, c.Query("cursor"), number
	page, ok := Get(c, func() (repository.Page[repository.AuditEntry], error) {
		return s.deps.Audit.ListAudit(c.Request.Context(), q)
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

// CSV quoting protects separators, not spreadsheet formulas. This export is
// intended for Excel: prefix risky text with a tab, which encoding/csv quotes
// inside the field. Retain the original text, including whitespace. JSON audit
// reads remain the lossless interface for machine consumers.
func csvText(raw string) string {
	meaningful := strings.TrimLeftFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) })
	first, _ := utf8.DecodeRuneInString(meaningful)
	if strings.ContainsRune("=+-@＝＋－＠", first) || len(raw) > 0 && strings.ContainsRune("\t\r\n", rune(raw[0])) {
		return "\t" + raw
	}
	return raw
}
func (s *server) exportAudit(c *gin.Context) {
	q, ok := auditFilters(c)
	if !ok {
		return
	}
	q.Limit = 500
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	if err := writer.Write([]string{"id", "at", "actor", "action", "target_kind", "target_id", "detail"}); err != nil {
		RespondError(c, err)
		return
	}
	// Buffer until all database pages succeed. A late read failure must still be
	// a JSON error, not a seemingly successful truncated CSV download.
	for {
		page, ok := Get(c, func() (repository.Page[repository.AuditEntry], error) {
			return s.deps.Audit.ListAudit(c.Request.Context(), q)
		})
		if !ok {
			return
		}
		for _, e := range page.Items {
			if err := writer.Write([]string{strconv.FormatInt(e.ID, 10), strconv.FormatInt(e.At, 10), csvText(e.Actor), csvText(e.Action), csvText(e.TargetKind), csvText(e.TargetID), csvText(e.Detail)}); err != nil {
				RespondError(c, err)
				return
			}
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		RespondError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="audit.csv"`)
	c.Data(200, "text/csv; charset=utf-8", data.Bytes())
}
