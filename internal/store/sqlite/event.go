// 本文件实现 contract.EventStore：事件追加与去重、按序号查询。对应实现
// 计划任务 6，依据 SPEC §2.5、§3.6、§12.8。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func (s *Store) AppendEvent(ctx context.Context, event model.TaskEvent) (model.TaskEvent, error) {
	panic("sqlite: AppendEvent 尚未实现（计划任务 6）")
}

func (s *Store) ListEvents(ctx context.Context, taskID string, filter contract.EventFilter) ([]model.TaskEvent, error) {
	panic("sqlite: ListEvents 尚未实现（计划任务 6）")
}
