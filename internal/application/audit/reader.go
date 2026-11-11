package audit

import (
	"context"
	"github.com/lober/lober/internal/application/audit/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	domainaudit "github.com/lober/lober/internal/domain/audit"
	"github.com/lober/lober/internal/domain/repository"
)

type reader struct{ log port.AuditLog }

func NewReader(log port.AuditLog) Reader {
	if log == nil {
		panic("审计查询缺少仓储")
	}
	return &reader{log: log}
}

// List 只允许无需改密的已认证管理员；身份错误先于范围、分页和仓储错误。
// 时间含两端，文本精确匹配；不可存文本仍交原查询返回空页，不改写筛选。
// 返回普通结果与原游标，ID 倒序；重复查询不写入、不新增审计。
// 游标后续页只读更小的 ID，因此并发新增不会倒灌；取消原样传给仓储。
func (r *reader) List(ctx context.Context, a ports.Actor, q Query) (Page, error) {
	if err := a.Authorize(access.PermAuditRead); err != nil {
		return Page{}, err
	}
	if !domainaudit.ValidRange(q.From, q.To) {
		return Page{}, ErrBadRequest
	}
	if _, err := repository.CheckPageQuery(q.Cursor, q.Limit, q.Page); err != nil {
		return Page{}, err
	}
	page, err := r.log.ListAudit(ctx, repository.AuditQuery{TargetKind: q.TargetKind, TargetID: q.TargetID, Actor: q.Actor, Action: q.Action, From: q.From, To: q.To, Page: q.Page, Limit: q.Limit, Cursor: q.Cursor})
	if err != nil {
		return Page{}, err
	}
	items := make([]Entry, len(page.Items))
	for i, e := range page.Items {
		items[i] = Entry{ID: e.ID, At: e.At, Actor: e.Actor, Action: e.Action, Detail: e.Detail, TargetKind: e.TargetKind, TargetID: e.TargetID}
	}
	return Page{Items: items, Next: page.Next, Total: page.Total}, nil
}
