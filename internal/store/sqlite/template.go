// 本文件实现 contract.TemplateStore：模板版本与 hook 引用。对应实现计划
// 任务 7，依据 SPEC §2.3、§2.6、§2.7。
//
// 骨架阶段占位，见 task.go 顶部注释。
package sqlite

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

func (s *Store) CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error) {
	panic("sqlite: CreateTemplate 尚未实现（计划任务 7）")
}

func (s *Store) GetTemplate(ctx context.Context, id string, version int) (model.Template, error) {
	panic("sqlite: GetTemplate 尚未实现（计划任务 7）")
}

func (s *Store) ListTemplates(ctx context.Context) ([]model.Template, error) {
	panic("sqlite: ListTemplates 尚未实现（计划任务 7）")
}

func (s *Store) UpdateTemplateState(ctx context.Context, id string, version int, state model.TemplateState) (model.Template, error) {
	panic("sqlite: UpdateTemplateState 尚未实现（计划任务 7）")
}

func (s *Store) DeleteTemplate(ctx context.Context, id string, version int) error {
	panic("sqlite: DeleteTemplate 尚未实现（计划任务 7）")
}
