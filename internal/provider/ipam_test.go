package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

func TestIPAM_AllocateReturnsFormValues(t *testing.T) {
	form := map[string]string{
		staticIPAMFormKeyIP:      "10.0.1.5",
		staticIPAMFormKeyGateway: "10.0.1.1",
		staticIPAMFormKeyMask:    "255.255.255.0",
	}
	ipam := newStaticIPAM(form)

	result, err := ipam.Allocate(context.Background(), contract.AllocRequest{Hostname: "host1"})
	if err != nil {
		t.Fatalf("static IPAM 不应报错: %v", err)
	}
	if result.IP != "10.0.1.5" || result.Gateway != "10.0.1.1" || result.Mask != "255.255.255.0" {
		t.Fatalf("got %+v，want 表单里填的三个值原样传出", result)
	}
}

func TestIPAM_ReleaseAndPingDoNotError(t *testing.T) {
	ipam := newStaticIPAM(nil)
	if err := ipam.Release(context.Background(), "10.0.1.5"); err != nil {
		t.Fatalf("Release 空实现不应报错: %v", err)
	}
	if err := ipam.Ping(context.Background()); err != nil {
		t.Fatalf("Ping 不应报错: %v", err)
	}
}
