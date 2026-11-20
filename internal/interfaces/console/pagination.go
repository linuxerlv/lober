package console

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"
)

// parseQueryPagination 按字段是否出现选择分页方式，保留旧游标请求。
func parseQueryPagination(c *gin.Context) (number, limit int, ok bool) {
	number, limit, ok = parsePaginationValues(c.Request.URL.Query())
	if !ok {
		Fail(c, 400, nil, model.ConsoleBadRequest)
	}
	return
}

// parsePaginationValues 只解析数字和分页方式，不写网页响应。
func parsePaginationValues(values url.Values) (number, limit int, ok bool) {
	_, hasPage := values["page"]
	_, hasSize := values["page_size"]
	if !hasPage && !hasSize {
		raw := values.Get("limit")
		if raw == "" {
			return 0, 0, true
		}
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 || limit > 500 {
			return 0, 0, false
		}
		return 0, limit, true
	}
	bad := func() (int, int, bool) { return 0, 0, false }
	if _, exists := values["cursor"]; exists {
		return bad()
	}
	if _, exists := values["limit"]; exists {
		return bad()
	}
	number, limit = 1, 20
	for _, field := range []struct {
		key     string
		present bool
		target  *int
	}{{"page", hasPage, &number}, {"page_size", hasSize, &limit}} {
		if !field.present {
			continue
		}
		raw := values[field.key]
		if len(raw) != 1 || raw[0] == "" {
			return bad()
		}
		for _, ch := range raw[0] {
			if ch < '0' || ch > '9' {
				return bad()
			}
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 {
			return bad()
		}
		*field.target = n
	}
	if !validPageNumbers(number, limit, "") {
		return bad()
	}
	return number, limit, true
}

// validPageNumbers 只检查网页数字能否表示页码，不读取仓储或筛选候选。
func validPageNumbers(page, limit int, cursor string) bool {
	if page < 0 || limit < 0 || limit > 500 || (!utf8.ValidString(cursor) || strings.ContainsRune(cursor, 0)) {
		return false
	}
	if page == 0 {
		return true
	}
	if cursor != "" {
		return false
	}
	if limit == 0 {
		limit = 20
	}
	return page-1 <= int(^uint(0)>>1)/limit
}
