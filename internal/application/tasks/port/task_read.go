package port

// 本文件的仓储口只供可信应用或启动组装调用；仓储不认证网页用户或机器令牌。
// 身份、权限或凭据检查的有无及先后，由各消费入口的操作约定负责。
// 输入规范化由调用应用负责；排序、空值、错误优先级、重复/版本冲突、锁内复查、原子审计及取消均沿用所指保存契约。
// 只读方法不写业务或审计、不发通知，故无原子写与提交复查；重复读取不保证多次或多页快照。

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// TaskGetter 按原编号读取冻结任务；编号校验和不存在错误由仓储保持。
type TaskGetter interface {
	// GetTask 的八项保存约定唯一委托 repository.Tasks.GetTask
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	GetTask(context.Context, string) (model.Task, error)
}

// TaskReader 只读取任务，保留仓储的分页、排序和包含端点的时间过滤。
type TaskReader interface {
	TaskGetter
	// ListTasks 的八项保存约定唯一委托 repository.Tasks.ListTasks
	// （internal/domain/repository/tasks.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListTasks(context.Context, repository.TaskQuery) (repository.Page[model.Task], error)
}

// EventReader 沿用原查询校验、升序和空结果，不改仓储错误。
type EventReader interface {
	// ListEvents 的八项保存约定唯一委托 repository.Events.ListEvents
	// （internal/domain/repository/events.go 方法说明及 doc.go 共同约定）；本口只缩小方法集，不另加权限、重试或事务。
	ListEvents(context.Context, string, int64, int) ([]repository.Event, error)
}
