package catalog

import (
	"net/url"
	"strings"
	"testing"
)

func TestSignalURLRoundTrip(t *testing.T) {
	address := SignalURL("https://gw.example/base/", "task/one", "tok en", "hook/report")
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	segments := strings.Split(u.EscapedPath(), "/")
	want := []string{"", "base", "v1", "signal", "task/one", "tok en", "hook/report"}
	if len(segments) != len(want) {
		t.Fatal(address)
	}
	for i, segment := range segments {
		got, err := url.PathUnescape(segment)
		if err != nil || got != want[i] {
			t.Fatalf("segment %d: %q %v", i, got, err)
		}
	}
}
