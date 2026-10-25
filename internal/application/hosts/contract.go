package hosts

import (
	"context"
	"github.com/lober/lober/internal/application/hosts/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

type Registry interface {
	// Import 遵守 docs/contracts/hosts.md「六条入口」「CSV 预演与提交」。
	// 先检查host:write；文件解析后批量读取，读取错误先于行拒绝；预演只读，提交按输入顺序整批原子保存及审计。
	Import(context.Context, ports.Actor, ImportRequest) (ImportResult, error)
	// List 遵守 docs/contracts/hosts.md「六条入口」「列表和分页」及「应用共同边界」。
	// 先检查host:read；原文筛选，不套向导Q规范化；空Items为数组，Total只用于页码。
	List(context.Context, ports.Actor, ListRequest) (HostPage, error)
	// Get 遵守 docs/contracts/hosts.md「六条入口」「单条写入与错误先后」及「应用共同边界」。
	// 先检查host:read，再规范化编号、查存在性；只读单台主机。
	Get(context.Context, ports.Actor, string) (model.Host, error)
	// Create 遵守 docs/contracts/hosts.md「六条入口」「单条写入与错误先后」及「应用共同边界」。
	// 先检查host:write，再规范化SN；创建及host.create审计同事务，重复编号冲突。
	Create(context.Context, ports.Actor, model.Host) (model.Host, error)
	// Update 遵守 docs/contracts/hosts.md「六条入口」「单条写入与错误先后」及「应用共同边界」。
	// 先检查host:write；Rev、路径SN、正文SN依次检查；整条替换，旧版本冲突，不自动刷新Rev。
	Update(context.Context, ports.Actor, UpdateRequest) (model.Host, error)
	// Delete 遵守 docs/contracts/hosts.md「六条入口」「单条写入与错误先后」及「应用共同边界」。
	// 先检查host:write和Rev，再规范化SN；存在性、版本与引用在仓储确认，与host.delete审计同事务。
	Delete(context.Context, ports.Actor, DeleteRequest) error
}
type Deps struct {
	Hosts port.Hosts
	Clock ports.Clock
}
type ListRequest struct {
	Page, Limit                        int
	Prefix, AttrKey, AttrValue, Cursor string
}
type HostPage struct {
	Items []model.Host
	Next  string
	Total *int64
}
type UpdateRequest struct {
	PathSN string
	Host   model.Host
}
type DeleteRequest struct {
	SN  string
	Rev int64
}

type ImportRequest struct {
	CSV    []byte
	DryRun bool
}
type ImportRow struct {
	Row        int
	SN, Action string
	Problems   []model.Problem
}
type ImportPlan struct {
	Rows  []ImportRow
	Total int
}
type ImportResult struct {
	Plan  ImportPlan
	Hosts []model.Host
}
type CSVFileError struct{ Message string }

func (e *CSVFileError) Error() string { return e.Message }

type ImportRejectedError struct {
	Plan    ImportPlan
	Message string
}

func (e *ImportRejectedError) Error() string { return e.Message }
