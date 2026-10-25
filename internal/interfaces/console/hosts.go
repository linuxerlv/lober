package console

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
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
	number, limit, ok := parseQueryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (hosts.HostPage, error) {
		return s.deps.HostRegistry.List(c.Request.Context(), actorOf(c), hosts.ListRequest{Prefix: c.Query("prefix"), AttrKey: c.Query("attr_key"), AttrValue: c.Query("attr_value"), Cursor: c.Query("cursor"), Limit: limit, Page: number})
	})
	if !ok {
		return
	}
	Ok(c, hostsPage(page.Items, page.Next, page.Total))
}
func (s *server) getHost(c *gin.Context) {
	h, ok := Get(c, func() (model.Host, error) {
		return s.deps.HostRegistry.Get(c.Request.Context(), actorOf(c), c.Param("sn"))
	})
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
	h, ok := Write(c, func() (model.Host, error) {
		return s.deps.HostRegistry.Create(c.Request.Context(), actorOf(c), hostOf(req))
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
	h, ok := Write(c, func() (model.Host, error) {
		return s.deps.HostRegistry.Update(c.Request.Context(), actorOf(c), hosts.UpdateRequest{PathSN: c.Param("sn"), Host: hostOf(req)})
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
	if err := s.deps.HostRegistry.Delete(c.Request.Context(), actorOf(c), hosts.DeleteRequest{SN: c.Param("sn"), Rev: rev}); err != nil {
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
	result, ok := Write(c, func() (hosts.ImportResult, error) {
		return s.deps.HostRegistry.Import(c.Request.Context(), actorOf(c), hosts.ImportRequest{CSV: data, DryRun: dryRun})
	})
	if !ok {
		return
	}
	if dryRun {
		Ok(c, csvPlanOf(result.Plan))
		return
	}
	Ok(c, hostsPage(result.Hosts, ""))
}
