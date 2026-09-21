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

func validFormData() map[string]string {
	return map[string]string{
		model.FactHostname:       "web-01",
		staticIPAMFormKeyIP:      "10.0.1.5",
		staticIPAMFormKeyGateway: "10.0.1.1",
		staticIPAMFormKeyMask:    "255.255.255.0",
	}
}

func validFacts() map[string]string {
	return map[string]string{
		model.FactOSFamily: "rhel",
		model.FactVendor:   "Dell",
		model.FactProduct:  "R740",
		model.FactDisks:    "/dev/disk/by-id/aaa|500000000000|false|SSD",
	}
}

func TestResolveTaskParams_AllSucceedMergesAllExpectedKeys(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1", TemplateVersion: 2}}
	diskPlanner := diskPlannerFirst{} // 用真实策略验证端到端组合

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	params, err := orchestrator.Resolve(context.Background(), validFormData(), validFacts())
	if err != nil {
		t.Fatalf("三个 provider 都成功时不应报错: %v", err)
	}

	wantKeys := []string{
		model.FactHostname, model.FactBizIP, model.FactBizGW, model.FactBizMask,
		model.FactDiskPrimary, "template_id", "template_version",
	}
	for _, k := range wantKeys {
		if _, ok := params[k]; !ok {
			t.Errorf("合并结果缺少预期键 %q，got %+v", k, params)
		}
	}

	if params[model.FactBizIP] != "10.0.1.5" {
		t.Errorf("biz_ip got %q, want 10.0.1.5", params[model.FactBizIP])
	}
	if params[model.FactDiskPrimary] != "/dev/disk/by-id/aaa" {
		t.Errorf("disk_primary got %q, want /dev/disk/by-id/aaa", params[model.FactDiskPrimary])
	}
	if params["template_id"] != "tpl-1" {
		t.Errorf("template_id got %q, want tpl-1", params["template_id"])
	}
}

func TestResolveTaskParams_TemplateResolverFailureFailsOverallNoPartialResult(t *testing.T) {
	resolver := failingResolver{err: errors.New("模板解析失败")}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	params, err := orchestrator.Resolve(context.Background(), validFormData(), validFacts())

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

	params, err := orch.Resolve(context.Background(), validFormData(), validFacts())
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
	params, err := orchestrator.Resolve(context.Background(), validFormData(), validFacts())

	if err == nil {
		t.Fatal("磁盘规划失败时整体必须返回错误")
	}
	if params != nil {
		t.Fatalf("失败时不应产生部分结果，got %+v", params)
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

func TestResolveTaskParams_MalformedDiskFactsFailsOverall(t *testing.T) {
	resolver := failingResolver{ref: contract.TemplateRef{TemplateID: "tpl-1"}}
	diskPlanner := diskPlannerFirst{}

	orchestrator := NewTaskParamsResolver(resolver, diskPlanner, contract.DiskIntent{})
	facts := validFacts()
	facts[model.FactDisks] = "字段数不对|500"

	params, err := orchestrator.Resolve(context.Background(), validFormData(), facts)
	if err == nil {
		t.Fatal("磁盘 facts 格式异常时应返回错误")
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
	if disks != nil {
		t.Fatalf("got %+v, want nil", disks)
	}
}

func TestDecodeDisksFromFacts_MultipleRecordsParsedCorrectly(t *testing.T) {
	facts := map[string]string{
		model.FactDisks: "/dev/disk/by-id/aaa|500000000000|false|SSD-A;/dev/disk/by-id/bbb|2000000000000|true|HDD-B",
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

	tk := model.Task{
		ID: "task-1",
		ResolvedParams: map[string]string{
			model.FactHostname: "web-01",
			model.FactBizIP:    "10.0.1.5",
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
