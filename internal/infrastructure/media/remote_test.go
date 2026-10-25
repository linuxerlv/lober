package iso

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func TestRemoteTLSPinnedAddressRetainsSNIAndVerification(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	names := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS certificate accepted") }))
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { names <- hello.ServerName; return nil, nil }}
	server.StartTLS()
	defer server.Close()
	s.transfer.resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.2")}, nil
	}
	s.transfer.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !strings.HasPrefix(addr, "10.0.0.2:") {
			t.Errorf("hostname dialed: %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "https://"))
	}
	if err := s.Register(t.Context(), item, "url", "https://mirror.example/medium.iso", "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err == nil {
		t.Fatal("untrusted test certificate accepted")
	}
	if name := <-names; name != "mirror.example" {
		t.Fatalf("TLS SNI=%q", name)
	}
}

func TestRemoteCompleteUnknownLengthAndExplicitRetryRestarts(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	requests := 0
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != "" {
			t.Error("explicit Retry retained partial offset")
		}
		if requests == 1 {
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("abc"))
			return
		}
		w.(http.Flusher).Flush() // Force chunked transfer without Content-Length.
		w.Write([]byte("abcdefghijkl"))
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 0, sourceDigest([]byte("abcdefghijkl")), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err == nil {
		t.Fatal("truncated response accepted")
	}
	if src := mustSource(t, s, item.ID, ""); src.State != model.SourceFailed || src.Fetched != 3 {
		t.Fatalf("interrupted progress: %+v", src)
	}
	if err := s.Retry(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	if src := mustSource(t, s, item.ID, ""); src.State != model.SourceFetched || src.Fetched != 12 || src.TotalSize != 12 {
		t.Fatalf("%+v", src)
	}
}

func sourceHTTP(t *testing.T, s *acquisitionFixture, h http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	s.transfer.resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if host == "blocked.example" {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.2")}, nil
	}
	s.transfer.dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !strings.HasPrefix(addr, "10.0.0.2:") {
			t.Errorf("did not dial pinned IP: %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}
	return "http://mirror.example/medium.iso"
}

func TestRemoteResumeUsesActualFileLength(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	raw := []byte("abcdefghijkl")
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Errorf("Range=%q", got)
		}
		w.Header().Set("Content-Range", "bytes 3-11/12")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(raw[3:])
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 0, sourceDigest(raw), sourceAudit()); err != nil {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	src.State = model.SourceFetching
	src = mustSourceSave(t, s, src)
	src.TotalSize = 12
	src.Fetched = 8
	mustSourceSave(t, s, src)
	if err := os.WriteFile(s.stagingPath(item.ID, ""), raw[:3], 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	src = mustSource(t, s, item.ID, "")
	if src.State != model.SourceFetched || src.Fetched != 12 {
		t.Fatalf("%+v", src)
	}
	got, err := os.ReadFile(s.sourcePath(src))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRemoteWithoutDigestRestartsChangedEntity(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 3-5/6")
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("new"))
			return
		}
		w.Write([]byte("NEWnew"))
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 6, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	src.State = model.SourceFetching
	mustSourceSave(t, s, src)
	if err := os.WriteFile(s.stagingPath(item.ID, ""), []byte("OLD"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(s.sourcePath(mustSource(t, s, item.ID, "")))
	if err != nil || string(got) != "NEWnew" {
		t.Fatalf("mixed resource published: %q, %v", got, err)
	}
}

func TestRemoteServerWithoutRangeTruncatesPartial(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("new")) })
	if err := s.Register(t.Context(), item, "url", addr, "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	src.State = model.SourceFetching
	mustSourceSave(t, s, src)
	if err := os.WriteFile(s.stagingPath(item.ID, ""), []byte("old-but-longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(s.sourcePath(mustSource(t, s, item.ID, "")))
	if err != nil || string(got) != "new" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRemoteRejectsInconsistentContentRangeAnd416(t *testing.T) {
	for _, tc := range []struct {
		status int
		cr     string
		ok     bool
	}{{206, "bytes 1-4/5", false}, {206, "bytes 0-9/10", false}, {206, "bytes +3-4/5", false}, {416, "", false}, {416, "bytes */10", false}, {416, "bytes */+3", false}, {416, "bytes */3", true}} {
		t.Run(fmt.Sprint(tc.status, tc.cr), func(t *testing.T) {
			s, item := sourceFixture(t, "rocky")
			addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Range", tc.cr)
				w.WriteHeader(tc.status)
				if tc.status == 206 {
					w.Write([]byte("xx"))
				}
			})
			if err := s.Register(t.Context(), item, "url", addr, "", 0, sourceDigest([]byte("abc")), sourceAudit()); err != nil {
				t.Fatal(err)
			}
			src := mustSource(t, s, item.ID, "")
			src.State = model.SourceFetching
			mustSourceSave(t, s, src)
			if err := os.WriteFile(s.stagingPath(item.ID, ""), []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			err := s.FetchOne(t.Context(), item.ID, "")
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
			src = mustSource(t, s, item.ID, "")
			if (src.State == model.SourceFetched) != tc.ok {
				t.Fatalf("%+v", src)
			}
		})
	}
}

func TestRemoteRedirectToLoopbackDiscardsPartial(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://blocked.example/private", http.StatusFound)
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	src := mustSource(t, s, item.ID, "")
	src.State = model.SourceFetching
	mustSourceSave(t, s, src)
	if err := os.WriteFile(s.stagingPath(item.ID, ""), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.FetchOne(t.Context(), item.ID, ""); err == nil {
		t.Fatal("unsafe redirect accepted")
	}
	if _, err := os.Stat(s.stagingPath(item.ID, "")); !os.IsNotExist(err) {
		t.Fatalf("unsafe partial retained: %v", err)
	}
}

func TestRemoteCancelDoesNotWaitForDownloadAndProgressCannotOverwrite(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	started := make(chan struct{})
	addr := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	if err := s.Register(t.Context(), item, "url", addr, "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.FetchOne(t.Context(), item.ID, "") }()
	<-started
	if err := s.Cancel(t.Context(), item.ID, sourceAudit()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
	if src := mustSource(t, s, item.ID, ""); src.State != model.SourceCancelled {
		t.Fatalf("cancel overwritten: %+v", src)
	}
}
