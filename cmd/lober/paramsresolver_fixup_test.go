// 本文件历史上测试的是 resolvedParamsKeyFixup——装配点曾经用来兜住
// internal/provider/resolve.go 产出键名体系与 internal/render/params.go
// 的 InlineParams 白名单不对齐这一裂缝的包装类型。n5-resolve-keys 这一轨
// 把修复挪到了裂缝的源头（resolve.go 自己直接产出白名单认得的裸键），
// resolvedParamsKeyFixup 因此被移除（见 wire.go 移除处的说明）。
//
// 本文件改为在装配点这一层验证同一件事，但对象换成真实的、完整装配出的
// contract.TaskParamsResolver（app.ParamsResolver）——不再是包着一个可
// 编程测试替身的 fixup，而是 provider.NewTaskParamsResolver 的真实产出，
// 走真实的 IPAM/模板解析/磁盘规划三个内置 provider。这比原来的测试更
// 接近"真的跑起来"：原来的测试只证明了"fixup 这一层转换逻辑本身没写错"，
// 不能证明"接完整个装配链路之后，真实建任务路径真的能拿到裸键"——而后者
// 正是本轨修复要解决的真实故障（e2e 脚本第一次跑到取应答文件这一步
// 100% 复现的 500）。
package main

import (
	"context"
	"github.com/lober/lober/internal/core/contract"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestParamsResolver_WiredEndToEndProducesInlineParamsWhitelistKeys 是本轨
// 修复的装配点级回归测试：给真实装配出的 app.ParamsResolver 喂一份与
// scripts/e2e.sh 建任务请求体同构的表单数据（裸键 "hostname"），断言
// 返回值恰好是 internal/render/params.go 的 InlineParams 白名单认得的
// 两个裸键——不多（template_id/template_version/biz_gw/biz_mask/
// disk_primary 不该泄漏进来）、不少（hostname/biz_ip 必须真的读到了
// 表单值，不是空字符串）。
//
// 修复前这条测试会失败：resolve.go 的 Resolve 曾经读 model.FactHostname
// 这个键去取表单值，表单里根本没有这个键，读到的是空字符串，
// resolvedParamsKeyFixup 尚未移除时会把这个空字符串直接剔除（fixup 对
// 空值键的既有处理），最终返回值里连 hostname 键本身都不存在。
func TestParamsResolver_WiredEndToEndProducesInlineParamsWhitelistKeys(t *testing.T) {
	cfg := testConfig(t) // IPAM: static, DiskPolicy: largest（testConfig 的既有配置）
	app := buildAppWithDBForTest(t, cfg)

	formData := map[string]string{
		"hostname":    "e2e-host-01", // 与 scripts/e2e.sh 的建任务请求体同构
		"biz_ip":      "10.0.1.5",
		"biz_gateway": "10.0.1.1",
		"biz_mask":    "255.255.255.0",
	}
	facts := map[string]string{
		model.FactOSFamily:                string(model.OSFamilyRHEL),
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "500000000000",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "false",
		"LOBER_INSPECT_DISK_0_MODEL":      "SSD",
	}

	res, err := app.ParamsResolver.Resolve(context.Background(), contract.ResolveRequest{
		FormData: formData, Facts: facts,
	})
	if err != nil {
		t.Fatalf("真实装配出的 ParamsResolver.Resolve 失败: %v", err)
	}
	got := res.ResolvedParams

	want := map[string]string{
		"hostname": "e2e-host-01",
		"biz_ip":   "10.0.1.5",
	}
	if len(got) != len(want) {
		t.Fatalf("返回值键数量 = %d, want %d（应恰好是 InlineParams 白名单裸键），got=%+v",
			len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("键 %q = %q, want %q，完整结果=%+v", k, got[k], v, got)
		}
	}

	// InlineParams 白名单认不出的键必须缺席——留下任何一个都会让六个
	// 家族的渲染器在渲染应答文件这一步 100% 报错，这正是本轨修复要解决
	// 的真实故障（e2e 脚本第一次跑到取应答文件这一步的 500）。
	forbidden := []string{model.FactHostname, model.FactBizIP, model.FactBizGW, model.FactBizMask,
		model.FactDiskPrimary, "template_id", "template_version"}
	for _, k := range forbidden {
		if v, ok := got[k]; ok {
			t.Errorf("键 %q 不应出现在真实装配链路的返回值中，但 got 值=%q，完整结果=%+v", k, v, got)
		}
	}
}

// TestParamsResolver_WiredEndToEnd_IPAMFailurePropagatesAsError 断言真实
// 装配链路下，阻塞式 provider 失败仍然阻塞整体 Resolve（§7.4 语义）——
// 用一个 static IPAM 天然不会失败的事实反过来验证模板解析步骤的阻塞
// 语义：模板解析找不到匹配规则时整体应报错、不产生部分结果。这条与
// resolvedParamsKeyFixup 曾经的
// TestResolvedParamsKeyFixup_PassesThroughError 覆盖的是同一件事（错误
// 路径必须原样阻塞建任务），只是验证对象换成了真实装配链路而不是桩。
func TestParamsResolver_WiredEndToEnd_IPAMFailurePropagatesAsError(t *testing.T) {
	cfg := testConfig(t)
	app := buildAppWithDBForTest(t, cfg)

	// rule-based 模板解析器在没有任何规则、且没有 fallback 配置时，对
	// 找不到匹配规则的 facts 返回错误（internal/provider/resolver.go 的
	// TestResolver_ZeroMatchReturnsError 已经锁住这个算法本身的行为）；
	// testConfig 没有配置任何解析规则，任意 facts 都会走到这条路径。
	formData := map[string]string{"hostname": "e2e-host-02"}
	facts := map[string]string{model.FactOSFamily: string(model.OSFamilyRHEL)}

	_, err := app.ParamsResolver.Resolve(context.Background(), contract.ResolveRequest{FormData: formData, Facts: facts})
	if err == nil {
		t.Fatal("没有配置任何模板解析规则时，Resolve 应该报错（阻塞式 provider 失败必须阻塞建任务）")
	}
}
