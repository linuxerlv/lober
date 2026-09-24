// 本文件实现 facts 读写端点：POST /v1/facts/{task}/{token} 与
// GET /v1/facts/{task}/{token}。对应实现计划任务 8，依据
// docs/contracts/http-machine.md 端点 5、6、SPEC §5.5。
//
// facts 是吃掉「需要新端点」这类需求的通用原语（§5.5）。两个端点都用
// 装机 token 或 finalize token 都可（firstboot 的 hook 也要读 facts）。
//
// 写入格式不用 JSON（§6.2）：installer ramdisk 里没有 jq，手工拼 JSON
// 转义会在日志带引号或换行时坏掉。body 是纯文本，每行一个键值对，空行与
// 注释行忽略。
package gateway

import (
	"bufio"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/core/model"
)

// parseFactsBody 解析 facts 写端点的纯文本 body：每行一个 key=value，
// = 后面到行尾都算 value（value 本身可含 =）。空行与以 # 开头的注释行
// 忽略。不含 = 的行也忽略（契约文档端点 5「错误行为」：忽略该行，不拒绝
// 整个请求——一行格式错不该让整批 fact 都写不进去）。
func parseFactsBody(body string) map[string]string {
	out := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := line[:idx]
		value := line[idx+1:]
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

// formatFactsBody 把 facts map 格式化为 key=value\n 逐行纯文本（契约文档
// 端点 6「成功响应」）。map 遍历顺序不确定，但 facts 是无序的键值集合，
// 契约文档没有要求任何特定顺序，因此这里不额外排序。
func formatFactsBody(facts map[string]string) string {
	var b strings.Builder
	for k, v := range facts {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}

// authenticateFactsRequest 校验 facts 端点的 token（同信号端点：装机 token
// 或 finalize token 均可）并交叉核对路径里的任务 id。ok 为 false 时调用方
// 应回 403。
func (s *Server) authenticateFactsRequest(c *gin.Context) (model.Task, bool) {
	pathTaskID := c.Param("task")
	token := c.Param("token")

	task, _, err := lookupTaskByEitherTokenPurpose(c.Request.Context(), s.deps.TaskStore, token)
	if err != nil || task.ID != pathTaskID {
		return model.Task{}, false
	}
	return task, true
}

// handleFactsWrite 处理 POST /v1/facts/:task/:token（对外 URL 仍是
// /v1/facts/{task}/{token}）：body 是纯文本，每行一个键值对，覆盖式合并
// 进该任务的 facts 存储。
func (s *Server) handleFactsWrite(c *gin.Context) {
	task, ok := s.authenticateFactsRequest(c)
	if !ok {
		c.Status(http.StatusForbidden)
		return
	}

	var bodyText string
	if c.Request.Body != nil {
		raw, _ := io.ReadAll(c.Request.Body)
		bodyText = string(raw)
	}

	facts := parseFactsBody(bodyText)
	_ = s.deps.FactStore.MergeFacts(c.Request.Context(), task.ID, facts)

	c.Status(http.StatusOK)
}

// handleFactsRead 处理 GET /v1/facts/:task/:token（对外 URL 仍是
// /v1/facts/{task}/{token}）：返回该任务当前全部 facts 的 key=value
// 逐行纯文本。
func (s *Server) handleFactsRead(c *gin.Context) {
	task, ok := s.authenticateFactsRequest(c)
	if !ok {
		c.Status(http.StatusForbidden)
		return
	}

	facts, err := s.deps.FactStore.GetFacts(c.Request.Context(), task.ID)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}

	// facts 的 value 来自机器上报，内容不受控，可能含 %；用 c.Data 而不是
	// c.String(code, body) 避免被当格式串解析（迁移契约 §6 的红线，与
	// answer.go 的 kickstart 正文是同一类风险）。
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(formatFactsBody(facts)))
}
