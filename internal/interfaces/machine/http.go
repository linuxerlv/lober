// Package machine exposes the plain HTTP installation protocol through declared interfaces.
package machine

import (
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/lober/lober/internal/application/discovery"
	"github.com/lober/lober/internal/application/execution"
)

type handler struct {
	gateway           execution.Gateway
	discovery         discovery.Machine
	discoveryNetworks []netip.Prefix
	mux               *http.ServeMux
}

func New(gateway execution.Gateway) http.Handler {
	return NewWithDiscovery(gateway, nil, nil)
}

func NewWithDiscovery(gateway execution.Gateway, discoveries discovery.Machine, networks []netip.Prefix) http.Handler {
	h := &handler{gateway: gateway, discovery: discoveries, discoveryNetworks: slices.Clone(networks), mux: http.NewServeMux()}
	h.mux.HandleFunc("/v1/boot", h.boot)
	h.mux.HandleFunc("/v1/answer/{task}/{token}/{file...}", h.answer)
	h.mux.HandleFunc("/v1/signal/{task}/{token}/{phase}", h.signal)
	h.mux.HandleFunc("/v1/signal/{task}/{token}/{phase}/{rest...}", h.signalExtra)
	h.mux.HandleFunc("/v1/inventory/{sn}/{token}", h.inventory)
	h.mux.HandleFunc("/v1/discovery/{mac}", h.discoveryReport)
	h.mux.HandleFunc("/", h.notFound)
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !routable(r.URL.Path) {
		fail(w, http.StatusNotFound)
		return
	}
	h.mux.ServeHTTP(w, r)
}

// routable 只认机器协议的已知路径形状，并在 ServeMux 之前挡掉空段、. 与 ..。
// ServeMux 会清理这些路径并重定向；直接交给它，原来的 404 会变成 301。
func routable(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	if len(parts) < 2 || parts[0] != "v1" {
		return false
	}
	switch parts[1] {
	case "boot":
		return len(parts) == 2
	case "answer", "signal":
		return len(parts) >= 5
	case "discovery":
		return len(parts) == 3
	case "inventory":
		return len(parts) == 4
	}
	return false
}
