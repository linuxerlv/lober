package hosts

import (
	"context"
	"github.com/lober/lober/internal/application/hosts/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

type Registry interface {
	List(context.Context, ports.Actor, ListRequest) (HostPage, error)
	Get(context.Context, ports.Actor, string) (model.Host, error)
	Create(context.Context, ports.Actor, model.Host) (model.Host, error)
	Update(context.Context, ports.Actor, UpdateRequest) (model.Host, error)
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
