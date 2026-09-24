// 本文件测试磁盘 dry-run 端点。对应实现计划任务 8「测试要锁住的行为」
// dry-run 部分。
package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
)

// TestDryRun_ReturnsLayoutAndNonEmptyReasoning 锁住「dry-run 返回布局与
// 说明，说明非空」。
//
// facts 字段改用 LOBER_INSPECT_DISK_ 前缀键（本轨修复：parseDisksFact
// 现在解析的是真机采集脚本实际产出的格式，不是本文件历史上自造的逗号/
// 分号格式——旧格式下这条测试能通过纯粹是因为用的是 stub DiskPlanner，
// stub 忽略了 disks 参数，从未真正验证过 parseDisksFact 的解析结果，
// 见下面新增的 TestDryRun_RealPlannerConsumesParsedDisksWithNonEmptyReasoning）。
func TestDryRun_ReturnsLayoutAndNonEmptyReasoning(t *testing.T) {
	ts := newTestServer(t)
	ts.Server.deps.DiskPlanner = &stubDiskPlanner{
		planFunc: func(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
			return contract.Layout{Selected: []contract.LayoutDisk{
				{Disk: contract.Disk{ByIDPath: "/dev/disk/by-id/b", SizeBytes: 2000}, Reason: "容量最大"},
			}}, nil
		},
	}
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "largest",
		"facts": map[string]string{
			"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/a",
			"LOBER_INSPECT_DISK_0_SIZE":       "1000",
			"LOBER_INSPECT_DISK_0_ROTATIONAL": "true",
			"LOBER_INSPECT_DISK_0_MODEL":      "ModelA",
			"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/b",
			"LOBER_INSPECT_DISK_1_SIZE":       "2000",
			"LOBER_INSPECT_DISK_1_ROTATIONAL": "true",
			"LOBER_INSPECT_DISK_1_MODEL":      "ModelB",
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("dry-run 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data diskPlanResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.Reasoning == "" {
		t.Errorf("dry-run 的说明不应为空")
	}
}

// TestDryRun_RealPlannerConsumesParsedDisksWithNonEmptyReasoning 用真实的
// 磁盘决策逻辑（不是忽略 disks 参数的 stub）验证 parseDisksFact 的解析
// 结果真的能驱动出正确的选择——这是本轨任务明确要求锁住的行为：「dry-run
// 能吃真实 facts 并返回非空的选择理由」（SPEC §8.2：dry-run 是开发期
// 性价比最高的两个端点之一，前提是它真的在用录制的硬件事实跑策略，不是
// 摆设）。
//
// console 不能 import provider（archtest 规则三），因此这里没有用真实的
// diskPlannerLargest，而是在本文件内实现一个同等语义的"选容量最大"逻辑
// ——重点不在于复刻 provider 的实现，而在于证明 parseDisksFact 产出的
// []contract.Disk 里 SizeBytes/ByIDPath 字段被正确填充，足以驱动一个真实
// 依赖这些字段的决策逻辑得出正确结论。
func TestDryRun_RealPlannerConsumesParsedDisksWithNonEmptyReasoning(t *testing.T) {
	ts := newTestServer(t)
	ts.Server.deps.DiskPlanner = &stubDiskPlanner{
		planFunc: func(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
			if len(disks) == 0 {
				t.Fatal("parseDisksFact 应从 facts 中解析出磁盘清单，但 Plan 收到的 disks 为空——" +
					"说明 dry-run 端点没有真的把 LOBER_INSPECT_DISK_ 前缀键转换成结构化磁盘清单")
			}
			var largest contract.Disk
			for _, d := range disks {
				if d.SizeBytes > largest.SizeBytes {
					largest = d
				}
			}
			return contract.Layout{Selected: []contract.LayoutDisk{
				{Disk: largest, Reason: "本测试内实现的选容量最大逻辑，选中 " + largest.ByIDPath},
			}}, nil
		},
	}
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "largest",
		"facts": map[string]string{
			"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/wwn-0x5000c500aaa",
			"LOBER_INSPECT_DISK_0_SIZE":       "500000000000",
			"LOBER_INSPECT_DISK_0_ROTATIONAL": "false",
			"LOBER_INSPECT_DISK_0_MODEL":      "SAMSUNG SSD",
			"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/wwn-0x5000c500bbb",
			"LOBER_INSPECT_DISK_1_SIZE":       "2000000000000",
			"LOBER_INSPECT_DISK_1_ROTATIONAL": "true",
			"LOBER_INSPECT_DISK_1_MODEL":      "WDC HDD",
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("dry-run 失败: %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data diskPlanResult `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Data.ChosenDisk != "/dev/disk/by-id/wwn-0x5000c500bbb" {
		t.Fatalf("应选中容量最大的盘（2TB 的 wwn-...bbb），got ChosenDisk=%q——"+
			"说明 parseDisksFact 未能正确解析出两块盘的 SizeBytes 字段",
			env.Data.ChosenDisk)
	}
	if env.Data.Reasoning == "" {
		t.Fatal("dry-run 的选择理由不应为空")
	}
}

// TestParseDisksFact_MalformedFieldSkipsEntryNotPanic 锁住「解析要对畸形
// 输入健壮」这条要求（本轨任务明确列出）：dry-run 端点面对真机导出的、
// 可能不完整或畸形的 facts 快照时，应跳过坏记录而不是 panic 或让整个
// 请求报错。
func TestParseDisksFact_MalformedFieldSkipsEntryNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parseDisksFact 不应 panic，got %v", r)
		}
	}()

	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "不是数字", // 畸形容量字段
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "false",
		"LOBER_INSPECT_DISK_0_MODEL":      "A",
		"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/bbb",
		"LOBER_INSPECT_DISK_1_SIZE":       "2000000000000",
		"LOBER_INSPECT_DISK_1_ROTATIONAL": "true",
		"LOBER_INSPECT_DISK_1_MODEL":      "B",
	}

	disks := parseDisksFact(facts)
	if len(disks) != 1 {
		t.Fatalf("畸形的第 0 块盘应被跳过，只保留第 1 块，got %d 块: %+v", len(disks), disks)
	}
	if disks[0].ByIDPath != "/dev/disk/by-id/bbb" {
		t.Fatalf("剩下的应是第 1 块盘，got %+v", disks[0])
	}
}

// TestParseDisksFact_EmptyFactsReturnsEmptyNotError 没有任何磁盘采集键时
// 应返回空切片，dry-run 请求体不带磁盘事实是合法输入（运维可能只是想
// 验证策略名校验逻辑）。
func TestParseDisksFact_EmptyFactsReturnsEmptyNotError(t *testing.T) {
	disks := parseDisksFact(map[string]string{})
	if len(disks) != 0 {
		t.Fatalf("got %+v, want 空切片", disks)
	}
}

// TestDryRun_UnknownPolicyRejected 锁住「未知策略名被拒」。
//
// gin 迁移后这条校验从手写的 map 查表改成了 binding:"oneof=..." 标签
// （dryrun.go 的 diskDryRunRequest 文档有说明），因此判定路径变成
// gin/validator 的 validator.ValidationErrors，apierr.Fail 按迁移契约
// §3 把它翻译成 422 + CodeValidationFailed，不再是手写校验时代的
// 200 + CodeBadRequest——这是把校验从 Go 代码换成标签的直接后果（契约
// §3 明确写了 Fail 要识别 validator.ValidationErrors），不是本次改动
// 意外弱化了这条校验：未知策略名依然会被拒绝，只是拒绝时携带的状态码/
// 业务码从「参数错误」类换成了「校验失败」类，语义上更贴切（这本来就是
// 一条格式/取值校验，不是查库之后才能判定的业务冲突）。
func TestDryRun_UnknownPolicyRejected(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "not-a-real-policy",
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("未知策略名应回 HTTP 422，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeValidationFailed) {
		t.Errorf("未知策略名应回 CodeValidationFailed(%d)，实际 code=%d", apierr.CodeValidationFailed, env.Code)
	}
}

// TestDryRun_EmptyPolicyRejected 锁住「diskPolicy 缺失或空串被拒」——
// diskDryRunRequest.DiskPolicy 的 binding:"required" 标签顶替的是原来
// 手写的 strings.TrimSpace(req.DiskPolicy)=="" 判断，同样走 422 +
// CodeValidationFailed（见上面 TestDryRun_UnknownPolicyRejected 的
// 说明：本文件里凡是从手写校验搬到 binding 标签的检查，判定路径都统一
// 变成 validator.ValidationErrors）。
func TestDryRun_EmptyPolicyRejected(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/disk-policy/dryrun", tok, map[string]any{
		"diskPolicy": "",
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("diskPolicy 为空应回 HTTP 422，实际 %d, body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeValidationFailed) {
		t.Errorf("diskPolicy 为空应回 CodeValidationFailed(%d)，实际 code=%d", apierr.CodeValidationFailed, env.Code)
	}
}
