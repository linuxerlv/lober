# 第二阶段整分支技术审查

结论：技术上可以合并。Critical 0、Important 0、Minor 1。唯一小问题是已有测试日志噪声，不阻止本阶段通过。本结论不授权实际合并或推送，也不代表后续环境验收完成。

审查范围：`bdc6371749a9122680a18383561567e89a05e9c2` → `33ed106963ffe6cba6f15a38fee4c5ea40008402`，共9个提交、62个文件。候选树为 `84af3b1a7399f586b7bc0bf1e80b761a54c46f29`。

## Strengths

- `internal/application/authoring/hooks.go:29`、`templates.go:41`、`editor.go:25`：三个应用入口自己检查调用人和输入，网页不再负责业务判断。未授权请求在碰数据库或时钟前返回。五种角色、强制改密和缺少身份的边界有独立应用测试。
- `internal/application/authoring/hooks.go:89`、`templates.go:108`：保存草稿沿用原来整份内容替换的规则。作者、审批人、状态和内置标记由可信对象提供；缺内容的草稿仍可保存。没有顺手增加审批门槛或接受前端伪造作者。
- `cmd/lober/wire_gen.go:38`、`internal/application/authoring/templates.go:182`、`internal/application/tasks/submit.go:12`：生产装配只造一把选择锁，模板审批、废弃和向导提交共用它。数据库独立保护仍保留，不能用进程内的锁替代不同服务实例之间的保护。
- `internal/application/authoring/editor.go:119`、`templates.go:251`：候选计算复制模板、绑定、参数选项与指针后再替换。不会污染后续候选或已读取的模板。不同“最新版本”的业务含义没有被合并。
- `internal/application/authoring/renderer.go:28`、`internal/interfaces/console/tasks.go:277`：任务预览从已冻结的快照生成。文件顺序、脚本去重、真实任务编号和示例凭据保留；渲染中途失败不会返回半份文件。
- `internal/archtest/authoring_rules_test.go:13`、`:50`：六个网页文件分别用16个拒绝例、5个允许例验证边界。夹具先通过类型检查，再进入实际守卫；允许例排除了“所有测试都因无关错误失败”的假保护。

## 全阶段兼容核对

### 32条公开接口

逐条对照了原网页实现、新应用入口、请求与响应结构、路由权限、审计动作及测试。以下 `H` 是 `/console/v1/hooks`，`T` 是 `/console/v1/templates`，`V` 是 `/:id/versions/:version`。这只是报告缩写，实际地址没有变化。

| # | 方法与地址 | 应用入口 | 核对结果 |
|---|---|---|---|
| 1 | GET H | Hooks.List | 原查询、分页、状态与排序保留 |
| 2 | POST H | Hooks.Create | 不完整草稿、可信作者、无审计保留 |
| 3 | GET H/:id/versions | Hooks.Versions | 原版本列表与空列表形状保留 |
| 4 | POST H/:id/versions | Hooks.NewVersion | 原来源版本和作者规则保留 |
| 5 | GET H+V | Hooks.Get | 原读取、诊断和404保留 |
| 6 | PUT H+V | Hooks.SaveDraft | 原替换保存、修订号和元信息规则保留 |
| 7 | DELETE H+V | Hooks.DeleteDraft | 原修订号、引用和删除规则保留 |
| 8 | PUT H/:id/name | Hooks.Rename | 原命名规则与hook.rename审计保留 |
| 9 | POST H+V/submit | Hooks.Submit | 原状态检查与hook.submit审计保留 |
| 10 | POST H+V/approve | Hooks.Approve | 原审批人、状态与hook.approve审计保留 |
| 11 | POST H+V/reject | Hooks.Reject | 原理由优先级与hook.reject审计保留 |
| 12 | POST H+V/deprecate | Hooks.Deprecate | 原状态与hook.deprecate审计保留 |
| 13 | POST H+V/validate | Hooks.Validate | 原问题明细、顺序和无审计保留 |
| 14 | GET T | Templates.List | 原查询、分页、批量引用读取保留 |
| 15 | POST T | Templates.Create | 不完整草稿、可信作者、无审计保留 |
| 16 | POST T/:id/derive | Templates.Derive | 原来源版本、独立身份与可派生状态保留 |
| 17 | GET T/:id/versions | Templates.Versions | 原版本列表和诊断保留 |
| 18 | POST T/:id/versions | Templates.NewVersion | 原来源版本与复制规则保留 |
| 19 | GET T+V | Templates.Get | 原读取、引用诊断和404保留 |
| 20 | PUT T+V | Templates.SaveDraft | 原替换保存、修订号和元信息规则保留 |
| 21 | DELETE T+V | Templates.DeleteDraft | 原修订号、引用和删除规则保留 |
| 22 | PUT T/:id/name | Templates.Rename | 原命名规则与template.rename审计保留 |
| 23 | POST T+V/submit | Templates.Submit | 原状态与template.submit审计保留 |
| 24 | POST T+V/approve | Templates.Approve | 共享选择锁、引用检查和审计保留 |
| 25 | POST T+V/reject | Templates.Reject | 原理由优先级与template.reject审计保留 |
| 26 | POST T+V/deprecate | Templates.Deprecate | 共享选择锁、原状态和审计保留 |
| 27 | POST T+V/validate | Templates.Validate | 原问题明细、顺序和无审计保留 |
| 28 | GET T+V/slots | Editor.Slots | 原扫描顺序、重复项和位置保留 |
| 29 | GET T+V/hook-candidates | Editor.Candidates | 两种分页、替换位置和可用性判断保留 |
| 30 | GET T+V/source-diff | Editor.SourceDiff | 原派生来源与最新来源比较保留 |
| 31 | POST T+V/preview-params | Editor.PreviewParams | 原主机、镜像、参数和问题结构保留 |
| 32 | POST T+V/preview | Editor.Preview | 原文件、示例标记与错误结果保留 |

`internal/interfaces/console/authoring_contract_test.go:24` 手写固定32条集合，比较权限和审计，并逐路由验证五种角色。它还实际执行草稿写入、读取和预览，比较操作前后审计；因此无审计约定不是只查路由标签。生命周期、编辑器、预览及应用测试补充成功内容与异常分支，未用“非403”断言替代完整业务检查。

六个网页处理文件保留公开请求和响应字段；新结果到网页结构的转换保留空数组、字段名、可省略字段和顺序。分页拆分只是先解析数值，再由应用检查原规则。`RespondError` 的类别映射保持；普通非法输入仍是相同400，仓储错误不会泄露底层内容。

### 数据库、权限与五项重点

| 核对项 | 代码和证据 | 结论 |
|---|---|---|
| “最新”的不同含义 | `authoring/hooks.go:29`、`templates.go:41`、`editor.go:61`、`:156`；`authoring_contract_test.go:168`；`wizard_test.go:222` | 普通列表按身份取版本；候选页码模式查全版本，游标模式按原身份结果展开；来源比较包含最新草稿；向导仍选当前已审批版本。没有互相替代。 |
| 多项错误同时出现 | `editor.go:61`、`hooks.go:175`、`templates.go:196`；`authoring_contract_test.go:143`；`lifecycle_test.go`新增溢出回归 | 缺模板先404，合法模板的坏槽位先于坏分页；拒绝理由先于版本键和修订号。超过整数范围的十进制输入被归为非法值，不会取解析器留下的最大整数。Task3的修复已纳入整分支。 |
| 写成功、回读失败 | `hooks.go:151`、`templates.go:170`；`hooks_test.go`写后回读用例；`templates_test.go:190` | 一次事务已提交，随后回读可返回500；应用没有重复写、补审计或假装回滚。成功写入只调用一次，测试确认实际持久化和一次审计。 |
| 冻结任务预览 | `renderer.go:28`、`tasks.go:277`；`tasks_test.go:185`、`:460` | 关闭全部编写读取口仍可预览。发布新模板、脚本和镜像不改变旧输出；传入新参数也不改冻结内容。真实任务编号保留，凭据仍是示例，公开JSON形状不变。 |
| 等锁取消和读取原配置 | `templates.go:189`、`:218`；`authoring_contract_test.go:351`；`media/bootconfig.go:19` | 等待进程锁不承诺立即响应取消；拿锁后仍把原上下文传给数据库，失败路径释放锁。ESXi读取保持安全根目录、路径限制、关闭文件和根句柄、读前读后取消检查。 |
| 审计原子性 | `authoring_contract_test.go:229`；定向读取`pgstore/hooks.go:153`、`pgstore/templates.go:226` | 原状态、审批人和审计仍处于同一事务。真实数据库强制审计失败后比较完整对象和审计，均不落盘；重试还验证锁已释放。 |
| 同进程与独立实例竞争 | `wizard_test.go:469`、`:561`、`:713`；`pgstore/tasks.go:103`、`:136` | 共用锁测试用进入信号和数据库阻塞证据建立先后。独立实例使用不同连接池和不同进程锁，覆盖审批/废弃先后及旧读取；数据库身份锁、版本锁和引用锁继续阻止过期选择提交。 |
| 可信调用人、写入与通知 | `authoring/hooks.go:78`、`templates.go:97`及应用角色测试；固定路由审计集合 | 作者、审批人与审计人来自已验证调用人。保留五角色原权限；草稿操作不新增审计，编写流程不新增通知。 |

数据库锁的判断没有只依赖接口测试。我定向读取了范围外的 `pgstore/templates.go`、`pgstore/hooks.go` 和 `pgstore/tasks.go`：模板状态变更先锁身份再锁版本，审批按原顺序锁引用脚本；任务在数据库内重新检查当前模板，再锁主机、镜像、模板版本与脚本引用。没有发现本次装配和应用层搬迁引入反向锁顺序或绕过数据库重查。

还定向读取了范围外的 `console/hosts.go`、`domain/model/ident.go`、`domain/repository/check.go`、`console/errors.go`、`application/tasks/submit.go` 和 `archtest/transition_uses_test.go`。目的分别是确认主机编号错误形状、原校验规则、错误状态、向导持锁范围及新反例进入真实守卫；未借此扩大到其他模块的完整审查。

### 83项过渡许可与边界

对集成基线和候选清单做了完整逐行对照，并独立计算集合差。不是仅凭最终总数或Task6最后5项判断。

| 清单 | 基线条数 | 候选条数 | 删除 | 新增 | 非本阶段删除 |
|---|---:|---:|---:|---:|---:|
| `internal/archtest/migration_test.go` 仓储使用许可 | 153 | 98 | 55 | 0 | 0 |
| `internal/archtest/transition_manifest_test.go` 过渡规则清单 | 207 | 179 | 28 | 0 | 0 |
| 合计 | 360 | 277 | 83 | 0 | 0 |

所有删除项都属于第二阶段；没有把许可改名、换阶段或迁入其他清单。其余介质、任务等阶段条目保留。第二阶段和wizard.go许可均为0，四张清单合计277。六项正式身份能力、唯一通知桥没有修改或扩容。

`internal/archtest/wizard_helpers_test.go` 沿真实类型对象追踪同包函数、方法值、包级别名和多层转发；仅允许三个应用接口各自登记的方法与 `FrozenPreview.RenderFrozen`，没有放行整包。领域渲染、小接口直读仓储、旧响应辅助、通知、诊断和错误转换绕路都有拒绝例。原向导12个拒绝例保留；旧辅助移出新增根文件，使允许例和拒绝例能独立验证原因。

新选择锁基础实现仅承担同步；应用没有导入具体兄弟应用。网页依赖中的旧Hooks/Templates仓储已删除。没有数据库结构、生产配置、依赖或前端行为变更，内置发布内容未变；新公共文档没有出现本地连接串或凭据。

## 验证证据

本审查完整分段阅读了指定差异；被截断的源码段另行补读。阅读了绑定设计、六任务计划、公开契约、模块说明、全局约束、五项关注点、证据方向、七项裁定、Task6审查及原始日志。没有派代理，没有运行本机Go、构建、格式化、Wire或测试，也没有重复已经证明属于同源码的虚拟机套件。

- `task-6-full-01.exit` 为2。原日志显示20分钟超时，控制台包耗时1200.059秒；堆栈包含真实600000轮口令校验。该次失败保留，不作为通过证据，也没有把超时简单说成数据库死锁。
- `task-6-full-02.exit` 为0，`.tree` 为 `b9d48a39c1025e9e1d2d4e823bf6759298356e05`，`.working-changes` 为空。直接核对原日志可见完整格式检查、vet、架构检查、全包测试、并发检查、Linux构建、Wire差异检查和网页接口类型检查均完成。
- full-02普通控制台测试175.575秒，数据库包111.865秒；并发检查控制台814.464秒，数据库包152.579秒。架构强制重跑7.969秒。由此可见关键数据库和控制台检查实际执行，并非所有项目都来自旧缓存。
- 独立比较被测树到HEAD，只有计划和阶段证据两份Markdown变化。生产代码、测试、边界检查、生成装配和配置与退出0的被测树相同。复用该结果成立。
- 原始`task-6-guard-01.log`记录聚焦边界用例通过，0.237秒；`task-6-seed-isolation-01.log`记录真实账号隔离和改密相关并发检查通过，187.600秒。读取了真实红灯日志，失败原因是新增断言而非夹具无法编译。
- 镜像选择补充用例通过；真实镜像测试和发布记录测试明确跳过，见下方范围限制。没有发现需要新增聚焦执行才能裁定的具体源码疑点。

### 对七项裁定的独立判断

| 裁定 | 审查判断 |
|---|---|
| 1. 旧辅助等最后消费者迁移后删除 | 可接受。HEAD已删除相应业务桥与许可，调用追踪仍阻止绕回旧实现。临时保留没有成为最终例外。 |
| 2. Task1先刻画旧行为，后续新行为先失败再通过 | 可接受。旧实现基线用于兼容对照；新增边界反例与口令准备断言有真实失败记录，未伪造旧行为失败。 |
| 3. 每任务聚焦，最终完整检查 | 可接受。最终退出0与候选同源码已独立证明；无需用重复同套件替代审查。 |
| 4. 固定本地候选提交后审查 | 可接受。范围可复现，没有据此声称已经合并或交付后续环境。 |
| 5. 同步适配machine.go唯一旧读取器消费者 | 可接受。仍用相同ISO.Artifacts根路径，构造不提前读文件；安全读取逻辑及资源关闭保持。整机安装结果另行保留边界。 |
| 6. 测试准备复用真实口令散列 | 可接受。`auth_test.go:62`按完整原文作键，锁覆盖查找、计算与写入，失败不缓存；生产函数、600000轮、真实登录和独立随机盐/固定向量测试不变。`:157`还验证并行准备、尾部空格区别、两个账号真实登录和改密隔离。不是减弱密码或跳过登录检查。 |
| 7. 已有Gin日志噪声暂不处理 | 可接受，但仍保留为Minor。影响是读失败日志的成本，未掩盖退出码或断言；不需要为此变更源码并重跑本阶段。 |

以上裁定均由实际代码和证据支持。Task6未能单独判断的累计83项、全部32条接口、数据库行为及五项重点，已在本报告完成整分支核对，不再沿用为待核项。

## Issues

### Critical (Must Fix)

None。未发现造成数据丢失、越权、安全保护退化或功能失效的阻塞问题。

### Important (Should Fix)

None。未发现必须在本阶段合并前补修的架构、兼容、事务、锁或测试缺口。

### Minor (Nice to Have)

1. **已有测试调试输出过多。**
   - 位置：`.superpowers/sdd/2026-10-23-backend-ddd-phase2-authoring-preview/vm-logs/task-6-seed-red-02.log:1`。
   - 问题：Gin调试警告和完整路由列表占据大量测试日志。独立确认了Task6这一发现；它是现有测试环境输出，不是本阶段新增业务缺陷。
   - 影响：查找真正失败的断言较费时，但原始断言和退出码仍明确，不削弱本次证据。
   - 修复：后续在测试统一初始化处关闭这类调试输出，保留错误、断言和必要日志。本阶段无需为它修改代码或重复套件。

## Recommendations

1. 本审查通过后，把公开阶段证据和计划中的“整分支审查待完成”更新为本次确切结论及报告位置。当前待完成标记与审查时点相符，不列为缺陷；状态更新后若仍只改文档，无需重跑相同源码套件。
2. 后续单独清理上述测试日志噪声，避免与本阶段业务搬迁混合。
3. 进入develop、test和preview时继续遵守仓库原分支验收流程。按完整模块保留回退边界；不能用本报告替代企业测试或人工验收。

## Declined to judge

- 真实ISO内容和真实镜像读取的端到端结果：未设置`LOBER_TEST_REAL_ISO`，相关测试明确跳过。本次只判断已覆盖的渲染、选择和安全读取行为。
- 实际内置发布记录生成结果：未启用`LOBER_RECORD_RELEASE`，该记录测试跳过；内置发布代码与内容在本范围未变。
- 完整机器安装结果，包括真实设备上的ESXi安装：没有整机执行证据，本阶段也未声称完成。读取器迁移的源码与测试已审查，但不据此推定真实安装通过。
- 前端完整类型检查、生产构建与浏览器人工操作：本分支没有前端源代码改动。已审查公开接口并确认接口类型检查通过，不把它扩称为完整前端验收。
- 实际合入develop后的组合结果：尚未执行合并，不能从模块候选推定与其他分支共同运行的结果。
- test分支企业测试结果：本轮没有对应执行证据，需按仓库流程另行完成。
- preview分支人工验收和内部试用结果：本轮没有人工验收证据，需另行完成。
- main发布、远程推送及实际合并操作：不属于本次只读技术审查的执行授权或成果。

除此之外，无搁置的本阶段源码行为；没有把接口、数据库、锁、许可删除或五项关注点移交给后续审查。

## Assessment

**Ready to merge? Yes（仅技术结论）。**

**Reasoning:** 本阶段实现符合设计与六任务计划。完整差异、兼容路径、数据库保护和原始验证证据支持通过；Critical 0、Important 0、Minor 1，唯一小问题是非阻塞的已有日志噪声。

**Spec compliance:** Pass。**Task quality:** Approved。

该结论只覆盖上述固定候选，允许控制者据此完成本阶段技术门槛；不表示已经合并，不授权实际合并或推送，也不替代所列后续验收。


归档说明：报告引用的临时审查目录是执行时的位置。本地临时目录在收尾时清理，原始测试日志保留在本阶段lober虚拟机记录中；提交范围、源码树、命令、结果和本报告保留在Git文档中。
