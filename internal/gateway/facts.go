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
func (s *Server) authenticateFactsRequest(r *http.Request) (model.Task, bool) {
	pathTaskID := r.PathValue("task")
	token := r.PathValue("token")

	task, _, err := lookupTaskByEitherTokenPurpose(r.Context(), s.deps.TaskStore, token)
	if err != nil || task.ID != pathTaskID {
		return model.Task{}, false
	}
	return task, true
}

// handleFactsWrite 处理 POST /v1/facts/{task}/{token}：body 是纯文本，每行
// 一个键值对，覆盖式合并进该任务的 facts 存储。
func (s *Server) handleFactsWrite(w http.ResponseWriter, r *http.Request) {
	task, ok := s.authenticateFactsRequest(r)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	var bodyText string
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		bodyText = string(raw)
	}

	facts := parseFactsBody(bodyText)
	_ = s.deps.FactStore.MergeFacts(r.Context(), task.ID, facts)

	w.WriteHeader(http.StatusOK)
}

// handleFactsRead 处理 GET /v1/facts/{task}/{token}：返回该任务当前全部
// facts 的 key=value 逐行纯文本。
func (s *Server) handleFactsRead(w http.ResponseWriter, r *http.Request) {
	task, ok := s.authenticateFactsRequest(r)
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	facts, err := s.deps.FactStore.GetFacts(r.Context(), task.ID)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(formatFactsBody(facts)))
}
