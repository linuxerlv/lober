package pgstore

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hookContentColumns 是保存草稿写的列：内容整条替换，改成零值的也照写；名字、状态、作者、审批人与内置标记由仓储管。
var hookContentColumns = []string{"description", "params", "timeout_sec", "required", "impls", "rev"}

// CreateHook 见 store.Hooks。名字撞了由唯一索引 idx_hooks_name_key 报出来，不先查：先查再插，两次同时新建就都查不到。
func (s *Store) CreateHook(ctx context.Context, h model.Hook) (model.Hook, error) {
	name, key, err := store.CheckNewHook(h)
	if err != nil {
		return model.Hook{}, err
	}
	h.ID, h.Version, h.Name, h.State, h.Rev = store.NewID(), 1, name, model.VersionDraft, 1
	row, err := toHookVersionRow(h)
	if err != nil {
		return model.Hook{}, err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		if err := hookTables.createIdent(tx, ident{ID: h.ID, Name: name, NameKey: key, LastVersion: 1}); err != nil {
			return err
		}
		return tx.Omit(clause.Associations).Create(&row).Error
	})
	if err != nil {
		return model.Hook{}, translate(err, false, s.nameHolder(ctx, hookTables, key))
	}
	return row.hook()
}

// SaveHookDraft 见 store.Hooks。保存替换的是内容；名字、状态、作者、审批人与内置标记由仓储管，带来的与库里的不同报
// ErrInvalid（store.CheckSameMeta）。
func (s *Store) SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error) {
	if err := store.CheckHookDraft(h); err != nil {
		return model.Hook{}, err
	}
	var row hookVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		old, err := lockHookVersion(tx, h.Key(), h.Rev)
		if err != nil {
			return err
		}
		what := "hook " + store.VersionID(h.ID, h.Version)
		if err := store.CheckDraft(what, old.meta()); err != nil {
			return err
		}
		if err := store.CheckSameMeta(what, old.meta(), store.HookMeta(h)); err != nil {
			return err
		}
		h.Rev = old.Rev + 1
		if row, err = toHookVersionRow(h); err != nil {
			return err
		}
		return tx.Model(&hookVersionRow{HookID: h.ID, Version: h.Version}).Select(hookContentColumns).Updates(&row).Error
	})
	if err != nil {
		return model.Hook{}, translate(err, false, nil)
	}
	return row.hook()
}

// NewHookVersion 见 store.Hooks。新版本的号由 allocVersion 分配，删掉的草稿的号不再发。
func (s *Store) NewHookVersion(ctx context.Context, id string, from int, author string) (model.Hook, error) {
	if err := store.CheckVersionKey(id, from); err != nil {
		return model.Hook{}, err
	}
	if err := store.CheckAuthor(author); err != nil {
		return model.Hook{}, err
	}
	var row hookVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		x, err := hookTables.lockForNewVersion(tx, id)
		if err != nil {
			return err
		}
		src, ok, err := hookVersionAt(tx, model.HookKey{ID: id, Version: from})
		switch {
		case err != nil:
			return err
		case !ok:
			return errorf(store.ErrNotFound, "hook %s 没有版本 %d", id, from)
		}
		next, err := hookTables.allocVersion(tx, x)
		if err != nil {
			return err
		}
		row = src // 内容照抄来源；名字随身份
		row.Version, row.State, row.Author, row.Approver, row.Rev = next, string(model.VersionDraft), author, "", 1
		return tx.Omit(clause.Associations).Create(&row).Error
	})
	if err != nil {
		return model.Hook{}, translate(err, false, nil)
	}
	return row.hook()
}

// SubmitHook 见 store.Hooks。
func (s *Store) SubmitHook(ctx context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(ctx, k, rev, a, model.VersionDraft, model.VersionReview, nil)
}

// ApproveHook 见 store.Hooks。重跑的判定看的是锁住之后读到的版本（store.ApproveHookProblems）。
func (s *Store) ApproveHook(ctx context.Context, k model.HookKey, rev int64, approver string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckApprover(approver); err != nil {
		return err
	}
	return s.hookStep(ctx, k, rev, a, model.VersionReview, model.VersionApproved, func(h *model.Hook) error {
		if err := store.ApproveHookProblems(*h, approver); err != nil {
			return err
		}
		h.Approver = approver
		return nil
	})
}

// RejectHook 见 store.Hooks。
func (s *Store) RejectHook(ctx context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(ctx, k, rev, a, model.VersionReview, model.VersionDraft, nil)
}

// DeprecateHook 见 store.Hooks。内置的版本也不能废弃：内置内容只读（C25）。正在审批的模板对这个版本加了读锁，废弃要等
// 那次审批提交（ApproveTemplate）。
func (s *Store) DeprecateHook(ctx context.Context, k model.HookKey, rev int64, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	return s.hookStep(ctx, k, rev, a, model.VersionApproved, model.VersionDeprecated, nil)
}

// hookStep 把版本 k 从 from 转到 to，审计已由调用方查过。先给版本行加写锁，再依次查 Rev、内置与状态；apply 非空时在
// 转换之前对锁住之后读到的版本跑它，返回错误就什么都不写（审批的重跑判定）。审计写在转换之后，写失败时整个事务回滚。
func (s *Store) hookStep(ctx context.Context, k model.HookKey, rev int64, a store.Audit, from, to model.VersionState,
	apply func(h *model.Hook) error) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockHookVersion(tx, k, rev)
		if err != nil {
			return err
		}
		target := store.VersionID(k.ID, k.Version)
		if err := store.CheckStep("hook "+target, row.meta(), from); err != nil {
			return err
		}
		if apply != nil {
			h, err := row.hook()
			if err != nil {
				return err
			}
			if err := apply(&h); err != nil {
				return err
			}
			row.Approver = h.Approver
		}
		row.State, row.Rev = string(to), row.Rev+1
		if err := tx.Model(&hookVersionRow{HookID: k.ID, Version: k.Version}).Select(stepColumns).Updates(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, hookTables.target, target)
	})
	return translate(err, false, nil)
}

// RenameHook 见 store.Hooks。名字走用户的入口（store.NormalizeName），用了保留前缀报 name_reserved。
func (s *Store) RenameHook(ctx context.Context, id, name string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	display, key, ps := store.NormalizeName(name, model.Subject{Kind: "hook", ID: id})
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return s.rename(ctx, hookTables, id, display, key, a)
}

// DeleteHookDraft 见 store.Hooks。先锁身份、再锁版本，次序与别的写入一致：删的是最后一个版本时要连身份一起删，同时
// 给这个身份开新版本的那一次排在后面。引用查的是 template_hook_refs，它由保存模板的各个版本时维护。
func (s *Store) DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		switch _, ok, err := hookTables.lockIdent(tx, k.ID); {
		case err != nil:
			return err
		case !ok:
			return hookNotFound(k)
		}
		row, err := lockHookVersion(tx, k, rev)
		if err != nil {
			return err
		}
		what := "hook " + store.VersionID(k.ID, k.Version)
		if err := store.CheckDraft(what, row.meta()); err != nil {
			return err
		}
		var by []templateHookRefRow
		err = tx.Where("hook_id = ? AND hook_version = ?", k.ID, k.Version).
			Order(`template_id COLLATE "C", template_version`).Limit(1).Find(&by).Error
		switch {
		case err != nil:
			return err
		case len(by) > 0: // 报按键排在最前的那一个，说法每次都一样
			return errorf(store.ErrReferenced, "%s 被模板 %s 引用：先改掉那个模板的装配表，或删掉那个模板草稿", what,
				store.VersionID(by[0].TemplateID, by[0].TemplateVersion))
		}
		if err := tx.Delete(&hookVersionRow{HookID: k.ID, Version: k.Version}).Error; err != nil {
			return err
		}
		return hookTables.dropIfEmpty(tx, k.ID)
	})
	return translate(err, true, nil)
}

// GetHook 见 store.Hooks。
func (s *Store) GetHook(ctx context.Context, k model.HookKey) (model.Hook, error) {
	if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
		return model.Hook{}, err
	}
	row, ok, err := hookVersionAt(s.read(ctx), k)
	switch {
	case err != nil:
		return model.Hook{}, err
	case !ok:
		return model.Hook{}, hookNotFound(k)
	}
	return row.hook()
}

// GetHooks 见 store.Hooks。键先全部查过格式，有一个不合就报 ErrInvalid，不当成找不到。
func (s *Store) GetHooks(ctx context.Context, ks []model.HookKey) (map[model.HookKey]model.Hook, error) {
	for _, k := range ks {
		if err := store.CheckVersionKey(k.ID, k.Version); err != nil {
			return nil, err
		}
	}
	out := make(map[model.HookKey]model.Hook, len(ks))
	for chunk := range slices.Chunk(ks, batchSize) {
		rows, err := hookVersionsIn(s.read(ctx), chunk)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			h, err := r.hook()
			if err != nil {
				return nil, err
			}
			out[h.Key()] = h
		}
	}
	return out, nil
}

// ListHookVersions 见 store.Hooks。身份至少有一个版本（删掉最后一个版本时身份一起删掉），一个都没取到就是身份不存在。
func (s *Store) ListHookVersions(ctx context.Context, id string) ([]model.Hook, error) {
	if err := store.CheckID(id); err != nil {
		return nil, err
	}
	var rows []hookVersionRow
	if err := hookTables.joined(s.read(ctx)).Where("hook_versions.hook_id = ?", id).Order("hook_versions.version").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errorf(store.ErrNotFound, "hook %s 不存在", id)
	}
	out := make([]model.Hook, len(rows))
	for i, r := range rows {
		h, err := r.hook()
		if err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, nil
}

// ListHooks 见 store.Hooks，条件、排序与游标见 listQuery。
func (s *Store) ListHooks(ctx context.Context, q store.HookQuery) (store.Page[model.Hook], error) {
	db, limit, err := hookTables.listQuery(s.read(ctx), q.Cursor, q.Limit, q.NamePrefix, q.States, q.ApprovedOnly)
	switch {
	case err != nil:
		return store.Page[model.Hook]{}, err
	case db == nil:
		return emptyPage[model.Hook](), nil
	}
	var rows []hookVersionRow
	if err := db.Find(&rows).Error; err != nil {
		return store.Page[model.Hook]{}, err
	}
	return page(rows, limit, hookVersionRow.hook, func(r hookVersionRow) string {
		return store.EncodeCursor(hookTables.idents, r.NameKey, r.HookID)
	})
}

// EnsureBuiltinHook 见 store.Hooks。比的是版本的全部字段，名字按身份上的，Rev 不比：内置的版本写入之后不会再变。名字
// 走内置装载的入口（store.CheckBuiltinHook），必须带保留前缀；名字随身份定下，见 ensureBuiltinIdent。名字被别的身份
// 占着由 ensureBuiltinIdent 在事务里查出来，报 DuplicateError：装载撞不上名字的唯一索引，translate 不用 holder。
//
// 先确定并锁住身份（ensureBuiltinIdent），锁住之后才查版本：两路同时装载同一个版本时，后到的一路等先到的一路提交，看得到
// 它写下的版本，内容相同就什么都不做。先查版本再锁身份的话，后到的一路看不到对方还没提交的版本，会去建同一个身份或写
// 同一个版本，撞上主键。这个次序只在 READ COMMITTED 下成立（见 write）。
func (s *Store) EnsureBuiltinHook(ctx context.Context, h model.Hook) error {
	key, err := store.CheckBuiltinHook(h)
	if err != nil {
		return err
	}
	id := store.VersionID(h.ID, h.Version)
	h.Rev = 1
	row, err := toHookVersionRow(h)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		if err := hookTables.ensureBuiltinIdent(tx, h.ID, h.Version, h.Name, key); err != nil {
			return err
		}
		old, ok, err := hookVersionAt(tx, h.Key())
		switch {
		case err != nil:
			return err
		case !ok:
			return tx.Omit(clause.Associations).Create(&row).Error
		}
		cur, err := old.hook()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(cur, h) {
			return errorf(store.ErrBuiltinMismatch, "内置的 hook %s 与库里的同一版本不同：改了内置内容就要升版本号", id)
		}
		return nil
	})
	return translate(err, false, nil)
}

// 下面是 hook 版本的读取，都连上身份表带出现在的名字（hookTables.joined）。

// hookVersionAt 在 db 里取版本 k，不存在时 ok 为 false。
func hookVersionAt(db *gorm.DB, k model.HookKey) (hookVersionRow, bool, error) {
	var row hookVersionRow
	err := hookTables.joined(db).Where("hook_versions.hook_id = ? AND hook_versions.version = ?", k.ID, k.Version).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return hookVersionRow{}, false, nil
	case err != nil:
		return hookVersionRow{}, false, err
	}
	return row, true, nil
}

// lockHookVersion 在事务 tx 里给版本 k 加写锁并取出它：不存在报 ErrNotFound，Rev 不是 rev 报 ErrConflict。只锁版本行，
// 身份行不锁：要锁身份的写入（删除、开新版本）先锁身份，次序不会反过来。
func lockHookVersion(tx *gorm.DB, k model.HookKey, rev int64) (hookVersionRow, error) {
	var row hookVersionRow
	err := hookTables.joined(tx).Clauses(lockOf("UPDATE", hookTables.versions)).
		Where("hook_versions.hook_id = ? AND hook_versions.version = ?", k.ID, k.Version).Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return hookVersionRow{}, hookNotFound(k)
	case err != nil:
		return hookVersionRow{}, err
	case row.Rev != rev:
		return hookVersionRow{}, staleRev("hook "+store.VersionID(k.ID, k.Version), row.Rev, rev)
	}
	return row, nil
}

// hookVersionsIn 在 db 里取 ks 指的那些 hook 版本，按 ID、版本号的字节序排，不存在的不在结果里；lock 给取到的版本行加锁。
// 排过序，几次加锁的顺序就都一样。
func hookVersionsIn(db *gorm.DB, ks []model.HookKey, lock ...clause.Expression) ([]hookVersionRow, error) {
	if len(ks) == 0 {
		return nil, nil
	}
	pairs := make([][]any, len(ks))
	for i, k := range ks {
		pairs[i] = []any{k.ID, k.Version}
	}
	var rows []hookVersionRow
	err := hookTables.joined(db).Clauses(lock...).Where("(hook_versions.hook_id, hook_versions.version) IN ?", pairs).
		Order(`hook_versions.hook_id COLLATE "C", hook_versions.version`).Find(&rows).Error
	return rows, err
}

func hookNotFound(k model.HookKey) error {
	return errorf(store.ErrNotFound, "hook %s 不存在", store.VersionID(k.ID, k.Version))
}

// 下面是 hook 与模板共用的部分：hook 与模板的身份、版本与名字规则一一对应（store.Templates 的说明），表也一一对应。入参
// 与版本状态的检查不用查库就能定下，放在 store 包（check_authoring.go）；这里只有身份表与版本表怎么读写。

// versioning 是 hook 或模板的身份表与版本表，共用的读写按它取表名。表名都是常量，拼进 SQL 也不会有注入。
type versioning struct {
	kind     string // 报错里的称呼
	target   string // 审计对象的 TargetKind
	idents   string // 身份表，也是列表游标里的列表名
	versions string // 版本表
	owner    string // 版本表里身份 ID 的列
}

var (
	hookTables     = versioning{kind: "hook", target: "hook", idents: "hooks", versions: "hook_versions", owner: "hook_id"}
	templateTables = versioning{kind: "模板", target: "template", idents: "templates", versions: "template_versions", owner: "template_id"}
)

// stepColumns 是状态转换写的列。
var stepColumns = []string{"state", "approver", "rev"}

// unapprovedStates 是未审批的版本的状态，与部分唯一索引 idx_*_versions_unapproved 的谓词一致。
var unapprovedStates = []string{string(model.VersionDraft), string(model.VersionReview)}

// joined 查版本表，连上身份表带出身份现在的名字与名字的 key（版本行的 Name 与 NameKey）。
func (v versioning) joined(db *gorm.DB) *gorm.DB {
	return db.Table(v.versions).Select(v.versions + ".*, " + v.idents + ".name, " + v.idents + ".name_key").
		Joins("JOIN " + v.idents + " ON " + v.idents + ".id = " + v.versions + "." + v.owner)
}

// lockOf 是只加在表 table 的行上的锁：连表查询时，身份行不跟着锁上。strength 是 UPDATE（写锁）或 SHARE（读锁）。
func lockOf(strength, table string) clause.Locking {
	return clause.Locking{Strength: strength, Table: clause.Table{Name: table}}
}

// lockIdent 在事务 tx 里给身份 id 加写锁并取出它，不存在时 ok 为 false。
func (v versioning) lockIdent(tx *gorm.DB, id string) (ident, bool, error) {
	var x ident
	err := tx.Table(v.idents).Clauses(forUpdate).Where("id = ?", id).Take(&x).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ident{}, false, nil
	case err != nil:
		return ident{}, false, err
	}
	return x, true, nil
}

// createIdent 建一个身份。名字被别的身份占了由唯一索引报出来，调用方用 nameHolder 翻成 DuplicateError。
func (v versioning) createIdent(tx *gorm.DB, x ident) error {
	return tx.Table(v.idents).Create(&x).Error
}

// lockForNewVersion 是开新版本的第一步：锁住身份 id。同一个身份同时开新版本的两次因此排队，后一次看得到前一次开的草稿，
// 由 allocVersion 报 ErrConflict。不存在报 ErrNotFound，内置的报 ErrImmutable（C25）。
func (v versioning) lockForNewVersion(tx *gorm.DB, id string) (ident, error) {
	x, ok, err := v.lockIdent(tx, id)
	switch {
	case err != nil:
		return ident{}, err
	case !ok:
		return ident{}, errorf(store.ErrNotFound, "%s %s 不存在", v.kind, id)
	case x.Builtin:
		return ident{}, errorf(store.ErrImmutable, "%s %s 是内置的，不能开新版本", v.kind, id)
	}
	return x, nil
}

// allocVersion 给锁住的身份 x 分配新版本的号：x 没有未审批的版本时（每个身份同时最多一个草稿或待审批的版本），取它分配过
// 的最大版本号加 1，记回身份上；有就报 ErrConflict。部分唯一索引兜底。
func (v versioning) allocVersion(tx *gorm.DB, x ident) (int, error) {
	var pending []int
	err := tx.Table(v.versions).Where(v.owner+" = ? AND state IN ?", x.ID, unapprovedStates).
		Order("version").Limit(1).Pluck("version", &pending).Error
	switch {
	case err != nil:
		return 0, err
	case len(pending) > 0:
		return 0, errorf(store.ErrConflict, "%s %s 已有未审批的版本 %d：先审批、驳回或删掉它", v.kind, x.ID, pending[0])
	}
	next := x.LastVersion + 1
	return next, tx.Table(v.idents).Where("id = ?", x.ID).Update("last_version", next).Error
}

// dropIfEmpty 在身份 id 的一个版本删掉之后调用：删的是最后一个版本时，身份一起删掉，名字随之空出来。调用方已锁住身份，
// 数版本与删身份之间插不进新版本。
func (v versioning) dropIfEmpty(tx *gorm.DB, id string) error {
	var n int64
	if err := tx.Table(v.versions).Where(v.owner+" = ?", id).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	return tx.Table(v.idents).Where("id = ?", id).Delete(&ident{}).Error
}

// ensureBuiltinIdent 确认内置版本 version 所属的身份，并给它加写锁，直到事务结束。
//
// 先插入一个内置的身份，撞上任何一个唯一约束就什么都不做：ON CONFLICT DO NOTHING 不带冲突目标，主键与名字的唯一索引
// 都当仲裁。别的事务刚插入同一个 ID 或同一个名字、还没提交时，这一步等它结束：对方提交了就什么都不做，回滚了就照常插入。
// 只拿主键当仲裁（ON CONFLICT (id)）不够：两路同时首次装载同一个新身份，都在对方插入之前查过冲突的话，后到的一路在名字
// 的索引上等先到的提交，之后直接报 23505，在事务外查到的占着名字的身份正是它自己要装载的，只能报出 DuplicateError。
//
// 再锁住身份。锁不到说明插入时撞上的是别的身份占着的名字（用户的身份用不了保留前缀，占着它的通常是另一个内置身份）：在
// 事务里按名字的 key 查出那个身份，报 DuplicateError；查不到说明它恰好改了名或删掉了，或者这个 ID 的身份恰好删掉了，报
// ErrConflict，重读之后再装。锁到了就比对：要是内置的、名字也相同，否则程序自带的内容与库里的对不上
// （ErrBuiltinMismatch）：名字在身份上，各版本共用，随身份定下，以后的版本不能换名，要改名只能换一个新 ID。身份上分配
// 过的最大版本号随装载的版本更新。
func (v versioning) ensureBuiltinIdent(tx *gorm.DB, id string, version int, name, key string) error {
	err := tx.Table(v.idents).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&ident{ID: id, Name: name, NameKey: key, Builtin: true, LastVersion: version}).Error
	if err != nil {
		return err
	}
	x, ok, err := v.lockIdent(tx, id)
	switch {
	case err != nil:
		return err
	case !ok:
		holder, err := v.nameOwner(tx, key)
		switch {
		case err != nil:
			return err
		case holder == "":
			return errorf(store.ErrConflict, "装载内置的 %s %s 时撞上的身份已经变了：重读之后再判断", v.kind, id)
		}
		return &store.DuplicateError{Field: "name", Existing: holder}
	case !x.Builtin || x.Name != name:
		return errorf(store.ErrBuiltinMismatch, "%s %s 在库里名为 %q（内置：%v），程序自带的是内置的 %q", v.kind, id, x.Name, x.Builtin, name)
	case version > x.LastVersion:
		return tx.Table(v.idents).Where("id = ?", id).Update("last_version", version).Error
	}
	return nil
}

// rename 给身份 id 改名，hook 与模板相同：不存在报 ErrNotFound，内置的报 ErrImmutable，名字被别的身份占了由唯一索引报
// 出来，翻成 DuplicateError。只改大小写时 key 还是自己的，撞不上。名字只在身份上，版本那几行没改，它们的 Rev 不变。
func (s *Store) rename(ctx context.Context, v versioning, id, name, key string, a store.Audit) error {
	err := s.write(ctx, func(tx *gorm.DB) error {
		switch x, ok, err := v.lockIdent(tx, id); {
		case err != nil:
			return err
		case !ok:
			return errorf(store.ErrNotFound, "%s %s 不存在", v.kind, id)
		case x.Builtin:
			return errorf(store.ErrImmutable, "%s %s 是内置的，不能改名", v.kind, id)
		}
		if err := tx.Table(v.idents).Where("id = ?", id).Updates(map[string]any{"name": name, "name_key": key}).Error; err != nil {
			return err
		}
		return addAudits(tx, a, v.target, id)
	})
	return translate(err, false, s.nameHolder(ctx, v, key))
}

// nameHolder 是 translate 要的 holder：撞上名字的唯一索引之后事务已经中止，在事务外查出占着 key 的身份。查不到（对方
// 刚好改了名，或者删掉了最后一个版本、身份随之删掉）或查失败就返回空串，translate 报 ErrConflict。
func (s *Store) nameHolder(ctx context.Context, v versioning, key string) func(string) string {
	return func(string) string {
		id, err := v.nameOwner(s.read(ctx), key)
		if err != nil {
			return ""
		}
		return id
	}
}

// nameOwner 在 db 里查出名字的 key 为 key 的身份，没有就返回空串。
func (v versioning) nameOwner(db *gorm.DB, key string) (string, error) {
	var ids []string
	if err := db.Table(v.idents).Where("name_key = ?", key).Limit(1).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// listQuery 查 ListHooks 与 ListTemplates 的入参，给出取一页的查询（多取一条）与条数。每个身份一项：默认是最新的版本，
// approvedOnly 时是当前的已审批版本，规则同 model.CurrentApproved（已审批的最高版本），没有就跳过；状态的过滤看的是选出
// 的那一项。名字的前缀按 store.NameKeyPrefix 转了再与名字的 key 比；starts_with 按原样比，_、% 与反斜杠不必转义。按名字的
// key、再按 ID 的字节序排，游标是上一页最后一项的这两样。条件存不下时查询为 nil：什么都匹配不上。
func (v versioning) listQuery(db *gorm.DB, cursor string, limit int, prefix string, states []model.VersionState,
	approvedOnly bool) (*gorm.DB, int, error) {
	n, err := store.CheckQuery(cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	var afterKey, afterID string
	if cursor != "" {
		if err := store.DecodeCursor(cursor, v.idents, &afterKey, &afterID); err != nil {
			return nil, 0, err
		}
	}
	var keep []string // 存不下的状态谁都不是，去掉；给了状态却一个都不剩，就什么都匹配不上
	for _, st := range states {
		if storable(string(st)) {
			keep = append(keep, string(st))
		}
	}
	if !storable(prefix) || len(states) > 0 && len(keep) == 0 {
		return nil, n, nil
	}
	pick := "SELECT max(w.version) FROM " + v.versions + " w WHERE w." + v.owner + " = " + v.versions + "." + v.owner
	if approvedOnly {
		pick += " AND w.state = '" + string(model.VersionApproved) + "'"
	}
	db = v.joined(db).Where(v.versions + ".version = (" + pick + ")")
	if prefix != "" {
		db = db.Where("starts_with("+v.idents+".name_key, ?)", store.NameKeyPrefix(prefix))
	}
	if len(keep) > 0 {
		db = db.Where(v.versions+".state IN ?", keep)
	}
	key, id := v.idents+`.name_key COLLATE "C"`, v.idents+`.id COLLATE "C"`
	if cursor != "" {
		db = db.Where("("+key+" > ? OR ("+key+" = ? AND "+id+" > ?))", afterKey, afterKey, afterID)
	}
	return db.Order(key + ", " + id).Limit(n + 1), n, nil
}
