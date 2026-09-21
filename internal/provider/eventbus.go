// 本文件实现 contract.EventBus：生命周期通知总线。对应实现计划任务 7，
// 依据 SPEC §7.5。
package provider

import (
	"log"
	"sync"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// eventBus 是 EventBus 的内置实现。
//
// 同步调用还是异步？选同步（任务 7 明确要求写进注释）：异步的话「任务
// 已 ready」的通知可能在进程退出时丢失；同步会让状态机等待订阅者，但
// 订阅者都是记日志级别的操作（DNS 注册、发通知），耗时可忽略。将来接入
// 真实的告警推送（如需要网络往返的 webhook）时，若延迟变得不可忽略，
// 需要改成异步加队列——这个取舍点要在这里留痕，免得日后有人在不知道
// 「当初为什么选同步」的情况下重新做一遍这个决策。
type eventBus struct {
	mu   sync.RWMutex
	subs map[contract.LifecycleEvent][]func(model.Task)
}

// NewEventBus 构造一个空的事件总线。
func NewEventBus() contract.EventBus {
	return &eventBus{subs: make(map[contract.LifecycleEvent][]func(model.Task))}
}

// Publish 发布一个生命周期事件给该事件的全部订阅者，同步调用。
//
// 订阅者的 panic 不能带走发布者——每个回调用 recover 包住并记日志
// （任务 7）：一个写 DNS 的回调 panic 不该让状态机崩溃。这是本类型存在
// 的核心职责，比「发布」这个动作本身更重要。
func (b *eventBus) Publish(event contract.LifecycleEvent, task model.Task) {
	b.mu.RLock()
	// 复制订阅者切片后再释放锁：回调可能耗时（哪怕设计上应该很快），
	// 持锁调用回调会阻塞其它 goroutine 的 Subscribe/Publish，且如果某个
	// 回调本身又调用了 Subscribe（不常见但不该被这里的锁禁止），持锁调用
	// 会直接死锁。
	callbacks := make([]func(model.Task), len(b.subs[event]))
	copy(callbacks, b.subs[event])
	b.mu.RUnlock()

	for _, cb := range callbacks {
		b.invokeSafely(event, cb, task)
	}
}

// invokeSafely 用 recover 包住单次回调调用，panic 时记日志而不向上传播。
func (b *eventBus) invokeSafely(event contract.LifecycleEvent, cb func(model.Task), task model.Task) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("provider/eventbus: 订阅者处理事件 %s（task_id=%s）时 panic: %v——"+
				"已被拦截，不影响发布者与其它订阅者（SPEC §7.5：fire-and-forget 语义）",
				event, task.ID, r)
		}
	}()
	cb(task)
}

// Subscribe 注册一个回调，收到 Publish 的事件时被同步调用。
func (b *eventBus) Subscribe(event contract.LifecycleEvent, callback func(model.Task)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[event] = append(b.subs[event], callback)
}

// 编译期断言：eventBus 必须满足 contract.EventBus。
var _ contract.EventBus = (*eventBus)(nil)
