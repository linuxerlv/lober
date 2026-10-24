# 独立审查 C：任务、身份、审计、执行桥与通知

> 本文是固定提交的独立源码审查记录。原文中的运行状态按审查完成时点保留；根端随后完成的正式VM验证、独立反例及对范围的裁定统一见[总审计](2026-10-25-ddd-final-audit.md)。不把作者读到测试源码等同于作者现场重跑。

审查对象为 `E:/src/lober/.worktree/rbac-integration`，固定提交 `2e64ef1d8fe2976ff69fd0634797103810418c86`。下文源码路径均相对这个目录。报告不是旧报告的复述，依据是本次直接阅读的定义、实现、调用方及测试断言。

结论：**Critical 0 / Important 0 / Minor 1**。未发现本范围可确认的越权、部分写入、旧审批选择绕过或事件订阅泄漏回归。主要保护确实在实际调用链中。但“每项公开能力都在方法旁说明八项约定”的严格标准尚未全部达到，见 M1。

本次只读代码和 Git，并写这一份报告。没有运行本机 Go、格式化、Node、构建或测试，没有连接 VM，没有读取私人连接文件或 DSN。下面“有测试”只表示已检查测试源码和断言，**不表示本次测试已通过**。原有两个 `ui/dist` 修改不在受审对象。

## 依据和判定方法

- 总设计 `docs/superpowers/specs/2026-10-22-backend-ddd-four-layer-design.md:97` 的第 6 节规定三种契约来源及八项约定；`:126` 第 7 节规定规则、锁内复查、原子审计和提交后通知；`:142` 第 8 节规定向导整批提交和可信操作者。
- `docs/contracts/backend-modules.md` 固定五个向导入口、字段、问题归属、选择保护和事务次序。`docs/contracts/identity-audit.md` 固定身份与审计十三入口。`docs/contracts/task-read-control-events.md` 固定任务七入口、控制补读和事件流。
- 核对第一、五、六阶段计划的范围；不把历史“待删除”清单当作当前访问许可，不以旧验收报告替代代码证据。
- 本报告只审查已有执行能力的调用桥、保存、审计、凭据和通知接线。没有把未启用机器服务时返回不可用，或未新增安装能力，判为迁移缺陷。

## 问题

### M1：部分正式小接口仍只有签名或统述，不能单靠定义确认八项约定

**位置**：`internal/application/tasks/contract.go:100`，`internal/application/tasks/port/repository.go:10`，`internal/application/identity/port/repository.go:9`，`internal/application/ports/preview.go:9`。

**触发**：其他模块准备接入 `Wizard.Hosts/ISOs/Templates/Params/Submit`、身份保存小接口或 `FrozenPreview.RenderFrozen`，以正式定义为依据实现新的调用方或替代实现。

**事实**：

- `Wizard` 五个方法只有三行共同说明，没有逐操作说明，也没有明确委托到某一份已有正式契约。它没有在定义处说明候选默认/最大页大小、模板页码与游标过滤次序差别、参数预览的普通问题随成功结果返回、查询无写入及重复/取消语义。
- 向导 `Hosts/ISOs/Templates/Hooks` 小接口及身份 `Users/RoleMenus` 小接口主要是方法签名。身份保存背后的 `domain/repository/users.go:11` 同样没有逐操作完整说明；实际错误顺序、请求原版本、最后管理员保护、保存后才撤销会话等分散在应用合同、文档和实现。
- `FrozenPreview` 只有“只用已经冻结的数据生成预览文件”，没有写返回文件与样例的顺序、错误与领域问题同时出现时的先后、空结果、取消传递或对既有正式契约的明确引用。调用方 `tasks.Reader.Preview` 已经依赖这些区别。

**违反**：总设计第 6 节将应用能力的唯一来源定为“Go 接口、命令和结果类型、方法旁的中文约定”；八项约定不适用也须写原因，不能只写方法签名。第六阶段计划的 Global Constraints 也重复了这一要求。

**影响**：目前实现和测试能拼出这些行为，尚无因此造成运行故障的证据。但新增调用方只看正式接口，会错误推断分页、问题与错误、取消及提交语义。不能据此宣布所有模块能力契约已完整落地。

**建议**：不改当前行为。给上述接口补逐方法中文说明，或在方法旁明确引用唯一的既有保存/渲染契约，并补齐差异；共同说明可复用，不要复制另一份独立规则。先列出八项，再明确只读操作不涉及原子写和提交复查的原因。更新说明时保留现有字段和错误顺序。

**验证建议**：文档本身先人工逐项核对。随后只在 lober VM 运行本报告末尾列出的已有向导、身份和任务预览测试。若增加用于冻结先后顺序的测试，也仅在 VM 执行；本报告没有要求为纯文字变更新增无意义测试。

## 公开操作与保存能力矩阵

“符合”表示这次静态检查未发现定义、调用和实现的冲突。它不替代 VM 的当前运行结果。核心测试已阅读实际断言和共同夹具；表内也列出相邻原有覆盖，供 VM 完整验证。真实数据库用例经 `pgtest.OpenStore/OpenAt` 进入保存实现，不仅是返回预设值的替身。

表中路径缩写：`application/`、`domain/`、`interfaces/` 均省略根前缀 `internal/`；`tasks/`、`identity/`、`audit/`、`execution/`、`ports/` 指 `internal/application/` 下相应包；`pgstore/` 指 `internal/infrastructure/persistence/pgstore/`；只有文件名时，沿用该行指定包。`console/` 指 `internal/interfaces/console/`。

| 操作 | 定义位置 | 实现与实际调用方 | 行为及数据库测试证据 | 结论 |
| --- | --- | --- | --- | --- |
| 向导主机候选 | `application/tasks/contract.go:104`；`port/repository.go:10` | `wizard.go:24` → `ListHosts`、`FindOpenTask`；`interfaces/console/wizard.go` | 先授权、规范化可规范化的前缀、仓储筛选及分页、逐主机可选诊断；`Test向导候选分页准备状态与缺失集合`、`Test向导五路权限及空批非法字段` | 实现符合；定义说明见 M1 |
| 向导镜像候选 | `tasks/contract.go:106` | `wizard.go:49` → 主机批读、镜像列表、`admission.ISOUsable` | 重复/缺失主机先于列表分页，候选保留平台问题；应用 `Test应用候选重复缺失保留问题顺序`；网页完整五步和候选分页用真实数据库 | 符合；定义说明见 M1 |
| 向导模板候选 | `tasks/contract.go:107` | `wizard.go:92` → 模板/组件读口、领域准入和发布清单 | 最新已审批版本，新草稿不覆盖；游标先分页后过滤，页码先过滤再计数；`Test向导当前已审批版本不会被新草稿取代`、`TestNumberedTemplateEligibilityBeforePagination` | 行为未见差异；SQL 规则重复由全局审查者报告，不在这里重复计数 |
| 向导参数预览 | `tasks/contract.go:108` | `params.go:34`、`selection.go:57` → `params.Evaluate`；网页转换字段 | 当前版本检查先于编号和输入行；按规范编号匹配 entered；一般问题随成功结果返回；`Test应用参数顺序缺失和重复编号`、`Test向导参数输入按规范SN定位并拒绝未知行` | 符合；定义说明见 M1 |
| 向导批量创建 | `tasks/contract.go:109`；`port/repository.go:26` | `submit.go:12` → `snapshot.Build` → `CreateCurrentTasks`；`cmd/lober/app.go:96` 显式注入 | 每个输入项自身参数构建快照，任何问题拒绝整批；操作者和时钟来自可信注入；保存返回成功后逐任务提示；`Test应用提交整批失败不写入不通知`、`Test应用提交顺序审计时间与诊断`、`Test向导参数输入顺序不串主机` | 符合；定义说明见 M1 |
| 当前版本批量保存 | `domain/repository/current_tasks.go:9` | `pgstore/tasks.go:41`、`:45`、`:115`；向导端只有当前版本能力 | 事务先锁模板身份并重查 CurrentApproved，后锁主机、镜像、版本、引用；任务与审计同事务；普通 CreateTasks 仍允许明确指定旧审批版本 | 符合；独立连接并发证据见下节 |
| 任务列表 | `tasks/read_contract.go:39` | `reader.go:42`；`console/tasks.go:104` | 身份→分页→起止时间→范围→主机编号→状态；顺序与 next/total 原样传回，先取消诊断后重试诊断；`Test任务查询保留错误先后`、`Test任务列表保留分页顺序与诊断`、`Test任务列表详情事件及查询边界` | 符合 |
| 任务详情 | `tasks/read_contract.go:44` | `reader.go:85`；`console/tasks.go:122` | 原编号只查一次任务，再诊断，不读取当前模板/组件；读取空值由网页转换；同上网页真实数据库测试 | 符合 |
| 已冻结任务预览 | `tasks/read_contract.go:48`；`ports/preview.go:10` | `reader.go:95` → 同一 `FrozenPreview`；`console/tasks.go:280` | 请求固件先于查任务；只传已存快照和实际任务 ID；错误优先于问题，成功才加固件样例标签；`Test任务预览仅传冻结快照与样例标签`、`Test任务预览只使用冻结快照` | 实现符合；共同渲染能力的说明见 M1 |
| 取消任务 | `tasks/control_contract.go:20`；`tasks/port/control.go:10` | `controller.go:20` → `execution.AuditedControl.Cancel` → `Service.control` → `UpdateTaskExecution` | 权限先于读取；固定 task.cancel、可信用户名和一次时钟；原状态/修订传后端；不提前用诊断拒绝；真实桥测试保存取消并验证旧版本冲突 | 符合 |
| 重试任务 | `tasks/control_contract.go:24` | 同上 Retry；`execution/service.go` 保留旧尝试撤销及新尝试记录 | 不换成最新版本重试；任务、执行记录、事件、审计同事务；`Test审计意图桥接保留真实保存与冲突`、`TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic` | 符合 |
| 控制成功后的补读与补提示 | `tasks/control_contract.go:17` | `controller.go:40` → 重读→Publish→诊断 | 后端失败不补读；提交成功但重读失败返回原错误，不补第二次提示；`Test任务控制提交后读取失败不额外通知`、`Test任务控制后端失败不重读不补发` | 符合；重读失败没有宣称回滚 |
| 未启用控制 | `tasks/controller.go:57`；文档控制约定 | `cmd/lober/app.go:90` 无机器服务注入 `NewUnavailableControl` | 不伪造诊断或状态，网页保留 422 与原提示；任务不存在先于不可用；`Test任务控制不存在先于不可用`、网页控制委托测试 | 保留原范围，非缺陷 |
| 直接查询事件 | `tasks/event_contract.go:20`；`tasks/port/task_read.go:22` | `events.go:43` → `pgstore/events.go:40`；`console/tasks.go:139` | 权限后直接 ListEvents，不新增查任务；字段独立复制，顺序与原错误保留；`Test任务事件直接查询不额外查任务`、`Test契约事件` | 符合 |
| 打开事件订阅 | `tasks/event_contract.go:27` | `events.go:49`；网页 `taskStream` | 身份→负序号→查任务→Subscribe→首读200；失败 Close 后普通错误返回；`Test事件订阅先订阅再首读且失败退订`、`Test任务SSE首读失败无流头且坏序号先拒绝` | 符合 |
| 订阅 Initial/Hints/Read/Close | `tasks/event_contract.go:31` | `events.go:79`；`sync.Once`；`console/tasks.go` 每页发送循环 | Initial/字段独立副本，Read 固定200且传原 context，Close 并发幂等；`Test事件订阅保留字段副本与上下文`、`Test事件订阅并发重复关闭` | 符合 |
| 登录 | `identity/contract.go:69` | `login.go:9` → 口令能力、凭据读锁、二次读取、菜单、会话；网页 `auth.go` | 不清理用户名/密码；不存在账号做假散列；最终复查到发布持读锁；`Test身份登录失败顺序与菜单故障`、`Test并发改密不能让旧口令登录补发会话` | 符合 |
| 认证与授权 | `identity/contract.go:83`、`:88` | `access.go:12`、`:31` → `domain/access.CheckRequest`；`console/server.go` | 每请求读取最新账号；Principal 有私有 owner/用户/令牌；不能正文制造；强制改密优先，普通本人更新不授予管理权限；十三入口矩阵、快照归属、认证副作用测试 | 符合 |
| 退出、本人信息 | `identity/contract.go:75`、`:79` | `access.go:48`、`:56`；网页 auth/profile | 退出只删本令牌；本人信息用请求快照加最新菜单；无数据库写入或审计；十三入口矩阵与身份测试检查 | 符合 |
| 长连接再次认证 | `identity/contract.go:92` | `access.go:63`；`console/tasks.go` 捕获最初 token | 每页发送前先 context 再原令牌、最新账号；异常账号只返回 false；禁用/改密/到期/退出测试及精确分页测试 | 符合 |
| 创建与列出用户 | `identity/contract.go:122`、`:126` | `users.go:12`、`:30`；`pgstore/users.go:35`、`:116` | 管理员限定；基础校验→散列→保存；创建/审计分别取时钟；列表按用户名排序且不逐人查菜单，结果不含密码 | 符合；存储小接口说明见 M1 |
| 更新/禁用用户 | `identity/contract.go:129`、`:135` | `users.go:48`、`:90`；`pgstore/users.go:55` | 请求原 Rev；初始口令真正更换；自禁用先拒绝；成功保存后才撤销会话；角色单独变化不清会话；最后管理员由表锁、行锁及数据库事实保护 | 符合；真实DB用户/管理员测试见下节 |
| 本人改密 | `identity/contract.go:140` | `password.go:10` | 旧口令错误先于新口令规则；散列锁外，锁内复查账号/版本/散列/原令牌；保存成功后撤销全部会话；改密、管理员重置并发测试 | 符合 |
| 本人资料 | `identity/contract.go:144` | `profile.go:9` | 只修改四项；CheckProfile 后比认证快照 Rev；写成功后菜单失败保留已提交事实；真实数据库 `Test资料已提交后菜单读取失败保留事实` 和慢资料测试 | 符合 |
| 角色菜单读/写 | `identity/contract.go:163` | `menus.go:13`、`:32`；`pgstore/role_menus.go:41` | nil 与 [] 区别、目录排序、不能提升接口权限；首次唯一约束及已有行锁比版本；配置与 before/after 审计同事务 | 符合；保存口说明见 M1 |
| 启动身份初始化 | `identity/contract.go:185` | `bootstrap.go:13`；`cmd/lober/app.go:46` | 内置准备之后 ListUsers(Limit=1)，有任何账号忽略 seed；空库显式 seed，bootstrap/user.create；并发初始化无额外重试协议 | 符合 |
| 审计列表 | `audit/contract.go:29`、`audit/reader.go:20` | `reader.go:25`；网页 `audit.go` → 唯一只读 ListAudit | 可信管理员；范围先于应用分页；文本原样、时间含端点、ID降序、保留 next/total；应用及网页审计分页过滤测试 | 符合 |
| 完整审计导出 | `audit/export.go:11` | `export.go:16`；网页全部完成后才发下载头 | 固定500逐页读、先完整内存缓冲；后页失败/取消无部分结果；CSV公式保护不改原文；真实501条 `Test审计导出后页失败不发送残缺下载` | 符合 |
| 密码与令牌计算 | `identity/port/credentials.go:7`、`:20` | `infrastructure/credentials/password.go:17`、`token.go:13` | 固定600000轮、16字节盐、32字节结果与常量时间比较；32字节随机令牌→64位小写十六进制；坏散列不会自选计算次数 | 符合；强度和格式测试源码保留 |
| 会话表 | `identity/sessions.go:14`、`:25`及各方法中文说明 | `sessions.go`；生产由同一 identity.Service 持有凭据锁 | 0取43200秒；expires<=now 到期；缺失不读时钟；负时钟/溢出饱和；并发互斥；测试覆盖期限、清理、构造依赖 | 符合，不新增持久会话承诺 |
| 审计意图桥 | `execution/control_intent.go:10`、`:17`、`:26` | `AuditedControl` 普通值转换；`cmd/lober/app.go:90` | 原任务/context/四项审计事实和错误原样传递；不新增权限或独立审计保存；替身精确传递测试加真实保存/冲突测试 | 符合 |
| 机器凭据与地址观察桥 | `execution/service.go` 的 Credentials 中文约定；`execution/observation.go:8`、`port/observation.go:4` | `interfaces/machine/http.go` 的 Engine/Observer；组装连接已有执行和观察能力 | 凭据先安装后收尾；只在原凭据检查成功处观察；原尝试号/有效期不变；`Test机器凭据保持安装与收尾顺序`、`Test机器日志观察保留原有效期`、`Test机器观察只在凭据检查通过后调用` | 符合本次桥接范围 |
| 任务提示发布/订阅 | `ports/notification.go:4` | `notification/hub.go:14`；组装单一实例 | 按任务隔离、容量1、合并；锁与 once 保护并发发布/退订；`Test通知按任务隔离合并并可退订` 及三个并发退订测试 | 符合；未承诺必达 |

## 八项约定的交叉检查

| 设计要求 | 实际证据与判断 |
| --- | --- |
| 1. 目的、归属、允许调用者 | 网页路由声明+identity Authorize+各任务/audit 应用入口 Authorize。身份 Principal 私有字段保护归属；普通 Actor/AuditIntent 是可信入口注入值，正文请求类型不含有效身份来源。 |
| 2. 输入、必填、大小、规范化 | 网页绑定只负责格式；应用按领域 NormalizeSN 和现有仓储 CheckPageQuery；密码、事件、保存数据继续原检查。向导和部分小接口的定义旁说明不全，见 M1。没有额外收紧已有查询或任务编号行为。 |
| 3. 成功、顺序、空值 | 反序 SN 参数及冻结快照断言；任务页面/用户菜单非 null 数组；total 只在页码模式；诊断 CanCancel→CanRetry；事件 Fields 独立副本且网页空映射为 {}。 |
| 4. 错误类别、问题定位、优先顺序 | `ports/failure.go` 只按类型/错误归属分类，`console/errors.go` 映射状态；无文字匹配。测试固定身份优先、时间/状态先后、渲染错误先于问题、首项问题状态、500不外泄及完整定位。 |
| 5. 写入范围和一起成功/失败 | pgstore.createTasks、UpdateUser、SaveRoleMenus、UpdateTaskExecution 在同一 write 事务里写业务及 addAudits。真实故障触发器测试检查任务/事件/运行记录/用户/版本均未残留，应用失败不撤销会话或通知。 |
| 6. 重复、重试、版本冲突 | 用户/菜单原 Rev、任务原状态+Rev 原样保存；后端锁内比较，不擅自刷新版本。只读允许看到新数据，不承诺多页快照。旧任务冻结快照不重算。 |
| 7. 并发最新事实和提交复查 | 当前模板身份读锁+数据库 CurrentApproved；主机固定次序写锁；镜像/版本/组件读锁；开放任务唯一索引兜底；用户最后管理员表锁；菜单首次唯一约束；执行行锁+版本条件。 |
| 8. 审计、通知、取消和超时 | 用户名与动作由应用固定，时间注入；保存成功后才通知/清会话；context 传给原仓储/渲染，控制提交后不插新取消检查；事件页前复查原令牌，最终关闭订阅。向导/预览等小接口方法旁说明仍需补齐，见 M1。 |

### 确实检查到的数据库并发与回滚保护

1. `Test任务提交与独立实例审批的契约`（`interfaces/console/wizard_test.go:561`）建立两个 Store、两个 Server 和独立连接，覆盖审批/同版废弃先完成、应用已读旧选择但尚未拿身份锁、任务已持锁三类交错；检查等待关系、409/data:null/原提示、任务数和 task.create 审计数。不是只靠同进程选择锁的测试。
2. `Test主机变更与向导提交按数据库事实交错`（`hosts_contract_test.go:503`）用独立连接：主机先改架构提交时向导 422 且无任务/任务审计；任务先锁主机时保留旧快照，后续主机修改正常完成。未把快照改成跟随当前主机重算。
3. `Test契约并发`（`pgstore/contract_concurrency_test.go:24`）覆盖反序批次 `CreateCurrentTasks`，恰好一批成功，其余是约定开放任务问题；不是通过自动重试绕过冲突。普通创建、条件推进和事件序号的原并发用例也保留。
4. `Test身份写入审计失败不撤销会话`（`identity/users_test.go:205`）在真实保存事务注入审计失败，逐一检查 reset/disable/password/profile/create；比较完整账号和版本、原令牌仍能认证、无新审计/新账号。`Test身份菜单首次竞争及审计回滚`（`menus_test.go:199`）同样检查真实配置和 before/after 事务。
5. `Test身份跨实例管理员移除仍由数据库保护`（`identity/users_test.go:333`）用不同凭据协调锁的两个服务同时降级不同管理员；一方成功、一方 ErrLastAdmin，最终恰好一位管理员，成功/失败各自版本和审计数准确。
6. `TestExecutionAuditFailureRollsBackRuntimeStateAndEvents`（`pgstore/execution_test.go:42`）比较完整任务和空事件；`Test审计意图桥接保留真实保存与冲突`（`execution/control_intent_test.go:65`）真实保存取消/重试并拒绝旧版本，审计恰好两次。

### 事件流和通知的实际顺序

`EventReader.Open` 先订阅再首读；首读失败马上退订。网页拿到成功订阅后才设置流头。`taskStream` 捕获最初令牌，每页（含空页）发送前检查 context 和身份；满200条继续补读，短页才等待提示或5秒轮询。任务结束不会自动关流。

`defer subscription.Close()` 与 ticker.Stop 覆盖断开、写失败、晚期读失败、提示关闭。`Test任务SSE精确分页空页复查与所有退出退订` 明确检查200/201/400条、末空页拒绝、取消、写失败、晚期错误、原令牌、Flush次数及不输出晚期JSON错误。真实HTTP/数据库 `Test任务SSE补读分页丢通知续读及断开退订` 读205条、正常提示及丢提示轮询，再断线续读并检查订阅归零。

`Notifier` 的“取消时不发布”应结合提交事实理解：事务因取消失败不发；仓储已经返回提交成功，即使随后 context 取消仍发。`Test应用事务已成功即使随后取消也通知` 明确固定此行为。未将这一句文案解释成应撤回已提交通知，也未据此建议改行为。

## 无法核实与暂不评判

- 本次没有新 VM 运行记录，不能确认固定 HEAD 的全套/竞态/构建/组装代码检查通过，也不能以测试名字和旧报告代替结果。测试性能和偶发失败须由根任务在 lober VM 验证。
- 本次没有证明所有可能输入下的 SQL 与领域筛选等价。`TestNumberedTemplateEligibilityBeforePagination` 有真实数据库比较，覆盖多类缺选项/内置/废弃情形；其中 builtinOK 仍手写。全局审查者负责“规则不应重复”的架构问题，此处不重复统计。
- `domain/repository/tasks.go:38` 的“审计不为零时，先校验审计，再在同一事务里写入”没有明说先于所有参数检查。`pgstore/execution.go:13` 先 CheckTransition 后 CheckAudit，但仍在任何写入前检查审计；外层 execution.control 又先 CheckAudit。不能据此确认正式错误顺序冲突，**不计问题**。
- Principal/Actor 按每请求身份快照工作；普通接口没有承诺撤权提交和已经开始的请求写入原子完成。长连接按页重查也不承诺每一字节和撤权原子完成。不能把这些明确边界当作绕过权限。
- 多实例持久会话、跨实例立即撤销所有令牌、通知必达、完整装机状态机的新设计不在迁移范围内。既有执行服务自身全部状态组合未在这份专项审查中重新穷举。

## 仅供 lober VM 使用的精确测试建议

以下是建议清单，**本审查没有执行**。根任务可把这些纳入其完整 VM 验证，不应替代仓库要求的 make check。

| 包 | 建议测试名 |
| --- | --- |
| `./internal/application/tasks` | `Test应用向导不能绕过权限`、`Test应用提交整批失败不写入不通知`、`Test应用提交顺序审计时间与诊断`、`Test应用提交成功后才通知`、`Test应用提交取消不提前通知`、`Test应用事务已成功即使随后取消也通知` |
| `./internal/application/tasks` | `Test任务查询保留错误先后`、`Test任务查询不能绕过权限`、`Test任务预览仅传冻结快照与样例标签`、`Test任务控制提交后读取失败不额外通知`、`Test任务控制后端失败不重读不补发`、`Test事件订阅先订阅再首读且失败退订`、`Test事件订阅并发重复关闭` |
| `./internal/interfaces/console` | `Test向导参数输入顺序不串主机`、`Test任务提交与独立实例审批的契约`、`Test主机变更与向导提交按数据库事实交错`、`Test任务预览只使用冻结快照`、`Test任务SSE补读分页丢通知续读及断开退订`、`Test任务SSE退出与到期只检查原令牌`、`Test任务SSE精确分页空页复查与所有退出退订` |
| `./internal/application/identity` | `Test本人改密锁内复查令牌和账号`、`Test并发改密不能让旧口令登录补发会话`、`Test并发本人改密不能覆盖管理员重置`、`Test身份写入审计失败不撤销会话`、`Test慢资料请求不能用猜测新修订号恢复旧权限或凭据`、`Test身份菜单首次竞争及审计回滚`、`Test身份跨实例管理员移除仍由数据库保护` |
| `./internal/interfaces/console` | `Test身份十三入口契约与强制改密矩阵`、`Test资料已提交后菜单读取失败保留事实`、`Test审计导出后页失败不发送残缺下载`、`Test审计分页过滤完整CSV及公式作为文本` |
| `./internal/infrastructure/persistence/pgstore` | `Test契约任务`、`Test契约并发`、`Test契约用户`、`Test契约角色菜单`、`Test角色菜单并发首次保存与更新`、`TestNumberedTemplateEligibilityBeforePagination`、`TestExecutionAuditFailureRollsBackRuntimeStateAndEvents` |
| `./internal/application/execution` | `Test审计意图桥接保留真实保存与冲突`、`TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic`、`Test机器凭据保持安装与收尾顺序`、`Test机器日志观察保留原有效期` |
| `./internal/interfaces/machine` | `Test机器观察只在凭据检查通过后调用` |
| `./internal/infrastructure/notification` | `Test通知按任务隔离合并并可退订`、`Test通知并发发布退订不竞态`、`Test通知同一订阅并发重复退订`、`Test通知同一订阅退订与发布并发` |

本范围是否完成迁移：主要流程与保护已在实际代码链中落地；全部公开能力的中文契约仍需按 M1 补齐。是否通过当前完整验收：等待根任务的 lober VM 结果。
