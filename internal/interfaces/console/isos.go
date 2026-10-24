package console

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type isoRequest struct {
	Name    string         `json:"name"`
	Family  catalog.Family `json:"family" binding:"required"`
	Series  catalog.Series `json:"series" binding:"required"`
	Sources []isoSourceReq `json:"sources" binding:"required,min=1"`
}
type isoSourceReq struct {
	Kind   SourceKind `json:"kind" binding:"required"`
	Addr   string     `json:"addr,omitempty"`
	Role   string     `json:"role,omitempty"`
	Size   int64      `json:"size,omitempty"`
	SHA256 string     `json:"sha256,omitempty"`
}
type optionResponse struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type isoResponse struct {
	ID        string                      `json:"id"`
	Name      string                      `json:"name"`
	Family    catalog.Family              `json:"family"`
	Series    catalog.Series              `json:"series"`
	Version   string                      `json:"version"`
	Arch      catalog.Arch                `json:"arch"`
	State     model.ISOState              `json:"state"`
	SHA256    string                      `json:"sha256"`
	Artifacts map[string]string           `json:"artifacts"`
	Offers    map[string][]optionResponse `json:"offers"`
	Rev       int64                       `json:"rev"`
}
type isoPage struct {
	Items []isoResponse `json:"items"`
	Next  string        `json:"next"`
	Total *int64        `json:"total,omitempty"`
}
type sourceRange struct {
	Start  int64 `json:"start"`
	Length int64 `json:"length"`
}
type sourceProgress struct {
	Role      string            `json:"role"`
	Kind      SourceKind        `json:"kind"`
	TotalSize int64             `json:"totalSize"`
	Fetched   int64             `json:"fetched"`
	State     model.SourceState `json:"state"`
	Error     string            `json:"error"`
	Rev       int64             `json:"rev"`
	Ranges    []sourceRange     `json:"ranges"`
}
type isoProgress struct {
	ID      string           `json:"id"`
	State   model.ISOState   `json:"state"`
	Rev     int64            `json:"rev"`
	Sources []sourceProgress `json:"sources"`
}
type isoQuery struct {
	Page     int            `form:"page"`
	PageSize int            `form:"page_size"`
	Family   catalog.Family `form:"family"`
	Series   catalog.Series `form:"series"`
	State    model.ISOState `form:"state"`
	Cursor   string         `form:"cursor"`
	Limit    int            `form:"limit"`
}
type sourceQuery struct {
	Role string `form:"role"`
}
type chunkHeaders struct {
	ContentRange string `form:"Content-Range" binding:"required"`
}

func toISOResponse(item model.ISO) isoResponse {
	artifacts := item.Artifacts
	if artifacts == nil {
		artifacts = map[string]string{}
	}
	offers := make(map[string][]optionResponse, len(item.Offers))
	for name, options := range item.Offers {
		values := make([]optionResponse, len(options))
		for i, o := range options {
			values[i] = optionResponse{o.Value, o.Label}
		}
		offers[name] = values
	}
	return isoResponse{item.ID, item.Name, item.Family, item.Series, item.Version, item.Arch, item.State, item.SHA256, artifacts, offers, item.Rev}
}
func (s *server) isoRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/isos", perm: PermISORead, tag: "isos", summary: "List ISOs", query: isoQuery{}, resp: isoPage{}, handle: s.listISOs},
		{method: "POST", path: "/console/v1/isos", perm: PermISOWrite, audit: "iso.create", tag: "isos", summary: "Create ISO and register sources", req: isoRequest{}, resp: isoResponse{}, handle: s.createISO},
		{method: "GET", path: "/console/v1/isos/:id", perm: PermISORead, tag: "isos", summary: "Get ISO", resp: isoResponse{}, handle: s.getISO},
		{method: "GET", path: "/console/v1/isos/:id/progress", perm: PermISORead, tag: "isos", summary: "Read persisted source ranges and progress", resp: isoProgress{}, handle: s.isoProgress},
		{method: "POST", path: "/console/v1/isos/:id/sources", perm: PermISOWrite, audit: "iso.source.register", tag: "isos", summary: "Register a missing ISO source", req: isoSourceReq{}, handle: s.registerISOSource},
		{method: "DELETE", path: "/console/v1/isos/:id", perm: PermISOWrite, audit: "iso.delete", tag: "isos", summary: "Delete ISO with durable cleanup", query: revisionQuery{}, handle: s.deleteISO},
		{method: "POST", path: "/console/v1/isos/:id/deprecate", perm: PermISOWrite, audit: "iso.deprecate", tag: "isos", summary: "Deprecate ISO", query: revisionQuery{}, handle: s.deprecateISO},
		{method: "POST", path: "/console/v1/isos/:id/cancel", perm: PermISOWrite, audit: "iso.cancel", tag: "isos", summary: "Cancel ISO acquisition", handle: s.cancelISO},
		{method: "POST", path: "/console/v1/isos/:id/retry", perm: PermISOWrite, audit: "iso.retry", tag: "isos", summary: "Retry ISO acquisition", handle: s.retryISO},
		{method: "POST", path: "/console/v1/isos/:id/reimport", perm: PermISOWrite, audit: "iso.reimport", tag: "isos", summary: "Reimport acquired ISO", handle: s.reimportISO},
		{method: "PUT", path: "/console/v1/isos/:id/content", perm: PermISOWrite, tag: "isos", summary: "Upload one exact byte range", query: sourceQuery{}, headers: chunkHeaders{}, req: "", requestMedia: "application/octet-stream", handle: s.uploadChunk},
	}
}
func (s *server) listISOs(c *gin.Context) {
	number, limit, ok := parseQueryPagination(c)
	if !ok {
		return
	}
	page, ok := Get(c, func() (media.ISOPage, error) {
		return s.deps.MediaRegistry.List(c.Request.Context(), actorOf(c), media.ListRequest{Page: number, Limit: limit, Cursor: c.Query("cursor"), Family: catalog.Family(c.Query("family")), Series: catalog.Series(c.Query("series")), State: model.ISOState(c.Query("state"))})
	})
	if !ok {
		return
	}
	items := make([]isoResponse, len(page.Items))
	for i, item := range page.Items {
		items[i] = toISOResponse(item)
	}
	Ok(c, isoPage{items, page.Next, page.Total})
}
func (s *server) getISO(c *gin.Context) {
	item, ok := Get(c, func() (model.ISO, error) {
		return s.deps.MediaRegistry.Get(c.Request.Context(), actorOf(c), c.Param("id"))
	})
	if ok {
		Ok(c, toISOResponse(item))
	}
}
func sourceSpec(r isoSourceReq) media.SourceSpec {
	return media.SourceSpec{Kind: string(r.Kind), Addr: r.Addr, Role: r.Role, SHA256: r.SHA256, Size: r.Size}
}
func (s *server) createISO(c *gin.Context) {
	var req isoRequest
	if !Bind(c, &req) {
		return
	}
	sources := make([]media.SourceSpec, len(req.Sources))
	for i, src := range req.Sources {
		sources[i] = sourceSpec(src)
	}
	result, err := s.deps.MediaRegistry.Create(c.Request.Context(), actorOf(c), media.CreateRequest{Name: req.Name, Family: req.Family, Series: req.Series, Sources: sources})
	if result.Created {
		c.Header("Location", "/console/v1/isos/"+result.ISO.ID)
	}
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, toISOResponse(result.ISO))
}
func (s *server) registerISOSource(c *gin.Context) {
	var req isoSourceReq
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.MediaRegistry.RegisterSource(c.Request.Context(), actorOf(c), media.RegisterRequest{ID: c.Param("id"), Source: sourceSpec(req)}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) deleteISO(c *gin.Context) {
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	if err := s.deps.MediaRegistry.Delete(c.Request.Context(), actorOf(c), media.RevisionRequest{ID: c.Param("id"), Rev: rev}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) deprecateISO(c *gin.Context) {
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	if err := s.deps.MediaRegistry.Deprecate(c.Request.Context(), actorOf(c), media.RevisionRequest{ID: c.Param("id"), Rev: rev}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) sourceOperation(c *gin.Context, fn func(context.Context, ports.Actor, string) error) {
	if err := fn(c.Request.Context(), actorOf(c), c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) cancelISO(c *gin.Context)   { s.sourceOperation(c, s.deps.MediaRegistry.Cancel) }
func (s *server) retryISO(c *gin.Context)    { s.sourceOperation(c, s.deps.MediaRegistry.Retry) }
func (s *server) reimportISO(c *gin.Context) { s.sourceOperation(c, s.deps.MediaRegistry.Reimport) }
func ParseContentRange(header string) (start, end, total int64, ok bool) {
	rest, ok := strings.CutPrefix(header, "bytes ")
	if !ok {
		return 0, 0, 0, false
	}
	span, denominator, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, 0, 0, false
	}
	lo, hi, ok := strings.Cut(span, "-")
	if !ok {
		return 0, 0, 0, false
	}
	integer := func(text string) (int64, bool) {
		if text == "" {
			return 0, false
		}
		for _, c := range text {
			if c < '0' || c > '9' {
				return 0, false
			}
		}
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil
	}
	start, ok = integer(lo)
	if !ok {
		return 0, 0, 0, false
	}
	end, ok = integer(hi)
	if !ok || end < start || end == math.MaxInt64 {
		return 0, 0, 0, false
	}
	if denominator == "*" {
		return start, end, -1, true
	}
	total, ok = integer(denominator)
	if !ok || total <= end {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

func (s *server) uploadChunk(c *gin.Context) {
	start, end, total, ok := ParseContentRange(c.GetHeader("Content-Range"))
	if !ok {
		Fail(c, 400, nil, model.ConsoleContentRange)
		return
	}
	if err := s.deps.MediaRegistry.Upload(c.Request.Context(), actorOf(c), media.UploadRequest{ID: c.Param("id"), Role: c.Query("role"), Start: start, End: end, Total: total, ContentLength: c.Request.ContentLength, Body: c.Request.Body}); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) isoProgress(c *gin.Context) {
	p, ok := Get(c, func() (media.Progress, error) {
		return s.deps.MediaRegistry.Progress(c.Request.Context(), actorOf(c), c.Param("id"))
	})
	if !ok {
		return
	}
	out := isoProgress{ID: p.ID, State: p.State, Rev: p.Rev, Sources: make([]sourceProgress, 0, len(p.Sources))}
	for _, src := range p.Sources {
		kind := SourceUpload
		switch src.Kind {
		case model.SourceRemote:
			kind = SourceURL
		case model.SourceLocal:
			kind = SourcePath
		}
		ranges := make([]sourceRange, len(src.Ranges))
		for i, r := range src.Ranges {
			ranges[i] = sourceRange{r.Start, r.Length}
		}
		out.Sources = append(out.Sources, sourceProgress{src.Role, kind, src.TotalSize, src.Fetched, src.State, src.Error, src.Rev, ranges})
	}
	Ok(c, out)
}
