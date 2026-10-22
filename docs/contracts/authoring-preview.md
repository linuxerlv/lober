# 编写与预览契约

这份表固定第二阶段迁移前的真实行为。路由以 `hooks.go`、`templates.go`、`lifecycle.go` 为准，共 32 条：组件 13 条，模板 19 条。它是迁移验收记录，不替代现有路由描述和生成检查。

## 共同约定

路径中的版本必须为正整数，编号必须通过现有编号检查。

全部成功响应都是 200：`{code:0,data:结果,message:""}`。失败仍含 `code,data,message`，`code` 等于响应状态。下表的“空”指 `data:null`。数组为空时返回 `[]`，映射为空时返回 `{}`。

所有正文沿用现有 JSON 绑定：只接受一个完整值，拒绝非法编码、类型不符和缺失的声明必填字段。未声明为绑定必填，不等于业务上有效；例如空名称在仓储返回问题明细。额外的作者、状态或审计字段不能生效。

| 权限 | admin | hook-author | hook-approver | operator | viewer |
| --- | --- | --- | --- | --- | --- |
| `hook:read`、`template:read` | 允许 | 允许 | 允许 | 允许 | 允许 |
| `hook:write`、`template:write` | 允许 | 允许 | 拒绝 | 拒绝 | 拒绝 |
| `hook:approve`、`template:approve` | 允许 | 拒绝 | 允许 | 拒绝 | 拒绝 |

无可信登录身份、角色无效或账号禁用为 401。强制改密码、权限不足为 403。应用入口迁移后也必须检查可信身份和权限。

## 请求和返回字段

下表字段名是网页实际收到的名字。创建和保存请求中的内容字段都不带绑定必填；保存额外要求 `rev>0`。仓储仍检查文本是否能保存。

| 名称 | 字段与空值 |
| --- | --- |
| 组件内容 | `name,description,timeoutSec,required,params,impls` |
| 模板内容 | `name,family,series,body,params,bindings,assembly` |
| 组件详情 | `id,version,name,description,state,author,approver,builtin,timeoutSec,required,params,impls,problems,rev` |
| 模板详情 | `id,version,name,state,author,approver,builtin,derivedFrom,family,series,body,params,bindings,assembly,problems,rev`；`derivedFrom` 为 `null` 或 `{id,version}` |
| 参数声明 `params[]` | `name,label,type,list,required,default,options,min,max,offer`；选项为 `{value,label}`；未设上下限时 `min/max:null` |
| 实现 `impls[]` | `family,series,kind,body,interpreter,timings`；其中数组为空仍为 `[]` |
| 绑定 `bindings` | 参数名到 `{source,value,attr,editable}` 的映射 |
| 装配 `assembly` | 插槽名到实例数组的映射；实例为 `{hookID,hookVersion,timeoutSec,required,bindings}`，未覆盖默认值的 `timeoutSec/required:null` |
| 问题 `problems[]` | `code,subject,message,next`；`subject` 为 `{kind,id,path,line,col}`。保留顺序和完整定位，不只返回提示文字 |
| 列表 | 身份列表 `{items:[详情],next}`；页码模式额外有 `total`。版本列表仅 `{items:[详情]}`，按版本升序 |
| 校验 | `{ok,problems}`；问题放在 200 正文，问题为空才 `ok:true` |
| 插槽 | `{slots:[{name,exec,timing,line,col}],problems}`；同名插槽只列第一次，重复问题保留后续位置 |
| 候选 | `{items:[{hook:组件详情,usable,problems}],next}`；页码模式额外有 `total` |
| 来源差异 | `{original:模板详情或null,current:模板详情或null,changed}`；没有来源时两个详情均为 `null`、`changed:false` |
| 参数预览 | `{fields:[{key,label,type,list,required,locked,prefill,options}],problems,sample}`；选项为 `{value,label}` |
| 文件预览 | `{files:[{kind,name,sample,body,slot?,digest?}],problems,sample}`；无插槽或摘要时省略对应字段 |

## 32 条真实路由

“读”“写”“审批”分别使用该对象的 `:read`、`:write`、`:approve` 权限。所有“无”审计操作也不发通知。诊断字段随详情返回；保存不要求内容已经完整。

| 序号 | 方法与路径 | 请求字段与绑定必填 | 权限 | 成功 data | 审计 |
| --- | --- | --- | --- | --- | --- |
| 1 | `GET /console/v1/hooks` | 查询 `prefix,state,approved,cursor,limit,page,page_size`；无绑定必填 | 读 | 组件列表 | 无 |
| 2 | `POST /console/v1/hooks` | 组件内容；无绑定必填 | 写 | 新组件草稿详情 | 无 |
| 3 | `GET /console/v1/hooks/:id/versions` | 无正文与查询必填 | 读 | 组件版本列表 | 无 |
| 4 | `POST /console/v1/hooks/:id/versions` | 查询 `from>0` 必填 | 写 | 复制出的新草稿详情 | 无 |
| 5 | `GET /console/v1/hooks/:id/versions/:version` | 无正文 | 读 | 组件详情 | 无 |
| 6 | `PUT /console/v1/hooks/:id/versions/:version` | 组件内容；正文 `rev>0` 必填 | 写 | 整份保存后的详情 | 无 |
| 7 | `DELETE /console/v1/hooks/:id/versions/:version` | 查询 `rev>0` 必填 | 写 | 空 | 无 |
| 8 | `PUT /console/v1/hooks/:id/name` | 正文 `name`；无绑定必填，仓储检查名称 | 写 | 空 | `hook.rename` |
| 9 | `POST /console/v1/hooks/:id/versions/:version/submit` | 查询 `rev>0` 必填 | 写 | 提交后补读详情 | `hook.submit` |
| 10 | `POST /console/v1/hooks/:id/versions/:version/approve` | 查询 `rev>0` 必填 | 审批 | 审批后补读详情 | `hook.approve` |
| 11 | `POST /console/v1/hooks/:id/versions/:version/reject` | 查询 `rev>0` 必填；正文 `reason` 绑定必填 | 审批 | 驳回后补读详情 | `hook.reject` |
| 12 | `POST /console/v1/hooks/:id/versions/:version/deprecate` | 查询 `rev>0` 必填 | 审批 | 废弃后补读详情 | `hook.deprecate` |
| 13 | `POST /console/v1/hooks/:id/versions/:version/validate` | 无正文与查询必填 | 写 | 校验结果 | 无 |
| 14 | `GET /console/v1/templates` | 查询 `prefix,state,approved,series,cursor,limit,page,page_size`；无绑定必填 | 读 | 模板列表 | 无 |
| 15 | `POST /console/v1/templates` | 模板内容；无绑定必填 | 写 | 新模板草稿详情 | 无 |
| 16 | `POST /console/v1/templates/:id/derive` | 正文 `from>0` 绑定必填、`name`；仓储检查名称 | 写 | 独立派生草稿详情 | 无 |
| 17 | `GET /console/v1/templates/:id/versions` | 无正文与查询必填 | 读 | 模板版本列表 | 无 |
| 18 | `POST /console/v1/templates/:id/versions` | 查询 `from>0` 必填 | 写 | 复制出的新草稿详情 | 无 |
| 19 | `GET /console/v1/templates/:id/versions/:version` | 无正文 | 读 | 模板详情 | 无 |
| 20 | `PUT /console/v1/templates/:id/versions/:version` | 模板内容；正文 `rev>0` 绑定必填 | 写 | 整份保存后的详情 | 无 |
| 21 | `DELETE /console/v1/templates/:id/versions/:version` | 查询 `rev>0` 必填 | 写 | 空 | 无 |
| 22 | `PUT /console/v1/templates/:id/name` | 正文 `name`；无绑定必填，仓储检查名称 | 写 | 空 | `template.rename` |
| 23 | `POST /console/v1/templates/:id/versions/:version/submit` | 查询 `rev>0` 必填 | 写 | 提交后补读详情 | `template.submit` |
| 24 | `POST /console/v1/templates/:id/versions/:version/approve` | 查询 `rev>0` 必填 | 审批 | 审批后补读详情 | `template.approve` |
| 25 | `POST /console/v1/templates/:id/versions/:version/reject` | 查询 `rev>0` 必填；正文 `reason` 绑定必填 | 审批 | 驳回后补读详情 | `template.reject` |
| 26 | `POST /console/v1/templates/:id/versions/:version/deprecate` | 查询 `rev>0` 必填 | 审批 | 废弃后补读详情 | `template.deprecate` |
| 27 | `POST /console/v1/templates/:id/versions/:version/validate` | 无正文与查询必填 | 写 | 校验结果 | 无 |
| 28 | `GET /console/v1/templates/:id/versions/:version/slots` | 无正文 | 读 | 插槽与定位 | 无 |
| 29 | `GET /console/v1/templates/:id/versions/:version/hook-candidates` | 查询 `slot` 声明必填；`index,prefix,cursor,limit,page,page_size` 可选 | 读 | 候选列表 | 无 |
| 30 | `GET /console/v1/templates/:id/versions/:version/source-diff` | 无正文 | 读 | 来源差异 | 无 |
| 31 | `POST /console/v1/templates/:id/versions/:version/preview-params` | 正文 `hostSN,isoID,series,firmware,entered`；无绑定必填，仍需合法 JSON，如 `{}` | 读 | 参数预览 | 无 |
| 32 | `POST /console/v1/templates/:id/versions/:version/preview` | 同参数预览 | 读 | 完整文件预览 | 无 |

候选入口没有直接绑定查询结构，而是先读模板、再检查正文插槽；缺失 `slot` 也沿用这个顺序。

## 问题与先后顺序

普通非法输入为 400，不存在为 404，修订冲突、重复、内置只读和被引用为 409。仓储 `ProblemsError` 总是 422，返回 `{problems:[问题]}`。被引用且带明细时保留 `{references:[定位]}`。

预览的领域问题按第一项类别返回：不兼容或状态问题为 409，其余为 422。不能按提示文字判断。参数预览的求值问题仍放在 200 正文。

| 入口或情形 | 先后与返回 |
| --- | --- |
| 身份列表 | 先检查分页，再检查 `approved`，再检查状态；模板再检查 `series`，然后查仓储与诊断 |
| 版本详情、插槽、来源差异、validate | 先检查路径版本键，再读对象，再做该操作的计算或补读 |
| 保存草稿 | 路径键 → 正文绑定 → 读取现有对象 → 非空 `name` 是否一致 → 仓储保存 → 返回诊断。名称不同仍为 400、`data:null` |
| 候选 | 路径键 → 读取模板 → 插槽存在 → `index` → 分页 → 候选读取与诊断。不存在模板加非法 `index` 仍为 404；存在模板加不存在插槽和非法 `index` 仍为 400 |
| 驳回 | 正文绑定和理由检查先于路径版本与 `rev`。空白理由加非法版本仍为 400、`ConsoleRejectReason`、`data:null`；空字符串缺失绑定仍为 `ConsoleBadRequest` |
| 状态变更 | 路径键 → `rev` → 仓储。仓储保留存在性、旧修订、状态、引用检查顺序；审批模板在事务内重查引用 |
| 预览 | 正文绑定 → 路径键 → 系列与固件值 → 模板 → 引用组件 → 显式镜像及系列匹配 → 主机 → 固件默认值 → 参数或快照 → 渲染 |

理由保留原文，不裁掉首尾空白后存储。全空白、不可存文本被拒绝；现有审计文本限制为 4096 字节。正文不能指定有效的操作者、动作或时间。

保存保留作者、审批人、状态、内置标记和派生来源。名称走独立改名入口；草稿正文不能偷偷改名。省略的内容字段按零值整份替换，不是只修改提供的字段。

诊断顺序继续由现有领域检查产生：模板先名称、系列和格式，再参数声明、绑定、正文、装配；插槽响应先扫描问题，再附重复插槽问题。候选只保留该实例路径及插槽、模板格式的相关问题，未审批引用问题保留，但它本身不使草稿候选 `usable:false`。不得重排或只留下第一条。

## 页型与来源不能互相替代

| 页型 | 缺省和边界 | 返回与选择 |
| --- | --- | --- |
| 游标 | 默认 100 条；`limit:0` 同省略；最多 500。负数、超限、坏游标或整数溢出为 400 | 身份按规范名称键、编号排序；`next` 是不透明位置，无下一页为 `""`；不出现 `total` |
| 页码 | 提供 `page` 或 `page_size` 即进入；默认第 1 页、每页 20 条，最多 500。必须为单个非空十进制正整数，拒绝偏移量溢出 | 返回 `total`；有后续记录时仍返回非空 `next`，末页为 `""`。不能同时提供 `cursor` 或 `limit`，即使它们为空或为 0 |

普通组件、模板列表每个身份默认选最新版本；`approved=true` 才选当前已审批版本。状态筛选不改变这两种选择语义。版本列表按版本号升序。

候选游标先分页身份，再展开这些身份的全部版本。同一身份有两个版本时，`limit=1` 可返回两项，仍能有 `next`。候选页码直接分页全部版本；一页一条就只返回一条，`total` 统计全部版本，不再展开一次。

来源差异的 `original` 固定取 `derivedFrom` 的版本。`current` 取该来源最大版本号，最新草稿也参与。`changed` 只比较版本号；读取不更新派生副本。

向导继续选当前已审批且可用的模板。来源差异看到第 2 版草稿，不代表向导能选第 2 版；第 1 版仍为当前已审批时，向导继续选第 1 版。内置发布选择规则不套到来源对比。

## 预览与写入边界

省略系列时用模板第一个系列。省略固件时用 UEFI。省略镜像或主机可用示例；显式 `isoID/hostSN` 不存在必须返回 404，不能退回示例。请求系列与显式镜像系列不符为 400。ESXi 没有真实镜像时仍报 `iso_version_missing`，不编造原始 `boot.cfg`。

参数预览允许草稿。文件预览先建立冻结快照；快照或渲染失败都不返回部分文件。文件种类、次序、路径、原始组件正文、摘要去重保持原样。`sample` 标签按镜像、主机、固件，再按渲染示例值的现有次序追加；只要存在示例标签，所有文件的 `sample` 为 `true`。

创建、保存、复制、派生、删除草稿，全部读取、validate 和预览，不增加审计或通知，也不创建任务。改名及四种状态变更的审计与业务写入在同一事务；操作者取可信身份，动作固定，时间取注入时钟。事务失败不留审计。

成功写入后的补读或诊断仍可能失败。此时写入可能已经提交；不能扩大成跨读取事务，也不能自动重试写入。新版本号删除后不重发。旧修订、内置只读、引用保护继续由原仓储保证。

请求取消继续通过上下文传给仓储和原配置读取。等待选择锁时不能立即取消；取得锁后仍将取消状态传给仓储，退出时释放锁。纯内存计算不新增立即中断能力。所有退出路径继续关闭资源，失败事务回滚。

## 验收证据

新增 `authoring_contract_test.go` 固定完整路由集合、权限和审计动作；通过真实网页与数据库检查无审计操作、错误外层结构、两种候选分页、来源草稿与向导选择。

继续复用 `hooks_test.go`、`templates_test.go`、`lifecycle_test.go` 的元数据、状态、修订和审计；`authoring_helpers_test.go`、`slots_test.go`、`selection_pagination_test.go` 的位置和候选；`preview_test.go`、`preview_params_test.go`、`tasks_test.go` 的文件、示例和旧快照。

仓储证据保留 `pgstore/contract_hooks_test.go`、`contract_templates_test.go`、`contract_hookrefs_test.go`、`contract_concurrency_test.go`、`contract_audit_test.go`、`contract_tasks_test.go`。向导当前模板保护继续由独立数据库连接验证，不能用进程锁测试代替。
