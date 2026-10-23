# 后端四层迁移第三阶段：主机实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 主机的查询、登记、替换、删除和CSV预演/导入通过应用契约执行，删除第三阶段16项临时放行，保留现有接口与数据库行为。

**Architecture:** 网页解析请求和转换响应；`application/hosts`组织权限、主机编号规范化、CSV检查与导入流程。领域继续提供唯一校验规则，原仓储负责版本、引用、锁和原子审计。任务与预览模块仍通过各自的小读取接口取主机，不调用主机应用的具体服务。

**Tech Stack:** Go1.27.1、Gin1.12.0、现有Wire、GORM与PostgreSQL；不新增依赖、表结构或前端功能。

**Spec:** [已批准四层设计](../specs/2026-10-22-backend-ddd-four-layer-design.md)、[阶段路线](2026-10-22-backend-ddd-roadmap.md)、[模块契约](../../contracts/backend-modules.md)。

状态：用户批准后的四项任务、逐项独立审查及整分支审查均通过。交付 `codex/ddd-hosts` 模块分支，保留工作区；develop 合并及后续环境另按仓库流程推进。实际结果见[第三阶段证据](../reviews/2026-10-23-ddd-phase3-evidence.md)。

## Global Constraints

- 设计第1节：“本次重构保留现有业务行为、数据库约束、数据格式和对外接口。”主机字段扩展、分页改版和导入策略变化不属于本阶段。
- 设计第5节：“应用模块不能直接导入另一个应用模块的具体服务。”向导与编写预览的主机读取接口继续独立，不改成转调主机服务。
- 设计第7节：“不引入可以执行任意数据库操作的通用事务回调，也不把数据库事务对象交给上层。”主机写入继续使用现有七方法仓储契约。
- 设计第7节：“审计与业务写入在同一事务。提交成功后才能发通知；事务失败不发通知。”原主机操作不发通知，本阶段也不增加通知。
- 设计第9节：“已迁移模块不得再调用旧业务流程。”第三阶段许可必须实际删除，不能改名、换阶段、转移清单或扩大其他能力。
- 设计第10节：“后端改动必须通过 `make check`”。测试、构建、gofmt、Wire及网页生成检查只在用户指定的lober虚拟机执行；本机处理文本、Git及传输。
- 起点是已集成第二阶段的develop，保留真实任务控制、五角色权限、两种分页、当前模板原子创建、数据库锁顺序及冻结任务预览。设计中的旧执行能力说明不能覆盖该基线。
- 不改变公开类型名称、JSON字段、原因代码、内置发布内容、数据库结构、配置或依赖；不刷新生成文件掩盖接口漂移。

## Review Focus

1. 更新同时含坏Rev、坏路径编号和不匹配正文时，仍按原顺序返回错误；单条编号规范化不能误用于列表prefix。Task1、2固定例子。
2. CSV包含BOM、引号内换行、后段语法错误和多处重复编号时，保留物理行号、全部重复位置和问题顺序；不能把格式错误变成逐行422。Task1、3覆盖。
3. CSV预演不是提交承诺。读取失败不能伪装新建；预演后主机又被修改时，正式导入仍执行数据库原子新建/覆盖，不新增Rev检查。Task1、3覆盖。
4. 导入中间写入、审计或等待锁时取消，整批记录与审计均回滚；不能逐条提交或自动重试。Task2、3以真实数据库验证。
5. 编辑、删除、导入与任务创建相撞时，数据库重查和同序加锁继续生效；已结束任务的引用也阻止删除。不能用进程锁替代独立连接验证。Task2、3覆盖。

---

## 1. 起点、交付和文件职责

- develop：`a9d5434fc507f8857c5fb36b252a41de1cce0ab3`，树`d25ce0d3cb26ce004ec65c03a8255df4997cfa35`。第二阶段合并验证见[集成记录](../reviews/2026-10-23-ddd-phase2-develop-integration.md)。
- 模块分支：`codex/ddd-hosts`，从上述提交建立，复用现有干净隔离工作区。原主目录文档和dev工作区产物不参与本阶段。
- 实施前重新核对develop与模块分支的实际提交。若develop前进，先保留新功能并受控集成，再重核契约和测试基线；不能硬重置或忽略新增提交。
- 本轮交付验收通过的模块分支及实施、审查记录；合入develop另核对实际新提交并验证合并结果。推送、test、preview及main推进不包含在实施计划授权中。

| 文件 | 职责 |
| --- | --- |
| `application/hosts/contract.go` | Registry、请求、分页、导入计划和两类导入错误；无网页标签 |
| `application/hosts/port/repository.go` | 仅声明本模块实际使用的主机仓储方法 |
| `application/hosts/registry.go` | 构造、权限、查询、编号与替换检查、可信审计 |
| `application/hosts/import.go`、`csv.go` | 预演/提交编排，以及唯一CSV解析与逐行规则 |
| `interfaces/console/hosts.go`、`csv.go` | 保留旧公开类型、输入解析和纯字段转换 |
| `cmd/lober/app.go`、`wire.go`、`wire_gen.go` | 显式组装；生成文件由虚拟机Wire工具产生 |
| `archtest/hosts_rules_test.go`、`wizard_helpers_test.go` | 主机入口直接/间接绕回旧业务的拒绝检查 |
| `docs/contracts/hosts.md`、阶段证据 | 人可读的兼容约定与实际验收；路由仍是既有接口描述来源 |

上表代码路径均相对`internal/`，`cmd/`与`docs/`除外。测试放在相应包旁，下面逐任务列出。

## 2. 先固定的对外契约

### 2.1 六条真实路由

| 入口 | 权限 | 成功结果与审计 |
| --- | --- | --- |
| GET `/console/v1/hosts` | `host:read` | `HostPage`；无审计 |
| GET `/console/v1/hosts/:sn` | `host:read` | `HostResponse`；无审计 |
| POST `/console/v1/hosts` | `host:write` | 新主机Rev=1；同事务`host.create` |
| PUT `/console/v1/hosts/:sn` | `host:write` | 整条替换，Rev+1；同事务`host.update` |
| DELETE `/console/v1/hosts/:sn?rev=...` | `host:write` | data=null；同事务`host.delete` |
| POST `/console/v1/hosts/import?dryRun=...` | `host:write` | 预演为`CsvPlan`，无写入；正式成功为`HostPage`，每台同事务`host.import` |

五角色均可读；只有admin、operator可写，包括导入预演。无身份、禁用或无效角色401；强制改密、权限不足403。网页继续查凭据，应用也先调用`ports.Actor.Authorize`。未授权时仓储与Clock调用数均为0。

成功仍为HTTP200和现有code/data/message外层。主机字段保持`sn,arch,firmware,attrs,rev`，空attrs为`{}`；空items/rows/problems为`[]`。公开名称`HostRequest/HostResponse/HostPage/HostQuery/RevisionQuery/ImportQuery/CsvRow/CsvPlan`及OpenAPI可选架构、固件、text/csv、oneOf保持。

### 2.2 查询、单条写入和错误顺序

1. 列表用`prefix`，不使用向导的`q`语义，也不规范化prefix。`prefix=sn-1`不能悄悄匹配SN-1。属性键值成对、按原文精确匹配；不得新增ValidAttrKey过滤校验或裁剪属性值。
2. 游标模式默认100、最多500；页码模式按字段是否出现选择，默认1/20、最多500。`page/page_size`不能与出现的`cursor/limit`混用，即使空值。保留数字格式、重复页码字段、溢出拒绝、total是否出现及不透明next。排序仍为SN字节序，如SN-1、SN-10、SN-2。
3. GET和单条写入通过原`model.NormalizeSN`：去首尾空白、先查ASCII再转大写；保留sn_invalid/sn_placeholder及完整定位。不把非法编号一律改为普通400。
4. 创建先Bind，再规范化编号，再由原仓储检查记录。创建的请求Rev不成为新门槛；仓储置1。架构和固件可空，attrs整份保留，值不额外解释为IP或主机名。
5. 更新顺序为Bind→Rev>=1→规范化路径SN→若正文SN非空则规范化并比对→仓储记录检查/版本写入。正文SN可省略，不能更改身份。坏Rev和正文/路径不匹配仍是400/ConsoleBadRequest；编号问题仍沿原问题映射。省略arch/firmware/attrs表示清空，不是局部合并。
6. 删除先解析正int64 Rev（含溢出处理），再规范化路径编号，再查存在性、Rev和任务引用。旧Rev409，不存在404；任何任务引用，包括已结束任务，都409。重复创建409，成功删除后再删404；不增加幂等成功。
7. 仓储ProblemsError仍422及problems；普通ErrInvalid400、不存在404、重复/版本/引用冲突409、未知错误500且不泄露内部信息。应用原样传递稳定错误，不能按字符串判断分支。
8. Actor和固定动作构造审计，Detail仍为空、Clock每次实际写调用一次；整批导入共用同一At。查询、预演、CSV拒绝不读Clock。不要把仓储原有记录检查提前为另一套规则，改变CheckAudit/CheckHosts或错误先后。

### 2.3 CSV预演与提交

- 保持POST原始正文读取。`dryRun`省略为false，有值只接受精确true/false；空值、TRUE、1等400。重复查询保留Gin1.12.0的首值行为，不新增去重策略。先查dryRun再读正文；读取失败400。保留text/csv描述，不新增媒体类型强制拒绝或正文大小限制。
- 仅开头BOM去除，表头裁剪空白后检查重复及合法性，必须含sn。保留字段sn/arch/firmware，其余为属性；rev不是新增的控制列。CSV库原有引号、列数、空行和CRLF处理保持。
- 文件无数据、无sn、坏表头、列数或引号错误为400、data=null及原ConsoleCSV消息；后段格式错误也整份失败，不返回半份计划，不读取或写入仓储。
- 行错误保留在计划中：NormalizeSN问题在先，再由Host.Validate按原顺序给出平台/属性问题。只对成功规范化的编号查重复；所有重复行都标error，并把host_duplicate_in_batch放到各行问题首位。行SN在可规范化时转规范值，否则保持原文；不另加问题行列定位。
- rows按文件记录顺序，row用`encoding/csv.FieldPos(0)`物理行号，total包含所有记录。引号内换行不能把后续行号改成逻辑序号；解析后的属性内容不额外trim。
- 解析完成后，仅将有效、唯一主机的编号交给GetHosts；空集合也保持一次调用。查询发生在dryRun及行拒绝判断之前：读取错误仍500，不能伪装create，也不能先返回422盖住它。
- 已存在且无行错误的记录标update，其他有效行标create。dryRun=true始终200计划，包含坏行；不写记录、审计或Clock。正式请求有坏行则422、data为完整原计划，message取文件顺序第一错误行的第一问题。
- 正式有效请求一次调用原ImportHosts：已有编号整条覆盖并Rev+1，新编号Rev=1，不比较CSV Rev。预演结果不预占身份，不承诺之后仍是create/update。正式响应按输入顺序返回实际存储结果，next=""、无total。
- 任一主机、审计或事务取消失败，整批不留记录/版本变化/审计。数据库仍用hostLockOrder排序加锁，再按输入顺序返回；保持分块写入的同一个事务。没有应用逐条保存、自动重试、补偿审计或通知。
- context原样传给仓储；纯CSV和内存计算不新增立即取消机制。读取空集合或尚未访问数据库的旧边界不能被扩大为新的中断承诺。

## 3. 实施接口决定

所有应用值不带Gin、网页标签、数据库对象或可伪造审计字段。最终接口为：

```go
type Registry interface {
    List(context.Context, ports.Actor, ListRequest) (HostPage, error)
    Get(context.Context, ports.Actor, string) (model.Host, error)
    Create(context.Context, ports.Actor, model.Host) (model.Host, error)
    Update(context.Context, ports.Actor, UpdateRequest) (model.Host, error)
    Delete(context.Context, ports.Actor, DeleteRequest) error
    Import(context.Context, ports.Actor, ImportRequest) (ImportResult, error)
}
```

- `ListRequest{Page,Limit int; Prefix,AttrKey,AttrValue,Cursor string}`；`HostPage{Items []model.Host; Next string; Total *int64}`。应用按原CheckHostQuery检查；游标类型与实际查询仍由仓储保证，不新增筛选政策。
- `UpdateRequest{PathSN string; Host model.Host}`：Host.SN是可空正文SN，最终写入规范化路径SN；`DeleteRequest{SN string; Rev int64}`。
- `ImportRequest{CSV []byte; DryRun bool}`；`ImportRow{Row int; SN,Action string; Problems []model.Problem}`；`ImportPlan{Rows []ImportRow; Total int}`；`ImportResult{Plan ImportPlan; Hosts []model.Host}`。动作字符串精确为create/update/error。
- `CSVFileError{Message string}`与`ImportRejectedError{Plan ImportPlan; Message string}`均以指针实现error。前者映射400/null，后者映射422/旧CsvPlan；普通错误继续原RespondError。映射只读字段，不能通过Error()或新辅助回调业务；错误接口本身的Error方法只是返回已有Message，不扩大架构放行。
- 网页保留现有host/csv类型及标签，用纯转换把应用问题交给jsonProblems。dryRun响应取Plan，正式成功取Hosts；不能直接把应用结构注册为公开响应。
- `NewRegistry(Deps) Registry`，`Deps{Hosts port.Hosts; Clock ports.Clock}`。缺仓储或Clock与NewHooks一样立即panic；不在console.New创建默认应用、不增加其他构造副作用。
- `port.Hosts`最终显式声明repository.Hosts原七方法CreateHost/UpdateHost/ImportHosts/DeleteHost/GetHost/GetHosts/ListHosts，签名、返回值和Audit逐字沿用。不嵌入Store，不扩展事务接口。
- 应用包单元测试只用契约替身。所有新增真实数据库、审计回滚、取消及向导竞争用例放`internal/interfaces/console/hosts_contract_test.go`；不让应用测试反向导入基础设施。
- `console.Deps.HostRegistry hosts.Registry`；组装provider固定`hostRegistryOf(st repository.Store, clock console.Clock) hosts.Registry`，只连接st与Clock。tasks/port、authoring/port原主机读取接口保持。
- Task2先交付Registry的五个非Import方法及port五方法；Task3增加真实Import与其所需GetHosts/ImportHosts。每个提交都能运行，不先提交空Import方法。Deps.Hosts临时只供尚未迁移的CSV，Task3删除最后消费者及字段。

## 4. 分任务实施

### Task 1: 固定六入口与CSV兼容基线

**Files:** 新建`docs/contracts/hosts.md`、`internal/interfaces/console/hosts_contract_test.go`；更新`docs/contracts/backend-modules.md`的契约链接。复用`internal/interfaces/console/{hosts_test.go,csv_test.go,selection_pagination_test.go}`及`internal/infrastructure/persistence/pgstore/contract_*_test.go`。

**Interfaces:** 产出第2节的6条固定路由、权限、审计、请求响应与错误顺序基线；没有新应用接口。

- [x] 写`Test主机六入口契约`：手写六条method/path/perm/audit集合，逐条核对；五角色实际经过网页权限检查，读均允许、写仅admin/operator。验证成功内容或稳定错误，不能只断言“不是403”。可复用seedHTTPUser；原真实登录及口令测试完整保留。
- [x] 写`Test主机输入多错保持原顺序`：PUT坏Rev+非法路径SN为400/ConsoleBadRequest；Rev=1+非法路径SN为422/sn_invalid；DELETE溢出Rev+非法SN先400。再覆盖正文与路径不一致、创建空SN422及创建Rev任意仍置1。
- [x] 写`Test主机列表保留两种分页与原文过滤`：SN-1/SN-10/SN-2排序、空items=[]、page的total、游标无total、混用空字段/数字溢出；小写prefix不规范化，属性空白原样精确过滤。
- [x] 写`Test主机CSV预演与失败优先级`：坏dryRun优先于坏CSV；后段列数错误400/null；有效+无效行并关闭读取仓储时500；读取正常时预演200计划、提交422同计划且无记录/审计变化。检查空dryRun及重复参数首值规则。
上述多错用例的固定测试数据至少包含：

```go
cases := []struct { rev int64; path string; wantHTTP int; wantProblem string }{
    {0, "ſn-1", 400, ""},
    {1, "ſn-1", 422, "sn_invalid"},
}
```

- [x] 这是旧行为刻画，先取得基线真实通过，不为制造红灯改旧实现。在lober VM运行：`go test ./internal/interfaces/console -run 'Test主机|TestCSV|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1`；`go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发' -count=1`。数据库用例必须实际运行。
- [x] 提交`test(hosts): pin registry and CSV contracts`，独立核对基线与原代码；发现现有缺陷保留失败，单列兼容修复，不顺手改变迁移范围。

### Task 2: 主机查询与单条写入进入应用

**Files:** 新建`internal/application/hosts/{contract.go,registry.go,registry_test.go,port/repository.go}`；修改`internal/interfaces/console/{hosts.go,server.go,harness_test.go,hosts_contract_test.go}`、`cmd/lober/{app.go,wire.go,wire_gen.go,assembly_test.go}`和`internal/archtest/{migration_test.go,transition_manifest_test.go,authoring_rules_test.go}`。

**Interfaces:** 交付第3节Registry的List/Get/Create/Update/Delete、NewRegistry/Deps和对应port五方法；生产显式注入HostRegistry。旧Deps.Hosts在生产HTTP中仅供CSV使用；现有测试读取口保持，Task3删除字段时统一改为显式store。

- [x] 新增应用测试先红：`Test主机应用权限先于输入`覆盖空Username、无效Role、Disabled、MustChangePassword及五角色组合，坏输入也先授权；测试替身记录仓储和Clock调用数，无权时都为0。`Test主机应用编号版本与整份替换`固定第2.2节先后、空字段清空、创建Rev忽略和错误透传；无新记录校验副本。
权限测试的替身计数名为repoCalls、clockCalls，拒绝后的共同断言为：

```go
if repoCalls != 0 || clockCalls != 0 { t.Fatal(repoCalls, clockCalls) }
```

- [x] 实现五方法，复用NormalizeSN、CheckHostQuery和原仓储。只在原写入位置取Clock，固定host.create/update/delete及空Detail。SN/Rev等前置失败不碰写仓储；记录内容合法性仍由原仓储保证，不把“所有非法记录都不进仓储”误作新契约。
- [x] 五个网页入口改为actorOf→HostRegistry→旧响应转换。列表改用纯parseQueryPagination，不经仍调用仓储校验的queryPagination；应用保持原检查。移除normalizedHostSN生产辅助，queryRev继续只做数字解析/错误响应。CSV入口暂留原路径，不添加许可。
- [x] 补`Test主机应用审计失败与删除引用保护`，经真实HTTP/应用与数据库验证创建、替换、删除审计失败均回滚完整对象/Rev/审计；任何任务状态的引用均409。新增用例放hosts_contract_test.go，复用既有审计失败注入方式。保留Test契约主机、Test契约审计、Test契约并发及默认隔离级别用例。
- [x] 明确组装HostRegistry；test helper显式构造，不在console.New补默认服务。Wire在VM生成并取回具体文件。阶段3删除CRUD仓储7项和normalizedHostSN规则1项，总数277→269；原阶段2与向导清零断言不动，只更新实际固定总数。
- [x] VM先运行新增应用测试证明缺入口/行为的真实红灯，再运行`go test ./internal/application/hosts -count=1`；`go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware'`；`go test ./internal/archtest`及上述数据库聚焦。格式、Wire gen/diff均在VM，成功后提交`refactor(hosts): extract registry application`并独立审查。

### Task 3: CSV预演与整批导入进入应用

**Files:** 新建`internal/application/hosts/{csv.go,import.go,csv_test.go,import_test.go}`；扩展该包`contract.go`与`port/repository.go`；修改`internal/interfaces/console/{hosts.go,csv.go,csv_test.go,hosts_test.go,hosts_contract_test.go,errors.go,server.go,harness_test.go,tasks_test.go,wizard_test.go}`、`cmd/lober/app.go`及`internal/archtest/{migration_test.go,transition_manifest_test.go,authoring_rules_test.go}`。其他编写预览测试仅在实际旧Deps字段引用需要适配时改，保留断言。

**Interfaces:** 增加Registry.Import和port的GetHosts/ImportHosts两方法，完整交付第3节导入值与错误。删除console.Deps.Hosts，所有主机HTTP只依赖HostRegistry。

- [x] 新增Import应用测试先红：授权先于CSV；格式错误不查库；GetHosts在dryRun/行拒绝之前，且空集合仍调用一次；预演/拒绝时Clock和写入调用均0。正式只调用ImportHosts一次，操作者/动作/At可信，返回原输入顺序。
- [x] CSV解析搬入hosts/csv.go，使用原encoding/csv和领域方法。原csv_test.go三组解析断言迁入应用测试；“已存在则update”的断言通过Import预演与可记录GetHosts的替身验证，不能删掉。保留字节BOM、物理行2/4、属性` A01 `和含真实换行的note值、全部重复行及六种问题顺序。
- [x] `Test主机CSV重复与首错次序`固定输入SN-1、sn-1、占位编号、坏平台、坏文本；所有重复位置首问题host_duplicate_in_batch，后续问题顺序不变。`Test主机导入预演后覆盖最新版本`：预演create后由第二连接先创建，正式导入返回Rev=2及本次内容；再次导入继续加Rev，不增加CSV旧版本拒绝。
原逐行样例的前六行首问题固定为：

```go
wantCodes := []string{"host_duplicate_in_batch", "host_duplicate_in_batch", "sn_placeholder", "host_platform_invalid", "text_not_storable", "text_not_storable"}
wantRows := []int{2, 4} // BOM与引号内换行样例的物理行
wantNote := "line1\nline2"
```

- [x] 实现解析→有效编号GetHosts→标记计划→dryRun/拒绝→原子ImportHosts，遵守第2.3节。网页仍解析dryRun、读字节，转换旧CsvPlan/HostPage；CSVFileError/ImportRejectedError在RespondError只读字段映射。删除旧parseHostCSV生产桥，console/csv.go仅保留原公开类型及纯转换。
- [x] `Test主机导入审计取消整批回滚`用真实数据库在中间行/审计失败、锁等待取消后比较整批原记录、Rev和审计不变；成功批次按SN-2/SN-1输入返回并审计。补`Test主机变更与向导提交按数据库事实交错`两子例：①主机应用先将SN-1从x86_64改为aarch64，在事务审计处用测试schema的SQL门闩暂缓提交；向导按旧事实预读后等待主机锁，放行主机提交后，任务创建应422、byHost[SN-1]首项arch_mismatch，任务与task.create审计均0。②向导先持主机锁并暂缓任务审计提交，主机应用导入在行锁等待；先放行任务，再导入aarch64，任务成功且快照Host.Arch仍x86_64，登记主机已为aarch64。用独立连接、进入信号、数据库阻塞证据和审计触发器门闩建立先后，不用sleep或进程锁冒充阻塞。
- [x] 删除Deps.Hosts并调整harness：测试store接口显式包含repository.Hosts，Registry、Wizard与AuthoringEditor各自从store取得所需读口，不借主机服务或恢复网页仓储。CurrentTaskCreator覆盖及唯一模板选择锁仍保持；主机模块不新增进程锁。关闭读取仓储的CSV失败测试改注入真实HostRegistry；任务冻结预览测试也把新主机入口及编写读口指向关闭store，完整结果比较不减。
- [x] 删除导入两项仓储、Deps字段一项及CSV五项规则，269→261。两类阶段3累计正好10+6=16；不删除其他阶段项。同步阶段2固定总数检查，继续保证阶段2/向导为0。
- [x] VM执行`go test ./internal/application/hosts -count=1`、`go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware'`、`go test ./internal/archtest`；数据库执行`go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test同时导入主机与建任务不会互相等死|Test同时导入同一组主机不会互相等死|Test写事务不随连接默认的隔离级别变' -count=1`。成功后提交`refactor(hosts): move CSV import behind contract`，独立审查事务、错误先后和测试注入。

### Task 4: 关闭主机边界并完整验收

**Files:** 新建`internal/archtest/hosts_rules_test.go`、`docs/superpowers/reviews/2026-10-23-ddd-phase3-evidence.md`；修改`internal/archtest/wizard_helpers_test.go`、`docs/contracts/{hosts.md,backend-modules.md}`、`docs/superpowers/plans/2026-10-22-backend-ddd-roadmap.md`和本计划检查框。两张许可源码清单已由Task2/3逐条清理，本任务只读核对，不为凑数量再删其他阶段。

**Interfaces:** 迁移根增加hosts.go、csv.go，仅登记hosts.Registry的六方法；向导/编写与正式六项编号能力、唯一通知桥都不扩容。

- [x] 先写类型有效的反例并在VM证明红灯：两个主机文件各覆盖直接仓储、局部别名、方法值、包级别名、两层旧辅助、旧响应转发、局部小接口、Host.Validate/NormalizeSN调用、错误转换夹带业务、通知/任务诊断、相似应用接口和未登记Registry方法。每例先通过Go类型检查，再进入既有typedTransitionFindings/wizardHelperFindings。
- [x] 扩展原根集合与精确方法允许表，沿同包真实符号追踪；纯HostResponse/CsvPlan转换和Registry六方法为正例。保留向导12反例、编写六文件正反例，夹具不能因新增常驻违规使“允许例”变失败，不能以取消原断言处理。
- [x] 新增`Test主机许可已清零`：阶段3为0；原阶段1/2、向导继续0。固定四清单88/125/47/1，总数261；统计真实清单变量，不误删migration_test.go用于格式/匹配检查的合成阶段样例；逐行比较起点只删10项仓储和6项规则，其他阶段新增/删除均0。保留正式能力与唯一通知转发的原检查。
- [x] 在VM执行根目录`make check`，原`go test -race -timeout=20m ./...`不改；再执行`pnpm --dir web check:console-types`，公开名称/生成结果无差异。没有前端业务修改时不重复全量前端构建；有实际改动才补相应验证。
- [x] 保存真实环境、精确树、命令、退出码、耗时、缓存/跳过项及失败修复。真实数据库必须执行；realISO、一次性发布记录及整机验收未执行就明确写出。不把前阶段成功或工具准备错误当成本阶段通过。
- [x] 完整验证通过后形成本地候选`refactor(hosts): enforce phase-three boundaries`，供独立Task4与整分支审查核对固定范围；两道审查通过才标记模块交付。计划状态补记只改文档时，用精确差异证明源码仍等于被测树，无需重复相同全套。
- [x] 整分支审查逐项关闭六入口、CSV两种结果、数据库契约及五项Review Focus。先修阻塞问题，日志等非阻塞项明确留存。交付codex/ddd-hosts；develop及后续环境按仓库流程另行推进。

## 5. lober虚拟机执行约定

1. 私有环境依据`E:\src\lober\docs\linux-test-environment.private.md`。连接参数仅静默读取所需部分，不把全文、筛选片段、口令、地址、密钥路径或连接串输出到工具日志、文档或Git。先确认hostname=lober。
2. 使用第三阶段独立目录，例如`/home/dev/lober-ddd-phase3-hosts-20261023`。不覆盖运行服务、产品库、制品或前两阶段证据。测试连接只用已核实的lober_test；测试夹具创建临时schema，私有连接文件忽略且权限600。
3. 传输精确Git归档并重建相同索引；显式保留已跟踪而被忽略的嵌入占位文件及文件模式。先核对树，再放连接文件。控制脚本用UTF-8/LF字节传输，不上传整个Windows工作目录。
4. 固定Go1.27.1、GOMAXPROCS=2、GOFLAGS=-p=2、GOPROXY=off、LOBER_TEST_BASH=/bin/bash、LOBER_TEST_BUSYBOX=/usr/bin/busybox；Node24.16.0/pnpm11.16.0用VM已有环境，不改全局配置或安装依赖。
5. 修改的Go文件只在VM执行gofmt；Wire使用`WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire gen ./cmd/lober`和`WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober`。取回工具实际修改的具体文件，重新暂存并验证新树；不得手写wire_gen.go。
6. 每任务先新增聚焦失败再实现；Task1旧行为基线先取真实通过。中间按任务聚焦及架构检查，Task4执行原完整make。相同源码、依赖和环境可以复用有效缓存；强制全模块发现仍按make的-count=1执行。
7. 第二阶段合并后的网页并发包实际844.374秒，原上限20分钟。本阶段不改真实密码强度、已有测试准备缓存、时限或断言。实际失败先保留日志/堆栈定位；不预先优化或跳过。开跑检查磁盘和其他Go进程，不清理其他人的目录或缓存。

## 6. 回退与自审

- 本阶段不预先宣称修复旧并发缺陷。若真实测试发现缺口，先稳定复现并独立提交兼容修复，注明是否依赖本阶段接口，不能借搬迁改变业务。
- 回退以第三阶段完整提交序列为界；保留第二阶段编写接口、共享选择锁、第一阶段CurrentTaskCreator及数据库身份锁。未解决的整批回滚、引用或任务并发缺陷阻止验收。
- 已按设计第1/5/7/9/10节对应约束、接口、事务与自动检查；五项Review Focus分别落到Task1/2、Task1/3、Task1/3、Task2/3、Task2/3，不留无测试承接的重点。
- 已核对接口接力：Task2五方法可独立交付，Task3才增加真实Import与两读写方法；最后消费者迁完再删Deps.Hosts，无空服务或新增临时许可。
- 已核对公开类型与错误转换：网页仍使用原DTO，应用不携带网页标签；typed错误映射只读字段，不能因新错误类型泛许应用方法。
- 已逐项核对阶段3为10+6项，277→261；阶段2原总数断言随每项实际删除同步调整，不能把历史快照当成不许继续清理的固定终态。
- 初次编写计划时未运行测试、构建、gofmt或Wire。用户批准后的实际实施与验收见[第三阶段证据](../reviews/2026-10-23-ddd-phase3-evidence.md)；四项任务、逐项独立审查及整分支审查均已完成，模块分支验收通过。