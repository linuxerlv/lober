# 09 心跳探测：辅助通道的补全与装配

> 本文档记录 t14-heartbeat 这次工作：把 `internal/provider/heartbeat.go`
> 已有的 tcp/icmp 两个探测器接入生产循环。依据：SPEC §6.6、§7.4；
> `internal/provider/factory.go` 顶部的契约缺口说明；`cmd/lober/scanner.go`
> 的既有循环模式。

## 现状：探测器一直都有，缺的是「循环有没有真的在跑」

在本轮工作开始之前，`internal/provider/heartbeat.go` 的 `tcpHeartbeat`
与 `icmpHeartbeat` 已经实现且有完整的单元测试覆盖（可达/不可达/并发上限/
无权限降级四类场景），`internal/provider/factory.go` 的
`newHeartbeatFromConfig` 也已经按 `config.Provider.HeartbeatMode`
（tcp/icmp/off）构造出对应实现，挂在 `Providers.Heartbeat` 字段上。

但这些代码从未被任何生产循环调用——工厂造出探测器之后，没有任何地方
定期查询任务、组装探测目标、调用 `Probe`、处理结果。探测器本身是一段
「随时能用但没人用」的死代码。本轮补的正是这一段：`cmd/lober/heartbeat.go`
的 `runHeartbeatLoop`，以及把它接入 `server.go` 的 `runServers` 生命周期。

## 心跳是辅助通道，不是主通道——这条边界体现在代码的哪些地方

SPEC §6.6 的定位很明确：机器主动推送信号（`/v1/boot` 之后的七个装机期
信号 + 四个重启后信号）后，拉取不再承担状态推进，只在「推送断了」时
兜底判断「机器是还活着还是真死了」。这条边界在实现里落成三个具体约束：

1. **探测失败绝不改任务状态**。`cmd/lober/heartbeat.go` 的
   `logHeartbeatResults` 是本文件里唯一触碰 `ProbeResult` 的函数，它
   只调用 `slog`，不持有任何状态机或 Store 的写入依赖——物理上做不到
   改状态，不是靠一条 `if` 判断绕过。§7.4 的状态转换表里根本没有
   「心跳失败」这一个触发源，如果探测失败能改状态，一次网络抖动就会
   把正在装的机器错误标成失败。
2. **只探测非终态且处于「装机中」区间的任务**。
   `heartbeatProbeableStates` 固定为
   `{booting, installing, rebooting, initializing}`——`pending` 还没到
   需要探活的阶段（机器甚至没请求过 `/v1/boot`），`done`/`failed` 是
   终态，探测已经没有意义。
3. **拉取的本质局限要显式承认，不是隐藏在注释里的免责声明**。ICMP/TCP
   在最值得关心的故障模式里全是正常的——anaconda 卡在依赖解析、hook
   死循环、仓库 404 重试时，内核活着、网络通着，拉取探测不到任何异常。
   它能替代的是「存活判断」（机器是不是彻底断了），替代不了「进展判断」
   （机器是不是卡住了）。进展判断的信息来源是信号 + syslog，不是心跳。
   运维如果指望心跳探测发现「装机卡住」，会等到装机超时（§3.3 的分阶段
   预算）才发现问题，比实际卡住的时刻晚得多。

## 默认 tcp 而非 icmp：为什么心跳这件事不值得给进程提权

`internal/provider/factory.go` 的 `newHeartbeatFromConfig` 与
`internal/config/config.go` 的 `defaultHeartbeatMode` 都已经把默认值
定成 `"tcp"`。理由不是「tcp 更简单」，是权限模型的取舍：ICMP 发包需要
特权套接字（root、`CAP_NET_RAW`，或调整内核 `ping_group_range`），而
心跳只是辅助通道——它的收益是「能探测到还没配业务 IP、只能靠 ICMP
探活的机器」，代价是给整个进程提权，提权的影响面是全系统。这笔交易
不划算，因此默认关掉需要提权的那条路径。

`icmpHeartbeat` 仍然完整实现且可通过配置显式启用（`heartbeat_mode:
icmp`），但**无权限时必须优雅降级，不能 panic**——这条约束由
`internal/provider/heartbeat_test.go` 的
`TestHeartbeatICMP_NoPrivilegeGracefulDegradation` 锁住：
`icmp.ListenPacket` 失败时返回一条 `Reached=false` 且 `Detail` 里写明
「需要提权」的结果，不让整批探测跟着失败，运维看到的是「心跳不可用，
原因是权限」而不是「所有机器都死了」。

## tcp 探测端口从哪来：一个已记录的契约缺口

SPEC §6.6 原文描述是「tcp 用表单中的业务 IP + 指定端口」，但
`contract.ProbeTarget` 只有 `TaskID` 与 `IP` 两个字段，没有端口。这处
缺口在 `internal/provider/heartbeat.go` 顶部已有详细记录，本轮沿用
同一个保守绕法：`newTCPHeartbeat(port)` 在**构造时**固定注入一个端口
（工厂里的 `defaultTCPHeartbeatPort = 22`），对全部目标使用同一个端口。
本轮新增的 `runHeartbeatLoop` 不重复这层决策——它只负责「从任务里取出
业务 IP、组装成 `ProbeTarget`」，端口已经在探测器构造时定死，调用循环
不需要也不应该关心端口从哪来。

正解方向（不是本轮能做的）：给 `contract.ProbeTarget` 或表单契约补一个
端口字段，由建任务时的表单数据带出具体业务端口，而不是全局固定一个
猜测值。

## `runHeartbeatLoop` 的设计：与 `runScanLoop` 同一套纪律

`cmd/lober/heartbeat.go` 的循环结构直接照抄
`cmd/lober/scanner.go`（超时扫描循环）的既有模式，理由是这两条后台
循环面对的是同一类工程问题：

- **启动时立即跑一轮，不等第一个间隔**——进程重启期间机器可能已经在
  装了，不该空等到第一个 60 秒才开始探测。
- **`ctx` 取消后清出返回的 `done` channel 必须被关闭**——调用方
  （`server.go` 的 `runServers`）据此判断循环真正退出，避免优雅关闭
  时 goroutine 泄漏。这条在 `-race` 下用
  `TestRunHeartbeatLoop_ContextCancelStopsLoopCleanly` 锁住。
- **单次执行失败不中断循环本身**——`heartbeatOnce` 里某一个状态的
  `ListTasks` 查询出错只记日志、继续查下一个状态；`Probe` 返回的失败
  结果也只记日志。循环退出的后果是静默停摆到下次重启，没人会注意到
  心跳探测不见了，这与超时扫描循环的取舍完全一致。
- **非正数间隔兜底为 60 秒**——`config.applyDefaults` 已经在配置层
  填充过默认值（§6.6 的默认 60 秒），这里的兜底是双重保险，防止测试
  或未来某个构造路径传入零值时循环忙等。

与扫描循环的差异只有一处：`runScanLoop` 依赖单方法窄接口
`timeoutScanner`，而心跳循环依赖两个窄接口——`heartbeatTaskLister`
（只需要 `ListTasks`）与 `contract.HeartbeatProviderV1`（已经是 M7
定义好的窄接口，直接复用不需要再包一层）。两个依赖分别对应「查任务」
与「探测」两件不相关的事，测试替身可以分别构造，不需要为了测一个方法
搭出另一个方法的假实现。

## 接入点：`server.go` 的 `runServers`，不是 `main.go`

心跳循环的启停加在 `cmd/lober/server.go` 的 `runServers` 里，与
`runScanLoop`（在 `main.go` 的 `serve` 里启动）不是同一个函数。这不是
风格不一致，是本轮任务的文件所有权边界决定的——本轮只拥有
`server.go`（且只允许加探测循环的启停，不改既有 listener 逻辑），
`main.go` 不在本轮可写文件列表里。

好在 `runServers` 本身就是唯一同时持有 `app`（心跳需要的
`app.Store`、`app.Providers.Heartbeat`）与 `cfg`（心跳需要的
`cfg.Provider.HeartbeatInterval`）的装配后调用点，且它的生命周期
（阻塞直到 `ctx` 取消或某个 listener 报错，随后走优雅关闭）与心跳
循环的启停时机完全吻合，因此这个接入点本身没有妥协。

心跳循环用独立的 `heartbeatCtx`/`cancelHeartbeat`（从外层 `ctx`
派生），而不是直接复用外层 `ctx`——两个 HTTP listener 走 `Shutdown`
优雅关闭（等在途请求完成，`shutdownGrace` 15 秒宽限期），心跳循环没有
「在途请求」这个概念，`ctx` 取消后应该立刻退出，不需要陪着 HTTP
listener 等宽限期。`runServers` 返回前用 `defer` 无条件
`cancelHeartbeat()` 并等待其 `done` channel，两条路径（正常经 `ctx.Done()`
退出、异常经某个 listener 报错退出）都会执行这个 `defer`。

## icmp 探测目标地址：`X-Real-IP` 是网关层的事，不是本轮能接的

SPEC §6.6 提到 icmp 探测器的目标地址来自 `/v1/boot` 请求的
`X-Real-IP`。本轮的 `runHeartbeatLoop` 只从 `task.ResolvedParams`
读取 `model.FactBizIP`（业务 IP），不读取 `X-Real-IP`——理由是
`X-Real-IP` 是引导请求时刻的瞬时信息，属于 `internal/gateway` 处理
`/v1/boot` 请求时才能看到的上下文，本轮拥有的文件里没有任何一处能
接触到这个值（`internal/gateway/**` 明确不在本轮可写范围）。若要让
icmp 探测器使用 `X-Real-IP` 而不是业务 IP，需要网关层在处理
`/v1/boot` 时把这个地址写进任务的某个字段（可能是 `ResolvedParams`
的另一个键，或 `model.Task` 的新字段），供本轮的 `heartbeatOnce`
读取——这是一处记入 `contractGaps` 的缺口，不是本轮能自行决定怎么补
的边界。当前实现对 tcp 与 icmp 两种模式统一使用业务 IP，这是「不知道
`X-Real-IP` 该从哪个字段读」时影响面最小的选择。

## 业务 IP 可达性：为什么这条不在本轮做

SPEC §6.6 末段要求，若用业务 IP 探测 `ready`，需要在建任务时校验业务
IP 对服务端可达——若业务网与管理网不通，探测永远失败、任务永远卡在
`initializing`（虽然探测失败不改状态，但运维会一直看到「未确认存活」
的日志，且如果控制台的状态判断依赖过心跳，会误以为机器卡住）。

这条校验逻辑本质上属于「建任务时的同步决策」，涉及的是
`internal/console` 处理建任务请求的路径（校验表单里的业务 IP 与
服务端的网络可达性），而 `internal/console/**` 明确不在本轮可写范围
（本轮只拥有 heartbeat 相关的六个文件）。本轮做的心跳探测本身不依赖
这条校验是否存在——即使业务网真的不通，探测失败也只是记日志，不会
导致任何状态被错误改变，只是运维会持续看到「未确认存活」的日志而已，
这是当前实现在缺了这条校验时能保持的最坏情况上界（不会更差）。

这条记入 `contractGaps`：正解方向是在建任务的同步决策链路（§7.4）
里加一步「业务 IP 可达性预检」，与 IPAM/DNS 决策同一批做完，而不是
等到心跳循环第一次探测失败才发现网络不通。

## Redfish 的接口预留：现在不做，接口留着

SPEC §10 失败模式 12 提到若将来接 Redfish（带内管理），需要间隔
≥60 秒、全局并发 ≤8、按 BMC 串行——这与当前 tcp/icmp 探测器「间隔
60 秒、全局并发 ≤32、目标间互相独立」的取舍方向一致但幅度不同（并发
上限更严格、且需要按目标分组串行而不是全局打散并发）。

本轮不实现 Redfish 探测器，也不改
`contract.HeartbeatProviderV1`——现有的 `Probe(ctx, targets)
[]ProbeResult` 接口形状本身没有任何假设「探测方式必须是无状态的批量
并发」，未来接 Redfish 时可以另起一个实现同一接口的探测器（按 BMC
分组、组内串行、组间可并发，且把并发上限降到 8），不需要改动
`runHeartbeatLoop` 或 `contract` 层的任何签名。这是「接口留着」的
具体含义：不是预写了 Redfish 相关的占位代码，是接口的形状恰好不
排斥这种扩展。

## 测试覆盖

`cmd/lober/heartbeat_test.go`（8 条）：

| 测试 | 锁住的行为 |
|---|---|
| `TestRunHeartbeatLoop_ExecutesImmediatelyOnStart` | 启动时立即探测一轮，长间隔下短时间内恰好一次 |
| `TestRunHeartbeatLoop_RepeatsOnInterval` | 循环按间隔重复探测 |
| `TestRunHeartbeatLoop_ContextCancelStopsLoopCleanly` | `ctx` 取消后 `done` channel 关闭，`-race` 下验证无 goroutine 泄漏 |
| `TestRunHeartbeatLoop_NonPositiveIntervalFallsBackToDefault` | 非正数间隔兜底为 60 秒，不忙等 |
| `TestRunHeartbeatLoop_OnlyQueriesProbeableStates` | 每轮恰好查询四个可探测状态各一次，不查 pending/done/failed |
| `TestRunHeartbeatLoop_ProbesOnlyTasksWithKnownAddress` | 没有业务 IP 的任务不进探测目标列表 |
| `TestRunHeartbeatLoop_ProbeFailureDoesNotPanicOrStopLoop` | 探测失败不 panic、不停摆，循环继续累加调用次数 |
| `TestRunHeartbeatLoop_NoProbeableTasksSkipsProbeCall` | 无可探测任务时不调用 `Probe`，但仍完成 `ListTasks` 查询 |

`internal/provider/heartbeat_test.go`（既有，本轮未改动，重新跑过
确认仍全绿）覆盖 tcp 可达/不可达/单目标失败不影响其余/并发上限、icmp
无权限优雅降级/`Ping` 不 panic、空目标列表。

`cmd/lober/server_test.go`（既有，本轮未改动测试代码本身，但
`runServers` 现在会启停心跳循环，三条既有测试
`TestRunServers_ContextCancelReturnsCleanly`、
`TestRunServers_SyslogReceivesUDPPacket`、
`TestRunServers_PortConflictFailsImmediately` 全部重新跑过确认
仍通过——证明心跳循环的接入没有改变 `runServers` 对外可观察的既有
行为）。

## 验证

`go vet ./cmd/lober/... ./internal/provider/... ./internal/config/...`、
`go test ./cmd/lober/... ./internal/provider/... ./internal/config/...
-v -count=1`、三者的 `-race` 版本、`gofmt -l .`（全仓）、
`go test ./... -count=1`（全仓 16 个包）、
`bash scripts/e2e.sh`（九阶段）均已实际执行并观察到通过输出，见本轮
提交前的验证记录。
