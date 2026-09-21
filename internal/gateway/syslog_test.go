// 本文件测试 syslog UDP 接收器。对应实现计划任务 8，依据
// docs/contracts/http-machine.md UDP :514 一节、SPEC §6.5。
//
// 测试直接调用处理单条 UDP 包的纯函数（不依赖真实的 net.ListenUDP），
// 与 answer_test.go/signal_test.go 等使用 httptest 的风格一致——syslog
// 本身没有 HTTP 请求/响应，因此这里绕开真实网络 I/O，只测「收到一条
// (源 IP, 原始字节) 之后网关做了什么」。
//
// 归档依据（源 IP → 任务）不是 Store 契约能表达的查询——model.Task 没有
// 「当前客户端 IP」字段，TaskStore 也没有按 IP 反查任务的方法（这是本
// 模块识别到的第三个契约缺口，记入 contractGaps）。本实现的绕法：网关
// 自己在 Server 内维护一份进程内的「客户端 IP → 任务 id」映射，在
// /v1/boot 命中授权、/v1/answer、/v1/signal 成功处理时更新这份映射
// （用请求的 clientIP 函数取值），syslog 接收器查这份映射决定归档目标。
// 进程重启后映射为空，此时全部 syslog 行会落公共文件——这与「找不到
// 任务归属就不算错误」的方向一致，不是缺陷。
package gateway

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// TestSyslog_MatchingIP_ArchivedToTaskLog 锁住：syslog 收到的行被归档到
// 匹配 IP 的任务。
func TestSyslog_MatchingIP_ArchivedToTaskLog(t *testing.T) {
	deps := newTestDeps()
	logStub := newLogStoreStub()
	deps.LogStore = logStub
	srv := NewServer(deps)

	srv.rememberClientTask("10.0.5.7", "task-syslog-1", model.PhasePackages)
	srv.handleSyslogLine("10.0.5.7", []byte("anaconda: repo 404 while fetching packages"))

	if len(logStub.appendLogCalls) != 1 {
		t.Fatalf("AppendLog 被调用 %d 次, want 1", len(logStub.appendLogCalls))
	}
	call := logStub.appendLogCalls[0]
	if call.taskID != "task-syslog-1" {
		t.Errorf("AppendLog 传入的 taskID = %q, want %q", call.taskID, "task-syslog-1")
	}
	if !strings.Contains(call.content, "repo 404") {
		t.Errorf("AppendLog 传入的 content = %q, 缺少原始日志内容", call.content)
	}
}

// TestSyslog_MatchingIP_AppendsTaskEvent 锁住契约文档 UDP 一节：解析失败
// （或本实现干脆不做结构化解析）的行仍整行落 TaskEvent（Source =
// EventSourceSyslog）。
func TestSyslog_MatchingIP_AppendsTaskEvent(t *testing.T) {
	deps := newTestDeps()
	store := fake.NewStore()
	deps.TaskStore = store
	deps.EventStore = store
	srv := NewServer(deps)

	srv.rememberClientTask("10.0.5.7", "task-syslog-2", model.PhasePackages)
	srv.handleSyslogLine("10.0.5.7", []byte("some raw syslog line"))

	events, err := store.ListEvents(t.Context(), "task-syslog-2", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Source == model.EventSourceSyslog && strings.Contains(e.Message, "some raw syslog line") {
			found = true
		}
	}
	if !found {
		t.Errorf("未找到 Source=EventSourceSyslog 且包含原始内容的事件，events=%+v", events)
	}
}

// TestSyslog_NoMatch_ArchivedToCommonFile 锁住：匹配不到的行落公共文件
// 而不是被丢弃——那些行可能来自一台还没建任务就在报错的机器，是有用的
// 取证材料。
func TestSyslog_NoMatch_ArchivedToCommonFile(t *testing.T) {
	deps := newTestDeps()
	logStub := newLogStoreStub()
	deps.LogStore = logStub
	srv := NewServer(deps)

	srv.handleSyslogLine("192.0.2.99", []byte("no known task for this IP"))

	if len(logStub.appendLogCalls) != 1 {
		t.Fatalf("AppendLog 被调用 %d 次, want 1", len(logStub.appendLogCalls))
	}
	call := logStub.appendLogCalls[0]
	if call.taskID != syslogUnmatchedTaskID {
		t.Errorf("AppendLog 传入的 taskID = %q, want %q（公共文件的占位任务 id）",
			call.taskID, syslogUnmatchedTaskID)
	}
	if !strings.Contains(call.content, "no known task for this IP") {
		t.Errorf("AppendLog 传入的 content 缺少原始内容，got %q", call.content)
	}
}
