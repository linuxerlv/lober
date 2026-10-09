package pgstore

import "gorm.io/gorm"

// 本文件是给外部测试包 pgstore_test 开的口子（Go 的惯用写法，文件名照例叫 export_test.go）。pgstore 的测试都写在外部
// 测试包里，才能 import 测试夹具 pgtest：pgtest 依赖 pgstore，包内测试再 import 它就成环。这里的声明只在 go test 时编进
// pgstore，生产代码看不到，也不进 pgstore 的 API。
//
// 名字都以 ForTest 结尾，用的地方一眼看得出是测试专用。变量与常量都直接指向包内的那一份，不另做包装：白盒断言查的、
// 表结构测试测的仍是包内的 tables、models、checkSchema 本身。包内的类型字段不导出，外部测试包读不到也写不出字面量，
// 所以另给几个只取字段、只拼字面量的方法与函数，里面不加判断。

// DBForTest 是仓储的 gorm 句柄。白盒断言经它直接查接口看不到的列（last_seq、last_version、template_hook_refs），或者
// 不经仓储改库里的数据造出反例。
func (s *Store) DBForTest() *gorm.DB { return s.db }

// BatchSizeForTest 是 batchSize：分批的用例要恰好多出一批。
const BatchSizeForTest = batchSize

// HookIdentsForTest 与 TemplateIdentsForTest 是两张身份表的表名：wantLastVersion 直接查上面的 last_version。
var (
	HookIdentsForTest     = hookTables.idents
	TemplateIdentsForTest = templateTables.idents
)

// 下面是表结构断言（schema.go）、查未结束任务的结束状态（tasks.go）与数据库错误的翻译（errors.go）：schema_test.go 测的
// 就是它们本身。

type (
	TableForTest    = table
	MismatchForTest = mismatch
)

var (
	TablesForTest             = tables
	ModelsForTest             = models
	CheckSchemaForTest        = checkSchema
	DescribeMismatchesForTest = describeMismatches
	DuplicateFieldsForTest    = duplicateFields
	FinishedStatesForTest     = finishedStates
	TranslateForTest          = translate
)

const (
	UnapprovedForTest    = unapproved
	OpenTaskIndexForTest = openTaskIndex
)

// NameForTest 是 t 的表名。
func (t table) NameForTest() string { return t.name }

// IndexesForTest 是 t 的期望值里主键以外的索引：索引名 → 是不是唯一索引。
func (t table) IndexesForTest() map[string]bool {
	m := make(map[string]bool, len(t.indexes))
	for _, ix := range t.indexes {
		m[ix.name] = ix.unique
	}
	return m
}

// FieldsForTest 是 m 的三个字段：表、项（索引名或约束名，主键是「主键」）、差在哪里与该怎么改。
func (m mismatch) FieldsForTest() (tbl, item, problem string) { return m.table, m.item, m.problem }

// NewTableForTest 拼一张只有主键与外键的表的期望结构。
func NewTableForTest(name string, primary []string, fks ...foreign) table {
	return table{name: name, primary: primary, foreign: fks}
}

// NewForeignForTest 拼一个外键的期望结构，参数依次是 foreign 的各个字段。
func NewForeignForTest(name string, columns []string, refTable string, refColumns []string, onDelete string) foreign {
	return foreign{name: name, columns: columns, refTable: refTable, refColumns: refColumns, onDelete: onDelete}
}
