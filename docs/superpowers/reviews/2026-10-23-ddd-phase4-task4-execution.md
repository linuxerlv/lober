# 第四阶段 Task4 执行记录

本项将导入与识别编排迁到同一个 `application/media.Sources`，文件层只负责读取、解析、提取和发布。候选等待主控的独立固定提交审查；本记录不代替审查通过结论。

## 实现与边界

- `SourceDeps.Import` 注入 `port.ImportFiles`；原 `Sources` 新增同步 `Import(ctx,id)`。后台的来源执行者与导入执行者是同一个应用对象。`supervisorOf` 不再接收旧对象；旧 `iso.Sources` 只留下删除清理。Wire 已在虚拟机重新生成，所有路径仍共用原来同一组来源锁和登记闸门。
- 应用保留原顺序：编号与取消检查、导入锁、镜像状态、来源齐备、主源打开与摘要、识别与系检查、额外介质、保存 importing、临时目录、提取、全部来源复核、先共享树后制品、清除旧错误、保存 ready。
- `domain/media` 只做纯计算。包括 `Identified`、`EditionFact`、`CheckIdentified`、`CheckImport`、`AllFetched`、文本元数据解析、SUSE 布局条件、Debian 网络介质精确版本绑定、Windows 版本/架构/版本标签检查和仓库完整性判断。恢复与重新导入共用 `AllFetched`。没有文件、网络、工具、时钟或随机读取。
- 应用的 `identify.go/suse.go/netboot.go/repository_media.go` 按旧顺序查询 `MediaView`。SUSE 优先于通用 .treeinfo，支持 Agama 嵌套 install 目录；ESXi 没有可靠版本仍拒绝。文本上限仍为 1MiB。Windows 只读取 WIM 头与 XML 元数据，原顺序读取器和压缩解析实现留在基础设施。
- 提取使用应用给出的识别结果、RepoPaths 和 RequiredRoles。适配器不再调用 catalog 选择平台政策，也不生成业务 Problem。外部错误事实由应用转成原问题代码；独立合并错误仍保留，已知故障的内部原因仍隐藏。
- 文件会话保留目标盘临时兄弟目录、owner 文件、完整同内容树复用、不同内容拒绝覆盖、临时树和 Windows 共享副本清理。部分发布失败仍返回 Publication；回滚仅撤销本会话新建且 owner 仍匹配的树。
- 最终保存失败时仍用 `context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)` 重读。只有同摘要、同 artifacts 且 ready 才认为成功。不确定时保留文件。只有确认同摘要且非 ready/deprecated 才允许回滚。取消、普通版本竞争和废弃仍不写导入失败；内容错误保留原 4096 字节错误保存边界。

## 主控批准的最小接口补充

1. 原 MediaView 只有 Stat/Text/WIM/Digest，无法实现 Debian 对任意 dists 套件和 pool 子目录的检查。主控批准增加 `Entries(context.Context,string)([]Entry,error)`，只返回按名称排序的直接子项；Entry 增加 Name 和 IsRegular，保留 Size/IsDir。不存在仍返回标准不存在错误，不暴露文件句柄或绝对路径。
2. 将 Debian 检查移到应用后，不能越过旧临时目录创建错误。主控批准 `BeginPrepare(context.Context,ExtractionPlan) error`，只执行旧临时根创建。应用先保存 importing，再 BeginPrepare、Debian 检查、Prepare。Prepare 不再重建临时根；Windows 共享临时目录仍在提取和必需产物检查之后创建。
3. 格式对应的复制动作及 .treeinfo 相对仓库根解析仍是文件机制，留在适配器。平台政策与 Debian 可用性判断归应用/领域。

## 测试归属与新增覆盖

原 37 个具名相关测试全部保留。26 个导入/识别行为测试迁到应用层，11 个读取/格式解析测试留在基础设施。包括 ImportFormats、OfficialSUSE、OfficialDebian、RechecksEverySource、PreservesPublicationAfterLostCommitAcknowledgment、ExistingDestinationNeverOverwritten。WIM 不支持的三十二位架构事实断言仍在基础设施；识别拒绝它的业务断言在应用层。

新增受控步骤覆盖：

- `Test镜像导入提交未知不删已发布内容`：真实数据库提交成功后丢失确认，后续读取不可用；两次独立五秒确认读取，制品和共享树都保留。
- `Test镜像多源完成复制后才复核`：确认复制文件存在后，在 Verify 边界改动额外源；检查原问题代码、额外角色和没有发布。
- `Test镜像导入中废弃不能被失败标记覆盖`：在文件发布边界提交 ready 再废弃，旧导入最终保存失败；废弃状态、内容与空来源错误保持。
- BeginPrepare 失败先于 Debian 扫描；Debian 检查失败不复制并清理临时目录。
- 有效额外源打开边界取消仍返回取消，不误报内容失败。
- Windows 共享树先发布，制品目录冲突只撤销本次共享树，保留其他拥有者内容。
- 故障转换保留独立合并错误；原纯领域版本、架构、版本标签、额外源与官方构建绑定规则。

所有新增时序测试使用明确的数据库/文件步骤，不依赖随机等待。迁移的 mutateImportContext 继续使用互斥保护，不恢复旧数据竞争。

## 本轮失败与处理

- 初始 RED 是目标能力不存在：领域 Identified/CheckIdentified/AllFetched 和 ImportFiles 契约/实现均未定义，符合预期。
- 第一次迁移运行中 `TestImportExistingDestinationNeverOverwritten` 失败。原因是带包装的 PublicationConflict 没有转成原 repository.ErrConflict。修正为逐分支递归转换，保留独立错误，后续该测试通过。
- 自查发现本次增加的 OpenSource 入口取消检查会改变旧错误顺序。先补定点测试观察 RED：有效额外源被误报为 iso_extra_source_invalid。随后恢复旧顺序：结构解析后由摘要读取响应取消，后续通过。这是修正本次迁移偏差，没有改变基线业务规则。
- 首次最终验证存在源码/证据失配：上传先于最后 Wire 文件拉回完全结束，快照包含旧生成调用。cmd/lober 构建失败，Test全模块依赖守卫、TestIncrementalGateRejectsPackageInExistingRoot 因同一参数数目错误失败；race 和后续检查没有启动。该轮不算最终证据。等待任务完整退出后，以完整非 Markdown 路径并集逐文件核对新快照，再从头运行相同普通、race、Wire 和干净目录命令。
- 本地编辑脚本曾出现 Python 嵌套引号语法错误及默认编码读取错误；均在本地编辑阶段修正，未运行本机 Go。没有通过减弱超时、设置、测试准备缓存或断言来消除失败。

## 自查与限制

已逐项核对导入锁、状态/错误顺序、懒打开、临时目录失败的清理时点、全来源末尾复核、部分发布事实、未知提交结果、废弃保护和 owner 权限。旧导入业务实现已删除；基础设施没有引入应用具体服务或新增永久旧流程许可。仅测试使用的旧路径辅助方法已移回测试文件。

本项没有运行整仓库 `make check`，按任务分工留给 Task6。未提交根计划与主控记录，未修改其他工作区，未合并、推送或晋级。未读取连接凭据、私有原始日志或本地数据库连接内容。

实物镜像输入缺失，最终普通与 race 各有两个可选测试明确跳过。Windows 测试使用测试目录中的工具样本，不能证明真实工具或完整实机安装兼容性。

## 固定提交与最终验证

源码候选：`75b04a548a0fdcfd27079d621b212bb2965dbc09`。提交标题：`refactor(media): move import decisions into media application`。基线：`fae02dcbfe5150b17988acb0fe5c9862834cc5a9`。

最终无写入测试快照：`b696ddc307f72e10511d6e7097fa9639224bdcfb`。2026-10-23 18:07:58Z 至 18:17:25Z，总计 9 分 27 秒，退出码 0。源码提交与快照做了全树非 Markdown 比较，差异为空；提交前另对 58 个改动路径并集核对 blob，全部相同。快照与源码提交只有主控未提交的根计划 Markdown 差异。本报告在测试后写入，没有修改代码。

最终普通和 race 各 9 个包通过，另有 port 包无测试文件；各 267 个顶层测试通过，含子项共 1380 个通过，0 个失败。两轮各跳过 `TestIdentifyRealISO`、`TestRealISO`，原因均为 `LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in`。没有因工具缺失跳过。测试样本通过不等于真实镜像安装与工具全面兼容。

两轮外层均使用 `-count=1`，外层包结果没有 `(cached)`。架构反例 `TestIncrementalGateRejectsPackageInExistingRoot` 内部子命令每轮有 62 条 `(cached)` 输出；未改变测试准备缓存或架构守卫子命令。不能把这部分说成全链路无缓存。

`WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober` 和 `git diff --exit-code` 均通过。最终 `.changes` 与 `.untracked` 都是 0 字节。VM 使用暂存快照作为源码基线，末尾 `git status --porcelain` 输出 2019 条 `A  `，全部是初始已暂存文件，没有未暂存修改或新未跟踪文件；不把该 status 说成空输出。

| 包 | 普通耗时（秒） | race 耗时（秒） |
|---|---:|---:|
| `internal/domain/media` | 0.006 | 1.026 |
| `internal/infrastructure/media` | 15.032 | 20.610 |
| `internal/infrastructure/media/iso9660` | 0.097 | 1.389 |
| `internal/infrastructure/media/isotest` | 0.031 | 3.093 |
| `internal/application/media` | 33.494 | 44.562 |
| `cmd/lober` | 29.047 | 111.986 |
| `internal/infrastructure/persistence/pgstore/pgtest` | 1.402 | 3.244 |
| `internal/infrastructure/persistence/pgstore` | 134.252 | 164.930 |
| `internal/archtest` | 160.995 | 187.293 |

## 每轮 VM 证据

所有命令由本任务 `vm.py` 在隔离虚拟机串行执行。本机未运行 Go、gofmt、Wire 或 Node 生成。使用默认既有环境及原 20 分钟 race 超时，未放宽配置。安全日志和证明保存在本任务目录 `vm-logs/<label>.*`；命令与快照分别在 `<label>.commands`、`<label>.snapshot.json`。

| 运行 | 固定树 | UTC 开始—结束 | 退出码 | changes/untracked 字节 |
|---|---|---|---:|---:|
| `t4-red-1` | `948898a76b1b113f471120c70a5458800b8b1666` | 2026-10-23T17:43:55Z—2026-10-23T17:43:56Z | 1 | 0/0 |
| `t4-green-1` | `6181103bbf3c483736b08107590b5c9155f8ff51` | 2026-10-23T17:50:39Z—2026-10-23T17:51:05Z | 1 | 1248/0 |
| `t4-green-2` | `998860e4129bce9e0c0710c07d1b9aa713e1ff48` | 2026-10-23T17:54:02Z—2026-10-23T17:54:40Z | 0 | 219/0 |
| `t4-cancel-red` | `2ca16de4ff9949b797379a0e1d584184784edb74` | 2026-10-23T17:57:13Z—2026-10-23T17:57:17Z | 1 | 0/0 |
| `t4-green-3` | `6ff8466480bf2afb5ee5d7cf6131bf8c1bda3b6a` | 2026-10-23T17:58:32Z—2026-10-23T17:59:22Z | 0 | 237/0 |
| `t4-final-format` | `d117428bb0d38831957c58e8b2350a3239c363f6` | 2026-10-23T18:02:15Z—2026-10-23T18:02:36Z | 0 | 183/0 |
| `t4-final-verify` | `dae3aa25264378701af94791e664184709f3a81a` | 2026-10-23T18:04:16Z—2026-10-23T18:06:41Z | 1 | 0/0 |
| `t4-final-verify-2` | `b696ddc307f72e10511d6e7097fa9639224bdcfb` | 2026-10-23T18:07:58Z—2026-10-23T18:17:25Z | 0 | 0/0 |

带格式化的轮次 changes 非空属预期，按明确 Go 文件列表拉回并等待完成。最终 Wire 生成文件 `cmd/lober/wire_gen.go` 也明确拉回。最终通过证据只使用 `t4-final-verify-2`；不拼接失配轮通过的包。

### t4-red-1

```sh
go test -count=1 ./internal/domain/media ./internal/infrastructure/media
```

结果摘录（完整输出见该轮安全日志）：

```text
# github.com/lober/lober/internal/domain/media [github.com/lober/lober/internal/domain/media.test]
internal/domain/media/rules_test.go:10:36: undefined: Identified
internal/domain/media/rules_test.go:11:4: undefined: Identified
internal/domain/media/rules_test.go:12:4: undefined: Identified
internal/domain/media/rules_test.go:15:13: undefined: CheckIdentified
internal/domain/media/rules_test.go:17:12: undefined: CheckIdentified
internal/domain/media/rules_test.go:17:28: undefined: Identified
internal/domain/media/rules_test.go:24:5: undefined: AllFetched
internal/domain/media/rules_test.go:25:6: undefined: AllFetched
internal/domain/media/rules_test.go:27:5: undefined: AllFetched
FAIL	github.com/lober/lober/internal/domain/media [build failed]
# github.com/lober/lober/internal/infrastructure/media [github.com/lober/lober/internal/infrastructure/media.test]
internal/infrastructure/media/import_contract_test.go:11:12: undefined: port.ImportFiles
internal/infrastructure/media/import_contract_test.go:11:28: undefined: ImportFiles
internal/infrastructure/media/import_contract_test.go:14:11: undefined: NewImportFiles
FAIL	github.com/lober/lober/internal/infrastructure/media [build failed]
FAIL
```

### t4-green-1

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion_test.go internal/application/media/identify.go internal/application/media/identify_real_test.go internal/application/media/identify_test.go internal/application/media/import.go internal/application/media/import_error.go internal/application/media/import_fixture_test.go internal/application/media/import_review_test.go internal/application/media/import_test.go internal/application/media/netboot.go internal/application/media/official_media_test.go internal/application/media/port/import.go internal/application/media/recover.go internal/application/media/repository_media.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/suse.go internal/application/media/wim_fixture_test.go internal/application/media/windows_test.go internal/archtest/deps_test.go internal/archtest/domain_rules_test.go internal/archtest/four_layer_test.go internal/archtest/transition_manifest_test.go internal/domain/media/identify.go internal/domain/media/netboot.go internal/domain/media/rules.go internal/domain/media/rules_test.go internal/domain/media/suse.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/extract.go internal/infrastructure/media/identify.go internal/infrastructure/media/identify_test.go internal/infrastructure/media/import.go internal/infrastructure/media/import_contract_test.go internal/infrastructure/media/import_fixture_test.go internal/infrastructure/media/import_session.go internal/infrastructure/media/source.go internal/infrastructure/media/wim.go internal/infrastructure/media/wim_test.go internal/infrastructure/media/windows.go internal/infrastructure/media/windows_test.go
go test -count=1 ./internal/domain/media ./internal/application/media ./internal/infrastructure/media ./cmd/lober -run "Test镜像识别|Test镜像恢复必须|Test导入会话|TestImport|TestIdentify|TestDebian|Test装配后台获取导入及启动恢复"
```

结果摘录（完整输出见该轮安全日志）：

```text
ok  	github.com/lober/lober/internal/domain/media	0.005s
--- FAIL: TestImportExistingDestinationNeverOverwritten (0.58s)
    import_test.go:405: publication_conflict: artifact destination already belongs to other content
FAIL
FAIL	github.com/lober/lober/internal/application/media	22.795s
ok  	github.com/lober/lober/internal/infrastructure/media	0.015s
ok  	github.com/lober/lober/cmd/lober	1.369s
FAIL
```

### t4-green-2

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion_test.go internal/application/media/identify.go internal/application/media/identify_real_test.go internal/application/media/identify_test.go internal/application/media/import.go internal/application/media/import_error.go internal/application/media/import_fixture_test.go internal/application/media/import_order_test.go internal/application/media/import_review_test.go internal/application/media/import_test.go internal/application/media/netboot.go internal/application/media/official_media_test.go internal/application/media/port/import.go internal/application/media/recover.go internal/application/media/repository_media.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/suse.go internal/application/media/wim_fixture_test.go internal/application/media/windows_test.go internal/archtest/authoring_rules_test.go internal/archtest/deps_test.go internal/archtest/domain_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/transition_manifest_test.go internal/domain/media/identify.go internal/domain/media/netboot.go internal/domain/media/rules.go internal/domain/media/rules_test.go internal/domain/media/suse.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/extract.go internal/infrastructure/media/identify.go internal/infrastructure/media/identify_test.go internal/infrastructure/media/import.go internal/infrastructure/media/import_contract_test.go internal/infrastructure/media/import_fixture_test.go internal/infrastructure/media/import_session.go internal/infrastructure/media/source.go internal/infrastructure/media/wim.go internal/infrastructure/media/wim_test.go internal/infrastructure/media/windows.go internal/infrastructure/media/windows_test.go
go test -count=1 ./internal/domain/media ./internal/application/media ./internal/infrastructure/media ./cmd/lober ./internal/archtest -run "Test镜像|Test导入会话|TestImport|TestIdentify|TestDebian|Test装配后台获取导入及启动恢复|Test全模块依赖守卫|Test主机许可已清零|Test第二阶段与向导许可已清零|Test领域拒绝随机数和外部操作|Test未知包不能绕过四层检查"
```

结果摘录（完整输出见该轮安全日志）：

```text
ok  	github.com/lober/lober/internal/domain/media	0.005s
ok  	github.com/lober/lober/internal/application/media	34.021s
ok  	github.com/lober/lober/internal/infrastructure/media	0.393s
ok  	github.com/lober/lober/cmd/lober	1.139s
ok  	github.com/lober/lober/internal/archtest	8.323s
```

### t4-cancel-red

```sh
go test -count=1 ./internal/application/media -run Test镜像打开有效额外源时取消仍返回取消
```

结果摘录（完整输出见该轮安全日志）：

```text
--- FAIL: Test镜像打开有效额外源时取消仍返回取消 (0.47s)
    import_cancel_order_test.go:22: 有效文件的取消被误报为无效额外源 iso_extra_source_invalid（iso 5rtnbuojouglill5hquzquh3l4 source.netboot）：附加来源 netboot 缺失、不完整或与主介质的平台和版本不符
FAIL
FAIL	github.com/lober/lober/internal/application/media	0.484s
FAIL
```

### t4-green-3

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion_test.go internal/application/media/identify.go internal/application/media/identify_real_test.go internal/application/media/identify_test.go internal/application/media/import.go internal/application/media/import_cancel_order_test.go internal/application/media/import_error.go internal/application/media/import_fixture_test.go internal/application/media/import_order_test.go internal/application/media/import_review_test.go internal/application/media/import_test.go internal/application/media/netboot.go internal/application/media/official_media_test.go internal/application/media/port/import.go internal/application/media/recover.go internal/application/media/repository_media.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/suse.go internal/application/media/wim_fixture_test.go internal/application/media/windows_test.go internal/archtest/authoring_rules_test.go internal/archtest/deps_test.go internal/archtest/domain_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/transition_manifest_test.go internal/domain/media/compatibility_test.go internal/domain/media/identify.go internal/domain/media/netboot.go internal/domain/media/rules.go internal/domain/media/rules_test.go internal/domain/media/suse.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/doc.go internal/infrastructure/media/extract.go internal/infrastructure/media/identify.go internal/infrastructure/media/identify_test.go internal/infrastructure/media/import.go internal/infrastructure/media/import_contract_test.go internal/infrastructure/media/import_fixture_test.go internal/infrastructure/media/import_session.go internal/infrastructure/media/source.go internal/infrastructure/media/wim.go internal/infrastructure/media/wim_test.go internal/infrastructure/media/windows.go internal/infrastructure/media/windows_test.go
go test -count=1 ./internal/domain/media ./internal/application/media ./internal/infrastructure/media/... ./cmd/lober ./internal/archtest -run "Test镜像|Test导入会话|TestImport|TestIdentify|TestDebian|Test装配后台获取导入及启动恢复|Test全模块依赖守卫|Test主机许可已清零|Test第二阶段与向导许可已清零|Test领域拒绝随机数和外部操作|Test未知包不能绕过四层检查|Test网络介质"
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

结果摘录（完整输出见该轮安全日志）：

```text
ok  	github.com/lober/lober/internal/domain/media	0.006s
ok  	github.com/lober/lober/internal/application/media	33.750s
ok  	github.com/lober/lober/internal/infrastructure/media	0.355s
ok  	github.com/lober/lober/internal/infrastructure/media/iso9660	0.006s [no tests to run]
ok  	github.com/lober/lober/internal/infrastructure/media/isotest	0.005s [no tests to run]
ok  	github.com/lober/lober/cmd/lober	1.195s
ok  	github.com/lober/lober/internal/archtest	7.758s
✓ wire: github.com/lober/lober/cmd/lober: unchanged /home/dev/lober-ddd-phase4-media-20261023/source/cmd/lober/wire_gen.go (6240.97ms)
```

此轮 iso9660/isotest 因名称筛选显示 `[no tests to run]`；最终全包两轮均实际执行。

### t4-final-format

```sh
gofmt -w cmd/lober/app.go cmd/lober/assembly_test.go cmd/lober/deletion_test.go internal/application/media/identify.go internal/application/media/identify_real_test.go internal/application/media/identify_test.go internal/application/media/import.go internal/application/media/import_cancel_order_test.go internal/application/media/import_error.go internal/application/media/import_error_test.go internal/application/media/import_fixture_test.go internal/application/media/import_order_test.go internal/application/media/import_publication_test.go internal/application/media/import_review_test.go internal/application/media/import_test.go internal/application/media/netboot.go internal/application/media/official_media_test.go internal/application/media/port/import.go internal/application/media/recover.go internal/application/media/repository_media.go internal/application/media/retry.go internal/application/media/sources.go internal/application/media/suse.go internal/application/media/wim_fixture_test.go internal/application/media/windows_test.go internal/archtest/authoring_rules_test.go internal/archtest/deps_test.go internal/archtest/domain_rules_test.go internal/archtest/four_layer_test.go internal/archtest/hosts_rules_test.go internal/archtest/transition_manifest_test.go internal/domain/media/compatibility_test.go internal/domain/media/identify.go internal/domain/media/netboot.go internal/domain/media/rules.go internal/domain/media/rules_test.go internal/domain/media/suse.go internal/infrastructure/media/acquisition_fixture_test.go internal/infrastructure/media/doc.go internal/infrastructure/media/extract.go internal/infrastructure/media/identify.go internal/infrastructure/media/identify_test.go internal/infrastructure/media/import.go internal/infrastructure/media/import_contract_test.go internal/infrastructure/media/import_fixture_test.go internal/infrastructure/media/import_session.go internal/infrastructure/media/source.go internal/infrastructure/media/wim.go internal/infrastructure/media/wim_test.go internal/infrastructure/media/windows.go internal/infrastructure/media/windows_test.go
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
go test -count=1 ./internal/domain/media ./internal/application/media ./internal/infrastructure/media ./cmd/lober -run "Test镜像文件故障转换保留并列错误|Test镜像共享树先发布且冲突只撤销本次新树|TestWIM不把三十二位映射成六十四位|Test装配后台获取导入及启动恢复|TestIdentifyWIMRejects32BitArchitecture"
```

结果摘录（完整输出见该轮安全日志）：

```text
✓ wire: github.com/lober/lober/cmd/lober: wrote /home/dev/lober-ddd-phase4-media-20261023/source/cmd/lober/wire_gen.go (5994.24ms)
ok  	github.com/lober/lober/internal/domain/media	0.005s [no tests to run]
ok  	github.com/lober/lober/internal/application/media	0.555s
ok  	github.com/lober/lober/internal/infrastructure/media	0.014s
ok  	github.com/lober/lober/cmd/lober	1.051s
```

此轮 domain 因名称筛选显示 `[no tests to run]`；最终全包两轮均实际执行。

### t4-final-verify

```sh
go test -json -count=1 ./internal/domain/media ./internal/application/media/... ./internal/infrastructure/media/... ./cmd/lober ./internal/infrastructure/persistence/pgstore/... ./internal/archtest
go test -json -race -count=1 -timeout=20m ./internal/domain/media ./internal/application/media/... ./internal/infrastructure/media/... ./cmd/lober ./internal/infrastructure/persistence/pgstore/... ./internal/archtest
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
git diff --exit-code
git status --porcelain
```

结果摘录（完整输出见该轮安全日志）：

```text
cmd/lober/wire_gen.go:38:122: too many arguments in call to supervisorOf
{"Action": "fail", "Package": "github.com/lober/lober/cmd/lober", "Elapsed": 0}
{"Action": "fail", "Package": "github.com/lober/lober/internal/archtest", "Test": "TestIncrementalGateRejectsPackageInExistingRoot", "Elapsed": 37.17}
{"Action": "fail", "Package": "github.com/lober/lober/internal/archtest", "Test": "Test全模块依赖守卫", "Elapsed": 8.64}
{"Action": "fail", "Package": "github.com/lober/lober/internal/archtest", "Elapsed": 47.068}
```

### t4-final-verify-2

```sh
go test -json -count=1 ./internal/domain/media ./internal/application/media/... ./internal/infrastructure/media/... ./cmd/lober ./internal/infrastructure/persistence/pgstore/... ./internal/archtest
go test -json -race -count=1 -timeout=20m ./internal/domain/media ./internal/application/media/... ./internal/infrastructure/media/... ./cmd/lober ./internal/infrastructure/persistence/pgstore/... ./internal/archtest
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
git diff --exit-code
git status --porcelain
```

结果：两轮所有包结果见上表，0 失败；每轮两个实物镜像测试跳过。Wire diff、工作区 diff 检查通过。末尾只有初始快照的已暂存文件列表。

## 实际删除的旧流程许可

本项真实删除 40 条 media 活动许可：media 从 51 条降为 11 条，全表从 158 条降为 118 条。剩余 media 为 delete.go 的 9 条、iso.go 的 2 条，待后续任务；没有新增替代许可。原基线指纹保持不变，删除项加入“已删除不得复活”的历史断言，历史表不授权调用。新 domain/media 已加入实际四层分类，以及禁止外部操作的反例检查。

```go
	{pkgISO, "internal/infrastructure/media/suse.go", "identifySUSELayout", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
	{pkgISO, "internal/infrastructure/media/suse.go", "identifySUSELayout", "github.com/lober/lober/internal/domain/catalog", "RepoPaths", "4"},
	{pkgISO, "internal/infrastructure/media/extract.go", "(*Sources).extract", "github.com/lober/lober/internal/domain/catalog", "RepoPaths", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "identifyProblem", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "FormatMedia", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "Formats", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesByEdition", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesMedia", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/catalog", "SeriesOfFormat", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/model", "IsSafeWord", "4"},
	{pkgISO, "internal/infrastructure/media/identify.go", "Identify", "github.com/lober/lober/internal/domain/model", "ValidLabel", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "importProblem", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/catalog", "ExtraSourceOf", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/catalog", "RequiredRoles", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/model", "ISO.Validate", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "CheckID", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ErrImmutable", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOSources.SourcesOf", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).Import", "github.com/lober/lober/internal/domain/repository", "ISOs.UpdateISO", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).readImportOutcome", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).openMedia", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/model", "StorableText", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "Audit", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOSources.SaveSource", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "(*Sources).failImport", "github.com/lober/lober/internal/domain/repository", "ISOs.UpdateISO", "4"},
	{pkgISO, "internal/infrastructure/media/import.go", "publishImportTree", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
	{pkgISO, "internal/infrastructure/media/netboot.go", "identifyNetboot", "github.com/lober/lober/internal/domain/catalog", "Arch.Valid", "4"},
	{pkgISO, "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/model", "NewProblem", "4"},
	{pkgISO, "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
	{pkgISO, "internal/infrastructure/media/source.go", "sourceRejected", "github.com/lober/lober/internal/domain/repository", "ProblemsError", "4"},
	{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).getSource", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
	{pkgISO, "internal/infrastructure/media/source.go", "(*Sources).getSource", "github.com/lober/lober/internal/domain/repository", "ISOSources.GetSource", "4"},
	{pkgISO, "internal/infrastructure/media/windows.go", "(*Sources).extractWindows", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
```

## 改动路径

以下 58 个源文件路径包括迁移的旧删除路径与新路径；Git 识别重命名后提交显示 52 项变更。另新增本执行报告。根计划与其他主控文件没有暂存。

```text
cmd/lober/app.go
cmd/lober/assembly_test.go
cmd/lober/deletion_test.go
cmd/lober/wire_gen.go
internal/application/media/identify.go
internal/application/media/identify_real_test.go
internal/application/media/identify_test.go
internal/application/media/import.go
internal/application/media/import_cancel_order_test.go
internal/application/media/import_error.go
internal/application/media/import_error_test.go
internal/application/media/import_fixture_test.go
internal/application/media/import_order_test.go
internal/application/media/import_publication_test.go
internal/application/media/import_review_test.go
internal/application/media/import_test.go
internal/application/media/netboot.go
internal/application/media/official_media_test.go
internal/application/media/port/import.go
internal/application/media/recover.go
internal/application/media/repository_media.go
internal/application/media/retry.go
internal/application/media/sources.go
internal/application/media/suse.go
internal/application/media/wim_fixture_test.go
internal/application/media/windows_test.go
internal/archtest/authoring_rules_test.go
internal/archtest/deps_test.go
internal/archtest/domain_rules_test.go
internal/archtest/four_layer_test.go
internal/archtest/hosts_rules_test.go
internal/archtest/transition_manifest_test.go
internal/domain/media/compatibility_test.go
internal/domain/media/identify.go
internal/domain/media/netboot.go
internal/domain/media/rules.go
internal/domain/media/rules_test.go
internal/domain/media/suse.go
internal/infrastructure/media/acquisition_fixture_test.go
internal/infrastructure/media/doc.go
internal/infrastructure/media/extract.go
internal/infrastructure/media/identify.go
internal/infrastructure/media/identify_real_test.go
internal/infrastructure/media/identify_test.go
internal/infrastructure/media/import.go
internal/infrastructure/media/import_contract_test.go
internal/infrastructure/media/import_fixture_test.go
internal/infrastructure/media/import_review_test.go
internal/infrastructure/media/import_session.go
internal/infrastructure/media/import_test.go
internal/infrastructure/media/netboot.go
internal/infrastructure/media/official_media_test.go
internal/infrastructure/media/source.go
internal/infrastructure/media/suse.go
internal/infrastructure/media/wim.go
internal/infrastructure/media/wim_test.go
internal/infrastructure/media/windows.go
internal/infrastructure/media/windows_test.go
docs/superpowers/reviews/2026-10-23-ddd-phase4-task4-execution.md
```

## 交接结论

实现和本项指定验证完成；候选等待主控独立审查。已知限制为实物镜像覆盖缺失、真实 Windows 工具与完整安装兼容性未验证。全量 make check 留给 Task6。首次最终验证的生成文件失配已完整留证，并由同一新固定树从头通过全部指定命令，不作为未解决源码故障。
