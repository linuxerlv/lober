// Package tasks 编排任务向导，网页字段和数据库记录由各自边界转换。
package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/selection"
)

var (
	ErrForbidden              = errors.New("没有创建任务的权限")
	ErrUnauthenticated        = errors.New("操作者身份无效")
	ErrPasswordChangeRequired = errors.New("需要先修改密码")
)

// ProblemsError 保留按原顺序收集的普通问题，展示方式由入口决定。
type ProblemsError struct{ Problems []model.Problem }

func (e *ProblemsError) Error() string { return e.Problems[0].Message }

type PageRequest struct {
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
}
type ISOChoice struct {
	ISO      model.ISO
	Problems []model.Problem
}
type ISOPage struct {
	Items []ISOChoice
	Next  string
}
type TemplateChoice struct {
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
	Problems []model.Problem
}
type TemplatePage struct {
	Items []TemplateChoice
	Next  string
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
	Hosts(context.Context, ports.Actor, HostRequest) (HostPage, error)
	ISOs(context.Context, ports.Actor, ISORequest) (ISOPage, error)
	Templates(context.Context, ports.Actor, TemplateRequest) (TemplatePage, error)
	Params(context.Context, ports.Actor, ParamsRequest) (ParamsResult, error)
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
	SelectionGuard port.SelectionGuard
	Diagnostics    port.TaskDiagnostics
}
