# 第四阶段交付与独立审查

> 最新状态（北京时间2026-10-24）：已本地合入dev（eb43646），合并后完整VM检查与独立审查通过。见[dev集成记录](2026-10-24-ddd-phase4-dev-integration.md)。以下保留模块交付时的历史记录。

第四阶段六项任务、逐项独立审查和整分支审查均已完成。分支 `codex/ddd-media` 可交付，尚未合入 dev、推送或推进后续环境。

- 十一条网页入口通过镜像应用契约处理；来源、导入、删除与后台作业由应用层安排，文件与网络实现只提供明确能力。
- 本阶段准确删除154项临时许可，261→107；剩余65/42项属于后续两阶段，逐项未变。
- 另外单列修复了旧启停死锁和错误转换丢失独立恢复错误的问题，未把行为修复当成单纯搬迁。
- 最终整分支审查无严重或重要问题。唯一过时包注释已在271dc754修正，再经独立局部复审关闭。

## 验证及源码对应关系

完整VM验收树为 `7b41519dc07097f2c6880d77f95cbf129124edd3`，原命令退出0，UTC2026-10-23 20:44:45–20:51:15。普通和竞态各26包使用合法缓存、5包实际运行、6包无测试。原点名测试另按count=1执行，管理员八入口与实际后台组装通过。完整记录见[执行证据](2026-10-23-ddd-phase4-evidence.md)。

该树以后只改文档，以及doc.go一行普通包注释。doc.go去注释后两版均只有`package iso`，没有构建指令，其余非Markdown路径与完整被测树逐字相同。包注释在VM单独通过只读格式和差异检查；没有重复整套测试，也不声称最终文档提交曾逐字运行全套。

以下保留整分支固定候选及最后局部复审的原报告，最后列出根对每个未判断项的处理。所有连接内容、私有原日志继续忽略保留，不进入提交。

## 整分支独立报告

## 第四阶段独立整分支审查

结论：可以作为合入 dev 的候选。Critical 0，Important 0，Minor 1。唯一发现是已过时的迁移注释，不阻断合入。

### 审查范围和方法

- BASE：48f6c2c1f1ee4aebdd0349868a097891e5c9ad13。
- HEAD：01bb4f7d3f05c4837732b00b0252cd05ac23136a。
- 固定范围：21 个提交；review-48f6c2c..01bb4f7.diff，955501 字节。没有重新导出整份差异。
- 工作区：E:/src/lober/.worktree/ddd-foundation-task-creation。完整代码位置均相对此目录；表中 application/、infrastructure/、interfaces/ 简称省略共同的 internal/ 前缀，同一行裸文件名沿用该模块目录。
- O：.superpowers/sdd/2026-10-23-backend-ddd-phase4-media-jobs。安全日志与证明位置均相对此目录。
- 按 requesting-code-review/code-reviewer.md 模板独立审查。审查者未参与实现或六项任务门禁，没有派出其他审查者。

先读 whole-branch-review-context.md、constraints.md、批准计划、绑定四层设计、docs/contracts/media.md，以及 progress.md 的全部裁定、延期项和复审闭环。随后分段读固定包中的业务、机制、接线、边界检查和测试差异。重复验收文档按有关证据定位，没有把各任务通过作为整分支通过的理由。

包外只做以下聚焦核对：核对当前 HEAD 与被测树之间的文件名单；定位已审代码行号；读 delete.go 未变化的中段，确认本地原件与暂存目录重叠时仍受保护；读 application_rules_test.go 的模块识别，排除新子包绕过检查；读 remote_test.go 的证书拒绝设置，判断遗留日志性质。没有泛查其他计划、工作树或私有环境资料。

审查期间只读源码、Git 状态和安全证据。没有本机 Go、格式化、Wire、Node 或测试运行，没有重复已有虚拟机测试，没有合并、推送、发布或环境晋级。唯一写入是本报告。

### Strengths

1. 网页入口确实只剩参数和响应处理。Registry 统一身份、规则、审计及调用顺序。网页端不再持有旧镜像仓储和旧获取控制器。十一入口、五类角色、管理员八项有效写入都有直接测试。
2. 文件与数据库的交界有真实顺序证据。测试使用 PostgreSQL、实际文件和可控阻塞点，检查版本、字节、区间、审计及重启结果。它们不只是检查函数被调用过。
3. 生产接线共用同一组来源锁和登记闸门。Registry 接到 Control 与 Deletion；Control 接到真实 Supervisor。唤醒发生在成功保存以后，失败不唤醒。后台仍根据已保存的事实恢复工作。
4. 导入和删除保留了保守清理规则。提交结果未知时保留文件；删除等待导入及来源锁后再确认记录缺失；本地原件、其他拥有者及符号链接外目标有保护。
5. 四层职责已落实到代码和可执行检查。领域处理纯规则，应用决定顺序，基础设施处理字节与外部工具。精确删去 154 项迁移许可，没有新增、改写或改挂到别的阶段。

### Issues

#### Critical (Must Fix)

无。

#### Important (Should Fix)

无。

#### Minor (Nice to Have)

**M1：包说明还把已删除的旧 Sources 写成待迁移。**

- 位置：[internal/infrastructure/media/doc.go:6](E:/src/lober/.worktree/ddd-foundation-task-creation/internal/infrastructure/media/doc.go:6)。
- 触发：维护者阅读该包说明时，会看到“Sources 只暂留删除清理，下一项迁移”。最终树已删除旧 Sources，删除流程也已进入应用层。
- 影响：会误导后续维护者对迁移完成度和职责位置的判断。不会改变运行行为，也没有形成实际旧业务回流。
- 最小建议：删掉这句，或改为“获取与删除共用组装点注入的来源锁和登记闸门”。无需新增测试。

### 十一网页入口覆盖

以下各入口均核对了实现、公开契约及对应测试。身份检查在能力读取前执行。三项读取允许契约内五类角色，八项写入只允许管理员；不是仅验证“非管理员被拒绝”。

| 入口 | 实现位置 | 核对结论与测试证据 |
| --- | --- | --- |
| List | interfaces/console/isos.go:117；application/media/registry.go:23 | 过滤、分页、排序及返回字段保持；isos_test.go:233、media_contract_test.go:26。 |
| Get | interfaces/console/isos.go:134；application/media/registry.go:42 | 读取身份、缺失错误及结果转换保持；isos_test.go:233、registry_test.go:92。 |
| Create | interfaces/console/isos.go:145；application/media/registry.go:106 | 校验及空间判断先行；第二个来源失败仍返回已创建编号和 Location；isos_test.go:153 用真实库检查镜像、首来源及两条审计。 |
| Register | interfaces/console/isos.go:164；application/media/registry.go:151；sources.go:46 | 重新读取镜像，登记与排队沿用现有原子操作；queue_atomicity_test.go:51 检查失败回滚；删除竞争见下表。 |
| Progress | interfaces/console/isos.go:262；application/media/progress.go:11 | 使用同一次来源进度事实构造区间和计数，保留脱敏；isos_test.go:338、436 检查持久化及并发读取。 |
| Upload | interfaces/console/isos.go:250；application/media/upload_request.go:45；upload.go:10 | 范围头、实际字节数、重叠和重放规则保留；不增加逐片审计；isos_test.go:285、383、453、476 及 acquisition_order_test.go:102。 |
| Cancel | interfaces/console/isos.go:204；application/media/registry.go:177；sources.go:120 | 先保存取消事实，再取消活跃读取；不会被旧获取版本覆盖；acquisition_order_test.go:102 的三个受控时序。 |
| Retry | interfaces/console/isos.go:205；application/media/registry.go:186；retry.go | 保留已完成来源，本地原件不要求托管文件清理能力；acquisition_order_test.go:342、retry_error_test.go 及迁移后的上传测试。 |
| Reimport | interfaces/console/isos.go:206；application/media/registry.go:195；retry.go | 排队与审计先持久化，未在请求中提取文件；queue_atomicity_test.go:101、reimport_request_test.go:15 检查回滚和新实例恢复。 |
| Deprecate | interfaces/console/isos.go:186；application/media/registry.go:204 | 版本约束及审计保留；导入后续失败不能覆盖已废弃状态；isos_test.go:233、import_order_test.go:232。 |
| Delete | interfaces/console/isos.go:175；application/media/registry.go:216；deletion.go:40 | 审计删除先于字节清理；已提交后的清理失败仍返回删除成功并留日志重试；deletion_integration_test.go:16、deletion_order_test.go:348。 |

共同证据：internal/application/media/registry_test.go:105、204；internal/interfaces/console/isos_test.go:359；media_contract_test.go:26、57、98。错误优先级包含多错组合，二进制上传描述、必填范围头及错误响应也有明确断言。

### 五项 Review Focus

| 重点 | 跨模块核对结果 | 直接证据 |
| --- | --- | --- |
| 部分创建事实与 Location | Create 保存镜像后逐个登记；失败时仍带已创建对象。网页在处理该错误前写 Location。没有把部分成功伪装成全部回滚。 | registry.go:106；isos.go:145；isos_test.go:153；registry_test.go:124。 |
| 最后片、取消与重放 | 上传先完整暂存并检查重叠，再写字节和已确认区间。关闭文件的错误在区间保存后返回。发布前重新读状态，完成保存受版本约束；取消获胜后旧版本不能写成完成。已完成来源只在字节相同的重放时成功。 | upload.go:10；fetch.go:176；acquisition_order_test.go:102；upload_close_order_test.go:59；infrastructure/media/upload_test.go:61。测试检查文件内容、区间、版本和状态，不仅检查错误。 |
| 导入未知提交 | 发布后的成功确认要求 ready、同摘要、同制品映射。确认读取有独立的五秒上限。无法确认时保留制品；只有确认允许回滚时才撤销本次新树。其他已有目录不由该次回滚删除。 | import.go:148、176、186、191；import_review_test.go:124；import_order_test.go:206；import_publication_test.go:14。多源复制后重新核验见 import_order_test.go:135 和 import_review_test.go:42。 |
| 删除与补来源、清理保护 | 登记和删除快照共用闸门，双向竞争均覆盖。未知删除结果保留快照和字节。清理先确认缺失，再等导入锁、取消并等来源锁，随后第二次确认缺失。基础设施使用拥有者标记和受限根目录处理文件。 | deletion.go:40、117；sources.go:46；deletion_order_test.go:74、184、243；deletion_integration_test.go:16；infrastructure/media/delete.go:26、52、107；delete_test.go:118；deletion_journal_test.go:13。 |
| 后台启停、恢复、名额与退避 | Start 防重复；Close 先取消再等作业，停止后拒绝新名额。恢复扫描不把上传来源作为远程下载。工作异常释放名额后按原间隔退避，日志不输出异常中的用户内容。 | supervisor.go:62、87、98、137、145、175；lifecycle_test.go:15、46、63；supervision_test.go:28、64、75；cmd/lober/assembly_test.go:128。 |

### 边界、接线与计划裁定

- cmd/lober/app.go:103–119 构造一次 SourceLocks 和 RegistrationGate，供 Sources、Deletion 共用。Wire 的最终生成接线与之对应。app.go:152 把 Control、Deletion 注入 Registry；网页只接 Registry。实际组装测试启动后台完成获取和导入，再验证重启恢复。
- Control 的 Register、Upload、Retry、Reimport 只在调用返回成功后 Wake。Cancel 沿用不唤醒行为。control_persistence_test.go:17 在 Wake 内查询真实数据库，证明通知前事实已存在。没有新增对外事件或必达承诺。
- port/ISOs 和 port/Sources 保留六项、七项窄操作，继续调用 SaveSourceAndAcquire 等原子方法。没有任意事务回调，也没有把数据库事务对象交给应用。pgstore/contract_isosources_test.go:187 和 queue_atomicity_test.go 覆盖审计失败回滚、旧版本冲突及重启恢复。
- 领域层只计算识别、路径成分、状态及范围规则。删除校验例外只允许 repository.CheckID、CheckSource、ErrInvalid 这三个纯符号。应用中的 io/fs 只开放 ErrNotExist 与 SkipAll 两个值。边界测试验证函数、类型和相邻包不能借此进入。
- 格式读取、目录枚举、网络安全检查、文件复制、证书校验和外部工具仍归基础设施。MediaView 返回事实；应用选择制品及所需角色。为 Debian 任意套件枚举增加 Name/IsRegular，没有把文件句柄放入领域。
- 基础设施不能通过窄接口反查库；网页新文件不能通过本地 GetISO 接口或方法值绕过 Registry；组装只允许构造和启停。media_rules_test.go:49、88、116、197 包含正反样例。模块识别也拒绝未声明子包。
- root-exact-audit.json 给出初始 261 项、删除 154 项、剩余 107 项。剩余为后两阶段 65/42 项；四组删除量 24/125/5/0。精确清单、摘要及反向重建检查一致，没有新许可、改名挪表或旧流程永久化。

已独立判断各项裁定，而非把批准视作免审：

1. “operator 可写”和 TargetID 说法按实际契约纠正为管理员及 Audit.Detail，是正确修订；生产权限与审计目标未放宽。管理员八项有效写入已补齐输入、动作、时间和版本断言。
2. UploadSession.Size 表示已暂存字节；关闭与发布顺序和 BeginPrepare 先于 Debian 校验的顺序都有兼容目的及测试。未发现借接口调整改变原错误优先级。
3. Task3 多错误转换曾丢掉独立恢复错误，最终递归转换保留错误树。source_error_test.go 保留原错误身份及 errors.Is 断言，已闭环。测试上下文的同步修复保留了真实变异及失败断言，没有削弱测试。
4. Task5 Start/Close 死锁修复是单独披露的行为修复。Close 先取消使等待恢复的 Start 能退出，锁内关闭状态和增加作业计数避免关停竞态。生命周期测试直接复现，理由成立。
5. Task6 I1 的新网页文件本地接口逃逸已最小封堵。该场景、原场景和正常接线都有正反测试。计划七个检查文件的职责映射合理，没有要求为了文件数量重复造检查。
6. Task5 的早期双树证据没有被当作“当轮全绿”。普通和竞态通过、随后架构检查失败、修正后聚焦验证的边界均已披露。最终 Task6 完整命令重新执行，覆盖最终实现与检查代码。

未发现计划本身需要阻断本次合入的矛盾。保持单实例暂存目录、复用原有原子数据库操作及保留未知提交结果，符合本阶段保行为要求。

### 验证证据及其边界

最新安全记录为 vm-logs/t6-i1-final01.safe.log。固定被测树 7b41519dc07097f2c6880d77f95cbf129124edd3，UTC 2026-10-23 20:44:45–20:51:15，退出码 0。安全证明的 changes/untracked 为空。

当前 HEAD 比该树只多四份 Markdown：路线图、第四阶段计划、公开证据、Task6 复审报告。没有生产源码、测试或配置差异。因此该次验证覆盖本次 HEAD 的可执行内容；不是声称该 HEAD 字面上已在虚拟机重跑。

| 验证 | 实际证据 |
| --- | --- |
| 原 make check | 格式检查、vet、强制全模块发现、普通测试、原 race -timeout=20m、Linux amd64 构建、Wire 漂移检查均退出 0。 |
| 缓存与实跑 | 最新普通和竞态各有 26 包合法缓存、5 包实际运行、6 包无测试。架构检查实际 139.958/170.847 秒。不能把缓存记作这轮重跑。 |
| 较早实际全包运行 | t6-final-01.safe.log 保留整轮记录，其中网页竞态包实际 853.074 秒，小于原 20 分钟上限。不是靠缩短范围或改超时通过。 |
| 原 -count=1 聚焦 | 应用 33.703 秒、媒体机制 11.913 秒、网页 14.077 秒、数据库 12.983 秒。原 cmd 模式没有匹配用例，未算成实际业务覆盖。 |
| 补足真实匹配 | 实际装配 Test装配后台获取导入及启动恢复 通过；管理员八项有效写入全部通过；移动后的实物识别点名执行，但因输入缺失跳过。 |
| 契约与可选检查 | 网页契约类型检查通过；镜像选择的 official/internal-signed 两分支通过；bash 脱敏检查通过；差异空白检查通过。 |

没有发现降低口令强度、改变测试准备缓存、删断言、放宽竞态超时来换通过的变更。本次审查不重复执行这些套件。

### 遗留日志的最终分级

Task3/5/6 留下的两类日志已经分别判断，不继续作为“待定 Minor”挂起。

| 遗留项 | 证据及判断 | 根应处理内容 |
| --- | --- | --- |
| 预期证书握手错误 | t3-final-3.safe.log:280–282 显示 TestRemoteTLSPinnedAddressRetainsSNIAndVerification 故意拒绝不可信证书后 PASS。remote_test.go:17 的服务器把接受不可信证书视作失败，并检查服务器名称。最新组合日志 TLS 0 行。该日志是测试刺激的预期结果，未显示生产回归；不计为问题。 | 关闭该延期项为“已解释”。保留证据；不要放松证书验证以消除输出。 |
| Gin 调试输出 | 最新组合日志实际 76 行，包括调试模式警告与路由注册列表。阅读该段未见请求正文、凭据或异常用户内容。它影响日志简洁度，没有证据表明本迁移改变生产日志或泄露数据；不计为本分支缺陷。 | 关闭该延期项为“非阻断日志噪声”。如后续清理，应单独只调整测试日志，不通过更改认证、生产配置或断言去消音。 |

### Recommendations

- 顺手修正 M1 的包注释。它只需一行文档调整，不应引出新一轮全仓验证。
- 保留这份报告与最终被测树之间的映射。若之后改动生产代码、测试或接线，应针对实际变更补证据。
- 可以进入 dev 集成评审。该结论没有授予实际合并或发布权限，也没有替代合并后的组合测试。

### Declined to judge

以下每项都已考虑，但现有范围或证据不足以作通过判断。根需逐项保留、裁定或安排；不得在最终说明中默认为通过。

1. TestRecordCurrentRelease：安全日志明确跳过一次性发布记录。此操作不在迁移授权中。根应保留“未执行”，仅在单独发布授权下处理。
2. iso9660 的 TestRealISO：LOBER_TEST_REAL_ISO 未设置，实物读取兼容未验证。根应保留该跳过；需要该结论时提供实物输入并在虚拟机聚焦运行。
3. 应用层 TestIdentifyRealISO：另一实物输入未设置，移动后已点名但仍跳过。根应单列该跳过；不能用原目录的无匹配结果或模拟镜像替代实物识别结果。
4. 所有普通及竞态用例的隐式跳过总数：输出不是逐例详细模式，无法统计完整总数。根应写“总数未知”，不能把三项显式跳过写成全套只跳过三项；无需为计数重跑全套。
5. 完整外部工具和各发行版实物兼容矩阵：只有已具备输入和工具的用例能提供证据。根应保留未验证边界；确有发布需要时再列清工具、镜像和聚焦用例，在虚拟机执行。
6. 真实机器安装：没有实机安装结果，本阶段也未授权重装机器。根应把它留到对应验收阶段，不能从文件提取和脚本测试推导“实机通过”。
7. 企业测试套件：没有该阶段执行记录。根应在后续企业测试环境单独验收，不能把 make check 称作企业验收。
8. 浏览器人工体验：本次没有浏览器操作记录。根应按后续预览验收安排，不把网页接口测试写成浏览器验收。
9. 全量 Vue 类型检查及构建：此前额外尝试缺少依赖；只有契约类型检查通过。根应继续注明未完成全量前端检查。本分支无前端源码变化，不要求为本次迁移改动依赖环境。
10. 多进程共用一个暂存目录：现有契约明确只支持单运行实例，锁和闸门也在进程内。根应保留此运行限制；若要支持多实例，应另立需求和跨进程协调设计。
11. 私有环境操作和原始敏感日志的独立重建：依照约束没有读取凭据、私有连接文档或原始日志。只对安全证明中的树、时间、退出码及运行记录作判断。根应保留其已有环境核对责任，不声称审查者重新连接并重跑验证。
12. 与 dev 其他待集成功能合并后的组合行为：本报告固定到本分支 HEAD，未进行合并，也没有该组合树证据。根应在获得授权后按分支流程执行集成测试；此项不能由本分支通过自动推定。

### Assessment

**Ready to merge? Yes.**

十一入口、五项高风险顺序、真实接线、数据库原子审计和四层约束均有相互印证的代码与测试证据，未发现阻断本分支合入 dev 的功能或边界缺陷。剩余 M1 是注释修正；上述未验范围继续保留，不构成生产发布或后续环境晋级的通过结论。

## 唯一收尾修复的独立复审

**M1：包说明仍称旧 Sources 删除清理待迁移** — ADDRESSED。`internal/infrastructure/media/doc.go:6` 已替换为“删除快照与文件清理由本包提供能力，执行顺序和共享锁由应用层控制。”不再声称旧 Sources 尚待迁移。

#### Finding Verdicts

- **M1** — ADDRESSED；证据为固定差异包及当前 `internal/infrastructure/media/doc.go:6`。固定 BASE `01bb4f7d3f05c4837732b00b0252cd05ac23136a`，HEAD `271dc754ea711d9894f1c75fe7be253260338dff`。
- 检查：只读一次 `review-01bb4f7..271dc75.diff`；改动仅上述一行普通注释。原报告没有 Critical/Important 待修问题。
- 检查：读取修复报告、根的 `root-delivery-proof-271dc75.json` 和本轮虚拟机安全证明。其他非 Markdown 路径逐字相同；`doc.go` 除目标注释外逐字相同，去掉普通注释和空白后两版都只有 `package iso`，没有构建指令。未把它表述为所有非 Markdown 字节相同。
- 检查：`m1-comment-vm` 安全证明的树为 `79eb9c340b54029505b91201d23e8a83a37094b7`，开始和结束均为 `2026-10-23T21:09:46Z`，退出码 0；changes、untracked、safe.log 均为空。报告中的检查为该虚拟机上的 `gofmt -l`、`git diff --check`、`git diff --exit-code`，静默通过。
- 检查：本次未运行 Go、Node、格式化、生成或测试，未重导出 Git 差异，未读私有配置或原始日志。纯注释修正不要求重复原完整验证；原完整验证仍绑定树 `7b41519dc07097f2c6880d77f95cbf129124edd3`，未声称最终 HEAD 字面重跑全套。

#### New Breakage in the Fix Diff

None。这一行普通注释没有引入运行、构建或测试行为变化；新说明符合本轮确认的职责划分。

#### Out-of-Scope Observations

None。原报告的未判定项和遗留日志仍由根逐项处理，本次不扩大范围，也不将它们改判为通过。

#### Verdict

**Fix round:** All findings addressed, no new Critical/Important breakage — M1 已关闭，无待修发现。

## 根对十二项未判断范围的处理

| 原编号 | 处理与保留限制 |
| --- | --- |
| 1 | 一次性发布记录未执行。本次不包含发布授权。 |
| 2 | TestRealISO缺少实物输入，保留跳过，不宣称实物读取兼容已验证。 |
| 3 | TestIdentifyRealISO已在新位置点名，仍因缺少输入跳过，不用模拟数据替代实物结论。 |
| 4 | 普通和竞态的全部隐式跳过总数未知。三个显式跳过不是全套跳过总数，不为计数重复全套。 |
| 5 | 外部工具及发行版实物矩阵未完整验证，只采用现有具备输入的用例事实。 |
| 6 | 未执行真实机器安装，不能从文件和脚本检查推导实机通过。 |
| 7 | 未执行后续企业测试套件，本次make检查不替代该阶段。 |
| 8 | 未做浏览器人工验收，留在后续预览流程。 |
| 9 | 网页源码未变；契约类型检查通过，全量Vue检查/构建未完成，不为此安装或升级依赖。 |
| 10 | 仍只支持一个运行实例使用暂存目录；没有增加多进程共用能力。 |
| 11 | 根通过隔离帮助程序核实VM、真实测试库、私有文件权限及安全结果；独立审查者没有重连或读取原敏感日志。旧服务重启原因仍未知。 |
| 12 | 本分支尚未合入dev，没有组合树测试结论。实际合并及合并后VM测试需按用户后续授权执行。 |

上述均是如实保留的验收边界，没有把未执行项写成通过，也没有遗漏本阶段必需功能。既有证书拒绝日志已解释，Gin调试路由输出已判断为非阻断噪声，未改认证、生产设置或断言去消音。

模块分支、工作区及本阶段必要日志保留；未删除用户文件、其他工作区或前阶段证据。执行中的全部判断与误判代价见[判断清单](2026-10-23-ddd-phase4-rulings.md)。
