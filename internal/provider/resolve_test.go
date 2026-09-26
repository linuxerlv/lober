package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// failingResolver 是可控失败的 TemplateResolver 测试替身。
type failingResolver struct {
	err error
	ref contract.TemplateRef
}

func (f failingResolver) Resolve(ctx context.Context, form map[string]string, facts contract.HWFacts) (contract.TemplateRef, error) {
	if f.err != nil {
		return contract.TemplateRef{}, f.err
	}
	return f.ref, nil
}

// failingDiskPlanner 是可控失败的 DiskPlanner 测试替身。
type failingDiskPlanner struct {
	err    error
	layout contract.Layout
}

func (f failingDiskPlanner) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	if f.err != nil {
		return contract.Layout{}, f.err
	}
	return f.layout, nil
}

// validFormData 里的 hostname 键改用裸键 "hostname"（本轨修复的核心）：
// scripts/e2e.sh 的建任务请求体（"formData":{"hostname":"e2e-host-01"}）
// 与 SPEC §8.3 一致确认表单字段是裸键，不是 model.FactHostname
// （"LOBER_HOSTNAME"，那是注入给机器侧 hook 的 fact 命名空间键，与表单键
// 是两个不同的命名空间）。修复前用 model.FactHostname 当键去读表单，
// Resolve 读到的永远是空字符串。
func validFormData() map[string]string {
	return map[string]string{
		"hostname":               "web-01",
		staticIPAMFormKeyIP:      "10.0.1.5",
		staticIPAMFormKeyGateway: "10.0.1.1",
		staticIPAMFormKeyMask:    "255.255.255.0",
	}
}

// validFacts 里的磁盘事实改用 LOBER_INSPECT_DISK_ 前缀（本轨修复：
// decodeDisksFromFacts 现在调用 ParseHWDisks，消费的是 render/inspect.go
// 真机采集脚本实际产出的键格式，不再是本文件历史上自造的 "|" 分隔格式）。
func validFacts() map[string]string {
	return map[string]string{
		model.FactOSFamily:                "rhel",
		model.FactVendor:                  "Dell",
		model.FactProduct:                 "R740",
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "500000000000",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "false",
		"LOBER_INSPECT_DISK_0_MODEL":      "SSD",
	}
}

// recordingDiskPlanner 包一层真实的 diskPlannerFirst，额外记录收到的
// disks 参数——用于证明编排器确实把 decodeDisksFromFacts 解析出的磁盘
// 清单转交给了磁盘规划器（而不是仅仅证明"某个键出现在返回值里"，因为
// 本轨修复后返回值不再携带 disk_primary，见下面测试的说明）。
type recordingDiskPlanner struct {
	inner      contract.DiskPlanner
	gotDisks   []contract.Disk
	planCalled bool
}

func (r *recordingDiskPlanner) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	r.planCalled = true
	r.gotDisks = disks
	return r.inner.Plan(ctx, disks, intent)
}

// TestResolveTaskParams_AllSucceedMergesAllExpectedKeys 锁住编排器合并
// 三个 provider 结果之后、最终返回值只包含 InlineParams 白名单认得的
// 裸键（hostname/biz_ip）这一核心行为——这是本轨修复的核心断言，修复前
// 会失败：Resolve 读表单用的是 model.FactHostname 这个键，读到的是空
// 字符串，成功合并的只有 biz_ip。
//
// 本测试同时断言 biz_gw/biz_mask/disk_primary/template_id/
// template_version 五个键不出现在返回值中——不是"这些决策没有发生"，而是
// "它们发生了，但按照 render/params.go 的 InlineParams 白名单硬约束，
// 不能出现在 ResolvedParams 里"（详见 resolve.go 的 Resolve 方法末尾
// 注释）。disk_primary 确实被正确计算这件事，改用 recordingDiskPlanner
// 记录 Plan 收到的磁盘清单来验证，而不是检查返回值里有没有这个键——
// 这不是弱化断言，是把"编排器把磁盘决策接对了"这件事换一个仍然真实、
// 仍然会在接线错误时失败的观测点来验证。
func TestResolveTaskParams_AllSucceedMergesAllExpectedKeys(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1", TemplateVersion: 2}}
	diskPlanner := &recordingDiskPlanner{inner: diskPlannerFirst{}}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams
	if err != nil {
		t.Fatalf("三个 provider 都成功时不应报错: %v", err)
	}

	want := map[string]string{
		"hostname": "web-01",
		"biz_ip":   "10.0.1.5",
	}
	if len(params) != len(want) {
		t.Fatalf("返回值键数量 = %d, want %d（返回值只应包含 InlineParams 白名单裸键），got %+v",
			len(params), len(want), params)
	}
	for k, v := range want {
		if params[k] != v {
			t.Errorf("键 %q = %q, want %q，got %+v", k, params[k], v, params)
		}
	}

	// InlineParams 白名单认不出的键必须缺席——留下任何一个都会让六个
	// 家族的渲染器在渲染应答文件这一步 100% 报错（本轨修复要解决的正是
	// 这个真实故障，见 resolve.go Resolve 方法末尾的详细说明）。
	forbidden := []string{model.FactBizGW, model.FactBizMask, model.FactDiskPrimary,
		"template_id", "template_version"}
	for _, k := range forbidden {
		if v, ok := params[k]; ok {
			t.Errorf("键 %q 不应出现在返回值中（InlineParams 白名单认不出），但 got 值=%q，完整结果=%+v",
				k, v, params)
		}
	}

	// 磁盘决策确实发生、确实收到了 decodeDisksFromFacts 解析出的磁盘——
	// 用调用记录验证，因为该决策结果按上面的断言不应出现在返回值里。
	if !diskPlanner.planCalled {
		t.Fatal("磁盘规划器的 Plan 未被调用——编排器没有把磁盘决策接上")
	}
	if len(diskPlanner.gotDisks) != 1 || diskPlanner.gotDisks[0].ByIDPath != "/dev/disk/by-id/aaa" {
		t.Errorf("磁盘规划器收到的磁盘清单不对，got %+v", diskPlanner.gotDisks)
	}
}

func TestResolveTaskParams_TemplateResolverFailureFailsOverallNoPartialResult(t *testing.T) {
	resolver := failingResolver{err: errors.New("模板解析失败")}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams

	if err == nil {
		t.Fatal("模板解析失败时整体必须返回错误")
	}
	if params != nil {
		t.Fatalf("失败时不应产生部分结果，got %+v，want nil——避免调用方误用部分参数", params)
	}
}

func TestResolveTaskParams_IPAMFailureFailsOverallNoPartialResult(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	// static IPAM 天然不会失败（它只是把表单值原样传出），因此要验证
	// 「IPAM 步骤失败」这条路径，需要绕过 NewTaskParamsResolver 默认装配
	// 的 static 实现，直接构造 taskParamsResolver 并注入一个可控失败的
	// ipamFactory——本测试文件与被测类型同包，可以直接访问私有字段。
	orch := &taskParamsResolver{
		resolver:    resolver,
		diskPlanner: diskPlanner,
		diskIntent:  contract.DiskIntent{},
		ipamFactory: func(form map[string]string) contract.IPAMProviderV1 {
			return failingIPAM{err: errors.New("IPAM 失败")}
		},
	}

	res, err := orch.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams
	if err == nil {
		t.Fatal("IPAM 失败时整体必须返回错误")
	}
	if params != nil {
		t.Fatalf("失败时不应产生部分结果，got %+v", params)
	}
}

func TestResolveTaskParams_DiskPlannerFailureFailsOverallNoPartialResult(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := failingDiskPlanner{err: errors.New("磁盘规划失败")}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams

	if err == nil {
		t.Fatal("磁盘规划失败时整体必须返回错误")
	}
	if params != nil {
		t.Fatalf("失败时不应产生部分结果，got %+v", params)
	}
}

// TestResolveTaskParams_ByHookDiskPolicy_OrchestratorSucceedsNoPartialFailure
// 锁住 by-hook 磁盘策略与同步编排器组合时的行为：by-hook 契约上返回
// len(Selected)==1 但 Disk 是零值（不做服务端决策，选盘逻辑推到 %pre 阶段
// 的 hook，SPEC §5.6）。编排器必须整体成功（这不是失败，是「没有服务端
// 决策」这一种合法结果）。
//
// 本测试历史上还断言过 model.FactDiskPrimary 键不出现在返回值中（防止
// %pre 的 hook 脚本把空字符串误当成真实但为空的盘路径）——本轨修复后这
// 条断言对任何磁盘策略都恒为真（返回值从不携带 disk_primary，见
// resolve.go Resolve 方法末尾说明），不再是 by-hook 特有的行为，因此不
// 再是一条能分辨"改对了"还是"改错了"的有效断言，本测试改为只锁住"by-hook
// 策略下编排器仍然成功、仍然产出白名单裸键"这一件事——"disk_primary 是否
// 该出现在返回值里"已经被上面的
// TestResolveTaskParams_AllSucceedMergesAllExpectedKeys 统一覆盖（对
// 所有磁盘策略都成立），"by-hook 本身选盘行为对不对"由
// diskplanner_test.go 的 TestDiskByHook_ReturnsEmptyPrimaryWithNonEmptyReason
// 单独锁住，不需要在这里重复。
func TestResolveTaskParams_ByHookDiskPolicy_OrchestratorSucceedsNoPartialFailure(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerByHook{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{Policy: "by-hook"})
	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: validFacts(),
	})
	params := res.ResolvedParams

	if err != nil {
		t.Fatalf("by-hook 策略不是失败，编排器不应报错: %v", err)
	}
	if params == nil {
		t.Fatal("by-hook 策略下编排器应正常产出参数（不是失败路径）")
	}
	// 其余键（IPAM/模板解析产出的部分）应照常存在，证明这不是整体失败。
	if params["biz_ip"] != "10.0.1.5" {
		t.Errorf("biz_ip got %q, want 10.0.1.5，完整结果=%+v", params["biz_ip"], params)
	}
	if params["hostname"] != "web-01" {
		t.Errorf("hostname got %q, want web-01，完整结果=%+v", params["hostname"], params)
	}
}

// failingIPAM 是可控失败的 IPAMProviderV1 测试替身。
type failingIPAM struct {
	err error
}

func (f failingIPAM) Allocate(ctx context.Context, req contract.AllocRequest) (contract.AllocResult, error) {
	return contract.AllocResult{}, f.err
}
func (f failingIPAM) Release(ctx context.Context, ip string) error { return nil }
func (f failingIPAM) Ping(ctx context.Context) error               { return nil }

// TestResolveTaskParams_MalformedDiskFactsFailsOverall 锁住的意图不变
// （磁盘采集事实异常时不能让任务带着错误的磁盘决策悄悄建成），但本轨
// 修复后失败路径变了：ParseHWDisks 对畸形字段值的设计是"跳过该条记录，
// 不让它污染其它记录"（hwfacts.go 已有完整测试覆盖，真实硬件上
// lsblk/lspci 输出千奇百怪，不能因为一条坏记录 panic 或让整体解析报错）。
// 因此"全部磁盘记录都畸形"在本轨修复后表现为"有效磁盘清单为空"，整体
// Resolve 仍然失败——只是失败原因从"解析报错"变成"DiskPlanner 面对零块盘
// 报错"（diskplanner.go 的既有行为：零块盘不能返回空布局，SPEC §10 失败
// 模式 5），效果同样是"不产生部分结果、不让运维带着错误磁盘信息把任务
// 建出来"。
func TestResolveTaskParams_MalformedDiskFactsFailsOverall(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	facts := validFacts()
	// 唯一一条磁盘记录的 SIZE 字段畸形——ParseHWDisks 会跳过整条记录，
	// 清单归零，diskPlannerFirst 面对零块盘报错。
	facts["LOBER_INSPECT_DISK_0_SIZE"] = "字段数不对"

	res, err := orchestrator.Resolve(context.Background(), contract.ResolveRequest{
		FormData: validFormData(), Facts: facts,
	})
	params := res.ResolvedParams
	if err == nil {
		t.Fatal("磁盘 facts 格式异常导致有效磁盘清单为空时应返回错误")
	}
	if params != nil {
		t.Fatalf("失败时不应产生部分结果，got %+v", params)
	}
}

func TestDecodeDisksFromFacts_EmptyReturnsNilNoError(t *testing.T) {
	disks, err := decodeDisksFromFacts(map[string]string{})
	if err != nil {
		t.Fatalf("facts 中没有磁盘键时不应报错: %v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("got %+v, want 空切片", disks)
	}
}

func TestDecodeDisksFromFacts_MultipleRecordsParsedCorrectly(t *testing.T) {
	facts := map[string]string{
		"LOBER_INSPECT_DISK_0_BYID":       "/dev/disk/by-id/aaa",
		"LOBER_INSPECT_DISK_0_SIZE":       "500000000000",
		"LOBER_INSPECT_DISK_0_ROTATIONAL": "false",
		"LOBER_INSPECT_DISK_0_MODEL":      "SSD-A",
		"LOBER_INSPECT_DISK_1_BYID":       "/dev/disk/by-id/bbb",
		"LOBER_INSPECT_DISK_1_SIZE":       "2000000000000",
		"LOBER_INSPECT_DISK_1_ROTATIONAL": "true",
		"LOBER_INSPECT_DISK_1_MODEL":      "HDD-B",
	}
	disks, err := decodeDisksFromFacts(facts)
	if err != nil {
		t.Fatalf("合法格式不应报错: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("应解析出 2 块盘，got %d", len(disks))
	}
	if disks[0].ByIDPath != "/dev/disk/by-id/aaa" || disks[0].SizeBytes != 500000000000 || disks[0].Rotational != false || disks[0].Model != "SSD-A" {
		t.Fatalf("第一块盘解析不对: %+v", disks[0])
	}
	if disks[1].ByIDPath != "/dev/disk/by-id/bbb" || disks[1].SizeBytes != 2000000000000 || disks[1].Rotational != true || disks[1].Model != "HDD-B" {
		t.Fatalf("第二块盘解析不对: %+v", disks[1])
	}
}

// ---------------------------------------------------------------------------
// TestLifecycle
// ---------------------------------------------------------------------------

// recordingDNS 记录被调用的 Register/Deregister 参数。
type recordingDNS struct {
	registered   []string
	deregistered []string
}

func (r *recordingDNS) Register(ctx context.Context, fqdn, ip string) error {
	r.registered = append(r.registered, fqdn+"="+ip)
	return nil
}
func (r *recordingDNS) Deregister(ctx context.Context, fqdn string) error {
	r.deregistered = append(r.deregistered, fqdn)
	return nil
}
func (r *recordingDNS) Ping(ctx context.Context) error { return nil }

// recordingNotify 记录被调用的事件类型。
type recordingNotify struct {
	events []contract.LifecycleEvent
}

func (r *recordingNotify) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	r.events = append(r.events, ev.Event)
	return nil
}
func (r *recordingNotify) Ping(ctx context.Context) error { return nil }

func TestLifecycle_OnlyReadyAndFailedTriggerCallbacks(t *testing.T) {
	bus := NewEventBus()
	dns := &recordingDNS{}
	notify := &recordingNotify{}
	RegisterLifecycleHooks(bus, dns, notify)

	// 发布「已创建」事件：不应触发任何 DNS 写入或通知——这两个不是
	// 生命周期订阅关心的事件（任务 8 明确要求）。
	bus.Publish(contract.LifecycleEventTaskCreated, model.Task{ID: "task-1"})

	if len(dns.registered) != 0 {
		t.Fatalf("task.created 不应触发 DNS 写入，got %v", dns.registered)
	}
	if len(notify.events) != 0 {
		t.Fatalf("task.created 不应触发通知，got %v", notify.events)
	}
}

func TestLifecycle_ReadyTriggersDNSRegistrationAndNotify(t *testing.T) {
	bus := NewEventBus()
	dns := &recordingDNS{}
	notify := &recordingNotify{}
	RegisterLifecycleHooks(bus, dns, notify)

	// 键改用裸键 "hostname"/"biz_ip"——与 resolve.go 的 Resolve 现在写回
	// ResolvedParams 时使用的键名一致（也是 lifecycle.go 的 onTaskReady
	// 现在读取的键名），不再是 fact 命名空间常量 model.FactHostname/
	// model.FactBizIP（那两个是注入给机器侧 hook 的 Facts 命名空间键）。
	tk := model.Task{
		ID: "task-1",
		ResolvedParams: map[string]string{
			"hostname": "web-01",
			"biz_ip":   "10.0.1.5",
		},
	}
	bus.Publish(contract.LifecycleEventTaskReady, tk)

	if len(dns.registered) != 1 || dns.registered[0] != "web-01=10.0.1.5" {
		t.Fatalf("ready 事件应触发 DNS 注册，got %v", dns.registered)
	}
	if len(notify.events) != 1 || notify.events[0] != contract.LifecycleEventTaskReady {
		t.Fatalf("ready 事件应触发通知，got %v", notify.events)
	}
}

func TestLifecycle_FailedTriggersNotifyOnlyNoDNS(t *testing.T) {
	bus := NewEventBus()
	dns := &recordingDNS{}
	notify := &recordingNotify{}
	RegisterLifecycleHooks(bus, dns, notify)

	tk := model.Task{ID: "task-1", FailureReason: model.FailureReasonInstallPhase}
	bus.Publish(contract.LifecycleEventTaskFailed, tk)

	if len(dns.registered) != 0 || len(dns.deregistered) != 0 {
		t.Fatalf("failed 事件不应触发任何 DNS 操作，got registered=%v deregistered=%v",
			dns.registered, dns.deregistered)
	}
	if len(notify.events) != 1 || notify.events[0] != contract.LifecycleEventTaskFailed {
		t.Fatalf("failed 事件应触发通知，got %v", notify.events)
	}
}
