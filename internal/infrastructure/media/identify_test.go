package iso

import (
	"context"
	"errors"
	"testing"
)

type identifyCancellingReader struct{ cancel context.CancelFunc }

func (r identifyCancellingReader) Read(b []byte) (int, error) { r.cancel(); return len(b), nil }

func TestIdentifyCancellationDuringWIMSeek(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := identifyForwardReader{ctx: ctx, r: identifyCancellingReader{cancel: cancel}}
	_, err := r.ReadAt(make([]byte, 208), 4096)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
