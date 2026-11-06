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
	configured bool
	calls      int
}

func (f *inventoryRequesterFake) Configured() bool { return f.configured }
func (f *inventoryRequesterFake) Request(context.Context, ports.Actor, string) (model.Host, error) {
	f.calls++
	return model.Host{}, inventory.ErrHostBusy
}

func TestInventoryRequestExplainsDisabledAndBusy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured bool
		status     int
		calls      int
	}{
		{"disabled", false, http.StatusUnprocessableEntity, 0},
		{"busy", true, http.StatusConflict, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(t)
			inv := &inventoryRequesterFake{configured: tc.configured}
			s := &server{deps: Deps{Inventory: inv}}
			s.requestInventory(c)
			if w.Code != tc.status || inv.calls != tc.calls {
				t.Fatalf("状态码=%d，采集调用次数=%d", w.Code, inv.calls)
			}
		})
	}
}
