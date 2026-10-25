# 第三阶段主机迁移证据

第三阶段主机模块的四项任务、完整检查、逐项独立审查及整分支审查均通过，交付 `codex/ddd-hosts` 分支并保留工作区。代码候选为 `b50c2674e37cb88dacd22203ea950d6a654ccf29`；之后只保存本次文档与审查记录。本文保留模块交付时尚未合并的历史过程；用户随后明确合入 `dev`，最新状态见[dev 集成记录](2026-10-23-ddd-phase3-dev-integration.md)。

集成起点为 `a9d5434fc507f8857c5fb36b252a41de1cce0ab3`。六入口及 CSV 兼容约定见[主机契约](../../contracts/hosts.md)。网页经 Registry 六方法执行，原数据库事务、版本、引用、审计、任务快照和锁顺序保留。

## 已完成任务

| 任务 | 本地提交 | 验证 |
| --- | --- | --- |
| 1，固定原行为 | 75f3ada55e901c3a1687c7f1c48649894dc2e39f | t1-baseline-2，树c25688073818ffaca40889c4d68386eb312426d8；退出0；网页12.031秒、数据库20.388秒；独立审查通过 |
| 2，查询与单条写入 | cfc8ddfb2b71f41f98694109ac7889d486cf815d | t2-final-1，树7315d8a1462e0a678cb3d8761ea50783e12fece5；聚焦与Wire退出0。只补组装测试后，t2-assembly-final-1树b526e88097fc962e6bc6b1020f6f3ed1a8875d99退出0；独立审查通过 |
| 3，CSV预演与导入 | 1eecac5ef8800435a259fe6cd0f6c1b61578af9b | t3-green-1树bae3110fecae7ad0b388fea189ad2dff2e73f55f；四组检查退出0。仅补强两份应用测试后，t3-final-1树96eea3ffa3efaacfbdd5daabd818b8600a85b036，应用与格式检查退出0；独立审查通过 |
| 4，边界与完整验收 | b50c2674e37cb88dacd22203ea950d6a654ccf29 | t4-full-2，同树00ad8ee29058bc2bf143edef4e837be2f5f18409退出0；逐项与整分支独立审查通过 |

任务1先取得原行为基线，没有为制造失败改旧代码。首次基线因测试账号无效失败，修复测试数据后重跑。任务2先因契约尚不存在得到真实失败；一次中间检查因残留未使用导入编译失败，另一次因审计故障注入使用无效网页账号失败；只修复相应导入和测试夹具后通过。任务3先因 Import/ImportRequest/CSVFileError 尚不存在失败。失败日志全部保留，不把这些失败算成通过。

任务2的生产组装使用契约替身测试；新增真实数据库测试按计划统一放在网页契约测试。实际 Wire 生成、差异检查和独立源码审查补核连线。代价：替身本身可能漏掉生成连线断开，后续如发现仍需补生产组装验证。

任务4首次执行者使用gpt-6-astra，因模型容量不足，在任何改动前退出。核对干净起点后改用gpt-6.1-sol high继续；独立审查和完整检查不减少。代价：替换执行者可能漏掉细小守卫路径，须由反例与独立审查复核。

## 主机边界和许可

两个主机文件各有15个拒绝例、3个允许例。每例先经 Go 类型检查，再进入原 typedTransitionFindings/wizardHelperFindings。旧业务放在非根文件，允许例不会被常驻违规污染。原向导12个拒绝例及编写六文件套件完整保留。

`t4-red-1`：树 `4f38fd4e9425717817e27e54a5f31d91f8b307b7`，2026-10-23T06:08:33Z至06:08:35Z，退出1。类型检查有效；两个文件各有10个间接调用、小接口或未登记应用能力漏报，断言得到空结果。直接仓储和领域规则已被原检查拒绝。

新增根仅 `hosts.go`、`csv.go`；应用许可仅 Registry.List/Get/Create/Update/Delete/Import。没有 Error 方法许可，通知和任务诊断不扩容。先单独在虚拟机格式化，再取回两份文件、重新暂存后检查。

`t4-green-1`：格式取回并重新暂存后的树 `797e1c4f040135e6f0b687e03494bf32ca215e79`，2026-10-23T06:10:49Z至06:13:27Z，退出0。`go test ./internal/archtest -count=1` 用时156.184秒；`gofmt -l`无输出，`git diff --exit-code`退出0，工具运行后修改和未跟踪文件均为空。

本阶段只删仓储10项和网页规则6项，277降为261。四张真实清单数量固定88/125/47/1。测试补回显式16项后，对每行六字段排序并计算摘要，必须等于起点清单；其他阶段不能新增、删除、改符号或改阶段。合成的格式/匹配样例继续保留。六项正式编号能力及唯一通知转发检查保持。

## 完整验收

### 第二轮完整验证通过

`t4-full-2`被测树仍为 `00ad8ee29058bc2bf143edef4e837be2f5f18409`，与首轮及恢复单例完全相同。2026-10-23T06:50:08Z至06:56:53Z，耗时405秒，整段退出0。工具运行后的源码修改和未跟踪文件均为空。

| 检查 | 实际结果 |
| --- | --- |
| 原make check格式、vet、普通全包 | 通过；强制全模块发现8.849秒，普通架构154.098秒 |
| 原go test -race -timeout=20m ./... | 通过；上轮失败的cmd/lober整包实际重跑137.808秒，架构212.215秒，无数据竞争或超时报告 |
| 数据库与网页全包 | 本轮复用有效缓存；同一被测树首轮实际普通/并发数据库160.346/204.299秒、网页234.409/988.370秒均通过 |
| GOOS=linux GOARCH=amd64 go build ./... | 通过 |
| WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober | 通过，生成文件无变化 |
| pnpm --dir web check:console-types | 通过，实际执行generate-console-types.mjs --check，公开名称及生成结果无差异 |
| git diff --exit-code及最终gofmt -l断言 | 退出0，未改源码 |

本轮普通/并发检查共47条包结果来自Go的有效缓存，其余实际执行；没有清理或改写缓存，没有降低密码强度、扩大超时或改断言。首轮完整失败仍保留，不折算成通过。

实际执行命令如下，set -e使每步均须成功才能继续：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
git diff --exit-code
test -z "$(gofmt -l internal/archtest/hosts_rules_test.go internal/archtest/wizard_helpers_test.go)"
```

| 点名审计 | 结果 |
| --- | --- |
| TestPreseedMirrorSelectionsAreSeenAndAuthenticated | 实际通过，1.42秒；原生Debian预设读取器可用 |
| TestRecordCurrentRelease | 跳过；未开启LOBER_RECORD_RELEASE，一次性不可变发布记录未执行 |
| TestIdentifyRealISO | 跳过；未设置LOBER_TEST_REAL_ISO，真实ISO识别未执行 |
| TestRealISO | 跳过；未设置LOBER_TEST_REAL_ISO，真实ISO读取未执行 |

真实整机安装验收、完整前端构建、浏览器验收、test企业套件、preview人工验收和main发布均未执行。本阶段没有前端业务修改。最终gofmt只查-l，未再写-w。

代码候选 `b50c267` 相对被测树仅以下3份Markdown不同：docs/contracts/hosts.md、docs/superpowers/plans/2026-10-23-backend-ddd-phase3-hosts.md、本文。之后的最终文档归档见文末。生产源码、测试、守卫、Makefile、依赖、前端及生成文件都与被测树相同；暂存与提交后各用Git精确差异复核。独立Task4审查及整分支审查均已通过。


### 第一轮真实失败

t4-full-1：被测树00ad8ee29058bc2bf143edef4e837be2f5f18409，2026-10-23T06:14:13Z至06:42:58Z，总耗时1725秒，make退出2。VM源码修改与未跟踪文件均为空。

格式、vet、强制全模块和普通全包通过；普通console234.409s、pgstore160.346s。race console988.370s、pgstore204.299s、arch243.734s通过；race cmd/lober66.283s失败。0个WARNING: DATA RACE，0个test timed out panic。本轮是一般断言/连接失败，不是数据竞争报告或20分钟超时。

首个失败：Test组件和模板应用同时组装，assembly_test.go:347，原用户改密请求返回500。其cleanup pgtest.go:194及后续测试建连共同出现数据库“the database system is shutting down”，SQLSTATE57P03共20次。首轮普通/并发输出中缓存包结果均为0。前相邻VM时间戳为2026/10/23 06:22:59（UTC）。无panic堆栈。日志显示数据库停机状态；当时先保留归属判断；随后根执行者核实的服务重启事件见下文。

11个顶层失败：

- Test组件和模板应用同时组装
- Test预览组装共用地址且不提前读取配置
- Test空库必须显式初始口令已有用户不再要求
- Test删除快照写失败保留数据及清理失败可重启重试
- TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol
- TestEnabledMachineAssemblyPersistsProtectedKeyAndServesCanonicalRoute
- TestEnabledMachineRejectsInvalidEndpointOrUnprotectedKey
- Test补来源调度失败必须回滚登记和审计
- Test重新导入调度失败必须回滚审计和来源修订
- Test后台工作panic释放资源退避后重试
- Test后台协调panic退避且取消立即退出

后续GOOS/GOARCH构建、Wire diff、Node类型生成、点名skip审计与最终git diff/format断言因make失败未执行。不能把此前阶段成功替代本次结果。原日志和tree/exit/started/finished/changes/untracked、snapshot全部保留。

单一待验证假设：数据库短暂停机使本轮启动组装用例失败，非主机守卫修改导致。证据是共同57P03、首个路径为既有用户改密、后续跨功能测试都无法建连，而本轮其他数据库包稍后实际通过。只读源码搜索未发现这些测试有停止数据库服务的命令。根核实服务事件后，必要时同源码、同race只复跑首个失败例；不修改服务、凭据、DB配置、超时或断言。不提交候选，等待归属裁定。


全部 Go、gofmt、Wire、构建及网页类型生成检查仅在 hostname=lober 的虚拟机执行。实际测试库已核实为 lober_test；夹具使用独立临时 schema。连接信息、凭据和私有路径不进入此记录。

环境固定 Go1.27.1、Node24.16.0、pnpm11.16.0、GOMAXPROCS=2、GOFLAGS=-p=2、GOPROXY=off、LOBER_TEST_BASH=/bin/bash、LOBER_TEST_BUSYBOX=/usr/bin/busybox。不开新服务，不改数据库配置、密码强度、缓存和时限。相同源码、依赖、环境复用有效缓存；全模块发现仍按原 make 的 -count=1 强制运行。

本阶段无前端业务修改，不运行全量前端构建。真实 ISO和一次性发布记录按上述点名检查跳过，整机安装验收未执行。

## 五项审查重点的测试承接

以下是供独立审查逐项核对的证据位置，尚不代表整分支审查已通过。

| 重点 | 测试 |
| --- | --- |
| 更新错误先后、列表prefix不套编号规范化 | Test主机输入多错保持原顺序、Test主机列表保留两种分页与原文过滤、Test主机应用编号版本与整份替换 |
| BOM、引号换行、后段格式错误和全部重复位置 | TestCSV解析保留属性原文与物理行号、TestCSV文件错误、Test主机CSV重复与首错次序、Test主机CSV重复问题置首但保留后续顺序 |
| 预演读取失败可见、预演后正式覆盖最新版本 | Test主机CSV预演与失败优先级、Test主机导入读取先于预演和拒绝、Test主机导入预演后覆盖最新版本 |
| 中间写入、审计与锁等待取消使整批回滚 | Test主机导入审计取消整批回滚、Test契约主机、Test契约审计 |
| 编辑/删除/导入与建任务相撞、已结束任务仍保护引用 | Test主机应用审计失败与删除引用保护、Test主机变更与向导提交按数据库事实交错、Test同时导入主机与建任务不会互相等死、Test同时导入同一组主机不会互相等死 |

数据库原默认隔离级别契约和普通CreateTasks/CurrentTaskCreator区分仍保留。新真实数据库用例统一放在网页hosts_contract_test.go，应用测试只使用契约替身。

## 审查状态

任务4独立审查与整分支审查仍待完成。整分支审查须逐项核对六入口、CSV两种响应、数据库契约及计划五项 Review Focus，之后才能标记模块交付。develop、test、preview、main按仓库流程另行推进。


## 前序验证的精确命令

以下结果只证明对应源码树，不能替代本次完整验收。起止时间均为UTC。

### t1-baseline-2

树 `c25688073818ffaca40889c4d68386eb312426d8`，2026-10-23T05:05:54Z 至 2026-10-23T05:06:41Z，退出0。

```sh
go test ./internal/interfaces/console -run 'Test主机|TestCSV|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发' -count=1
```

### t2-final-1

树 `7315d8a1462e0a678cb3d8761ea50783e12fece5`，2026-10-23T05:27:56Z 至 2026-10-23T05:31:27Z，退出0。

```sh
set -euo pipefail
test -z "$(gofmt -l internal/application/hosts/contract.go internal/application/hosts/registry.go internal/application/hosts/registry_test.go internal/application/hosts/port/repository.go internal/interfaces/console/hosts.go internal/interfaces/console/server.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts_contract_test.go cmd/lober/app.go cmd/lober/wire.go cmd/lober/wire_gen.go cmd/lober/assembly_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/archtest/authoring_rules_test.go)"
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/archtest -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发|Test写事务不随连接默认的隔离级别变' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

### t2-assembly-final-1

树 `b526e88097fc962e6bc6b1020f6f3ed1a8875d99`，2026-10-23T05:36:08Z 至 2026-10-23T05:36:19Z，退出0。

```sh
set -euo pipefail
test -z "$(gofmt -l cmd/lober/assembly_test.go)"
go test ./cmd/lober -run 'Test主机' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

### t3-green-1

树 `bae3110fecae7ad0b388fea189ad2dff2e73f55f`，2026-10-23T05:48:24Z 至 2026-10-23T05:52:08Z，退出0。

```sh
test -z "$(gofmt -l cmd/lober/app.go internal/application/hosts/contract.go internal/application/hosts/csv.go internal/application/hosts/csv_test.go internal/application/hosts/import.go internal/application/hosts/import_test.go internal/application/hosts/port/repository.go internal/archtest/authoring_rules_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/interfaces/console/csv.go internal/interfaces/console/csv_test.go internal/interfaces/console/errors.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts.go internal/interfaces/console/hosts_contract_test.go internal/interfaces/console/hosts_test.go internal/interfaces/console/server.go internal/interfaces/console/tasks_test.go internal/interfaces/console/wizard_test.go)"
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware'
go test ./internal/archtest
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test同时导入主机与建任务不会互相等死|Test同时导入同一组主机不会互相等死|Test写事务不随连接默认的隔离级别变' -count=1
```

### t3-final-1

树 `96eea3ffa3efaacfbdd5daabd818b8600a85b036`，2026-10-23T05:54:59Z 至 2026-10-23T05:55:00Z，退出0。

```sh
test -z "$(gofmt -l cmd/lober/app.go internal/application/hosts/contract.go internal/application/hosts/csv.go internal/application/hosts/csv_test.go internal/application/hosts/import.go internal/application/hosts/import_test.go internal/application/hosts/port/repository.go internal/archtest/authoring_rules_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/interfaces/console/csv.go internal/interfaces/console/csv_test.go internal/interfaces/console/errors.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts.go internal/interfaces/console/hosts_contract_test.go internal/interfaces/console/hosts_test.go internal/interfaces/console/server.go internal/interfaces/console/tasks_test.go internal/interfaces/console/wizard_test.go)"
go test ./internal/application/hosts -count=1
```


### 根执行者只读核实的服务事件

服务事件记录显示：2026-10-23T06:22:56Z数据库服务进入Stopping；06:23:06Z Stopped/Starting；06:23:08Z Started。数据库实际仍为lober_test，服务器本次启动时间06:23:06.383579Z，当前不在恢复。首个57P03时间06:22:59Z正处于停止窗口。该窗口无内核OOM记录；源码和Makefile无数据库服务重启命令。没有修改服务、凭据或数据库配置。

本次实际诊断内存总量为2111MiB，不使用旧环境容量说明。安全诊断与原服务日志由根保留；公共记录不包含连接信息。服务事件支持数据库停机造成这批连接失败；谁触发服务重启尚未据此宣称已查明。

按根裁定，保持生产和测试源码不变：prepare核对恢复后，仅同race20m复跑首个失败用例；成功后重新执行原make和后续检查。可复用相同源码、依赖和环境的成功缓存；不清缓存、不改限制、不跳过失败包。


### 恢复单例与第二轮完整检查

t4-recovery-1使用与首轮完全相同的树00ad8ee29058bc2bf143edef4e837be2f5f18409。命令go test -race -timeout=20m ./cmd/lober -run '^Test组件和模板应用同时组装$' -count=1，随后git diff --exit-code。UTC06:48:15Z至06:48:36Z，退出0，实际用例16.501秒，VM修改及未跟踪文件为空。

再次prepare确认无竞争Go任务、实际测试库正常和可用磁盘37G后，t4-full-2开始运行。仍使用同树与原完整命令，源码、测试、生成文件和配置没有改变；本地收尾仅3份Markdown文档不同。首轮成功结果可复用有效缓存，但首轮失败包必须实际重跑；架构强制发现保持-count=1。


## 最终审查与分支交付

[整分支最终审查](2026-10-23-ddd-phase3-final-review.md)覆盖 `a9d5434..b50c267` 的5个提交、32个文件。严重、重要和轻微问题均无发现。六入口、CSV两种结果、五项重点、真实数据库事务和全部原有边界保护均已闭环。

| 任务 | 实施记录 | 独立审查 |
| --- | --- | --- |
| 1 | [原行为基线](2026-10-23-ddd-phase3-task1-execution.md) | [审查通过](2026-10-23-ddd-phase3-task1-review.md) |
| 2 | [查询与单写迁移](2026-10-23-ddd-phase3-task2-execution.md) | [审查通过](2026-10-23-ddd-phase3-task2-review.md) |
| 3 | [CSV与整批导入](2026-10-23-ddd-phase3-task3-execution.md) | [审查通过](2026-10-23-ddd-phase3-task3-review.md) |
| 4 | [边界与完整验收](2026-10-23-ddd-phase3-task4-execution.md) | [审查通过](2026-10-23-ddd-phase3-task4-review.md) |

两项执行裁定及判断错误的代价见[裁定记录](2026-10-23-ddd-phase3-decisions.md)。以下是最终审查未作完成判定的5项，逐项确认均属于原计划边界，并非被遗漏的主机验收条件：

1. 一次性发布记录、真实ISO及整机安装未执行；本次自动检查不能替代实机验收。
2. 没有网页业务或结构变更，已按计划通过类型生成兼容检查；不宣称完成完整前端构建或浏览器验收。
3. 本轮交付模块分支。develop集成及test、preview、main需按仓库流程另行推进，本轮未执行。
4. 数据库停止窗口、恢复和同源码重跑已核实；重启发起者未知，未宣称修复运维问题。
5. 其他模块仍按路线迁移，剩余261项临时许可属于其他阶段；不能把本次主机验收称为整个后端迁移完成。

用户最后要求保持代码和测试设置不变。本次恢复单例、完整重跑和两道审查均遵守。最终收尾仅修改和保存 `docs/` 下的Markdown；用Git精确差异核对，生产、测试、守卫、生成文件、依赖、网页及Makefile仍等于被测树 `00ad8ee29058bc2bf143edef4e837be2f5f18409`。文档补记不重复相同套件。

原始运行与服务诊断日志保留在 lober VM 的 `/home/dev/lober-ddd-phase3-hosts-20261023/logs/`。连接文件及原始敏感日志不进入Git。删除本计划临时目录的命令被自动审批审查拦截，提示“被策略阻止”，未提供更具体原因；没有重试删除，目录保留在Git忽略范围中。模块分支、工作区、其他阶段记录和VM原始日志均保留，代码及验收结果不受影响。
