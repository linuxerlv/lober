package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// TaskGetter 按原编号读取冻结任务；编号校验和不存在错误由仓储保持。
type TaskGetter interface {
	GetTask(context.Context, string) (model.Task, error)
}

// TaskReader 只读取任务，保留仓储的分页、排序和包含端点的时间过滤。
type TaskReader interface {
	TaskGetter
	ListTasks(context.Context, repository.TaskQuery) (repository.Page[model.Task], error)
}

// EventReader 沿用原查询校验、升序和空结果，不改仓储错误。
type EventReader interface {
	ListEvents(context.Context, string, int64, int) ([]repository.Event, error)
}
