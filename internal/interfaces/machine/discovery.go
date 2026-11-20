package machine

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"

	"github.com/lober/lober/internal/domain/discovery"
)

// 使用直接连接的地址。代理转发时由代理限制装机网来源。
func (h *handler) allowDiscovery(r *http.Request) bool {
	if h.discovery == nil {
		return false
	}
	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr := remote.Addr().WithZone("").Unmap()
	for _, network := range h.discoveryNetworks {
		if network.Contains(addr) {
			return true
		}
	}
	return false
}

func (h *handler) discoveryReport(w http.ResponseWriter, r *http.Request) {
	if !h.allowDiscovery(r) {
		fail(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		fail(w, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, discovery.MaxReportBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		fail(w, http.StatusBadRequest)
		return
	}
	switch err = h.discovery.Submit(r.Context(), r.PathValue("mac"), token, body); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, discovery.ErrDisabled):
		fail(w, http.StatusNotFound)
	case errors.Is(err, discovery.ErrToken):
		fail(w, http.StatusUnauthorized)
	case errors.Is(err, discovery.ErrReport):
		fail(w, http.StatusUnprocessableEntity)
	default:
		fail(w, http.StatusServiceUnavailable)
	}
}
