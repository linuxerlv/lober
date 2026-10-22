// Package ports 定义多个应用流程共用的外部能力。
package ports

// Notifier 只提示某任务可能有新事件，多次提示可以合并。
// 消费者收到提示后重新读取已提交事件；不能把提示当作完整事件记录。
// Publish 必须在业务事务成功提交后调用；失败或取消时不发布。
// Subscribe 返回容量为一的提示通道与可重复调用的退订函数。
// 退订会关闭通道，并且可以和发布并发调用。
type Notifier interface {
	Publish(string)
	Subscribe(string) (<-chan struct{}, func())
}
