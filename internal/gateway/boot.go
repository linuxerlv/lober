// 本文件实现引导决策端点 GET /v1/boot。对应实现计划任务 4、5，依据
// docs/contracts/http-machine.md 端点 2、SPEC §4.2、§4.3、§4.6、§9.1。
package gateway

import "net/http"

// handleBoot 处理 GET /v1/boot：引导决策的唯一入口，默认拒绝（§9.1 第一道
// 闸门）。任务 4（no_task 路径）、任务 5（命中路径）实现。
func (s *Server) handleBoot(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 4/5 实现")
}
