package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/application/inventory"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
)

type inventoryRequesterFake struct {
	err   error
	calls int
}

func (f *inventoryRequesterFake) Request(context.Context, ports.Actor, string) (model.Host, error) {
	f.calls++
	return model.Host{}, f.err
}

func TestInventoryRequestExplainsDisabledAndBusy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		calls  int
	}{
		{"disabled", nil, http.StatusUnprocessableEntity, 0},
		{"unconfigured", inventory.ErrMenuUnconfigured, http.StatusUnprocessableEntity, 1},
		{"busy", inventory.ErrHostBusy, http.StatusConflict, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(t)
			inv := &inventoryRequesterFake{err: tc.err}
			s := &server{}
			if tc.err != nil {
				s.deps.Inventory = inv
			}
			s.requestInventory(c)
			if w.Code != tc.status || inv.calls != tc.calls {
				t.Fatalf("状态码=%d，采集调用次数=%d", w.Code, inv.calls)
			}
		})
	}
}
