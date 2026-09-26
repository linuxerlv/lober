// 本文件测试主机磁盘清单的录入与消费。
//
// **五个家族里有四个（Debian/Ubuntu/SUSE/Windows）与 ESXi 都依赖服务端选盘**，
// 只有 RHEL 靠 by-hook + %include 把选盘推到了 %pre 阶段的 hook。而建任务时
// 服务端拿不到任何磁盘信息：
//
//	handleCreateTask 传给 ParamsResolver 的 facts 只有
//	SN / MAC / UUID / arch / os_family
//
// 于是 largest / first / by-model / raid1-ssd 四个策略在建任务时必然以「磁盘
// 清单为空」失败（lober.yaml.example 的默认值 largest 正是其中之一），而
// preseed 等应答文件即便建出了任务也会因为 Facts[LOBER_DISK_PRIMARY] 为空而
// 拒绝渲染。
//
// SPEC §11 Phase 2 的正解是 inspect 阶段硬件采集（RenderInspectScript 已经
// 写好，但没有接进任何应答文件）。在那之前，这里补的是 §7.4 早已给出的另一条
// 路：**手工录入兜底**。POST /hosts 本来就是「discovery 尚未覆盖到的机器」的
// 入口，让它同时接受磁盘清单是这条路上最小的一步——不改建任务流程、不加新的
// 依赖、不需要机器先跑一轮。
//
// FactStore 帮不上：它按 **taskID** 存 facts，而建任务时任务还不存在，是循环
// 依赖。所以磁盘清单必须挂在 Host 上。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestHost_AcceptsDiskInventory 锁住 POST /hosts 接受磁盘清单并落库。
func TestHost_AcceptsDiskInventory(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]any{
		"sn": "SN-DISKS-1", "mac": "aa:bb:cc:00:00:01",
		"arch": "x86_64", "firmware": "bios",
		"disks": []map[string]any{
			{"byIdPath": "/dev/disk/by-id/wwn-0xaaa", "sizeBytes": 480_000_000_000,
				"rotational": false, "model": "SSD-A"},
			{"byIdPath": "/dev/disk/by-id/wwn-0xbbb", "sizeBytes": 2_000_000_000_000,
				"rotational": true, "model": "HDD-B"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("带磁盘清单的建主机应成功: %d, body=%s", rr.Code, rr.Body.String())
	}

	host := findHostBySN(t, ts, "SN-DISKS-1")
	if len(host.Disks) != 2 {
		t.Fatalf("磁盘清单未落库，实际 %d 块", len(host.Disks))
	}
	if host.Disks[0].ByIDPath != "/dev/disk/by-id/wwn-0xaaa" {
		t.Errorf("第一块盘的 by-id 路径不对: %q", host.Disks[0].ByIDPath)
	}
	if host.Disks[1].SizeBytes != 2_000_000_000_000 {
		t.Errorf("第二块盘的容量不对: %d", host.Disks[1].SizeBytes)
	}
	if host.Disks[0].Rotational {
		t.Error("SSD 的 rotational 应为 false")
	}
}

// TestHost_DisksOptional 回归保护：不带 disks 时照常建主机。
//
// 兼容性要求：现有调用方（scripts/e2e*.sh、WebUI）都不传这个字段。
func TestHost_DisksOptional(t *testing.T) {
	ts := newTestServer(t)
	rr := ts.doJSON("POST", "/console/v1/hosts", ts.login("eve-admin"), map[string]any{
		"sn": "SN-NODISK-1", "mac": "aa:bb:cc:00:00:02",
		"arch": "x86_64", "firmware": "uefi",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("不带 disks 应照常建主机: %d, body=%s", rr.Code, rr.Body.String())
	}
	host := findHostBySN(t, ts, "SN-NODISK-1")
	if len(host.Disks) != 0 {
		t.Errorf("不带 disks 时应为空，实际 %d 块", len(host.Disks))
	}
}

// TestHost_RejectsDiskWithoutByIDPath 锁住 by-id 路径必填。
//
// 磁盘必须用 by-id 这类持久标识，不能用内核名（/dev/sda）：内核名在不同启动
// 之间会漂移，写进分区指令可能**擦错盘**（SPEC §10 失败模式 5）。空路径的盘
// 进了清单，磁盘规划器会选中它然后渲染出空的 --ondisk=。
func TestHost_RejectsDiskWithoutByIDPath(t *testing.T) {
	ts := newTestServer(t)
	rr := ts.doJSON("POST", "/console/v1/hosts", ts.login("eve-admin"), map[string]any{
		"sn": "SN-BADDISK-1", "mac": "aa:bb:cc:00:00:03",
		"arch": "x86_64", "firmware": "bios",
		"disks": []map[string]any{{"sizeBytes": 480_000_000_000, "model": "NoPath"}},
	})
	if rr.Code == http.StatusOK {
		t.Error("磁盘缺 byIdPath 应被拒——内核名在不同启动之间会漂移，" +
			"空路径会让磁盘规划器渲染出空的 --ondisk=，可能擦错盘")
	}
}

// TestCreateTask_FeedsHostDisksToResolver 锁住建任务时把主机磁盘转成 facts。
//
// 这是整条链的接线点：磁盘清单录进了 Host，但磁盘规划器读的是 facts 里的
// LOBER_INSPECT_DISK_* 键（provider/hwfacts.go 的 ParseHWDisks 解析）。不做
// 这层转换，录进去的清单就送不到决策器手上。
func TestCreateTask_FeedsHostDisksToResolver(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")
	set := seedActiveArtifactSet(t, ts, "as-disks", model.OSFamilyDebian)

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]any{
		"sn": "SN-FEED-1", "mac": "aa:bb:cc:00:00:04",
		"arch": "x86_64", "firmware": "bios",
		"disks": []map[string]any{
			{"byIdPath": "/dev/disk/by-id/wwn-0xfeed", "sizeBytes": 999_000_000_000,
				"rotational": true, "model": "FeedDisk"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建主机失败: %d, %s", rr.Code, rr.Body.String())
	}

	tpl := seedActiveTemplateForFamily(t, ts, tok, set.ID)
	rr = ts.doJSON("POST", "/console/v1/tasks", tok, map[string]any{
		"hostId": "SN-FEED-1", "templateId": tpl, "formData": map[string]string{"hostname": "h1"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建任务失败: %d, %s", rr.Code, rr.Body.String())
	}

	got := ts.Resolver.LastFacts
	if got == nil {
		t.Fatal("ParamsResolver 没有被调用，或没记录 facts")
	}
	if got["LOBER_INSPECT_DISK_0_BYID"] != "/dev/disk/by-id/wwn-0xfeed" {
		t.Errorf("磁盘的 by-id 没进 facts，实际 facts 里的磁盘键: %v", diskKeys(got))
	}
	if got["LOBER_INSPECT_DISK_0_SIZE"] != "999000000000" {
		t.Errorf("磁盘容量没进 facts: %q", got["LOBER_INSPECT_DISK_0_SIZE"])
	}
	if got["LOBER_INSPECT_DISK_0_ROTATIONAL"] != "1" {
		t.Errorf("rotational 应编码成 1（ParseHWDisks 的约定）: %q",
			got["LOBER_INSPECT_DISK_0_ROTATIONAL"])
	}
}

func diskKeys(facts map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range facts {
		if len(k) > 19 && k[:19] == "LOBER_INSPECT_DISK_" {
			out[k] = v
		}
	}
	return out
}

// seedActiveTemplateForFamily 造一个 active 模板并返回它的 ID。
//
// 磁盘策略用 largest：这正是 lober.yaml.example 的默认值，也是本文件要验证的
// 那条「服务端选盘」路径——by-hook 会跳过磁盘规划，测不到东西。
func seedActiveTemplateForFamily(t *testing.T, ts *testServer, tok, setID string) string {
	t.Helper()
	rr := ts.doJSON("POST", "/console/v1/templates", tok, map[string]any{
		"name": "tpl-disks", "artifactSetId": setID,
		"answerTemplate": "# answer\n", "diskPolicy": "largest",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("建模板失败: %d, %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Data templateItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &created)

	rr = ts.doJSON("POST", "/console/v1/templates/"+created.Data.ID+"/state", tok,
		map[string]any{"state": "active", "version": created.Data.Version})
	if rr.Code != http.StatusOK {
		t.Fatalf("激活模板失败: %d, %s", rr.Code, rr.Body.String())
	}
	return created.Data.ID
}

// findHostBySN 按 SN 从 store 里取主机。
//
// 不用 GetHost：它的 key 是 store 实现的内部约定（fake 用 "sn:<SN>" 前缀，
// 与 console 的 hostID() 返回的裸 SN 不同），测试依赖那个约定会在换实现时碎掉。
func findHostBySN(t *testing.T, ts *testServer, sn string) model.Host {
	t.Helper()
	hosts, err := ts.Store.ListHosts(t.Context())
	if err != nil {
		t.Fatalf("列主机失败: %v", err)
	}
	for _, h := range hosts {
		if h.SN == sn {
			return h
		}
	}
	t.Fatalf("找不到 SN=%s 的主机（库里有 %d 台）", sn, len(hosts))
	return model.Host{}
}
