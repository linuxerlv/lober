# 后端四层基础与任务创建 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]` / `- [x]`) syntax for tracking.

**Goal:** 建立四层与模块契约检查，迁移装机向导五个入口，使原有业务和接口保持兼容。

**Architecture:** 先迁移纯领域与仓储契约，再迁移基础设施和网页适配目录。向导通过任务应用服务组织流程，模块边界由白名单检查。其余业务在明确登记的过渡范围内保持原行为，按总路线逐步迁移。

**Tech Stack:** 沿用 go.mod 中的 Go、Gin、GORM、PostgreSQL、Wire；不新增运行依赖，不调整前端。

**Spec:** `docs/superpowers/specs/2026-10-22-backend-ddd-four-layer-design.md`；总路线为 `docs/superpowers/plans/2026-10-22-backend-ddd-roadmap.md`。

状态：第一阶段源码任务1至5已验收，Task6文档完整检查与整分支审查已完成。检查框表示已完成的实际步骤；最后文档增量复核、精确提交树核对与交付按最终记录办理。未合入develop、推送或发布。

## Global Constraints

- “本次重构保留现有业务行为、数据库约束、数据格式和对外接口。”
- “业务对象、网页输入输出类型、数据库记录保持分离。”
- “审计与业务写入在同一事务。提交成功后才能发通知；事务失败不发通知。”
- “应用模块不能直接导入另一个应用模块的具体服务。”
- “临时兼容入口按‘调用方、允许调用的旧包、清除阶段’逐项登记。禁止通配放行整个旧目录。”
- “后端改动必须通过 `make check`。”真实数据库测试不得因缺少连接而跳过。
- 不实现新的登录、角色、主机字段或装机执行能力。保持现有五角色权限，三角色草案不在本计划中落地。
- 全部说明和新增业务注释使用中文。不能读取或打印本地数据库连接内容。

## Review Focus

1. 主机输入顺序与规范化后的键：参数必须留在正确主机上；重复主机、未知参数行继续按原契约拒绝。由任务 1、5 的测试覆盖。
2. 两个服务实例共享数据库但不共享进程锁：审批与任务提交必须遵守已定义的提交事实；不能以同实例测试代替。由任务 2 验证，缺口阻止验收。
3. 写入失败、取消或通知订阅关闭：不能发出未提交任务提示，也不能重复关闭订阅。由任务 4、5 覆盖。
4. 绕过网页层直接调用应用入口：未授权用户不能读取向导数据或创建任务。由任务 3、5 覆盖。
5. 已保存内置旧版本、最新版本及新草稿共存：选择结果与现有行为一致，已有任务的快照不随发布变化。由任务 1、3、5 覆盖。

## 文件与职责

| 原路径 | 目标路径 | 处理方式 |
| --- | --- | --- |
| `internal/core/*` | `internal/domain/*` | 包名和纯规则保留，更新导入 |
| `internal/render` | `internal/domain/render` | 纯生成逻辑连同原测试和样例搬迁 |
| `internal/store` 根目录 | `internal/domain/repository` | 按对象拆接口，保留验证、查询和错误契约；移出随机编号 |
| `internal/store/pgstore` | `internal/infrastructure/persistence/pgstore` | 原实现、迁移与契约测试一起搬迁；pgtest 保留为其测试子包 |
| `internal/builtin` | `internal/infrastructure/builtin` | 原内容和样例搬迁 |
| `internal/iso` | `internal/infrastructure/media` | 第一阶段保留包名 iso 和原行为，完整拆流程留给镜像阶段；同时搬迁 iso9660、isotest 子包 |
| `internal/console` | `internal/interfaces/console` | 第一阶段只抽向导；其余文件列入过渡清单 |
| `internal/console/notify.go` | `internal/infrastructure/notification/hub.go` | 纯通知实现，不依赖 console |
| 新增 | `internal/application/tasks/{contract.go,wizard.go,selection.go,params.go,submit.go}` | 应用输入输出契约与五个操作 |
| 新增 | `internal/application/tasks/port/{repository.go,selection.go}` | 任务流程所需的小接口 |
| 新增 | `internal/application/ports/{actor.go,clock.go,notification.go}` | 可信操作者、时间和通知约定 |
| 新增 | `internal/domain/access/permissions.go` | 从 console 迁移现有权限表，保持现有角色含义 |
| 新增 | `internal/domain/selection/builtin.go` | 根据显式发布清单判断版本可选性 |
| 新增 | `internal/infrastructure/identity/id.go` | 原 NewID 的唯一实现 |
| 修改 | `internal/archtest`、`cmd/lober`、`Makefile` | 依赖检查、组装和生成校验随目录迁移同步更新 |

本阶段不把未迁移的镜像流程标成终态基础设施。对应豁免注明将在镜像阶段移出应用流程。不得通过重命名声称已完成全项目重构。

## Task 1: 固定现有契约与迁移边界

**Files:** 新建 `docs/contracts/backend-modules.md`、`internal/archtest/migration_test.go`；修改 `internal/console/wizard_test.go`、`internal/archtest/deps_test.go`。

**Interfaces:** 复用现有 `goPackage`、`violation`、`rule`；新增 `migrationException{Caller, File, Function, Target, Symbol, RemoveIn string}` 与 `checkMigrationExceptions([]migrationException) []violation`。Caller、Target 均为完整包路径；File 为模块相对文件路径；Function 为允许调用的具体函数或方法，包级声明使用固定值 `package-init`；Symbol 为允许使用的具体目标符号。RemoveIn 只能取总路线中的阶段编号。调用方文件、函数和目标符号须同时匹配，不能放行同包其他处理函数。

- [x] 记录工作区、分支、已附加工作树和未提交文件。按 using-git-worktrees 技能建立隔离执行目录和 `codex/ddd-foundation-task-creation` 分支。当前分支是其他设计任务分支，禁止在其上直接全库搬迁。把用户已确认设计、总路线和本计划的精确版本带入新分支，不能夹带其他未提交文件。
- [x] 在 `backend-modules.md` 登记向导五条现有路由，每条写明请求字段、返回字段、权限、错误类别、问题列表、审计、分页与顺序；以 `wizard.go`、`wizard_test.go` 和现有接口测试为依据。
- [x] 补行为断言：`Test向导参数输入顺序不串主机` 使用 `SN-2,SN-1` 及不同参数值，断言快照对应正确；重复规范化键和未知输入行不产生任务或审计。复用已有夹具，不复制整套建库逻辑。
- [x] 增加 `Test迁移豁免必须精确且有清除阶段`：空调用方、空文件或函数、通配目标、空 Symbol、无效阶段均必须产生违规；完全相同的条目重复也违规；同包另一处理函数不得借用豁免。运行 `go test ./internal/archtest -run Test迁移豁免 -count=1`，先看到预期失败，再实现验证函数使其通过。
- [x] 逐项登记现有非向导处理函数允许继续访问的具体仓储符号；不对整个 console 包开口子。新增业务文件默认不在豁免中。
- [x] 运行现有向导、仓储契约及 `make check` 获取迁移前基线。失败必须记录并归因；缺测试数据库时停止数据库相关验收，不打印连接信息。
- [x] 只提交本任务文件：`test(arch): pin migration contracts and exceptions`。

## Task 2: 验证事务契约的实际保护范围

**Files:** 修改 `internal/store/pgstore/contract_tasks_test.go`、`contract_concurrency_test.go`、`internal/console/wizard_test.go`；更新 `docs/contracts/backend-modules.md`。

**Interfaces:** 保持 `store.Tasks.CreateTasks(context.Context, []model.Task, store.Audit) ([]model.Task,error)`、`Templates.ApproveTemplate` 和 `DeprecateTemplate` 的现有签名。此任务是行为审计，不暗改其业务含义。

- [x] 增加 `Test任务提交与独立实例审批的契约`：两个独立服务连接同一测试库，分别提交旧已审批版本任务和审批新版本；双方不能共享 console 的锁。用数据库锁及可观察的执行阶段协调先后，禁止依靠固定睡眠碰运气。
- [x] 分别钉住“新版本审批先提交，向导旧版本提交应被拒绝”和“任务已取得有效提交保护，审批只能在约定顺序完成”两种顺序；断言最终任务版本、任务数和审计数，不只断言响应状态。
- [x] 增加同版本废弃与提交的对应测试；复用现有并发批次用例验证开放任务唯一性、整批回滚和锁顺序。
- [x] 运行 `go test ./internal/store/pgstore ./internal/console -run 'Test任务提交与独立实例|Test契约并发|Test向导提交期间' -count=1`，记录每个用例结果。
- [x] 若失败表明旧实现只靠进程内锁，提交失败测试和缺陷说明到独立缺陷分支，不把失败测试混入可集成分支。先按 systematic-debugging 完成修复设计及独立修复计划，修复通过后再继续本计划。禁止通过放宽断言、增加进程锁或删测试继续搬迁。
- [x] 若通过，或独立修复已验收，登记仓储已有的有效保护及锁顺序；提交 `test(tasks): verify cross-instance submission contracts`。

任务 2 的缺陷分支是明确的停止条件：涉及改变提交保护时应单列修复，而非由搬迁执行者自行猜测新仓储接口。后续任务只有在此项解决后才能宣称契约完整。

## Task 3: 迁移领域及契约，先清理会越界的辅助函数

**Files:** 按文件表迁移 core、render 和 store 根文件；新建 `domain/model/user.go`、`source.go`，`domain/access/permissions.go`、`domain/selection/builtin.go`、`infrastructure/identity/id.go`；修改全部 Go 导入与 `internal/archtest`。

**Interfaces:** `model.User/Role/Source/SourceKind/SourceState/Range` 保持原字段和枚举值；`repository.Users/ISOSources` 引用 model 类型。`identity.NewID() string` 保持 128 位随机数、26 字符小写 base32。`access.AllPerms() []string`、`access.PermsOf(model.Role) []string` 保留现有返回值。

新增 `selection.Manifest{Templates []model.TemplateKey; Hooks []model.HookKey}`、`selection.CurrentBuiltin(t model.Template,hooks map[model.HookKey]model.Hook,manifest Manifest) bool`。该函数只消费输入，不导入内置内容实现。

- [x] 增加守卫反例 `Test领域拒绝随机数和外部操作`，覆盖 crypto/rand、文件、网络、时钟、网页库、数据库库及反向导入；覆盖整个 repository 子树，不只 model。
- [x] 将 `store.NewID` 移到 `infrastructure/identity` 并更新所有调用，包括 pgtest.Schema 中的测试库名称生成。`store/check.go` 目前包含 crypto/rand，不能原封不动搬进领域层。原分页编码和验证函数保持行为，使用所需的纯标准库白名单。临时给旧 pgstore/pgtest 的精确调用开放该实现，随任务 4 搬迁同步删除旧路径条目。
- [x] 迁移纯包与测试/样例；把仓储接口按主机、镜像、编写、任务、事件、审计、用户分为对应 `.go` 文件。Store 总接口只给组装和仍未迁移模块使用，新增应用服务不得消费它。
- [x] 把 User、Role、Source 等业务类型移进 model；验证函数仍可留在 repository，不能让 model 反向依赖 repository 错误类型。保持数据库字段映射和外部序列化结果。
- [x] 把权限表从 `console/perm.go` 移到 domain/access；旧权限常量用精确转发兼容，未来身份阶段清除。新增 `Test权限迁移保持五角色矩阵` 和无效角色无权限断言。
- [x] 将 `currentBuiltinSelection` 提取为 selection.CurrentBuiltin，输入改为显式 Manifest。增加 `Test内置选择拒绝旧发布但不改变冻结任务`，覆盖非内置、当前内置、过期内置和旧任务读取。
- [x] 更新 `cmd/lober` 和全仓导入。仓储路径暂从旧目录实现域接口，不创建第二套模型或复制规则实现。
- [x] 运行 `go test ./internal/domain/... ./internal/archtest` 和 `make check`，期望均通过。提交 `refactor(domain): separate rules and repository contracts`。

## Task 4: 搬迁外部实现与网页目录，接通新的组装

**Files:** 按文件表搬迁 pgstore、builtin、iso、console；新建 `application/ports/notification.go`、`infrastructure/notification/hub.go`；修改 `cmd/lober/{app.go,wire.go,wire_gen.go,logging.go,supervisor.go,deletion.go,bootconfig.go}` 及相关测试、`internal/archtest`。

**Interfaces:** `ports.Notifier` 保持 `Publish(string)`、`Subscribe(string)(<-chan struct{},func())`；`notification.NewHub() ports.Notifier`。数据库和镜像构造函数签名保持原行为，仅更新包路径和领域类型。现有 console.NewHub 可按精确符号过渡转发，组装直接改为新实现。

- [x] 先增加 `Test基础设施拒绝接口层和应用具体服务` 与 `Test未知包不能绕过四层检查`，运行守卫确认失败，再实现新的 layerOf 和白名单规则。
- [x] 用路径搬迁保留所有 `_test.go`、testdata、SQL、嵌入文件与工具资料。更新嵌入路径和测试路径；不重命名 SQL 表、列和约束。
- [x] 移动通知实现并搬迁原通知测试。增加重复退订、退订与发布并发测试，确认不会重复关闭通道；保留合并提示和补读约定。
- [x] 将旧 iso 流程作为受控过渡实现；按具体符号登记其流程职责和消除阶段。不得允许它导入新的应用服务实现。
- [x] 迁移 console 到 interfaces/console。已登记旧处理函数可继续使用仓储；向导在任务 5 结束时必须撤销全部仓储和领域判定豁免。
- [x] 更新 pgstore 的测试导入、archtest 路径常量及 Wire 检查；执行 `go tool wire ./cmd/lober` 生成组装代码，不手改生成内容。
- [x] 运行 `go test ./internal/archtest ./internal/infrastructure/... ./internal/interfaces/console ./cmd/lober` 和 `make check`。提交 `refactor(backend): establish four-layer package boundaries`。

## Task 5: 定义任务应用契约并抽取向导五个操作

**Files:** 新建 `application/tasks/{contract.go,wizard.go,selection.go,params.go,submit.go,wizard_test.go,submit_test.go}`、`application/tasks/port/{repository.go,selection.go}`、`application/ports/{actor.go,clock.go}`；修改 `interfaces/console/{wizard.go,server.go,errors.go,wizard_test.go,harness_test.go,templates.go,tasks.go,task_dto.go,lifecycle.go}`、`cmd/lober/{app.go,wire.go,wire_gen.go}`及现有架构守卫。`lifecycle.go` 只替换模板审批、废弃的两处共享锁调用。

**Interfaces:**

```go
// ports 中的类型。
type Actor struct { Username string; Role model.Role; Disabled, MustChangePassword bool }
type Clock func() int64

// tasks 中的应用输入。没有 JSON、网页绑定和数据库标记。
type PageRequest struct { Cursor string; Limit int }
type HostRequest struct { PageRequest; Q, AttrKey, AttrValue string }
type ISORequest struct { PageRequest; HostSNs []string }
type TemplateRequest struct { PageRequest; ISOID string }
type Selection struct { ISOID, TemplateID string; TemplateVersion int }
type ParamsRequest struct { Selection; HostSNs []string; Entered map[string]map[string]string }
type SubmitHost struct { SN string; Entered map[string]string }
type SubmitRequest struct { Selection; Hosts []SubmitHost }

type HostChoice struct { Host model.Host; Problems []model.Problem }
type HostPage struct { Items []HostChoice; Next string }
type ISOChoice struct { ISO model.ISO; Problems []model.Problem }
type ISOPage struct { Items []ISOChoice; Next string }
type TemplateChoice struct { Template model.Template; Hooks map[model.HookKey]model.Hook; Problems []model.Problem }
type TemplatePage struct { Items []TemplateChoice; Next string }
type HostParams struct { SN string; Fields []params.Field; Problems []model.Problem }
type ParamsResult struct { Hosts []HostParams; Common []model.Problem }
type TaskChoice struct { Task model.Task; CanCancel, CanRetry []model.Problem }

type Wizard interface {
    Hosts(context.Context, ports.Actor, HostRequest) (HostPage, error)
    ISOs(context.Context, ports.Actor, ISORequest) (ISOPage, error)
    Templates(context.Context, ports.Actor, TemplateRequest) (TemplatePage, error)
    Params(context.Context, ports.Actor, ParamsRequest) (ParamsResult, error)
    Submit(context.Context, ports.Actor, SubmitRequest) ([]TaskChoice, error)
}
func NewWizard(d WizardDeps) Wizard
```

`WizardDeps` 字段固定为 `Hosts port.Hosts; ISOs port.ISOs; Templates port.Templates; Hooks port.Hooks; Tasks port.Tasks; Manifest selection.Manifest; Clock ports.Clock; Notifier ports.Notifier; SelectionGuard port.SelectionGuard; Diagnostics port.TaskDiagnostics`。

`port.TaskDiagnostics` 只包含 `CanCancel(model.Task) []model.Problem`、`CanRetry(model.Task) []model.Problem`。注入现有任务控制对象的只读能力；为空时保持原空诊断。应用预先计算模板与任务诊断。接口层共用纯字段转换函数；未迁移入口仍可保留原计算包装，向导不得调用这些包装。

`port.Hosts` 只包含原仓储的 ListHosts/GetHosts；`ISOs` 只包含 GetISO/ListISOs；`Templates` 只包含 GetTemplate/ListTemplates/ListTemplateVersions；`Hooks` 只包含 GetHooks；`Tasks` 只包含 FindOpenTask/CreateCurrentTasks。方法签名逐字沿用任务 3 后 repository 对应方法，不嵌入全量仓储接口。任务 2 已产生独立修复契约：CurrentTaskCreator.CreateCurrentTasks 在同一事务内保护当前模板选择，ErrTemplateNotCurrent 对外映射原有 409；普通 CreateTasks 仍接受固定的旧已审批版本。应用向导只消费前者。

`port.SelectionGuard` 的方法为 `ReadLock() func()`、`WriteLock() func()`，返回释放函数。过渡实现封装现有 templateSelection 读写锁，所有向导、审批及废弃入口使用同一实例。它只能保留原进程内协调，不代替任务 2 中的数据库保护；编写模块迁移后移出旧接口层。

由 `console.NewSelectionGuard()` 返回该小接口，组装只创建一次，并同时注入 console 和应用。`console.Clock` 是 `ports.Clock` 的类型别名。保留仅注入 Clock 的非向导测试构造，不从旧仓储自动拼装向导。真实数据库并发测试中的停顿点改接应用的 Tasks 依赖，不删除屏障。

`tasks.ErrForbidden`、`tasks.ErrUnauthenticated`、`tasks.ErrPasswordChangeRequired` 为应用错误。接口层分别映射现有无权限、未登录、强制改密响应，复用现有状态和原因，不新增对外错误格式。

`tasks.ProblemsError` 只携带 `[]model.Problem`，由接口层沿用现有首问题分类决定状态；不能使用固定返回 422 的仓储问题错误替代。保留全部已有错误优先级。

- [x] 先写上述契约与中文行为注释；列出输入规范化、空批、错误优先级、事务与通知约定，沿用任务 1 基线，不替换成执行者个人偏好。
- [x] 添加 `Test应用向导不能绕过权限`，五个操作分别覆盖空操作者、停用用户、须改密码用户、无创建权限角色；断言任何仓储方法都未调用。合法管理员和运维权限按现有矩阵保留。
- [x] 添加 `Test应用提交整批失败不写入不通知`、`Test应用提交成功后才通知`、`Test应用提交取消不提前通知`。假仓储在 CreateCurrentTasks 内阻塞，断言其返回前发布数为零；返回错误或取消后仍为零；成功后每个返回任务发布一次。
- [x] 添加主机顺序、缺失主机、重复规范化编号、未知输入行、当前审批版本、新草稿和内置版本的应用测试。复用领域构造数据；不在应用测试中重新实现准入算法。
- [x] 运行 `go test ./internal/application/tasks -count=1`，确认因服务未实现而失败，再将原 wizard.go 中的数据获取和业务编排移入任务服务。领域规则继续调用已有实现。
- [x] 在应用入口检查可信 Actor 和 task:create 权限；审计动作固定 task.create，时间取注入 Clock，创建人来自 Actor。应用不接受浏览器传入的 createdBy/createdAt。
- [x] 接口层保留原请求与响应类型、路由、绑定检查和状态映射。`console.Deps` 增加 `Wizard tasks.Wizard`；五个处理函数只转换类型并调用 Wizard。批准/废弃旧入口使用共享 SelectionGuard，防止旧实例锁与应用锁分离。
- [x] 更新组装构造 tasks.NewWizard 并注入 console。移除 wizard.go 的仓储、builtin、admission、snapshot 导入及业务辅助函数；只允许为了输入输出类型引用 model、catalog、params 值类型。
- [x] 守卫增加符号级反例：即使与其他旧处理函数同包，wizard.go 调用仓储、领域判定或旧业务辅助函数也必须失败。反例包含通过 s.deps、局部变量和函数转发调用的写法；使用 go/types 解析引用，禁止只按文本查找。
- [x] 撤销全部向导豁免；运行 `go test ./internal/application/... ./internal/interfaces/console ./internal/archtest ./cmd/lober` 与 `make check`。提交 `refactor(tasks): route wizard through application contracts`。

## Task 6: 真实链路验收与阶段交付

**Files:** 新建 `docs/superpowers/reviews/2026-10-22-ddd-phase1-evidence.md`；更新 `docs/contracts/backend-modules.md`、迁移清单与本计划检查框。

**Interfaces:** 不新增业务接口。核对任务 1 固定的五个网页契约与任务 5 应用入口逐项对应。

- [x] 按收尾验证调度裁定，复用Task5同源码的真实架构全包普通139.766s/并发171.967s结果。新增包、符号转发、四层与间接调用反例均实际执行。最终文档树另由make check强制count1总守卫，不为任务名称重复全包。
- [x] 按已批准裁定复用Task5最终同源码真实执行的应用、网页、仓储和组装普通/并发检查，向导、真实仓储并发、回滚及快照通过。原单独count1组合未执行，准确命令与时间见阶段证据；若源码再改则重新验证。
- [x] 在冻结文档树ea47b2b1运行根目录 `make check`，退出0；强制count1总守卫27.542s，格式、静态检查、普通/并发测试、Linux构建和Wire通过。普通/并发测试复用有效缓存，命令与原先真实执行结果已写入阶段证据；没有保存凭据。
- [x] 检查五条路由的地址、请求响应、权限、分页、空列表、错误原因和审计结果与基线一致；确认没有数据库结构变化、前端变化及新增业务行为。
- [x] 检查旧 core、store、console、render、builtin、iso 导入路径已清除；剩余过渡职责按文件、符号和清除阶段列出。不能把其余模块仍未拆分的状态写成“后端四层全部完成”。
- [x] 完成相对起点cd398fb至固定树ea47b2b1的独立整分支审查，规格符合、质量Approved，0项严重/重要问题。唯一轻微问题是本计划旧状态，最后文档增量已修正并单列复核。提交序列及保留独立并发修复的回退办法已记录。
- [ ] 最后文档增量复核、完整检查与精确树核对完成后，只提交本阶段三份文档；准确提交编号由最终交付提供。
- [ ] 交付第一阶段结果。是否合入 develop 按用户授权与仓库流程办理，后续阶段逐份编写实施计划。

收尾证据：[第一阶段证据](../reviews/2026-10-22-ddd-phase1-evidence.md)。Task1至5依据实际完成记录与前序提交勾选；Task6已经完成的完整检查、现状核对和整分支审查如上。最后文档增量与提交/交付仍按实际结果办理，不提前勾选。

## 自审记录

- 设计第 1、3、10 节：总路线和本计划阶段范围承接，第一阶段不冒充全项目完成。
- 第 4、5、9 节：任务 1、3、4、5 覆盖目录、模块依赖与过渡豁免。
- 第 6、7 节：任务 1、2、3、5 覆盖契约来源、错误、事务、并发、审计和通知。
- 第 8 节：任务 5 完整覆盖向导五个操作；预览、任务查询和控制按总路线归后续阶段，不在此处提前声称迁移完成。
- 输入输出类型与 port 方法来源已明确。随机编号移出领域；通知实现移出接口；权限规则只保留一份。
- 任务 2 失败时有明确停止和独立修复路径，不允许用进程内锁掩盖数据库契约缺口。
