package provider

import (
	"strings"
	"testing"
)

func nic(name, mac, pciPath string, linkUp bool) HWNIC {
	return HWNIC{Name: name, MAC: mac, LinkUp: linkUp, PCIPath: pciPath}
}

// ---------------------------------------------------------------------------
// TestNICPlanner_MACMatch
// ---------------------------------------------------------------------------

func TestNICPlanner_MACMatchTakesPriorityOverLinkState(t *testing.T) {
	// 判据优先级第一条：MAC 精确匹配——主机清单里记录的 MAC 是最可靠的
	// 判据，即使它当前 link 是 down，也应该被选为装机网卡（清单记录的是
	// "这台机器接了哪根线"这个物理事实，比"此刻探测到的 link 状态"更可信；
	// link 可能因为交换机端口临时抖动而短暂显示 down）。
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),  // link up 但不是清单记录的 MAC
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", false), // link down 但 MAC 匹配清单
	}
	intent := NICIntent{KnownMAC: "aa:bb:cc:dd:ee:01"}

	decision, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("MAC 匹配场景应成功，got err=%v", err)
	}
	if decision.MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("应选中 MAC 精确匹配的网卡（即使 link down），got MAC=%s, want aa:bb:cc:dd:ee:01",
			decision.MAC)
	}
}

func TestNICPlanner_NoMACFallsBackToLinkState(t *testing.T) {
	// 判据优先级第二条：无 MAC 匹配时按 link 状态选装机网卡——当前通着的
	// 那块就是机器实际用来 PXE 引导到这里的那块。
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", false),
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true),
	}
	intent := NICIntent{} // 没有清单 MAC，或清单 MAC 未命中任何一块

	decision, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("按 link 状态回退场景应成功，got err=%v", err)
	}
	if decision.MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("无 MAC 匹配时应选 link up 的网卡，got MAC=%s, want aa:bb:cc:dd:ee:01",
			decision.MAC)
	}
}

func TestNICPlanner_UnmatchedKnownMACFallsBackToLinkState(t *testing.T) {
	// KnownMAC 非空但在候选网卡里找不到匹配项时，同样回退到 link 状态——
	// 不应因为"清单给了 MAC 却没找到"就直接报错，机器可能是新插的卡还没
	// 登记进清单，link 状态仍是可用的次优判据。
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", false),
	}
	intent := NICIntent{KnownMAC: "ff:ff:ff:ff:ff:ff"} // 清单 MAC 未命中任何候选

	decision, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("清单 MAC 未命中时应回退而不是报错，got err=%v", err)
	}
	if decision.MAC != "aa:bb:cc:dd:ee:00" {
		t.Fatalf("应回退到 link up 的网卡，got MAC=%s, want aa:bb:cc:dd:ee:00", decision.MAC)
	}
}

// ---------------------------------------------------------------------------
// TestNICPlanner_PCIPath
// ---------------------------------------------------------------------------

func TestNICPlanner_PCIPathDisambiguatesSameModelMultiNIC(t *testing.T) {
	// 判据优先级第三条：同型号多网卡时用 PCI 路径区分板载与扩展卡。本测试
	// 场景：两块网卡都 link up（模拟同型号双口卡都插了线但只有一个是装机口
	// 的边界情况——link 状态无法区分时，PCIPath 排序作为确定性依据），且都
	// 没有清单 MAC 匹配，此时应按 PCIPath 的确定规则选择（不是随机或依赖
	// 输入顺序）。
	nics := []HWNIC{
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true),
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),
	}
	intent := NICIntent{PreferredPCIPath: "0000:01:00.0"}

	decision, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("PCI 路径判据场景应成功，got err=%v", err)
	}
	if decision.MAC != "aa:bb:cc:dd:ee:00" {
		t.Fatalf("应按 PreferredPCIPath 选中 0000:01:00.0 对应的网卡，got MAC=%s, want aa:bb:cc:dd:ee:00",
			decision.MAC)
	}
}

func TestNICPlanner_MultipleLinkUpWithoutPCIHintPicksDeterministically(t *testing.T) {
	// 多块网卡都 link up、无 MAC 匹配、也没有给出 PreferredPCIPath 时，
	// 必须仍有一个确定的选择规则（不能依赖输入顺序，否则同一台机器两次
	// 采集上报顺序不同就会选出不同的网卡）——按 PCIPath 字典序取第一个，
	// 与 diskplanner.go 的 diskByIDPathAsc 同一方向的确定性要求。
	nicsOrderA := []HWNIC{
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true),
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),
	}
	nicsOrderB := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true),
	}

	d1, err1 := PlanInstallNIC(nicsOrderA, NICIntent{})
	d2, err2 := PlanInstallNIC(nicsOrderB, NICIntent{})
	if err1 != nil || err2 != nil {
		t.Fatalf("两种输入顺序都应成功，got err1=%v err2=%v", err1, err2)
	}
	if d1.MAC != d2.MAC {
		t.Fatalf("同一组网卡换一个输入顺序得到不同结果——违反确定性要求，"+
			"got d1.MAC=%s, d2.MAC=%s", d1.MAC, d2.MAC)
	}
}

// ---------------------------------------------------------------------------
// TestNICPlanner_OutputIsMACNotInterfaceName
// ---------------------------------------------------------------------------

func TestNICPlanner_DecisionOutputsMACNotInterfaceName(t *testing.T) {
	// 计划明确要求：输出的是 MAC 而不是接口名——接口名会漂移，MAC 不会。
	nics := []HWNIC{nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true)}
	decision, err := PlanInstallNIC(nics, NICIntent{})
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if decision.MAC == "" {
		t.Fatal("决策结果必须含 MAC")
	}
	// 决策类型本身不应该有一个"InterfaceName 是权威标识"的字段——这里用
	// 反射不现实，改为断言 Reason 文本引用的是 MAC 而不是接口名，作为
	// "输出以 MAC 为准"这个设计意图的行为验证。
	if !strings.Contains(decision.Reason, decision.MAC) {
		t.Fatalf("决策理由应引用 MAC（%s）而不是仅引用接口名，got Reason=%q",
			decision.MAC, decision.Reason)
	}
}

// ---------------------------------------------------------------------------
// TestNICPlanner_ErrorCases
// ---------------------------------------------------------------------------

func TestNICPlanner_ZeroNICsReturnsError(t *testing.T) {
	_, err := PlanInstallNIC(nil, NICIntent{})
	if err == nil {
		t.Fatal("零网卡必须返回明确错误——没有网卡就没有装机网络，" +
			"不能返回一个看似合法但实际不可用的空决策（SPEC §10 失败模式 6）")
	}
}

func TestNICPlanner_AllDownReturnsError(t *testing.T) {
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", false),
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", false),
	}
	_, err := PlanInstallNIC(nics, NICIntent{})
	if err == nil {
		t.Fatal("全部网卡 link down 且无 MAC 匹配时必须返回明确错误——" +
			"没有任何依据能确定哪块是装机网卡，不能猜一个（SPEC §10 失败模式 6：" +
			"选错网卡会导致装完网络不通，需人到现场）")
	}
}

func TestNICPlanner_ZeroNICsErrorMessageIsLocatable(t *testing.T) {
	_, err := PlanInstallNIC(nil, NICIntent{})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "0") && !strings.Contains(err.Error(), "零") &&
		!strings.Contains(err.Error(), "空") {
		t.Fatalf("错误信息应能定位问题（提及零/空/0 块网卡），got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestNICPlanner_BusinessNICSelection：业务网卡决策
// ---------------------------------------------------------------------------

func TestNICPlanner_BusinessNICDefaultsToInstallNICWhenOnlyOneCandidate(t *testing.T) {
	// 只有一块网卡时，业务网卡与装机网卡必然是同一块——没有第二块卡可选。
	nics := []HWNIC{nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true)}
	install, err := PlanInstallNIC(nics, NICIntent{})
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	biz, err := PlanBusinessNIC(nics, NICIntent{}, install)
	if err != nil {
		t.Fatalf("业务网卡决策不应出错: %v", err)
	}
	if biz.MAC != install.MAC {
		t.Fatalf("单网卡机器上业务网卡应与装机网卡相同，got biz.MAC=%s, install.MAC=%s",
			biz.MAC, install.MAC)
	}
}

func TestNICPlanner_BusinessNICPrefersDistinctFromInstallWhenMultipleAvailable(t *testing.T) {
	// 多网卡机器上，业务网卡与装机网卡通常是不同的两块物理网卡（装机网在
	// 装机 VLAN，业务网在另一 VLAN），业务网卡决策应能选出与装机网卡不同
	// 的那块，而不是无条件复用装机网卡的结果。
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true), // 装机网卡（清单命中）
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true), // 业务网卡候选
	}
	intent := NICIntent{KnownMAC: "aa:bb:cc:dd:ee:00", BusinessMAC: "aa:bb:cc:dd:ee:01"}
	install, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	biz, err := PlanBusinessNIC(nics, intent, install)
	if err != nil {
		t.Fatalf("业务网卡决策不应出错: %v", err)
	}
	if biz.MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("应按 BusinessMAC 选中业务网卡，got biz.MAC=%s, want aa:bb:cc:dd:ee:01", biz.MAC)
	}
}

func TestNICPlanner_BusinessNICUnspecifiedReturnsError(t *testing.T) {
	// 多网卡机器上，若没有任何依据（BusinessMAC 未指定）能确定哪块是业务
	// 网卡，必须明确报错，不能猜一个——业务网配置写错网卡同样会导致装完
	// 网络不通。
	nics := []HWNIC{
		nic("eth0", "aa:bb:cc:dd:ee:00", "0000:01:00.0", true),
		nic("eth1", "aa:bb:cc:dd:ee:01", "0000:02:00.0", true),
	}
	intent := NICIntent{KnownMAC: "aa:bb:cc:dd:ee:00"} // 没有 BusinessMAC
	install, err := PlanInstallNIC(nics, intent)
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	_, err = PlanBusinessNIC(nics, intent, install)
	if err == nil {
		t.Fatal("多网卡且业务网卡无法确定时必须报错，不能猜一个" +
			"（SPEC §10 失败模式 6：网卡选错会导致装完网络不通）")
	}
}
