// 本文件实现 contract.TemplateStore：模板版本与 hook 引用。对应实现计划
// 任务 7，依据 SPEC §2.3、§2.6、§2.7。
package sqlite

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

const templateColumns = `id, version, name, artifact_set_id, answer_template,
	kernel_args, disk_policy, on_failure, required_form_fields_json, state`

const templateHookRefColumns = `template_id, template_version, hook_id, hook_digest, stage, ref_order, required, params_defaults_json`

type templateRowScanner interface {
	Scan(dest ...any) error
}

// scanTemplateRowNoRefs 扫描 templates 表一行，不含 HookRefs（HookRefs 由
// loadHookRefs 单独查询后拼装，见 GetTemplate）。
func scanTemplateRowNoRefs(row templateRowScanner) (model.Template, error) {
	var (
		tpl               model.Template
		requiredFieldsRaw string
	)
	err := row.Scan(
		&tpl.ID, &tpl.Version, &tpl.Name, &tpl.ArtifactSetID, &tpl.AnswerTemplate,
		&tpl.KernelArgs, &tpl.DiskPolicy, &tpl.OnFailure, &requiredFieldsRaw, &tpl.State,
	)
	if err != nil {
		return model.Template{}, err
	}
	if err := decodeJSON(requiredFieldsRaw, &tpl.RequiredFormFields); err != nil {
		return model.Template{}, fmt.Errorf("sqlite: 解码模板 %q 的 required_form_fields_json 失败: %w", tpl.ID, err)
	}
	return tpl, nil
}

// loadHookRefs 按 (template_id, template_version) 取回该模板版本的全部
// hook 引用，按 ref_order 升序排列——排序交给 SQL 里的 ORDER BY 是刻意的
// 唯一例外：model.Template.HooksForStage 的「排序收敛在一处」指的是
// 「按阶段过滤后再排」这一步，不含本层的取回顺序；这里的 ORDER BY 只是
// 让入库顺序与 ref_order 保持一致，本身不做任何按阶段的过滤或二次排序，
// 与 HooksForStage 的职责不重叠（计划任务 7「设计要点」：hook 引用的排序
// 交给 model 的方法，不在 SQL 里 ORDER BY——该设计要点约束的是「按阶段
// 过滤」这一步不要在 SQL 里做，取回全部引用时仍需要一个确定顺序才能
// 稳定往返，用 ref_order 排序不是在复制 HooksForStage 的逻辑）。
func loadHookRefs(ctx context.Context, q querier, templateID string, templateVersion int) ([]model.HookRef, error) {
	query := fmt.Sprintf(`SELECT %s FROM template_hook_refs
		WHERE template_id = ? AND template_version = ? ORDER BY ref_order ASC`, templateHookRefColumns)
	rows, err := q.QueryContext(ctx, query, templateID, templateVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []model.HookRef
	for rows.Next() {
		var (
			ref            model.HookRef
			skipTemplateID string
			skipVersion    int
			defaultsRaw    string
		)
		if err := rows.Scan(&skipTemplateID, &skipVersion, &ref.HookID, &ref.HookDigest, &ref.Stage, &ref.Order, &ref.Required, &defaultsRaw); err != nil {
			return nil, err
		}
		defaults, err := decodeJSONMapNilIfEmpty(defaultsRaw)
		if err != nil {
			return nil, fmt.Errorf("sqlite: 解码 hook 引用 params_defaults_json 失败: %w", err)
		}
		ref.ParamsDefaults = defaults
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

// CreateTemplate 创建一个新模板版本，同时写入 hook 引用列表。
func (s *Store) CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error) {
	requiredFieldsJSON, err := encodeJSON(nonNilStringSlice(tpl.RequiredFormFields))
	if err != nil {
		return model.Template{}, fmt.Errorf("sqlite: 编码 required_form_fields_json 失败: %w", err)
	}

	query := fmt.Sprintf(`INSERT INTO templates (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, templateColumns)
	if _, err := s.db.ExecContext(ctx, query,
		tpl.ID, tpl.Version, tpl.Name, tpl.ArtifactSetID, tpl.AnswerTemplate,
		tpl.KernelArgs, tpl.DiskPolicy, tpl.OnFailure, requiredFieldsJSON, tpl.State,
	); err != nil {
		return model.Template{}, translateConstraintErr(err, "CreateTemplate")
	}

	for _, ref := range tpl.HookRefs {
		defaultsJSON, err := encodeJSON(nonNilStringMap(ref.ParamsDefaults))
		if err != nil {
			return model.Template{}, fmt.Errorf("sqlite: 编码 hook 引用 params_defaults_json 失败: %w", err)
		}
		refQuery := fmt.Sprintf(`INSERT INTO template_hook_refs (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, templateHookRefColumns)
		if _, err := s.db.ExecContext(ctx, refQuery,
			tpl.ID, tpl.Version, ref.HookID, ref.HookDigest, ref.Stage, ref.Order, ref.Required, defaultsJSON,
		); err != nil {
			return model.Template{}, translateConstraintErr(err, "CreateTemplate(hook_ref)")
		}
	}

	return tpl, nil
}

// GetTemplate 按 (id, version) 查模板，version 非正数表示取该 id 下最新
// 版本（取 MAX(version) 那一行，计划任务 7「模板的关键点」）。取 pin
// 版本时把 hook 引用一起取回来——§2.6 的可复现性要求还原出的是那一版的
// 完整内容。查不到返回 contract.ErrNotFound。
func (s *Store) GetTemplate(ctx context.Context, id string, version int) (model.Template, error) {
	var (
		tpl model.Template
		err error
	)
	if version > 0 {
		query := fmt.Sprintf(`SELECT %s FROM templates WHERE id = ? AND version = ?`, templateColumns)
		tpl, err = scanTemplateRowNoRefs(s.db.QueryRowContext(ctx, query, id, version))
	} else {
		query := fmt.Sprintf(`SELECT %s FROM templates WHERE id = ? ORDER BY version DESC LIMIT 1`, templateColumns)
		tpl, err = scanTemplateRowNoRefs(s.db.QueryRowContext(ctx, query, id))
	}
	if err != nil {
		return model.Template{}, translateConstraintErr(err, "GetTemplate")
	}

	refs, err := loadHookRefs(ctx, s.db, tpl.ID, tpl.Version)
	if err != nil {
		return model.Template{}, translateConstraintErr(err, "GetTemplate(hook_refs)")
	}
	tpl.HookRefs = refs
	return tpl, nil
}

// ListTemplates 列出全部模板（不区分版本），不含 HookRefs——列表视图不需要
// 每行都拼装 hook 引用，需要详情时调用方应改用 GetTemplate。
func (s *Store) ListTemplates(ctx context.Context) ([]model.Template, error) {
	query := fmt.Sprintf(`SELECT %s FROM templates`, templateColumns)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, translateConstraintErr(err, "ListTemplates")
	}
	defer rows.Close()

	out := make([]model.Template, 0)
	for rows.Next() {
		tpl, err := scanTemplateRowNoRefs(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListTemplates")
		}
		out = append(out, tpl)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListTemplates")
	}
	return out, nil
}

// UpdateTemplateState 更新模板状态。
func (s *Store) UpdateTemplateState(ctx context.Context, id string, version int, state model.TemplateState) (model.Template, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE templates SET state = ? WHERE id = ? AND version = ?`, state, id, version)
	if err != nil {
		return model.Template{}, translateConstraintErr(err, "UpdateTemplateState")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Template{}, fmt.Errorf("sqlite: UpdateTemplateState 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		return model.Template{}, contract.ErrNotFound
	}
	return s.GetTemplate(ctx, id, version)
}

// DeleteTemplate 删除模板。若存在引用该模板的非终态 task，返回
// contract.ErrStillReferenced（§2.7）。判定看任务表而不是看模板状态
// （计划任务 7）。
func (s *Store) DeleteTemplate(ctx context.Context, id string, version int) error {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM templates WHERE id = ? AND version = ?`, id, version).Scan(&exists)
	if err != nil {
		return translateConstraintErr(err, "DeleteTemplate")
	}

	var refCount int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks
		WHERE template_id = ? AND template_version = ? AND state NOT IN (?, ?)`,
		id, version, model.TaskStateDone, model.TaskStateFailed).Scan(&refCount)
	if err != nil {
		return translateConstraintErr(err, "DeleteTemplate(check_refs)")
	}
	if refCount > 0 {
		return contract.ErrStillReferenced
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM template_hook_refs WHERE template_id = ? AND template_version = ?`, id, version); err != nil {
		return translateConstraintErr(err, "DeleteTemplate(hook_refs)")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ? AND version = ?`, id, version); err != nil {
		return translateConstraintErr(err, "DeleteTemplate")
	}
	return nil
}
