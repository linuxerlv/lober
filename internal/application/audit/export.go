package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"github.com/lober/lober/internal/application/ports"
	domainaudit "github.com/lober/lober/internal/domain/audit"
	"strconv"
)

// ExportCSV 与 List 使用相同身份、范围、分页及仓储错误顺序，每页固定 500 条。
// 只在全部页成功后返回完整缓冲；后页失败或取消时返回 nil，不能下载半份结果。
// 不写入、不新增审计；重复读取及并发新增遵守原游标规则，不承诺跨页数据库快照。
// 列序固定、LF 换行且不带 BOM；只有 CSV 文本加公式保护，原文与 JSON 不改。
func (r *reader) ExportCSV(ctx context.Context, a ports.Actor, f Filter) ([]byte, error) {
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	if err := writer.Write([]string{"id", "at", "actor", "action", "target_kind", "target_id", "detail"}); err != nil {
		return nil, err
	}
	q := Query{Filter: f, Limit: 500}
	for {
		page, err := r.List(ctx, a, q)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, e := range page.Items {
			if err := writer.Write([]string{strconv.FormatInt(e.ID, 10), strconv.FormatInt(e.At, 10), domainaudit.SpreadsheetText(e.Actor), domainaudit.SpreadsheetText(e.Action), domainaudit.SpreadsheetText(e.TargetKind), domainaudit.SpreadsheetText(e.TargetID), domainaudit.SpreadsheetText(e.Detail)}); err != nil {
				return nil, err
			}
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
