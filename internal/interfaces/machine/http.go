// Package machine exposes the plain HTTP installation protocol through declared interfaces.
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

type handler struct{ gateway execution.Gateway }

func New(gateway execution.Gateway) http.Handler { return &handler{gateway: gateway} }
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
		case execution.AnswerConflict, execution.SignalConflict:
			return 409
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
	// DHCP 把这一个地址同时交给网卡的引导客户端与安装器：iPXE 执行返回的菜单，
	// 而 Debian 安装器在 auto 模式下会把同一个地址当 preseed 下载，并覆盖内核
	// 命令行上的 preseed/url（D1）。装好了识别就交给安装器，其余一律按 iPXE 走。
	if installerAgent(r.UserAgent()) {
		h.installerBoot(w, r, q.Get("sn"))
		return
	}
	result, err := h.gateway.BootMenu(r.Context(), execution.BootInput{SN: q.Get("sn"), Arch: arch, Firmware: fw})
	if err != nil {
		fail(w, gatewayStatus(err, 503))
		return
	}
	bootResult(w, result)
}

// installerAgent 报告这次 /v1/boot 请求是不是安装器自己发起的。识别宁可保守：
// 只认抓包证实的 debian-installer，未知或空的 UA 一律走 iPXE 菜单，行为与分流
// 之前完全一致。
func installerAgent(agent string) bool {
	return strings.HasPrefix(strings.TrimSpace(agent), "debian-installer")
}

// installerBoot 给按 DHCP bootfile 地址前来取 preseed 的 Debian 安装器发真实应答：
// 它到这一步时 iPXE 必已引导过，pending 的任务没有安装器可发，保持不给安装；
// 非 preseed 格式不会出现 debian-installer 客户端，同样落回不给安装。发应答与
// /v1/answer 同步做状态推进（d-i 不会再取命令行里的应答地址，状态机若不在这里
// 推进会停在 booting）。
func (h *handler) installerBoot(w http.ResponseWriter, r *http.Request, sn string) {
	result, err := h.gateway.InstallerAnswer(r.Context(), execution.InstallerAnswerInput{SN: sn, RemoteAddr: r.RemoteAddr})
	if err != nil {
		fail(w, gatewayStatus(err, 503))
		return
	}
	bootResult(w, result)
}
func (h *handler) file(w http.ResponseWriter, r *http.Request, id, token, name string) {
	body, err := h.gateway.File(r.Context(), execution.FileInput{TaskID: id, Token: token, Name: name, RemoteAddr: r.RemoteAddr})
	if err != nil {
		fail(w, gatewayStatus(err, 403))
		return
	}
	w.Write(body)
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
	sig := executionrules.Signal{Phase: phase}
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
	if err := h.gateway.Signal(r.Context(), execution.SignalInput{TaskID: id, Token: token, RemoteAddr: r.RemoteAddr, Signal: sig}); err != nil {
		fail(w, gatewayStatus(err, 409))
		return
	}
	w.WriteHeader(http.StatusOK)
}
