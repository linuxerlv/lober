package audit

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
)

var ErrBadRequest = errors.New("审计时间范围无效")

type Filter struct {
	TargetKind, TargetID, Actor, Action string
	From, To                            int64
}
type Query struct {
	Filter
	Page, Limit int
	Cursor      string
}
type Entry struct {
	ID, At                                      int64
	Actor, Action, Detail, TargetKind, TargetID string
}
type Page struct {
	Items []Entry
	Next  string
	Total *int64
}
type Reader interface {
	List(context.Context, ports.Actor, Query) (Page, error)
	ExportCSV(context.Context, ports.Actor, Filter) ([]byte, error)
}
