package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/selection"
	"gorm.io/gorm"
)

const templateCandidateBatchSize = 256

// ListTemplateCandidates 见repository.Templates.ListTemplateCandidates；本次所有批共用私有只读事务。
func (s *Store) ListTemplateCandidates(ctx context.Context, q repository.TemplateQuery) (repository.Page[repository.TemplateCandidateFacts], error) {
	if q.Selection == nil {
		return repository.Page[repository.TemplateCandidateFacts]{}, repository.ErrInvalid
	}
	// 先只构造查询、检查参数，不执行SQL，保留非法参数先于连接/取消错误的顺序。
	prepared, _, err := templateTables.listQuery(s.db, q.Cursor, q.Limit, q.NamePrefix, q.States, true, q.Page, false)
	if err != nil {
		return repository.Page[repository.TemplateCandidateFacts]{}, err
	}
	if prepared == nil || !storable(string(q.Series)) {
		return emptyPage[repository.TemplateCandidateFacts](q.Page), nil
	}
	var out repository.Page[repository.TemplateCandidateFacts]
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = scanTemplateCandidates(tx, q)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		// 取消可能先让database/sql自动回滚，提交因此只报事务已结束；只在这条路径保留取消原因。
		if errors.Is(err, sql.ErrTxDone) {
			if canceled := ctx.Err(); canceled != nil {
				err = canceled
			}
		}
		return repository.Page[repository.TemplateCandidateFacts]{}, err
	}
	return out, nil
}

func scanTemplateCandidates(tx *gorm.DB, q repository.TemplateQuery) (repository.Page[repository.TemplateCandidateFacts], error) {
	db, limit, err := templateTables.listQuery(tx, q.Cursor, q.Limit, q.NamePrefix, q.States, true, q.Page, false)
	if err != nil {
		return repository.Page[repository.TemplateCandidateFacts]{}, err
	}
	out := emptyPage[repository.TemplateCandidateFacts](q.Page)
	if db == nil || !storable(string(q.Series)) {
		return out, nil
	}
	if q.Series != "" {
		series, err := encodeJSON([]catalog.Series{q.Series})
		if err != nil {
			return repository.Page[repository.TemplateCandidateFacts]{}, err
		}
		db = db.Where("template_versions.series @> CAST(? AS jsonb)", series)
	}
	manifest := selection.Manifest{Templates: q.Selection.BuiltinTemplates, Hooks: q.Selection.BuiltinHooks}
	offset := int64(0)
	if q.Page > 0 {
		offset = int64((q.Page - 1) * limit)
	}
	var total int64
	var afterKey, afterID, lastCursor string
	for {
		batch := db.Session(&gorm.Session{})
		if afterID != "" {
			batch = batch.Where(`(templates.name_key COLLATE "C" > ? OR (templates.name_key COLLATE "C" = ? AND templates.id COLLATE "C" > ?))`, afterKey, afterKey, afterID)
		}
		var rows []templateVersionRow
		if err := batch.Limit(templateCandidateBatchSize).Find(&rows).Error; err != nil {
			return repository.Page[repository.TemplateCandidateFacts]{}, err
		}
		templates := make([]model.Template, 0, len(rows))
		for _, row := range rows {
			tpl, err := row.template()
			if err != nil {
				return repository.Page[repository.TemplateCandidateFacts]{}, err
			}
			templates = append(templates, tpl)
		}
		hooks, err := getHooks(tx, repository.TemplateHookKeys(templates))
		if err != nil {
			return repository.Page[repository.TemplateCandidateFacts]{}, err
		}
		for i, tpl := range templates {
			if !selection.TemplateEligible(tpl, hooks, q.Selection.ISO, manifest) {
				continue
			}
			index := total
			total++
			// 用相减比较窗口，不计算可能溢出的offset+limit。
			if index < offset || index-offset >= int64(limit) {
				if q.Page == 0 {
					out.Next = lastCursor
					return out, nil
				}
				continue
			}
			own := make(map[model.HookKey]model.Hook)
			for _, key := range model.HookRefs(tpl) {
				if h, ok := hooks[key]; ok {
					own[key] = h
				}
			}
			out.Items = append(out.Items, repository.TemplateCandidateFacts{Template: tpl, Hooks: own})
			lastCursor = repository.EncodeCursor(templateTables.idents, rows[i].NameKey, tpl.ID)
		}
		if len(rows) < templateCandidateBatchSize {
			break
		}
		last := rows[len(rows)-1]
		afterKey, afterID = last.NameKey, last.TemplateID
	}
	if q.Page > 0 {
		out.Total = &total
		if total > offset && total-offset > int64(limit) {
			out.Next = lastCursor
		}
	}
	return out, nil
}
