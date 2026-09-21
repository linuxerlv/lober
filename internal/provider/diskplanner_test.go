package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// disk 是测试里构造 contract.Disk 的简写辅助函数。
func disk(byID string, size int64, rotational bool, model string) contract.Disk {
	return contract.Disk{ByIDPath: byID, SizeBytes: size, Rotational: rotational, Model: model}
}

// ---------------------------------------------------------------------------
// TestDiskFirst
// ---------------------------------------------------------------------------

func TestDiskFirst_NormalMultiDisk(t *testing.T) {
	p := diskPlannerFirst{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/aaa", 100, false, "X"),
		disk("/dev/disk/by-id/bbb", 200, false, "Y"),
	}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("first 策略多盘场景应成功，got err=%v", err)
	}
	if len(layout.Selected) != 1 {
		t.Fatalf("first 策略应恰好选一块盘，got %d 块", len(layout.Selected))
	}
	if layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/aaa" {
		t.Fatalf("first 策略应取输入顺序第一块，got %s，want aaa"+
			"——策略名为 first 的语义就是「按输入顺序取第一块」，选错会让批量机型装错盘",
			layout.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskFirst_SingleDisk(t *testing.T) {
	p := diskPlannerFirst{}
	disks := []contract.Disk{disk("/dev/disk/by-id/only", 100, false, "X")}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("单块盘应成功，got err=%v", err)
	}
	if len(layout.Selected) != 1 || layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/only" {
		t.Fatalf("单块盘场景应选中唯一那块盘，got %+v", layout.Selected)
	}
}

func TestDiskFirst_ZeroDisksReturnsError(t *testing.T) {
	p := diskPlannerFirst{}
	_, err := p.Plan(context.Background(), nil, contract.DiskIntent{})
	if err == nil {
		t.Fatal("零块盘必须返回错误——空布局会让 anaconda 交互式提问，机器挂起等人" +
			"（SPEC §10 失败模式 5），不能静默放行")
	}
}

func TestDiskFirst_ReasonNonEmptyWithBasis(t *testing.T) {
	p := diskPlannerFirst{}
	disks := []contract.Disk{disk("/dev/disk/by-id/aaa", 12345, false, "ModelX")}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	reason := layout.Selected[0].Reason
	if reason == "" {
		t.Fatal("说明不能为空——dry-run 端点要展示「为什么选这块盘」")
	}
	if !strings.Contains(reason, "12345") {
		t.Fatalf("说明应含判断依据（容量值），got %q", reason)
	}
}

// ---------------------------------------------------------------------------
// TestDiskLargest
// ---------------------------------------------------------------------------

func TestDiskLargest_NormalMultiDisk(t *testing.T) {
	p := diskPlannerLargest{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/small", 100, false, "X"),
		disk("/dev/disk/by-id/big", 999, false, "Y"),
		disk("/dev/disk/by-id/mid", 500, false, "Z"),
	}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("largest 多盘场景应成功: %v", err)
	}
	if layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/big" {
		t.Fatalf("largest 策略应选容量最大的盘，got %s，want big", layout.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskLargest_SingleDisk(t *testing.T) {
	p := diskPlannerLargest{}
	disks := []contract.Disk{disk("/dev/disk/by-id/only", 42, false, "X")}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("单块盘应成功: %v", err)
	}
	if layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/only" {
		t.Fatalf("单块盘应选中它自己，got %s", layout.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskLargest_ZeroDisksReturnsError(t *testing.T) {
	p := diskPlannerLargest{}
	_, err := p.Plan(context.Background(), nil, contract.DiskIntent{})
	if err == nil {
		t.Fatal("零块盘必须返回错误（SPEC §10 失败模式 5）")
	}
}

func TestDiskLargest_TieBreakDeterministic(t *testing.T) {
	p := diskPlannerLargest{}
	order1 := []contract.Disk{
		disk("/dev/disk/by-id/zzz", 500, false, "X"),
		disk("/dev/disk/by-id/aaa", 500, false, "Y"),
	}
	order2 := []contract.Disk{
		disk("/dev/disk/by-id/aaa", 500, false, "Y"),
		disk("/dev/disk/by-id/zzz", 500, false, "X"),
	}

	layout1, err1 := p.Plan(context.Background(), order1, contract.DiskIntent{})
	if err1 != nil {
		t.Fatalf("不应出错: %v", err1)
	}
	layout2, err2 := p.Plan(context.Background(), order2, contract.DiskIntent{})
	if err2 != nil {
		t.Fatalf("不应出错: %v", err2)
	}

	if layout1.Selected[0].Disk.ByIDPath != layout2.Selected[0].Disk.ByIDPath {
		t.Fatalf("同容量并列时打乱输入顺序两次结果应相同，got %s vs %s——"+
			"若依赖输入顺序，机器侧采集顺序的抖动会让同一台机器两次建任务选中不同的盘，"+
			"这正是纯函数确定性要求要防止的",
			layout1.Selected[0].Disk.ByIDPath, layout2.Selected[0].Disk.ByIDPath)
	}
	if layout1.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/aaa" {
		t.Fatalf("并列时应按 by-id 字典序取第一个，got %s，want aaa", layout1.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskLargest_ReasonNonEmptyWithBasis(t *testing.T) {
	p := diskPlannerLargest{}
	disks := []contract.Disk{disk("/dev/disk/by-id/aaa", 99999, false, "X")}
	layout, _ := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if !strings.Contains(layout.Selected[0].Reason, "99999") {
		t.Fatalf("说明应含容量值，got %q", layout.Selected[0].Reason)
	}
}

// ---------------------------------------------------------------------------
// TestDiskByModel
// ---------------------------------------------------------------------------

func TestDiskByModel_SingleMatch(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/nvme", 100, false, "SAMSUNG NVMe"),
		disk("/dev/disk/by-id/ssd", 200, false, "SAMSUNG MZ7LH480HAHQ-00005"),
	}
	intent := contract.DiskIntent{Policy: "by-model", Params: map[string]string{"model": "MZ7LH"}}
	layout, err := p.Plan(context.Background(), disks, intent)
	if err != nil {
		t.Fatalf("单块匹配应成功: %v", err)
	}
	if layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/ssd" {
		t.Fatalf("应匹配到型号含子串的那块盘，got %s", layout.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskByModel_MultiMatchDeterministic(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/zzz", 100, false, "SATA-SSD-A"),
		disk("/dev/disk/by-id/aaa", 200, false, "SATA-SSD-B"),
	}
	intent := contract.DiskIntent{Params: map[string]string{"model": "SATA-SSD"}}
	layout, err := p.Plan(context.Background(), disks, intent)
	if err != nil {
		t.Fatalf("多块匹配应成功: %v", err)
	}
	if layout.Selected[0].Disk.ByIDPath != "/dev/disk/by-id/aaa" {
		t.Fatalf("多块匹配时应按设备标识排序取第一个，got %s，want aaa", layout.Selected[0].Disk.ByIDPath)
	}
}

func TestDiskByModel_ZeroMatchReturnsErrorNoFallback(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{disk("/dev/disk/by-id/a", 100, false, "WDC-HDD")}
	intent := contract.DiskIntent{Params: map[string]string{"model": "SAMSUNG"}}
	_, err := p.Plan(context.Background(), disks, intent)
	if err == nil {
		t.Fatal("零块匹配必须返回错误，不能回落到其它策略——" +
			"回落会让运维以为装在了指定型号盘上，实际装在了随便一块盘上，" +
			"而这个错误要等擦盘完成才会被发现（计划任务 2）")
	}
}

func TestDiskByModel_SubstringMatchWithVendorPrefix(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{disk("/dev/disk/by-id/a", 100, false, "SAMSUNG MZ7LH480HAHQ-00005")}
	intent := contract.DiskIntent{Params: map[string]string{"model": "SAMSUNG"}}
	layout, err := p.Plan(context.Background(), disks, intent)
	if err != nil {
		t.Fatalf("填厂商名应能匹配到完整型号: %v", err)
	}
	if len(layout.Selected) != 1 {
		t.Fatalf("应恰好选中一块盘，got %d", len(layout.Selected))
	}
}

func TestDiskByModel_CaseInsensitive(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{disk("/dev/disk/by-id/a", 100, false, "Samsung MZ7LH")}
	intent := contract.DiskIntent{Params: map[string]string{"model": "samsung"}}
	layout, err := p.Plan(context.Background(), disks, intent)
	if err != nil {
		t.Fatalf("大小写不敏感匹配应成功: %v", err)
	}
	if !strings.Contains(layout.Selected[0].Reason, "不敏感") {
		t.Fatalf("说明应写明大小写处理方式，got %q", layout.Selected[0].Reason)
	}
}

func TestDiskByModel_MissingModelParamReturnsError(t *testing.T) {
	p := diskPlannerByModel{}
	disks := []contract.Disk{disk("/dev/disk/by-id/a", 100, false, "X")}
	_, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err == nil {
		t.Fatal("intent.Params 缺少 model 键时应返回错误，不能静默匹配空字符串")
	}
}

func TestDiskByModel_ZeroDisksReturnsError(t *testing.T) {
	p := diskPlannerByModel{}
	intent := contract.DiskIntent{Params: map[string]string{"model": "X"}}
	_, err := p.Plan(context.Background(), nil, intent)
	if err == nil {
		t.Fatal("零块盘必须返回错误（SPEC §10 失败模式 5）")
	}
}

// ---------------------------------------------------------------------------
// TestDiskRAID1
// ---------------------------------------------------------------------------

func TestDiskRAID1_TwoSameSizeSSDsPairNormally(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/ssd1", 500, false, "SSD"),
		disk("/dev/disk/by-id/ssd2", 500, false, "SSD"),
	}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("两块同容量 SSD 应正常组对: %v", err)
	}
	if len(layout.Selected) != 2 {
		t.Fatalf("RAID1 应返回主盘加一个成员，共 2 项，got %d", len(layout.Selected))
	}
}

func TestDiskRAID1_OnlyOneSSDReturnsErrorNoDegrade(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/ssd1", 500, false, "SSD"),
		disk("/dev/disk/by-id/hdd1", 2000, true, "HDD"),
	}
	_, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err == nil {
		t.Fatal("只有一块 SSD 时必须返回错误，不能退化为单盘——" +
			"退化会让运维以为装了 RAID1、实际是单盘，那块盘坏掉时才发现没有冗余" +
			"（计划任务 3）")
	}
}

func TestDiskRAID1_ZeroSSDReturnsError(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{disk("/dev/disk/by-id/hdd1", 2000, true, "HDD")}
	_, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err == nil {
		t.Fatal("零块 SSD 必须返回错误")
	}
}

func TestDiskRAID1_DifferentSizeSSDsReturnsError(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/ssd1", 500, false, "SSD-A"),
		disk("/dev/disk/by-id/ssd2", 1000, false, "SSD-B"),
	}
	_, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err == nil {
		t.Fatal("两块 SSD 但容量不同时应返回错误——" +
			"不同容量会浪费到小的那块，且多数 RAID1 实现会拒绝，不能跨容量强行配对")
	}
}

func TestDiskRAID1_MixedDisksOnlySelectsSSD(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/ssd1", 500, false, "SSD"),
		disk("/dev/disk/by-id/ssd2", 500, false, "SSD"),
		disk("/dev/disk/by-id/hdd1", 2000, true, "HDD"),
		disk("/dev/disk/by-id/hdd2", 2000, true, "HDD"),
	}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("混合盘时应正常从 SSD 中组对: %v", err)
	}
	for _, sel := range layout.Selected {
		if sel.Disk.Rotational {
			t.Fatalf("混合盘场景下选中的盘不应是旋转盘（HDD），got %+v", sel.Disk)
		}
	}
}

func TestDiskRAID1_ThreeSameSizeSSDsDeterministicPair(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disksOrder1 := []contract.Disk{
		disk("/dev/disk/by-id/ccc", 500, false, "SSD"),
		disk("/dev/disk/by-id/aaa", 500, false, "SSD"),
		disk("/dev/disk/by-id/bbb", 500, false, "SSD"),
	}
	disksOrder2 := []contract.Disk{
		disk("/dev/disk/by-id/bbb", 500, false, "SSD"),
		disk("/dev/disk/by-id/ccc", 500, false, "SSD"),
		disk("/dev/disk/by-id/aaa", 500, false, "SSD"),
	}

	l1, err1 := p.Plan(context.Background(), disksOrder1, contract.DiskIntent{})
	if err1 != nil {
		t.Fatalf("不应出错: %v", err1)
	}
	l2, err2 := p.Plan(context.Background(), disksOrder2, contract.DiskIntent{})
	if err2 != nil {
		t.Fatalf("不应出错: %v", err2)
	}

	got1 := []string{l1.Selected[0].Disk.ByIDPath, l1.Selected[1].Disk.ByIDPath}
	got2 := []string{l2.Selected[0].Disk.ByIDPath, l2.Selected[1].Disk.ByIDPath}
	if got1[0] != got2[0] || got1[1] != got2[1] {
		t.Fatalf("三块同容量 SSD 时选定的两块必须是确定的，got %v vs %v", got1, got2)
	}
	if got1[0] != "/dev/disk/by-id/aaa" || got1[1] != "/dev/disk/by-id/bbb" {
		t.Fatalf("应按 by-id 字典序取前两块，got %v，want [aaa bbb]", got1)
	}
}

func TestDiskRAID1_MultipleGroupsPicksLargest(t *testing.T) {
	p := diskPlannerRAID1SSD{}
	disks := []contract.Disk{
		disk("/dev/disk/by-id/small1", 200, false, "SSD-small"),
		disk("/dev/disk/by-id/small2", 200, false, "SSD-small"),
		disk("/dev/disk/by-id/big1", 800, false, "SSD-big"),
		disk("/dev/disk/by-id/big2", 800, false, "SSD-big"),
	}
	layout, err := p.Plan(context.Background(), disks, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	for _, sel := range layout.Selected {
		if sel.Disk.SizeBytes != 800 {
			t.Fatalf("容量不同的多组 SSD 应取容量最大的一组，got size=%d", sel.Disk.SizeBytes)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDiskByHook
// ---------------------------------------------------------------------------

func TestDiskByHook_ReturnsEmptyPrimaryWithNonEmptyReason(t *testing.T) {
	p := diskPlannerByHook{}
	layout, err := p.Plan(context.Background(), []contract.Disk{disk("/dev/disk/by-id/a", 1, false, "X")}, contract.DiskIntent{})
	if err != nil {
		t.Fatalf("by-hook 不应报错: %v", err)
	}
	if len(layout.Selected) != 1 {
		t.Fatalf("by-hook 应返回恰好一项占位结果，got %d", len(layout.Selected))
	}
	if layout.Selected[0].Disk != (contract.Disk{}) {
		t.Fatalf("by-hook 的主盘必须为空——它不做决策，got %+v", layout.Selected[0].Disk)
	}
	if layout.Selected[0].Reason == "" {
		t.Fatal("by-hook 的说明不能为空，必须写明由 pre 阶段 hook 决定")
	}
}

// ---------------------------------------------------------------------------
// TestDiskFactory
// ---------------------------------------------------------------------------

func TestDiskFactory_AllFiveKnownNamesConstructWorkingPlanner(t *testing.T) {
	cases := []struct {
		name          string
		disks         []contract.Disk
		intent        contract.DiskIntent
		wantSelectLen int
	}{
		{
			"first",
			[]contract.Disk{disk("/dev/disk/by-id/a", 1, false, "X")},
			contract.DiskIntent{},
			1,
		},
		{
			"largest",
			[]contract.Disk{disk("/dev/disk/by-id/a", 1, false, "X")},
			contract.DiskIntent{},
			1,
		},
		{
			"by-model",
			[]contract.Disk{disk("/dev/disk/by-id/a", 1, false, "MATCH")},
			contract.DiskIntent{Params: map[string]string{"model": "MATCH"}},
			1,
		},
		{
			"raid1-ssd",
			[]contract.Disk{
				disk("/dev/disk/by-id/a", 1, false, "SSD"),
				disk("/dev/disk/by-id/b", 1, false, "SSD"),
			},
			contract.DiskIntent{},
			2,
		},
		{
			"by-hook",
			nil,
			contract.DiskIntent{},
			1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			planner, err := NewDiskPlanner(c.name)
			if err != nil {
				t.Fatalf("工厂对合法策略名 %q 应成功构造，got err=%v", c.name, err)
			}
			layout, err := planner.Plan(context.Background(), c.disks, c.intent)
			if err != nil {
				t.Fatalf("策略 %q 对已知输入应能正常决策，got err=%v", c.name, err)
			}
			if len(layout.Selected) != c.wantSelectLen {
				t.Fatalf("策略 %q 的 Selected 长度 = %d，want %d", c.name, len(layout.Selected), c.wantSelectLen)
			}
		})
	}
}

func TestDiskFactory_UnknownNameReturnsError(t *testing.T) {
	_, err := NewDiskPlanner("no-such-policy")
	if err == nil {
		t.Fatal("未知策略名必须返回错误——配置里写错一个 provider 名字应该在启动时被发现")
	}
}
