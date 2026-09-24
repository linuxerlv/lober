// 本文件定义 tasks 表的行结构体、索引 tag、与 model.Task 的双向映射，
// 对应实现计划任务 3、4、5（三个任务合并交付），依据设计文档 §5、§6。
//
// 列名、类型与顺序对应 internal/store/sqlite/schema/001_init.sql（已移除）的
// tasks 表（旧 schema 是列名与类型的事实来源，本次迁移前不删）。
package task

import (
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// taskRow 是 tasks 表在 GORM 侧的行结构体。字段分组与顺序对齐
// model.Task，方便对照两者是否漏写。
//
// 三个唯一索引里只有 idx_tasks_sn_active 的 WHERE 含逗号，需要反斜杠
// 转义（GORM 按逗号切 tag 选项，schema/utils.go:23-26 的 ParseTagSetting
// 支持尾部反斜杠跨过分隔符）。这是全项目风险最高的一处：转义写错时
// GORM 不报错，只是把 WHERE 截断或丢掉，索引退化成无条件唯一索引——
// 症状不是测试失败，而是「装完的机器再也不能重装」。见 store_test.go
// 里 TestCreateTask_SameSNAfterDoneSucceeds 这条硬性回归测试。
type taskRow struct {
	// 标识与创建信息
	ID        string    `gorm:"column:id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	CreatedBy string    `gorm:"column:created_by;not null;default:''"`

	// 三个查找键，按 SN → MAC → UUID 优先级回退匹配（§4.3）。
	// idx_tasks_sn_active：同一 SN 只允许一个非终态任务（partial unique
	// index），WHERE 含逗号，必须反斜杠转义。
	//
	// 转义要写两个反斜杠 `\\,`，不是计划文档示例里的单个 `\,`——单反斜杠
	// 在 Go 的 struct tag 语法里不是合法转义序列（reflect.StructTag.Lookup
	// 内部对整个带引号的 tag 值调用 strconv.Unquote，`\,` 会让 Unquote
	// 报 "invalid syntax"，导致 Lookup 直接返回 ok=false——整条 gorm tag
	// 静默消失，比设计文档 §6 描述的"WHERE 被截断"更严重：不是索引退化，
	// 是整个字段的 tag 从未被 GORM 看到）。用 `\\,` 时 Unquote 先把
	// 两个反斜杠还原成一个，GORM 的 schema.ParseTagSetting（按单个反斜杠
	// 结尾识别"跨过分隔符继续拼接"）再吃掉那一个反斜杠，最终效果才是
	// WHERE 子句里出现一个逗号。已用 reflect.StructTag.Lookup 实测验证：
	// `\,` 令 Lookup 返回 ok=false，`\\,` 令 Lookup 返回 ok=true 且值里
	// 恰好保留一个反斜杠——即 GORM 解析器期望的输入。GORM 自己的测试
	// fixture（schema/index_test.go 的 UserIndex.Name5/Name6）同样用
	// `\\,`，印证这不是本包一厂的解读。
	HostSN   string `gorm:"column:host_sn;not null;default:'';index:idx_tasks_sn_active,unique,where:state NOT IN ('done'\\,'failed') AND host_sn != ''"`
	HostMAC  string `gorm:"column:host_mac;not null;default:'';index:idx_tasks_host_mac"`
	HostUUID string `gorm:"column:host_uuid;not null;default:'';index:idx_tasks_host_uuid"`

	// 三项 pin 引用（§2.6）
	TemplateID      string             `gorm:"column:template_id;not null"`
	TemplateVersion int                `gorm:"column:template_version;not null"`
	ArtifactSetID   string             `gorm:"column:artifact_set_id;not null"`
	HookDigests     pgtype.StringSlice `gorm:"column:hook_digests_json;type:jsonb"`

	// 表单与决策参数
	FormData       pgtype.StringMap `gorm:"column:form_data_json;type:jsonb"`
	ResolvedParams pgtype.StringMap `gorm:"column:resolved_params_json;type:jsonb"`

	// 状态与并发控制
	State          model.TaskState `gorm:"column:state;not null"`
	StateUpdatedAt time.Time       `gorm:"column:state_updated_at;not null"`
	Version        int             `gorm:"column:version;not null;default:1"`

	CurrentPhase   model.Phase `gorm:"column:current_phase;not null;index:idx_tasks_phase_entered,priority:1"`
	PhaseEnteredAt time.Time   `gorm:"column:phase_entered_at;not null;index:idx_tasks_phase_entered,priority:2"`

	AttemptCount int        `gorm:"column:attempt_count;not null;default:0"`
	LastSeq      int        `gorm:"column:last_seq;not null;default:0"`
	LastSignalAt *time.Time `gorm:"column:last_signal_at"`

	// 凭据。两个 token 各自的 partial unique index，条件是非空——避免
	// token 冲突被误发给两个任务。
	Token             string     `gorm:"column:token;not null;default:'';index:idx_tasks_token,unique,where:token != ''"`
	TokenExpiresAt    *time.Time `gorm:"column:token_expires_at"`
	FinalizeToken     string     `gorm:"column:finalize_token;not null;default:'';index:idx_tasks_finalize_token,unique,where:finalize_token != ''"`
	FinalizeExpiresAt *time.Time `gorm:"column:finalize_expires_at"`

	// 失败详情
	FailureReason model.FailureReason               `gorm:"column:failure_reason;not null;default:''"`
	FailureDetail pgtype.JSON[*model.FailureDetail] `gorm:"column:failure_detail_json;type:jsonb"`
}

// TableName 固定表名为 "tasks"，不依赖 GORM 的复数推导——GORM 默认会把
// taskRow 推成 "task_rows"，与旧 schema 的表名不一致。
func (taskRow) TableName() string {
	return "tasks"
}

// Models 返回供组合器收集给 AutoMigrate 的行结构体指针清单。
func Models() []any {
	return []any{&taskRow{}}
}

// taskFromModel 把 model.Task 映射成行结构体，供写入路径使用。
//
// 四个可空时间列（LastSignalAt、TokenExpiresAt、FinalizeExpiresAt）用
// pgtype.NullTimeFromUnix 转换：model 侧的 0 表示「尚未发生」，映射为
// nil（落地 SQL NULL），不能写成 time.Unix(0,0)（1970-01-01）留假数据
// 在库里（设计文档 §5）。
func taskFromModel(t model.Task) taskRow {
	return taskRow{
		ID:        t.ID,
		CreatedAt: time.Unix(t.CreatedAt, 0).UTC(),
		CreatedBy: t.CreatedBy,

		HostSN:   t.HostSN,
		HostMAC:  t.HostMAC,
		HostUUID: t.HostUUID,

		TemplateID:      t.TemplateID,
		TemplateVersion: t.TemplateVersion,
		ArtifactSetID:   t.ArtifactSetID,
		HookDigests:     pgtype.StringSlice(t.HookDigests),

		FormData:       pgtype.StringMap(t.FormData),
		ResolvedParams: pgtype.StringMap(t.ResolvedParams),

		State:          t.State,
		StateUpdatedAt: time.Unix(t.StateUpdatedAt, 0).UTC(),
		Version:        t.Version,

		CurrentPhase:   t.CurrentPhase,
		PhaseEnteredAt: time.Unix(t.PhaseEnteredAt, 0).UTC(),

		AttemptCount: t.AttemptCount,
		LastSeq:      t.LastSeq,
		LastSignalAt: pgtype.NullTimeFromUnix(t.LastSignalAt),

		Token:             t.Token,
		TokenExpiresAt:    pgtype.NullTimeFromUnix(t.TokenExpiresAt),
		FinalizeToken:     t.FinalizeToken,
		FinalizeExpiresAt: pgtype.NullTimeFromUnix(t.FinalizeExpiresAt),

		FailureReason: t.FailureReason,
		FailureDetail: pgtype.JSON[*model.FailureDetail]{Val: t.FailureDetail},
	}
}

// toModel 把行结构体映射回 model.Task，供读取路径使用。是
// taskFromModel 的逆操作。
func (r taskRow) toModel() model.Task {
	return model.Task{
		ID:        r.ID,
		CreatedAt: r.CreatedAt.Unix(),
		CreatedBy: r.CreatedBy,

		HostSN:   r.HostSN,
		HostMAC:  r.HostMAC,
		HostUUID: r.HostUUID,

		TemplateID:      r.TemplateID,
		TemplateVersion: r.TemplateVersion,
		ArtifactSetID:   r.ArtifactSetID,
		HookDigests:     []string(r.HookDigests),

		FormData:       map[string]string(r.FormData),
		ResolvedParams: map[string]string(r.ResolvedParams),

		State:          r.State,
		StateUpdatedAt: r.StateUpdatedAt.Unix(),
		Version:        r.Version,

		CurrentPhase:   r.CurrentPhase,
		PhaseEnteredAt: r.PhaseEnteredAt.Unix(),

		AttemptCount: r.AttemptCount,
		LastSeq:      r.LastSeq,
		LastSignalAt: pgtype.UnixFromNullTime(r.LastSignalAt),

		Token:             r.Token,
		TokenExpiresAt:    pgtype.UnixFromNullTime(r.TokenExpiresAt),
		FinalizeToken:     r.FinalizeToken,
		FinalizeExpiresAt: pgtype.UnixFromNullTime(r.FinalizeExpiresAt),

		FailureReason: r.FailureReason,
		FailureDetail: r.FailureDetail.Val,
	}
}
