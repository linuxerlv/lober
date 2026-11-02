package machine

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/lober/lober/internal/application/execution"
)

func gatewayStatus(err error, fallback int) int {
	var failure *execution.GatewayError
	if errors.As(err, &failure) {
		switch failure.Kind {
		case execution.LookupUnavailable:
			return 503
		case execution.RenderRejected:
			return 422
		case execution.Unauthorized:
			return 403
		case execution.FileNotFound:
			return 404
		case execution.AnswerConflict, execution.SignalConflict, execution.InventoryBusy:
			return 409
		case execution.ReportRejected:
			return 422
		case execution.NotSupported:
			return 404
		}
	}
	return fallback
}

func bootResult(w http.ResponseWriter, result execution.BootResult) {
	if !result.Found {
		io.WriteString(w, "#!ipxe\nexit || sanboot --no-describe --drive 0x80\n")
		return
	}
	w.Write(result.Body)
}

func fail(w http.ResponseWriter, code int) { http.Error(w, http.StatusText(code), code) }

func (h *handler) notFound(w http.ResponseWriter, _ *http.Request) { fail(w, http.StatusNotFound) }

func only(q url.Values, names ...string) bool {
	for name, values := range q {
		if len(values) != 1 {
			return false
		}
		known := false
		for _, allowed := range names {
			if name == allowed {
				known = true
			}
		}
		if !known {
			return false
		}
	}
	return true
}
