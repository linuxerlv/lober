package repository

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
)

// CurrentTaskCreator 供向导按当前选择建任务。入参、默认字段、结果顺序和原子审计与 Tasks.CreateTasks 相同。
// 在事务保护内重查 model.CurrentApproved；选择已失效时报 ErrTemplateNotCurrent，整批任务和审计都不写。
// 保护一直持有到提交或回滚，影响当前选择的审批、废弃和内置装载必须等待。其他准入失败仍报 BatchError。
type CurrentTaskCreator interface {
	// CreateCurrentTasks 共用doc.go的可信调用、空值、错误、重复、原子保存、并发与取消边界；方法差异以本接口上方当前模板保护说明为准。
	CreateCurrentTasks(context.Context, []model.Task, Audit) ([]model.Task, error)
}
