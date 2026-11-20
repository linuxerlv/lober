package iso

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
)

func shortRemoteTransfer() *Transfer {
	tr := NewTransfer(Dirs{})
	tr.timeouts = downloadTimeouts{
		lookup: 100 * time.Millisecond, connect: 100 * time.Millisecond,
		tlsHandshake: 100 * time.Millisecond, responseHeader: 100 * time.Millisecond,
		readIdle: 150 * time.Millisecond,
	}
	tr.resolver = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil
	}
	return tr
}

func remoteTestServer(t *testing.T, tr *Transfer, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	tr.dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
}

func TestRemoteTimeouts(t *testing.T) {
	for _, phase := range []string{"lookup", "connect", "TLS", "headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			tr := shortRemoteTransfer()
			address := "http://mirror.example/medium.iso"
			switch phase {
			case "lookup":
				tr.resolver = func(ctx context.Context, _ string) ([]netip.Addr, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			case "connect":
				tr.dialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			case "TLS":
				address = "https://mirror.example/medium.iso"
				tr.dialContext = func(context.Context, string, string) (net.Conn, error) {
					client, peer := net.Pipe()
					t.Cleanup(func() { peer.Close() })
					go func() { _, _ = io.Copy(io.Discard, peer) }()
					return client, nil
				}
			default:
				remoteTestServer(t, tr, func(w http.ResponseWriter, r *http.Request) {
					if phase == "body" {
						w.Header().Set("Content-Length", "100")
						_, _ = w.Write([]byte("abc"))
						w.(http.Flusher).Flush()
					}
					<-r.Context().Done()
				})
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
			resp, err := tr.httpClient().Do(req)
			if err == nil {
				defer resp.Body.Close()
				_, err = io.ReadAll(resp.Body)
			}
			if err == nil || ctx.Err() != nil {
				t.Fatalf("%s 应由阶段超时退出，不能等调用方超时：err=%v，ctx=%v", phase, err, ctx.Err())
			}
		})
	}
}

func TestRemoteProgressHasNoTotalTimeout(t *testing.T) {
	tr := shortRemoteTransfer()
	tr.timeouts.responseHeader = time.Second
	tr.timeouts.readIdle = 500 * time.Millisecond
	remoteTestServer(t, tr, func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 8; i++ {
			_, _ = w.Write([]byte("a"))
			w.(http.Flusher).Flush()
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://mirror.example/medium.iso", nil)
	resp, err := tr.httpClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "aaaaaaaa" {
		t.Fatalf("持续有进展的下载被中断：%q，%v", body, err)
	}
}

func TestRemoteTimeoutRetainsCallerCancellation(t *testing.T) {
	tr := shortRemoteTransfer()
	started := make(chan struct{})
	remoteTestServer(t, tr, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://mirror.example/medium.iso", nil)
		resp, err := tr.httpClient().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("请求没有到达测试服务")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消信号丢失：%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消后仍在等待")
	}
}

func TestRemoteStallPreservesPartialFileAndFailure(t *testing.T) {
	s, item := sourceFixture(t, "rocky")
	s.transfer.timeouts = shortRemoteTransfer().timeouts
	address := sourceHTTP(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	if err := s.Register(t.Context(), item, "url", address, "", 0, "", sourceAudit()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	err := s.FetchOne(ctx, item.ID, "")
	src := mustSource(t, s, item.ID, "")
	if err == nil || ctx.Err() != nil || src.State != model.SourceFailed || src.Fetched != 3 || src.Error != "无法下载镜像" {
		t.Fatalf("停滞下载未正确保存失败和进度：%+v，%v，ctx=%v", src, err, ctx.Err())
	}
	if strings.Contains(err.Error(), "mirror") || strings.Contains(err.Error(), address) {
		t.Fatal("下载错误暴露来源地址", err)
	}
	session, err := s.transfer.BeginFetch(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if offset, err := session.Resume(t.Context()); err != nil || offset != 3 {
		t.Fatalf("下载断点丢失：offset=%d，%v", offset, err)
	}
}
