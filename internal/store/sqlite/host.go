// 本文件实现 contract.HostStore。对应实现计划任务 7，依据 SPEC §4.3。
//
// hosts 表的主键是 mac（schema/001_init.sql：sn 故意不建唯一索引，白牌机
// SN 可能留空或整批重复）。GetHost 的 hostID 取 mac 值本身——
// contract.HostStore 文档明确「本接口不规定主机主键形态，只规定查找
// 语义」，用 schema 已有的真实主键作为 id 是最简单、不引入额外映射的
// 选择。
package sqlite

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

const hostColumns = `sn, mac, uuid, arch, firmware, rack, last_seen_at`

type hostRowScanner interface {
	Scan(dest ...any) error
}

func scanHostRow(row hostRowScanner) (model.Host, error) {
	var h model.Host
	err := row.Scan(&h.SN, &h.MAC, &h.UUID, &h.Arch, &h.Firmware, &h.Rack, &h.LastSeenAt)
	if err != nil {
		return model.Host{}, err
	}
	return h, nil
}

// UpsertHost 按 mac（hosts 表主键）落库或更新一条主机记录。arch 与
// firmware 是必填列，空值在写入时报错而不是存进去——存进去的后果是建
// 任务时过滤不出可装的 OS，而那个错误要等机器引导时才被 §4.6 拦下
// （计划任务 7「主机的关键点」）。
func (s *Store) UpsertHost(ctx context.Context, host model.Host) (model.Host, error) {
	if host.Arch == "" {
		return model.Host{}, fmt.Errorf("sqlite: UpsertHost 拒绝写入：arch 为空（SPEC §4.3 末条要求必填）")
	}
	if host.Firmware == "" {
		return model.Host{}, fmt.Errorf("sqlite: UpsertHost 拒绝写入：firmware 为空（SPEC §4.3 末条要求必填）")
	}

	query := fmt.Sprintf(`INSERT INTO hosts (%s) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mac) DO UPDATE SET
			sn = excluded.sn, uuid = excluded.uuid, arch = excluded.arch,
			firmware = excluded.firmware, rack = excluded.rack, last_seen_at = excluded.last_seen_at`,
		hostColumns)
	_, err := s.db.ExecContext(ctx, query,
		host.SN, host.MAC, host.UUID, host.Arch, host.Firmware, host.Rack, host.LastSeenAt)
	if err != nil {
		return model.Host{}, translateConstraintErr(err, "UpsertHost")
	}
	return host, nil
}

// GetHost 按 id（即 mac）查主机。查不到返回 contract.ErrNotFound。
func (s *Store) GetHost(ctx context.Context, hostID string) (model.Host, error) {
	query := fmt.Sprintf(`SELECT %s FROM hosts WHERE mac = ?`, hostColumns)
	row := s.db.QueryRowContext(ctx, query, hostID)
	h, err := scanHostRow(row)
	if err != nil {
		return model.Host{}, translateConstraintErr(err, "GetHost")
	}
	return h, nil
}

// FindHostByIdentity 按身份查主机，判据与
// TaskStore.FindPendingTaskByIdentity 使用同一套 SN → MAC → UUID 回退
// 优先级（空键跳过），保证「建任务时选的主机」与「引导时匹配的主机」
// 判据一致（计划任务 7「主机的关键点」：可以把回退逻辑抽成一个共用
// 函数——本实现的两处回退分别针对 tasks 表与 hosts 表两张不同的表，
// 列名不同，抽成共用函数需要额外的列名参数化，收益小于直接复制一份
// 三段式查询；两处的判据顺序与空键跳过语义完全一致，这是保证一致性的
// 真正要点，不是「有没有共用一段代码」）。
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
		query := fmt.Sprintf(`SELECT %s FROM hosts WHERE %s = ?`, hostColumns, a.column)
		row := s.db.QueryRowContext(ctx, query, a.key)
		h, err := scanHostRow(row)
		if err == nil {
			return h, nil
		}
		if translated := translateConstraintErr(err, "FindHostByIdentity"); translated != contract.ErrNotFound {
			return model.Host{}, translated
		}
	}
	return model.Host{}, contract.ErrNotFound
}

// ListHosts 列出全部已发现主机。文本过滤由调用方在 Go 里做。
func (s *Store) ListHosts(ctx context.Context) ([]model.Host, error) {
	query := fmt.Sprintf(`SELECT %s FROM hosts`, hostColumns)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, translateConstraintErr(err, "ListHosts")
	}
	defer rows.Close()

	out := make([]model.Host, 0)
	for rows.Next() {
		h, err := scanHostRow(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListHosts")
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListHosts")
	}
	return out, nil
}
