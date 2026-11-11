package execution

import (
	"github.com/lober/lober/internal/application/execution/port"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

// NewIPObserver 属于执行模块，只供可信组装点连接非nil的临时地址登记能力。
// 构造无副作用、不失败；返回回调接收已验证任务和原连接地址，不规范化或新增大小限制。
// 无执行信息时不登记；否则原样传任务编号、尝试号和地址，普通到期值为IssuedAt+InstallLife，
// 已到重启阶段为IssuedAt+86400，保留原整数相加行为，不修正溢出或负值。
// 回调无返回值或错误，无列表、结果顺序或原因代码；实现可拒绝登记。
// 只调用临时地址登记，不写任务或事件，无事务、版本冲突、提交复查或新重试。
// 重复调用按sink覆盖规则处理，并发安全由sink负责；调用期间不得修改任务。
// 无审计或通知；不接收上下文、不新增取消或超时，登记同步返回。
func NewIPObserver(sink port.IPObservationSink) func(model.Task, string) {
	return func(task model.Task, address string) {
		if task.Execution == nil {
			return
		}
		life := int64(executionrules.InstallLife)
		if task.Execution.ReachedReboot {
			life = 86400
		}
		sink.ObserveIP(task.ID, task.Execution.Attempt, address, task.Execution.IssuedAt+life)
	}
}
