# 后端四层结构与模块契约收尾证据

状态：**源码、契约和本轮完整VM验收已由独立审查确认通过，严重0、重要0、轻微0待修。原六项及本轮新增W-I1、夹具M1、文档状态M2全部关闭。** 最后文档范围核对与提交随本次文档提交生效完成；在此候选冻结时没有此前已发生的最终文档提交。

## 对象与同源证据

第二轮功能提交为 `76ef20aadba11f3632e26f21dcf9acb883bfb737`，完整树为 `2b33cd37ca76ab7e9986b252711ad0590a62c6fb`。第一轮e74fbb3的命令与失败证据另保留，不能代表新提交。分支为 `codex/ddd-final-closure`。它包含Task1–7逐项独立审查后的提交；任务中用于固定字节的临时快照不是真实功能提交。

第二轮目录为 `/home/dev/lober-ddd-final-closure-20261025/acceptance-final-76ef20a`。新helper只允许本地六份授权文档候选有差异，索引为空，全部非文档物理字节逐一等于真实HEAD。bundle只含已审提交；VM是新的干净、分离检出，不覆盖旧目录，也不带入dev未提交网页修改。

运行前HEAD树、索引树相同，2152个跟踪文件物理字节与提交逐一相同。清单SHA-256为 `b2cc69a4482581b85ba92408ecf886dd1a411428a839c9a37841c446b2a41978`。实际选入46个Go包、580个源码、测试和嵌入输入；没有忽略活动源码。运行后完整清单与运行前逐项相同，提交、索引、2152个物理文件、46包/580个实际输入及生成类型均无差异；W/task8-r2-same-source-proof.json记录此结果。生成类型SHA-256为 `08dc80a1e7c472e907d8f710845ce09f5999c0ec792f2aa78776bd407405941c`。最终文档只含授权六份Markdown；2148份非文档和580实际输入仍等受测提交，369行原来源字节不变。最终候选以task8-final-docs-freeze.json及对应等价证明交最后文档核对。

只公开安全摘要。连接内容、凭据、原私人日志和完整本地证据保留在忽略目录 `.superpowers/sdd/2026-10-25-backend-ddd-final-closure`，不入库。下文以W指此目录。命令三件套为同名 `.command.json`、`.log`、`.exit`，分别保存原命令、未经删减的原输出和退出码。

## 原审计六项逐项复核

原审计固定旧提交的严重0、重要3、轻微3仍保留，见[原总审计](2026-10-25-ddd-final-audit.md)。下表给出本轮实现、实际验证和最终独立关闭结论；原审计的旧结论不回写。

| 原项 | 本轮改变与具体证据 | 最终状态 |
| --- | --- | --- |
| I1 机器执行链职责 | 纯状态、控制、冻结文件选择和日志资格归[领域执行](../../../internal/domain/execution)。[Gateway](../../../internal/application/execution/gateway.go)负责凭据、冻结渲染和推进；[日志入口](../../../internal/application/execution/installer_logs.go)读取任务、判断、追加和成功后通知。HTTP只解析和输出，Collector只管归属与字节。重点核对File观察在选择前、InstallerAnswer观察在Answer后、Signal观察失败仍独立处理。 | 关闭；最终独立审查通过 |
| I2 自动守卫漏拦 | [真实类型检查](../../../internal/archtest/interface_capabilities_test.go)接入原全模块发现及检查；[最终矩阵](../../../internal/archtest/final_closure_rules_test.go)覆盖新文件、子包、自建能力、未调用声明、方法值、函数字段、客户端、通知与状态。Task7初审发现的两项重要漏口另修复并复审，原首次失败保留。 | 关闭；最终独立审查通过 |
| I3 镜像取消类型 | [导入应用](../../../internal/application/media/import.go)三个额外来源错误分支先识别取消/截止并原样返回；普通内容错误仍按原分类。真实父取消后用未取消上下文读库核对状态，合成能力取消也核对整条镜像和来源；会话关闭一次。 | 关闭；最终独立审查通过 |
| M1 方法旁约定 | 方法旁连接唯一来源；[369项能力导航](../../contracts/application-capabilities.md)逐项列八项来源、实际调用者和测试。构造、纯值、只读、写入、字节会话分别说明“不适用”的原因，不能用一个共同句掩盖事务或取消差异。 | 关闭；最终独立审查通过 |
| M2 分页文字 | [镜像约定](../../contracts/media.md)写明游标默认100，页码默认第1页/每页20，最大500；行为未改。 | 关闭；最终独立审查通过 |
| M3 模板双规则 | 删除SQL中的准入重复实现，仓储取得模板及精确组件后调用唯一[TemplateEligible](../../../internal/domain/selection/template.go)。[候选读取](../../../internal/infrastructure/persistence/pgstore/template_selection.go)只为本次读取使用只读REPEATABLE READ；游标、页码与计数保持各自原顺序。 | 关闭；最终独立审查通过 |

I3是错误行为纠正；Task6是单次读取一致性收紧。二者分别提交，不能称全部只是搬目录。其余网页、机器协议、冻结数据、错误优先级、数据库版本/锁、审计原子范围及通知位置按原约定保留。

## 369项、八项来源的语义复核

沿用Task6已接受的 `task6-inventory.json` 和 `task6-accepted-anchor-baseline.json`。369行对应2952个来源单元；647个源码锚点文字与接受基线相同。W/task8-r2-contract-trace.json逐行保存新提交的方法、声明、八项各自来源、调用及验证来源；369行这些单元与第一轮逐字相同。另补3个正式守卫验证坐标，W/task8-r2-navigation-check1.json记录5501个相对链接、369/369行及零坐标错误；旧647个接受锚点文字未变。这些工具只核坐标和字节，不能证明行为。

本次按下表逐组读方法说明、其明确委托的原方法/文档，以及实现和测试断言。继承口沿明确委托追到原方法，不因“接口已登记”就自动接受。普通记录字段随消费方法追溯，构造和纯值也保留八项不适用原因。最终独立者另核全模块语义。

| 范围 | 行数 | 实际核对的语义和边界 |
| --- | ---: | --- |
| tasks及其消费口 | 16+21 | 可信权限先于输入；候选不是提交保证。只读无审计/原子写。Submit原版本与当前选择在库内复查、整批审计、成功后逐项提示。控制提交后补读可失败；事件先订阅后首读，200条、5秒补读、原令牌复查及退订。 |
| authoring及其消费口 | 37+38 | 13组件、14模板、5编辑方法分别沿真实路由；草稿整份替换，版本号不重发，审批复查精确引用。补读失败不撤销已提交写入。纯目录、原配置、冻结预览和正式渲染不新增事务或装机格式。 |
| hosts及其消费口 | 6+7 | 普通列表保留原文前缀；单条编号规范化。CSV先完整解析、再一次读库、再判断预演/拒绝，正式整批保存和审计；错误与结果保持输入顺序，无自动重试或通知。 |
| media及其消费口 | 11+66 | 网页身份与可信后台区分；来源各自提交，文件和数据库无共同事务。会话关闭、恢复、部分发布及未知提交各有不同边界；不能统称可重复。取消类型纠正不新增检查位置。登记闸门与来源锁只保护本实例。 |
| identity及其消费口 | 31+10 | 请求身份不能伪造；散列强度、假校验、锁外计算与锁内复读保持。审计和账号共同提交后才清会话。角色变化与跨实例立即撤权不扩展；菜单显示不授接口权限。 |
| audit及其消费口 | 2+1 | 权限先于范围/分页；只读不审计、不通知、不保证跨页快照；导出500条逐页，后页错误/取消不给半份文件。 |
| 共同能力 | 8 | Actor是快照判断；Clock不承诺单调；AuditIntent是值。通知容量一可合并，取消订阅可重复。选择锁等待不立即取消；FrozenPreview只用已冻结输入。 |
| 原仓储 | 46 | 只接受可信应用；非nil空集合、原检查顺序和原Rev。审计与业务同事务，返回时已提交，仓储不通知。普通写保持READ COMMITTED；查询不承诺多次读取快照。 |
| execution及其消费口 | 42+6 | 令牌按Task/Attempt/IssuedAt/范围绑定；Boot先提交再渲染。File/InstallerAnswer/Signal各自观察、凭据次数、错误来源与正文抑制逐条核对。日志先任务事实、领域资格、追加、成功提示，保留读取后追加的窗口。 |
| 纯执行规则 | 20 | 输入与显式秒数，拒绝不改原任务；执行记录与Signals复制。精确重复判断早于终态和期限；重启后旧安装信号例外保持。纯计算不读时钟、存储或外部，不自行审计/通知。 |
| 模板资格 | 1 | 只复用CurrentBuiltin与TemplateUsable，不重做激活、不替调用者选版本。ISO/发布清单是应用预先取得的输入，不许扩成同刻全局快照。 |

新增的标准库文件读取分类及两个正式矩阵属于自动守卫验证，没有新增业务能力，369行行为来源不变。原74格逐项保留；另有55种同型入口×3类代表调用方共165格，以及纯处理正例。两条正式路径的实际PASS必须来自本轮详细全包输出，不能引用第一轮私有失败来替代。

四张过渡表仍逐表0；39/68/154精确删除历史和六项正式编号能力按原守卫保留，没有新临时放行、扩大包许可、减小扫描范围或删除旧反例。实际本轮点名结果随详细测试清单补齐。

## VM完整命令及结果

第二轮已实际确认主机lober、用户dev，原工具和资源可用；无其他Go/make/测试进程争用。真实数据库为lober_test、非恢复；连接文件忽略且0600。W/task8-r2-preflight1与task8-r2-manifest-before1均退出0，清缓存命令也退出0。后续原命令现已全部串行完成并退出0。

固定设置为：

```sh
GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off
LOBER_TEST_BASH=/bin/bash LOBER_TEST_BUSYBOX=/usr/bin/busybox
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0
```

串行原命令如下，完整竞态仍是 `go test -race -timeout=20m ./...`。没有改变密码强度、会话缓存、超时、断言或原archtest内部探针。

```sh
go clean -testcache
make check
pnpm --dir web check:console-types
go test -count=1 -v ./...
git diff --check
git diff --exit-code
git diff --cached --exit-code
```

### 第二轮新真实提交的最终VM结果

实际命令索引为W/task8-r2-command-index.json，每条均核对原日志首部HEAD为76ef20a。不是旧快照或旧make的复用。

| 原命令 | 本轮三件套名称 | 退出码 | UTC开始→结束 |
| --- | --- | ---: | --- |
| `go clean -testcache` | task8-r2-clean1 | 0 | 2026-10-25T07:10:24Z → 2026-10-25T07:10:24Z |
| `make check` | task8-r2-make1 | 0 | 2026-10-25T07:10:25Z → 2026-10-25T07:46:47Z |
| `pnpm --dir web check:console-types` | task8-r2-console-types1 | 0 | 2026-10-25T07:46:48Z → 2026-10-25T07:46:51Z |
| `go test -count=1 -v ./...` | task8-r2-full-v1 | 0 | 2026-10-25T07:46:51Z → 2026-10-25T07:55:07Z |
| `git diff --check` | task8-r2-diff-check1 | 0 | 2026-10-25T07:55:08Z → 2026-10-25T07:55:08Z |
| `git diff --exit-code` | task8-r2-diff-working1 | 0 | 2026-10-25T07:55:09Z → 2026-10-25T07:55:09Z |
| `git diff --cached --exit-code` | task8-r2-diff-index1 | 0 | 2026-10-25T07:55:09Z → 2026-10-25T07:55:09Z |

make耗时2182秒。格式、vet、原独立全模块检查、普通测试、原20m竞态、Linux amd64构建和Wire漂移均完成。类型生成检查只核后端导出的网页类型一致性，不代表完整网页构建或人工验收。

| 本轮实际检查 | 通过包 | 无测试文件包 | 缓存包 | 明确Skip |
| --- | ---: | ---: | ---: | --- |
| make普通测试 | 38 | 8 | 0 | 非详细输出，隐式总数未知 |
| make原20m竞态 | 38 | 8 | 0 | 非详细输出，隐式总数未知 |
| 全包详细测试 | 38 | 8 | 0 | 3，逐项见下 |

详细输出实际1228个顶层、4384个子项PASS，0实际FAIL。原架构测试通过t.Log记录的两个预期child FAIL完整保留，没有误计成当前进程失败。130个去重重点、134个包内定义全部有真实PASS及原日志行号，清单在文末；没有数据库Skip。

| 明确跳过的测试 | 本轮原因与不代表的范围 |
| --- | --- |
| TestIdentifyRealISO | LOBER_TEST_REAL_ISO未启用；没有验证真实安装介质识别。 |
| TestRealISO | 同一真实介质开关未启用；没有验证真实ISO内容遍历。 |
| TestRecordCurrentRelease | LOBER_RECORD_RELEASE未设为1；一次性冻结记录没有执行，不新增历史样本。 |

两个审计问题的正式回归现在实际PASS：镜像额外来源OpenSource及两处读取边界的取消/超时/包装与真实父取消，仍保持完整状态并关闭资源；守卫拒绝原同一非法输入。唯一源码波的两个正式矩阵在这次full-v中分别为原74/新增165全部PASS，类型路径另有50纯处理正例PASS，均0Skip/FAIL。full-v实际打印的原74输入JSON与已接受审查输入逐项相同，证据W/task8-r2-source-wave-matrix-proof.json。

以下第一轮过程按原样保留，用于解释为什么先前make通过仍需修复。

### 第一轮历史检查与控制屏障

第一轮真实候选e74fbb3的 `go clean -testcache` 与 `make check` 均退出0。make于UTC 2026-10-25T05:29:15Z至06:06:18Z完成，共2223秒；格式、vet、原全模块门禁、普通与竞态、Linux amd64构建、Wire漂移检查完成。

| 本轮实际检查 | 通过包 | 无测试文件包 | 缓存包 | Skip |
| --- | ---: | ---: | ---: | --- |
| make普通测试 | 38 | 8 | 0 | 非详细输出，隐式数未知 |
| make原20m竞态 | 38 | 8 | 0 | 非详细输出，隐式数未知 |

当时尚未完成全部验收。独立审查发现新疑点后，原make自然完成；后续命令前有明确控制屏障。`task8-console-types1` 标为NOT_EXECUTED/CONTROL_ONLY，没有执行、没有退出码，不计测试结果。当时正式types、全包详细测试和三个差异命令暂停；后续已在第二轮新提交重新按原链执行，结果见上，旧make不计为新提交通过。

原始输出按命令结束后落盘，进度只用只读进程观察，不把缓冲输出称为实时日志。当时尚无详细顶层/子项、明确Skip及重点PASS；本次现已由第二轮真实输出补齐，没有沿用旧审计数字。旧105个重点中104个函数原名保留；原 `TestSyslogRejectsOldAttemptsAndBoundedPackets` 的旧尝试/4096字节/终态断言迁至 `Test日志原旧尝试终态与受限包断言`。最终按实际名称核对，没有伪造旧名PASS；原128项加两个正式矩阵，现为130名/134处实际执行。

### 整分支独立反例

独立审查者提供74格标准库读取矩阵：机器io/fs、embed，以及console/子包/machine和全部应用/port中的filepath.Glob/Walk、zip.OpenReader、text/template.ParseFiles。函数只在未调用声明中，探针不读取这些业务路径。

另建 `/home/dev/lober-ddd-final-closure-20261025/acceptance-review-readers-probe1`，从同一真实e74fbb3提交检出，只增加一份审查者测试，没有改已跟踪生产内容。先确认无Go/make/test残留，再串行运行：

```sh
go test -count=1 -v -run '^(TestWholeBranchStandardFileReadersMustBeRejected|TestWholeBranchFileReadersReachActualModuleGuard)$' ./internal/archtest
```

首次及同代码、同VM、同设置原样复跑均退出1，结果一致：2顶层失败、116子项失败、32子项通过、0Skip。每组74格中58格漏拦，16格已被旧精确规则拒绝；没有把这些16格也称缺口。单夹具的58项发现为空；真实模块先通过合法基线，再完成实际包发现和类型装载，输出明确 `actual module types loaded before rejection assertions`。真实模块58个失败文件没有任何同文件违规位置，失败不来自编译、加载或只比错规则名。

证据为W/task8-readers-probe1、task8-readers-probe1-repeat三件套及摘要；逐文件核对在task8-readers-probe-file-findings.json。此结果证明第一轮套件没有覆盖这一新漏口；没有证实当前生产代码已使用这些读取路径。上述首次失败事实保留。根随后只开一次源码修复波；只改8个archtest文件，不改生产。新增标准库来源分类、原74格与165格正式反例，并把authoring/hosts相同的旧桥夹具提为一个按需生成器。原条件/声明字节与旧断言保留。源码波的集中测试、完整arch和独立门禁通过；同一总审查者范围复审关闭W-I1和重复夹具M1，无本波新增严重/重要/轻微。它们只确认本波范围，不能替代新提交的完整验收。本波还保留首轮集中失败及同码复跑：新增ReadFileFS三类调用方和实际模块共6处位置预期先匹配了类型名，真实方法位置已被拒绝。最终只把marker改为`ReadFile}`，原74格及另7份源码不变，没有把这个坐标错误算成有效漏拦。

## 全程裁定、理由与错误代价

明确裁定以W/task8-ledger-ruling-index-v2.json的19条为准。旧16条索引原件保留，其中129、131是过程元数据，下面明确标为历史过程，不冒充新裁定。159号裁定含8项连续编号处置，逐项展开。它们保留失败与改判，不能把后续通过倒写成从未失败。

| 记录 | 决定 | 理由与若判断错误的代价 |
| --- | --- | --- |
| 39 工作区工具 | 原生工具误报ref不可用，核实Git提交、分支和目标不存在后，用Git在忽略.worktree建立独立分支。 | 保留dev用户修改和所有旧目录。若目标核错，可能覆盖工作；先核精确路径与Git身份。 |
| 41 文本准备 | 无Git Bash，本机仅用Python生成任务brief及证据目录，不运行WSL测试。 | 避免混入第二测试环境。拆段错会漏要求，因此按八个任务标题精确提取并附全部共同约束。 |
| 43 基线顺序 | 先跑原完整VM基线；Task1普通注释可并行，Task2不能抢VM。 | 先保留旧源码的真实基准，成本是一轮完整基线时间。 |
| 60 审查与提交顺序 | 以已确认计划为准，先冻结未提交候选和每文件摘要，再独立审查，通过后提交。 | 与模板先提交后审查的顺序不同。若把working-tree冒充已提交会失去身份，故逐轮标真实Head与候选字节。 |
| 72 Task2补观察 | 已启动的原三包回归继续；之后只加点名详细旧取消测试，不重复整包。 | 非详细输出看不到该项是否真跑。代价是一次定点读取；隐式Skip仍未知，不从包通过推导零跳过。 |
| 76 Task2坐标 | 6个import.go调用链接随Task3已有导航维护一并修。 | 只是行号变化，不改行为；若未修会指错调用，最终整分支须复核关闭。 |
| 80 Task3恢复 | 原实施者达到用量限制，续接者先修草稿中文编码，再走原黄金/有效失败/修复顺序。 | 两草稿是CP936，原件和摘要保留；编码错误不是规则失败证据。不重置Task1/2，不丢旧字节。 |
| 83 原谓词 | CanBoot保留pending/booting，BootDeliverable保留非终态，Preseed保留booting/installing，InstallerAnswer只判断原格式。 | 不能合并成一个更严格判断，否则异常任务的返回和错误次序会变。Task3不提前宣布Task4/5接线完成。 |
| 91 Task4所有权 | machine构造改依赖Gateway时，允许必要的machine规则测试适配精确execution导入。 | 只限准确包对；扩大为全部application/port会留下旁路，旧仓储许可留给Task7正式清除。 |
| 95 Signal计时 | cmd继续测公开Gateway.Signal整个调用，诊断时长现在含之前HTTP内的地址观察。 | 为复刻旧跨度加应用时钟/回调会增加职责。必须公开此诊断差异；若存在未见消费者会影响阈值判断，由独立审查判级。 |
| 97 仅文档换行 | 两份Markdown统一LF，证明其他字节相同；不为此重跑Go/Wire/Node。已启动的第二回归继续完成。 | 无构建输入变化；证明错才可能漏测，所以保存前后清单和换行归一等价证据。 |
| 101 Task4表格 | 两个能力表分组空行随Task5导航维护修复。Signal跨度被逐项审查接受为非阻断差异。 | 表格不连续会影响阅读，不影响代码；最终核对不能漏掉。计时接受仅限已见契约/消费者范围。 |
| 117 Task6取消 | 原完整回归确有取消错误竞态；同码复跑PASS不能抹掉。只对新私有查询的ErrTxDone且ctx已取消映射ctx.Err。 | database/sql可先自动回滚，GORM随后提交只返事务已结束。扩大映射会吞真实错误，提交成功后查取消又会把成功改成失败，二者均未做。 |
| 127 Task7范围 | 全应用/port禁止外部操作及随机；新增时钟限制只收execution及port，identity/audit原更严限制不变。 | 不能误改media重试时钟、5秒未知提交补读或原两项fs错误值许可；误判会改变既有恢复行为。 |
| 129 历史过程：Task7首轮修复 | 保留green2完整失败和同码复跑；恢复authoring原纯Render许可，按真类型识别DTO，对相同规则/对象/位置去重。 | 单看方法名会误拦无关Cancel；重复发现会破坏旧精确len=2断言。没有改原断言、生产、扫描或加宽豁免。 |
| 131 历史过程：Task7重验 | green3重点7顶层/154子项、0Skip通过后，继续最终全arch与独立全模块门禁。 | 重点通过只解释已修问题，不能替代原完整范围；本条没有额外范围裁定。 |


| 新增裁定 | 决定 | 理由与若判断错误的代价 |
| --- | --- | --- |
| 153 第6份文档 | backend-modules末段只改已落地状态，并指上方唯一详细规则。 | 原末段与当前实现相冲突。若写过头会夸大一致性，因此保留跨页、输入ISO及普通写边界，最后独立核对。 |
| 159.1 Signal计时 | 接受Gateway含观察的诊断范围，公开披露。 | 未见精确旧跨度承诺或消费者；若判断错，耗时消费者可能误读，需调整观测契约。 |
| 159.2 旧Gin告警 | 作为既有非阻塞记录，不改设置静音。 | 本轮未引入，未见凭据泄露；若判断错，需单独排查真实泄露或运行影响，不称告警已消失。 |
| 159.3 日志读后追加 | 按批准兼容范围保留窗口。 | 本计划不新增Attempt原子复查；若未来要拒绝迟到日志，须另设计一起保存的边界和版本复查。 |
| 159.4 候选全扫 | 接受精确总数的全量扫描和长只读快照。 | Task6已明确成本，不承诺秒数或实测峰值内存；生产规模过大时另做负载测试和优化。 |
| 159.5 跨实例删除闸门 | 仍未确认，不认定已修或验证通过。 | 部署承诺和跨实例用例未定；若部署需要此保证，须补契约与测试，当前结果不能代替。 |
| 159.6 新能力范围 | 本轮不新增跨实例立即撤权、持久会话、消息必达和真实装机能力。 | 已确认计划明确排除；若需求包含它们，另立契约与计划，避免把现有结果扩大为新保证。 |
| 159.7 其他验收 | 保留原dev网页文件；真实ISO/实机、完整前端、企业及后续环境另做。 | 本轮工具、开关及授权不覆盖；若扩大结论，用户会误认为对应场景已验收。 |
| 159.8 Skip及计数 | 等真实输出，不提前写0；非详细隐藏项仍未知。 | 包通过不能证明每个库用例都跑了；真实数据库不能跳过后称通过。 |
| 175 唯一源码修复波 | 原Task7实施者集中修W-I1及重复夹具，同一总审查者只复审该波。 | 58种空发现是承重缺口；代价是新增集中/完整arch、范围复审、真实提交后的全部原验收，不能复用e74的make0。 |
| 187 控制屏障 | 第一轮占位只记NOT_EXECUTED/CONTROL_ONLY，利用write-once前置断言停住后续。 | 保留make自然结束和原输出；分类错可能把占位当结果，所以没有command/exit，真实命令使用新名字。 |
| 189 有文档候选的检出 | 本地只容许精确6docs差异、索引空、非docs全字节等HEAD；VM仍真实提交干净检出。 | 保留草稿原位。名单或比对错会漏未审改动，因此精确六路径和逐文件字节核对，并用VM提交/实际输入前后清单约束。 |

## 边界、未验与疑虑

Task4的machine.signal诊断计时包含地址观察；业务时钟、返回、字段及观察位置保持。Task6页码为得到精确总数需读完候选与引用，时间成本随数据量增长；内存只保留当前批及目标页，不声称性能提高。只读REPEATABLE READ只覆盖单次私有候选调用，不扩成跨页、跨请求、ISO/发布清单同刻事实，也不改变普通写事务。

新增标准库读取矩阵只证明本轮列出的实际来源与形态，不声称穷尽未来标准库或所有动态外部实现。

日志保留任务读取后追加的窗口，不增加原子Attempt复查。地址租约重启会丢失，通知可合并，UDP不保证必达。未确认的跨实例删除闸门风险没有在本轮验证、修复或标成通过。

真实ISO两个自选用例和一次性记录明确Skip，理由已逐项列出。真实机器安装、企业套件、完整前端构建和浏览器人工验收不由后端Go检查代表。未推送、合入dev或晋级test/preview/main，也没有清理任何分支、工作区或VM目录。

本轮详细原输出仍可见40条Gin调试告警及既有路由列表；只公开计数，不复制原私人日志。按159.2保留为既有非阻塞观察，没有改设置消音。原authoring_rules_test.go:23、hosts_rules_test.go:17重复夹具已由唯一源码波抽取共同生成器，并经范围复审关闭。backend末段旧状态已最小修正，最终独立证据/契约核对已确认关闭。源码、契约与本轮验收无待修严重/重要/轻微项；最后仍按同一审查者的文档范围核对及根提交流程完成文档收尾。

## 本轮130个点名、134处实际PASS

下列行号均指W/task8-r2-full-v1.log的原输出；正式源码链接用于定位测试定义。130个去重名称对应134个定义，四个同名测试在应用和网页两处均实际PASS。原105旧重点中104个名称保留，另1个只按明确迁移后的实际名称列PASS。原计划28个去重重点加唯一源码波两个正式矩阵，与旧重点合并后为130名；没有把未运行的旧名写PASS。

| 正式测试定义 | 实际结果 | 原输出PASS行 |
| --- | --- | --- |
| [TestBootServesPreseedAnswerToDebianInstallerAgent](../../../internal/application/execution/gateway_test.go#L139) | PASS | L2705 |
| [TestBoundedBinarySignalAndStrictQuery](../../../internal/interfaces/machine/http_test.go#L56) | PASS | L16681 |
| [TestCanonicalFrozenFileRouting](../../../internal/application/execution/gateway_test.go#L98) | PASS | L2701 |
| [TestCredentialsBindTaskAttemptScopeAndExpiry](../../../internal/application/execution/token_test.go#L13) | PASS | L2673 |
| [TestExecutionAuditFailureRollsBackRuntimeStateAndEvents](../../../internal/infrastructure/persistence/pgstore/execution_test.go#L42) | PASS | L13890 |
| [TestISOSources](../../../internal/infrastructure/persistence/pgstore/contract_isosources_test.go#L15) | PASS | L13340 |
| [TestIncrementalGateRejectsPackageInExistingRoot](../../../internal/archtest/cache_test.go#L14) | PASS | L4255 |
| [TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol](../../../cmd/lober/machine_delivery_test.go#L28) | PASS | L1316 |
| [TestNumberedTemplateEligibilityBeforePagination](../../../internal/infrastructure/persistence/pgstore/pagination_test.go#L179) | PASS | L13900 |
| [TestNumberedWizardListsAndEligibleTemplateTotals](../../../internal/interfaces/console/selection_pagination_test.go#L12) | PASS | L16507 |
| [TestProtocolRequiresFrozenHooksAndExactDuplicateReports](../../../internal/domain/execution/protocol_test.go#L45) | PASS | L10313 |
| [TestProtocolTracksInstallRebootInitializationAndDone](../../../internal/domain/execution/protocol_test.go#L71) | PASS | L10315 |
| [TestSourceAuditRollback](../../../internal/infrastructure/persistence/pgstore/contract_isosources_test.go#L104) | PASS | L13342 |
| [TestSyslogNeedsAuthenticatedIPLeaseAndRedactsCredentials](../../../internal/infrastructure/events/syslog_test.go#L33) | PASS | L12354 |
| 旧`TestSyslogRejectsOldAttemptsAndBoundedPackets` → [Test日志原旧尝试终态与受限包断言](../../../internal/application/execution/installer_logs_test.go#L131) | PASS | L2791 |
| [TestTimeoutRestartRetryRevokesOldCredentialAndAuditIsAtomic](../../../internal/application/execution/service_test.go#L208) | PASS | L2807 |
| [TestWholeBranchFileReadersReachActualModuleGuard](../../../internal/archtest/standard_file_operations_test.go#L211) | PASS | L8323 |
| [TestWholeBranchStandardFileReadersMustBeRejected](../../../internal/archtest/standard_file_operations_test.go#L141) | PASS | L7553 |
| [Test主机六入口契约](../../../internal/interfaces/console/hosts_contract_test.go#L54) | PASS | L15999 |
| [Test主机列表保留两种分页与原文过滤](../../../internal/interfaces/console/hosts_contract_test.go#L180) | PASS | L16008 |
| [Test主机变更与向导提交按数据库事实交错](../../../internal/interfaces/console/hosts_contract_test.go#L503) | PASS | L16026 |
| [Test主机导入审计取消整批回滚](../../../internal/interfaces/console/hosts_contract_test.go#L610) | PASS | L16034 |
| [Test主机导入预演后覆盖最新版本](../../../internal/interfaces/console/hosts_contract_test.go#L407) | PASS | L16018 |
| [Test主机输入多错保持原顺序](../../../internal/interfaces/console/hosts_contract_test.go#L152) | PASS | L16006 |
| [Test事件订阅保留字段副本与上下文](../../../internal/application/tasks/events_test.go#L91) | PASS | L3678 |
| [Test事件订阅先订阅再首读且失败退订](../../../internal/application/tasks/events_test.go#L67) | PASS | L3676 |
| [Test事件订阅并发重复关闭](../../../internal/application/tasks/events_test.go#L174) | PASS | L3684 |
| [Test任务SSE精确分页空页复查与所有退出退订](../../../internal/interfaces/console/tasks_test.go#L769) | PASS | L16573 |
| [Test任务SSE补读分页丢通知续读及断开退订](../../../internal/interfaces/console/tasks_test.go#L294) | PASS | L16537 |
| [Test任务SSE退出与到期只检查原令牌](../../../internal/interfaces/console/tasks_test.go#L525) | PASS | L16555 |
| [Test任务SSE首读失败无流头且坏序号先拒绝](../../../internal/interfaces/console/tasks_test.go#L750) | PASS | L16563 |
| [Test任务事件提供者复用原仓储与通知实例](../../../cmd/lober/assembly_test.go#L704) | PASS | L659 |
| [Test任务事件直接查询不额外查任务](../../../internal/application/tasks/events_test.go#L54) | PASS | L3674 |
| [Test任务列表保留分页顺序与诊断](../../../internal/application/tasks/reader_test.go#L123) | PASS | L3704 |
| [Test任务列表详情事件及查询边界](../../../internal/interfaces/console/tasks_test.go#L51) | PASS | L16531 |
| [Test任务控制不存在先于不可用](../../../internal/application/tasks/controller_test.go#L88) | PASS | L3662 |
| [Test任务控制后端失败不重读不补发](../../../internal/application/tasks/controller_test.go#L150) | PASS | L3668 |
| [Test任务控制提交后读取失败不额外通知](../../../internal/application/tasks/controller_test.go#L125) | PASS | L3666 |
| [Test任务控制显式共用后端时钟和通知](../../../cmd/lober/assembly_test.go#L660) | PASS | L655 |
| [Test任务控制未启用明确不可用](../../../cmd/lober/assembly_test.go#L678) | PASS | L657 |
| [Test任务提交与独立实例审批的契约](../../../internal/interfaces/console/wizard_test.go#L561) | PASS | L16669 |
| [Test任务查询不能绕过权限](../../../internal/application/tasks/reader_test.go#L85) | PASS | L3702 |
| [Test任务查询保留错误先后](../../../internal/application/tasks/reader_test.go#L53) | PASS | L3693 |
| [Test任务终态规则接入真实全模块扫描](../../../internal/archtest/task_finalize_rules_test.go#L813) | PASS | L9210 |
| [Test任务读取和预览显式组装](../../../cmd/lober/assembly_test.go#L566) | PASS | L653 |
| [Test任务预览仅传冻结快照与样例标签](../../../internal/application/tasks/reader_test.go#L164) | PASS | L3711 |
| `Test任务预览只使用冻结快照` [应用](../../../internal/application/authoring/renderer_test.go#L34) / [网页](../../../internal/interfaces/console/tasks_test.go#L464) | PASS × 2 | L2104、L16551 |
| [Test全模块依赖守卫](../../../internal/archtest/deps_test.go#L225) | PASS | L4570 |
| [Test内置选择拒绝旧发布但不改变冻结任务](../../../internal/interfaces/console/builtin_selection_test.go#L15) | PASS | L15896 |
| [Test冻结组件重复摘要后续损坏仍拒绝](../../../internal/domain/execution/delivery_test.go#L16) | PASS | L10305 |
| [Test删除和补来源按两种先后次序保护真实文件](../../../internal/application/media/deletion_order_test.go#L74) | PASS | L3312 |
| [Test向导五路权限及空批非法字段](../../../internal/interfaces/console/wizard_test.go#L325) | PASS | L16641 |
| [Test向导候选分页准备状态与缺失集合](../../../internal/interfaces/console/wizard_test.go#L251) | PASS | L16637 |
| [Test向导参数输入按规范SN定位并拒绝未知行](../../../internal/interfaces/console/wizard_test.go#L355) | PASS | L16643 |
| [Test向导参数输入顺序不串主机](../../../internal/interfaces/console/wizard_test.go#L383) | PASS | L16645 |
| [Test向导当前已审批版本不会被新草稿取代](../../../internal/interfaces/console/wizard_test.go#L222) | PASS | L16635 |
| [Test启动组装只委托两个初始化入口](../../../internal/archtest/authoring_rules_test.go#L110) | PASS | L4039 |
| [Test基础设施拒绝接口层和应用具体服务](../../../internal/archtest/four_layer_test.go#L9) | PASS | L5733 |
| [Test契约ISO来源排队按版本一次提交](../../../internal/infrastructure/persistence/pgstore/contract_isosources_test.go#L187) | PASS | L13346 |
| [Test契约事件](../../../internal/infrastructure/persistence/pgstore/contract_events_test.go#L16) | PASS | L12870 |
| [Test契约任务](../../../internal/infrastructure/persistence/pgstore/contract_tasks_test.go#L24) | PASS | L13507 |
| [Test契约并发](../../../internal/infrastructure/persistence/pgstore/contract_concurrency_test.go#L24) | PASS | L12810 |
| [Test契约用户](../../../internal/infrastructure/persistence/pgstore/contract_users_test.go#L21) | PASS | L13872 |
| [Test契约角色菜单](../../../internal/infrastructure/persistence/pgstore/contract_role_menus_test.go#L14) | PASS | L13348 |
| [Test审批模板等引用的hook版本提交之后再判定](../../../internal/infrastructure/persistence/pgstore/pgstore_test.go#L484) | PASS | L13958 |
| [Test审计分页过滤完整CSV及公式作为文本](../../../internal/interfaces/console/audit_test.go#L277) | PASS | L14401 |
| [Test审计导出后页失败不发送残缺下载](../../../internal/interfaces/console/audit_test.go#L40) | PASS | L14237 |
| [Test审计意图桥接保留真实保存与冲突](../../../internal/application/execution/control_intent_test.go#L65) | PASS | L2699 |
| `Test并发改密不能让旧口令登录补发会话` [应用](../../../internal/application/identity/password_test.go#L115) / [网页](../../../internal/interfaces/console/auth_concurrency_test.go#L34) | PASS × 2 | L3129、L14725 |
| `Test并发本人改密不能覆盖管理员重置` [应用](../../../internal/application/identity/password_test.go#L172) / [网页](../../../internal/interfaces/console/password_test.go#L103) | PASS × 2 | L3133、L16451 |
| [Test应用事务已成功即使随后取消也通知](../../../internal/application/tasks/submit_test.go#L151) | PASS | L3730 |
| [Test应用候选重复缺失保留问题顺序](../../../internal/application/tasks/wizard_test.go#L303) | PASS | L3770 |
| [Test应用参数顺序缺失和重复编号](../../../internal/application/tasks/wizard_test.go#L180) | PASS | L3750 |
| [Test应用向导不能绕过权限](../../../internal/application/tasks/wizard_test.go#L122) | PASS | L3740 |
| [Test应用提交取消不提前通知](../../../internal/application/tasks/submit_test.go#L112) | PASS | L3728 |
| [Test应用提交成功后才通知](../../../internal/application/tasks/submit_test.go#L61) | PASS | L3724 |
| [Test应用提交整批失败不写入不通知](../../../internal/application/tasks/submit_test.go#L14) | PASS | L3718 |
| [Test应用提交顺序审计时间与诊断](../../../internal/application/tasks/submit_test.go#L38) | PASS | L3720 |
| [Test应用边界不接受其他服务或伪装接口包](../../../internal/archtest/transition_rules_test.go#L105) | PASS | L9297 |
| `Test慢资料请求不能用猜测新修订号恢复旧权限或凭据` [应用](../../../internal/application/identity/profile_test.go#L76) / [网页](../../../internal/interfaces/console/profile_concurrency_test.go#L80) | PASS × 2 | L3142、L16484 |
| [Test所有应用外部操作都受检查](../../../internal/archtest/final_closure_rules_test.go#L127) | PASS | L5254 |
| [Test执行与日志职责不能回到适配器](../../../internal/archtest/final_closure_rules_test.go#L153) | PASS | L5421 |
| [Test执行信号指纹与事件键兼容](../../../internal/domain/execution/protocol_test.go#L214) | PASS | L10328 |
| [Test执行期限等号与重启后重报](../../../internal/domain/execution/protocol_test.go#L249) | PASS | L10333 |
| [Test执行规则拒绝不改输入](../../../internal/domain/execution/protocol_test.go#L229) | PASS | L10330 |
| [Test接口传输输出可用但客户端取事实被拒绝](../../../internal/archtest/final_closure_rules_test.go#L73) | PASS | L4944 |
| [Test接口全文件与自建能力不得绕过](../../../internal/archtest/final_closure_rules_test.go#L43) | PASS | L4888 |
| [Test新模块不能漏出守卫](../../../internal/archtest/iso_test.go#L61) | PASS | L6283 |
| [Test日志传输先截断再脱敏且保持捕获时间](../../../internal/infrastructure/events/syslog_test.go#L60) | PASS | L12356 |
| [Test日志收录按事实判断并在成功后通知](../../../internal/application/execution/installer_logs_test.go#L57) | PASS | L2774 |
| [Test未知包不能绕过四层检查](../../../internal/archtest/four_layer_test.go#L34) | PASS | L5775 |
| [Test本人改密锁内复查令牌和账号](../../../internal/application/identity/password_test.go#L74) | PASS | L3120 |
| [Test机器入口保留观察时机与双重验证](../../../internal/application/execution/gateway_test.go#L327) | PASS | L2741 |
| [Test机器入口错误来源与状态保持](../../../internal/application/execution/gateway_test.go#L202) | PASS | L2717 |
| [Test机器凭据保持安装与收尾顺序](../../../internal/application/execution/service_test.go#L252) | PASS | L2809 |
| [Test机器应答渲染后取消不泄出正文](../../../internal/application/execution/gateway_test.go#L293) | PASS | L2729 |
| [Test机器接口不得读取仓储](../../../internal/archtest/final_closure_rules_test.go#L108) | PASS | L4975 |
| [Test机器日志观察保留原有效期](../../../internal/application/execution/observation_test.go#L24) | PASS | L2797 |
| [Test机器观察只在凭据检查通过后调用](../../../internal/application/execution/gateway_test.go#L175) | PASS | L2707 |
| [Test模板候选取消不返回半页或半总数](../../../internal/infrastructure/persistence/pgstore/template_selection_contract_test.go#L286) | PASS | L14123 |
| [Test模板候选读取期间变更仍使用同次事实](../../../internal/infrastructure/persistence/pgstore/template_selection_contract_test.go#L209) | PASS | L14121 |
| [Test模板候选资格只由领域实现](../../../internal/infrastructure/persistence/pgstore/template_selection_contract_test.go#L22) | PASS | L14113 |
| [Test模板候选跨批精确引用与分页一致](../../../internal/infrastructure/persistence/pgstore/template_selection_contract_test.go#L93) | PASS | L14117 |
| [Test模板取消等待选择锁后释放锁且不写入](../../../internal/interfaces/console/authoring_contract_test.go#L351) | PASS | L15721 |
| [Test真实接线先准备全部内置内容再创建管理员](../../../cmd/lober/assembly_test.go#L782) | PASS | L742 |
| [Test第六阶段迁移精确删除39项当前四表全零](../../../internal/archtest/phase6_history_test.go#L259) | PASS | L6532 |
| [Test纯执行规则只归领域](../../../internal/archtest/execution_rules_test.go#L17) | PASS | L4828 |
| [Test终态零许可且历史不能复活调用](../../../internal/archtest/task_finalize_rules_test.go#L793) | PASS | L9125 |
| [Test编写审计失败状态与审批人均回滚](../../../internal/interfaces/console/authoring_contract_test.go#L229) | PASS | L15555 |
| [Test编写输入多错时保留原先错误](../../../internal/interfaces/console/authoring_contract_test.go#L143) | PASS | L15152 |
| [Test编写页型与来源选择互不替代](../../../internal/interfaces/console/authoring_contract_test.go#L168) | PASS | L15233 |
| [Test编写预览路由契约基线](../../../internal/interfaces/console/authoring_contract_test.go#L24) | PASS | L15071 |
| [Test装配只豁免规范Wire注入文件且仍检查其导入](../../../internal/archtest/wire_rules_test.go#L8) | PASS | L9303 |
| [Test角色菜单并发首次保存与更新](../../../internal/infrastructure/persistence/pgstore/contract_role_menus_test.go#L73) | PASS | L13352 |
| [Test资料已提交后菜单读取失败保留事实](../../../internal/interfaces/console/profile_concurrency_test.go#L28) | PASS | L16479 |
| [Test身份写入审计失败不撤销会话](../../../internal/application/identity/users_test.go#L205) | PASS | L3170 |
| [Test身份十三入口契约与强制改密矩阵](../../../internal/interfaces/console/identity_contract_test.go#L16) | PASS | L16181 |
| [Test身份登录失败顺序与菜单故障](../../../internal/application/identity/access_test.go#L250) | PASS | L3071 |
| [Test身份菜单首次竞争及审计回滚](../../../internal/application/identity/menus_test.go#L199) | PASS | L3109 |
| [Test身份跨实例管理员移除仍由数据库保护](../../../internal/application/identity/users_test.go#L333) | PASS | L3181 |
| [Test通知同一订阅并发重复退订](../../../internal/infrastructure/notification/hub_test.go#L59) | PASS | L12634 |
| [Test通知同一订阅退订与发布并发](../../../internal/infrastructure/notification/hub_test.go#L78) | PASS | L12636 |
| [Test通知并发发布退订不竞态](../../../internal/infrastructure/notification/hub_test.go#L44) | PASS | L12632 |
| [Test通知按任务隔离合并并可退订](../../../internal/infrastructure/notification/hub_test.go#L8) | PASS | L12630 |
| [Test镜像打开有效额外源时取消仍返回取消](../../../internal/application/media/import_cancel_order_test.go#L48) | PASS | L3393 |
| [Test镜像文件能力只向精确接口开放](../../../internal/archtest/four_layer_test.go#L62) | PASS | L5779 |
| [Test镜像额外源打开取消类型保持](../../../internal/application/media/import_cancel_order_test.go#L18) | PASS | L3387 |
| [Test镜像额外源普通错误仍按原分类](../../../internal/application/media/import_metadata_cancel_test.go#L170) | PASS | L3422 |
| [Test镜像额外源读取取消超时类型与状态保持](../../../internal/application/media/import_metadata_cancel_test.go#L127) | PASS | L3405 |
| [Test领域拒绝随机数和外部操作](../../../internal/archtest/domain_rules_test.go#L10) | PASS | L4684 |

## 独立结论与最终文档证明

最终独立报告W/task-8-final-review.md已确认源码、契约和新真实提交的规定VM验收通过，严重0、重要0、轻微0待修。原I1/I2/I3/M1/M2/M3及本轮W-I1、重复夹具M1、文档状态M2全部关闭。审查者逐项比对原命令/退出码/摘要、完整前后清单、130名134处原PASS、两个正式矩阵及369行语义，没有重跑测试或用实施报告代替独立结论。

[公开最终审查](2026-10-25-ddd-final-closure-review.md)按该结论发布。最终六文档候选再核精确路径和SHA、UTF-8/LF、表格/链接、index空、2148非文档及580实际输入等价，交同一审查者只做最后文档差异核对；没有因状态文字再跑Go/Wire/Node。在候选冻结时最终文档提交尚未发生，最后文档核对与提交随本次文档提交生效完成；不声称已推送、合入dev或发布。

已接受的R2报告不可变存档为W/task-8-report-r2-accepted.md，SHA-256 `ae37703472b7ade9abdb50fc1529c9a8a4c8fa0f08717604e75d935d7b351ff2`。实时报告只追加最终文档过程，保留该字节前缀；所有旧日志/清单和原审计历史均保留。
