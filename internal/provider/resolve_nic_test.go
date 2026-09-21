// 本文件测试同步决策编排器新增的网卡决策步骤（对应实现计划 P2-02，
// 铁律第 3 条：只加新分支/新函数，不改既有步骤行为——本文件的每个测试
// 都在验证"新增步骤不影响旧行为"这一点，与 resolve_test.go 里既有的
// 磁盘/IPAM/模板解析测试相互独立）。
package provider

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
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
	// 新增的网卡决策步骤必须是完全的 no-op——不产生新键，也不报错、不
	// 影响既有的磁盘/IPAM/模板解析结果。这条测试与 resolve_test.go 的
	// TestResolveTaskParams_AllSucceedMergesAllExpectedKeys 断言同一个
	// 调用组合，是对"不改既有步骤行为"这条铁律的直接回归防护。
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1", TemplateVersion: 2}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	params, err := orchestrator.Resolve(context.Background(), validFormData(), validFacts())
	if err != nil {
		t.Fatalf("无网卡 facts 时不应报错: %v", err)
	}

	if _, ok := params[nicInstallMACParamKey]; ok {
		t.Fatalf("没有采集到任何网卡事实时不应产出网卡决策键 %s，got %+v",
			nicInstallMACParamKey, params)
	}

	// 既有断言原样保留（回归防护）。
	wantKeys := []string{
		model.FactHostname, model.FactBizIP, model.FactBizGW, model.FactBizMask,
		model.FactDiskPrimary, "template_id", "template_version",
	}
	for _, k := range wantKeys {
		if _, ok := params[k]; !ok {
			t.Errorf("合并结果缺少预期键 %q，got %+v", k, params)
		}
	}
}

func TestResolveTaskParams_WithNICFactsAddsInstallNICMACKey(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	params, err := orchestrator.Resolve(context.Background(), validFormData(), factsWithNICs())
	if err != nil {
		t.Fatalf("有完整网卡事实时不应报错: %v", err)
	}

	if params[nicInstallMACParamKey] != "aa:bb:cc:dd:ee:00" {
		t.Fatalf("应按 link 状态选出 eth0（link up）为装机网卡，got %s=%q, want aa:bb:cc:dd:ee:00",
			nicInstallMACParamKey, params[nicInstallMACParamKey])
	}

	// 既有的磁盘/IPAM/模板解析结果不应被网卡决策步骤影响。
	if params[model.FactDiskPrimary] == "" {
		t.Fatal("磁盘决策结果不应被网卡决策步骤影响")
	}
	if params[model.FactBizIP] != "10.0.1.5" {
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

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	params, err := orchestrator.Resolve(context.Background(), validFormData(), facts)
	if err != nil {
		t.Fatalf("网卡决策失败不应让整体编排失败，got err=%v", err)
	}
	if _, ok := params[nicInstallMACParamKey]; ok {
		t.Fatalf("网卡决策失败时不应写入 %s 键（与磁盘决策 by-hook 场景"+
			"「宁可键缺失也不要误导性空值」同一方向），got %+v", nicInstallMACParamKey, params)
	}
	// 其余步骤的结果应照常存在。
	if params[model.FactDiskPrimary] == "" {
		t.Fatal("网卡决策失败不应影响磁盘决策结果")
	}
}
