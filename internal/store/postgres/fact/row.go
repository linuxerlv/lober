// 本文件定义 task_facts 表的 GORM 行结构体，对应实现计划任务 7 后半段，
// 依据 SPEC §5.5、schema/001_init.sql（已移除） 的 task_facts 表。
//
// facts 是纯粹的 (task_id, key) -> value KV 存储，不对应 model 包里的
// 具名类型（见 internal/core/model/facts.go 文件头注释：该文件只定义
// 键命名空间常量，不定义值的结构）。因此本文件不做「行结构体 <-> model
// 类型」的双向映射——contract.FactStore 的方法签名本身就是
// map[string]string，行结构体只是 GORM 落库用的中间形态，直接在
// store.go 里逐行转换，不需要单独的 Row <-> map 转换函数。
package fact

import "time"

// factRow 对应 task_facts 表的一行。TableName 固定表名，不依赖 GORM 的
// 复数推导——复数推导对 "fact" 这种规则词恰好猜得对（facts），但依赖
// 一个「猜对」的行为而不是显式声明，是把偶然正确当成契约。
type factRow struct {
	// TaskID 与 Key 组成复合主键，对应 schema 里的
	// PRIMARY KEY (task_id, key)。
	TaskID string `gorm:"column:task_id;primaryKey"`
	Key    string `gorm:"column:key;primaryKey"`

	// Value 是 facts 的值本身，schema 里 NOT NULL DEFAULT ''——空字符串是
	// 合法值（某些 fact 键的取值本就允许为空），不是 SQL NULL 的替代品，
	// 所以本字段用 string 而不是 *string。
	Value string `gorm:"column:value;not null;default:''"`

	// UpdatedAt 是本行最后一次被合并写入的时刻。schema 里是
	// INTEGER NOT NULL（Unix 秒），恒有值——不存在「facts 行存在但
	// updated_at 未设置」这种状态，所以用 time.Time 而不是 *time.Time
	// （与 pgtype.NullTimeFromUnix 处理「可能尚未发生」的可空时间列不同，
	// 本列没有这种零值歧义）。
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

// TableName 固定表名为 task_facts，理由见本文件头注释。
func (factRow) TableName() string {
	return "task_facts"
}

// Models 返回本聚合包持有的全部行结构体，供组合器收集给 AutoMigrate
// （与 task/event 等其余聚合包同一形态，计划任务 7 接口契约）。
func Models() []any {
	return []any{&factRow{}}
}
