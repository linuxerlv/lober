# 后端模块与向导接口约定

本文固定第一阶段及合入dev后的现有行为。依据为 `internal/interfaces/console/wizard.go`、`wizard_test.go`、网页返回类型及 `internal/infrastructure/persistence/pgstore` 的查询实现。迁移不能改变下面的字段、权限、问题归属、排序或写入结果。

## 五个向导入口

路径前缀都是 `/console/v1/wizard`。请求正文使用 JSON。每个入口都要求 `task:create` 权限：管理员 `admin`、操作员 `operator` 可以访问；查看者 `viewer`、编写者 `hook-author`、审批者 `hook-approver` 返回 403。未登录、会话过期、账号禁用或角色无效返回 401。必须先改密码的账号返回 403。

所有成功响应的网页状态码为 200，外层字段为 `code:0`、`data`、`message:""`。错误外层字段仍为 `code`、`data`、`message`，`code` 与网页状态码相同。没有错误明细时 `data` 为 `null`。下表的返回字段都指 `data` 内部。

| 入口 | 请求字段 | 返回字段 | 错误和问题 | 审计 | 分页和顺序 |
| --- | --- | --- | --- | --- | --- |
| `GET /hosts` | 查询参数 `q`、`attr_key`、`attr_value`、`cursor`、`limit` 可省略；`attr_key` 与 `attr_value` 必须同时提供或同时省略 | `items:[主机字段 + selectable + problems]`、`next` | 非法页大小、游标或只提供一个属性过滤参数为 400。已有未结束任务仍列出主机，但 `selectable:false`，`problems` 含 `host_has_open_task` | 不写 | 默认 100 条，最多 500 条；按规范序列号的字节序升序。`q` 是前缀；能规范化时先规范化。属性按给定键和值精确过滤。`next` 是最后一台主机的位置 |
| `POST /isos` | 必填非空数组 `hostSNs`；可选 `cursor`、`limit` | `items:[镜像字段 + usable + problems]`、`next` | 请求不完整为 400。序列号非法为问题列表。重复或未登记主机为 422，明细为 `common/byHost`。候选平台不兼容放入该镜像的 `problems`，不会使整个响应失败 | 不写 | 只查准备完成的镜像，按名称、编号的字节序升序；默认 100、最多 500 条。平台不同的镜像仍列出并标成不可用。每个镜像的问题按输入主机顺序列出 |
| `POST /templates` | 必填 `isoID`；可选 `cursor`、`limit` | `items:[模板字段 + usable:true]`、`next` | 请求非法为 400；镜像不存在为 404；镜像不可用按问题类别返回 409 或 422。模板本身不适配、缺选项集合、引用废弃组件或不在当前内置发布清单时，从候选中移除 | 不写 | 每个模板只选最高的已审批版本，新草稿不会顶替它；不回退到更旧的已审批版本。按规范名称键、编号的字节序升序，默认 100、最多 500 条。先对仓储候选分页，再剔除不可用项，故页面可能不足一页甚至为空，但 `next` 仍可能非空 |
| `POST /params` | 必填 `isoID`、`templateID`、正整数 `templateVersion`、非空 `hostSNs`；可选 `entered:{序列号:{字段键:字符串}}` | `hosts:[{sn,fields,problems}]`、`common` | 请求非法、未知输入行或规范化后重复的输入行返回 400。选中的模板或镜像不存在返回 404；不是当前已审批版本或内置发布已过期返回 409。一般准入和参数问题随 200 响应放入 `common` 或各主机的 `problems`；不能把它们改成整页失败 | 不写；拒绝的输入也不写任务或审计 | 不分页；主机顺序与 `hostSNs` 一致，序列号规范化。按序列号匹配 `entered`，不能靠映射的遍历顺序分配。字段顺序沿用参数求值结果；缺失主机或整批有公共问题时该主机字段为空 |
| `POST /tasks` | 必填 `isoID`、`templateID`、正整数 `templateVersion`、非空 `hosts:[{sn,entered?}]`；`entered` 为字段键到字符串的映射 | `tasks:[任务字段]` | 请求非法为 400；对象不存在为 404；模板不是当前已审批版本或当前内置发布为 409。整批准入或参数失败为 422，返回 `common/byHost`；任何主机失败，一台都不创建。仓储冲突沿用 409，未知内部错误为 500 | 每台成功任务各写一条 `task.create`。操作者取当前登录账号，时间取注入时钟；请求中的 `createdBy/createdAt` 不生效。任务与审计同一事务，失败不留部分记录；只有成功提交后才通知订阅者 | 不分页；返回任务保持 `hosts` 输入顺序。数据库内部为防止互相等待而排序加锁，不能因此重排输入参数或返回快照 |

`cursor` 是不透明的位置字符串，调用方原样传回。没有下一页时 `next` 为 `""`。`limit:0` 与省略相同；负数或大于 500 为 400。前四个入口没有写入审计的路由动作；第五个入口使用 `task.create`。

上表前三个候选列表还支持dev已有的页码模式：GET查询参数和POST正文均使用 `page/page_size`。提供页码字段就进入该模式，不能同时提供 `cursor/limit`；默认第1页、每页20条，页码和大小必须是正整数，每页最多500条，偏移量不能溢出。页码响应包含 `total`，游标响应不包含它。模板页码模式先过滤不可用候选，再计数和分页；游标模式仍保持上表的先分页后过滤。主机和镜像页码模式保持相同筛选、排序和可用性标记。

模板候选的唯一资格规则是 `domain/selection.TemplateEligible`，只组合 `CurrentBuiltin` 与 `admission.TemplateUsable`。最高审批版本先选出再判资格；不因它不合格而回退旧审批版本，也不把激活诊断加进资格。

页码向导调用 `ListTemplateCandidates`，每项带模板自身精确引用的组件。应用合并页内组件为同一只读映射，用原来的激活诊断生成 `Problems`，不再次调用 `GetHooks`。权限→镜像读取及可用性→分页检查的顺序不变。游标向导仍先 `ListTemplates` 分候选页，再 `GetHooks` 和领域资格筛选，因此空页仍可能有非空 `next`。

`ListTemplateCandidates` 必须提供 `Selection`。兼容的 `ListTemplates(Selection)` 委托同一扫描器，只剥离组件事实；无 `Selection` 的普通查询保持原逻辑。仓储直接使用 `Selection` 的游标查询先筛资格再取页，最多找到 `limit+1` 个合格项即可结束；它与向导游标的先分页后筛选是两个原有入口顺序。

单次候选查询在私有只读 `REPEATABLE READ` 事务中读取。模板按规范名称键、编号的字节序每批256项，用上一批最后位置续读；每批只读取去重后的精确组件版本，每批最多1000项。所有模板与组件批共用同一事务。页码为计算完整合格总数必须读完所有候选及其引用，代价是全量扫描；这里只保留当前批和目标页最多500项及它们各自引用，不保证更快。

这只保证一次页码查询内部的模板、组件和总数使用同次事实。ISO与Manifest仍是应用先取得的输入，不保证与库中事实同刻。查询不持写锁，不承诺跨请求、跨页快照，也不保证展示后仍能提交；提交入口继续重查。发生读取错误或取消时返回错误，不交付半页或半总数。不改写事务的 `READ COMMITTED` 级别，不向应用暴露通用事务。

## 返回字段

| 类型 | 字段 |
| --- | --- |
| 主机 | `sn, arch, firmware, attrs, rev`；空属性返回 `{}` |
| 镜像 | `id, name, family, series, version, arch, state, sha256, artifacts, offers, rev`；`offers` 的每个选项含 `value,label` |
| 模板 | `id, version, name, state, author, approver, builtin, derivedFrom, family, series, body, params, bindings, assembly, problems, rev`；`derivedFrom` 为 `null` 或 `{id,version}`。`params/bindings/assembly` 沿用编写接口的网页字段，不直接输出数据库记录 |
| 模板参数声明 `params[]` | `name,label,type,list,required,default,options,min,max,offer`；选项为 `value,label`，未设数值上下限时 `min/max` 为 `null` |
| 模板绑定 `bindings` | 参数名到 `{source,value,attr,editable}` 的映射 |
| 模板装配 `assembly` | 插槽名到实例数组的映射；实例含 `hookID,hookVersion,timeoutSec,required,bindings`。`timeoutSec/required` 可为 `null`，实例绑定仍为上述绑定字段 |
| 参数字段 `fields[]` | `key,label,type,list,required,locked,prefill,options`；选项含 `value,label`。字段键如 `tpl/hostname`，输入值是字符串；列表按换行解析。`prefill` 是绑定或默认来源的预填值，不能把它当成用户刚输入的值 |
| 任务 | `id,hostSN,isoID,templateID,templateVersion,state,snapshot,createdBy,createdAt,rev,canCancel,canRetry`；新建任务为 `pending` |
| 冻结快照 `snapshot` | `schema,family,series,format,host,iso,template,slots`。主机为 `sn,arch,firmware,attrs`；镜像为 `id,name,version,arch,artifacts`；模板为 `id,version,name,body,params` |
| 快照参数值 | `type,list,items`；`items` 是解析后的字符串数组，不能把两台主机的值互换 |
| 快照插槽 `slots[]` | `seq,name,exec,timing,instances`；实例含 `hookID,hookVersion,hookName,kind,body,interpreter,timeoutSec,required,digest,params` |
| 问题 | `code,subject,message,next`；`subject` 含 `kind,id,path,line,col`。保留机器可识别的编号和定位字段，不能只留下提示文字 |

## 问题归属与批量失败

普通问题明细为 `{problems:[问题]}`。第一条问题属于不兼容或状态类别时返回 409，其他类别返回 422。批量失败明细为 `{common:[问题],byHost:{规范序列号:[问题]}}`，返回 422；`common` 存镜像与模板问题，`byHost` 存各主机的问题。空问题数组输出 `[]`。

批量错误提示优先取 `common` 第一项；没有公共问题时按主机序列号的字典序找第一项。`byHost` 是映射，本身不承诺网页键的排列顺序，不能按它重建输入顺序。

| 问题来源 | 现有例子和处理 |
| --- | --- |
| 主机选择 | `host_has_open_task`、`host_duplicate_in_batch`、`host_not_registered` |
| 平台兼容 | `arch_mismatch`、`firmware_mismatch`、`host_platform_invalid`；镜像不可用时先报镜像问题，不拿空架构制造平台冲突 |
| 模板准入 | `template_not_approved`、`template_blocked_by_deprecated`、`series_not_ticked`、`iso_offer_missing` |
| 参数求值 | `param_value_invalid`、`param_required`、`param_locked`、`param_unknown_field`、`param_attr_missing`、`param_attr_unusable`、`param_not_offered`、`param_sub_unavailable`；沿用领域规则的完整问题列表 |
| 选择过期 | 新已审批版本已替代当前选择、模板或引用的内置版本不在随程序发布的清单中，返回 409；旧任务的冻结快照仍可读取 |

重复主机仍是整批错误，不允许“第一台成功，重复项失败”。参数预览用规范化序列号找输入行；提交用每个 `hosts[]` 项自身的参数生成快照。`SN-2,SN-1` 必须各自保留 `node-two,node-one`，响应和数据库中的快照都要一致。

当前版本选择通过 `CurrentTaskCreator.CreateCurrentTasks` 在同一个数据库事务内确认。任务应用的 `port.Tasks` 只提供 `FindOpenTask/CreateCurrentTasks`，由组装显式注入；网页通过 `console.Deps.Wizard` 调用应用，不退回普通 `Tasks.CreateTasks`。后者继续允许明确指定旧的已审批版本。

向导事务依次锁定：模板身份（读锁）→ 按序列号字节序排列的主机（写锁）→ 镜像（读锁）→ 所选模板版本（读锁）→ 排序后的引用组件版本（读锁）→ 插入任务和审计。模板状态变更先锁身份（写锁），再锁目标版本，审批时再锁引用组件。保护一直持有到提交或回滚。

审批或废弃先于向导身份保护提交时，向导重查 `model.CurrentApproved`。所选版本已失效就报 `ErrTemplateNotCurrent`（同时属于 `ErrConflict`），网页返回 409、原有 `ConsoleTemplateCurrent` 提示、`data:null`，不留任务或任务审计。已不存在的身份或版本仍报 404。当前选择失效优先于一般准入问题；其他准入失败仍为 `BatchError` 和 422。这是独立并发修复对旧检查空隙的校正，不是目录迁移的附带行为变化。

向导已经保护模板身份时，新版本审批和同版本废弃都等待任务事务完成。数据库锁等待由两个独立服务和连接池验证。普通 `CreateTasks` 保持主机→镜像→模板版本→引用组件的顺序，不新增当前版本限制。

内置装载、开新版本、删除草稿已经先锁身份；改名只锁身份。保存草稿只锁版本，不再等待身份；派生只普通读取来源，再创建独立身份，因此没有版本→旧身份的反向等待。模板状态变更保留先校验版本键、再检查存在性、修订号、状态和引用组件的错误顺序。

## 模块边界和临时放行

最终按接口、应用、领域、基础设施四层组织。向导五个操作进入任务应用模块；规则、网页字段和数据库记录各自独立。应用模块只通过已登记的小接口取得其他模块的能力，不直接导入其他模块的具体服务。随机编号、当前时间、文件、网络和数据库操作不进入领域层。

未迁移控制台的临时放行清单是 `internal/archtest/migration_test.go` 中的 `migrationExceptions`。每条同时写 `Caller`、`File`、`Function`、`Target`、`Symbol`、`RemoveIn`。调用包与目标包用完整导入路径；文件相对模块根；方法写作 `(*server).listHosts`；包级类型声明固定写 `package-init`。经仓储接口调用的方法写作 `Hosts.ListHosts`，普通符号写作 `Page`。来源读取使用的是控制台自有的 `ISOSourceState` 接口，登记其真实目标，不能冒充 `repository.ISOSources` 调用。

例如，`internal/interfaces/console/hosts.go` 的 `(*server).listHosts` 仅允许访问 `github.com/lober/lober/internal/domain/repository` 的 `Hosts.ListHosts`、`HostQuery`、`Page`，清除阶段为 `3`。同包的 `createHost`、新文件或另一符号不能借用这条放行。清单为固定条目，不从待检查的代码自动生成许可。

控制台与镜像清单已接入全模块检查。检查先确定四层依赖，再用实际类型识别每一次仓储方法和领域判定访问。局部变量别名、保存成方法值、同包新函数和同名新文件都不能借旧许可；装载失败和类型错误直接失败。许可只来自源文件中的固定条目，不根据被检查代码自动生成。

| 总路线清除阶段 | 本次登记的现有代码 |
| --- | --- |
| `1` 四层基础与向导 | 五个操作已进入任务应用服务，全部向导临时放行已删除；网页只解析输入和转换结果 |
| `2` 编写与预览 | 32条入口已通过编写应用；阶段2许可已清零，组件和模板仓储不再注入网页 |
| `3` 主机 | `hosts.go` 中逐条登记的查询、登记、编辑、删除、导入，以及主机依赖字段 |
| `4` 镜像与后台作业 | `isos.go`、`isosource.go` 中逐条登记的旧仓储和来源读取符号，以及镜像依赖字段 |
| `5` 身份权限与审计 | 登录、认证、用户管理、权限、审计查询和网页角色描述的精确符号 |
| `6` 任务查询与收尾 | 任务查询、事件流、控制和共享错误转换中尚存的精确旧符号；最后全部清零 |

旧 `templateHooks`、`templateProblems`、`ProblemsOf`、`HookProblems` 和 `renderPreview` 生产桥已删除。向导和编写入口不得经同包辅助重回旧业务。剩余阶段继续逐项清理；新业务文件、函数和符号默认不在放行清单中。

## 已固定的验收例子

`Test向导参数输入顺序不串主机` 使用反序主机和不同参数，检查网页响应与数据库冻结快照。`Test向导参数输入按规范SN定位并拒绝未知行` 检查未知行、重复规范化键返回 400，且任务、审计都不增加。

现有完整五步、整批拒绝、候选分页、当前已审批版本、五角色权限、内置发布选择和并发批次测试继续保留。真实数据库不可用时必须记录失败，不能跳过后声称验收完成。

## 领域迁移后的职责

`internal/core/*` 已迁到 `internal/domain/*`，`internal/render` 已迁到 `internal/domain/render`，原测试与样例一起保留。`internal/store` 根接口、验证、查询和错误契约已迁到 `internal/domain/repository`，接口按主机、镜像、编写、任务、事件、审计和用户拆文件。`Store` 只供组装与未迁移模块使用，新增应用服务使用小接口。

用户、五角色、镜像来源和已收取字节区间由 `domain/model` 定义。用户模型没有网页或数据库标签；控制台通过现有 `userResponse` 转换，数据库继续使用独立记录。所有用户响应仍排除密码，保留原八项字段，以及dev新增的 `email/avatar/bio/menuIds/menuRev`，共13项。

权限的唯一规则在 `domain/access`，控制台旧权限名字只精确转发，身份阶段清除。`domain/selection.CurrentBuiltin` 只消费显式 `Manifest` 和模型数据；旧发布不能新选，已存任务仍读取冻结快照。组件引用提取只有 `model.HookRefs` 一份实现，`repository.HookRefs` 暂时委托兼容。

随机编号只有 `infrastructure/identity.NewID` 一份实现，仍为 128 位随机数编码的 26 字符小写 base32。它是没有业务依赖的叶子工具，公开函数就是这一项小能力契约。旧路径的六条临时豁免已删除；`internal/archtest/identity_rules_test.go` 的正式 `identityCapabilities` 只允许新路径中 `CreateHook`、`CreateISO`、`CreateTemplate`、`DeriveTemplate`、`createTasks`、测试夹具 `newSchema` 调用 `NewID`。正式能力没有清除阶段，不扩大为整个数据库包许可。点导入、包级函数值、其他函数和其他文件都不能借用；数据库不能另用随机数库，领域继续禁止随机操作。


## 外部实现搬迁后的边界

`pgstore` 位于 `internal/infrastructure/persistence/pgstore`，`pgtest` 仍是其测试子包。原数据库记录、事务、表结构和测试一起搬迁。普通 `Tasks.CreateTasks` 仍能保存指定的旧审批版本；`CurrentTaskCreator.CreateCurrentTasks` 保留独立事务保护。目录变化不修改数据库名称、约束或错误优先级。

内置正文位于 `internal/infrastructure/builtin`，七种格式、共享正文、发布清单、指纹、样例和验证工具完整保留。它仍只做纯内容构建；允许嵌入正文，不允许文件、网络、时钟、随机数或上下文异步能力。纯渲染仍在 `domain/render`。

镜像目录是 `internal/infrastructure/media`，包名仍为 `iso`，其 `iso9660` 与 `isotest` 子包一起搬迁。旧 `Sources` 仍承担获取、导入、状态和恢复流程；这不是已完成的终态拆分。固定 `mediaMigrationExceptions` 精确列出文件、函数和目标符号，清除阶段为总路线第4阶段。新函数不能使用这张旧清单，也不能导入应用具体服务。

网页入口位于 `internal/interfaces/console`。原非向导仓储调用仍由 `migrationExceptions` 逐条登记；旧领域判定和渲染在 `consoleRuleMigrationExceptions` 中按所属模块的清除阶段登记。任务5已撤销全部向导豁免；实际符号检查同时追踪同包辅助函数、方法值和包级函数别名，不能经旧转换函数重新执行业务规则。

`application/tasks.Wizard` 提供五个向导操作。每个操作先检查注入的 `ports.Actor`；权限仍由 `domain/access` 唯一规则决定。模板候选的 `Problems` 和提交结果的 `CanCancel/CanRetry` 在应用计算，网页只做字段转换。任务诊断只通过 `port.TaskDiagnostics` 注入；未提供时保持空诊断，不新增执行规则。

生产组装创建一个 `infrastructure/selection.NewGuard()`，经共同 `ports.SelectionGuard` 同时注入任务应用和模板应用。模板审批、废弃在写入与补读期间持写锁，任务提交持读锁。网页没有锁字段或默认锁。它只负责同进程协调，数据库的当前版本事务保护继续独立生效。`console.Clock` 是 `ports.Clock` 别名；每个候选任务的创建时间和整批审计时间仍按原位置取时钟，成功提交后才逐任务通知。

`application/ports.Notifier` 是唯一共同通知契约，保持 `Publish(string)` 和 `Subscribe(string) (<-chan struct{}, func())`。实现位于 `infrastructure/notification`，使用锁与一次性退订保护，多次提示合并为一个；消费者收到提示后补读已提交事件。事务失败或取消不能发布。此次不改变原来的发布位置。

生产组装直接调用 `notification.NewHub`。为保留旧构造时未传通知的默认行为，`console.Notifier` 暂作契约别名，`console.NewHub` 仅精确转发；这一条基础设施访问单列 `notificationMigrationExceptions`，第6阶段删除。网页层其他函数和文件不能借此导入通知实现。

全模块自动检查只登记明确的四层模块。应用模块只导入领域、共同能力和自己的接口子包；基础设施只导入实现所需的明确能力接口，不能导入网页层或应用具体服务。接口名字类似 `ports`、接口目录中的未知子包、新的未登记实现都不能绕过。`cmd/lober` 继续负责组装，Wire 生成文件由工具生成。

编写与预览的 32 条真实路由、错误顺序、分页和来源选择，见[编写与预览契约](authoring-preview.md)。

主机六条真实路由、两种分页、错误顺序与 CSV 预演和提交，见[主机登记与 CSV 契约](hosts.md)。

## 第一阶段收尾快照

源码任务1至5已验收，五个向导操作进入任务应用，向导临时放行数为0。下表330条记录功能分支验收时的快照；合入dev后增加36项既有访问、删除6项旧许可，集成时为360条，见[集成记录](../superpowers/reviews/2026-10-22-ddd-develop-integration.md)。这些是后续阶段的明确过渡职责，不能据此称整个后端已经拆完。
正式编号能力仍限原六个函数，不计入过渡清单。以下数量只记录现状，不给新增函数授权；唯一执行规则仍在架构检查源码中。

| 固定清单 | 条数 | 清除阶段和条数 |
| --- | --- | --- |
| migrationExceptions | 134 | 2：48；3：10；4：24；5：24；6：28 |
| mediaMigrationExceptions | 123 | 4：123 |
| consoleRuleMigrationExceptions | 72 | 2：23；3：6；4：5；5：26；6：12 |
| notificationMigrationExceptions | 1 | 6：1，仅console.NewHub精确转发 |

逐文件、函数和符号清单，以及提交历史、裁定、失败修复和保留独立并发修复的回退办法，见[第一阶段证据](../superpowers/reviews/2026-10-22-ddd-phase1-evidence.md)。
Task6只补文档，不改数据库结构、网页字段、前端或生产行为。普通CreateTasks保存指定旧审批版本与当前选择原子创建的区别继续保留。

## 第二阶段边界

编写网页只依赖 `authoring.Hooks`、`authoring.Templates`、`authoring.Editor` 和共用 `ports.FrozenPreview`。请求解析与字段转换留在网页；权限、版本读取、诊断、审批和预览编排在应用。公开网页类型和JSON字段保持原样。

`authoring/port` 的组件、模板、主机和镜像接口只声明实际所需的方法。应用不能借总仓储或其他应用的具体实现调用业务。`media.BootConfigReader` 实现冻结介质配置读取；`FrozenPreview.RenderFrozen` 只消费已保存的快照，不选最新模板，也不拿真实任务令牌构造示例。

类型检查的根集合覆盖向导和编写的全部处理文件，并沿同包函数、方法值、局部与包级别名追踪。只允许已登记的应用方法和 `FrozenPreview.RenderFrozen`；小仓储接口、通知和任务诊断仍拒绝。错误转换只允许原 `StatusOfProblems` 调用 `Code.Category`，不能借错误转换回调仓储。

阶段2累计删除83项精确许可：仓储清单55项，网页规则清单28项；其中78项由Task2–5逐步删除，本次收尾删最后5项。没有转移清除阶段或新增临时许可。向导和阶段2许可均为0，当前总数277。

| 固定清单 | 当前条数 | 剩余清除阶段 |
| --- | --- | --- |
| migrationExceptions | 98 | 3：10；4：24；5：35；6：29 |
| mediaMigrationExceptions | 125 | 4：125 |
| consoleRuleMigrationExceptions | 53 | 3：6；4：5；5：30；6：12 |
| notificationMigrationExceptions | 1 | 6：1，仍仅console.NewHub |

六项正式编号能力不变。完整检查与独立审查状态见[第二阶段证据](../superpowers/reviews/2026-10-23-ddd-phase2-evidence.md)。本地候选提交不代表已合入develop或通过企业和人工验收。


## 第三阶段主机边界

`application/hosts.Registry` 只提供 List/Get/Create/Update/Delete/Import 六方法。网页的 `hosts.go` 和 `csv.go` 加入既有类型追踪根；纯请求、响应转换留在网页。错误转换只读取 CSVFileError/ImportRejectedError 的字段，不增加 Error 方法许可。

`hosts/port.Hosts` 明确声明原仓储七个方法。主机写入仍由原数据库事务检查版本、引用和审计；整批导入不逐条提交。任务与编写的主机读口保持独立，不转调主机服务。

阶段3累计删除仓储许可10项、网页规则许可6项，277降为261。阶段1、2、3与向导剩余均为0。架构测试对真实四清单固定数量，并用起点逐项内容摘要核对：补回精确删除的16项后，必须等于起点，其他阶段不能改动。

| 固定清单 | 当前条数 | 剩余清除阶段 |
| --- | --- | --- |
| migrationExceptions | 88 | 4：24；5：35；6：29 |
| mediaMigrationExceptions | 125 | 4：125 |
| consoleRuleMigrationExceptions | 47 | 4：5；5：30；6：12 |
| notificationMigrationExceptions | 1 | 6：1，仍仅console.NewHub |

六项正式编号能力和唯一通知转发的原检查保持。模块分支完整验证、逐项独立审查及整分支审查均通过，实际状态见[第三阶段证据](../superpowers/reviews/2026-10-23-ddd-phase3-evidence.md)。用户随后明确集成目标为 `dev`，本地合并及合并后验证见[dev 集成记录](../superpowers/reviews/2026-10-23-ddd-phase3-dev-integration.md)；后续企业、人工验收另行推进。


## 第六阶段任务读取、控制和事件边界

七个任务入口均已通过任务应用：`application/tasks.Reader` 负责列表、详情和冻结预览，`Controller` 负责取消和重试，`EventReader` 负责事件查询和订阅。网页只解析请求、取得可信身份、转换结果和处理连接；生产与测试显式组装三个入口，不从旧仓储、控制或通知依赖回退。控制仍由原执行服务原子保存状态、执行记录、事件和审计，通知只提示补读已提交事实。

本阶段精确删除最后39项过渡许可，当前 `migrationExceptions`、`mediaMigrationExceptions`、`consoleRuleMigrationExceptions`、`notificationMigrationExceptions` 四表逐表为0；冻结39项和更早68/154项历史记录、摘要及六项正式编号能力不变。错误说明与公开枚举经应用契约提供，内置初始化归编写应用，机器渲染、观察策略和密钥读写分别归所属应用或基础设施；启动入口保留构造、普通字段转换和启停。

七条路径、错误先后、空数组、审计及流行为见[任务读取、控制和事件约定](task-read-control-events.md)。模块分支的原完整验收和整分支代码审查已通过；证据与范围见[第六阶段验证证据](../superpowers/reviews/2026-10-24-ddd-phase6-evidence.md)及[最终审查记录](../superpowers/reviews/2026-10-24-ddd-phase6-final-review.md)。此前各阶段表格保留为当时的历史状态，不作为当前许可。

## 向导应用共同边界

`tasks.Wizard` 各方法旁的引用把本节和上文对应入口连接为该操作的完整约定；[能力导航](application-capabilities.md)只指路，不另定规则。应用输入为 Go 类型，不处理网页字段是否出现、JSON 绑定或 HTTP 状态；这些仍由网页入口负责。可信身份与 `task:create` 先于应用参数检查。

Hosts、ISOs、Templates、Params 都只读取事实和计算结果，不写任务、审计或通知。因此没有原子写入、提交时复查或版本冲突重试。重复查询可看到新事实；各次依赖读取之间、不同页之间都没有共同快照。候选不是提交成功的保证，Submit 必须重新准入。

向导不自动重试提交。相同主机重复提交仍由开放任务约束拒绝，不能拿旧输入默默重建。Submit 的结果、锁内当前版本复查、整批任务和审计以及提交后逐任务通知，唯一沿用上文“问题归属与批量失败”；注入诊断缺失时保留空诊断，存在时先取消、再重试诊断。

所有方法将原上下文传给仓储；等待进程选择锁不能立即取消，取得锁后仍把取消状态交给仓储。纯计算不新增中断或计时器。提交成功后不追加取消检查，不撤销已保存任务；数据库与审计失败不通知。列表与结果类型中的空集合由既有转换输出 `[]/{}`，`next` 末页为空串，`total` 只在页码模式存在。

### 单次模板候选读取的后续细化

当前页码候选会在仓储中读取模板及组件事实，再过滤、计数和分页，但这些分步读取目前没有固定为同一次一致读取。后续 Task6 将只收紧这一私有查询内部的事实一致性，保证同一次候选筛选、总数和页面使用同一组事实。此处记录目标，尚未实现；不承诺跨页快照，也不改变普通写事务和 Submit 的现有复查。
