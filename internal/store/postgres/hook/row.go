// 本文件定义 hooks 表的行结构体、索引 tag、与 model.Hook 的双向映射。
// 对应实现计划任务 9（hook 聚合的前一半），依据 SPEC §2.2、§2.6、§2.7。
//
// 复合主键 (id, version)：同一 id 的每个版本都是独立行，GORM 用多个
// primaryKey tag 表达（设计文档 §3、计划任务 9 的「五处关键语义」第一条）。
package hook

import (
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// hookRow 是 hooks 表的行结构体，字段与列一一对应旧 schema
// （internal/store/sqlite/schema/001_init.sql（已移除）的 hooks 表）。
type hookRow struct {
	ID      string `gorm:"column:id;primaryKey"`
	Version int    `gorm:"column:version;primaryKey"`

	Name        string `gorm:"column:name"`
	Description string `gorm:"column:description"`

	Stage string `gorm:"column:stage"`
	// OSFamilies 存 JSON 数组（旧 schema 注释：os_families 存 JSON 数组，
	// 列名 os_families_json；计划文档任务 8 曾把它误描述成逗号分隔文本，
	// sqlite 版 hook.go 已指出这处矛盾并遵循 schema 的真实定义——本实现
	// 同样遵循 schema，不遵循计划文档的文字描述）。
	OSFamilies pgtype.JSON[[]model.OSFamily] `gorm:"column:os_families_json;type:jsonb"`

	Interpreter string `gorm:"column:interpreter"`

	Body string `gorm:"column:body"`
	// BodySHA256 上要有索引：GetHookByDigest 按它反查（渲染时只有 digest
	// 可用，task/template pin 住的是 digest 不是 id）。
	BodySHA256 string `gorm:"column:body_sha256;index:idx_hooks_digest"`

	ParamsSchema string                          `gorm:"column:params_schema"`
	Requires     pgtype.JSON[model.HookRequires] `gorm:"column:requires_json;type:jsonb"`

	Required   bool `gorm:"column:required"`
	TimeoutSec int  `gorm:"column:timeout_sec"`

	State    string `gorm:"column:state"`
	Author   string `gorm:"column:author"`
	Approver string `gorm:"column:approver"`
	// ApprovedAt 是「0 表示未设置」的时间列 → *time.Time，0 ↔ NULL
	// （计划任务 9 接口契约明确要求）。
	ApprovedAt *time.Time `gorm:"column:approved_at"`
}

// TableName 固定表名为 hooks，不依赖 GORM 的复数推导（计划任务 3 同一
// 约定对全部聚合包适用）。
func (hookRow) TableName() string {
	return "hooks"
}

// Models 返回本聚合的全部行结构体指针，供组合器收集给 AutoMigrate。
func Models() []any {
	return []any{&hookRow{}}
}

// fromModel 把 model.Hook 转换成待写入的行结构体。
func fromModel(h model.Hook) hookRow {
	return hookRow{
		ID:           h.ID,
		Version:      h.Version,
		Name:         h.Name,
		Description:  h.Description,
		Stage:        string(h.Stage),
		OSFamilies:   pgtype.JSON[[]model.OSFamily]{Val: h.OSFamilies},
		Interpreter:  h.Interpreter,
		Body:         h.Body,
		BodySHA256:   h.BodySHA256,
		ParamsSchema: h.ParamsSchema,
		Requires:     pgtype.JSON[model.HookRequires]{Val: h.Requires},
		Required:     h.Required,
		TimeoutSec:   h.TimeoutSec,
		State:        string(h.State),
		Author:       h.Author,
		Approver:     h.Approver,
		ApprovedAt:   pgtype.NullTimeFromUnix(h.ApprovedAt),
	}
}

// toModel 是 fromModel 的逆操作。
func (r hookRow) toModel() model.Hook {
	return model.Hook{
		ID:           r.ID,
		Version:      r.Version,
		Name:         r.Name,
		Description:  r.Description,
		Stage:        model.HookStage(r.Stage),
		OSFamilies:   r.OSFamilies.Val,
		Interpreter:  r.Interpreter,
		Body:         r.Body,
		BodySHA256:   r.BodySHA256,
		ParamsSchema: r.ParamsSchema,
		Requires:     r.Requires.Val,
		Required:     r.Required,
		TimeoutSec:   r.TimeoutSec,
		State:        model.HookState(r.State),
		Author:       r.Author,
		Approver:     r.Approver,
		ApprovedAt:   pgtype.UnixFromNullTime(r.ApprovedAt),
	}
}
