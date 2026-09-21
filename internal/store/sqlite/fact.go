// 本文件实现 contract.FactStore：facts 的合并写入与读取。对应实现计划
// 任务 7，依据 SPEC §5.5。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import "context"

func (s *Store) MergeFacts(ctx context.Context, taskID string, facts map[string]string) error {
	panic("sqlite: MergeFacts 尚未实现（计划任务 7）")
}

func (s *Store) GetFacts(ctx context.Context, taskID string) (map[string]string, error) {
	panic("sqlite: GetFacts 尚未实现（计划任务 7）")
}
