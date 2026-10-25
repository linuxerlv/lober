package repository

import "context"

// Events 是任务的追加式日志，不审计。追加事件不算改任务：任务的 State 与 Rev 都不变，调用方手里的 TaskExpect
// 照样有效。已结束的任务照样可以追加，迟到的信号只记为事件。
type Events interface {
	// AppendEvents 给任务追加事件，Seq 由仓储按任务连续分配。Key 区分大小写；Key 已存在的那一条跳过，报告为
	// Duplicate，不是错误：第 7 层收到重发的信号时据此去重。任务不存在报 ErrNotFound；事件不合法（CheckEvents）报
	// ErrInvalid，整批不写。
	// AppendEvents 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；本方法的事件差异仍以上方逐方法说明为准。
	// 先CheckID再CheckEvents，空批仍要求任务存在；任务行锁内分配序号与去重，结果逐项对应输入。
	// 事件及LastSeq一起提交，不改任务Rev；无Key事件不去重，重复空批成功无写入。
	AppendEvents(ctx context.Context, taskID string, es []NewEvent) ([]Appended, error)
	// ListEvents 返回 Seq 大于 afterSeq 的事件，按 Seq 升序；limit 的规则同分页（CheckQuery），afterSeq 为负报
	// ErrInvalid（CheckEventQuery）。任务不存在报 ErrNotFound，同 AppendEvents。
	// ListEvents 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；只读，不写审计或通知，无提交复查。
	// 先CheckEventQuery，再查事件；无返回事件时另查任务存在性，缺失ErrNotFound。
	// 结果只读且与其他查询不共用快照，无提交复查；取消仍沿用doc.go。
	ListEvents(ctx context.Context, taskID string, afterSeq int64, limit int) ([]Event, error)
}
