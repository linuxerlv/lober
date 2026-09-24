// 本文件定义 audit_log 表在 GORM 下的行结构体，对应实现计划任务 8 的
// audit 三分之一。列名、类型与索引以 internal/store/sqlite/schema/001_init.sql
// 第 174-181 行为事实来源，字段语义以 internal/core/contract/store.go 的
// AuditRecord 与 AuditStore 文档为事实来源。
package audit

import (
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// row 是 audit_log 表的 GORM 映射。字段顺序与旧 schema 的列声明顺序一致
// （id、ts、actor、action、target、detail_json），方便对照。
//
// id 用 bigserial 而不是旧 schema 的 INTEGER AUTOINCREMENT：交付判定与
// 计划任务 8 明确要求「audit_log.id 同样用 bigserial」——PostgreSQL 的
// serial 上限在高频写入场景下不如 bigserial 宽裕，且与其余聚合包
// （tasks.id 等）的整数主键风格保持一致。
//
// Detail 字段用 pgtype.JSON[string] 而不是裸 string 加 GORM 的
// serializer 标签：AuditRecord.Detail 本身是自由文本（不是结构化数据），
// 但按旧实现（internal/store/sqlite/audit.go）的约定，落库时仍需要
// JSON 编码进 detail_json 这个 jsonb 列——JSON.Value/Scan 对一个字符串
// 值编码/解码得到的正是"该字符串的 JSON 字面量"，与旧实现的
// encodeJSON(record.Detail)/decodeJSON(detailJSON, &rec.Detail) 语义
// 完全对应，含中文、引号、换行的原文都能原样往返。
type row struct {
	ID         int64               `gorm:"column:id;primaryKey"`
	TS         int64               `gorm:"column:ts;not null"`
	Actor      string              `gorm:"column:actor;not null"`
	Action     string              `gorm:"column:action;not null"`
	Target     string              `gorm:"column:target;not null;default:''"`
	DetailJSON pgtype.JSON[string] `gorm:"column:detail_json;type:jsonb;not null;default:'{}'"`
}

// TableName 固定表名为 audit_log，与旧 schema 一致——GORM 的默认复数化
// 规则会把 row 映射成 rows，必须显式指定。
func (row) TableName() string {
	return "audit_log"
}
