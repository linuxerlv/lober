// 本文件实现审计中间件：记录写操作的操作人、动作、目标资源与客户端 IP。
// 对应实现计划任务 2，依据 SPEC §9.4。
//
// 只审计写操作（POST/PUT/DELETE），读操作不记——读的量大且价值低，需要时
// nginx access log 有记录（计划任务 2 明确要求）。做成中间件而不是十几个
// handler 各写一遍：避免十几处重复代码，且重复代码必然漏几个。
package console

import (
	"github.com/gin-gonic/gin"
)

// auditMiddleware 在调用链走完之后（无论 handler 是否报错）写一条审计
// 记录。action 是「资源.动作」形式的固定字符串（如 task.create），调用点
// 在各资源域的 routesXxx() 里逐条指定。
//
// 目标资源 id 从 URL 路径参数取，改用 gin 的 c.Param("id")——迁移前用的是
// Go 1.22+ ServeMux 的 r.PathValue("id")，两者语义等价（都是从路由声明的
// 命名参数取值），但 gin 接管路由之后路径参数只能通过 gin.Context.Param
// 取到：Go 标准库的 PathValue 依赖请求经过 ServeMux 且路由模式用
// "{id}" 语法登记，gin 引擎登记的是 ":id" 语法，走的是完全不同的一套
// 路由树，PathValue 在 gin 路由下永远取不到值。大部分写端点的资源 id 就
// 在路径里；没有路径参数的端点（如 login 不算写操作、task.create 的资源
// id 是响应体里服务端生成的新 id，请求路径里没有）用空字符串，不强行凑
// 一个无意义的值。
//
// 审计写入失败不影响主流程（计划任务 2 明确要求：审计存储故障不该让运维
// 无法建任务）——本函数吞掉 Audit.Log 的错误，只在 Audit 本身可用时才
// 调用，且调用被放在 c.Next() 完成之后，不会阻塞或影响业务响应已经写出
// 的内容。
func (s *Server) auditMiddleware(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if s.deps.Audit == nil {
			return
		}
		user, ok := userFromContext(c.Request.Context())
		actor := ""
		if ok {
			actor = user.Username
		}
		target := c.Param("id")
		ip := s.clientIP(c.Request)
		// 错误被有意吞掉：审计存储故障不该让运维无法建任务（计划任务 2）。
		// c.Request.Context() 在这里仍然有效——本中间件函数还在处理同一个
		// 请求，尚未返回，请求的 context 只在整个 ServeHTTP 调用链结束或
		// 客户端断开时才会被取消。
		_ = s.deps.Audit.Log(c.Request.Context(), actor, action, target, "client_ip="+ip)
	}
}
