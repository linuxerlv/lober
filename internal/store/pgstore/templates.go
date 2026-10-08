package pgstore

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 模板与 hook 一一对应，身份、名字与状态转换用 hooks.go 里共用的那几个函数；这里只写模板自己的：派生、审批时连同引用
// 的 hook 版本一起判定、按系过滤，以及保存任何一个版本时在同一个事务里重写它在 template_hook_refs 里的行。

// templateContentColumns 是保存草稿写的列：内容整条替换，改成零值的也照写。派生来源只由派生写，保存时不写。
var templateContentColumns = []string{"family", "series", "body", "params", "bindings", "assembly", "rev"}

// CreateTemplate 见 store.Templates。派生要记来源，只能走 DeriveTemplate，所以入参不能带 DerivedFrom。名字撞了由唯一
// 索引 idx_templates_name_key 报出来，理由同 CreateHook。
func (s *Store) CreateTemplate(ctx context.Context, tpl model.Template) (model.Template, error) {
	name, key, err := store.CheckNewTemplate(tpl)
	if err != nil {
		return model.Template{}, err
	}
	tpl.ID, tpl.Version, tpl.Name, tpl.State, tpl.Rev = store.NewID(), 1, name, model.VersionDraft, 1
	var row templateVersionRow
	err = s.write(ctx, func(tx *gorm.DB) error {
		var err error
		row, err = insertNewTemplate(tx, tpl, key)
		return err
	})
	if err != nil {
		return model.Template{}, translate(err, false, s.nameHolder(ctx, templateTables, key))
	}
	return row.template()
}

// SaveTemplateDraft 见 store.Templates。规则同 SaveHookDraft；派生来源只由派生写，保存时改了也报 ErrInvalid。这个版本
// 引用的 hook 版本在同一个事务里重写。
func (s *Store) SaveTemplateDraft(ctx context.Context, tpl model.Template) (model.Template, error) {
	if err := store.CheckTemplateDraft(tpl); err != nil {
		return model.Template{}, err
	}
	k := model.TemplateKey{ID: tpl.ID, Version: tpl.Version}
	var row templateVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		old, err := lockTemplateVersion(tx, k, tpl.Rev)
		if err != nil {
			return err
		}
		what := "模板 " + store.VersionID(k.ID, k.Version)
		if err := store.CheckDraft(what, old.meta()); err != nil {
			return err
		}
		if err := store.CheckSameMeta(what, old.meta(), store.TemplateMeta(tpl)); err != nil {
			return err
		}
		cur, err := old.template()
		if err != nil {
			return err
		}
		if err := store.CheckSameDerivedFrom(what, cur.DerivedFrom, tpl.DerivedFrom); err != nil {
			return err
		}
		tpl.Rev = old.Rev + 1
		if row, err = toTemplateVersionRow(tpl); err != nil {
			return err
		}
		if err := tx.Model(&templateVersionRow{TemplateID: k.ID, Version: k.Version}).Select(templateContentColumns).Updates(&row).Error; err != nil {
			return err
		}
		return rewriteRefs(tx, tpl)
	})
	if err != nil {
		return model.Template{}, translate(err, false, nil)
	}
	return row.template()
}

// NewTemplateVersion 见 store.Templates。规则同 NewHookVersion；派生来源跟着内容照抄：来源出了新版本时，派生模板的每个
// 版本都要能比出差异（C24）。
func (s *Store) NewTemplateVersion(ctx context.Context, id string, from int, author string) (model.Template, error) {
	if err := store.CheckVersionKey(id, from); err != nil {
		return model.Template{}, err
	}
	if err := store.CheckAuthor(author); err != nil {
		return model.Template{}, err
	}
	var row templateVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		x, err := templateTables.lockForNewVersion(tx, id)
		if err != nil {
			return err
		}
		src, ok, err := templateVersionAt(tx, model.TemplateKey{ID: id, Version: from})
		switch {
		case err != nil:
			return err
		case !ok:
			return errorf(store.ErrNotFound, "模板 %s 没有版本 %d", id, from)
		}
		next, err := templateTables.allocVersion(tx, x)
		if err != nil {
			return err
		}
		tpl, err := src.template() // 内容照抄来源；名字随身份
		if err != nil {
			return err
		}
		tpl.Version, tpl.State, tpl.Author, tpl.Approver, tpl.Rev = next, model.VersionDraft, author, "", 1
		if row, err = toTemplateVersionRow(tpl); err != nil {
			return err
		}
		return insertTemplateVersion(tx, row, refRows(tpl))
	})
	if err != nil {
		return model.Template{}, translate(err, false, nil)
	}
	return row.template()
}

// DeriveTemplate 见 store.Templates。复制的是来源版本的族、系、正文、参数、取值来源与装配表；名字、作者与派生来源是新
// 身份自己的。来源只读不锁：派生出的是一份独立的副本，来源之后怎么改都与它无关（C24）。派生是用户的路径，名字走用户的
// 入口（store.NormalizeName）：从内置模板派生时也不能沿用带保留前缀的名字。
func (s *Store) DeriveTemplate(ctx context.Context, from model.TemplateKey, name, author string) (model.Template, error) {
	if err := store.CheckVersionKey(from.ID, from.Version); err != nil {
		return model.Template{}, err
	}
	if err := store.CheckAuthor(author); err != nil {
		return model.Template{}, err
	}
	display, key, ps := store.NormalizeName(name, model.Subject{Kind: "template"}) // 新身份还没有 ID
	if len(ps) > 0 {
		return model.Template{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	var row templateVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		srcRow, ok, err := templateVersionAt(tx, from)
		switch {
		case err != nil:
			return err
		case !ok:
			return templateNotFound(from)
		}
		src, err := srcRow.template()
		if err != nil {
			return err
		}
		row, err = insertNewTemplate(tx, model.Template{
			ID: store.NewID(), Version: 1, Name: display, State: model.VersionDraft, Author: author, DerivedFrom: &from,
			Family: src.Family, Series: src.Series, Body: src.Body, Params: src.Params, Bindings: src.Bindings, Assembly: src.Assembly,
			Rev: 1,
		}, key)
		return err
	})
	if err != nil {
		return model.Template{}, translate(err, false, s.nameHolder(ctx, templateTables, key))
	}
	return row.template()
}

// SubmitTemplate 见 store.Templates。
func (s *Store) SubmitTemplate(ctx context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(ctx, k, rev, a, model.VersionDraft, model.VersionReview, nil)
}

// ApproveTemplate 见 store.Templates。锁住模板版本之后，按 HookRefs 的顺序给它引用的 hook 版本加读锁再取出：hook 的废弃
// 要等这次审批提交；先提交的废弃，这里读到的就是废弃之后的，判定因此报 hook_ref_deprecated（C26）。引用的版本不存在的
// 不放进判定的入参，由判定报 hook_ref_missing。
func (s *Store) ApproveTemplate(ctx context.Context, k model.TemplateKey, rev int64, approver string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckApprover(approver); err != nil {
		return err
	}
	return s.templateStep(ctx, k, rev, a, model.VersionReview, model.VersionApproved, func(tx *gorm.DB, tpl *model.Template) error {
		hooks := map[model.HookKey]model.Hook{}
		for chunk := range slices.Chunk(store.HookRefs(*tpl), batchSize) {
			rows, err := hookVersionsIn(tx, chunk, lockOf("SHARE", hookTables.versions))
			if err != nil {
				return err
			}
			for _, r := range rows {
				h, err := r.hook()
				if err != nil {
					return err
				}
				hooks[h.Key()] = h
			}
		}
		if err := store.ApproveTemplateProblems(*tpl, hooks, approver); err != nil {
			return err
		}
		tpl.Approver = approver
		return nil
	})
}

// RejectTemplate 见 store.Templates。
func (s *Store) RejectTemplate(ctx context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(ctx, k, rev, a, model.VersionReview, model.VersionDraft, nil)
}

// DeprecateTemplate 见 store.Templates。内置的版本也不能废弃：内置内容只读（C25）。
func (s *Store) DeprecateTemplate(ctx context.Context, k model.TemplateKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.templateStep(ctx, k, rev, a, model.VersionApproved, model.VersionDeprecated, nil)
}

// templateStep 同 hookStep，转的是模板版本 k；apply 还拿得到这次写入的事务，审批要在里面给引用的 hook 版本加锁。
func (s *Store) templateStep(ctx context.Context, k model.TemplateKey, rev int64, a store.Audit, from, to model.VersionState,
	apply func(tx *gorm.DB, tpl *model.Template) error) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockTemplateVersion(tx, k, rev)
		if err != nil {
			return err
		}
		target := store.VersionID(k.ID, k.Version)
		if err := store.CheckStep("模板 "+target, row.meta(), from); err != nil {
			return err
		}
		if apply != nil {
			tpl, err := row.template()
			if err != nil {
				return err
			}
			if err := apply(tx, &tpl); err != nil {
				return err
			}
			row.Approver = tpl.Approver
		}
		row.State, row.Rev = string(to), row.Rev+1
		if err := tx.Model(&templateVersionRow{TemplateID: k.ID, Version: k.Version}).Select(stepColumns).Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, templateTables.target, target)
	})
	return translate(err, false, nil)
}

// RenameTemplate 见 store.Templates。名字走用户的入口，规则同 RenameHook。
func (s *Store) RenameTemplate(ctx context.Context, id, name string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	display, key, ps := store.NormalizeName(name, model.Subject{Kind: "template", ID: id})
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return s.rename(ctx, templateTables, id, display, key, a)
}

// DeleteTemplateDraft 见 store.Templates。加锁的次序同 DeleteHookDraft。它记着的 hook 引用由外键随它一起删掉
// （CASCADE）；任务只引用已审批的版本，挡不着删草稿。
func (s *Store) DeleteTemplateDraft(ctx context.Context, k model.TemplateKey, rev int64) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		switch _, ok, err := templateTables.lockIdent(tx, k.ID); {
		case err != nil:
			return err
		case !ok:
			return templateNotFound(k)
		}
		row, err := lockTemplateVersion(tx, k, rev)
		if err != nil {
			return err
		}
		if err := store.CheckDraft("模板 "+store.VersionID(k.ID, k.Version), row.meta()); err != nil {
			return err
		}
		if err := tx.Delete(&templateVersionRow{TemplateID: k.ID, Version: k.Version}).Error; err != nil {
			return err
		}
		return templateTables.dropIfEmpty(tx, k.ID)
	})
	return translate(err, true, nil)
}

// GetTemplate 见 store.Templates。
func (s *Store) GetTemplate(ctx context.Context, k model.TemplateKey) (model.Template, error) {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return model.Template{}, err
	}
	row, ok, err := templateVersionAt(s.read(ctx), k)
	switch {
	case err != nil:
		return model.Template{}, err
	case !ok:
		return model.Template{}, templateNotFound(k)
	}
	return row.template()
}

// ListTemplateVersions 见 store.Templates，规则同 ListHookVersions。
func (s *Store) ListTemplateVersions(ctx context.Context, id string) ([]model.Template, error) {
	if err := store.CheckID(id); err != nil {
		return nil, err
	}
	var rows []templateVersionRow
	if err := templateTables.joined(s.read(ctx)).Where("template_versions.template_id = ?", id).
		Order("template_versions.version").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errorf(store.ErrNotFound, "模板 %s 不存在", id)
	}
	out := make([]model.Template, len(rows))
	for i, r := range rows {
		tpl, err := r.template()
		if err != nil {
			return nil, err
		}
		out[i] = tpl
	}
	return out, nil
}

// ListTemplates 见 store.Templates。按系过滤看的是选出的那一项，与状态的过滤一样；系存成 jsonb 数组，按包含关系比。
func (s *Store) ListTemplates(ctx context.Context, q store.TemplateQuery) (store.Page[model.Template], error) {
	db, limit, err := templateTables.listQuery(s.read(ctx), q.Cursor, q.Limit, q.NamePrefix, q.States, q.ApprovedOnly)
	switch {
	case err != nil:
		return store.Page[model.Template]{}, err
	case db == nil, !storable(string(q.Series)):
		return emptyPage[model.Template](), nil
	}
	if q.Series != "" {
		series, err := encodeJSON([]catalog.Series{q.Series})
		if err != nil {
			return store.Page[model.Template]{}, err
		}
		db = db.Where("template_versions.series @> CAST(? AS jsonb)", series)
	}
	var rows []templateVersionRow
	if err := db.Find(&rows).Error; err != nil {
		return store.Page[model.Template]{}, err
	}
	return page(rows, limit, templateVersionRow.template, func(r templateVersionRow) string {
		return store.EncodeCursor(templateTables.idents, r.NameKey, r.TemplateID)
	})
}

// EnsureBuiltinTemplate 见 store.Templates，规则同 EnsureBuiltinHook：名字必须带保留前缀，随身份定下。内置模板不是派生
// 来的，带了派生来源报 ErrInvalid（store.CheckBuiltinTemplate）。
func (s *Store) EnsureBuiltinTemplate(ctx context.Context, tpl model.Template) error {
	key, err := store.CheckBuiltinTemplate(tpl)
	if err != nil {
		return err
	}
	id := store.VersionID(tpl.ID, tpl.Version)
	tpl.Rev = 1
	row, err := toTemplateVersionRow(tpl)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		old, ok, err := templateVersionAt(tx, model.TemplateKey{ID: tpl.ID, Version: tpl.Version})
		switch {
		case err != nil:
			return err
		case ok:
			cur, err := old.template()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(cur, tpl) {
				return errorf(store.ErrBuiltinMismatch, "内置的模板 %s 与库里的同一版本不同：改了内置内容就要升版本号", id)
			}
			return nil
		}
		if err := templateTables.ensureBuiltinIdent(tx, tpl.ID, tpl.Version, tpl.Name, key); err != nil {
			return err
		}
		return insertTemplateVersion(tx, row, refRows(tpl))
	})
	return translate(err, false, s.nameHolder(ctx, templateTables, key))
}

// 下面是模板版本的写入与读取，规则同 hook 的那几个函数。引用随版本一起写：保存模板的任何一个版本时，它引用了哪些 hook
// 版本都在同一个事务里记下，删 hook 草稿与建任务都靠这份记录。

// insertNewTemplate 在事务 tx 里建新身份与它的版本 tpl，连同版本引用的 hook 版本。名字撞了由唯一索引报出来。
func insertNewTemplate(tx *gorm.DB, tpl model.Template, key string) (templateVersionRow, error) {
	row, err := toTemplateVersionRow(tpl)
	if err != nil {
		return templateVersionRow{}, err
	}
	if err := templateTables.createIdent(tx, ident{ID: tpl.ID, Name: tpl.Name, NameKey: key, LastVersion: tpl.Version}); err != nil {
		return templateVersionRow{}, err
	}
	return row, insertTemplateVersion(tx, row, refRows(tpl))
}

// insertTemplateVersion 在事务 tx 里写下一个模板版本与它引用的 hook 版本。
func insertTemplateVersion(tx *gorm.DB, row templateVersionRow, refs []templateHookRefRow) error {
	if err := tx.Omit(clause.Associations).Create(&row).Error; err != nil {
		return err
	}
	return insertRefs(tx, refs)
}

// rewriteRefs 在事务 tx 里重写模板版本 tpl 引用的 hook 版本：保存草稿改了装配表，引用就跟着变。
func rewriteRefs(tx *gorm.DB, tpl model.Template) error {
	err := tx.Where("template_id = ? AND template_version = ?", tpl.ID, tpl.Version).Delete(&templateHookRefRow{}).Error
	if err != nil {
		return err
	}
	return insertRefs(tx, refRows(tpl))
}

func insertRefs(tx *gorm.DB, refs []templateHookRefRow) error {
	if len(refs) == 0 { // GORM 不收空的一批
		return nil
	}
	return tx.Omit(clause.Associations).CreateInBatches(&refs, batchSize).Error
}

// templateVersionAt 在 db 里取版本 k，不存在时 ok 为 false。
func templateVersionAt(db *gorm.DB, k model.TemplateKey) (templateVersionRow, bool, error) {
	var row templateVersionRow
	err := templateTables.joined(db).Where("template_versions.template_id = ? AND template_versions.version = ?", k.ID, k.Version).
		Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return templateVersionRow{}, false, nil
	case err != nil:
		return templateVersionRow{}, false, err
	}
	return row, true, nil
}

// lockTemplateVersion 在事务 tx 里给版本 k 加写锁并取出它：不存在报 ErrNotFound，Rev 不是 rev 报 ErrConflict。只锁版本
// 行，理由同 lockHookVersion。
func lockTemplateVersion(tx *gorm.DB, k model.TemplateKey, rev int64) (templateVersionRow, error) {
	var row templateVersionRow
	err := templateTables.joined(tx).Clauses(lockOf("UPDATE", templateTables.versions)).
		Where("template_versions.template_id = ? AND template_versions.version = ?", k.ID, k.Version).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return templateVersionRow{}, templateNotFound(k)
	case err != nil:
		return templateVersionRow{}, err
	case row.Rev != rev:
		return templateVersionRow{}, staleRev("模板 "+store.VersionID(k.ID, k.Version), row.Rev, rev)
	}
	return row, nil
}

func templateNotFound(k model.TemplateKey) error {
	return errorf(store.ErrNotFound, "模板 %s 不存在", store.VersionID(k.ID, k.Version))
}
