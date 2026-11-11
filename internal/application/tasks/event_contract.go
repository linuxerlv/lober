package tasks

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
)

// Event 是已提交事件的普通数据；Fields 与仓储和其他返回值互不共用。
type Event struct {
	TaskID    string
	Seq, At   int64
	Kind, Key string
	Fields    map[string]string
}

// EventReader 供可信调用者读取任务事件，先检查 task:read。不写业务、审计或通知。
// 重复读取允许看到新提交事件，不保证多页快照；没有写事务、版本冲突或提交复查。
// 原 context 交给仓储，取消和超时由原仓储处理；应用不启动计时器或后台工作。
type EventReader interface {
	// List 直接调用原 ListEvents，不额外查任务。编号、非负序号、上限500及默认值沿用仓储检查。
	// 权限错误优先，随后返回原查询错误；成功按序号升序，空结果是非空数组。
	List(context.Context, ports.Actor, string, int64, int) ([]Event, error)
	// Open 依次检查权限、负序号、查任务、订阅、首读200条。首读失败先退订再返回原错误。
	// 编号原样交仓储，订阅只提供合并提示，不保证每次发布均有独立提示。
	Open(context.Context, ports.Actor, string, int64) (EventSubscription, error)
}

// EventSubscription 只读已提交事件。调用者负责每页身份复查、网络发送和最终 Close。
// 不因任务结束自动关闭，不新增审计、通知或后台工作。
// 四方法共同遵守 docs/contracts/task-read-control-events.md「事件流」；只读已提交事实，无原子写或提交复查，
// 无版本冲突重试。除Read传入上下文外不接收取消/超时，重复与并发Close按下方方法说明。
type EventSubscription interface {
	// Initial 返回首批独立副本，空结果是非空数组。
	Initial() []Event
	// Hints 返回原通知通道；关闭表示调用者应结束读取。
	Hints() <-chan struct{}
	// Read 固定每页200，原样转交 context 和序号，保留原错误及升序结果。
	Read(context.Context, int64) ([]Event, error)
	// Close 释放订阅，可顺序或并发重复调用，不重复退订。
	Close()
}
type EventDeps struct {
	Tasks    port.TaskGetter
	Events   port.EventReader
	Notifier ports.Notifier
}
