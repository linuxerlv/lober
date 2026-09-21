// 本文件实现 contract.HookStore：hook 版本与 digest 反查。对应实现计划
// 任务 8，依据 SPEC §2.2、§2.6、§2.7。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

func (s *Store) CreateHook(ctx context.Context, hook model.Hook) (model.Hook, error) {
	panic("sqlite: CreateHook 尚未实现（计划任务 8）")
}

func (s *Store) GetHook(ctx context.Context, id string, version int) (model.Hook, error) {
	panic("sqlite: GetHook 尚未实现（计划任务 8）")
}

func (s *Store) GetHookByDigest(ctx context.Context, digest string) (model.Hook, error) {
	panic("sqlite: GetHookByDigest 尚未实现（计划任务 8）")
}

func (s *Store) ListHooks(ctx context.Context) ([]model.Hook, error) {
	panic("sqlite: ListHooks 尚未实现（计划任务 8）")
}

func (s *Store) UpdateHookState(ctx context.Context, id string, version int, state model.HookState, approver string, approvedAt int64) (model.Hook, error) {
	panic("sqlite: UpdateHookState 尚未实现（计划任务 8）")
}
