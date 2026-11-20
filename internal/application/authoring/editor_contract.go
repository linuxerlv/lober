package authoring

import (
	"context"

	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// Editor 的五项操作只读，不写业务、审计或通知，故无原子写、提交复查或版本重试。
// 每次查询可看到新事实，不保证多次读取快照一致；上下文传给仓储与渲染，纯内存计算不新增中断。
type Editor interface {
	// Slots 遵守 docs/contracts/authoring-preview.md 第28项及「问题与先后顺序」「预览与写入边界」。
	// 先检查template:read和版本键，再读模板；插槽按正文顺序，同名只列首次，问题保留全部定位。
	Slots(context.Context, ports.Actor, model.TemplateKey) (SlotResult, error)
	// Candidates 遵守 docs/contracts/authoring-preview.md 第29项及「问题与先后顺序」「页型与来源不能互相替代」。
	// 先检查template:read、模板和插槽，再检查index与分页；游标按身份展开版本，页码直接分页版本。
	Candidates(context.Context, ports.Actor, CandidateRequest) (CandidatePage, error)
	// SourceDiff 遵守 docs/contracts/authoring-preview.md 第30项及「页型与来源不能互相替代」「预览与写入边界」。
	// 先检查template:read及版本键；无来源时双nil且Changed=false，当前来源可为最新草稿，不更新派生副本。
	SourceDiff(context.Context, ports.Actor, model.TemplateKey) (SourceDiffResult, error)
	// PreviewParams 遵守 docs/contracts/authoring-preview.md 第31项及「问题与先后顺序」「预览与写入边界」。
	// 先检查template:read；显式对象缺失不回退示例，求值问题按原顺序随结果返回，不创建任务。
	PreviewParams(context.Context, ports.Actor, PreviewRequest) (PreviewParamsResult, error)
	// Preview 遵守 docs/contracts/authoring-preview.md 第32项及「问题与先后顺序」「预览与写入边界」。
	// 先检查template:read；建立快照后渲染，任何失败不返回部分文件，所有示例标签决定文件Sample。
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
