package authoring

import (
	"context"

	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

type Editor interface {
	Slots(context.Context, ports.Actor, model.TemplateKey) (SlotResult, error)
	Candidates(context.Context, ports.Actor, CandidateRequest) (CandidatePage, error)
	SourceDiff(context.Context, ports.Actor, model.TemplateKey) (SourceDiffResult, error)
	PreviewParams(context.Context, ports.Actor, PreviewRequest) (PreviewParamsResult, error)
	Preview(context.Context, ports.Actor, PreviewRequest) (PreviewResult, error)
}
type EditorDeps struct {
	Templates port.TemplateReader
	Hooks     port.HookReader
	Hosts     port.HostReader
	ISOs      port.ISOReader
	Renderer  ports.FrozenPreview
}
type CandidateRequest struct {
	Key   model.TemplateKey
	Slot  string
	Index *int
	PageRequest
	Prefix string
}
type Candidate struct {
	Hook     HookView
	Usable   bool
	Problems []model.Problem
}
type CandidatePage struct {
	Items []Candidate
	Next  string
	Total *int64
}
type SlotInfo struct {
	Name      string
	Exec      bool
	Timing    catalog.Timing
	Line, Col int
}
type SlotResult struct {
	Slots    []SlotInfo
	Problems []model.Problem
}
type SourceDiffResult struct {
	Original, Current *TemplateView
	Changed           bool
}
