package discovery

import (
	"reflect"
	"testing"
)

func TestHostDiskReport(t *testing.T) {
	attrs := map[string]string{"inv_collected_at": "20", "inv_disks": "sda:100:旧型号\nsdb:200:数据盘\nsda1:10:分区\nsr0:500:光驱"}
	report := &Report{Sections: []Section{{Key: "storage", Status: "read", Items: []Item{
		{Name: "sda", Fields: map[string]string{"类型": "磁盘", "容量（字节）": "101", "序列号": "新序列", "型号": "新型号"}},
		{Name: "nvme0n1p1", Fields: map[string]string{"容量（字节）": "4"}},
	}}}}
	for _, tc := range []struct {
		name   string
		at     int64
		want   []HostDisk
		status DiskStatus
	}{
		{"采用较新的采集", 10, []HostDisk{{Device: "sda", Bytes: "100", Model: "旧型号"}, {Device: "sdb", Bytes: "200", Model: "数据盘"}}, "partial"},
		{"同时采集时按设备补全", 20, []HostDisk{{Device: "sda", Bytes: "101", Model: "新型号", Serial: "新序列"}, {Device: "sdb", Bytes: "200", Model: "数据盘"}}, "partial"},
		{"采用较新的发现", 30, []HostDisk{{Device: "sda", Bytes: "101", Model: "新型号", Serial: "新序列"}}, "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := HostDisks(attrs, report, tc.at)
			if !reflect.DeepEqual(got.Disks, tc.want) || got.Status != tc.status {
				t.Fatalf("得到 %+v", got)
			}
		})
	}
	if got := HostDisks(nil, nil, 0); got.Status != "none" || got.Disks == nil || len(got.Disks) != 0 {
		t.Fatalf("空报告 %+v", got)
	}
	report.Sections[0].Status = "partial"
	report.Sections[0].Reason = "阵列信息不完整"
	if got := HostDisks(nil, report, 30); got.Status != "partial" || got.Notice != "阵列信息不完整" {
		t.Fatalf("缺项报告 %+v", got)
	}
	report.Sections[0] = Section{Key: "storage", Status: "absent"}
	if got := HostDisks(attrs, report, 30); len(got.Disks) != 0 {
		t.Fatal("明确没有磁盘的最新报告仍显示旧磁盘", got)
	}
}

func TestHistoricalDiskFields(t *testing.T) {
	report := &Report{Sections: []Section{{Key: "storage", Status: "read", Items: []Item{
		{Name: "/dev/vda", Fields: map[string]string{"类型": "系统可见磁盘", "容量": "40 GB", "型号": "SSD", "序列号": "A"}},
		{Name: "nvme0n1", Fields: map[string]string{"容量（字节）": "9007199254740993", "序列号": "B"}},
		{Name: "sdc", Fields: map[string]string{"类型": "阵列控制器", "容量": "1 TB"}},
		{Name: "sda1", Fields: map[string]string{"容量": "20 GB"}},
		{Name: "loop0", Fields: map[string]string{"容量": "20 GB"}},
		{Name: "/other/vdb", Fields: map[string]string{"容量": "20 GB"}},
		{Name: "sdb", Fields: map[string]string{"容量（字节）": "0"}},
	}}}}
	got := HostDisks(nil, report, 1)
	want := []HostDisk{{Device: "vda", CapacityText: "40 GB", Model: "SSD", Serial: "A"}, {Device: "nvme0n1", Bytes: "9007199254740993", Serial: "B"}}
	if !reflect.DeepEqual(got.Disks, want) || got.Status != "ready" {
		t.Fatalf("历史容量、设备和序列号 %+v", got)
	}
}
