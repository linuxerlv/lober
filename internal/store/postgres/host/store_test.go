// 本文件测试 internal/store/postgres/host 的 4 个 contract.HostStore
// 方法。测试覆盖清单来自计划任务 8（host 聚合）的交付判定：
//
//	UpsertHost：首次插入、同 mac 第二次（验证是更新而非插入）、
//	两台不同 mac 但 SN 相同（必须都能存在）。
//	FindHostByIdentity：SN 命中、SN 空而 MAC 命中、三键全空返回 ErrNotFound。
//	GetHost / ListHosts 各覆盖命中与空库。
package host

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newTestStore 建一个独立 schema 的 Store，供每个测试用例使用。
func newTestStore(t *testing.T) *Store {
	db := pgtest.NewDB(t, Models()...)
	return New(db)
}

func TestUpsertHost_首次插入(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	h := model.Host{
		SN:         "SN-001",
		MAC:        "00:11:22:33:44:55",
		UUID:       "uuid-001",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		Rack:       "R1",
		LastSeenAt: 1000,
	}

	got, err := s.UpsertHost(ctx, h)
	if err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}
	if got != h {
		t.Fatalf("UpsertHost 返回值 = %+v, want %+v", got, h)
	}

	// 用 GetHost 复核确实落库。
	fetched, err := s.GetHost(ctx, h.MAC)
	if err != nil {
		t.Fatalf("GetHost 失败: %v", err)
	}
	if fetched != h {
		t.Fatalf("GetHost 返回 = %+v, want %+v", fetched, h)
	}
}

func TestUpsertHost_同mac第二次是更新不是插入(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := model.Host{
		SN:         "SN-001",
		MAC:        "00:11:22:33:44:55",
		UUID:       "uuid-001",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		Rack:       "R1",
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, first); err != nil {
		t.Fatalf("第一次 UpsertHost 失败: %v", err)
	}

	second := model.Host{
		SN:         "SN-001-CHANGED",
		MAC:        "00:11:22:33:44:55", // 同 mac
		UUID:       "uuid-001-changed",
		Arch:       model.ArchARM64,
		Firmware:   model.FirmwareBIOS,
		Rack:       "R2",
		LastSeenAt: 2000,
	}
	if _, err := s.UpsertHost(ctx, second); err != nil {
		t.Fatalf("第二次 UpsertHost 失败: %v", err)
	}

	all, err := s.ListHosts(ctx)
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListHosts 返回 %d 条, want 1 条（同 mac 第二次应是更新而非插入）", len(all))
	}

	fetched, err := s.GetHost(ctx, second.MAC)
	if err != nil {
		t.Fatalf("GetHost 失败: %v", err)
	}
	if fetched != second {
		t.Fatalf("GetHost 返回 = %+v, want 更新后的 %+v", fetched, second)
	}
}

func TestUpsertHost_两台不同mac但SN相同(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 白牌机场景：SN 整批重复，但 mac 各不相同，两条记录都必须能存在
	// ——这正是 sn 上不建唯一索引的理由。
	hostA := model.Host{
		SN:         "To Be Filled By O.E.M.",
		MAC:        "aa:aa:aa:aa:aa:aa",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}
	hostB := model.Host{
		SN:         "To Be Filled By O.E.M.",
		MAC:        "bb:bb:bb:bb:bb:bb",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}

	if _, err := s.UpsertHost(ctx, hostA); err != nil {
		t.Fatalf("UpsertHost hostA 失败: %v", err)
	}
	if _, err := s.UpsertHost(ctx, hostB); err != nil {
		t.Fatalf("UpsertHost hostB 失败（SN 相同不应导致唯一约束冲突）: %v", err)
	}

	all, err := s.ListHosts(ctx)
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListHosts 返回 %d 条, want 2 条（两台 SN 相同的白牌机都应能存在）", len(all))
	}
}

func TestUpsertHost_拒绝空Arch或空Firmware(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	noArch := model.Host{
		MAC:        "cc:cc:cc:cc:cc:cc",
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, noArch); err == nil {
		t.Fatal("UpsertHost 对空 Arch 应报错，实际未报错")
	}

	noFirmware := model.Host{
		MAC:        "dd:dd:dd:dd:dd:dd",
		Arch:       model.ArchX86_64,
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, noFirmware); err == nil {
		t.Fatal("UpsertHost 对空 Firmware 应报错，实际未报错")
	}
}

func TestFindHostByIdentity_SN命中(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	h := model.Host{
		SN:         "SN-FIND-001",
		MAC:        "11:11:11:11:11:11",
		UUID:       "uuid-find-001",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, h); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	got, err := s.FindHostByIdentity(ctx, h.SN, "", "")
	if err != nil {
		t.Fatalf("FindHostByIdentity 失败: %v", err)
	}
	if got != h {
		t.Fatalf("FindHostByIdentity 返回 = %+v, want %+v", got, h)
	}
}

func TestFindHostByIdentity_SN空而MAC命中(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	h := model.Host{
		SN:         "", // SN 为空
		MAC:        "22:22:22:22:22:22",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, h); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	// sn 传空串，应跳过 SN 分支，回退到 MAC 分支命中。
	got, err := s.FindHostByIdentity(ctx, "", h.MAC, "")
	if err != nil {
		t.Fatalf("FindHostByIdentity 失败: %v", err)
	}
	if got != h {
		t.Fatalf("FindHostByIdentity 返回 = %+v, want %+v", got, h)
	}
}

func TestFindHostByIdentity_三键全空返回ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.FindHostByIdentity(ctx, "", "", "")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Fatalf("FindHostByIdentity 三键全空 err = %v, want ErrNotFound", err)
	}
}

func TestFindHostByIdentity_全空库中查不到返回ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.FindHostByIdentity(ctx, "不存在的SN", "不存在的MAC", "不存在的UUID")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Fatalf("FindHostByIdentity 查不到 err = %v, want ErrNotFound", err)
	}
}

func TestGetHost_命中(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	h := model.Host{
		MAC:        "33:33:33:33:33:33",
		Arch:       model.ArchX86_64,
		Firmware:   model.FirmwareUEFI,
		LastSeenAt: 1000,
	}
	if _, err := s.UpsertHost(ctx, h); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	got, err := s.GetHost(ctx, h.MAC)
	if err != nil {
		t.Fatalf("GetHost 失败: %v", err)
	}
	if got != h {
		t.Fatalf("GetHost 返回 = %+v, want %+v", got, h)
	}
}

func TestGetHost_空库返回ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetHost(ctx, "不存在的mac")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Fatalf("GetHost 空库 err = %v, want ErrNotFound", err)
	}
}

func TestListHosts_命中(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	h1 := model.Host{MAC: "44:44:44:44:44:44", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI, LastSeenAt: 1000}
	h2 := model.Host{MAC: "55:55:55:55:55:55", Arch: model.ArchARM64, Firmware: model.FirmwareBIOS, LastSeenAt: 2000}

	if _, err := s.UpsertHost(ctx, h1); err != nil {
		t.Fatalf("UpsertHost h1 失败: %v", err)
	}
	if _, err := s.UpsertHost(ctx, h2); err != nil {
		t.Fatalf("UpsertHost h2 失败: %v", err)
	}

	all, err := s.ListHosts(ctx)
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListHosts 返回 %d 条, want 2 条", len(all))
	}
}

func TestListHosts_空库返回空切片(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	all, err := s.ListHosts(ctx)
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("ListHosts 空库返回 %d 条, want 0 条", len(all))
	}
}
