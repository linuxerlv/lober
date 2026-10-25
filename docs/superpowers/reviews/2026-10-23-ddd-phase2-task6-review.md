# Task6 独立审查

Spec compliance：✅ 本任务候选符合要求。Task quality：Approved。

问题计数：Critical 0；Important 0；Minor 1（测试输出噪声，不阻止本任务通过）。

审查范围：`c577c61bd5649971041eecea9cb4c2a742e5a38d` → `33ed106963ffe6cba6f15a38fee4c5ea40008402`。本结论只批准Task6候选，不代表整分支、develop集成、企业测试或人工验收完成。

## 已核对的优点与要求

1. `internal/archtest/authoring_rules_test.go:13`、`:45`、`:50`：六个编写文件分别有16个拒绝例和5个允许例。每个例子先通过Go类型检查，再调用实际守卫。旧辅助、别名、方法值、两层转发、小接口、错误转换、通知、诊断、未登记方法及领域渲染均有对应路径。
2. `internal/archtest/authoring_rules_test.go:23`、`:73`：反例共用的旧登录辅助放在非根文件，纯转换及三个应用入口、冻结预览使用同一套夹具而应无违规。这排除了“无关常驻违规导致全部反例假通过”的问题。旧响应辅助实际到达仓储读取和Role.Valid；小接口不是靠语法错误制造失败。
3. `internal/archtest/wizard_helpers_test.go:26`、`:69`、`:80`、`:94`、`:103`：检查沿真实types.Object追踪同包函数与包级别名；检查根新增全部编写文件。允许项精确到Wizard五方法、Hooks十三方法、Templates十四方法、Editor五方法和FrozenPreview.RenderFrozen。未放行整个应用包或共同能力包。原StatusOfProblems/Code.Category例外仍精确限定到文件、函数和方法。
4. `internal/archtest/wizard_rules_test.go:18`、`:35`：唯一夹具改动是将旧模板业务辅助移到legacy_templates.go；12个拒绝例与纯转换正例保持。对旧诊断、通知、小接口直写的拒绝未削弱。定向补读了差异未完整展示的测试后半段。
5. `internal/archtest/migration_test.go:176`、`:213`、`internal/archtest/transition_manifest_test.go:148`：本差异确实只删除最后4条仓储许可和1条领域规则许可，均为阶段2，没有新增或更换阶段。`internal/archtest/authoring_rules_test.go:87` 检查阶段2与wizard.go许可为0、四张清单合计277。六项正式编号能力和唯一通知桥在本任务没有修改或扩容。此前78项的逐条历史核对见下方范围限制。
6. `internal/interfaces/console/server.go:20`、`cmd/lober/app.go:133`：删除Hooks/Templates仓储字段与生产注入。本任务未重新加入SelectionGuard或旧业务桥。`internal/interfaces/console/harness_test.go:39`、`:48`、`:64`：向导和三个编写服务直接从显式测试store取得能力；同一guard传给模板和向导；门控测试只替换CurrentTaskCreator，未把读取口换成无效替身。
7. `internal/interfaces/console/hooks.go:104`、`internal/interfaces/console/templates.go:138`：网页保留解析，把状态、分页有效性和版本键检查交给已有应用入口。为检查“删除网页检查会不会接受非法输入或改变公开错误”，定向核对了未改的pagination.go、authoring/hooks.go:29、templates.go:41及:88、repository/check.go:202和console/errors.go:127。应用仍检查原规则，普通ErrInvalid仍映射相同400原因；新解析继续保留数字溢出及页型约束。
8. `internal/interfaces/console/lifecycle_test.go:205`：关闭仓储直接注入模板应用的Hooks读取口，500与不泄露底层错误的断言保留。`internal/interfaces/console/tasks_test.go:237`：关闭全部编写读取口后仍比较完整冻结预览，未删减旧快照断言。为判断templateKeyOf检查转移的影响，补读了差异在函数中截断的模板保存/删除，以及未改的生命周期、预览和候选调用点；未发现本次转移造成可见错误变化。
9. `internal/interfaces/console/auth_test.go:57`、`:62`：缓存键是完整原文字符串；互斥锁覆盖查找、真实HashPassword调用和存入，结果为不可变字符串。只改变测试准备，未改生产散列、真实登录或改密。错误不会存入缓存，失败退出仍释放锁。缓存没有按账号、裁剪后的密码或固定弱哈希替代真实计算。
10. `internal/interfaces/console/auth_test.go:15`、`:157`、`:181`：独立盐和固定向量测试直接调用HashPassword，检查600000轮、16字节盐、32字节结果、两个随机结果不同及错误密码。新测试并行建立四个账号，验证相同原文复用、尾部空格区分；随后用两个真实账号登录、改其中一个密码，验证另一个会话及旧密码仍有效、首个旧密码失败、新密码成功。准备缓存没有替代这些真实验证。
11. `docs/superpowers/reviews/2026-10-23-ddd-phase2-evidence.md:50`、`:58`、`:72`、`:117`：公开文档区分full-01超时失败与full-02成功，保留原20分钟门槛；说明跳过的真实镜像和发布记录、未执行整机验收、未进行整分支审查。没有把本地候选当成已交付。新增公开内容未出现SSH端点、密钥、口令或私有连接串。计划最后两项仍未勾选。

## 问题

### Critical

None。

### Important

None。

### Minor

- `.superpowers/sdd/2026-10-23-backend-ddd-phase2-authoring-preview/vm-logs/task-6-seed-red-02.log:1`：真实红灯日志包含Gin debug警告和完整路由打印，正文断言被大量准备输出淹没。这不推翻红灯，也不是本任务引入的生产缺陷。后续可在测试初始化统一关闭这类调试输出，保留错误与断言；本次无需因此重复测试。

## 证据与审查方式

- 阅读了任务brief、context、全局约束、五项关注点、证据方向、六项rulings、实施报告以及规定的reviewer模板。使用指定diff作为修改的唯一差异来源；首次显示被工具截断的文档片段单独补读。未重跑Git差异或改变索引、HEAD、分支。
- 定向读 `internal/archtest/transition_uses_test.go:57`，核对新夹具确实进入生产全模块守卫使用的类型检查链；补读wizard_helpers_test.go差异缺失的节点建表/递归部分，确认包级函数别名的追踪方式。不是按报告自评认定有效。
- 原始 `vm-logs/task-6-red-01.log:1` 显示间接访问等反例得到空违规列表而失败；未出现夹具类型错误。`vm-logs/task-6-guard-01.log:1` 显示新旧反例和正例聚焦检查通过，耗时0.237秒。
- 原始 `vm-logs/task-6-seed-red-02.log` 显示四账号准备后因同口令重复散列断言失败，包耗时1.358秒；`vm-logs/task-6-seed-isolation-01.log:1` 显示真实账号隔离及原改密聚焦并发检查通过，耗时187.600秒。
- full-01退出2、full-02退出0及其原始.exit/.tree/.working-changes/日志末尾已由控制者读取确认，本审查复用该证据，没有重复全套。被测树b9d48a39c1025e9e1d2d4e823bf6759298356e05到候选树84af3b1a7399f586b7bc0bf1e80b761a54c46f29仅计划与阶段证据两份Markdown变化，控制者已核对；本审查未将实施者自述单独当成完成证明。
- 本地只读文本及进行指定报告写入。未执行Go、格式化、构建、Wire、前端生成或测试。没有发现需要新聚焦执行才能判断的具体源码疑点；未派子代理。

## Cannot verify from this diff

- `docs/superpowers/reviews/2026-10-23-ddd-phase2-evidence.md:32` 的累计55+28删除涉及Tasks2–5，本任务差异只含最后5项。总数断言不能代替此前78项逐条核对；整分支审查应对集成基线逐条确认只有阶段2许可删除，其他阶段无增删、转名或转阶段。本任务自身的5项删除已核实。
- `docs/superpowers/reviews/2026-10-23-ddd-phase2-evidence.md:94` 的32条路由、数据库完整契约及五项关注点横跨前五任务，不能由本Task6差异单独得出整分支兼容结论。控制者须完成已安排的冻结提交整分支审查，之后才能标记模块交付。

## Declined to judge

- 整分支逐路由和数据库行为的独立批准：不在本次Task6提交范围；已在Cannot verify列出，交由单独整分支审查。未默认为通过。
- 真实ISO、完整机器安装、企业测试、preview人工验收和develop合并结果：本轮没有相应执行证据，公开文档也未声称通过。

## 最终判断

Task6可进入下一道整分支审查。当前没有必须修复的源码或测试缺陷；1项Minor仅为既有测试日志噪声。独立整分支审查仍是模块交付门槛。


归档说明：报告引用的临时审查目录是执行时的位置。本地临时目录在收尾时清理，原始测试日志保留在本阶段lober虚拟机记录中；提交范围、源码树、命令、结果和本报告保留在Git文档中。
