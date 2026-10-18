// Package machine exposes the plain HTTP installation protocol through declared interfaces.
package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

type Signal struct {
	Phase    string
	Slot     int
	Timing   catalog.Timing
	Instance int
	Digest   string
	RC       int
	Body     []byte
}
type Engine interface {
	Boot(context.Context, string, catalog.Arch, catalog.Firmware) (model.Task, bool, error)
	GetAuthorized(context.Context, string, string, bool) (model.Task, error)
	Answer(context.Context, string, string) (model.Task, error)
	Signal(context.Context, string, string, Signal) error
	Credentials(model.Task) (string, string)
}
type Request struct {
	Context context.Context
	Task    model.Task
	Kind    catalog.FileKind
	Slot    string
	Values  map[catalog.SystemValue]string
}
type Renderer func(Request) ([]byte, error)
type Endpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }
type Observer func(model.Task, string)
type handler struct {
	engine    Engine
	render    Renderer
	endpoints Endpoints
	observe   Observer
}

func New(engine Engine, render Renderer, endpoints Endpoints, observer ...Observer) http.Handler {
	h := &handler{engine: engine, render: render, endpoints: endpoints}
	if len(observer) > 0 {
		h.observe = observer[0]
	}
	return h
}
func (h *handler) values(task model.Task) map[catalog.SystemValue]string {
	install, final := h.engine.Credentials(task)
	return map[catalog.SystemValue]string{"gateway": h.endpoints.Gateway, "artifacts": h.endpoints.Artifacts, "smb": h.endpoints.SMB, "syslog-addr": h.endpoints.SyslogAddr, "task": task.ID, "sn": task.HostSN, "token": install, "finalize-token": final}
}
func fail(w http.ResponseWriter, code int) { http.Error(w, http.StatusText(code), code) }
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Path == "/v1/boot" {
		if r.Method != http.MethodGet {
			fail(w, 405)
			return
		}
		h.boot(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) < 5 || parts[0] != "v1" || !model.ValidID(parts[2]) || !model.IsSafeWord(parts[3]) {
		fail(w, 404)
		return
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			fail(w, 404)
			return
		}
	}
	switch parts[1] {
	case "answer":
		if r.Method != http.MethodGet {
			fail(w, 405)
			return
		}
		if r.URL.RawQuery != "" {
			fail(w, 400)
			return
		}
		h.file(w, r, parts[2], parts[3], strings.Join(parts[4:], "/"))
	case "signal":
		if r.Method != http.MethodPost {
			fail(w, 405)
			return
		}
		if len(parts) != 5 {
			fail(w, 404)
			return
		}
		h.signal(w, r, parts[2], parts[3], parts[4])
	default:
		fail(w, 404)
	}
}
func (h *handler) boot(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !only(q, "sn", "arch", "firmware") {
		fail(w, 400)
		return
	}
	arch := catalog.Arch(q.Get("arch"))
	if !arch.Valid() {
		arch, _ = catalog.ArchFromIPXE(q.Get("arch"))
	}
	fw := catalog.Firmware(q.Get("firmware"))
	if !fw.Valid() {
		fw, _ = catalog.FirmwareFromIPXE(q.Get("firmware"))
	}
	if !arch.Valid() || !fw.Valid() {
		fail(w, 400)
		return
	}
	task, found, err := h.engine.Boot(r.Context(), q.Get("sn"), arch, fw)
	if err != nil {
		fail(w, 503)
		return
	}
	if !found {
		io.WriteString(w, "#!ipxe\nexit || sanboot --no-describe --drive 0x80\n")
		return
	}
	body, err := h.render(Request{Context: r.Context(), Task: task, Kind: catalog.FileMenu, Values: h.values(task)})
	if err != nil {
		fail(w, 422)
		return
	}
	w.Write(body)
}
func (h *handler) file(w http.ResponseWriter, r *http.Request, id, token, name string) {
	task, err := h.engine.GetAuthorized(r.Context(), id, token, false)
	if err != nil {
		fail(w, 403)
		return
	}
	if h.observe != nil {
		h.observe(task, r.RemoteAddr)
	}
	var body []byte
	var kind catalog.FileKind
	var slotName string
	if strings.HasPrefix(name, "hooks/") {
		digest := strings.TrimPrefix(name, "hooks/")
		if len(digest) != 64 {
			fail(w, 404)
			return
		}
		found := false
		for _, slot := range task.Snapshot.Slots {
			if !slot.Exec {
				continue
			}
			for _, hook := range slot.Instances {
				if hook.Digest == digest {
					sum := sha256.Sum256([]byte(hook.Body))
					if hex.EncodeToString(sum[:]) != digest {
						fail(w, 422)
						return
					}
					body = []byte(hook.Body)
					found = true
				}
			}
		}
		if !found {
			fail(w, 404)
			return
		}
	} else {
		if name == fileName(task.Snapshot.Format, catalog.FileAnswer) {
			kind = catalog.FileAnswer
		} else if aux := fileName(task.Snapshot.Format, catalog.FileAux); aux != "" && name == aux {
			kind = catalog.FileAux
		} else {
			for _, slot := range task.Snapshot.Slots {
				if slot.Exec && name == "scripts/"+strconv.Itoa(slot.Seq)+".sh" {
					kind = catalog.FileScript
					slotName = slot.Name
					break
				}
			}
		}
		if kind == "" {
			fail(w, 404)
			return
		}
		body, err = h.render(Request{Context: r.Context(), Task: task, Kind: kind, Slot: slotName, Values: h.values(task)})
		if err != nil {
			fail(w, 422)
			return
		}
	}
	// Recheck state by CAS before releasing the primary answer; concurrent cancel wins.
	if kind == catalog.FileAnswer {
		if _, err := h.engine.Answer(r.Context(), id, token); err != nil {
			fail(w, 409)
			return
		}
	}
	w.Write(body)
}
func fileName(format catalog.Format, kind catalog.FileKind) string {
	name, _ := catalog.DeliveryFileName(format, kind)
	return name
}
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
func (h *handler) signal(w http.ResponseWriter, r *http.Request, id, token, phase string) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !only(q, "slot", "timing", "instance", "digest", "rc") {
		fail(w, 400)
		return
	}
	sig := Signal{Phase: phase}
	slotPhase := phase == "slot-start" || phase == "slot-end" || phase == "hook-start" || phase == "hook-report"
	if slotPhase {
		sig.Slot, err = strconv.Atoi(q.Get("slot"))
		if err != nil || sig.Slot < 1 {
			fail(w, 400)
			return
		}
		sig.Timing = catalog.Timing(q.Get("timing"))
		if !sig.Timing.Valid() {
			fail(w, 400)
			return
		}
		rc, parseErr := strconv.ParseInt(q.Get("rc"), 10, 32)
		if parseErr != nil {
			fail(w, 400)
			return
		}
		sig.RC = int(rc)
		if strings.HasPrefix(phase, "hook-") {
			sig.Instance, err = strconv.Atoi(q.Get("instance"))
			sig.Digest = q.Get("digest")
			bytes, digestErr := hex.DecodeString(sig.Digest)
			if err != nil || sig.Instance < 1 || digestErr != nil || len(bytes) != 32 || strings.ToLower(sig.Digest) != sig.Digest {
				fail(w, 400)
				return
			}
		} else if q.Has("instance") || q.Has("digest") {
			fail(w, 400)
			return
		}
	} else if len(q) != 0 {
		fail(w, 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	sig.Body, err = io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413)
		} else {
			fail(w, 400)
		}
		return
	}
	if h.observe != nil {
		final := sig.Timing == catalog.TimingFirstboot || phase == "booted" || phase == "init-start" || phase == "init-done" || phase == "ready"
		if task, err := h.engine.GetAuthorized(r.Context(), id, token, final); err == nil {
			h.observe(task, r.RemoteAddr)
		}
	}
	if err := h.engine.Signal(r.Context(), id, token, sig); err != nil {
		fail(w, 409)
		return
	}
	w.WriteHeader(http.StatusOK)
}
