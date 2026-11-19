package console

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/discovery"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
)

type discoveryQuery struct {
	RegisteredSN string `form:"registered_sn"`
	Limit        int    `form:"limit"`
	Cursor       string `form:"cursor"`
}

type discoveryRegistration struct {
	SN string `json:"sn" binding:"required"`
}

func (s *server) discoveryRoutes() []route {
	return []route{
		{method: "GET", path: "/console/v1/discoveries", perm: PermHostRead, tag: "hosts", summary: "List PXE discoveries", query: discoveryQuery{}, resp: discovery.Page{}, handle: s.listDiscoveries},
		{method: "GET", path: "/console/v1/discoveries/:mac", perm: PermHostRead, tag: "hosts", summary: "Read discovered hardware", resp: data.Record{}, handle: s.getDiscovery},
		{method: "POST", path: "/console/v1/discoveries/:mac/register", perm: PermHostWrite, audit: "host.create", tag: "hosts", summary: "Register a discovered host", req: discoveryRegistration{}, resp: hostResponse{}, handle: s.registerDiscovery},
	}
}

func (s *server) listDiscoveries(c *gin.Context) {
	var limit int
	if raw, ok := c.GetQuery("limit"); ok {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 {
			Fail(c, 400, nil, model.ConsoleBadRequest)
			return
		}
	}
	svc := s.deps.Discovery
	if svc == nil {
		Ok(c, map[string]any{"items": []any{}})
		return
	}
	page, err := svc.List(c.Request.Context(), actorOf(c), c.Query("cursor"), c.Query("registered_sn"), limit)
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, page)
}

func (s *server) getDiscovery(c *gin.Context) {
	svc := s.deps.Discovery
	if svc == nil {
		Fail(c, 404, nil, model.ConsoleNotFound)
		return
	}
	record, err := svc.Get(c.Request.Context(), actorOf(c), c.Param("mac"))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, record)
}

func (s *server) registerDiscovery(c *gin.Context) {
	svc := s.deps.Discovery
	if svc == nil {
		Fail(c, 404, nil, model.ConsoleNotFound)
		return
	}
	var req discoveryRegistration
	if !Bind(c, &req) {
		return
	}
	host, err := svc.Register(c.Request.Context(), actorOf(c), c.Param("mac"), req.SN)
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, toHostResponse(host))
}
