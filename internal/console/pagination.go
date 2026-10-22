package console

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// queryPagination selects numbered mode by parameter presence, including empty
// values. Requests without numbered parameters retain cursor/limit behavior.
func queryPagination(c *gin.Context) (number, limit int, ok bool) {
	values := c.Request.URL.Query()
	_, hasPage := values["page"]
	_, hasSize := values["page_size"]
	if !hasPage && !hasSize {
		limit, ok = queryLimit(c)
		return 0, limit, ok
	}
	bad := func() (int, int, bool) { Fail(c, 400, nil, model.ConsoleBadRequest); return 0, 0, false }
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
	if _, err := store.CheckPageQuery("", limit, number); err != nil {
		return bad()
	}
	return number, limit, true
}
