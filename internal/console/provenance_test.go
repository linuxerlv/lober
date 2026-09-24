// 本文件测试 P4-04 任务 1 的任务溯源视图，依据该计划「测试要锁住的行为」
// 五条，以及它的驱动场景「误装事故的四个问题」。
package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// TestTaskProvenance_ResolvesAllPinnedReferences 验证溯源视图把 pin 的三项
// （模板、制品集、hook digest 列表）全部解析成可读信息。
//
// 这是本任务的核心价值（计划任务 1 原话）：光给 digest 与版本号，运维还要
// 自己去查三张表；解析好了就是一屏能看完的信息。事故发生后的处置速度取决于
// 这一屏。
func TestTaskProvenance_ResolvesAllPinnedReferences(t *testing.T) {
	ts := newTestServer(t)
	taskID := seedProvenanceTask(t, ts)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/tasks/"+taskID+"/provenance", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data taskProvenance `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)
	p := resp.Data

	// 第一块：创建信息（操作人、时间、客户端 IP）——回答「谁在什么时候建了
	// 那个任务」。
	if p.CreatedBy != "eve-admin" || p.CreatedAt != 1000 {
		t.Errorf("创建信息 = %q/%d, want eve-admin/1000", p.CreatedBy, p.CreatedAt)
	}
	if p.CreatedFromIP != "10.0.0.9" {
		t.Errorf("CreatedFromIP = %q, want 10.0.0.9 —— 客户端 IP 从审计记录的 detail 里取，"+
			"它是「谁建的」之外的第二条线索（同一账号被多人共用时只能靠 IP 区分）", p.CreatedFromIP)
	}

	// 第二块：pin 的三项都解析成可读信息——回答「当时选的是哪台机器、哪个
	// 模板、哪个制品集」。
	if p.Template.Name != "rocky9-标准" || p.Template.Version != 1 {
		t.Errorf("Template = %+v, want name=rocky9-标准 version=1 —— "+
			"只给 templateId 与版本号，运维还要自己去查模板表", p.Template)
	}
	if p.ArtifactSet.Name != "rocky9.4-x86_64" {
		t.Errorf("ArtifactSet.Name = %q, want rocky9.4-x86_64", p.ArtifactSet.Name)
	}

	// hook 的作者与审批人必须关联出来——回答「那个任务用了哪些 hook？谁写的、
	// 谁批的」。hook 以 root 身份在分区之前运行（§9.4），事故后这两个名字是
	// 追责链上最关键的一环。
	if len(p.Hooks) != 1 {
		t.Fatalf("Hooks 有 %d 项, want 1: %+v", len(p.Hooks), p.Hooks)
	}
	h := p.Hooks[0]
	if h.Name != "配置业务网卡" || h.Author != "carol-author" || h.Approver != "dave-approver" {
		t.Errorf("Hook = %+v, want name=配置业务网卡 author=carol-author approver=dave-approver", h)
	}

	// 第三块：完整事件时间线——回答「从建任务到擦盘之间发生了什么」。
	if len(p.Timeline) != 1 || p.Timeline[0].Message != "取用引导菜单" {
		t.Errorf("Timeline = %+v, want 1 条「取用引导菜单」", p.Timeline)
	}
}

// TestTaskProvenance_DeletedReferenceShowsHintNotError 验证被引用对象已被删除
// 时显示提示而**不是**报错（计划任务 1 的一条数据完整性要求）。
//
// 为什么这条必须有测试（计划原话）：事故往往发生在几个月后，而那时相关对象
// 可能已经清理过。§2.7 只保证「被 active 模板引用的 hook 不能删」，模板本身
// 在任务终态后是可以删的——此时若溯源视图直接 500，运维在最需要它的时刻拿不
// 到任何信息。
func TestTaskProvenance_DeletedReferenceShowsHintNotError(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	// 任务 pin 了一个根本不存在的模板、制品集与 hook digest——等价于「建完
	// 任务之后这三个对象都被删了」。
	tk := model.Task{
		ID: "task-orphan", CreatedAt: 2000, CreatedBy: "eve-admin",
		HostSN: "SN-ORPHAN", TemplateID: "tpl-已删除", TemplateVersion: 7,
		ArtifactSetID: "art-已删除", HookDigests: []string{"digest-已删除"},
		FormData: map[string]string{}, ResolvedParams: map[string]string{},
		State: model.TaskStateFailed, StateUpdatedAt: 2000,
		CurrentPhase: model.PhasePending, PhaseEnteredAt: 2000,
	}
	if _, err := ts.Store.CreateTask(ctx, tk); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/tasks/task-orphan/provenance", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200 —— 被引用对象已删除是正常情况（事故往往"+
			"发生在几个月后，那时对象可能已清理），必须显示提示而不是报错。body=%s",
			rr.Code, rr.Body.String())
	}
	var resp struct {
		Data taskProvenance `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)
	p := resp.Data

	if !p.Template.Missing {
		t.Errorf("Template.Missing = false, want true —— 查不到的引用要显式标记，" +
			"不能只留一个空名字让运维以为是数据没加载出来")
	}
	// pin 的版本号必须仍然给出来（计划任务 1 原话：「显示『该模板已删除』并
	// 给出 pin 的版本号」）——版本号是从备份里找回那一版模板的唯一线索。
	if p.Template.Version != 7 {
		t.Errorf("Template.Version = %d, want 7 —— 对象没了，pin 的版本号仍是"+
			"从备份里找回那一版的唯一线索", p.Template.Version)
	}
	if !p.ArtifactSet.Missing {
		t.Errorf("ArtifactSet.Missing = false, want true")
	}
	if len(p.Hooks) != 1 || !p.Hooks[0].Missing {
		t.Errorf("Hooks = %+v, want 1 项且 Missing=true —— digest 查不到时仍要"+
			"列出那个 digest，它是「当时到底跑了什么」的唯一记录", p.Hooks)
	}
	if p.Hooks[0].Digest != "digest-已删除" {
		t.Errorf("Hooks[0].Digest = %q, want digest-已删除", p.Hooks[0].Digest)
	}
}

// TestTaskProvenance_IncludesSubsequentOperations 验证第五块「后续操作」把
// 重试、取消等记录带出来，含 P4-01 的自动重试（计划任务 1 测试要求）。
func TestTaskProvenance_IncludesSubsequentOperations(t *testing.T) {
	ts := newTestServer(t)
	taskID := seedProvenanceTask(t, ts)
	ctx := context.Background()

	// 一条人工重试、一条自动重试——两者都要出现在后续操作里。自动重试的
	// actor 不是人（P4-01 的自动重试由扫描循环发起），溯源视图不能因为
	// 「没有操作人」就把它漏掉：一台机器被自动重试过三次这件事，正是事故
	// 复盘时要看的。
	for _, rec := range []contract.AuditRecord{
		{TS: 1100, Actor: "eve-admin", Action: "task.retry", Target: taskID, Detail: "client_ip=10.0.0.9"},
		{TS: 1200, Actor: "system", Action: "task.auto_retry", Target: taskID, Detail: "reason=boot_timeout"},
	} {
		if err := ts.Store.AppendAudit(ctx, rec); err != nil {
			t.Fatalf("AppendAudit 失败: %v", err)
		}
	}

	tok := ts.login("eve-admin")
	rr := ts.doJSON("GET", "/console/v1/tasks/"+taskID+"/provenance", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data taskProvenance `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &resp)

	actions := map[string]bool{}
	for _, op := range resp.Data.Operations {
		actions[op.Action] = true
	}
	if !actions["task.retry"] || !actions["task.auto_retry"] {
		t.Errorf("Operations 的动作集合 = %v, want 含 task.retry 与 task.auto_retry —— "+
			"自动重试的 actor 不是人，不能因为「没有操作人」就漏掉它", actions)
	}
	// task.create 属于第一块「创建信息」，不该在后续操作里重复出现。
	if actions["task.create"] {
		t.Errorf("Operations 里出现了 task.create —— 它属于第一块创建信息，" +
			"重复列出会让时间线上同一件事出现两次")
	}
}

// TestTaskProvenance_NonAdminForbidden 验证非管理员访问被拒。
//
// 溯源视图含操作人信息（计划任务 1 的权限一节）。§9.4 没有定义「审计角色」，
// 加一项角色是契约变更，因此先只给管理员。
func TestTaskProvenance_NonAdminForbidden(t *testing.T) {
	ts := newTestServer(t)
	taskID := seedProvenanceTask(t, ts)
	for _, username := range []string{"alice-viewer", "bob-operator", "carol-author", "dave-approver"} {
		t.Run(username, func(t *testing.T) {
			rr := ts.doJSON("GET", "/console/v1/tasks/"+taskID+"/provenance", ts.login(username), nil)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s 访问溯源视图状态码 = %d, want 403", username, rr.Code)
			}
		})
	}
}

// TestTaskProvenance_TaskNotFound 验证任务本身不存在时返回 404，与「任务存在
// 但引用已删除」这种正常情况区分开。
func TestTaskProvenance_TaskNotFound(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("GET", "/console/v1/tasks/task-根本不存在/provenance", tok, nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d, want 404 —— 任务不存在与「任务存在但 pin 的对象已删除」"+
			"是两种不同情况，前者是请求错了，后者是正常的历史数据", rr.Code)
	}
}
