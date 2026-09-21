// 本文件实现 contract.TaskStore：任务的建、取、列、身份查找、CAS 状态
// 转换、运行时字段更新、超时扫描、token 查询。对应实现计划任务 3、4、5，
// 依据 SPEC §2.4、§3.3、§3.4、§3.6、§4.2、§4.3、§4.4、§4.7、§6.4。
//
// 当前为骨架阶段（计划任务 1）：方法签名齐全以满足编译期接口断言，方法体
// panic——这是刻意的：conformance 套件此刻应该「全红」，红的形式是 panic
// 而不是编译错误，后续任务逐个替换成真实实现。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

func (s *Store) CreateTask(ctx context.Context, task model.Task) (model.Task, error) {
	panic("sqlite: CreateTask 尚未实现（计划任务 3）")
}

func (s *Store) GetTask(ctx context.Context, taskID string) (model.Task, error) {
	panic("sqlite: GetTask 尚未实现（计划任务 3）")
}

func (s *Store) FindPendingTaskByIdentity(ctx context.Context, sn, mac, uuid string) (model.Task, error) {
	panic("sqlite: FindPendingTaskByIdentity 尚未实现（计划任务 3）")
}

func (s *Store) ListTasks(ctx context.Context, filter contract.TaskFilter) ([]model.Task, error) {
	panic("sqlite: ListTasks 尚未实现（计划任务 3）")
}

func (s *Store) CASTaskState(ctx context.Context, taskID string, expectedState model.TaskState, expectedVersion int, newState model.TaskState) (model.Task, error) {
	panic("sqlite: CASTaskState 尚未实现（计划任务 4）")
}

func (s *Store) MutateTaskFields(ctx context.Context, taskID string, mutation model.TaskMutation) (model.Task, error) {
	panic("sqlite: MutateTaskFields 尚未实现（计划任务 4）")
}

func (s *Store) ListTimedOutTasks(ctx context.Context, asOf int64) ([]model.Task, error) {
	panic("sqlite: ListTimedOutTasks 尚未实现（计划任务 5）")
}

func (s *Store) GetTaskByToken(ctx context.Context, token string, purpose model.TokenPurpose) (model.Task, error) {
	panic("sqlite: GetTaskByToken 尚未实现（计划任务 5）")
}
