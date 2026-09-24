// 本文件实现 contract.TemplateStore：模板版本与 hook 引用。对应实现计划
// 任务 9 的一半（template 聚合），依据 SPEC §2.3、§2.6、§2.7。
package template

import (
	"context"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// Store 实现 contract.TemplateStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// CreateTemplate 创建一个新模板版本，把模板自身与它的 hook_refs 一起写
// 入，必须在同一个事务里——否则可能出现有模板没引用列表的中间态（任务
// 简报明确要求）。
func (s *Store) CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := fromModel(tpl)
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for _, ref := range tpl.HookRefs {
			refRow := hookRefFromModel(tpl.ID, tpl.Version, ref)
			if err := tx.Create(&refRow).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.Template{}, pgerr.Translate(err, "CreateTemplate")
	}
	return tpl, nil
}

// GetTemplate 按 (id, version) 查模板，version 非正数（0 或负数）表示取
// 该 id 下最新版本——这是「取 pin 住的那一版」与「取最新版」的统一入口
// （任务简报明确要求两者都要走取最新，不能只实现 0）。把 hook_refs 一起
// 读出来装回 model.Template，按 ref_order 排序。查不到返回
// contract.ErrNotFound。
func (s *Store) GetTemplate(ctx context.Context, id string, version int) (model.Template, error) {
	var row templateRow
	query := s.db.WithContext(ctx).Where("id = ?", id)
	if version > 0 {
		query = query.Where("version = ?", version)
	} else {
		query = query.Order("version DESC")
	}
	if err := query.First(&row).Error; err != nil {
		return model.Template{}, pgerr.Translate(err, "GetTemplate")
	}

	refs, err := s.loadHookRefs(ctx, row.ID, row.Version)
	if err != nil {
		return model.Template{}, pgerr.Translate(err, "GetTemplate(hook_refs)")
	}

	tpl := row.toModel()
	tpl.HookRefs = refs
	return tpl, nil
}

// loadHookRefs 按 (template_id, template_version) 取回该模板版本的全部
// hook 引用，按 ref_order 升序排列——ref_order 保证引用顺序，读回时必须
// 按它排序（任务简报明确要求）。用 Go 侧排序而不是 SQL ORDER BY 也可以，
// 这里用数据库的 Order 子句表达同一语义，两者等价，选数据库排序是因为
// 结果集本就来自这条查询，不需要额外的 sort 步骤。
func (s *Store) loadHookRefs(ctx context.Context, templateID string, templateVersion int) ([]model.HookRef, error) {
	var rows []templateHookRefRow
	err := s.db.WithContext(ctx).
		Where("template_id = ? AND template_version = ?", templateID, templateVersion).
		Order("ref_order ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	refs := make([]model.HookRef, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, r.toModel())
	}
	return refs, nil
}

// ListTemplates 列出全部模板（不区分版本），不含 HookRefs——列表视图不
// 需要每行都拼装 hook 引用，需要详情时调用方应改用 GetTemplate（与旧
// sqlite 实现同一语义）。
func (s *Store) ListTemplates(ctx context.Context) ([]model.Template, error) {
	var rows []templateRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListTemplates")
	}

	out := make([]model.Template, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toModel())
	}
	return out, nil
}

// UpdateTemplateState 更新模板状态。命中 0 行时返回
// contract.ErrNotFound（与其它聚合包的 CAS 类方法同一约定：受影响行数
// 是判定「存不存在」的直接依据，不需要先查一次再更新）。
func (s *Store) UpdateTemplateState(ctx context.Context, id string, version int, state model.TemplateState) (model.Template, error) {
	result := s.db.WithContext(ctx).Model(&templateRow{}).
		Where("id = ? AND version = ?", id, version).
		Update("state", string(state))
	if result.Error != nil {
		return model.Template{}, pgerr.Translate(result.Error, "UpdateTemplateState")
	}
	if result.RowsAffected == 0 {
		return model.Template{}, contract.ErrNotFound
	}
	return s.GetTemplate(ctx, id, version)
}

// DeleteTemplate 删除模板。存在引用该模板的非终态 task 时返回
// contract.ErrStillReferenced；终态（done/failed）task 引用的模板可以删
// （SPEC §2.7）。
//
// 判定要查 tasks 表的 (template_id, template_version) + state，但
// tasks 表的行结构体归 task 包所有（另一个 agent 正在并行写它）——不
// import task 包，改用 GORM 的 Table("tasks") 直接查 count，见
// checkStillReferenced（任务简报「注意」一节明确要求，deviations 里已
// 说明这一做法）。
func (s *Store) DeleteTemplate(ctx context.Context, id string, version int) error {
	referenced, err := s.checkStillReferenced(ctx, id, version)
	if err != nil {
		return pgerr.Translate(err, "DeleteTemplate(check_refs)")
	}
	if referenced {
		return contract.ErrStillReferenced
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("template_id = ? AND template_version = ?", id, version).
			Delete(&templateHookRefRow{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND version = ?", id, version).Delete(&templateRow{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return pgerr.Translate(err, "DeleteTemplate")
	}
	return nil
}

// checkStillReferenced 查 tasks 表里是否存在引用该 (template_id,
// template_version) 且状态非终态的行。下方 NOT IN ('done', 'failed') 与
// model.TaskState.IsTerminal() 的判据必须保持一致（该方法只判定终态是
// done 与 failed 两个值）——这是一个纯聚合查询，不像其它路径那样已经把
// 整表取到 Go 侧再过滤，此处保留字面量集合，用本注释显式标记同步关系
// （与旧实现 sqlite/template.go 的同款注释同一理由）。
func (s *Store) checkStillReferenced(ctx context.Context, templateID string, templateVersion int) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Table("tasks").
		Where("template_id = ? AND template_version = ? AND state NOT IN (?, ?)",
			templateID, templateVersion, string(model.TaskStateDone), string(model.TaskStateFailed)).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
