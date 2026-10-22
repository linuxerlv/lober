# 第二阶段编写与预览证据

本阶段在 `codex/ddd-authoring` 实施，集成起点为 `bdc6371749a9122680a18383561567e89a05e9c2`。Task1–5已实现且分别独立审查通过。Task6候选完整验证已通过；独立Task6审查和整分支审查尚未执行，不能据此宣布交付。

## 修改范围

32条路由保持原字段、权限、错误、分页和写入约定，详见[契约](../../contracts/authoring-preview.md)。组件、模板编写和编辑预览分别进入三个应用接口；选择锁由任务和模板应用共享，文件预览只消费冻结快照。网页旧仓储字段及业务桥删除。

没有数据库结构、公开类型、配置、发布清单、内置正文或前端业务变化；没有新增依赖、审计动作或通知。正式编号能力仍为六项，唯一通知转发仍只允许console.NewHub。

## 前五项证据

| 任务 | 提交 | 虚拟机验证与审查 |
| --- | --- | --- |
| 1，契约基线 | 75e3a50 | task-1-contract-02，树471dde0cea30faf7c7365b50deeebdf4eeccbafa；网页42.637秒、数据库47.316秒；退出0；独立审查0问题 |
| 2，组件应用 | 9103a10 | task-2-test-03，树f6d0acabb6d989af013f7fbf06243a60bb9d3cb8；聚焦、任务全包、候选和Wire通过；独立审查0问题 |
| 3，模板与共享锁 | 392c0a0、bbabdab | 初审发现数字溢出兼容和测试锁组装问题；分别以真实失败和同对象组装验证修复，复审关闭。最终树26bb67211cb03713ada81414045676540194ed15 |
| 4，编辑辅助 | 19db515 | task-4-green-05，树43e18517fb5cc20c17879f5db04b16f583b9d3f3；聚焦、应用/网页/组装全包、架构、格式和Wire通过；独立审查0问题 |
| 5，冻结预览 | c577c61 | task-5-final-01，树bd996cf0b9b2246942fe024d313fc4f17cccf2d8；预览、旧Editor、候选、机器接口、渲染、内置、完整架构和Wire通过；独立审查0问题 |

## 临时许可清零

| 时间点 | 仓储清单阶段2剩余 | 网页规则清单阶段2剩余 |
| --- | --- | --- |
| 集成起点及Task1 | 55 | 28 |
| Task2后 | 37 | 28 |
| Task3及其修复后 | 17 | 25 |
| Task4后 | 9 | 16 |
| Task5后 | 4 | 1 |
| Task6候选 | 0 | 0 |

累计删除55+28=83项，360降为277；没有删除其他阶段许可，也没有改阶段、改符号或扩容续用。最后5项为authoringQuery的HookQuery与VersionState.Valid、templateKeyOf的CheckVersionKey，以及Deps中的Hooks和Templates类型。前序提交已逐项删除78项，本提交只删除实际尚存5项。

剩余清单分别为98、125、53、1项，按顺序是migrationExceptions、mediaMigrationExceptions、consoleRuleMigrationExceptions、notificationMigrationExceptions。向导许可仍为0；六项编号能力不计入临时许可。

## Task6红绿检查

反例先经Go类型检查再交给既有typedTransitionFindings/wizardHelperFindings。六个编写处理文件各验证直接仓储、别名、方法值、多层转发、包级别名、旧响应规则、小仓储接口、领域渲染，以及错误转换夹带业务、通知、诊断和未登记应用方法。纯转换、三个明确应用接口和FrozenPreview分别有正例。

`task-6-red-01`：树62f6bcd15d1a4f4294347987cc57377b6c58d310，退出1。夹具均成功装载和类型检查；原守卫未报告间接路径等违规，断言得到空列表而失败。另一个失败精确报告5项残留许可与282总数。直接仓储和领域调用已能被原守卫拒绝。

`task-6-green-01`：树d8a66b1d82a110b9dd6158aa1efb51dae656ceba，退出1。生产全模块守卫通过；`TestIncrementalGateRejectsPackageInExistingRoot` 的全包编译发现两处旧测试仍读取已删除的Deps字段，`Test向导不能经同包辅助函数绕回旧业务/纯类型转换` 因模板辅助文件也成检查根而误报。修复为应用构造直接注入测试仓储；原向导模板辅助夹具仅换到legacy_templates.go，12个拒绝反例和纯转换断言保持不变，避免靠无关的常驻违规让反例通过。

测试准备缓存的红灯seed-red-02在四个合法账号创建成功后，因相同口令重复计算而失败（1.358秒）；seed-red-01只因夹具用户名非法失败，不充作行为红灯。最小缓存后seed-green-01并发聚焦通过120.872秒，追加多账号真实登录/改密隔离及原改密测试通过187.600秒。独立随机盐与向量测试继续直接执行生产函数。

格式命令单独在虚拟机执行。工具改动取回具体文件、重新暂存，再测试新树；格式前树不作为最终验证树。

## 最终完整验证

首次完整检查 `task-6-full-01` 测试树为8e7195a5a27dd433a6fca45d634d2027e48f757f，make退出2。格式、vet、强制全模块、普通全包通过；普通网页239.582秒、数据库113.890秒。并发数据库通过144.557秒，网页在1200.059秒达到原20分钟时限。没有数据竞争报告或此前失败断言。

超时时运行 `Test任务提交与独立实例审批的契约/任务已锁版本，同版本废弃等待提交`，该子例仅运行9秒；堆栈位于真实登录CheckPassword的600000轮PBKDF2，goroutine可运行，未显示卡在选择锁。完整原日志和堆栈保留。已核对重复账号准备成本，改为测试准备按口令原文并发安全复用由原生产函数生成的完整强度散列；真实登录、生产随机盐、独立向量与全部原断言保持。新增并发准备、不同原文口令、多账号真实登录与改密隔离验证；不提高时限，不降低密码强度。该轮失败导致后续Linux构建、Wire、网页类型检查及可选条件审计未执行；这些步骤在下述full-02才实际完成。

测试、构建、gofmt和Wire只在hostname为lober的Linux虚拟机运行，本机仅处理文本、Git和传输。测试库为lober_test，测试夹具创建临时schema；不使用产品库。

固定Go1.27.1、GOMAXPROCS=2、GOFLAGS=-p=2、GOPROXY=off；原Bash/BusyBox测试条件保持。并发全包保留 `-race -timeout=20m`，不降密码强度、不放宽断言、不跳过实际失败。生成网页类型只依赖既有Node/pnpm和内置模块，不安装node_modules。

第二次完整检查 `task-6-full-02`，被测树 `b9d48a39c1025e9e1d2d4e823bf6759298356e05`，退出0。开始 2026-10-22T19:22:03Z，结束 2026-10-22T19:45:11Z。普通网页 175.575s、数据库 111.865s；并发网页 814.464s、数据库 152.579s，保留原20分钟时限。

| 检查 | full-02实际结果 |
| --- | --- |
| gofmt，仅go list返回包 | 通过，无未格式化文件 |
| go vet ./... | 通过 |
| 强制全模块依赖守卫，-count=1 | 通过 |
| go test ./... | 全包通过 |
| go test -race -timeout=20m ./... | 全包通过，无数据竞争报告 |
| GOOS=linux GOARCH=amd64 go build ./... | 通过 |
| WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober | 通过，未改生成文件 |
| pnpm --dir web check:console-types | 通过，公开网页类型无漂移 |
| git diff --exit-code及工具working-changes | 退出0、变更为空 |

本阶段未修改前端业务，按计划不运行全量前端构建；没有安装node_modules。最终仅补阶段证据与计划检查框两份Markdown，生产源码、测试、守卫、构建配置及生成文件与上述被测树完全相同；最终提交树和精确差异保存在Task6报告。独立Task6及整分支审查仍未执行。

实际顺序为根目录 `make check`、`pnpm --dir web check:console-types`，再对下列四个可选用例点名核对，最后 `git diff --exit-code`。未开启LOBER_RECORD_RELEASE的一次性发布记录；未设置LOBER_TEST_REAL_ISO。

可选条件实际输出：

```text
--- PASS: TestPreseedMirrorSelectionsAreSeenAndAuthenticated (1.21s)
    --- PASS: TestPreseedMirrorSelectionsAreSeenAndAuthenticated/official (0.65s)
    --- PASS: TestPreseedMirrorSelectionsAreSeenAndAuthenticated/internal-signed (0.56s)
    render_test.go:284: one-time immutable release recording
--- SKIP: TestRecordCurrentRelease (0.00s)
    identify_real_test.go:17: LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in
--- SKIP: TestIdentifyRealISO (0.00s)
    realiso_test.go:15: LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in
--- SKIP: TestRealISO (0.00s)
```

未执行真实整机安装验收。

## 五项审查重点

| 重点 | 已有测试证据；最终独立审查待执行 |
| --- | --- |
| 不同最新版本定义 | Test编写页型与来源选择互不替代、候选全部版本分页、来源草稿与向导当前已审批选择 |
| 多错时返回顺序 | Test编写输入多错时保留原先错误、候选模板/插槽/index/分页组合和数字溢出回归 |
| 写后补读失败 | 组件状态写后补读、模板写后引用读取失败测试，只写一次且错误可见 |
| 旧任务冻结数据 | Test任务预览只使用冻结快照及真实任务预览测试，改新模板/发布和关闭编写仓储后结果不变 |
| 取消与资源释放 | 模板锁等待取消、审计回滚、共享锁组装、ESXi读取取消与路径保护；锁等待不承诺立即中断 |

数据库契约继续执行组件、模板、引用、审计、并发、普通CreateTasks和CurrentTaskCreator。数据库身份锁与引用锁保持原顺序，不能用进程锁通过替代数据库检查。

## 实施中的六项取舍

| 决定 | 原因 | 判断错的代价与保护 |
| --- | --- | --- |
| Task3先迁模板入口，旧templateHooks/templateResponses仅保留给尚未迁移的Task4/5消费者，最后消费者迁走再删 | 计划删除时点存在重叠；每步必须仍能构建，且不能扩大许可 | 可能暂留兼容桥；Task6以阶段2零许可和实际调用追踪关闭，最终旧桥已删除 |
| Task1先证明旧行为真实通过；Task2起新增应用入口先失败再实现 | Task1固定现状，不能为了流程故意制造旧行为失败 | 旧缺陷可能直到迁移后才发现；多错输入、事务、独立审查与最终全套继续补查 |
| 每项运行规定的聚焦测试和架构检查，最终Task6运行完整make；相同源码、依赖和环境可复用有效缓存 | 遵守阶段计划，避免每次小提交都重复约19分钟网页并发全包 | 局部遗漏可能延后到Task6发现；最终全套和独立整分支审查仍是交付门槛 |
| Task6完整验证通过后先做本地候选提交，独立Task6和整分支审查通过后才宣布交付 | 审查需要固定提交范围；计划中的“审查后提交”与这个要求存在顺序冲突，本地提交可撤销 | 会留下尚待审查的候选；状态必须明确，发现问题继续修复，不提前推送、合并或交付 |
| Task5额外适配cmd/lober/machine.go唯一旧bootConfigReader消费者，换同实现的media.NewBootConfigReader及同一根路径 | 计划漏列真实消费者，删旧实现后必须适配才能构建；不改变安装或介质流程，不提前读文件 | 若根路径或读取语义错误会影响真实ESXi安装；保留原路径，以原组装测试及读取器路径逃逸、取消测试验证 |
| Task6仅在seedAuthUser测试准备中按口令原文并发安全复用真实600000轮散列；生产函数、随机盐与向量测试、真实登录和原断言不变 | 首轮全套在1200.059秒超时，堆栈显示登录计算；只读调用计数与原日志证实重复准备成本，保留原20分钟门槛 | 复用可能掩盖不同口令或账号隔离问题；新增并发准备、原文区分、多账号真实登录与改密隔离测试，原完整make重新执行 |

## 待完成门槛与回退

Task6独立审查和冻结提交的整分支审查由控制者另行执行。未执行项不记通过。本阶段没有推送、合并或发布；合入develop前仍须核对新提交并对合并结果验证。

回退第二阶段必须回退其完整提交序列，不能只倒退锁实现而留下新组装。保留第一阶段当前模板原子创建及身份锁顺序。bbabdab是本阶段数字溢出兼容修复，依赖本阶段应用接口；回退本阶段时不能单独保留为无依赖修复。
