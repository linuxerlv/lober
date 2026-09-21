package model

import "testing"

// snapshotFactKeysV1 是 v1 契约承诺的键名快照清单，独立于 facts.go 中的
// factKeysV1 手写维护。它与 FactKeysV1() 的返回值逐一比对且长度必须相等
// ——任何删除或改名都会让这个测试变红，这正是想要的效果（SPEC §5.4，
// 计划任务 8 测试要求）。
//
// 这份快照抄自 SPEC §5.4 的 24 个键清单，新增键时必须同时改这里与
// facts.go，那一次「同时改」就是在提醒作者：这是对外契约变更。
var snapshotFactKeysV1 = []string{
	"LOBER_CONTRACT_VERSION",
	"LOBER_TASK_ID", "LOBER_SN", "LOBER_MAC", "LOBER_UUID", "LOBER_HOSTNAME",
	"LOBER_OS_FAMILY", "LOBER_OS_VERSION", "LOBER_ARCH",
	"LOBER_DISKS", "LOBER_DISK_PRIMARY", "LOBER_NICS", "LOBER_RAM_MB", "LOBER_CPU_CORES", "LOBER_VENDOR", "LOBER_PRODUCT",
	"LOBER_BIZ_IP", "LOBER_BIZ_GW", "LOBER_BIZ_MASK",
	"LOBER_ARTIFACT_BASE", "LOBER_GATEWAY_BASE",
	"LOBER_RACK", "LOBER_ROW", "LOBER_DC",
}

// TestFactKeysV1_MatchesSnapshot 逐一比对且长度必须相等（计划任务 8 明确
// 要求的锁定方式）。
func TestFactKeysV1_MatchesSnapshot(t *testing.T) {
	got := FactKeysV1()

	if len(got) != len(snapshotFactKeysV1) {
		t.Fatalf("FactKeysV1() 返回 %d 个键，want %d 个 —— "+
			"SPEC §5.4 承诺 v1 契约恰好 24 个键，数量不符说明有键被删除、"+
			"改名或误增，任何一种都会让已在生产环境跑的 hook 静默出错",
			len(got), len(snapshotFactKeysV1))
	}

	gotSet := make(map[string]bool, len(got))
	for _, k := range got {
		gotSet[k] = true
	}
	for _, want := range snapshotFactKeysV1 {
		if !gotSet[want] {
			t.Errorf("FactKeysV1() 缺少键 %q —— "+
				"v1 契约只增不删，这个键一旦消失，所有依赖它的已上线 hook "+
				"会在拿不到值时静默出错而非在注册期报错（SPEC §5.4）", want)
		}
	}

	wantSet := make(map[string]bool, len(snapshotFactKeysV1))
	for _, k := range snapshotFactKeysV1 {
		wantSet[k] = true
	}
	for _, k := range got {
		if !wantSet[k] {
			t.Errorf("FactKeysV1() 包含快照清单之外的键 %q —— "+
				"新增键必须同时更新本测试的快照清单，否则这是一次未被记录的契约变更", k)
		}
	}
}

// TestFactKeys_UniformPrefix 验证所有键带统一前缀——前缀是命名空间的全部
// 保障，hook 作者靠它区分「服务端给的」与「自己定的」（SPEC §5.4）。
func TestFactKeys_UniformPrefix(t *testing.T) {
	for _, k := range FactKeysV1() {
		if len(k) < len("LOBER_") || k[:len("LOBER_")] != "LOBER_" {
			t.Errorf("键 %q 不带 LOBER_ 前缀 —— "+
				"命名空间前缀是 hook 作者区分「服务端给的」与「自己定的」变量的唯一依据，"+
				"一个例外就会破坏这个约定", k)
		}
	}
}

// TestFactContractVersion_IsOne 验证契约版本为 1，并说明升版本的代价
// （SPEC §5.4）。
func TestFactContractVersion_IsOne(t *testing.T) {
	if FactContractVersion != 1 {
		t.Errorf("FactContractVersion = %d, want 1 —— "+
			"当前 v1 契约尚未有理由升版本；升版本需要一个双写周期"+
			"（先在两个版本下都提供旧字段与新字段，等所有 hook 迁移完才能真正废弃旧字段），"+
			"不是改一个数字这么简单", FactContractVersion)
	}
}

// TestIsFactV1 验证「某键是否属于 v1 契约」的查询函数（SPEC §7.5：把运行时
// 意外提前为注册时错误）。
func TestIsFactV1(t *testing.T) {
	if !IsFactV1(FactTaskID) {
		t.Errorf("IsFactV1(%q) = false, want true —— 这是 v1 契约中的合法键", FactTaskID)
	}
	if IsFactV1("LOBER_SOMETHING_MADE_UP") {
		t.Error("IsFactV1(\"LOBER_SOMETHING_MADE_UP\") = true, want false —— " +
			"该键不在 v1 契约中，若被误判为合法，hook 注册期的 requires.facts 校验就会失去意义")
	}
}
