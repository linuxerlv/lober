// 本文件锁住建任务时把决策结果落进 FactStore。
//
// **这是决策链的最后一环，此前完全断开。** resolve.go 自己的注释记录了这个
// 断口（原文）：
//
//	遗留的契约缺口（未受本次改动影响，据实记录）：biz_gw/biz_mask/
//	disk_primary/nic_install_mac 四项决策结果目前没有任何生产代码路径把它们
//	写进 Facts ... 它们在本函数修复前、修复后都同样「算出来了但送不到任何
//	消费者手上」
//
// 而渲染上下文的 Facts 来自 `FactStore.GetFacts(ctx, task.ID)`
// （gateway/rendercontext.go:66），于是六个家族的应答文件都读不到
// LOBER_DISK_PRIMARY——preseed 会直接拒绝渲染（上一个 commit 改的），
// kickstart 的非 by-hook 策略同样拒绝。
//
// 实测（2026-09-26）：Debian 主机录好磁盘清单、模板用 largest、任务建得出来，
// 取 preseed 时服务端返回「应答文件渲染失败，请联系运维检查模板配置」。
//
// **为什么不在渲染时重算**：磁盘规划本身是确定性的（diskplanner.go 先按
// by-id 路径排序再选，显式消除了 map 遍历的随机性），但 SPEC §2.6 要求决策
// 固化——resolve.go 的注释专门警告过「可能有人在渲染时再调一次编排器，那会让
// 同一任务前后两次拿到不同的 IP」。磁盘今天是确定的，IPAM 从来不是，而它们
// 共用同一条决策路径。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestCreateTask_PersistsDecidedFacts 锁住决策结果落进 FactStore。
func TestCreateTask_PersistsDecidedFacts(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")
	set := seedActiveArtifactSet(t, ts, "as-facts", model.OSFamilyDebian)

	// 决策事实走 factsFunc（Resolve 的第二个返回值），不是 resolveFunc——
	// resolveFunc 的返回值写进 Task.ResolvedParams，受 render/params.go 的
	// InlineParams 白名单校验，混进 disk_primary 会让渲染 500。真实的
	// taskParamsResolver 同样把两者分开返回。
	ts.Resolver.factsFunc = func(formData, facts map[string]string) map[string]string {
		return map[string]string{
			model.FactDiskPrimary: "/dev/disk/by-id/wwn-0xdecided",
			model.FactBizIP:       "10.20.0.15",
		}
	}

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]any{
		"sn": "SN-FACTS-1", "mac": "aa:bb:cc:11:00:01",
		"arch": "x86_64", "firmware": "bios",
		"disks": []map[string]any{
			{"byIdPath": "/dev/disk/by-id/wwn-0xdecided", "sizeBytes": 999_000_000_000,
				"rotational": true, "model": "D"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建主机失败: %d, %s", rr.Code, rr.Body.String())
	}

	tplID := seedActiveTemplateForFamily(t, ts, tok, set.ID)
	rr = ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "SN-FACTS-1", "templateId": tplID,
		"formData": map[string]string{"hostname": "h-facts"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建任务失败: %d, %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	facts, err := ts.Store.GetFacts(t.Context(), created.Data.ID)
	if err != nil {
		t.Fatalf("取 facts 失败: %v", err)
	}

	// 渲染上下文的 Facts 来自这里（gateway/rendercontext.go），所以主盘必须在。
	if got := facts[model.FactDiskPrimary]; got != "/dev/disk/by-id/wwn-0xdecided" {
		t.Errorf("Facts[%s] = %q——渲染上下文从 FactStore 取 facts，"+
			"主盘不在这里的话六个家族的应答文件都读不到它（preseed 直接拒绝渲染，"+
			"kickstart 的非 by-hook 策略同样拒绝）。实际 facts: %v",
			model.FactDiskPrimary, got, facts)
	}
}

// TestCreateTask_NoDisksNoFactsWritten 锁住没有磁盘时不写空值。
//
// 写 `LOBER_DISK_PRIMARY=""` 比不写更糟：渲染器的判据是「键存在且非空」，
// 空值会让它以为决策做过了。
func TestCreateTask_NoDisksNoFactsWritten(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")
	set := seedActiveArtifactSet(t, ts, "as-nofacts", model.OSFamilyDebian)

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]any{
		"sn": "SN-NOFACTS-1", "mac": "aa:bb:cc:11:00:02",
		"arch": "x86_64", "firmware": "bios",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建主机失败: %d, %s", rr.Code, rr.Body.String())
	}

	tplID := seedActiveTemplateForFamily(t, ts, tok, set.ID)
	rr = ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "SN-NOFACTS-1", "templateId": tplID,
		"formData": map[string]string{"hostname": "h-nofacts"},
	})
	// 建任务本身可能成功（stub resolver 不做磁盘规划），关键是不写空值。
	if rr.Code != http.StatusOK {
		return // 真实 resolver 会在这里失败，那也是正确行为
	}
	var created struct {
		Data taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	facts, err := ts.Store.GetFacts(t.Context(), created.Data.ID)
	if err != nil {
		t.Fatalf("取 facts 失败: %v", err)
	}
	if v, ok := facts[model.FactDiskPrimary]; ok && v == "" {
		t.Errorf("没有磁盘时写了空的 Facts[%s]——渲染器的判据是「键存在且非空」，"+
			"空值会让它以为决策做过了", model.FactDiskPrimary)
	}
}

// TestCreateTask_PassesTemplateDiskPolicyToResolver 锁住模板的磁盘策略传到决策器。
//
// **两半读了不同的源，此前必然冲突。** Template.DiskPolicy 只被渲染器读
// （render/kickstart.go 的 partitionDirectives、render/preseed.go 的
// preseedPartitioning），而决策器用的是装配点从 lober.yaml 的
// provider.disk_policy 建好的那一个 DiskIntent。
//
// 实测后果（2026-09-26，Debian 那轮）：模板声明 largest、VM 的 lober.yaml 配的是
// by-hook，于是规划器走 by-hook 分支返回零值 Disk、不写 LOBER_DISK_PRIMARY，而
// preseed 渲染器按 largest 要求那个 fact 存在，取应答文件时直接渲染失败，
// 服务端只回一句「应答文件渲染失败，请联系运维检查模板配置」。
//
// 反过来（模板 by-hook、全局 largest）是规划器算了一个没人用的结果——不致命但
// 同样是两半不一致。全局配置无论设成哪个值都会踩中一种。
func TestCreateTask_PassesTemplateDiskPolicyToResolver(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")
	set := seedActiveArtifactSet(t, ts, "as-policy", model.OSFamilyDebian)

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]any{
		"sn": "SN-POLICY-1", "mac": "aa:bb:cc:11:00:03",
		"arch": "x86_64", "firmware": "bios",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建主机失败: %d, %s", rr.Code, rr.Body.String())
	}

	tplID := seedActiveTemplateForFamily(t, ts, tok, set.ID)
	// 版本号取 1：seedActiveTemplateForFamily 建的是第 1 版。
	tpl, err := ts.Store.GetTemplate(t.Context(), tplID, 1)
	if err != nil {
		t.Fatalf("取模板失败: %v", err)
	}

	rr = ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "SN-POLICY-1", "templateId": tplID,
		"formData": map[string]string{"hostname": "h-policy"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建任务失败: %d, %s", rr.Code, rr.Body.String())
	}

	if got := ts.Resolver.LastDiskPolicy; got != tpl.DiskPolicy {
		t.Errorf("决策器收到的磁盘策略 = %q, want 模板声明的 %q——"+
			"这一环断掉时规划器会悄悄退回 lober.yaml 的全局配置，"+
			"而渲染器读的是模板的那个值，两半不一致就会渲染失败",
			got, tpl.DiskPolicy)
	}
}
