package machine

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
)

func (h *handler) boot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !only(q, "sn", "arch", "firmware") {
		fail(w, http.StatusBadRequest)
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
		fail(w, http.StatusBadRequest)
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
