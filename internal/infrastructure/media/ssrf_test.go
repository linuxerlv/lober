package iso

import (
	"net/netip"
	"net/url"
	"testing"
)

func TestCheckURL(t *testing.T) {
	for _, tc := range []struct {
		raw, ip string
		allowed bool
	}{
		{"https://mirror.example/a", "203.0.113.10", true},
		{"http://mirror.example/a", "10.0.0.2", true}, // Air-gapped mirrors intentionally use private networks.
		{"http://mirror.example/a", "192.168.0.2", true},
		{"http://mirror.example/a", "172.16.0.2", true},
		{"ftp://mirror.example/a", "203.0.113.10", false},
		{"file:///tmp/a", "203.0.113.10", false},
		{"//mirror.example/a", "203.0.113.10", false},
		{"http://127.0.0.1/a", "127.0.0.1", false},
		{"http://127.1.2.3/a", "127.1.2.3", false},
		{"http://[::1]/a", "::1", false},
		{"http://0.0.0.0/a", "0.0.0.0", false},
		{"http://mirror.example/a", "169.254.1.1", false},
		{"http://mirror.example/a", "fe80::1", false},
		{"http://mirror.example/a", "::ffff:127.0.0.1", false},
	} {
		t.Run(tc.raw+tc.ip, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			err = CheckURL(u, []netip.Addr{netip.MustParseAddr(tc.ip)})
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v: %v", tc.allowed, err)
			}
		})
	}
	u, _ := url.Parse("http://mirror.example")
	if CheckURL(u, nil) == nil {
		t.Fatal("unresolved address accepted")
	}
	if CheckURL(u, []netip.Addr{netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("127.0.0.1")}) == nil {
		t.Fatal("unsafe second DNS address accepted")
	}
}
