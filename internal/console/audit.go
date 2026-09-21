// 本文件实现审计中间件：记录写操作的操作人、动作、目标资源与客户端 IP。
// 对应实现计划任务 2，依据 SPEC §9.4。
//
// 只审计写操作（POST/PUT/DELETE），读操作不记——读的量大且价值低，需要时
// nginx access log 有记录（计划任务 2 明确要求）。做成中间件而不是十几个
// handler 各写一遍：避免十几处重复代码，且重复代码必然漏几个。
package console

import (
	"net/http"
)

// auditMiddleware 包装 next，在调用完成后（无论 handler 是否报错）写一条
// 审计记录。action 是「资源.动作」形式的固定字符串（如 task.create），
// 调用点在 routes() 里逐条指定。
//
// 目标资源 id 从 URL 路径参数取（Go 1.22+ ServeMux 的 {id} 命名参数）——
// 大部分写端点的资源 id 就在路径里；没有路径参数的端点（如 login 不算
// 写操作、task.create 的资源 id 是响应体里服务端生成的新 id，请求路径里
// 没有）用空字符串，不强行凑一个无意义的值。
//
// 审计写入失败不影响主流程（计划任务 2 明确要求：审计存储故障不该让运维
// 无法建任务）——本函数吞掉 Audit.Log 的错误，只在 Audit 本身可用时才
// 调用，且调用被放在 next.ServeHTTP 完成之后，不会阻塞或影响业务响应
// 已经写出的内容。
func (s *Server) auditMiddleware(action string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		if s.deps.Audit == nil {
			return
		}
		user, ok := userFromContext(r.Context())
		actor := ""
		if ok {
			actor = user.Username
		}
		target := r.PathValue("id")
		ip := s.clientIP(r)
		// 错误被有意吞掉：审计存储故障不该让运维无法建任务（计划任务 2）。
		// r.Context() 在这里仍然有效——本中间件函数还在处理同一个请求，
		// 尚未返回，请求的 context 只在整个 ServeHTTP 调用链结束或客户端
		// 断开时才会被取消。
		_ = s.deps.Audit.Log(r.Context(), actor, action, target, "client_ip="+ip)
	})
}
