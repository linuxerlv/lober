package pgstore

import (
	"fmt"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hook 与模板各有一张身份表和一张版本表，约定同 rows_registry.go。名字放在身份上，各版本共用，版本表里不存：改名不必
// 改每个版本。读版本时连上身份表，带出身份现在的名字（版本行的 Name 与 NameKey），所以返回的每个版本 Name 都是身份
// 现在的名字。

// ident 是 hooks 与 templates 两张身份表共有的列。两张表各嵌它一次，名字的唯一索引由 GORM 按表名命名：
// idx_hooks_name_key 与 idx_templates_name_key（schema.go 的期望值）。
type ident struct {
	ID      string `gorm:"primaryKey"`
	Name    string `gorm:"not null"`             // 规范化之后的名字（model.NormalizeName 的 display）
	NameKey string `gorm:"not null;uniqueIndex"` // 名字的 key：大小写不同也算同名（C12），用户建的与内置的共用这个名字空间
	Builtin bool   `gorm:"not null"`             // 内置的身份只读（C25）
	// LastVersion 是分配过的最大版本号，只增不减，删掉的草稿的号不再发：那个草稿的键可能还记在调用方手里的旧副本、
	// 派生模板的 DerivedFrom 与审计里，号再发出去，这些就会对到另一份内容上。
	LastVersion int `gorm:"not null"`
}

// hookRow 是 hooks 表的一行：一个 hook 的身份。嵌入的字段要有导出的名字并标上 embedded：GORM 跳过名字不导出的字段，
// 匿名嵌入的 ident 就是这样的名字。
type hookRow struct {
	Ident ident `gorm:"embedded"`
}

func (hookRow) TableName() string { return "hooks" }

// templateRow 是 templates 表的一行：一个模板的身份。
type templateRow struct {
	Ident ident `gorm:"embedded"`
}

func (templateRow) TableName() string { return "templates" }

// hookVersionRow 是 hook_versions 表的一行：hook 的一个版本。部分唯一索引守着「每个身份同时最多一个未审批的版本」。
// WHERE 里的逗号要写成 \,（struct tag 是带引号的串，源码里再转义一层成 \\,），GORM 才不把它当成索引选项之间的分隔，
// 否则 WHERE 被截断，由表结构断言查出来。
type hookVersionRow struct {
	HookID      string `gorm:"primaryKey;uniqueIndex:idx_hook_versions_unapproved,where:state IN ('draft'\\, 'review')"`
	Version     int    `gorm:"primaryKey;check:version >= 1"`
	Description string `gorm:"not null"`
	State       string `gorm:"not null;check:state IN ('draft', 'review', 'approved', 'deprecated')"`
	Author      string `gorm:"not null"`
	Approver    string `gorm:"not null"`
	Builtin     bool   `gorm:"not null"`
	Params      string `gorm:"type:jsonb;not null"` // []model.ParamDecl 的 JSON
	TimeoutSec  int    `gorm:"not null"`
	Required    bool   `gorm:"not null"`
	Impls       string `gorm:"type:jsonb;not null"` // []model.Impl 的 JSON
	Rev         int64  `gorm:"not null;check:rev >= 1"`

	// Hook 只为让 AutoMigrate 建外键 fk_hook_versions_hook，读写都不碰它。
	Hook hookRow `gorm:"foreignKey:HookID;references:ID;constraint:OnDelete:RESTRICT"`
	// Name 与 NameKey 是读版本时连上身份表带出来的，不是这张表的列，也不写入。
	Name    string `gorm:"->;-:migration"`
	NameKey string `gorm:"->;-:migration"`
}

func (hookVersionRow) TableName() string { return "hook_versions" }

// templateVersionRow 是 template_versions 表的一行：模板的一个版本，约定同 hookVersionRow。所勾的系存成 jsonb 数组，
// ListTemplates 按包含关系（@>）过滤。
type templateVersionRow struct {
	TemplateID  string `gorm:"primaryKey;uniqueIndex:idx_template_versions_unapproved,where:state IN ('draft'\\, 'review')"`
	Version     int    `gorm:"primaryKey;check:version >= 1"`
	State       string `gorm:"not null;check:state IN ('draft', 'review', 'approved', 'deprecated')"`
	Author      string `gorm:"not null"`
	Approver    string `gorm:"not null"`
	Builtin     bool   `gorm:"not null"`
	DerivedFrom string `gorm:"type:jsonb;not null"` // *model.TemplateKey 的 JSON，不是派生来的为 null（C24）
	Family      string `gorm:"not null"`
	Series      string `gorm:"type:jsonb;not null"` // []catalog.Series 的 JSON
	Body        string `gorm:"not null"`
	Params      string `gorm:"type:jsonb;not null"` // []model.ParamDecl 的 JSON
	Bindings    string `gorm:"type:jsonb;not null"` // map[string]model.Binding 的 JSON
	Assembly    string `gorm:"type:jsonb;not null"` // map[string][]model.Instance 的 JSON
	Rev         int64  `gorm:"not null;check:rev >= 1"`

	// Template 只为建外键 fk_template_versions_template。
	Template templateRow `gorm:"foreignKey:TemplateID;references:ID;constraint:OnDelete:RESTRICT"`
	Name     string      `gorm:"->;-:migration"`
	NameKey  string      `gorm:"->;-:migration"`
}

func (templateVersionRow) TableName() string { return "template_versions" }

// templateHookRefRow 是 template_hook_refs 表的一行：模板的一个版本引用了一个 hook 版本（store.HookRefs）。保存模板的
// 任何一个版本时在同一个事务里重写，删掉模板版本时随之删掉（CASCADE）。DeleteHookDraft 按 (hook_id, hook_version)
// 的索引查它；对 hook_versions 刻意不建外键，见 schema.go。
type templateHookRefRow struct {
	TemplateID      string `gorm:"primaryKey"`
	TemplateVersion int    `gorm:"primaryKey"`
	HookID          string `gorm:"primaryKey;index:idx_template_hook_refs_hook,priority:1"`
	HookVersion     int    `gorm:"primaryKey;index:idx_template_hook_refs_hook,priority:2"`

	// Template 只为建外键 fk_template_hook_refs_template。
	Template templateVersionRow `gorm:"foreignKey:TemplateID,TemplateVersion;references:TemplateID,Version;constraint:OnDelete:CASCADE"`
}

func (templateHookRefRow) TableName() string { return "template_hook_refs" }

// toHookVersionRow 把 hook 版本 h 转成一行，名字不存（在身份上）。
func toHookVersionRow(h model.Hook) (hookVersionRow, error) {
	params, err := encodeJSON(h.Params)
	if err != nil {
		return hookVersionRow{}, err
	}
	impls, err := encodeJSON(h.Impls)
	if err != nil {
		return hookVersionRow{}, err
	}
	return hookVersionRow{
		HookID: h.ID, Version: h.Version, Description: h.Description, State: string(h.State), Author: h.Author,
		Approver: h.Approver, Builtin: h.Builtin, Params: params, TimeoutSec: h.TimeoutSec, Required: h.Required,
		Impls: impls, Rev: h.Rev, Name: h.Name,
	}, nil
}

// hook 把一行转回 model.Hook，Name 是连上身份表带出来的名字。参数声明与实现每次都从 JSON 解出新的值，所以返回值就是
// 深拷贝，nil 与空的切片也照写进去的分开。
func (r hookVersionRow) hook() (model.Hook, error) {
	h := model.Hook{
		ID: r.HookID, Version: r.Version, Name: r.Name, Description: r.Description, State: model.VersionState(r.State),
		Author: r.Author, Approver: r.Approver, Builtin: r.Builtin, TimeoutSec: r.TimeoutSec, Required: r.Required, Rev: r.Rev,
	}
	if err := decodeJSON(r.Params, &h.Params); err != nil {
		return model.Hook{}, fmt.Errorf("hook %s 的参数声明：%w", store.VersionID(r.HookID, r.Version), err)
	}
	if err := decodeJSON(r.Impls, &h.Impls); err != nil {
		return model.Hook{}, fmt.Errorf("hook %s 的实现：%w", store.VersionID(r.HookID, r.Version), err)
	}
	return h, nil
}

// meta 是这个版本里由仓储管的字段，不必解开 JSON 的列。
func (r hookVersionRow) meta() store.VersionMeta {
	return store.VersionMeta{Name: r.Name, State: model.VersionState(r.State), Author: r.Author, Approver: r.Approver, Builtin: r.Builtin}
}

// toTemplateVersionRow 把模板版本 t 转成一行，名字不存。
func toTemplateVersionRow(t model.Template) (templateVersionRow, error) {
	var enc [5]string
	for i, v := range []any{t.DerivedFrom, t.Series, t.Params, t.Bindings, t.Assembly} {
		s, err := encodeJSON(v)
		if err != nil {
			return templateVersionRow{}, err
		}
		enc[i] = s
	}
	return templateVersionRow{
		TemplateID: t.ID, Version: t.Version, State: string(t.State), Author: t.Author, Approver: t.Approver,
		Builtin: t.Builtin, DerivedFrom: enc[0], Family: string(t.Family), Series: enc[1], Body: t.Body,
		Params: enc[2], Bindings: enc[3], Assembly: enc[4], Rev: t.Rev, Name: t.Name,
	}, nil
}

// template 把一行转回 model.Template，规则同 hook。
func (r templateVersionRow) template() (model.Template, error) {
	t := model.Template{
		ID: r.TemplateID, Version: r.Version, Name: r.Name, State: model.VersionState(r.State), Author: r.Author,
		Approver: r.Approver, Builtin: r.Builtin, Family: catalog.Family(r.Family), Body: r.Body, Rev: r.Rev,
	}
	for _, f := range []struct {
		what string
		col  string
		into any
	}{
		{"派生来源", r.DerivedFrom, &t.DerivedFrom}, {"系", r.Series, &t.Series}, {"参数声明", r.Params, &t.Params},
		{"取值来源", r.Bindings, &t.Bindings}, {"装配表", r.Assembly, &t.Assembly},
	} {
		if err := decodeJSON(f.col, f.into); err != nil {
			return model.Template{}, fmt.Errorf("模板 %s 的%s：%w", store.VersionID(r.TemplateID, r.Version), f.what, err)
		}
	}
	return t, nil
}

// meta 同 hookVersionRow.meta。
func (r templateVersionRow) meta() store.VersionMeta {
	return store.VersionMeta{Name: r.Name, State: model.VersionState(r.State), Author: r.Author, Approver: r.Approver, Builtin: r.Builtin}
}

// refRows 是模板版本 t 在 template_hook_refs 里的行：它引用的 hook 版本，去重、排过序（store.HookRefs）。只记 HookID 合
// model.ValidID 的：库里每个 hook 版本的 ID 都合它，别的 ID 永远对不上任何版本，删 hook 草稿与建任务查到的结果不变。草稿
// 却可以引用这样的 ID（激活时才报 hook_ref_missing），它可以很长，原样写进这张表的主键与索引，压不下去时就超过 btree
// 索引一行的上限（SQLSTATE 54000），能存成 UTF-8 文本的草稿反倒存不下了（spec §3 总则）。
func refRows(t model.Template) []templateHookRefRow {
	refs := store.HookRefs(t)
	rows := make([]templateHookRefRow, 0, len(refs))
	for _, k := range refs {
		if model.ValidID(k.ID) {
			rows = append(rows, templateHookRefRow{TemplateID: t.ID, TemplateVersion: t.Version, HookID: k.ID, HookVersion: k.Version})
		}
	}
	return rows
}
