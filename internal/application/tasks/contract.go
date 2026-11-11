// Package tasks 编排任务向导，网页字段和数据库记录由各自边界转换。
package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/selection"
)

var (
	ErrForbidden              = ports.ErrForbidden
	ErrUnauthenticated        = ports.ErrUnauthenticated
	ErrPasswordChangeRequired = ports.ErrPasswordChangeRequired
)

// ProblemsError 保留按原顺序收集的普通问题，展示方式由入口决定。
type ProblemsError struct{ Problems []model.Problem }

func (e *ProblemsError) Error() string { return e.Problems[0].Message }

type PageRequest struct {
	Page   int
	Cursor string
	Limit  int
}
type HostRequest struct {
	PageRequest
	Q, AttrKey, AttrValue string
}
type ISORequest struct {
	PageRequest
	HostSNs []string
}
type TemplateRequest struct {
	PageRequest
	ISOID string
}
type Selection struct {
	ISOID, TemplateID string
	TemplateVersion   int
}
type ParamsRequest struct {
	Selection
	HostSNs []string
	Entered map[string]map[string]string
}
type SubmitHost struct {
	SN      string
	Entered map[string]string
}
type SubmitRequest struct {
	Selection
	Hosts []SubmitHost
}

type HostChoice struct {
	Host     model.Host
	Problems []model.Problem
}
type HostPage struct {
	Items []HostChoice
	Next  string
	Total *int64
}
type ISOChoice struct {
	ISO      model.ISO
	Problems []model.Problem
}
type ISOPage struct {
	Items []ISOChoice
	Next  string
	Total *int64
}
type TemplateChoice struct {
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
	Problems []model.Problem
}
type TemplatePage struct {
	Items []TemplateChoice
	Next  string
	Total *int64
}
type HostParams struct {
	SN       string
	Fields   []params.Field
	Problems []model.Problem
}
type ParamsResult struct {
	Hosts  []HostParams
	Common []model.Problem
}
type TaskChoice struct {
	Task                model.Task
	CanCancel, CanRetry []model.Problem
}

// Wizard 先检查可信操作者，再检查输入；空批拒绝，编号规范化后仍保留输入顺序。
// 参数预览先检查选择是否当前，再检查输入行。一般问题随预览返回，提交则整批拒绝。
// 创建人与时间取注入能力；任务和审计同一事务，只有创建成功后才逐任务通知。
type Wizard interface {
	// Hosts 遵守 docs/contracts/backend-modules.md「五个向导入口」GET /hosts 行及「向导应用共同边界」。
	// 只读候选；Q可规范化时先规范化，保留不可选主机和原顺序，逐台补开放任务问题。
	Hosts(context.Context, ports.Actor, HostRequest) (HostPage, error)
	// ISOs 遵守 docs/contracts/backend-modules.md「五个向导入口」POST /isos 行及「向导应用共同边界」。
	// 先规范化并查主机、拒绝重复或缺失，再检查分页；平台问题放在各候选内。
	ISOs(context.Context, ports.Actor, ISORequest) (ISOPage, error)
	// Templates 遵守 docs/contracts/backend-modules.md「五个向导入口」POST /templates 行及「向导应用共同边界」。
	// 先读镜像并拒绝不可用，再检查分页；游标先分页再过滤，页码先过滤再计数和分页。
	Templates(context.Context, ports.Actor, TemplateRequest) (TemplatePage, error)
	// Params 遵守 docs/contracts/backend-modules.md「五个向导入口」POST /params 行及「问题归属与批量失败」。
	// 只读预览；先确认当前选择，再检查输入行，一般参数问题随结果返回，保留输入主机顺序。
	Params(context.Context, ports.Actor, ParamsRequest) (ParamsResult, error)
	// Submit 遵守 docs/contracts/backend-modules.md「五个向导入口」POST /tasks 行及「问题归属与批量失败」。
	// 持进程选择读锁；CreateCurrentTasks在数据库保护内复查当前模板，任务与审计整批提交，成功才通知。
	Submit(context.Context, ports.Actor, SubmitRequest) ([]TaskChoice, error)
}
type WizardDeps struct {
	Hosts          port.Hosts
	ISOs           port.ISOs
	Templates      port.Templates
	Hooks          port.Hooks
	Tasks          port.Tasks
	Manifest       selection.Manifest
	Clock          ports.Clock
	Notifier       ports.Notifier
	SelectionGuard ports.SelectionGuard
	Diagnostics    port.TaskDiagnostics
}
