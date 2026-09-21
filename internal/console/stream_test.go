// 本文件测试 SSE 事件流端点。对应实现计划任务 7「测试要锁住的行为」。
//
// 全部测试都设超时保护（context.WithTimeout 或 httptest 的
// ResponseRecorder 写入配合手动取消），避免某个断言写错导致测试挂起
// 拖慢整条 CI（计划任务 7 明确要求）。
package console

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/model"
)

// flushRecorder 包装 httptest.ResponseRecorder，额外统计 Flush 调用
// 次数，供「每条事件后有 flush」的断言使用。
type flushRecorder struct {
	*httptest.ResponseRecorder
	mu         sync.Mutex
	flushCount int
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	f.flushCount++
	f.mu.Unlock()
}

func (f *flushRecorder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushCount
}

// TestStream_ResponseHeaders 锁住「响应头三项正确」。
func TestStream_ResponseHeaders(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-1", model.TaskStateDone)
	tok := ts.login("bob-operator")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-1/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	ts.Server.ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := rr.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
}

// seedStreamTask 准备一条任务与两条事件，state 由调用方指定（终态用于
// 验证主动关流，非终态用于验证上下文取消退出）。
func seedStreamTask(t *testing.T, ts *testServer, taskID string, state model.TaskState) {
	t.Helper()
	ctx := t.Context()
	if _, err := ts.Store.CreateTask(ctx, model.Task{
		ID: taskID, State: state, Version: 1, CurrentPhase: model.PhaseDone,
	}); err != nil {
		t.Fatalf("准备任务失败: %v", err)
	}
	for seq := 10; seq <= 20; seq += 10 {
		if _, err := ts.Store.AppendEvent(ctx, model.TaskEvent{
			TaskID: taskID, Seq: seq, Phase: model.PhaseDone, Level: model.EventLevelInfo, Message: "x",
		}); err != nil {
			t.Fatalf("准备事件失败: %v", err)
		}
	}
}

// TestStream_ExistingEventsPushedImmediately 锁住「已有事件被立即推送，
// 格式含 id 行与数据行」。
func TestStream_ExistingEventsPushedImmediately(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-2", model.TaskStateDone)
	tok := ts.login("bob-operator")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-2/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	ts.Server.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "id: 10\n") {
		t.Errorf("响应体应含 'id: 10' 行，实际:\n%s", body)
	}
	if !strings.Contains(body, "event: task-event\n") {
		t.Errorf("响应体应含 'event: task-event' 行，实际:\n%s", body)
	}
	if !strings.Contains(body, `"seq":10`) {
		t.Errorf("响应体应含序号为 10 的 data 行，实际:\n%s", body)
	}
}

// TestStream_ResumeWithLastEventID 锁住「带续传 id 的请求只收到该序号
// 之后的事件」。
func TestStream_ResumeWithLastEventID(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-3", model.TaskStateDone)
	tok := ts.login("bob-operator")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-3/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Last-Event-ID", "10")
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	ts.Server.ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "id: 10\n") {
		t.Errorf("带 Last-Event-ID=10 续传时不应再收到序号 10 本身，实际:\n%s", body)
	}
	if !strings.Contains(body, "id: 20\n") {
		t.Errorf("应收到序号 20（在 10 之后），实际:\n%s", body)
	}
}

// TestStream_TerminalStateClosesStream 锁住「任务终态时收到终态事件且流
// 被关闭」。终态事件本身就是 seedStreamTask 写入的最后一条（seq=20），
// 断言点是「handler 在推送完后正常返回（不需要等到 ctx 超时）」。
func TestStream_TerminalStateClosesStream(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-4", model.TaskStateFailed)
	tok := ts.login("bob-operator")

	// 用一个明显比轮询间隔更长的超时：如果 handler 没有在检测到终态后
	// 主动返回，会一直阻塞到这个超时，测试失败信息会指向「没有主动关
	// 流」而不是模糊的挂起。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-4/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	go func() {
		ts.Server.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-done:
		// handler 自行返回——终态检测到后主动关流，符合预期。
	case <-time.After(2500 * time.Millisecond):
		t.Fatalf("任务已是终态，handler 应在推送完终态事件后主动返回，但等待超时（可能没有做主动关流）")
	}

	if !strings.Contains(rr.Body.String(), "id: 20\n") {
		t.Errorf("应收到终态事件（seq=20），实际:\n%s", rr.Body.String())
	}
}

// TestStream_ContextCancellationExitsHandler 锁住「请求上下文取消后
// handler 返回（用超时验证，不返回就是泄漏）」。用一个非终态任务（没有
// 终态事件驱动关流），验证唯一让 handler 退出的手段是上下文取消。
func TestStream_ContextCancellationExitsHandler(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-5", model.TaskStatePending)
	tok := ts.login("bob-operator")

	ctx, cancel := context.WithCancel(context.Background())
	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-5/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		ts.Server.ServeHTTP(rr, req)
		close(done)
	}()

	// 给 handler 一点时间完成首批推送，然后取消上下文模拟客户端断开。
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// handler 检测到 ctx.Done() 后退出，符合预期——没有这一行为就是
		// 连接泄漏。
	case <-time.After(3 * time.Second):
		t.Fatalf("请求上下文取消后 handler 应尽快返回，但等待超时——可能存在 goroutine/连接泄漏")
	}
}

// TestStream_FlushCalledPerEvent 锁住「每条事件后有 flush（用一个记录
// flush 次数的响应记录器验证）」。
func TestStream_FlushCalledPerEvent(t *testing.T) {
	ts := newTestServer(t)
	seedStreamTask(t, ts, "task-6", model.TaskStateDone)
	tok := ts.login("bob-operator")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := newJSONRequest(t, "GET", "/console/v1/tasks/task-6/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	ts.Server.ServeHTTP(rr, req)

	// 至少：写头后一次 flush + 两条事件各一次 flush = 3 次。
	if rr.count() < 3 {
		t.Errorf("flush 次数 = %d, want >= 3（写头 1 次 + 两条事件各 1 次）", rr.count())
	}
}
