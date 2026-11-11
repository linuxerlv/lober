package iso

import (
	"context"
	"errors"
	appmedia "github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type checkpointStore struct {
	repository.Store
	committed chan struct{}
	once      sync.Once
}

func (s *checkpointStore) SaveSource(ctx context.Context, src model.Source, a repository.Audit) (model.Source, error) {
	saved, e := s.Store.SaveSource(ctx, src, a)
	if e == nil && saved.Fetched == 3 && saved.State == model.SourceFetching {
		s.once.Do(func() { close(s.committed) })
	}
	return saved, e
}
func Test镜像关闭后从真实断点恢复(t *testing.T) {
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
			t.Error("恢复位置错误", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("def"))
	})
	if e := s.Register(t.Context(), item, "url", addr, "", 6, sourceDigest([]byte("abcdef")), sourceAudit()); e != nil {
		t.Fatal(e)
	}
	observed := &checkpointStore{Store: s.store, committed: make(chan struct{})}
	service := appmedia.NewSources(appmedia.SourceDeps{ISOs: observed, Sources: observed, Transfer: s.transfer, Locks: s.locks, Gate: s.gate})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.FetchOne(ctx, item.ID, "") }()
	select {
	case <-observed.committed:
	case <-time.After(10 * time.Second):
		t.Fatal("没有真实提交断点")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("关闭未退出")
	}
	src := mustSource(t, s, item.ID, "")
	if src.State != model.SourceFetching || src.Fetched != 3 {
		t.Fatal(src)
	}
	b, e := os.ReadFile(s.stagingPath(item.ID, ""))
	if e != nil || string(b) != "abc" {
		t.Fatal(string(b), e)
	}
	next := newAcquisitionFixture(s.store, s.dirs)
	next.transfer.resolver, next.transfer.dialContext = s.transfer.resolver, s.transfer.dialContext
	if e = next.FetchOne(t.Context(), item.ID, ""); e != nil {
		t.Fatal(e)
	}
	src = mustSource(t, next, item.ID, "")
	b, e = os.ReadFile(next.sourcePath(src))
	if e != nil || src.State != model.SourceFetched || src.Fetched != 6 || string(b) != "abcdef" {
		t.Fatal(src, string(b), e)
	}
}
