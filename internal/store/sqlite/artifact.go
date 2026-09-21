// 本文件实现 contract.ArtifactSetStore：制品集的状态更新与导入写回。
// 对应实现计划任务 9，依据 SPEC §2.1、§2.7。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

func (s *Store) CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error) {
	panic("sqlite: CreateArtifactSet 尚未实现（计划任务 9）")
}

func (s *Store) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	panic("sqlite: GetArtifactSet 尚未实现（计划任务 9）")
}

func (s *Store) ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error) {
	panic("sqlite: ListArtifactSets 尚未实现（计划任务 9）")
}

func (s *Store) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error) {
	panic("sqlite: UpdateArtifactSetState 尚未实现（计划任务 9）")
}

func (s *Store) SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	panic("sqlite: SetArtifactSetContent 尚未实现（计划任务 9）")
}

func (s *Store) DeleteArtifactSet(ctx context.Context, id string) error {
	panic("sqlite: DeleteArtifactSet 尚未实现（计划任务 9）")
}
