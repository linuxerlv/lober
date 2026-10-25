# 后端四层迁移第一阶段证据

状态：源码任务1至5已验收。Task6仅补文档，冻结文档树完整检查与独立整分支审查均已通过；最后文档结果增量另行复核后才提交。
已验收源码提交：6b9b9a22e38ba6941a47541dfe26d52a5959e1cd，文件树a38b435a17c2958588e5d2d580362025abd82736。
本文不写自身提交编号；最后提交、准确文件树和工作区状态由最终交付提供。

## 1. 本阶段完成范围与未完成范围

本阶段建立四层目录和自动依赖检查，迁移纯领域与仓储契约，抽出通知实现，并让五个任务向导操作通过任务应用服务。
Task5中的向导旧许可已全部删除。源码的完整检查和独立复审已通过；文档收尾不扩大实现范围。

其余编写、预览、主机、镜像后台流程、身份、审计、任务查询和事件流仍有明确过渡职责。
目录已经改名，不等于整个后端的业务职责已拆完。后续五阶段没有在本阶段提前实现。

搬迁与应用抽取保持原网页接口、数据库结构和既有业务结果。
独立的模板并发修复纠正了旧竞态窗口中的错误成功，已在第5节单列，不混写成“所有历史行为完全不变”。
没有新增登录方案、角色、主机字段或装机执行能力。角色仍是原五类，未实现的任务执行能力仍沿用原不可用结果。

## 2. 用户工作区、执行环境与隔离

用户原目录是 E:/src/lober，原任务分支为 codex/console-contract-auth-design。
实现位于 E:/src/lober/.worktree/ddd-foundation-task-creation，分支 codex/ddd-foundation-task-creation；没有把原工作树的未提交改动带入。

原生工作区创建报 git spawn ENOENT，附加工具又拒绝非托管目录，故使用已忽略目录中的手工 Git worktree。
它有独立暂存区，共享主库对象和分支信息；不能声称已经由桌面应用托管，交付必须给出上述准确路径。

本阶段未切换用户主目录分支，未清理其他工作区，也未自行合入 develop、推送或发布。
手工目录不能直接假定可用桌面托管归档/恢复；将来清理需先另行核实提交、私有环境和保留资料。

正式 Go 检查使用隔离 Docker Linux、Go1.27.1、PostgreSQL17及非超级用户测试角色，工作目录 /workspace 映射执行工作区。
容器必须能读取 Git 公共目录，且具备测试所需 Bash、BusyBox、make、gcc。缺真实数据库时不跳过测试来换绿色。

当时实际命令前缀如下；私有文件不交付，复现时应自行准备等价环境，不抄入连接内容：

```text
docker exec --env-file .superpowers/sdd/2026-10-22-backend-ddd-phase1/runner.env --env GIN_MODE=release -w /workspace lober-ddd-phase1-runner2
```

早期记录未显式写 -w 时，容器默认工作目录也是 /workspace。历史首次集中命令使用过 runner，后续正式完整检查使用 runner2，不能混写名称。
runner.env 选中的工具链是 /opt/lober/go、go1.27.1；不带该环境文件的容器默认 Go1.26.7 不是同一验证环境。

没有读取、打印或提交测试连接、口令、令牌和私有环境内容。真实库连接来自 LOBER_TEST_PG_DSN 或忽略的本地连接文件。
Windows的失败见第7节；本记录不声称原生Windows整套验收通过。

## 3. 提交历史与验收快照

原始起点：cd398fb965ee288eda0eb1030d79d1716ccd6515。
以下提交已按实际历史核对。文档收尾提交由最终交付给出，避免文档引用自身提交编号。

| 顺序 | 提交 | 实际内容 |
| --- | --- | --- |
| 1 | 6481ac5e3f488b6f770cd2e5cde37183c1796648 | 保存已批准设计、总路线和第一阶段计划 |
| 2 | 54b06732519ed777bbd1c90b5bd061bf39891496 | 只调整 Task N 标题以支持任务提取 |
| 3 | 074b45e962bf3331c099522ba2c2be60263fe635 | 固定向导契约、迁移清单和顺序断言 |
| 4 | 12c762600b2b4d803437a6aad7ba5ae8dd1533d3 | 回接独立的模板当前选择并发修复 |
| 5 | 7d6aa4871255f6c205c52a4e1ebfe05a5c83fe79 | 计划改用原子当前选择契约，明确纯渲染归领域 |
| 6 | 982064ce23f4f2750f773dfea8a1698771b473c7 | 迁移领域、仓储契约、权限、选择与编号实现 |
| 7 | 8da23166469f0b9a7a48fe8acda8d229a69fa179 | 搬迁外部实现与网页目录、抽取通知、接通严格依赖检查；含限定的退出测试整理 |
| 8 | eada679dddf581aec34a0453b8ac7817dedddc31 | 补全已批准的应用诊断、小接口、共享锁与错误约定 |
| 9 | 6b9b9a22e38ba6941a47541dfe26d52a5959e1cd | 五向导操作应用化、间接调用限制及唯一组件键收集函数 |
| 10 | 文档收尾提交由最终交付提供 | 正式阶段证据、最终现状核对与计划检查框 |

独立修复原提交为8619b239f9dd6c6c1081f96a6f98796d03c3e133，位于 codex/ddd-task-selection-concurrency。
迁移分支对应提交是12c7626，二者文件树完全相同，都是e749d74c3ff8fc3aa258acc9e9547e608a84e5cf；这是一次修复的两份历史记录。

| 项目 | 固定并验收的文件树 | 状态 |
| --- | --- | --- |
| Task1 | e783bae6d5cc6236bd42f302550e5f42228b9dd9 | 已提交、完整检查与独立审查通过 |
| Task2独立修复 | e749d74c3ff8fc3aa258acc9e9547e608a84e5cf | 原分支与迁移分支提交相同树，检查和审查通过 |
| Task3 | 6925e8cef5969bad687f74a34d467c77760bb3a7 | 修复全部审查问题后完整检查与提交树一致 |
| Task4 | 0b017ead5ebec23650328563b78d1d7a26571b69 | 初审、限定测试增量复审、最终全套均通过 |
| Task5 | a38b435a17c2958588e5d2d580362025abd82736 | 初审唯一问题已复审关闭；完整make退出0；提交树一致、工作区干净 |
| Task6初始文档审查树 | ea47b2b10619eb53f8d9ab9ac95832cf73bad92a | 完整make退出0、整分支审查Approved；最后结果增量另行复核 |

原计划中的单独 test(tasks) 审计提交没有另行产生：先保存失败补丁，再在独立修复提交中保留测试。
执行台账顶部旧 pending 行不是最终状态依据；本表依据后续完成记录和实际提交。

## 4. 全部已批准裁定

这些裁定限定了本阶段怎样执行。它们不是事后用来放宽断言的理由。

| 裁定 | 理由、范围与代价 |
| --- | --- |
| 使用手工隔离工作区 | 原生创建和附加失败；在已忽略目录隔离实现，保留用户原目录。代价是桌面不自动托管，交付必须写准路径 |
| 任务标题改为 Task N | 仅让提取工具识别任务，不改计划内容。若需撤销，仅回退该文档改动 |
| 固定树后并行只读审查 | 针对性行为检查完成后，可以让全套等待与静态审查并行；原审会说明全套未完成。任何源码变化都需精确补审；只有结果完成且提交树一致才验收 |
| 对示证缺陷先独立修复 | Task2真实数据库两场景失败，停止搬迁；独立分支修复，不再为已授权的必要步骤反复请求确认。修复可独立回退，无数据库结构变更，普通旧版创建不改 |
| 当前选择提交使用独立小能力 | CurrentTaskCreator.CreateCurrentTasks不嵌入普通Tasks。调用方显式注入，不能退回CreateTasks。普通方法继续能保存指定的旧审批版本 |
| 状态变更先于保护时返回409 | 审批替代或同版本废弃已先提交，就属于当前选择失效；一般镜像/主机准入仍为422。代价是纠正了原竞态窗口的旧结果，须单列说明 |
| 用户保密由实际网页转换验证 | 领域User移除网页标签，保留独立数据库记录；登录、资料、创建、修改、列表真实响应都检查密码不出现，并保留passwordChangeRequired等八字段。若遗漏入口会有泄漏风险，故不得只测领域对象序列化 |
| HookRefs唯一实现归model | repository只精确委托，selection只用模型，避免两份提取/排序/去重算法。代价是增加一个纯领域公共函数，不扩大依赖 |
| 纯渲染归domain/render | 原测试、样例随包搬迁；内置文件加载归基础设施，不用目录改名声称流程已拆完 |
| 编号改为正式叶子能力 | identity.NewID无业务依赖，是一项小函数契约；只允许原六个实际函数调用，不开放整个数据库包，也不额外引入通用容器。若未来改注入，范围是这六处；领域仍禁止随机数 |
| 保留唯一通知兼容入口 | console.Notifier是共同契约别名，console.NewHub只转发新实现，保留未传通知时的旧默认值；生产直接组装notification。只有该转发许可，阶段6删除；代价是一条明确临时桥接 |
| 实际符号检查提前进入Task4 | 原清单必须约束真正调用，复用已有x/tools，不改版本、不加生产运行依赖。额外工作只在检查中；Task5复用并撤销向导项，不另建第二规则来源 |
| Go1.27装载读取依赖源码 | 默认导出数据装载报错后改为完整源码装载，不吞错误；新增包、子目录、依赖和类型错误都失败。没有把无法检查当作无违规 |
| 应用结果携带预计算诊断 | TemplateChoice增加Problems，TaskChoice带Task/CanCancel/CanRetry，Submit返回后者；可选TaskDiagnostics只读现有控制对象能力，nil保留原空诊断。否则网页转换会偷偷继续判规则或丢字段；代价只在内部Go契约，不改网页结构 |
| 普通多问题错误不带网页码 | tasks.ProblemsError只携带领域问题，网页沿用原首问题分类。不能用固定422的仓储错误替代，改变既有409/422顺序 |
| 一次构造共享选择锁 | console.NewSelectionGuard返回窄接口，组装只构造一次；应用与旧模板审批/废弃共用，仅改lifecycle原两处模板锁。它只保留同进程协调，不能替代数据库事务保护 |
| 时钟别名与构造兼容 | console.Clock是ports.Clock别名；仅Clock的非向导构造保留，不从旧仓储隐式拼Wizard。真实数据库停顿点重接服务窄Tasks，诊断对象在构造时注入；代价是内部构造调整，不改外部行为 |
| HTTP退出先取证再限改测试 | 有限三次原例观测未复现；同步标准库实验确认未读正文可导致多余新连接。只整理socket测试私有连接池、完整读取/错误和清理；保留3秒断言、生产15秒、请求30秒、密码强度。可回退的纯测试改动，未宣称捕获原失败原因 |
| 批量组件键只留一份收集策略 | Task5审查确认重复后，抽为repository.TemplateHookKeys，继续调用model.HookRefs和CheckVersionKey，只收集、过滤、去重和排序，不读仓储。新旧调用者各自GetHooks；旧网页仍按文件/函数/符号/阶段2许可，向导不能绕回旧helper |
| Task6仅文档时复用同源码实跑 | 若后续只改文档，复用Task5最终同源码真正执行、非缓存的应用/网页/数据库/组装结果，不为任务名称重跑全套count1组合。最终文档树仍运行make check及其强制count1架构守卫；其他有效缓存可用。任何源码变化都使该复用前提失效，须补实际执行；不能声称跑了没跑的组合命令 |
| 主动中止待修树的完整检查 | Task5原树已知需要修改，经控制器明确指令，仅中止自己核实过的进程组；退出143是主动中止，不算业务失败或套件通过。新树须重新完整检查和补审 |
| 无自行集成发布 | 设计与本分支交付都不是发布批准；是否进develop由用户授权，之后仍经test、preview、main流程。未新增登录、三角色方案或主机字段 |

## 5. 独立并发缺陷与事务证据

原向导先在事务外读当前审批版本，进程内锁只覆盖当前服务。任务锁旧版本行，另一个服务审批新版本行时能先提交。
因此“旧选择已失效，任务却成功写入”的窗口真实存在，不能靠目录迁移自动消失。

原六场景使用两个独立服务和连接池，第二个服务自己登录，不共享选择锁或会话。
先失败四过二败，重复两次仍相同；普通仓储接受指定旧审批版本的对照通过。

| 真实顺序 | 修复后必须满足 |
| --- | --- |
| 新版本审批先完成，再交旧选择 | 409，零任务、零task.create审计 |
| 应用已读选择、尚未取得数据库身份保护，另一实例审批先提交 | 409，零任务、零任务审计 |
| 任务已取得保护，插入被真实数据库屏障挡住，另一实例审批 | 审批等待任务事务；释放屏障后双方成功，任务快照仍保存所选版本 |
| 同版本废弃先完成，再提交 | 409，零任务、零任务审计 |
| 应用已读选择、尚未取得保护，同版本废弃先提交 | 409，零任务、零任务审计；这是明确批准的当前选择窗口纠正 |
| 任务已取得保护，另一实例废弃同版本 | 废弃等待任务事务；释放屏障后双方按顺序成功 |

不是靠“睡一会儿没完成”猜锁生效。测试用pg_stat_activity、pg_locks、pg_blocking_pids确认实际等待关系。
例如一次验收记录任务连接5111在tasks表等待夹具5113，审批又等待该任务；同版本废弃对应5125/5127。释放插入屏障后各自成功。

当前提交的锁顺序：模板身份读锁→按规范编号字节序的主机写锁→镜像读锁→所选模板版本读锁→排序组件版本读锁→任务和审计插入。
模板审批、废弃先锁身份再锁版本；保护一直持有至提交或回滚。内置装载、开版本、删草稿等既有身份锁顺序已审核，未添加反向等待环。

错误约定：不存在仍404；存在但非当前选择为ErrTemplateNotCurrent，包装ErrConflict，网页仍409、原提示、data:null；一般准入仍BatchError/422。
普通CreateTasks不加当前限制。任务和审计同事务，失败整批回滚；同主机反序竞争只允许一批及其审计成功。

## 6. 五网页入口与应用契约

完整字段契约仍由仓库内 docs/contracts/backend-modules.md 维护，以下是自包含的验收要点。
Task5已同步该文档的显式Wizard注入与向导许可清零；Task6只核对补漏，不覆盖成旧CurrentTasks字段说法。

共同路径前缀/console/v1/wizard。五操作均要求task:create：管理员和操作员可用，其余三角色403；未登录、停用、过期或无效角色401；必须改密403。
成功外层仍code:0、data、message为空；错误保持原状态/原因，通常data:null。

| 网页入口 | 应用入口 | 不能改变的要点 |
| --- | --- | --- |
| GET /hosts | Wizard.Hosts | q前缀、属性成对过滤、cursor/limit；开放任务主机仍列出且不可选；默认100、最大500，规范编号字节序 |
| POST /isos | Wizard.ISOs | 非空hostSNs；缺失/重复为整批问题；只分页查准备镜像，候选不兼容放每项问题；输入主机顺序保留 |
| POST /templates | Wizard.Templates | 先判断镜像，再分页；每模板只取最高已审批，不回退更旧版；当前内置清单和引用限制；先分页再筛，允许空页但next非空 |
| POST /params | Wizard.Params | 选中模板/镜像缺失404，当前选择失效409先于一般参数问题；entered按规范编号匹配，未知行/规范化重复键400；一般问题仍随200返回 |
| POST /tasks | Wizard.Submit | 每主机自身entered生成快照；任一失败整批零写入；成功顺序与输入一致；创建人/时间来自可信Actor与Clock，浏览器字段无效；每任务审计、提交后通知 |

分页0和省略等价，负数或大于500为400；没有下一页next为空。空问题与字段列表保持[]，空属性保持{}，原可空字段不变。
重复主机、缺失主机、参数行和批量问题归属沿用旧行为，不把不同入口的问题去重方式擅自统一。

应用五入口均在仓储前检查Actor；无效身份→强制改密→创建权限的错误顺序不变，拒绝时仓储调用数为零。
窄Tasks仅FindOpenTask/CreateCurrentTasks；组装显式构造Wizard并注入共享锁、通知、时钟、发布清单和只读诊断。

已验收实现保留逐任务读取时钟、批量通过后再取审计时间。提交返回前零通知；错误或提交取消不通知；已提交成功后才取消不能抹掉提示。
共享进程读锁只覆盖原Params/Submit范围，模板审批/废弃使用同一写锁，不扩大其他操作加锁范围。

## 7. 验证过程、原始失败与修复

下面关键命令与结果直接写在本文。括号内历史记录名只作定位；这些临时日志日后清理也不影响本文理解。
命令均接第2节实际Docker前缀，特殊runner名称、是否缓存、是否中断另行注明。

### 7.1 Task1：固定契约与环境补齐

- `go test ./internal/archtest -run Test迁移豁免 -count=1`：空验证实现先失败，17类非法定义、重复及五种借用都被观察到；实现后相同命令通过，Linux独立0.007s。历史记录task-1-guard-green.log。
- `go test ./internal/console ./internal/store/pgstore -run 'Test向导|Test契约' -count=1`：在最初runner执行，console25.332s、pgstore37.766s，真实数据库通过。包含反序主机参数不串行、未知行/规范化重复键不留任务或审计。
- 最终runner2 `make check`退出0，强制守卫22.310s；race console639.864s、pgstore138.483s、arch158.426s、cmd111.012s实际通过；Linux构建和Wire差异检查通过。部分普通测试使用前面有效缓存。

环境失败完整保留为事实：Windows首次make退出1，停在普通测试；本地测试库未启动，另有绝对路径、符号链接权限、Bash和BusyBox缺条件。
第一次Linux缺Git公共目录读取，git ls-files退出128，且缺shell工具；第一次cmd具体失败被长输出遮盖，不能事后猜用例。

补Git挂载和shell路径后，BusyBox文件仍未真正安装，普通测试再失败，后续race/构建/生成未执行。
补实际可执行BusyBox后先跑 `go test ./internal/builtin ./internal/render -count=1`，builtin0.679s、render6.740s通过，再执行上述最终全套。
Windows与Linux初始日志有捕获片段，不能称完整历史；没有为补旧日志重跑缺环境的测试，也未删除测试或降低密码计算。

Task1轻微文档遗漏“attr_key/attr_value必须同供或同省，否则400”已在独立修复中补正，不是未关闭项。

### 7.2 Task2：先证明旧缺口，再独立修复

- `go test ./internal/store/pgstore ./internal/console -run 'Test任务提交与独立实例|Test契约并发|Test向导提交期间' -count=1 -v`：真实六场景四过二败，旧版本任务和审计错误成功；原同实例及普通批次并发通过。
- `go test ./internal/console -run 'Test任务提交与独立实例' -count=2 -v`：重复两次同样四过二败，16.594s；不是一次调度偶然。
- `go test ./internal/store/pgstore -run 'Test契约任务/建任务：普通仓储' -count=1 -v`：旧版普通创建对照通过，0.174s。
- 独立修复生产代码前再次跑原场景，仍四过二败，10.309s；随后修复共同身份保护。中间GORM直接扫描领域模板的关联解析失败，改成version/state事实行，不放宽断言。
- `go test ./internal/store/pgstore ./internal/console ./cmd/lober -run 'Test契约任务|Test契约并发|Test任务提交与独立实例|Test向导提交期间|Test向导事务内' -count=1 -v`：仓储与网页通过，包括事务内普通准入、当前选择409、审计失败回滚和第5节实际阻塞。cmd该筛选没有用例，不能算实跑。

首轮完整make的唯一失败是原真实socket退出测试；普通console141.093s、race console559.890s、pgstore112.656s通过，但构建和Wire未执行。
历史日志底部是race Error1，未单独附外层退出码，本文只称“失败”，不额外推算。

明确授权后原socket `go test -race ./cmd/lober -run TestHTTP真实监听登录及取消退出 -count=3 -v` 三次通过，整个用例各3.51/3.45/3.35s，包11.338s。
没有改超时或源码；再次完整make退出0，cmd race29.265s实际运行，其他已成功包可用缓存，Linux构建/Wire均通过。失败历史没有抹掉，见下一节后续诊断。

### 7.3 Task3：领域边界及审查修正

`Test领域拒绝随机数和外部操作`、`Test编号临时入口必须精确到实际调用`、`Test业务用户不带网页或数据库标签`先失败：旧store子树放过外部操作、用户带网页标签。
实现后领域、编号、权限、内置选择和真实用户响应集中检查通过；console10.918s、pgstore3.370s。cmd筛选未匹配，后由全套覆盖。

中间机械搬迁有包名、导入、类型和字段误替换导致的编译错误，逐位置修正，没有修改业务断言。
缓存夹具在暂存前读旧路径失败，一次中间命令暂缓该例；完整暂存后 `go test ./internal/domain/... ./internal/infrastructure/identity ./internal/archtest -count=1` 无跳过通过，arch62.200s。

独立初审发现一项必须修正问题：context的四类超时/截止函数与AfterFunc会绕过只查time的限制。
先失败再修；同时恢复builtin原不允许context的边界，并修正局部同名变量误报。普通导入、别名、点导入、函数值、repository/nested及渲染均覆盖，合法Context类型和pgtest定时仍允许。

关键命令：`go test ./internal/archtest -run 'Test领域禁止通过上下文|Test领域上下文限制' -count=1` 先失败；最终相关新旧检查通过0.029s，builtin回归0.026s，遮蔽回归0.034s。
初审与两次精确复审关闭全部问题；只修改守卫，不改生产行为。

首轮完整运行中守卫还在改，即使退出0也只算中间结果；最终冻结6925e8树后另跑 `make check`退出0。
最终强制守卫17.847s、普通arch64.721s、race arch67.944s重新运行；相同业务源码可复用前轮成功结果（race console567.728s、pgstore99.276s、cmd80.106s）。Linux构建和Wire通过。

### 7.4 Task4：四层、通知与HTTP退出取证

- `go test ./internal/archtest -run 'Test基础设施拒绝接口层和应用具体服务|Test未知包不能绕过四层检查' -count=1 -v` 先失败：新路径没归类、旧路径仍放行、通知不能用共同契约；实现后通过。
- 默认类型数据装载在Go1.27报bufio缺类型，改读完整依赖源码。真实类型检查仍会报告依赖、类型和装载错误；别名、方法值、新文件/函数、伪装ports和真实小模块类型错误均有反例。
- `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire ./cmd/lober`由工具重生成；没有手写生成文件。
- 搬迁后网页原测试强转已搬出的私有hub而无法编译。改为经通知契约观察两个真实事件流退订，通知包内仍检查真实订阅表在重复退订和发布并发后清空；console95.762s、notification0.009s完整通过。

首轮 `make check`退出2：唯一失败是cmd的真实socket退出测试，提示“HTTP service did not stop”；console race607.020s、pgstore112.017s、arch151.859s通过，构建与生成未到达。
这次失败不能算绿色，也不能说所有包重跑前都失败。

随后先只读调查正确Go1.27.1源码，再加临时ConnState、httptrace和退出计时，保持密码、请求与正文处理不变。
只跑原例 `go test -race -run '^TestHTTP真实监听登录及取消退出$' -count=3 -v -timeout=3m ./cmd/lober`，三次都通过，11.815s；每轮只有一条用过的连接。

观测到Body.Close返回可以早于PutIdleConn，但没有捕获原失败现场的多余新连接。
临时补丁未提交，结束后恢复精确暂存树63b005b9，未暂存差异为零；不做无限重复试跑。

独立标准库同步实验使用同一Go1.27.1及race。首次响应只发头，正文等待信号；未读Close返回后强制第二次真实拨号B成功但暂不返回。
确认服务端B为StateNew后才释放第一正文，第二请求明确复用A，再释放B的拨号返回。

3秒快照：A已经关闭，B仍为从未Active的新连接；B随后关闭，Shutdown在5.770695185秒返回nil，小于原生产15秒。
对照只改变“先读到正文结尾再Close”：仅一次拨号，Shutdown372.618微秒返回nil。独立实验一次完成并通过race，无固定睡眠碰窗口。

这证明测试夹具的未读正文可能制造多余连接，使3秒假设失效；不证明已经直接抓到原次全套失败的唯一原因。
当时的Go1.27.1机制是Close可先返回、正文后台读完后才归池；重叠拨号可以留下未用连接，服务端将StateNew保留超过5秒再按空闲处理。

批准的永久改动仅在原socket测试：私有Transport，所有响应显式读完并检查读取/关闭错误，退出时清理空闲连接。
原3秒断言、30秒请求期限、生产15秒Shutdown和密码运算不变。该精确增量18行新增/4行删除，独立复审通过。

目标race三次通过11.194s；新树0b017ead上最终 `make check`退出0。
强制守卫21.445s、普通cmd8.753s/arch86.415s、race cmd52.555s/arch102.955s实际执行；其他未改包复用前轮有效成功缓存，包括console已实跑607.020s。Linux构建、Wire差异检查都完成。

### 7.5 Task5：应用抽取、唯一审查问题与最终通过

已确认的针对性过程如下，不能替代最终全套：

| 命令或用例 | 真实结果 |
| --- | --- |
| `go test ./internal/application/tasks -count=1`，只有契约/测试而NewWizard未实现 | 先失败，exit1；实现后应用全包0.008s通过 |
| 同包旧helper、方法值、两层转发、模板/任务诊断、包级函数值7类反例 | 先发现绕行而失败；新增路径检查后通过；真实总守卫23.64s通过 |
| 新窄接口直写与网页通知反例 | 先失败；纳入同一实际类型/本地引用路径检查后通过，纯类型转换正例仍允许 |
| `go tool wire ./cmd/lober`（fallback和无artifact设置） | 工具生成成功，未手写 |
| 首次应用/console/cmd针对检查 | 新错误映射样例的arch_mismatch问题漏传两项消息参数，夹具构造panic；只补测试参数，未改生产行为；应用0.008s、cmd7.872s已过 |
| 向导限制与新错误/诊断映射 | 守卫0.023s、网页两例1.420s通过 |
| `go test ./internal/application/... ./internal/interfaces/console ./internal/archtest ./cmd/lober -skip TestIncrementalGateRejectsPackageInExistingRoot -count=1` | 暂存前中间结果exit0：应用0.008s、console111.897s、arch50.939s、cmd9.928s；不当作最终跳过许可 |
| 完整暂存后相同包 `-count=1`，不带skip | 原候选acd6ed00上exit0：应用0.008s、console153.017s、arch118.852s、cmd17.939s，新增文件缓存夹具实际通过 |

原审唯一必须修正问题：application的templateHooks复制了旧网页helper整段组件键收集、合法性过滤、跨模板去重和排序。
没有声称两条路径已经给出不同结果；问题是新添了第二份业务策略，未来会漂移，违反单一实现要求。

原审记录task-5-review.md：Critical0、Important1、Minor0；其余权限、顺序、诊断、事务、通知、锁和真实数据库屏障符合契约。
控制器确认修复后，主动终止了唯一核实过的本次make进程组；不动其他执行者或容器。

该原始完整检查退出143。终止前fmt、vet、强制总守卫24.438s通过，普通测试刚开始；race、构建、Wire未完成。
这是主动安全中止，既不是完整通过，也不是业务失败。不能把其部分结果算作修复后新树的全套证据。

共享纯函数最终定为repository.TemplateHookKeys，调用原model.HookRefs和CheckVersionKey，不读仓储，不改输入。
应用与旧网页各自取得这些键后调用自己的GetHooks；非法草稿过滤、非nil空列表、编号后按数字版本排序均保留。

新增函数尚未实现时 `TestTemplateHookKeys`相关测试先失败，exit1。
实现后运行：

```text
go test ./internal/domain/repository ./internal/application/tasks ./internal/interfaces/console ./internal/archtest -run 'TestTemplateHookKeys|Test应用|Test向导|Test任务提交与独立实例|Test.*模板|Test全模块依赖守卫' -count=1
```

真实结果exit0：repository0.010s、tasks0.010s、console32.789s、arch27.896s。含真实数据库模板/跨实例回归，没有跳过数据库。
行为检查覆盖重复引用、非法编号、零/负版本、数字排序、nil/空输入、空装配和全部非法引用。

阶段2的旧helper仅把原两项纯函数许可改成精确TemplateHookKeys，GetHooks许可保留，不扩大文件或函数范围。
新增向导绕回旧templateHooks反例；全模块检查器未削弱。

修复相对acd6ed00只有6文件、82新增/43删除，新冻结树a38b435a17c2958588e5d2d580362025abd82736。
限定复审task-5-fix1-review.md确认I1已解决，Critical/Important/Minor均无剩余；与原审未变部分合起来覆盖新树。

Task5修复后完整 `make check` 自然结束，退出0，没有中断、跳过或本轮重试。对应task-5-fix1-make-check.log，完整关键结果如下：

| 实际命令 | 结果 |
| --- | --- |
| 格式检查、`go vet ./...` | 通过 |
| `go test -count=1 -run '^Test全模块依赖守卫$' ./internal/archtest` | 实际执行，24.702s通过 |
| `go test ./...` | 全部通过；tasks0.024s、cmd22.597s、console170.973s、pgstore93.252s、arch139.766s、repository0.031s均实际执行 |
| `go test -race -timeout=20m ./...` | 全部通过；tasks1.056s、cmd98.111s、console621.596s、pgstore124.432s、arch171.967s、repository1.114s均实际执行，没有数据竞争或超时 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 全包通过 |
| `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober` | 通过，没有生成内容漂移 |

普通检查还实际执行builtin1.054s、media33.559s、pgtest1.372s；并发检查还实际执行builtin4.156s、media50.246s、notification1.101s、pgtest5.014s。
未变的其他领域包及identity、iso9660、isotest沿用有效缓存；通知普通测试也是缓存。没有声称每包强制重新执行。

控制器验收后提交6b9b9a22e38ba6941a47541dfe26d52a5959e1cd，提交树与审查树完全一致，均为a38b435a17c2958588e5d2d580362025abd82736，提交后工作区干净。
原审task-5-review.md的唯一Important及task-5-fix1-review.md已全部关闭，没有待延期审查项。

## 8. 文件搬迁、保留内容与规则唯一归属

| 原位置 | 当前实际位置 | 保留内容 |
| --- | --- | --- |
| internal/core/* | internal/domain/* | 纯规则、模型及原测试样例 |
| internal/render | internal/domain/render | 纯生成逻辑、原测试和全部样例 |
| internal/store根 | internal/domain/repository | 按对象拆小契约，保留校验、查询和错误约定 |
| internal/store/pgstore | internal/infrastructure/persistence/pgstore | 原实现、记录转换、建表与约束、真实数据库测试；pgtest仍是子包 |
| internal/builtin | internal/infrastructure/builtin | 七种格式、共享正文、发布清单、指纹、答案样例及验证工具 |
| internal/iso | internal/infrastructure/media | 包名仍iso，iso9660与isotest一起保留；流程仍是第4阶段过渡 |
| internal/console | internal/interfaces/console | 原网页输入输出和未迁移入口，五向导只接应用 |
| console通知实现 | internal/infrastructure/notification | 同一锁及一次性退订，独立共同Notifier契约 |

Task4逐路径核对39个数据库文件、86个内置文件、38个镜像文件、51个网页原文件，没有缺失。
非Go正文与样例逐字节不变，只有native-content.md中的两个失效渲染文档链接修正。
数据库19、内置7、镜像20、网页25个生产Go文件在归一新旧路径、除去导入块后正文无意外变化；通知抽取单列验证。

权限只有domain/access一份；业务模型与网页字段、数据库记录分开。
model.HookRefs负责单模板提取，repository.TemplateHookKeys负责跨模板有效键收集；二者职责不同且后者只复用前者和既有键校验。

随机编号只有infrastructure/identity.NewID，正式能力仅六处：CreateHook、CreateISO、CreateTemplate、DeriveTemplate、createTasks和pgtest.newSchema。
它不是待清零的临时例外。领域仍禁止文件、网络、数据库、读取时钟、随机数和上下文计时/异步操作；内置正文保留更严的纯内容边界。

## 9. 剩余过渡职责与后续阶段

已验收a38b435a中向导许可为零。其他过渡许可仍有330条；不能写“全部旧职责都已迁走”。
这里只记录已验收固定清单，不据此新增任何运行许可。Task6不修改任何生产或测试源码；后续源码变化须重核对。

| 清单 | 条数 | 清除阶段 |
| --- | --- | --- |
| migrationExceptions | 134 | 2：48；3：10；4：24；5：24；6：28 |
| mediaMigrationExceptions | 123 | 4：123 |
| consoleRuleMigrationExceptions | 72 | 2：23；3：6；4：5；5：26；6：12 |
| notificationMigrationExceptions | 1 | 6：1，仅console.NewHub |

| 后续阶段 | 尚未迁移的范围 |
| --- | --- |
| 2 编写与预览 | hook/模板编写、审批、废弃、版本与预览；旧templateHooks、旧诊断包装及接口层选择锁过渡实现 |
| 3 主机 | 登记、编辑、删除、批量导入、查询和规则 |
| 4 镜像与后台作业 | 来源、上传、获取、导入、删除、恢复与资源释放；Sources仍混有流程，不是终态基础设施 |
| 5 身份与审计 | 登录、用户管理、权限操作、审计查询；旧权限名字转发清理，五角色保持 |
| 6 任务读取与收尾 | 列表、详情、控制、事件流、共享错误转换、通知转发和组装点遗留流程；最后迁移例外清零 |

附录给出已验收清单的具体文件、函数、目标符号及阶段；唯一运行规则来源仍是受管理源码里的固定清单。
历史文档和故意验证旧路径失败的内存反例可保留旧名称；最终检查应确认没有旧路径的真实导入，不能粗暴删历史或反例。

## 10. 回退第一阶段并保留独立并发修复

不要把原始起点到最终提交整段盲目撤销，那会把已验收的并发保护一起移除。
独立修复原分支8619b239仍在，迁移分支等价12c7626必须保留。

建议在单独回退分支，以保留修复的12c7626为落点，按后进先出撤销其后的阶段提交：最终文档收尾提交、6b9b9a2，再eada679、8da2316、982064c、7d6aa48。
这是操作设计，不表示本阶段已执行回退。若未来有其他模块合入，按仓库模块回退流程另行处理冲突，不直接移动用户主分支。

保留内容包括CurrentTaskCreator/CreateCurrentTasks、身份先锁及事务内重查、失效选择409、普通CreateTasks旧版语义、真实跨实例与审计回滚测试。
回退冲突不能为了好合并删掉这些保护；需按修复契约重新验收。

12c落点保留此前的设计和契约基线。若连这些文档/测试也要移除，应单列范围，不顺手丢修复。
8da2316也包含纯socket测试整理，整提交回退会带走它；如仍要保留，应另行审查转接该函数内18+/4-增量并验证，不能说它本来就独立提交。

## 11. Task6现状核对与验收记录

Task6以6b9b9a22为起点，只修改本文、backend-modules契约的收尾说明与阶段1计划检查框。生产源码、测试、依赖版本及生成组装内容均不改。

### 已完成的静态核对

- 使用Go标准库go/parser以ImportsOnly逐个解析cmd和internal下292个实际Go文件，旧core/store/console/render/builtin/iso导入数为0，退出0。它只读取真正的import，不把注释、文档或内存反例字符串计为导入。
- `git diff --name-only cd398fb965ee288eda0eb1030d79d1716ccd6515 HEAD -- web` 无输出：前端改动文件为0。
- 将起点的internal/store/pgstore与现有新路径对照，schema.go、四个rows文件及open.go的数据库标签全部一致。去掉导入声明、归一store→repository及User/Role/Source等已搬模型后，这六个文件正文相同。表、列、索引、约束和迁移入口未变；rows_audit只改包名及相应注释。
- 五路由、权限、分页、空列表、错误类别、问题归属、审计与顺序已逐项对照第6节和backend-modules契约。普通旧版CreateTasks与当前选择原子接口仍分开；源码验证见Task5最终真实执行，不增加一次虚构命令。
- 剩余330条精确过渡许可、0条向导豁免和正式6处编号能力已核对；附录固定于已验收a38b435a，执行规则仍只来自源码清单。

### 检查调度裁定与实际边界

Task6仅改文档，按已批准裁定复用Task5同源码的非缓存联合行为结果及完整架构反例。Task5实际普通/并发命令与秒数见第7.5节。
原计划Task6单独的 `go test -count=1 ./internal/application/tasks ./internal/interfaces/console ./internal/infrastructure/persistence/pgstore ./cmd/lober` 组合没有运行，不能写成已执行。
Task6也不为任务名称重复架构全包count1；文档树的make check强制实际执行总依赖守卫，其他未变源码的有效缓存可复用。任何生产或测试源码再改都使该复用前提失效。

### Task6完整检查的实际结果

对初始文档树ea47b2b10619eb53f8d9ab9ac95832cf73bad92a运行第2节Docker前缀加 `make check`，自然结束，退出0。没有中断、跳过或本轮重试。
临时记录task-6-make-check.log末尾为TASK6_MAKE_CHECK_EXIT=0；关键结果直接保留如下，清理临时日志后仍可理解：

| 实际命令 | 结果 |
| --- | --- |
| 格式检查、`go vet ./...` | 通过 |
| `go test -count=1 -run '^Test全模块依赖守卫$' ./internal/archtest` | 本次实际执行，27.542s通过 |
| `go test ./...` | 全包通过，所有有测试包均复用有效缓存 |
| `go test -race -timeout=20m ./...` | 全包通过，所有有测试包均复用有效缓存 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 全包通过 |
| `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober` | 通过，无生成内容漂移 |

这份结果与第7.5节Task5同源码的真实普通/并发执行一起构成联合行为证据；没有把缓存写成重新实跑。

### 独立整分支审查与轻微文档修正

独立审查对照起点cd398fb965ee288eda0eb1030d79d1716ccd6515与固定树ea47b2b10619eb53f8d9ab9ac95832cf73bad92a，规格符合，质量Approved。
审查逐项读取450个文件差异段；归一路径及已搬类型后314段机械变化相等，其余136段逐项核对。七份拆分仓储契约声明保留；330项许可与本附录171行逐项一致；web、go.mod、go.sum无变化。

结论为0项严重、0项重要、1项轻微问题。唯一M1是phase1计划第13行仍称“未执行、待选择”，与已完成框矛盾。
本次最后文档增量已改成实际阶段状态，并补实际完整检查和整分支审查结果；该文档增量单列复核，不改生产或测试源码。没有延期不处理的审查项。
原审查记录final-branch-review.md；审查者没有运行测试或改暂存区，也不以此前任务Approved代替本轮整分支检查。

### 最后提交与交付约束

本次仅补已发生的验收结果、轻微状态修正和已完成检查框，不新增架构裁定，不改变第4节任何批准范围，也保留第7.4节“原HTTP失败现场原因未直接捕获”的限制。
最后结果增量须通过精确文档复核及完整检查，由控制器核对准确树后才提交。准确提交编号、文件树与工作区状态由最终交付提供，不在本文预造自身提交编号。
没有自行合并develop、推送或发布。第一阶段完成后，后续阶段仍须各自编写实施计划并按用户授权办理。

## 附录：剩余许可逐项记录

以下由已验收a38b435a树中的两份受管理清单逐项核对，合并同函数同目标包的符号展示，共330条许可、171个表格行。它是证据记录，不是另一套运行规则。
目标包采用相对 internal 的路径，完整前缀为 github.com/lober/lober/internal/。文件名已确定调用包，函数及目标符号必须同时匹配。阶段1向导许可已经没有；正式6处编号能力不在本表。

| 清除阶段 | 调用文件 | 函数或声明 | 目标包 | 仅允许的符号 |
| --- | --- | --- | --- | --- |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).createHook` | `domain/repository` | `Hooks.CreateHook` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).deleteHookDraft` | `domain/repository` | `Hooks.DeleteHookDraft` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).getHook` | `domain/repository` | `Hooks.GetHook` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).listHookVersions` | `domain/repository` | `Hooks.ListHookVersions` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).listHooks` | `domain/repository` | `Hooks.ListHooks`、`Page` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).newHookVersion` | `domain/repository` | `Hooks.NewHookVersion` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).renameHook` | `domain/repository` | `Hooks.RenameHook` |
| 2 | `internal/interfaces/console/hooks.go` | `(*server).saveHookDraft` | `domain/repository` | `Hooks.GetHook`、`Hooks.SaveHookDraft` |
| 2 | `internal/interfaces/console/hooks.go` | `ProblemsOf` | `domain/activation` | `Hook` |
| 2 | `internal/interfaces/console/hooks.go` | `authoringQuery` | `domain/model` | `VersionState.Valid` |
| 2 | `internal/interfaces/console/hooks.go` | `authoringQuery` | `domain/repository` | `HookQuery` |
| 2 | `internal/interfaces/console/hooks.go` | `hookKeyOf` | `domain/repository` | `CheckVersionKey` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).approveHook` | `domain/repository` | `Audit`、`Hooks.ApproveHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).approveTemplate` | `domain/repository` | `Audit`、`Templates.ApproveTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).deprecateHook` | `domain/repository` | `Hooks.DeprecateHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).deprecateTemplate` | `domain/repository` | `Templates.DeprecateTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).rejectHook` | `domain/repository` | `Hooks.RejectHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).rejectTemplate` | `domain/repository` | `Templates.RejectTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).reloadHook` | `domain/repository` | `Hooks.GetHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).reloadTemplate` | `domain/repository` | `Templates.GetTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).submitHook` | `domain/repository` | `Hooks.SubmitHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).submitTemplate` | `domain/repository` | `Templates.SubmitTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).templateProblems` | `domain/activation` | `Template` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).validateHook` | `domain/repository` | `Hooks.GetHook` |
| 2 | `internal/interfaces/console/lifecycle.go` | `(*server).validateTemplate` | `domain/repository` | `Templates.GetTemplate` |
| 2 | `internal/interfaces/console/lifecycle.go` | `package-init` | `domain/repository` | `Audit` |
| 2 | `internal/interfaces/console/lifecycle.go` | `rejectReason` | `domain/model` | `StorableText` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).previewTemplate` | `domain/catalog` | `Firmware.Valid`、`Series.Valid` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).previewTemplate` | `domain/model` | `NewProblem` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).previewTemplate` | `domain/repository` | `Hosts.GetHost`、`ISOs.GetISO`、`Templates.GetTemplate` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).previewTemplate` | `domain/snapshot` | `Build` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).renderPreview` | `domain/catalog` | `DeliveryFileName`、`DeliveryFiles` |
| 2 | `internal/interfaces/console/preview.go` | `(*server).renderPreview` | `domain/render` | `HookFileName`、`Render`、`Request`、`RuntimeFileName` |
| 2 | `internal/interfaces/console/preview.go` | `SampleISO` | `domain/catalog` | `Format.SystemValues`、`RequiredRoles`、`Series.Family`、`Series.Format` |
| 2 | `internal/interfaces/console/server.go` | `package-init` | `domain/repository` | `Hooks`、`Templates` |
| 2 | `internal/interfaces/console/slots.go` | `(*server).slotsOf` | `domain/activation` | `Template` |
| 2 | `internal/interfaces/console/slots.go` | `(*server).slotsOf` | `domain/model` | `TemplateKey.String` |
| 2 | `internal/interfaces/console/slots.go` | `(*server).slotsOf` | `domain/repository` | `Templates.GetTemplate` |
| 2 | `internal/interfaces/console/slots.go` | `(*server).slotsOf` | `domain/scan` | `Scan` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).createTemplate` | `domain/repository` | `Templates.CreateTemplate` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).deleteTemplateDraft` | `domain/repository` | `Templates.DeleteTemplateDraft` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).deriveTemplate` | `domain/repository` | `Templates.DeriveTemplate` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).getTemplate` | `domain/repository` | `Templates.GetTemplate` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).listTemplateVersions` | `domain/repository` | `Templates.ListTemplateVersions` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).listTemplates` | `domain/catalog` | `Series.Valid` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).listTemplates` | `domain/repository` | `Page`、`TemplateQuery`、`Templates.ListTemplates` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).newTemplateVersion` | `domain/repository` | `Templates.NewTemplateVersion` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).renameTemplate` | `domain/repository` | `Templates.RenameTemplate` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).saveTemplateDraft` | `domain/repository` | `Templates.GetTemplate`、`Templates.SaveTemplateDraft` |
| 2 | `internal/interfaces/console/templates.go` | `(*server).templateHooks` | `domain/repository` | `Hooks.GetHooks`、`TemplateHookKeys` |
| 2 | `internal/interfaces/console/templates.go` | `templateKeyOf` | `domain/repository` | `CheckVersionKey` |
| 2 | `internal/interfaces/console/templates.go` | `toTemplateResponse` | `domain/activation` | `Template` |
| 3 | `internal/interfaces/console/csv.go` | `parseHostCSV` | `domain/model` | `Host.Validate`、`HostReservedAttrs`、`NewProblem`、`NormalizeSN`、`ValidAttrKey` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).createHost` | `domain/repository` | `Hosts.CreateHost` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).deleteHost` | `domain/repository` | `Hosts.DeleteHost` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).getHost` | `domain/repository` | `Hosts.GetHost` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).importHosts` | `domain/repository` | `Hosts.GetHosts`、`Hosts.ImportHosts` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).listHosts` | `domain/repository` | `HostQuery`、`Hosts.ListHosts`、`Page` |
| 3 | `internal/interfaces/console/hosts.go` | `(*server).updateHost` | `domain/repository` | `Hosts.UpdateHost` |
| 3 | `internal/interfaces/console/hosts.go` | `normalizedHostSN` | `domain/model` | `NormalizeSN` |
| 3 | `internal/interfaces/console/server.go` | `package-init` | `domain/repository` | `Hosts` |
| 4 | `internal/infrastructure/media/delete.go` | `(*Sources).CleanupDeleted` | `domain/model` | `ISO.Validate` |
| 4 | `internal/infrastructure/media/delete.go` | `(*Sources).CleanupDeleted` | `domain/repository` | `CheckID`、`CheckSource`、`ErrConflict`、`ErrInvalid` |
| 4 | `internal/infrastructure/media/delete.go` | `(*Sources).confirmDeleted` | `domain/repository` | `ErrConflict`、`ErrNotFound`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/delete.go` | `cleanupOwnedTree` | `domain/repository` | `ErrConflict` |
| 4 | `internal/infrastructure/media/extract.go` | `(*Sources).extract` | `domain/catalog` | `RepoPaths` |
| 4 | `internal/infrastructure/media/identify.go` | `Identify` | `domain/catalog` | `Arch.Valid`、`FormatMedia`、`Formats`、`SeriesByEdition`、`SeriesMedia`、`SeriesOfFormat` |
| 4 | `internal/infrastructure/media/identify.go` | `Identify` | `domain/model` | `IsSafeWord`、`ValidLabel` |
| 4 | `internal/infrastructure/media/identify.go` | `identifyProblem` | `domain/model` | `NewProblem` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).Import` | `domain/catalog` | `ExtraSourceOf`、`RequiredRoles` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).Import` | `domain/model` | `ISO.Validate` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).Import` | `domain/repository` | `Audit`、`CheckID`、`ErrConflict`、`ErrImmutable`、`ISOSources.SaveSource`、`ISOSources.SourcesOf`、`ISOs.GetISO`、`ISOs.UpdateISO` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).failImport` | `domain/model` | `StorableText` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).failImport` | `domain/repository` | `Audit`、`ISOSources.GetSource`、`ISOSources.SaveSource`、`ISOs.GetISO`、`ISOs.UpdateISO` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).openMedia` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/infrastructure/media/import.go` | `(*Sources).readImportOutcome` | `domain/repository` | `ISOs.GetISO` |
| 4 | `internal/infrastructure/media/import.go` | `importProblem` | `domain/model` | `NewProblem` |
| 4 | `internal/infrastructure/media/import.go` | `publishImportTree` | `domain/repository` | `ErrConflict` |
| 4 | `internal/infrastructure/media/iso.go` | `(*Sources).FetchOne` | `domain/repository` | `Audit`、`ErrConflict`、`ErrInvalid`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/iso.go` | `New` | `domain/repository` | `Store` |
| 4 | `internal/infrastructure/media/iso.go` | `package-init` | `domain/repository` | `Store` |
| 4 | `internal/infrastructure/media/local.go` | `(*Sources).fetchLocal` | `domain/repository` | `Audit`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/netboot.go` | `identifyNetboot` | `domain/catalog` | `Arch.Valid` |
| 4 | `internal/infrastructure/media/recover.go` | `(*Sources).Recover` | `domain/repository` | `ISOQuery`、`ISOSources.ListUnfinishedSources`、`ISOSources.SourcesOf`、`ISOs.GetISO`、`ISOs.ListISOs` |
| 4 | `internal/infrastructure/media/recover.go` | `allFetched` | `domain/catalog` | `ExtraSourceOf` |
| 4 | `internal/infrastructure/media/remote.go` | `(*Sources).fetchRemote` | `domain/repository` | `Audit`、`ISOSources.SaveSource`、`ProblemsError` |
| 4 | `internal/infrastructure/media/remote.go` | `parseContentRange` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/infrastructure/media/remote.go` | `rangeNumber` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).failFetch` | `domain/repository` | `Audit`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).finishFetch` | `domain/model` | `NewProblem` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).finishFetch` | `domain/repository` | `Audit`、`ErrConflict`、`ErrRejected`、`ISOSources.SaveSource`、`ProblemsError` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).getSource` | `domain/repository` | `ErrNotFound`、`ISOSources.GetSource` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).restorePublication` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).resumeOffset` | `domain/repository` | `Audit`、`ErrInvalid`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/source.go` | `(*Sources).writeFetch` | `domain/repository` | `Audit`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/source.go` | `checkSourceKey` | `domain/repository` | `CheckSourceKey` |
| 4 | `internal/infrastructure/media/source.go` | `sourceRejected` | `domain/model` | `NewProblem` |
| 4 | `internal/infrastructure/media/source.go` | `sourceRejected` | `domain/repository` | `ErrInvalid`、`ProblemsError` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).Cancel` | `domain/repository` | `Audit`、`CheckAudit`、`ErrConflict`、`ISOSources.SaveSource`、`ISOSources.SourcesOf`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).Progress` | `domain/repository` | `ISOSources.RangesOf`、`ISOSources.SourcesOf`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).Register` | `domain/repository` | `Audit` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).RegisterQueued` | `domain/repository` | `Audit` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).Reimport` | `domain/repository` | `Audit` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).RequestReimport` | `domain/repository` | `Audit`、`CheckAudit`、`ErrConflict`、`ISOSources.SaveSourceAndAcquire`、`ISOSources.SourcesOf`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).Retry` | `domain/repository` | `Audit`、`CheckAudit`、`ErrConflict`、`ISOSources.SourcesOf`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).register` | `domain/catalog` | `ExtraSourceOf` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).register` | `domain/repository` | `Audit`、`CheckAudit`、`CheckSource`、`ErrConflict`、`ISOSources.GetSource`、`ISOSources.SaveSource`、`ISOSources.SaveSourceAndAcquire`、`ISOs.GetISO` |
| 4 | `internal/infrastructure/media/sources.go` | `(*Sources).retrySource` | `domain/repository` | `Audit`、`ErrInvalid`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/upload.go` | `(*Sources).Upload` | `domain/repository` | `Audit`、`CheckAudit`、`ErrConflict`、`ISOSources.RangesOf`、`ISOSources.SaveRange`、`ISOSources.SaveSource` |
| 4 | `internal/infrastructure/media/upload.go` | `compareRange` | `domain/repository` | `ErrConflict` |
| 4 | `internal/infrastructure/media/windows.go` | `(*Sources).extractWindows` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/interfaces/console/isos.go` | `(*exactChunkReader).Read` | `domain/repository` | `ErrInvalid` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).checkSourceSpace` | `domain/model` | `NewProblem` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).createISO` | `domain/repository` | `CheckNewISO`、`ISOs.CreateISO` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).deprecateISO` | `domain/repository` | `ISOs.DeprecateISO` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).getISO` | `domain/repository` | `ISOs.GetISO` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).isoProgress` | `domain/repository` | `ISOs.GetISO` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).isoProgress` | `interfaces/console` | `ISOSourceState.SourcesOf` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).listISOs` | `domain/catalog` | `Family.Valid`、`Series.Valid` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).listISOs` | `domain/model` | `ISOState.Valid` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).listISOs` | `domain/repository` | `ISOQuery`、`ISOs.ListISOs`、`Page` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).registerISOSource` | `domain/repository` | `ISOs.GetISO` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).sourceOperation` | `domain/repository` | `Audit` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).sourceProgress` | `domain/repository` | `ErrConflict`、`ErrNotFound` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).sourceProgress` | `interfaces/console` | `ISOSourceState.GetSource`、`ISOSourceState.RangesOf` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).uploadChunk` | `domain/repository` | `CheckSourceKey`、`ErrConflict`、`ErrNotFound` |
| 4 | `internal/interfaces/console/isos.go` | `(*server).uploadChunk` | `interfaces/console` | `ISOSourceState.GetSource` |
| 4 | `internal/interfaces/console/isos.go` | `checkISOSource` | `domain/catalog` | `ExtraSourceOf` |
| 4 | `internal/interfaces/console/isos.go` | `checkISOSource` | `domain/repository` | `CheckSource`、`ErrInvalid` |
| 4 | `internal/interfaces/console/isosource.go` | `package-init` | `domain/repository` | `Audit` |
| 4 | `internal/interfaces/console/server.go` | `package-init` | `domain/repository` | `ISOs` |
| 5 | `internal/interfaces/console/audit.go` | `(*server).exportAudit` | `domain/repository` | `AuditEntry`、`AuditLog.ListAudit`、`Page` |
| 5 | `internal/interfaces/console/audit.go` | `(*server).listAudit` | `domain/repository` | `AuditEntry`、`AuditLog.ListAudit`、`Page` |
| 5 | `internal/interfaces/console/audit.go` | `auditFilters` | `domain/repository` | `AuditQuery` |
| 5 | `internal/interfaces/console/audit.go` | `toAuditItem` | `domain/repository` | `AuditEntry` |
| 5 | `internal/interfaces/console/auth.go` | `(*server).login` | `domain/model` | `Role.Valid` |
| 5 | `internal/interfaces/console/auth.go` | `(*server).login` | `domain/repository` | `ErrNotFound`、`Users.GetUser` |
| 5 | `internal/interfaces/console/auth.go` | `HashPassword` | `domain/model` | `StorableText` |
| 5 | `internal/interfaces/console/auth.go` | `HashPassword` | `domain/repository` | `ErrInvalid` |
| 5 | `internal/interfaces/console/perm.go` | `AllPerms` | `domain/access` | `AllPerms` |
| 5 | `internal/interfaces/console/perm.go` | `PermsOf` | `domain/access` | `PermsOf` |
| 5 | `internal/interfaces/console/perm.go` | `package-init` | `domain/access` | `PermAnyUser`、`PermAuditRead`、`PermHookApprove`、`PermHookRead`、`PermHookWrite`、`PermHostRead`、`PermHostWrite`、`PermISORead`、`PermISOWrite`、`PermTaskCancel`、`PermTaskCreate`、`PermTaskRead`、`PermTaskRetry`、`PermTemplateApprove`、`PermTemplateRead`、`PermTemplateWrite`、`PermUserManage` |
| 5 | `internal/interfaces/console/server.go` | `(*server).auditFrom` | `domain/repository` | `Audit` |
| 5 | `internal/interfaces/console/server.go` | `(*server).authenticate` | `domain/model` | `Role.Valid` |
| 5 | `internal/interfaces/console/server.go` | `(*server).authenticate` | `domain/repository` | `ErrNotFound`、`Users.GetUser` |
| 5 | `internal/interfaces/console/server.go` | `package-init` | `domain/repository` | `AuditLog`、`Users` |
| 5 | `internal/interfaces/console/users.go` | `(*server).createUser` | `domain/model` | `Role.Valid`、`StorableText`、`ValidIdent` |
| 5 | `internal/interfaces/console/users.go` | `(*server).createUser` | `domain/repository` | `Users.CreateUser` |
| 5 | `internal/interfaces/console/users.go` | `(*server).disableUser` | `domain/repository` | `Users.GetUser`、`Users.UpdateUser` |
| 5 | `internal/interfaces/console/users.go` | `(*server).listUsers` | `domain/repository` | `Page`、`UserQuery`、`Users.ListUsers` |
| 5 | `internal/interfaces/console/users.go` | `(*server).updateUser` | `domain/model` | `Role.Valid` |
| 5 | `internal/interfaces/console/users.go` | `(*server).updateUser` | `domain/repository` | `Users.GetUser`、`Users.UpdateUser` |
| 6 | `internal/interfaces/console/errors.go` | `RespondError` | `domain/repository` | `BatchError`、`ErrConflict`、`ErrDuplicate`、`ErrImmutable`、`ErrInvalid`、`ErrLastAdmin`、`ErrNotFound`、`ErrReferenced`、`ErrTemplateNotCurrent`、`ProblemsError`、`ReferencedError` |
| 6 | `internal/interfaces/console/errors.go` | `StatusOfProblems` | `domain/model` | `Code.Category` |
| 6 | `internal/interfaces/console/notify.go` | `NewHub` | `infrastructure/notification` | `NewHub` |
| 6 | `internal/interfaces/console/openapi.go` | `(*schemaBuilder).definition` | `domain/catalog` | `AllArchs`、`AllFirmwares`、`AllFormats`、`AllSeries`、`Families` |
| 6 | `internal/interfaces/console/openapi.go` | `(*schemaBuilder).definition` | `domain/model` | `Roles` |
| 6 | `internal/interfaces/console/server.go` | `package-init` | `domain/repository` | `Events`、`Tasks` |
| 6 | `internal/interfaces/console/taskcontrol.go` | `Problems` | `domain/repository` | `ErrRejected`、`ProblemsError` |
| 6 | `internal/interfaces/console/taskcontrol.go` | `package-init` | `domain/repository` | `Audit` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).controlTask` | `domain/repository` | `Tasks.GetTask` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).getTask` | `domain/repository` | `Tasks.GetTask` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).listEvents` | `domain/repository` | `Event`、`Events.ListEvents` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).listTasks` | `domain/model` | `NormalizeSN`、`TaskState.Valid` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).listTasks` | `domain/repository` | `Page`、`TaskQuery`、`Tasks.ListTasks` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).previewTask` | `domain/catalog` | `Firmware.Valid` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).previewTask` | `domain/repository` | `Tasks.GetTask` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).streamAuthorized` | `domain/model` | `Role.Valid` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).streamAuthorized` | `domain/repository` | `Users.GetUser` |
| 6 | `internal/interfaces/console/tasks.go` | `(*server).taskStream` | `domain/repository` | `Event`、`Events.ListEvents`、`Tasks.GetTask` |
| 6 | `internal/interfaces/console/tasks.go` | `toEventItem` | `domain/repository` | `Event` |
