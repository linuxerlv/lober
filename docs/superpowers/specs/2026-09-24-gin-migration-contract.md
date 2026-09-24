# gin 迁移契约 — HTTP 层从标准库迁到 gin

| | |
|---|---|
| 版本 | 1.0（契约定稿，实现前） |
| 日期 | 2026-09-24 |
| 状态 | 冻结 —— 并行开发期间本文档不再变更 |
| 影响 | internal/console（29 端点）、internal/gateway（6 端点）、web/ 前端请求层 |

本文档是并行开发的**唯一真源**。每个 agent 只读本文档与自己负责的文件，不需要
知道别人在做什么。契约冻结意味着：实现期发现契约有问题要上报，不要自行改动。

---

## 0. 一处必须先纠正的既有认知

**后端已经在输出真实 HTTP 状态码，不是"固定 200"。** 这一点经实测确认：

```
ErrNotFound         → HTTP 404  {"code":1004,"data":null,"message":"..."}
ErrConflict         → HTTP 409  {"code":1005,"data":null,"message":"..."}
ErrActiveTaskExists → HTTP 409  {"code":1006,"data":null,"message":"..."}
ErrStillReferenced  → HTTP 409  {"code":1007,"data":null,"message":"..."}
WriteOK             → HTTP 200  {"code":0,"data":{...},"message":""}
```

`docs/contracts/http-console.md:116` 那句「HTTP 状态码固定为 `200`」是**过期文档**，
与代码不符。`sentinelMappings` 的 `httpStatus` 字段一直是真实状态码，`writeEnvelope`
也按传入值写出，从未强制 200。

前端之所以看不出区别，是因为 vben 的 `defaultResponseInterceptor` 只读 `body.code`、
**忽略** HTTP 状态码 —— 不是后端没给。

**因此本次迁移不需要"改造错误语义"**，后端的状态码语义已经正确。需要做的是：
让前端开始用它，并把过期文档改对。

## 1. 范围

**做**：42 个 handler 的传输层从 `net/http` 换成 gin；请求解析与校验改用 gin 的
binding + validator 标签；前端改用 HTTP 状态码判成败；改对过期文档。

**不做**：不改任何业务语义。不改对外 URL、HTTP 方法、响应体 JSON 结构与字段名。
不改状态码映射（已正确）。不改 `web/packages/` 下的 vben 上游代码。

## 2. 响应契约（不变，照抄现状）

`internal/apierr` 的三个类型与全部错误码**一个都不改**：

```go
type Envelope struct {
    Code    Code   `json:"code"`
    Data    any    `json:"data"`
    Message string `json:"message"`
}
```

| Code | 值 | HTTP 状态码 | 语义 |
|---|---|---|---|
| CodeOK | 0 | 200 | 成功 |
| CodeInternal | 1000 | 500 | 内部错误 |
| CodeBadRequest | 1001 | 400 | 参数格式或语义不合法 |
| CodeUnauthenticated | 1002 | 401 | 未认证 |
| CodeForbidden | 1003 | 403 | 无权限 |
| CodeNotFound | 1004 | 404 | 不存在 |
| CodeConflict | 1005 | 409 | 状态冲突（CAS 失败） |
| CodeActiveTaskExists | 1006 | 409 | 该主机已有进行中任务 |
| CodeStillReferenced | 1007 | 409 | 仍被引用，不能删除 |
| CodeReinstallDenied | 1008 | 409 | 拒绝重装 |
| CodeValidationFailed | 1009 | 422 | 校验失败 |

**状态码与 code 是两个消费者各取所需**，不是冗余：状态码给 curl、网关、监控、
gin 的日志中间件；`code` 给前端区分同状态码下的不同业务分支（409 下 1005/1006/1007/1008
是四个不同的 UI 提示）。删掉任何一边都会丢信息。

**安全边界（不可破）**：未识别的错误，完整信息只进 `log/slog`，响应体只给通用文案。
数据库错误可能带连接串与用户名，进了 HTTP 响应就等于把内部拓扑暴露给浏览器端。
这是 `apierr.go` 注释标为「本包最要紧的一条安全边界」的约定。

## 3. apierr 新增的 gin 适配层（唯一的新 API）

```go
// internal/apierr/gin.go（新建）
func OK(c *gin.Context, data any)      // 200 + Envelope{CodeOK, data, ""}
func Fail(c *gin.Context, err error)   // 按 err 决定状态码与 code，写 Envelope
func Abort(c *gin.Context, err error)  // 同 Fail，额外调 c.Abort()，供中间件用
```

- 三者内部复用现有的 `lookupSentinel` 与 `*Error` 判定逻辑，**不重写映射表**。
- `Fail` 额外识别 `validator.ValidationErrors` → 422 + `CodeValidationFailed`，
  message 必须是可展示中文，不能把 validator 的英文原文
  （`Key: 'X.Field' Error:Field validation for 'Field' failed on the 'required' tag`）
  直接吐给用户。需覆盖的 tag：`required`、`min`、`max`、`oneof`、`email`、`ip`、
  `mac`、`hostname`。
- **`WriteOK` / `WriteError`（标准库版）保留不删** —— gateway 不迁到信封，
  且迁移期间两套要能并存。

## 4. console 路由清单（29 端点，冻结）

路径前缀统一 `/console/v1`。`路径` 列已含前缀之后的部分。**对外 URL 一个字节不变**，
gin 的 `:param` 只是路由声明语法，不改 URL 本身。

| 方法 | 路径 | 权限码 | 审计动作 | handler |
|---|---|---|---|---|
| POST | `/auth/login` | — | — | handleLogin（公开，无需认证） |
| GET | `/user/info` | — | — | handleUserInfo |
| GET | `/auth/codes` | — | — | handleAuthCodes |
| POST | `/auth/logout` | — | — | handleLogout |
| GET | `/hosts` | — | — | handleListHosts |
| POST | `/hosts` | codeArtifactImport | host.create | handleCreateHost |
| POST | `/artifact-sets` | codeArtifactImport | artifact_set.create | handleCreateArtifactSet |
| GET | `/artifact-sets` | — | — | handleListArtifactSets |
| POST | `/artifact-sets/:id/verify` | codeArtifactImport | artifact_set.verify | handleVerifyArtifactSet |
| DELETE | `/artifact-sets/:id` | codeArtifactImport | artifact_set.delete | handleDeleteArtifactSet |
| POST | `/hooks` | codeHookWrite | hook.create | handleCreateHook |
| GET | `/hooks` | — | — | handleListHooks |
| PUT | `/hooks/:id` | codeHookWrite | hook.update | handleUpdateHook |
| POST | `/hooks/:id/validate` | codeHookWrite | hook.validate | handleValidateHook |
| GET | `/hooks/:id/versions` | — | — | handleHookVersions |
| POST | `/hooks/:id/approve` | — | hook.approve | handleApproveHook |
| POST | `/hooks/:id/reject` | — | hook.reject | handleRejectHook |
| GET | `/hooks/:id/audit` | — | — | handleHookAudit |
| GET | `/templates/:id/form-schema` | — | — | handleTemplateFormSchema |
| POST | `/templates/:id/preview` | — | — | handleTemplatePreview |
| POST | `/tasks` | codeTaskCreate | task.create | handleCreateTask |
| GET | `/tasks` | — | — | handleListTasks |
| GET | `/tasks/:id/events` | — | — | handleTaskEvents |
| GET | `/tasks/:id/stream` | — | — | handleTaskStream（SSE） |
| POST | `/tasks/:id/retry` | codeTaskRetry | task.retry | handleTaskRetry |
| POST | `/tasks/:id/cancel` | codeTaskCancel | task.cancel | handleTaskCancel |
| POST | `/disk-policy/dryrun` | — | — | handleDiskDryRun |

**全部业务端点都要求已登录**（viewer 只读也要登录，SPEC §12.5）。`/auth/login` 是
唯一的公开端点。权限码为 `—` 表示只要求登录、无特定权限码。

**权限码是按端点分配的，不是按前缀** —— 例如 `/hooks` 下 create/update/validate 要
`codeHookWrite`，而 approve/reject 不要权限码（只记审计）。因此不能简单套 gin 的
`RouterGroup` 层级中间件，注册函数必须支持逐端点指定。

## 5. console 骨架 API（六个资源域照此接线）

```go
// internal/console/server.go
// Server 仍实现 http.Handler —— 装配点 cmd/lober 与全部测试都按 http.Handler
// 用它，gin.Engine 本身实现 ServeHTTP，这一点不变。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)

// handle 注册一个业务端点：要求已登录；code 非空时检查权限码；
// action 非空时写审计。等价于迁移前的 handleFunc，参数含义逐一对应。
func (s *Server) handle(method, path, code, action string, h gin.HandlerFunc)

// handlePublic 注册不需要认证的端点（目前只有 POST /auth/login）。
func (s *Server) handlePublic(method, path string, h gin.HandlerFunc)

// internal/console/rbac.go
func (s *Server) requireAuth() gin.HandlerFunc
func (s *Server) requireCode(code string) gin.HandlerFunc

// internal/console/audit.go
func (s *Server) auditMiddleware(action string) gin.HandlerFunc
```

调用示例（六个资源域各自的 `routesXxx()` 里这样写）：

```go
func (s *Server) routesHost() {
    s.handle("GET", "/hosts", "", "", s.handleListHosts)
    s.handle("POST", "/hosts", codeArtifactImport, "host.create", s.handleCreateHost)
}
```

**gin 引擎配置（骨架 agent 负责，其余 agent 不碰）**：

- 用 `gin.New()` 而非 `gin.Default()`：后者自带 Logger 与 Recovery 中间件，
  会往 stdout 打自己格式的访问日志，与项目的 `log/slog` 两套并行输出。
  Recovery 要自己接一个走 `apierr.Abort` 的版本，保证 panic 也返回统一信封。
- `gin.SetMode(gin.ReleaseMode)`：debug 模式会在启动时往 stdout 打全部路由表，
  淹掉测试输出，生产也不该跑 debug。

## 6. gateway 契约（与 console 完全不同）

**gateway 不走 JSON 信封、不改状态码语义、对外零变化。**

它输出 iPXE 引导脚本与 kickstart/preseed/autoyast 应答文件，被 iPXE 固件与各家 OS
安装器**逐字符消费**。Content-Type 是 `text/plain`（应答文件按 OS 家族有不同 type，
见 `answer.go` 的 `answerFileContentType`）。JSON 错误响应对 iPXE 毫无意义 ——
它只认 `contract.NoTaskScript(serial)` 那种纯文本脚本。

| 方法 | 路径 | 输出 |
|---|---|---|
| GET | `/boot.ipxe` | iPXE 引导脚本 |
| GET | `/v1/boot` | iPXE 菜单或 no_task 脚本 |
| GET | `/v1/answer/:task/:token/:file` | 应答文件（按 OS 家族定 Content-Type） |
| POST | `/v1/signal/:task/:token/:phase` | 纯文本确认 |
| POST | `/v1/facts/:task/:token` | 纯文本确认 |
| GET | `/v1/facts/:task/:token` | facts 内容 |

gateway 只换三样：`http.ServeMux` → gin 引擎、handler 签名 → `func(c *gin.Context)`、
路径参数语法 `{x}` → `:x`。**输出的字节序列一个都不能变。**

**`c.String` 的格式化陷阱（本次迁移最容易静默出错的地方）**：`c.String(code, format, values...)`
会把第二个参数当格式串。iPXE 脚本与应答文件里很可能含 `%`（如 kickstart 的
`%packages`、`%post` 段）。必须用 `c.Data(code, contentType, []byte(body))`，
或 `c.String(code, "%s", body)`。直接 `c.String(code, body)` 会让 `%packages`
被当成格式动词而输出错乱 —— 且大概率不报错，只是产出一份损坏的应答文件，
症状是装机到一半失败。

`syslog.go` 不是 HTTP 端点（是 syslog 监听器），不要动它的监听逻辑。

## 7. 校验重构规则

换 gin binding 标签是本次的主要收益：把手写的「取字段 → 判空 → 拼错误」换成结构体标签。

**规则：校验条件一条都不能少。** 先逐个读现有的手写校验、列成清单，再用标签表达。
标签表达不了的（跨字段依赖、需要查库、需要调 provider）留在 handler 里用 Go 代码做，
不要为了"全用标签"而弱化校验。

各文件的手写校验密度（供估工，非硬指标）：

| 文件 | handler 数 | 手写校验点 |
|---|---|---|
| host.go | 2 | 6 |
| dryrun.go | 1 | 6 |
| artifact.go | 4 | 3 |
| task.go | 5 | 3 |
| hook.go | 8 | 2 |
| template.go | 4 | 2 |
| stream.go | 1 | 1 |
| auth.go | 4 | 0 |

## 8. 前端契约变化（唯一的对外行为变化）

后端已经在输出真实状态码，前端要开始用它。

**改**：`web/apps/web-ele/src/api/request.ts` —— 弃用 vben 上游的
`defaultResponseInterceptor({codeField:'code', dataField:'data', successCode:0})`，
自写一个等价拦截器：HTTP 2xx 判成功并解包 `data`；非 2xx 按状态码分流，
同时读 `body.code` 做细分（409 下 1005/1006/1007/1008 对应四种不同提示）。

`authenticateResponseInterceptor`（token 过期）现在能直接用 HTTP 401 判定，
比翻 `body.code` 更自然。`errorMessageResponseInterceptor` 沿用 `message` 字段。

**不改**：`web/packages/` 下任何 vben 上游代码。改了升级会冲突。

**代价（已确认接受）**：此举让项目从此偏离 vben 的响应约定，升级上游时要重新对齐。
自写的拦截器文件头必须注释说明这一点与原因，免得后人以为是临时代码而删掉。

## 9. 文档要改对的地方

`docs/contracts/http-console.md:116` 的「HTTP 状态码固定为 `200`（框架侧按 body 里的
`code` 字段判定成功与否，不看 HTTP 状态码）」与代码不符（见 §0），改成：
状态码按 §2 的表真实返回；`code` 字段用于区分同状态码下的不同业务分支。

同文件 `:122-126` 有一段偏差声明说「`internal/apierr` 包尚未创建」—— 该包早已存在，
这段声明过期，删掉或改成指向实际实现。

## 10. 验证标准

每个 agent 的交付判定：`go build ./...` 通过、`go vet` 自己的包通过、
`gofmt -l` 对改过的文件无输出、自己负责的测试 PASS。

整体验收（主会话负责）：

- `make check` 退出码 0（fmt / vet / test / race 四步）
- gateway 的逐字节输出断言全部原样通过（`answer_test.go`、`boot_test.go`、
  `siglink_test.go`）—— 这是那一层唯一的安全网，任何一条失败都说明输出变了，
  必须查到底，不能改断言迁就实现
- 29 个 console 端点的对外 URL、方法、响应体 JSON 结构与迁移前逐一一致
- 手工核对：`curl -i` 打一个 404 与一个 409 端点，确认状态码与 body.code 同时正确


