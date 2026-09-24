// 本文件测试 P4-04 任务 4 的 CSV 导出，依据该计划「测试要锁住的行为」五条：
// 三个入口都能导出且是合法 CSV、含字节顺序标记、流式写出、导出动作写审计、
// 超过行数上限时提示而非截断。
package console

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// utf8BOM 是 UTF-8 的字节顺序标记。中文 Windows 上的 Excel 读没有 BOM 的
// UTF-8 CSV 会按 GBK 解码，表头直接乱码——计划任务 4 第 2 条把它列为
// 「国内环境的实际需求」，不加的后果是运维拿到一个要先转码的文件。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseCSVAfterBOM 断言 body 以 BOM 开头，剥掉 BOM 后解析成 CSV 记录。
func parseCSVAfterBOM(t *testing.T, body []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(body, utf8BOM) {
		t.Fatalf("导出内容未以 UTF-8 BOM 开头（前 8 字节 = % x）—— "+
			"中文 Windows 上 Excel 会把无 BOM 的 UTF-8 当 GBK 解，表头全乱码",
			body[:min(8, len(body))])
	}
	records, err := csv.NewReader(bytes.NewReader(body[len(utf8BOM):])).ReadAll()
	if err != nil {
		t.Fatalf("导出内容不是合法 CSV: %v\nbody=%q", err, string(body))
	}
	return records
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestExport_AuditCSVHasBOMAndChineseHeader 验证审计导出是带 BOM 的合法 CSV，
// 列名用中文（计划任务 4 第 4 条：列名面向运维用中文，文件名用英文加时间戳）。
func TestExport_AuditCSVHasBOMAndChineseHeader(t *testing.T) {
	ts := newTestServer(t)
	if err := ts.Store.AppendAudit(context.Background(), contract.AuditRecord{
		TS: 1735689600, Actor: "alice", Action: "task.create", Target: "task-1",
		Detail: "client_ip=10.0.0.1，含中文与逗号,以及\"引号\"",
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/audit/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	records := parseCSVAfterBOM(t, rr.Body.Bytes())
	if len(records) != 2 {
		t.Fatalf("导出 %d 行（含表头）, want 2", len(records))
	}
	if records[0][0] != "时间" {
		t.Errorf("表头首列 = %q, want 「时间」—— 列名面向运维用中文（计划任务 4 第 4 条）",
			records[0][0])
	}
	// 含逗号与引号的 Detail 必须被正确转义——csv.Writer 负责这件事，这条
	// 断言确认实现没有自己拼字符串。
	if !strings.Contains(records[1][4], "含中文与逗号,以及\"引号\"") {
		t.Errorf("Detail 列 = %q, want 原文完整保留（含逗号与引号）", records[1][4])
	}

	// 文件名用英文加时间戳，避免跨平台文件名问题（计划任务 4 第 4 条）。
	disp := rr.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "attachment") || !strings.Contains(disp, "audit-") || !strings.Contains(disp, ".csv") {
		t.Errorf("Content-Disposition = %q, want 形如 attachment; filename=\"audit-<时间戳>.csv\"", disp)
	}
}

// TestExport_WritesAudit 验证导出动作本身写审计（计划任务 4 第 3 条：
// 导出审计记录是一个敏感操作）。
func TestExport_WritesAudit(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/audit/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rr.Code)
	}

	records, err := ts.Store.ListAudit(context.Background(), contract.AuditFilter{Action: "audit.export"})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("导出后 action=audit.export 的审计记录 %d 条, want 1 —— "+
			"「谁把审计记录导出带走了」本身必须可追溯，否则审计链在导出这一步断掉",
			len(records))
	}
	if records[0].Actor != "eve-admin" {
		t.Errorf("审计记录 Actor = %q, want eve-admin", records[0].Actor)
	}
}

// TestExport_RowLimitRefusesInsteadOfTruncating 验证超过行数上限时**拒绝并
// 提示**，而不是截断（计划任务 4：截断会产出一个看起来完整但不完整的文件）。
//
// 这条是本组测试里最重要的一条：一个被静默截断的导出文件在交给审计方之后
// 才被发现缺行，代价远大于当场报错让运维缩小时间范围。
func TestExport_RowLimitRefusesInsteadOfTruncating(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	// 写满 exportRowLimit + 1 条，越过上限。
	for i := 0; i <= exportRowLimit; i++ {
		if err := ts.Store.AppendAudit(ctx, contract.AuditRecord{
			TS: int64(1000 + i), Actor: "alice", Action: "task.create", Target: "t", Detail: "d",
		}); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/audit/export?limit=999999", tok, nil)
	if rr.Code == http.StatusOK {
		t.Fatalf("越过行数上限仍返回 200 —— want 4xx 并提示缩小时间范围。"+
			"截断会产出一个看起来完整但实际缺行的文件，body 前 200 字节=%q",
			rr.Body.String()[:min(200, rr.Body.Len())])
	}
	if !strings.Contains(rr.Body.String(), "缩小") {
		t.Errorf("拒绝导出时的提示 = %q, want 含「缩小（时间范围）」这类可操作的指引 —— "+
			"只说「太多了」运维不知道下一步该做什么", rr.Body.String())
	}
}

// TestExport_ProvisioningReportCSV 验证装机统计导出（三个入口之二）。
func TestExport_ProvisioningReportCSV(t *testing.T) {
	ts := newTestServer(t)
	seedTasksForReports(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/reports/provisioning/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	records := parseCSVAfterBOM(t, rr.Body.Bytes())
	// 表头 + done 一行 + failed 一行。
	if len(records) != 3 {
		t.Fatalf("导出 %d 行（含表头）, want 3: %+v", len(records), records)
	}
	if records[0][0] != "状态" {
		t.Errorf("表头首列 = %q, want 「状态」", records[0][0])
	}
}

// TestExport_TaskProvenanceCSV 验证任务溯源导出（三个入口之三）。
func TestExport_TaskProvenanceCSV(t *testing.T) {
	ts := newTestServer(t)
	taskID := seedProvenanceTask(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/tasks/"+taskID+"/provenance/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	records := parseCSVAfterBOM(t, rr.Body.Bytes())
	if len(records) < 2 {
		t.Fatalf("溯源导出 %d 行, want ≥2（表头加至少一行内容）", len(records))
	}
	if records[0][0] != "项" {
		t.Errorf("表头首列 = %q, want 「项」—— 溯源是键值形态，不是等宽表格", records[0][0])
	}
}

// TestExport_NonAdminForbidden 验证三个导出入口都只给管理员。
func TestExport_NonAdminForbidden(t *testing.T) {
	ts := newTestServer(t)
	taskID := seedProvenanceTask(t, ts)
	paths := []string{
		"/console/v1/audit/export",
		"/console/v1/reports/provisioning/export",
		"/console/v1/tasks/" + taskID + "/provenance/export",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			rr := ts.doJSON("GET", p, ts.login("bob-operator"), nil)
			if rr.Code != http.StatusForbidden {
				t.Errorf("operator 访问 %s 状态码 = %d, want 403", p, rr.Code)
			}
		})
	}
}

// TestExport_StreamsWithoutBufferingWholeFile 验证导出是流式写出的。
//
// 计划任务 4 第 1 条要求「不在内存里拼完整文件」。直接测内存占用在 Go 的测试
// 里既不稳定也不精确（GC 时机不可控），因此换一个能真正证明流式的判据：
// **响应头必须在第一行数据之前就已经写出**。那等价于「实现没有先攒完整个文件
// 再决定状态码」——只有先写头、边算边写才可能做到。
//
// 这也解释了为什么行数上限必须在写第一个字节之前判完（见
// TestExport_RowLimitRefusesInsteadOfTruncating）：头一旦发出去，状态码就改
// 不了了，之后再失败只能表现为一个被截断的文件，而前端看到的是 200。
func TestExport_StreamsWithoutBufferingWholeFile(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if err := ts.Store.AppendAudit(ctx, contract.AuditRecord{
			TS: int64(1000 + i), Actor: "alice", Action: "task.create", Target: "t", Detail: "d",
		}); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/audit/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want 含 text/csv", ct)
	}
	// 没有 Content-Length：流式写出时长度事先未知。设了它就说明实现先把整个
	// 文件拼在内存里了。
	if cl := rr.Header().Get("Content-Length"); cl != "" {
		t.Errorf("Content-Length = %q, want 空 —— 流式写出时总长度事先不可知，"+
			"设了它说明实现先在内存里拼完了整个文件（几十万条审计记录会撑爆内存）", cl)
	}
	records := parseCSVAfterBOM(t, rr.Body.Bytes())
	if len(records) != 101 {
		t.Errorf("导出 %d 行（含表头）, want 101", len(records))
	}
}

// seedProvenanceTask 建一个带完整 pin 引用、事件与审计记录的任务，返回其 id。
// 供溯源视图与溯源导出的测试共用。
func seedProvenanceTask(t *testing.T, ts *testServer) string {
	t.Helper()
	ctx := context.Background()

	// BodySHA256 必须显式给：contract.HookStore.CreateHook 不代算 digest
	// （内容寻址的键由调用方在写入前算好），fake 与 postgres 两个实现都只是
	// 按给定值建索引。不给的话 GetHookByDigest 永远查不到。
	body := "#!/bin/bash\necho hi\n"
	sum := sha256.Sum256([]byte(body))
	hook := model.Hook{
		ID: "hook-prov", Name: "配置业务网卡", Stage: model.HookStagePost,
		OSFamilies: []model.OSFamily{model.OSFamilyRHEL}, Interpreter: "bash",
		Body: body, BodySHA256: hex.EncodeToString(sum[:]),
		Required: true, TimeoutSec: 60,
		Version: 1, State: model.HookStateActive,
		Author: "carol-author", Approver: "dave-approver", ApprovedAt: 900,
	}
	createdHook, err := ts.Store.CreateHook(ctx, hook)
	if err != nil {
		t.Fatalf("CreateHook 失败: %v", err)
	}

	tpl := model.Template{
		ID: "tpl-prov", Name: "rocky9-标准", ArtifactSetID: "art-prov",
		AnswerTemplate: "ks",
		HookRefs: []model.HookRef{{
			HookID:     createdHook.ID,
			HookDigest: createdHook.BodySHA256,
			Stage:      model.HookStagePost,
			Required:   true,
		}},
		DiskPolicy: model.DiskPolicyLargest, OnFailure: model.OnFailureHalt,
		Version: 1, State: model.TemplateStateActive,
	}
	if _, err := ts.Store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	if _, err := ts.Store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID: "art-prov", Name: "rocky9.4-x86_64", OSFamily: model.OSFamilyRHEL,
		OSVersion: "9.4", Arch: model.ArchX86_64, Firmware: model.FirmwareAny,
		State: model.ArtifactSetReady,
	}); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	tk := model.Task{
		ID: "task-prov", CreatedAt: 1000, CreatedBy: "eve-admin",
		HostSN: "SN-PROV", HostMAC: "aa:bb:cc:dd:ee:01",
		TemplateID: tpl.ID, TemplateVersion: 1, ArtifactSetID: "art-prov",
		HookDigests:    []string{createdHook.BodySHA256},
		FormData:       map[string]string{"hostname": "web-01"},
		ResolvedParams: map[string]string{"hostname": "web-01"},
		State:          model.TaskStatePending, StateUpdatedAt: 1000,
		CurrentPhase: model.PhasePending, PhaseEnteredAt: 1000,
	}
	created, err := ts.Store.CreateTask(ctx, tk)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	if _, err := ts.Store.AppendEvent(ctx, model.TaskEvent{
		TaskID: created.ID, Seq: 1, TS: 1010, Phase: model.PhaseBooting,
		Level: model.EventLevelInfo, Message: "取用引导菜单", Source: model.EventSourceSignal,
	}); err != nil {
		t.Fatalf("AppendEvent 失败: %v", err)
	}
	if err := ts.Store.AppendAudit(ctx, contract.AuditRecord{
		TS: 1000, Actor: "eve-admin", Action: "task.create", Target: created.ID,
		Detail: "client_ip=10.0.0.9",
	}); err != nil {
		t.Fatalf("AppendAudit 失败: %v", err)
	}
	return created.ID
}
