// 本文件测试 P4-04 的审计查询与三张装机统计报表端点。
//
// 依据 docs/superpowers/plans/2026-09-20-lober-phase4-04-audit.md 任务 2
// 与任务 3 的「测试要锁住的行为」，以及 SPEC §9.4（权限分级）、§3.4（失败
// 原因分类的诊断含义）。
package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// seedAuditRecords 往 fake.Store 写一组 actor × action 交叉的审计记录，
// 供审计查询各条子测试共用。
func seedAuditRecords(t *testing.T, ts *testServer) {
	t.Helper()
	records := []contract.AuditRecord{
		{TS: 1000, Actor: "alice", Action: "task.create", Target: "task-1", Detail: "d1"},
		{TS: 2000, Actor: "alice", Action: "task.cancel", Target: "task-1", Detail: "d2"},
		{TS: 3000, Actor: "bob", Action: "task.create", Target: "task-2", Detail: "d3"},
		{TS: 4000, Actor: "bob", Action: "hook.approve", Target: "hook-x", Detail: "d4"},
	}
	for i, r := range records {
		if err := ts.Store.AppendAudit(context.Background(), r); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}
}

// TestAuditQuery_FourFiltersComposable 验证 GET /audit 的四项查询条件各自
// 与组合生效（计划任务 2：操作人、时间范围、动作类型、目标资源）。
func TestAuditQuery_FourFiltersComposable(t *testing.T) {
	ts := newTestServer(t)
	seedAuditRecords(t, ts)
	tok := ts.login("eve-admin")

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"无条件返回全部", "", 4},
		{"按操作人", "?actor=alice", 2},
		{"按动作类型", "?action=task.create", 2},
		{"按目标资源", "?target=task-1", 2},
		{"按时间范围闭区间", "?since=2000&until=3000", 2},
		{"操作人加动作组合", "?actor=bob&action=task.create", 1},
		{"四项全给", "?actor=alice&action=task.create&target=task-1&since=500&until=1500", 1},
		{"组合无交集返回空", "?actor=alice&action=hook.approve", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := ts.doJSON("GET", "/console/v1/audit"+c.query, tok, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET /audit%s 状态码 = %d, want 200, body=%s", c.query, rr.Code, rr.Body.String())
			}
			var resp struct {
				Code int               `json:"code"`
				Data []auditRecordItem `json:"data"`
			}
			decodeJSON(t, rr.Body.Bytes(), &resp)
			if len(resp.Data) != c.want {
				t.Errorf("GET /audit%s 返回 %d 条, want %d —— 四项条件必须按「与」相交，"+
					"写成「或」会让运维在几十万条里翻不出想要的那几条",
					c.query, len(resp.Data), c.want)
			}
		})
	}
}

// TestAuditQuery_NonAdminForbidden 验证非管理员访问审计查询被拒（计划任务 2
// 与任务 1 的权限要求一致：审计含操作人信息，只给管理员）。
func TestAuditQuery_NonAdminForbidden(t *testing.T) {
	ts := newTestServer(t)
	for _, username := range []string{"alice-viewer", "bob-operator", "carol-author", "dave-approver"} {
		t.Run(username, func(t *testing.T) {
			rr := ts.doJSON("GET", "/console/v1/audit", ts.login(username), nil)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s 访问 GET /audit 状态码 = %d, want 403 —— "+
					"审计记录含全体操作人的动作明细，泄露给非管理员等于把「谁做了什么」"+
					"公开给所有登录用户", username, rr.Code)
			}
		})
	}
}

// TestAuditQuery_DefaultLimitApplied 验证未显式传 limit 时套用默认上限，
// 不做无界查询。
//
// contract.AuditFilter.Limit 的零值语义是「不限制」，而审计表会长到几十万条
// （计划任务 2 的规模问题）——因此默认上限必须由 HTTP 层补上，不能把零值
// 原样透给 Store。
func TestAuditQuery_DefaultLimitApplied(t *testing.T) {
	ts := newTestServer(t)
	for i := 0; i < 5; i++ {
		if err := ts.Store.AppendAudit(context.Background(), contract.AuditRecord{
			TS: int64(1000 + i), Actor: "alice", Action: "task.create", Target: "t", Detail: "d",
		}); err != nil {
			t.Fatalf("AppendAudit(#%d) 失败: %v", i, err)
		}
	}
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/audit?limit=2", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []auditRecordItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if len(resp.Data) != 2 {
		t.Fatalf("limit=2 返回 %d 条, want 2", len(resp.Data))
	}
	// 倒序：最新的在前（§9.4 事后追责，最近的记录价值最高）。
	if resp.Data[0].TS != 1004 || resp.Data[1].TS != 1003 {
		t.Errorf("limit=2 返回的 TS = [%d, %d], want [1004, 1003] —— "+
			"必须是「倒序之后取前 N 条」而不是「任取 N 条」",
			resp.Data[0].TS, resp.Data[1].TS)
	}
}

// seedTasksForReports 建一批任务并推到各自的终态，供三张报表的子测试共用。
// 返回建好的任务 id 列表，顺序与 specs 一致。
func seedTasksForReports(t *testing.T, ts *testServer) {
	t.Helper()
	ctx := context.Background()

	specs := []struct {
		sn     string
		state  model.TaskState
		reason model.FailureReason
	}{
		{"SN-D1", model.TaskStateDone, ""},
		{"SN-D2", model.TaskStateDone, ""},
		{"SN-D3", model.TaskStateDone, ""},
		{"SN-F1", model.TaskStateFailed, model.FailureReasonPrecheck},
		{"SN-F2", model.TaskStateFailed, model.FailureReasonPrecheck},
		{"SN-F3", model.TaskStateFailed, model.FailureReasonBootTimeout},
	}

	for i, spec := range specs {
		tk := model.Task{
			ID:             "task-report-" + spec.sn,
			CreatedAt:      int64(10000 + i*100),
			CreatedBy:      "eve-admin",
			HostSN:         spec.sn,
			TemplateID:     "tpl-1",
			ArtifactSetID:  "art-1",
			FormData:       map[string]string{},
			ResolvedParams: map[string]string{},
			State:          model.TaskStatePending,
			StateUpdatedAt: int64(10000 + i*100),
			CurrentPhase:   model.PhasePending,
			PhaseEnteredAt: int64(10000 + i*100),
		}
		created, err := ts.Store.CreateTask(ctx, tk)
		if err != nil {
			t.Fatalf("CreateTask(%s) 失败: %v", spec.sn, err)
		}
		moved, err := ts.Store.CASTaskState(ctx, created.ID, model.TaskStatePending, created.Version, spec.state)
		if err != nil {
			t.Fatalf("CASTaskState(%s → %s) 失败: %v", spec.sn, spec.state, err)
		}
		if spec.reason != "" {
			r := spec.reason
			if _, err := ts.Store.MutateTaskFields(ctx, moved.ID, model.TaskMutation{FailureReason: &r}); err != nil {
				t.Fatalf("MutateTaskFields(%s 写失败原因) 失败: %v", spec.sn, err)
			}
		}
	}
}

// TestReports_ProvisioningCountsByState 验证装机量报表按状态分组计数
// （计划任务 3 第一张表：容量规划与汇报用）。
func TestReports_ProvisioningCountsByState(t *testing.T) {
	ts := newTestServer(t)
	seedTasksForReports(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/reports/provisioning", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data provisioningReport `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)

	if resp.Data.Total != 6 {
		t.Errorf("Total = %d, want 6", resp.Data.Total)
	}
	byState := map[string]int{}
	for _, row := range resp.Data.ByState {
		byState[row.State] = row.Count
	}
	if byState["done"] != 3 || byState["failed"] != 3 {
		t.Errorf("ByState = %+v, want done=3 failed=3", resp.Data.ByState)
	}
}

// TestReports_FailureBreakdownWithShare 验证失败分布报表按失败原因分组，
// 给出计数与占比（计划任务 3 第二张表，「最有价值的一张」）。
//
// 占比必须给：只给计数的话运维无法判断「precheck 失败 12 次」是严重问题
// 还是噪音——12 次占 12% 与占 80% 是两种完全不同的处置优先级。
func TestReports_FailureBreakdownWithShare(t *testing.T) {
	ts := newTestServer(t)
	seedTasksForReports(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/reports/failures", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data failureReport `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)

	if resp.Data.TotalFailed != 3 {
		t.Fatalf("TotalFailed = %d, want 3", resp.Data.TotalFailed)
	}
	// 降序：占比最高的排最前，运维一眼看到最该改进的环节。
	if len(resp.Data.ByReason) != 2 {
		t.Fatalf("ByReason 有 %d 项, want 2（precheck 与 boot_timeout）: %+v",
			len(resp.Data.ByReason), resp.Data.ByReason)
	}
	top := resp.Data.ByReason[0]
	if top.Reason != string(model.FailureReasonPrecheck) || top.Count != 2 {
		t.Errorf("ByReason[0] = %+v, want precheck/2 —— 必须按计数降序，"+
			"占比最高的排最前", top)
	}
	// 2/3 ≈ 0.6667，允许浮点误差。
	if top.Share < 0.66 || top.Share > 0.67 {
		t.Errorf("ByReason[0].Share = %v, want ≈0.6667", top.Share)
	}
	// 可重装标记：§3.4 的分类有诊断含义，precheck 可重装、init_failed 不可。
	if !top.Reinstallable {
		t.Errorf("precheck 的 Reinstallable = false, want true（§3.4 白名单）")
	}
}

// TestReports_FailureBreakdownDrillDown 验证失败分布能下钻到具体任务列表
// （计划任务 3：「不可下钻就只是一个数字」）。
func TestReports_FailureBreakdownDrillDown(t *testing.T) {
	ts := newTestServer(t)
	seedTasksForReports(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/tasks?failureReason=precheck", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data []taskItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)
	if len(resp.Data) != 2 {
		t.Fatalf("GET /tasks?failureReason=precheck 返回 %d 条, want 2 —— "+
			"失败分布报表要能点进去看是哪几个任务、是不是同一个人建的（§10 失败模式 2b 的信号）",
			len(resp.Data))
	}
	for _, item := range resp.Data {
		if item.FailureReason != string(model.FailureReasonPrecheck) {
			t.Errorf("下钻结果里混入 FailureReason=%q 的任务", item.FailureReason)
		}
	}
}

// seedPhaseEvents 给一个任务写一串带阶段与时间戳的事件，模拟完整或不完整的
// 阶段序列。phases 里每项是「阶段名 → 进入该阶段的时刻」。
func seedPhaseEvents(t *testing.T, ts *testServer, taskID string, phases []struct {
	phase model.Phase
	ts    int64
}) {
	t.Helper()
	ctx := context.Background()
	for i, p := range phases {
		if _, err := ts.Store.AppendEvent(ctx, model.TaskEvent{
			TaskID:  taskID,
			Seq:     i + 1,
			TS:      p.ts,
			Phase:   p.phase,
			Level:   model.EventLevelInfo,
			Message: "进入阶段 " + string(p.phase),
			Source:  model.EventSourceSignal,
		}); err != nil {
			t.Fatalf("AppendEvent(%s #%d) 失败: %v", taskID, i, err)
		}
	}
}

// TestReports_PhaseDurationsSkipIncompleteTasks 验证阶段耗时报表从事件表算，
// 且**跳过**数据不完整的任务而不是把缺失阶段算成 0（计划任务 3 明确要求）。
//
// 为什么这条最容易写错：中途失败的任务只有部分阶段有事件。把缺失阶段当 0
// 会把中位数往下拽——失败越多，报表显示的耗时越"好看"，与真实情况完全相反，
// 而这种错误在数据上看起来完全正常，不会有任何报错。
func TestReports_PhaseDurationsSkipIncompleteTasks(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	// 三个任务：两个有完整的 booting→pre 序列，一个只进了 booting 就没了。
	for i, sn := range []string{"SN-P1", "SN-P2", "SN-P3"} {
		tk := model.Task{
			ID: "task-phase-" + sn, CreatedAt: int64(20000 + i*100), CreatedBy: "eve-admin",
			HostSN: sn, TemplateID: "tpl-1", ArtifactSetID: "art-1",
			FormData: map[string]string{}, ResolvedParams: map[string]string{},
			State: model.TaskStatePending, StateUpdatedAt: int64(20000 + i*100),
			CurrentPhase: model.PhasePending, PhaseEnteredAt: int64(20000 + i*100),
		}
		if _, err := ts.Store.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask(%s) 失败: %v", sn, err)
		}
	}

	type phaseAt = struct {
		phase model.Phase
		ts    int64
	}
	// booting 耗时 10 秒与 30 秒两个样本；第三个任务只有 booting 起点，
	// 没有下一个阶段，因此 booting 耗时无法计算，必须被跳过。
	seedPhaseEvents(t, ts, "task-phase-SN-P1", []phaseAt{
		{model.PhaseBooting, 1000}, {model.PhasePre, 1010},
	})
	seedPhaseEvents(t, ts, "task-phase-SN-P2", []phaseAt{
		{model.PhaseBooting, 2000}, {model.PhasePre, 2030},
	})
	seedPhaseEvents(t, ts, "task-phase-SN-P3", []phaseAt{
		{model.PhaseBooting, 3000},
	})

	tok := ts.login("eve-admin")
	rr := ts.doJSON("GET", "/console/v1/reports/phase-durations", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data phaseDurationReport `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)

	var booting *phaseDurationRow
	for i := range resp.Data.ByPhase {
		if resp.Data.ByPhase[i].Phase == string(model.PhaseBooting) {
			booting = &resp.Data.ByPhase[i]
		}
	}
	if booting == nil {
		t.Fatalf("报表里没有 booting 阶段: %+v", resp.Data.ByPhase)
	}
	if booting.SampleCount != 2 {
		t.Errorf("booting.SampleCount = %d, want 2 —— 只进了 booting 就没有下一个阶段的"+
			"任务算不出耗时，必须跳过而不是算成 0（把缺失当 0 会让失败越多、报表越好看）",
			booting.SampleCount)
	}
	// 两个样本 10 与 30，中位数取 20（偶数个样本取中间两个的均值）。
	if booting.MedianSec != 20 {
		t.Errorf("booting.MedianSec = %d, want 20（样本 10 与 30）", booting.MedianSec)
	}
}

// TestReports_PhaseDurationsUseMedianNotMean 验证统计口径是中位数与高分位，
// 不是平均值（计划任务 3：一条要定死的统计口径）。
//
// 装机耗时分布是长尾的（少数机器因重试或慢盘远超常规），平均值会被异常值
// 拉高到没有参考价值。本测试用一个极端离群样本把两种口径拉开：样本
// {10, 10, 10, 1000} 的平均值是 257.5，中位数是 10——若实现用的是平均值，
// 断言会立刻失败。
func TestReports_PhaseDurationsUseMedianNotMean(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	type phaseAt = struct {
		phase model.Phase
		ts    int64
	}
	durations := []int64{10, 10, 10, 1000}
	for i, d := range durations {
		sn := "SN-M" + string(rune('1'+i))
		tk := model.Task{
			ID: "task-med-" + sn, CreatedAt: int64(30000 + i*100), CreatedBy: "eve-admin",
			HostSN: sn, TemplateID: "tpl-1", ArtifactSetID: "art-1",
			FormData: map[string]string{}, ResolvedParams: map[string]string{},
			State: model.TaskStatePending, StateUpdatedAt: int64(30000 + i*100),
			CurrentPhase: model.PhasePending, PhaseEnteredAt: int64(30000 + i*100),
		}
		if _, err := ts.Store.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask(%s) 失败: %v", sn, err)
		}
		base := int64(100000 + i*10000)
		seedPhaseEvents(t, ts, tk.ID, []phaseAt{
			{model.PhaseBooting, base}, {model.PhasePre, base + d},
		})
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("GET", "/console/v1/reports/phase-durations", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data phaseDurationReport `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)

	var booting *phaseDurationRow
	for i := range resp.Data.ByPhase {
		if resp.Data.ByPhase[i].Phase == string(model.PhaseBooting) {
			booting = &resp.Data.ByPhase[i]
		}
	}
	if booting == nil {
		t.Fatalf("报表里没有 booting 阶段: %+v", resp.Data.ByPhase)
	}
	if booting.MedianSec != 10 {
		t.Errorf("booting.MedianSec = %d, want 10 —— 样本 {10,10,10,1000} 的平均值是 257，"+
			"中位数是 10；报出 257 说明用了平均值，那个数字会被长尾异常值拉到没有参考价值",
			booting.MedianSec)
	}
	// 高分位要反映出那个离群样本的存在，否则报表看不出「有机器特别慢」。
	if booting.P95Sec < 1000 {
		t.Errorf("booting.P95Sec = %d, want ≥1000 —— 高分位的用途正是暴露长尾，"+
			"若它也是 10 就说明分位数算错了", booting.P95Sec)
	}
}

// TestReports_NonAdminForbidden 验证三张报表都只给管理员。
func TestReports_NonAdminForbidden(t *testing.T) {
	ts := newTestServer(t)
	paths := []string{
		"/console/v1/reports/provisioning",
		"/console/v1/reports/failures",
		"/console/v1/reports/phase-durations",
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
