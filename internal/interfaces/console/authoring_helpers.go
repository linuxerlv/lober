package console

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
)

type hookCandidateQuery struct {
	Slot     string `form:"slot" binding:"required"`
	Index    int    `form:"index"`
	Prefix   string `form:"prefix"`
	Cursor   string `form:"cursor"`
	Limit    int    `form:"limit"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
type hookCandidateResponse struct {
	Hook     hookResponse  `json:"hook"`
	Usable   bool          `json:"usable"`
	Problems []problemJSON `json:"problems"`
}
type hookCandidatePage struct {
	Items []hookCandidateResponse `json:"items"`
	Next  string                  `json:"next"`
	Total *int64                  `json:"total,omitempty"`
}
type sourceDiffResponse struct {
	Original *templateResponse `json:"original"`
	Current  *templateResponse `json:"current"`
	Changed  bool              `json:"changed"`
}

func (s *server) hookCandidates(c *gin.Context) {
	key, ok := templateKeyOf(c)
	if !ok {
		return
	}
	req := authoring.CandidateRequest{Key: key, Slot: c.Query("slot"), Prefix: c.Query("prefix")}
	if raw := c.Query("index"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			n = 0
		}
		req.Index = &n
	}
	page, limit, valid := parsePaginationValues(c.Request.URL.Query())
	if !valid {
		page = -1
	}
	req.PageRequest = authoring.PageRequest{Page: page, Limit: limit, Cursor: c.Query("cursor")}
	result, ok := Get(c, func() (authoring.CandidatePage, error) {
		return s.deps.AuthoringEditor.Candidates(c.Request.Context(), actorOf(c), req)
	})
	if !ok {
		return
	}
	items := make([]hookCandidateResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, hookCandidateResponse{toHookResponse(item.Hook), item.Usable, jsonProblems(item.Problems)})
	}
	Ok(c, hookCandidatePage{Items: items, Next: result.Next, Total: result.Total})
}
func (s *server) sourceDiff(c *gin.Context) {
	key, ok := templateKeyOf(c)
	if !ok {
		return
	}
	result, ok := Get(c, func() (authoring.SourceDiffResult, error) {
		return s.deps.AuthoringEditor.SourceDiff(c.Request.Context(), actorOf(c), key)
	})
	if !ok {
		return
	}
	response := sourceDiffResponse{Changed: result.Changed}
	if result.Original != nil {
		v := templateViewResponse(*result.Original)
		response.Original = &v
	}
	if result.Current != nil {
		v := templateViewResponse(*result.Current)
		response.Current = &v
	}
	Ok(c, response)
}
