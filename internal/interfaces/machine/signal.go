package machine

import (
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

func (h *handler) signal(w http.ResponseWriter, r *http.Request) {
	task, token := r.PathValue("task"), r.PathValue("token")
	// 先查路径，再查方法：原来的入口也是这个顺序。
	if !model.ValidID(task) || !model.IsSafeWord(token) {
		fail(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed)
		return
	}
	h.signalRequest(w, r, task, token, model.SignalPhase(r.PathValue("phase")))
}

// signalExtra 接住多出来的路径段。原来的入口先看方法、再看段数：
// 非 POST 一律 405，POST 才是 404。
func (h *handler) signalExtra(w http.ResponseWriter, r *http.Request) {
	if !model.ValidID(r.PathValue("task")) || !model.IsSafeWord(r.PathValue("token")) {
		fail(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed)
		return
	}
	fail(w, http.StatusNotFound)
}

func (h *handler) signalRequest(w http.ResponseWriter, r *http.Request, id, token string, phase model.SignalPhase) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !only(q, "slot", "timing", "instance", "digest", "rc") {
		fail(w, http.StatusBadRequest)
		return
	}
	sig := executionrules.Signal{Phase: phase}
	slotPhase := phase == model.PhaseSlotStart || phase == model.PhaseSlotEnd || phase == model.PhaseHookStart || phase == model.PhaseHookReport
	if slotPhase {
		sig.Slot, err = strconv.Atoi(q.Get("slot"))
		if err != nil || sig.Slot < 1 {
			fail(w, http.StatusBadRequest)
			return
		}
		sig.Timing = catalog.Timing(q.Get("timing"))
		if !sig.Timing.Valid() {
			fail(w, http.StatusBadRequest)
			return
		}
		rc, parseErr := strconv.ParseInt(q.Get("rc"), 10, 32)
		if parseErr != nil {
			fail(w, http.StatusBadRequest)
			return
		}
		sig.RC = int(rc)
		if strings.HasPrefix(string(phase), "hook-") {
			sig.Instance, err = strconv.Atoi(q.Get("instance"))
			sig.Digest = q.Get("digest")
			bytes, digestErr := hex.DecodeString(sig.Digest)
			if err != nil || sig.Instance < 1 || digestErr != nil || len(bytes) != 32 || strings.ToLower(sig.Digest) != sig.Digest {
				fail(w, http.StatusBadRequest)
				return
			}
		} else if q.Has("instance") || q.Has("digest") {
			fail(w, http.StatusBadRequest)
			return
		}
	} else if len(q) != 0 {
		fail(w, http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	sig.Body, err = io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge)
		} else {
			fail(w, http.StatusBadRequest)
		}
		return
	}
	if err := h.gateway.Signal(r.Context(), execution.SignalInput{TaskID: id, Token: token, RemoteAddr: r.RemoteAddr, Signal: sig}); err != nil {
		fail(w, gatewayStatus(err, 409))
		return
	}
	w.WriteHeader(http.StatusOK)
}
