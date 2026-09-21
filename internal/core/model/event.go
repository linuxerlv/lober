// 本文件定义任务事件类型：TaskEvent、EventSource、EventLevel。对应实现
// 计划任务 7，依据 SPEC §2.5、§3.5。
//
// FailureDetail（§3.5 的六个诊断字段）定义在 task.go 中——它是 Task 的
// 一个字段，与任务状态机紧密相关，归入任务 2 的产出范围。
package model

// EventSource 标识事件的来源渠道（§2.5）。
type EventSource string

const (
	EventSourceSignal    EventSource = "signal"
	EventSourceSyslog    EventSource = "syslog"
	EventSourceNginx     EventSource = "nginx"
	EventSourceHeartbeat EventSource = "heartbeat"
	EventSourceProvider  EventSource = "provider"
)

// EventLevel 是事件的级别。
type EventLevel string

const (
	EventLevelInfo  EventLevel = "info"
	EventLevelWarn  EventLevel = "warn"
	EventLevelError EventLevel = "error"
)

// TaskEvent 是任务事件表的一行，append-only（§2.5）。
//
// 日志正文不入库——按 task_id/phase 落文件，本结构只存路径与尾部片段
// （Message 承载尾部片段，完整日志由 LogStore 按路径读取）。
type TaskEvent struct {
	TaskID string
	Seq    int // (TaskID, Seq) 唯一索引用于信号去重；非正值表示非信号来源
	TS     int64
	Phase  Phase
	Level  EventLevel

	Message    string // 简述或日志尾部片段
	RC         int
	HookDigest string // 若与某个 hook 相关

	Source EventSource
}
