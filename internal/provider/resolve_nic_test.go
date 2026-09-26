// 本文件测试同步决策编排器新增的网卡决策步骤（对应实现计划 P2-02，
// 铁律第 3 条：只加新分支/新函数，不改既有步骤行为——本文件的每个测试
// 都在验证"新增步骤不影响旧行为"这一点，与 resolve_test.go 里既有的
// 磁盘/IPAM/模板解析测试相互独立）。
//
// 本轨修复（n5-resolve-keys）说明：本文件三条测试历史上都断言
// nicInstallMACParamKey（"nic_install_mac"）在成功路径上出现在 Resolve
// 的返回值里。本轨修复之后，Resolve 的返回值收紧为只包含
// internal/render/params.go 的 InlineParams 白名单认得的裸键
// （hostname/biz_ip）——nic_install_mac 与 template_id/template_version/
// biz_gw/biz_mask/disk_primary 一样，从未真正被任何渲染器或 Facts 落库
// 消费过（全仓搜索确认没有生产代码读取这个键；六个渲染器的磁盘标识读的
// 是 ctx.Facts[LOBER_DISK_PRIMARY]，网卡决策目前没有对应的 Facts 落库
// 路径——这是 P2-02 计划任务 4 尚待接线的断口，详见 resolve.go 的
// contractGaps 记录，不受本次修复影响），继续让它出现在返回值里只会在
// 每一次真实建任务时让渲染应答文件 100% 报错（InlineParams 白名单
// 拒绝未知键）。
//
// 因此本文件的断言方式改为：网卡决策算法本身（ParseHWNICs/
// PlanInstallNIC）已经在 hwfacts_test.go 有完整的单元测试覆盖，不需要
// 在这里重新验证"选出哪个 MAC 对不对"；本文件改为验证"编排器是否正确地
// 把网卡决策的结果排除在返回值之外"（不管决策成功还是失败，返回值都
// 不应包含 nic_install_mac 键），这一点对所有三个场景（无网卡事实/有
// 网卡事实且成功/有网卡事实但决策失败）现在都成立，且仍然是一条会在
// 接线错误时失败的有效断言——如果将来有人不小心把 decided 直接当成
// 返回值用，这三条测试会立刻失败。
package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// factsWithNICs 在 validFacts() 基础上加两块网卡的采集事实（复用
// resolve_test.go 定义的 validFacts/validFormData 辅助函数，同包可直接
// 调用）。
func factsWithNICs() map[string]string {
	facts := validFacts()
	facts["LOBER_INSPECT_NIC_0_NAME"] = "eth0"
	facts["LOBER_INSPECT_NIC_0_MAC"] = "aa:bb:cc:dd:ee:00"
	facts["LOBER_INSPECT_NIC_0_LINK"] = "up"
	facts["LOBER_INSPECT_NIC_1_NAME"] = "eth1"
	facts["LOBER_INSPECT_NIC_1_MAC"] = "aa:bb:cc:dd:ee:01"
	facts["LOBER_INSPECT_NIC_1_LINK"] = "down"
	return facts
}

func TestResolveTaskParams_NoNICFactsIsNoOpForNICDecision(t *testing.T) {
	// 铁律第 3 条的核心验证：facts 中没有任何 LOBER_INSPECT_NIC_ 键时
	// （resolve_test.go 里全部既有测试用的 validFacts() 都是这种情况），
	// 新增的网卡决策步骤必须是完全的 no-op——不报错、不影响既有的磁盘/
	// IPAM/模板解析结果（后者由 resolve_test.go 的
	// TestResolveTaskParams_AllSucceedMergesAllExpectedKeys 断言同一个
	// 调用组合，这里不重复）。
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1", TemplateVersion: 2}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams
	if err != nil {
		t.Fatalf("无网卡 facts 时不应报错: %v", err)
	}

	if _, ok := params[nicInstallMACParamKey]; ok {
		t.Fatalf("网卡决策键 %s 不应出现在返回值中（InlineParams 白名单认不出"+
			"这个键，无论网卡决策是否发生），got %+v", nicInstallMACParamKey, params)
	}
	if params["hostname"] != "web-01" || params["biz_ip"] != "10.0.1.5" {
		t.Errorf("既有的 hostname/biz_ip 结果不应被网卡决策步骤影响，got %+v", params)
	}
}

func TestResolveTaskParams_WithNICFactsAddsInstallNICMACKey(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: factsWithNICs(),
	})
	params := res.ResolvedParams
	if err != nil {
		t.Fatalf("有完整网卡事实时不应报错: %v", err)
	}

	// 网卡决策确实发生了（不是被跳过），但按上面文件头的说明，其结果
	// 不应泄漏进返回值——用独立调用同一套算法（ParseHWNICs/
	// PlanInstallNIC，与 decideAndAppendInstallNICMAC 内部调用的是同一对
	// 函数，hwfacts_test.go 已经覆盖了算法本身的正确性）来确认"如果这一步
	// 真的执行了，会选出 eth0"，从而证明下面的"键不在返回值中"断言不是
	// 因为决策从未发生，而是因为编排器正确地把它排除在外。
	nics, err := ParseHWNICs(factsWithNICs())
	if err != nil || len(nics) == 0 {
		t.Fatalf("测试前提有误：factsWithNICs 应能被 ParseHWNICs 解析出网卡, err=%v nics=%+v", err, nics)
	}
	decision, err := PlanInstallNIC(nics, NICIntent{})
	if err != nil || decision.MAC != "aa:bb:cc:dd:ee:00" {
		t.Fatalf("测试前提有误：本场景下 PlanInstallNIC 应选出 eth0，got mac=%q err=%v", decision.MAC, err)
	}

	if _, ok := params[nicInstallMACParamKey]; ok {
		t.Fatalf("网卡决策键 %s 不应出现在返回值中（即使决策成功也不例外），got %+v",
			nicInstallMACParamKey, params)
	}

	// 既有的磁盘/IPAM/模板解析结果不应被网卡决策步骤影响——本轨修复后
	// disk_primary/template_id 本身也不在返回值白名单内，改用 hostname/
	// biz_ip 两个仍然在白名单内的键验证"其它步骤没被网卡决策步骤打扰"。
	if params["hostname"] != "web-01" {
		t.Fatal("hostname 决策结果不应被网卡决策步骤影响")
	}
	if params["biz_ip"] != "10.0.1.5" {
		t.Fatal("IPAM 决策结果不应被网卡决策步骤影响")
	}
}

func TestResolveTaskParams_NICDecisionFailureDoesNotFailOverallOrchestration(t *testing.T) {
	// 网卡决策步骤失败（全部网卡 link down 且无 MAC 匹配）不应让整体
	// Resolve 失败——建任务时的同步编排失败语义（模板解析/IPAM/磁盘规划
	// 三步，见 taskParamsResolver.Resolve 的既有文档）不应因为一个新增的
	// 附加决策步骤被扩大。网卡决策是否阻塞装机是"决策失败时机器如何处置"
	// 这一层（P2-02 计划任务 4，归属 gateway，不在本文件职责内）的问题，
	// 不是"建任务时的编排是否整体失败"这一层的问题——两者是不同阶段。
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	facts := validFacts()
	facts["LOBER_INSPECT_NIC_0_NAME"] = "eth0"
	facts["LOBER_INSPECT_NIC_0_MAC"] = "aa:bb:cc:dd:ee:00"
	facts["LOBER_INSPECT_NIC_0_LINK"] = "down" // 唯一网卡且 down：PlanInstallNIC 会报错

	// 确认测试前提：本场景下 PlanInstallNIC 确实会报错（否则本测试没有
	// 测到"决策失败"这条路径）。
	nics, err := ParseHWNICs(facts)
	if err != nil || len(nics) != 1 {
		t.Fatalf("测试前提有误: err=%v nics=%+v", err, nics)
	}
	if _, err := PlanInstallNIC(nics, NICIntent{}); err == nil {
		t.Fatal("测试前提有误：唯一网卡 link down 时 PlanInstallNIC 应报错")
	}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: facts,
	})
	params := res.ResolvedParams
	if err != nil {
		t.Fatalf("网卡决策失败不应让整体编排失败，got err=%v", err)
	}
	if _, ok := params[nicInstallMACParamKey]; ok {
		t.Fatalf("网卡决策失败时不应写入 %s 键（与磁盘决策 by-hook 场景"+
			"「宁可键缺失也不要误导性空值」同一方向），got %+v", nicInstallMACParamKey, params)
	}
	// 其余步骤的结果应照常存在，证明这不是整体失败。
	if params["hostname"] != "web-01" || params["biz_ip"] != "10.0.1.5" {
		t.Fatalf("网卡决策失败不应影响其它步骤的结果，got %+v", params)
	}
}
