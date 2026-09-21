// 本文件实现内置 Notify：记结构化日志，是 MVP 的告警出口。对应实现计划
// 任务 5，依据 SPEC §7.1、§7.4、§7.5。
package provider

import (
	"context"
	"log"

	"github.com/lober/lober/internal/core/contract"
)

// builtinNotify 是 NotifyProviderV1 的内置实现：记结构化日志。这是 MVP
// 的告警出口——日志里含任务 id、状态、失败原因，便于运维用 journalctl
// 过滤（任务 5 明确要求）。
type builtinNotify struct{}

// NewBuiltinNotify 构造一个内置 Notify。
func NewBuiltinNotify() contract.NotifyProviderV1 {
	return &builtinNotify{}
}

// Emit 记一行结构化日志。字段顺序固定（event、task_id、detail 逐个展开），
// 便于运维写 journalctl 的 grep 表达式时有稳定的字段位置可依赖。
func (b *builtinNotify) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	log.Printf("provider/notify: event=%s task_id=%s detail=%v", ev.Event, ev.TaskID, ev.Detail)
	return nil
}

// Ping 直接返回成功。
func (b *builtinNotify) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：builtinNotify 必须满足 contract.NotifyProviderV1。
var _ contract.NotifyProviderV1 = (*builtinNotify)(nil)
