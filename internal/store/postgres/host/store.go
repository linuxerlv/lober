// 本文件实现 contract.HostStore，对应实现计划任务 8 三分之一（host
// 聚合），依据 SPEC §4.3。
//
// hosts 表的主键是 mac（不是 sn）：白牌机 SN 可能留空、可能填占位字符串、
// 甚至整批重复，给 sn 建唯一索引会让整批白牌机入库失败，症状是 discovery
// 同步静默丢主机——sn 上只建普通索引，语义与旧实现
// internal/store/sqlite/host.go 一致，详见 row.go 文件头注释。
package host

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// Store 实现 contract.HostStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。db 应已通过 AutoMigrate(Models()...)
// 建好 hosts 表（生产装配点）或由 pgtest.NewDB 建好（测试）。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Models 返回本聚合的 GORM model 列表，供 AutoMigrate 与下游组合器
// （cmd/lober 的装配点、pgtest 测试夹具）使用。
func Models() []any {
	return []any{&hostRow{}}
}

// UpsertHost 按 mac（hosts 表主键）落库或更新一条主机记录。arch 与
// firmware 是必填列，空值在写入时报错而不是存进去——存进去的后果是
// 建任务时过滤不出可装的 OS，而那个错误要等机器引导时才被 §4.6 拦下
// （与旧实现 internal/store/sqlite/host.go 同一约束）。
func (s *Store) UpsertHost(ctx context.Context, h model.Host) (model.Host, error) {
	if h.Arch == "" {
		return model.Host{}, fmt.Errorf("postgres: UpsertHost 拒绝写入：arch 为空（SPEC §4.3 末条要求必填）")
	}
	if h.Firmware == "" {
		return model.Host{}, fmt.Errorf("postgres: UpsertHost 拒绝写入：firmware 为空（SPEC §4.3 末条要求必填）")
	}

	row := fromModel(h)
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "mac"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sn", "uuid", "arch", "firmware", "rack", "last_seen_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return model.Host{}, pgerr.Translate(err, "UpsertHost")
	}
	return h, nil
}

// GetHost 按 id（即 mac）查主机。查不到返回 contract.ErrNotFound。
func (s *Store) GetHost(ctx context.Context, hostID string) (model.Host, error) {
	var row hostRow
	err := s.db.WithContext(ctx).Where("mac = ?", hostID).First(&row).Error
	if err != nil {
		return model.Host{}, pgerr.Translate(err, "GetHost")
	}
	return row.toModel(), nil
}

// FindHostByIdentity 按身份查主机，判据与
// TaskStore.FindPendingTaskByIdentity 使用同一套 SN → MAC → UUID 回退
// 优先级（空键跳过），保证「建任务时选的主机」与「引导时匹配的主机」
// 判据一致（计划任务 8 设计要点）。查不到返回 contract.ErrNotFound。
func (s *Store) FindHostByIdentity(ctx context.Context, sn, mac, uuid string) (model.Host, error) {
	type attempt struct {
		key    string
		column string
	}
	attempts := []attempt{
		{key: sn, column: "sn"},
		{key: mac, column: "mac"},
		{key: uuid, column: "uuid"},
	}

	for _, a := range attempts {
		if a.key == "" {
			continue
		}
		var row hostRow
		err := s.db.WithContext(ctx).Where(a.column+" = ?", a.key).First(&row).Error
		if err == nil {
			return row.toModel(), nil
		}
		if translated := pgerr.Translate(err, "FindHostByIdentity"); translated != contract.ErrNotFound {
			return model.Host{}, translated
		}
	}
	return model.Host{}, contract.ErrNotFound
}

// ListHosts 列出全部已发现主机。文本过滤由调用方在 Go 里做。
func (s *Store) ListHosts(ctx context.Context) ([]model.Host, error) {
	var rows []hostRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListHosts")
	}

	out := make([]model.Host, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toModel())
	}
	return out, nil
}
