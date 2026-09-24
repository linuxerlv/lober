// 本文件定义 hosts 表的 GORM row 类型，与 model.Host 之间的互转。
// 对应实现计划任务 8 三分之一（host 聚合），依据 SPEC §4.3。
//
// 列名与类型的事实来源是 internal/store/sqlite/schema/001_init.sql（已移除）：
//
//	CREATE TABLE hosts (
//	    sn              TEXT NOT NULL,
//	    mac             TEXT NOT NULL,
//	    uuid            TEXT NOT NULL DEFAULT '',
//	    arch            TEXT NOT NULL,
//	    firmware        TEXT NOT NULL,
//	    rack            TEXT NOT NULL DEFAULT '',
//	    last_seen_at    INTEGER NOT NULL,
//	    PRIMARY KEY (mac)
//	);
//
// 主键是 mac，不是 sn：白牌机的 SN 可能留空、可能填占位字符串
// "To Be Filled By O.E.M."、甚至整批重复。给 sn 建唯一索引会让整批白牌机
// 入库时因唯一约束冲突失败，症状是 discovery 同步静默丢主机——sn 上只建
// 普通索引（加速 FindHostByIdentity 的 SN 分支查询），不建唯一约束。
package host

import "github.com/lober/lober/internal/core/model"

// hostRow 是 hosts 表的 GORM 映射。字段顺序与旧 schema 一致，方便对照。
type hostRow struct {
	// MAC 是主键。旧 schema 里 mac 列本身没有 NOT NULL 之外的约束，主键
	// 约束由 PRIMARY KEY (mac) 承担——GORM 的 primaryKey tag 等价于此。
	MAC string `gorm:"column:mac;primaryKey"`

	// SN 故意只建普通索引，不建唯一索引（见本文件头注释）。
	SN string `gorm:"column:sn;not null;index:idx_hosts_sn"`

	// UUID 允许空串，旧 schema 用 DEFAULT '' 表达「没有」而非 SQL NULL
	// ——FindHostByIdentity 的空键跳过逻辑判的是空串，不是 NULL，用
	// not null 列更贴合这个语义，不需要引入指针类型。
	UUID string `gorm:"column:uuid;not null;default:''"`

	// Arch 与 Firmware 是必填列：建任务过滤与引导校验都依赖这两列非空，
	// UpsertHost 在写入前会拒绝空值（与旧 sqlite 实现同一约束）。
	Arch     string `gorm:"column:arch;not null"`
	Firmware string `gorm:"column:firmware;not null"`

	Rack string `gorm:"column:rack;not null;default:''"`

	// LastSeenAt 恒有值（不是「尚未发生」语义），直接用 int64 存 Unix
	// 秒，不需要 pgtype.NullTimeFromUnix 那套可空转换。
	LastSeenAt int64 `gorm:"column:last_seen_at;not null"`
}

// TableName 固定表名为 hosts，不用 GORM 默认的复数推断（复数推断对
// "host" 这个词形没问题，但显式声明让表名不随 GORM 版本的推断规则变化
// 而漂移，这是本次迁移里其它聚合包同样遵循的约定）。
func (hostRow) TableName() string {
	return "hosts"
}

// toModel 把 GORM row 转换成 model.Host。
func (r hostRow) toModel() model.Host {
	return model.Host{
		SN:         r.SN,
		MAC:        r.MAC,
		UUID:       r.UUID,
		Arch:       model.Arch(r.Arch),
		Firmware:   model.FirmwareMode(r.Firmware),
		Rack:       r.Rack,
		LastSeenAt: r.LastSeenAt,
	}
}

// fromModel 把 model.Host 转换成 GORM row，供写入使用。
func fromModel(h model.Host) hostRow {
	return hostRow{
		SN:         h.SN,
		MAC:        h.MAC,
		UUID:       h.UUID,
		Arch:       string(h.Arch),
		Firmware:   string(h.Firmware),
		Rack:       h.Rack,
		LastSeenAt: h.LastSeenAt,
	}
}
