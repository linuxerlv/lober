package console

import (
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/scan"
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
	tpl, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if !ok {
		return
	}
	result, problems := scan.Scan(tpl.Body)
	for i := range problems {
		problems[i].Subject.Kind = model.KindTemplate
		problems[i].Subject.ID = k.String()
	}
	// Duplicate-slot rules remain in core. Return the editor's first occurrence
	// plus the exact locations of later duplicates, without inventing a scanner.
	for _, problem := range activation.Template(tpl, nil) {
		if problem.Code == "slot_duplicate" {
			problems = append(problems, problem)
		}
	}
	slots := make([]slotInfo, 0, len(result.Slots))
	seen := map[string]bool{}
	for _, seg := range result.Segments {
		if seg.Kind != scan.KindSlot || seen[seg.Slot.Name] {
			continue
		}
		seen[seg.Slot.Name] = true
		slots = append(slots, slotInfo{seg.Slot.Name, seg.Slot.Exec, seg.Slot.Timing, seg.Line, seg.Col})
	}
	Ok(c, slotList{slots, jsonProblems(problems)})
}
