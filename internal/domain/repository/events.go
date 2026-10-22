package repository

import "context"

// Events 是任务的追加式日志，不审计。追加事件不算改任务：任务的 State 与 Rev 都不变，调用方手里的 TaskExpect
// 照样有效。已结束的任务照样可以追加，迟到的信号只记为事件。
type Events interface {
	// AppendEvents 给任务追加事件，Seq 由仓储按任务连续分配。Key 区分大小写；Key 已存在的那一条跳过，报告为
	// Duplicate，不是错误：第 7 层收到重发的信号时据此去重。任务不存在报 ErrNotFound；事件不合法（CheckEvents）报
	// ErrInvalid，整批不写。
	AppendEvents(ctx context.Context, taskID string, es []NewEvent) ([]Appended, error)
	// ListEvents 返回 Seq 大于 afterSeq 的事件，按 Seq 升序；limit 的规则同分页（CheckQuery），afterSeq 为负报
	// ErrInvalid（CheckEventQuery）。任务不存在报 ErrNotFound，同 AppendEvents。
	ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]Event, error)
}
