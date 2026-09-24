// 本文件实现并发准入控制 AdmissionController（实现计划 phase4-01 任务 1）。
// 依据 SPEC §10 失败模式 11、§3.3。
//
// 背景：一批机器同时上电时，几十台同时拉几 GB 的仓库会让传输全部变慢——
// 慢到超过阶段预算时它们会一起超时失败。失败的原因看起来是超时，实际是
// 带宽争抢，这类故障极难从单台机器的日志看出来（§10 运维点）。
//
// 限流做在状态机层，不是 HTTP 层：
//  1. HTTP 层的限流只能拒绝请求，而拒绝一个引导请求意味着那台机器要么
//     停在 iPXE 要么进 shell，都不是好结果；状态机层可以让它继续 PXE
//     轮询，机器会自己再来（§4.2 的常态行为）。
//  2. 要限的是「同时在装的机器数」而不是「每秒请求数」——前者与带宽占用
//     直接相关，后者不相关。
//  3. 状态机知道任务状态，HTTP 层不知道；判断「现在有几台在装」需要
//     查状态。
//
// 计数口径：状态在 booting 或 installing 的任务数。不计 rebooting 与
// initializing——那两个阶段不占带宽（机器在重启或在跑初始化脚本）。
//
// 计数方式：每次判定时查库（两次 ListTasks，任务表是百级，这个查询很
// 便宜），不用内存计数器——进程重启后内存计数会丢，而重启期间已经在装的
// 机器仍在占带宽，重启后放行一批新机器会造成真正的拥塞。
//
// 为什么是独立的 AdmissionController 而不是 Machine 的方法：本任务的文件
// 归属只包含 admission.go 本身，不包含 machine.go/boot.go（七个 agent 按
// 文件分区并行开发，Machine 的结构体定义与 StartBoot 的调用点都在归属范围
// 之外）。做成一个独立类型，只依赖 contract.TaskStore（Machine 已持有的
// 同一个窄接口的父接口 contract.Store 天然满足它），使本文件不需要改动
// Machine 的结构体定义或 StartBoot 的实现就能独立编译、独立测试。真正把它
// 接入 StartBoot 的调用序列（precheck 通过之后、attempt++ 之前）需要由
// 拥有 boot.go 的改动方完成——见本次任务返回值的 contractGaps 与
// unfinished 说明。
//
// 契约缺口：internal/config 已冻结（本模块不允许修改），因此没有为准入
// 上限单独加一个 YAML 字段。最保守的处理：本模块提供一个包级默认值
// defaultAdmissionLimit，NewAdmissionController 以此为初始值，同时留出
// SetLimit 注入口——装配点（cmd/lober）今后若要从配置读取上限，只需要在
// 读到 config 之后调一次 SetLimit，不需要改动本文件。真正的解法是往
// config.Provider 或新增一个 config.Admission 分组里加一个
// concurrency_limit 字段，由 M9 装配时读出并注入。
package statemachine

import (
	"context"
	"sync/atomic"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// defaultAdmissionLimit 是并发准入控制的包级默认上限。
//
// 计划任务 1 要求「上限的默认值要给一个保守的数并说明怎么调：它取决于
// 机架上行带宽与单机的仓库拉取量」。本模块没有被注入任何真实的带宽或
// 传输量数据（P2-04 任务 5 的实测数据不在本模块的六个依赖范围内），因此
// 这里给出的是一个不依赖具体机房条件的保守估算方法与示例值：
//
//	并发上限 ≈ 机架上行带宽（Mbps） / 单机平均传输速率需求（Mbps）
//
// 例如：机架上行 1 Gbps（≈1000 Mbps），若要求每台机器至少能以 50 Mbps
// 的速率拉取仓库（对应主流 Linux 发行版的 packages 阶段 60 分钟预算内
// 传输完几 GB 的量级），则上限 ≈ 1000/50 = 20。20 是一个中等机架的保守
// 估算，取整数、留有余量（不是精确到刚好打满带宽），运维应结合本机房的
// 实测上行带宽与单机传输量调整——调整方式是调用 SetLimit，或待 config
// 补上对应字段后从配置读取（见本文件顶部契约缺口说明）。
const defaultAdmissionLimit = 20

// admissionCountedStates 是准入计数口径：只有处于这两个状态的任务才算
// 「占带宽」。rebooting 与 initializing 不计入——机器在重启或跑初始化
// 脚本，不占传输带宽；pending 还没开始占带宽；done/failed 是终态。
var admissionCountedStates = []model.TaskState{
	model.TaskStateBooting,
	model.TaskStateInstalling,
}

// AdmissionDecision 是 AdmissionController.Check 的返回结构：既给出
// 「是否放行」的结论，也给出计算这个结论所依据的原始数字
// （InFlight/Limit），供调用方在拒绝时把「当前在装 N 台，上限 M 台」写进
// 事件文案（计划任务 2，由 P2-console 那一波实现，本结构是它的数据来源）。
type AdmissionDecision struct {
	// Admitted 为 true 表示当前可以继续签发装机 token；为 false 表示已达
	// 并发上限，本次引导请求应该排队（不消耗 attempt、不置 failed），
	// 具体的「排队」表达方式（返回什么样的 iPXE 载荷）由调用方决定——
	// Check 本身只做「是否放行」这一个纯读判定，不产生任何副作用、不
	// 触碰任务的任何字段。
	Admitted bool

	// InFlight 是当前处于 booting 或 installing 状态的任务数（判定时刻
	// 的查库结果）。
	InFlight int

	// Limit 是本次判定使用的上限值（AdmissionController.limit 的快照），
	// 附带在结果里而不要求调用方另外去读，避免「判定用的是哪个值」与
	// 「事件文案里写的是哪个值」出现不一致的窗口。
	Limit int
}

// AdmissionController 是并发准入判定的独立实现，只依赖
// contract.TaskStore——不依赖 Machine 的其它五个依赖（Clock、
// TokenIssuer、Prechecker、LogStore、EventBus），因为准入判定本身不需要
// 它们：这是一个纯粹的「查库计数、比较上限」的只读操作。
type AdmissionController struct {
	store contract.TaskStore
	limit int
}

// NewAdmissionController 构造一个准入控制器，limit 初始化为包级默认值
// defaultAdmissionLimit（不是 0——0 有「不限」的特殊含义，不能与「调用方
// 没有明确配置」混淆；默认必须是一个生效中的保守上限，见本文件顶部常量
// 文档）。
func NewAdmissionController(store contract.TaskStore) *AdmissionController {
	return &AdmissionController{store: store, limit: defaultAdmissionLimit}
}

// SetLimit 覆盖并发准入上限，供装配点在读到配置（若 config 未来补上对应
// 字段）或运维通过某种运行期接口调整时调用。0 表示不限（向后兼容
// Phase 1——Phase 1 没有准入控制，未显式配置上限的部署不应该被本功能
// 改变行为）。
//
// 不加锁保护：limit 预期只在启动装配阶段写一次，之后只被 Check 读取；
// 如果将来需要支持运行期热调整（多个 goroutine 并发调用 SetLimit 与
// Check），需要改成 atomic.Int64 或加锁——当前的调用场景里没有这个需求，
// 加锁是不必要的复杂度。
func (a *AdmissionController) SetLimit(limit int) {
	a.limit = limit
}

// globalAdmissionLimit 是 StartBoot（boot.go）在准入检查这一步实际使用的
// 上限来源，用 atomic.Int64 而不是普通 int 字段——原因见下。
//
// 为什么需要一个包级变量而不是把 *AdmissionController 存成 Machine 的
// 字段：Machine 的结构体定义与六个依赖的构造签名在 machine.go（本任务的
// 文件归属不含 machine.go，见任务顶部铁律 1「归属文件清单」），无法在
// 不改动 machine.go 的前提下给 Machine 加一个新字段或给 New() 加第七个
// 构造参数。而 AdmissionController 本身只依赖 contract.TaskStore——
// StartBoot 已经持有 m.store（类型是更宽的 contract.Store，天然满足
// TaskStore），因此 StartBoot 可以在每次调用时用 m.store 现场构造一个
// AdmissionController，不需要 Machine 持有它作为字段；唯一还缺的是
// 「配置的上限值」这个数字本身要从某处传进来——globalAdmissionLimit 就是
// 那个传递通道：装配点（cmd/lober/wire.go）读到配置后调用
// SetGlobalAdmissionLimit 写一次，StartBoot 每次调用时读一次。
//
// 用 atomic.Int64 而不是普通 int + 包级 var：并发装机场景下，Check 会被
// 大量 goroutine（多个同时到达的 /v1/boot 请求）并发读取；若配置热更新
// 或多个测试并行修改这个值，普通 int 的读写在 -race 下会被判定为数据
// 竞争。atomic.Int64 是本文件顶部 SetLimit 方法自己文档里提到的「若需要
// 支持运行期热调整，需要改成 atomic.Int64」这条——这里正是那个场景：
// globalAdmissionLimit 与实例字段 limit 不同，它确实会被多个 goroutine
// 并发访问（每个 HTTP 请求对应的 goroutine 都会在 StartBoot 里读它）。
//
// 初始值即 defaultAdmissionLimit——未经装配点调用 SetGlobalAdmissionLimit
// 时，StartBoot 的准入检查仍然生效于一个保守的默认上限，不会因为「忘记
// 装配」而静默变成不限（这与 NewAdmissionController 默认落到
// defaultAdmissionLimit、而不是 0，是同一条设计原则）。
var globalAdmissionLimit atomic.Int64

func init() {
	globalAdmissionLimit.Store(int64(defaultAdmissionLimit))
}

// SetGlobalAdmissionLimit 供装配点（cmd/lober/wire.go）在读到配置中的
// 并发上限之后调用一次，覆盖 StartBoot 准入检查实际生效的上限。0 表示
// 不限（与 AdmissionController.SetLimit(0) 同一条语义，向后兼容未显式
// 配置上限的部署）。
//
// 并发安全：可以在运行期任意时刻调用（不仅是启动装配阶段）而不需要额外
// 加锁——底层是 atomic.Int64，与 StartBoot 里的读取操作天然互斥于同一个
// 内存字（不会读到一半新一半旧的字节）。
func SetGlobalAdmissionLimit(limit int) {
	globalAdmissionLimit.Store(int64(limit))
}

// newAdmissionControllerFromGlobalLimit 用 store 与当前的
// globalAdmissionLimit 现场构造一个 AdmissionController，供 StartBoot
// （boot.go）在准入检查这一步调用。每次调用都现场构造而不缓存实例：
// AdmissionController 本身不持有任何跨调用状态（limit 之外没有其它字段
// 需要保留，Check 是纯读操作），现场构造的成本是一次结构体字面量分配，
// 可忽略不计，换来的是不需要在 Machine 上增加任何字段。
func newAdmissionControllerFromGlobalLimit(store contract.TaskStore) *AdmissionController {
	a := NewAdmissionController(store)
	a.SetLimit(int(globalAdmissionLimit.Load()))
	return a
}

// Check 判定当前是否可以继续放行一次装机引导（在签发 token 之前调用）。
// 纯读操作：不修改任何任务字段、不产生事件、不做任何 CAS。
//
// 调用位置（计划任务 1「判定的位置：开始引导那一步（签发 token 之前）」）
// 应该是 StartBoot 的五步顺序中 precheck 通过之后、attempt++ 之前——但
// StartBoot 的实现在 boot.go（本轮不属于本文件的归属范围），因此本方法
// 只提供判定本身，实际的调用点接入需要由拥有 boot.go 的改动方完成：
// 在 attempt++ 之前调用 Check，Admitted=false 时不签 token、不增加
// attempt、不置 failed，直接把「排队」信号返回给网关层处理。
func (a *AdmissionController) Check(ctx context.Context) (AdmissionDecision, error) {
	if a.limit == 0 {
		return AdmissionDecision{Admitted: true, InFlight: 0, Limit: 0}, nil
	}

	inFlight, err := a.countInFlightTasks(ctx)
	if err != nil {
		return AdmissionDecision{}, err
	}

	return AdmissionDecision{
		Admitted: inFlight < a.limit,
		InFlight: inFlight,
		Limit:    a.limit,
	}, nil
}

// countInFlightTasks 查库统计当前处于 booting 或 installing 状态的任务数。
//
// 用两次 ListTasks（contract.TaskFilter 只支持单个 State 过滤，见
// internal/core/contract/store.go）而不是一次性拿全部任务再在 Go 里过滤：
// 任务表是百级，两次窄查询与一次全表扫描的成本量级相同，但窄查询让 Store
// 实现（如 internal/store/postgres）有机会用状态列上的索引，且不需要把
// 无关状态的任务也传输回内存。
func (a *AdmissionController) countInFlightTasks(ctx context.Context) (int, error) {
	total := 0
	for _, state := range admissionCountedStates {
		tasks, err := a.store.ListTasks(ctx, contract.TaskFilter{State: state})
		if err != nil {
			return 0, err
		}
		total += len(tasks)
	}
	return total, nil
}
