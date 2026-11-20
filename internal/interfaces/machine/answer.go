package machine

import (
	"net/http"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/model"
)

func (h *handler) answer(w http.ResponseWriter, r *http.Request) {
	task, token := r.PathValue("task"), r.PathValue("token")
	// 路径里的任务与令牌先查，和后端原先把它们放在方法检查之前的顺序一致。
	if !model.ValidID(task) || !model.IsSafeWord(token) {
		fail(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed)
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, http.StatusBadRequest)
		return
	}
	h.file(w, r, task, token, r.PathValue("file"))
}

func (h *handler) file(w http.ResponseWriter, r *http.Request, id, token, name string) {
	body, err := h.gateway.File(r.Context(), execution.FileInput{TaskID: id, Token: token, Name: name, RemoteAddr: r.RemoteAddr})
	if err != nil {
		fail(w, gatewayStatus(err, 403))
		return
	}
	w.Write(body)
}
