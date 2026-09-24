// 本文件定义 task_events 表的 GORM 行结构体，与 internal/core/model.TaskEvent
// 的双向映射，以及供组合器收集给 AutoMigrate 的 Models()。对应实现计划任务 7
// 的前一半（event 聚合），依据 SPEC §2.5、§3.6、§12.8。
package event

import (
	"github.com/lober/lober/internal/core/model"
)

// row 是 task_events 表的一行，字段与列一一对应 schema/001_init.sql 的
// task_events 表（旧 schema 是本次迁移列名/类型的事实来源）。
//
// id 用 bigserial：ListEvents 按 id 排序而非 seq（见 store.go 的说明），
// conformance/store.go:1173 有一处断言依赖 id 单调递增，因此 id 的自增
// 属性不是可选的实现细节。GORM 对 int64 主键默认生成 bigserial，但计划
// 任务 7 明确要求不假设、要在测试里查 information_schema.columns 的
// column_default 确认（见 store_test.go 的 TestModels_IDColumnIsBigserial）。
//
// 唯一索引 idx_task_events_task_seq 建在 (task_id, seq) 上，无 WHERE 条件
// （与 tasks 表那三个 partial index 不同）。索引名在本文件是字面量字符串
// ——Go 的 struct tag 是字面常量，不能在 tag 里引用包外常量拼接——但必须与
// pgerr.IdxTaskEventsTaskSeq 的字面值一致：pgerr.Translate 未来补上 23505
// 分支时按 ConstraintName 判定，两处字面量不一致会让该分支静默失效（计划
// 任务 2 风险提示明确点出的隐患）。row_test.go 的
// TestIndexNameMatchesPgerrConstant 断言两处字面值相等，防止将来改名时
// 漏改一处。
type row struct {
	ID         int64  `gorm:"column:id;primaryKey"`
	TaskID     string `gorm:"column:task_id;not null"`
	Seq        int    `gorm:"column:seq;not null;index:idx_task_events_task_seq,unique"`
	TS         int64  `gorm:"column:ts;not null"`
	Phase      string `gorm:"column:phase;not null"`
	Level      string `gorm:"column:level;not null"`
	Message    string `gorm:"column:message;not null;default:''"`
	RC         int    `gorm:"column:rc;not null;default:0"`
	HookDigest string `gorm:"column:hook_digest;not null;default:''"`
	Source     string `gorm:"column:source;not null"`
}

// TableName 固定表名为 task_events，不依赖 GORM 的复数推导——推导规则在
// 表名本身已经是复数形式（event → events）时容易产生歧义，行结构体的类型名
// 是单数 row（包名 event 已经提供了聚合语境），显式声明避免任何猜测。
func (row) TableName() string {
	return "task_events"
}

// Models 返回本聚合的全部 GORM model，供组合器收集给 AutoMigrate（计划
// 任务 7「接口契约」：与任务 3 的 task 包同一形态，八个聚合包统一导出
// New(db) 与 Models() 两个符号）。
func Models() []any {
	return []any{&row{}}
}

// fromModel 把 model.TaskEvent 映射成待写入的行。id 不在映射范围内——
// TaskEvent 本身不含 id 字段（旧实现 sqlite/event.go 的注释已经说明理由：
// AppendEvent 的返回值不需要 id，ListEvents 的排序用途不需要暴露给
// domain 层），id 由数据库自增生成，映射方向上没有对应的输入。
func fromModel(e model.TaskEvent) row {
	return row{
		TaskID:     e.TaskID,
		Seq:        e.Seq,
		TS:         e.TS,
		Phase:      string(e.Phase),
		Level:      string(e.Level),
		Message:    e.Message,
		RC:         e.RC,
		HookDigest: e.HookDigest,
		Source:     string(e.Source),
	}
}

// toModel 是 fromModel 的逆操作。
func toModel(r row) model.TaskEvent {
	return model.TaskEvent{
		TaskID:     r.TaskID,
		Seq:        r.Seq,
		TS:         r.TS,
		Phase:      model.Phase(r.Phase),
		Level:      model.EventLevel(r.Level),
		Message:    r.Message,
		RC:         r.RC,
		HookDigest: r.HookDigest,
		Source:     model.EventSource(r.Source),
	}
}
