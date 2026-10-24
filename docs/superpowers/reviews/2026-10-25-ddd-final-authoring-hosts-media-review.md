# 编写与预览、主机、镜像与后台作业独立审查

> 本文是固定提交的独立源码审查记录。原文中的运行状态按审查完成时点保留；根端随后完成的正式VM验证、独立反例及对范围的裁定统一见[总审计](2026-10-25-ddd-final-audit.md)。不把作者读到测试源码等同于作者现场重跑。

审查日期：2026-10-25。受审目录：`E:/src/lober/.worktree/rbac-integration`。固定提交：`2e64ef1d8fe2976ff69fd0634797103810418c86`。

结论：三个模块的真实请求入口已经进入应用层，数据库原子写入、审批锁、主机并发保护和冻结预览均有实际实现，并非只搬目录。镜像导入仍有一处取消错误被改成业务错误的缺口；镜像分页说明另有一处与实现不符。不能判定“全部契约已完整通过”。

本模块独立问题数量：**Critical 0 / Important 1 / Minor 1**。另有“方法旁契约说明缺少明确委托”的全模块共性问题，交总审查统一计数，未在这里重复累计。取消问题由源码路径确认，已提供独立 VM 复核用例，本审查未执行该用例。后续 VM 结果应追加到总报告，不应把本报告当作测试通过记录。

## 本轮证据范围

- 只读生产源码、测试源码、设计、契约、阶段2/3/4计划与 Git 状态。没有执行本机 Go、gofmt、Node、构建或测试；没有连接 VM，也没有读私人连接或 DSN 文件。
- 已核对 HEAD。原有 `ui/dist` 两项修改未纳入受审范围，没有修改受审目录、Git 状态或源码。
- 旧阶段报告只作为背景。本轮“实现相符”指源码与契约相符；“有测试”指当前提交里存在检查该行为的实际测试，不代表本轮运行通过。
- 主要依据：设计第6节的八项契约要求、第7节的事务及最新事实要求；`docs/contracts/{authoring-preview,hosts,media}.md`；对应阶段计划的范围。没有要求每个方法机械重复八个标签。
- 这里的 Important/Minor 是问题等级。契约完整性、实现和测试情况分别在下表说明，不复用这些等级的缩写。

## 公开应用操作和能力清单

### 编写与预览

| 公开入口 | 完整操作清单 | 真实调用者 |
| --- | --- | --- |
| `Hooks`，`hooks_contract.go:47` | List、Versions、Get、Create、SaveDraft、NewVersion、DeleteDraft、Rename、Submit、Approve、Reject、Deprecate、Validate，共13项 | `interfaces/console/hooks.go:78`、`lifecycle.go:20` |
| `Templates`，`templates_contract.go:37` | Derive、List、Versions、Get、Create、SaveDraft、NewVersion、DeleteDraft、Rename、Submit、Approve、Reject、Deprecate、Validate，共14项 | `interfaces/console/templates.go:99`、`lifecycle.go:30` |
| `Editor`，`editor_contract.go:12` | Slots、Candidates、SourceDiff、PreviewParams、Preview，共5项 | `interfaces/console/templates.go:110`、`authoring_helpers.go`、`preview.go:52` |
| 启动和纯读取 | Bootstrapper.Initialise；PublicCatalog；MachineRenderer.Render；FrozenPreview.RenderFrozen | 启动组装、公开目录请求、机器协议适配器、任务 Reader.Preview。机器协议调用权限由对应适配器约定，未误套网页角色规则 |

32条编写网页路由与应用操作一一对应。构造器只组装依赖，不作为额外业务操作计数。Phase6增加的内置初始化、公开目录和机器渲染也已读到当前实现。

| 能力包 | 方法清单；其余约定的来源 |
| --- | --- |
| `authoring/port/repository.go:9` HookReader/Hooks | GetHook、GetHooks、ListHookVersions、ListHooks；CreateHook、SaveHookDraft、NewHookVersion、SubmitHook、ApproveHook、RejectHook、DeprecateHook、RenameHook、DeleteHookDraft。保存含义继承 `domain/repository/hooks.go:8` 和包共同约定 `doc.go:9` |
| 同文件 `TemplateReader/Templates:29` | GetTemplate、ListTemplateVersions、ListTemplates；CreateTemplate、SaveTemplateDraft、NewTemplateVersion、DeriveTemplate、SubmitTemplate、ApproveTemplate、RejectTemplate、DeprecateTemplate、RenameTemplate、DeleteTemplateDraft。继承 `domain/repository/templates.go:8`，包括引用一起保存和锁内审批 |
| 同文件 `BuiltinRepository:54` | EnsureBuiltinHook、EnsureBuiltinTemplate；幂等、冲突重试、单对象事务见 `bootstrap_contract.go:5`、repository Hooks/Templates |
| `authoring/port/bootconfig.go:8` | ReadESXiBootConfig、GetHost、GetISO；后两项复用对应只读仓储约定，原配置只由基础设施读取 |
| 共用能力 | Actor.Authorize、Clock、SelectionGuard.ReadLock/WriteLock、FrozenPreview.RenderFrozen。应用不直接调用其他应用具体服务 |

### 主机

`hosts.Registry` 六项为 Import、List、Get、Create、Update、Delete（`contract.go:10`）。六条真实路由在 `interfaces/console/hosts.go:55`，实际处理函数只做解析、应用调用和响应转换。

`hosts/port/repository.go:9` 的七项能力为 GetHosts、ImportHosts、CreateHost、UpdateHost、DeleteHost、GetHost、ListHosts。保存规则继承 `domain/repository/hosts.go:8`、`doc.go:9`、`types.go:65`；不存在额外的通用事务入口。

### 镜像与后台作业

`media.Registry` 十一项为 List、Get、Create、RegisterSource、Progress、Upload、Cancel、Retry、Reimport、Deprecate、Delete（`contract.go:13`）。真实路由在 `interfaces/console/isos.go:102`，均调用对应 Registry 方法。

| 能力包或公开内部入口 | 完整方法清单 |
| --- | --- |
| `media/port/repository.go:9` ISOs | CreateISO、UpdateISO、DeprecateISO、DeleteISO、GetISO、ListISOs |
| 同文件 `Sources:19` | SaveSource、SaveSourceAndAcquire、GetSource、SourcesOf、ListUnfinishedSources、SaveRange、RangesOf |
| `media/port/control.go:10` SourceControl / Deletion / Waker | Register、Upload、Cancel、Retry、Reimport、FreeSpace；Delete；Wake |
| `media/port/transfer.go:10` Transfer | ValidateSource、BeginFetch、BeginPublish、BeginUpload、ParkRetry、FreeSpace |
| 同文件 FetchSession / UploadSession / RetryFiles | Resume、Open、Step、Verify、Publish、Restore、Close；Size、CheckOverlap、Write、VerifyReplay、Close；Restore、Commit |
| `media/port/import.go:10` ImportFiles / ImportSession | Open；OpenSource、BeginPrepare、Prepare、Verify、Publish、Rollback、Close |
| 同文件 MediaView | Stat、Entries、Text、WIM、Digest |
| `media/port/deletion.go:14` | Journal.Persist/List/Discard；DeletedFiles.Cleanup |
| `media/port/coordination.go:5` | SourceLocks.Lock/Track/Cancel；RegistrationGate.Lock/Unlock/RLock/RUnlock |
| Sources 公开业务能力 | Register、RegisterQueued、Upload、Cancel、Retry、RequestReimport、Progress、Recover、FetchOne、Import、FreeSpace；只供可信应用或后台调用，不是额外网页权限入口 |
| Supervisor | Start、Close、Wake；构造接收 Recover/FetchOne/Import、GetSource、RetryAll 小接口。Deletion 另公开 RetryAll |
| 镜像识别辅助 | Identify 消费 MediaView；识别、齐备、导入和删除判定在 `domain/media`；文件能力不取得 Store |

上述内部方法的角色是受信任能力调用，不要求再次验证网页 Actor；网页调用始终先经 Registry 授权。以下实现核对综合使用类型、方法中文约定、`docs/contracts/media.md:41-109` 与仓储共同约定，没有仅因签名存在而判通过。但接口代码没有正式委托到这些外部说明，契约表达仍有下列共性缺口。

## 共性契约表达缺口（交总审查统一计数）

设计第6节明确把应用操作的唯一来源定为“Go接口、命令和结果类型、方法旁的中文约定”，并列出八项必须写清的内容。以下接口没有给出足够的旁注，也没有明确说明遵守哪份外部唯一契约。不能把本报告从多处拼出的行为，反过来当作代码已经明确表达了完整契约。

| 模块 | 接口旁的真实情况 | 外部已有细节 | 本轮判断 |
| --- | --- | --- | --- |
| 编写与预览 | `hooks_contract.go:47-61` 只有授权、草稿与仓储审批三点总述；`templates_contract.go:37-52` 相同；`editor_contract.go:12-17` 为裸签名。均无指向authoring-preview.md的正式引用。`port/repository.go` 部分方法有具体约定，但未统一委托到domain/repository同名能力 | authoring-preview.md已说明输入、排序空值、错误顺序、事务、重试、并发、审计和取消；repository中也有保存规则与共同约定 | 已写行为大部分有实际实现和测试；应用契约表达未完整闭合。Bootstrapper和MachineRenderer的详细旁注是当前正例，不应与裸签名一起判缺失 |
| 主机 | `hosts/contract.go:10-16` 六个操作全是签名，未说明目的、允许调用者、输入、结果、错误、原子边界、版本和取消，也无hosts.md的委托；`hosts/port/repository.go:9-17` 只有实际使用方法的总述 | hosts.md逐操作说明很具体；domain/repository/hosts.go和doc.go说明保存规则；应用/网页/真实库测试也在 | 实现与已写规则相符；接口自身没有把调用者连接到这些正式说明，不能报告契约表达完整 |
| 镜像 | `media/contract.go:13-26` 仅身份、只读审计、取消总述，无media.md引用；`registry.go`各实现方法旁已有较详细中文步骤、重复、事务和审计说明，但排序、空值等仍需外查文档。内部port多为目的及少数边界说明，未明确委托repository或media.md | media.md覆盖绝大多数八项要求；transfer/import/deletion能力旁注比另两模块完整；实现仍有I1错误 | 不能仅看contract.go判“全部无契约”，也不能据外部文档存在判“契约表达完备”。应补明确唯一来源委托和缺项，保留已有细节而非复制多份规范 |

建议在公开接口或方法旁明确引用唯一的中文约定及保存能力来源，补足外部文件没有覆盖的内部能力边界。不需要为每个签名复制八标签，也不应让代码注释与文档各自维护一份独立规则。该共性问题与后续矩阵的“实现相符”结论分别成立。

## 按设计八项要求核对

| 要求 | 编写与预览 | 主机 | 镜像与后台作业 |
| --- | --- | --- | --- |
| 1. 目的、归属、允许调用者 | Hooks/Templates/Editor逐方法授权；Bootstrapper与MachineRenderer点名可信调用者 | 六入口均先Authorize | Registry先Authorize；内部可信能力和后台角色明确 |
| 2. 输入、必填、限制、规范化 | 网页绑定、版本键、名称检查、4096字节审计限制；保存整份替换 | NormalizeSN；Rev；原文过滤；CSV标准解析，无新增大小限制 | 来源预检与真正网络/路径校验分开；确长上传；ID/Role/摘要限制 |
| 3. 结果、排序、空值 | 版本升序、身份分页、候选两种页型、来源最新版本各自明确；网页转换空数组/对象 | 编号字节序；导入按文件顺序；空attrs为对象 | 来源/区间顺序有实现；进度确认区间有3轮版本复核；默认页大小说明有Minor问题 |
| 4. 稳定错误和优先级 | 先后顺序固定；Problem与仓储错误按类型映射 | Rev先于编号；CSV读库先于预演/行拒绝 | 大部分保持；额外源读取取消被改业务错误，见Important问题 |
| 5. 写入范围、原子性 | 版本、引用、审批人、审计一起提交；写后补读独立 | 单条和整批导入均审计同事务 | 创建分步提交有明确约定；来源排队、区间、审计各有事务；文件与库的边界明确 |
| 6. 重复、重试、版本冲突 | 新版本不重发已用号；内置装载仅冲突重试4次；写后读失败不重试 | 更新比较Rev；导入覆盖最新；无自动重试 | 取消16轮、进度3轮、相同末片重放、来源Rev、重新排队、未知提交结果确认均有实现 |
| 7. 最新事实、提交复查 | 事务锁身份/版本/引用；模板审批/废弃不只靠共享进程锁 | 行锁/同序加锁；任务并发事实由真实独立连接测试 | SaveSource父行锁，排队复查ISO和所有来源，SaveRange拒绝取消；跨实例删除快照完整性未获证明，列风险 |
| 8. 审计、通知、取消、超时、关闭 | 可信操作者；无审计操作不新增通知；锁释放；渲染失败不返回半包 | 可信操作者和注入时间；CSV拒绝/预演不读写入时钟；整批取消回滚 | 提交后Wake；Close先取消再等；会话defer关闭；取消类型问题见下 |

## 逐模块操作契约矩阵

“相符”仅指静态审查相符。所有测试名称都对应当前提交中的源码，未在本轮运行。

### 编写与预览矩阵

| 操作 | 契约定义行 | 实现和真实调用方 | 行为/真实数据库测试证据 | 结论 |
| --- | --- | --- | --- | --- |
| Hooks.List / Templates.List | authoring-preview.md:91、109、114；repository/types.go:95 | hooks.go:29、templates.go:41；console/hooks.go:113、templates.go:117；pgstore/hooks.go:303、templates.go:349 | Test编写页型与来源选择互不替代；Test契约hook、Test契约模板；selection_pagination_test.go | 分页、最新版本与已审批选择相符；不是应用层替仓储重写选择 |
| Hooks.Versions/Get、Templates.Versions/Get | 契约:36、94；repository/hooks.go:41-47、templates.go:28-30 | hooks.go:49-76、templates.go:68-95；对应console处理函数 | hooks_test.go、templates_test.go、Test契约hook、Test契约模板 | 键先校验、版本升序、详情附完整领域问题；空数组由网页转换保持 |
| Hooks.Create/SaveDraft、Templates.Create/SaveDraft | 契约:11、23、95、103、128-130；repository Hooks/Templates | hooks.go:78-109、templates.go:97-128；pgstore对应Create/Save；console内容DTO不接受身份元字段 | Test组件草稿保存保留身份元数据；Test模板应用派生和保存只覆盖内容；Test编写预览路由契约基线；真实库模板/组件契约 | 保存不要求完整激活；名称不可偷改；元数据保留；Rev锁内复查；无额外审计 |
| Hooks.NewVersion/DeleteDraft、Templates.NewVersion/DeleteDraft | 契约:53、56、67、70、130；repository/hooks.go:22、38；templates.go:8 | hooks.go:111-140、templates.go:130-159；pgstore锁身份后分配版本/删除 | Test同时开新版本等对方提交之后报冲突；Test契约hook、Test契约模板；contract_hookrefs_test.go | 已用版本号不重发；内置/引用保护实存；删除不是仅应用预检查 |
| Templates.Derive | 契约:65、118、128；repository/templates.go:16 | templates.go:236；pgstore/templates.go:131；console/templates.go:173 | Test模板应用派生和保存只覆盖内容；Test编写页型与来源选择互不替代；真实库Test契约模板 | 来源允许任意状态；记录固定DerivedFrom；独立草稿，无新增审计 |
| Hooks.Rename、Templates.Rename | 契约:58、71、101、128；repository/hooks.go:36 | hooks.go:145、templates.go:164；pgstore/hooks.go:554通用身份改名事务 | Test编写预览路由契约基线；Test契约审计；Test契约hook/模板 | 固定动作、可信Actor/Clock；名称和审计同事务；内置只读 |
| Hooks.Submit/Approve/Reject/Deprecate | 契约:59-61、97-101、128-132；repository/hooks.go:27 | hooks.go:151-200；pgstore/hooks.go:110-183；lifecycle.go:44-79 | Test组件驳回先检查理由再检查版本和修订；Test组件状态写后补读失败不重试；Test编写审计失败状态与审批人均回滚；Test契约hook | 审批锁内调用同一领域判定；Rev/状态/作者规则和审计一起提交；写后读失败不假装回滚 |
| Templates.Submit/Approve/Reject/Deprecate | 契约:72-75、98、128-132、146；repository/templates.go:20 | templates.go:170-223；pgstore/templates.go:169-263；Approve/Deprecate共享SelectionGuard | Test模板应用审批使用同一个选择锁；Test模板取消等待选择锁后释放锁且不写入；Test审批模板等引用的hook版本提交之后再判定；Test任务提交与独立实例审批的契约 | 不只进程锁：事务先锁模板身份、版本，再以共享锁读取引用组件；审计失败回滚状态/审批人/Rev |
| Hooks.Validate / Templates.Validate | 契约:37、62、76、105、128 | hooks.go:202、templates.go:225；activation.Hook/Template；console/lifecycle.go:73、98 | Test编写预览路由契约基线；既有lifecycle_test.go；应用权限测试 | 写权限；返回200与完整问题清单；不写入 |
| Editor.Slots | 契约:38、94、105 | editor.go:34-59；scan.Scan + activation.Template的重复插槽问题；网页纯转换 | editor_test.go、slots_test.go、authoring_helpers_test.go | 第一次插槽位置及后续重复位置保留；未新增另一份扫描规则 |
| Editor.Candidates | 契约:83、96、105、116 | editor.go:61-154；console/authoring_helpers.go | Test候选只采纳当前替换位置的问题；Test编写输入多错时保留原先错误；Test编写页型与来源选择互不替代 | 对象→插槽→index→分页；草稿未审批问题不单独使usable为假；游标展开与页码逐版本有别 |
| Editor.SourceDiff | 契约:40、118-120 | editor.go:156-182；固定来源Get + 全版本最大值；无保存动作 | Test来源差异选择最新草稿但不改派生模板；Test编写页型与来源选择互不替代 | original固定；current含最新草稿；没有用向导已审批选择替换 |
| Editor.PreviewParams | 契约:89、99、124-126 | preview.go:20-85；params.Evaluate；console/preview.go:52 | Test参数预览问题仍以成功结果返回；Test预览显式对象不回退且缺省次序不变 | 草稿可预览；求值问题留200；显式缺失不回退示例 |
| Editor.Preview / FrozenPreview.RenderFrozen | 契约:99、124-126、146；ports/preview.go:9 | preview.go:87→snapshot.Build；renderer.go:28→domain/render；tasks/reader.go:115同一Renderer | Test文件预览失败丢弃全部文件；Test预览冻结脚本按摘要去重且保持文件次序；Test任务预览只使用冻结快照；cmd/lober/assembly_test.go:356 | 同一份冻结渲染；原组件正文/摘要去重和文件次序保留；失败不返回部分文件 |
| Bootstrapper.Initialise | bootstrap_contract.go:5-20；repository/hooks.go:48-54、templates.go:31 | bootstrap.go:24-49；cmd/lober/app.go:152提供builtin.Hooks/Templates；pgstore.EnsureBuiltin* | Test内置初始化保留顺序四次冲突与取消；Test内置初始化保留早先成功且可重复调用；Test同时装载同一份内置内容后到的等先到的提交之后什么都不做；Test同时首次装载同一个新身份两路都没看到对方 | 单对象原子、整表非原子、仅ErrConflict重试4次；启动可信入口；相同内容幂等 |
| PublicCatalog / MachineRenderer.Render | schema_contract.go:14；machine_render.go:12-35 | PublicCatalog返回目录副本；MachineRenderer仅消费冻结Task，ESXi辅助文件才读原配置 | Test公开目录保持顺序与独立副本；Test机器渲染只用冻结任务且仅ESXi辅助文件读取原配置 | 不访问活动模板/主机；机器渲染不注入示例；错误文本为接口已明确约定，不据文字分支 |

### 主机矩阵

| 操作 | 契约定义行 | 实现和真实调用方 | 行为/真实数据库测试证据 | 结论 |
| --- | --- | --- | --- | --- |
| List | hosts.md:16、22-45；repository/types.go:65-83 | registry.go:19-31；console/hosts.go:65；pgstore/hosts.go:194-229 | Test主机列表保留两种分页与原文过滤；Test契约主机 | 属性配对、原文过滤、编号字节序和空数组相符；默认游标100/页码20 |
| Get | hosts.md:49；repository/hosts.go:8-15、30 | registry.go:33-41；console/hosts.go:78；pgstore/hosts.go:159 | Test主机应用编号版本与整份替换；Test主机输入多错保持原顺序；Test契约主机 | 应用规范化；仓储按规范键读；无写时钟、审计或通知 |
| Create | hosts.md:50、54；repository/hosts.go:17-20 | registry.go:43-52；pgstore/hosts.go:28-49 | Test主机六入口契约；Test主机应用审计失败与删除引用保护；Test契约主机、Test契约审计 | 可信Actor/Clock；唯一约束兜底；Rev=1，业务记录与审计同事务 |
| Update | hosts.md:51-54；repository/hosts.go:21 | registry.go:54-75；pgstore/hosts.go:53-78 | Test主机应用编号版本与整份替换；Test主机应用审计失败与删除引用保护；Test主机变更与向导提交按数据库事实交错 | Rev先于编号；正文身份不可改；整份替换含零值；锁内版本保护；任务冻结旧事实或拒绝新不兼容事实 |
| Delete | hosts.md:53-54；repository/hosts.go:28 | registry.go:77-88；pgstore/hosts.go:136-155 | Test主机应用审计失败与删除引用保护中所有任务状态；Test契约主机 | 存在性→版本→任务引用；结束任务仍引用保护；审计失败回滚 |
| Import | hosts.md:56-67；repository/hosts.go:23-27 | import.go:10-51、csv.go:17-103；console/hosts.go:143；pgstore/hosts.go:84-132 | Test主机CSV重复问题置首但保留后续顺序；Test主机导入读取先于预演和拒绝；Test主机导入预演后覆盖最新版本；Test主机导入审计取消整批回滚；Test同时导入主机与建任务不会互相等死；Test同时导入同一组主机不会互相等死 | 文件解析→一次GetHosts→计划→预演/拒绝→一次ImportHosts；排序加锁、分块同事务；返回输入顺序；每台审计同一时间；无新增通知 |

主机关键数据库证据不是单进程替身：`hosts_contract_test.go:512-514` 用同一测试schema的两份独立Store；通过数据库阻塞观察确认顺序。`610`起注入中间主机失败、中间审计失败和锁等待取消，再核对整批对象、版本和审计没有变化。

### 镜像网页与后台矩阵

| 操作 | 契约定义行 | 实现和真实调用方 | 行为/真实数据库测试证据 | 结论 |
| --- | --- | --- | --- | --- |
| Registry.List/Get | media.md:29-30、55；repository/types.go:65-93 | registry.go:23-49；console/isos.go:117、134；pgstore/isos.go:137、154 | Test镜像十一入口描述及审计契约；Test五角色读写权限；ISO列表/仓储契约 | 权限、顺序、空值相符；默认页大小文档错误见M1 |
| Registry.Create | media.md:31、43-49 | registry.go:104-146；console/isos.go:145在Created后设Location；每来源Control.Register | Test创建后登记失败仍返回编号；Test镜像非法输入先于网络安全拒绝；console/isos_test.go真实来源部分创建与审计 | 镜像/来源分步提交明确，第二项失败保留前项；不伪造整批事务 |
| RegisterSource / Register / RegisterQueued | media.md:32、45、51、95-97 | registry.go:151；sources.go:46-116；control.go:17；pgstore/sources.go:82-186 | Test补来源调度失败必须回滚登记和审计；TestSourceAuditRollback；Test契约ISO来源排队按版本一次提交 | 父ISO行锁保护新键；失败镜像额外源+排队+审计原子；主源/必需额外源和Rev提交时复查 |
| Registry.Progress / Sources.Progress | media.md:33、57-59 | progress.go:11-53；sources.go:175-209；console/isos.go:262 | Test进度三次重读耗尽；Test进度返回确认区间；TestISOSources | 网页确认区间与旧Missing含义未混用；3轮按来源Rev复核，不夸大一致快照 |
| Registry.Upload / Sources.Upload | media.md:34、61-67 | upload_request.go:45-72；upload.go:10-104；transfer.BeginUpload；pgstore.SaveRange:240-275 | Test上传流式确长与范围错误分开；TestUploadRejectedDoesNotOverwriteConfirmedBytes；Test镜像取消与最后片发布两方向竞争；TestUploadFinalChunkReplayedAfterLostAcknowledgement；TestISOSources | 暂存/长度/重叠检查先于确认字节；SaveRange锁内检查fetching、并集计数和Rev；旧取消版本不可被保存成完成 |
| Registry.Cancel / Sources.Cancel | media.md:35、71 | registry.go:177；sources.go:120-158；pgstore.SaveSource | TestRemoteCancelDoesNotWaitForDownloadAndProgressCannotOverwrite；TestCancelRetryAllSourcesAndFetchedPreserved；TestISOSources | 逐来源，先保存再Cancel；16轮冲突；非跨来源事务；已终态无新增审计 |
| Registry.Retry / Sources.Retry | media.md:36、73 | registry.go:186；retry.go:12-74；transfer.ParkRetry + SaveSource事务 | TestRetryAuditFailurePreservesPartialFileAndRanges；Test镜像重试保留文件恢复失败；TestSourceAuditRollback | 仅failed/cancelled，原件不改；区间/进度/审计同事务；恢复错误没有吞掉 |
| Registry.Reimport / RequestReimport | media.md:37、79、97 | registry.go:195；retry.go:77-103；SaveSourceAndAcquire | Test重新导入调度失败必须回滚审计和来源修订；Test契约ISO来源排队按版本一次提交；Test控制器只在保存成功后唤醒 | 先落排队和审计后Wake；网页不提取；旧版本重放不重复审计 |
| Registry.Deprecate | media.md:38、95 | registry.go:204；pgstore/isos.go:89-110 | Test镜像管理员八个写入口传递有效输入；Test契约ISO；审计契约 | Rev解析→审计/编号→锁版本→ready状态；废弃不可逆 |
| Registry.Delete / Deletion.Delete / RetryAll | media.md:39、89-97 | deletion.go:40-153；journal.Persist；pgstore/isos.go:114-133；DeletedFiles.Cleanup | Test删除和补来源按两种先后次序保护真实文件；Test删除丢确认保留字节快照并在重启后清理；Test等导入锁后必须再次确认删除；Test坏快照不会阻止其他删除恢复；Test已提交删除丢弃日志失败仍成功且可重试 | 同实例完整快照、确认后清理、本地原件/其他拥有者保护有实现与真实库证据；独立实例快照竞争列风险 |
| Sources.FetchOne / finishFetch / FreeSpace | media.md:47、65-75；port/transfer.go:10-47 | fetch.go:86-222；真正文件/网络在infrastructure/media；会话defer.Close，保存失败Restore | Test镜像获取会话每条退出路径关闭；TestWorkerShutdownPreservesRemoteCheckpointForRestart；Test镜像关闭后从真实断点恢复；remote_test.go | 关闭保留断点；最后核对来源状态/Rev后发布；数据库拒绝不撤销已取消状态；余量只比较staging |
| Sources.Import | media.md:75、79-85；port/import.go:10-26 | import.go:31-215；domain/media.CheckImport；真实ImportSession.Verify/Publish/Rollback | Test镜像多源完成复制后才复核；Test镜像导入提交未知不删已发布内容；TestImportPreservesPublicationAfterLostCommitAcknowledgment；Test镜像共享树先发布且冲突只撤销本次新树 | 摘要/状态/制品及未知提交保护落地；额外源Text取消类型未保留，见I1 |
| Sources.Recover | media.md:101 | recover.go:27-74；domain/media.AllFetched | TestRecoverFetchAndImportNoWorkStarted；TestRecoverDebianNeedsExtraAndReimportRejectsAcquiring | 仅列待办、ISOID/Role/Kind排序；不开始获取/提取/清理；需额外来源的导入不提前排队 |
| Supervisor.Start/Close/Wake | media.md:103-109 | supervisor.go:43-210；Control仅成功后Wake；app.go:83提供同实例能力 | Test控制器只在保存成功后唤醒；Test关闭能取消正在启动的恢复读取；Test关闭等待作业且拒绝再占名额；Test后台工作panic释放资源退避后重试；Test后台协调panic退避且取消立即退出 | 有界名额、同实例去重、容量1合并Wake、周期补扫；关闭先Cancel后等锁/作业；固定异常日志及25.6秒实际退避上限相符 |

媒体数据库证据：`contract_isosources_test.go:15` 实际使用 `pgtest.OpenStore`，核对区间合并、短重传、取消后SaveRange拒绝、旧Rev拒绝、重试清零和来源排序；`104`起核对审计失败回滚；`187`起核对ISO/Source/审计原子排队与旧版本重放。应用媒体集成测试由 `import_test.go:21` 的 `pgtest.Open` 或 `deletion_fixture_test.go:34` 的真实schema/Store组装；不是只凭mock断言。是否在本轮VM实际运行，须看根审查的最新日志。

## 已确认问题

### I1：额外镜像识别把取消或超时改成“额外源无效”

- 等级：Important。位置：`internal/application/media/import.go:112-119`；下游取消来源：`internal/infrastructure/media/import_session.go:120-136`。
- 触发：Debian主来源和netboot额外来源均已fetched、摘要合法。额外来源Digest成功后，读取 `.disk/info` 时取消或超时。第一次读取由 `identifyNetboot` 发起，第二次读取用于再次核对NETBOOT标记，两处分支都把任意错误替换为 `iso_extra_source_invalid`。
- 差异：`identifyNetboot` (`netboot.go:9-14`) 原样返回Text错误。真实Text实现会返回 `context.Canceled` 或 `context.DeadlineExceeded`。到Import后 `errors.Is(err, context.Canceled/DeadlineExceeded)` 变成false，调用者得到领域问题。
- 违反：media.md:75“取消上下文贯穿来源读取”；设计第6节的稳定错误与取消要求。导入的统一清理 `import.go:69` 本来特意排除这两种错误，现在排除条件失效。须区分两种影响：真实mediaText主要从传入的ctx.Err取得取消或超时；父上下文已经取消时，后续数据库读取会被取消，不能据此声称真实父取消会把镜像写成failed。只有合成的“能力返回取消/超时、父上下文仍可用”测试才能进一步证明failImport误写failed。实际父取消已可由源码确认的影响是返回错误类型丢失；状态仍应保留acquiring。
- 影响入口：直接可信调用 `Sources.Import`，生产后台 `Supervisor.reconcile` 调用的Import同样经过这里。`POST /isos/:id/reimport` 是先排队的异步入口，网页不会直接收到这个导入错误；不能把它描述成网页立即返回422。
- 现有测试缺口：`Test镜像打开有效额外源时取消仍返回取消` 只在OpenSource之后立刻取消，下一步Digest先返回取消，未到这两个Text分支。
- 建议：先保留取消/截止错误，再仅对真正介质问题映射 `iso_extra_source_invalid`；其他输入读取错误是否应保持原类型也应按现有兼容约定判断，不顺带扩大行为。
- VM补证文件：`evidence/probe-media-cancel_test.go`。父测试名称保持 `TestProbeMediaExtraSourceCancellationRemainsTyped`，共6个叶子用例。第一组 `能力取消或超时/边界/取消或超时` 共4例，第1次和第2次 `.disk/info` 读取分别合成返回Canceled/DeadlineExceeded，父上下文仍可用；它们检查类型丢失及合成子能力失败误写状态。第二组 `父上下文取消/边界` 共2例，使用真实 `context.WithCancel`，在指定Text边界先取消父ctx、再调用真实MediaView.Text，从实际metadata读取取得Canceled；用 `context.WithoutCancel` 重读数据库，分别检查错误类型和acquiring保留。摘要、上传、真实文件和PostgreSQL仍走原实现。没有sleep/timer，没有修改DEV或原测试断言，也没有在本机运行。
- 建议在独立VM probe克隆复制后运行：`go test -count=1 ./internal/application/media -run '^TestProbeMediaExtraSourceCancellationRemainsTyped$' -v`。预期受审提交失败；这是待VM补证的预期，不能当作已执行结果。

### M1：镜像列表默认条数说明与实际接口不符

- 等级：Minor。位置：`docs/contracts/media.md:55`。
- 触发：数据库有超过20条镜像，调用 `GET /console/v1/isos`，不传page、page_size、limit或cursor。
- 约定写“默认每页20”。实际 `console/isos.go:118` 调用 `parseQueryPagination`；`pagination.go:27-30` 返回Page=0/Limit=0；`repository/types.go:65-68` 与 `check.go:31、136、145` 定义游标默认100，页码默认20；`pgstore/isos.go:154` 使用该默认值。
- 影响：维护者按契约预测列表页长时得到错误结论；这也是当前两份契约互相矛盾。未发现实际接口在迁移中变更的证据。
- 建议：修正文档为“游标默认100，页码默认20，最多500”，不改现有实现。补一个无分页字段且超过20条记录的网页断言可固定差异；本轮未执行该断言。

## 无法核实与暂不评判

1. **独立实例登记与删除快照竞争。** media.md:89、`port/coordination.go:12` 写快照要覆盖已完成登记来源，设计第7节又明确进程锁不能替代数据库并发保证。但 SourceLocks 的注释明确限定同实例，后台也明确不保证多进程只执行一次。本轮未找到“多实例共享目录时的完整删除保证”或“只允许单实例部署”的明确部署契约。

   当前局限可由源码定位：Deletion.Delete在 `deletion.go:53-66` 先SourcesOf/Persist、后DeleteISO；Gate是 `infrastructure/media/coordination.go:39` 的sync.RWMutex。第二实例拥有另一Gate，普通SaveSource虽然锁父ISO (`pgstore/sources.go:91-98`)，新增来源却不递增父ISO Rev（只有acquireRev分支 `176-179` 会递增），所以删除旧Rev不会因此必然冲突。若第二实例在Persist后登记并写入来源字节，快照可能不含该角色，后续按快照清理会留下其暂存文件。

   当前 `Test删除和补来源按两种先后次序保护真实文件` 显式共用同一Gate，证明了同实例承诺，不能证明独立实例。此项先列风险，不计入已确认缺陷。若总审查认定跨实例完整快照属于明确承诺，应以两个Gate、两份Store、Persist后屏障、第二实例登记/上传、再释放Delete的VM用例确认；检查来源字节和删除日志最终状态。

2. **本轮没有执行任何测试。** 测试源码证明覆盖位置，无法证明当前固定提交在loberVM上全部通过。真实数据库连接是否可用、测试是否跳过、竞态运行是否通过，由根审查的最新实跑日志判断。
3. **机器协议权限和任务创建全链路**由另一路独立审查负责。本报告仅确认编写渲染消费冻结输入、与任务预览共享Renderer，以及引用审批和主机变更的真实数据库保护；不重复宣称任务/身份模块整体通过。
4. 没有发现三模块新增数据库表、公开字段或新通知语义的源码证据。生成结果、全局架构守卫覆盖和六阶段合并范围由全局审查负责。

## 给loberVM的建议复核入口

以下仅建议，不是执行记录。测试必须实际使用PostgreSQL且不得跳过；不应只跑应用替身用例代替保存契约。

- 编写：`Test编写预览路由契约基线`、`Test编写输入多错时保留原先错误`、`Test编写页型与来源选择互不替代`、`Test编写审计失败状态与审批人均回滚`、`Test模板取消等待选择锁后释放锁且不写入`、`Test任务提交与独立实例审批的契约`、`Test审批模板等引用的hook版本提交之后再判定`，加组件/模板/审计仓储契约与内置初始化并发用例。
- 主机：`Test主机六入口契约`、`Test主机输入多错保持原顺序`、`Test主机列表保留两种分页与原文过滤`、`Test主机导入预演后覆盖最新版本`、`Test主机变更与向导提交按数据库事实交错`、`Test主机导入审计取消整批回滚`、两种同时导入/建任务防互等用例。
- 镜像：`TestISOSources`、`TestSourceAuditRollback`、`Test契约ISO来源排队按版本一次提交`；媒体矩阵中的上传取消、末片重放、未知提交、删除两方向、恢复和关闭测试；另独立执行I1的6例probe，并分开统计4例合成能力错误与2例真实父取消。
- 完整后端验证仍按仓库要求在loberVM运行 `make check`。本报告没有降低或替代该要求。
