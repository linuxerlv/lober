# M1 契约与地基 实现计划

> **数据层迁移说明**：本文记录的是数据层仍为 SQLite 时的情况，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **本计划是 M2–M8 的串行前置，必须先完整执行完并 commit。** 它不产出任何可运行的功能，
> 只产出「所有模块共同引用的类型、接口、数据库 schema、测试替身与契约一致性测试套件」。
> M2–M8 七个模块在此之上完全并行，彼此只通过本计划定义的接口通信。
>
> **本计划描述做什么与为什么，不给代码。** 每个任务的「设计要点」是需要你在写代码时
> 亲自落实的决策，不是可以照抄的片段。遇到本计划没说清的细节，按设计要点的意图判断，
> 不要为了「像计划里写的」而牺牲正确性。

**目标：** 建立 Lober 的 Go 工程骨架与全部跨模块契约，使 M2–M8 能并行开发且集成时零接口返工。

**架构：** 三层单向依赖。`model` 是纯数据类型（无行为逻辑、无项目内依赖），`contract` 是
跨模块接口（只引用 `model`），实现包各自依赖前两者且互不可见。另有两个仅供测试的包：
`conformance` 是 Store 的可执行行为契约，`fake` 是各模块共用的测试替身。

**技术栈：** Go 1.23 · `modernc.org/sqlite`（纯 Go，无 CGO，气隙环境交叉编译友好）·
标准库 `net/http`（1.22+ ServeMux 路径参数）· `text/template` 与 `html/template` ·
`log/slog` · `gopkg.in/yaml.v3` · 标准库 `testing` + `go-cmp`

**SPEC 依据：** §1.1 分包、§2 数据模型、§3 状态机、§5.4 fact 契约、§6.1 信号点、
§7.1 provider 接口、§8 API

---

## 架构总览

### 依赖方向是并行开发的全部保障

```
model  ←  contract  ←  各模块实现（M2-M8）
  ↑                          ↓
  └──  fake / conformance（仅测试）    cmd/lober（唯一装配点）
```

三条规则，`internal/archtest` 在 CI 里强制：

| 规则 | 违反的后果 |
|---|---|
| `model` 不 import 任何项目内包 | 它是依赖图的根，一旦引用别的包，所有模块都被那个包污染 |
| `contract` 只 import `model` | 接口层带上实现依赖，模块就无法独立编译测试 |
| 实现包互不 import | 两个模块焊在一起，再也拆不开，并行开发失去意义 |

`cmd/lober`（M9）是唯一豁免的包 —— 装配点必须看见所有实现。

### 为什么契约必须先于实现定稿

七个模块并行时，各自对同一个接口的语义可能有不同假设。三个模块都实现完了才发现
`FindPendingTaskByIdentity` 的回退优先级各写各的，那时三边都得改。契约层的成本是
一次串行的前置投入，换来的是集成时不返工。

**conformance 套件是这个保障的执行机制**：Store 的行为写成可执行断言，内存实现与
SQLite 实现跑同一套。于是「集成」这件事变成机械操作 —— 加方法 → 先加断言 →
两个实现补齐 → 套件变绿。

### 文件结构

| 文件 | 职责 |
|---|---|
| `go.mod` `.gitignore` | module `github.com/lober/lober` |
| `internal/core/model/task.go` | Task、TaskState、Phase、FailureReason、TaskMutation |
| `internal/core/model/token.go` | TokenPurpose、两种 TTL、attempt 上限、应答文件状态门控集合 |
| `internal/core/model/signal.go` | SignalKind、信号序号表、信号到状态与阶段的映射 |
| `internal/core/model/budget.go` | 分阶段超时预算表与超时失败原因映射 |
| `internal/core/model/inventory.go` | Host、MachineIdentity、FirmwareMode、Arch、ArtifactSet、固件兼容矩阵 |
| `internal/core/model/authoring.go` | Hook、Template、HookRef、HookStage、hook digest |
| `internal/core/model/event.go` | TaskEvent、EventSource、EventLevel、FailureDetail |
| `internal/core/model/facts.go` | fact 键常量与契约版本 |
| `internal/core/model/render.go` | RenderContext、BootProbe、BaseURLs、MAC 归一化 |
| `internal/core/contract/store.go` | Store 窄接口族、查询过滤器、sentinel error |
| `internal/core/contract/service.go` | StateMachine、Renderer、Prechecker、Clock、EventBus 等接缝 |
| `internal/core/contract/provider.go` | 七个 provider 接口与失败语义表 |
| `internal/core/contract/conformance/store.go` | ★ Store 行为契约测试套件 |
| `internal/core/fake/clock.go` | 可控时钟 |
| `internal/core/fake/store.go` | 内存 Store，通过 conformance 套件 |
| `internal/store/sqlite/schema/001_init.sql` | 建表 SQL（内嵌） |
| `internal/store/sqlite/migrate.go` | 迁移执行器与数据库打开 |
| `internal/apierr/apierr.go` | 人面响应包裹与错误码映射 |
| `internal/config/config.go` | YAML 配置结构、默认值、启动校验 |
| `internal/archtest/deps_test.go` | 依赖方向守卫 |
| `docs/contracts/*.md` | HTTP 与文件系统契约的冻结文本 |
| `Makefile` | build / test / race / vet / check |

---

### 任务 1：Go module 与依赖方向声明

**产出文件：** `go.mod`、`.gitignore`、`internal/core/model/doc.go`、`internal/core/contract/doc.go`

**为什么需要这一步：** 依赖方向一旦被破坏，并行开发就失去意义（见上）。把约束写成包文档，
让后来改代码的人看得见，而不是只活在评审者的记忆里。

**设计要点：**

- **SQLite 驱动选 `modernc.org/sqlite` 而非 `mattn/go-sqlite3`。** 后者需要 CGO，
  气隙环境里交叉编译要准备目标平台的工具链；前者是纯 Go，`GOOS=linux go build` 即可
  产出能带进内网的二进制。代价是性能略低，对百台规模无影响。
- **两个 `doc.go` 的正文要写成约束而非介绍。** `model` 那份写明「本包不得 import
  任何项目内包」，`contract` 那份写明「修改已发布的方法签名会破坏并行开发，必须作为
  独立的契约变更任务处理」。这两句是给未来的人看的。

**实现流程：**

1. `git init`，`go mod init github.com/lober/lober`
2. 加三个直接依赖：SQLite 驱动、`go-cmp`（结构体比对，测试用）、`yaml.v3`（配置）
3. `.gitignore` 要覆盖：编译产物、SQLite 的三个文件（`.db` 与 WAL/SHM 副产物）、
   `node_modules`、前端构建产物目录 `internal/console/ui/dist`
4. 写两个包文档

**测试要锁住的行为：** 本任务无行为可测，正确性由编译与后续的 `archtest` 保证。

**验证：** `go build ./... && go vet ./...` 无输出。

**完成标志：** commit `chore: init go module and lock dependency direction`

---

### 任务 2：任务状态与失败态分类

**产出文件：** `internal/core/model/task.go`、`task_test.go`

**为什么需要这一步：** 失败态能否触发重装是全系统最重要的安全约束（SPEC §3.4）。
`rebooting` 与 `initializing` 超时的机器**已经装好、盘上可能已有数据**，重装会擦掉。
这条判定必须是类型自带的能力，而不是散落在各 handler 里的条件判断。

**数据结构：**

`TaskState` 七个取值：`pending` `booting` `installing` `rebooting` `initializing`
`done` `failed`。**无 `armed`** —— SPEC 0.4 已删除该状态（§3.2）。终态是 `done` 与 `failed`。

`FailureReason` 九个取值，按可否重装分两组：

| 可重装 | 不可重装 |
|---|---|
| `boot_timeout` 取到 token 但没走到 installer | `reboot_timeout` 已装好但起不来 |
| `attempt_exceeded` 菜单取用超过 3 次 | `init_timeout` 已就绪但初始化未完成 |
| `install_phase` pre/packages/post 超时或失败 | `init_failed` 同上 |
| `hook_required` required hook 非零退出 | |
| `precheck` §4.6 一致性校验不过 | |
| `canceled` 人工取消 | |

`Phase` 八个取值，对应超时预算的单位：`pending` `booting` `pre` `packages` `post`
`rebooting` `initializing` `done`。

`Task` 的字段分六组（详见 SPEC §2.4）：标识与创建信息、三个查找键、三项 pin 引用、
表单与决策参数、状态与并发控制、凭据与失败详情。

**设计要点：**

- **可重装判定做成白名单查表，不是黑名单。** 未列入白名单的失败原因一律不可重装 ——
  包括将来新增而忘了归类的。方向是刻意选的：误判成可重装会擦掉生产机，
  误判成不可重装只是多一次人工介入。
- **`Task` 上有 `Version` 字段用于乐观锁**（§3.6），所有状态转换走 CAS。
- **`FinalizeToken` 有独立的过期时间字段**，不复用装机 token 的：装机 token 保持紧
  （90 分钟），重启后阶段的凭据是 24 小时，两者互不牵连（§6.4）。
- **用一个「可选字段更新集」类型表达部分更新**（每个字段是指针，nil 表示不改），
  而不是给 Store 加二十个 `UpdateXxx` 方法。它是 Store 写操作的统一入参。

**测试要锁住的行为：**

- 九种失败原因的可重装判定逐一正确 —— 这是本模块唯一真正危险的逻辑
- **未知失败原因默认不可重装** —— 不锁住的话，将来新增原因时忘了归类就会默默变成
  「可重装」，而那意味着可能擦掉一台已装好的机器
- 终态与非终态的判定

**验证：** `go test ./internal/core/model/ -run 'TestFailureReason|TestTaskState' -v` 全过。

**完成标志：** commit `feat(model): task state machine types with reinstall safety classification`

---

### 任务 3：token 语义与准入常量

**产出文件：** `internal/core/model/token.go`

**为什么需要这一步：** token 是范围的分界符，也是唯一的执行许可（§0.1）。删掉 armed
之后，「有任务才签发 token、有 token 才能装机」这条协议级约束是防误装的主要防线（§3.2）。
相关常量分散在各模块里各写一份，迟早对不上。

**设计要点：**

- **两种 token 用途要能区分**（`install` 与 `finalize`）。装机 token 权限覆盖应答文件与
  全部信号；finalize token 写进目标系统，只能发重启后阶段的几个信号（§6.4）。
  Store 的 token 查询以用途为参数，用途不符等同查不到。
- **TTL 是两个常量：装机 90 分钟、finalize 24 小时。** 计时起点是首次 `/v1/boot`
  匹配成功时，不是建任务时 —— 运维可能上午建任务、下午才上电，`pending` 无 TTL（§3.3），
  而 token 的 90 分钟必须是装机窗口本身（§4.4）。
- **attempt 上限常量为 3**（§9.1 第三道闸门）。
- **应答文件的可服务状态集合是 `{booting, installing}`**（§4.4）。用状态门控而非一次性
  失效：一次性失效会直接掐死正常重试。这个集合放 model 里，网关与控制台各自引用同一份。

**测试要锁住的行为：** 常量本身无逻辑，不单独测；它们的语义由任务 15 的 conformance
套件（token 查询）与 M5 的端点测试覆盖。

**验证：** `go build ./...` 通过。

**完成标志：** 与任务 4 合并 commit。

---

### 任务 4：信号序号表 —— 「只进不退」的判据

**产出文件：** `internal/core/model/signal.go`、`signal_test.go`

**为什么需要这一步：** SPEC §3.6 要求延迟到达的 `packages-start` 不能把已 `post-done`
的任务打回去。判据是给每个信号一个单调序号，收到的序号不大于当前水位就只记事件、
不改状态。这张表是 M3（状态机）与 M5（网关）共同依赖的唯一判据。

**数据结构：** 一张表，键是信号名（§6.1 的十二个），值含三项：单调序号、
该信号推进到的状态、推进到的阶段。

十一个装机流程信号按时序给递增序号，**序号之间留间隔**（如 10、20、30…），
便于将来插入中间信号而不必改已有值。

**设计要点：**

- **`hook-report` 的序号是 0，且不改状态与阶段。** 它与阶段正交 —— 一个阶段内可来
  任意多次（有几个 hook 就有几次），永不推进水位。
- **信号名来自 URL 路径段，必须查表校验。** 表里没有的一律拒绝，不要用字符串前缀
  之类的宽松匹配。
- **另有一个集合列出 finalize token 有权发送的信号**（`booted` `init-start`
  `init-done` `ready` `hook-report`，§6.4）。M5 用它做权限判定。

**测试要锁住的行为：**

- 十一个流程信号的序号严格递增 —— 这是「只进不退」的全部基础
- `hook-report` 序号为 0 且不带状态与阶段
- 关键信号的状态跳转正确（`rebooting` → rebooting 态、`booted` → initializing 态、
  `ready` → done 态）
- **未知信号名被拒绝** —— 它是 URL 路径段，不校验就等于让机器指定自己的阶段

**验证：** `go test ./internal/core/model/ -run TestSignal -v` 全过。

**完成标志：** commit `feat(model): token semantics and signal table with monotonic watermark`

---

### 任务 5：分阶段超时预算表

**产出文件：** `internal/core/model/budget.go`、`budget_test.go`

**为什么需要这一步：** 只设总超时意味着卡在 `pre` 也要等 90 分钟才知道。分阶段预算能在
数分钟内报出，且报出的是具体阶段（§3.3）。

**数据结构：** 两张表。预算表给六个阶段各一个时长；超时原因表给六个阶段各一个失败原因。

| 阶段 | 预算 | 超时后的失败原因 |
|---|---|---|
| pending | **无** | — |
| booting | 15 min | boot_timeout |
| pre | 10 min | install_phase |
| packages | 60 min | install_phase |
| post | 20 min | install_phase |
| rebooting | 8 min | **reboot_timeout（不可重装）** |
| initializing | 15 min | **init_timeout（不可重装）** |

**设计要点：**

- **`pending` 不在预算表里，查询返回「无预算」而不是零值。** 这是刻意的：那台机器
  什么时候上电不在本系统控制范围内（§0.1），给它设超时等于替运维的排期做判断。
  用「键不存在」表达「永不超时」，比用 0 或一个哨兵值更难被误用。
- **另提供一个「有预算的阶段清单」查询**，供 Store 构造超时扫描查询时使用。

**测试要锁住的行为：**

- **`pending` 查不到预算** —— 不锁住的话，将来某次「顺手补个默认超时」会静默把所有
  长期挂着的任务判成失败
- 六个阶段的预算值精确匹配
- 六个阶段到失败原因的映射精确匹配
- **`rebooting` 与 `initializing` 的超时原因必须落在不可重装那一组** —— 这条测试把
  两张表连起来，是防止「改了超时原因映射却没想到重装后果」的唯一屏障

**验证：** `go test ./internal/core/model/ -v` 全过。

**完成标志：** commit `feat(model): per-phase timeout budgets, pending deliberately unbounded`

---

### 任务 6：清单类型与固件兼容矩阵

**产出文件：** `internal/core/model/inventory.go`、`inventory_test.go`

**为什么需要这一步：** SPEC §0.3 的固件维度决定了 §4.6 的硬拒绝能否实现。
「哪个 OS 家族能在哪种固件下装」会被 M5（引导时按实际上报值校验）与 M6（建任务时
过滤可选 OS）各引用一次 —— 两处必须同源，否则会出现「建任务时允许、引导时拒绝」
这类只能在机房里发现的不一致。

**数据结构：**

`OSFamily` 六个取值、`FirmwareMode` 三个（`bios` `uefi` `any`，其中 `any` 只有
制品集会取）、`Arch` 两个。

固件兼容矩阵：Linux 四家（rhel/debian/ubuntu/suse）BIOS 与 UEFI 都支持；
Windows 两种都支持但**需按固件导两套制品**；**ESXi 只支持 UEFI** ——
BIOS 路径要 syslinux 的 `mboot.c32` 加 iPXE 的 COMBOOT 支持，MVP 明确排除（附录 A.14）。

`Host` 是已发现主机清单的一行，除三个查找键外**必须含 `Arch` 与 `Firmware`**（§4.3 末条），
另有机架位与**上次被发现时间** —— 后者是建任务确认框的必显字段（§3.2）。

`ArtifactSet` 的 `Firmware` 字段承载固件维度：Linux 四家取 `any`（一套制品通吃），
Windows 取具体值（同一版本导两个制品集），ESXi 只有 `uefi`。
`ArtifactPaths` 中所有路径**相对于制品根目录、不含 scheme 与主机名** —— 拼 URL 是
渲染器的事（§1.5：模板中不出现字面 `http://`）。

**设计要点：**

- **把固件做成制品集的一个字段，而不是 `paths.extra` 里的分键。** 于是选集逻辑变成对
  四元组（家族、版本、架构、固件）的一次查表，`any` 参与匹配时放宽固件项即可 ——
  渲染器里不必为 Windows 写特例分支。代价是 Windows 的导入操作要做两遍，这个代价
  真实存在，不该藏在数据结构里。
- **iPXE 的 `${platform}` 与 `${buildarch}` 各有一个解析函数**，把原始字符串映射成
  枚举，无法映射时报错。取值需实机核实（附录 B），所以解析集中在一处 —— 将来发现
  新取值时补一个分支，而不是在各调用点做字符串比较。
- **制品集提供「能否发给某固件模式的机器」的判定方法**，`any` 对两种固件都返回真。

**测试要锁住的行为：**

- 九组「家族 × 固件」组合的兼容判定，**特别是 ESXi + BIOS 必须为假**
- 未知 OS 家族不被静默接受
- 制品集的固件匹配：`any` 匹配两者、指定固件只匹配自己
- iPXE 平台字符串的解析与对无法映射值的拒绝

**验证：** `go test ./internal/core/model/ -v` 全过。

**完成标志：** commit `feat(model): inventory types and firmware compatibility matrix`

---

### 任务 7：hook、模板与事件类型

**产出文件：** `internal/core/model/authoring.go`、`event.go`、`authoring_test.go`

**为什么需要这一步：** hook 正文按内容寻址（§2.2）：digest 由正文算出，改正文即新版本
新 digest，模板 pin 的是 digest，所以审批过的内容不会被偷换（§2.7）。

**设计要点：**

- **hook digest 必须与 coreutils `sha256sum` 的输出一致。** 机器侧的 `run_hook` 用
  `sha256sum -c` 校验下载到的脚本（§6.3），算法对不上会导致所有 hook 都校验失败。
  实现就是正文字节的裸 sha256 十六进制小写 —— **不加盐、不加前缀、不做任何规范化**。
- **`HookStage` 四个取值要有合法性判定方法。** 它来自用户输入（注册 hook 时填），
  必须白名单校验 —— 阶段不是排序而是能力边界（§5.2），放错阶段的 hook 根本跑不起来。
- **模板提供「取某阶段的 hook 引用、按 order 升序」的方法。** 排序收敛在这里一处，
  渲染器直接消费结果不再自己排 —— 否则 M4 与 M6（preview）会各排一遍，迟早不一致。
- **hook 的 `OSFamilies` 为空表示不限家族**，不是「不支持任何家族」。真正的校验点在
  模板保存时（§2.2）。
- **`HookRef` pin 的是 digest 不是 id**，这是 §2.3 的浮动引用防护。
- **事件类型只存日志路径与尾部片段，不存正文**（§2.5）：数百台机器每台数十 MB 日志
  入库会很难看。正文按 `task_id/phase` 落文件。
- **另有一个「失败详情」结构承载 §3.5 的六个字段**（阶段、退出码、hook digest、
  日志尾部、syslog 尾部、距上次信号多久）。`state=failed` 本身无用，这六项才是运维要看的。

**测试要锁住的行为：**

- **digest 等于 `printf '<正文>' | sha256sum` 的输出** —— 用一个具体正文与预先算好的
  64 位十六进制值比对，并验证尾部多一个空格即得到不同 digest
- 阶段合法性判定拒绝白名单外的值
- hook 的家族支持判定（含空列表表示不限）
- 模板取某阶段 hook 时按 order 升序、且不含其它阶段的条目

**验证：** `go test ./internal/core/model/ -v` 全过。

**完成标志：** commit `feat(model): hook, template and append-only event types`

---

### 任务 8：fact 命名空间 —— 对外 API，只增不删

**产出文件：** `internal/core/model/facts.go`、`facts_test.go`

**为什么需要这一步：** SPEC §5.4：一旦有数十个 hook 在用就不能改名。所以 fact 键必须是
常量、必须有契约版本号、必须有测试防止误删。

**数据结构：** 24 个键常量（清单见 SPEC §5.4），全部以 `LOBER_` 前缀；一个契约版本常量
（当前为 1）；一个包含全部键的切片；一个「某键是否属于 v1 契约」的查询函数供 hook
注册期校验使用（§7.5：把运行时意外提前为注册时错误）。

**设计要点：**

- **切片与常量都要有，不是只留切片。** 常量给渲染器按名引用（拼错会编译失败），
  切片给「无条件注入全部键」的遍历用。
- **宁可多注入，也别让 hook 去发明自己的取数方式**（§5.4）—— 一旦某个 hook 开始
  curl 控制台 API 拿数据，耦合就失控了。所以键要给全，包括机架位这类多数 hook 用不到的。

**测试要锁住的行为：**

- **一张「v1 契约承诺的键名」快照清单，与实际切片逐一比对，且长度必须相等。**
  任何删除或改名都会让测试变红 —— 这正是想要的效果。新增键时同时改两处，
  那一次「同时改」就是在提醒作者：这是对外契约变更。
- 所有键带统一前缀 —— 前缀是命名空间的全部保障，hook 作者靠它区分「服务端给的」
  与「自己定的」
- 契约版本为 1，并在测试失败信息里写明「升版本需要一个双写周期」

**验证：** `go test ./internal/core/model/ -run Fact -v` 全过。

**完成标志：** commit `feat(model): freeze v1 fact namespace contract`

---

### 任务 9：渲染上下文与 URL 基址

**产出文件：** `internal/core/model/render.go`、`render_test.go`

**为什么需要这一步：** SPEC §1.5 要求模板中不出现字面 `http://`，将来上 TLS 只改配置
不改代码。做法是所有 URL 从一个基址类型的方法拼出来。

**设计要点：**

- **基址类型提供三个拼接方法**：网关 URL、制品 URL、仓库 URL。制品与仓库都落在
  `/artifacts/` 前缀下（与 nginx 的 location 对齐，§1.4），但分成两个方法 ——
  将来仓库可能挪到独立 location（那里有 `open_file_cache` 差异）。
- **拼接必须规范化重复斜杠，同时保留结尾斜杠。** Ubuntu 的 `ds=nocloud-net;s=<url>`
  要求 URL 是目录且结尾带 `/`（§4.4），拼接函数吃掉它就会导致 cloud-init 取不到
  应答文件 —— 而这个故障的现场表现是「机器装到一半停住」，极难定位到一个斜杠。
- **MAC 归一化函数是安全关键。** iPXE 发的是小写冒号分隔，discovery 导入可能是大写或
  连字符。归一化为小写冒号分隔；**无法识别时返回空串** —— 空 MAC 在 §4.3 的双因子
  校验里会被当作「对不上」，是安全的方向（宁可多一次人工介入，不要错装）。
- **`RenderContext` 是渲染的全部输入，渲染器不查库。** 这是控制台 preview 端点能用
  假数据跑的前提，也是 golden 测试的前提。它含：任务、模板、制品集、主机、已排好序的
  hook 列表、fact 表、基址、两个 token、syslog 目标、**机器实际上报的固件模式**
  （非任务记录值 —— Windows/ESXi 菜单据此选制品键，§4.6）。
- **`RenderContext` 提供「本任务的应答文件名」方法**，按 OS 家族返回：
  rhel→`ks.cfg`、**suse→`autoinst.xml`**、ubuntu→`user-data`、debian→`preseed.cfg`、
  windows→`unattend.xml`、esxi→`boot.cfg`。

> **SUSE 用 AutoYaST，不是 kickstart。** 引导参数是 `autoyast=` 而非 `inst.ks=`，
> 格式是独立的 XML。**绝不要因为两者都被口头称作「ks」就把 SUSE 路由到 kickstart
> 渲染器** —— 语法完全不同，产出的文件安装器根本不认。

**测试要锁住的行为：**

- URL 拼接对基址与路径各自带/不带斜杠的四种组合都产出正确结果
- **结尾斜杠被保留** —— 单独一条测试，因为它对应一个极难定位的真实故障
- scheme 完全来自配置（传 `https://` 进去得到 `https://` 出来）
- MAC 归一化对三种输入格式（大写冒号、小写连字符、无分隔）产出同一结果，
  对垃圾输入返回空串
- 六个 OS 家族的应答文件名，**特别是 suse 不等于 `ks.cfg`**

**验证：** `go test ./internal/core/model/ -v` 全过。

**完成标志：** commit `feat(model): render context and scheme-agnostic URL bases`

---

### 任务 10：Store 契约

**产出文件：** `internal/core/contract/store.go`

**为什么需要这一步：** 这是 M2 的实现目标，也是 M3–M7 的数据出口。拆成窄接口族而不是
一个大接口：渲染器只需要模板查询，超时扫描只需要任务查询 —— 窄依赖让每个模块的测试
替身只实现自己用到的那几个方法。

**sentinel error 五个**，调用方用 `errors.Is` 判定而不是比较字符串：

| error | 语义与用法 |
|---|---|
| `ErrNotFound` | 查不到。**token 查询的过期与用途不符也返回它** —— 不泄露「token 存在但过期了」 |
| `ErrConflict` | CAS 失败：期望的 state 或 version 与库中不符。调用方重读后决定是否重试，**绝不能无条件覆盖** |
| `ErrDuplicateSeq` | 信号去重命中。**这不是错误路径**，是正常的重复投递（机器侧重试三次），handler 应回成功 |
| `ErrActiveTaskExists` | 同一 SN 已有非终态任务（§3.6） |
| `ErrStillReferenced` | 引用完整性拦截（§2.7）：被 active template 引用的只能停用不能删 |

**接口族与关键方法的职责：**

`TaskStore` —— 状态机唯一的持久化出口：

- **按身份查 pending 任务**：按 SN → MAC → UUID 优先级回退（§4.3），**空键跳过**。
  只匹配 `pending` 状态 —— 其它状态的任务不接受新的引导请求（§4.2）。查不到返回
  `ErrNotFound`，网关据此回 `no_task`、不建任务不记状态（§0.1）。
- **CAS 改状态**：是唯一允许改 state 的方法。以「id + 期望 state + 期望 version」为条件，
  不匹配返回 `ErrConflict`，成功时 version 自增并返回更新后的任务。
- **改运行时字段**：不涉及状态转换的字段（信号水位、attempt、token、决策参数）。
  **携带 state 时必须返回错误** —— 留一条不走 CAS 的改状态路径，迟早有人用它绕过
  「禁止重装」的判定。
- **超时扫描查询**：返回「阶段进入时间 + 预算 < 现在」的任务，**必须排除无预算的阶段**。
- **按 token 查任务**：以用途为参数，不存在、已过期、用途不符都返回 `ErrNotFound`。

`HostStore` —— **按身份查主机用与任务查询同一套回退优先级**，保证「建任务时选的主机」
与「引导时匹配的主机」判据一致。

`ArtifactSetStore` —— 状态更新与内容写回**分成两个方法**：
「改 state / manifest / 错误原因」与「写回 OSFamily 与 Paths」。后者是导入 job 完成后
才知道的（识别与抽取的产物，§2.1），登记时运维只给名字与本地路径。分开的理由是
**导入失败时只改状态、不该碰已写入的 Paths**。

`HookStore` —— **按 digest 反查正文**是渲染时的取法（task pin 的是 digest 不是 id，§2.6）。
正文不可变，改正文即新版本新 digest。

`TemplateStore` —— **取 pin 住的那一版**；版本号传非正数表示取最新。

`EventStore` —— 追加时 `(task_id, seq)` 已存在返回 `ErrDuplicateSeq`。
**seq 非正表示非信号来源的事件**（扫描循环、人工操作），由实现分配自增值、不参与去重。
查询支持「seq 大于某值」以供 SSE 用 `Last-Event-ID` 续传（§12.8），不从头重放。

`FactStore` —— 写入是**合并**而非替换（同键覆盖），跨阶段跨重启传递（§5.5）。

`Store` 组合全部，供装配点使用；**各模块应依赖窄接口，不要依赖组合接口**。
另有一个事务运行接口，**实现必须拒绝嵌套调用** —— SQLite 单写者模型下嵌套会死锁。

**设计要点：**

- **查询过滤器用结构体而非一长串参数**，便于将来加字段不改签名。
- **接口方法的参数名用描述性词而非单字母。** 单字母参数名（`f`、`s`）会和实现者常用的
  receiver 名冲突 —— 这在验证时真实发生过（`s redeclared in this block`）。
- **`q` 这类文本搜索不进 Store 接口。** 主机数是百级，控制台 handler 取回后在 Go 里
  过滤即可；给 Store 加查询参数要连带改 conformance 套件与两个实现，换来的只是几百行
  数据的遍历节省。主机数涨到万级时再把过滤下推到 SQL。

**测试要锁住的行为：** 接口本身无行为；它的语义由任务 15 的 conformance 套件定义。

**验证：** `go build ./... && go vet ./...` 通过（接口无实现也应能编译）。

**完成标志：** commit `feat(contract): store interfaces, sentinel errors and token purposes`

---

### 任务 11：服务契约 —— 模块间的接缝

**产出文件：** `internal/core/contract/service.go`

**为什么需要这一步：** 这个文件定义 M3（状态机）、M4（渲染器）与 M5（网关）、M6（控制台）
之间的全部接缝。M5 与 M6 只依赖这些接口、**不 import M3/M4 的实现包**，因此四个模块
可以完全并行。

**接口与它们的职责：**

**`Clock`** —— 存在的唯一理由是超时逻辑必须可测：分阶段超时与 token TTL 都需要在测试里
瞬时推进时间。60 分钟的 packages 预算没法真等。

**`Prechecker`** —— §4.6 的三项一致性校验，在签发 token 与下发菜单之前调用：
架构一致性、固件模式兼容、制品完整性。失败要带「哪一项」，便于给运维精确原因
（「这台机器是 BIOS，任务要装 ESXi」）。制品完整性需要检查文件是否在盘上，
用 `io/fs.FS` 注入而不是直接调 `os.Stat` —— 测试里换成内存文件系统。

**`TokenIssuer`** —— 只负责生成不可预测的随机串。持久化与校验是 Store 的事。
**分开是因为 token 必须能被状态门控**（仅 booting/installing 时可用，§4.4），
而状态在库里 —— 自包含的签名 token（JWT 之类）做不到即时状态门控。

**`StateMachine`** —— `task.state` 的唯一所有者。网关与控制台都只通过它改状态，
这是「网关与控制台是同一进程的两个 listener」的根本理由（§1.1）。八个方法：

| 方法 | 职责与关键约束 |
|---|---|
| 开始引导 | 顺序硬约束：先 precheck（不过则置 failed 且**不消耗 attempt**）→ attempt++ → 超上限则置 failed → 签 token → CAS 到 booting |
| 标记应答文件已取 | booting → installing。**幂等** —— installer 可能重复取（Ubuntu 的 cloud-init 取两个文件） |
| 应用信号 | 幂等：seq 重复、序号不大于水位、终态任务收到信号，三种情况都只记事件不改状态 |
| 签发 finalize token | 渲染 `%post` 之前调用，写进目标系统供重启后阶段用（§6.4） |
| 置失败 | 写带 §3.5 六个字段的诊断事件 |
| 取消 | 人工取消 → failed。`pending` 任务取消后即不再响应引导（§3.2 的缓解手段） |
| 重试 | 重置 attempt、换新 token → pending。**对三种不可重装的失败原因必须拒绝**（§3.4）—— 拒绝在状态机里，不是靠 UI 隐藏按钮 |
| 扫描超时 | 由 30 秒周期循环调用。返回本轮**判失败**的任务数 |

**`Renderer`** —— 纯函数：输入渲染上下文，输出文本，不查库、不碰时钟。两个方法：
渲染 iPXE 菜单、渲染应答文件。后者要校验传入的文件名等于上下文算出的文件名 ——
路径段来自机器请求，不能用它去选模板（Ubuntu 例外，额外接受 `meta-data`）。

**另有一个「无任务时的 iPXE 脚本」函数**，不是 Renderer 的方法 —— 它不需要任何上下文，
且必须在「连任务都没有」时可用。正文三行：shebang、一行 echo、`sanboot --no-describe
--drive 0x80`。**那行 sanboot 是 §10 失败模式 1 的唯一保护**：BIOS/MBR 模式下没有
`efibootmgr`，装完的机器完全依赖它回本地盘。

**`LogStore`** —— 承接「日志正文不入库」（§2.5）：按任务与阶段落文件，读尾部供失败
事件与 UI 展示。

**`AuditLogger`** —— 记人面动作（§9.4）。与任务事件分开：审计对象不止任务
（还有 hook 审批、provider 配置），且必须记操作人。

**`EventBus`** —— 生命周期通知（§7.5）。**它是 fire-and-forget 的：发布不返回错误，
回调也不返回错误。**

> **因此 EventBus 只能承载「失败了也不影响任务状态」的扩展点** —— 即 §7.4 里
> 只告警与只记录那几个（DNS、Notify、Heartbeat）。
>
> **阻塞式 provider 绝不能挂在 EventBus 上。** IPAM / TemplateResolver / DiskPlanner
> 的失败语义是阻塞建任务（§7.4：IP 没申请到不能装），而「任务已创建」事件发出时
> 任务已经落库了 —— 那时再报错为时已晚。SPEC §7.5 的图示描述的是因果关系，
> 不是实现机制。

**`TaskParamsResolver`** —— 建任务路径上的**同步**决策接口，由 M7 实现、M6 在写库
**之前**调用。它的存在理由就是 EventBus 做不到阻塞。返回错误即建任务失败，错误直接
呈现给运维。返回的参数成为任务的决策参数并被固化（§2.6：决策要固化、IO 可以实时），
因此 provider 热更新不会让同一任务前后决策不一致。

**设计要点：**

- **precheck 失败不消耗 attempt 计数**（§4.6）：这不是可重试的偶发故障，重试必然
  同样失败。消耗 attempt 只会让运维少三次真正的重试机会。
- **信号的「已接受」与「改变了状态」是两回事**，返回结构要分别表达 —— handler 据此
  决定回什么，以及是否要落日志文件。

**测试要锁住的行为：** 接口本身无行为。但这个文件里有两处可测的具体逻辑：
`no_task` 脚本必须含那行 sanboot（不含则失败模式 1 失去保护），以及 precheck 错误
能被类型断言取出并读到「哪一项不过」。

**验证：** `go build ./... && go vet ./...` 通过。

**完成标志：** commit `feat(contract): state machine, renderer, precheck and lifecycle seams`

---

### 任务 12：Provider 契约

**产出文件：** `internal/core/contract/provider.go`、`provider_test.go`

**为什么需要这一步：** SPEC §7.6 明确 MVP 不做加载器，只定接口加内置实现。
**接口边界是真正有价值的部分**，所以它属于契约层；实现属于 M7。

**核心内容是一张失败语义表**（§7.4）。统一成「失败即中止」会让一次 DNS 抖动把装好的
机器标成失败；统一成「失败即忽略」会让 IP 没申请到的机器带着空配置装上去。

| provider | 调用时机 | 失败策略 |
|---|---|---|
| IPAM | 建任务 | 阻塞建任务 —— IP 没申请到不能装 |
| TemplateResolver / DiskPlanner | 建任务 | 阻塞 —— 决策出不来无法继续 |
| Inventory | 建任务 / 清单同步 | 阻塞，但允许手工录入兜底 |
| DNS / Notify | ready 之后 | **只告警**，不影响任务状态 |
| Heartbeat | 周期轮询 | 记录探测失败，不改任务状态 |

**七个接口的职责：**

IPAM（申请与释放 IP）、DNS（注册与注销）、Inventory（按 SN 查主机）、
Notify（推生命周期事件）、Heartbeat（批量探测存活）、TemplateResolver（纯决策，
按硬件事实选模板）、DiskPlanner（纯决策，选盘）。

前五个带一个「探活」方法 —— 它是热更新的健康检查钩子（§7.3）：新插件拉起后先探活，
失败则保留旧进程、拒绝本次 reload。MVP 的内置实现直接返回成功。

**设计要点：**

- **每个接口独立版本化**（名字带 V1 后缀，§7.1）。窄且稳才能版本化；定义成
  「插件可以做任何事」则一年后没人敢改。
- **失败语义查不到时返回「未声明」而不是默认值。** 加新 provider 时必须显式声明失败
  语义 —— 给默认值会让人忘了想清楚「这个失败该不该拦住装机」。
- **两个纯决策接口不带探活方法也不做 IO**（§7.2）：这套系统会擦盘，磁盘规划函数若行为
  不完全确定则故障无法复现。
- **磁盘类型只用 by-id / by-path / WWN 标识设备，不用内核分配的名字**（§10 失败模式 5）。
- **磁盘规划结果必须带「为什么这么选」的说明** —— dry-run 端点要展示它（§8.2）。

**测试要锁住的行为：**

- 七个 provider 各自的失败策略与 §7.4 的表一致
- **未知 provider 类型查不到失败策略** —— 强制新增时显式声明

**验证：** `go test ./internal/core/contract/ -v` 全过。

**完成标志：** commit `feat(contract): provider interfaces with per-extension-point failure policy`

---

### 任务 13：数据库 schema —— 两条索引承担了安全职责

**产出文件：** `internal/store/sqlite/schema/001_init.sql`、`migrate.go`、`migrate_test.go`

**为什么需要这一步：** schema 是数据契约，所以属于本计划而非 M2。其中两条索引不是性能
优化，是安全机制。

**十张表**：`schema_version`、`hosts`、`artifact_sets`、`hooks`、`templates`、
`template_hook_refs`、`tasks`、`task_events`、`task_facts`、`audit_log`。
字段与 `model` 的类型一一对应；时间列存 Unix 秒；结构化字段（制品路径、参数、
requires、params_schema）存 JSON 文本列。

**两条安全索引：**

1. **`tasks` 上按 SN 的 partial unique index，条件是状态不在终态集合内。**
   保证同一 SN 只有一个非终态任务（§3.6、§4.7 约束 2）。没有它，网关查到同一主机的
   两个 pending 任务时**无法决定渲染哪种引导形态** —— 而两种形态可能是 `wimboot` 与
   `mboot.efi` 这种完全不同的东西。终态任务不占用这条索引，所以装完的机器可以再建新任务。
2. **`task_events` 上 `(task_id, seq)` 的唯一索引。** 这是信号去重的唯一依据（§2.5、§3.6）。

另有两条唯一索引给两种 token（条件是非空），以及若干查找与扫描索引。

**设计要点：**

- **`hosts.sn` 不能建唯一索引，只建普通索引。** 白牌机 SN 可能留空、填
  `To Be Filled By O.E.M.`、甚至整批重复（§4.3）。`mac` 可以唯一。
- **`hooks` 的主键是 (id, version)，另有 digest 的唯一索引。** 渲染时按 digest 反查。
- **`artifact_sets.firmware` 与 `hosts.arch`/`hosts.firmware` 都是必填列**（§4.3 末条）。
- **迁移执行器按文件名顺序应用未执行过的文件**，版本号取文件名前三位数字，
  所以文件必须命名为 `NNN_描述.sql`。每个文件在一个事务里应用并记录版本。
- **打开数据库时设四个 pragma**：WAL（让扫描循环的读不阻塞信号写）、外键、
  忙等超时（吸收 SQLite 单写者下的短暂竞争）、`synchronous=NORMAL`。
- **连接池上限设为 1。** SQLite 只允许一个写者，放开连接数会把锁竞争变成
  `SQLITE_BUSY` 风暴，不如在连接池这一层排队。读性能由 WAL 保证。

**测试要锁住的行为：**

- 十张表都建出来了
- **迁移可重复执行**（第二次是空操作）—— 服务重启会再跑一次
- **同一 SN 的第二个非终态任务被拒绝，但第一个进终态后可以再建** —— 这条测试保护的是
  「网关无法决定渲染哪种引导形态」那个故障
- **重复的 `(task_id, seq)` 被拒绝** —— 信号去重机制的基础

**验证：** `go test ./internal/store/sqlite/ -v` 全过。

**完成标志：** commit `feat(store): sqlite schema with safety indexes and embedded migrations`

---

### 任务 14：人面响应包裹与错误码

**产出文件：** `internal/apierr/apierr.go`、`apierr_test.go`

**为什么需要这一步：** SPEC §12.5 说明这是选型带来的约束 —— vben 的默认响应拦截器按
`code`/`data`/`successCode:0` 解包。照它做可以不改框架代码，升级上游时无冲突。
**机器面不适用** —— 那边是 iPXE 与 busybox curl，保持纯文本与裸响应（§6.2）。

**错误码十个**：0 成功；1000 内部错误、1001 请求错误、1002 未认证、1003 无权限、
1004 不存在、1005 状态冲突、1006 该主机已有进行中任务、1007 仍被引用、
1008 拒绝重装、1009 校验失败。

**设计要点：**

- **把五个 Store sentinel error 映射成面向运维的中文文案。** 错误码只给程序看，
  `message` 是直接显示给人的，所以要写成运维能据此行动的话。特别是拒绝重装那条：
  要说明「该失败状态的机器已完成安装，重装会擦除磁盘数据」，而不是干巴巴的「操作被拒绝」。
- **未识别的错误一律回通用文案。** 数据库错误里可能带连接串与用户名 ——
  内部细节记进服务端日志，不进 HTTP 响应。
- **另提供一个「业务错误」类型**供 handler 主动构造（携带 HTTP 状态码、业务码、文案），
  与 sentinel 映射走同一个输出函数。

**测试要锁住的行为：**

- 成功响应的三个字段形态与 `Content-Type`
- 五个 sentinel error 各自映射到正确的 HTTP 状态码与业务码，**且 message 非空**
  （UI 直接展示它）
- **未识别错误不泄露细节** —— 用一个含数据库用户名的错误验证响应里只有通用文案

**验证：** `go test ./internal/apierr/ -v` 全过。

**完成标志：** commit `feat(apierr): vben-compatible response envelope and error mapping`

---

### 任务 15：配置与可控时钟

**产出文件：** `internal/config/config.go`、`config_test.go`、`internal/core/fake/clock.go`

**为什么需要这一步：** 配置里最要紧的一条是 §1.4 的「`listen` 写具体 IP」。
写成 `0.0.0.0` 时**一切功能正常、测试全过**，只是装机 VLAN 上的裸机获得了访问控制台 API
的能力 —— 信任域被静默合并。这种「错了也不报错」的配置必须在启动时校验。

**配置分六组：** 网关（监听地址、两个基址 URL、syslog 监听与目标）、控制台
（监听地址、token 密钥与 TTL）、存储（三个路径）、扫描（间隔）、认证（用户列表）、
provider（四个选择项加四个参数）。

**默认值：** 扫描间隔 30 秒（§3.3）、syslog 端口 514、控制台 token TTL 12 小时、
IPAM `static`、DNS `noop`、resolver `rule-based`、磁盘策略 `largest`、
心跳模式 `tcp`、心跳间隔 60 秒。

**启动校验的四类：**

1. **两个监听地址必须是具体 IP。** 拒绝空主机部分、拒绝通配地址、拒绝域名
   （只接受 IP 字面量）。理由见上 —— Go 进程应绑回环并由 nginx 做唯一入口，
   通配地址没有正当用途。
2. **两个基址 URL 与三个存储路径非空。** 每个渲染出的 URL 都从基址来，空值会产出
   形如 `/artifacts/...` 的相对 URL，而 iPXE 对它的解析行为不确定。
3. **用户列表非空，且每个用户有名字、哈希、至少一个角色。** 空列表意味着没人能登录。
4. **provider 参数的合法组合**：心跳模式必须是 tcp/icmp/off 之一；磁盘策略必须是
   五个具名策略之一；**选了 by-model 必须同时给型号串** —— 不给的运行期表现是
   「所有机器的磁盘规划都失败」，不如启动就拒绝。

**可控时钟：** 供测试瞬时推进时间，提供「现在」、「前进一段」、「设为某时刻」三个操作，
并发安全。另给一个固定起点的构造函数，避免每个测试各写一个时间字面量。

**设计要点：**

- **心跳默认 tcp 而非 icmp。** ICMP 需要特权套接字（root / CAP_NET_RAW /
  `ping_group_range`），而心跳只是辅助通道（§6.6）—— 不值得为它给整个进程提权。
- **用户来自配置文件的 bcrypt 哈希，不做用户管理界面。** 装机系统的操作员数量是个位数
  （§9.4 的角色表）。

**测试要锁住的行为：**

- 最小配置能解析
- **三种通配监听地址形式都被拒绝**（`0.0.0.0:port`、`:port`、`[::]:port`）——
  这条对应一个「测试全过但信任域已合并」的静默故障
- 空基址被拒绝
- 默认值被正确应用（特别是心跳模式与磁盘策略）
- **by-model 不给型号串被拒绝，给了则接受**
- 未知的心跳模式与磁盘策略被拒绝

**验证：** `go test ./internal/config/ ./internal/core/... -v` 全过。

**完成标志：** commit `feat(config): config with wildcard-listen guard, plus controllable clock`

---

### 任务 16：Store 契约一致性套件 —— 「按契约集成」的执行机制

**产出文件：** `internal/core/contract/conformance/store.go`

**为什么需要这一步：** 这是本计划最重要的产出。套件是一个导出函数，接收「造一个空 Store」
的工厂，任何 Store 实现都能喂给它跑。内存实现与 SQLite 实现跑同一套断言 ——
**于是并行模块用内存实现写的测试，在换成 SQLite 后行为不会变。**

集成规则因此变得机械：新增 Store 方法 → 先往套件加断言 → 两个实现各自补齐 →
套件变绿即集成完成。

**注意它是库代码不是 `_test.go`** —— 要能被两个实现包的测试 import，所以必须独立编译。

**十三个子测试及各自守的东西：**

| 子测试 | 守什么 |
|---|---|
| CAS 正常路径与版本自增 | 状态转换的基本正确性 |
| CAS 用旧 version 失败、用错 from 状态失败 | 这是并发保护的全部内容 |
| 同一 SN 只能有一个非终态任务 | 网关无法决定渲染哪种引导形态那个故障（§4.7） |
| 终态后同一 SN 可再建任务 | 装完再重装是正常流程，不能被索引挡住 |
| 身份查找的 MAC 回退 | SN 不可信时的兜底（§4.3） |
| **空 SN 不匹配「SN 也为空」的任务** | 否则任何不报 SN 的机器都会撞上第一个空 SN 任务 |
| 只返回 pending 任务 | installing 中的任务再来一次引导请求不应重新签 token |
| 信号 seq 去重返回专用 error | 机器侧重试三次，重复投递是常态 |
| 非信号事件（seq 非正）可连续写入 | 扫描循环与人工操作的事件不参与去重 |
| 事件按「seq 大于某值」查询 | SSE 续传不从头重放（§12.8） |
| token 查询：有效、用途不符、过期、未知四种情形 | 四种都要正确，且后三种统一返回「查不到」 |
| **finalize token 活得比装机 token 长，但也会过期** | §6.4 的全部意义 |
| **改运行时字段时携带状态必须报错** | 防止绕过 CAS 与禁止重装判定 |
| **超时扫描永不返回 pending 任务** | §3.3，用「一年之后再扫」验证 |
| facts 写入是合并而非替换 | 跨阶段传递（§5.5） |
| 模板 pin 版本取到的是那一版的正文与 hook 引用 | §2.6 可复现 |
| hook 按 digest 反查，改一个字节即查不到 | §2.7 审批过的内容不会被偷换 |
| 制品导入写回：OSFamily 与 Paths 完整往返 | 含切片与 map 的 JSON 序列化容易掉字段 |
| **导入失败只改状态、不清空已写入的 Paths** | 两个方法分开的全部理由 |

**设计要点：**

- **套件要提供一个建任务的辅助函数**，所有子测试共用 —— 避免每处各写一份字段字面量，
  将来加必填字段时只改一处。
- **断言失败信息要写明「为什么这条重要」**，不只是「got X want Y」。实现者看到
  `pending task returned by timeout scan — it must never time out (§3.3)` 能立刻明白
  改哪里；看到 `got 1 want 0` 只会困惑。
- **每个子测试用独立的 Store 实例**（工厂每次返回新的），避免相互污染。

**验证：** `go build ./... && go vet ./...` 通过（此刻还没有实现可跑）。

**完成标志：** commit `feat(conformance): executable store contract suite`

---

### 任务 17：内存 Store —— 并行模块的测试夹具

**产出文件：** `internal/core/fake/store.go`、`store_test.go`

**为什么需要这一步：** M3–M8 六个模块的测试都用它。有了它，那六个模块**不需要等 M2 的
SQLite 实现**就能开工 —— 这是整个并行方案成立的关键。

**实现流程：**

1. 先写调用 conformance 套件的测试文件（它会全红）
2. 用 map 存各类实体，一把读写锁保护（状态机测试会并发 CAS）
3. 逐个接口实现让套件变绿
4. 末尾加**编译期断言**：这个类型满足组合 Store 接口与事务运行接口

**设计要点：**

- **所有返回值必须深拷贝。** 返回内部指针会让一个测试里的修改渗进另一个测试，
  而这种 bug 极难定位。含切片与 map 的字段（hook digests、参数、制品路径）要逐个拷。
- **身份查找与 partial unique index 的语义要手工模拟**：遍历时跳过空键、
  只匹配 pending、判重时只看非终态任务。这些是 SQL 里由索引保证的，内存实现要显式写。
- **改运行时字段时若携带状态就返回错误**，与 SQLite 实现行为一致 —— 这条是套件断言的。
- **事务运行在内存实现里退化为直接执行**（单进程单锁，没有中途失败需要回滚的持久化）。
  真实实现必须开事务且拒绝嵌套。
- **非信号事件用递减的负数序号**，与信号的正序号天然不冲突。

**测试要锁住的行为：** 全部由 conformance 套件定义，本任务不另写断言。

**验证：** `go test ./internal/core/fake/ -v` —— 套件下的全部子测试通过；
`go build ./...` 确认编译期断言成立。

**完成标志：** commit `feat(fake): in-memory store passing the conformance suite`

---

### 任务 18：HTTP 与文件系统契约冻结文档

**产出文件：** `docs/contracts/http-machine.md`、`http-console.md`、`filesystem.md`

**为什么需要这一步：** M5（网关）、M6（控制台）、M8（WebUI）并行开发时互相看不到对方的
代码，只能对着这三份文档写。**文档里的路径、字段名、状态码就是集成时的验收标准。**

这三份是**规格文档**，不是计划 —— 它们要写成端点表加约定说明，交付后冻结。
下面列出每份必须写清的内容。

#### `http-machine.md`（机器面，装机 VLAN）

响应一律是**纯文本裸响应**，不包裹 JSON —— 客户端是 iPXE 与 busybox curl（§6.2）。

六个端点加一个 UDP 监听（§8.1）。每个端点要写明：认证方式、成功响应形态、错误行为。
必须写清的五条约定：

1. **`/v1/boot` 始终返回 200**，即使没任务、即使校验失败。iPXE 对非 200 的处理是跳到
   `|| shell`，那会把机器停在交互式提示符上等人；正确行为是让它回本地盘或继续 PXE 轮询。
2. **`no_task` 的载荷固定是那三行 sanboot 脚本**，逐字符写进文档 —— M5 要按它断言。
3. **token 走路径段不走 query**（§4.4）：Ubuntu 的 `ds=nocloud-net;s=<url>` 要求 URL 是
   目录且结尾带 `/`，query string 会坏掉；Debian 的 `preseed/url=` 里带 `&` 在内核命令行上
   也容易出事。
4. **应答文件名按 OS 家族固定**（含 suse 的 `autoinst.xml` 不是 `ks.cfg`），
   由渲染上下文的方法统一决定，handler 不自己拼。
5. **信号端点的 `digest` query 参数**（仅 hook-report 用）：SPEC §3.5 要求失败事件带
   hook digest，但 §6.3 的参考实现没传它 —— 这是 SPEC 自身的缺口，补法是加这个参数。
   格式是 64 位十六进制小写；**不合格时忽略该参数而非拒绝整个信号** ——
   上报失败绝不中断装机（§6.3），一个畸形 digest 不该让机器装不完。

#### `http-console.md`（人面，管理网）

全部挂在 `/console/v1` 下，**响应统一包裹**（§8.2、§12.5），错误码表引用 `apierr`。

五组端点（§8.2）：认证四个、主机清单两个、制品集四个、hook 六个、模板四个、
任务七个、磁盘 dry-run 一个。

必须写清的四条约定：

1. **主机清单两个端点**（SPEC 没有正式定义，但 §12.6 的页面与 §7.4 的手工录入兜底都需要它）。
   字段名与 `model.Host` 逐一对应，不另起名字。`arch` 与 `firmware` 必填（§4.3 末条）；
   MAC 入库前归一化，非法值拒绝。**查询端点是建任务页 SN 字段的唯一数据源**
   （§4.3 硬要求：不接受自由文本）。带文本搜索参数 —— 不是便利功能：主机数超过一页而
   选不到目标机器时，运维会去想办法绕过这个约束，而它是删掉 armed 后唯一的人工确认点。
2. **列表端点的分页约定**：`data` 就是数组、不带总数，前端做客户端分页。
   `limit`/`offset` 保留但作用是防止一次拉出几万行（默认上限 1000），不是给分页器用。
   理由是规模：内网装机控制台的主机与任务都是百级，服务端分页要给每个列表查询加一个
   count 查询，换来的只是几百行数据的传输节省。**任务表历史数据稳定顶到上限时再加总数字段**，
   那是一次独立的契约变更。
3. **建任务的入参是主机清单 id，不是 SN 字符串。** 服务端据此取出 SN 与 MAC 写进任务，
   两者构成引导时的双因子。
4. **SSE 端点不套响应包裹** —— 它是事件流。要写明事件格式、`id` 取自事件序号、
   重连带 `Last-Event-ID` 续传、终态时服务端主动关流，以及三个必需的响应头
   （含 `X-Accel-Buffering: no` 作为 nginx 关缓冲的兜底）。

#### `filesystem.md`（磁盘布局）

nginx 按路径找文件、Go 按同一套路径签发 URL，**不一致即引导时 404**（§10 运维点一）。
所以这份布局是 M5（渲染 URL）、M7（导入落盘）、M9（nginx 配置）三方的共同契约。

要写清服务端布局（数据库、制品、日志三棵树）与目标机器上的布局（§5.8 的
`/var/lib/lober/`），加三条硬约束：

1. **制品目录下的路径与制品集里存的相对路径逐字符一致**
2. **hook 目录名就是正文的 sha256**，机器侧靠它做校验（§6.3）
3. **启动时校验 ready 状态制品集的文件真实存在**，别等机器来了才发现

另写明三个目录的权限（数据库与日志仅 Go 进程读写，制品需 nginx 可读）。

**完成标志：** commit `docs: freeze machine/console HTTP and filesystem contracts`

---

### 任务 19：依赖方向守卫与构建脚本

**产出文件：** `internal/archtest/deps_test.go`、`Makefile`

**为什么需要这一步：** 并行开发最容易出的错是「某模块直接 import 了另一个模块的实现」——
编译能过、测试能跑，但集成时两个模块就焊在一起了，再也拆不开。用一个测试在 CI 里拦住它。

**实现流程：**

1. 用 `go list` 取每个本项目包直接 import 的本项目包（含测试 import）
2. 断言 `model` 的项目内依赖为空
3. 断言 `contract` 的项目内依赖只有 `model`
4. 按包路径前缀把包归到各模块，断言不存在跨模块的 import
5. 反向断言装配点确实在装配（装配点还不存在时跳过该条）

**设计要点：**

- **只约束 `contract` 本身，不约束它的子包。** `conformance` 会 import `contract`，
  这是允许的。
- **装配点豁免跨模块限制** —— 它的职责就是看见所有实现。
- **Makefile 的 `race` 目标独立于 `test`。** 状态机的 CAS 与扫描循环是并发的，
  没有竞态检测的测试通过不代表并发安全。`check` 目标串起格式化、vet、两轮测试 ——
  每个任务的最后一步都应该跑它。

**测试要锁住的行为：** 三条依赖规则各一条断言，加装配点的反向确认。

**验证：** `go test ./internal/archtest/ -v` —— 此刻只有 model 与 contract 存在，
前三条应通过，装配点那条跳过。之后 `make check` 全绿。

**完成标志：** commit `test: guard dependency direction, add make check`

---

## 交付验收：M2–M8 可以开工的条件

- [ ] `make check` 全绿
- [ ] conformance 套件在内存实现上全部子测试通过
- [ ] `model` 的项目内 import 为空（由 archtest 保证）
- [ ] `docs/contracts/` 下三份文档已提交
- [ ] 每个任务一个 commit，`git log` 可见完整序列

确认后，M2–M8 七个模块可以并行开工。每个模块的计划都以「只依赖 `model` 与 `contract`」
为前提写成。

**不要在模块计划里修改本计划产出的任何接口签名。** 确有必要时，作为一次独立的
「契约变更」处理：改接口 → 改 conformance 套件 → 改内存实现 → 通知所有并行模块。
这个流程刻意做得麻烦，因为契约漂移是并行开发唯一的致命风险。

