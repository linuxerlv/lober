// 本文件实现首跳脚本端点 GET /boot.ipxe。对应实现计划任务 3，依据
// docs/contracts/http-machine.md 端点 1、SPEC §4.1、§4.7。
package gateway

import "net/http"

// handleBootScript 处理 GET /boot.ipxe：全网共用一份、静态文本，不看机器
// 身份、不做任何决策（§4.7：分叉全部收敛在 /v1/boot）。任务 3 实现。
func (s *Server) handleBootScript(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 3 实现")
}
