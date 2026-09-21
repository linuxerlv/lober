// 本文件实现 SSE 事件流端点 GET /tasks/{id}/stream。对应实现计划任务 7，
// 依据 docs/contracts/http-console.md 约定 4、SPEC §12.6、§12.8。
//
// 这个端点不套响应包裹——它是事件流，不是 JSON。事件来源用轮询 Store
// 的事件表（按序号递增），间隔 1-2 秒：简单、无状态、进程重启不丢；
// 装机事件的产生频率是分钟级，这个延迟对运维无感。
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// streamPollInterval 是轮询 Store 事件表的间隔（计划任务 7：MVP 选轮询，
// 间隔取 1-2 秒）。
const streamPollInterval = 1500 * time.Millisecond

// routesStream 单独注册 SSE 端点——它不走 handleFunc（不套响应包裹，且
// 认证仍然按 Authorization 头校验，本函数手动做认证检查而不是复用
// requireAuth 中间件，因为认证失败时的响应体形态与其它端点一致，仍然是
// apierr 的 Envelope，只有「认证通过之后」的正常响应才不套包裹）。
func (s *Server) routesStream() {
	s.handleFunc("GET", "/tasks/{id}/stream", "", "", s.handleTaskStream)
}

// handleTaskStream 处理 GET /tasks/{id}/stream。
//
// 五条实现要点（计划任务 7）：
//  1. 三个响应头：Content-Type、Cache-Control、X-Accel-Buffering
//  2. 事件 id 取任务事件的序号，客户端重连带 Last-Event-ID 续传，不从头
//     重放
//  3. 每条事件后必须 flush
//  4. 任务进终态时发一条终态事件后主动关流
//  5. 客户端断开时要能退出循环——监听请求上下文的取消
func (s *Server) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		// 测试或部署环境的 ResponseWriter 不支持 Flush 时，SSE 的核心特性
		// （逐条推送）无法工作，直接报错比默默退化成一次性响应更安全——
		// 运维会看到明确的错误而不是「日志看起来冻住了」却毫无线索。
		http.Error(w, "本响应写入器不支持流式推送", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	afterSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))

	ctx := r.Context()
	ticker := time.NewTicker(streamPollInterval)
	defer ticker.Stop()

	// 首次立即推送一批已有事件（不等第一个 tick），否则客户端首次连接要
	// 等一个轮询周期才看到任何数据。
	if done := s.streamPushBatch(ctx, w, flusher, taskID, &afterSeq); done {
		return
	}

	for {
		select {
		case <-ctx.Done():
			// 客户端断开：必须退出循环，否则 goroutine 与底层连接资源
			// 泄漏（计划任务 7 明确要求：运维在列表与详情间来回切换会
			// 攒住一堆连接）。
			return
		case <-ticker.C:
			if done := s.streamPushBatch(ctx, w, flusher, taskID, &afterSeq); done {
				return
			}
		}
	}
}

// streamPushBatch 拉取 taskID 在 afterSeq 之后的全部新事件并逐条推送，
// 每条推送后 flush。若本批事件里出现终态事件，推送完成后返回 done=true，
// 调用方据此关闭连接（主动关流，不等客户端断开）。
func (s *Server) streamPushBatch(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, taskID string, afterSeq *int) bool {
	events, err := s.deps.Events.ListEvents(ctx, taskID, contract.EventFilter{AfterSeq: *afterSeq})
	if err != nil {
		return false
	}

	for _, e := range events {
		writeSSEEvent(w, e)
		flusher.Flush()
		if e.Seq > *afterSeq {
			*afterSeq = e.Seq
		}
	}

	tk, err := s.deps.Tasks.GetTask(ctx, taskID)
	if err == nil && tk.State.IsTerminal() {
		return true
	}
	return false
}

// writeSSEEvent 按约定 4 的格式写一条 SSE 事件：id 行、event 行、data
// 行，以空行结束。
func writeSSEEvent(w http.ResponseWriter, e model.TaskEvent) {
	payload := taskEventItem{
		TaskID: e.TaskID, Seq: e.Seq, TS: e.TS, Phase: string(e.Phase),
		Level: string(e.Level), Message: e.Message, RC: e.RC,
		HookDigest: e.HookDigest, Source: string(e.Source),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\n", e.Seq)
	fmt.Fprintf(w, "event: task-event\n")
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// parseLastEventID 解析 Last-Event-ID 请求头。解析失败或为空时返回 0
// （表示「从头」，即不设置下限——ListEvents 的 AfterSeq=0 语义是「不
// 过滤」，与「续传时只要更新的事件」两种含义在数值上恰好重合：首次连接
// 没有 Last-Event-ID，语义就是要全部已有事件）。
func parseLastEventID(raw string) int {
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}
