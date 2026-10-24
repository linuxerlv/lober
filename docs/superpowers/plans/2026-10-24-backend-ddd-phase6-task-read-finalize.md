# 第六阶段：任务查询、控制、事件流与通知收尾实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 七个任务入口通过应用契约处理，清除最后39项过渡许可，并把启动组装点中剩余的业务步骤归还所属模块。

**Architecture:** 接口层解析请求、取得可信身份、转换响应和发送事件流；任务应用负责查询、诊断、冻结预览和控制编排。已有执行服务继续负责状态推进及原子保存，通知只提示补读数据库事实。通用错误说明和公开枚举经应用契约提供，组装点只连接实现、启动和停止。

**Tech Stack:** Go、Gin、PostgreSQL、Wire；现有pnpm契约检查。沿用仓库工具版本，不加第三方依赖，不改数据库结构。

**Spec:** [已确认的四层设计](../specs/2026-10-22-backend-ddd-four-layer-design.md)、[总路线](2026-10-22-backend-ddd-roadmap.md)、[第五阶段dev集成记录](../reviews/2026-10-24-ddd-phase5-dev-integration.md)。同时读取仓库AGENTS.md、`docs/contracts/backend-modules.md`的既有任务创建契约及下文全部约束；身份复查仍遵守`docs/contracts/identity-audit.md`，冻结渲染仍遵守`docs/contracts/authoring-preview.md`。

状态：待用户审阅。本文只规定实施工作，尚未修改业务代码或测试。本轮未在本机或VM运行测试。

## Global Constraints

- 保留现有业务行为、数据库约束、数据格式和对外接口。当前已存在的执行服务照常委托；未启用时仍明确返回不可用，不增加装机能力。
- 接口层不访问仓储、不调用领域判定；应用模块不导入其他应用模块的具体服务。基础设施只依赖精确的能力契约。
- 领域层没有文件、网络、数据库、当前时间和随机数操作。业务写入和审计同一事务；成功提交后才通知。
- 不引入任意数据库操作的通用事务回调，不把事务对象交给上层。数据库的锁、版本条件、唯一约束和锁内复查保持原样。
- 操作契约以Go接口、普通输入输出类型、方法旁中文说明为准；覆盖设计第6节的八项约定。不另造一份内部机器描述文件。
- 网页路由、权限、审计动作、字段、错误、空数组与null形式保持兼容；不调整登录、菜单、密码强度、会话缓存、既有超时或断言。
- 所有Go、gofmt、Wire、Node、构建和测试只在 **lober VM** 执行。本机只做源码阅读、Git操作和文档编辑。环境连接方式读取本地忽略文件 `docs/linux-test-environment.private.md`，不得提交或转述其秘密。
- 固定VM环境 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`，Bash `/bin/bash`、BusyBox `/usr/bin/busybox`。完整竞态检查保持 `go test -race -timeout=20m ./...`；不改测试参数来绕过失败。
- 本阶段分支 `codex/ddd-task-read-finalize`，起点为已验收dev的 `7c0edfdb67282ccfd25e9d2f31448cb0bb0faa2b`；工作区 `E:/src/lober/.worktree/ddd-task-read-finalize`。其他工作区、用户改动、历史证据和VM目录保留。
- 按之前方式执行：每项任务由独立实施者负责；完成后先审契约、再审代码与验证证据；最后整分支独立审查。共享文件按任务顺序修改，不并发写同一文件。本文批准后才开始实施。
- 交付模块分支。后续集成目标是用户明确的 `dev`；本次计划不代表批准合并、推送或推进test、preview、main。

## Review Focus

1. **同时有多个坏参数：** 分页、时间、主机编号、状态及固件的报错先后不能变。Task3的 `Test任务查询保留错误先后` 与 `Test任务预览固件先于查任务` 固定这个顺序。
2. **控制已提交，随后读取失败：** 不能误称回滚，也不能新增或吞掉原有通知。Task4的 `Test任务控制提交后读取失败不额外通知` 和真实保存测试覆盖。
3. **事件超过一页，提示丢失或连接失效：** 不能漏补读、反复发旧事件、泄漏订阅，或改用新令牌延长旧连接。Task5的订阅顺序测试及既有三个事件流测试覆盖。
4. **错误被包装或混合，详情为空：** 分类、优先顺序、定位信息、空数组和500保密行为保持一致。Task1的 `Test错误说明保留包装优先级与普通值` 及网页兼容测试覆盖。
5. **初始化冲突或机器重启：** 内置内容重试上限、启动顺序、持久密钥和日志地址有效期不能变。Task6的启动测试、Task7的密钥及观察测试覆盖。

---

## 1. 当前事实与范围

本计划依据上述起点源码。第五阶段已集成，不重复执行它的迁移或使用它的测试结果冒充第六阶段结果。

### 1.1 七个入口的保留契约

| 入口 | 目的和权限 | 保留的顺序与结果 |
| --- | --- | --- |
| GET `/console/v1/tasks` | 任务列表，`task:read` | 分页检查→created_from→created_to→范围→规范化host_sn→state→查询→逐项CanCancel再CanRetry。保留现有仓储排序、Next、Total及空数组。 |
| GET `/console/v1/tasks/:id` | 冻结详情及诊断，`task:read` | 按原ID查一次，再取得诊断；不读当前主机、镜像、模板或hook重新计算快照。 |
| GET `/console/v1/tasks/:id/events` | 已提交事件，`task:read` | after_seq解析→limit解析→直接ListEvents；不另加GetTask改变故障顺序。Seq大于after_seq、升序，空结果是数组。 |
| GET `/console/v1/tasks/:id/stream` | 持续补读，`task:read`，保留stream标记 | after_seq先解析，再由非空Last-Event-ID覆盖；查任务→订阅→首批200条→设置响应头；每页补读200条，5秒兜底轮询。 |
| POST `/console/v1/tasks/:id/cancel` | 委托取消，`task:cancel` | 查任务→生成可信审计意图→委托原控制能力→成功后重读→补发提示→诊断；动作仍为`task.cancel`。 |
| POST `/console/v1/tasks/:id/retry` | 委托重试，`task:retry` | 与取消相同，动作仍为`task.retry`；旧任务版本交原执行服务复查，不能自动改用新版本。 |
| POST `/console/v1/tasks/:id/preview` | 冻结快照预览，`task:read` | JSON解析→检查非空请求固件→查任务→请求固件或快照固件；都为空才用UEFI并追加`firmware`样例标签；原渲染器及问题顺序不变。 |

任务读取、事件读取和预览不写审计或业务数据。控制仍由原执行服务将状态、执行记录、事件与审计原子提交，应用不另写审计。

数据库查询的游标、排序、包含端点的时间过滤、页码默认值、上限500及无效ID行为由原仓储契约决定；不另加应用ID格式检查。应用入口先检查可信Actor；网页身份解析和每请求身份快照仍归第五阶段的Identity入口。

### 1.2 事件流和通知的精确边界

- `after_seq`非法时，即使Last-Event-ID有效也先返回400。Last-Event-ID为空不覆盖，非空必须能解析为非负int64。
- 订阅必须早于第一次读取事件。首读失败时立即退订，用普通网页错误响应，尚未发送事件流头。
- 头保持 `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`。格式保持 `id: <Seq>\nevent: task\ndata: <JSON>\n\n`，空页也执行原Flush。
- 接口层保存最初的令牌。每个发送页之前按原顺序检查context和 `Identity.StreamAuthorized`，包括空页；连接中不从新登录令牌续命。保留现有账号复查频率，不承诺撤权提交与每条网络写入原子完成。
- 页满200继续补读；短页等提示或5秒轮询；任务结束不自动关流。断开、写入失败、提示通道关闭、晚期读取失败均结束并退订；已发头后不再写JSON错误。
- `ports.Notifier`仍是唯一通知契约。容量为一、按任务隔离、合并提示、并发发布/退订与重复退订保持现状；不新增必达、持久队列、后台转发或应用计时器。
- 现有执行服务提交成功会发布；旧网页控制成功重读后还会补发一次。保留这两处提示的时机，不趁重构去重。提交成功而重读失败时，后端提示可能已发出，应用不再补发；不能声称响应失败等于数据库回滚。

### 1.3 需要同时收尾的外围职责

`console/errors.go`还识别仓储错误，`openapi.go`还直接枚举领域目录，`pagination.go/queryPagination`还调用仓储检查，`notify.go`还转发基础设施构造。它们都在本阶段范围内。

`cmd/lober/app.go`仍循环安装内置内容、重试冲突；`machine.go`仍决定机器渲染所用固件和原始文件、日志地址有效期，并实现私钥文件读写。Task6、7迁出这些实现。配置检查、发布清单取值和普通字段转换、日志、HTTP挂载、定时启动Sweep、停止和资源释放留在组装入口；不能借此保留状态判定、保存或渲染流程。

既有测试日志噪声不作为本阶段必改项。发现真实行为缺陷时先记录失败证据，分开提交修复及影响说明，不能把行为改变藏进搬迁。

## 2. 39项精确清理与历史证据

起点四表分别为 **27 / 0 / 11 / 1**，共39，RemoveIn全部为`6`。每个条目完整字段为调用包、文件、函数、目标包、符号、清除阶段。下表按共同位置分组，组内每个符号分别是一项。

| 当前表 | 文件 / 函数 | 原符号 | 数量 | 删除任务 |
| --- | --- | --- | ---: | --- |
| migrationExceptions | pagination.go / queryPagination | repository.CheckPageQuery | 1 | 3 |
| migrationExceptions | errors.go / RespondError | BatchError、ErrConflict、ErrDuplicate、ErrImmutable、ErrInvalid、ErrLastAdmin、ErrNotFound、ErrReferenced、ProblemsError、ReferencedError | 10 | 1 |
| migrationExceptions | server.go / package-init | repository.Events、repository.Tasks | 2 | 5 |
| migrationExceptions | taskcontrol.go / Problems | repository.ErrRejected、repository.ProblemsError | 2 | 4 |
| migrationExceptions | tasks.go / controlTask | repository.Tasks.GetTask | 1 | 4 |
| migrationExceptions | tasks.go / getTask | repository.Tasks.GetTask | 1 | 3 |
| migrationExceptions | tasks.go / listEvents | repository.Event、repository.Events.ListEvents | 2 | 5 |
| migrationExceptions | tasks.go / listTasks | repository.Page、repository.TaskQuery、repository.Tasks.ListTasks | 3 | 3 |
| migrationExceptions | tasks.go / previewTask | repository.Tasks.GetTask | 1 | 3 |
| migrationExceptions | tasks.go / taskStream | repository.Event、repository.Events.ListEvents、repository.Tasks.GetTask | 3 | 5 |
| migrationExceptions | tasks.go / toEventItem | repository.Event | 1 | 5 |
| consoleRuleMigrationExceptions | errors.go / RespondError | repository.ErrTemplateNotCurrent | 1 | 1 |
| consoleRuleMigrationExceptions | errors.go / StatusOfProblems | model.Code.Category | 1 | 1 |
| consoleRuleMigrationExceptions | openapi.go / definition | catalog.AllArchs、AllFirmwares、AllFormats、AllSeries、Families及model.Roles | 6 | 2 |
| consoleRuleMigrationExceptions | tasks.go / listTasks | model.NormalizeSN、model.TaskState.Valid | 2 | 3 |
| consoleRuleMigrationExceptions | tasks.go / previewTask | catalog.Firmware.Valid | 1 | 3 |
| notificationMigrationExceptions | notify.go / NewHub | notification.NewHub | 1 | 5 |

按任务串行收缩：**39→27→21→12→9→0**。Task1至5每次删除实际旧引用后，同一提交删除对应许可并更新精确剩余集；每个剩余集都由冻结39项扣除本表已完成任务的明确删除项得到，逐字段和原顺序核对；不得挪表、改名、改阶段、增加许可或放行整个包。

Task1第一次删除时就接好历史重建，不能拖到最后才修旧守卫：

- 新建仅供测试的 `phaseSixOriginalEntries`，从起点四表逐字段、原顺序冻结39项，不从正在变化的当前表反算。
- `phaseFiveBaselineEntries(table, current)`先验证current是该原始表的有序子集，再返回原始39项的对应表副本；同时证明输入不变、不能复活已删调用。
- 第五阶段的39项检查改为检查这个历史视图，保留原39项摘要和68项删除集摘要；增加当前任务的独立精确剩余集检查，不放松成“至多39项”。
- 第四阶段重建先恢复39，再补既有68，仍恢复107；再与既有154项删除集一起核对原261项。保留其四个原始摘要和全部154条记录。
- 原39项逐字段SHA256：migration `a28dab617ab35d3974dd809813bdf827338a98185361a92f3e40e210f0f6de8b`；rules `04264bbe752a287b4c36354564a36611a1822969bf9a3cf6ca6be6260742d659`；notification `ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6`；空media表 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。
- 最终当前四表为空，历史39/68/154只能供测试重建。`transitionAllows`等运行中的检查只读取当前表；不允许历史表成为许可。六个随机编号正式能力原记录不变。

## 3. 文件与职责

新增文件均是实施目标，当前尚不存在；已有文件按下列任务修改。每项任务的Files是该实施者的所有权边界，共享文件只在前一项已提交并通过审查后接手。下文签名中的tasksport、authoringport、executionport分别指对应模块的精确port子包，不能写成路径中的斜杠类型。

| 位置 | 职责 |
| --- | --- |
| application/ports/failure.go | 对共用领域错误给出普通分类和详情，不包含网页状态码 |
| application/identity/contract.go、authoring/schema_contract.go | 公开角色和目录枚举的应用读取契约，仍使用唯一领域来源 |
| application/tasks/read_contract.go、reader.go | 列表、详情和冻结预览；普通输入输出类型 |
| application/tasks/control_contract.go、controller.go | 按ID控制、可信审计意图、重读及提交后补发提示 |
| application/tasks/event_contract.go、events.go | 事件读取、打开订阅、首批读取及退订 |
| application/tasks/port/task_read.go、control.go | 只读任务/事件和原控制后端的小接口 |
| application/authoring/bootstrap_contract.go、bootstrap.go | 内置内容初始化及原冲突重试 |
| application/authoring/machine_render.go | 机器文件渲染编排，复用领域渲染和BootConfig契约 |
| application/execution/observation.go、port/observation.go | 已认证机器的日志地址有效期；通过小接口登记观察结果 |
| infrastructure/machinekeys/key.go | 持久机器密钥的原有文件与随机数操作 |
| interfaces/console/tasks.go、task_dto.go、errors.go、openapi.go | 解析、应用调用、普通值转换和网络响应 |
| cmd/lober/app.go、machine.go、wire.go、wire_gen.go | 显式构造和注入；同一后端、通知、时钟和预览能力实例 |
| archtest/task_finalize_rules_test.go、phase6_history_test.go | 终态边界、精确清理和历史不可放行证明 |

## 4. 实施任务

各任务均执行：先新增针对缺失行为的测试，在VM确认失败原因，再实现、复跑并提交。纯搬迁沿用既有测试；不为每个转发另写重复测试。若接口尚不存在，可先加入最小可编译契约或明确返回失败的实现，使测试因行为缺失而失败；不能把编译失败或夹具装载失败算作有效反例。

### Task1：统一错误说明，接好历史重建

**Files:**
- Create: `internal/application/ports/failure.go`、`failure_test.go`；`internal/archtest/phase6_history_test.go`。
- Modify: `internal/interfaces/console/errors.go`、`errors_test.go`；`internal/archtest/migration_test.go`、`transition_manifest_test.go`、`identity_history_test.go`、`identity_audit_rules_test.go`。
- Test: 既有 `internal/archtest/media_rules_test.go`、`internal/interfaces/console/errors_test.go`。

**Interfaces:**
- Consumes: 现有domain/repository错误、model.Problem、第五阶段和第四阶段历史记录。
- Produces: `ports.DescribeFailure(error) (ports.Failure, bool)`；`ports.ProblemClass([]model.Problem) ports.FailureKind`。`Failure`是普通值：Kind、Problems []model.Problem、References []model.Subject、Common []model.Problem、ByHost map[string][]model.Problem；不携带repository或网页类型。
- `FailureKind`使用具名常量：TemplateNotCurrent、LastAdmin、ReferencedDetail、BatchRejected、StoredProblems、DomainProblems、NotFound、Conflict、Duplicate、Immutable、Referenced、Invalid、ProblemConflict、ProblemRejected。这里只指分类，状态码仍在console映射。
- Produces: 第2节所述 `phaseFiveBaselineEntries` 历史视图及当前精确剩余集27项。

- [ ] **Step 1：写失败测试。** `Test错误说明保留包装优先级与普通值` 用包装错误、errors.Join和空详情固定：模板当前/最后管理员→引用详情→批量问题→仓储问题→model.Problem值→指针→普通仓储哨兵。加入 `errors.Join(repository.ErrConflict, model.Problem)`，应先按领域问题处理；仓储ProblemsError即使是state类也仍422。复制结果的切片/映射后不得改动原错误。未知错误返回false，仍由原500路径记录原错误。基本断言例如：

```go
got, ok := ports.DescribeFailure(fmt.Errorf("wrapped: %w", repository.ErrNotFound))
if !ok || got.Kind != ports.NotFound {
    t.Fatal(got, ok)
}
```
- [ ] **Step 2：VM运行** `go test -count=1 -v ./internal/application/ports ./internal/interfaces/console ./internal/archtest`，确认新增行为尚未满足；保留失败记录。
- [ ] **Step 3：实现分类和转换。** 保留RespondError前面各模块专用错误及授权错误的原顺序，然后调用DescribeFailure；该函数内保持上述完整顺序，不能先吞掉单条领域问题。ProblemClass只根据原第一条问题的类别返回冲突或拒绝，空/未知类别仍拒绝。网页状态码、中文消息、首条Common或按主机编号排序取首条消息、定位详情、空数组及null规则不变。
- [ ] **Step 4：首次删除同提交接好历史。** 复制起点39项，核对第2节摘要，删除本任务12项。新增测试证明原四表摘要、68/154历史摘要、输入不变、非法新增/调序/改字段被拒、已删条目不能经历史放行；旧第五/第四阶段检查通过历史视图继续成立。当前精确27项仍是原表有序子集。
- [ ] **Step 5：VM复跑Step2及既有错误响应、历史和完整依赖检查，全通过。** 500响应不能含 `private backend credential`，日志仍收到原错误；负例必须先完成类型检查。
- [ ] **Step 6：提交并独立审查。** `refactor(console): describe failures through application contract`。只暂存Files中的实际改动；审查错误优先级和历史是否被降格。

### Task2：接口描述只读取应用公开的枚举

**Files:**
- Create: `internal/application/authoring/schema_contract.go`、`schema_contract_test.go`。
- Modify: `internal/application/identity/contract.go`、`menus_test.go`；`internal/interfaces/console/openapi.go`、`openapi_test.go`；`internal/archtest/identity_audit_rules_test.go`、`transition_manifest_test.go`、`phase6_history_test.go`。

**Interfaces:**
- Consumes: model.Roles和catalog现有五组目录，只在各自应用模块调用。
- Produces: `identity.PublicRoles() []model.Role`；`authoring.PublicCatalog() authoring.CatalogEnums`，字段Families []catalog.Family、Formats []catalog.Format、Series []catalog.Series、Archs []catalog.Arch、Firmwares []catalog.Firmware。
- 无调用身份要求：只返回随程序发布的公开静态目录，不读数据、配置或会话；每次返回独立切片，原顺序不变。

- [ ] **Step 1：写失败测试。** `Test公开目录保持顺序与独立副本`、`Test公开角色保持顺序与独立副本` 与唯一领域来源逐项比较，修改返回切片后第二次返回不变；新接口不另存一套硬编码枚举。角色断言例如：

```go
if got := identity.PublicRoles(); !reflect.DeepEqual(got, model.Roles()) {
    t.Fatal(got)
}
```
- [ ] **Step 2：VM运行** `go test -count=1 -v ./internal/application/authoring ./internal/application/identity ./internal/interfaces/console ./internal/archtest`，核对失败来自新增契约。
- [ ] **Step 3：替换schemaBuilder中六处领域枚举调用。** 保留SourceKind的原网页枚举和所有reflect类型、引用、可选字段、顺序及75条路由描述；不生成第二套对外契约。身份类型守卫仅登记新公开只读函数，不扩大到服务构造器或其他身份操作。
- [ ] **Step 4：删除六项规则许可，当前21项。** VM复跑Step2、`pnpm --dir web check:console-types`；既有OpenAPI重复编码、角色名称、Last-Event-ID和请求schema测试通过，前端生成类型无差异。
- [ ] **Step 5：提交并独立审查。** `refactor(console): read schema enums from application contracts`。

### Task3：任务列表、详情和冻结预览经应用入口

**Files:**
- Create: `internal/application/tasks/read_contract.go`、`reader.go`、`reader_test.go`、`port/task_read.go`；`docs/contracts/task-read-control-events.md`。
- Modify: `internal/interfaces/console/tasks.go`、`task_dto.go`、`pagination.go`、`errors.go`、`harness_test.go`、`tasks_test.go`、`preview_test.go`、`wizard_test.go`；`cmd/lober/app.go`、`wire.go`、`wire_gen.go`、`assembly_test.go`；两张当前许可表及 `phase6_history_test.go`；`docs/contracts/backend-modules.md`。

**Interfaces:**
- Consumes: `port.TaskGetter.GetTask(context.Context, string) (model.Task, error)`；`port.TaskReader`嵌入TaskGetter并增加 `ListTasks(context.Context, repository.TaskQuery) (repository.Page[model.Task], error)`；现有port.TaskDiagnostics和ports.FrozenPreview。
- Produces: `tasks.Reader`的 `List(context.Context, ports.Actor, ListRequest) (TaskPage, error)`、`Get(context.Context, ports.Actor, string) (TaskChoice, error)`、`Preview(context.Context, ports.Actor, string, PreviewRequest) (PreviewResult, error)`；`NewReader(ReaderDeps) Reader`。
- `ReaderDeps`字段Tasks port.TaskReader、Diagnostics port.TaskDiagnostics、Renderer ports.FrozenPreview。Tasks/Renderer必需；Diagnostics缺省保留原空诊断。新增生产提供者 `taskReaderOf(st repository.Store, ctl console.TaskControl, renderer ports.FrozenPreview) tasks.Reader`；Task4删除旧控制契约时把ctl类型同步改为tasksport.Control。
- `ListRequest`嵌入现有PageRequest，另有HostSN、State、CreatedFrom、CreatedTo string；时间保留原文本，便于在应用内按旧顺序解析。网页taskQuery的对外字段类型不变。
- `TaskPage{Items []TaskChoice; Next string; Total *int64}`；`PreviewRequest{Firmware catalog.Firmware}`；`PreviewResult{Files []ports.PreviewFile; Sample []string; Problems []model.Problem}`。新增 `tasks.ErrBadRequest` 对应原400。新契约文档记录七条原接口、问题/空值/审计/流行为并链接Go契约；初次提交标清控制和订阅仍待Task4/5迁移，不冒称已实现。

- [ ] **Step 1：写失败测试。** `Test任务查询保留错误先后` 设置分页无效+时间无效、范围倒置+编号无效、编号无效+状态无效的组合；记录零仓储调用及先返回的原错误。`Test任务查询不能绕过权限` 覆盖无身份、禁用、强制改密和原五角色；拒绝发生在参数和仓储之前。`Test任务列表保留分页顺序与诊断` 固定查询参数、返回顺序、Next/Total、空数组及CanCancel→CanRetry。用本步计数仓储夹具构造reader，calls记录其调用，断言：

```go
_, err := reader.Get(t.Context(), ports.Actor{}, "not-an-id")
if !errors.Is(err, ports.ErrUnauthenticated) || calls != 0 {
    t.Fatal(err, calls)
}
```
- [ ] **Step 2：补预览测试。** `Test任务预览固件先于查任务` 固定无效固件+不存在任务时先400且不查仓储。`Test任务预览仅传冻结快照与样例标签` 覆盖请求覆盖、快照回退、UEFI样例、渲染问题/错误；伪造依赖记录只能收到原快照，不允许读当前对象。复用既有冻结快照及引用删除保护测试。
- [ ] **Step 3：VM运行** `go test -count=1 -v ./internal/application/tasks ./internal/interfaces/console ./cmd/lober ./internal/archtest`，确认新测试先失败。
- [ ] **Step 4：实现Reader并同时接生产和测试。** 应用先Authorize(task:read)。HTTP仍先做分页数字/混用方式的语法解析，随后只绑定原始时间、编号和状态；应用先CheckPageQuery，再按原strconv.ParseInt规则依次读时间、检查范围、NormalizeSN、State.Valid。这样不把时间语法错误提前到分页业务错误之前。预览在查任务前检查请求固件，随后按第1节调用同一个冻结渲染契约；问题经原Judge路径，不以仓储ProblemsError代替。
- [ ] **Step 5：去掉旧入口依赖。** list/get/preview不再用Deps.Tasks、TaskCtl或FrozenPreview；删除仅任务列表还在用的queryPagination，保留parseQueryPagination等网页语法函数。Deps暂时保留Tasks/Events给尚未迁移的控制和流使用；测试通过显式应用装配替换旧渲染/诊断注入，不新增生产回退。任务向导和Reader共用原诊断能力。
- [ ] **Step 6：VM生成Wire，复跑Step3。** `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober`，随后 `go tool wire diff ./cmd/lober` 使用同样两个环境值。新增 `Test任务读取和预览显式组装` 证明仓储、冻结渲染与诊断实际接入；删除九项许可，当前12项。
- [ ] **Step 7：提交并独立审查。** `refactor(tasks): route reads and frozen preview through application`。重点比较原HTTP错误顺序和对外JSON，不重写既有测试断言。

### Task4：取消与重试只通过任务控制契约

**Files:**
- Create: `internal/application/tasks/control_contract.go`、`controller.go`、`controller_test.go`、`port/control.go`。
- Modify: `internal/interfaces/console/tasks.go`、`server.go`、`errors.go`、`harness_test.go`、`tasks_test.go`、`audit_test.go`、`wizard_test.go`；`cmd/lober/app.go`、`wire.go`、`wire_gen.go`、`assembly_test.go`；`migration_test.go`、`phase6_history_test.go`；`docs/contracts/task-read-control-events.md`。
- Delete: `internal/interfaces/console/taskcontrol.go`、`cmd/lober/taskcontrol.go`。其引用必须同提交替换，不保留旧别名或转发。
- Test: `internal/application/execution/control_intent_test.go`、`service_test.go`及现有数据库执行契约。

**Interfaces:**
- Consumes: Task3的port.TaskGetter、port.TaskDiagnostics；`port.Control`嵌入TaskDiagnostics并有 `Cancel(context.Context, model.Task, ports.AuditIntent) error`、`Retry(context.Context, model.Task, ports.AuditIntent) error`。由既有execution.AuditedControl结构实现，tasks不导入execution。
- Produces: `tasks.Controller.Cancel(context.Context, ports.Actor, string) (TaskChoice, error)`、`Retry(context.Context, ports.Actor, string) (TaskChoice, error)`；`NewController(ControllerDeps) Controller`。
- `ControllerDeps{Tasks port.TaskGetter; Backend port.Control; Clock ports.Clock; Notifier ports.Notifier}`，均显式必需。`NewUnavailableControl() port.Control`返回无诊断、Cancel/Retry为 `tasks.ErrControlUnavailable` 的应用实现，供机器未启用时注入。生产提供者 `taskBackendOf(*machineBundle) tasksport.Control` 接替taskControlOf；`taskControllerOf(repository.Store, tasksport.Control, ports.Clock, ports.Notifier) tasks.Controller` 同时接入Wire。wizardOf和taskReaderOf同步消费这个后端。

- [ ] **Step 1：写失败测试。** `Test任务控制拒绝越权且不读取`、`Test任务控制不存在先于不可用`、`Test任务控制保留版本审计与顺序` 固定第一次GetTask→Clock→后端→第二次GetTask→Publish→CanCancel→CanRetry。Actor/Action由应用决定，Detail仍空，At只用同一注入时钟；请求无法伪造审计。
- [ ] **Step 2：写失败/部分成功测试。** `Test任务控制提交后读取失败不额外通知` 让后端模拟已提交并发布、第二次读取失败：返回原错误，保留一次后端提示，无额外发布，不声称回滚。读取失败夹具中readErr为预置错误，分别记录后端及应用发布次数，断言：

```go
if !errors.Is(err, readErr) || backendPublishes != 1 || appPublishes != 0 {
    t.Fatal(err, backendPublishes, appPublishes)
}
```

后端失败、版本冲突、不可用、审计失败均不触发应用重读/补发；成功时即使已有后端提示仍补发，context在提交后取消也不新增提前返回点。
- [ ] **Step 3：VM运行** `go test -count=1 -v ./internal/application/tasks ./internal/application/execution ./internal/interfaces/console ./cmd/lober ./internal/archtest`，记录有效失败。
- [ ] **Step 4：实现并接线。** 应用先Authorize原cancel/retry权限，查原任务后生成AuditIntent，委托原AuditedControl；不在任务应用复制状态机判定、CAS版本比较、凭据撤销或保存逻辑。原Service继续锁内复查并原子保存。网页只传actor/id并转换结果；ErrControlUnavailable仍422及原消息。
- [ ] **Step 5：删除旧TaskControl/Problems和auditIntentFrom。** 原Problems只在测试使用，改用测试中真实的repository.ProblemsError表达原后端拒绝，不新增同名应用兼容入口。Deps.TaskCtl删除，向导诊断、Reader和Controller后端来自同一实例。生产fallback迁入应用；不实现任何新状态转移。
- [ ] **Step 6：VM重新生成Wire，复跑Step3。** 复用 `Test审计意图桥接保留真实保存与冲突`、`TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic`，证明真实数据库失败不多写状态/事件/审计；保留同主机已有未结束任务的拒绝。补组装用例覆盖机器启用/未启用及重复提示。删除三项许可，当前9项。
- [ ] **Step 7：提交并独立审查。** `refactor(tasks): orchestrate audited task control in application`。

### Task5：事件读取与订阅经应用，删除通知兼容入口

**Files:**
- Create: `internal/application/tasks/event_contract.go`、`events.go`、`events_test.go`。
- Modify: `internal/application/tasks/port/task_read.go`；`internal/interfaces/console/tasks.go`、`server.go`、`harness_test.go`、`tasks_test.go`及所有引用旧通知入口的测试；`cmd/lober/app.go`、`machine.go`、`main.go`、`wire.go`、`wire_gen.go`、`assembly_test.go`、`machine_test.go`、`machine_delivery_test.go`、`access_integration_test.go`；`migration_test.go`、`transition_manifest_test.go`、`phase6_history_test.go`、相关过渡反例；`docs/contracts/task-read-control-events.md`。
- Delete: `internal/interfaces/console/notify.go`。原别名和NewHub均删除。
- Test: `internal/infrastructure/notification/hub_test.go`、现有任务事件流测试。

**Interfaces:**
- Consumes: TaskGetter；新增 `port.EventReader.ListEvents(context.Context, string, int64, int) ([]repository.Event, error)`；现有ports.Notifier。
- Produces: `tasks.EventReader.List(context.Context, ports.Actor, string, int64, int) ([]Event, error)`、`Open(context.Context, ports.Actor, string, int64) (EventSubscription, error)`；`NewEventReader(EventDeps) EventReader`，`EventDeps{Tasks port.TaskGetter; Events port.EventReader; Notifier ports.Notifier}`。提供者 `taskEventsOf(repository.Store, ports.Notifier) tasks.EventReader` 加入Wire，复用现有唯一Hub。
- `tasks.Event{TaskID string; Seq, At int64; Kind, Key string; Fields map[string]string}`，不别名仓储类型；Fields独立复制，网页仍转为原空对象。
- `EventSubscription`方法：`Initial() []Event`、`Hints() <-chan struct{}`、`Read(context.Context, int64) ([]Event, error)`、`Close()`。Open先Authorize(task:read)，再拒绝负after_seq，随后按GetTask→订阅→首读的顺序返回首批；Read固定200，普通List保留原仓储查询校验。Close安全重复，应用不启动goroutine或计时器。

- [ ] **Step 1：写失败测试。** `Test任务事件直接查询不额外查任务` 固定权限→原ListEvents，无额外GetTask。`Test事件订阅先订阅再首读且失败退订` 记录GetTask→Subscribe→ListEvents(200)；在订阅与首读之间提交事件应可被首读/提示覆盖，首读失败只退订一次。为该失败夹具记录calls，断言：

```go
want := []string{"get", "subscribe", "list:200", "close"}
if !reflect.DeepEqual(calls, want) {
    t.Fatal(calls)
}
```

`Test事件订阅保留字段副本与上下文` 覆盖独立映射、空结果、原context、重复Close及订阅后读失败。
- [ ] **Step 2：VM运行** `go test -count=1 -v ./internal/application/tasks ./internal/interfaces/console ./internal/infrastructure/notification ./cmd/lober ./internal/archtest`，确认新增行为先失败。
- [ ] **Step 3：接应用数据能力，保留接口网络循环。** HTTP负责Last-Event-ID覆盖、5秒ticker、Flush、写入及最初令牌复查；应用负责Get/Subscribe/首次读取、普通事件转换、后续读取和退订。接口对Open成功立即defer Close，发送初页前才设头；第1.2节每个退出分支必须保留。
- [ ] **Step 4：删除旧仓储/通知依赖。** Deps改为TaskReader、TaskController、TaskEvents三个应用入口；删除Tasks、Events、FrozenPreview、TaskCtl、Notifier和server.hub。Clock仍用于原请求耗时日志，类型直接写ports.Clock；cmd和测试中的console.Clock/Notifier统一改用ports，不保留别名。缺必需应用入口在构造时明确失败，不从接口层创建通知或仓储。
- [ ] **Step 5：改测试装配并验证同一实例。** 原辅助函数签名尽量保留；需单独换后端、时钟、渲染或Hub的测试，通过测试专用装配 helper重建相应应用，显式共享同一Hub和诊断对象。若需保存这些测试能力，用测试文件中的fixture及t.Cleanup；不在生产Deps留下只为测试使用的旧字段，不缓存认证结果、不减密码工作量、不放宽断言。枚举所有旧NewHub、TaskCtl、FrozenPreview及server.hub引用，逐一改接线。
- [ ] **Step 6：VM重新生成Wire，复跑Step2和事件流既有回归。** 必须实际执行 `Test任务SSE补读分页丢通知续读及断开退订`、`Test任务SSE连接期间复查禁用改密及会话过期`、`Test任务SSE退出与到期只检查原令牌`；增加HTTP测试固定首读失败无流头、坏after_seq优先、200/201/400条分页、晚期错误只断流、空页Flush、通道关闭和写入失败退订。保留通知并发发布、并发重复退订及发布与退订交错的竞态用例。
- [ ] **Step 7：删除最后九项，当前四表全零。** 改写原“原位置允许访问”的过渡反例为拒绝，并保留真实类型检查和允许的应用入口正例；同一提交加入当前四表必须为空的断言，不等Task8。历史链仍39+68+154。
- [ ] **Step 8：提交并独立审查。** `refactor(tasks): read committed events through application subscriptions`。

### Task6：内置内容初始化归编写应用

**Files:**
- Create: `internal/application/authoring/bootstrap_contract.go`、`bootstrap.go`、`bootstrap_test.go`。
- Modify: `internal/application/authoring/port/repository.go`；`internal/domain/repository/hooks.go`（仅把过时的“组装点重试”说明改成应用重试）；`cmd/lober/app.go`、`wire.go`、`wire_gen.go`、`assembly_test.go`、`bootstrap_secret_test.go`；`internal/archtest/builtin_context_test.go`、`authoring_rules_test.go`（仅同步新入口边界）。

**Interfaces:**
- Consumes: `port.BuiltinRepository.EnsureBuiltinHook(context.Context, model.Hook) error`、`EnsureBuiltinTemplate(context.Context, model.Template) error`；由现有仓储结构实现。
- Produces: `authoring.Bootstrapper.Initialise(context.Context) error`；`NewBootstrapper(port.BuiltinRepository, []model.Hook, []model.Template) Bootstrapper`。发布内容由组装点从builtin包取值注入，不由领域或应用读取文件。提供者 `builtinBootstrapperOf(repository.Store) authoring.Bootstrapper` 加入Wire；app增加此依赖，Initialise只做两个初始化入口的顺序调用。
- 契约：按原清单顺序先全部hook再模板；每个对象最多四次，只有ErrConflict重试，每次调用前检查context；非冲突或第4次错误原样返回。已成功对象保持幂等，不承诺跨全部对象的大事务，也不新增审计/通知。

- [ ] **Step 1：写失败测试。** `Test内置初始化保留顺序四次冲突与取消` 固定冲突3次后成功、冲突4次报原错误、非冲突只调用1次、已取消不调用、hook失败不启动模板；无审计/通知。对连续冲突的单对象夹具，calls记录存储调用：

```go
if calls != 4 || !errors.Is(err, repository.ErrConflict) {
    t.Fatal(calls, err)
}
```

`Test启动先内置内容后身份初始化` 记录顺序，内置失败时不创建初始管理员。
- [ ] **Step 2：VM运行** `go test -count=1 -v ./internal/application/authoring ./cmd/lober ./internal/archtest`，核对新增失败。
- [ ] **Step 3：实现并接线。** 删除cmd的ensureBuiltin及仓储循环；app只顺序委托编写Bootstrapper和原identity.Bootstrapper。保留构造时的发布清单映射，不在应用读builtin文件；不公开通用事务或任意存储操作回调。
- [ ] **Step 4：VM生成Wire并复跑Step2。** 复用 `Test装配初始管理员强制改密及内置内容` 和 `Test空库必须显式初始口令已有用户不再要求`，确保初始化失败、重启幂等和原密码要求不变；39项历史及当前0许可检查继续通过。
- [ ] **Step 5：提交并独立审查。** `refactor(authoring): own builtin initialization workflow`。

### Task7：迁出机器组装点的渲染、观察和密钥实现

**Files:**
- Create: `internal/application/authoring/machine_render.go`、`machine_render_test.go`；`internal/application/execution/observation.go`、`observation_test.go`、`port/observation.go`；`internal/infrastructure/machinekeys/key.go`、`key_test.go`。
- Modify: `internal/application/execution/service.go`、`service_test.go`；`cmd/lober/machine.go`、`machine_test.go`、`machine_delivery_test.go`；`internal/archtest/deps_test.go`、`four_layer_test.go`、`identity_audit_rules_test.go`、`machine_rules_test.go`、`execution_guard_test.go`、`task_finalize_rules_test.go`（新建对应守卫部分）。
- Test: `internal/interfaces/machine/http_test.go`、`internal/infrastructure/events/syslog_test.go`。

**Interfaces:**
- Produces: `authoring.MachineRenderer.Render(context.Context, MachineRenderRequest) ([]byte, error)`；`NewMachineRenderer(authoringport.BootConfig) MachineRenderer`。`MachineRenderRequest{Task model.Task; Kind catalog.FileKind; Slot string; Values map[catalog.SystemValue]string}`。
- Produces: `(*execution.Service).Credentials(model.Task) (string, string)`，仍按Install、Finalize顺序调用已有Token能力。
- Consumes/Produces: `executionport.IPObservationSink.ObserveIP(string, int, string, int64)`；`execution.NewIPObserver(port.IPObservationSink) func(model.Task, string)`。Collector以现有方法结构实现，无需导入应用具体服务。
- Produces: `machinekeys.LoadOrCreate(string) ([]byte, error)`，只做原私密文件操作；只注册这个精确基础设施包，不开放任意子包或扩充六个随机编号能力；新包必须加入layerOf、layers和minChecked及完整模块实际分派，精确允许原密钥实现所需的标准库，不给它仓储、接口层或应用具体服务依赖。

- [ ] **Step 1：写失败测试。** `Test机器渲染只用冻结任务且仅ESXi辅助文件读取原配置` 覆盖Execution有/无固件、ESXi辅助文件与其他文件、原始配置读取失败、领域问题和完整Values/Slot透传；只生成请求的文件，不注入预览样例。`Test机器日志观察保留原有效期` 固定Execution=nil不登记，普通IssuedAt+5400，ReachedReboot为IssuedAt+86400，Attempt/地址保持原值，复用原相加行为而不偷偷修正。sink夹具记录Observed调用的expires和attempt；IssuedAt=1000、Attempt=2且尚未重启时：

```go
if expires != 6400 || attempt != 2 {
    t.Fatal(expires, attempt)
}
```
- [ ] **Step 2：密钥失败测试。** `Test机器密钥创建重读与权限保持` 覆盖32字节、新建目录0700、文件0600（已有父目录权限不变）、非绝对路径、符号链接/非普通文件、错误长度、权限错误、已存在文件重读及并发首次创建；成功读取者必须得到同一个已落盘密钥。旧实现中竞争方可能看到尚未写完的文件而失败，本次保留该失败，不增加等待或锁来承诺所有竞争方成功。保留旧独占创建、写入/同步/关闭的错误结果，不输出密钥。新增组装反例证明cmd不再允许原machineSigningKey位置读取随机数。
- [ ] **Step 3：VM运行** `go test -count=1 -v ./internal/application/authoring ./internal/application/execution ./internal/infrastructure/machinekeys ./internal/infrastructure/events ./internal/interfaces/machine ./cmd/lober ./internal/archtest`，确认新增失败。
- [ ] **Step 4：分别迁移三处实现。** 渲染应用决定原Execution固件、仅`esxi-ks`且FileAux时读BootConfig，再调用同一domain/render；读取失败保持 `immutable boot configuration unavailable`，渲染失败/问题保持 `machine rendering rejected`。观察策略移入execution，经小接口调用Collector；机器HTTP仍只在原凭据检查成功的位置调用观察。密钥文件实现移入machinekeys，算法、路径默认值和错误遮蔽保持原样。
- [ ] **Step 5：cmd只构造和转换字段。** 原machine.Renderer闭包仅把machine.Request转普通MachineRenderRequest后委托；Credentials委托Service；Observer直接注入NewIPObserver。保留现有URL配置检查、machine.enabled关闭分支、受保护密钥默认路径、信号普通字段映射和日志、HTTP挂载、Sweep启动停止及syslog读取循环；它们不判断任务规则或写任务。
- [ ] **Step 6：VM复跑Step3与原组装/交付回归。** 明确执行 `TestEnabledMachineAssemblyPersistsProtectedKeyAndServesCanonicalRoute`、`TestEnabledMachineRejectsInvalidEndpointOrUnprotectedKey`、`TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol`。新增包接入完整扫描与精确导入规则，删除原cmd随机数例外及其允许正例，替换为拒绝；原六项编号正式能力及凭据算法白名单保持不变。此任务不应改变Wire依赖图，仍运行wire diff确认。
- [ ] **Step 7：提交并独立审查。** `refactor(machine): keep assembly free of business workflows`。

### Task8：终态守卫、原命令验收与整分支审查

**Files:**
- Create/Complete: `internal/archtest/task_finalize_rules_test.go`；`docs/superpowers/reviews/2026-10-24-ddd-phase6-evidence.md`、`2026-10-24-ddd-phase6-final-review.md`。
- Modify: `internal/archtest/deps_test.go`、`console_rules_test.go`、`console_test.go`、`application_rules_test.go`、`transition_uses_test.go`、`transition_rules_test.go`、`capability_rules_test.go`、`phase6_history_test.go`；本计划及总路线的实际进度。
- Inspect without weakening: `guard_test.go`、`cache_test.go`、`identity_rules_test.go`、`identity_audit_rules_test.go`、`media_rules_test.go`、`wire_rules_test.go`及数据库契约。

**Interfaces:**
- Consumes: Task1至7的全部契约及测试证据，当前零许可和原始39/68/154历史。
- Produces: 接入现有全模块检查的类型守卫，以及同一冻结候选树的VM验收与独立审查结论。没有新业务入口。

- [ ] **Step 1：先写有效违规样例。** `Test任务终态边界拒绝仓储和后端旁路` 覆盖别名、方法值、嵌入/本地小接口、同包新函数、辅助函数转发、指针/类型别名/复制结构；接口不能GetTask/ListTasks/ListEvents、调用Control后端、NewHub/通知Publish或直接做状态/固件判断，cmd不能重做查询、控制、渲染、内置重试或状态赋值。加入真实应用入口、纯DTO字段转换、发布清单取值、构造与启停的允许样例。每个违规样例必须可类型检查，不能以错误方法签名冒充拦截。以下完整源码应类型检查成功，再被边界守卫拒绝：

```go
package console
import (
    "context"
    "github.com/lober/lober/internal/domain/model"
)
type leaked interface {
    GetTask(context.Context, string) (model.Task, error)
}
func bypass(ctx context.Context, r leaked) {
    call := r.GetTask
    _, _ = call(ctx, "task-id")
}
```
- [ ] **Step 2：补跨层和扫描证明。** `Test任务应用只使用本模块契约` 拒绝tasks直接导入authoring/execution服务、Gin、数据库和基础设施，以及绕过小接口的文件/网络/随机数操作。`Test终态零许可且历史不能复活调用` 检查四表逐表0、原39摘要、68/154历史记录、六个编号能力、Principal/AuditIntent的值和指针公开方法集保护不退步。完整发现新增包、缓存失效和构建标签排除检查保持有效。
- [ ] **Step 3：VM运行** `go test -count=1 -v ./internal/archtest`，确认新增反例在接守卫前失败；实现检查并接同一完整模块扫描，复跑通过。禁止只检查示例或只搜索字符串。提交 `test(arch): enforce final task boundaries without migration permits`，通过独立任务审查后冻结候选源码。
- [ ] **Step 4：按第5节完整验收。** 不以定向测试代替make check，不复制前五阶段的PASS；记录源码、数据库真实执行、工具环境、跳过/缓存/未匹配情况。发现失败按原命令诊断并复跑，源码变化后刷新候选树和相关证据。
- [ ] **Step 5：独立整分支审查。** 审查者读取设计、本计划、dev起点到候选源码的全部差异、逐项审查记录和原始验证证据，重新核对错误顺序、冻结快照、提交/重读/通知、订阅竞态和零许可历史链。严重或重要问题必须关闭并复审；修复涉及代码时重新执行必要检查，最终完整验收覆盖最终源码树。
- [ ] **Step 6：提交公开交付文档。** `docs: record phase-six validation and review`。公开记录只放脱敏结果和必要摘要，不提交原日志、连接文件、密钥、运行时配置或用户文件；附最终代码/文档树关系及未验范围。保留模块分支，等待后续集成指令。

## 5. lober VM执行与验收

### 5.1 准备和证据

实施开始后才读取本地私有环境文档（原路径 `E:/src/lober/docs/linux-test-environment.private.md`，不要求它出现在已跟踪工作区）。使用新自有目录，例如 `/home/dev/lober-ddd-phase6-task-read-finalize-20261024`；存在时先确认所有权、分支和状态，不覆盖别的阶段目录。按文档同步已提交候选源码，不把私有文件或其他工作区生成物混入。

每轮记录分支、HEAD、代码树摘要、VM实际检出/索引树、已跟踪差异和普通未跟踪文件；检查忽略目录没有额外Go/JS源码参与构建。只复用忽略的测试连接材料，权限600；连接必须指向 `lober_test` 并验证真实主库非恢复模式。不得输出连接串或原配置。

重新测量VM可用资源、磁盘和所需工具；不能把旧阶段资源数当本轮数值。检查 `/bin/bash`、`/usr/bin/busybox`、Go/Wire、make、pnpm、必要镜像与脚本工具、离线模块及前端依赖能用。按私有文档现有路径使用环境，不调整密码强度、会话缓存、测试超时或断言来适配机器。

任务验证命令均在该VM目录、固定环境下执行。Wire生成和检查固定使用 `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0`。若工具或依赖不可用，记录环境阻塞并按文档修复，不把跳过报成通过。

### 5.2 原完整命令及本阶段定向命令

在固定环境 `export GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off` 后执行：

```bash
# 只清测试结果缓存，保留离线编译依赖；不能改测试本身。
go clean -testcache
make check
pnpm --dir web check:console-types

go test -count=1 -v \
  ./internal/application/ports ./internal/application/tasks \
  ./internal/application/authoring ./internal/application/identity \
  ./internal/application/execution ./internal/archtest \
  ./internal/infrastructure/notification ./internal/infrastructure/machinekeys \
  ./internal/infrastructure/events ./internal/infrastructure/persistence/pgstore \
  ./internal/interfaces/console ./internal/interfaces/machine ./cmd/lober

git diff --check
```

`make check`必须完整包括格式、vet、全模块发现、普通测试、原 `go test -race -timeout=20m ./...`、Linux amd64构建和Wire漂移检查。命令、起止时间、退出码和日志摘要由私有证据保存。定向命令跑完整包，不能用无匹配的正则冒充新增用例通过；实际清点本计划列举用例是否执行。

按原数据库契约继续验证任务/事件顺序、版本冲突、同主机未结束任务约束、失败回滚和审计原子性，以及内置初始化、机器重启凭据和引用保护。数据库测试若因连接或工具缺失跳过，阶段不能宣称已完整验收。

如前端契约输出有非预期变化，先修复兼容性；不直接重生成快照来接受变化。本阶段不改前端行为；若最终确实改到前端源码，应追加仓库规定的类型检查、相关前端测试和构建。

### 5.3 失败复跑与交付边界

保留首次失败、命令和堆栈，先定位产品、测试还是环境问题；代码和设置不变时先在同一VM复跑失败用例，再按原完整命令验收。不要增加竞态超时、缩小用例、减少密码工作量、吞错或依赖缓存通过。确需修复代码时单独提交，并使最终完整检查覆盖修复后的冻结树。

记录包数、顶层/子用例数、明确Skip、无测试包、缓存及未匹配情况；非详细日志不能推断隐藏的Skip为0。完整验收前后对照实际源码及生成文件，证明所测对象就是交付对象；之后只有文档提交时，明确代码未变，不把新HEAD说成重新跑过。

完成标准：七个任务入口经应用；错误说明和枚举不再直连领域判定/仓储；组装点的业务实现已移出；旧通知/控制转发删除；当前四表0且历史保护完整；原接口、事务、会话与通知行为有实际VM结果；逐任务及整分支独立审查通过。只做到清零或只跑定向测试都不算完成。

## 6. 回退和执行记录

各Task保留独立提交和对应审查记录；修复也独立提交，不重写已确认的dev历史。模块尚未集成时保持模块分支及前置dev不动。以后若获准集成且发现缺陷，按仓库流程回退第六阶段整体模块，再在模块分支修复、复验，不回退前五阶段。

后续实施证据记录到Task8列出的两个公开review文档，原日志保存在私有忽略目录。现有已验收阶段的树、摘要和原日志均保留。

## 7. 计划自查

本轮仅由计划作者自查，没有实施或测试。设计第4—9节分别落实到Task3—7的职责归属、Task1/2的公共契约、Task4的原子控制、Task5的流与通知以及Task8守卫；设计第10节落实到独立分支、VM原完整命令和后续dev集成边界。

已逐项核对七个路由、39项删除归属、共享文件串行所有权、类型生产/消费关系和五类失败风险。历史重建随首次删除接入，最终零许可随Task5接入，不推迟到终审时补救。所有验收结果在执行后填写，本文不把规划当完成。
