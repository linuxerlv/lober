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
	// List 的应用约定唯一连接 docs/contracts/identity-audit.md「十三条入口」GET /audit行、
	// 「返回字段、菜单与权限」「认证、并发与取消」及「审计读取共同边界」。
	// 先检查audit:read，再查时间范围、分页；保留ID降序和原游标，空Items为数组。
	List(context.Context, ports.Actor, Query) (Page, error)
	// ExportCSV 遵守 docs/contracts/identity-audit.md「十三条入口」GET /audit/export行、
	// 「审计文件格式」及「审计读取共同边界」。每页固定500条，全部成功才返回完整字节；
	// 空结果仍有表头，后页错误或取消返回nil，不交付部分文件，不承诺跨页快照。
	ExportCSV(context.Context, ports.Actor, Filter) ([]byte, error)
}
