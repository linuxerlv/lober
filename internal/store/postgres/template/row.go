// 本文件定义 templates 与 template_hook_refs 两张表的 GORM 行结构体，
// 与 model.Template/model.HookRef 之间的双向映射。对应实现计划任务 9 的
// 一半（template 聚合），依据 SPEC §2.3、§2.6、§2.7。
//
// 两张表归一个包：TemplateStore 这一个接口同时覆盖模板与它的 hook 引用
// 列表，切分依据是契约边界不是表数量（设计文档 §3）。
//
// 列名与类型的事实来源是 internal/store/sqlite/schema/001_init.sql：
//
//	CREATE TABLE templates (
//	    id                      TEXT NOT NULL,
//	    version                 INTEGER NOT NULL,
//	    name                    TEXT NOT NULL,
//	    artifact_set_id         TEXT NOT NULL,
//	    answer_template         TEXT NOT NULL,
//	    kernel_args             TEXT NOT NULL DEFAULT '',
//	    disk_policy             TEXT NOT NULL,
//	    on_failure              TEXT NOT NULL,
//	    required_form_fields_json TEXT NOT NULL DEFAULT '[]',
//	    state                   TEXT NOT NULL,
//	    PRIMARY KEY (id, version)
//	);
//
//	CREATE TABLE template_hook_refs (
//	    template_id         TEXT NOT NULL,
//	    template_version    INTEGER NOT NULL,
//	    hook_id             TEXT NOT NULL,
//	    hook_digest         TEXT NOT NULL,
//	    stage               TEXT NOT NULL,
//	    ref_order           INTEGER NOT NULL,
//	    required            INTEGER NOT NULL DEFAULT 0,
//	    params_defaults_json TEXT NOT NULL DEFAULT '{}'
//	);
package template

import (
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// templateRow 是 templates 表的 GORM 映射。主键是复合的 (id, version)——
// 同一 id 的每个版本都是独立行，pin 住具体版本与取最新版都要能按这两列
// 精确查询。
type templateRow struct {
	ID      string `gorm:"column:id;primaryKey"`
	Version int    `gorm:"column:version;primaryKey"`

	Name           string `gorm:"column:name;not null"`
	ArtifactSetID  string `gorm:"column:artifact_set_id;not null"`
	AnswerTemplate string `gorm:"column:answer_template;not null"`
	KernelArgs     string `gorm:"column:kernel_args;not null;default:''"`
	DiskPolicy     string `gorm:"column:disk_policy;not null"`
	OnFailure      string `gorm:"column:on_failure;not null"`

	// RequiredFormFields 存 JSON 数组，用 pgtype.StringSlice 承载
	// （jsonb 列），nil ↔ SQL NULL、空 slice ↔ JSON "[]"。
	RequiredFormFields pgtype.StringSlice `gorm:"column:required_form_fields_json;type:jsonb;default:'[]'"`

	State string `gorm:"column:state;not null"`
}

// TableName 固定表名为 templates，不用 GORM 的复数推导（与其它聚合包同一
// 约定：显式声明让表名不随 GORM 版本的推断规则变化而漂移）。
func (templateRow) TableName() string {
	return "templates"
}

// toModel 把 GORM row 转换成 model.Template，不含 HookRefs——hook 引用
// 列表由 store.go 里的 loadHookRefs 单独查询后拼装（模板本体与引用列表
// 是两张表，行结构体的转换职责只到「自己那张表的列」为止）。
func (r templateRow) toModel() model.Template {
	return model.Template{
		ID:                 r.ID,
		Name:               r.Name,
		ArtifactSetID:      r.ArtifactSetID,
		AnswerTemplate:     r.AnswerTemplate,
		KernelArgs:         r.KernelArgs,
		DiskPolicy:         model.DiskPolicy(r.DiskPolicy),
		OnFailure:          model.OnFailurePolicy(r.OnFailure),
		RequiredFormFields: []string(r.RequiredFormFields),
		Version:            r.Version,
		State:              model.TemplateState(r.State),
	}
}

// fromModel 把 model.Template 转换成 GORM row（不含 HookRefs），供写入
// 使用。
func fromModel(tpl model.Template) templateRow {
	return templateRow{
		ID:                 tpl.ID,
		Version:            tpl.Version,
		Name:               tpl.Name,
		ArtifactSetID:      tpl.ArtifactSetID,
		AnswerTemplate:     tpl.AnswerTemplate,
		KernelArgs:         tpl.KernelArgs,
		DiskPolicy:         string(tpl.DiskPolicy),
		OnFailure:          string(tpl.OnFailure),
		RequiredFormFields: pgtype.StringSlice(tpl.RequiredFormFields),
		State:              string(tpl.State),
	}
}

// templateHookRefRow 是 template_hook_refs 表的 GORM 映射。本表**无主键**
// ——它是模板对 hook 版本的引用列表，不是被寻址的实体（契约文档明确指出
// 「无主键（是引用列表）」）。按 (template_id, template_version) 建索引，
// 供 GetTemplate 取回某模板版本的全部引用。
type templateHookRefRow struct {
	// TemplateID/TemplateVersion 组成外键式的关联，不是主键——同一
	// (template_id, template_version, hook_id) 理论上可能出现多条（同一
	// hook 在同一模板里按不同 stage 各引用一次），本表刻意不加唯一约束，
	// 交给应用层的写入顺序保证语义正确。
	TemplateID      string `gorm:"column:template_id;index:idx_template_hook_refs_template,priority:1"`
	TemplateVersion int    `gorm:"column:template_version;index:idx_template_hook_refs_template,priority:2"`

	HookID     string `gorm:"column:hook_id;not null"`
	HookDigest string `gorm:"column:hook_digest;not null"`
	Stage      string `gorm:"column:stage;not null"`

	// RefOrder 保证引用顺序，读回时按它排序——契约明确要求「引用的顺序与
	// 内容都要一致」。
	RefOrder int  `gorm:"column:ref_order;not null"`
	Required bool `gorm:"column:required;not null;default:false"`

	// ParamsDefaults 存 JSON 对象，用 pgtype.StringMap 承载（jsonb 列），
	// nil ↔ SQL NULL、空 map ↔ JSON "{}"。
	ParamsDefaults pgtype.StringMap `gorm:"column:params_defaults_json;type:jsonb;default:'{}'"`
}

// TableName 固定表名为 template_hook_refs。
func (templateHookRefRow) TableName() string {
	return "template_hook_refs"
}

// toModel 把 GORM row 转换成 model.HookRef，不含模板归属信息
// （TemplateID/TemplateVersion 是查询条件，不是 HookRef 本身的字段）。
//
// ParamsDefaults 为空时统一还原成 nil，而不是留下一个长度为 0 的 map：
// 这是旧 SQLite 实现的既有行为（sqlite/template.go 的
// decodeJSONMapNilIfEmpty 与写入侧的 nonNilStringMap 配对——写入时 nil
// 归一成 '{}'，读回时空对象归一成 nil），本次迁移只换实现不改行为，因此
// 照搬这条归一化。
//
// 与 pgtype.StringMap 本身「区分 nil 与空 map」的语义并不矛盾：那是通用
// 类型对 SQL NULL 与 JSON '{}' 的忠实区分，而这一列在业务上没有「引用了
// hook 但显式给了一个空参数表」与「没给参数表」的区别，两者都是「无默认
// 参数」。归一化放在映射层而不是 pgtype 层，其它列因此不受影响。
func (r templateHookRefRow) toModel() model.HookRef {
	params := map[string]string(r.ParamsDefaults)
	if len(params) == 0 {
		params = nil
	}
	return model.HookRef{
		HookID:         r.HookID,
		HookDigest:     r.HookDigest,
		Stage:          model.HookStage(r.Stage),
		Order:          r.RefOrder,
		Required:       r.Required,
		ParamsDefaults: params,
	}
}

// hookRefFromModel 把 model.HookRef 转换成 GORM row，附带其归属的模板
// (id, version)——HookRef 本身不携带归属信息，写入时必须由调用方
// （CreateTemplate）提供。
//
// ParamsDefaults 为 nil 时写入一个空 map 而不是 SQL NULL，与 toModel 的
// 归一化配对，也与 params_defaults_json 列 NOT NULL DEFAULT '{}' 的既有
// schema 约定一致（001_init.sql）。
func hookRefFromModel(templateID string, templateVersion int, ref model.HookRef) templateHookRefRow {
	params := ref.ParamsDefaults
	if params == nil {
		params = map[string]string{}
	}
	return templateHookRefRow{
		TemplateID:      templateID,
		TemplateVersion: templateVersion,
		HookID:          ref.HookID,
		HookDigest:      ref.HookDigest,
		Stage:           string(ref.Stage),
		RefOrder:        ref.Order,
		Required:        ref.Required,
		ParamsDefaults:  pgtype.StringMap(params),
	}
}

// Models 返回本聚合包持有的全部行结构体，供组合器收集给 AutoMigrate
// （与其它聚合包同一形态，计划任务 9 接口契约）。
func Models() []any {
	return []any{&templateRow{}, &templateHookRefRow{}}
}
