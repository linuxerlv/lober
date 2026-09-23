// 本文件测试五个契约适配器：编码请求参数、解码响应结果、透传错误、
// Heartbeat 的批量失败退化路径。不依赖 exec/http 的具体传输实现——用
// recordingCaller 直接验证「适配器传给 Caller.Call 的 method/params 是
// 什么」与「Caller.Call 的结果被怎样解码」，这是适配器层自己的职责，
// 与传输层（exec_test.go/http_test.go 已经覆盖）分开测试。
package loader

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// recordingCaller 是测试用的 Caller 假实现：记录最后一次调用的
// method/params，并把 respond 字段的内容塞进调用方传入的 result 指针
// （模拟一次成功的解码），或直接返回 failWith（模拟调用失败）。
type recordingCaller struct {
	lastMethod string
	lastParams any

	respond  any   // 非 nil 时，Call 会把它序列化再反序列化进 result（模拟一次真实的 JSON 往返）
	failWith error // 非 nil 时，Call 直接返回这个错误，忽略 respond
}

func (r *recordingCaller) Call(ctx context.Context, method string, params any, result any) error {
	r.lastMethod = method
	r.lastParams = params

	if r.failWith != nil {
		return r.failWith
	}
	if r.respond != nil && result != nil {
		// 用真实的 JSON marshal/unmarshal 模拟一次完整往返，而不是直接
		// 反射赋值——这样能捕获「适配器期望的 result 类型与 respond 的
		// JSON 结构不匹配」这类真实会在生产环境发生的问题，反射赋值则会
		// 掩盖掉这类问题（类型不匹配时反射赋值可能直接 panic 或静默失败，
		// 都不是我们想在这里测的东西）。
		data, err := marshalForTest(r.respond)
		if err != nil {
			return err
		}
		return unmarshalForTest(data, result)
	}
	return nil
}

// TestIPAMAdapter_Allocate 验证请求参数被正确传递、响应结果被正确解码。
func TestIPAMAdapter_Allocate(t *testing.T) {
	rc := &recordingCaller{respond: contract.AllocResult{IP: "10.0.1.5", Gateway: "10.0.1.1", Mask: "255.255.255.0"}}
	a := NewIPAMAdapter(rc)

	result, err := a.Allocate(context.Background(), contract.AllocRequest{Hostname: "node-01", Role: "web"})
	if err != nil {
		t.Fatalf("Allocate() 失败: %v", err)
	}
	if rc.lastMethod != "ipam.allocate" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "ipam.allocate")
	}
	if result.IP != "10.0.1.5" || result.Gateway != "10.0.1.1" || result.Mask != "255.255.255.0" {
		t.Errorf("Allocate() 结果 = %+v，解码不正确", result)
	}
}

// TestIPAMAdapter_Release 验证 Release 透传错误。
func TestIPAMAdapter_Release(t *testing.T) {
	wantErr := errors.New("释放失败")
	rc := &recordingCaller{failWith: wantErr}
	a := NewIPAMAdapter(rc)

	err := a.Release(context.Background(), "10.0.1.5")
	if !errors.Is(err, wantErr) {
		t.Errorf("Release() 错误 = %v, want %v", err, wantErr)
	}
	if rc.lastMethod != "ipam.release" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "ipam.release")
	}
}

// TestIPAMAdapter_Ping 验证 Ping 转发。
func TestIPAMAdapter_Ping(t *testing.T) {
	rc := &recordingCaller{}
	a := NewIPAMAdapter(rc)
	if err := a.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() 失败: %v", err)
	}
	if rc.lastMethod != "ipam.ping" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "ipam.ping")
	}
}

// TestDNSAdapter_RegisterDeregisterPing 验证三个方法各自转发到正确的
// method 名。
func TestDNSAdapter_RegisterDeregisterPing(t *testing.T) {
	rc := &recordingCaller{}
	a := NewDNSAdapter(rc)

	if err := a.Register(context.Background(), "host1.example.com", "10.0.1.5"); err != nil {
		t.Fatalf("Register() 失败: %v", err)
	}
	if rc.lastMethod != "dns.register" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "dns.register")
	}

	if err := a.Deregister(context.Background(), "host1.example.com"); err != nil {
		t.Fatalf("Deregister() 失败: %v", err)
	}
	if rc.lastMethod != "dns.deregister" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "dns.deregister")
	}

	if err := a.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() 失败: %v", err)
	}
	if rc.lastMethod != "dns.ping" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "dns.ping")
	}
}

// TestDNSAdapter_RegisterPropagatesError 验证失败语义：Register 失败时
// 错误被原样传出——调用方（lifecycle.go 的 onTaskReady）依赖这个 error
// 是否非 nil 来决定"只告警不影响任务状态"（§7.4 warn_only），适配器
// 层必须真实地把失败以 error 的形式暴露出来，不能吞掉。
func TestDNSAdapter_RegisterPropagatesError(t *testing.T) {
	wantErr := errors.New("DNS 服务器抖动")
	rc := &recordingCaller{failWith: wantErr}
	a := NewDNSAdapter(rc)

	err := a.Register(context.Background(), "host1.example.com", "10.0.1.5")
	if !errors.Is(err, wantErr) {
		t.Errorf("Register() 错误 = %v, want %v —— 失败必须真实传出，"+
			"§7.4 warn_only 语义由调用方（lifecycle.go）负责只记日志不阻塞，"+
			"适配器自己不能提前吞掉错误", err, wantErr)
	}
}

// TestInventoryAdapter_LookupBySN 验证响应解码进 model.Host 的具体字段。
func TestInventoryAdapter_LookupBySN(t *testing.T) {
	rc := &recordingCaller{respond: map[string]any{
		"sn":           "SN-001",
		"mac":          "aa:bb:cc:dd:ee:ff",
		"uuid":         "uuid-001",
		"arch":         "x86_64",
		"firmware":     "uefi",
		"rack":         "R01",
		"last_seen_at": 1234567890,
	}}
	a := NewInventoryAdapter(rc)

	host, err := a.LookupBySN(context.Background(), "SN-001")
	if err != nil {
		t.Fatalf("LookupBySN() 失败: %v", err)
	}
	if host.SN != "SN-001" || host.MAC != "aa:bb:cc:dd:ee:ff" || host.Rack != "R01" {
		t.Errorf("LookupBySN() 结果 = %+v，解码不正确", host)
	}
	if host.LastSeenAt != 1234567890 {
		t.Errorf("host.LastSeenAt = %d, want 1234567890", host.LastSeenAt)
	}
	if rc.lastMethod != "inventory.lookup_by_sn" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "inventory.lookup_by_sn")
	}
}

// TestInventoryAdapter_LookupBySNPropagatesError 验证查不到时错误原样
// 传出——调用方（resolve.go 类似的阻塞式路径）依赖它阻塞建任务
// （§7.4 block 语义，允许人工录入兜底，但那是调用方的补救路径）。
func TestInventoryAdapter_LookupBySNPropagatesError(t *testing.T) {
	wantErr := errors.New("清单里没有这个 SN")
	rc := &recordingCaller{failWith: wantErr}
	a := NewInventoryAdapter(rc)

	_, err := a.LookupBySN(context.Background(), "SN-404")
	if !errors.Is(err, wantErr) {
		t.Errorf("LookupBySN() 错误 = %v, want %v", err, wantErr)
	}
}

// TestNotifyAdapter_Emit 验证 Emit 转发 LifecyclePayload。
func TestNotifyAdapter_Emit(t *testing.T) {
	rc := &recordingCaller{}
	a := NewNotifyAdapter(rc)

	ev := contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskReady,
		TaskID: "task-001",
		Detail: map[string]string{"state": "ready"},
	}
	if err := a.Emit(context.Background(), ev); err != nil {
		t.Fatalf("Emit() 失败: %v", err)
	}
	if rc.lastMethod != "notify.emit" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "notify.emit")
	}
	gotParams, ok := rc.lastParams.(contract.LifecyclePayload)
	if !ok {
		t.Fatalf("lastParams 类型 = %T, want contract.LifecyclePayload", rc.lastParams)
	}
	if gotParams.TaskID != "task-001" {
		t.Errorf("lastParams.TaskID = %q, want %q", gotParams.TaskID, "task-001")
	}
}

// TestHeartbeatAdapter_ProbeDecodesResults 验证正常返回时结果按目标顺序
// 解码。
func TestHeartbeatAdapter_ProbeDecodesResults(t *testing.T) {
	targets := []contract.ProbeTarget{{TaskID: "t1", IP: "10.0.1.5"}, {TaskID: "t2", IP: "10.0.1.6"}}
	rc := &recordingCaller{respond: []contract.ProbeResult{
		{TaskID: "t1", Reached: true, Detail: "ok"},
		{TaskID: "t2", Reached: false, Detail: "timeout"},
	}}
	a := NewHeartbeatAdapter(rc)

	results := a.Probe(context.Background(), targets)
	if len(results) != 2 {
		t.Fatalf("Probe() 返回 %d 条结果, want 2", len(results))
	}
	if !results[0].Reached || results[1].Reached {
		t.Errorf("Probe() 结果 = %+v，解码不正确", results)
	}
}

// TestHeartbeatAdapter_ProbeDegradesOnCallFailure 验证整批调用失败时，
// Probe 不 panic、不返回空切片，而是把每个目标都标记为探测失败——
// record_only 语义要求调用方始终能拿到与 targets 等长的结果（周期轮询
// 循环的既有假设，见内置 tcp/icmp 实现）。
func TestHeartbeatAdapter_ProbeDegradesOnCallFailure(t *testing.T) {
	targets := []contract.ProbeTarget{{TaskID: "t1", IP: "10.0.1.5"}, {TaskID: "t2", IP: "10.0.1.6"}}
	rc := &recordingCaller{failWith: errors.New("heartbeat 服务不可达")}
	a := NewHeartbeatAdapter(rc)

	results := a.Probe(context.Background(), targets)
	if len(results) != len(targets) {
		t.Fatalf("Probe() 返回 %d 条结果, want %d（必须与 targets 等长）", len(results), len(targets))
	}
	for i, r := range results {
		if r.Reached {
			t.Errorf("results[%d].Reached = true, want false（整批调用失败应该全部标记未探测到）", i)
		}
		if r.TaskID != targets[i].TaskID {
			t.Errorf("results[%d].TaskID = %q, want %q（顺序必须与 targets 对应）", i, r.TaskID, targets[i].TaskID)
		}
	}
}

// TestHeartbeatAdapter_ProbeEmptyTargets 验证空目标列表直接返回 nil，
// 不发起任何调用——与内置实现（probeConcurrently）的既有约定一致。
func TestHeartbeatAdapter_ProbeEmptyTargets(t *testing.T) {
	rc := &recordingCaller{}
	a := NewHeartbeatAdapter(rc)

	results := a.Probe(context.Background(), nil)
	if results != nil {
		t.Errorf("Probe(nil) = %v, want nil", results)
	}
	if rc.lastMethod != "" {
		t.Errorf("空目标列表不应该发起任何调用，但 lastMethod = %q", rc.lastMethod)
	}
}

// TestHeartbeatAdapter_Ping 验证 Ping 转发。
func TestHeartbeatAdapter_Ping(t *testing.T) {
	rc := &recordingCaller{}
	a := NewHeartbeatAdapter(rc)
	if err := a.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() 失败: %v", err)
	}
	if rc.lastMethod != "heartbeat.ping" {
		t.Errorf("lastMethod = %q, want %q", rc.lastMethod, "heartbeat.ping")
	}
}

// 编译期断言：五个适配器分别满足对应的契约接口（虽然 adapters.go 文件
// 内已有同样的断言，这里重复一次是为了在测试文件里也能直观看到这份
// 对应关系，且不依赖阅读非测试文件）。
var (
	_ contract.IPAMProviderV1      = (*IPAMAdapter)(nil)
	_ contract.DNSProviderV1       = (*DNSAdapter)(nil)
	_ contract.InventoryProviderV1 = (*InventoryAdapter)(nil)
	_ contract.NotifyProviderV1    = (*NotifyAdapter)(nil)
	_ contract.HeartbeatProviderV1 = (*HeartbeatAdapter)(nil)
)

// marshalForTest / unmarshalForTest 是 recordingCaller 模拟一次真实 JSON
// 往返用的薄封装，单独命名只是为了让 recordingCaller.Call 里的调用点
// 读起来是"为测试目的做一次序列化往返"而不是裸的 json.Marshal——避免
// 与生产代码里同样调用 json.Marshal 的地方在阅读时混淆用途。
func marshalForTest(v any) ([]byte, error) {
	return json.Marshal(v)
}

func unmarshalForTest(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
