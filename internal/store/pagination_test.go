package store_test

import (
	"errors"
	"github.com/lober/lober/internal/store"
	"testing"
)

func TestNumberedPaginationValidation(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name, cursor      string
		limit, page, want int
		invalid           bool
	}{
		{name: "legacy default", want: 100},
		{name: "numbered default", page: 1, want: 20},
		{name: "numbered max", page: 2, limit: 500, want: 500},
		{name: "negative page", page: -1, invalid: true},
		{name: "negative size", page: 1, limit: -1, invalid: true},
		{name: "size too large", page: 1, limit: 501, invalid: true},
		{name: "mixed cursor", page: 1, cursor: "cursor", invalid: true},
		{name: "offset overflow", page: maxInt, limit: 500, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.CheckPageQuery(tc.cursor, tc.limit, tc.page)
			if tc.invalid {
				if !errors.Is(err, store.ErrInvalid) {
					t.Fatalf("want ErrInvalid got %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("want limit %d got %d, %v", tc.want, got, err)
			}
		})
	}
}
