// Package media 编排镜像网页操作，保留原来的保存边界和失败顺序。
package media

import (
	"context"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"io"
)

// Registry 所有操作先检查可信身份。读口不记审计；写口只有管理员可调用。
// 请求取消贯穿下游能力，但不撤销已经提交的镜像、来源或审计。
type Registry interface {
	// List 遵守 docs/contracts/media.md 第2节GET /行、第4节及第1节共同约定；
	// 先权限再分页，再查族、系、状态；游标默认100，页码默认20，空Items为数组。
	List(context.Context, ports.Actor, ListRequest) (ISOPage, error)
	// Get 遵守 docs/contracts/media.md 第2节GET /:id行及第1节共同约定；只读，编号先于存在性。
	Get(context.Context, ports.Actor, string) (model.ISO, error)
	// Create 遵守 docs/contracts/media.md 第2节POST /行、第3节及第1节共同约定；
	// 逐来源预检后才创建，来源分别提交；后续失败保留Created与ISO，不声称整批回滚。
	Create(context.Context, ports.Actor, CreateRequest) (CreateResult, error)
	// RegisterSource 遵守 docs/contracts/media.md 第2节sources行、第3节及第1节共同约定；
	// 先镜像再预检和余量，失败镜像补源通过下游同事务重新排队，成功才唤醒。
	RegisterSource(context.Context, ports.Actor, RegisterRequest) error
	// Progress 遵守 docs/contracts/media.md 第2节progress行及第4节；
	// 先镜像再来源，确认区间后重查来源Rev最多3轮；不承诺整镜像一致快照。
	Progress(context.Context, ports.Actor, string) (Progress, error)
	// Upload 遵守 docs/contracts/media.md 第2节content行及第4节；
	// 检查范围与实际字节，重复一致字节可成功；字节与区间不共用事务，网页上传不审计。
	Upload(context.Context, ports.Actor, UploadRequest) error
	// Cancel 遵守 docs/contracts/media.md 第2节cancel行及第5节；
	// 先权限，来源逐条提交取消及审计，再取消本实例读取；不提供跨来源事务。
	Cancel(context.Context, ports.Actor, string) error
	// Retry 遵守 docs/contracts/media.md 第2节retry行及第5节；
	// 只重置failed/cancelled；失败恢复暂存文件，成功后唤醒，无可重试来源时成功且无新审计。
	Retry(context.Context, ports.Actor, string) error
	// Reimport 遵守 docs/contracts/media.md 第2节reimport行及第6节；
	// 请求只保存重新排队与审计后唤醒，不同步提取，旧版本或状态竞争返回冲突。
	Reimport(context.Context, ports.Actor, string) error
	// Deprecate 遵守 docs/contracts/media.md 第2节deprecate行及第7节；
	// 正Rev先于存储审计/编号检查，锁内复查版本和ready状态，与审计一起提交。
	Deprecate(context.Context, ports.Actor, RevisionRequest) error
	// Delete 遵守 docs/contracts/media.md 第2节DELETE行及第7节；
	// 快照落盘先于数据库删除；引用与版本在事务内复查，已提交后清理失败交后台重试。
	Delete(context.Context, ports.Actor, RevisionRequest) error
}
type Deps struct {
	ISOs     port.ISOs
	Sources  port.Sources
	Control  port.SourceControl
	Deletion port.Deletion
	Clock    ports.Clock
}
type ListRequest struct {
	Page, Limit int
	Cursor      string
	Family      catalog.Family
	Series      catalog.Series
	State       model.ISOState
}
type ISOPage struct {
	Items []model.ISO
	Next  string
	Total *int64
}
type SourceSpec struct {
	Kind, Addr, Role, SHA256 string
	Size                     int64
}
type CreateRequest struct {
	Name    string
	Family  catalog.Family
	Series  catalog.Series
	Sources []SourceSpec
}

// CreateResult 创建提交后即返回编号，后续来源失败也保留 Created 和 ISO。
type CreateResult struct {
	ISO     model.ISO
	Created bool
}
type RegisterRequest struct {
	ID     string
	Source SourceSpec
}
type RevisionRequest struct {
	ID  string
	Rev int64
}
type UploadRequest struct {
	ID, Role                         string
	Start, End, Total, ContentLength int64
	Body                             io.Reader
}
type Progress struct {
	ID      string
	State   model.ISOState
	Rev     int64
	Sources []SourceProgress
}
type SourceProgress struct {
	Role               string
	Kind               model.SourceKind
	TotalSize, Fetched int64
	State              model.SourceState
	Error              string
	Rev                int64
	Ranges             []model.Range
}

// RangeError 只表示读取正文前的范围或声明长度错误，不含网页展示信息。
type RangeError struct{}

func (*RangeError) Error() string { return "上传范围或声明长度无效" }
