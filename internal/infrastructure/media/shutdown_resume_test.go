package iso

import (
	"context"
	"errors"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/domain/model"
	"net/http"
	"testing"
	"time"
)

func TestWorkerShutdownPreservesRemoteCheckpointForRestart(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", "6")
			w.Write([]byte("abc"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Range") != "bytes=3-" {
			t.Error("checkpoint was not resumed", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("def"))
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 6, sourceDigest([]byte("abcdef")), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.FetchOne(ctx, item.ID, "") }()
	deadline := time.Now().Add(10 * time.Second)
	for mustSource(t, s, item.ID, "").Fetched != 3 {
		if time.Now().After(deadline) {
			t.Fatal("download did not checkpoint")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	if src.State != model.SourceFetching || src.Fetched != 3 {
		t.Fatal("shutdown lost resumable state", src)
	}
	restarted := newAcquisitionFixture(s.store, s.dirs)
	restarted.transfer.resolver, restarted.transfer.dialContext = s.transfer.resolver, s.transfer.dialContext
	work, err := restarted.Recover(t.Context())
	if err != nil || len(work) != 1 || work[0].Kind != appmedia.ResumeFetch {
		t.Fatal("restart did not resume", work, err)
	}
	if err := restarted.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	if src := mustSource(t, restarted, item.ID, ""); src.State != model.SourceFetched || src.Fetched != 6 {
		t.Fatal(src)
	}
}
