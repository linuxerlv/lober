# Task3 获取流程拆分执行记录

当前状态：候选实现完成自查，最终整组验证通过。来源拆分按指定标题提交供根独立审查；本记录不标记任务通过。旧夹具修复已单列提交 `4d7c24d`。

起点：`3eb6647468c9f2c765a4b8951d5ca4d9f9e95005`。工作分支：`codex/ddd-media`。

## 实现

- 应用 `Sources` 负责登记、状态、取消的16轮冲突重读、逐来源重试、上传区间、恢复分页与排序、重新导入排队。保留 `SaveSourceAndAcquire` 原子操作，失败镜像补来源与重新导入未拆成两次提交。
- `Transfer` 的获取、上传、重试文件会话只操作文件与网络，不访问仓储或创建业务问题。实际文件长度优先；无摘要断点归零、200重来、206/416严格范围、全部地址逐请求检查、固定拨号地址与TLS验证继续保留。下载复用256KiB缓冲，每次同步后才报告计数。
- 新旧流程共用 `SourceLocks` 和 `RegistrationGate`。旧对象仅保留导入与清理；根组装显式注入同一实例。旧后台接新恢复/获取与独立旧导入能力，未迁移后台自身。控制适配器只保存排队然后唤醒。
- 原真实数据库/文件测试通过仅测试夹具组合新旧能力，保留原断言。两处网页测试改组装，新加 `media_acquisition_test.go` 支持真实来源。没有生产兼容转发层。受影响消费者超出Files概括的路径是 `internal/interfaces/console/isos_test.go`、`media_contract_test.go`、`media_acquisition_test.go`，以及清理测试 `internal/infrastructure/media/delete_test.go` 的目录配置同步；根已批准这些测试组装调整。另经裁定单列 `internal/infrastructure/media/import_review_test.go` 旧测试夹具同步修复。
- 74条旧获取许可已删除，media清单125→51，总清单232→158。其他阶段的原始摘要继续固定；通过补回逐项删除内容核对起点。`migrationExceptions` 在Task2后已无本项获取许可，本项未删其条目；另两处总数断言随实际删除量同步更新。

## 两项协调裁定

1. `UploadSession.Size() int64`：提供完整暂存并验证后的实际片长，保存区间不使用请求声明长度。根已更新设计交接。其他接口保持指定签名。
2. 上传关闭顺序保持旧流程：Write+Sync→SaveRange→重读来源与区间→齐片才检查Close→BeginPublish。应用使用一次性关闭函数；部分片关闭与临时片清理错误沿用旧处理。关闭失败仍保留已提交区间，但不发布。

## 精确接口

- `NewSources(SourceDeps) *Sources`；依赖 `ISOs`, `Sources`, `Transfer`, `Locks`, `Gate`，均为本模块能力接口。
- `Register/RegisterQueued(context.Context,model.ISO,string,string,string,int64,string,repository.Audit) error`。
- `FetchOne(context.Context,string,string) error`；`Recover(context.Context)([]ResumeWork,error)`；`RequestReimport(context.Context,string,repository.Audit) error`。
- `ResumeWork{ISOID,Role string; Kind ResumeKind}`，只有 `fetch/import`。上传fetch待办由后台过滤，等待客户端送片。
- `NewControl(*Sources,port.Waker) port.SourceControl`；Waker只有Wake。Register调用RegisterQueued，Reimport调用RequestReimport，成功提交才Wake。
- `SourceLocks{Lock(id,role string)func(); Track(id,role string,context.CancelFunc)func(); Cancel(id,role string)}`；Gate为Lock/Unlock/RLock/RUnlock。
- `Transfer{ValidateSource(ctx,Source)(Source,error); BeginFetch/BeginPublish(ctx,Source)(FetchSession,error); BeginUpload(ctx,Source,int64,io.Reader)(UploadSession,error); ParkRetry(ctx,Source)(RetryFiles,error); FreeSpace(ctx)(int64,int64,error)}`。
- `FetchSession{Resume(ctx)(int64,error); Open(ctx)(FetchInfo,error); Step(ctx)(int64,bool,error); Verify(ctx)(VerifiedContent,error); Publish/Restore(ctx)error; Close()error}`。
- `FetchInfo{Offset,TotalSize int64;Complete,Reset bool}`；`VerifiedContent{Size int64;SHA256 string}`。
- `UploadSession{Size()int64;CheckOverlap(ctx,[]Range)error;Write/VerifyReplay(ctx)error;Close()error}`；`RetryFiles{Restore()error;Commit()error}`。
- `Fault{Kind FaultKind;Role,Expected,Actual string;Cause error}`，Error/Unwrap；枚举URLRejected、PathRejected、ChunkRejected、ContentMismatch、PublicationConflict、InvalidFile。

## 行为证据

- `Test镜像取消与最后片发布两方向竞争`：真实库/文件，阻断校验或改名后返回；覆盖取消先提交、改名后取消先提交、取消持旧版本但完成先提交。验证旧版本不能覆盖cancelled、失败发布恢复part、完成重放不改字节/版本、错误重放不改区间、发布会话Close恰好一次。
- `Test镜像关闭后从真实断点恢复`：受控HTTP和真实数据库保存完成信号，确认3字节断点已提交后关闭，再从bytes=3-恢复成6字节。没有用固定sleep证明顺序。
- `Test镜像获取会话每条退出路径关闭`：真实库加可控外部会话，覆盖begin/resume/修正保存/open/reset/开始保存/step/进度保存/verify/大小/publish/最终保存/success，共13条路径；已打开会话关闭一次，最终保存失败调用恢复，读错误不吞已同步位置。
- `Test镜像最后片关闭失败发生在确认区间之后`：确认Write→SaveRange→Close→Publish；可控Close失败时区间已提交、来源仍fetching、没有发布。
- `Test镜像传输暂存完整片后才写确认文件`：真实临时文件，超长片不碰确认文件，错误重叠不改字节，校验实际摘要后发布。
- 原Upload、Remote、RetryAuditFailure、WorkerShutdown、QueueAtomicity对应中文排队测试、`Test装配后台获取导入及启动恢复`保留。

## 失败与修正

- `t3-red-1`：预期缺Transfer/NewSources/SourceDeps/ResumeWork及共享协调构造，退出1。
- `t3-guard-red-1`：预期精确media/port也被旧守卫拒绝，退出1；green-2正反例通过后才实现会话。
- `t3-transfer-red-1`：预期缺NewTransfer，退出1。
- `t3-format-1`：Wire成功；恢复新测试通过；旧关闭恢复测试漏改ResumeFetch名称导致编译失败，修正消费者引用。
- `t3-save-red-1`：新迁移引入三处保存失败空返回丢失来源身份，resume-save/open-save/step-save失败；固定使用入口编号与角色归属错误。该问题是本次迁移回归，不是旧行为变更。
- `t3-save-green-1`：拆retry文件时漏保留sources.go的errors导入，编译失败；green-2原13条用例与架构检查通过。
- `t3-local-red-1`：应用仍要求本地来源进入ParkRetry；改为仅非local来源调用文件移走能力，真实原件不变。
- `t3-close-red-1`：候选顺序为write/range/publish/close，关闭错误被忽略；按裁定更正。`t3-format-2`对应测试通过。
- `t3-final-1`：完整任务在普通测试组失败；`TestCleanupDeletedPreservesLocalOriginalAndForeignPublication` 修改旧对象的 LocalRoots 后，新 Transfer 仍保留构造时的副本，登记被拒绝。此轮尚未执行竞态、console、架构、Wire与格式检查。仅补测试组装使双方接同样目录，全部保护断言保留；`t3-fixture-1` 原失败用例通过，再从头执行同一完整命令。
- `t3-final-2`：普通四包通过；竞态在旧 `TestImportRechecksEverySourceAfterAllCopies` 的debian/windows-server两子例报告数据竞争，console/架构/Wire/格式后续命令未执行。根核对原文件未改：`mutateImportContext.Err` 的done布尔写入与database/sql后台调用Err读取没有同步。`t3-import-race-red-1` 单例原样再次复现。仅给该旧夹具Err与完成状态观察加同一Mutex，篡改恰好一次且完成后再观察；原字节/失败状态/无发布断言全部保留。`t3-import-race-green-1` 原命令竞态通过；根批准独立提交 `4d7c24d`（`test(media): synchronize import mutation fixture`）。该修复为旧测试夹具竞态，未改生产导入。随后 `t3-final-3` 从头执行同一完整任务命令。
- 提交前本地文件数断言初次未考虑Git将recover.go识别为重命名，因而停止提交；改用 `--no-renames` 核对完整新增与删除路径，源码未变。
- 本地一次Python读文件未指定UTF-8导致解码失败，随后的guard-green-1命令文件未创建，VM未启动；改用显式UTF-8与新标签green-2。几次只读rg/PowerShell路径写法失败已改用显式路径，没有运行本机Go。
- 详细测试包含既有GIN路由调试输出，未通过更改测试设置隐藏。所有Go、gofmt、Wire与测试均在指定VM。没有运行本机Go或读取凭据、连接文档、DSN、私有原始日志。

## 逐轮VM证明

表内时间均为UTC。退出码来自取回的证明；非详细输出不能证明跳过数量为零。详细输出只统计实际出现的SKIP行。所有测试显式count=1；逐轮安全日志未发现cached标记。格式化两轮的源码变更随后按显式Go路径拉回，并在最终无写入轮复验。

| 标签 | 候选树 | 开始 | 结束 | 退出 |
|---|---|---|---|---|
| t3-red-1 | `2e0530613e45d9deca2c9dab0b4168b1df0f6472` | 2026-10-23T16:21:05Z | 2026-10-23T16:21:06Z | 1 |
| t3-guard-red-1 | `1ed4fcc634de6d0c0fe085ccb40fb71bcf26bf71` | 2026-10-23T16:22:14Z | 2026-10-23T16:22:16Z | 1 |
| t3-guard-green-2 | `6499e6ea13c3351aac8f5c33a5cbbfdb20a42251` | 2026-10-23T16:23:21Z | 2026-10-23T16:23:22Z | 0 |
| t3-transfer-red-1 | `3dafc30e91f863a8eecd70bd829106aab59863ac` | 2026-10-23T16:24:12Z | 2026-10-23T16:24:13Z | 1 |
| t3-format-1 | `c5118c20f9ce4e42add17aa17b251900d545e614` | 2026-10-23T16:31:12Z | 2026-10-23T16:31:28Z | 1 |
| t3-focus-1 | `3394b2153936d48a85ff91ca119e674dddc94e90` | 2026-10-23T16:33:29Z | 2026-10-23T16:33:42Z | 0 |
| t3-order-1 | `c57030753cfbe1f6eb29d083cd80406ef588e954` | 2026-10-23T16:35:47Z | 2026-10-23T16:35:55Z | 0 |
| t3-save-red-1 | `a83a7e662409696856f4dfc7adc1a5ccf99a74d0` | 2026-10-23T16:37:36Z | 2026-10-23T16:37:43Z | 1 |
| t3-save-green-1 | `9d080c8fee5632a881a31813eaaa04f701372c89` | 2026-10-23T16:39:28Z | 2026-10-23T16:39:29Z | 1 |
| t3-save-green-2 | `15464eb67d9b6b07286406307491e8e9a3790f95` | 2026-10-23T16:41:13Z | 2026-10-23T16:43:43Z | 0 |
| t3-local-red-1 | `8f8c92f6e4abc673a5a7fcf85224b219f32347a0` | 2026-10-23T16:44:10Z | 2026-10-23T16:44:13Z | 1 |
| t3-close-red-1 | `ec159bcb5e8d169c7581c20d9edfb1940940bdf3` | 2026-10-23T16:46:29Z | 2026-10-23T16:46:32Z | 1 |
| t3-format-2 | `3a37c147b0c60a112246195848298008f2de2eb9` | 2026-10-23T16:48:31Z | 2026-10-23T16:48:42Z | 0 |
| t3-final-1 | `494d807743fd733ae11a51ab3c073432fec6c058` | 2026-10-23T16:50:36Z | 2026-10-23T16:53:12Z | 1 |
| t3-fixture-1 | `b253f8990a1cc743646c963f885f113ef12c7ed1` | 2026-10-23T16:55:50Z | 2026-10-23T16:55:54Z | 0 |
| t3-final-2 | `1bf0e05483bae70d3873a822ca9bfb49a2dc3ab9` | 2026-10-23T16:59:16Z | 2026-10-23T17:05:46Z | 1 |
| t3-import-race-red-1 | `1bf0e05483bae70d3873a822ca9bfb49a2dc3ab9` | 2026-10-23T17:07:12Z | 2026-10-23T17:07:16Z | 1 |
| t3-import-race-green-1 | `b6a9ad30e60b4c1b2843adc1daec3c7e8f150e05` | 2026-10-23T17:08:00Z | 2026-10-23T17:08:07Z | 0 |
| t3-final-3 | `d6fb356035da818fe34d1c0edb5813b7e69e9e2d` | 2026-10-23T17:08:44Z | 2026-10-23T17:21:05Z | 0 |

每轮原命令保存在同目录 `<label>.commands`；完整候选与命令由 `<label>.snapshot.json` 固定。安全输出及tree/started/finished/exit/changes/untracked证明保存在 `vm-logs/`。

## 逐条删除的许可

| 原文件 | 删除条数 |
|---|---:|
| `internal/infrastructure/media/iso.go` | 4 |
| `internal/infrastructure/media/local.go` | 2 |
| `internal/infrastructure/media/recover.go` | 6 |
| `internal/infrastructure/media/remote.go` | 5 |
| `internal/infrastructure/media/source.go` | 15 |
| `internal/infrastructure/media/sources.go` | 35 |
| `internal/infrastructure/media/upload.go` | 7 |
| 合计 | 74 |

```go
{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/iso.go", "(*Sources).FetchOne", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/local.go", "(*Sources).fetchLocal", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "allFetched", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOQuery", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.ListUnfinishedSources", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/recover.go", "(*Sources).Recover", "github.com/lober/lober/internal/domain/repository", "ISOs.ListISOs", "4"},
{pkgISO, "internal/infrastructure/media/remote.go", "parseContentRange", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/remote.go", "rangeNumber", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/remote.go", "(*Sources).fetchRemote", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "checkSourceKey", "github.com/lober/lober/internal/domain/repository", "CheckSourceKey", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).resumeOffset", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).restorePublication", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ErrRejected", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).finishFetch", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).failFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).writeFetch", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RegisterQueued", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).register", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Cancel", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Retry", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).retrySource", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Reimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSourceAndAcquire", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).RequestReimport", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
{pkgISO, "internal/infrastructure/media/sources.go", "(*Sources).Progress", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "compareRange", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "CheckAudit", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.RangesOf", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveRange", "4"},
{pkgISO, "internal/infrastructure/media/upload.go", "(*Sources).Upload", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
```

## 改动路径

- `cmd/lober/app.go`
- `cmd/lober/assembly_test.go`
- `cmd/lober/deletion.go`
- `cmd/lober/queue_atomicity_test.go`
- `cmd/lober/supervision_test.go`
- `cmd/lober/supervisor.go`
- `cmd/lober/wire.go`
- `cmd/lober/wire_gen.go`
- `internal/application/media/acquisition_order_test.go`
- `internal/application/media/control.go`
- `internal/application/media/fetch.go`
- `internal/application/media/port/control.go`
- `internal/application/media/port/coordination.go`
- `internal/application/media/port/transfer.go`
- `internal/application/media/recover.go`
- `internal/application/media/retry.go`
- `internal/application/media/sources.go`
- `internal/application/media/sources_integration_test.go`
- `internal/application/media/upload.go`
- `internal/application/media/upload_close_order_test.go`
- `internal/archtest/authoring_rules_test.go`
- `internal/archtest/four_layer_test.go`
- `internal/archtest/hosts_rules_test.go`
- `internal/archtest/iso_test.go`
- `internal/archtest/transition_manifest_test.go`
- `internal/archtest/transition_rules_test.go`
- `internal/infrastructure/media/acquisition_fixture_test.go`
- `internal/infrastructure/media/checkpoint_order_test.go`
- `internal/infrastructure/media/coordination.go`
- `internal/infrastructure/media/delete.go`
- `internal/infrastructure/media/delete_test.go`
- `internal/infrastructure/media/fetch_session.go`
- `internal/infrastructure/media/import_review_test.go`（独立提交的旧夹具同步修复）
- `internal/infrastructure/media/import_test.go`
- `internal/infrastructure/media/iso.go`
- `internal/infrastructure/media/local.go`
- `internal/infrastructure/media/recover.go`
- `internal/infrastructure/media/recover_test.go`
- `internal/infrastructure/media/reimport_request_test.go`
- `internal/infrastructure/media/remote.go`
- `internal/infrastructure/media/remote_test.go`
- `internal/infrastructure/media/retry_files.go`
- `internal/infrastructure/media/shutdown_resume_test.go`
- `internal/infrastructure/media/source.go`
- `internal/infrastructure/media/source_test.go`
- `internal/infrastructure/media/sources.go`
- `internal/infrastructure/media/ssrf.go`
- `internal/infrastructure/media/transfer.go`
- `internal/infrastructure/media/transfer_test.go`
- `internal/infrastructure/media/upload.go`
- `internal/infrastructure/media/upload_test.go`
- `internal/interfaces/console/isos_test.go`
- `internal/interfaces/console/media_acquisition_test.go`
- `internal/interfaces/console/media_contract_test.go`

- `docs/superpowers/reviews/2026-10-23-ddd-phase4-task3-execution.md`

## 自查

已按源码逐段核对错误顺序、上传关闭、字节落盘与记录保存的先后、取消版本、共用锁与闸门、旧导入和清理的剩余调用。没有修改数据库格式、网页协议、账号设置、测试准备缓存、超时或断言强度。未启动子代理/独立审查、未合并/推送/发布。全库make留给Task6。

## 最终验证与提交范围

最终无写入验证为 `t3-final-3`，候选树 `d6fb356035da818fe34d1c0edb5813b7e69e9e2d`，UTC `2026-10-23T17:08:44Z` 至 `2026-10-23T17:21:05Z`，退出0。VM的changes与untracked证明均为空。

| 包 | 普通测试 | 并发检查 |
|---|---:|---:|
| application/media | 9.070s | 11.486s |
| infrastructure/media | 40.240s | 49.064s |
| cmd/lober | 29.598s | 114.961s |
| persistence/pgstore | 119.588s | 158.165s |

console整包通过201.027s，archtest整包通过149.586s。随后Wire diff、53个现存改动Go文件的gofmt检查及git diff均通过。总共改动54个Go路径，其中旧recover.go删除；旧夹具同步修复单列在前一提交中。Go来源、测试、配置均没有在最终测试后修改。

详细日志共1570条PASS（包含子例），只有 `TestIdentifyRealISO` 在普通与并发两轮各跳过一次，原因均是 `LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in`。未提供真实镜像路径，未改变该设置。没有其他详细SKIP行，没有FAIL、数据竞争或cached标记。架构命令没有使用-v，不据此宣称它的内部跳过数量为零。所有逐轮go test均显式count=1；未削减密码强度、缓存准备逻辑、竞态超时、测试范围或原断言。

提交前已将53个本项Go路径逐一显式暂存，与最终测试树比较，唯一差异是根拥有而未暂存的计划Markdown。加入本执行记录后仍只允许Markdown差异；根计划不纳入来源拆分提交。来源拆分候选标题为 `refactor(media): separate acquisition state from byte operations`，本记录随该候选提交。另一个提交为 `4d7c24d test(media): synchronize import mutation fixture`。最终提交编号写入私有交接报告并回报根。

自查未发现待修正的功能问题。可选真实镜像覆盖仍缺外部文件；全库make按任务安排留给Task6。是否通过由根的固定BASE到最终HEAD独立审查决定。

## 原始验证命令

以下逐轮命令均交给指定VM执行。失败会停止该轮余下命令；实际执行范围见失败记录。

### t3-red-1

```sh
go test ./internal/application/media -run Test镜像恢复只列持久化来源 -count=1
```

### t3-guard-red-1

```sh
go test ./internal/archtest -run Test镜像文件能力只向精确接口开放 -count=1
```

### t3-guard-green-2

```sh
go test ./internal/archtest -run "Test镜像文件能力只向精确接口开放|TestISORules|Test基础设施拒绝接口层和应用具体服务" -count=1
```

### t3-transfer-red-1

```sh
go test ./internal/infrastructure/media -run Test镜像传输暂存完整片后才写确认文件 -count=1
```

### t3-format-1

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion.go cmd/lober/queue_atomicity_test.go cmd/lober/supervision_test.go cmd/lober/supervisor.go cmd/lober/wire.go internal/application/media/port/control.go internal/archtest/four_layer_test.go internal/archtest/iso_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/import_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/local.go internal/infrastructure/media/recover_test.go internal/infrastructure/media/reimport_request_test.go internal/infrastructure/media/remote.go internal/infrastructure/media/remote_test.go internal/infrastructure/media/shutdown_resume_test.go internal/infrastructure/media/source.go internal/infrastructure/media/source_test.go internal/infrastructure/media/sources.go internal/infrastructure/media/ssrf.go internal/infrastructure/media/upload.go internal/infrastructure/media/upload_test.go internal/interfaces/console/isos_test.go internal/interfaces/console/media_contract_test.go internal/application/media/control.go internal/application/media/fetch.go internal/application/media/port/coordination.go internal/application/media/port/transfer.go internal/application/media/recover.go internal/application/media/sources.go internal/application/media/sources_integration_test.go internal/application/media/upload.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/coordination.go internal/infrastructure/media/fetch_session.go internal/infrastructure/media/retry_files.go internal/infrastructure/media/transfer.go internal/infrastructure/media/transfer_test.go internal/interfaces/console/media_acquisition_test.go
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/interfaces/console -run "Test镜像恢复只列持久化来源|Test镜像传输暂存完整片后才写确认文件" -count=1
```

### t3-focus-1

```sh
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/interfaces/console -run "Test镜像|Test.*Upload|Test.*Remote|Test.*RetryAuditFailure|Test.*WorkerShutdown|Test补来源调度|Test重新导入调度|Test装配后台获取导入及启动恢复" -count=1 -v
```

### t3-order-1

```sh
go test ./internal/application/media ./internal/infrastructure/media -run "Test镜像取消与最后片发布两方向竞争|Test镜像关闭后从真实断点恢复|Test镜像获取会话每条退出路径关闭" -count=1 -v
```

### t3-save-red-1

```sh
go test ./internal/application/media -run Test镜像获取会话每条退出路径关闭 -count=1 -v
```

### t3-save-green-1

```sh
go test ./internal/application/media -run Test镜像获取会话每条退出路径关闭 -count=1 -v
go test ./internal/archtest -count=1
```

### t3-save-green-2

```sh
go test ./internal/application/media -run "Test镜像获取会话每条退出路径关闭|Test镜像取消与最后片发布两方向竞争" -count=1 -v
go test ./internal/archtest -count=1
```

### t3-local-red-1

```sh
go test ./internal/application/media -run Test镜像重试本地来源不要求托管文件能力 -count=1 -v
```

### t3-close-red-1

```sh
go test ./internal/application/media -run Test镜像最后片关闭失败发生在确认区间之后 -count=1 -v
```

### t3-format-2

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion.go cmd/lober/queue_atomicity_test.go cmd/lober/supervision_test.go cmd/lober/supervisor.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/port/control.go internal/archtest/authoring_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/iso_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_rules_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/import_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/local.go internal/infrastructure/media/recover_test.go internal/infrastructure/media/reimport_request_test.go internal/infrastructure/media/remote.go internal/infrastructure/media/remote_test.go internal/infrastructure/media/shutdown_resume_test.go internal/infrastructure/media/source.go internal/infrastructure/media/source_test.go internal/infrastructure/media/sources.go internal/infrastructure/media/ssrf.go internal/infrastructure/media/upload.go internal/infrastructure/media/upload_test.go internal/interfaces/console/isos_test.go internal/interfaces/console/media_contract_test.go internal/application/media/acquisition_order_test.go internal/application/media/control.go internal/application/media/fetch.go internal/application/media/port/coordination.go internal/application/media/port/transfer.go internal/application/media/recover.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/sources_integration_test.go internal/application/media/upload.go internal/application/media/upload_close_order_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/checkpoint_order_test.go internal/infrastructure/media/coordination.go internal/infrastructure/media/fetch_session.go internal/infrastructure/media/retry_files.go internal/infrastructure/media/transfer.go internal/infrastructure/media/transfer_test.go internal/interfaces/console/media_acquisition_test.go
go test ./internal/application/media ./internal/infrastructure/media -run "Test镜像|TestUpload|TestRetryAuditFailure|TestWorkerShutdown" -count=1 -v
```

### t3-final-1

```sh
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -count=1 -v
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -race -timeout=20m -count=1 -v
go test ./internal/interfaces/console -count=1 -v
go test ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
test -z "$(gofmt -l cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion.go cmd/lober/queue_atomicity_test.go cmd/lober/supervision_test.go cmd/lober/supervisor.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/port/control.go internal/archtest/authoring_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/iso_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_rules_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/import_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/local.go internal/infrastructure/media/recover_test.go internal/infrastructure/media/reimport_request_test.go internal/infrastructure/media/remote.go internal/infrastructure/media/remote_test.go internal/infrastructure/media/shutdown_resume_test.go internal/infrastructure/media/source.go internal/infrastructure/media/source_test.go internal/infrastructure/media/sources.go internal/infrastructure/media/ssrf.go internal/infrastructure/media/upload.go internal/infrastructure/media/upload_test.go internal/interfaces/console/isos_test.go internal/interfaces/console/media_contract_test.go internal/application/media/acquisition_order_test.go internal/application/media/control.go internal/application/media/fetch.go internal/application/media/port/coordination.go internal/application/media/port/transfer.go internal/application/media/recover.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/sources_integration_test.go internal/application/media/upload.go internal/application/media/upload_close_order_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/checkpoint_order_test.go internal/infrastructure/media/coordination.go internal/infrastructure/media/fetch_session.go internal/infrastructure/media/retry_files.go internal/infrastructure/media/transfer.go internal/infrastructure/media/transfer_test.go internal/interfaces/console/media_acquisition_test.go)"
git diff --exit-code
```

### t3-fixture-1

```sh
gofmt -w internal/infrastructure/media/delete_test.go
go test ./internal/infrastructure/media -run TestCleanupDeletedPreservesLocalOriginalAndForeignPublication -count=1 -v
```

### t3-final-2

```sh
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -count=1 -v
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -race -timeout=20m -count=1 -v
go test ./internal/interfaces/console -count=1 -v
go test ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
test -z "$(gofmt -l internal/infrastructure/media/delete_test.go cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion.go cmd/lober/queue_atomicity_test.go cmd/lober/supervision_test.go cmd/lober/supervisor.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/port/control.go internal/archtest/authoring_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/iso_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_rules_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/import_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/local.go internal/infrastructure/media/recover_test.go internal/infrastructure/media/reimport_request_test.go internal/infrastructure/media/remote.go internal/infrastructure/media/remote_test.go internal/infrastructure/media/shutdown_resume_test.go internal/infrastructure/media/source.go internal/infrastructure/media/source_test.go internal/infrastructure/media/sources.go internal/infrastructure/media/ssrf.go internal/infrastructure/media/upload.go internal/infrastructure/media/upload_test.go internal/interfaces/console/isos_test.go internal/interfaces/console/media_contract_test.go internal/application/media/acquisition_order_test.go internal/application/media/control.go internal/application/media/fetch.go internal/application/media/port/coordination.go internal/application/media/port/transfer.go internal/application/media/recover.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/sources_integration_test.go internal/application/media/upload.go internal/application/media/upload_close_order_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/checkpoint_order_test.go internal/infrastructure/media/coordination.go internal/infrastructure/media/fetch_session.go internal/infrastructure/media/retry_files.go internal/infrastructure/media/transfer.go internal/infrastructure/media/transfer_test.go internal/interfaces/console/media_acquisition_test.go)"
git diff --exit-code
```

### t3-import-race-red-1

```sh
go test ./internal/infrastructure/media -run TestImportRechecksEverySourceAfterAllCopies -race -timeout=20m -count=1 -v
```

### t3-import-race-green-1

```sh
gofmt -w internal/infrastructure/media/import_review_test.go
go test ./internal/infrastructure/media -run TestImportRechecksEverySourceAfterAllCopies -race -timeout=20m -count=1 -v
```

### t3-final-3

```sh
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -count=1 -v
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/infrastructure/persistence/pgstore -race -timeout=20m -count=1 -v
go test ./internal/interfaces/console -count=1 -v
go test ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
test -z "$(gofmt -l internal/infrastructure/media/import_review_test.go internal/infrastructure/media/delete_test.go cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion.go cmd/lober/queue_atomicity_test.go cmd/lober/supervision_test.go cmd/lober/supervisor.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/port/control.go internal/archtest/authoring_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/iso_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_rules_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/import_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/local.go internal/infrastructure/media/recover_test.go internal/infrastructure/media/reimport_request_test.go internal/infrastructure/media/remote.go internal/infrastructure/media/remote_test.go internal/infrastructure/media/shutdown_resume_test.go internal/infrastructure/media/source.go internal/infrastructure/media/source_test.go internal/infrastructure/media/sources.go internal/infrastructure/media/ssrf.go internal/infrastructure/media/upload.go internal/infrastructure/media/upload_test.go internal/interfaces/console/isos_test.go internal/interfaces/console/media_contract_test.go internal/application/media/acquisition_order_test.go internal/application/media/control.go internal/application/media/fetch.go internal/application/media/port/coordination.go internal/application/media/port/transfer.go internal/application/media/recover.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/sources_integration_test.go internal/application/media/upload.go internal/application/media/upload_close_order_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/checkpoint_order_test.go internal/infrastructure/media/coordination.go internal/infrastructure/media/fetch_session.go internal/infrastructure/media/retry_files.go internal/infrastructure/media/transfer.go internal/infrastructure/media/transfer_test.go internal/interfaces/console/media_acquisition_test.go)"
git diff --exit-code
```
