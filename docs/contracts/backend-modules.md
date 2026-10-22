# 后端模块与向导接口约定

本文固定第一阶段迁移前的现有行为。依据为 `internal/interfaces/console/wizard.go`、`wizard_test.go`、网页返回类型及 `internal/infrastructure/persistence/pgstore` 的查询实现。迁移不能改变下面的字段、权限、问题归属、排序或写入结果。

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
| `2` 编写与预览 | `hooks.go`、`templates.go`、`lifecycle.go`、`preview.go`、`slots.go` 中逐条登记的仓储访问，以及对应依赖字段 |
| `3` 主机 | `hosts.go` 中逐条登记的查询、登记、编辑、删除、导入，以及主机依赖字段 |
| `4` 镜像与后台作业 | `isos.go`、`isosource.go` 中逐条登记的旧仓储和来源读取符号，以及镜像依赖字段 |
| `5` 身份权限与审计 | 登录、认证、用户管理、权限、审计查询和网页角色描述的精确符号 |
| `6` 任务查询与收尾 | 任务查询、事件流、控制和共享错误转换中尚存的精确旧符号；最后全部清零 |

共享 `templateHooks` 仍供未迁移的编写和预览入口使用，保留阶段 `2` 的精确条目。向导迁移后不得再用这个兼容入口。各阶段清理对应条目；新业务文件、函数和符号默认不在放行清单中。

## 已固定的验收例子

`Test向导参数输入顺序不串主机` 使用反序主机和不同参数，检查网页响应与数据库冻结快照。`Test向导参数输入按规范SN定位并拒绝未知行` 检查未知行、重复规范化键返回 400，且任务、审计都不增加。

现有完整五步、整批拒绝、候选分页、当前已审批版本、五角色权限、内置发布选择和并发批次测试继续保留。真实数据库不可用时必须记录失败，不能跳过后声称验收完成。

## 领域迁移后的职责

`internal/core/*` 已迁到 `internal/domain/*`，`internal/render` 已迁到 `internal/domain/render`，原测试与样例一起保留。`internal/store` 根接口、验证、查询和错误契约已迁到 `internal/domain/repository`，接口按主机、镜像、编写、任务、事件、审计和用户拆文件。`Store` 只供组装与未迁移模块使用，新增应用服务使用小接口。

用户、五角色、镜像来源和已收取字节区间由 `domain/model` 定义。用户模型没有网页或数据库标签；控制台通过现有 `userResponse` 转换，数据库继续使用独立记录。所有用户响应仍排除密码，保留 `passwordChangeRequired` 等八个字段。

权限的唯一规则在 `domain/access`，控制台旧权限名字只精确转发，身份阶段清除。`domain/selection.CurrentBuiltin` 只消费显式 `Manifest` 和模型数据；旧发布不能新选，已存任务仍读取冻结快照。组件引用提取只有 `model.HookRefs` 一份实现，`repository.HookRefs` 暂时委托兼容。

随机编号只有 `infrastructure/identity.NewID` 一份实现，仍为 128 位随机数编码的 26 字符小写 base32。它是没有业务依赖的叶子工具，公开函数就是这一项小能力契约。旧路径的六条临时豁免已删除；`internal/archtest/identity_rules_test.go` 的正式 `identityCapabilities` 只允许新路径中 `CreateHook`、`CreateISO`、`CreateTemplate`、`DeriveTemplate`、`createTasks`、测试夹具 `newSchema` 调用 `NewID`。正式能力没有清除阶段，不扩大为整个数据库包许可。点导入、包级函数值、其他函数和其他文件都不能借用；数据库不能另用随机数库，领域继续禁止随机操作。


## 外部实现搬迁后的边界

`pgstore` 位于 `internal/infrastructure/persistence/pgstore`，`pgtest` 仍是其测试子包。原数据库记录、事务、表结构和测试一起搬迁。普通 `Tasks.CreateTasks` 仍能保存指定的旧审批版本；`CurrentTaskCreator.CreateCurrentTasks` 保留独立事务保护。目录变化不修改数据库名称、约束或错误优先级。

内置正文位于 `internal/infrastructure/builtin`，七种格式、共享正文、发布清单、指纹、样例和验证工具完整保留。它仍只做纯内容构建；允许嵌入正文，不允许文件、网络、时钟、随机数或上下文异步能力。纯渲染仍在 `domain/render`。

镜像目录是 `internal/infrastructure/media`，包名仍为 `iso`，其 `iso9660` 与 `isotest` 子包一起搬迁。旧 `Sources` 仍承担获取、导入、状态和恢复流程；这不是已完成的终态拆分。固定 `mediaMigrationExceptions` 精确列出文件、函数和目标符号，清除阶段为总路线第4阶段。新函数不能使用这张旧清单，也不能导入应用具体服务。

网页入口位于 `internal/interfaces/console`。原非向导仓储调用仍由 `migrationExceptions` 逐条登记；旧领域判定和渲染在 `consoleRuleMigrationExceptions` 中按所属模块的清除阶段登记。任务5已撤销全部向导豁免；实际符号检查同时追踪同包辅助函数、方法值和包级函数别名，不能经旧转换函数重新执行业务规则。

`application/tasks.Wizard` 提供五个向导操作。每个操作先检查注入的 `ports.Actor`；权限仍由 `domain/access` 唯一规则决定。模板候选的 `Problems` 和提交结果的 `CanCancel/CanRetry` 在应用计算，网页只做字段转换。任务诊断只通过 `port.TaskDiagnostics` 注入；未提供时保持空诊断，不新增执行规则。

生产组装创建一个 `SelectionGuard`，同时注入任务应用和旧模板审批、废弃入口。过渡实现仍位于 console，编写模块迁移时移出。它只负责同进程协调，数据库的当前版本事务保护继续独立生效。`console.Clock` 是 `ports.Clock` 别名；每个候选任务的创建时间和整批审计时间仍按原位置取时钟，成功提交后才逐任务通知。

`application/ports.Notifier` 是唯一共同通知契约，保持 `Publish(string)` 和 `Subscribe(string) (<-chan struct{}, func())`。实现位于 `infrastructure/notification`，使用锁与一次性退订保护，多次提示合并为一个；消费者收到提示后补读已提交事件。事务失败或取消不能发布。此次不改变原来的发布位置。

生产组装直接调用 `notification.NewHub`。为保留旧构造时未传通知的默认行为，`console.Notifier` 暂作契约别名，`console.NewHub` 仅精确转发；这一条基础设施访问单列 `notificationMigrationExceptions`，第6阶段删除。网页层其他函数和文件不能借此导入通知实现。

全模块自动检查只登记明确的四层模块。应用模块只导入领域、共同能力和自己的接口子包；基础设施只导入实现所需的明确能力接口，不能导入网页层或应用具体服务。接口名字类似 `ports`、接口目录中的未知子包、新的未登记实现都不能绕过。`cmd/lober` 继续负责组装，Wire 生成文件由工具生成。
