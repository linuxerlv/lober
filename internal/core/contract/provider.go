// 本文件定义 Provider 契约：七个扩展点接口与按扩展点分别定义的失败语义表。
// 对应实现计划任务 12，依据 SPEC §7.1、§7.2、§7.3、§7.4、§7.5、§7.6、
// §10 失败模式 5。
//
// SPEC §7.6 明确 MVP 不做加载器，只定接口加内置实现——接口边界是真正有
// 价值的部分，所以它属于契约层；具体实现（static/noop/rule-based/具名
// 磁盘策略）属于 M7。
package contract

import (
	"context"

	"github.com/lober/lober/internal/core/model"
)

// AllocRequest 是向 IPAMProviderV1 申请 IP 的请求参数。
type AllocRequest struct {
	// Hostname 是本次申请关联的主机名，供 IPAM 侧登记用途。
	Hostname string
	// Role 是业务角色标签（表单填入），部分 IPAM 实现按角色分池分配。
	Role string
}

// AllocResult 是 IPAMProviderV1.Allocate 的结果（写回 task 的
// resolved_params 后固化，§2.6）。
type AllocResult struct {
	IP      string
	Gateway string
	Mask    string
}

// IPAMProviderV1 申请与释放业务 IP（§7.1、§5.7：业务 IP 写配置但不激活）。
//
// 带探活方法：这是热更新的健康检查钩子（§7.3）——新插件拉起后先探活，
// 失败则保留旧进程、拒绝本次 reload。MVP 的内置实现直接返回成功。
type IPAMProviderV1 interface {
	Allocate(ctx context.Context, req AllocRequest) (AllocResult, error)
	Release(ctx context.Context, ip string) error
	Ping(ctx context.Context) error
}

// DNSProviderV1 注册与注销主机名到 IP 的映射（§7.1）。
type DNSProviderV1 interface {
	Register(ctx context.Context, fqdn, ip string) error
	Deregister(ctx context.Context, fqdn string) error
	Ping(ctx context.Context) error
}

// InventoryProviderV1 按 SN 查询外部清单系统（§7.1），供建任务时校验或
// 建任务/清单同步时兜底数据来源。
type InventoryProviderV1 interface {
	LookupBySN(ctx context.Context, sn string) (model.Host, error)
	Ping(ctx context.Context) error
}

// LifecyclePayload 是 NotifyProviderV1.Emit 的通知内容。刻意不直接复用
// model.Task——通知的载荷可能只需要任务的一个子集，且未来可能携带非
// Task 的信息（如批量统计），独立类型避免这个接口的形状被 Task 的字段
// 变动牵连。
type LifecyclePayload struct {
	Event  LifecycleEvent
	TaskID string
	Detail map[string]string
}

// NotifyProviderV1 推送生命周期事件到外部系统（如告警、工单，§7.1、§7.5）。
type NotifyProviderV1 interface {
	Emit(ctx context.Context, ev LifecyclePayload) error
	Ping(ctx context.Context) error
}

// ProbeTarget 是 HeartbeatProviderV1.Probe 的探测目标。
type ProbeTarget struct {
	TaskID string
	IP     string
}

// ProbeResult 是单个探测目标的结果。
type ProbeResult struct {
	TaskID  string
	Reached bool
	Detail  string
}

// HeartbeatProviderV1 批量探测已就绪主机的存活状态（§7.1、§6.6：辅助通道，
// 非主通道）。
type HeartbeatProviderV1 interface {
	Probe(ctx context.Context, targets []ProbeTarget) []ProbeResult
	Ping(ctx context.Context) error
}

// HWFacts 是 TemplateResolverV1.Resolve 用于决策的硬件事实输入，来自
// discovery 或 pre 阶段采集的 fact（§5.6：先采集，再服务端决策）。
type HWFacts struct {
	OSFamily string
	Vendor   string
	Product  string
	Arch     string
}

// TemplateRef 是 TemplateResolverV1.Resolve 的决策结果：选中的模板与版本。
type TemplateRef struct {
	TemplateID      string
	TemplateVersion int
}

// TemplateResolverV1 按硬件事实与表单选模板（§7.1、§7.6：MVP 形态是规则
// 表，按 os + version + vendor + product 匹配，自上而下第一条命中生效）。
//
// 纯决策，不带探活方法也不做 IO（§7.2、§7.6 设计要点）：这套系统会擦盘，
// 决策函数若行为不完全确定则故障无法复现，因此本接口刻意不提供 Ping——
// 纯函数不需要健康检查，加了反而暗示「它可能因为外部依赖不可用而失败」，
// 这与「纯决策」的定位矛盾。
type TemplateResolver interface {
	Resolve(ctx context.Context, form map[string]string, facts HWFacts) (TemplateRef, error)
}

// DiskIntent 是 DiskPlannerV1.Plan 的意图输入：模板声明的磁盘策略
// （model.DiskPolicy），加上该策略可能需要的补充参数（如 by-model 策略
// 需要的型号字符串，来自 hook 参数或表单）。
type DiskIntent struct {
	Policy string // 取值对应 model.DiskPolicy 的字符串形式
	Params map[string]string
}

// Disk 是磁盘规划的输入设备清单。刻意只用 by-id/by-path/WWN 一类的持久化
// 标识，不用内核分配的名字（如 /dev/sda）——内核命名在同一台机器的不同
// 次启动之间可能变化，磁盘规划结果若引用了会漂移的名字，写进应答文件后
// 现场执行时可能已经对不上（§10 失败模式 5）。
type Disk struct {
	// ByIDPath 是形如 /dev/disk/by-id/... 的持久化设备路径。
	ByIDPath   string
	SizeBytes  int64
	Rotational bool
	Model      string
}

// LayoutDisk 是 Layout 中选中的一块盘及选择理由。
type LayoutDisk struct {
	Disk Disk
	// Reason 是「为什么选这块盘」的说明，dry-run 端点要展示它（§8.2、
	// 计划任务 12 设计要点）——磁盘规划会擦盘，决策依据必须可审计，不能
	// 只有一个结果没有理由。
	Reason string
}

// Layout 是 DiskPlannerV1.Plan 的决策结果。
type Layout struct {
	Selected []LayoutDisk
}

// DiskPlannerV1 按磁盘清单与意图选盘（§7.1、§5.6、§7.6）。
//
// 纯决策，不带探活方法也不做 IO，理由与 TemplateResolver 相同（§7.2）：
// 磁盘规划函数若行为不完全确定，故障无法复现，而这套系统会擦盘，复现
// 能力是硬要求。
type DiskPlanner interface {
	Plan(ctx context.Context, disks []Disk, intent DiskIntent) (Layout, error)
}

// ProviderKind 标识扩展点种类，供 FailurePolicyFor 查询失败语义表使用。
type ProviderKind string

const (
	ProviderKindIPAM             ProviderKind = "ipam"
	ProviderKindDNS              ProviderKind = "dns"
	ProviderKindInventory        ProviderKind = "inventory"
	ProviderKindNotify           ProviderKind = "notify"
	ProviderKindHeartbeat        ProviderKind = "heartbeat"
	ProviderKindTemplateResolver ProviderKind = "template_resolver"
	ProviderKindDiskPlanner      ProviderKind = "disk_planner"
)

// FailurePolicy 描述某扩展点失败时应如何处理（§7.4）。
type FailurePolicy string

const (
	// FailurePolicyBlock 表示阻塞：该扩展点的失败必须阻止当前操作继续
	// （如阻塞建任务）。IPAM、TemplateResolver、DiskPlanner、Inventory
	// 都是这一类——决策或数据出不来，无法继续（Inventory 允许人工录入
	// 兜底，但那是调用方的补救路径，不改变「失败即阻塞本次调用」这一点）。
	FailurePolicyBlock FailurePolicy = "block"

	// FailurePolicyWarnOnly 表示只告警，不影响任务状态。DNS、Notify 属于
	// 这一类：一次 DNS 抖动不该把已经装好的机器标成失败。
	FailurePolicyWarnOnly FailurePolicy = "warn_only"

	// FailurePolicyRecordOnly 表示只记录探测结果，不改变任务状态。
	// Heartbeat 属于这一类：它是辅助通道，非主通道（§6.6）。
	FailurePolicyRecordOnly FailurePolicy = "record_only"
)

// failurePolicyTable 是 §7.4 失败语义表的原始数据。
var failurePolicyTable = map[ProviderKind]FailurePolicy{
	ProviderKindIPAM:             FailurePolicyBlock,
	ProviderKindTemplateResolver: FailurePolicyBlock,
	ProviderKindDiskPlanner:      FailurePolicyBlock,
	ProviderKindInventory:        FailurePolicyBlock,
	ProviderKindDNS:              FailurePolicyWarnOnly,
	ProviderKindNotify:           FailurePolicyWarnOnly,
	ProviderKindHeartbeat:        FailurePolicyRecordOnly,
}

// FailurePolicyFor 查询某扩展点的失败语义。ok 为 false 表示该扩展点未在
// 表中声明——查不到返回「未声明」而不是任何默认值，是刻意的方向：加新
// provider 类型时必须显式声明失败语义，给默认值会让人忘了想清楚「这个
// 失败该不该拦住装机」（计划任务 12 设计要点、§7.4）。调用方在 ok=false
// 时应视为配置或代码错误，不能自行猜测一个策略继续运行。
func FailurePolicyFor(kind ProviderKind) (policy FailurePolicy, ok bool) {
	policy, ok = failurePolicyTable[kind]
	return policy, ok
}
