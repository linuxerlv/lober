// 本文件实现 facts 读写端点：POST /v1/facts/{task}/{token} 与
// GET /v1/facts/{task}/{token}。对应实现计划任务 8，依据
// docs/contracts/http-machine.md 端点 5、6、SPEC §5.5。
package gateway

import "net/http"

// handleFactsWrite 处理 POST /v1/facts/{task}/{token}：body 是纯文本，每行
// 一个键值对，覆盖式合并进该任务的 facts 存储。任务 8 实现。
func (s *Server) handleFactsWrite(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 8 实现")
}

// handleFactsRead 处理 GET /v1/facts/{task}/{token}：返回该任务当前全部
// facts 的 key=value 逐行纯文本。任务 8 实现。
func (s *Server) handleFactsRead(w http.ResponseWriter, r *http.Request) {
	panic("todo: 任务 8 实现")
}
