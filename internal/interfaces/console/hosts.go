package console

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type hostRequest struct {
	SN       string            `json:"sn"`
	Arch     catalog.Arch      `json:"arch,omitempty"`
	Firmware catalog.Firmware  `json:"firmware,omitempty"`
	Attrs    map[string]string `json:"attrs"`
	Rev      int64             `json:"rev"`
}
type hostResponse struct {
	SN       string            `json:"sn"`
	Arch     catalog.Arch      `json:"arch" openapi:"allowEmpty"`
	Firmware catalog.Firmware  `json:"firmware" openapi:"allowEmpty"`
	Attrs    map[string]string `json:"attrs"`
	Rev      int64             `json:"rev"`
}
type hostPage struct {
	Items []hostResponse `json:"items"`
	Next  string         `json:"next"`
	Total *int64         `json:"total,omitempty"`
}
type hostQuery struct {
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
	Prefix    string `form:"prefix"`
	AttrKey   string `form:"attr_key"`
	AttrValue string `form:"attr_value"`
	Cursor    string `form:"cursor"`
	Limit     int    `form:"limit"`
}
type revisionQuery struct {
	Rev int64 `form:"rev" binding:"required,gt=0"`
}
type importQuery struct {
	DryRun bool `form:"dryRun"`
}

func toHostResponse(h model.Host) hostResponse {
	attrs := h.Attrs
	if attrs == nil {
		attrs = map[string]string{}
	}
	return hostResponse{h.SN, h.Arch, h.Firmware, attrs, h.Rev}
}
func (s *server) hostRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/hosts", perm: PermHostRead, tag: "hosts", summary: "List hosts", query: hostQuery{}, resp: hostPage{}, handle: s.listHosts},
		{method: "POST", path: "/console/v1/hosts", perm: PermHostWrite, audit: "host.create", tag: "hosts", summary: "Create host", req: hostRequest{}, resp: hostResponse{}, handle: s.createHost},
		{method: "POST", path: "/console/v1/hosts/import", perm: PermHostWrite, audit: "host.import", tag: "hosts", summary: "Preview or import host CSV", query: importQuery{}, req: "", requestMedia: "text/csv", resp: csvPlan{}, respVariants: []any{hostPage{}}, errorData: csvPlan{}, handle: s.importHosts},
		{method: "GET", path: "/console/v1/hosts/:sn", perm: PermHostRead, tag: "hosts", summary: "Get host", resp: hostResponse{}, handle: s.getHost},
		{method: "PUT", path: "/console/v1/hosts/:sn", perm: PermHostWrite, audit: "host.update", tag: "hosts", summary: "Replace host", req: hostRequest{}, resp: hostResponse{}, handle: s.updateHost},
		{method: "DELETE", path: "/console/v1/hosts/:sn", perm: PermHostWrite, audit: "host.delete", tag: "hosts", summary: "Delete host", query: revisionQuery{}, handle: s.deleteHost},
	}
}
func (s *server) listHosts(c *gin.Context) {
	number, limit, ok := queryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (repository.Page[model.Host], error) {
		return s.deps.Hosts.ListHosts(c.Request.Context(), repository.HostQuery{SNPrefix: c.Query("prefix"), AttrKey: c.Query("attr_key"), AttrValue: c.Query("attr_value"), Cursor: c.Query("cursor"), Limit: limit, Page: number})
	})
	if !ok {
		return
	}
	Ok(c, hostsPage(page.Items, page.Next, page.Total))
}
func normalizedHostSN(c *gin.Context, raw string) (string, bool) {
	sn, err := model.NormalizeSN(raw)
	if err != nil {
		RespondError(c, err)
		return "", false
	}
	return sn, true
}
func (s *server) getHost(c *gin.Context) {
	sn, ok := normalizedHostSN(c, c.Param("sn"))
	if !ok {
		return
	}
	h, ok := Get(c, func() (model.Host, error) { return s.deps.Hosts.GetHost(c.Request.Context(), sn) })
	if ok {
		Ok(c, toHostResponse(h))
	}
}
func hostOf(req hostRequest) model.Host {
	return model.Host{SN: req.SN, Arch: req.Arch, Firmware: req.Firmware, Attrs: req.Attrs, Rev: req.Rev}
}
func (s *server) createHost(c *gin.Context) {
	var req hostRequest
	if !Bind(c, &req) {
		return
	}
	sn, ok := normalizedHostSN(c, req.SN)
	if !ok {
		return
	}
	req.SN = sn
	h, ok := Write(c, func() (model.Host, error) {
		return s.deps.Hosts.CreateHost(c.Request.Context(), hostOf(req), s.auditFrom(c, ""))
	})
	if ok {
		Ok(c, toHostResponse(h))
	}
}
func (s *server) updateHost(c *gin.Context) {
	var req hostRequest
	if !Bind(c, &req) {
		return
	}
	if req.Rev < 1 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	sn, ok := normalizedHostSN(c, c.Param("sn"))
	if !ok {
		return
	}
	if req.SN != "" {
		bodySN, ok := normalizedHostSN(c, req.SN)
		if !ok {
			return
		}
		if bodySN != sn {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
	}
	req.SN = sn
	h, ok := Write(c, func() (model.Host, error) {
		return s.deps.Hosts.UpdateHost(c.Request.Context(), hostOf(req), s.auditFrom(c, ""))
	})
	if ok {
		Ok(c, toHostResponse(h))
	}
}
func queryRev(c *gin.Context) (int64, bool) {
	rev, err := strconv.ParseInt(c.Query("rev"), 10, 64)
	if err != nil || rev < 1 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return 0, false
	}
	return rev, true
}
func (s *server) deleteHost(c *gin.Context) {
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	sn, ok := normalizedHostSN(c, c.Param("sn"))
	if !ok {
		return
	}
	if err := s.deps.Hosts.DeleteHost(c.Request.Context(), sn, rev, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func hostsPage(hs []model.Host, next string, total ...*int64) hostPage {
	items := make([]hostResponse, len(hs))
	for i, h := range hs {
		items[i] = toHostResponse(h)
	}
	var count *int64
	if len(total) > 0 {
		count = total[0]
	}
	return hostPage{items, next, count}
}
func (s *server) importHosts(c *gin.Context) {
	dryRun := false
	if raw, exists := c.GetQuery("dryRun"); exists {
		if raw != "true" && raw != "false" {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		dryRun = raw == "true"
	}
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	p, hosts, err := parseHostCSV(data, nil)
	if err != nil {
		Fail(c, 400, nil, err.Error())
		return
	}
	sns := make([]string, len(hosts))
	for i, h := range hosts {
		sns[i] = h.SN
	}
	known, ok := Get(c, func() (map[string]model.Host, error) { return s.deps.Hosts.GetHosts(c.Request.Context(), sns) })
	if !ok {
		return
	}
	rejected := false
	message := model.ConsoleRejected
	for i := range p.Rows {
		row := &p.Rows[i]
		if row.Action == "error" {
			if !rejected {
				message = row.Problems[0].Message
			}
			rejected = true
			continue
		}
		if _, exists := known[row.SN]; exists {
			row.Action = "update"
		}
	}
	if dryRun {
		Ok(c, p)
		return
	}
	if rejected {
		Fail(c, 422, p, message)
		return
	}
	imported, ok := Write(c, func() ([]model.Host, error) {
		return s.deps.Hosts.ImportHosts(c.Request.Context(), hosts, s.auditFrom(c, ""))
	})
	if ok {
		Ok(c, hostsPage(imported, ""))
	}
}
