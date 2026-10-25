package port

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type InstallerLogRepository interface {
	// GetTask 的八项约定唯一委托 repository.Tasks.GetTask 及仓储doc.go；本口只缩小方法集。
	GetTask(context.Context, string) (model.Task, error)
	// AppendEvents 的八项约定唯一委托 repository.Events.AppendEvents 及仓储doc.go；不额外检查Attempt。
	AppendEvents(context.Context, string, []repository.NewEvent) ([]repository.Appended, error)
}
