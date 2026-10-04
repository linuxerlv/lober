package pgstore

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hookContentColumns 是保存草稿写的列：内容整条替换，改成零值的也照写；名字、状态、作者、审批人与内置标记由仓储管。
var hookContentColumns = []string{"description", "params", "timeout_sec", "required", "impls", "rev"}

// CreateHook 见 store.Hooks。名字撞了由唯一索引 idx_hooks_name_key 报出来，不先查：先查再插，两次同时新建就都查不到。
func (s *Store) CreateHook(ctx context.Context, h model.Hook) (model.Hook, error) {
	if h.ID != "" || h.Version != 0 || h.State != "" || h.Approver != "" || h.Builtin || h.Rev != 0 {
		return model.Hook{}, errorf(store.ErrInvalid, "新建 hook 只带内容与作者：ID、版本、状态、审批人、内置标记与 Rev 由仓储填")
	}
	if err := checkAuthor(h.Author); err != nil {
		return model.Hook{}, err
	}
	name, key, ps := normalizeName(h.Name, model.Subject{Kind: "hook"}) // 还没有 ID
	if ps = append(ps, hookTextProblems(h, "")...); len(ps) > 0 {
		return model.Hook{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
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
// ErrInvalid（checkSameMeta）。
func (s *Store) SaveHookDraft(ctx context.Context, h model.Hook) (model.Hook, error) {
	if err := checkKey(h.ID, h.Version); err != nil {
		return model.Hook{}, err
	}
	if ps := hookTextProblems(h, versionID(h.ID, h.Version)); len(ps) > 0 {
		return model.Hook{}, &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	var row hookVersionRow
	err := s.write(ctx, func(tx *gorm.DB) error {
		old, err := lockHookVersion(tx, h.Key(), h.Rev)
		if err != nil {
			return err
		}
		what := "hook " + versionID(h.ID, h.Version)
		if err := checkDraft(what, old.meta()); err != nil {
			return err
		}
		if err := checkSameMeta(what, old.meta(), hookMeta(h)); err != nil {
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
	if err := checkKey(id, from); err != nil {
		return model.Hook{}, err
	}
	if err := checkAuthor(author); err != nil {
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
	if err := checkApprover(approver); err != nil {
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
	if err := checkKey(k.ID, k.Version); err != nil {
		return err
	}
	err := s.write(ctx, func(tx *gorm.DB) error {
		row, err := lockHookVersion(tx, k, rev)
		if err != nil {
			return err
		}
		target := versionID(k.ID, k.Version)
		if err := checkStep("hook "+target, row.meta(), from); err != nil {
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

// RenameHook 见 store.Hooks。
func (s *Store) RenameHook(ctx context.Context, id, name string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if !model.ValidID(id) {
		return badID(id)
	}
	display, key, ps := normalizeName(name, model.Subject{Kind: "hook", ID: id})
	if len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	return s.rename(ctx, hookTables, id, display, key, a)
}

// DeleteHookDraft 见 store.Hooks。先锁身份、再锁版本，次序与别的写入一致：删的是最后一个版本时要连身份一起删，同时
// 给这个身份开新版本的那一次排在后面。引用查的是 template_hook_refs，它由保存模板的各个版本时维护。
func (s *Store) DeleteHookDraft(ctx context.Context, k model.HookKey, rev int64) error {
	if err := checkKey(k.ID, k.Version); err != nil {
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
		what := "hook " + versionID(k.ID, k.Version)
		if err := checkDraft(what, row.meta()); err != nil {
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
				versionID(by[0].TemplateID, by[0].TemplateVersion))
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
	if err := checkKey(k.ID, k.Version); err != nil {
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
		if err := checkKey(k.ID, k.Version); err != nil {
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
	if !model.ValidID(id) {
		return nil, badID(id)
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
		return encodeCursor(hookTables.idents, r.NameKey, r.HookID)
	})
}

// EnsureBuiltinHook 见 store.Hooks。比的是版本的全部字段，名字按身份上的，Rev 不比：内置的版本写入之后不会再变。
func (s *Store) EnsureBuiltinHook(ctx context.Context, h model.Hook) error {
	id := versionID(h.ID, h.Version)
	key, ps, err := checkBuiltin(h.ID, h.Version, hookMeta(h), model.Subject{Kind: "hook", ID: id})
	if err != nil {
		return err
	}
	if ps = append(ps, hookTextProblems(h, id)...); len(ps) > 0 {
		return &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}
	}
	h.Rev = 1
	row, err := toHookVersionRow(h)
	if err != nil {
		return err
	}
	err = s.write(ctx, func(tx *gorm.DB) error {
		old, ok, err := hookVersionAt(tx, h.Key())
		switch {
		case err != nil:
			return err
		case ok:
			cur, err := old.hook()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(cur, h) {
				return errorf(store.ErrBuiltinMismatch, "内置的 hook %s 与库里的同一版本不同：改了内置内容就要升版本号", id)
			}
			return nil
		}
		if err := hookTables.ensureBuiltinIdent(tx, h.ID, h.Version, h.Name, key); err != nil {
			return err
		}
		return tx.Omit(clause.Associations).Create(&row).Error
	})
	return translate(err, false, s.nameHolder(ctx, hookTables, key))
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
		return hookVersionRow{}, staleRev("hook "+versionID(k.ID, k.Version), row.Rev, rev)
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
	return errorf(store.ErrNotFound, "hook %s 不存在", versionID(k.ID, k.Version))
}

func hookMeta(h model.Hook) meta {
	return meta{name: h.Name, state: h.State, author: h.Author, approver: h.Approver, builtin: h.Builtin}
}

// hookTextProblems 是 hook 版本 h 里存不下的文本（model.StorableText，spec §3 总则），每一处一条 text_not_storable：
// 说明落在 hook 整体上，参数声明与实现按位置，Subject 的写法同激活判定，id 是「ID@版本」，新建时为空。text 与 jsonb
// 都不收 NUL，encoding/json 还会把非法的 UTF-8 换成 U+FFFD，读回来的就不是写进去的，所以族、系、种类这些照目录取值的
// 字段也查。规则与内存实现的同名函数相同，由契约测试守着两边一致。
func hookTextProblems(h model.Hook, id string) []model.Problem {
	whole := model.Subject{Kind: "hook", ID: id}
	var ps []model.Problem
	if !model.StorableText(h.Description) {
		ps = append(ps, notStorable(whole))
	}
	ps = append(ps, declTextProblems(h.Params, whole)...)
	for i, impl := range h.Impls {
		if !storable(impl.Body, string(impl.Family), string(impl.Kind), string(impl.Interpreter)) ||
			!storableAll(impl.Series) || !storableAll(impl.Timings) {
			ps = append(ps, notStorable(model.Subject{Kind: "impl", ID: id, Path: "impl/" + strconv.Itoa(i+1)}))
		}
	}
	return ps
}

// 下面是 hook 与模板共用的部分：两边的身份、版本与名字规则一一对应（store.Templates 的说明），表也一一对应。

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

// ensureBuiltinIdent 确认内置版本 version 所属的身份：不存在就建一个内置的身份，名字被别的身份占了由唯一索引报出来；已
// 存在的要是内置的、名字也相同，否则程序自带的内容与库里的对不上（ErrBuiltinMismatch）：名字在身份上，各版本共用。身份上
// 分配过的最大版本号随装载的版本更新。
func (v versioning) ensureBuiltinIdent(tx *gorm.DB, id string, version int, name, key string) error {
	x, ok, err := v.lockIdent(tx, id)
	switch {
	case err != nil:
		return err
	case !ok:
		return v.createIdent(tx, ident{ID: id, Name: name, NameKey: key, Builtin: true, LastVersion: version})
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
// 刚好又删掉了）就返回空串，translate 原样返回撞上的数据库错误。
func (s *Store) nameHolder(ctx context.Context, v versioning, key string) func(string) string {
	return func(string) string {
		var ids []string
		if err := s.read(ctx).Table(v.idents).Where("name_key = ?", key).Limit(1).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
			return ""
		}
		return ids[0]
	}
}

// listQuery 查 ListHooks 与 ListTemplates 的入参，给出取一页的查询（多取一条）与条数。每个身份一项：默认是最新的版本，
// approvedOnly 时是当前的已审批版本，规则同 model.CurrentApproved（已审批的最高版本），没有就跳过；状态的过滤看的是选出
// 的那一项。名字的前缀与名字的 key 比，只把 ASCII 字母转成小写；starts_with 按原样比，_、% 与反斜杠不必转义。按名字的
// key、再按 ID 的字节序排，游标是上一页最后一项的这两样。条件存不下时查询为 nil：什么都匹配不上。
func (v versioning) listQuery(db *gorm.DB, cursor string, limit int, prefix string, states []model.VersionState,
	approvedOnly bool) (*gorm.DB, int, error) {
	n, err := store.CheckQuery(cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	var afterKey, afterID string
	if cursor != "" {
		if err := decodeCursor(cursor, v.idents, &afterKey, &afterID); err != nil {
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
		db = db.Where("starts_with("+v.idents+".name_key, ?)", asciiLower(prefix))
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

// meta 是版本里由仓储管的字段，hook 与模板同名同义。
type meta struct {
	name, author, approver string
	state                  model.VersionState
	builtin                bool
}

// 下面的检查与内存实现的同名函数规则相同，由契约测试守着两边一致。

// versionID 是一个版本在审计对象、Subject 与报错里的写法：「ID@版本」，与第 1 层判定里的相同。
func versionID(id string, version int) string {
	return id + "@" + strconv.Itoa(version)
}

// checkKey 查收到的版本键：ID 过 model.ValidID，版本号从 1 起。不合的报 ErrInvalid，不当成找不到。
func checkKey(id string, version int) error {
	switch {
	case !model.ValidID(id):
		return badID(id)
	case version < 1:
		return errorf(store.ErrInvalid, "版本号要从 1 起，收到 %d", version)
	}
	return nil
}

// checkAuthor 查新版本的作者：审批人不能是作者（C22），没有作者这条就无从查起；作者要写进库里，还得存得下。
func checkAuthor(author string) error {
	if author == "" || !model.StorableText(author) {
		return errorf(store.ErrInvalid, "作者要是非空的可存文本，收到 %q", author)
	}
	return nil
}

// checkApprover 查审批人存得下。空的或就是作者的，由重跑的判定报出（activation.Approver）。
func checkApprover(approver string) error {
	if !model.StorableText(approver) {
		return errorf(store.ErrInvalid, "审批人 %q 存不下：不是合法的 UTF-8，或含 NUL", approver)
	}
	return nil
}

// normalizeName 按 model.NormalizeName 规范化名字；不合法时返回它的问题，Subject 换成 subj。
func normalizeName(raw string, subj model.Subject) (display, key string, ps []model.Problem) {
	display, key, err := model.NormalizeName(raw)
	var p model.Problem
	if errors.As(err, &p) {
		p.Subject = subj
		return "", "", []model.Problem{p}
	}
	return display, key, nil
}

// checkStep 查版本 what 能不能从 from 转出：内置的只读（C25），状态不对是不合规矩的转换，调用方重读之后再判断。
func checkStep(what string, m meta, from model.VersionState) error {
	switch {
	case m.builtin:
		return errorf(store.ErrImmutable, "%s 是内置的，只读", what)
	case m.state != from:
		return errorf(store.ErrConflict, "%s 现为 %s，这一步要从 %s 转出", what, m.state, from)
	}
	return nil
}

// checkDraft 查版本 what 是不是能改、能删的草稿：内置的只读（C25），已提交、已审批与已废弃的都不能改（C23）。
func checkDraft(what string, m meta) error {
	switch {
	case m.builtin:
		return errorf(store.ErrImmutable, "%s 是内置的，只读", what)
	case m.state != model.VersionDraft:
		return errorf(store.ErrImmutable, "%s 现为 %s，只有草稿能改、能删", what, m.state)
	}
	return nil
}

// checkSameMeta 查保存时带来的、由仓储管的字段与库里的相同：名字改用 Rename，状态只经状态转换改，作者在开版本时定下，
// 审批人由审批写，内置标记只由装载内置内容写。不同的报 ErrInvalid，不悄悄丢掉。
func checkSameMeta(what string, old, in meta) error {
	switch {
	case in.name != old.name:
		return errorf(store.ErrInvalid, "%s 的名字是 %q，收到的是 %q：改名要另走 Rename", what, old.name, in.name)
	case in != old:
		return errorf(store.ErrInvalid, "%s 的状态、作者、审批人与内置标记只由仓储写，保存时要原样带回", what)
	}
	return nil
}

// checkBuiltin 查装载的内置版本：要是内置的已审批版本，键合格式，作者与审批人存得下，名字已经是规范的写法（再规范化
// 一次不变，库里存的与程序自带的才比得上）。不合这些的是程序的错，报 ErrInvalid。返回名字的 key，以及名字不合法时的
// 问题（Subject 为 subj），由调用方与文本的问题一起报。
func checkBuiltin(id string, version int, m meta, subj model.Subject) (string, []model.Problem, error) {
	if err := checkKey(id, version); err != nil {
		return "", nil, err
	}
	switch {
	case !m.builtin || m.state != model.VersionApproved:
		return "", nil, errorf(store.ErrInvalid, "装载的内置内容要是内置的已审批版本，收到内置为 %v、状态为 %s", m.builtin, m.state)
	case !storable(m.author, m.approver):
		return "", nil, errorf(store.ErrInvalid, "内置内容的作者或审批人存不下：不是合法的 UTF-8，或含 NUL")
	}
	display, key, ps := normalizeName(m.name, subj)
	if len(ps) == 0 && display != m.name {
		return "", nil, errorf(store.ErrInvalid, "内置内容的名字 %q 不是规范的写法 %q", m.name, display)
	}
	return key, ps, nil
}

func notStorable(s model.Subject) model.Problem {
	return model.NewProblem("text_not_storable", s)
}

// storableAll 同 storable，收的是 catalog 里那些以字符串为底的类型。
func storableAll[S ~string](ss []S) bool {
	return !slices.ContainsFunc(ss, func(s S) bool { return !model.StorableText(string(s)) })
}

// declTextProblems 是参数声明里存不下的文本，每个声明一条，Path 为 param/<序号>，其余同 whole。
func declTextProblems(decls []model.ParamDecl, whole model.Subject) []model.Problem {
	var ps []model.Problem
	for i, d := range decls {
		ok := storable(d.Name, d.Label, string(d.Type), d.Default, d.Offer)
		for _, o := range d.Options {
			ok = ok && storable(o.Value, o.Label)
		}
		if !ok {
			s := whole
			s.Path = "param/" + strconv.Itoa(i+1)
			ps = append(ps, notStorable(s))
		}
	}
	return ps
}

// asciiLower 只把 ASCII 的大写字母转成小写，名字的 key 就是这样来的（model.NormalizeName）。strings.ToLower 按 Unicode
// 的规则转，会把开尔文符号 K（U+212A）转成 k，前缀就会匹配上本不该匹配的名字。
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
