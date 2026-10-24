# Phase4 Task5 执行记录

候选源码提交：`7d5acabb508fbd33ed262cf0934a4967efce7343`。基线：`7bc62f7494e892d6a77de686dbe1d6f30a555eb1`。等待控制方独立审查；本文不替代审查通过或 Task6 完整验收。

## 实现与行为

删除流程、后台调度已迁入 application/media；文件日志、字节清理留在 infrastructure/media。删除旧 iso.Sources 持仓储构造器及 cmd/lober 的删除/调度业务，没有兼容空壳。最终装配是 Sources / Deletion → Supervisor → Control → Registry。Sources 仍同时承担获取与 Import，真实 ImportFiles 通过 SourceDeps.Import 注入，获取、导入和删除使用同一 SourceLocks、RegistrationGate。

NewDeletion 要求 ISOs、Sources、Journal、Files、Locks、Gate、Logger。DeletionSnapshot 保留原字段 `ISO model.ISO`、`Sources []model.Source` 和原JSON形状。DeletionJournal 契约是 Persist(context.Context,DeletionSnapshot)、List(context.Context)([]DeletionSnapshot,error)、Discard(context.Context,string)；DeletedFiles 是 Cleanup(context.Context,DeletionSnapshot)。SupervisorDeps 只收恢复/执行、GetSource、RetryAll 和配置，不再兼任 SourceControl。

删除仍先串行日志操作，再取登记写锁，读取镜像/版本和来源，先持久化快照再提交删除。结果未知时不删字节，只有确认记录还在才撤销快照。删除已提交但清理失败仍返回成功并保留日志；清理成功才丢日志，丢弃失败可重试。清理先做领域校验、确认不存在、取得导入锁、取消获取并取得来源锁、再次确认不存在，然后清理字节。无效日志不会挡住有效日志恢复。

日志保留 staging/deletions/<id>.json、独占临时文件、文件Sync→Rename→目录Sync、0600/0700权限、严格JSON/ID/非nil来源检查和合并错误。文件清理继续保护本地原文件、其他owner、根目录与符号链接，重复清理安全。

调度保留工作键id/role/kind、名额、非阻塞Wake、异常隔离和固定脱敏日志。退避100ms起步、attempt最多9、公式上限30s，实际封顶25.6s；未改原公式。默认Workers=2、PollMS=1000、至少1和毫秒转换溢出检查保留在原config.go。main.go已有Start/Close调用，无需改动。

## 单列行为修复：启动中关闭可以取消恢复读取

旧Start持锁调用Recover，旧Close先等同一锁再cancel。恢复读取在等取消时会互相等待。`Test关闭能取消正在启动的恢复读取` 在 t5-green-compile-01 的原1秒断言失败，控制方明确批准最小修复。

Close先cancel再取锁标记关闭并等待全部工作；Start恢复后检查ctx，取消后不启动循环、不增加工作。WaitGroup增加与关闭仍互斥。保持相同测试、原断言和超时，随后普通/race通过。这是实际行为修复，不称作纯搬迁。

## 领域边界和旧许可

CheckDeletion直接复用原CheckID、CheckSource、ErrInvalid及ISO.Validate，不搬公共规则、不复制领域校验。完整架构检查发现inLayer1先排除了repository；控制方复核后裁定只允许 caller==domain/media 且 imp==domain/repository 的精确导入。没有修改通用inLayer1，也没有放行media子包、repository子包或数据库实现。

类型检查只放行三个纯符号，拒绝仓储类型、方法值、接口别名等。新增真实checkModule正反例。首轮我误把repositoryother当禁止路径，但它本来是普通领域包，已换为数据库适配器反例，未扩大通用限制。旧confirmDeleted许可测试已改为拒绝反例。

实际移除11条media许可，media剩余0、总许可118→107、media历史删除114→125。与基线逐条比对，每条在历史审计中恰好出现一次；原摘要4e93eaf8a62876bfa694042b348843f41709e729f45b4354708802cfe5df19db保持不变。完整许可列表见后文。

## 原测试迁移与新增覆盖

- cmd/lober/deletion_test.go的1项测试迁入application/media/deletion_integration_test.go；原assembly的持久化日志/重启恢复测试也迁入。保留真实数据库、文件、快照和审计事实。
- cmd/lober/supervision_test.go的3项、queue_atomicity_test.go的2项原测试迁入应用包。名称逐个核对无遗漏，保留原触发器、事务回滚、版本、审计和重启断言。
- cmd/lober保留真实newApp的Test装配后台获取导入及启动恢复，覆盖后台获取/导入、页面、重复启动、重启及上传恢复。没有用轻量假组装替换真实集成测试。
- 用真实数据库、文件和屏障强制删除/补来源两种顺序。断言快照确认前记录仍在、删除审计未出现；补来源先发生时额外文件被清理，删除先发生时未登记文件保留；同时检查来源、审计、快照结果。
- 丢失提交确认、第二次确认失败、丢弃日志失败保留字节/快照；新服务恢复后检查真实删除审计仅1次。坏JSON和领域非法快照旁的有效快照继续清理。
- 原错误owner、共享本地根、符号链接、活动HTTP获取取消检查保留。原记录仍在时ErrConflict且不删字节的断言移到应用清理测试。
- 控制器Register/Upload/Retry/Reimport失败不Wake；成功Wake回调读取真实数据库，证明来源状态/重新排队已经提交。
- 生命周期覆盖启动中Close、重复Close、关闭后Start、等待所有工作、满名额、同键去重、关闭后不再launch、panic日志脱敏。原异常后释放/退避重试/取消立即退出检查保留。
- 日志测试读取原JSON字段，检查有效日志旁坏JSON、未知字段、nil来源、ID不符、尾部多JSON、符号链接、权限、临时文件、重复Discard、路径穿越和取消。

## 验证范围与限制

Go/gofmt/Wire/Node和类型检查全部经本计划vm.py在lober VM执行。本机仅编辑、Python比较和Git操作。未本地运行Go，未读取连接值、DSN、私有运行配置或原始日志。单一VM作业、唯一标签；上传、生成、格式拉回均等待完成。未清缓存或更改原测试设置，race保持-timeout=20m。完整make check留给Task6。

t5-full-01是中间轮，整体exit=1：原五包普通/race全部通过，然后架构失败；后续架构race/Wire/typecheck未执行。期间本地增加测试和更新计数，因此不能作为最终树证据。完整比较旧树与冻结树2032个路径，只有7个应用/架构测试文件变化；生产、配置、生成文件及infra/cmd/console/pgstore全部既有测试逐文件一致。按控制方条件裁定采用分组证据，不声称最终树重新跑了五包整组，Task6仍做单树完整验收。

五包普通耗时：application/media39.175s、infrastructure/media15.813s、cmd/lober21.429s、console201.241s、pgstore118.599s。race分别49.952s、20.428s、86.903s、863.627s、156.941s。console耗时较长但没有超时，未默认归因环境问题。

中间整组普通/race每轮各1次可选TestIdentifyRealISO跳过，该轮共2次；最终应用包普通/race也各1次，共2次（这两轮累计4次）。未提供外部真实镜像，不声称验证真实发行版/安装工具兼容性。顶层测试使用-count=1，无缓存通过；架构缓存回归测试在自己的临时树内按原设计验证缓存，不改其行为。

t5-freeze-focused-01的应用新用例普通/race、domain/arch全普通/race和Wire diff通过；最后网页类型检查失败：vue-tsc: not found、spawn ENOENT、node_modules missing。该轮exit=1，不能称为整轮通过。依赖处置及最终检查见后文。

## 自查

已对照锁顺序、两次确认、未知结果不删字节、成功持久化后唤醒、同一协调实例、旧JSON兼容、真实newApp覆盖及11条许可。git diff --check通过。源码冻结后不再改Go文件，仅补报告。

本地辅助命令曾出现默认GBK读取UTF8失败（改显式UTF8）、报告草稿脚本引号错误（改直接Python写入）、rg的Windows通配路径错误（改目录与-g）。没有把这些命令算成验证通过，也没有从中推导Go行为。

候选仍需控制方独立审查，Task6完整make及终态审计尚未执行。可选真实镜像测试未执行；没有已知未修复业务失败。

提交前另检查了暂存树。第一次严格整树比较被控制方正在维护的计划Markdown差异阻止，未提交；随后按既定规则验证全部非Markdown相同，只暂存明确36个源码路径，未暂存根计划。Git将4对移动识别为rename，提交显示32个变动项。源码提交成功后的HEAD树查询曾因Windows命令转发把修订语法解析错误而失败；没有重提一次提交，改用git show --format=%T确认已提交的树正确。此辅助命令失败不改变测试结果。

## 精确移除的11项许可

```go
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/model", "ISO.Validate", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckID", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
{pkgISO, "internal/infrastructure/media/delete.go", "cleanupOwnedTree", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
{pkgISO, "internal/infrastructure/media/iso.go", "package-init", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
{pkgISO, "internal/infrastructure/media/iso.go", "New", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
```

## 每轮命令、输出和远端证据

全部证明在本计划vm-logs中，仅读取safe.log。原设置与缓存保留；顶层测试-count=1，race原20分钟。

### t5-red-01

预期RED：新构造器和依赖类型尚不存在。

树 `274f18625f9b3edb394c434277068435ddc68945`；UTC 2026-10-23T18:31:14Z → 2026-10-23T18:31:14Z；exit=1。

远端changes：`空`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
go test ./internal/application/media -run "Test关闭后不能重新启动|Test删除缺少依赖在组装时拒绝" -count=1
```

```text
internal/application/media/lifecycle_test.go:12:7: undefined: NewSupervisor
internal/application/media/lifecycle_test.go:12:34: undefined: SupervisorDeps
internal/application/media/lifecycle_test.go:20:2: undefined: NewDeletion
internal/application/media/lifecycle_test.go:20:14: undefined: DeletionDeps
FAIL	github.com/lober/lober/internal/application/media [build failed]
FAIL
```

### t5-green-compile-01

Wire成功生成；只在启动中关闭回归失败，保留原1秒断言。其余所选基础设施/组装测试通过。格式与Wire完整拉回后才继续。

树 `8c0c5d4454175899594b25a14c2a849eac2d340f`；UTC 2026-10-23T18:36:29Z → 2026-10-23T18:36:45Z；exit=1。

远端changes：`cmd/lober/app.go
cmd/lober/assembly_test.go
cmd/lober/wire_gen.go
internal/application/media/deletion.go
internal/application/media/deletion_fixture_test.go
internal/application/media/deletion_integration_test.go
internal/application/media/lifecycle_test.go
internal/application/media/port/deletion.go
internal/application/media/queue_atomicity_test.go
internal/application/media/supervision_test.go
internal/application/media/supervisor.go
internal/archtest/transition_manifest_test.go
internal/domain/media/deletion.go
internal/infrastructure/media/acquisition_fixture_test.go
internal/infrastructure/media/delete.go
internal/infrastructure/media/delete_test.go
internal/infrastructure/media/deletion_journal.go
internal/infrastructure/media/iso.go`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/wire.go internal/application/media/deletion.go internal/application/media/deletion_fixture_test.go internal/application/media/deletion_integration_test.go internal/application/media/lifecycle_test.go internal/application/media/port/deletion.go internal/application/media/queue_atomicity_test.go internal/application/media/supervision_test.go internal/application/media/supervisor.go internal/archtest/transition_manifest_test.go internal/domain/media/deletion.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/delete_test.go internal/infrastructure/media/deletion_journal.go internal/infrastructure/media/iso.go internal/infrastructure/media/source.go
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober -run "Test关闭|Test删除缺少|Test后台|Test持久化删除|Test删除快照|Test补来源|Test重新导入调度|TestCleanupDeleted|Test装配后台" -count=1
```

```text
--- FAIL: Test关闭能取消正在启动的恢复读取 (1.00s)
    lifecycle_test.go:57: 关闭没有取消启动读取
FAIL
FAIL	github.com/lober/lober/internal/application/media	2.862s
ok  	github.com/lober/lober/internal/infrastructure/media	1.809s
ok  	github.com/lober/lober/cmd/lober	1.196s
FAIL
```

### t5-domain-red-01

预期RED：持仓储、方法值、接口别名三个负例尚未被拒绝。

树 `4fb0ecb9994b27c3f78800ccc3a1a2f65869e2f5`；UTC 2026-10-23T18:39:13Z → 2026-10-23T18:39:14Z；exit=1。

远端changes：`空`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
go test ./internal/archtest -run Test镜像删除领域只调用纯仓储校验 -count=1
```

```text
--- FAIL: Test镜像删除领域只调用纯仓储校验 (0.00s)
        media_domain_rules_test.go:22: allowed=false got=[]
        media_domain_rules_test.go:22: allowed=false got=[]
        media_domain_rules_test.go:22: allowed=false got=[]
FAIL
FAIL	github.com/lober/lober/internal/archtest	0.006s
FAIL
```

### t5-focused-02

聚焦普通/race四包通过，随后完整拉回格式。

树 `da786f00aac06ad1971bc4717bb350085e31df69`；UTC 2026-10-23T18:40:25Z → 2026-10-23T18:40:55Z；exit=0。

远端changes：`internal/application/media/deletion_fixture_test.go
internal/application/media/deletion_integration_test.go
internal/application/media/deletion_order_test.go
internal/application/media/supervisor.go
internal/archtest/media_domain_rules_test.go
internal/archtest/transition_uses_test.go
internal/infrastructure/media/deletion_journal_test.go`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/deletion.go internal/application/media/deletion_fixture_test.go internal/application/media/deletion_integration_test.go internal/application/media/deletion_order_test.go internal/application/media/lifecycle_test.go internal/application/media/port/deletion.go internal/application/media/queue_atomicity_test.go internal/application/media/supervision_test.go internal/application/media/supervisor.go internal/archtest/media_domain_rules_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_uses_test.go internal/domain/media/deletion.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/delete_test.go internal/infrastructure/media/deletion_journal.go internal/infrastructure/media/deletion_journal_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/source.go
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/archtest -run "Test关闭|Test删除|Test等导入|Test坏快照|Test已提交删除|Test旧删除|Test镜像删除领域|Test后台|Test持久化删除|Test补来源|Test重新导入调度|TestCleanupDeleted|Test装配后台" -count=1
go test -race -timeout=20m ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/archtest -run "Test关闭|Test删除|Test等导入|Test坏快照|Test已提交删除|Test旧删除|Test镜像删除领域|Test后台|Test持久化删除|Test补来源|Test重新导入调度|TestCleanupDeleted|Test装配后台" -count=1
```

```text
ok  	github.com/lober/lober/internal/application/media	4.307s
ok  	github.com/lober/lober/internal/infrastructure/media	1.692s
ok  	github.com/lober/lober/cmd/lober	1.300s
ok  	github.com/lober/lober/internal/archtest	0.009s
ok  	github.com/lober/lober/internal/application/media	6.928s
ok  	github.com/lober/lober/internal/infrastructure/media	3.001s
ok  	github.com/lober/lober/cmd/lober	4.706s
ok  	github.com/lober/lober/internal/archtest	1.059s
```

### t5-format-final-01

中间格式轮；聚焦四包通过，拉回所有格式变更。名称中的final不代表最终无写入证据。

树 `94eff053878756fd4d3adbff751c2c34017f039b`；UTC 2026-10-23T18:43:29Z → 2026-10-23T18:43:35Z；exit=0。

远端changes：`internal/application/media/deletion_cleanup_test.go
internal/application/media/deletion_order_test.go
internal/domain/media/deletion_test.go`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/deletion.go internal/application/media/deletion_cleanup_test.go internal/application/media/deletion_fixture_test.go internal/application/media/deletion_integration_test.go internal/application/media/deletion_order_test.go internal/application/media/lifecycle_test.go internal/application/media/port/deletion.go internal/application/media/queue_atomicity_test.go internal/application/media/supervision_test.go internal/application/media/supervisor.go internal/archtest/media_domain_rules_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_uses_test.go internal/domain/media/deletion.go internal/domain/media/deletion_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/delete_test.go internal/infrastructure/media/deletion_journal.go internal/infrastructure/media/deletion_journal_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/source.go
go test ./internal/application/media ./internal/domain/media ./internal/infrastructure/media ./internal/archtest -run "Test删除|Test清理拒绝|Test镜像删除领域" -count=1
```

```text
ok  	github.com/lober/lober/internal/application/media	2.278s
ok  	github.com/lober/lober/internal/domain/media	0.007s
ok  	github.com/lober/lober/internal/infrastructure/media	0.040s
ok  	github.com/lober/lober/internal/archtest	0.009s
```

### t5-full-01

整体exit1。前两条五包普通/race通过，domain普通通过，arch普通失败；之后所有命令未执行。本地同期修改了测试，因此不作最终同树证据。

树 `b6d90a46edb637cf77e5d66c18257a60f4e9e759`；UTC 2026-10-23T18:45:11Z → 2026-10-23T19:05:30Z；exit=1。

远端changes：`空`；untracked：`空`。顶层输出中的skip=2，cached=0。

```sh
go test ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore -count=1 -v
go test -race -timeout=20m ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore -count=1 -v
go test ./internal/domain/media ./internal/archtest -count=1
go test -race -timeout=20m ./internal/domain/media ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
cd web
pnpm --filter @vben/web-ele typecheck
```

```text
--- SKIP: TestIdentifyRealISO (0.00s)
ok  	github.com/lober/lober/internal/application/media	39.175s
ok  	github.com/lober/lober/internal/infrastructure/media	15.813s
ok  	github.com/lober/lober/cmd/lober	21.429s
ok  	github.com/lober/lober/internal/interfaces/console	201.241s
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore	118.599s
--- SKIP: TestIdentifyRealISO (0.00s)
ok  	github.com/lober/lober/internal/application/media	49.952s
ok  	github.com/lober/lober/internal/infrastructure/media	20.428s
ok  	github.com/lober/lober/cmd/lober	86.903s
ok  	github.com/lober/lober/internal/interfaces/console	863.627s
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore	156.941s
ok  	github.com/lober/lober/internal/domain/media	0.008s
--- FAIL: Test第二阶段与向导许可已清零 (0.00s)
--- FAIL: TestIncrementalGateRejectsPackageInExistingRoot (31.92s)
            deps_test.go:228: github.com/lober/lober/internal/domain/media：github.com/lober/lober/internal/domain/repository（internal/domain/media/deletion.go:5:2）违反「层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包」
            deps_test.go:228: github.com/lober/lober/internal/domain/media：github.com/lober/lober/internal/domain/repository（internal/domain/media/deletion.go:5:2）违反「层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包」
--- FAIL: Test全模块依赖守卫 (8.23s)
    deps_test.go:228: github.com/lober/lober/internal/domain/media：github.com/lober/lober/internal/domain/repository（internal/domain/media/deletion.go:5:2）违反「层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包」
--- FAIL: Test主机许可已清零 (0.00s)
    hosts_rules_test.go:262: mediaMigrationExceptions=0 want 11
    hosts_rules_test.go:293: mediaMigrationExceptions changed beyond the exact phase-three removals: f40cfcc7ff5a4df292a2c69883f49507c66a9870d6ada2eaa0b7dc1b5949e788
    hosts_rules_test.go:297: total=107 want 118
--- FAIL: Test镜像过渡方法和唯一通知转发必须精确 (0.00s)
    transition_rules_test.go:100: internal/infrastructure/media/delete.go [github.com/lober/lober/internal/infrastructure/media：github.com/lober/lober/internal/domain/repository.ISOs.GetISO（internal/infrastructure/media/delete.go:1:166）违反「迁移豁免必须精确到调用包、文件、函数、目标包和符号，并写明总路线的清除阶段」：internal/infrastructure/media/delete.go:(*Sources).confirmDeleted 未登记]
FAIL
FAIL	github.com/lober/lober/internal/archtest	41.196s
FAIL
```

### t5-import-red-01

精确导入正例按预期失败；repositoryother测试假设同时失败，随后修正反例，不修改通用领域依赖规则。

树 `3d2034f5d2909a62f57e98c591423a0f142e20da`；UTC 2026-10-23T19:10:19Z → 2026-10-23T19:10:20Z；exit=1。

远端changes：`空`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
go test ./internal/archtest -run Test镜像删除领域导入只开放精确包 -count=1
```

```text
--- FAIL: Test镜像删除领域导入只开放精确包 (0.00s)
        media_domain_rules_test.go:56: allowed=true got=[github.com/lober/lober/internal/domain/media：github.com/lober/lober/internal/domain/repository（internal/domain/media/x.go:4:2）违反「层 1 只依赖标准库和层 1 自己的包，不 import 别的模块或本模块的其他包」]
        media_domain_rules_test.go:56: allowed=false got=[]
FAIL
FAIL	github.com/lober/lober/internal/archtest	0.006s
FAIL
```

### t5-freeze-focused-01

应用新用例普通/race、domain/arch全普通/race、Wire diff通过。最后Vue全类型因vue-tsc/node_modules缺失失败，整体exit1。格式已完整拉回后才冻结最终树。

树 `c552968aad92ddd34db8923f1568bb6359a30421`；UTC 2026-10-23T19:11:10Z → 2026-10-23T19:16:45Z；exit=1。

远端changes：`internal/application/media/control_persistence_test.go
internal/application/media/deletion_order_test.go
internal/archtest/media_domain_rules_test.go`；untracked：`空`。顶层输出中的skip=0，cached=0。

```sh
gofmt -w internal/application/media/control_persistence_test.go internal/application/media/deletion_order_test.go internal/archtest/hosts_rules_test.go internal/archtest/authoring_rules_test.go internal/archtest/transition_rules_test.go internal/archtest/deps_test.go internal/archtest/media_domain_rules_test.go
go test ./internal/application/media -run "Test控制器只在保存成功后唤醒|Test删除和补来源按两种先后次序保护真实文件" -count=1 -v
go test -race -timeout=20m ./internal/application/media -run "Test控制器只在保存成功后唤醒|Test删除和补来源按两种先后次序保护真实文件" -count=1 -v
go test ./internal/domain/media ./internal/archtest -count=1
go test -race -timeout=20m ./internal/domain/media ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
cd web
pnpm --filter @vben/web-ele typecheck
```

```text
ok  	github.com/lober/lober/internal/application/media	2.535s
ok  	github.com/lober/lober/internal/application/media	3.716s
ok  	github.com/lober/lober/internal/domain/media	0.006s
ok  	github.com/lober/lober/internal/archtest	132.241s
ok  	github.com/lober/lober/internal/domain/media	1.025s
ok  	github.com/lober/lober/internal/archtest	177.946s
$ vue-tsc --noEmit --skipLibCheck
sh: 1: vue-tsc: not found
[ERR_PNPM_RECURSIVE_RUN_FIRST_FAIL] @vben/web-ele@5.8.0 typecheck: `vue-tsc --noEmit --skipLibCheck`
spawn ENOENT
[WARN]  Local package.json exists, but node_modules missing, did you mean to install?
```

### t5-final-scoped-01

最终冻结树检查，范围由控制方依据完整路径比较批准：应用全包普通/race、domain/arch全普通/race、Wire diff、31个明确Go文件只读格式检查、git diff、控制台契约生成类型校验。没有重跑未变化的其他四包，也没有声称完整Vue类型校验通过。

树 `4f6048df22d97ae59ac5bc39bc2734f135d5fc5c`；UTC 2026-10-23T19:20:04Z → 2026-10-23T19:27:03Z；exit=0。

远端changes：`空`；untracked：`空`。顶层输出中的skip=2，cached=0。

```sh
go test ./internal/application/media -count=1 -v
go test -race -timeout=20m ./internal/application/media -count=1 -v
go test ./internal/domain/media ./internal/archtest -count=1
go test -race -timeout=20m ./internal/domain/media ./internal/archtest -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
task_fmt=$(gofmt -l cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/wire.go cmd/lober/wire_gen.go internal/application/media/control_persistence_test.go internal/application/media/deletion.go internal/application/media/deletion_cleanup_test.go internal/application/media/deletion_fixture_test.go internal/application/media/deletion_integration_test.go internal/application/media/deletion_order_test.go internal/application/media/lifecycle_test.go internal/application/media/port/deletion.go internal/application/media/queue_atomicity_test.go internal/application/media/supervision_test.go internal/application/media/supervisor.go internal/archtest/authoring_rules_test.go internal/archtest/deps_test.go internal/archtest/hosts_rules_test.go internal/archtest/media_domain_rules_test.go internal/archtest/transition_manifest_test.go internal/archtest/transition_rules_test.go internal/archtest/transition_uses_test.go internal/domain/media/deletion.go internal/domain/media/deletion_test.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/delete.go internal/infrastructure/media/delete_test.go internal/infrastructure/media/deletion_journal.go internal/infrastructure/media/deletion_journal_test.go internal/infrastructure/media/iso.go internal/infrastructure/media/source.go)
if [ -n "$task_fmt" ]; then printf "%s\n" "$task_fmt"; exit 1; fi
git diff --check
git diff --exit-code
pnpm --dir web check:console-types
```

```text
--- SKIP: TestIdentifyRealISO (0.00s)
ok  	github.com/lober/lober/internal/application/media	38.934s
--- SKIP: TestIdentifyRealISO (0.00s)
ok  	github.com/lober/lober/internal/application/media	48.878s
ok  	github.com/lober/lober/internal/domain/media	0.006s
ok  	github.com/lober/lober/internal/archtest	133.465s
ok  	github.com/lober/lober/internal/domain/media	1.030s
ok  	github.com/lober/lober/internal/archtest	181.564s
$ node scripts/generate-console-types.mjs --check
```

## 全文件集合比较

比较git已跟踪和非忽略新增文件的完整并集，按git规范化blob散列；不只比已修改文件。第一份是full-01对冻结树的精确差异；第二份是最终无写入树对提交候选（允许报告Markdown）的比较。

```json
{
  "label": "t5-full-01",
  "tested_tree": "b6d90a46edb637cf77e5d66c18257a60f4e9e759",
  "compared_complete_union": 2032,
  "local_files": 2032,
  "tested_files": 2031,
  "non_markdown_differences": [
    "internal/application/media/control_persistence_test.go",
    "internal/application/media/deletion_order_test.go",
    "internal/archtest/authoring_rules_test.go",
    "internal/archtest/deps_test.go",
    "internal/archtest/hosts_rules_test.go",
    "internal/archtest/media_domain_rules_test.go",
    "internal/archtest/transition_rules_test.go"
  ],
  "markdown_differences": []
}
```

```json
{
  "label": "t5-final-scoped-01",
  "tested_tree": "4f6048df22d97ae59ac5bc39bc2734f135d5fc5c",
  "compared_complete_union": 2033,
  "local_files": 2033,
  "tested_files": 2032,
  "non_markdown_differences": [],
  "markdown_differences": [
    "docs/superpowers/reviews/2026-10-23-ddd-phase4-task5-execution.md"
  ]
}
```

```json
{
  "tested_tree": "4f6048df22d97ae59ac5bc39bc2734f135d5fc5c",
  "source_commit_tree": "469e1f9a8fae310d24e555190ad6e22ecf7a46c7",
  "non_markdown_differences": [],
  "markdown_differences": [
    "docs/superpowers/plans/2026-10-23-backend-ddd-phase4-media-jobs.md"
  ]
}
```

## 源码提交文件

```text
M	cmd/lober/app.go
M	cmd/lober/assembly_test.go
D	cmd/lober/deletion.go
M	cmd/lober/wire.go
M	cmd/lober/wire_gen.go
A	internal/application/media/control_persistence_test.go
A	internal/application/media/deletion.go
A	internal/application/media/deletion_cleanup_test.go
A	internal/application/media/deletion_fixture_test.go
R056	cmd/lober/deletion_test.go	internal/application/media/deletion_integration_test.go
A	internal/application/media/deletion_order_test.go
A	internal/application/media/lifecycle_test.go
A	internal/application/media/port/deletion.go
R079	cmd/lober/queue_atomicity_test.go	internal/application/media/queue_atomicity_test.go
R084	cmd/lober/supervision_test.go	internal/application/media/supervision_test.go
R057	cmd/lober/supervisor.go	internal/application/media/supervisor.go
M	internal/archtest/authoring_rules_test.go
M	internal/archtest/deps_test.go
M	internal/archtest/hosts_rules_test.go
A	internal/archtest/media_domain_rules_test.go
M	internal/archtest/transition_manifest_test.go
M	internal/archtest/transition_rules_test.go
M	internal/archtest/transition_uses_test.go
A	internal/domain/media/deletion.go
A	internal/domain/media/deletion_test.go
M	internal/infrastructure/media/acquisition_fixture_test.go
M	internal/infrastructure/media/delete.go
M	internal/infrastructure/media/delete_test.go
A	internal/infrastructure/media/deletion_journal.go
A	internal/infrastructure/media/deletion_journal_test.go
M	internal/infrastructure/media/iso.go
M	internal/infrastructure/media/source.go
```

控制方已独立复算2032路径，批准仅补受变化影响的应用与架构包。正确的网页相关验证是 `pnpm --dir web check:console-types`；没有修改web文件、没有安装依赖。Vue全类型的缺失失败与本项契约检查分别记录。
