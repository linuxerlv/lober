package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

func TestInventory_LookupBySN_Found(t *testing.T) {
	store := fake.NewStore()
	if _, err := store.UpsertHost(context.Background(), model.Host{SN: "SN-001", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}); err != nil {
		t.Fatalf("准备测试数据失败: %v", err)
	}

	inv := NewBuiltinInventory(store)
	host, err := inv.LookupBySN(context.Background(), "SN-001")
	if err != nil {
		t.Fatalf("查到清单里存在的 SN 应成功: %v", err)
	}
	if host.SN != "SN-001" {
		t.Fatalf("got SN=%s, want SN-001", host.SN)
	}
}

func TestInventory_LookupBySN_NotFound(t *testing.T) {
	store := fake.NewStore()
	inv := NewBuiltinInventory(store)

	_, err := inv.LookupBySN(context.Background(), "SN-does-not-exist")
	if err == nil {
		t.Fatal("查不到清单里不存在的 SN 时必须返回错误——MVP 没有外部 CMDB，" +
			"查不到应阻塞建任务，允许人工录入兜底")
	}
}

func TestInventory_Ping(t *testing.T) {
	store := fake.NewStore()
	inv := NewBuiltinInventory(store)
	if err := inv.Ping(context.Background()); err != nil {
		t.Fatalf("Ping 不应报错: %v", err)
	}
}
