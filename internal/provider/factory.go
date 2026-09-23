// 本文件实现按配置构造七个 provider 的工厂入口，并暴露事件总线与同步
// 决策编排器供装配点（cmd/lober，M9）取用。对应实现计划任务 7、8，依据
// SPEC §7.2、§7.6。
//
// 契约缺口（汇总，具体分析见各自实现文件）：
//  1. config.Provider.IPAM / DNS / Resolver 目前只有单一取值
//     "static"/"noop"/"rule-based"，本工厂对未知取值返回错误（不回落到
//     默认实现），与「未知的选择项返回错误」这条设计要点一致。
//  2. config.Provider 没有 resolver 规则表文件路径字段（见 resolver.go）、
//     没有 tcp 心跳探测端口字段（见 heartbeat.go）——两处都用工厂内的
//     固定值顶上，等 M1 补齐字段后只需改本文件里对应的构造调用。
//  3. Inventory / Notify 没有对应的配置选择项（SPEC 与计划都只描述了
//     IPAM/DNS/Resolver/DiskPolicy/Heartbeat 五个可选项）——按计划任务 5
//     的描述，这两者在 MVP 阶段只有内置实现、不通过配置选择，工厂对它们
//     无条件构造内置版本，不校验任何配置字段。
package provider

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/provider/loader"
)

// defaultTCPHeartbeatPort 是 tcp 心跳探测器在 config 未提供端口字段时使用
// 的固定端口。选 22（SSH）的理由：这是几乎所有目标 OS 家族默认会监听的
// 端口，作为「机器活着」的信号意义明确；相比之下选一个业务端口需要先知道
// 部署的业务是什么，MVP 阶段没有这个信息来源。
const defaultTCPHeartbeatPort = 22

// Providers 是工厂构造出的七个 provider 实现的集合，供装配点
// （cmd/lober）逐个取用装配进状态机、生命周期订阅等模块。
//
// 用结构体而非七个返回值：七个具名字段比七个位置参数更不容易在装配点
// 传错顺序，且将来某个 provider 增加第二种依赖（如 Inventory 需要
// HostStore）时，调用方按字段名取值不受影响。
type Providers struct {
	IPAM             contract.IPAMProviderV1
	DNS              contract.DNSProviderV1
	Inventory        contract.InventoryProviderV1
	Notify           contract.NotifyProviderV1
	Heartbeat        contract.HeartbeatProviderV1
	TemplateResolver contract.TemplateResolver
	DiskPlanner      contract.DiskPlanner
}

// NewProviders 按配置构造七个 provider 的内置实现。
//
// hosts 是 Inventory 实现依赖的主机查询窄接口（见 inventory.go 的
// hostLister）——工厂不直接依赖 contract.Store 的完整组合接口，只要求
// 调用方（装配点）传入满足这个窄接口的对象，通常就是真实的 Store 实现
// 本身（它天然满足）。
//
// 未知的选择项返回错误而不是回落到默认（任务 7 明确要求）：配置里写错
// 一个 provider 名字应该在启动时被发现。
func NewProviders(cfg config.Provider, hosts hostLister) (Providers, error) {
	ipam, err := newIPAMFromConfig(cfg.IPAM, cfg.IPAMLoader)
	if err != nil {
		return Providers{}, err
	}

	dns, err := newDNSFromConfig(cfg.DNS, cfg.DNSLoader)
	if err != nil {
		return Providers{}, err
	}

	resolver, err := newResolverFromConfig(cfg.Resolver)
	if err != nil {
		return Providers{}, err
	}

	diskPlanner, err := NewDiskPlanner(string(cfg.DiskPolicy))
	if err != nil {
		return Providers{}, err
	}

	heartbeat, err := newHeartbeatFromConfig(cfg.HeartbeatMode, cfg.HeartbeatLoader)
	if err != nil {
		return Providers{}, err
	}

	return Providers{
		IPAM:             ipam,
		DNS:              dns,
		Inventory:        NewBuiltinInventory(hosts),
		Notify:           NewBuiltinNotify(),
		Heartbeat:        heartbeat,
		TemplateResolver: resolver,
		DiskPlanner:      diskPlanner,
	}, nil
}

// newIPAMFromConfig 按配置的 IPAM 选择项构造实现。
//
// static 实现在建任务时才知道表单数据（见 ipam.go 的 staticIPAM.form
// 字段），工厂阶段还没有表单——这里先构造一个空表单的占位实例占住
// Providers.IPAM 字段的位置，真正参与决策时由
// taskParamsResolver.Resolve（resolve.go）为每次请求单独构造一个带当次
// 表单数据的实例，不复用工厂阶段这个占位实例。工厂返回的这个实例仅用于
// Ping 探活与满足 Providers 结构体字段非 nil 这两个用途。
//
// exec/http 分支（本次任务，SPEC §7.2）不受上面这条限制——
// loader.IPAMAdapter.Allocate 直接把 contract.AllocRequest（含
// Hostname/Role）编码发给外部程序/服务，不需要 static 实现那种「工厂
// 阶段先占位、调用阶段再重建」的两步走，本工厂构造出来的这一个实例
// 从一开始就是最终会被调用的那个实例。
//
// 契约缺口：taskParamsResolver.ipamFactory（resolve.go）目前硬编码为
// `func(form) contract.IPAMProviderV1 { return newStaticIPAM(form) }`，
// 不读取 Providers.IPAM 字段，也不知道工厂这里可能构造出了一个 exec/http
// 加载的 IPAM 实现——这意味着即使运维把 provider.ipam 配成 "exec" 且
// 本工厂据此成功构造出 Providers.IPAM，装机建任务时真正被调用的仍然是
// resolve.go 里硬编码的 static 实现，Providers.IPAM 字段实际上只被
// Ping 探活等辅助路径使用。这不是我能在本文件内部悄悄绕过的缺口——
// resolve.go 不在本任务的文件归属范围内（铁律第 1 条：分派处/既有
// 编排逻辑归其它轨或保持原样），正解是把 taskParamsResolver 的构造
// 参数从「无参数的 ipamFactory 闭包」改成「接受 Providers.IPAM 或一个
// 按表单数据重新注入 form 的窄接口」，这需要改 resolve.go 与它的构造
// 调用点（装配点 cmd/lober），已写入本次任务的 wiringNeeded。
func newIPAMFromConfig(kind string, loaderCfg config.LoaderConfig) (contract.IPAMProviderV1, error) {
	switch kind {
	case "static":
		return newStaticIPAM(nil), nil
	case "exec", "http":
		caller, err := newLoaderCaller(kind, loaderCfg)
		if err != nil {
			return nil, fmt.Errorf("provider: 构造 IPAM 加载器失败: %w", err)
		}
		return loader.NewIPAMAdapter(caller), nil
	default:
		return nil, fmt.Errorf("provider: 未知的 IPAM 选择项 %q，MVP 内置 static，"+
			"另支持 exec/http 两种加载器（SPEC §7.2）", kind)
	}
}

// newDNSFromConfig 按配置的 DNS 选择项构造实现。
func newDNSFromConfig(kind string, loaderCfg config.LoaderConfig) (contract.DNSProviderV1, error) {
	switch kind {
	case "noop":
		return newNoopDNS(), nil
	case "exec", "http":
		caller, err := newLoaderCaller(kind, loaderCfg)
		if err != nil {
			return nil, fmt.Errorf("provider: 构造 DNS 加载器失败: %w", err)
		}
		return loader.NewDNSAdapter(caller), nil
	default:
		return nil, fmt.Errorf("provider: 未知的 DNS 选择项 %q，MVP 内置 noop，"+
			"另支持 exec/http 两种加载器（SPEC §7.2）", kind)
	}
}

// newLoaderCaller 按 kind（"exec" 或 "http"）与 loaderCfg 构造一个通用的
// loader.Caller，供 IPAM/DNS/Heartbeat 三个扩展点的 exec/http 分支共用
// ——三者需要的构造逻辑完全相同（只是把 config.LoaderConfig 的字段搬进
// loader.ExecConfig/HTTPConfig/Config），抽成一个函数避免三处 case 分支
// 各自重复同样的字段搬运代码。
//
// kind 必须是 "exec" 或 "http"（调用方在进入这个分支前已经用 switch
// 保证了这一点），其它取值会走到 default 分支返回错误——这是防御性的
// "不应该发生"分支，正常路径不会触达。
func newLoaderCaller(kind string, loaderCfg config.LoaderConfig) (loader.Caller, error) {
	sharedCfg := loader.Config{
		Timeout:          loaderCfg.Timeout,
		MaxRetries:       loaderCfg.MaxRetries,
		RetryBackoff:     loaderCfg.RetryBackoff,
		BreakerThreshold: loaderCfg.BreakerThreshold,
	}

	switch kind {
	case "exec":
		return loader.NewExecLoader(loader.ExecConfig{Command: loaderCfg.Command, Args: loaderCfg.Args}, sharedCfg)
	case "http":
		return loader.NewHTTPLoader(loader.HTTPConfig{URL: loaderCfg.URL}, sharedCfg)
	default:
		return nil, fmt.Errorf("provider: newLoaderCaller 收到不支持的 kind %q（只接受 exec/http，"+
			"调用方应该已经用 switch 排除了其它取值——走到这里说明调用处有 bug）", kind)
	}
}

// newResolverFromConfig 按配置的 Resolver 选择项构造实现。
//
// rule-based 实现目前总是用空规则表构造（等价于全匹配到默认模板的兜底
// 规则）——config.Provider 没有规则表文件路径字段（见 resolver.go 顶部
// 契约缺口说明），工厂无法从配置读到真实规则，因此不能在这里编造一份
// 看起来更完整但实际来源不明的规则表。
func newResolverFromConfig(kind string) (contract.TemplateResolver, error) {
	switch kind {
	case "rule-based":
		return NewTemplateResolver(nil), nil
	default:
		return nil, fmt.Errorf("provider: 未知的 Resolver 选择项 %q，MVP 只内置 rule-based 一种实现", kind)
	}
}

// newHeartbeatFromConfig 按配置的心跳模式构造实现。
//
// off 模式返回一个总是空结果的实现，不是 nil——调用方（周期轮询循环）
// 不需要额外判空分支，Providers.Heartbeat 字段在任何合法配置下都是可用
// 的非 nil 值，这是「构造期决定行为，调用期不需要再做特殊判断」的一贯
// 原则（与 by-hook 磁盘策略、noop DNS 是同一种设计思路）。
//
// exec/http 分支（本次任务，SPEC §7.2）：loader.HeartbeatAdapter.Probe
// 已经保证「整批调用失败时退化为逐目标失败，不改变结果切片长度」（见
// adapters.go 该方法的文档），与 tcp/icmp 实现「单目标失败不影响其余」
// 的既有约定在可观察行为上一致——调用方不需要区分心跳背后是内置探测器
// 还是加载器。
func newHeartbeatFromConfig(mode string, loaderCfg config.LoaderConfig) (contract.HeartbeatProviderV1, error) {
	switch mode {
	case "tcp":
		return newTCPHeartbeat(defaultTCPHeartbeatPort), nil
	case "icmp":
		return newICMPHeartbeat(), nil
	case "off":
		return offHeartbeat{}, nil
	case "exec", "http":
		caller, err := newLoaderCaller(mode, loaderCfg)
		if err != nil {
			return nil, fmt.Errorf("provider: 构造心跳加载器失败: %w", err)
		}
		return loader.NewHeartbeatAdapter(caller), nil
	default:
		return nil, fmt.Errorf("provider: 未知的心跳模式 %q，合法取值为 tcp/icmp/off/exec/http 之一"+
			"（SPEC §6.6、§7.2）", mode)
	}
}

// offHeartbeat 是心跳模式为 off 时的实现：Probe 总是返回空结果（不探测
// 任何目标），Ping 总是成功（它没有任何可能失败的外部依赖）。
type offHeartbeat struct{}

func (offHeartbeat) Probe(ctx context.Context, targets []contract.ProbeTarget) []contract.ProbeResult {
	return nil
}

func (offHeartbeat) Ping(ctx context.Context) error {
	return nil
}

var _ contract.HeartbeatProviderV1 = offHeartbeat{}
