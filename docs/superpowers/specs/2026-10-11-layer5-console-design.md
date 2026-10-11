# 第 5 层（控制台）设计

日期：2026-10-11（2026-10-12 按第 4 层设计定稿 ISO 接缝）
状态：已确认
依据：`specs/2026-09-30-lober-rewrite-design.md`（下称总 spec）§2.3–§2.7、§3、§4.5、§5.2–§5.3、§6.1–§6.4、§7.2 第 5 条、§7.4
第 3 层交接：`plans/2026-10-09-rewrite-layer3-render.md`「留给后续层的约定」第 1 条（预览、示例值、装载内置内容、过滤旧身份）
第 4 层设计：`specs/2026-10-11-layer4-iso-design.md` §4.5、§6、§12
两份遗留问题清单归第 5 层的条目：L-8（全局词表与 `Subject.Kind` 常量）、S3-10（挂在 L-8 上）

## 1. 目标与范围

给人用的全部 HTTP 接口，以及第一次出现的装配点。它只做编排：从仓储取数据，交给第 1 层判定，把结果写回仓储，自己不写业务规则（总 spec §4.5）。

不在本层：

| 事项 | 归谁 |
|---|---|
| ISO 来源的真实实现、启动时的获取/导入恢复 | 第 4 层 |
| 网关、状态机、事件归档 | 第 7 层 |
| 前端页面、由 OpenAPI 生成 TS 类型 | 第 6 层 |
| 逐来源、逐分片的导入进度 | 第 4 层的 `store.ISOSources` 落地后 |

**不依赖第 4 层。** 总 spec §6.1 的依赖方向是「控制台依赖第 1 层、存储契约里自己用到的能力，以及自己声明的接口」。第 4 层尚未实现，控制台对它的全部需要压缩成一个自己声明的窄接口（§6），测试里用内存 mock 顶上。

## 2. 已定的决定

| 决定 | 由谁定 |
|---|---|
| 全层一次做完：主机、ISO、hook、模板、向导、任务、用户与权限、审计 | 用户，2026-10-11 |
| 只 mock ISO 来源；仓储、第 1 层判定、渲染、内置内容全用真的 | 用户，2026-10-11 |
| 用户、角色与会话本轮完整实现，不推后 | 用户，2026-10-11 |
| 会话放内存、不建表；用户与角色落库（§5） | 本设计 |
| ISO 接缝按总 spec §6.2 第 6 条由控制台声明，参数只用 `model.ISO` 与原始类型（§6） | 用户，2026-10-12 |
| 导入进度本轮只回 ISO 记录的粗状态 | 用户，2026-10-12 |
| 口令散列用标准库 `crypto/pbkdf2`，不引 `x/crypto` | 本设计 |
| gowrap 改手写装饰器（§8） | 本设计，依据总 spec §8「维护状态没核实就改成手写」 |
| 分支从 `feat/render` 开出，不从 `dev`（§10） | 本设计 |

## 3. 包与文件

| 包 | 职责 | 可以依赖 |
|---|---|---|
| `internal/console` | HTTP API：路由表、信封、状态码映射、认证与权限、各资源处理函数、OpenAPI 文档、SSE | 标准库、`gin-gonic/gin`、`internal/core/...`、`internal/store`、`internal/render`、`internal/builtin` |
| `cmd/lober` | 装配点：配置、wire 组合、装载内置内容、优雅退出；ISO 来源与任务控制的占位实现 | 全部模块（守卫里唯一这样的包） |
| `internal/store` | 契约加 `Users` 聚合、`User`/`Role` 类型 | 标准库、层 1 |
| `internal/store/pgstore` | 实现 `Users`，加 `users` 表 | 层 1、store、gorm、pgx |
| `internal/archtest` | 加「控制台」与「装配点」两条规则 | — |

不给 `internal/config`、`internal/apierr` 单开包：配置并进 `cmd/lober`，错误映射是 console 的一部分。少两个包就少两条守卫规则，而这两件事都没有第二个消费者。

## 4. HTTP 面

### 4.1 响应信封

沿用 vben-admin 默认拦截器的约定（`codeField: 'code'` / `dataField: 'data'` / `successCode: 0`），第 6 层不必改框架代码：

```json
{"code": 0, "data": <端点数据>, "message": ""}
```

失败时 **`data` 装结构化详情，`message` 装可展示的中文汇总**：

| 失败种类 | data |
|---|---|
| 第 1 层判定、`ProblemsError` | `{"problems": [{code, subject, message, next}]}` |
| 批量建任务（`BatchError`） | `{"common": [...], "byHost": {"SN-1": [...]}}`，与 `admission.BatchResult` 同形 |
| 404/409/500 这类没有逐条原因的 | `null` |

`message` 取第一条问题的 `Message`；没有问题时用该错误码的中文兜底。前端拿 `data.problems[].subject` 把错误标到具体的行或单元格（总 spec §6.2 第 1 条）。

### 4.2 状态码映射

一条规则说清：**422 = 读懂了，但内容不合规矩，逐条原因在 `data.problems` 里；400 = 请求本身读不懂或入参违反前提。**

| 来源 | 码 |
|---|---|
| gin 绑定 / JSON 解析失败、缺必填 | 400 |
| `store.ErrInvalid`（ID 格式、时间、Rev 缺失、空批） | 400 |
| 未登录、会话失效 / 权限码不够 | 401 / 403 |
| `store.ErrNotFound` | 404 |
| `CategoryIncompatible`、`CategoryState`、`ErrConflict`/`ErrDuplicate`/`ErrImmutable`/`ErrReferenced` | 409 |
| `CategoryInvalid`、`CategoryIncomplete`、`ProblemsError`、`BatchError` | 422 |
| `ErrAuditRequired`、未识别错误 | 500 |

### 4.3 路由表是唯一事实源

```go
type route struct {
    method, path string
    perm         string   // 权限码；空串表示免登录（只有登录本身）
    audit        string   // 审计动作；空串表示不审计
    tag          string   // OpenAPI 分组
    summary      string   // OpenAPI，必填
    req, resp    any      // OpenAPI 的 schema 名，可空
    handle       gin.HandlerFunc
}
```

OpenAPI 文档由这张表生成，挂在 `GET /openapi.json`（在 `/console/v1` 外面，不随 API 版本走）。

### 4.4 角色与权限码

角色是 `store.Role`（落库），权限码是 console 的一张表（不落库）：角色是数据，权限是界面规则。

| 权限码 | viewer | operator | hook-author | hook-approver | admin |
|---|---|---|---|---|---|
| `host:read` `iso:read` `hook:read` `template:read` `task:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `host:write` `task:create` `task:cancel` `task:retry` | | ✓ | | | ✓ |
| `hook:write` `template:write` | | | ✓ | | ✓ |
| `hook:approve` `template:approve` | | | | ✓ | ✓ |
| `iso:write` `user:manage` `audit:read` | | | | | ✓ |

`admin` 是全部权限码，不逐条列——加新权限码时忘了给 admin，由测试抓到。

### 4.5 认证与审计的走法

- 登录：`POST /console/v1/auth/login` → 32 字节 `crypto/rand` 的 hex token。**会话在内存里**（`token → {用户名, 到期时间}`，TTL 12 小时，时钟由装配点注入）。
- 中间件链：panic 兜底 → 请求日志 → 认证（`Authorization: Bearer`；**SSE 那条路额外收 `?access_token=`**，因为 `EventSource` 发不了头）→ 权限 → 处理函数。
- 审计：中间件按「当前用户 + 路由表声明的动作 + 注入时钟」准备，**处理函数用 `console.AuditFrom(c, detail)` 取到手再交给仓储**。路由声明了动作而处理函数传了零值 `Audit`，仓储报 `ErrAuditRequired` → 500，测试立刻红。这是总 spec §6.3「漏了声明，测试里会直接报错」的落点。
- 口令散列：`crypto/pbkdf2` + SHA-256，60 万次迭代，32 字节输出，16 字节随机盐，编码成 `pbkdf2-sha256$<迭代>$<盐>$<散列>`。

### 4.6 端点清单

| 分组 | 端点 |
|---|---|
| 认证 | `POST /auth/login`、`POST /auth/logout`、`GET /user/info` |
| 用户 | `GET`/`POST /users`、`PUT`/`DELETE /users/{username}` |
| 主机 | `GET /hosts`、`POST /hosts`、`POST /hosts/import?dryRun=`、`GET`/`PUT`/`DELETE /hosts/{sn}` |
| ISO | `GET /isos`、`POST /isos`（`sources` 是列表，一条主介质加该系要求的附加来源）、`GET`/`DELETE /isos/{id}`、`POST /isos/{id}/deprecate`、`POST /isos/{id}/cancel`、`POST /isos/{id}/retry`、`POST /isos/{id}/reimport`、`PUT /isos/{id}/content` |
| hook | `GET /hooks`、`POST /hooks`、`PUT /hooks/{id}/name`、`GET /hooks/{id}/versions`、`POST /hooks/{id}/versions`、`GET`/`PUT`/`DELETE /hooks/{id}/versions/{version}`、`POST .../submit` `.../approve` `.../reject` `.../deprecate`、`POST .../validate` |
| 模板 | 同上，另加 `POST /templates/{id}/derive`、`POST /templates/{id}/versions/{version}/preview`、`GET /templates/{id}/versions/{version}/slots` |
| 向导 | `GET /wizard/hosts?q=`、`POST /wizard/isos`、`POST /wizard/templates`、`POST /wizard/params`、`POST /wizard/tasks` |
| 任务 | `GET /tasks`、`GET /tasks/{id}`、`GET /tasks/{id}/events`、`GET /tasks/{id}/stream`、`POST /tasks/{id}/cancel`、`POST /tasks/{id}/retry`、`POST /tasks/{id}/preview` |
| 审计 | `GET /audit`、`GET /audit/export` |

向导五步严格照总 spec §5.3：选主机（`GET /wizard/hosts`，有未结束任务的照常列出但标 `selectable:false` 并给原因）→ 选 ISO（`POST /wizard/isos` 传选中的 SN，逐台跑 `admission.ISOUsable`）→ 选模板（`POST /wizard/templates` 传 ISO，跑 `TemplateUsable`，每个模板取 `model.CurrentApproved`）→ 填参数（`POST /wizard/params` 跑 `params.Evaluate`，逐台一张表）→ 提交（`POST /wizard/tasks`，服务端**不信任前端**，整批重跑准入、逐台重跑参数求值、逐台 `snapshot.Build`，一次 `CreateTasks`）。

CSV 导入照总 spec §5.3 拆成两步：`?dryRun=true` 只解析并逐行标出「新建/更新/错误」，操作员确认后再不带 `dryRun` 提交一次。无状态，客户端把同一个文件发两遍。

向导列候选模板时，**按 `builtin` 当前带的清单把旧身份过滤掉**（第 3 层交接的 S5-1）：`builtin.Templates()` 与 `builtin.Hooks()` 各给一份「ID + 版本」，程序升级后库里还留着旧的内置版本，向导只提供程序当前带的那些。

## 5. 用户、角色与会话

`internal/store` 加 `Users` 聚合，pgstore 加 `users` 表。**只加新表，不动第 2 层已有的任何索引与约束**——总 spec §4.2 规定改旧定义要先写升级动作，加表 AutoMigrate 就够。

```go
type User struct {
    Username    string  // 身份：标识符，1–32
    DisplayName string  // 可存文本
    Role        Role
    Password    string  // pbkdf2 散列，永不经 HTTP 出去
    Disabled    bool
    CreatedAt   int64   // Unix 秒，由调用方传入：仓储不读时钟
    Rev         int64
}
type Role string // viewer、operator、hook-author、hook-approver、admin
```

- 用户名是身份，`CreateUser` 撞名报 `DuplicateError{Field: "username"}`。
- `DELETE /users/{username}` 是禁用，不删行：审计要能一直指到这个人。禁用后会话立即失效。恢复用 `PUT` 把 `disabled` 置回 false。
- 初始管理员由装配点在库里没有用户时按配置的种子口令建一个，建完强制改密；库里已有用户时不种。
- **会话不放库**：单进程，内存 map + 互斥锁 + TTL，重启即重新登录。会话不是业务数据（总 spec §6.1「数据以仓储为准」管的是主机、任务这些），放进库里要额外处理过期清理，而 AutoMigrate 表达不了。这一条与总 spec §4.2「会话推迟到第 5 层」不冲突——那一句只定时间，没定位置。

## 6. 两个接缝：由控制台声明，装配点注入

总 spec §6.1「实现模块之间互不引用，由调用方声明一个只含所需操作的接口」。因此两个接口的参数类型只能是第 1 层或存储契约里的东西，两个实现模块都不 import console。

### 6.1 ISO 来源

```go
// internal/console/isosource.go
type ISOSource interface {
    // Register 登记一个来源并开始获取。kind 是 url、upload 或 path；addr 是链接或服务器本地
    // 路径；role 是来源的角色，空串是主介质，附加来源传 catalog 给的附加来源名（Debian 是
    // netboot，总 spec §2.1 第 54 行）；size 是浏览器上传的总字节数，0 表示未知；sha256 是
    // 期望的摘要，可空（C3）。
    Register(ctx context.Context, iso model.ISO, kind, addr, role string, size int64, sha256 string, a store.Audit) error
    // Upload 接收浏览器上传的一段字节，offset 是它在文件里的起始字节。分片大小由客户端定，
    // 第 4 层按字节区间记，不收尾：字节齐不齐由它自己判。
    Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a store.Audit) error
    Cancel(ctx context.Context, id string, a store.Audit) error    // 取消获取
    Retry(ctx context.Context, id string, a store.Audit) error     // 失败后重试获取
    Reimport(ctx context.Context, id string, a store.Audit) error  // 字节已到齐，重新导入
    // FreeSpace 报告暂存区与制品目录的余量。建 ISO 前调它（总 spec §5.1 第 1 步、
    // 第 4 层设计 §4.5），不够就拒绝并说明还差多少。
    FreeSpace(ctx context.Context) (staging, artifacts int64, err error)
}
```

参数用原始类型而不是 `Source` 结构体，是为了让 `internal/iso` **不 import console 就能满足这个接口**；`cmd/lober` 用一行 `var _ console.ISOSource = (*iso.Sources)(nil)` 静态兜住，第 4 层的签名一漂，编译就红。

**四个写方法带 `store.Audit`**（2026-10-12 定）：总 spec §6.2 第 8 条要求「登记来源」要审计，没有载体就写不进去。`store.Audit` 是存储契约类型，符合本节开头「参数只能是第 1 层或存储契约里的东西」；同节的 `TaskControl` 已经这么写。

**`Register` 与 `Upload` 带 `role`**（2026-10-12 定）：一个 ISO 可以有多条来源——主介质加该系要求的附加来源，目前只有 Debian 要同版本的官方 netboot 包（netinst ISO 里的 initrd 是光驱版，不能网络装机）。`Cancel`/`Retry`/`Reimport` 按 ISO ID 寻址，作用于它的全部来源，不需要 role。控制台一次可以登记多条来源，请求体里的 `sources` 是个列表，`role` 为空的只能有一条。

**与第 4 层设计的差异**：第 4 层设计 §2、§12 写的是「控制台经 `iso.Sources` 调本层」，那会让 console import iso，与总 spec §6.1 冲突。已定按总 spec：接口在 console 一侧，`iso.Sources` 是第 4 层那一侧的名字。**留给第 4 层的约定**：`Sources` 的方法签名要用本节这些原始类型；若第 4 层坚持用 `store.Source`，就在 `cmd/lober` 加一个几行的适配器，不要让 console 去 import `internal/iso`。第 4 层计划（e08b430）已按本节改过它的 `Sources`。

进度只读、控制台不自己写（第 4 层设计 §12）。本轮 `GET /isos/{id}` 只回 ISO 记录的 `State` 与 `Rev`；逐来源、逐分片的进度等第 4 层的存储契约落地后补 `GET /isos/{id}/progress`。

### 6.2 任务控制

取消与重试经过状态机，C34 只在状态机里实现一次（总 spec §6.2 第 4 条）。

```go
type TaskControl interface {
    Cancel(ctx context.Context, t model.Task, a store.Audit) error
    Retry(ctx context.Context, t model.Task, a store.Audit) error
    CanCancel(t model.Task) []model.Problem // 给界面置灰
    CanRetry(t model.Task)  []model.Problem
}
```

占位实现**一律返回「第 7 层未实现」**，不写临时规则。原因很具体：`model.Task` 现在没有「失败前有没有进过重启中」这个字段，第 7 层才会加时机进度；此刻写一个「failed 就能重试」的版本会直接违反 C34，比没有这个功能更糟。第 7 层换掉 `cmd/lober/taskcontrol.go` 一个文件即可。

## 7. SSE 与通知中心

`GET /console/v1/tasks/{id}/stream` 照总 spec §6.2 第 9 条：连上先按 `Seq` 补读全量事件推过去，之后收到「任务 X 有更新」的通知再增量补读；另有 5 秒兜底轮询，通知丢了不错乱。客户端断开即退订。

通知中心是 `console.Notifier` 接口（`Publish(taskID string)`）加内存实现。本轮控制台是唯一发布者（建任务、取消、重试之后）；第 7 层的状态机与事件模块装配时拿同一个。

## 8. 装配点与生成代码

启动顺序（总 spec §7.4「这时能起控制台」）：

1. 解析命令行参数 → 读配置 → 起 `slog`
2. `pgstore.Open(dsn)`：AutoMigrate + 表结构断言
3. 装载内置内容：`builtin.Templates()` / `builtin.Hooks()` 逐条 `EnsureBuiltin*`，撞 `ErrConflict` 按第 3 层交接重试，撞 `ErrBuiltinMismatch` 拒绝启动并说明原因（总 spec §6.2 第 11 条）；库里没有用户时按配置的种子口令建初始管理员
4. wire 组合出 console，挂上 ISO 来源与任务控制的实现
5. 起 HTTP 服务，`SIGINT`/`SIGTERM` 时优雅退出

**配置**：YAML 一份，路径由 `--config` 给（默认 `/etc/lober/lober.yaml`），DSN 可被 `LOBER_PG_DSN` 环境变量覆盖。本轮只放 `listen`、`pg`、`log`、`tls`（可选）、会话 TTL、初始管理员种子口令。第 4 层要的五个目录（暂存、制品、SMB、Windows 工具、允许登记的本地路径）等第 4 层再加——现在加了也没有人读。

**wire**：用。`go.mod` 里以 `tool github.com/goforj/wire/cmd/wire` 钉住 v1.2.0，`cmd/lober/wire.go` 写注入函数，`wire_gen.go` 提交入库，`make check` 重新生成并 `git diff` 比对。

**gowrap**：不用，改手写装饰器。核实结果：最新发布 v1.4.2，2024-12-11，此后近十个月无发布，早于 Go 1.25 的 `go tool` 用法。存储层的调用日志装饰器手写约 40 行，没有代码生成步骤要守，以后要换 gowrap 不影响任何调用点。总 spec §8 明文的退路就是「不合格就改成手写装饰器」。

## 9. 测试

总 spec §7.2 第 5 条逐条对应：

| 要求 | 怎么落 |
|---|---|
| 每个接口都有 HTTP 测试，连真实 PostgreSQL | `console_test` 外部包 + 一个夹具：`pgtest.Open(t)` 起仓储、固定时钟、ISO 来源用内存 mock、种子数据直接写库 |
| 权限矩阵 | 表驱动：每条路由 × 五个角色，断言 200/401/403；免登录路由单列 |
| 每个要审计的写操作都留下审计记录 | 枚举路由表里 `audit` 非空的路由，逐条打一次请求再查 `ListAudit`，断言 `TargetKind`/`TargetID`/`Actor`/`Action` |
| 批量建任务一台不过就一台都不建 | 向导提交的反例：批里混一台有未结束任务、一台平台冲突、一台参数不合法，断言 `BatchError` 且 `ListTasks` 与审计都为空 |
| 每条路由都有 OpenAPI 描述 | 遍历 gin 的路由树与文档的 paths 对账 |

纯函数（信封、状态码映射、CSV 解析、权限表、OpenAPI 文档、口令散列）走普通单测，不连库。夹具**每个顶层测试开一个仓储**，不每个子测试开——第 2 层评审提过的连接数上限在这里同样适用。

## 10. 分支与并行

第 4 层计划定了「分支从 dev 开出」，而第 3 层尚未合入 dev。第 5 层要用渲染（预览）与内置内容（装载），**因此从 `feat/render` 开出**，合并顺序是：第 3 层 → dev，第 4、5 层各自 rebase 后并入。

已知的合并冲突点只有两处，都不涉及逻辑：

- `internal/store/store.go` 的 `Store` 接口：第 4 层加 `ISOSources`，本层加 `Users`，各取各的
- `internal/store/pgstore/schema.go` 的表清单：第 4 层加两张表，本层加 `users`，各加各的

层内并行照总 spec §7.4：`feat/console` 开在主目录，层内任务各在 `.tmp/wf/` 下的本地克隆里做，逐个并回分支。

## 11. make check

现有五步（fmt/vet/test/race/cross）之外加一步 `gen`：跑 wire 重新生成，`git diff --quiet -- cmd/lober/wire_gen.go`，不一致就失败。OpenAPI 用结构化断言而不用 golden 文件——六十余条路由的 schema 手维护一份 golden 不现实，而「每条路由都有描述」「schema 名都解析得过」「编码两次相同」这三条已把总 spec §6.2 第 10 条钉死。

## 12. 任务分解

| # | 任务 | 依赖 |
|---|---|---|
| T1 | 骨架：包说明、信封、状态码映射、守卫两条新规则 | — |
| T2 | 第 1 层补全局词表与 `Subject.Kind` 常量（L-8、S3-10） | — |
| T3 | 存储层 `Users` 聚合、pgstore `users` 表、契约用例 | — |
| T4 | 路由表、权限表、OpenAPI 生成与对账测试 | T1、T2、T3 |
| T5 | 认证中间件、内存会话、登录/登出/用户信息、用户管理端点 | T4 |
| T6 | 主机：列表、增删改、CSV 两步导入 | T5 |
| T7 | ISO：列表、创建、废弃、删除 + 来源接缝、内存 mock、占位实现 | T5 |
| T8 | hook 与模板：列表、版本、读取、保存、开新版本、改名、派生 | T5 |
| T9 | hook 与模板：提交、审批、驳回、废弃、validate | T8 |
| T10 | 预览：模板预览（调 `render.Render`）、编辑器用的插槽清单 | T5 |
| T11 | 向导：候选 ISO、候选模板、参数表单、批量提交 | T5 |
| T12 | 任务：列表、详情、事件、SSE、取消/重试、任务预览 | T5 |
| T13 | 审计查询与 CSV 导出 | T5 |
| T14 | 装配点：配置、wire、内置内容装载、装饰器、优雅退出 | T6–T13 |
| T15 | 文档同步与整体验收 | T14 |

T1、T2、T3 三个互不相干，可并行。T6–T13 里，除 T8→T9 有先后，其余六个（T6、T7、T10、T11、T12、T13）彼此不碰文件。

## 13. 推迟到后续层

- **第 4 层**：`ISOSource` 的真实实现换掉占位；`Sources.Register` 用同样的原始类型签名；启动时的获取/导入恢复；配置里加五个目录；`store.ISOSources` 落地后补 `GET /isos/{id}/progress`
- **第 7 层**：`TaskControl` 的真实实现；状态机与事件模块拿到同一个通知中心；`model.Task` 加时机进度字段
- **第 6 层**：前端页面；由 `GET /openapi.json` 生成 TS 类型

## 14. 风险

| 风险 | 处置 |
|---|---|
| 内存会话让重启后的所有人掉线 | 单进程部署，重新登录即可；要多实例部署时改成 `sessions` 表，届时 `Deps.Sessions` 换实现即可，接口已隔开 |
| 第 4 层若坚持用 `store.Source` 作 `Register` 的参数，`cmd/lober` 要加适配器 | §6.1 已写成留给第 4 层的约定；适配器只有几行，且编译期断言会先报红 |
| `make check` 的 `gen` 一步依赖 `git diff`，工作树不干净时会误报 | 只对 `cmd/lober/wire_gen.go` 一个路径做 diff，且在跑生成之前先确认该文件已跟踪 |
| 第 2 层评审提过的连接数上限：console 的 HTTP 测试每个顶层测试开一个仓储 | 夹具里显式限流，超限就失败而不是把测试库的连接撑满 |
| gin 是第 5 层引入的第一个 Web 框架，白名单从零建 | 只在 `ruleConsole` 里放行 `gin-gonic/gin` 一家，其余第三方一律违规 |
