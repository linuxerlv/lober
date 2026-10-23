# 第四阶段边界与完整验收证据

Task6 实现与验收记录。Task6 独立审查和整分支审查仍待根执行者完成；本文不宣称模块分支可交付或已合入 dev。

## 范围与结果

- 起点：dev `48f6c2c1f1ee4aebdd0349868a097891e5c9ad13`。本项基线：`4010bb2b8a2b03f8ae7079cbead7a1a3a7f4b2b2`，分支 `codex/ddd-media`。
- 本项只收紧实际架构检查、补管理员写入口测试和说明文档。没有改产品业务、角色、数据库格式、设置、依赖或前端源码。
- 网页镜像入口沿实际类型追踪同包方法、函数值、方法值与泛型辅助函数；只准公开 Registry 的十一方法。新网页文件也不能调用内部镜像能力。
- 文件实现不能持有 Store、自己的仓储/控制能力或匿名仓储接口，也不能用别名、点导入、包级函数值或同包辅助绕过。方法集合识别仓储能力，不能靠改接口名字获得许可。
- 文件实现可以消费文件会话与领域事实，但不能改 ISO/Source 状态；组装入口只保留镜像构造、依赖接线和 Start/Close，不承担镜像仓储调用、业务方法或状态判断。状态字段按真实类型识别，指针别名、括号和复制结构不能绕过。现有 logging.go 的主机/任务转发没有迁入本阶段。
- 领域只复用精确的 CheckID、CheckSource、ErrInvalid。原精确包导入许可保留，相邻包和子包仍拒绝。应用消费 fs.ErrNotExist、fs.SkipAll 两个纯错误值，拒绝文件接口与 ReadFile/WalkDir 等操作；根已逐行确认 SkipAll 只终止自身递归，读取仍走 MediaView。
- `catalog.Arch.Valid` 是单个架构值检查正例，不把整个领域包或业务判定函数放行。新包发现、类型装载失败、旧阶段清零与已有方向守卫继续生效。
- 管理员八个写入口分别使用有效输入，断言成功、能力实际收到的编号/版本/来源/正文及审计；创建还检查 Created 与返回镜像。原四角色拒绝写入、无效身份零访问测试保留。没有修改权限实现。

## 先失败再修正

1. `t6-red-01`：类型样例先通过 Go 类型检查，随后实际守卫漏报自己的仓储接口、别名、点导入、方法值、同包转发、状态写入与cmd流程。应用外部操作首轮只检查解析，其补证见后文。类型样例失败是 `allowed=false findings=[]`，不是语法或未定义符号；纯架构值正例也暴露原“全部领域函数拒绝”的过宽规则。
2. `t6-green-01`：新守卫反例与管理员测试已通过，但全包检查把应用只用 io/fs 错误值误判为直接文件操作。`TestIncrementalGateRejectsPackageInExistingRoot` 与 `Test全模块依赖守卫` 因 identify.go、repository_media.go、suse.go 的 io/fs 导入失败。保留失败，改为检查精确符号。
3. `t6-red-02`：有效类型样例进一步暴露匿名仓储字段、新网页文件和 fs.ReadFile 仍漏报。泛型同包转发已能被拦截。补入同一生产遍历，未用无效样例冒充通过。
4. `t6-green-02`：新样例通过；原全包检查发现 repository_media.go:73、93 的 fs.SkipAll 被误拒。根检查实际递归后裁定允许这个精确纯值。没有改业务代码或替换文件能力。原失败日志保留。
5. `t6-green-03`：重新执行原失败的完整架构/应用包命令，并核对 Wire，退出0。
6. `t6-final-01`：原完整命令及补充测试退出0，19:53:51Z–20:20:34Z；console竞态853.074秒，pgstore竞态169.566秒。它是首轮树的完整结果，不代表后续补丁的验收。
7. `t6-red-03`：自查补入指针别名、命名指针、括号左侧、括号取地址、复制结构及其字面量，9个状态反例均通过类型检查后得到 `allowed=false findings=[]`。同时暂时恢复旧 applicationRule，对最终合法空白导入先做类型检查，再真实复现 os、net/http、os/exec 漏报。其他原有跨层拒绝、领域实际 ReadFile 拒绝及后续 TaskState 正例仍通过。
8. 最小修正：恢复应用规则，状态判断改为检查字段真实的 ISOState/SourceState 类型，并去掉表达式外括号；复制类型的字面量逐字段判断。没有按路径扩大豁免，也没有触碰镜像产品行为或第五、六阶段状态。
9. `t6-green-04` 重新执行新增完整样例、原完整架构/应用包命令和 Wire；完成格式取回后再次冻结，再以 `t6-final-02` 从头执行所有原完整命令。结果和命令见下表。

首轮应用导入方向样例沿用旧解析帮助函数，未先检查类型，不作为类型有效的RED证据。最终样例改成合法空白导入并先做类型检查。根裁定等待首轮完整作业结束后，用最终有效样例对旧 applicationRule 补RED；同时核查状态守卫的指针别名和带括号左侧，再修正并重新完整验收。该补证据过程与原完整作业结果均保留；首轮未出现console失败，不存在用守卫修正解释console失败的问题。

管理员测试是已批准行为的补充覆盖，首跑通过；没有编造该既有行为曾失败。审计脚本首次解析同一行空清单时自身断言失败，修正解析后才输出下面的精确比对；它不算 Go 红灯证据。

## 环境与执行方式

- 所有 Go、gofmt、Wire、Node 类型检查、测试和构建只在 lober VM。隔离目录 `/home/dev/lober-ddd-phase4-media-20261023/source`，归属标记 `ddd-phase4-media-20261023`。使用本阶段 vm.py，一次仅一个作业，传输和格式取回均等待完成。
- 本轮准备核实：hostname=lober，Go1.27.1 linux/amd64，Node24.16.0，pnpm11.16.0，内存2111MiB，首次完整验收前可用盘32GiB，最终t6-final-02前再次核实31GiB；各次均无竞争Go作业。数据库确为 lober_test，未恢复，数据库启动时间 `2026-10-23 06:23:06.383579+00`。旧服务此前为何重启仍未知；本项没有重启服务或数据库。
- 固定 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`，`LOBER_TEST_BASH=/bin/bash`、`LOBER_TEST_BUSYBOX=/usr/bin/busybox`。使用已有私有DSN；文件忽略且600，值不进入证据。没有升级依赖、清缓存、缩短原20分钟竞态超时或降低测试准备强度。
- 每轮候选树由完整已跟踪与非忽略新增文件建立独立索引；远端重建后比较 Git 树。源码修改和取回记录在 changes；最终同树验收前冻结非Markdown内容。

## 每轮树、时间和退出码

所有时间为 UTC。每轮完整命令在其后列出；日志只引用脱敏副本，原始日志留在本阶段私有目录。

| 作业 | 候选树 | 开始 | 结束 | 退出码 | 远端修改 |
| --- | --- | --- | --- | --- | --- |
| t6-red-01 | `b455ea5c0bc30f367935304d95b9d957f76d8956` | 2026-10-23T19:39:46Z | 2026-10-23T19:39:48Z | 1 |  |
| t6-green-01 | `f31e82b99b8113997981ca9599db9ec41b9676e6` | 2026-10-23T19:42:32Z | 2026-10-23T19:43:20Z | 1 | internal/application/media/registry_test.go、internal/archtest/application_rules_test.go、internal/archtest/hosts_rules_test.go、internal/archtest/media_rules_test.go、internal/archtest/transition_uses_test.go |
| t6-red-02 | `77df4a3486f45e13934f1fadc26518c7107c5a2f` | 2026-10-23T19:44:54Z | 2026-10-23T19:44:56Z | 1 |  |
| t6-green-02 | `0554845bd78257f92237922db273dcacdffcf9ac` | 2026-10-23T19:46:34Z | 2026-10-23T19:47:20Z | 1 | internal/archtest/media_rules_test.go、internal/archtest/transition_uses_test.go |
| t6-green-03 | `051598f158e00d63290c87149423b8468616e591` | 2026-10-23T19:49:16Z | 2026-10-23T19:51:53Z | 0 | internal/archtest/media_rules_test.go |
| t6-final-01 | `f98d54142b4bb3b69d9199bc9fc282806285a0a0` | 2026-10-23T19:53:51Z | 2026-10-23T20:20:34Z | 0 |  |
| t6-red-03 | `8dbdb4541ddcd7ec61548bd5d40b1a609dacb04c` | 2026-10-23T20:21:39Z | 2026-10-23T20:21:41Z | 1 |  |
| t6-green-04 | `fd3199a99ce0bfcbf4dd7b35c936ef38d2fd9cca` | 2026-10-23T20:22:20Z | 2026-10-23T20:25:03Z | 0 | internal/archtest/media_rules_test.go、internal/archtest/transition_uses_test.go |
| t6-final-02 | `6ac66191d7a9ea3bdf16694d2a1e95cb2251e7fe` | 2026-10-23T20:26:05Z | 2026-10-23T20:32:48Z | 0 |  |

### t6-red-01

```sh
go test ./internal/archtest -run 'Test镜像(文件实现拒绝借能力接口查库|网页入口追踪同包旧业务|组装只构造启停不处理业务|应用不能直接读文件或跨层)' -count=1 -v
```

### t6-green-01

```sh
gofmt -w internal/archtest/media_rules_test.go internal/archtest/transition_uses_test.go internal/archtest/wizard_helpers_test.go internal/archtest/application_rules_test.go internal/archtest/iso_test.go internal/archtest/hosts_rules_test.go internal/application/media/registry_test.go
go test ./internal/archtest ./internal/application/media -count=1
```

### t6-red-02

```sh
go test ./internal/archtest -run 'Test镜像(文件实现拒绝借能力接口查库|网页入口追踪同包旧业务|应用只接受文件缺失错误事实|新网页文件不能调用内部能力)' -count=1 -v
```

### t6-green-02

```sh
gofmt -w internal/archtest/media_rules_test.go internal/archtest/transition_uses_test.go internal/archtest/application_rules_test.go
go test ./internal/archtest -run 'Test镜像' -count=1 -v
go test ./internal/archtest ./internal/application/media -count=1
```

### t6-green-03

```sh
gofmt -w internal/archtest/media_rules_test.go internal/archtest/transition_uses_test.go
go test ./internal/archtest ./internal/application/media -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

### t6-final-01

```sh
echo 'TASK6 make check START'
date -u +%FT%TZ
make check
echo 'TASK6 make check EXIT=0'
date -u +%FT%TZ
echo 'TASK6 console types START'
pnpm --dir web check:console-types
echo 'TASK6 console types EXIT=0'
date -u +%FT%TZ
echo 'TASK6 original focused START'
go test ./internal/application/media ./internal/infrastructure/media ./internal/interfaces/console ./cmd/lober ./internal/infrastructure/persistence/pgstore -run 'Test(镜像|ISO|Source|ISOSources|契约ISO|契约审计|Import|Upload|Remote|Recover|Worker|删除快照|补来源|重新导入|后台)' -count=1
echo 'TASK6 original focused EXIT=0'
date -u +%FT%TZ
echo 'TASK6 original optional START'
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
echo 'TASK6 original optional EXIT=0'
date -u +%FT%TZ
git diff --exit-code
echo 'TASK6 original diff EXIT=0'
echo 'TASK6 moved real ISO and admin START'
date -u +%FT%TZ
go test ./internal/application/media -run '^(TestIdentifyRealISO|Test镜像管理员八个写入口传递有效输入)$' -v -count=1
echo 'TASK6 moved real ISO and admin EXIT=0'
date -u +%FT%TZ
echo 'TASK6 actual assembly START'
go test ./cmd/lober -run '^Test装配后台获取导入及启动恢复$' -v -count=1
echo 'TASK6 actual assembly EXIT=0'
date -u +%FT%TZ
echo 'TASK6 optional shell START'
go test ./internal/domain/render -run '^TestPreseedCleanupRedactsInstallerAuthenticationURLs$' -v -count=1
echo 'TASK6 optional shell EXIT=0'
date -u +%FT%TZ
git diff --exit-code
echo 'TASK6 final diff EXIT=0'
```

### t6-red-03

```sh
go test ./internal/archtest -run '^Test镜像(文件实现拒绝借能力接口查库|组装只构造启停不处理业务|应用不能直接读文件或跨层|领域不能读取文件)$' -v -count=1
```

### t6-green-04

```sh
gofmt -w internal/archtest/media_rules_test.go internal/archtest/transition_uses_test.go
go test ./internal/archtest -run '^Test镜像' -v -count=1
go test ./internal/archtest ./internal/application/media -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

### t6-final-02

```sh
echo 'TASK6 make check START'
date -u +%FT%TZ
make check
echo 'TASK6 make check EXIT=0'
date -u +%FT%TZ
echo 'TASK6 console types START'
pnpm --dir web check:console-types
echo 'TASK6 console types EXIT=0'
date -u +%FT%TZ
echo 'TASK6 original focused START'
go test ./internal/application/media ./internal/infrastructure/media ./internal/interfaces/console ./cmd/lober ./internal/infrastructure/persistence/pgstore -run 'Test(镜像|ISO|Source|ISOSources|契约ISO|契约审计|Import|Upload|Remote|Recover|Worker|删除快照|补来源|重新导入|后台)' -count=1
echo 'TASK6 original focused EXIT=0'
date -u +%FT%TZ
echo 'TASK6 original optional START'
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
echo 'TASK6 original optional EXIT=0'
date -u +%FT%TZ
git diff --exit-code
echo 'TASK6 original diff EXIT=0'
echo 'TASK6 moved real ISO and admin START'
date -u +%FT%TZ
go test ./internal/application/media -run '^(TestIdentifyRealISO|Test镜像管理员八个写入口传递有效输入)$' -v -count=1
echo 'TASK6 moved real ISO and admin EXIT=0'
date -u +%FT%TZ
echo 'TASK6 actual assembly START'
go test ./cmd/lober -run '^Test装配后台获取导入及启动恢复$' -v -count=1
echo 'TASK6 actual assembly EXIT=0'
date -u +%FT%TZ
echo 'TASK6 optional shell START'
go test ./internal/domain/render -run '^TestPreseedCleanupRedactsInstallerAuthenticationURLs$' -v -count=1
echo 'TASK6 optional shell EXIT=0'
date -u +%FT%TZ
git diff --exit-code
echo 'TASK6 final diff EXIT=0'
```

## 红灯实际输出摘录

`t6-red-01`：

```text
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
    media_rules_test.go:58: allowed=false findings=[]
--- FAIL: Test镜像文件实现拒绝借能力接口查库 (0.01s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/自己的仓储接口 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/类型别名 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/点导入 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/来源控制 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/删除流程 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/方法值 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/包级方法值 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/同包转发 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/本地小接口 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/结构体状态赋值 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/复合字面量状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/纯架构值检查 (0.00s)
    media_rules_test.go:77: allowed=false findings=[]
    media_rules_test.go:77: allowed=false findings=[]
    media_rules_test.go:77: allowed=false findings=[]
    media_rules_test.go:77: allowed=false findings=[]
    media_rules_test.go:77: allowed=false findings=[]
--- FAIL: Test镜像网页入口追踪同包旧业务 (0.01s)
    --- FAIL: Test镜像网页入口追踪同包旧业务/相同接口新增方法 (0.00s)
    --- FAIL: Test镜像网页入口追踪同包旧业务/同包两层方法值 (0.00s)
    --- FAIL: Test镜像网页入口追踪同包旧业务/包级函数值 (0.00s)
    --- FAIL: Test镜像网页入口追踪同包旧业务/局部小接口 (0.00s)
    --- FAIL: Test镜像网页入口追踪同包旧业务/旧Sources (0.00s)
    media_rules_test.go:91: allowed=false findings=[]
    media_rules_test.go:91: allowed=false findings=[]
    media_rules_test.go:91: allowed=false findings=[]
    media_rules_test.go:91: allowed=false findings=[]
    media_rules_test.go:91: allowed=false findings=[]
    media_rules_test.go:91: allowed=false findings=[]
--- FAIL: Test镜像组装只构造启停不处理业务 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/查镜像方法值 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/本地接口查镜像 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/来源持久化 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/恢复流程 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/状态更新 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/状态判断 (0.00s)
    media_rules_test.go:97: 镜像应用放过 os
    media_rules_test.go:97: 镜像应用放过 io/fs
    media_rules_test.go:97: 镜像应用放过 net/http
    media_rules_test.go:97: 镜像应用放过 os/exec
--- FAIL: Test镜像应用不能直接读文件或跨层 (0.00s)
```

`t6-red-02`：

```text
    media_rules_test.go:73: allowed=false findings=[]
--- FAIL: Test镜像文件实现拒绝借能力接口查库 (0.01s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/仅持有匿名仓储 (0.00s)
    media_rules_test.go:144: var read=fs.ReadFile allowed=false findings=[]
--- FAIL: Test镜像应用只接受文件缺失错误事实 (0.00s)
--- FAIL: Test镜像新网页文件不能调用内部能力 (0.00s)
```

`t6-red-03`：

```text
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
    media_rules_test.go:80: allowed=false findings=[]
--- FAIL: Test镜像文件实现拒绝借能力接口查库 (0.01s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/指针类型别名写状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/命名指针写来源状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/括号左侧写状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/括号取地址写状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/复制结构类型写状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/复制结构字面量写状态 (0.00s)
    --- FAIL: Test镜像文件实现拒绝借能力接口查库/复制结构位置字面量写状态 (0.00s)
    media_rules_test.go:134: allowed=false findings=[]
    media_rules_test.go:134: allowed=false findings=[]
--- FAIL: Test镜像组装只构造启停不处理业务 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/指针别名判断状态 (0.00s)
    --- FAIL: Test镜像组装只构造启停不处理业务/复制结构判断状态 (0.00s)
    media_rules_test.go:154: 镜像应用放过 os
    media_rules_test.go:154: 镜像应用放过 net/http
    media_rules_test.go:154: 镜像应用放过 os/exec
--- FAIL: Test镜像应用不能直接读文件或跨层 (0.00s)
```

## 最终验收、缓存与跳过

原 Task6 全部最终命令在同一候选树 `6ac66191d7a9ea3bdf16694d2a1e95cb2251e7fe` 完成，整轮退出0；changes、untracked 均为空。make check 保持格式、vet、强制全模块发现（make共享前置目标，本次调用只执行一次）、普通测试、原竞态20m、Linux amd64构建和Wire差异；另外完成契约类型、两条原点名命令，以及补充的实际ISO位置、真实组装和可选外部工具用例。

本次强制全模块发现实际运行7.249秒；普通与竞态各26个包使用缓存、5个包实际运行、6个包无测试文件。没有把缓存包写成本轮重新执行。实际运行的包与耗时如下：

`ordinary`：

```text
ok  	github.com/lober/lober/internal/archtest	136.068s
ok  	github.com/lober/lober/internal/domain/model	0.035s
ok  	github.com/lober/lober/internal/domain/render	8.722s
ok  	github.com/lober/lober/internal/domain/scan	0.011s
ok  	github.com/lober/lober/internal/infrastructure/builtin	2.663s
```

`race`：

```text
ok  	github.com/lober/lober/internal/archtest	185.436s
ok  	github.com/lober/lober/internal/domain/model	1.205s
ok  	github.com/lober/lober/internal/domain/render	10.642s
ok  	github.com/lober/lober/internal/domain/scan	1.035s
ok  	github.com/lober/lober/internal/infrastructure/builtin	6.169s
```

原点名测试实际执行：应用34.489秒、文件实现13.074秒、网页14.496秒、真实数据库13.206秒；该命令cmd/lober无匹配。额外真实组装1.08秒，管理员八个写入口全部成功，Bash清理0.08秒。内置Preseed镜像认证两个分项均成功。

实际输出中的缓存行：

```text
ok  	github.com/lober/lober/cmd/lober	(cached)
ok  	github.com/lober/lober/internal/application/authoring	(cached)
ok  	github.com/lober/lober/internal/application/execution	(cached)
ok  	github.com/lober/lober/internal/application/hosts	(cached)
ok  	github.com/lober/lober/internal/application/media	(cached)
ok  	github.com/lober/lober/internal/application/tasks	(cached)
ok  	github.com/lober/lober/internal/domain/access	(cached)
ok  	github.com/lober/lober/internal/domain/activation	(cached)
ok  	github.com/lober/lober/internal/domain/admission	(cached)
ok  	github.com/lober/lober/internal/domain/catalog	(cached)
ok  	github.com/lober/lober/internal/domain/media	(cached)
ok  	github.com/lober/lober/internal/domain/params	(cached)
ok  	github.com/lober/lober/internal/domain/repository	(cached)
ok  	github.com/lober/lober/internal/domain/selection	(cached)
ok  	github.com/lober/lober/internal/domain/snapshot	(cached)
ok  	github.com/lober/lober/internal/infrastructure/events	(cached)
ok  	github.com/lober/lober/internal/infrastructure/identity	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media/iso9660	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media/isotest	(cached)
ok  	github.com/lober/lober/internal/infrastructure/notification	(cached)
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore	(cached)
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest	(cached)
ok  	github.com/lober/lober/internal/infrastructure/selection	(cached)
ok  	github.com/lober/lober/internal/interfaces/console	(cached)
ok  	github.com/lober/lober/internal/interfaces/machine	(cached)
ok  	github.com/lober/lober/cmd/lober	(cached)
ok  	github.com/lober/lober/internal/application/authoring	(cached)
ok  	github.com/lober/lober/internal/application/execution	(cached)
ok  	github.com/lober/lober/internal/application/hosts	(cached)
ok  	github.com/lober/lober/internal/application/media	(cached)
ok  	github.com/lober/lober/internal/application/tasks	(cached)
ok  	github.com/lober/lober/internal/domain/access	(cached)
ok  	github.com/lober/lober/internal/domain/activation	(cached)
ok  	github.com/lober/lober/internal/domain/admission	(cached)
ok  	github.com/lober/lober/internal/domain/catalog	(cached)
ok  	github.com/lober/lober/internal/domain/media	(cached)
ok  	github.com/lober/lober/internal/domain/params	(cached)
ok  	github.com/lober/lober/internal/domain/repository	(cached)
ok  	github.com/lober/lober/internal/domain/selection	(cached)
ok  	github.com/lober/lober/internal/domain/snapshot	(cached)
ok  	github.com/lober/lober/internal/infrastructure/events	(cached)
ok  	github.com/lober/lober/internal/infrastructure/identity	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media/iso9660	(cached)
ok  	github.com/lober/lober/internal/infrastructure/media/isotest	(cached)
ok  	github.com/lober/lober/internal/infrastructure/notification	(cached)
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore	(cached)
ok  	github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest	(cached)
ok  	github.com/lober/lober/internal/infrastructure/selection	(cached)
ok  	github.com/lober/lober/internal/interfaces/console	(cached)
ok  	github.com/lober/lober/internal/interfaces/machine	(cached)
```

显式可选用例的实际跳过与原因：

```text
    render_test.go:284: one-time immutable release recording
--- SKIP: TestRecordCurrentRelease (0.00s)
    realiso_test.go:15: LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in
--- SKIP: TestRealISO (0.00s)
    identify_real_test.go:17: LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in
--- SKIP: TestIdentifyRealISO (0.00s)
```

原点名命令的cmd/lober显示 no tests to run；原可选命令的infrastructure/media也无匹配。额外真实组装用例通过，管理员八个分项通过，Bash清理用例通过。RecordCurrentRelease因一次性发布记录选择退出；两个实际ISO用例因LOBER_TEST_REAL_ISO未设置跳过，不能把命令退出0写成这些实物覆盖已执行。

原普通/竞态命令不逐例列出跳过项，不能从简洁输出推断全链路零跳过。已搜索所有当前 Skip 入口并用额外 -v 展示相关可选测试；实物ISO、安装资格与未具备外部工具的覆盖不声称通过。没有安装Vue依赖，也没有声称全量Vue检查成功。既有GIN调试提示及预期TLS握手拒绝日志不通过设置隐藏；整轮可见噪声按实际脱敏输出记录，留给整分支审查判断。

最终安全输出中GIN调试提示76行、TLS handshake error 0行；这个计数只覆盖该非verbose与补充verbose组合的实际可见输出，不代表所有测试均无同类输出。

## 精确迁移审计

直接从起点 Git 对象提取四张实际清单，与本轮清单逐项比较。根执行者随后独立解析核对，证据为本计划目录 root-exact-audit.py/json，八个摘要与以下结果一致：未新增或改写任何保留项；只删除第四阶段条目。阶段1、2、3与向导继续0，阶段4为0；阶段5为65，阶段6为42。

| 清单 | 起点 | 现值 | 删除 | 起点逐项SHA256 | 现值逐项SHA256 |
| --- | ---: | ---: | ---: | --- | --- |
| migrationExceptions | 88 | 64 | 24 | `6196c008c4430e233c854b0a891446e2b6605f43f97b2cd0fe3555752324590f` | `b6db3a31128980a22f2fd4467d493f9fb8bc836e118b43cf9b286f8abdd68dab` |
| mediaMigrationExceptions | 125 | 0 | 125 | `4e93eaf8a62876bfa694042b348843f41709e729f45b4354708802cfe5df19db` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| consoleRuleMigrationExceptions | 47 | 42 | 5 | `258e968b25d41520233c8f43af192c2fe31d41fc125d7f56fd78a13999252cff` | `1eafc4f0f58a25d813550e3fc2a47860dc7d21f34c1b6eb1535cb8ff3a89befc` |
| notificationMigrationExceptions | 1 | 1 | 0 | `ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6` | `ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6` |

总计261→107，删除154=24网页仓储+5网页规则+125旧镜像许可。摘要算法：六个字段以制表符连接，每行LF，排序后计算SHA256。实际测试补回精确删除集，检查重建起点摘要；同时拒绝重复、错阶段和旧许可复活。原 hosts_rules 的阶段三快照改为本轮真实起点与删除集；阶段三清零断言保留。

### 精确删除集（154项）

字段依次是调用包、文件、函数、目标包、符号、清除阶段。

`migrationExceptions`：24项。

```text
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*exactChunkReader).Read	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).createISO	github.com/lober/lober/internal/domain/repository	CheckNewISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).createISO	github.com/lober/lober/internal/domain/repository	ISOs.CreateISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).deprecateISO	github.com/lober/lober/internal/domain/repository	ISOs.DeprecateISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).getISO	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).isoProgress	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).isoProgress	github.com/lober/lober/internal/interfaces/console	ISOSourceState.SourcesOf	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/repository	ISOQuery	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/repository	ISOs.ListISOs	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/repository	Page	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).registerISOSource	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).sourceOperation	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).sourceProgress	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).sourceProgress	github.com/lober/lober/internal/domain/repository	ErrNotFound	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).sourceProgress	github.com/lober/lober/internal/interfaces/console	ISOSourceState.GetSource	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).sourceProgress	github.com/lober/lober/internal/interfaces/console	ISOSourceState.RangesOf	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).uploadChunk	github.com/lober/lober/internal/domain/repository	CheckSourceKey	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).uploadChunk	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).uploadChunk	github.com/lober/lober/internal/domain/repository	ErrNotFound	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).uploadChunk	github.com/lober/lober/internal/interfaces/console	ISOSourceState.GetSource	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	checkISOSource	github.com/lober/lober/internal/domain/repository	CheckSource	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	checkISOSource	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isosource.go	package-init	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	ISOs	4
```

`mediaMigrationExceptions`：125项。

```text
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).CleanupDeleted	github.com/lober/lober/internal/domain/model	ISO.Validate	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).CleanupDeleted	github.com/lober/lober/internal/domain/repository	CheckID	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).CleanupDeleted	github.com/lober/lober/internal/domain/repository	CheckSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).CleanupDeleted	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).CleanupDeleted	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).confirmDeleted	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).confirmDeleted	github.com/lober/lober/internal/domain/repository	ErrNotFound	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	(*Sources).confirmDeleted	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/delete.go	cleanupOwnedTree	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/extract.go	(*Sources).extract	github.com/lober/lober/internal/domain/catalog	RepoPaths	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	Arch.Valid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	FormatMedia	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	Formats	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	SeriesByEdition	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	SeriesMedia	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/catalog	SeriesOfFormat	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/model	IsSafeWord	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	Identify	github.com/lober/lober/internal/domain/model	ValidLabel	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/identify.go	identifyProblem	github.com/lober/lober/internal/domain/model	NewProblem	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/catalog	ExtraSourceOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/catalog	RequiredRoles	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/model	ISO.Validate	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	CheckID	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ErrImmutable	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).Import	github.com/lober/lober/internal/domain/repository	ISOs.UpdateISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/model	StorableText	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/repository	ISOSources.GetSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).failImport	github.com/lober/lober/internal/domain/repository	ISOs.UpdateISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).openMedia	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	(*Sources).readImportOutcome	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	importProblem	github.com/lober/lober/internal/domain/model	NewProblem	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/import.go	publishImportTree	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	(*Sources).FetchOne	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	(*Sources).FetchOne	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	(*Sources).FetchOne	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	(*Sources).FetchOne	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	New	github.com/lober/lober/internal/domain/repository	Store	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/iso.go	package-init	github.com/lober/lober/internal/domain/repository	Store	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/local.go	(*Sources).fetchLocal	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/local.go	(*Sources).fetchLocal	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/netboot.go	identifyNetboot	github.com/lober/lober/internal/domain/catalog	Arch.Valid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	(*Sources).Recover	github.com/lober/lober/internal/domain/repository	ISOQuery	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	(*Sources).Recover	github.com/lober/lober/internal/domain/repository	ISOSources.ListUnfinishedSources	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	(*Sources).Recover	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	(*Sources).Recover	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	(*Sources).Recover	github.com/lober/lober/internal/domain/repository	ISOs.ListISOs	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/recover.go	allFetched	github.com/lober/lober/internal/domain/catalog	ExtraSourceOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/remote.go	(*Sources).fetchRemote	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/remote.go	(*Sources).fetchRemote	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/remote.go	(*Sources).fetchRemote	github.com/lober/lober/internal/domain/repository	ProblemsError	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/remote.go	parseContentRange	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/remote.go	rangeNumber	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).failFetch	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).failFetch	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/model	NewProblem	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/repository	ErrRejected	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).finishFetch	github.com/lober/lober/internal/domain/repository	ProblemsError	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).getSource	github.com/lober/lober/internal/domain/repository	ErrNotFound	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).getSource	github.com/lober/lober/internal/domain/repository	ISOSources.GetSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).restorePublication	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).resumeOffset	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).resumeOffset	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).resumeOffset	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).writeFetch	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	(*Sources).writeFetch	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	checkSourceKey	github.com/lober/lober/internal/domain/repository	CheckSourceKey	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	sourceRejected	github.com/lober/lober/internal/domain/model	NewProblem	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	sourceRejected	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/source.go	sourceRejected	github.com/lober/lober/internal/domain/repository	ProblemsError	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	CheckAudit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Cancel	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Progress	github.com/lober/lober/internal/domain/repository	ISOSources.RangesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Progress	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Progress	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Register	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RegisterQueued	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Reimport	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	CheckAudit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSourceAndAcquire	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).RequestReimport	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Retry	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Retry	github.com/lober/lober/internal/domain/repository	CheckAudit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Retry	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Retry	github.com/lober/lober/internal/domain/repository	ISOSources.SourcesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).Retry	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/catalog	ExtraSourceOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	CheckAudit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	CheckSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	ISOSources.GetSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSourceAndAcquire	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).register	github.com/lober/lober/internal/domain/repository	ISOs.GetISO	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).retrySource	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).retrySource	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/sources.go	(*Sources).retrySource	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/suse.go	identifySUSELayout	github.com/lober/lober/internal/domain/catalog	Arch.Valid	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/suse.go	identifySUSELayout	github.com/lober/lober/internal/domain/catalog	RepoPaths	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	Audit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	CheckAudit	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	ISOSources.RangesOf	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	ISOSources.SaveRange	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	(*Sources).Upload	github.com/lober/lober/internal/domain/repository	ISOSources.SaveSource	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/upload.go	compareRange	github.com/lober/lober/internal/domain/repository	ErrConflict	4
github.com/lober/lober/internal/infrastructure/media	internal/infrastructure/media/windows.go	(*Sources).extractWindows	github.com/lober/lober/internal/domain/repository	ErrInvalid	4
```

`consoleRuleMigrationExceptions`：5项。

```text
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).checkSourceSpace	github.com/lober/lober/internal/domain/model	NewProblem	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/catalog	Family.Valid	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/catalog	Series.Valid	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	(*server).listISOs	github.com/lober/lober/internal/domain/model	ISOState.Valid	4
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/isos.go	checkISOSource	github.com/lober/lober/internal/domain/catalog	ExtraSourceOf	4
```

`notificationMigrationExceptions`：0项。

```text

```

### 精确保留集（107项）

字段依次是调用包、文件、函数、目标包、符号、清除阶段。

`migrationExceptions`：64项。

```text
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).exportAudit	github.com/lober/lober/internal/domain/repository	AuditEntry	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).exportAudit	github.com/lober/lober/internal/domain/repository	AuditLog.ListAudit	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).exportAudit	github.com/lober/lober/internal/domain/repository	Page	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).listAudit	github.com/lober/lober/internal/domain/repository	AuditEntry	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).listAudit	github.com/lober/lober/internal/domain/repository	AuditLog.ListAudit	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	(*server).listAudit	github.com/lober/lober/internal/domain/repository	Page	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	auditFilters	github.com/lober/lober/internal/domain/repository	AuditQuery	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/audit.go	toAuditItem	github.com/lober/lober/internal/domain/repository	AuditEntry	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/auth.go	(*server).login	github.com/lober/lober/internal/domain/repository	ErrNotFound	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/auth.go	(*server).login	github.com/lober/lober/internal/domain/repository	Users.GetUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/auth.go	HashPassword	github.com/lober/lober/internal/domain/repository	ErrInvalid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	BatchError	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrConflict	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrDuplicate	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrImmutable	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrInvalid	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrLastAdmin	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrNotFound	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrReferenced	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ProblemsError	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ReferencedError	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).roleMenus	github.com/lober/lober/internal/domain/repository	RoleMenus.GetRoleMenus	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).saveRoleMenus	github.com/lober/lober/internal/domain/repository	CheckRoleMenus	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).saveRoleMenus	github.com/lober/lober/internal/domain/repository	RoleMenuSettings	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).saveRoleMenus	github.com/lober/lober/internal/domain/repository	RoleMenus.SaveRoleMenus	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/pagination.go	queryPagination	github.com/lober/lober/internal/domain/repository	CheckPageQuery	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/password.go	(*server).changePassword	github.com/lober/lober/internal/domain/repository	ErrConflict	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/password.go	(*server).changePassword	github.com/lober/lober/internal/domain/repository	Users.GetUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/password.go	(*server).changePassword	github.com/lober/lober/internal/domain/repository	Users.UpdateUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/profile.go	(*server).updateProfile	github.com/lober/lober/internal/domain/repository	CheckProfile	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/profile.go	(*server).updateProfile	github.com/lober/lober/internal/domain/repository	ErrConflict	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/profile.go	(*server).updateProfile	github.com/lober/lober/internal/domain/repository	Users.UpdateUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	(*server).auditFrom	github.com/lober/lober/internal/domain/repository	Audit	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	(*server).authenticate	github.com/lober/lober/internal/domain/repository	ErrNotFound	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	(*server).authenticate	github.com/lober/lober/internal/domain/repository	Users.GetUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	AuditLog	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	Events	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	RoleMenus	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	Tasks	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	package-init	github.com/lober/lober/internal/domain/repository	Users	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/taskcontrol.go	Problems	github.com/lober/lober/internal/domain/repository	ErrRejected	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/taskcontrol.go	Problems	github.com/lober/lober/internal/domain/repository	ProblemsError	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/taskcontrol.go	package-init	github.com/lober/lober/internal/domain/repository	Audit	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).controlTask	github.com/lober/lober/internal/domain/repository	Tasks.GetTask	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).getTask	github.com/lober/lober/internal/domain/repository	Tasks.GetTask	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listEvents	github.com/lober/lober/internal/domain/repository	Event	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listEvents	github.com/lober/lober/internal/domain/repository	Events.ListEvents	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listTasks	github.com/lober/lober/internal/domain/repository	Page	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listTasks	github.com/lober/lober/internal/domain/repository	TaskQuery	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listTasks	github.com/lober/lober/internal/domain/repository	Tasks.ListTasks	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).previewTask	github.com/lober/lober/internal/domain/repository	Tasks.GetTask	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).streamAuthorized	github.com/lober/lober/internal/domain/repository	Users.GetUser	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).taskStream	github.com/lober/lober/internal/domain/repository	Event	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).taskStream	github.com/lober/lober/internal/domain/repository	Events.ListEvents	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).taskStream	github.com/lober/lober/internal/domain/repository	Tasks.GetTask	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	toEventItem	github.com/lober/lober/internal/domain/repository	Event	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).createUser	github.com/lober/lober/internal/domain/repository	Users.CreateUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).disableUser	github.com/lober/lober/internal/domain/repository	Users.GetUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).disableUser	github.com/lober/lober/internal/domain/repository	Users.UpdateUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).listUsers	github.com/lober/lober/internal/domain/repository	Page	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).listUsers	github.com/lober/lober/internal/domain/repository	UserQuery	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).listUsers	github.com/lober/lober/internal/domain/repository	Users.ListUsers	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).updateUser	github.com/lober/lober/internal/domain/repository	Users.GetUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).updateUser	github.com/lober/lober/internal/domain/repository	Users.UpdateUser	5
```

`mediaMigrationExceptions`：0项。

```text

```

`consoleRuleMigrationExceptions`：42项。

```text
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/auth.go	(*server).login	github.com/lober/lober/internal/domain/model	Role.Valid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/auth.go	HashPassword	github.com/lober/lober/internal/domain/model	StorableText	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	RespondError	github.com/lober/lober/internal/domain/repository	ErrTemplateNotCurrent	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/errors.go	StatusOfProblems	github.com/lober/lober/internal/domain/model	Code.Category	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).listRoleMenus	github.com/lober/lober/internal/domain/model	Roles	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/menus.go	(*server).saveRoleMenus	github.com/lober/lober/internal/domain/model	Role.Valid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/catalog	AllArchs	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/catalog	AllFirmwares	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/catalog	AllFormats	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/catalog	AllSeries	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/catalog	Families	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/openapi.go	(*schemaBuilder).definition	github.com/lober/lober/internal/domain/model	Roles	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/password.go	(*server).changePassword	github.com/lober/lober/internal/domain/model	Role.Valid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/password.go	(*server).changePassword	github.com/lober/lober/internal/domain/model	StorableText	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	AllPerms	github.com/lober/lober/internal/domain/access	AllPerms	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	PermsOf	github.com/lober/lober/internal/domain/access	PermsOf	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermAnyUser	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermAuditRead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermHookApprove	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermHookRead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermHookWrite	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermHostRead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermHostWrite	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermISORead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermISOWrite	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTaskCancel	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTaskCreate	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTaskRead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTaskRetry	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTemplateApprove	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTemplateRead	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermTemplateWrite	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/perm.go	package-init	github.com/lober/lober/internal/domain/access	PermUserManage	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/server.go	(*server).authenticate	github.com/lober/lober/internal/domain/model	Role.Valid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listTasks	github.com/lober/lober/internal/domain/model	NormalizeSN	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).listTasks	github.com/lober/lober/internal/domain/model	TaskState.Valid	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).previewTask	github.com/lober/lober/internal/domain/catalog	Firmware.Valid	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/tasks.go	(*server).streamAuthorized	github.com/lober/lober/internal/domain/model	Role.Valid	6
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).createUser	github.com/lober/lober/internal/domain/model	Role.Valid	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).createUser	github.com/lober/lober/internal/domain/model	StorableText	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).createUser	github.com/lober/lober/internal/domain/model	ValidIdent	5
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/users.go	(*server).updateUser	github.com/lober/lober/internal/domain/model	Role.Valid	5
```

`notificationMigrationExceptions`：1项。

```text
github.com/lober/lober/internal/interfaces/console	internal/interfaces/console/notify.go	NewHub	github.com/lober/lober/internal/infrastructure/notification	NewHub	6
```

## 自查与边界

- 本项没有发现新的产品行为缺陷。前项既有修复仍单列：Task3 保留主错误和恢复错误；Task5 Close 先取消再等待锁，Start 恢复后复核取消。整分支审查应看到这些行为修复，不能称为纯搬迁。
- 只读核对实际组合：Registry→Control/Deletion；Control→Sources/Supervisor.Wake；Sources、Deletion共用锁与登记闸门；Supervisor只消费恢复/获取/导入、来源只读与清理重试能力。全仓与真实组装测试验证此组合，静态守卫不代替运行时业务断言。
- 本项没有合并、推送、发布或推进 test/preview/main；没有删除用户文件、其他工作区或前阶段日志。
- Task6独立审查与覆盖本阶段完整提交序列的整分支审查尚待根执行者；审查/交付勾选保留未完成。

## 改动文件与同树证明

- `docs/contracts/media.md`
- `docs/superpowers/plans/2026-10-22-backend-ddd-roadmap.md`
- `docs/superpowers/plans/2026-10-23-backend-ddd-phase4-media-jobs.md`
- `docs/superpowers/reviews/2026-10-23-ddd-phase4-evidence.md`
- `internal/application/media/registry_test.go`
- `internal/archtest/application_rules_test.go`
- `internal/archtest/hosts_rules_test.go`
- `internal/archtest/iso_test.go`
- `internal/archtest/media_rules_test.go`
- `internal/archtest/transition_uses_test.go`
- `internal/archtest/wizard_helpers_test.go`

```json
{
  "tested_tree": "6ac66191d7a9ea3bdf16694d2a1e95cb2251e7fe",
  "candidate_tree_before_report_write": "08ed3c73edf82fa55ab7ba9727efa4c0f77ded73",
  "complete_union_paths": 2036,
  "non_markdown_paths": 1870,
  "non_markdown_differences": [],
  "markdown_differences": [
    "docs/superpowers/plans/2026-10-22-backend-ddd-roadmap.md",
    "docs/superpowers/plans/2026-10-23-backend-ddd-phase4-media-jobs.md",
    "docs/superpowers/reviews/2026-10-23-ddd-phase4-evidence.md"
  ]
}
```

报告/计划等Markdown可在验收后补写；以上完整路径集合核对确认非Markdown内容仍等于被测树。

完整本地证明目录：`.superpowers/sdd/2026-10-23-backend-ddd-phase4-media-jobs`。所有轮次的 snapshot、tree、exit、started、finished、changes、untracked、safe.log均保留；不提交私有原日志。
