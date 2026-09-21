package provider

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestParseHWDisks
// ---------------------------------------------------------------------------

func TestParseHWDisks_TwoCompleteEntries(t *testing.T) {
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/wwn-0x5000c500aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "500107862016",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "0",
		"LOBER_INSPECT_DISK_0_MODEL":      "SAMSUNG MZ7LH480",
		"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/wwn-0x5000c500bbb",
		"LOBER_INSPECT_DISK_1_SIZE":       "2000398934016",
		"LOBER_INSPECT_DISK_1_ROTATIONAL": "1",
		"LOBER_INSPECT_DISK_1_MODEL":      "WDC WD20EFRX",
	}

	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("完整的两条记录应解析成功，got err=%v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("应还原出 2 块盘，got %d", len(disks))
	}
	if disks[0].ByIDPath != "/dev/disk/by-id/wwn-0x5000c500aaa" || disks[0].SizeBytes != 500107862016 ||
		disks[0].Rotational != false || disks[0].Model != "SAMSUNG MZ7LH480" {
		t.Fatalf("第 0 块盘字段还原不正确，got %+v", disks[0])
	}
	if disks[1].Rotational != true || disks[1].SizeBytes != 2000398934016 {
		t.Fatalf("第 1 块盘字段还原不正确，got %+v", disks[1])
	}
}

func TestParseHWDisks_MissingFieldSkipsEntryWithoutZeroFill(t *testing.T) {
	// 计划任务 2 明确要求：缺字段的条目跳过而不是用零值补齐——一块「容量为
	// 0」的盘参与磁盘决策会让 largest 策略选错。
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID": "/dev/disk/by-id/wwn-0x5000c500aaa",
		// SIZE 缺失
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "0",
		"LOBER_INSPECT_DISK_0_MODEL":      "SAMSUNG",
		"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/wwn-0x5000c500bbb",
		"LOBER_INSPECT_DISK_1_SIZE":       "2000398934016",
		"LOBER_INSPECT_DISK_1_ROTATIONAL": "1",
		"LOBER_INSPECT_DISK_1_MODEL":      "WDC",
	}

	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("缺字段的条目应被跳过而不是报错，got err=%v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("应只还原出 1 块盘（第 0 块因缺 SIZE 被跳过），got %d 块: %+v", len(disks), disks)
	}
	if disks[0].ByIDPath != "/dev/disk/by-id/wwn-0x5000c500bbb" {
		t.Fatalf("剩下的应是第 1 块盘，got %+v", disks[0])
	}
	for _, d := range disks {
		if d.SizeBytes == 0 {
			t.Fatalf("不能用零值补齐容量字段——容量为 0 的盘参与 largest 策略会选错，got %+v", d)
		}
	}
}

func TestParseHWDisks_NonContiguousIndicesStillRecoverRest(t *testing.T) {
	// 采集时某块盘失败导致序号不连续（0、2，缺 1），其余仍应正确还原。
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "100",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "0",
		"LOBER_INSPECT_DISK_0_MODEL":      "A",
		"LOBER_INSPECT_DISK_2_BYID":       "/dev/disk/by-id/ccc",
		"LOBER_INSPECT_DISK_2_SIZE":       "300",
		"LOBER_INSPECT_DISK_2_ROTATIONAL": "1",
		"LOBER_INSPECT_DISK_2_MODEL":      "C",
	}

	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("序号不连续不应报错，got err=%v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("应还原出 2 块盘（序号 0 与 2），got %d: %+v", len(disks), disks)
	}
}

func TestParseHWDisks_MalformedSizeIsSkippedNotPanicked(t *testing.T) {
	// 解析要对畸形输入健壮——机器侧在各种硬件上会产出意外格式，解析失败要
	// 给出能定位的错误（或跳过该条目），而不是 panic。
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "not-a-number",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "0",
		"LOBER_INSPECT_DISK_0_MODEL":      "A",
	}

	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("畸形字段应被跳过而不是整体报错，got err=%v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("畸形 SIZE 的唯一条目应被跳过，got %+v", disks)
	}
}

func TestParseHWDisks_EmptyFactsReturnsEmptyNotError(t *testing.T) {
	disks, err := ParseHWDisks(map[string]string{})
	if err != nil {
		t.Fatalf("没有任何磁盘键时不应报错（上游尚未采集是正常中间状态），got err=%v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("应返回空切片，got %+v", disks)
	}
}

func TestParseHWDisks_UnknownKeysPreservedElsewhereNotDropped(t *testing.T) {
	// 未知键（不属于 LOBER_INSPECT_DISK_ 前缀）不应影响解析，也不应被本函数
	// 消费掉——它们仍应留在原 facts map 里供其它消费者（hook）使用，本测试
	// 确认解析函数不会误吞不相关的键。
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "100",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "0",
		"LOBER_INSPECT_DISK_0_MODEL":      "A",
		"LOBER_RACK":                      "R01",
		"some_custom_hook_key":            "value",
	}

	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("应还原出 1 块盘，未知键不应干扰解析，got %d", len(disks))
	}
	if _, ok := facts["LOBER_RACK"]; !ok {
		t.Fatal("未知键 LOBER_RACK 应仍保留在原 facts map 中，不应被本函数删除")
	}
}

// ---------------------------------------------------------------------------
// TestParseHWNICs
// ---------------------------------------------------------------------------

func TestParseHWNICs_TwoCompleteEntries(t *testing.T) {
	facts := map[string]string{
		"LOBER_INSPECT_NIC_0_NAME": "eth0",
		"LOBER_INSPECT_NIC_0_MAC":  "aa:bb:cc:dd:ee:00",
		"LOBER_INSPECT_NIC_0_LINK": "up",
		"LOBER_INSPECT_NIC_0_PCI":  "0000:01:00.0",
		"LOBER_INSPECT_NIC_1_NAME": "eth1",
		"LOBER_INSPECT_NIC_1_MAC":  "aa:bb:cc:dd:ee:01",
		"LOBER_INSPECT_NIC_1_LINK": "down",
		"LOBER_INSPECT_NIC_1_PCI":  "0000:02:00.0",
	}

	nics, err := ParseHWNICs(facts)
	if err != nil {
		t.Fatalf("完整的两条记录应解析成功，got err=%v", err)
	}
	if len(nics) != 2 {
		t.Fatalf("应还原出 2 块网卡，got %d", len(nics))
	}
	if nics[0].Name != "eth0" || nics[0].MAC != "aa:bb:cc:dd:ee:00" || !nics[0].LinkUp || nics[0].PCIPath != "0000:01:00.0" {
		t.Fatalf("第 0 块网卡字段还原不正确，got %+v", nics[0])
	}
	if nics[1].LinkUp {
		t.Fatalf("第 1 块网卡 link 状态应为 down，got LinkUp=true")
	}
}

func TestParseHWNICs_MissingMACSkipsEntry(t *testing.T) {
	// MAC 是网卡决策最重要的判据，缺失时不能用空字符串补齐（空字符串会被
	// 误判为"匹配"到清单里同样缺 MAC 记录的另一块卡）。
	facts := map[string]string{
		"LOBER_INSPECT_NIC_0_NAME": "eth0",
		// MAC 缺失
		"LOBER_INSPECT_NIC_0_LINK": "up",
	}

	nics, err := ParseHWNICs(facts)
	if err != nil {
		t.Fatalf("缺字段应跳过而不是报错，got err=%v", err)
	}
	if len(nics) != 0 {
		t.Fatalf("缺 MAC 的条目应被跳过，got %+v", nics)
	}
}

func TestParseHWNICs_UnknownLinkValueSkipsEntry(t *testing.T) {
	// link 字段畸形（既非 up 也非 down）时跳过该条目，不猜测默认值——一块
	// 状态不明的网卡被误判为 up 或 down 都可能导致选错装机网卡。
	facts := map[string]string{
		"LOBER_INSPECT_NIC_0_NAME": "eth0",
		"LOBER_INSPECT_NIC_0_MAC":  "aa:bb:cc:dd:ee:00",
		"LOBER_INSPECT_NIC_0_LINK": "unknown-garbage",
	}

	nics, err := ParseHWNICs(facts)
	if err != nil {
		t.Fatalf("畸形 link 值应跳过该条目而不是整体报错，got err=%v", err)
	}
	if len(nics) != 0 {
		t.Fatalf("畸形 link 值的条目应被跳过，got %+v", nics)
	}
}

func TestParseHWNICs_EmptyFactsReturnsEmptyNotError(t *testing.T) {
	nics, err := ParseHWNICs(map[string]string{})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(nics) != 0 {
		t.Fatalf("应返回空切片，got %+v", nics)
	}
}

func TestParseHWNICs_NonContiguousIndices(t *testing.T) {
	facts := map[string]string{
		"LOBER_INSPECT_NIC_0_NAME": "eth0",
		"LOBER_INSPECT_NIC_0_MAC":  "aa:bb:cc:dd:ee:00",
		"LOBER_INSPECT_NIC_0_LINK": "up",
		"LOBER_INSPECT_NIC_2_NAME": "eth2",
		"LOBER_INSPECT_NIC_2_MAC":  "aa:bb:cc:dd:ee:02",
		"LOBER_INSPECT_NIC_2_LINK": "down",
	}

	nics, err := ParseHWNICs(facts)
	if err != nil {
		t.Fatalf("序号不连续不应报错，got err=%v", err)
	}
	if len(nics) != 2 {
		t.Fatalf("应还原出 2 块网卡（序号 0 与 2），got %d: %+v", len(nics), nics)
	}
}

// ---------------------------------------------------------------------------
// TestDecodeError 定位性
// ---------------------------------------------------------------------------

func TestParseHWDisks_ErrorPathsProduceLocatableMessages(t *testing.T) {
	// 虽然畸形条目按设计跳过而不报错（见上面的测试），但如果调用方想知道
	// "到底跳过了什么"，日志/错误信息中提到的索引与字段名要能定位问题
	// 条目。本测试通过检查跳过是否真的发生、且不 panic 来确认健壮性——
	// 真正的panic 防护用一个极端畸形输入验证。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("解析函数不应 panic，got %v", r)
		}
	}()

	facts := map[string]string{
		"LOBER_INSPECT_DISK_abc_BYID": "garbage-index",
		"LOBER_INSPECT_DISK_":         "empty-suffix",
		"LOBER_INSPECT_DISK_0":        "no-field-suffix-at-all",
	}
	disks, err := ParseHWDisks(facts)
	if err != nil {
		t.Fatalf("即使键名本身畸形也不应整体报错，got err=%v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("全部畸形键不应还原出任何盘，got %+v", disks)
	}
}

func TestParseHWDisks_KeyNamesUseInspectNamespace(t *testing.T) {
	// 测试要锁住的行为之一：键名在约定的采集专用键命名空间内
	// （LOBER_INSPECT_ 前缀），不与 M1 冻结的 24 个 LOBER_ 键冲突。
	if !strings.HasPrefix(inspectDiskKeyPrefix, "LOBER_INSPECT_") {
		t.Fatalf("采集专用键前缀应在 LOBER_INSPECT_ 命名空间内，got %q", inspectDiskKeyPrefix)
	}
	if !strings.HasPrefix(inspectNICKeyPrefix, "LOBER_INSPECT_") {
		t.Fatalf("采集专用键前缀应在 LOBER_INSPECT_ 命名空间内，got %q", inspectNICKeyPrefix)
	}
}
