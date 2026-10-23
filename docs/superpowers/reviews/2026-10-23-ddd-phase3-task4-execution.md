本记录保留执行或审查当时的状态。最终交付及验证见[第三阶段验收记录](2026-10-23-ddd-phase3-evidence.md)。本机临时目录清理被自动审批策略阻止，现保留在Git忽略范围中；原始运行日志也保留在 lober VM 的第三阶段测试目录。

# Task 4 执行报告

状态：实现和第二轮完整检查已通过，本地候选已提交b50c2674e37cb88dacd22203ea950d6a654ccf29。Task4独立审查、整分支审查和模块交付待根执行者处理。Task4独立审查、整分支审查和模块交付待根执行者处理。

## 范围与实现

仅修改Task4允许的7个路径：

- internal/archtest/hosts_rules_test.go
- internal/archtest/wizard_helpers_test.go
- docs/contracts/hosts.md
- docs/contracts/backend-modules.md
- docs/superpowers/plans/2026-10-22-backend-ddd-roadmap.md
- docs/superpowers/plans/2026-10-23-backend-ddd-phase3-hosts.md
- docs/superpowers/reviews/2026-10-23-ddd-phase3-evidence.md

迁移根只增加hosts.go/csv.go；应用方法表只增加Registry.List/Get/Create/Update/Delete/Import。不新增Error或通知/诊断能力。两个文件各15个拒绝例、3个允许例，所有源码先通过Go类型检查。旧业务放在非根文件，允许例无常驻违规污染；原向导12反例与编写六文件套件未改。

新增Test主机许可已清零直接检查真实数组88/125/47/1、阶段1/2/3与向导为0。固定起点a9d5434fc507f8857c5fb36b252a41de1cce0ab3的逐项六字段内容摘要；补回显式10+6删除项后必须匹配起点，拒绝其他阶段任意增删改名/改阶段。合成格式和匹配示例未改。两张许可源码只读。

额外本地只读Git差异审计：起点至HEAD，migration_test.go新增条目0/删除10，transition_manifest_test.go新增0/删除6，所有删除阶段均3。正式编号六项与唯一通知转发原检查保持。

计划状态改为用户已批准实施，前3任务按已审查报告打勾。Task4的独立审查、整分支审查和模块交付仍保持未完成，不把候选提交当作模块交付。

## 失败先行与格式

- t4-red-1：树4f38fd4e9425717817e27e54a5f31d91f8b307b7；命令go test ./internal/archtest -run '^Test主机入口不能绕回旧业务$' -count=1；2026-10-23T06:08:33Z至06:08:35Z，退出1。无类型错误；两文件各10个间接、小接口、未登记应用能力漏报，findings=[]。直接仓储和领域调用原检查已拒绝。
- t4-format-1：树491aaadaf06d71990d4ac247fc36909bf8e05ca3；仅gofmt -w internal/archtest/hosts_rules_test.go internal/archtest/wizard_helpers_test.go；06:10:04Z开始/结束，退出0。两文件取回、重新暂存后才运行绿色验证。
- t4-green-1：树797e1c4f040135e6f0b687e03494bf32ca215e79；go test ./internal/archtest -count=1、gofmt -l 两文件、git diff --exit-code；06:10:49Z至06:13:27Z，退出0，架构156.184s，修改及未跟踪文件均为空。

本地首次尝试安全读取RED日志时Python默认编码不匹配，UnicodeDecodeError；无源码修改发生。改用显式UTF-8读取后正常；这是本地日志读取错误，不作为测试红灯。测试、gofmt、构建、Wire和Node生成检查全部仅在VM执行。

## 完整检查

开跑前vm-check.py prepare实际检查：hostname=lober、实际lober_test、无其他Go测试/构建、可用磁盘38G；Go1.27.1/Node24.16.0/pnpm11.16.0。固定GOMAXPROCS=2/GOFLAGS=-p=2/GOPROXY=off和Bash/BusyBox。未手工读取或输出私有连接数据；连接由既有helper静默处理。

t4-full-1被测树00ad8ee29058bc2bf143edef4e837be2f5f18409，实际make退出2，详见失败节。命令：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
git diff --exit-code
test -z "$(gofmt -l internal/archtest/hosts_rules_test.go internal/archtest/wizard_helpers_test.go)"
```

原make check及go test -race -timeout=20m ./...保持。无前端业务修改，不跑全量前端构建。使用实际测试数据库，既有pgtest缺DSN为Fatal而非Skip；本阶段目标数据库契约无Skip。

日志均在本任务vm-logs/t4-*.{log,tree,exit,started,finished,changes,untracked}，快照t4-*.snapshot.json固定命令和Git树。公共证据只写无私密内容。

## 自审和待办

没有生产行为、公开类型、数据库结构、配置、凭据、密码强度、缓存或超时修改；无新依赖。旧套件断言未取消。无合并、推送或环境推进。

完整检查结束后补真实退出、时间、缓存、跳过、候选提交及精确文档差异证明。不能把前序聚焦通过或环境准备通过代替本次完整验收。

两项既有裁定已写公共证据：provider契约替身可能漏生产连线，Wire和独立审查补核；模型容量故障后的可用模型替换可能漏细小守卫路径，反例和独立审查要求不减。


## 完整检查第一轮真实失败

t4-full-1：被测树00ad8ee29058bc2bf143edef4e837be2f5f18409，2026-10-23T06:14:13Z至06:42:58Z，总耗时1725秒，make退出2。VM源码修改与未跟踪文件均为空。

格式、vet、强制全模块和普通全包通过；普通console234.409s、pgstore160.346s。race console988.370s、pgstore204.299s、arch243.734s通过；race cmd/lober66.283s失败。0个WARNING: DATA RACE，0个test timed out panic。本轮是一般断言/连接失败，不是数据竞争报告或20分钟超时。

首个失败：Test组件和模板应用同时组装，assembly_test.go:347，原用户改密请求返回500。其cleanup pgtest.go:194及后续测试建连共同出现数据库“the database system is shutting down”，SQLSTATE57P03共20次。首轮普通/并发输出中缓存包结果均为0。首个57P03在原日志第595行，前相邻VM时间戳为2026/10/23 06:22:59（UTC），可供服务事件关联。无panic堆栈。日志显示数据库停机状态；具体服务事件及归属待根核实，尚不预判为业务代码缺陷或确定的环境责任。

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


### 根执行者只读核实的服务事件

服务事件记录显示：2026-10-23T06:22:56Z数据库服务进入Stopping；06:23:06Z Stopped/Starting；06:23:08Z Started。数据库实际仍为lober_test，服务器本次启动时间06:23:06.383579Z，当前不在恢复。首个57P03时间06:22:59Z正处于停止窗口。该窗口无内核OOM记录；源码和Makefile无数据库服务重启命令。没有修改服务、凭据或数据库配置。

本次实际诊断内存总量为2111MiB，不使用旧环境容量说明。安全诊断与原服务日志由根保留；公共记录不包含连接信息。服务事件支持数据库停机造成这批连接失败；谁触发服务重启尚未据此宣称已查明。

按根裁定，保持生产和测试源码不变：prepare核对恢复后，仅同race20m复跑首个失败用例；成功后重新执行原make和后续检查。可复用相同源码、依赖和环境的成功缓存；不清缓存、不改限制、不跳过失败包。


### 恢复单例与第二轮完整检查

t4-recovery-1使用与首轮完全相同的树00ad8ee29058bc2bf143edef4e837be2f5f18409。命令go test -race -timeout=20m ./cmd/lober -run '^Test组件和模板应用同时组装$' -count=1，随后git diff --exit-code。UTC06:48:15Z至06:48:36Z，退出0，实际用例16.501秒，VM修改及未跟踪文件为空。

再次prepare确认无竞争Go任务、实际测试库正常和可用磁盘37G后，t4-full-2开始运行。仍使用同树与原完整命令，源码、测试、生成文件和配置没有改变；本地收尾仅3份Markdown文档不同。首轮成功结果可复用有效缓存，但首轮失败包必须实际重跑；架构强制发现保持-count=1。


## 最终结果

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

收尾提交相对被测树仅以下3份Markdown不同：docs/contracts/hosts.md、docs/superpowers/plans/2026-10-23-backend-ddd-phase3-hosts.md、本文。生产源码、测试、守卫、Makefile、依赖、前端及生成文件都与被测树相同；暂存与提交后各用Git精确差异复核。独立Task4审查和整分支审查仍待完成。



## 固定候选与交接

- 本地提交：b50c2674e37cb88dacd22203ea950d6a654ccf29，refactor(hosts): enforce phase-three boundaries。
- 提交树：733bb16eccac1b3223d50e1d7874853c6c38d928。
- 本任务起点：1eecac5ef8800435a259fe6cd0f6c1b61578af9b。
- 最终完整被测树：00ad8ee29058bc2bf143edef4e837be2f5f18409。
- 暂存前后与提交后都核实：相对被测树仅docs/contracts/hosts.md、docs/superpowers/plans/2026-10-23-backend-ddd-phase3-hosts.md、docs/superpowers/reviews/2026-10-23-ddd-phase3-evidence.md三份Markdown不同。其余源码、测试、Makefile、依赖、生成文件和前端逐字相同。
- verify-proof.py t4-full-2 --commit HEAD --allow-docs-after实际通过；核对exit0、snapshot/tree相同、VM changes/untracked空、提交差异只Markdown。
- git diff --cached --check通过；提交后git status --short无输出。无本地Go/Node生成/gofmt/构建，没有merge/push或环境推进。
- public证据：docs/superpowers/reviews/2026-10-23-ddd-phase3-evidence.md。full1失败、服务重启、恢复单例、full2成功、47条缓存包结果、3个跳过、未执行项目、provider/model两项裁定与代价均已记录。
- 已完整完成Task4实施与候选交付准备；独立Task4审查和整分支审查未开始，计划第6/7项仍待完成，不提前宣称模块交付。根执行者接手两道审查与最终状态文档。

已知顾虑：没有未修的代码失败；数据库重启发起者未查明，服务事件与恢复结果完整保留。真实ISO、一次性发布记录、整机安装、全量前端和后续环境均未据此宣称验收通过。
