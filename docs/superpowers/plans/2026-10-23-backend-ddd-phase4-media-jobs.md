# 后端四层迁移第四阶段：镜像与后台作业实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 镜像的十一条网页入口通过应用契约执行；来源获取、上传、导入、删除和后台恢复各有明确归属；清除第四阶段154项临时放行，保持现有接口、持久化结果和文件保护。

**Architecture:** 接口层负责解析和响应；`application/media` 组织业务、审计、状态和恢复；`domain/media` 保存纯内存的镜像判定。`infrastructure/media` 只实现文件、网络、格式读取、工具运行和清理日志等能力，不再取得仓储或决定业务状态。后台调度从启动目录移入镜像应用，启动目录只组装和启停。

**Tech Stack:** 沿用 Go、Gin、PostgreSQL、现有 Wire 和 pnpm 工具；不加队列服务、数据库表、依赖库或配置项。

**Spec:** [已批准四层设计](../specs/2026-10-22-backend-ddd-four-layer-design.md)、[阶段路线](2026-10-22-backend-ddd-roadmap.md)、[模块契约](../../contracts/backend-modules.md)。

状态：用户已确认，第四阶段按“逐项实施、逐项独立审查、最终整分支审查”开始执行。Task1核对源码发现下文原写“admin/operator可写”有误，已更正为现有的“仅admin可写”；不修改实际权限。任务完成状态与真实验证结果另记，不能把本计划当作验收完成。

## Global Constraints

- 设计第1节：“本次重构保留现有业务行为、数据库约束、数据格式和对外接口。”不改镜像格式支持、角色、分页、任务控制、审批选择、冻结预览或已有机器安装能力。
- 设计第5节：“应用模块不能直接导入另一个应用模块的具体服务。”镜像模块自己的能力放在 `application/media/port`；不把全部接口塞进共同 `ports`。
- 设计第6节：“不能只写方法签名就称为契约完成。”每个操作写明调用身份、输入、结果、错误顺序、写入边界、重复调用、并发、审计及取消行为。
- 设计第7节：“不引入可以执行任意数据库操作的通用事务回调，也不把数据库事务对象交给上层。”继续使用 `SaveSourceAndAcquire` 等现有原子操作。
- 设计第7节：“审计与业务写入在同一事务。提交成功后才能发通知；事务失败不发通知。”本模块的 Wake 只是唤醒恢复扫描；持久化事实才是工作依据，不增加对外事件或必达承诺。
- 设计第9节：“已迁移模块不得再调用旧业务流程。”清除154项时不能改阶段、改名挪表、把125项旧流程许可改成永久许可，或扩大其他阶段访问。
- 所有 Go、gofmt、Wire、Node 类型生成及测试都在 lober VM 执行，按用户指定的私有环境文档连接。本机仅做阅读、Git、文档和安全传输；不打印凭据，不提交私有连接文件。
- 保持原 `go test -race -timeout=20m ./...`，不降口令强度、不改测试准备缓存、超时或断言来换通过。发现旧缺陷时保留复现，单列修复及行为影响，不把搬迁当作修复证明。
- 领域没有文件句柄、`io/fs`、网络、工具、系统路径、时钟或随机读取。应用也不直接 `os.Open`、发网络请求或启动工具；通过自己的能力接口取得结果。取消可传标准 `context.Context`。

## Review Focus

1. 创建后第二个来源登记失败：保留已经提交的镜像、前面成功的来源、审计和 Location；不改成假原子创建，也不能丢失可恢复的编号。Task1/2补网页及真实库断言。
2. 最后一片上传、取消与完成发布相遇：错误字节、被取消的旧版本不能变成完成记录；完成响应丢失后重发相同字节仍可成功。Task3用屏障控制顺序，检查字节、区间、Rev和状态。
3. 文件已发布但数据库提交结果未知：只有确认同摘要、同制品、ready才当成功；结果未知时保留文件，不能回滚删掉可能已提交的制品。Task4保留并加强丢失确认测试。
4. 删除与补来源相遇、删除提交结果未知、清理遇到其他内容：快照必须覆盖登记来源；确认记录仍存在或结果未知时不删字节；本地原件、符号链接外目标、其他镜像和其他拥有者目录不受损。Task5做两方向竞争及重启重试。
5. 启停与后台异常相遇：上传来源不被当成远程下载；重复 Start 不多开循环，Close 停止并等待作业；异常释放工作名额后按原规则退避，不泄露日志中的用户内容。Task5补生命周期和资源断言。

## 起点、范围与回退

- 集成分支是用户明确的 `dev`，起点 `48f6c2c1f1ee4aebdd0349868a097891e5c9ad13`，树 `c9126ced8686cd56674ddb89a707c86b97e23675`。第三阶段实际合并 `f6a20d0` 的源码与该起点相同，只有收尾文档不同；[合并验证](../reviews/2026-10-23-ddd-phase3-dev-integration.md)已在 lober VM 通过。
- 模块分支 `codex/ddd-media` 从上述 dev 建立，复用干净隔离工作区。原主目录、dev未提交网页文件及前三阶段分支不参与本轮修改。历史 `develop` 不作为新阶段集成目标。
- 执行前重新核对 dev 与模块分支。dev 若前进，先保留新提交并受控集成，再重核契约、许可和虚拟机基线；禁止硬重置或覆盖新功能。
- 本计划交付验收通过的模块分支。合入 dev 要另核对当时实际提交并验证合并结果；不含推送、test企业套件、preview人工验收或main发布。
- 回退第四阶段完整提交序列，保留前三阶段应用入口、主机保护、当前模板选择锁、真实任务控制及冻结预览。独立缺陷修复须单列提交，并注明是否可脱离本阶段保留。

## 现有代码与契约基线

| 现有位置 | 当前职责及迁移要求 |
| --- | --- |
| `internal/interfaces/console/isos.go`、`isosource.go` | 十一条路由，创建预检、余量、进度重读、上传范围与旧能力约定；迁到应用后只剩解析、身份和返回转换 |
| `internal/infrastructure/media/{iso,sources,source,remote,local,upload,recover}.go` | 获取状态、来源登记、取消、重试、恢复与文件操作混在一起；状态与仓储流程进入应用，字节操作保留 |
| `internal/infrastructure/media/{import,identify,suse,netboot,extract,windows}.go` | 导入编排、识别、提取、发布及提交结果核对；分开业务判定和外部操作，不重写镜像解析器 |
| `cmd/lober/supervisor.go` | 工作名额、去重、退避、恢复扫描和唤醒；整体职责迁入镜像应用 |
| `cmd/lober/deletion.go` | 删除快照、登记闸门、数据库删除及清理重试；业务流程进入应用，持久化日志进入基础设施 |
| `domain/repository/{isos,sources}.go`、`pgstore` | ISO与来源的版本、审计、区间合并和引用保护；接口含义、表结构及事务不变 |
| `infrastructure/media/{iso9660,wim,ssrf,bootconfig}*` | 格式和外部能力，保留真实测试；启动配置读取继续满足编写与任务自己的只读能力 |

所有现有“第4层/layer4”注释指历史镜像模块，不等同于目标基础设施职责。迁移不能只改包名。

### 网页入口和兼容要求

前缀 `/console/v1/isos`。五角色可读；仅admin可写，operator及其余三角色写操作403，依据为现有access.PermsOf与TestISO所有接口五角色权限。原会话、禁用账号、无效角色和必须改密检查保持。应用入口再次使用 `ports.Actor.Authorize` 和现有 `access.PermISORead/PermISOWrite`，拒绝后不读库、不取时钟、不读请求流。

| 入口 | 迁移后应用方法 | 保持的结果、失败与审计 |
| --- | --- | --- |
| GET `/` | List | 两种分页、family/series/state筛选；空items为数组，next及total出现条件不变；无审计 |
| GET `/:id` | Get | 原对象与404；artifacts/offers为空对象；无审计 |
| POST `/` | Create | 校验名称/族/系→来源按输入顺序预检→唯一角色及恰好一个主来源→余量→CreateISO→逐条登记；创建后即使登记失败也带Location；各步沿用iso.create |
| POST `/:id/sources` | RegisterSource | 先读现有ISO，再预检/余量/登记；失败镜像补来源的排队和审计同事务；iso.source.register |
| GET `/:id/progress` | Progress | 返回ISO的id/state/rev及来源的role/kind/totalSize/fetched/state/error/rev/ranges；保持来源顺序；每来源区间读取后重读Rev，最多3轮，否则409；无审计 |
| PUT `/:id/content` | Upload | 二进制流；保持Content-Range严格语法、ContentLength及实际长度检查；重复一致片可接受，不一致或越界不能污染确认字节；网页分片仍无审计 |
| POST `/:id/cancel` | Cancel | 原只允许acquiring/failed；逐来源提交，不虚构整批原子；最多16轮版本冲突重读；先持久化cancelled再取消正在读取的上下文；iso.cancel |
| POST `/:id/retry` | Retry | 只重置failed/cancelled，已fetched不动；零进度与区间重置由原事务保证；文件暂存/恢复与审计失败约定保持；iso.retry |
| POST `/:id/reimport` | Reimport | 只接受failed且所有必需来源已完成；先原子保存排队事实和iso.reimport审计，再唤醒，不在网页请求中做提取 |
| POST `/:id/deprecate` | Deprecate | 正rev解析后按原版本和状态检查；仅ready可废弃；iso.deprecate；事务内保护保持 |
| DELETE `/:id` | Delete | 正rev、快照持久化、审计删除；有任务引用仍409；快照失败不删除；提交后清理失败仍成功，日志留待恢复；iso.delete |

路由、描述类型、字段名、状态码、问题代码和中文消息不改。非法范围使用原 `ConsoleContentRange`；普通输入400、缺失404、版本/状态/引用409、业务问题原422、内部错误500且不暴露路径或连接信息。不得用错误文字判断类别。

来源外部kind仍为 `url/path/upload`，内部仍为remote/local/upload。upload文件名只是标签，落库地址清空；size必须正数；摘要仍只接受64位小写十六进制。未知大小的url仍可登记，不臆造非零大小。额外角色只接受当前系登记的角色。

余量按非path来源size累加，溢出400；总数0不查余量，只比较staging，不改为“两目录都要预留”。本地原件就地读，不复制、不删除。创建不是跨来源原子事务；来源排队、分片区间及各审计写入保持各自现有事务边界。

### 内部契约交接

以下是本计划确定的目标名字和类型。执行者应先读本节及已批准设计；Task1把行为整理为 `docs/contracts/media.md`，Go接口和旁注才是内部能力的唯一实现来源。

`application/media/contract.go`：公开 `Registry`，十一方法均以 `ctx context.Context, actor ports.Actor` 开头：

```go
List(ctx, actor, ListRequest) (ISOPage, error)
Get(ctx, actor, id string) (model.ISO, error)
Create(ctx, actor, CreateRequest) (CreateResult, error)
RegisterSource(ctx, actor, RegisterRequest) error
Progress(ctx, actor, id string) (Progress, error)
Upload(ctx, actor, UploadRequest) error
Cancel(ctx, actor, id string) error
Retry(ctx, actor, id string) error
Reimport(ctx, actor, id string) error
Deprecate(ctx, actor, RevisionRequest) error
Delete(ctx, actor, RevisionRequest) error
```

上块为签名简写：ctx和actor的完整类型如前句，不是可直接复制的Go接口声明。构造 `NewRegistry(Deps) Registry`；Deps显式注入 `ISOs port.ISOs`、`Sources port.Sources`、`Control port.SourceControl`、`Deletion port.Deletion`、`Clock ports.Clock`。缺项在组装时失败，不在console.New补默认服务。

| 类型 | 精确字段及意义 |
| --- | --- |
| ListRequest | `Page, Limit int; Cursor string; Family catalog.Family; Series catalog.Series; State model.ISOState` |
| ISOPage | `Items []model.ISO; Next string; Total *int64` |
| SourceSpec | `Kind, Addr, Role, SHA256 string; Size int64`；在media包中定义，网页类型显式转换 |
| CreateRequest | `Name string; Family catalog.Family; Series catalog.Series; Sources []SourceSpec` |
| CreateResult | `ISO model.ISO; Created bool`；CreateISO提交后就置true，后续登记错误仍带此结果；接口层据此先写Location再映射错误 |
| RegisterRequest | `ID string; Source SourceSpec` |
| RevisionRequest | `ID string; Rev int64` |
| UploadRequest | `ID, Role string; Start, End, Total, ContentLength int64; Body io.Reader`；Total=-1表示范围总长为*，ContentLength=-1表示未知；End为闭区间末端 |
| Progress | `ID string; State model.ISOState; Rev int64; Sources []SourceProgress` |
| SourceProgress | `Role string; Kind model.SourceKind; TotalSize, Fetched int64; State model.SourceState; Error string; Rev int64; Ranges []model.Range`；不用旧missing进度替代网页已确认ranges |
| RangeError | 有具体 `Error() string` 方法的类型；只用于原uploadChunk的范围/总长/ContentLength预检错误，映射原ContentRange消息；读取后才发现正文短/长仍沿用ErrInvalid及原普通400消息，不扩大归类；没有网页标签和状态码 |

`port/repository.go` 分开显式声明 `ISOs` 的六个方法 `CreateISO/UpdateISO/DeprecateISO/DeleteISO/GetISO/ListISOs`，以及 `Sources` 的七个方法 `SaveSource/SaveSourceAndAcquire/GetSource/SourcesOf/ListUnfinishedSources/SaveRange/RangesOf`，完整签名分别与现有 `repository.ISOs/ISOSources` 一致。`DeleteSource` 当前流程不用，不注入；不可嵌入完整Store或为凑接口加入未用方法。

`port/control.go` 的 SourceControl保持现有受信任内部签名：`Register(context.Context, model.ISO, string, string, string, int64, string, repository.Audit) error`、`Upload(context.Context,string,string,int64,io.Reader,repository.Audit) error`、`Cancel/Retry/Reimport(context.Context,string,repository.Audit) error`、`FreeSpace(context.Context)(int64,int64,error)`。Deletion为`Delete(context.Context,string,int64,repository.Audit) error`。它们仅由Registry及组装调用，网页不再持有。Task2暂接现有实现，Task3至5替换真实实现，不增加新空服务或旧流程许可。

`NewSources(SourceDeps) *Sources` 为同一镜像应用内的获取实现；保留Register与RegisterQueued两种内部语义，二者完整参数与旧实现一致，后者原子排队失败镜像。另有`FetchOne(context.Context,string,string) error`、`Recover(context.Context)([]ResumeWork,error)`、`RequestReimport(context.Context,string,repository.Audit) error`，只给受信任后台及Registry控制适配器。SourceDeps包含ISOs、Sources、Transfer、Locks、Gate，Import及删除分别在后续任务接入。ResumeWork含`ISOID,Role string; Kind ResumeKind`，Kind只有fetch/import；不得把upload的fetch条目交给FetchOne。

最终控制构造为`NewControl(*Sources, port.Waker) port.SourceControl`，Waker只有`Wake()`。调用图为Sources/Deletion→Supervisor→Control→Registry；Control.Register调用RegisterQueued，Control.Reimport调用RequestReimport，成功后Wake。Sources的登记入口持登记读闸门，Task3同时删除旧supervisor.Register外层重复持锁，避免有写者等待时嵌套读锁造成阻塞。

`port/coordination.go` 定义同一实例的 `SourceLocks`：`Lock(id,role string) func()`、`Track(id,role string,cancel context.CancelFunc) func()`、`Cancel(id,role string)`；Track返回注销函数。定义 `RegistrationGate` 的 `Lock/Unlock/RLock/RUnlock`。同步实现放 `infrastructure/media/coordination.go`，不保存仓储。Task3过渡期间老Import/CleanupDeleted与新来源流程必须共用这两项能力；最终仍每个staging目录仅一个运行实例，不声称跨进程共享目录安全。

### 文件和网络能力的粒度

`port/transfer.go` 以可关闭会话隔离外部操作：`Transfer`含 `ValidateSource(context.Context,model.Source)(model.Source,error)`、`BeginFetch(context.Context,model.Source)(FetchSession,error)`、`BeginPublish(context.Context,model.Source)(FetchSession,error)`、`BeginUpload(context.Context,model.Source,int64,io.Reader)(UploadSession,error)`、`ParkRetry(context.Context,model.Source)(RetryFiles,error)`、`FreeSpace(context.Context)(int64,int64,error)`。验证仅归一化经过允许根检查的local路径，url逐次解析和连接保护仍由网络实现保证。

- FetchSession：`Resume(context.Context)(int64,error)`恢复发布中断并报告真实持久化长度；应用先保存修正的Fetched。`Open(context.Context)(FetchInfo,error)`开始受保护网络/本地读取；FetchInfo为`Offset,TotalSize int64; Complete, Reset bool`，包括服务器拒绝断点后的归零。应用按旧顺序保存新计数，再循环`Step(context.Context)(offset int64, done bool, err error)`；只报告已经同步的绝对字节位置。err与新offset同时返回时，仍先尝试保存已同步进度，不能吞掉读错误。Open遇到URL安全拒绝时沿用旧行为删除托管part，并返回Reset=true的零进度事实及错误，应用保存计数后再返回原问题；不能只删文件不修正持久化进度。`Verify(context.Context)(VerifiedContent,error)`报告普通文件的实际大小及摘要并检查读取前后稳定，VerifiedContent为`Size int64; SHA256 string`；应用比对预期、重读来源仍fetching且Rev相同后，才调用`Publish(context.Context) error`重命名。`Restore(context.Context) error`在来源最终保存失败时恢复part；`Close() error`释放句柄及网络响应，不删除有效断点。
- UploadSession：BeginUpload只写受大小限制的临时片，错误不得触碰确认文件；`CheckOverlap(context.Context,[]model.Range) error`检查真实字节；`Write(context.Context) error`写入并同步；`VerifyReplay(context.Context) error`核对完成后重放；`Close() error`关闭并移除本次临时片。应用负责调用SaveSource、SaveRange、重读来源及全片齐备后的最终发布；完成发布复用FetchSession的本地发布能力，构造入口为`Transfer.BeginPublish(context.Context,model.Source)(FetchSession,error)`，不得发网络请求。
- RetryFiles：`Restore() error`恢复临时停放字节；`Commit() error`在重置持久化成功后移除停放文件；只有非local来源参与。原始文件、区间和审计失败行为按现有测试保留。
- `port.Fault`是外部失败事实，字段`Kind FaultKind; Role, Expected, Actual string; Cause error`，有Error/Unwrap；Kind精确列为URLRejected、PathRejected、ChunkRejected、ContentMismatch、PublicationConflict、InvalidFile。应用将其转换为原ProblemsError/稳定错误；基础设施不导入repository、不自己设ISO/Source状态、不创建业务Problem。

`port/import.go` 定义 `ImportFiles.Open(context.Context,model.ISO)(ImportSession,error)`，会话的`OpenSource(context.Context,model.Source)(MediaView,error)`按应用指定顺序打开主源和额外源；MediaView提供`Stat(context.Context,string)(Entry,error)`、`Text(context.Context,string,int64)(string,error)`、`WIM(context.Context,string)([]mediarules.EditionFact,error)`、`Digest(context.Context)(string,error)`。Entry只有`Size int64; IsDir bool`，不存在用标准fs.ErrNotExist表示；不把fs.FS、os.File、绝对路径或工具句柄交给应用/领域。

ImportSession还含`Prepare(context.Context,ExtractionPlan)(map[string]string,error)`、`Verify(context.Context) error`、`Publish(context.Context)(Publication,error)`、`Rollback(context.Context,Publication) error`、`Close() error`。ExtractionPlan含`ISO model.ISO; Identified mediarules.Identified; RepoPaths []string; RequiredRoles []string`；Publication含`Artifacts map[string]string; MadeArtifacts,MadeSMB bool`，会话内部保留具体根路径和owner。Prepare只提取/复制临时树；Verify在所有消费者完成后重核全部已打开来源；Publish保持SMB先于制品的顺序；Rollback只撤销本会话新发布的树。Close释放媒体与临时树，不能代替应用决定删除已发布内容。发布部分成功再失败时也必须返回Publication，使应用能按数据库事实处理。

`domain/media`（别名mediarules）新增纯规则：`Identified{Format catalog.Format; Series catalog.Series; Version string; Arch catalog.Arch; Editions []model.Option; RepoRoot string}`、`EditionFact{Name,Flags,Version string; Arch catalog.Arch}`；`CheckIdentified(Identified) error`复用原版本模式与标签规则，`CheckImport(model.ISO,Identified,[]model.Source) error`复用来源和平台条件，`AllFetched(model.ISO,[]model.Source) bool`复用当前必需角色规则，`CheckDeletion(model.ISO,[]model.Source) error`复用旧CleanupDeleted的ID/路径组件、来源归属与角色唯一检查。后者仍允许acquiring/failed缺Version/Arch，校验时沿用pending/x86_64临时值，不把这些值写回对象。目录/文本读取顺序保留在应用识别编排，事实解析和条件在领域，不能把fs.FS搬进领域。catalog已有规则继续调用唯一实现。

`port/deletion.go` 定义`DeletionSnapshot{ISO model.ISO; Sources []model.Source}`，序列化字段及内容与旧格式一致。`DeletionJournal`含`Persist(context.Context,DeletionSnapshot) error`、`List(context.Context)([]DeletionSnapshot,error)`、`Discard(context.Context,string) error`；List逐项验证文件，返回已解析的有效快照及汇总错误，坏文件不能阻止其余快照恢复。`DeletedFiles.Cleanup(context.Context,DeletionSnapshot) error`只执行受控字节清理。应用在清理前确认记录已删除并持有对应import/来源锁；适配器内部仍保护目录拥有者、路径和local原件，重复执行安全。

`NewDeletion(DeletionDeps) *Deletion`实现内部Delete与`RetryAll(context.Context) error`；Deps为ISOs、Sources、Journal、Files、Locks、Gate、Logger。`NewSupervisor(context.Context,SupervisorDeps) *Supervisor`提供Start() error、Wake()、Close()；Deps为恢复/FetchOne/Import小接口、Sources只读接口、Deletion.RetryAll能力、Logger、Workers和PollInterval。日志用注入的slog.Logger，退避时钟只在应用；新Clock能力仅用于审计，不挪到领域。

## 文件职责与任务依赖

| 任务 | 文件归属 | 可独立审查的交付 |
| --- | --- | --- |
| 1 | `docs/contracts/media.md`；现有console/cmd/media/pgstore测试及新增兼容测试 | 固定当前行为、失败顺序和持久化边界，不开始搬流程 |
| 2 | 新建`application/media/{contract,registry,progress,upload_request}.go`和`port/{repository,control}.go`；console及app/Wire接线 | 十一网页入口走真实应用契约，内部暂用现有已登记实现 |
| 3 | 新建`application/media/{sources,fetch,upload,retry,recover}.go`、`port/{coordination,transfer}.go`；基础设施同名文件拆外部操作 | 来源、获取、上传、重试、取消和恢复由应用负责；网络文件实现不写数据库 |
| 4 | 新建`domain/media/{rules,identify}.go`、`application/media/{import,identify,suse,netboot}.go`及`port/import.go`；修改基础设施识别/提取文件 | 导入及提交结果核对由应用负责，格式读取与发布由能力实现承担 |
| 5 | 新建`application/media/{deletion,supervisor}.go`、`port/deletion.go`、`infrastructure/media/{deletion_journal,coordination}.go`；cmd生命周期和Wire | 删除可重启恢复，调度退出完整；旧Sources和cmd业务编排清除 |
| 6 | 新建`archtest/media_rules_test.go`及阶段证据；更新原守卫、契约、路线与计划 | 第四阶段154项清零、其他阶段不变，完整VM验收和独立整分支审查 |

依次执行1→2→3→4→5→6。每项交付可构建、可运行且有真实消费者；临时接力只复用仍有登记的旧实现，不新增空方法、默认服务、通配许可或复制业务实现。Task3至5共享锁，不能在切换中建第二套fileLocks/active。

### Task 1：固定镜像与作业契约，补齐危险输入基线

**Files:** 新建`docs/contracts/media.md`、`internal/interfaces/console/media_contract_test.go`；修改`isos_test.go`、`cmd/lober/{assembly,deletion,queue_atomicity,supervision}_test.go`和必要的`internal/infrastructure/media/*_test.go`；数据库补充放`pgstore/contract_isosources_test.go`。本任务不改生产流程。

**Interfaces:** 消费现有十一入口、repository.ISOs/ISOSources、iso.Sources和启动组装；产出行为契约与基线测试，不先建立未使用的应用骨架。

- [ ] 逐条记录上文八类契约和十一入口；核对真实源代码、路由描述及数据库实现。区分网页sourceProgress的已确认ranges与旧Sources.Progress的missing，明确审计和多来源非原子边界。
- [ ] 补`Test镜像创建后来源失败保留位置及已提交事实`：第二来源失败时非200、Location为已创建ID；镜像与第一来源仍在，失败来源不存在，审计只包含已提交操作。补非法输入与有效但被网络安全拒绝的来源之错误优先顺序。

本例在Task1用真实来源实现，不用只记录调用的mock来证明保存结果。给现有测试文件加入媒体包别名iso；临时目录无允许本地根，所以第二来源会在实际登记时被拒绝：

```go
func Test镜像创建后来源失败保留位置及已提交事实(t *testing.T) {
    s, st, _, _, token := newISOServer(t)
    s.server.deps.ISOSource = iso.New(st, iso.Dirs{
        Staging: t.TempDir(), Artifacts: t.TempDir(),
    })
    w := httpCall(s, "POST", "/console/v1/isos", `{"name":"partial","family":"linux","series":"debian","sources":[{"kind":"upload","size":4},{"kind":"path","addr":"/outside/netboot.iso","role":"netboot"}]}`, token)
    id := strings.TrimPrefix(w.Header().Get("Location"), "/console/v1/isos/")
    if w.Code != 422 || id == "" { t.Fatal(w.Code, w.Body.String()) }
    item, err := st.GetISO(t.Context(), id)
    if err != nil || item.State != model.ISOAcquiring { t.Fatal(item, err) }
    src, exists, err := st.GetSource(t.Context(), id, "")
    if err != nil || !exists || src.State != model.SourcePending || src.TotalSize != 4 { t.Fatal(src, exists, err) }
    _, exists, err = st.GetSource(t.Context(), id, "netboot")
    if err != nil || exists { t.Fatal(exists, err) }
    audits, err := st.ListAudit(t.Context(), repository.AuditQuery{TargetKind:"iso", TargetID:id, Action:"iso.create"})
    if err != nil || len(audits.Items) != 2 { t.Fatal(audits, err) }
}
```

两条审计分别来自创建与主来源登记。第二来源没有成功写入，不能额外记一条。Task2改为通过Registry接入同一真实能力，Task3再迁移构造位置；保留这些断言。

- [ ] 补`Test镜像多错仍按原先顺序返回`：坏rev先于删除读取；补来源先查ISO再查来源字段；上传先范围语法再来源键、存在性、kind和边界。给出具体400/404/409或问题代码断言，禁止只测“有错”。
- [ ] 在VM运行既有与新增点名测试：`go test ./internal/interfaces/console ./cmd/lober ./internal/infrastructure/media ./internal/infrastructure/persistence/pgstore -run 'Test(ISO|镜像|Source|ISOSources|契约ISO|契约审计|删除快照|补来源|重新导入|后台|Import|Upload|Remote|Worker|Recover)' -count=1`。预期当前行为全通过；纯兼容补测无需人为制造红灯。若发现原缺陷，先保留失败事实，单列修复后再继续。
- [ ] 独立审查契约、测试断言及范围；提交`test(media): pin media and job compatibility contracts`，只暂存本项文件。

### Task 2：十一网页入口改用镜像应用契约

**Files:** 新建上文Registry、类型及port文件和`application/media/{registry,progress,upload_request}_test.go`；修改`interfaces/console/{isos,isosource,errors,server,harness_test,isos_test}.go`、Task1网页测试、`cmd/lober/{app,wire,wire_gen}.go`和必要组装测试；逐项更新`archtest/{migration,transition_manifest,authoring_rules,hosts_rules}_test.go`。

**Interfaces:** 消费Task1固定的外部行为；实现上文十一方法及类型。SourceControl与Deletion暂接现有supervisor/journal，仓储通过6+7显式方法注入。产出所有网页流量的唯一媒体入口，后续任务只替换内部能力，不再改网页业务行为。

- [ ] 先写新应用接口测试并在VM运行，预期缺少media.Registry而编译失败。覆盖五角色，拒绝后仓储、时钟、FreeSpace与Body读取均零调用；覆盖CreateResult与错误同时返回、余量累加溢出、进度3次重读耗尽和流式确长检查。
- [ ] 实现Registry，中文旁注写清每个操作的错误顺序、时钟/审计、取消与持久化范围。创建和来源登记审计保持原action和空TargetID传入方式，由原仓储补目标；分片传零Audit。CreateResult不能因error而被通用Get/Write丢弃。
- [ ] 网页只保留JSON/查询/Content-Range语法解析、媒体类型描述、actorOf及响应转换；`exactChunkReader`、状态预检、余量和进度循环迁入应用。保持ParseContentRange现有行为；新增RangeError转换只覆盖旧预检条件，正文实际长度不符仍保留原ErrInvalid消息，不让其他ErrInvalid误报范围消息。
- [ ] 组装显式构造NewRegistry；新依赖为空不得回退直连仓储。删除console.Deps的ISOs、Sources、ISOSource和ISODeletion四项；保留网页SourceKind值/响应类型，移除旧调用接口，不靠同名别名留下仓储访问。
- [ ] 同步迁移调用侧测试夹具，真实数据库与网页权限测试保留。VM运行`go test ./internal/application/media ./internal/interfaces/console ./cmd/lober ./internal/archtest -count=1`、网页类型生成检查；Wire在VM生成并取回单个文件，VM执行Wire diff。预期无响应或类型差异。
- [ ] 实际删除网页仓储24项、规则5项，261→232，四清单变64/125/42/1；更新前阶段统计断言的实际数量，原阶段清零断言不动。独立审查后提交`refactor(media): route console through media contracts`。

### Task 3：拆开来源状态与字节操作

**Files:** 新建应用Sources/Fetch/Upload/Retry/Recover和Transfer/Coordination契约及测试；拆分基础设施`{iso,sources,source,remote,local,upload,recover}.go`及其测试；修改cmd supervisor的能力接线、Queue与组装测试、两张许可清单及`archtest/{iso,four_layer,transition_rules}_test.go`。`infrastructure/media/coordination.go`在本项先落地供新旧流程共用，Task5不重复新建。

**Interfaces:** 消费Task2 SourceControl的既有签名；实现NewSources和FetchOne/Recover/RequestReimport及Transfer会话协议。过渡期间Import和CleanupDeleted仍由旧实现承担，root把相同SourceLocks/Gate注入两者；旧supervisor的source字段改为应用Sources，增加`importer interface{ Import(context.Context,string) error }`字段接现有iso.Sources，launch参数使用media.ResumeWork。只调用新来源恢复与旧Import，不复制恢复规则。Task4把importer改接新应用Import，Task5再迁走整个supervisor。Reimport控制适配器调用RequestReimport然后Wake，不能同步导入。

- [ ] VM先跑新契约测试，预期新构造或能力不存在而失败。为Transfer建立真文件/受控HTTP测试；为应用使用可阻断的会话替身验证顺序，不用假替身代替最终真实数据库和文件组合测试。
- [ ] 迁出登记、状态转换、cancel的16次冲突重读、retry逐来源重置、allFetched恢复选择及扫描分页排序。失败镜像补来源与RequestReimport继续调用SaveSourceAndAcquire，禁止拆成两次提交；恢复只列持久化工作，不发下载或导入。
- [ ] 实现会话并用于生产流程；网络每次请求/重定向重新校验全部地址、固定拨号地址且保留TLS验证，不启代理或透明解压。保留无摘要断点归零、服务器200重来、206/416精确范围及实际part长度优先；复制缓冲与旧256KiB同步点保持。
- [ ] 守卫只允许`internal/infrastructure/media`实现精确`internal/application/media/port`，不放行整个application或其服务，也不把此能力扩给iso9660子包；以正反例验证后再用于会话实现。旧路径仍有的仓储调用只受已有逐项许可约束，不增加通配。
- [ ] 保留upload乱序/重复/重叠、先完整暂存验证再写确认文件、SaveRange原子区间合并。先发布取消函数再重读状态；服务退出/超时保留fetching断点，显式Cancel才置cancelled。重试只移走托管字节，审计失败恢复文件与数据库事实。
- [ ] 补`Test镜像取消与最后片发布两方向竞争`、`Test镜像关闭后从真实断点恢复`、`Test镜像获取会话每条退出路径关闭`；断言旧Rev不能覆盖cancelled、已fetched重放不改字节、非法片不写ranges、关闭次数为1。使用通道/受控数据库等待确认竞争，不以固定sleep当竞争证明。
- [ ] VM运行`go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -count=1`及相同包`-race -timeout=20m`。保留原Upload/Remote/RetryAuditFailure/WorkerShutdown/QueueAtomicity测试，只有消费者路径变化，不弱化断言。
- [ ] 删除本项实际已迁出的media许可，逐条登记而不预填中间数量；旧Import/Delete还使用的许可保留到所属项。核对其他阶段无改动；独立审查后提交`refactor(media): separate acquisition state from byte operations`。

### Task 4：导入、识别与恢复只走明确能力

**Files:** 新建domain/media规则、应用import/identify/suse/netboot与ImportFiles契约及测试；拆分基础设施`{import,identify,suse,netboot,extract,windows}.go`；保留`iso9660`、WIM与工具实现；修改旧Import消费者、组装及许可。新domain包加入实际四层分类和禁外部操作反例。

**Interfaces:** 消费Task3 Sources、协调锁和恢复工作，提供同模块`Import(context.Context,string) error`给后台。使用ImportSession/MediaView顺序协议；替换原Sources.Import及其旧仓储依赖，不增加应用到基础设施的导入。

- [ ] 先在VM跑新ImportFiles实现检查和领域纯规则测试，预期目标能力不存在失败。逐项迁移原ImportFormats、OfficialSUSE/OfficialDebian、RechecksEverySource、PreservesPublicationAfterLostCommitAcknowledgment、ExistingDestinationNeverOverwritten测试到正确归属；原文件读取/解析测试继续留在基础设施。
- [ ] 应用保留原锁与错误顺序：校验ID/ctx→取import锁→ISO状态→来源齐备→打开主源/摘要→识别/系匹配→额外源与netboot检查→保存importing和摘要→提取/复制→全部来源复核→先SMB后制品发布→清旧错误→ready保存。不能提前打开无关媒体改变原失败顺序。Import方法加入同一应用Sources，supervisor的importer接此对象；不保留第二份导入业务。
- [ ] 识别编排经MediaView按原顺序取事实；SUSE优先于通用.treeinfo，保留Agama嵌套目录、Debian仓库与额外netboot绑定、WIM版本/架构/edition检查和ESXi没有可靠版本就拒绝的行为。文本仍限制1MiB；WIM继续只读元数据，不把整张镜像缓存进内存。纯条件只保留一份领域实现，基础设施只解析格式及提供事实。
- [ ] 提取前由应用传入RepoPaths/RequiredRoles和识别结果，基础设施不再自行选平台政策。保留跨盘临时兄弟目录、owner文件、同内容完整树复用、其他内容拒绝覆盖、临时树与Windows副本清理。
- [ ] ready保存失败后按原 `context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)`重读：同摘要、同Artifacts且ready则成功；不确定则保留。只在确定同摘要且非ready/deprecated时允许回滚本次新树。普通锁竞争、废弃与请求取消不能记成导入失败；真正内容错误保留原错误截断和落库行为。
- [ ] 补`Test镜像导入提交未知不删已发布内容`、`Test镜像多源完成复制后才复核`、`Test镜像导入中废弃不能被失败标记覆盖`：以可控制的数据库/文件步骤验证，不依赖随机时间。VM跑领域、应用、基础设施、cmd和pgstore相关测试及race；实际工具缺失、实物ISO未给出时明确记录，不能报完整实机兼容通过。
- [ ] 删除本项对应旧media许可，不为原125项建立永久替代表。更新Wire并核对；独立审查后提交`refactor(media): move import decisions into media application`。

### Task 5：迁出删除流程与后台调度，收紧启停

**Files:** 新建应用deletion/supervisor、Journal和DeletedFiles契约、基础设施deletion_journal与清理实现及测试；迁移`cmd/lober/{supervisor,deletion}.go`业务与对应测试到所属层，保留cmd真实组装测试；更新`app,main,wire,wire_gen`和许可。沿用Task3协调实例。

**Interfaces:** 实现NewDeletion、RetryAll、NewSupervisor/Start/Wake/Close；控制适配器维持SourceControl，只有登记/上传/重试/重导入持久化成功才Wake。最终root直接组装Registry、Sources、ImportFiles、Deletion和Supervisor；旧iso.Sources仓储构造器及cmd业务实现删除，不用空兼容壳延寿。

- [ ] 在VM先跑新目标包测试，预期新删除/调度构造不存在而失败。迁移原deletion/supervision/queue_atomicity测试；保留cmd从实际newApp到后台、网页和重启的组装覆盖。
- [ ] 删除应用先串行化journal操作，再持有登记写闸门，按原顺序读ISO/rev及来源，持久化快照后调用DeleteISO；失败或提交结果未知，确认记录存在才丢快照，绝不清字节。成功后清理失败返回成功并保留快照；清理成功才discard，discard失败留待恢复。
- [ ] Journal实现保留`staging/deletions/<id>.json`、临时文件、独占创建、文件Sync→Rename→目录Sync、0600/0700权限、严格JSON/ID/非nil来源校验和汇总错误。不改变旧快照字段，不引入数据库队列表；启动后仍能读取搬迁前日志。
- [ ] 清理应用先调用领域CheckDeletion核对快照，再保留两次确认：先确认记录不存在→取同一import锁→取消活动获取并取各来源锁→再次确认不存在→字节清理。文件实现保护owner、根目录和符号链接。本地来源即使与staging允许根重合也不删，其他ISO/owner及未知结果不删；重复清理安全。
- [ ] 调度保留Workers/PollMS配置及原校验：默认2个工作名额、1000ms轮询，分别至少1，轮询转换不得溢出；工作键id/role/kind去重、非阻塞Wake、原100ms起步/上限30s/attempt最多9的退避公式、异常隔离及日志脱敏。Start先验证恢复读取再确认启动；Close可重复，停止且等待所有工作结束；upload恢复只等待客户端。
- [ ] 补Review Focus第4/5项：真实库竞争检查删除/补来源两种顺序；旧快照恢复、坏快照旁有效快照继续、错误owner、共享本地根、丢提交确认；另测Close与launch/Start竞争、满名额、异常后释放与取消立即退出。检查磁盘哨兵、快照、审计和活动数，不能只断言error。
- [ ] VM执行`go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore -count=1`及对应race；Wire生成、网页类型检查。独立审查后提交`refactor(media): own durable cleanup and background lifecycle`。

### Task 6：第四阶段边界守卫、完整验收与整分支审查

**Files:** 新建`internal/archtest/media_rules_test.go`、`docs/superpowers/reviews/2026-10-23-ddd-phase4-evidence.md`；修改`archtest/{deps,iso,transition_uses,transition_rules,transition_manifest,migration,four_layer,domain_rules,authoring_rules,hosts_rules}_test.go`及契约、路线和本计划。只改本阶段需要的边界，不替后续身份和任务阶段搬迁。

**Interfaces:** 承接Task1至5的真实应用和能力；固定第四阶段为0、阶段1/2/3及向导继续0。审查公开Registry、内部SourceControl、Transfer/ImportSession/Journal和后台生命周期的组合，不只检查文件是否存在。

- [ ] 先补会失败的守卫：console重新用仓储/旧Sources、media基础设施持有Store或通过方法值/同包辅助查库、应用导入基础设施或其他应用服务、领域读文件、cmd重新承担镜像状态流程均被拒绝；允许精确media/port实现与正常事实读取。正反例先通过类型检查，不能用语法错误假装守卫有效。
- [ ] 第四阶段累计正好删除24项网页仓储、5项网页规则、125项旧镜像例外，共154项，261→107；四清单为64/0/42/1。保留阶段5为65项、阶段6为42项；其余内容逐项与起点比较相同，不只比数量。原阶段3清单内容摘要是阶段快照，更新为本轮真实基线和删除集，保留原三阶段清零保障。
- [ ] 收紧实际类型检查：不把“基础设施不能导入任何领域函数”当目标，也不换成整个domain/media包通配放行。最终本方案的业务规则由应用/领域调用，文件实现通过明确port事实返回；旧media临时清单清零并保留防复活检查。新包、别名、点导入、包级函数值和方法值不能绕过。cmd只保留本模块构造/启停，镜像业务函数不能留在组装入口。
- [ ] lober VM按下节原命令执行完整检查；保存每轮精确树、环境、命令、起止时间、退出码、缓存/跳过项、错误及最终差异。先修真实阻塞问题，不改设置、不漏跑失败包。
- [ ] 此前五项任务与所有危险输入闭环后，提交`refactor(media): enforce phase-four boundaries`供本项独立审查；再做一次覆盖本阶段完整提交序列的整分支审查，包含十一入口及五项Review Focus。审查明确列出未判断项，根执行者逐项裁定，不默认忽略。
- [ ] 两道收尾审查均通过才标记模块分支可交付。文档-only收尾以Git精确差异证明代码仍等于被测树，不重复同一套件。保留模块分支、工作区和必要原日志；清理不得删用户文件或其他阶段证据。

## lober VM验收步骤

1. 执行时只从用户指定的私有环境文档读取需要的连接方式，先核实hostname为lober、Go/Node/pnpm、磁盘、内存、测试数据库确为lober_test且未在恢复，没有竞争Go作业。连接内容不输出。旧服务重启原因未知，不能假定本次稳定或自行重启服务。
2. 建立本阶段独立目录，例如`/home/dev/lober-ddd-phase4-media-20261023`，记录归属；只传已核对提交/候选树，重建Git索引并对完整树。不能覆盖产品服务、前阶段目录、用户文件、数据库或现有制品。测试日志与安全结果文件分开保留。
3. 使用已有私有DSN或环境变量，确认真实库为lober_test；私有文件忽略且600。临时schema测试仍按现有方式隔离。工具就绪与连接核实不算验收；数据库用例意外skip不能称完整通过。
4. 所有点名红绿测试、gofmt和Wire生成均在VM；只取回明确源码/生成文件，并核对本地差异。沿用`GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`及既有LOBER_TEST_BASH/LOBER_TEST_BUSYBOX路径，不升级依赖、不清成功缓存；若当前资源不同，先记录再解释，不编造容量。
5. 最终执行：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/application/media ./internal/infrastructure/media ./internal/interfaces/console ./cmd/lober ./internal/infrastructure/persistence/pgstore -run 'Test(镜像|ISO|Source|ISOSources|契约ISO|契约审计|Import|Upload|Remote|Recover|Worker|删除快照|补来源|重新导入|后台)' -count=1
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
git diff --exit-code
```

6. make保持全模块强制发现`-count=1`、普通测试、原race20m、Linux amd64构建和Wire diff；额外点名`-count=1`证明镜像、来源、数据库和新危险输入实际执行。记录实际缓存和所有skip；实物ISO、外部工具、一次性发布记录未具备就如实记未执行。没有前端业务变化时不强行加全量前端构建；若实现必须改前端，先明确范围并补typecheck/相关测试/build。
7. 失败先保留日志/堆栈和安全环境事实，再判断代码、测试或环境；环境恢复后保持代码和设置，复跑原失败用例，再从头完成原完整命令。不能因缓存或前一阶段成功把本轮失败改写为通过。不重启数据库、不改全局配置、不删除他人缓存或目录。

## 计划自检

- 已按批准设计检查四层方向、契约唯一来源、业务与事务边界、后台取消和恢复、原件/制品保护；各项均有任务和验收对应。
- 已核对实际清单：网页24、网页规则5、media125，共154；剩余107属于身份和任务收尾，不跨阶段删除。
- 已核对接力：Task2先切网页，复用已登记内部实现；Task3/4/5逐项替换来源、导入、删除和调度，共享同一协调实例；没有要求同时提交无法独立构建的空服务。
- 已核对类型：公开网页应用不携带repository.Audit或网页上下文；内部审计只接受可信Registry/组装；Progress保留ranges，Create保留部分结果，文件会话逐条定义关闭与回滚权限。
- 已核对五项重点均在所属任务补明确断言，已有真实数据库/文件测试复用且不弱化。实施若发现旧一致性缺口，必须先失败复现、单列修复，不静默改变契约。
- 制定计划时只保存文档，未实施或测试。用户确认后的实际执行与审查另记；不能把计划检查或开始实施称为第四阶段已验收。
