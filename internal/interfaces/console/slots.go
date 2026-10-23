package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/domain/catalog"
)

type slotInfo struct {
	Name   string         `json:"name"`
	Exec   bool           `json:"exec"`
	Timing catalog.Timing `json:"timing"`
	Line   int            `json:"line"`
	Col    int            `json:"col"`
}
type slotList struct {
	Slots    []slotInfo    `json:"slots"`
	Problems []problemJSON `json:"problems"`
}

func (s *server) slotsOf(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	result, ok := Get(c, func() (authoring.SlotResult, error) {
		return s.deps.AuthoringEditor.Slots(c.Request.Context(), actorOf(c), k)
	})
	if !ok {
		return
	}
	slots := make([]slotInfo, 0, len(result.Slots))
	for _, v := range result.Slots {
		slots = append(slots, slotInfo{v.Name, v.Exec, v.Timing, v.Line, v.Col})
	}
	Ok(c, slotList{slots, jsonProblems(result.Problems)})
}
