package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
)

var ErrControlUnavailable = errors.New("task control is not implemented")

// Controller 允许具有task:cancel或task:retry权限的可信操作者控制任务。
// 编号原样交仓储处理，不新增格式、长度或必填检查。身份错误先于查任务，查任务先于不可用。
// 成功返回重读的任务，先取得取消诊断再取得重试诊断；网页将空诊断转换为数组。
// 后端错误原样返回，包括不存在、版本冲突、业务问题和审计失败，不自动重试。
// 后端按原版本在锁内复查最新事实，将状态、执行记录、事件和审计一起保存；应用不直接保存。
// 重复调用仍由后端按当前状态和版本决定，不保证重复成功，也不替换成更新版本。
// 审计只用可信操作者、固定动作、空说明和一次注入时钟。后端成功后重读，再补发提示。
// 已提交但重读失败时返回原错误，不补发，不声称回滚。context原样传递，不新增超时或提交后检查。
type Controller interface {
	// Cancel 先检查task:cancel，审计动作固定为task.cancel；成功后返回重读任务及诊断。
	Cancel(context.Context, ports.Actor, string) (TaskChoice, error)
	// Retry 先检查task:retry，审计动作固定为task.retry；保留原版本交后端复查。
	Retry(context.Context, ports.Actor, string) (TaskChoice, error)
}
type ControllerDeps struct {
	Tasks    port.TaskGetter
	Backend  port.Control
	Clock    ports.Clock
	Notifier ports.Notifier
}
