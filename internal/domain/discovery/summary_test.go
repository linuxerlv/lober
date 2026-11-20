package discovery

import (
	"testing"

	"github.com/lober/lober/internal/domain/inventory"
)

func TestInventorySummary(t *testing.T) {
	r := Report{Sections: []Section{
		{Key: "system", Items: []Item{{Name: "整机", Fields: map[string]string{"启动方式": "UEFI", "厂商": "A", "型号": "X"}}}},
		{Key: "cpu", Items: []Item{{Name: "处理器概况", Fields: map[string]string{"线程数": "8", "型号": "C"}}}},
		{Key: "memory", Items: []Item{{Fields: map[string]string{"可用总量": "2097152 kB"}}}},
		{Key: "storage", Items: []Item{{Name: "vda", Fields: map[string]string{"类型": "磁盘", "容量（字节）": "1024"}}}},
	}}
	got := r.InventorySummary()
	if got.SystemDisk != "vda" || got.Attrs[inventory.InvMemMib] != "2048" || got.Attrs[inventory.InvCPUCores] != "8" {
		t.Fatal("详细报告应提供装机所需的概况", got)
	}
	r.Sections[3].Items[0].Fields = map[string]string{"类型": "磁盘", "未读取·容量（字节）": "驱动未提供"}
	if got := r.InventorySummary(); got.SystemDisk != "" || got.Attrs[inventory.InvMemMib] != "2048" {
		t.Fatal("缺少容量的磁盘不能被选中", got)
	}
}
