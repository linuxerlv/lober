// 本文件定义 artifact_sets 表的行结构体，与 model.ArtifactSet 的双向映射，
// 对应实现计划任务 8（artifact 三分之一），依据 SPEC §2.1、§2.7。
//
// 为什么 FailReason 有专属列而不是像旧 SQLite 实现那样塞进 paths_json
// 信封：旧实现受"schema 是数据契约、本次任务不允许 ALTER TABLE"的约束
// （见 internal/store/sqlite/artifact.go 文件头），只能把 FailReason 编码
// 进本该只装 ArtifactPaths 的既有列，两个写方法因此都要做
// read-modify-write。本次迁移的schema真源是 GORM model tag + AutoMigrate
// （设计文档 §0、§1），不存在"不能加列"的约束，FailReason 直接给一个
// fail_reason 列——UpdateArtifactSetState 与 SetArtifactSetContent 从而
// 能各自用一条不依赖对方已有状态的 UPDATE 语句，不必先读出整列再拼回去，
// 这正是设计文档说"能达到的程度"时期望的形态：GORM 的字段级 Updates 天然
// 就只碰自己关心的列。
package artifact

import (
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// artifactSetRow 是 artifact_sets 表对应的 GORM 行结构体。字段与列一一
// 对应 model.ArtifactSet，JSON 载荷（Initrd、Extra）用 pgtype 的类型。
//
// 不单独给 Kernel/Repo 用 pgtype——它们是普通字符串列，只有 Initrd（切片）
// 与 Extra（map）需要 JSONB 的 Scanner/Valuer。
type artifactSetRow struct {
	ID              string `gorm:"column:id;primaryKey"`
	Name            string `gorm:"column:name"`
	OSFamily        string `gorm:"column:os_family"`
	OSVersion       string `gorm:"column:os_version"`
	Arch            string `gorm:"column:arch"`
	Firmware        string `gorm:"column:firmware"`
	SourceISOSHA256 string `gorm:"column:source_iso_sha256"`
	State           string `gorm:"column:state"`
	ManifestSHA256  string `gorm:"column:manifest_sha256"`

	// FailReason 见文件头注释：本次迁移给它专属列，不复用旧实现的信封
	// JSON 手法。
	FailReason string `gorm:"column:fail_reason"`

	// Paths 三个标量字段直接映射，Initrd/Extra 走 JSONB。
	PathsKernel string             `gorm:"column:paths_kernel"`
	PathsInitrd pgtype.StringSlice `gorm:"column:paths_initrd;type:jsonb"`
	PathsRepo   string             `gorm:"column:paths_repo"`
	PathsExtra  pgtype.StringMap   `gorm:"column:paths_extra;type:jsonb"`
}

// TableName 固定表名为 artifact_sets，不依赖 GORM 的复数推导——推导规则
// 对 "artifactSetRow" 这类复合词的复数化结果不直观，固定写法比依赖推导
// 更能让读者一眼确认表名（计划任务 3 同款约定，本包沿用）。
func (artifactSetRow) TableName() string {
	return "artifact_sets"
}

// Models 返回本包全部行结构体类型，供组合器收集给 AutoMigrate（计划任务
// 8 的接口契约：每个聚合包导出 New 与 Models，形态与任务 3 一致）。
func Models() []any {
	return []any{&artifactSetRow{}}
}

// fromModel 把 model.ArtifactSet 转换成行结构体，供写入前使用。
func fromModel(a model.ArtifactSet) artifactSetRow {
	return artifactSetRow{
		ID:              a.ID,
		Name:            a.Name,
		OSFamily:        string(a.OSFamily),
		OSVersion:       a.OSVersion,
		Arch:            string(a.Arch),
		Firmware:        string(a.Firmware),
		SourceISOSHA256: a.SourceISOSHA256,
		State:           string(a.State),
		ManifestSHA256:  a.ManifestSHA256,
		FailReason:      a.FailReason,
		PathsKernel:     a.Paths.Kernel,
		PathsInitrd:     pgtype.StringSlice(a.Paths.Initrd),
		PathsRepo:       a.Paths.Repo,
		PathsExtra:      pgtype.StringMap(a.Paths.Extra),
	}
}

// toModel 是 fromModel 的逆操作，供查询后使用。
func (r artifactSetRow) toModel() model.ArtifactSet {
	return model.ArtifactSet{
		ID:              r.ID,
		Name:            r.Name,
		OSFamily:        model.OSFamily(r.OSFamily),
		OSVersion:       r.OSVersion,
		Arch:            model.Arch(r.Arch),
		Firmware:        model.FirmwareMode(r.Firmware),
		SourceISOSHA256: r.SourceISOSHA256,
		State:           model.ArtifactSetState(r.State),
		ManifestSHA256:  r.ManifestSHA256,
		FailReason:      r.FailReason,
		Paths: model.ArtifactPaths{
			Kernel: r.PathsKernel,
			Initrd: []string(r.PathsInitrd),
			Repo:   r.PathsRepo,
			Extra:  map[string]string(r.PathsExtra),
		},
	}
}
