# 人面 HTTP 契约（控制台，管理网）

> 冻结文档，不是计划。M6（控制台）据此实现，M8（WebUI）据此对接。
> 全部端点挂在 `/console/v1` 下。客户端是浏览器（经 vben-admin 前端框架的
> axios 封装），因此**响应统一包裹**——这与机器面（裸文本）截然相反。
>
> 依据：SPEC §2.4、§2.6、§3.2、§4.3、§8.2、§8.3、§9.1、§9.4、§12.5、§12.6、§12.8。
> 类型字段名对应 `internal/core/model`：`Task`（task.go）、`Host`/`ArtifactSet`
> （inventory.go）、`Hook`/`Template`/`HookRef`（authoring.go）、`TaskEvent`
> （event.go）。
>
> **变更记录：新增 `POST /hooks/{id}/reject`（驳回 hook 审批）。** 原因：本
> 文档冻结时全文搜索 reject / 驳回 / decline 零匹配——`POST /hooks/{id}/approve`
> 只能把 hook 从 `draft` 推进到 `review` 再到 `active`，一旦提交 `review`
> 就无法退回，审批人若发现问题（脚本有安全隐患、参数声明有误等）没有任何
> 拒绝入口，只能眼看着 hook 卡在 `review` 或被迫违心通过。这是一个被独立
> 核实过的真实契约缺口（grep 契约文档零匹配 + 读 `internal/console/hook.go`
> 确认只有 approve 一个流转端点），经编排方核实后授权补上。新增端点编号为
> `28`（追加在端点总表末尾，不重排 12-27 号——本次是新增能力，不是对既有
> 端点的修订，重排会让已经存在的 PR/评审记录里的端点编号失去指代意义）。
> 影响模块：`internal/console`（新增 `handleRejectHook`，与既有
> `handleApproveHook` 对称）；不影响 `internal/core/model`（`HookState` 四个
> 取值不变，驳回只是 `review → draft` 的状态回退）；不影响
> `internal/store/sqlite`、`internal/core/fake`（复用既有的
> `HookStore.UpdateHookState`，未新增 store 方法）。
>
> **变更记录 2：新增 `GET /hooks/{id}/audit`（查审计记录）。** 原因：上一条
> 变更记录（reject）自己写明了一个遗留缺口——「审计日志目前只能写、不能
> 读……供后续补一个审计查询端点时参考」。核实确认这个缺口比表面看到的更深：
> `contract.AuditLogger` 只有 `Log` 方法，全项目没有任何查询方法；唯一的
> 生产实现（`cmd/lober/auditlog.go`）只把记录写进 `log/slog`，从不写
> `internal/store/sqlite/schema/001_init.sql` 第 174 行早已建好的
> `audit_log` 表——该文件自己的注释也承认这是绕过，正解是给
> `contract.Store` 补一个窄接口。经编排方核实后授权补上，按 M1 的五步
> 契约变更流程执行（改接口 → 改 conformance 断言 → 改内存实现 → 改
> SQLite 实现 → 接上调用方）。新增端点编号为 `29`（同样追加在末尾，
> 不重排既有编号，理由与变更记录 1 相同）。
>
> 影响模块：`internal/core/contract`（新增 `AuditStore` 窄接口、
> `AuditRecord`/`AuditFilter` 类型，纳入组合 `Store` 接口）；
> `internal/core/contract/conformance`（新增五条断言，覆盖写入后能查回、
> detail 原文往返、按时间倒序、limit 生效、查无记录返回空）；
> `internal/core/fake`、`internal/store/sqlite`（各自实现 `AuditStore`，
> 两者跑同一套 conformance 断言）；`cmd/lober`（`auditlog.go` 换成落库
> 实现，失败时降级写 slog 兜底，不让审计故障升级成业务故障）；
> `internal/console`（新增 `Deps.AuditQuery`、`handleHookAudit`）。
> **不影响** `internal/core/model`、`internal/store/sqlite/schema`
> （`audit_log` 表结构不变，本次只是第一次真正读写它）。

## 响应包裹格式

所有非 SSE 端点统一返回：

```json
{"code": 0, "data": <端点各自的数据>, "message": ""}
```

`code != 0` 即失败，`message` 可直接展示给用户。这是 vben-admin 默认响应拦截器
的约定（`codeField: 'code'` / `dataField: 'data'` / `successCode: 0`），照它做
不需要改前端框架代码（SPEC §12.5）。

失败响应形态：

```json
{"code": <非零错误码>, "data": null, "message": "<可展示的中文错误说明>"}
```

HTTP 状态码固定为 `200`（框架侧按 body 里的 `code` 字段判定成功与否，不看
HTTP 状态码）；唯一例外是认证中间件本身拒绝请求（未带 `Authorization` 头或
token 无效）时返回 HTTP `401`，body 仍为上述失败形态。

## 错误码表

> **偏差声明：** 计划要求错误码表引用 `internal/apierr`。截至本任务执行时，
> `internal/apierr` 包尚未创建（M1 任务 1 的产出文件清单里没有它，SPEC.md
> 全文也没有列出具体的十个码）。下表是本任务按 SPEC §8.2/§9.1/§9.4 涉及的
> 失败场景设计的十个错误码，供 M6 在创建 `internal/apierr` 时直接采用；
> 若 `internal/apierr` 已存在具体实现，以其为准，本表需同步更新。

| code | 常量名建议 | 含义 | 典型触发端点 |
|---|---|---|---|
| 0 | `CodeOK` | 成功 | 全部 |
| 1001 | `CodeInvalidParam` | 请求参数格式错误或缺失必填字段 | 全部有入参的端点 |
| 1002 | `CodeUnauthorized` | 未登录或 token 失效 | 全部（中间件） |
| 1003 | `CodeForbidden` | 已登录但权限不足（§9.4 角色/权限码不满足） | 建任务、hook 编辑/审批、provider 配置、模板变更 |
| 1004 | `CodeNotFound` | 目标资源不存在（host/artifact-set/hook/template/task id 找不到） | 全部按 id 操作的端点 |
| 1005 | `CodeConflict` | 状态冲突，操作在当前状态下不允许（如对非终态模板执行删除、对非 `rebooting`/`initializing` 超时失败执行 retry 之外的合法拒绝） | `DELETE artifact-sets/{id}`、`DELETE templates/{id}`（引用完整性）、`retry` |
| 1006 | `CodeMACInvalid` | MAC 地址经 `model.NormalizeMAC` 归一化后为空（格式不合法） | `POST /hosts` |
| 1007 | `CodeValidationFailed` | hook/模板校验失败（`bash -n`/shellcheck/`params_schema` 超出子集） | `POST /hooks/{id}/validate` |
| 1008 | `CodeReferencedResource` | 资源仍被引用，无法删除（§2.7 引用完整性） | `DELETE artifact-sets/{id}` |
| 1009 | `CodeInternal` | 服务端内部错误（渲染失败、存储层错误等未归类情形） | 全部 |

---

## 端点总表

### 认证（4 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 1 | POST | `/console/v1/auth/login` | 无（登录本身） | `{ accessToken }` |
| 2 | GET | `/console/v1/user/info` | 已登录 | `UserInfo`（含 `roles: string[]`） |
| 3 | GET | `/console/v1/auth/codes` | 已登录 | 权限码 `string[]` |
| 4 | POST | `/console/v1/auth/logout` | 已登录 | `null` |

### 主机清单（2 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 5 | GET | `/console/v1/hosts` | 已登录（只读，`viewer` 即可） | `HostListItem[]` |
| 6 | POST | `/console/v1/hosts` | `admin`（手工录入兜底，§7.4） | 新建的 `HostListItem` |

### 制品集（4 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 7 | POST | `/console/v1/artifact-sets` | `admin`（`artifact:import`） | 新建的 `ArtifactSetItem`（`state = importing`） |
| 8 | GET | `/console/v1/artifact-sets` | 已登录 | `ArtifactSetItem[]` |
| 9 | POST | `/console/v1/artifact-sets/{id}/verify` | `admin` | `null`（异步重算，立即返回接受） |
| 10 | DELETE | `/console/v1/artifact-sets/{id}` | `admin` | `null` |

### hook（9 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 11 | POST | `/console/v1/hooks` | `hook-author`（`hook:write`） | 新建的 `HookItem`（`state = draft`） |
| 12 | GET | `/console/v1/hooks?stage=&os_family=` | 已登录 | `HookItem[]` |
| 13 | PUT | `/console/v1/hooks/{id}` | `hook-author`（`hook:write`） | 新版本 `HookItem` |
| 14 | POST | `/console/v1/hooks/{id}/validate` | `hook-author` | `HookValidateResult` |
| 15 | GET | `/console/v1/hooks/{id}/versions` | 已登录 | `HookItem[]` |
| 16 | POST | `/console/v1/hooks/{id}/approve` | `hook-approver`（`hook:approve`） | 更新后的 `HookItem` |
| 28 | POST | `/console/v1/hooks/{id}/reject` | `hook-approver`（`hook:approve`） | 更新后的 `HookItem`（附回显的 `reason`） |
| 29 | GET | `/console/v1/hooks/{id}/audit?limit=` | `hook-author`（`hook:write`）或 `hook-approver`（`hook:approve`） | `AuditRecordItem[]`（本次变更新增，见文首「变更记录」） |

### 模板（4 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 17 | POST | `/console/v1/templates` | `admin` | 新建的 `TemplateItem`（`state = draft`） |
| 18 | PUT | `/console/v1/templates/{id}` | `admin` | 新版本 `TemplateItem` |
| 19 | GET | `/console/v1/templates/{id}/form-schema` | 已登录 | 聚合后的 JSON Schema |
| 20 | POST | `/console/v1/templates/{id}/preview` | 已登录 | `PreviewResult` |

### 任务（7 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 21 | POST | `/console/v1/tasks` | `operator`（`task:create`） | 新建的 `TaskItem`（`state = pending`） |
| 22 | GET | `/console/v1/tasks?state=&sn=` | 已登录 | `TaskItem[]` |
| 23 | GET | `/console/v1/tasks/{id}/events` | 已登录 | `TaskEventItem[]` |
| 24 | GET | `/console/v1/tasks/{id}/stream` | 已登录 | SSE 事件流（不套响应包裹，见「约定 4」） |
| 25 | POST | `/console/v1/tasks/{id}/retry` | `operator`（`task:retry`） | 更新后的 `TaskItem` |
| 26 | POST | `/console/v1/tasks/{id}/cancel` | `operator`（`task:cancel`） | 更新后的 `TaskItem` |

### 磁盘 dry-run（1 个）

| # | 方法 | 路径 | 权限 | 返回 |
|---|---|---|---|---|
| 27 | POST | `/console/v1/disk-policy/dryrun` | 已登录 | `DiskPlanResult` |

---

## 逐组说明

### 1. 认证

#### `POST /auth/login`

**入参：** `{ "username": string, "password": string }`

**成功：** `data = { "accessToken": string }`。不做 refresh token
（`enableRefreshToken` 默认 false），单 access token（SPEC §12.5）。

**失败：** 用户名或密码错误 → `CodeUnauthorized`（`message` 统一为「用户名或密码
错误」，不区分「用户不存在」与「密码错误」两种情况，避免帮攻击者做用户名枚举）。

#### `GET /user/info`

**成功：** `data = UserInfo`：

```json
{"username": "string", "realName": "string", "roles": ["operator"]}
```

`roles` 元素取值为 SPEC §9.4 五个角色之一：`viewer`/`operator`/`hook-author`/
`hook-approver`/`admin`。

#### `GET /auth/codes`

**成功：** `data = string[]`，权限码集合。取值域见 SPEC §9.4 权限码表：
`task:create`、`task:retry`、`task:cancel`、`hook:write`、`hook:approve`、
`provider:write`、`artifact:import`。

#### `POST /auth/logout`

**成功：** `data = null`。服务端使当前 access token 失效（若采用有状态 token；
若采用无状态 JWT 则本端点仅为前端清理本地状态提供语义上的一致落点，服务端不
维护黑名单——MVP 阶段选择由 M6 实现时决定，不在本文档冻结）。

---

### 2. 主机清单

字段名与 `model.Host` 逐一对应，**不另起名字**：

```json
{
  "id": "string",
  "sn": "string",
  "mac": "string",
  "uuid": "string",
  "arch": "x86_64",
  "firmware": "uefi",
  "rack": "string",
  "lastSeenAt": 1695000000
}
```

`arch` 取值域为 `model.Arch`（`x86_64`/`arm64`）；`firmware` 取值域为
`model.FirmwareMode` 的机器可上报子集（`bios`/`uefi`，不含 `any`——`any` 只在
`ArtifactSet` 上出现）。`id` 是主机清单表的自有主键，不是 SN/MAC/UUID 三者
之一——这是为了让「建任务的入参是主机清单 id」（约定 3）能稳定引用一条主机
记录，即便 SN/MAC/UUID 某个字段后续被修正。

#### `GET /hosts?q=`

**Query 参数：**

| 参数 | 必需 | 说明 |
|---|---|---|
| `q` | 否 | 文本搜索，匹配 `sn`/`mac`/`rack` 子串（不区分大小写） |
| `limit` | 否 | 默认 1000，见「约定 2」 |
| `offset` | 否 | 默认 0 |

**成功：** `data = HostListItem[]`（数组，见约定 2，不带总数）。

**这是建任务页 SN 字段的唯一数据源**（SPEC §4.3、§12.5：`host-select` 组件从
本端点取值，不是 `input`）。`q` 参数不是便利功能：主机数超过一页而运维找不到
目标机器时，若没有搜索手段就会想办法绕过这个约束（比如去改前端传参直接拼 SN
自由文本）——而这是删掉 armed 状态后唯一的人工确认点（SPEC §9.1 第二道闸门、
§10 失败模式 2b），**必须提供**，不能因为「主机数不多」而省略。

#### `POST /hosts`

手工录入兜底（SPEC §7.4：discovery 尚未覆盖到的机器）。

**入参：**

```json
{"sn": "string", "mac": "string", "uuid": "string", "arch": "x86_64", "firmware": "uefi", "rack": "string"}
```

`arch` 与 `firmware` 必填（SPEC §4.3 末条：这两项不是查找键，但是建任务时过滤
可选 OS 列表与 §4.6 校验基准所必需，缺失会让下游校验形同虚设）。`sn` 与 `mac`
也必填；`uuid` 可选（SPEC §4.3 的三个查找键容忍留空，但 SN/MAC 是主要判据）。

**处理：** MAC 入库前经 `model.NormalizeMAC` 归一化；归一化结果为空串
（格式不合法）→ 拒绝，返回 `CodeMACInvalid`，**不接受空 MAC 入库**
（model 包作者说明第 5 点：不能把非法值静默存成空值）。

**成功：** `data = HostListItem`（含服务端分配的 `id`）。

---

### 3. 制品集

字段名与 `model.ArtifactSet` 对应：

```json
{
  "id": "string", "name": "string",
  "osFamily": "rhel", "osVersion": "string", "arch": "x86_64", "firmware": "any",
  "sourceIsoSha256": "string", "state": "importing",
  "manifestSha256": "string",
  "paths": {"kernel": "string", "initrd": ["string"], "repo": "string", "extra": {}}
}
```

`state` 取值域为 `model.ArtifactSetState`（`importing`/`ready`/`failed`/
`deprecated`）。

#### `POST /artifact-sets`

**入参：** `{ "sourcePath": "string", "name": "string" }`（气隙环境以登记服务器
本地路径为主，SPEC §2.1）。**MVP 阶段不实现 WebUI 直传大文件**——本端点只接受
`sourcePath`，浏览器上传作为 Phase 2 的补充能力，不在本次冻结的入参形态内。

**成功：** `data = ArtifactSetItem`（`state = importing`），导入是异步 job，
本端点立即返回，进度由前端轮询 `GET /artifact-sets` 或未来的 SSE 端点观察
（MVP 未定义制品集专属的 SSE，前端轮询列表即可，规模是十位数量级的制品集）。

#### `GET /artifact-sets`

**成功：** `data = ArtifactSetItem[]`（数组，同约定 2）。

#### `POST /artifact-sets/{id}/verify`

重算 `manifestSha256` 校验完整性。**成功：** `data = null`，异步执行，结果体现
在下次 `GET /artifact-sets` 的 `state` 字段（校验失败置 `failed`）。

#### `DELETE /artifact-sets/{id}`

**前置条件（SPEC §2.7）：** 仅 `state = deprecated` 且未被任何 `active`
template 引用时可删除。

**失败：**

| 情形 | code |
|---|---|
| `state != deprecated` | `CodeConflict` |
| 仍被引用 | `CodeReferencedResource` |

---

### 4. hook

字段名与 `model.Hook` 对应：

```json
{
  "id": "string", "name": "string", "description": "string",
  "stage": "pre", "osFamilies": ["rhel"], "interpreter": "bash",
  "body": "string", "bodySha256": "string",
  "paramsSchema": "string",
  "requires": {"contractVersion": ">=1", "facts": ["LOBER_SN"], "capabilities": []},
  "required": true, "timeoutSec": 300, "version": 1, "state": "draft",
  "author": "string", "approver": "string", "approvedAt": 0
}
```

`stage` 取值域为 `model.HookStage`（`pre`/`post-nochroot`/`post`/`firstboot`）；
`state` 取值域为 `model.HookState`（`draft`/`review`/`active`/`deprecated`）。

#### `POST /hooks`

**入参：** 除 `id`/`bodySha256`/`version`/`state`/`approver`/`approvedAt`
外的全部字段（这五项由服务端计算/管理：`bodySha256` 由 `model.HookDigest(body)`
算出，`version = 1`，`state = draft`）。

**成功：** `data = HookItem`。

#### `GET /hooks?stage=&os_family=`

两个 query 参数均可选，均为精确匹配过滤（不做子串搜索）。`os_family` 匹配
`osFamilies` 数组包含该值，或该 hook 的 `osFamilies` 为空（对应
`model.Hook.SupportsFamily` 的语义：空列表表示不限家族）。

**成功：** `data = HookItem[]`（同约定 2）。

#### `PUT /hooks/{id}`

建新版本，**不改旧版本**（hook 正文不可变，SPEC §2.2、§9.4）。

**入参：** 同 `POST /hooks`。

**成功：** `data = HookItem`（新记录，`version = 旧版本 + 1`，`bodySha256`
按新 `body` 重算，`state` 重置为 `draft`——修改正文即回到未审批状态）。

#### `POST /hooks/{id}/validate`

**成功：** `data = HookValidateResult`：

```json
{"passed": true, "issues": [{"level": "error", "message": "string"}]}
```

校验内容：`bash -n`（或对应 interpreter 的语法检查）、shellcheck（若
interpreter 为 bash）、`requires` 里声明的 `facts` 是否为 `model.IsFactV1`
承诺的键、`paramsSchema` 是否落在 SPEC §12.7 定义的 JSON Schema 子集内。

**注意：** `passed = false` 时 HTTP/`code` 仍为成功（这是「校验结果」不是
「请求失败」），`issues` 非空即代表未通过。

#### `GET /hooks/{id}/versions`

**成功：** `data = HookItem[]`，该 `id` 对应 hook 的全部历史版本，按 `version`
升序。

#### `POST /hooks/{id}/approve`

`draft → review → active` 单向流转，每次调用推进一步（不能跳步）。

**失败：** 当前 `state = active` 或 `deprecated`（已是终态或已审批完成）时
→ `CodeConflict`。

**成功：** `data = HookItem`（更新后的 `state`，若推进到 `active` 则同时写入
`approver` 与 `approvedAt`）。

**权限细分：** `draft → review` 允许 `hook-author` 自己发起；
`review → active` 必须 `hook-approver`（`hook:approve`），且**审批人不能等于
作者**（SPEC §9.4：`pre` 阶段 hook 能碰分区，需要独立审批权限，双人复核的
前提是审批人与作者不是同一人）——违反 → `CodeForbidden`。

#### `POST /hooks/{id}/reject`（本次变更新增，见文首「变更记录」）

`review → draft` 单步回退，与 `POST /hooks/{id}/approve` 对称。**不引入新
状态**——`model.HookState` 的四个取值（`draft`/`review`/`active`/
`deprecated`）是冻结契约，驳回只是把状态改回 `draft`，不是发明第五个值。

**入参：**

```json
{"reason": "string"}
```

`reason` 必填且不能是空白字符串——驳回必须能填理由，否则作者不知道为什么
被拒。为空 → `CodeBadRequest`。

**成功：** `data = HookItem` 附加一个 `reason` 字段（回显请求里的理由）：

```json
{
  "id": "string", "...": "...余下字段与 HookItem 相同，state 变回 draft",
  "reason": "string"
}
```

**失败：**

- 当前 `state` 不是 `review`（即 `draft`/`active`/`deprecated`）→
  `CodeConflict`，`message` 说明具体原因（`draft` 驳回没有意义，还没提交
  审批；`active` 已经生效，要停用应该用 `deprecate` 而不是 `reject`；
  `deprecated` 已是终态）。
- 无 `hook:approve` 权限码 → `CodeForbidden`（驳回同样是审批权，不是作者
  自己能做的事，与 approve 的 `review → active` 分支权限要求一致）。
- 审批人（此处即驳回人）等于作者 → `CodeForbidden`（与 approve 的
  「审批人不能等于作者」对称：作者不能驳回自己的 hook）。

**不变量：** 驳回不改动 `body`/`bodySha256`。SPEC §2.7 要求 hook 正文
不可变、模板 pin 住的是 digest——驳回只是状态回退，绝不触碰正文，否则就
是「审批过的内容被偷换」的反面案例。

**理由的可查性（原契约缺口，已在本次变更中补上，见下方
`GET /hooks/{id}/audit`）：** `model.Hook` 没有专门字段存驳回理由（该
struct 是冻结契约，本次变更不改它）。理由通过两条路径暴露：(1) 本端点的
响应体原样回显；(2) 写入一条 `action = "hook.reject"` 的审计日志
（`internal/console/audit.go` 的 `AuditLogger.Log`，`detail` 字段含理由
原文）。此前（本文档更早版本记录的缺口）审计日志只能写、不能读：
`contract.AuditLogger` 接口只有 `Log` 方法，全项目没有任何读取/查询审计
日志的存储方法或 HTTP 端点，理由的“可查”只在驳回发生的**当次响应**里
对调用方成立，作者若当时不在场就永远查不到。本次变更新增了
`contract.AuditStore`（查询面，`internal/core/contract/store.go`）与
`GET /hooks/{id}/audit`（下方端点），补上了这条读路径——完整取舍与
两层缺口的说明见 `docs/real-machine/05-审计持久化.md`。

#### `GET /hooks/{id}/audit?limit=`（本次变更新增，见文首「变更记录」）

按 `hook id` 查该 hook 的审计记录，**按时间倒序**返回（最新的在前）。
`limit` 可选，默认 1000（与其它列表端点的默认上限一致，见约定 2 的
`defaultListLimit`）。

**成功：** `data = AuditRecordItem[]`：

```json
[{"ts": 0, "actor": "string", "action": "string", "target": "string", "detail": "string"}]
```

字段与 `contract.AuditRecord` 对应；`target` 恒等于路径里的 `{id}`
（本端点已经按 target 过滤，附带这个字段只是让响应形态自描述，不需要
调用方从 URL 反推）。**没有任何审计记录时返回空数组，不是错误**——一个
从未被驳回过的 hook 没有审计记录是正常状态。

**权限：** `hook-author`（`hook:write`）或 `hook-approver`
（`hook:approve`）之一即可（`admin` 通过角色聚合权限码天然满足，见
`internal/console/rbac.go` 的 `roleCodes` 表）。**不开放给 `operator`/
`viewer`**——审计记录含操作人信息（`actor` 字段），SPEC §9.4 按敏感操作
分级授权，与这个 hook 的审批流程无关的角色不应看到谁驳回过、驳回了什么
（这与「查看的是哪个 hook」无关，是角色本身的权限边界）。无权限 →
`CodeForbidden`。

**为什么按 hook id 而不是全局 `/audit?target=`（设计取舍）：** 全局端点
需要更粗放的权限（至少 admin），否则任何持有任意一个审计相关权限码的
用户都能查看全部资源的审计流；本端点挂在 `/hooks/{id}` 之下，天然把
授权范围收窄到「与这一个 hook 有关的人」，不需要额外发明一个更高权限级
别。当前唯一的消费场景（hook 页面查驳回理由）也只需要按 hook 维度查，
全局查询留给确有需要时再补（那时可以复用同一个 `contract.AuditStore`，
不需要改契约）。

---

### 5. 模板

字段名与 `model.Template` 对应：

```json
{
  "id": "string", "name": "string",
  "artifactSetId": "string", "answerTemplate": "string",
  "hookRefs": [{"hookId": "string", "hookDigest": "string", "stage": "pre", "order": 10, "required": true, "paramsDefaults": {}}],
  "kernelArgs": "string", "diskPolicy": "first", "onFailure": "reboot",
  "requiredFormFields": ["string"], "version": 1, "state": "draft"
}
```

`diskPolicy` 取值域为 `model.DiskPolicy`；`onFailure` 取值域为
`model.OnFailurePolicy`；`state` 取值域为 `model.TemplateState`。

#### `POST /templates`

**入参：** 除 `id`/`version`/`state` 外的全部字段。`hookRefs` 中的 `hookDigest`
由服务端按 `hookId` 查出该 hook **当前 active 版本**的 `bodySha256` 自动填入
（前端不传，或传了服务端也以查出的值为准）——pin 的是内容而非引用（SPEC §2.7）。

**成功：** `data = TemplateItem`（`version = 1`，`state = draft`）。

#### `PUT /templates/{id}`

建新版本。**入参/成功** 同 hook 的 `PUT` 端点语义（新记录，`version + 1`，
`state` 重置为 `draft`）。

#### `GET /templates/{id}/form-schema`

**成功：** `data` 为 JSON Schema 对象（SPEC §8.3、§12.7），由三段聚合：
固定字段（`sn`/`os`/`version`/`hostname`/`bizIp`）+
`template.requiredFormFields` + 该模板全部 `hookRefs` 的 `paramsSchema` 合并
去重。**这个端点不套 `{code,data,message}` 之外的额外包装**——`data` 本身就是
可直接喂给前端 schema-to-form 映射层（`src/adapter/form-schema.ts`）的 JSON
Schema，不再嵌一层。

#### `POST /templates/{id}/preview`

**入参：** `{ "hostId": "string", "formData": {} }`（假数据渲染用，`hostId`
可选——不传则用一份服务端内置的假 `Host`）。

**成功：** `data = PreviewResult`：

```json
{"answerFile": "string", "bootMenu": "string", "warnings": ["string"]}
```

`answerFile` 是渲染出的应答文件全文，`bootMenu` 是渲染出的 iPXE 菜单文本，
`warnings` 是非致命提示（如某个 hook 参数用了默认值）。**渲染失败时返回
`CodeInternal`，`message` 带具体渲染错误**——这个端点存在的意义就是把「建任务
→ 上电 → 等数分钟 → 失败 → 翻日志」的反馈周期压到秒级（SPEC §8.2），错误信息
必须直接可读。

---

### 6. 任务

字段名与 `model.Task` 对应（省略 `token`/`finalizeToken` 两个敏感字段——
**人面 API 不回传任务的装机凭据**，这不是字段名对齐问题，是安全边界：
`GET /tasks` 与 `GET /tasks/{id}` 系列端点的响应中不包含 `token`、
`tokenExpiresAt`、`finalizeToken`、`finalizeExpiresAt` 四个字段）：

```json
{
  "id": "string", "createdAt": 0, "createdBy": "string",
  "hostSn": "string", "hostMac": "string", "hostUuid": "string",
  "templateId": "string", "templateVersion": 1,
  "artifactSetId": "string", "hookDigests": ["string"],
  "formData": {}, "resolvedParams": {},
  "state": "pending", "stateUpdatedAt": 0, "version": 1,
  "currentPhase": "pending", "phaseEnteredAt": 0,
  "attemptCount": 0, "lastSeq": 0, "lastSignalAt": 0,
  "failureReason": "", "failureDetail": null
}
```

`state` 取值域为 `model.TaskState`；`currentPhase` 取值域为 `model.Phase`；
`failureReason` 取值域为 `model.FailureReason`（空串表示未失败）；
`failureDetail` 为 `null` 或 `model.FailureDetail` 对应结构：

```json
{"phase": "packages", "exitCode": 1, "hookDigest": "", "logTail": "string", "syslogTail": "string", "sinceLastSig": "string"}
```

#### `POST /tasks`

**建任务即授权装机，无 arm/disarm**（SPEC §3.2、§12.5）。

**入参：**

```json
{"hostId": "string", "templateId": "string", "formData": {}}
```

**关键约定（约定 3）：入参是主机清单 id，不是 SN 字符串。** 服务端据
`hostId` 从主机清单查出 `sn`/`mac`，写入新任务的 `hostSn`/`hostMac`——两者
构成引导时 `/v1/boot` 的双因子（SPEC §4.3、§9.1 第二道闸门）。**本端点不接受
直接传入 `hostSn`/`hostMac` 字段**，杜绝自由文本绕过主机清单选择这一唯一的
人工确认点。

**成功：** `data = TaskItem`（`state = pending`，`resolvedParams` 由
`formData` 与 provider 决策结果、系统生成值合并算出，`templateVersion`/
`artifactSetId`/`hookDigests` 按 SPEC §2.6 pin 住模板当前 active 版本的具体值）。

**失败：** `hostId` 查不到 → `CodeNotFound`；`templateId` 非 `active` 状态
→ `CodeConflict`（不允许用 draft/deprecated 模板建任务）。

#### `GET /tasks?state=&sn=`

**Query 参数：** 均可选，`state` 精确匹配 `model.TaskState` 之一，`sn` 精确
匹配 `hostSn`（不是子串搜索——SN 本身就是精确标识，不需要模糊查询）。

**成功：** `data = TaskItem[]`（同约定 2，`limit` 默认上限 1000）。

#### `GET /tasks/{id}/events`

**成功：** `data = TaskEventItem[]`，字段与 `model.TaskEvent` 对应：

```json
{"taskId": "string", "seq": 0, "ts": 0, "phase": "pre", "level": "info", "message": "string", "rc": 0, "hookDigest": "", "source": "signal"}
```

按 `seq` 升序返回全量（不分页——单任务事件量在百级，SPEC §2.5 的日志正文
不入库设计已经把体量控制住）。

#### `GET /tasks/{id}/stream`

见「约定 4」。

#### `POST /tasks/{id}/retry`

重置 `attemptCount` 为 0，签发新 token（旧 token 立即失效），状态置回
`pending`。

**拒绝条件（SPEC §3.4、§8.2 显式要求）：** 当前 `failureReason` 对应
`Reinstallable() == false`（即 `model.FailureReasonRebootTimeout`/
`FailureReasonInitTimeout`/`FailureReasonInitFailed` 三者之一）时 **必须拒绝**，
返回 `CodeConflict`——这类任务的机器可能已经装好、盘上已有数据，重装会擦掉
（M1 model 包作者说明：`Reinstallable()` 是白名单查表，M6 调用它判断，不自己
重新实现判定逻辑）。

**成功：** `data = TaskItem`（更新后）。

#### `POST /tasks/{id}/cancel`

**成功：** 状态置为终态（`failed`，`failureReason` 记为一个表示「人工取消」的
值——对应 `model.FailureReasonCanceled`），`data = TaskItem`。取消后该主机的
后续 `/v1/boot` 请求查不到 pending 任务，视为无任务处理（不再响应该主机的
引导请求，SPEC §8.2）。

**失败：** 任务已处于终态 → `CodeConflict`（终态不可二次取消）。

---

### 7. 磁盘 dry-run

#### `POST /disk-policy/dryrun`

**入参：** `{ "diskPolicy": "raid1-ssd", "facts": {} }`（`facts` 是一份模拟的
硬件采集结果，字段形如 `LOBER_DISKS` 的实际取值）。

**成功：** `data = DiskPlanResult`：

```json
{"chosenDisk": "string", "reasoning": "string"}
```

不落地，纯计算——这是把装机模板调试周期从「上电等十分钟」压到秒级的另一个
端点（与 `preview` 同精神，SPEC §8.2）。

---

## 约定（四条，逐条冻结）

### 约定 1：主机清单两个端点

见「2. 主机清单」整节。**字段名与 `model.Host` 逐一对应，不另起名字**——
`arch` 与 `firmware` 必填，MAC 入库前归一化、非法值拒绝（`CodeMACInvalid`）。
**`GET /hosts` 是建任务页 SN 字段的唯一数据源**，不接受自由文本；`q` 文本搜索
参数不是便利功能，是删掉 armed 状态后唯一的人工确认点在主机规模超过一页时
仍然可用的必要条件（SPEC §9.1、§10 失败模式 2b）。

### 约定 2：列表端点的分页

**`data` 就是数组，不带总数**，前端做客户端分页。适用于全部返回 `T[]` 的
端点：`hosts`、`artifact-sets`、`hooks`、`hooks/{id}/versions`、`tasks`。

`limit`/`offset` 保留，但作用是**防止一次拉出几万行**（默认上限 1000），
**不是给分页器用**。理由是规模：内网装机控制台的主机与任务都是百级，
服务端分页要给每个列表查询加一个 count 查询，换来的只是几百行数据的传输
节省，性价比不成立。**任务表历史数据稳定顶到 1000 上限时再加总数字段**，
那是一次独立的契约变更，不在本次冻结范围内预先设计。

### 约定 3：建任务的入参是主机清单 id，不是 SN 字符串

见「6. 任务」的 `POST /tasks` 小节。服务端据 `hostId` 取出 `sn` 与 `mac`
写进任务，两者构成引导时的双因子（SPEC §4.3）。

### 约定 4：SSE 端点不套响应包裹

`GET /tasks/{id}/stream` 是事件流，**不返回 `{code,data,message}` 结构**。

**事件格式：**

```
id: 110
event: task-event
data: {"seq":110,"ts":1695000000,"phase":"done","level":"info","message":"ready","rc":0,"hookDigest":"","source":"signal"}

```

（每条事件以空行结束，符合 SSE 规范；`data` 字段内容是单行 JSON，对应
`model.TaskEvent` 的 JSON 序列化。）

- **`id` 取自事件序号**：即该 `TaskEvent.Seq`（信号来源为服务端信号表序号；
  非信号来源如 syslog/nginx/heartbeat/provider 用递减负数序号，与 fake Store
  的约定一致，见 M1 任务 17 设计要点）。
- **重连带 `Last-Event-ID` 续传**：客户端断线重连时通过 `Last-Event-ID`
  请求头带上收到的最后一个 `id`，服务端从该 `seq` 之后续传，**不从头重放**
  （SPEC §12.8）。
- **终态时服务端主动关流**：任务进入 `done` 或 `failed`（`TaskState.IsTerminal()`
  为真）后，服务端发送该终态事件后主动关闭连接，不等客户端断开。
- **三个必需响应头：**

  | 响应头 | 值 | 作用 |
  |---|---|---|
  | `Content-Type` | `text/event-stream` | SSE 标准 |
  | `Cache-Control` | `no-cache` | 防中间层缓存事件流 |
  | `X-Accel-Buffering` | `no` | nginx 关缓冲的兜底（SPEC §1.4、§12.8：即便 nginx 配置忘了关 `proxy_buffering`，这个头也能让 nginx 不缓冲响应体） |

  服务端设置这三个头，**不依赖 nginx 配置正确**作为唯一保障——nginx 侧的
  `proxy_buffering off` 是第一层，`X-Accel-Buffering: no` 是第二层兜底
  （两层都做，因为 nginx 配置是部署期产物，Go 侧响应头是代码产物，后者更
  容易保证不被遗漏）。

**认证：** 与其它端点相同（`Authorization` 头），**不接受 token 进 query
string**——`EventSource` 原生 API 不能设请求头，但本契约要求前端用 `fetch` +
`ReadableStream` 读取（不用 `EventSource`），因此 `Authorization` 头可以照常带，
token 不会落进 nginx access log（SPEC §9.5、§12.8）。
