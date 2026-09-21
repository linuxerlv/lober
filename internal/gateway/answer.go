// 本文件实现应答文件端点 GET /v1/answer/{task}/{token}/{file}。对应实现
// 计划任务 6，依据 docs/contracts/http-machine.md 端点 3、SPEC §4.4、§4.5、
// §5.1。
package gateway

import "net/http"

// handleAnswer 处理 GET /v1/answer/{task}/{token}/{file}：应答文件是装机的
// 实际指令，暴露控制用状态门控（§4.4）。任务 6 实现。
func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 6 实现")
}
