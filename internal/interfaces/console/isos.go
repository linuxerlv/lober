package console

import (
	"context"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
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
	number, limit, ok := queryPagination(c)
	if !ok {
		return
	}
	q := repository.ISOQuery{Family: catalog.Family(c.Query("family")), Series: catalog.Series(c.Query("series")), Cursor: c.Query("cursor"), Limit: limit, Page: number}
	if q.Family != "" && !q.Family.Valid() || q.Series != "" && !q.Series.Valid() {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	if state := c.Query("state"); state != "" {
		if !model.ISOState(state).Valid() {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		q.States = []model.ISOState{model.ISOState(state)}
	}
	page, ok := Get(c, func() (repository.Page[model.ISO], error) { return s.deps.ISOs.ListISOs(c.Request.Context(), q) })
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
	item, ok := Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), c.Param("id")) })
	if ok {
		Ok(c, toISOResponse(item))
	}
}

// checkISOSource handles record prerequisites; layer4 alone owns URL resolution,
// allowed local roots and byte/media validation. Upload's filename is a client
// label; its persisted source has no address.
func checkISOSource(series catalog.Series, req *isoSourceReq) error {
	if !req.Kind.Valid() || req.Size < 0 {
		return repository.ErrInvalid
	}
	if req.Role != "" {
		extra, ok := catalog.ExtraSourceOf(series)
		if !ok || req.Role != extra.Role {
			return repository.ErrInvalid
		}
	}
	kind := model.SourceRemote
	source := model.Source{ISOID: "preflight", Role: req.Role, State: model.SourcePending, TotalSize: req.Size, ExpectedSHA256: req.SHA256}
	switch req.Kind {
	case SourceURL:
		source.URL = req.Addr
	case SourcePath:
		kind = model.SourceLocal
		source.Path = req.Addr
	case SourceUpload:
		kind = model.SourceUpload
		req.Addr = ""
		if req.Size <= 0 {
			return repository.ErrInvalid
		}
	}
	source.Kind = kind
	return repository.CheckSource(source)
}
func (s *server) checkSourceSpace(c *gin.Context, sources []isoSourceReq) bool {
	var total int64
	for _, src := range sources {
		if src.Kind == SourcePath {
			continue
		} // Layer4 reads the local original in place.
		if src.Size > math.MaxInt64-total {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return false
		}
		total += src.Size
	}
	if total == 0 {
		return true
	}
	free, _, err := s.deps.ISOSource.FreeSpace(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return false
	}
	if free < total {
		return Judge(c, []model.Problem{model.NewProblem("iso_space_insufficient", model.Subject{Kind: model.KindISO, Path: "sources"}, free, total-free)})
	}
	return true
}
func (s *server) createISO(c *gin.Context) {
	var req isoRequest
	if !Bind(c, &req) {
		return
	}
	item := model.ISO{Name: req.Name, Family: req.Family, Series: req.Series}
	if err := repository.CheckNewISO(item); err != nil {
		RespondError(c, err)
		return
	}
	main := 0
	roles := map[string]bool{}
	for i := range req.Sources {
		src := &req.Sources[i]
		if roles[src.Role] {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
		roles[src.Role] = true
		if src.Role == "" {
			main++
		}
		if err := checkISOSource(req.Series, src); err != nil {
			RespondError(c, err)
			return
		}
	}
	if main != 1 {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	}
	if !s.checkSourceSpace(c, req.Sources) {
		return
	}
	created, ok := Write(c, func() (model.ISO, error) { return s.deps.ISOs.CreateISO(c.Request.Context(), item, s.auditFrom(c, "")) })
	if !ok {
		return
	}
	// Registration is independent persisted work. Even if a later source fails,
	// expose the row identity so the operator can inspect, complete or delete it.
	c.Header("Location", "/console/v1/isos/"+created.ID)
	for _, src := range req.Sources {
		if err := s.deps.ISOSource.Register(c.Request.Context(), created, string(src.Kind), src.Addr, src.Role, src.Size, src.SHA256, s.auditFrom(c, "")); err != nil {
			RespondError(c, err)
			return
		}
	}
	Ok(c, toISOResponse(created))
}
func (s *server) registerISOSource(c *gin.Context) {
	var req isoSourceReq
	if !Bind(c, &req) {
		return
	}
	item, ok := Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), c.Param("id")) })
	if !ok {
		return
	}
	if err := checkISOSource(item.Series, &req); err != nil {
		RespondError(c, err)
		return
	}
	if !s.checkSourceSpace(c, []isoSourceReq{req}) {
		return
	}
	err := s.deps.ISOSource.Register(c.Request.Context(), item, string(req.Kind), req.Addr, req.Role, req.Size, req.SHA256, s.auditFrom(c, ""))
	if err != nil {
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
	// No direct-store fallback: assembly must persist the deletion snapshot before
	// removing rows and arrange cleanup retries after the committed deletion.
	if s.deps.ISODeletion == nil {
		RespondError(c, errors.New("ISO deletion adapter missing"))
		return
	}
	if err := s.deps.ISODeletion.Delete(c.Request.Context(), c.Param("id"), rev, s.auditFrom(c, "")); err != nil {
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
	if err := s.deps.ISOs.DeprecateISO(c.Request.Context(), c.Param("id"), rev, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) sourceOperation(c *gin.Context, fn func(context.Context, string, repository.Audit) error) {
	if err := fn(c.Request.Context(), c.Param("id"), s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) cancelISO(c *gin.Context)   { s.sourceOperation(c, s.deps.ISOSource.Cancel) }
func (s *server) retryISO(c *gin.Context)    { s.sourceOperation(c, s.deps.ISOSource.Retry) }
func (s *server) reimportISO(c *gin.Context) { s.sourceOperation(c, s.deps.ISOSource.Reimport) }

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

// exactChunkReader streams without buffering. A short/long body returns an
// error while layer4 is still spooling, before confirmed bytes or ranges change.
type exactChunkReader struct {
	r         io.Reader
	remaining int64
}

func (r *exactChunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, repository.ErrInvalid
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) && r.remaining > 0 {
		return n, repository.ErrInvalid
	}
	return n, err
}
func (s *server) uploadChunk(c *gin.Context) {
	start, end, total, ok := ParseContentRange(c.GetHeader("Content-Range"))
	if !ok {
		Fail(c, 400, nil, model.ConsoleContentRange)
		return
	}
	role, id := c.Query("role"), c.Param("id")
	if err := repository.CheckSourceKey(id, role); err != nil {
		RespondError(c, err)
		return
	}
	source, exists, err := s.deps.Sources.GetSource(c.Request.Context(), id, role)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !exists {
		RespondError(c, repository.ErrNotFound)
		return
	}
	if source.Kind != model.SourceUpload {
		RespondError(c, repository.ErrConflict)
		return
	}
	length := end - start + 1
	if end >= source.TotalSize || total != -1 && total != source.TotalSize || c.Request.ContentLength >= 0 && c.Request.ContentLength != length {
		Fail(c, 400, nil, model.ConsoleContentRange)
		return
	}
	err = s.deps.ISOSource.Upload(c.Request.Context(), id, role, start, &exactChunkReader{r: c.Request.Body, remaining: length}, s.auditFrom(c, ""))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
func (s *server) isoProgress(c *gin.Context) {
	item, ok := Get(c, func() (model.ISO, error) { return s.deps.ISOs.GetISO(c.Request.Context(), c.Param("id")) })
	if !ok {
		return
	}
	sources, ok := Get(c, func() ([]model.Source, error) { return s.deps.Sources.SourcesOf(c.Request.Context(), item.ID) })
	if !ok {
		return
	}
	out := isoProgress{ID: item.ID, State: item.State, Rev: item.Rev, Sources: make([]sourceProgress, 0, len(sources))}
	for _, source := range sources {
		progress, ok := Get(c, func() (sourceProgress, error) { return s.sourceProgress(c.Request.Context(), source) })
		if !ok {
			return
		}
		out.Sources = append(out.Sources, progress)
	}
	Ok(c, out)
}
func (s *server) sourceProgress(ctx context.Context, source model.Source) (sourceProgress, error) {
	for attempt := 0; attempt < 3; attempt++ {
		ranges, err := s.deps.Sources.RangesOf(ctx, source.ISOID, source.Role)
		if err != nil {
			return sourceProgress{}, err
		}
		current, exists, err := s.deps.Sources.GetSource(ctx, source.ISOID, source.Role)
		if err != nil {
			return sourceProgress{}, err
		}
		if !exists {
			return sourceProgress{}, repository.ErrNotFound
		}
		if current.Rev != source.Rev {
			source = current
			continue
		}
		spans := make([]sourceRange, len(ranges))
		for i, r := range ranges {
			spans[i] = sourceRange{r.Start, r.Length}
		}
		kind := SourceUpload
		switch source.Kind {
		case model.SourceRemote:
			kind = SourceURL
		case model.SourceLocal:
			kind = SourcePath
		}
		return sourceProgress{source.Role, kind, source.TotalSize, source.Fetched, source.State, source.Error, source.Rev, spans}, nil
	}
	return sourceProgress{}, repository.ErrConflict
}
