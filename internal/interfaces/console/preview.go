package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/params"
)

type previewRequest struct {
	HostSN   string            `json:"hostSN,omitempty"`
	ISOID    string            `json:"isoID,omitempty"`
	Series   catalog.Series    `json:"series,omitempty"`
	Firmware catalog.Firmware  `json:"firmware,omitempty"`
	Entered  map[string]string `json:"entered,omitempty"`
}
type previewFile struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Sample bool   `json:"sample"`
	Body   string `json:"body"`
	Slot   string `json:"slot,omitempty"`
	Digest string `json:"digest,omitempty"`
}
type previewResponse struct {
	Files    []previewFile `json:"files"`
	Problems []problemJSON `json:"problems"`
	Sample   []string      `json:"sample"`
}

type previewParamsResponse struct {
	Fields   []paramFieldDTO `json:"fields"`
	Problems []problemJSON   `json:"problems"`
	Sample   []string        `json:"sample"`
}

func enteredOf(values map[string]string) map[params.FieldKey]string {
	out := make(map[params.FieldKey]string, len(values))
	for key, value := range values {
		out[params.FieldKey(key)] = value
	}
	return out
}
func previewFiles(values []ports.PreviewFile) []previewFile {
	out := make([]previewFile, len(values))
	for i, v := range values {
		out[i] = previewFile{Kind: v.Kind, Name: v.Name, Sample: v.Sample, Body: v.Body, Slot: v.Slot, Digest: v.Digest}
	}
	return out
}
func (s *server) previewParams(c *gin.Context) {
	var req previewRequest
	if !Bind(c, &req) {
		return
	}
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	result, ok := Get(c, func() (authoring.PreviewParamsResult, error) {
		return s.deps.AuthoringEditor.PreviewParams(c.Request.Context(), actorOf(c), authoring.PreviewRequest{Key: k, HostSN: req.HostSN, ISOID: req.ISOID, Series: req.Series, Firmware: req.Firmware, Entered: enteredOf(req.Entered)})
	})
	if ok {
		Ok(c, previewParamsResponse{Fields: paramFields(result.Fields), Problems: jsonProblems(result.Problems), Sample: result.Sample})
	}
}
func (s *server) previewTemplate(c *gin.Context) {
	var req previewRequest
	if !Bind(c, &req) {
		return
	}
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	result, ok := Get(c, func() (authoring.PreviewResult, error) {
		return s.deps.AuthoringEditor.Preview(c.Request.Context(), actorOf(c), authoring.PreviewRequest{Key: k, HostSN: req.HostSN, ISOID: req.ISOID, Series: req.Series, Firmware: req.Firmware, Entered: enteredOf(req.Entered)})
	})
	if ok {
		Ok(c, previewResponse{Files: previewFiles(result.Files), Problems: jsonProblems(result.Problems), Sample: result.Sample})
	}
}
