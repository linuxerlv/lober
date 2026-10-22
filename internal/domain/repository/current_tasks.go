package repository

import (
	"context"

	"github.com/lober/lober/internal/domain/model"
)

// CurrentTaskCreator 供向导按当前选择建任务。入参、默认字段、结果顺序和原子审计与 Tasks.CreateTasks 相同。
// 在事务保护内重查 model.CurrentApproved；选择已失效时报 ErrTemplateNotCurrent，整批任务和审计都不写。
// 保护一直持有到提交或回滚，影响当前选择的审批、废弃和内置装载必须等待。其他准入失败仍报 BatchError。
type CurrentTaskCreator interface {
	CreateCurrentTasks(context.Context, []model.Task, Audit) ([]model.Task, error)
}
