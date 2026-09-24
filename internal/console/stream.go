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

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// streamPollInterval 是轮询 Store 事件表的间隔（计划任务 7：MVP 选轮询，
// 间隔取 1-2 秒）。
const streamPollInterval = 1500 * time.Millisecond

// routesStream 单独注册 SSE 端点。它仍然要求已登录（用 s.handle 而不是
// s.handlePublic），但不套 apierr 的响应包裹——认证失败时 requireAuth
// 中间件走 apierr.Abort，响应体仍然是标准 Envelope；只有「认证通过
// 之后」的正常响应才不走 apierr.OK（那会写一个完整的 JSON 信封并结束
// 响应，与 SSE 逐条推送的长连接语义冲突，见迁移契约 §4「特别注意」）。
// code、action 都传空——本端点不要求特定权限码，也不记审计（契约 §4
// 冻结表：GET /tasks/:id/stream 两列都是 "—"）。
func (s *Server) routesStream() {
	s.handle("GET", "/tasks/:id/stream", "", "", s.handleTaskStream)
}

// handleTaskStream 处理 GET /tasks/:id/stream。
//
// 五条实现要点（计划任务 7）：
//  1. 三个响应头：Content-Type、Cache-Control、X-Accel-Buffering
//  2. 事件 id 取任务事件的序号，客户端重连带 Last-Event-ID 续传，不从头
//     重放
//  3. 每条事件后必须 flush
//  4. 任务进终态时发一条终态事件后主动关流
//  5. 客户端断开时要能退出循环——监听请求上下文的取消
//
// 不走 apierr.OK/Fail：那两者内部调用 c.JSON，会写一个完整的 JSON 信封
// 并结束响应，与本端点「保持连接、逐条推送」的语义冲突（迁移契约 §4）。
// 直接操作 c.Writer（gin.ResponseWriter）：它是 http.ResponseWriter 的
// 超集，本文件原有的 Header()/WriteHeader()/Write() 调用方式不需要改
// 写法，只是换了持有者。
//
// gin.ResponseWriter 接口本身声明了 http.Flusher（迁移契约之外的一个
// 事实：gin 的封装决定的，见 gin/response_writer.go），因此
// c.Writer.(http.Flusher) 这个类型断言在 gin 下恒真，不会再像标准库时代
// 那样在「底层写入器不支持 Flush」时失败——gin.ResponseWriter.Flush()
// 内部自己会判断底层是否真的实现 http.Flusher，不支持时静默空操作。
// 保留这个断言只是防御性写法（万一某个未来版本的 gin 改变这个接口
// 约定），生产环境走标准库 net/http.Server 时底层一定支持 Flush，测试
// 里的 flushRecorder 也显式实现了 Flush，因此这条曾经的「显式报错」
// 路径在 gin 之下已经是不可达分支，不是本次改动主动去掉了一层校验。
func (s *Server) handleTaskStream(c *gin.Context) {
	taskID := c.Param("id")

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "本响应写入器不支持流式推送", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	afterSeq := parseLastEventID(c.GetHeader("Last-Event-ID"))

	// pushed 记录本连接已推送过的事件 Seq。单靠 afterSeq 水位不够：
	// contract.EventStore 规定 Seq 非正数表示非信号来源的事件（扫描循环判
	// 超时、人工操作、任务创建），而水位初值是 0、只在 e.Seq > afterSeq 时
	// 抬高 —— 非正 Seq 永远不满足，水位卡在 0，于是每轮轮询都把这些事件
	// 重新推一遍。真机上的现象是任务详情页历史事件每 1.5 秒刷屏一次，且
	// 重复内容挤占连接、把真正的新信号延迟到十秒量级。
	//
	// 用 Seq 做键是安全的：task_events 上 (task_id, seq) 有唯一索引，
	// 同一任务内 Seq 唯一标识一条事件。水位仍然保留 —— 它让查询在 SQL 侧
	// 就剪掉正 Seq 的历史，去重集合只兜非正 Seq 那部分。
	pushed := make(map[int]struct{})

	ctx := c.Request.Context()
	ticker := time.NewTicker(streamPollInterval)
	defer ticker.Stop()

	// 首次立即推送一批已有事件（不等第一个 tick），否则客户端首次连接要
	// 等一个轮询周期才看到任何数据。
	if done := s.streamPushBatch(ctx, w, flusher, taskID, &afterSeq, pushed); done {
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
			if done := s.streamPushBatch(ctx, w, flusher, taskID, &afterSeq, pushed); done {
				return
			}
		}
	}
}

// streamPushBatch 拉取 taskID 在 afterSeq 之后的全部新事件并逐条推送，
// 每条推送后 flush。若本批事件里出现终态事件，推送完成后返回 done=true，
// 调用方据此关闭连接（主动关流，不等客户端断开）。
func (s *Server) streamPushBatch(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, taskID string, afterSeq *int, pushed map[int]struct{}) bool {
	events, err := s.deps.Events.ListEvents(ctx, taskID, contract.EventFilter{AfterSeq: *afterSeq})
	if err != nil {
		return false
	}

	for _, e := range events {
		// 已推过的不再推。非正 Seq 的事件只能靠这里拦住 —— 它们抬不高水位，
		// 每轮查询都会被重新取回来（见 handler 里 pushed 的说明）。
		if _, ok := pushed[e.Seq]; ok {
			continue
		}
		pushed[e.Seq] = struct{}{}

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
