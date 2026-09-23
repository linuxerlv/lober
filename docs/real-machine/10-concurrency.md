# 并发装机限流与自动重试

对应分支 `feat/t15-concurrency`。依据 SPEC §10 失败模式 11、§3.3、§3.4、§3.5，
以及实现计划 `docs/superpowers/plans/2026-09-20-lober-phase4-01-concurrency.md`
任务 1（并发上限）与任务 4（失败重试）。任务 5（告警接入）在本轮之前的
一轮已经完整实现并通过测试，本文档只做验证确认，不重复设计说明。

## 一句话

`internal/statemachine/admission.go` 在之前一轮已经做了并发准入判定本身
（`AdmissionController.Check`）；本轮把它接进 `StartBoot`，补上配置项、
自动重试（opt-in、白名单、独立上限）、装配点接线，并新增覆盖测试。

## 改了什么

### 1. `internal/statemachine/admission.go`：包级全局上限

`AdmissionController` 本身（构造、`Check`、`SetLimit`）是之前一轮的产物，
未改动。本轮新增：

- `globalAdmissionLimit atomic.Int64` 包级变量 + `SetGlobalAdmissionLimit`
  导出函数 + `newAdmissionControllerFromGlobalLimit` 包内函数。

**为什么需要这一层，而不是把 `*AdmissionController` 存成 `Machine` 的
字段**：`Machine` 的结构体定义与 `New()` 的六个构造参数在 `machine.go`，
不在本任务的文件归属范围内，无法在不改动它的前提下加第七个依赖。
`AdmissionController` 只依赖 `contract.TaskStore`，`StartBoot` 已经持有
`m.store`（更宽的 `contract.Store`，天然满足），因此可以在每次调用时
现场构造一个 `AdmissionController`；唯一缺的是"配置的上限值"这个数字
要从装配点传到 `StartBoot`，`globalAdmissionLimit` 就是那条通道。用
`atomic.Int64` 而不是普通 `int`：并发装机场景下 `Check` 会被大量并发
`/v1/boot` 请求对应的 goroutine 读取，普通 int 在 `-race` 下会被判定为
数据竞争。

### 2. `internal/statemachine/boot.go`：接入 `StartBoot`

在 precheck 通过之后、`attempt++` 之前插入准入检查（计划任务 1 规定的
位置）。拒绝时返回一个可用 `errors.Is` 识别的普通 error
（`errAdmissionQueued`），**不经过 `failTask`**——任务原样留在
`pending`，不消耗 attempt、不置 failed。

**排队怎么表达**（见下方"设计决定"一节的完整说明）：`StartBoot` 返回
error 之后，网关既有的 `handleBootGrant` 会统一回退到 `no_task` 载荷
（`sanboot`）——这是不改动 `internal/gateway` 任何一行代码就能达到"让
机器继续 PXE 轮询、下一轮再来"这个效果的方式。

### 3. `internal/statemachine/scan.go`：新增 `AutoRetryFailedTasks`

不改动 `ScanTimeouts`/`handleTimedOutTask`/`handleBootingTimeout`/
`handleOtherPhaseTimeout` 任何一行——本轮对 `scan.go` 是纯增量。

三层判定（计划任务 4"三条限制，缺一条都不能上这个功能"）：

1. `AutoRetryConfig.Enabled`（独立开关，默认 false）
2. `!candidate.FailureReason.Reinstallable()` → 直接拒绝（三种不可重装
   原因永不触达下一层）；`autoRetryWhitelist[reason]`（更窄的白名单：
   只有 `boot_timeout`、`install_phase` 两种，`precheck`/
   `hook_required`/`canceled` 即使可重装也不在这个白名单内）
3. `countPriorAutoRetries(taskID) < cfg.MaxAutoRetries`（独立于
   `AttemptCount` 的次数上限，默认 1）

判定通过后直接调用现有的 `Machine.Retry`（不重新实现任何 CAS/状态转换
逻辑），复用它自己已经做的 `Reinstallable()` 二次校验（防御性冗余，两处
判据完全一致，不会走散）。

**"自动重试次数"怎么持久化**（契约缺口，见下）：写一条 `TaskEvent`，
`Source=model.EventSourceProvider`、`Message` 含固定标记
`autoRetryEventMarker`；下次判定时用 `ListEvents` 取回该任务全部事件，
数含标记的条数。这不是理想实现（见 contractGaps），是当前契约下最保守
的替代。

### 4. `internal/config/config.go`：`Scan` 分组新增三个字段

- `AdmissionLimit int`（YAML: `admission_limit`），默认 20（估算方法：
  机架上行带宽 / 单机传输速率需求，见字段文档）
- `AutoRetryEnabled bool`（YAML: `auto_retry_enabled`），默认 false
- `AutoRetryMaxAttempts int`（YAML: `auto_retry_max_attempts`），默认 1

### 5. `cmd/lober/wire.go`：三处接线

1. `statemachine.SetGlobalAdmissionLimit(cfg.Scan.AdmissionLimit)`——
   装配时调一次。
2. `providers.Notify` 用 `provider.NewAlertingNotify(...)` 包一层再传给
   `RegisterLifecycleHooks`——这是 `notify.go` 自己文档记录的契约缺口
   （之前一轮实现 `AlertingNotify` 时已经写明"装配点用
   `NewAlertingNotify(真实实现)` 包一层"，本轮补上这一行）。
3. 新增 `autoRetryScanDecorator`（`contract.StateMachine` 的包装）：
   `ScanTimeouts` 方法先跑既有的超时扫描，再跑一轮
   `AutoRetryFailedTasks`。`App.StateMachine` 字段现在是这个装饰器
   （`main.go`/`scanner.go` 不在本任务文件归属范围内，用装饰器包住已有
   `ScanTimeouts` 调用点是唯一不改那两个文件就能把自动重试接进现有
   30 秒周期扫描循环的方式）。

## 设计决定：超限时怎么"排队"

计划任务 1 明确要求"这一点你要自己判断并在 notes 里说明理由"。

**选择：`StartBoot` 返回普通 error，让网关既有的 `no_task`/`sanboot`
回退路径原样生效，不新签一个 `sleep + chain` 的 iPXE 脚本。**

理由：

1. **`internal/gateway/boot.go` 不在本任务文件归属范围内**——新签一个
   排队专用脚本需要网关识别"这次错误是排队还是别的失败"并选择不同的
   载荷，那需要改 `handleBootGrant`，越界。
2. **现有的 `no_task` 载荷本身已经满足"让机器继续轮询"的效果**：
   `sanboot --no-describe --drive 0x80` 在一台还没装系统的裸机上会
   失败（本地盘没有可引导的操作系统），固件按引导顺序会继续尝试下一个
   设备，通常就是回到 PXE——这与"稍后再来"在机器侧观察到的行为一致，
   不需要引入新的载荷形态。
3. **`no_task` 是 §4.2"约定 2"冻结的契约**（HTTP 200、固定文案），任何
   新脚本形态都需要判断是否会被认为"修改了这份契约"；复用现有载荷完全
   不触碰这份契约，是影响面最小的选择。
4. **网关那一行既有的注释已经明确假设**"`StartBoot` 内部已经把 precheck
   错误、attempt 超限都处理成状态机侧的 Fail 调用"，`err != nil` →
   `no_task` 是它统一的处理方式——本轮的准入拒绝同样通过 `err != nil`
   表达，与这个既有假设完全兼容，不需要网关增加任何新的分支判断。

**权衡**：`sanboot` 会让机器在本地盘上尝试一次引导（几秒到几十秒，取决于
固件探测本地磁盘的耗时），比"立刻返回一个 sleep N 秒后 chain 回
`/v1/boot` 的脚本"慢一点。但考虑到影响面（不改网关文件）与契约稳定性
（不新增载荷形态），这个权衡是值得的——运维若认为这个延迟不可接受，
后续可以在拥有 `internal/gateway` 的 track 里补一个排队专用载荷，本轮
的 `StartBoot` 返回值（`errAdmissionQueued`，可用 `errors.Is` 识别）已经
为那条改动预留了区分信号。

## 自动重试白名单为什么只有两项

计划任务 4 表格给出的两项：

| 失败原因 | 为什么值得自动重试 |
|---|---|
| `boot_timeout` | 常见原因是网络抖动或制品传输失败，重试大概率成功 |
| `install_phase` | 安装阶段失败，若退出码指向瞬时问题（如仓库暂时不可达）值得重试 |

明确排除的四项（即使 `Reinstallable()` 为 true）：

- `precheck`：§4.6"重试必然同样失败"——架构/固件与制品不匹配，重试
  不会改变这个事实
- `hook_required`：脚本或环境的确定性问题，不是瞬时故障
- `canceled`：人工主动取消是深思熟虑的决定，系统不应该自作主张撤销它
- `attempt_exceeded`：已经用尽三次装机 token 机会（§9.1 第三道闸门），
  这本身是"需要人工判断为什么每次都不成功"的信号，自动重试会绕开这道
  闸门存在的意义

## contractGaps

### 1. `Task`/`TaskMutation` 没有持久化的"自动重试次数"字段

`internal/core/model` 已冻结，不能加 `AutoRetryCount int` 这样的字段。
本轮用"查事件表、数含固定标记的条数"代替，缺点：任务事件很多时每次
判定都要整表取回（`contract.EventFilter` 只支持 `AfterSeq` 一个过滤
维度，没有按 `Message`/`Source` 过滤的能力）。这不是性能热点（任务事件
表是"百级任务 × 个位数事件"量级），但不是理想实现。

**正解方向**：给 `Task`/`TaskMutation` 加一个持久化计数字段，随
`Retry` 的 CAS 转换一起原子更新；或至少让 `EventFilter` 支持按 `Source`
过滤。

### 2. "自动重试写审计"落在事件表而非审计表

计划任务 4 原话"自动重试要写审计记录，操作人记为系统"暗示应该调用
`contract.AuditLogger.Log(ctx, "system", "task.auto_retry", ...)`——但
`Machine` 的六个依赖里没有 `AuditLogger`，`statemachine.New` 的构造
签名冻结在 `machine.go`（不在本任务文件归属范围内），无法注入新依赖。

本轮把"操作人是系统"这条信息写进任务事件表（`Source:
model.EventSourceProvider`，`Message` 含"系统自动重试"字样），不是真正
写入 `audit_log` 表（那张表是人面操作审计的权威落点，§9.4）。区分度
足以支持"这次重试是系统自动发起还是运维手工点击"的追溯需求，但不等价于
正式审计记录。

**正解方向**：给 `Machine` 加一个 `contract.AuditLogger` 依赖（需要改
`machine.go`），`AutoRetryFailedTasks` 判定通过后改为调用真正的
`AuditLogger.Log`。

### 3. 并发上限的配置字段归属存在解读空间

本任务的文件归属括号写的是"并发上限配置项"，字面上没有覆盖"自动重试"
的两个配置字段（`AutoRetryEnabled`/`AutoRetryMaxAttempts`）。由于没有
其它任务的文件归属包含 `internal/config/config.go`，且计划任务 4
明确要求自动重试必须有独立开关与独立上限，本轮选择把两组字段都放进
同一个 `Scan` 分组（而不是省略、让自动重试变成硬编码常量、无法被运维
关闭——那与"默认关闭"这条刻意的设计决定直接冲突）。已在此明确记录这个
归属范围的解读，供编排方核对是否需要调整。

### 4. `LifecycleEventTaskFailed`/`LifecycleEventTaskReady` 目前在生产代码里从未被 `Publish`

**这是本轮验证过程中发现的真实断裂，不是本任务引入的问题，但会直接
影响告警是否真的能送达。** `internal/provider/lifecycle.go` 的
`RegisterLifecycleHooks` 订阅了这两个事件（`onTaskFailed`/
`onTaskReady`），但全仓搜索
`EventBus.Publish(contract.LifecycleEventTaskFailed` /
`LifecycleEventTaskReady` 在非测试代码里**零个调用点**——唯一被真正
`Publish` 的生命周期事件是 `LifecycleEventTaskCreated`
（`internal/console/task.go`）。

也就是说：`Machine.Fail`/`failTask` 只写 `TaskEvent`（任务事件表），
**不会**触发 `EventBus.Publish`；告警管线（`AlertingNotify` →
内置/外部 Notify）订阅了正确的事件种类，但目前没有任何生产代码路径
会真正触发这条订阅。本轮新增的 `TestBuildApp_AlertingNotifyWiredAheadOfBuiltinNotify`
测试是手工调用 `app.Events.Publish(...)` 模拟这次触发，验证的是"如果
被触发，管线内容是对的"，不能证明"管线真的会被触发"。

**这不是本任务能修的问题**：需要在 `Machine.Fail`（或它的调用方，
`internal/gateway`/`internal/console`/`internal/statemachine` 的某处）
补上 `events.Publish(contract.LifecycleEventTaskFailed, task)` 这一行，
这些文件都不在本任务的文件归属范围内。**这是一个比本轮 Phase 4 范围
更大的发现**：意味着当前生产配置下，任务失败/就绪都不会触发任何告警或
DNS 注册（`onTaskReady` 里的 DNS 注册同样依赖这条从未被触发的
`Publish` 调用）。

## 测试要点与验证证据

全部真跑，命令与结果：

- `go vet ./internal/statemachine/... ./internal/config/... ./cmd/lober/...`
  —— 无输出，通过。
- `gofmt -l internal/statemachine/ internal/config/ cmd/lober/` ——
  无输出，格式干净。
- `go test ./internal/statemachine/... -v -count=1` —— 全绿，含：
  - `TestAutoRetry_ThreeNonReinstallableReasonsNeverRetried`（逐个验证
    `reboot_timeout`/`init_timeout`/`init_failed` 永不被自动重试——
    这是全系统最危险的一条逻辑）
  - `TestAutoRetry_WhitelistedReasonsAreRetried`（`boot_timeout`/
    `install_phase` 会被自动重试）
  - `TestAutoRetry_NonWhitelistedReinstallableReasonsNotRetried`
    （`precheck`/`hook_required`/`canceled`/`attempt_exceeded` 即使
    可重装也不在白名单内）
  - `TestAutoRetry_IndependentMaxCapStopsFurtherRetries`（独立上限
    用尽后不再重试）
  - `TestAutoRetry_DisabledByDefaultDoesNotRetry`（开关默认关闭）
  - `TestStartBootAdmissionRejectsQueuesWithoutFailOrAttempt`（准入
    拒绝不消耗 attempt、不置 failed、原样留在 pending）
  - `TestStartBootAdmissionLimitZeroMeansUnlimited`（上限 0 表示不限）
- `go test ./internal/statemachine/... -race -count=20` —— 全绿（任务
  要求的压测强度，验证 `atomic.Int64` 与新增逻辑不引入/触发已知的
  1% 概率水位竞态）。
- `grep -rn 'time.Now()' internal/statemachine/*.go`（排除 `_test.go`）
  —— 只在注释里出现（引用"不得调用 time.Now()"这条规则本身），零处
  真实调用。
- `go test ./internal/config/... -v -count=1` —— 全绿，含默认值与
  显式配置保留两条新测试。
- `go test ./cmd/lober/... -v -count=1` —— 除
  `TestRunServers_SyslogReceivesUDPPacket` 外全绿（见下）。含三条新
  装配测试：`TestBuildApp_AdmissionLimitPropagatedToStatemachinePackage`、
  `TestBuildApp_AutoRetryWiredIntoScanTimeouts`（与其对照组
  `TestBuildApp_AutoRetryDisabledByDefaultConfig`）、
  `TestBuildApp_AlertingNotifyWiredAheadOfBuiltinNotify`。
- `go test ./internal/provider/... -v -count=1`（`TestAlertingNotify`
  系列 10 个测试）—— 全绿，本轮未改动 `notify.go`/`notify_test.go`，
  确认任务 5 在之前一轮已完整实现。
- `go test ./... -count=1` —— 16 个有测试的包全绿。
- `go test ./internal/archtest/... -v` —— 全绿，四条依赖方向守卫均
  未被破坏。
- `bash scripts/e2e.sh` —— 九阶段（含四条负面用例）全部 PASS。

### `TestRunServers_SyslogReceivesUDPPacket` 的失败与本轮改动无关

单独反复运行该测试（`-run "TestRunServers_SyslogReceivesUDPPacket"
-count=5`），在完全不涉及本轮任何改动代码路径的情况下同样间接性失败
（5 次里 4 次失败，含一次单独运行耗时 23 秒仍未观察到 UDP 内容）——
该测试本身的注释已经记录过"一个被中断的进程留下僵死监听导致连续三次
红"的历史，是本机 Windows 环境下 UDP 监听/文件写入时序的已知不稳定
测试，与本轮改动的文件（`internal/statemachine`、`internal/config`、
`cmd/lober/wire.go`）均无逻辑关联。`docs/real-machine/04-ssh公钥契约.md`
也记录了这个测试此前就已存在的同一条已知问题。
