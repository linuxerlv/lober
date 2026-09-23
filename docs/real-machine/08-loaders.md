# exec/http Provider 加载器

对应分支 `feat/t13-loaders`。依据 SPEC §7.2、§7.4、§7.6，与
`docs/phase2-loader-需求依据.md` 的开工判据。

## 一句话

七个 provider 契约接口（`internal/core/contract/provider.go`）原本只有
一种内置实现（`static`/`noop`/`rule-based`/`tcp`/`icmp`/`off`）。本轮加了
第二种实现方式：把方法调用转成子进程 stdio 上的 JSON 或一次 HTTP POST，
对调用方完全透明——拿到的仍然是同一个契约接口，不知道背后是内置代码、
子进程还是 webhook（SPEC §7.2）。

## 为什么现在做（覆盖需求依据文档的"推迟"结论）

`docs/phase2-loader-需求依据.md` 的结论是"现在不满足开工条件，应推迟"
——因为 Phase 1 尚未上真机，无法给出"哪个扩展点接哪个外部系统"的具体
答案。**本轮仍然做，是因为用户在本次并行开发任务里明确要求"除了 TLS 和
安全启动，其他都做"**，这是一条覆盖该文档保守结论的显式指令，不是本轮
对该文档结论的反驳或推翻。做法上体现这条差异：本轮只交付 SPEC §7.6
说的"加载机制"本身（协议、传输、超时、重试、熔断），不编造任何具体的
「IPAM 该接哪个厂商系统」之类的假设——那部分仍然是未知数，交给运维在
真正需要接某个外部系统时自己写脚本或 webhook 服务端，加载器负责的是
"接进来之后怎么被可靠地调用"。

## 架构：Caller 是唯一的公共抽象

```
contract.IPAMProviderV1  ←── loader.IPAMAdapter ──┐
contract.DNSProviderV1   ←── loader.DNSAdapter   ─┤
contract.InventoryProviderV1 ←── loader.InventoryAdapter ─┼── loader.Caller ──┬── loader.ExecLoader（子进程）
contract.NotifyProviderV1 ←── loader.NotifyAdapter ───────┤                   └── loader.HTTPLoader（HTTP POST）
contract.HeartbeatProviderV1 ←── loader.HeartbeatAdapter ─┘
```

- **`Caller`**（`protocol.go`）：一个方法 `Call(ctx, method, params, result) error`。
  `ExecLoader`（`exec.go`）与 `HTTPLoader`（`http.go`）各自实现它，分别
  对应子进程 stdio 与 HTTP POST 两种传输——两者共用同一套 JSON 信封
  （`Request`/`Response`）、同一套重试退避（`callWithRetry`）、同一套
  熔断状态机（`breaker.go`）。
- **五个 Adapter**（`adapters.go`）：把 `Caller` 的通用方法包成具体契约
  接口的方法。例如 `IPAMAdapter.Allocate` 内部调用
  `caller.Call(ctx, "ipam.allocate", req, &result)`。只适配五个（IPAM/
  DNS/Inventory/Notify/Heartbeat），**不适配 TemplateResolver/
  DiskPlanner**——这两个是纯决策接口，刻意不带 `Ping` 方法（契约文档：
  "纯函数不需要健康检查"），需求依据文档第 4 节与 SPEC §7.6 已经给出
  明确结论"不需要外部化"，套加载器与这条设计意图直接矛盾。

**给 exec 脚本 / http 服务端作者的字段命名提示**：`Request.Params` 与
`Response.Result` 里携带的具体参数/返回值类型（`contract.AllocRequest`、
`contract.ProbeTarget` 等）**没有 JSON 标签**（`internal/core/contract`
是冻结契约，本任务不能加标签）——`encoding/json` 对无标签字段的默认行为
是直接用 Go 的导出字段名，因此实际收到/要返回的 JSON 键是**大写开头**的
`Hostname`/`Role`/`IP`/`Gateway`/`Mask`/`TaskID`/`Event`/`Detail` 等，
不是小写的 `hostname`/`ip`。已用 `contract.AllocRequest{Hostname:
"node-1", Role: "web"}` 实测确认序列化结果为
`{"Hostname":"node-1","Role":"web"}`。`InventoryAdapter.LookupBySN` 是
唯一的例外——它的返回类型 `inventoryLookupResult`（`adapters.go`
私有类型）是本包自定义的、带 JSON 标签的小写字段（`sn`/`mac`/`uuid`/
`arch`/`firmware`/`rack`/`last_seen_at`），因为它的真实返回类型
`model.Host` 也没有 JSON 标签，与其让外部程序对着大写字段名，不如在
适配器这一层定义一个明确标注了字段名的中间类型。
- **`ReloadableLoader`**（`reload.go`）：包一层原子可替换的 `Caller`。
  SPEC §7.2 的加载器对比表说 exec/http "改已有的" → "不重启（下次调用
  即生效）"——这条属性对"改脚本文件内容/改 webhook 服务端逻辑"本身是
  免费的（每次调用都是全新的子进程/HTTP 请求，天然读到最新内容），
  `ReloadableLoader` 解决的是另一半："改加载器的构造参数本身"（换一个
  不同的 command 路径、不同的 url、不同的超时设置）需要重新
  `NewExecLoader`/`NewHTTPLoader`，`ReloadableLoader.Swap` 让调用方能
  原子替换底层实例而不需要重启整个进程，且不影响正在执行中的调用
  （in-flight 调用已经拿到旧实例的引用，会跑完这一次）。

## 失败语义：本轮没有新造一套，是复用既有的

SPEC §7.4 的失败语义表（阻塞 / 只告警 / 只记录）**不是加载器自己的
逻辑，是调用方（`internal/provider/resolve.go` 与
`internal/provider/lifecycle.go`）已经实现好的既有行为**：

- `resolve.go` 的 `taskParamsResolver.Resolve` 依次调用
  `IPAMProviderV1.Allocate`、`TemplateResolver.Resolve`、
  `DiskPlanner.Plan`，任一返回非 nil error 就整体失败——这是"阻塞建
  任务"语义的落地方式，与被调用的具体实现（内置还是加载器）无关。
- `lifecycle.go` 的 `onTaskReady`/`onTaskFailed` 调用
  `DNSProviderV1.Register`/`NotifyProviderV1.Emit`，失败只记日志、不
  向上传播——这是"只告警"语义的落地方式，同样与具体实现无关。

因此本轮**没有在加载器包里重新实现一套"阻塞 vs 告警"的判断逻辑**——
只要 `ExecLoader`/`HTTPLoader`/五个 Adapter 老老实实地在失败时返回一个
非 nil 的 Go `error`（它们确实这样做，见 `adapters_test.go` 的
`TestDNSAdapter_RegisterPropagatesError` 等测试），既有的调用方代码就会
自动按正确的策略处理，不需要改动 `resolve.go`/`lifecycle.go` 一行代码
——这正是 SPEC §7.6 "接口边界是真正有价值的部分，加载机制可以后补"这句
话的字面意思：加载器是接口的另一种实现，接口不变，调用方不用变。

`HeartbeatAdapter.Probe` 是唯一需要特殊处理的一个，原因是方法签名本身
不返回 `error`（`Probe(ctx, targets) []ProbeResult`，与"record_only 只
记录、不阻塞任何流程"的语义对应）。整批加载器调用失败时，
`HeartbeatAdapter.Probe` 把它降级成"每个目标各自标记为未探测到"，而不是
panic 或返回长度不一致的切片——与内置 tcp/icmp 实现"单目标失败不影响其余"
的既有约定在可观察行为上一致。

## 配置：新增 `LoaderConfig`，五个独立字段

`internal/config/config.go` 的 `Provider` 结构体新增
`IPAMLoader`/`DNSLoader`/`InventoryLoader`/`NotifyLoader`/
`HeartbeatLoader` 五个 `LoaderConfig` 字段（YAML:
`ipam_loader`/`dns_loader`/`inventory_loader`/`notify_loader`/
`heartbeat_loader`）。`IPAM`/`DNS`/`HeartbeatMode` 三个既有的选择项
字符串新增合法值 `"exec"`/`"http"`（`Resolver` 没有新增——见上一节）。

`Validate()` 新增结构性校验（`validateLoaderSelection`）：选择项是
`exec`/`http` 时，强制要求对应 `LoaderConfig.Type` 显式填写、与选择项
一致、且 `Type=exec` 时 `Command` 非空、`Type=http` 时 `URL` 非空——
不给的运行期表现是"运行期第一次调用才报错"（例如第一次建任务时 IPAM
加载器才发现没配 command），必须在启动时拒绝。选择项不是 exec/http 时，
对应 `LoaderConfig` 的内容完全被忽略，不做任何校验——运维切换回内置
实现不需要同步清空已经写好的加载器参数段。

`deploy/lober.yaml.example` 的 `provider` 段已加上完整的字段占位（默认
留空、不生效）与两个真实示例（IPAM 走 exec、DNS 走 http）的注释块。

## 工厂接线：`factory.go` 新增 3 个 case 分支

`newIPAMFromConfig`/`newDNSFromConfig`/`newHeartbeatFromConfig` 三个
既有 switch 各加一个 `case "exec", "http":` 分支，调用新的私有辅助函数
`newLoaderCaller`（按 kind 构造 `ExecLoader` 或 `HTTPLoader`），再用对应
的 Adapter 包一层返回。三个既有 case（`static`/`noop`/`tcp`/`icmp`/
`off`）的代码与行为完全没有改动。

`TestFactory_IPAMExec_ConstructsWorkingAdapter`/
`TestFactory_DNSHTTP_ConstructsWorkingAdapter`/
`TestFactory_HeartbeatHTTP_ConstructsWorkingAdapter`（`factory_test.go`）
端到端验证了这条接线——不只是"类型对了"，是真的构造出一个子进程/HTTP
服务端、真的发起一次调用、确认结果被正确解码。

Inventory/Notify 两个扩展点**没有加对应的 case 分支**：`factory.go`
现状对这两者无条件构造内置版本，不通过配置选择（`factory.go` 顶部
既有的契约缺口说明第 3 条：MVP 阶段两者只有内置实现）。本轮新增的
`InventoryLoader`/`NotifyLoader` 两个配置字段**目前只有结构性校验，
没有工厂接线**——运维即使填了这两段配置，工厂也不会读取它们，见下方
"未接线的部分"。

## 已验证的行为（真实测试，不是"读代码觉得没问题"）

以下每一条都有对应的自动化测试，且实际跑过、看到通过输出（不是推断）：

- exec 加载器：子进程正常返回并被正确解码
  （`TestExecLoader_DecodesResult`）；非零退出报错
  （`TestExecLoader_NonZeroExit`）；退出码 0 但业务错误同样报错
  （`TestExecLoader_BusinessError`）；**超时后子进程被真正杀掉**——用一个
  `sleep 2 分钟` 的子进程验证，`Call()` 在配置的 200ms 超时后就返回，
  远早于子进程自然退出的时间，证据是 Go 标准库 `os/exec.Cmd.Wait()`
  的既定契约（只有进程真正终止才会返回），不是"等超时就假装完事"
  （`TestExecLoader_TimeoutKillsProcess`）。
- http 加载器：200 正常解码（`TestHTTPLoader_DecodesResult`）；非 2xx
  报错（`TestHTTPLoader_NonSuccessStatus`）；超时报错且不等服务端响应
  （`TestHTTPLoader_Timeout`）。
- 熔断：连续失败达到阈值后 `Allow()` 返回 false，之后的调用不再真正
  拉起子进程/发 HTTP 请求（`TestExecLoader_CircuitBreakerStopsCallingProcess`、
  `TestHTTPLoader_CircuitBreakerStopsCallingServer` 直接断言服务端收到的
  请求数没有超过阈值，不是间接推断）；一次成功清零连续失败计数
  （`TestCircuitBreaker_SuccessResetsCount`）。
- 配置校验：选了 exec 没给 command / 选了 http 没给 url / loader 段
  忘填 type / type 与选择项不一致 / type 不在 exec/http 白名单内，五种
  笔误场景全部在 `config.Validate()` 阶段被拒绝
  （`config_test.go` 的五个对应测试）。
- 失败语义透传：`DNSAdapter.Register` 失败时错误原样传出
  （`TestDNSAdapter_RegisterPropagatesError`），`InventoryAdapter.LookupBySN`
  同理；`HeartbeatAdapter.Probe` 整批失败时降级为逐目标失败而不是
  panic 或截断结果（`TestHeartbeatAdapter_ProbeDegradesOnCallFailure`）。
- 并发安全：`ReloadableLoader` 在 `go test -race` 下的并发 Call/Swap
  混合压测无数据竞争（`TestReloadableLoader_ConcurrentCallAndSwap`）；
  `circuitBreaker` 同样过 `-race`。
- 全仓回归：`go test ./... -count=1` 与 `go test ./... -race`（对
  `internal/provider/...` 全部子包）均全绿，`internal/archtest` 的三条
  依赖方向守卫（model 无依赖、contract 只依赖 model、七个实现模块互不
  import）均通过——`internal/provider/loader` 作为 `internal/provider`
  的子包，与该模块内其它文件共享同一个模块分组，不违反"互不 import"
  规则。

## 不做什么（明确排除，避免范围误解）

- **不做 grpc**：SPEC §7.2 的判据是"有真逻辑的集成"才需要 grpc（协议
  复杂、需要保持连接或多步交互），这是 Phase 3 范围，本轮未实现。
- **不做 starlark**：纯决策接口（TemplateResolver/DiskPlanner）继续用
  内置策略，SPEC §7.6 的既定结论，本轮不改动。
- **不做 TLS 与安全启动**：任务描述明确排除的两项。
- **不做 §7.3 grpc 类型的完整热更新五件套**（换二进制原子链接、
  in-flight drain、健康检查回滚、崩溃自动重启、协议版本协商）——那五条
  是 grpc 类型的专属要求（§7.3 标题就是"热更新要求（grpc 类型）"）。
  exec/http 类型只需要"改已有的不重启"这一条属性，`ReloadableLoader`
  提供的原子替换已经满足它，不需要照搬 grpc 的全部五条。

## 未接线的部分（wiringNeeded，需要别的轨接线）

1. **`resolve.go` 的 `ipamFactory` 硬编码为 `newStaticIPAM`，完全不读
   `Providers.IPAM` 字段**——即使运维把 `provider.ipam` 配成 `"exec"`
   且本工厂成功构造出一个 `loader.IPAMAdapter`，真正参与建任务决策的
   仍然是 `resolve.go` 里硬编码的 static 实现，`Providers.IPAM` 字段
   实际上只被 `Ping` 探活等辅助路径使用。用 `cmd/lober/wire.go` 第
   434 行确认过：`provider.NewTaskParamsResolver(providers.TemplateResolver,
   providers.DiskPlanner, diskIntent)` 这一调用完全没有传入
   `providers.IPAM`。正解需要改
   `internal/provider/resolve.go`（`taskParamsResolver` 的构造签名与
   `ipamFactory` 字段的构造方式）与 `cmd/lober/wire.go`
   第 434 行附近（把 `providers.IPAM` 传进去），两者都不在本任务的文件
   归属范围内。
2. **Inventory/Notify 没有工厂 case 分支**——`InventoryLoader`/
   `NotifyLoader` 两个配置字段目前只有结构性校验，没有对应的
   `newInventoryFromConfig`/`newNotifyFromConfig` 函数与选择项字段
   （`factory.go` 现状：这两者无条件构造内置版本）。若要接线，需要在
   `config.Provider` 补两个选择项字符串字段（如 `Inventory`/`Notify`），
   并在 `factory.go` 补两个对应的构造函数——这些改动都在本任务允许的
   文件范围内（`config.go`/`factory.go`），但因为需求依据文档明确说
   "不知道要接哪个具体外部系统"，本轮选择不新增这两个选择项字段（避免
   在没有具体需求的情况下猜测字段命名与默认值），只做了 `LoaderConfig`
   本身与结构性校验，接口留好，真正的选择项字段留给"确实需要接某个具体
   系统"的那一刻再加。
