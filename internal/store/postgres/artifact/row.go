// 本文件定义 artifact_sets 表的行结构体，与 model.ArtifactSet 的双向映射，
// 对应实现计划任务 8（artifact 三分之一），依据 SPEC §2.1、§2.7。
//
// 为什么 FailReason 有专属列而不是像旧 SQLite 实现那样塞进 paths_json
// 信封：旧实现受"schema 是数据契约、本次任务不允许 ALTER TABLE"的约束
// （见 internal/store/sqlite/artifact.go（已移除）文件头），只能把 FailReason 编码
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

	// Channel 是交付通道（P3-03 任务 1）。
	//
	// **不设 default 也不设 not null**：历史行（Phase 1/2 导入的制品集）在
	// AutoMigrate 加列之后这一列是 NULL 或空串，而 model 侧零值等价于
	// installer 驱动通道（见 model.DeliveryChannel.Normalize），两边语义天然
	// 对齐，不需要一次数据回填。给 default 'installer' 反而要考虑「已有行是否
	// 被 AutoMigrate 回填」这个 GORM 行为差异——已存在的列 AutoMigrate 不会
	// 改动（见 memory: AutoMigrate 的两个索引盲区），依赖它做回填不可靠。
	Channel string `gorm:"column:channel"`

	// Image 是镜像直写通道专有的三项，整体作为一个 JSONB 列。
	//
	// 为什么是 JSONB 而不是拆成七八个平铺列：这几项**只对镜像直写通道有意义**，
	// 拆成平铺列会让 installer 驱动通道的每一行都带七八个恒为空的列；而
	// PartitionLayout 内部还有一个变长的分区数组，平铺不了。
	//
	// nil 指针映射 SQL NULL（pgtype.JSON 的既有约定），于是「installer 驱动
	// 通道没有这一项」与「镜像直写但字段全空」在库里是两种可区分的状态。
	Image pgtype.JSON[*model.ImageArtifact] `gorm:"column:image_json;type:jsonb"`

	// Paths 三个标量字段直接映射，Initrd/Extra 走 JSONB。
	PathsKernel string             `gorm:"column:paths_kernel"`
	PathsInitrd pgtype.StringSlice `gorm:"column:paths_initrd;type:jsonb"`
	PathsRepo   string             `gorm:"column:paths_repo"`
	// PathsMediaRoot 是安装介质根（anaconda 的 inst.repo 指向它），与
	// PathsRepo（包索引目录）是两个语义不同的路径——见
	// model.ArtifactPaths.MediaRoot 的文档。纯新增列，不带 index 标签。
	PathsMediaRoot string           `gorm:"column:paths_media_root;not null;default:''"`
	PathsExtra     pgtype.StringMap `gorm:"column:paths_extra;type:jsonb"`
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
		// 写入时归一化：零值折叠成 "installer"，库里不出现两种表示同一件事的
		// 值。读取侧仍然容忍 NULL/空串（历史行就是那样），见 toModel。
		Channel:        string(a.Channel.Normalize()),
		Image:          pgtype.JSON[*model.ImageArtifact]{Val: a.Image},
		PathsKernel:    a.Paths.Kernel,
		PathsInitrd:    pgtype.StringSlice(a.Paths.Initrd),
		PathsRepo:      a.Paths.Repo,
		PathsMediaRoot: a.Paths.MediaRoot,
		PathsExtra:     pgtype.StringMap(a.Paths.Extra),
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
		// Normalize 把历史行的 NULL/空串折叠成 installer——那些行是 Phase 1/2
		// 导入的，本来就属于 installer 驱动通道。
		Channel: model.DeliveryChannel(r.Channel).Normalize(),
		Image:   r.Image.Val,
		Paths: model.ArtifactPaths{
			Kernel:    r.PathsKernel,
			Initrd:    []string(r.PathsInitrd),
			Repo:      r.PathsRepo,
			MediaRoot: r.PathsMediaRoot,
			Extra:     map[string]string(r.PathsExtra),
		},
	}
}
