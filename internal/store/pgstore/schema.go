package pgstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// table 是本层一张表的期望结构。各项的写法照 PostgreSQL 规范化之后的输出（pg_get_indexdef、pg_get_expr、
// pg_get_constraintdef 的 pretty 形式）：比如写 <> 而不是 !=，字符串常量带上 ::text，IN 写成 = ANY (ARRAY[...])。
// 先建一次表、把真实的定义打出来，再照着写。
type table struct {
	name    string
	primary []string // 主键的列，按顺序
	indexes []index  // 主键以外的索引
	foreign []foreign
	checks  []check
}

type index struct {
	name    string
	unique  bool
	columns []string
	where   string // 部分索引的谓词，空表示没有
}

type foreign struct {
	name       string
	columns    []string
	refTable   string
	refColumns []string
	onDelete   string // RESTRICT、CASCADE、NO ACTION、SET NULL 或 SET DEFAULT
}

type check struct {
	name string
	def  string
}

// versionStates 是 hook_versions 与 template_versions 的 CHECK：与 model.VersionState 认的完全相同。
const versionStates = "CHECK (state = ANY (ARRAY['draft'::text, 'review'::text, 'approved'::text, 'deprecated'::text]))"

// unapproved 是未审批（草稿或待审批）的版本，每个身份同时最多一个。
const unapproved = "state = ANY (ARRAY['draft'::text, 'review'::text])"

// taskStates 是 tasks 的 CHECK：与 model.TaskState 认的完全相同（TaskState.Valid）。
const taskStates = "CHECK (state = ANY (ARRAY['pending'::text, 'booting'::text, 'installing'::text, 'rebooting'::text, " +
	"'initializing'::text, 'done'::text, 'failed'::text, 'cancelled'::text]))"

// openTaskIndex 是守着 C31 的部分唯一索引：一台主机同一时刻最多一个未结束的任务，结束了的（done、failed、cancelled，
// 与 model.TaskState.Finished 一致）不占位。建任务与推进撞上它时，tasks.go 按这个名字认出来，翻成 host_has_open_task。
const openTaskIndex = "idx_tasks_open"

// tables 是本层全部表的期望结构，Open 与 schema_test 共用这一份，翻译数据库错误时也按它认约束名。计划「表结构」一节
// 列的十张表都在这里。
var tables = []table{
	{
		name: "iso_sources", primary: []string{"iso_id", "role"},
		foreign: []foreign{{name: "fk_iso_sources_iso", columns: []string{"iso_id"}, refTable: "isos", refColumns: []string{"id"}, onDelete: "CASCADE"}},
		checks: []check{
			{"chk_iso_sources_rev", "CHECK (rev >= 1)"},
			{"chk_iso_sources_state", "CHECK (state = ANY (ARRAY['pending'::text, 'fetching'::text, 'fetched'::text, 'failed'::text, 'cancelled'::text]))"},
			{"chk_iso_sources_kind", "CHECK (kind = ANY (ARRAY['remote'::text, 'upload'::text, 'local'::text]))"},
			{"chk_iso_sources_fetched", "CHECK (fetched >= 0)"},
		},
	},
	{
		name: "iso_upload_ranges", primary: []string{"iso_id", "role", "start"},
		foreign: []foreign{{name: "fk_iso_upload_ranges_source", columns: []string{"iso_id", "role"}, refTable: "iso_sources", refColumns: []string{"iso_id", "role"}, onDelete: "CASCADE"}},
		checks:  []check{{"chk_iso_upload_ranges_start", "CHECK (start >= 0)"}, {"chk_iso_upload_ranges_length", "CHECK (length > 0)"}},
	},
	{
		name:    "hosts",
		primary: []string{"sn"},
		checks:  []check{{"chk_hosts_rev", "CHECK (rev >= 1)"}},
	},
	{
		name:    "isos",
		primary: []string{"id"},
		indexes: []index{
			// C4：内容相同的只导入一次。空的 SHA256 与导入失败的 ISO 不占位
			{name: "idx_isos_sha256", unique: true, columns: []string{"sha256"},
				where: "sha256 <> ''::text AND state <> 'failed'::text"},
		},
		checks: []check{
			{"chk_isos_rev", "CHECK (rev >= 1)"},
			{"chk_isos_state", "CHECK (state = ANY (ARRAY['acquiring'::text, 'importing'::text, 'ready'::text, " +
				"'failed'::text, 'deprecated'::text]))"},
		},
	},
	{
		// hook 的身份：名字放在身份上，各版本共用，唯一约束按名字的 key（C12），用户建的与内置的共用这个名字空间
		name:    "hooks",
		primary: []string{"id"},
		indexes: []index{{name: "idx_hooks_name_key", unique: true, columns: []string{"name_key"}}},
	},
	{
		// 外键是 RESTRICT：身份只在它的最后一个版本删掉时一起删（DeleteHookDraft），已审批与已废弃的版本永不删除，
		// 它们的身份也就一直在
		name:    "hook_versions",
		primary: []string{"hook_id", "version"},
		indexes: []index{{name: "idx_hook_versions_unapproved", unique: true, columns: []string{"hook_id"}, where: unapproved}},
		foreign: []foreign{{name: "fk_hook_versions_hook", columns: []string{"hook_id"},
			refTable: "hooks", refColumns: []string{"id"}, onDelete: "RESTRICT"}},
		checks: []check{
			{"chk_hook_versions_version", "CHECK (version >= 1)"},
			{"chk_hook_versions_state", versionStates},
			{"chk_hook_versions_rev", "CHECK (rev >= 1)"},
		},
	},
	{
		name:    "templates",
		primary: []string{"id"},
		indexes: []index{{name: "idx_templates_name_key", unique: true, columns: []string{"name_key"}}},
	},
	{
		name:    "template_versions",
		primary: []string{"template_id", "version"},
		indexes: []index{{name: "idx_template_versions_unapproved", unique: true, columns: []string{"template_id"}, where: unapproved}},
		foreign: []foreign{{name: "fk_template_versions_template", columns: []string{"template_id"},
			refTable: "templates", refColumns: []string{"id"}, onDelete: "RESTRICT"}},
		checks: []check{
			{"chk_template_versions_version", "CHECK (version >= 1)"},
			{"chk_template_versions_state", versionStates},
			{"chk_template_versions_rev", "CHECK (rev >= 1)"},
		},
	},
	{
		// 模板的每个版本引用了哪些 hook 版本，只有删 hook 草稿时查它（DeleteHookDraft）；审批模板与建任务不读它，用
		// store.HookRefs 现算。刻意不对 hook_versions 建外键：草稿可以引用不存在的版本，激活时才报 hook_ref_missing
		name:    "template_hook_refs",
		primary: []string{"template_id", "template_version", "hook_id", "hook_version"},
		indexes: []index{{name: "idx_template_hook_refs_hook", columns: []string{"hook_id", "hook_version"}}},
		foreign: []foreign{{
			name: "fk_template_hook_refs_template", columns: []string{"template_id", "template_version"},
			refTable: "template_versions", refColumns: []string{"template_id", "version"}, onDelete: "CASCADE",
		}},
	},
	{
		// 外键都是 RESTRICT：已建的任务要一直查得到它引用的主机、ISO 与模板版本（C26），任务结束了也一样
		name:    "tasks",
		primary: []string{"id"},
		indexes: []index{
			{name: "idx_tasks_created_at", columns: []string{"created_at"}},
			// C31。建任务时先在事务里对锁住的主机重跑准入，它是最后一道防线；推进回未结束的状态时靠它挡住
			{name: openTaskIndex, unique: true, columns: []string{"host_sn"},
				where: "state <> ALL (ARRAY['done'::text, 'failed'::text, 'cancelled'::text])"},
			{name: "idx_tasks_state", columns: []string{"state"}},
		},
		foreign: []foreign{
			{name: "fk_tasks_host", columns: []string{"host_sn"}, refTable: "hosts", refColumns: []string{"sn"}, onDelete: "RESTRICT"},
			{name: "fk_tasks_iso", columns: []string{"iso_id"}, refTable: "isos", refColumns: []string{"id"}, onDelete: "RESTRICT"},
			{name: "fk_tasks_template", columns: []string{"template_id", "template_version"},
				refTable: "template_versions", refColumns: []string{"template_id", "version"}, onDelete: "RESTRICT"},
		},
		checks: []check{
			{"chk_tasks_created_at", "CHECK (created_at > 0)"},
			{"chk_tasks_rev", "CHECK (rev >= 1)"},
			{"chk_tasks_state", taskStates},
		},
	},
	{
		// 事件的主键与去重用的索引都是复合的：旧系统漏标一个列的 tag，把 (task_id, seq) 建成了 (seq)，不同任务的事件被当成
		// 重复丢掉。空的 Key 不去重，不占位
		name:    "task_events",
		primary: []string{"task_id", "seq"},
		indexes: []index{{name: "idx_task_events_key", unique: true, columns: []string{"task_id", "key"}, where: "key <> ''::text"}},
		foreign: []foreign{{name: "fk_task_events_task", columns: []string{"task_id"},
			refTable: "tasks", refColumns: []string{"id"}, onDelete: "RESTRICT"}},
		checks: []check{
			{"chk_task_events_at", "CHECK (at > 0)"},
			{"chk_task_events_seq", "CHECK (seq >= 1)"},
		},
	},
	{
		name:    "audit_log",
		primary: []string{"id"},
		indexes: []index{
			{name: "idx_audit_log_actor", columns: []string{"actor"}},
			{name: "idx_audit_log_at", columns: []string{"at"}},
			{name: "idx_audit_log_target", columns: []string{"target_kind", "target_id"}},
		},
		checks: []check{{"chk_audit_log_at", "CHECK (at > 0)"}},
	},
}

// mismatch 是表结构断言查出的一处不一致。
type mismatch struct {
	table   string
	item    string // 索引名或约束名；主键是「主键」
	problem string // 差在哪里，以及该怎么改
}

func (m mismatch) String() string {
	return m.table + " " + m.item + "：" + m.problem
}

// describeMismatches 把不一致逐条写成一行，供 Open 的错误用。
func describeMismatches(ms []mismatch) string {
	lines := make([]string, len(ms))
	for i, m := range ms {
		lines[i] = "  - " + m.String()
	}
	return strings.Join(lines, "\n")
}

// 实际的结构从系统目录查，都限定在当前 schema：库里还有别人的 schema（几个代理同时跑测试），同名的表不算这一个的。
// 查的是 pg_index 与 pg_constraint 本身，而不是 pg_indexes 里拼好的 CREATE INDEX：那一句带着 schema 名，还得再拆
// 出列与谓词。列按位置排、以「, 」连接，比对的就是列的集合与顺序。

const indexQuery = `
SELECT t.relname::text AS tbl, i.relname::text AS name, x.indisunique AS uniq, x.indisprimary AS prim,
	(SELECT string_agg(pg_get_indexdef(x.indexrelid, k, true), ', ' ORDER BY k)
		FROM generate_series(1, x.indnkeyatts) AS k) AS cols,
	COALESCE(pg_get_expr(x.indpred, x.indrelid, true), '') AS pred
FROM pg_index x
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_class t ON t.oid = x.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = current_schema() AND t.relname IN ?`

// constraintQuery 只查外键与 CHECK：主键与唯一约束背后都有索引，由 indexQuery 查；NOT NULL 在 PostgreSQL 18 起
// 也进了 pg_constraint，不归断言管。被引用的表不在当前 schema 时带上 schema 名，同名的别处的表因此对不上。
const constraintQuery = `
SELECT t.relname::text AS tbl, c.conname::text AS name, c.contype::text AS kind,
	pg_get_constraintdef(c.oid, true) AS def,
	COALESCE((SELECT string_agg(a.attname::text, ', ' ORDER BY k.ord)
		FROM unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum), '') AS cols,
	COALESCE(CASE WHEN r.relnamespace = t.relnamespace THEN r.relname::text
		ELSE rn.nspname::text || '.' || r.relname::text END, '') AS ref_tbl,
	COALESCE((SELECT string_agg(a.attname::text, ', ' ORDER BY k.ord)
		FROM unnest(c.confkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = k.attnum), '') AS ref_cols,
	c.confdeltype::text AS on_delete
FROM pg_constraint c
JOIN pg_class t ON t.oid = c.conrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
LEFT JOIN pg_class r ON r.oid = c.confrelid
LEFT JOIN pg_namespace rn ON rn.oid = r.relnamespace
WHERE n.nspname = current_schema() AND t.relname IN ? AND c.contype IN ('c', 'f')`

// actualIndex 是查出来的一个索引。
type actualIndex struct {
	Tbl, Name  string
	Uniq, Prim bool
	Cols, Pred string
}

// actualConstraint 是查出来的一个外键或 CHECK。Kind 是 f（外键）或 c（CHECK）；OnDelete 是 pg_constraint 里的
// 一个字母，见 onDeleteActions。
type actualConstraint struct {
	Tbl, Name, Kind, Def            string
	Cols, RefTbl, RefCols, OnDelete string
}

// onDeleteActions 把 pg_constraint.confdeltype 的字母换成 SQL 的写法。
var onDeleteActions = map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}

// checkSchema 查当前 schema 里 want 列出的每张表，返回全部不一致，一致时返回 nil。不一致按 want 里表的顺序排，
// 每张表依次是主键、索引、外键、CHECK，多出来的排在期望的后面、按名字排。
func checkSchema(ctx context.Context, db *gorm.DB, want []table) ([]mismatch, error) {
	names := make([]string, len(want))
	for i, t := range want {
		names[i] = t.name
	}
	var idx []actualIndex
	if err := db.WithContext(ctx).Raw(indexQuery, names).Scan(&idx).Error; err != nil {
		return nil, fmt.Errorf("pgstore：查索引：%w", err)
	}
	var cons []actualConstraint
	if err := db.WithContext(ctx).Raw(constraintQuery, names).Scan(&cons).Error; err != nil {
		return nil, fmt.Errorf("pgstore：查约束：%w", err)
	}
	var out []mismatch
	for _, t := range want {
		out = append(out, compareTable(t, idx, cons)...)
	}
	return out, nil
}

// compareTable 比对一张表。idx 与 cons 是查出来的全部，只看这张表的。
func compareTable(t table, idx []actualIndex, cons []actualConstraint) []mismatch {
	var out []mismatch
	add := func(item, format string, args ...any) {
		out = append(out, mismatch{table: t.name, item: item, problem: fmt.Sprintf(format, args...)})
	}

	indexes := map[string]actualIndex{} // 主键以外的
	var pk *actualIndex
	for _, a := range idx {
		switch {
		case a.Tbl != t.name:
		case a.Prim:
			pk = &a
		default:
			indexes[a.Name] = a
		}
	}
	// 主键的名字也要比：translate 按名字认出主键撞车（duplicateFields 里的 hosts_pkey），名字对不上，撞上已登记的 SN 只会报出
	// 原样的数据库错误。列不对时只报列：照那条改法重建的主键，名字就是 PostgreSQL 默认的「表名_pkey」
	wantPK, wantPKName := strings.Join(t.primary, ", "), t.name+"_pkey"
	switch {
	case pk == nil:
		add("主键", "没有主键，应为 (%s)。主键只在建表时建，AutoMigrate 不会补：手工 ALTER TABLE %s ADD PRIMARY KEY (%s)",
			wantPK, t.name, wantPK)
	case pk.Cols != wantPK:
		add("主键", "主键 %s 是 (%s)，应为 (%s)。AutoMigrate 不改已有的主键：先 ALTER TABLE %s DROP CONSTRAINT %s，"+
			"再 ALTER TABLE %s ADD PRIMARY KEY (%s)", pk.Name, pk.Cols, wantPK, t.name, pk.Name, t.name, wantPK)
	case pk.Name != wantPKName:
		add("主键", "主键叫 %s，应为 %s：翻译数据库错误时按这个名字认出主键撞车。AutoMigrate 不改已有约束的名字："+
			"手工 ALTER TABLE %s RENAME CONSTRAINT %s TO %s", pk.Name, wantPKName, t.name, pk.Name, wantPKName)
	}

	for _, w := range t.indexes {
		a, ok := indexes[w.name]
		delete(indexes, w.name)
		want := describeIndex(w.unique, strings.Join(w.columns, ", "), w.where)
		switch {
		case !ok:
			add(w.name, "缺少这个索引，应为 %s。AutoMigrate 按名字补建缺了的索引，启动时还缺，说明行类型的 gorm tag 与 "+
				"schema.go 的期望值对不上", want)
		case describeIndex(a.Uniq, a.Cols, a.Pred) != want:
			add(w.name, "现在是 %s，应为 %s。同名的索引已经存在时 AutoMigrate 不会改它：先 DROP INDEX %s，再重新启动，"+
				"由 AutoMigrate 按期望值重建", describeIndex(a.Uniq, a.Cols, a.Pred), want, w.name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(indexes)) {
		a := indexes[name]
		add(name, "多出来的索引 %s，期望值里没有它。是手工建的就 DROP INDEX %s；是行类型新加的 tag，先把它登记进 "+
			"schema.go 的期望值", describeIndex(a.Uniq, a.Cols, a.Pred), name)
	}

	constraints := map[string]actualConstraint{}
	for _, c := range cons {
		if c.Tbl == t.name {
			constraints[c.Name] = c
		}
	}
	expect := func(name, want string) {
		a, ok := constraints[name]
		delete(constraints, name)
		switch {
		case !ok:
			add(name, "缺少这个约束，应为 %s。AutoMigrate 按名字补建缺了的约束，启动时还缺，说明行类型的 gorm tag 与 "+
				"schema.go 的期望值对不上", want)
		case describeConstraint(a) != want:
			add(name, "现在是 %s，应为 %s。同名的约束已经存在时 AutoMigrate 不会改它：先 ALTER TABLE %s DROP CONSTRAINT %s，"+
				"再重新启动，由 AutoMigrate 按期望值重建", describeConstraint(a), want, t.name, name)
		}
	}
	for _, f := range t.foreign {
		expect(f.name, describeForeign(strings.Join(f.columns, ", "), f.refTable, strings.Join(f.refColumns, ", "), f.onDelete))
	}
	for _, c := range t.checks {
		expect(c.name, c.def)
	}
	for _, name := range slices.Sorted(maps.Keys(constraints)) {
		add(name, "多出来的约束 %s，期望值里没有它。是手工建的就 ALTER TABLE %s DROP CONSTRAINT %s；是行类型新加的 tag，"+
			"先把它登记进 schema.go 的期望值", describeConstraint(constraints[name]), t.name, name)
	}
	return out
}

// describeIndex 是索引在说明里的写法：唯一的以 UNIQUE 开头，接着是括号里的列，部分索引再接 WHERE 与谓词。比对也
// 比它，唯一性、列的集合与顺序、谓词一项不同就对不上。
func describeIndex(unique bool, cols, where string) string {
	s := "(" + cols + ")"
	if unique {
		s = "UNIQUE " + s
	}
	if where != "" {
		s += " WHERE " + where
	}
	return s
}

// describeForeign 是外键在说明里的写法，与 pg_get_constraintdef 的相仿，删除时的动作总是写出来。
func describeForeign(cols, refTable, refCols, onDelete string) string {
	return fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s) ON DELETE %s", cols, refTable, refCols, onDelete)
}

// describeConstraint 是查出来的约束在说明里的写法：外键按列、被引用的表与列、删除时的动作拼出来；CHECK 用
// pg_get_constraintdef 给的定义。
func describeConstraint(c actualConstraint) string {
	if c.Kind == "f" {
		return describeForeign(c.Cols, c.RefTbl, c.RefCols, onDeleteActions[c.OnDelete])
	}
	return c.Def
}
