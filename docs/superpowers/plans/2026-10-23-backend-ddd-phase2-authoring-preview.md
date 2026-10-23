# 后端四层迁移第二阶段：编写与预览实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 组件（代码中称hook）和模板的编写、审批、编辑辅助及预览通过应用契约调用，清除第二阶段全部83项临时放行，保留现有网页和数据库行为。

**Architecture:** 网页只解析输入、取得可信操作者、调用应用接口、转换响应。`application/authoring` 组织流程，`domain` 保留唯一规则，原仓储继续负责事务内复查与原子审计。模板审批和任务提交共用选择锁；编写预览与任务预览共用冻结内容生成能力。

**Tech Stack:** Go 1.27.1、Gin、现有Wire、GORM与PostgreSQL；不新增依赖、数据库迁移或前端功能。

**Spec:** [已批准四层设计](../specs/2026-10-22-backend-ddd-four-layer-design.md)、[总路线第二阶段](2026-10-22-backend-ddd-roadmap.md)、[现有模块契约](../../contracts/backend-modules.md)。

状态：用户已于2026-10-23批准。六项实施、逐任务独立审查和整分支独立审查均已完成；完整检查在lober虚拟机通过。模块分支codex/ddd-authoring已按用户授权在本地合入develop，合并树通过lober虚拟机完整检查；见[集成记录](../reviews/2026-10-23-ddd-phase2-develop-integration.md)。详见[阶段证据](../reviews/2026-10-23-ddd-phase2-evidence.md)。

## Global Constraints

- 设计第1节：“本次重构保留现有业务行为、数据库约束、数据格式和对外接口。”实施基线是已经合入dev功能的develop，不能退回设计编写时的旧代码。
- 设计第5节：“应用模块不能直接导入另一个应用模块的具体服务。”共同能力通过明确接口连接。
- 设计第7节：“不引入可以执行任意数据库操作的通用事务回调，也不把数据库事务对象交给上层。”
- 设计第7节：“审计与业务写入在同一事务。提交成功后才能发通知；事务失败不发通知。”本模块原本不发通知，迁移后也不增加通知。
- 设计第9节：“已迁移模块不得再调用旧业务流程。”不能把第二阶段许可改成第六阶段来凑清零。
- 设计第10节：“后端改动必须通过 `make check`”。所有测试、构建和Wire生成均在用户指定的 `lober` 虚拟机执行；本机只处理源码、文档与Git。
- 第一阶段的真实任务控制、当前模板事务保护、五角色权限、页码分页和公开类型名称均保留。设计中旧的“执行能力尚不可用”描述不能覆盖集成基线已有的真实实现。
- 不更改内置正文、发布清单、指纹、网页字段和现有原因代码。不引入第二份独立的接口描述；现有路由和生成一致性检查继续生效。

## Review Focus

1. 两种“最新”不能混用：普通列表、候选全部版本、来源最新版本、向导当前已审批版本各保留原定义；Task 1、3、4固定例子。
2. 请求同时有多处错误时，仍先返回原先那项。例如候选模板不存在且index非法，应先返回404；Task 1、4验证。
3. 生命周期写入成功后，响应补读可能失败。这时不能声称事务已回滚，也不能自动再写一次；Task 2、3验证。
4. 任务预览必须使用已有冻结快照。新模板、内置发布变化或修改原参数，不能改变旧任务的文件；Task 5验证。
5. 取消发生在等待选择锁、数据库操作或ESXi配置读取时，不能漏释放锁或留下部分审计；Task 3、5覆盖。现有锁不支持立即取消等待，不在搬迁时承诺新的中断能力。

---

## 1. 起点、分支与本轮交付

- 起点：本地 `develop` 的 `bdc6371749a9122680a18383561567e89a05e9c2`，源码树 `e142da851e771ef6a93732d31addda9104e3af65`。
- 模块分支：`codex/ddd-authoring`，从该提交建立。当前复用已隔离的工作区；不修改主目录的用户文档，也不处理原dev工作区的网页产物。
- 第一阶段及集成的验证依据见[集成记录](../reviews/2026-10-22-ddd-develop-integration.md)。其中的通过结果属于该基线，不能冒充第二阶段验收。
- 计划已获用户批准，已沿用逐任务实施、独立审查方式完成第二阶段；本轮交付通过技术验收的模块分支。
- 实施后先在模块分支验收，再按仓库流程合入develop。推送及test、preview、main的推进不包含在本计划的实施授权中。

## 2. 文件与职责

| 路径 | 处理与职责 |
| --- | --- |
| `docs/contracts/authoring-preview.md` | Task 1新建，固定现有32条编写及预览路由的请求、返回、错误、权限与副作用 |
| `internal/application/authoring/{hooks_contract,hooks,templates_contract,templates,editor_contract,editor,preview_contract,preview,renderer}.go` | 分任务新建；前三组应用操作及冻结预览实现，禁止网页类型和数据库实现 |
| `internal/application/authoring/port/repository.go` | 新建，只声明实际需要的编写仓储与主机、镜像读取能力 |
| `internal/application/authoring/port/bootconfig.go` | 新建，声明原ESXi配置读取能力 |
| `internal/application/ports/{actor,selection,preview}.go` | 扩展可信操作者；增加共用选择锁、冻结预览的接口和值类型 |
| `internal/infrastructure/selection/{guard,guard_test}.go` | 新建，只实现共用读写锁，禁止其他业务或外部操作 |
| `internal/infrastructure/media/{bootconfig,bootconfig_test}.go` | 从组装点移入原ESXi安全读取实现和测试，不改原目录保护 |
| `internal/interfaces/console/{hooks,templates,lifecycle,slots,authoring_helpers,preview}.go` | 保留路由、输入输出与纯转换；移出仓储调用、诊断与流程 |
| `internal/interfaces/console/{server,errors,pagination,tasks,harness_test}.go` | 注入应用接口、错误映射、纯请求解析；任务预览只更换共用生成能力，原查询流程留在第六阶段 |
| `internal/application/tasks/{contract,wizard,submit}.go` 与 `port/selection.go` | 使用共用锁与操作者检查，删除原私有锁契约；不改变五个向导操作 |
| `cmd/lober/{app,wire,wire_gen,assembly_test,bootconfig,deletion_test}.go` | 注入真实能力与同一个选择锁，移动配置读取实现；Wire由虚拟机工具生成 |
| `internal/archtest/{deps,application_rules,four_layer,transition_uses,transition_manifest,migration,wizard_helpers,wizard_rules}_test.go` | 复用类型检查及调用追踪，精确登记新锁实现，逐项删除第二阶段许可 |

各组新建相邻的 `*_test.go`；Task 6另外新建 `internal/archtest/authoring_rules_test.go`。已有测试文件不随意改名，便于对照基线。

## 3. 先固定的对外契约

### 3.1 操作范围与权限

路由前缀分别为 `/console/v1/hooks` 与 `/console/v1/templates`；下表中 `V` 表示 `/:id/versions/:version`。

| 对象与入口 | 权限 | 成功结果及写入 |
| --- | --- | --- |
| hooks：GET根路径、GET `/:id/versions`、GET `V` | `hook:read` | 身份分页、升序版本列表、带诊断详情；只读 |
| hooks：POST根路径、POST `/:id/versions?from=...`、PUT `V`、DELETE `V?rev=...` | `hook:write` | 创建、复制、整份保存草稿、删除；无审计，不发通知 |
| hooks：PUT `/:id/name` | `hook:write` | 身份改名；同事务 `hook.rename`，成功data为null |
| hooks：POST `V/submit`、POST `V/validate` | `hook:write` | submit写状态和 `hook.submit`；validate只读，返回ok/problems |
| hooks：POST `V/approve`、`V/reject`、`V/deprecate` | `hook:approve` | 对应状态变更及 `hook.approve/reject/deprecate`，返回补读详情 |
| templates：前三类基础读写、版本操作与改名 | 对应 `template:read/write` | 同hooks，另有POST `/:id/derive`；派生独立草稿、无审计 |
| templates：POST `V/submit`、`V/validate`、`V/approve`、`V/reject`、`V/deprecate` | 对应 `template:write/approve` | 审计动作前缀为template；审批在事务内检查引用 |
| templates：GET `V/slots`、`V/hook-candidates`、`V/source-diff` | `template:read` | 插槽位置、候选可用性、来源差异；只读 |
| templates：POST `V/preview-params`、`V/preview` | `template:read` | 参数字段或完整安装文件；不写任务、状态、审计或通知 |

合计hooks 13条、templates 19条，共32条；Task 1按真实路由重新逐项核对数量，禁止凭本表分组漏掉入口。

五种有效角色均能读取；admin、hook-author能编写；admin、hook-approver能审批。应用入口也必须检查，不能只靠网页中间件。无可信身份、角色无效或禁用返回401；强制改密码、权限不足返回403。

### 3.2 必须保持的语义

1. 成功状态码仍为200，外层为现有code/data/message。数组空值保持 `[]`，不改成null；无来源差异仍为original/current=null、changed=false。
2. 游标模式默认100、最多500；页码模式默认第1页、每页20条，最多500，带total。保持 `page/page_size`、字段是否出现、数字溢出及混用拒绝规则。
3. 普通身份列表默认取最新版本；approved过滤才取当前已审批版本。候选游标先分页身份，再展开该身份所有版本；候选页码直接按全部版本分页，不能再展开一遍或改变total。
4. source-diff按固定DerivedFrom取original，current取该来源最大版本号，即使最新是草稿。只比较版本号设置changed，不更新派生副本，也不套用向导的内置发布选择规则。
5. 保存草稿允许内容暂不完整；返回诊断，不把保存改成审批。旧Rev、非法状态转换、内置只读、引用保护、版本号不重发继续由原仓储保证。
6. Name通过独立身份改名入口修改。草稿请求中的非空Name与当前名字不符，仍返回原400。作者、审批人、状态、内置标记与DerivedFrom不能由请求伪造或被整份保存覆盖。
7. 仓储ProblemsError仍按现有映射返回422；普通ErrInvalid为400；不存在404；冲突、重复、不可变、被引用409。预览领域问题仍由首项类别映射409或422，不按错误文字判断。
8. 驳回理由保留原文，拒绝空白、不可存文本及超出原4096字节审计限制的内容；不擅自trim后存储。审计操作者来自Actor，动作由应用操作固定，时间由注入时钟提供；正文不能传入有效的审计信息。
9. 生命周期和改名的审计与状态在同一事务。创建、保存、复制、派生、删除草稿、validate与预览不新增审计。写后补读或诊断失败，可能已提交成功；不扩大成跨读取事务，不自动重试写入。
10. preview-params可对草稿执行；参数问题在200正文中返回。preview先构建快照，失败不返回部分文件。文件种类、次序、路径、原始hook正文、摘要去重、sample标签次序及Sample标记保持原样。
11. 显式HostSN/ISOID不存在，不得退回示例。系列不匹配仍400；省略系列时用模板首个系列；firmware默认UEFI。ESXi无真实镜像仍报iso_version_missing，不编造原boot.cfg。
12. 取消通过context传给仓储和原配置读取；纯内存计算与读写锁不新增立即中断能力。保持事务回滚、资源关闭和所有退出路径释放锁。

### 3.3 原有证据位置

- `hooks_test.go`、`templates_test.go`、`lifecycle_test.go`：草稿、身份、版本、十个生命周期入口、权限及审计。
- `authoring_helpers_test.go`、`slots_test.go`、`selection_pagination_test.go`：局部候选问题、替换序号、游标展开、来源草稿、位置和页码。
- `preview_test.go`、`preview_params_test.go`、`tasks_test.go`：示例与实物、参数与文件预览、全部内置格式、旧任务快照。
- `pgstore/contract_{hooks,templates,hookrefs,concurrency,audit,tasks}_test.go` 与 `console/wizard_test.go`：事务、引用、独立实例并发、当前模板原子创建。不能将进程锁测试当成数据库保护测试。

## 4. 实施接口决定

以下类型都是普通Go值，不含Gin上下文、网页标签、数据库对象或可伪造的Actor字段。方法统一接收 `context.Context` 和独立的 `ports.Actor`。

路由继续注册现有请求/响应类型。特别是 `previewResponse.Files` 仍使用带原JSON标签的 `previewFile`，只做应用值到网页值的字段转换，不能直接暴露ports.PreviewFile后改变字段大小写或公开类型名称。

### 4.1 编写服务

`hooks_contract.go` 定义 `Hooks`，`templates_contract.go` 定义 `Templates`。构造函数分别为 `NewHooks(HookDeps) Hooks`、`NewTemplates(TemplateDeps) Templates`，返回真实实现，不保留空方法。

下列为Hooks的方法。Templates把Hook、HookKey、HookView、HookRevision、HookRejection分别替换为Template对应类型；其余方法名和参数次序不变：

```go
List(context.Context, ports.Actor, ListRequest) (HookPage, error) // 模板用TemplateListRequest、TemplatePage
Versions(context.Context, ports.Actor, string) ([]HookView, error)
Get(context.Context, ports.Actor, model.HookKey) (HookView, error)
Create(context.Context, ports.Actor, model.Hook) (HookView, error)
SaveDraft(context.Context, ports.Actor, model.Hook) (HookView, error)
NewVersion(context.Context, ports.Actor, VersionSource) (HookView, error)
DeleteDraft(context.Context, ports.Actor, HookRevision) error
Rename(context.Context, ports.Actor, RenameRequest) error
Submit(context.Context, ports.Actor, HookRevision) (HookView, error)
Approve(context.Context, ports.Actor, HookRevision) (HookView, error)
Reject(context.Context, ports.Actor, HookRejection) (HookView, error)
Deprecate(context.Context, ports.Actor, HookRevision) (HookView, error)
Validate(context.Context, ports.Actor, model.HookKey) ([]model.Problem, error)
```

模板另有 `Derive(context.Context, ports.Actor, DeriveRequest) (TemplateView, error)`。不暴露EnsureBuiltin方法；内置装载继续由原启动流程执行。

值类型固定如下：`PageRequest{Page,Limit int; Cursor string}`；`ListRequest{PageRequest; Prefix string; State model.VersionState; Approved bool}`；`TemplateListRequest{ListRequest; Series catalog.Series}`；`VersionSource{ID string; From int}`；`RenameRequest{ID,Name string}`；`DeriveRequest{From model.TemplateKey; Name string}`。

`HookRevision{Key model.HookKey; Rev int64}` 与 `TemplateRevision{Key model.TemplateKey; Rev int64}`；Rejection各自内嵌Revision并加 `Reason string`。`HookView{Hook model.Hook; Problems []model.Problem}`、`TemplateView{Template model.Template; Problems []model.Problem}`；Page各自含 `Items []对应View; Next string; Total *int64`。

驳回的空白或不可存理由使用 `authoring.ErrRejectReason`，网页精确映射400与现有ConsoleRejectReason；超出4096字节仍沿原审计检查路径返回，不改成新的业务原因。Reject保持“正文解析→理由检查→版本键→Rev→仓储操作”先后；适配器无法解析的版本/Rev传0，由应用在原检查位置拒绝，不能用提前写响应的positiveInt/queryRev抢先覆盖理由错误。

`HookDeps{Hooks port.Hooks; Clock ports.Clock}`；`TemplateDeps{Templates port.Templates; Hooks port.HookReader; Clock ports.Clock; SelectionGuard ports.SelectionGuard}`。查询不读取时钟；仅原有审计操作构造repository.Audit。

`port.HookReader`精确包含现有GetHook、GetHooks、ListHookVersions、ListHooks四方法；`port.TemplateReader`包含GetTemplate、ListTemplateVersions、ListTemplates三方法，签名逐字沿用domain/repository。`port.Hooks`、`port.Templates`分别内嵌Reader，再显式声明相应仓储现有写方法，排除EnsureBuiltinHook/EnsureBuiltinTemplate；不嵌入总Store。

### 4.2 编辑辅助与预览

`NewEditor(EditorDeps) Editor` 提供：

```go
Slots(context.Context, ports.Actor, model.TemplateKey) (SlotResult, error)
Candidates(context.Context, ports.Actor, CandidateRequest) (CandidatePage, error)
SourceDiff(context.Context, ports.Actor, model.TemplateKey) (SourceDiffResult, error)
PreviewParams(context.Context, ports.Actor, PreviewRequest) (PreviewParamsResult, error)
Preview(context.Context, ports.Actor, PreviewRequest) (PreviewResult, error)
```

`EditorDeps{Templates port.TemplateReader; Hooks port.HookReader; Hosts port.HostReader; ISOs port.ISOReader; Renderer ports.FrozenPreview}`。HostReader仅GetHost，ISOReader仅GetISO，签名沿用现有仓储；不调用hosts/media/tasks应用具体服务。

此处是阶段终态。Task 4先声明Templates/Hooks两个字段；Task 5连同真实预览实现增加其余三项，不提供空Renderer凑构造参数。

- `CandidateRequest{Key model.TemplateKey; Slot string; Index *int; PageRequest; Prefix string}`。Index=nil表示追加，非nil只能为1到现有实例数+1。网页无法解析index时传非nil的0；无法解析分页时传Page=-1。应用仍自行检查这些值，在“取模板→检查插槽→检查index→检查分页”的原位置报错，不在请求解析时抢先返回400。
- `Candidate{Hook HookView; Usable bool; Problems []model.Problem}`；CandidatePage使用Items/Next/Total。草稿hook的hook_ref_unapproved保留为问题，但不单独令Usable=false；不关联当前插入位置的问题不得隐藏候选。替换时只保留新组件同名参数绑定，不改原装配表及引用映射。
- `SlotInfo{Name string; Exec bool; Timing catalog.Timing; Line,Col int}`；`SlotResult{Slots []SlotInfo; Problems []model.Problem}`。首个插槽保留顺序和位置，后续重名位置仍返回诊断；不另写扫描器。
- `SourceDiffResult{Original,Current *TemplateView; Changed bool}`，遵守3.2的来源选择规则。
- `PreviewRequest{Key model.TemplateKey; HostSN,ISOID string; Series catalog.Series; Firmware catalog.Firmware; Entered map[params.FieldKey]string}`。
- `PreviewParamsResult{Fields []params.Field; Problems []model.Problem; Sample []string}`；`PreviewResult{Files []ports.PreviewFile; Problems []model.Problem; Sample []string}`。应用的非空领域问题错误使用 `authoring.ProblemsError{Problems []model.Problem}`，由现有网页类别映射转换；参数预览的问题仍放在成功结果里。

### 4.3 共用操作者检查与两项能力

1. 在 `ports.Actor` 上增加 `Authorize(permission string) error`，唯一实现身份检查和调用domain/access权限表的检查。共用 `ports.ErrUnauthenticated/ErrPasswordChangeRequired/ErrForbidden`。tasks旧错误名作精确别名，旧authorize只转发；保持原401、强制改密403、无权限403的先后，不另写权限表。
2. `ports.SelectionGuard` 原样保留 `ReadLock() func()`、`WriteLock() func()`；从tasks私有port移出，删除旧契约文件。`infrastructure/selection.NewGuard() ports.SelectionGuard`只封装sync.RWMutex。模板Approve/Deprecate在写入和补读期间持有写锁，任务Submit继续读锁，同一次组装必须是同一个实例；仓储原身份先锁、再锁版本及引用锁完全保留。
3. `ports.FrozenPreview` 定义 `RenderFrozen(context.Context, model.Snapshot, catalog.Firmware, string) ([]PreviewFile, []string, []model.Problem, error)`。`PreviewFile{Kind,Name string; Sample bool; Body,Slot,Digest string}`；`PreviewEndpoints{Gateway,Artifacts,SMB,SyslogAddr string}`。该能力只处理已冻结数据，供编写预览与原任务预览共同使用，调用方先完成对应权限检查；它不是额外的匿名网页入口。

`authoring.NewPreviewRenderer(port.BootConfig, ports.PreviewEndpoints) ports.FrozenPreview`实现原renderPreview/previewValues流程。保留sample-token、sample-finalize-token、sample-task和原四项缺省地址，禁止把真实任务凭据装进预览结果。

`port.BootConfig`保留 `ReadESXiBootConfig(context.Context, model.SnapshotISO) (string,error)`。实现从cmd移入 `media.NewBootConfigReader(root string) *BootConfigReader`，只按冻结repo路径读取原文；保留os.OpenRoot、相对路径与符号链接保护、取消检查和文件关闭。读者缺失只在需要ESXi配置时返回原内部错误，不使其他格式突然无法预览。

console.Deps的新增字段名固定为 `HookAuthoring authoring.Hooks`、`TemplateAuthoring authoring.Templates`、`AuthoringEditor authoring.Editor`、`FrozenPreview ports.FrozenPreview`。构造均显式注入，不在console.New里临时创建应用服务。组装provider分别命名 `hookAuthoringOf`、`templateAuthoringOf`、`authoringEditorOf`、`previewRendererOf`，只连接上面列出的依赖。

NewHooks/NewTemplates的必需仓储、Clock与后者的SelectionGuard缺失时立即拒绝构造，沿用NewWizard的panic方式；NewEditor按当前已实现方法检查必需依赖。NewPreviewRenderer的BootConfig允许nil并保留原ESXi按需报错，空Endpoints仍取原示例值，不引入默认仓储或默认应用实现。

## Task 1: 固定网页、保存和错误先后基线

**Files:** 新建 `docs/contracts/authoring-preview.md`、`internal/interfaces/console/authoring_contract_test.go`；更新 `docs/contracts/backend-modules.md` 链接。复用3.3列出的现有测试。

**Interfaces:** 输入是bdc6371的真实路由与repository契约；产出是逐路由基线表和兼容测试。本任务不增加空应用服务。

- [x] 将3.1分组展开成32条真实路由，记录请求字段、绑定必填、状态、响应字段、问题顺序、空值、五角色权限、审计及分页。逐项核对hooks.go/templates.go/lifecycle.go，而不是复制旧设计路径。
- [x] 添加 `Test编写预览路由契约基线`：精确断言路由集合与权限、审计动作；断言创建/保存/复制/派生/删除草稿及全部读取操作没有新增审计。
- [x] 添加 `Test编写输入多错时保留原先错误`：不存在模板+非法index为404；存在模板+不存在slot+非法index为400；空白驳回理由+非法版本仍先返回ConsoleRejectReason；草稿Name不符为400；显式不存在ISO/Host不得返回示例成功。断言原响应结构，不只断言状态码。
- [x] 添加 `Test编写页型与来源选择互不替代`：游标一身份两个版本仍能带next；页码不重复展开、total包含全部版本；source-diff最新草稿只用于对比；向导仍选当前已审批版本。
- [x] 在lober VM运行 `go test -count=1 ./internal/interfaces/console -run 'Test编写|Test作者对象|TestAuthoring|Test模板列表|Test预览|TestPreview|Test生命周期'`，并运行现有仓储编写契约测试。固定现有行为的测试应通过；若发现基线缺陷，保存失败并单列修复，不修改期望来伪造通过。
- [x] 仅提交本任务文件：`test(authoring): pin authoring and preview contracts`。

## Task 2: 组件编写与生命周期经应用入口

**Files:** 新建 `authoring/hooks_contract.go`、`hooks.go`、`hooks_test.go`、`port/repository.go`；修改 `ports/actor.go`、`tasks/{contract,wizard}.go`、`console/{hooks,lifecycle,server,errors,harness_test}.go`、相关原测试、`cmd/lober/{app,wire,wire_gen,assembly_test}.go`、对应守卫许可。

**Interfaces:** 实现4.1的Hooks/HookDeps/HookView及4.3的Actor.Authorize。此时只切换组件入口，模板和预览继续原行为；不提交未实现的模板服务。

- [x] 先写 `Test组件应用拒绝无权调用且不碰仓储`：五角色、无效身份、禁用、强制改密码、非法输入组合；未授权时仓储/时钟调用数均为0。初次执行因新应用入口缺失而失败，保存红灯。
- [x] 写 `Test组件草稿保存保留身份元数据` 与 `Test组件状态写后补读失败不重试`：伪造作者/状态被覆盖或拒绝；同名保存、空字段替换、旧Rev沿用仓储结果；Submit成功后GetHook失败仅写一次，错误可见，不能声称已撤销提交。
- [x] 实现全部13项Hooks操作。只调用原仓储和activation.Hook；不把审批判断复制到网页或重写成另一份条件。缺失的批量读取项仍由原诊断表达。
- [x] Actor检查在应用入口首先执行；动作名和审计时间在应用构造。保留路由审计元数据供接口描述使用，组件处理函数不再调用auditFrom。
- [x] 将组件转换改成只消费HookView.Problems；移除ProblemsOf/HookProblems的生产转发。对尚未迁移的候选处理临时在原位置调用同一个activation.Hook，并仅承接原精确许可，Task 4必须删除，不扩大到整个文件。
- [x] 在VM运行 `go test -count=1 ./internal/application/ports ./internal/application/tasks ./internal/application/authoring ./internal/interfaces/console ./cmd/lober -run 'Test组件|Test口令|Test作者对象|TestHook|Test生命周期|Test向导'`；再运行 `go test -count=1 ./internal/archtest`。确认新旧接口可同时组装，生成Wire后执行diff检查。
- [x] 通过后提交 `refactor(authoring): route hook operations through application`。

## Task 3: 模板编写、审批与选择锁统一

**Files:** 新建 `authoring/{templates_contract,templates,templates_test}.go`、`ports/selection.go`、`infrastructure/selection/{guard,guard_test}.go`；修改 `authoring/port/repository.go`、`tasks/{contract,submit}.go`、`console/{templates,lifecycle,server,harness_test}.go`、`cmd/lober/{app,wire,wire_gen,assembly_test}.go`、锁的架构登记及并发测试；删除 `tasks/port/selection.go` 的旧契约。

**Interfaces:** 实现4.1的Templates与TemplateDeps；使用4.3的共用SelectionGuard。保留Hooks已验收契约与原CurrentTaskCreator，不新增通用事务接口。

- [x] 先写 `Test模板应用审批使用同一个选择锁`：记录ReadLock/WriteLock进入、释放和补读顺序；审批、废弃覆盖错误和取消路径；测试开始应因旧网页持锁/应用入口缺失而失败。
- [x] 写 `Test模板应用派生和保存只覆盖内容`：来源任意状态可派生；不覆盖Name/作者/审批人/内置标记/DerivedFrom；深层map和slice不能因响应转换串改原值。创建或保存后加载引用失败，记录只写一次，不自动重试。
- [x] 实现模板14项编写操作，复用activation.Template与仓储引用提取。移除网页templateHooks/templateProblems/templateResponses中的业务；先让本任务处理函数直接转换TemplateView，未迁移编辑辅助在Task 4收尾。
- [x] 将锁契约与实现移出console。生产组装创建一次selection.NewGuard，传入任务与模板服务；删除console.NewSelectionGuard及其默认兜底，测试组装也显式共享同一对象。新锁包只允许sync与共同能力包，不能放宽所有基础设施依赖。
- [x] 在真实数据库保留并执行 `Test任务提交与独立实例审批的契约`、`Test向导提交期间新版本审批必须等待任务提交` 和仓储 `Test契约并发`。覆盖双方先提交顺序、template审批/废弃与当前任务创建、hook废弃与引用审批；沿用已有阻塞观察方法，不用固定sleep冒充已等待。
- [x] 在 `console/authoring_contract_test.go` 增加 `Test编写审计失败状态与审批人均回滚`：通过真实组装的应用服务调用，仓储失败注入后版本状态、Rev、Approver、审计均不变；取消时也不发通知、不漏锁。应用包单元测试只使用契约替身，不反向导入数据库实现。锁等待不支持立即取消的现状写入契约。
- [x] VM执行 `go test -count=1 ./internal/application/... ./internal/infrastructure/selection ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober -run 'Test模板|Test编写|Test生命周期|Test契约|Test任务提交与独立实例|Test向导提交期间|Test选择锁'`，再检查架构与Wire。通过后提交 `refactor(authoring): move template lifecycle and shared selection guard`。

## Task 4: 编辑辅助操作只消费应用结果

**Files:** 新建 `authoring/{editor_contract,editor,editor_test}.go`，扩展 `port/repository.go`；修改 `console/{authoring_helpers,slots,templates,hooks,server,harness_test}.go`、现有辅助测试、`pagination.go`纯解析部分与组装函数。

**Interfaces:** 实现Editor的Slots/Candidates/SourceDiff三个操作与4.2结果类型；Preview两个操作在Task 5加入，当前接口只声明已实现方法，避免空方法占位。

- [x] 先写 `Test编辑辅助直调也检查模板读取权限`、`Test候选只采纳当前替换位置的问题`、`Test来源差异选择最新草稿但不改派生模板`。断言字段、顺序、位置、问题归属及依赖对象不被修改；在新操作缺失时保存红灯。
- [x] 提取候选的装配副本、参数绑定交集、相关问题筛选和版本展开。只调用scan.Scan与activation.Template的原规则；槽不存在、index越界、无关草稿错误、未审批但可编辑的hook分别固定结果。
- [x] 在console/pagination.go提取 `parsePaginationValues(url.Values) (page,limit int,ok bool)`，不写响应；原parseQueryPagination包装它并保留失败响应。候选接口直接调用纯函数，转换为4.2规定的无效值后调用应用，不再借queryPagination调用仓储。应用在原检查位置验证数字、页型及查询条件；同时新增不存在模板+解析错误的404回归，防止拆层改变错误先后。
- [x] 版本列表保留仓储升序；source-diff取最大版本而不筛已审批；空来源返回两个nil指针与false。Slots保留首次出现和重复位置，空列表显式构造为空slice。
- [x] 改造直接挂载候选处理函数的单元测试：显式注入应用和可信测试Actor；不能为保留旧测试绕过应用鉴权。保留真实网页权限和数据库集成用例。
- [x] VM执行 `go test -count=1 ./internal/application/authoring ./internal/interfaces/console -run 'Test编辑|Test候选|Test来源|TestAuthoring|Test插槽|Test编写|TestSelection'`，及架构全包。通过后提交 `refactor(authoring): move editor queries behind contracts`。

## Task 5: 两种编写预览与任务冻结预览共用实现

**Files:** 新建 `authoring/{preview_contract,preview,renderer,preview_test,renderer_test}.go`、`port/bootconfig.go`、`ports/preview.go`、`media/{bootconfig,bootconfig_test}.go`；修改Editor契约和Deps、`console/{preview,tasks,server,harness_test}.go`及现有预览测试、`cmd/lober/{app,wire,wire_gen,assembly_test,deletion_test}.go`；移除 `cmd/lober/bootconfig.go` 中原读取实现。

**Interfaces:** 实现4.2的两个Preview操作、4.3的FrozenPreview与BootConfig；console.Deps注入 `FrozenPreview ports.FrozenPreview`，供尚在第六阶段的previewTask使用，不导入authoring具体实现。

- [x] 先写 `Test参数预览问题仍以成功结果返回`、`Test文件预览失败丢弃全部文件`、`Test任务预览只使用冻结快照`。同时覆盖显式对象不存在、不兼容系列、缺失真实ESXi镜像、默认UEFI及sample标签精确次序；保存新应用入口缺失的红灯。
- [x] 将previewTemplateFacts、示例构造、entered规范类型、参数求值和快照编排移入authoring。SampleISO/Host仅为应用私有示例函数，不建立可供生产入口绕回网页的兼容桥。
- [x] 将renderPreview/previewValues移入NewPreviewRenderer；依原DeliveryFiles顺序生成，hook按原摘要/文件名去重，失败返回nil文件。调用方继续合并各自sample标签并统一标记文件，不让共用能力读取当前模板或当前发布清单。
- [x] previewTask保留现有任务读取、权限、固件回退和返回形状，只将原同包生成调用换成注入能力。增加真实路由回归：创建任务后变更模板/发布，再预览，正文、摘要、任务标识仍来自旧冻结数据。
- [x] 移动并运行 `TestESXi配置读取原文且不能逃离制品根`；新增已取消context返回context.Canceled且不返回内容的用例，应用替身覆盖读取取消/失败。读审原文件及目录的defer关闭均保留；禁止在模板预览里伪造boot.cfg。现有Test预览ESXi与全部内置系列仍实际执行。
- [x] console删除BootConfig/PreviewEndpoints字段和定义，任务/编写共同通过应用能力；组装从配置注入同一renderer，启动时不提前读取介质文件。
- [x] VM执行 `go test -count=1 ./internal/application/authoring ./internal/infrastructure/media ./internal/interfaces/console ./cmd/lober -run 'Test预览|TestPreview|Test任务预览|Test文件预览|Test参数预览|TestESXi配置|TestUbuntu|TestWindows'`，及领域render/builtin原测试、架构全包和Wire差异检查。通过后提交 `refactor(authoring): unify preview orchestration over frozen data`。

## Task 6: 关闭第二阶段边界并验收

**Files:** 新建 `internal/archtest/authoring_rules_test.go`、`docs/superpowers/reviews/2026-10-23-ddd-phase2-evidence.md`；更新现有架构检查、阶段契约、backend-modules、路线和本计划检查框；移除不再使用的console辅助及依赖字段。

**Interfaces:** 网页最终只注入 `authoring.Hooks`、`authoring.Templates`、`authoring.Editor` 与共用FrozenPreview；删除console.Deps.Hooks/Templates/SelectionGuard及旧业务辅助。主机、镜像、身份、任务查询的剩余依赖按原阶段继续登记。

- [x] 先补类型有效的反例：编写处理函数直接取仓储、局部别名、方法值、两层同包转发、包级函数别名、旧响应转换重新计算规则、小接口直连仓储、调用领域渲染，全部必须被拒绝。纯字段转换与调用已登记应用入口应通过。
- [x] 扩展既有typedTransitionFindings/wizardHelperFindings的根集合覆盖本阶段处理文件，不只做字符串搜索。保留向导禁止调用TaskDiagnostics/Notifier等旧能力的原反例；只放行本阶段明确的应用服务和FrozenPreview方法，不泛许所有ports能力。
- [x] 逐项删除 `migrationExceptions` 的阶段2共55项、`consoleRuleMigrationExceptions` 的阶段2共28项；阶段2及向导许可均为0。原360项在无额外清理时降为277项。若连带移除其他已失效许可，逐条说明；不能把83项转移阶段、改符号名续用或自动扩容清单。
- [x] 删除模板/组件仓储字段和旧ProblemsOf/HookProblems/templateHooks/templateProblems/renderPreview等生产桥；harness_test中的Wizard从测试仓储独立取得Hooks/Templates，不再从已删除的console.Deps字段取值。确认错误转换共享函数仍只映射错误，不经它们回调仓储或领域业务；正式编号六项能力及唯一通知桥不扩大。
- [x] 在lober VM执行根目录 `make check`，包括强制全模块检查、普通与并发全包、Linux构建和Wire差异。再执行 `pnpm --dir web check:console-types`；生成文件应无差异，不以刷新它掩盖公开名称变化。没有前端业务修改时不重复全量前端构建；若实际修改前端，则按仓库要求补类型、相关测试与构建。
- [x] 对冻结提交完成独立整分支审查，逐项对应32条路由、数据库契约和五个Review Focus。记录完整命令、退出码、跳过项、失败及修复、源码树和实际环境；未执行的检查不能记通过。
- [x] 仅在通过后提交 `refactor(authoring): enforce phase-two contract boundaries`，交付模块分支。合入develop前核对其实际新提交并对合并结果重新验证；发现缺陷按模块回退，保留第一阶段保护。

## 5. lober虚拟机执行约定

1. 连接方式、密钥和测试库按用户的私有环境说明确认，先核对hostname为lober。说明位于 `E:\src\lober\docs\linux-test-environment.private.md`；不得把其中的口令、地址、密钥内容或连接串复制进计划、提交和公开日志。
2. 使用新的第二阶段独立目录，不覆盖运行中服务、现有源码、配置、安装制品或第一阶段证据。测试连接只指向已核实名称的lober_test，测试夹具自己管理临时schema。
3. 传输精确Git归档，保留可供架构检查使用的Git索引；两份受管理但被忽略的console/ui/dist占位文件需显式加入索引。先核对源树，再放入不跟踪的测试连接文件。不要上传整个Windows工作目录或凭据。
4. Go使用1.27.1。每次统一设置 `LOBER_TEST_BASH=/bin/bash`、`LOBER_TEST_BUSYBOX=/usr/bin/busybox`、`GOMAXPROCS=2`、`GOFLAGS=-p=2`，以及相同的PATH和项目依赖代理；复用现有项目缓存，不改全局配置。需要Node命令时按私有说明使用虚拟机既有Node/pnpm。
   Wire更新统一在虚拟机执行 `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire gen ./cmd/lober`，随后执行 `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober`；把工具实际生成的wire_gen.go带回对应模块提交，禁止手写生成结果。
5. 执行前检查可用磁盘和并发占用，避免测试临时文件填满盘。不得删除其他人的测试目录或缓存腾空间；不得改动产品数据库、systemd、网络或客户虚拟机来准备本次源码测试。
6. 编写应用新测试先失败再通过；固定旧行为的基线测试先取得真实结果。相同源码、依赖和环境可以复用有效测试缓存，强制全模块检查仍按make规则执行。文件变动、失败或证据缺口才补相应检查，不按任务名称重复昂贵全套。
7. 上轮网页并发测试实际1147.910秒，接近现有20分钟时限。保留原密码计算强度、时限和断言；超时先留堆栈与现场，核对基线和环境，不能直接扩大时限或跳过。未设置的可选测试条件如实列出，不声称完成整机安装验收。

## 6. 缺陷与回退边界

- 若测试证明现有数据一致性缺陷，先保存稳定失败，独立提交修复，再继续搬迁；该修复是否依赖模块代码须写入证据，不将新行为混在机械移动中。
- 编写阶段回退其完整提交序列，不能单独倒退锁实现而保留新组装，不能删除第一阶段CurrentTaskCreator或身份锁顺序。
- 若独立修复需要保留，先在基线兼容形式落定、记录原提交与依赖，再独立保留。未解决的审批/废弃/任务提交缺陷阻止验收。
- 最终不产生数据库结构、发布清单、配置格式或前端业务变化。发现这类必要变化时停止相应任务并单列设计影响，不能自动扩大本次范围。

## 7. 自审记录

- 设计第1、3、10节：只迁移第二阶段；起点为集成后的develop，第一阶段已验收能力与其他模块保持。
- 设计第4、5、9节：文件职责、三个应用接口、仓储小接口、两项流程共用能力、锁实现与直接/间接反例均有任务承接。
- 设计第6节：逐路由基线由Task 1固定；输入、输出、顺序、错误、权限、写入、重复调用、并发、审计、取消均有明确约定。
- 设计第7、8节：不替换数据库提交时复查；审批/废弃与任务当前选择、hook引用、审计回滚及旧快照由Task 3、5保留。
- 方法与类型在4.1至4.3统一定义，后续任务按同一签名使用；阶段性接口只声明已实现方法，不提交空服务。
- 五项Review Focus分别落到Task 1/3/4、Task 1/4、Task 2/3、Task 5、Task 3/5的具体测试。
- 契约基线仍有32条路由，临时许可83项；实施前逐条核对而非用数量代替内容。所有测试命令都明确在lober VM执行，本次编写计划没有执行业务测试。
