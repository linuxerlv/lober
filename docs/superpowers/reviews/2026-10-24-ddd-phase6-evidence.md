# 第六阶段验证证据

模块分支的实现、逐项独立审查、原完整验收和整分支独立代码审查已完成。原整分支审查为Critical 0、Important 0、Minor 1；唯一总览文档过时项在本次Step6更新，等待根端仅文档关闭检查。审查详情见[最终审查记录](2026-10-24-ddd-phase6-final-review.md)。本交付保留模块分支，未合并、推送或推进环境。

## 候选源码与范围

- 分支：`codex/ddd-task-read-finalize`；集成起点：已验收 dev `7c0edfdb67282ccfd25e9d2f31448cb0bb0faa2b`。
- 冻结代码提交：`7f2c35a006f80436ec73d10d9ab6206574985535`；树：`11a9363dfc3c1b71b2d12ab830510249a79857be`。
- 接口保留请求解析、可信身份、应用调用、普通字段转换和网络发送。七个任务入口经任务应用，错误说明及公开枚举经应用契约，初始化、机器渲染、观察策略和密钥实现归所属模块。
- 没有新增装机能力，没有调整网页字段/错误顺序/事务/密码强度/会话缓存/测试时限/数据库结构。当前四张迁移表逐表为 0，39 项原始摘要、68/154 项历史及六处编号能力继续受守卫保护。

## 各任务代码关

这些是本阶段逐项实施和独立审查的记录，不是复制前五阶段的验收结果。各任务的首次失败、修正、有效失败证明和最终通过记录均保留；表格只列其最终代码关，不表示首次即通过。

| 任务 | 代码提交 | 本任务验证范围 | 独立结论 |
| --- | --- | --- | --- |
| 1 错误说明与历史重建 | `476f4bbf1b50074f853d0c03edbd33e742b7c9b8` | ports、console、archtest 完整三包 | Approved |
| 2 应用公开枚举 | `66505c4b02ea0d45fdff7d8dbba0b87b09234ec2` | authoring、identity、console、archtest 完整四包与生成类型检查 | Approved |
| 3 查询与冻结预览 | `e9c88a33ff588871fbd3b828bc7777c4412b272d` | tasks、console、cmd/lober、archtest 完整四包与 Wire | Approved |
| 4 原子控制编排 | `71a9e0b49ba1e147e7627ae6327f4a43fb1b1001` | tasks、execution、console、cmd/lober、archtest 完整五包，含真实保存与冲突 | Approved；一项文档过时问题交任务 5 并已关闭 |
| 5 事件订阅与通知收尾 | `0947e1716b2787e500eac5d7a16210f63a2482b9` | 完整五包、三项原事件流回归、通知竞态、生成类型与 Wire | Approved；当前四表清零 |
| 6 内置初始化 | `8c1521a15b5880ab08b548b22a276cba8d654aff` | authoring、cmd/lober、archtest 完整三包与原启动回归 | Approved |
| 7 机器组装职责收回 | `2edc0a37a8e2b01f528f09092ba4197e7c846534` | authoring、execution、machinekeys、events、machine、cmd/lober、archtest 完整七包与原交付回归 | Approved；两项轻微记录见下文 |
| 8 终态守卫 | `939c734`、`adab754`、`02e61d8`、`7f2c35a` | 完整 archtest；经三轮修复关闭本地接口/状态来源/控制指针等漏检与回归 | 代码关 Approved；整阶段验收另列 |

任务 8 不是一次即通过。独立审查指出三个原漏检及后续 N1–N4 回归，三轮修复分别增加真实合法反例，经过有效失败、定向通过、冻结源码完整包验证和提交摘要核对。最终代码关已通过；其后的整分支独立审查重新核对了这些具体问题，未发现仍开放的缺陷或新倒退。

任务8对原有四个任务计时日志包装作严格结构验证，核对准确文件、接收者、真实嵌入Store、原样参数、结果、标签和四条语句；同名伪类型、换文件、改参数/结果及新增helper均有真实拒绝样例。未新建迁移或能力许可表，也未豁免整个logging.go。

任务 4 删除旧审计辅助后，对应测试改走真实取消路由，继续核对可信操作者、原动作、空详情和固定注入时间；非空详情由原执行桥接测试保留。没有声称这些断言逐字节未改。

任务 7 的观察接口精确检查实际集中在 `internal/archtest/task_finalize_rules_test.go`，隐藏包完整分派另有测试，原 `execution_guard_test.go` 保持完整。独立审查核对等价覆盖，根端接受此归置；没有为满足文件清单制造空改动。已有 Gin 调试警告和路由输出按计划不列为本阶段必改项，没有过滤真实失败，也不声称日志完全安静。

## 初始失败与最终验收的区别

阶段初始原 `go test -count=1 ./...` 退出 1：33 包通过、2 包失败、8 包无测试。两个失败包因漏导出原生 Bash/BusyBox 路径，未改源码和测试，按规定环境原样重跑两包后通过。该重跑只解决环境误配，不把初次全量结果改称通过，也不代替下面最终原命令验收。

本次额外的 `go list -m all` 只读诊断在 `GOPROXY=off` 下不能补查未缓存的全图元数据；原样重放同样失败。没有联网补图或改依赖。实际 `go list -deps -test ./...` 已成功解析 553 个活动依赖包；下面原 make 检查才证明本次构建/测试所需依赖可用，不宣称整个离线模块图完整。

## 本次环境和整树证明

环境重新测量于北京时间 2026-10-24 22:36：lober VM、dev 用户，4 个处理器、总内存 2111 MiB、可用内存 1669 MiB、可用磁盘约 21 GiB。Go 1.27.1、Node 24.16.0、pnpm 11.16.0、GNU Make 4.3，原 Wire 工具、Bash/BusyBox 和测试所需原生工具均可用。

固定设置保持 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`，原生工具 `/bin/bash`、`/usr/bin/busybox`，Wire 固定 `fallback` 和关闭加载产物。全部 Go、Wire、Node、构建和测试只在 VM 执行。

测试连接按真实读取顺序落到模块根忽略文件，权限 0600。只查询确认库名 `lober_test`、非恢复模式和测试角色；连接内容、密码和运行时配置没有进入公开稿。

验收前本地、提交与 VM 的全部 2114 个已跟踪文件逐字节相等，Git 模式/对象与索引树一致；清单摘要为 `29891be8bf074b1aece0bab180cf3804264f3611e3f639a28ef4cbc53d0250c2`。VM 为该提交的分离检出，本地保留命名分支。45 个实际包的 553 个构建/测试/嵌入输入全受跟踪，只有规范 Wire 注入文件被当前普通构建排除。

没有忽略源码进入实际活动源码目录。205 个本地私有证据 Go 副本保留在 `.superpowers`，没有传入 VM 构建；VM 的实际 Go 输入不含这些副本。不能把这句话改成“任何地方都不存在忽略 Go 文件”。前端依赖目录未安装，但本次唯一前端契约脚本仅用 Node 内置模块和 Go 导出器，不依赖第三方前端包；本阶段未改前端行为，也未声称运行完整前端应用构建。

## 原命令验收结果

以下检查按批准顺序执行，只清测试结果缓存，不清离线编译依赖。以下原命令均已实际完成，没有用定向通过替代原命令。

| 顺序 | 原命令 | 实际结果 |
| --- | --- | --- |
| 1 | `go clean -testcache` | PASS，退出 0 |
| 2 | `make check` | PASS，退出0；普通与竞态各45包：37通过、8无测试文件；外层缓存/未匹配均0 |
| 3 | `pnpm --dir web check:console-types` | PASS，退出0，仅检查 |
| 4 | 下列完整 13 包详细测试 | PASS，退出0；实际6分18秒，13包全部通过 |
| 5 | `git diff --check` | PASS，退出0 |

`make check` 已实际完成格式、vet、全模块发现、普通测试、原 `go test -race -timeout=20m ./...`、Linux amd64 构建和 Wire 漂移检查；北京时间22:38:51至23:11:47，约32分56秒，竞态控制台包876.017秒，未改20分钟限制。失败会保留首条原命令与堆栈，先在同一源码/环境原样重跑失败用例，再重跑原完整命令，不改测试或时限。

```bash
go test -count=1 -v \
  ./internal/application/ports ./internal/application/tasks \
  ./internal/application/authoring ./internal/application/identity \
  ./internal/application/execution ./internal/archtest \
  ./internal/infrastructure/notification ./internal/infrastructure/machinekeys \
  ./internal/infrastructure/events ./internal/infrastructure/persistence/pgstore \
  ./internal/interfaces/console ./internal/interfaces/machine ./cmd/lober
```

原13包详细测试共487个顶层和2313个子测试通过，外层失败、明确Skip、无测试文件、缓存和未匹配包均为0。非详细 make 输出不能证明隐藏 Skip 为 0；故意违规子进程的 FAIL/缓存/未匹配文本与外层包结果分开统计。

34个计划名称全部精确对应实际外层PASS，无需名称映射或新增空测试。任务/事件顺序、版本冲突、同主机未结束任务约束、失败回滚、审计原子性、内置初始化、重启凭据和引用保护均有真实测试库PASS记录，没有因缺少连接而跳过。原详细命令使用-count=1，验收期间未改测试连接夹具或断言。

生成的 `generated.ts` 验收前摘要为 `08dc80a1e7c472e907d8f710845ce09f5999c0ec792f2aa78776bd407405941c`；检查模式不接受重生成快照来掩盖差异。验收后再次核对本地、VM前后、提交的全部2114文件模式、对象和物理字节完全相等，553活动构建/测试/嵌入输入一致；HEAD和索引仍为冻结候选，生成文件摘要不变。

## 计划用例与真实数据库执行

下列34个计划名称均在原13包详细命令中精确出现外层PASS。名称清单只用于核对已批准行为，未新增凑数测试，也未替代完整包。表中包路径相对仓库根。

| 原计划测试名称 | 实际通过包 |
| --- | --- |
| `TestEnabledMachineAssemblyPersistsProtectedKeyAndServesCanonicalRoute` | `cmd/lober` |
| `TestEnabledMachineRejectsInvalidEndpointOrUnprotectedKey` | `cmd/lober` |
| `TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol` | `cmd/lober` |
| `TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic` | `internal/application/execution` |
| `Test事件订阅保留字段副本与上下文` | `internal/application/tasks` |
| `Test事件订阅先订阅再首读且失败退订` | `internal/application/tasks` |
| `Test任务SSE补读分页丢通知续读及断开退订` | `internal/interfaces/console` |
| `Test任务SSE连接期间复查禁用改密及会话过期` | `internal/interfaces/console` |
| `Test任务SSE退出与到期只检查原令牌` | `internal/interfaces/console` |
| `Test任务事件直接查询不额外查任务` | `internal/application/tasks` |
| `Test任务列表保留分页顺序与诊断` | `internal/application/tasks` |
| `Test任务应用只使用本模块契约` | `internal/archtest` |
| `Test任务控制不存在先于不可用` | `internal/application/tasks` |
| `Test任务控制保留版本审计与顺序` | `internal/application/tasks` |
| `Test任务控制拒绝越权且不读取` | `internal/application/tasks` |
| `Test任务控制提交后读取失败不额外通知` | `internal/application/tasks` |
| `Test任务查询不能绕过权限` | `internal/application/tasks` |
| `Test任务查询保留错误先后` | `internal/application/tasks` |
| `Test任务终态边界拒绝仓储和后端旁路` | `internal/archtest` |
| `Test任务读取和预览显式组装` | `cmd/lober` |
| `Test任务预览仅传冻结快照与样例标签` | `internal/application/tasks` |
| `Test任务预览固件先于查任务` | `internal/application/tasks` |
| `Test公开目录保持顺序与独立副本` | `internal/application/authoring` |
| `Test公开角色保持顺序与独立副本` | `internal/application/identity` |
| `Test内置初始化保留顺序四次冲突与取消` | `internal/application/authoring` |
| `Test启动先内置内容后身份初始化` | `cmd/lober` |
| `Test审计意图桥接保留真实保存与冲突` | `internal/application/execution` |
| `Test机器密钥创建重读与权限保持` | `internal/infrastructure/machinekeys` |
| `Test机器日志观察保留原有效期` | `internal/application/execution` |
| `Test机器渲染只用冻结任务且仅ESXi辅助文件读取原配置` | `internal/application/authoring` |
| `Test空库必须显式初始口令已有用户不再要求` | `cmd/lober` |
| `Test终态零许可且历史不能复活调用` | `internal/archtest` |
| `Test装配初始管理员强制改密及内置内容` | `cmd/lober` |
| `Test错误说明保留包装优先级与普通值` | `internal/application/ports` |

真实数据库代表用例包括 `Test契约任务` 的顺序/分页、版本冲突、三个原写操作审计失败回滚子例；`Test契约事件` 的每任务连续Seq；`Test契约并发` 的同主机唯一成功与事件并发序号；`TestExecutionAuditFailureRollsBackRuntimeStateAndEvents`；内置内容并发与真实启动；`TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic`；冻结预览及主机引用保护。它们经原pgtest连接真实PostgreSQL测试库，不是只用内存替身。机器真实接线和冻结文件协议也实际通过。

## 执行决定与误判代价

以下 18 项已逐条对照原批准决定和成本记录；不是新增授权。

| 项 | 执行决定 | 判断错的代价或补救 |
| --- | --- | --- |
| 1 | 按用户批准更新计划状态 | 若误判授权，需撤回状态说明；不改业务行为。 |
| 2 | 复用已核对干净的独立工作区 | 若选错工作区，会影响隔离；其他工作区不得动。 |
| 3 | 跳过本机工具初始化，全部工具验证放 VM | 若 VM 工具不齐，必须补环境证据，不能报通过。 |
| 4 | 新接口可先放可编译空实现来写失败测试 | 若把编译错误当行为失败，需重做有效失败证明。 |
| 5 | 密钥竞争保留原来可能读到半成品而失败的行为 | 若要保证竞争方全部成功，需另行设计与验收。 |
| 6 | 只给任务标题补空格，让技能脚本提取任务 | 若提取仍不准，需修文档格式；任务内容不变。 |
| 7 | 每项跑计划要求的完整相关包，最终冻结源码再跑原 make check | 若最终漏跑，整阶段不能验收。 |
| 8 | 初始环境漏导出原生工具路径后，先原样复跑失败包，再继续 | 若只靠复跑冒充原全量通过，会误报；原首次失败保留。 |
| 9 | 第一项补登记两个精确错误分类入口 | 若登记过宽，可能放过越层调用；未知入口仍须拒绝。 |
| 10 | 第二至五项同步登记各自精确应用入口 | 若登记过宽会放松边界；必须配真实类型正反例。 |
| 11 | 第三项补改遗漏的 server.go 依赖字段 | 若接线错误，路由会失效；保留未迁入口到其所属任务完成。 |
| 12 | 通用测试夹具随剩余允许位置改名，不改断言和历史 | 若改名掩盖缺口，需回补真实类型拒绝证明；最终不保留临时许可。 |
| 13 | 第四项轻微文档过时交给本来就负责同文档的第五项 | 若不闭合会误导进度；第五项已独立确认修正。 |
| 14 | 补改命令行接口文档导出器，复用同一纯文档构造 | 若输出变化会破坏前端契约；须证明输出等价且生成类型不变。 |
| 15 | 历史通知反例允许夹具额外加载一个真实包 | 若扩大默认加载或伪造类型会削弱检查；默认路径、类型检查和错误处理均保留。 |
| 16 | 第七项的精确观察接口检查集中放收尾测试文件，原执行检查文件保持 | 若漏掉旧文件独有要求需回补；实际完整扫描与等价覆盖已经审查，最终文档写清位置。 |
| 17 | 保留启动入口原有的仓储计时日志包装，只原样转交参数并记录耗时 | 若允许范围过宽会留下业务旁路；增加日志类型内的查询/规则反例，不给整个文件豁免。 |
| 18 | 第八项只收紧镜像测试中两条仍允许任务越层的旧样例 | 若顺带改其他媒体规则或历史会削弱保护；只改这两条预期与名称，保留样例源码。 |

## 保留的行为边界和未验范围

会话撤权仍遵守原复查频率，不承诺撤权提交与每条网络写入同时完成。机器密钥独占创建仍可能让竞争方读到半成品而失败，没有新增等待或承诺所有竞争方成功。控制已提交而随后读取失败时，不能声称数据库回滚。

真实 ISO 镜像检查是原有选择性覆盖，本次没有设置该开关；确定性 ISO 单元夹具仍随原命令运行。完整 make 的非详细输出不能推断全部隐藏 Skip 数，实际可观察范围会单独记录。

整分支独立代码审查已通过，原结论Critical 0、Important 0、Minor 1如实保留。本次Step6只提交两个公开记录、计划、路线和后端总览共五个Markdown文件；唯一Minor的修正位置是[后端总览的第六阶段小节](../../contracts/backend-modules.md#第六阶段任务读取控制和事件边界)，等待根端仅文档关闭检查。原代码候选2114文件中，三个获准更新的既有Markdown有变化，其余2111文件、全部553活动构建输入和生成类型摘要不变；两份新文件也仅为Markdown。完整套件测的是前述冻结代码提交，后来的文档HEAD没有重跑完整Go套件。没有合并、推送或推进dev/test/preview/main，工作区和私有证据继续保留。
