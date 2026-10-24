# 后端终态独立审查 A：结构、规则归属与守卫

> 本文是固定提交的独立源码审查记录。原文中的运行状态按审查完成时点保留；根端随后完成的正式VM验证、独立反例及对范围的裁定统一见[总审计](2026-10-25-ddd-final-audit.md)。不把作者读到测试源码等同于作者现场重跑。

结论：四层目录、控制台主流程、组装和原过渡表清零已有实质落地；不能据此认定整个后端已达到总设计终态。既有机器执行链仍横跨接口、应用和基础设施承担业务规则；新文件的主机仓储旁路也未被当前守卫完整覆盖。

本轮暂计 **C=0 / I=2 / M=1**。C/I/M 分别为严重、重要、轻微。两个 I 是架构交付与自动守卫缺口，不是已证明的生产数据错误。M 是已有行为对照测试的规则双实现例外。下文严格区分源码事实与尚待 VM 复现的结论。

## 1. 范围与证据身份

- 唯一生产源码：`E:/src/lober/.worktree/rbac-integration`，HEAD=`2e64ef1d8fe2976ff69fd0634797103810418c86`。开始和结束读取均为此提交。
- 仅有两项已知工作区变化：`internal/interfaces/console/ui/dist/_placeholder-keeps-embed-underscore-mode.js` 删除、`internal/interfaces/console/ui/dist/index.html` 修改。本审查忽略它们，不评价这些页面产物或其运行效果。
- 阅读依据：2026-10-22 四层总设计，尤其 §4/5/6/7/9/10；六阶段总路线；`docs/contracts` 六份契约；第六阶段批准计划中保留原执行服务、机器协议和日志采集行为的范围。
- 只读源码与 Git。未在本机执行 Go、gofmt、Wire、Node、构建或测试；未连接 VM；未读取私有连接资料或数据库连接串。历史审查记录仅用于识别批准范围，没有把旧 PASS 当本次证明。
- 根代理额外授权后，仅另写私有复现文件 `evidence/probe-structure_test.go`；它不在 DEV 源码中，也未在本机执行。正式 VM 复现结果待根代理回传。
- 其他审查者负责细业务契约。本审查核对依赖、职责、规则来源、组装和防绕过能力，不重复全模块的错误顺序、事务锁时序和每条请求语义。

## 2. 重要发现 I1：机器执行链仍未完成职责拆分

**位置与实际路径：**

1. `internal/application/execution/protocol.go:52` 的 `boot` 根据平台兼容、引导次数和时间决定任务失败或进入引导；`:101` 的 `advance` 是纯状态推进；`:244` 和 `:250` 的 `CanCancel/CanRetry` 决定是否允许取消、重试。`advance` 的注释自己明确写为纯函数。规则没有外部操作，符合应归领域的性质，却仍由应用包拥有。
2. `internal/interfaces/machine/http.go:167` 的 `installerBoot` 在 `:173` 读取冻结格式决定是否可走安装器应答，并串联 `Preseed → render → Credentials → Answer → observe`。`:192` 的 `file` 遍历快照插槽，决定哪些组件可以取出，在 `:217` 校验正文摘要，再在 `:256` 决定发主应答前推进状态。请求路径解析、响应头和写响应可以留接口；这些快照选择与业务步骤编排超出了转换职责。
3. 同文件 `:338` 的 `final` 条件，和 `internal/application/execution/service.go:124` 的 `finalSignal` 逐项重复：首次启动时机、booted、init-start、init-done、ready。它们分别为观察认证和实际信号选择凭据范围。以后增加阶段必须同时改两处。
4. `internal/infrastructure/events/syslog.go:66` 的 `Receive` 在 `:78` 读任务，`:85` 决定已结束或旧尝试的日志不得记录，`:92` 保存事件，`:94` 提交后通知。网络地址、临时地址表和日志字节处理属于基础设施；取业务事实、决定可收日志、写业务事件和发提示的流程仍留在基础设施。

**触发例：** 启用机器能力后，Debian 安装器请求 `/v1/boot`，或请求冻结任务组件、上报首次启动信号、发送 syslog。以上都是当前接线可达的生产路径，见 `cmd/lober/machine.go:60` 前后建立 Engine、Renderer、Collector，以及 `cmd/lober/main.go` 启动 syslog 循环。

**违反的终态约定：** 总设计 §4 纯规则归领域、§5 接口只依赖应用入口和必要值类型、§7 一条规则一份实现、§10 接口无业务流程/基础设施只实现契约；总路线的对应终态条目。

**实际影响：** 执行规则仍需跨三层维护；信号范围已经存在条件双写。无法用“领域唯一规则”和“接口只有转换”描述整个后端。这里没有证明现有信号、文件或日志行为已经出错，也没有要求新增执行能力。

**批准范围的区别：** 第六阶段计划第 1 节明确保留既有执行服务；Task7 也只迁移组装里的渲染、观察策略和密钥。按这个较窄范围，它是有意保留的既有职责债务，不能当成该阶段偷偷新增的行为回归。但这与总设计宣称整个后端终态之间仍有范围差距，原过渡表为零并未登记或消除它。

**建议：** 单列机器执行链收尾。将纯 `boot/advance/控制诊断/信号范围/日志可接收条件` 移入领域，应用只读事实、调用规则和提交。提供取机器文件及安装器应答的应用入口，把快照选择、摘要验证与推进顺序一并移入。日志基础设施只提供地址和字节能力，由应用组织保存与通知。保留全部旧协议行为，先固定回归，不扩大业务。

**验证状态：** 归属和重复条件由源码已证实；运行行为及搬迁后的兼容性待 VM。现有测试名称见第 6 节。

## 3. 重要发现 I2：终态守卫仍有可绕过的能力边界

### 3.1 新控制台文件可自建主机仓储接口

**位置：** `internal/archtest/wizard_helpers_test.go:26` 仅把 `migratedConsoleFile` 列出的旧文件作为调用追踪起点；`:94` 的列表不包含新文件和 `routes.go`。`transition_uses_test.go:133` 对控制台自建接口只额外识别镜像仓储方法名。`task_finalize_rules_test.go:445` 只补任务能力名；`identity_audit_rules_test.go:36` 只补身份和审计能力名。

**可复现输入：** 新文件 `internal/interfaces/console/host_extra.go`：

```go
package console

import (
    "context"
    m "github.com/lober/lober/internal/domain/model"
)

type hostReader interface {
    GetHost(context.Context, string) (m.Host, error)
}

func readHost(ctx context.Context, r hostReader) (m.Host, error) {
    return r.GetHost(ctx, "SN-1")
}
```

导入规则允许 context/model；这不引用 repository 的真实符号，GetHost 也不在上述全局方法名补丁中；新文件不是旧根列表的一员。即使只声明未调用的危险能力，按当前终态要求也不应凭文件名获得许可。新路由从 `routes.go` 接到新文件时，旧根列表本身也不提供检查入口。

私有探针 `evidence/probe-structure_test.go` 复用现有 `identityFixtureImports`、`identityTypedFixture`：先真实类型检查，再走 `checkModule + typedTransitionFindings`，断言应拒绝。**本审查没有运行；待 VM 预期为红，红表示守卫没有报出违规。**

### 3.2 机器接口仍明文允许仓储契约

`internal/archtest/machine_rules_test.go:11` 用同一个检查器处理执行、机器接口和日志基础设施，在 `:28` 的许可式中对三者都开放 `pkgStore`、整个领域和大部分标准库。`execution_guard_test.go:28` 起的正例还明确断言机器接口导入仓储是允许的。`taskFinalFindings` 在 `task_finalize_rules_test.go:19` 只管 console 和 cmd，机器接口不进入该补强。

例如在机器接口新增接收 `repository.Tasks` 并调用 `GetTask` 的函数，按这些规则没有对应拒绝项。这不是当前机器接口已经直接读仓储的指控；它是与 §5/§9 目标相反的正式许可。

### 3.3 主机和编写应用能直接读文件或连网络

`internal/archtest/application_rules_test.go:93` 的外部操作限制只覆盖 tasks、identity、audit、media；`:110` 随后放行标准库。hosts/authoring 目前没有同等限制。

例如在 `internal/application/hosts` 新增 `import "os"` 和 `func readLocal(p string)([]byte,error){return os.ReadFile(p)}`，或者在 authoring 使用 `http.Get/sql.Open`，现有应用导入规则会放行，类型检查阶段也没有补拒。此处只证明守卫遗漏；本轮扫生产源码未发现 hosts/authoring 正在直接这样访问文件、网络或数据库。

**违反的终态约定：** 总设计 §5 接口禁止仓储、应用通过自有外部能力；§9 全模块边界与故意违规样例，不能只保护旧文件；§10 防止职责重新混杂。

**实际影响：** 现有检查即使全绿，也不能证明新入口与所有应用模块都遵守目标边界。新文件或小接口可以恢复旧职责，而四张迁移表仍保持零。

**建议：** 以所有生产控制台文件为检查起点，统一识别各仓储能力和自建能力，不只识别三个模块的方法名。为机器接口单列接口层规则。将所有应用模块统一纳入外部文件/网络/数据库/随机数约束，外部操作通过精确 port。补真实类型可通过的拒绝样例，保留纯输出转换正例。

**验证状态：** 分支遗漏和明文正例由源码证实；新文件完整探针待根代理在 VM 独立副本执行。不得把静态推导写成已跑失败。

## 4. 轻微发现 M1：模板页码筛选仍保留双份规则，已有有限对照保护

**位置：** `internal/infrastructure/persistence/pgstore/template_selection.go:11` 明确说明它是 `admission.TemplateUsable` 和旧内置选择的 SQL 等价实现；`:19-47` 重写审批态、适用系、废弃引用、内置清单与 offer 可用性。

**触发例与调用证据：** `application/tasks/wizard.go:111-124` 在页码模式传 `TemplateSelection`，仓储 `templates.go:367-373` 在计数分页前用 SQL 规则；游标模式在应用直接调用 `selection.CurrentBuiltin` 与 `admission.TemplateUsable`。领域规则或内置清单规则变化后，需要同时维护 SQL 表达。

**设计与批准范围：** 这是 §7“一条规则一份实现”的例外，不是锁内读取新事实后复用领域函数。`docs/contracts/backend-modules.md:21` 已明确要求页码先筛选再计数，不能为去重破坏该外部行为。查询下推有实际理由，本报告不把它误判为新业务政策。

**已有保护：** `pgstore/pagination_test.go:179` 的 `TestNumberedTemplateEligibilityBeforePagination` 覆盖新草稿、缺 offer、废弃组件、旧/当前内置版本，并对 nil、空、有效 offers 三组逐页对比数量和编号。`:228` 直接调用 `admission.TemplateUsable`。因此不能说完全无防漂移测试，也不能说当前已经选错。

**剩余限制：** 测试在 `:222-227` 仍手写 builtinOK，没有直接用 `selection.CurrentBuiltin`；样本不是全部状态、引用和发布清单组合。当前契约批准分页结果，不等于总设计已记录并批准业务规则双实现的架构例外。

**影响与建议：** 当前可观察错误未证实，按 M 记录。至少将此双实现列入正式架构例外和变更检查清单，让测试直接复用两个领域判定，并补关键组合；若要严格达到唯一实现，设计领域拥有的查询条件表达，再由仓储翻译，或以可接受成本在应用筛选后分页。不要在本审查中擅自改分页语义。

## 5. 检查矩阵

以下“符合”只指本次源码检查，不代表本次已实际运行。

| 设计条目 | 生产代码证据 | 自动守卫或测试 | 结论 |
| --- | --- | --- | --- |
| §4 四层目录、组装不作第五业务层 | 当前生产目录集中在 interfaces/application/domain/infrastructure；cmd 另列 | `Test新模块不能漏出守卫`、`Test全模块依赖守卫` | 目录符合；执行链职责例外见 I1 |
| §5 控制台面向应用入口 | `interfaces/console/server.go:18` 的 Deps 仅应用入口、时钟和日志，无仓储 | `transitionRestricted`、`wizardHelperFindings`、`taskFinalFindings` | 已迁移生产入口实质符合；新文件旁路见 I2 |
| §5 应用不导入别的具体应用服务 | 全部 application 生产导入仅本模块 port、共同 ports、领域 | `applicationModule/applicationRule`、`Test应用边界不接受其他服务或伪装接口包` | 当前导入符合；外部标准库守卫缺口见 I2 |
| §5 领域不读外部事实 | domain 生产源码未见文件/网络/数据库/时钟/随机数导入或调用；模型无网页/数据库字段标签 | 标准库白名单、`checkClock`、`checkContextEffects`、`Test领域拒绝随机数和外部操作` | 当前领域纯度符合；不代表所有纯规则都已搬入领域 |
| §5 基础设施只导入精确能力包 | media 只用 media/port；credentials 只用 identity/port；notification/selection 用共同 ports | `Test基础设施拒绝接口层和应用具体服务`、`Test镜像文件能力只向精确接口开放` | 导入方向符合；events 流程归属仍见 I1 |
| §5 基础设施不形成实现互调网 | 除 media 自身解析子包、pgtest 测试设施与六项编号叶子能力外，未见数据库/文件/通知实现互相导入 | `identityCapabilities` 精确函数许可及导入白名单 | 当前网络符合；六项编号是正式能力，不是临时豁免 |
| §6 业务模型/网页/数据库记录分开 | domain/model、console DTO、pgstore rows 独立；应用输入为普通 Go 值 | 模型边界测试、原有网页与仓储契约测试 | 结构符合；细字段兼容交其他审查者和 VM |
| §6 调用方小接口 | tasks、authoring、hosts、identity、audit、media 的 port 文件；执行观察 port；共同 Clock/Notifier/FrozenPreview | 编译接口实现及对应组装测试 | 已有明确方法集，不是总服务大接口；新接口防绕过不足见 I2 |
| §7 提交时复用规则 | pgstore/tasks.go 调 `repository.BatchProblems`、`model.CurrentApproved`；hooks/templates 调 `repository.Approve*Problems` | 原真实数据库任务创建、审批/废弃与并发契约 | 源码复用链存在；并发/回滚结果待 VM，未重新裁决细语义 |
| §7 相关写入与审计、提交后通知 | `pgstore/tasks.go:45-87` 在同一事务写任务与审计；应用提交成功后 Publish；共享 notifier 注入 | submit/控制/事件、数据库原子性、组装测试 | 当前接线和事务结构符合；未宣称本次实测 |
| §7 规则唯一来源 | activation/admission/params/snapshot/selection 供应用与仓储复用 | 相应领域及数据库契约测试 | 主要迁移规则已复用；I1 与 M1 是剩余例外 |
| §9 未知包失败、扫描不空跑 | deps_test.go 的 `packagesUnder/moduleSourceDirs/checkModule/checkTypedTransitions`，Makefile 强制 `archcheck -count=1` | `Test未知包不能绕过四层检查`、`TestIncrementalGateRejectsPackageInExistingRoot`、构建约束及计数反例 | 有真实的发现与类型装载机制，非只查目录；方法语义覆盖仍见 I2 |
| §9 原过渡入口清零 | migrationExceptions 及另三表均空，旧 internal/core/store/console/iso/render/builtin 生产根不存在 | 第六阶段39项原始摘要/精确删除/当前四表零，之前68/154历史保护 | 原登记迁移范围已清零；不能据此推出所有新路径职责都已完成 |
| §10 组装只连接与启停 | `cmd/lober/app.go:46` 只先内置初始化再身份初始化；Wire 单例 notifier/guard/renderer；machineAdapter 字段转换 | `Test启动组装只委托两个初始化入口`、任务接线与机器组装测试、Wire diff | 生产组装已实质收尾；VM 生成漂移结果待根代理 |

## 6. 建议在 lober VM 补跑的精确测试

以下均未由本审查者执行。根代理的正式固定树检查与探针检查应分开记录；探针失败不能污染或冒充交付树的 `make check` 结果。

1. 在独立临时副本，把 `evidence/probe-structure_test.go` 复制为 `internal/archtest/probe_structure_test.go`，运行 `go test -count=1 -v -run '^TestFinalAuditConsoleNewFileCannotInventHostRepository$' ./internal/archtest`。当前预期失败，原因必须是“终态守卫漏拦”，而非装载/编译错误。记下日志、实际源码身份和退出码。
2. 正式树守卫：`Test全模块依赖守卫`、`TestIncrementalGateRejectsPackageInExistingRoot`、`Test未知包不能绕过四层检查`、`Test任务终态规则接入真实全模块扫描`、`Test终态零许可且历史不能复活调用`、`Test第六阶段迁移精确删除39项当前四表全零`、`Test装配只豁免规范Wire注入文件且仍检查其导入`。
3. 执行链回归：execution 包的 `TestProtocolRequiresFrozenHooksAndExactDuplicateReports`、`TestProtocolTracksInstallRebootInitializationAndDone`、`TestCredentialsBindTaskAttemptScopeAndExpiry`；machine 包的 `TestCanonicalFrozenFileRouting`、`TestBootServesPreseedAnswerToDebianInstallerAgent`、`Test机器观察只在凭据检查通过后调用`；events 包的 `TestSyslogNeedsAuthenticatedIPLeaseAndRedactsCredentials`、`TestSyslogRejectsOldAttemptsAndBoundedPackets`。
4. 分页双实现现有对照：pgstore 包 `TestNumberedTemplateEligibilityBeforePagination`；console 包 `TestNumberedWizardListsAndEligibleTemplateTotals`、`Test内置选择拒绝旧发布但不改变冻结任务`。
5. 关键组装：cmd/lober 包 `Test任务读取和预览显式组装`、`Test任务控制显式共用后端时钟和通知`、`Test任务控制未启用明确不可用`、`Test任务事件提供者复用原仓储与通知实例`、`Test真实接线先准备全部内置内容再创建管理员`、`TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol`。

仍需完整 `make check`，以上只用于定位证据，不替代全套检查。数据库项必须实际连测试库且没有 Skip。本报告不记录连接材料。

## 7. 暂不裁决和限制

- 75条控制台接口的每个字段、错误优先级、冻结参数顺序、会话并发、镜像资源释放细节由另外两位审查者核对，避免重复结论。
- 本次不宣称 SQL 两份规则已经不一致，也不宣称现有 hosts/authoring 已直接操作外部资源；已证明的是规则双实现与自动检查覆盖缺口。
- 执行链是迁移前已有并在阶段计划中明确保留的实现。本报告指出总设计验收范围缺失，不把历史源码路径或历史注释本身当当前缺陷，不要求实现新装机能力。
- UI dirty、真实安装器/真实镜像内容、外网环境和部署效果不在本次只读审查证据内。VM 最新检查结果由根代理补充，不能由本报告推断通过。
- 未发现新的源码层跨应用具体服务导入、领域外部操作、原迁移表非零、启动组装暗藏内置重试或控制台生产直连实际仓储的证据。这个负面检查结果不覆盖 I2 的未来新增能力旁路。

最终判断：可认定“六阶段所登记的控制台迁移与组装清理已实质完成”，不可认定“全后端四层职责和防绕过自动守卫已全部落地”。两个 I 应在宣称总设计完成前关闭，或由用户明确确认、登记为仍未完成的范围；M 应保留可见的例外与防漂移检查。
