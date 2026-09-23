// 本文件把通用的 Caller（exec 或 http 加载器）适配成五个契约接口的具体
// 实现：IPAMProviderV1、DNSProviderV1、InventoryProviderV1、
// NotifyProviderV1、HeartbeatProviderV1。
//
// 只适配这五个，不适配 TemplateResolver/DiskPlanner：docs/phase2-loader-
// 需求依据.md 第 4 节与 SPEC §7.6 已经明确回答「纯决策的两个需要外部化
// 吗？不需要」，且这两个契约接口刻意不带 Ping 方法（provider.go 的接口
// 文档：纯函数不需要健康检查，加了反而暗示"它可能因为外部依赖不可用而
// 失败"）——把它们也套上 exec/http 加载器与这条设计意图直接矛盾，因此
// 本文件不提供这两个的适配器。
//
// 每个适配器都是「把 Go 方法调用编码成 Caller.Call 的一次通用调用，再把
// 结果解码回具体返回类型」的薄封装，不包含任何 exec/http 特有的逻辑
// （那些都在 exec.go/http.go 里）——这正是 Caller 接口存在的意义：适配器
// 代码对「背后是子进程还是 HTTP」完全无感知（§7.2：加载器对调用方透明）。
//
// method 命名约定：`<小写 provider 名>.<小写方法名>`，例如 "ipam.allocate"、
// "dns.register"——外部程序/服务的作者据此实现自己的分发逻辑（收到
// Request.Method 后决定做什么）。
package loader

import (
	"context"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// IPAM
// ---------------------------------------------------------------------------

// IPAMAdapter 把 Caller 适配成 contract.IPAMProviderV1。
type IPAMAdapter struct {
	caller Caller
}

// NewIPAMAdapter 构造一个 IPAMAdapter。
func NewIPAMAdapter(caller Caller) *IPAMAdapter {
	return &IPAMAdapter{caller: caller}
}

func (a *IPAMAdapter) Allocate(ctx context.Context, req contract.AllocRequest) (contract.AllocResult, error) {
	var result contract.AllocResult
	err := a.caller.Call(ctx, "ipam.allocate", req, &result)
	return result, err
}

func (a *IPAMAdapter) Release(ctx context.Context, ip string) error {
	return a.caller.Call(ctx, "ipam.release", struct {
		IP string `json:"ip"`
	}{IP: ip}, nil)
}

func (a *IPAMAdapter) Ping(ctx context.Context) error {
	return a.caller.Call(ctx, "ipam.ping", struct{}{}, nil)
}

var _ contract.IPAMProviderV1 = (*IPAMAdapter)(nil)

// ---------------------------------------------------------------------------
// DNS
// ---------------------------------------------------------------------------

// DNSAdapter 把 Caller 适配成 contract.DNSProviderV1。
type DNSAdapter struct {
	caller Caller
}

// NewDNSAdapter 构造一个 DNSAdapter。
func NewDNSAdapter(caller Caller) *DNSAdapter {
	return &DNSAdapter{caller: caller}
}

func (a *DNSAdapter) Register(ctx context.Context, fqdn, ip string) error {
	return a.caller.Call(ctx, "dns.register", struct {
		FQDN string `json:"fqdn"`
		IP   string `json:"ip"`
	}{FQDN: fqdn, IP: ip}, nil)
}

func (a *DNSAdapter) Deregister(ctx context.Context, fqdn string) error {
	return a.caller.Call(ctx, "dns.deregister", struct {
		FQDN string `json:"fqdn"`
	}{FQDN: fqdn}, nil)
}

func (a *DNSAdapter) Ping(ctx context.Context) error {
	return a.caller.Call(ctx, "dns.ping", struct{}{}, nil)
}

var _ contract.DNSProviderV1 = (*DNSAdapter)(nil)

// ---------------------------------------------------------------------------
// Inventory
// ---------------------------------------------------------------------------

// InventoryAdapter 把 Caller 适配成 contract.InventoryProviderV1。
type InventoryAdapter struct {
	caller Caller
}

// NewInventoryAdapter 构造一个 InventoryAdapter。
func NewInventoryAdapter(caller Caller) *InventoryAdapter {
	return &InventoryAdapter{caller: caller}
}

// inventoryLookupResult 是 LookupBySN 对应外部程序应答的 result 字段
// 形状，字段与 model.Host（contract.InventoryProviderV1.LookupBySN 的真实
// 返回类型，见 internal/core/model/inventory.go）逐一对应，而不是本文件
// 自行发明的精简形状——model 包冻结，字段名与数量以它为准，本类型只是
// 给这些字段加上 JSON 标签，供外部程序按这份字段名产出应答。
type inventoryLookupResult struct {
	SN         string             `json:"sn"`
	MAC        string             `json:"mac"`
	UUID       string             `json:"uuid"`
	Arch       model.Arch         `json:"arch"`
	Firmware   model.FirmwareMode `json:"firmware"`
	Rack       string             `json:"rack"`
	LastSeenAt int64              `json:"last_seen_at"`
}

func (a *InventoryAdapter) LookupBySN(ctx context.Context, sn string) (model.Host, error) {
	var result inventoryLookupResult
	err := a.caller.Call(ctx, "inventory.lookup_by_sn", struct {
		SN string `json:"sn"`
	}{SN: sn}, &result)
	if err != nil {
		return model.Host{}, err
	}
	return model.Host{
		SN:         result.SN,
		MAC:        result.MAC,
		UUID:       result.UUID,
		Arch:       result.Arch,
		Firmware:   result.Firmware,
		Rack:       result.Rack,
		LastSeenAt: result.LastSeenAt,
	}, nil
}

func (a *InventoryAdapter) Ping(ctx context.Context) error {
	return a.caller.Call(ctx, "inventory.ping", struct{}{}, nil)
}

var _ contract.InventoryProviderV1 = (*InventoryAdapter)(nil)

// ---------------------------------------------------------------------------
// Notify
// ---------------------------------------------------------------------------

// NotifyAdapter 把 Caller 适配成 contract.NotifyProviderV1。
type NotifyAdapter struct {
	caller Caller
}

// NewNotifyAdapter 构造一个 NotifyAdapter。
func NewNotifyAdapter(caller Caller) *NotifyAdapter {
	return &NotifyAdapter{caller: caller}
}

func (a *NotifyAdapter) Emit(ctx context.Context, ev contract.LifecyclePayload) error {
	return a.caller.Call(ctx, "notify.emit", ev, nil)
}

func (a *NotifyAdapter) Ping(ctx context.Context) error {
	return a.caller.Call(ctx, "notify.ping", struct{}{}, nil)
}

var _ contract.NotifyProviderV1 = (*NotifyAdapter)(nil)

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

// HeartbeatAdapter 把 Caller 适配成 contract.HeartbeatProviderV1。
//
// Probe 与其余四个适配器的方法不同：契约签名是「批量输入、批量输出，且
// 不返回 error」（HeartbeatProviderV1.Probe(ctx, targets) []ProbeResult，
// 契约文档：Heartbeat 是辅助通道，失败语义是 record_only，不阻塞任何
// 流程）。因此 Probe 内部调用 Caller.Call 失败时，不能像其它适配器那样
// 直接把 error 返回给调用方（方法签名不允许），而是把「整批调用失败」
// 转成「每个目标各自的 ProbeResult.Reached=false」——这与内置 tcp/icmp
// 实现「单个目标探测失败不影响其余目标的结果切片长度」的既有约定保持
// 一致：调用方（周期轮询循环）始终能拿到与 targets 等长的结果切片，
// 不需要额外判断「这次 Probe 是不是整体失败了」。
type HeartbeatAdapter struct {
	caller Caller
}

// NewHeartbeatAdapter 构造一个 HeartbeatAdapter。
func NewHeartbeatAdapter(caller Caller) *HeartbeatAdapter {
	return &HeartbeatAdapter{caller: caller}
}

func (a *HeartbeatAdapter) Probe(ctx context.Context, targets []contract.ProbeTarget) []contract.ProbeResult {
	if len(targets) == 0 {
		return nil
	}

	// result 直接是 []contract.ProbeResult 的 JSON 数组（不包一层
	// {"results": [...]}对象）——Caller.Call 的 result 参数只是「传给
	// json.Unmarshal 的目标地址」，传 *[]contract.ProbeResult 就要求外部
	// 程序的 Response.Result 字段本身是一个 JSON 数组，这是最直接、外部
	// 实现者最容易理解的形状，不需要多一层包装对象。
	var results []contract.ProbeResult
	err := a.caller.Call(ctx, "heartbeat.probe", struct {
		Targets []contract.ProbeTarget `json:"targets"`
	}{Targets: targets}, &results)

	if err != nil {
		// 整批调用失败：按 record_only 语义，把失败转成「每个目标都探测
		// 失败」，而不是让 Probe panic 或返回长度不一致的切片。
		degraded := make([]contract.ProbeResult, len(targets))
		for i, t := range targets {
			degraded[i] = contract.ProbeResult{
				TaskID:  t.TaskID,
				Reached: false,
				Detail:  "heartbeat 加载器调用失败: " + err.Error(),
			}
		}
		return degraded
	}

	return results
}

func (a *HeartbeatAdapter) Ping(ctx context.Context) error {
	return a.caller.Call(ctx, "heartbeat.ping", struct{}{}, nil)
}

var _ contract.HeartbeatProviderV1 = (*HeartbeatAdapter)(nil)
