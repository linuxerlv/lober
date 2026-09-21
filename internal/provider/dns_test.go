package provider

import (
	"context"
	"testing"
)

func TestDNS_RegisterDeregisterPingDoNotError(t *testing.T) {
	dns := newNoopDNS()
	if err := dns.Register(context.Background(), "host1.example.com", "10.0.1.5"); err != nil {
		t.Fatalf("noop DNS Register 不应返回错误: %v", err)
	}
	if err := dns.Deregister(context.Background(), "host1.example.com"); err != nil {
		t.Fatalf("noop DNS Deregister 不应返回错误: %v", err)
	}
	if err := dns.Ping(context.Background()); err != nil {
		t.Fatalf("noop DNS Ping 不应返回错误: %v", err)
	}
}
