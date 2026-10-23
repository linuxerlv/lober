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
	List(context.Context, ports.Actor, ListRequest) (ISOPage, error)
	Get(context.Context, ports.Actor, string) (model.ISO, error)
	Create(context.Context, ports.Actor, CreateRequest) (CreateResult, error)
	RegisterSource(context.Context, ports.Actor, RegisterRequest) error
	Progress(context.Context, ports.Actor, string) (Progress, error)
	Upload(context.Context, ports.Actor, UploadRequest) error
	Cancel(context.Context, ports.Actor, string) error
	Retry(context.Context, ports.Actor, string) error
	Reimport(context.Context, ports.Actor, string) error
	Deprecate(context.Context, ports.Actor, RevisionRequest) error
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
