// 本文件实现信号端点 POST /v1/signal/{task}/{token}/{phase}。对应实现
// 计划任务 7，依据 docs/contracts/http-machine.md 端点 4、SPEC §6.1、§6.2、
// §6.4。
package gateway

import "net/http"

// handleSignal 处理 POST /v1/signal/{task}/{token}/{phase}：状态机由信号
// 驱动，这是机器侧唯一的主动上报通道。任务 7 实现。
func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 7 实现")
}
