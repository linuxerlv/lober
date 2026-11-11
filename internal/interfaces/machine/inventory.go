package machine

import (
	"io"
	"net/http"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/model"
)

// maxInventoryBytes 与领域层的上报上限一致；这里先按 HTTP 层挡一次，不把超大正文读进内存。
const maxInventoryBytes = 64 << 10

func (h *handler) inventory(w http.ResponseWriter, r *http.Request) {
	sn, token := r.PathValue("sn"), r.PathValue("token")
	// 采集入口原来就对错方法回 404，不是 405；这里保持原样。
	if r.Method != http.MethodPost || !model.IsSafeWord(token) {
		fail(w, http.StatusNotFound)
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxInventoryBytes+1))
	if err != nil || len(body) > maxInventoryBytes {
		fail(w, http.StatusBadRequest)
		return
	}
	// 交给网关：分类、错误映射都在执行模块里做，接口层不知道采集模块存在。
	if err := h.gateway.InventoryReport(r.Context(), execution.InventoryInput{
		SN: sn, Token: token, Body: body, RemoteAddr: r.RemoteAddr,
	}); err != nil {
		code := gatewayStatus(err, 503)
		if code == http.StatusUnprocessableEntity {
			// 值不可信：正文带类别名，便于串口日志直接看出问题。
			http.Error(w, err.Error(), code)
			return
		}
		fail(w, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
