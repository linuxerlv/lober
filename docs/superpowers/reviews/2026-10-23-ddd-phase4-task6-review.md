# 第四阶段 Task6 独立审查

最终结果：本项规格与质量门禁通过。首轮发现的重要问题 I1 已在 `cd9acd9` 修复，经另一位审查者局部复审确认关闭；未发现新问题。七文件去向说明 M2 已补齐。既有日志噪声 M1 保留给整分支审查判断。

这只代表第六项任务通过，整分支审查尚未完成，模块尚未交付或合入 dev。

## 首轮固定候选审查（历史结果）

以下保留对 `4010bb2..604e61f` 的原报告。其“未通过”是修复前结论，不是最终门禁结果。

## 规格符合性：未通过

- 本次固定审查：BASE `4010bb2b8a2b03f8ae7079cbead7a1a3a7f4b2b2`，HEAD `604e61f055c232ad48e7e3eb8b9010094fec8b61`。代码质量：**需要修正**。严重 0，重要 1，次要 2。
- 主要缺口是新网页文件可以用自己声明的小接口调用镜像仓储。已有文件中的同类反例会拒绝，换一个新文件就不受这条检查保护。见重要问题 I1。
- 这只是 Task6 审查。整分支审查、合入和交付均未判定通过。计划保留两项未勾选是正确的，见 `docs/superpowers/plans/2026-10-23-backend-ddd-phase4-media-jobs.md:263`。

### 已确认的优点与规格核对

| 要求 | 判断与证据 |
| --- | --- |
| 正反例先能通过类型检查 | 新的 `mediaTypedFixture` 在调用实际检查函数前调用 `types.Config.Check`，解析和类型错误均立即失败。见 `internal/archtest/media_rules_test.go:20`。应用跨层和领域文件读取反例也先检查类型，见同文件 `:140`、`:159`。 |
| 别名、点导入、方法值、同包转发 | 文件实现反例覆盖仓储类型、自己的能力接口、匿名接口、别名、点导入、包级方法值和辅助调用；允许文件会话与事实。见 `internal/archtest/media_rules_test.go:47`。网页已有入口覆盖两层方法值、包级别名、泛型转发和十一公开方法，见 `:86`。新文件仍有 I1 缺口。 |
| 状态字段与组装入口 | `mediaStateFindings` 按实际字段的 ISOState/SourceState 类型识别赋值、取地址、结构字面量；去括号后检查，能覆盖指针别名和复制结构。组装入口还禁止读取这两类状态，保留 TaskState 正例。见 `internal/archtest/transition_uses_test.go:195`、`internal/archtest/media_rules_test.go:114`。没有把整个领域包永久放行。 |
| 文件实现不能持有仓储 | `internal/archtest/iso_test.go:52` 删除 repository 导入许可；`transition_uses_test.go:102` 进一步检查方法集合、同名仓储方法和精确 port 类型；匿名接口声明也在 `:67` 检查。`catalog.Arch.Valid` 是单个明确例外。 |
| 应用只接受 io/fs 两个错误值 | `transition_uses_test.go:113` 仅允许 `ErrNotExist` 和 `SkipAll`，其他符号均拒绝。`media_rules_test.go:177` 同时验证两个正例及 ReadFile、WalkDir、FS 三个反例。没有改业务代码来迁就守卫。 |
| 领域只复用三个纯 repository 符号 | `transition_uses_test.go:122` 只允许精确 `domain/media` 的 CheckID、CheckSource、ErrInvalid；子包不继承许可。为确认原规则与新规则组合，聚焦查看 `media_domain_rules_test.go:13`、`:43`：纯校验、持有仓储、方法值、别名，以及相邻包、双方子包的拒绝测试仍存在。最终绿灯日志 `vm-logs/t6-green-04.safe.log:8`、`:20` 对应通过。 |
| 精确删除 154，保留 107 | `media_rules_test.go:209` 不是只比数量：将当前清单与明确删除集合并，检查重复、清除阶段、四个起点摘要，再检查总数和后两阶段 65/42。原前三阶段与向导清零仍由 `hosts_rules_test.go:72` 保护。新增删除集是 24/125/5/0，当前四清单目标是 64/0/42/1。八个摘要与 `root-exact-audit.json:1` 一致。原始起点对象的独立重算不在本次固定差异中，见无法确认项 C2。 |
| Task2 管理员八个写入口 | `registry_test.go:204` 每项使用有效输入，并检查无错误、实际调用顺序、编号、版本、来源和上传字节；创建检查 Created 和返回镜像；非上传检查审计，上传检查原有零审计。能力记录器在 `:143`。这验证应用参数传递，不冒充真实数据库提交。最终日志 `t6-final-02.safe.log:124` 至 `:141` 显示八项均执行。 |
| 不改变产品行为和其他阶段 | 固定差异的 Go 改动全部在测试文件；没有业务、数据库、前端、依赖、口令、缓存、超时或生成代码变化。`docs/contracts/media.md:116` 如实说明当前职责，并单列前项旧缺陷修正。Task6 本身没有新增通用事务回调或外部事件。前项实际事务与行为仍待整分支检查。 |
| 完整验收与报告真实性 | 原完整命令确实执行；失败轮没有改写为成功，最终冻结树另跑完整验收。具体核对见下一节。 |

### 已核对的验收证据

- 只读取本阶段脱敏日志和安全结果文件。没有本机 Go、Node、格式化、生成、测试或 VM 操作；没有重跑整套测试。
- `vm-logs/t6-final-02.tree:1` 为 `6ac66191d7a9ea3bdf16694d2a1e95cb2251e7fe`；started/finished 分别是 UTC 20:26:05、20:32:48，exit 为 0，changes/untracked 空。九轮的 tree、started、finished、exit、changes、untracked 均与报告表格吻合。
- `vm-logs/t6-final-02.safe.log:3` 至 `:84` 包含格式检查、vet、强制全模块发现 `-count=1`、普通测试、原 `go test -race -timeout=20m ./...`、Linux amd64 构建及 Wire 差异检查。未缩短竞态超时。运行脚本 `t6-final-02.run.sh:2` 使用 `set -euo pipefail`，故后续 EXIT=0 标记不是忽略前面错误后无条件成功的证据。
- 同日志普通段 `:7`、竞态段 `:45` 各有 26 个缓存包、5 个实际执行包、6 个无测试包。架构普通 136.068 秒、竞态 185.436 秒，强制发现 7.249 秒，与报告一致。没有将缓存描述为这轮重新执行。
- 同日志 `:87` 控制台契约类型检查成功；`:91` 原点名检查应用、文件实现、网页、真实数据库实际执行；cmd 无匹配。`:149` 的额外真实组装检查成功，弥补原过滤条件没有命中组装测试。
- 同日志 `:106`、`:114`、`:142` 明确出现三个跳过：一次性发布记录、两个未设置真实镜像输入的检查。`:95`、`:113` 是两个无匹配位置。`:100` 镜像认证两分项成功；`:235` Bash 清理成功。报告没有把跳过写成实物覆盖已执行。
- 同日志实际有 76 行 GIN 调试输出，TLS handshake error 为 0；报告准确限定为本轮可见输出。日志噪声保留为次要问题 M1。
- `vm-logs/t6-red-03.safe.log:15`、`:67`、`:85` 证实新增状态反例与应用导入反例曾漏报；`:90` 领域读取拒绝仍通过。`t6-green-04.safe.log:65` 至 `:71`、`:115`、`:116`、`:120` 对应修正后通过；尾部同时记录完整架构和应用包通过。首轮仅解析的应用反例未被伪称为类型有效证据。
- 首两轮误报也保存：`t6-green-01.safe.log:16` 拒绝整个 io/fs；`t6-green-02.safe.log:126` 拒绝精确 SkipAll。`t6-red-02.safe.log:13`、`:57`、`:61` 保存匿名仓储、ReadFile 和新文件漏报。未只看最后一次成功输出。
- `t6-final02-environment.safe.txt:1` 核实 lober、真实测试库、无竞争 Go 进程、31 GiB 空间、2111 MiB 内存和工具版本。`t6-committed-comparison.json:1` 记录 HEAD 与被测树仅三份 Markdown 不同，1870 个非 Markdown 路径无差异。根另有独立同树核对；本审查没有重做 Git 推导。

### 问题

#### 严重

无。

#### 重要

**I1：新网页文件中的本地镜像仓储接口能绕过检查。**

- 位置：`internal/archtest/transition_uses_test.go:131` 的 console 分支；`internal/archtest/wizard_helpers_test.go:94` 的固定文件名单；新文件反例在 `internal/archtest/media_rules_test.go:195`。
- 当前全局 console 分支只拒绝 repository 来源、精确 media/port 来源、非公开 media 方法及特定 ISOSourceState 方法。自己声明的 `reader.GetISO` 的声明包是 console，不符合这些条件。
- 本地接口调用的补充检查只从 `migratedConsoleFile` 列出的文件出发。将下面代码放在夹具的 `new.go`，既没有导入违规，也没有被列为检查起点；全局检查不会报告它。该代码是可独立通过 Go 类型检查的最小例子，问题由逐分支静态判断确定；本审查未声称已在 VM 运行它。

```go
package console

type reader interface { GetISO() }

func newMediaHandler(r reader) {
    read := r.GetISO
    read()
}
```

- 后果：把镜像读取能力改写为同包小接口，并把入口放入新文件，就能重新让网页承担仓储调用。已有 `isos.go` 的“局部小接口”反例会拒绝这个写法，但 `new.go` 的三条反例只覆盖直接使用外部类型，没有覆盖两种方式的组合。因此不能完成“新文件、方法值不能绕过”的要求。
- 建议：将镜像仓储方法的本地接口调用纳入不依赖文件名的 console 检查。不要把所有后续阶段的小接口一并禁止。将最小例子加入现有新文件反例，再补真实 GetISO 签名、直接调用与方法值两种形式。
- 建议仅在 VM 先运行 `go test ./internal/archtest -run '^Test镜像新网页文件不能调用内部能力$' -count=1 -v`，记录修正前失败和修正后成功。已有全套记录没有包含此样例，不能用原全绿代替这一步。

#### 次要

**M1：测试仍有既有日志噪声。**

- 位置：`vm-logs/t6-final-02.safe.log:151`；说明位置 `task-6-report.md:383`。
- 真实组装检查输出 GIN 调试警告和路由信息，共 76 行。报告诚实保留，本轮没有错误地声称输出干净。这不是 Task6 引入的产品缺陷，不阻止合入；应在后续专门整理测试日志，不能通过修改本轮设置或隐藏失败来消除记录。

**M2：计划指定的七个修改文件，在 Task6 差异中没有对应改动或逐项去向说明。**

- 位置：`task-6-brief.md:3`；固定审查包 `review-4010bb2..604e61f.diff:6` 文件列表；实际改动声明 `task-6-report.md:716`。
- 缺少的计划文件是 `internal/archtest/{deps,transition_rules,transition_manifest,migration,four_layer,domain_rules,authoring_rules}_test.go`。新建文件及 iso、transition_uses、hosts、契约、路线、计划均有对应差异；额外的 application_rules、wizard_helpers 和 registry 测试有任务理由。
- 这些文件可能已在 Task1至5完成所需改动，本次也未发现必须机械改动七个文件的理由。但不能据此把逐文件要求当成已核验。请根执行者明确记录已前置完成的提交/职责，或改正 Task6 文件清单。无需为凑文件数制造无意义改动。

### 无法从本次差异确认的项目

- **C1，前项业务契约与五项危险输入：** `task-6-brief.md:5`、`constraints.md:15`，以及 `docs/contracts/media.md:116` 所述 Registry→Control/Deletion、共享锁、Transfer/ImportSession/Journal、Supervisor 组合均是前项代码。本差异仅加检查与管理员测试，不能独立证明每项错误顺序、原子写入、通知时机、取消和恢复行为。整分支审查需覆盖十一入口及五个 Review Focus；本项没有代其通过。
- **C2，初始 dev 清单的原始逐项真实性：** `media_rules_test.go:209` 固定的摘要与删除集实现可核验；`root-exact-audit.json:1` 的八摘要、四数量和阶段数量均与文档一致。固定 Task6 差异不含起点清单的完整 Git 对象，也没有重新执行 Git 提取。根已有独立审计，应保留其起点对象和逐项比较证明；此处不把审计摘要冒充本审查重新逐项比较过起点。
- **C3，全部隐式跳过及真实镜像/安装能力：** `task-6-report.md:377`、`t6-final-02.safe.log:108`、`:116`、`:144`。普通和竞态日志是简洁输出，无法确认其每个用例有无隐式跳过。真实镜像输入缺失，实际安装与未具备外部工具也没有执行证明；报告已正确限制声明。
- **C4，同树证明和环境历史：** `t6-committed-comparison.json:1`、`t6-final02-environment.safe.txt:1`。本审查核对安全结果与日志一致，没有读取私有连接资料、原日志或重新计算 Git 全路径集合，不能独立见证传输过程、历史服务重启原因及所有私有文件权限。根的独立同树与环境核对负责这些事实。
- **C5，局部反例的 VM 结果：** I1 所给新文件本地接口样例尚无已有运行证据。静态控制流足以说明缺口；需要实施者按上述单例在 VM 留下红绿证据，而非本机测试。

### 审查范围与结构判断

- 固定 2094 行差异包分段阅读。工具曾截断两处文档内容，已只补读截断区间；没有重新生成 Git 差异。
- 差异截在函数中间，为完成新文件风险判断，补读 `wizardHelperFindings` 的开头；为确认实际全包装载和新子包是否漏掉，补读 `checkTypedTransitions` 与 `applicationModule` 的开头。后者只承认已登记的精确应用包和其 port，新子包并非自动被标准库放行。
- 包外聚焦核对仅为纯 repository 精确导入许可这一明确风险，定位并读取 `media_domain_rules_test.go`。它证明原相关类型反例及导入正反例仍在；没有扩展审查其他业务实现。
- 新建 `media_rules_test.go` 共 414 行，其中大段是必须明确固定的 154 项删除数据，剩余按边界类别分测试。增长有明确用途，未发现需要阻止合入的结构问题。新增状态检查独立成函数，仓储方法名单集中，未重复产品业务逻辑。

### 质量结论

**需要修正。** 验收记录、精确删除检查、管理员入口补测与绝大部分类型边界均有实证；但新文件与本地接口组合可以越过这次要建立的检查，I1 修复前不应批准 Task6。另保留两项次要问题和五项无法独立确认的边界，由根逐项裁定。

## 修复局部复审

**I1：新网页文件中的本地镜像仓储接口能绕过检查 — ADDRESSED。** `internal/archtest/transition_uses_test.go:133` 对同包接口的镜像仓储方法作全局拒绝，已不依赖入口文件名单。`internal/archtest/media_rules_test.go:205` 至 `:208` 补齐最小/真实 GetISO 签名、直接调用/方法值四种反例；`:209`、`:210` 的任务和身份接口仍允许。

#### Finding Verdicts

- **I1，全局覆盖 — ADDRESSED。** `transition_uses_test.go:57` 遍历所有传入文件的声明，`:66` 取得实际使用对象，再由 `:73` 调用同一限制函数。新增分支位于 `:131` 的 console 全局判断内；`:133` 检查实际函数、声明包和镜像仓储方法名，`:134` 至 `:136` 检查接口接收者。直接调用与保存方法值都会使用该方法对象，因此原 `new.go` 例子不能再漏过。
- **I1，类型有效与两种签名 — ADDRESSED。** `media_rules_test.go:32`、`:33` 增加 time/context 依赖夹具，Context 保留四个标准方法；`:43` 类型检查失败立即终止，`:46` 才执行实际边界遍历。`:213` 把九项样例全部放入 `new.go`。四个新增反例确实包含 `GetISO()` 和 `GetISO(context.Context,string)(model.ISO,error)`，各有直接调用和方法值形式。
- **I1，不误禁任务/身份接口 — ADDRESSED。** `transition_uses_test.go:194` 的既有名单只列镜像及其来源的仓储方法，未加入 GetTask/GetUser。`media_rules_test.go:209`、`:210` 两项允许样例各包含直接调用与方法值；安全绿灯日志 `vm-logs/t6-i1-green01.safe.log:19`、`:20` 均通过。
- **M2，七个计划文件的去向说明 — ADDRESSED（文档责任）。** `docs/superpowers/plans/2026-10-23-backend-ddd-phase4-media-jobs.md:254` 修正本项直接修改名单；`:260` 至 `:266` 逐项列出七文件在 Task2–5 的职责及提交。固定差异包包含该文档修正，满足原审查要求的“记录已前置完成的提交/职责”。未重新审查前项代码或重算旧提交。
- **M1，既有日志噪声 — DEFERRED，非本轮阻塞。** `task-6-report.md:1060` 保留说明；本轮安全日志仍有 76 行 GIN 调试输出，未隐藏或改设置。后续由整分支审查裁定。

#### New Breakage in the Fix Diff

- **None。** 未发现本修复引入的 Critical / Important / Minor 问题。实际代码差异仅涉及两个架构测试文件；新增全局限制没有删除旧规则、扩大许可或改产品行为。函数类型断言有前面的 `*types.Func` 判断保护；接口接收者判断不会把普通同包函数或具体类型方法一并拒绝。

#### Checks

- **固定范围核对。** 已完整读取 `review-604e61f..cd9acd9.diff`；首次合并输出被工具截断后，只补读缺失区间。包中两个提交为 `bf6020b` 和 `cd9acd9`，基线 `604e61f055c232ad48e7e3eb8b9010094fec8b61`，终点 `cd9acd9f339d5b99f86c1d587e72f5425c62bfc6`。未重新运行 Git 推导差异。
- **修前失败核对。** `vm-logs/t6-i1-red01.safe.log:6`、`:8`、`:10`、`:12` 均为 `allowed=false findings=[]`；`:19` 至 `:22` 四项失败，原三项反例和两项允许样例通过。失败来自漏报，没有类型错误冒充拦截。该轮树 `1250f60a9e7b33ebb3408fc61a482194cda65f9d`，退出 1，与 `task-6-report.md:793` 一致。
- **修后通过核对。** `vm-logs/t6-i1-green01.safe.log:11` 至 `:22` 九项均通过，退出 0；`.changes` 仅列 `internal/archtest/media_rules_test.go`，对应报告所述格式取回。该轮树 `00c69c0f24da51135edc8a48c944b6faf168bbef`，与 `task-6-report.md:794` 一致。
- **最终完整验收核对。** `vm-logs/t6-i1-final01.tree:1` 为 `7b41519dc07097f2c6880d77f95cbf129124edd3`；开始/结束为 UTC 20:44:45 / 20:51:15，退出 0，changes/untracked 均为零字节，与 `task-6-report.md:795` 一致。
- **覆盖修正代码的完整检查核对。** `vm-logs/t6-i1-final01.safe.log:3` 至 `:85` 依次记录格式、vet、强制全模块发现、普通测试、原 `go test -race -timeout=20m ./...`、Linux amd64 构建及 Wire 差异检查。全模块发现为 6.850 秒；架构普通/竞态分别为 139.958 / 170.847 秒，均实际执行。逐段计数均为 26 包缓存、5 包实际执行、6 包无测试文件，与报告一致。
- **原命令核对。** 只读字节比较确认 `t6-i1-final01.commands` 与 `t6-final-02.commands` 完全相同；没有额外重复整套架构测试。新轮安全日志 `:87` 至 `:98` 记录契约类型和原点名测试成功；`:124` 至 `:141` 管理员八入口通过；`:230` 真实组装通过；`:238`、`:239` Bash 检查通过。
- **限制核对。** 最终安全日志 `:108`、`:116`、`:144` 明列三项跳过，`:95`、`:113` 两处无匹配；报告 `:1058` 没把它们写成实物覆盖已执行。逐例未展开的普通/竞态测试，其隐式跳过总数仍未知。可见 GIN 输出 76 行、TLS handshake error 0 行，与报告一致。
- **环境和源码一致性证明核对。** `t6-i1-environment.safe.txt:2` 记录 lober、测试库 lober_test 未恢复且无竞争 Go 作业。`t6-i1-committed-comparison.json:3` 至 `:12` 绑定上述固定提交和最终被测树，1870 个非 Markdown 路径零差异，仅公开 evidence Markdown 有差异；与报告 `:1088` 至 `:1097` 一致。本审查核对安全证明，未重新独立计算 Git 全路径集合。
- **执行范围核对。** 本机未运行 Go、Node、格式化、生成或测试，未重跑套件；未读取私有环境文档、runtime.private.json、原始 .log 或其他计划目录。只写本复审文件，未改源码、索引、HEAD 或分支。已有记录已回答本修复的具体疑问，无需建议追加 VM 用例。

#### Out-of-Scope Observations

- **M1（原有 Minor，非阻塞）。** `vm-logs/t6-i1-final01.safe.log:151` 起仍有既有网页框架调试警告及路由输出。保持原记录，交整分支审查；本轮没有新增范围外缺陷。
- **范围限制保留。** 原审查 C1–C4 中前项业务组合、原始清单、实物/安装能力及历史环境的判断，仍由根和整分支审查负责。本轮新增红绿证据已补上原 C5 的本地接口例子，未借此宣布整分支或模块可交付。

#### Verdict

**Fix round: All findings addressed, no new Critical/Important breakage。** I1 已修复；M2 文档责任已补齐。M1 按既定安排保留到整分支审查，不阻塞本轮局部复审。

## 根执行者对无法确认项的处理

- C1：前五项业务契约均有单独实现、VM证据和独立门禁；整分支另查十一入口及五项危险输入，未用本项测试替代它。
- C2：根独立从起点 `48f6c2c` 提取四清单并逐条比较，确认只删第四阶段154项、没有新增或改写保留项。原Git对象和本阶段审计证明保留。65/42项后续阶段未变。
- C3：两个实物镜像检查和一次性发布记录跳过，不宣称实物、安装、企业测试或完整Vue检查已执行。非逐例日志无法给出全部隐式跳过总数，已如实保留限制。
- C4：根独立核对隔离环境及最终Git差异。最终被测树 `7b41519dc07097f2c6880d77f95cbf129124edd3` 原完整命令退出0，UTC20:44:45–20:51:15；远端changes/untracked为空。源码提交与被测树只有证据Markdown差异。连接私有文件由隔离帮助程序核对，不公开值；旧服务重启原因未知，未作稳定性推断。
- C5：I1的4个类型有效反例已真实失败复现，修正后9个正反例通过。随后原完整命令再次通过，并经独立局部复审关闭，已不再缺少这一证明。

收尾文档不改源码；根会在交付前再次对最终提交与上述被测树作精确差异检查。
