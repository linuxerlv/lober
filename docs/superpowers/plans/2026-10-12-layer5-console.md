# 重写第 5 层：控制台 实现计划

**2026-10-14 开始执行：** 用户已启动第 5 层目标，开发与验证使用 WSL Ubuntu。沿用已确认的设计与本计划，在主检出 `feat/console` 顺序执行；先合入 `dev` 的第 3 层最终版本与 `feat/iso` 的第 4 层实现。任务完成以实际测试、提交与执行记录为准，未执行的复选框保持未完成。

**接口更新（优先于下文历史步骤）：** 第 4 层已经实现，任务 7/14 不再注入 ISO 占位实现：装配点注入真实 `iso.Sources`，控制台只声明所需接口；`Register` 仅登记 pending，获取与导入由装配点调度，启动恢复用 `Recover` 的返回清单。审计删除前持久化 `DeletionSnapshot`，删除成功后调用 `CleanupDeleted` 并持久化重试。上传分片仍不声明审计动作。第 7 层的任务控制仍保留明确拒绝的占位实现，不在本层猜重试安全规则。

## 审查重点

- 禁用用户、变更角色与过期会话必须在下一次请求立即生效；口令散列不能进入响应或日志（任务 5）。
- CSV 重复主机、非法 UTF-8、空列与保留属性必须在 dry-run 给逐行原因，失败不能留下部分写入（任务 6）。
- ISO 登记后重启、失败重试、删除后清理失败必须保留可恢复状态，本地原文件不能删除（任务 7、14）。
- 客户端提交过期版本、不可改参数或混入一台不合规则的主机时，全批拒绝且不留下创建审计（任务 11）。
- SSE 断线、通知丢失与并发发布不得漏事件、重复推进或泄漏订阅；原始审计 CSV 中的公式字符需按文本导出（任务 12、13）。

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 给人用的全部 HTTP 接口，以及第一次出现的装配点 `cmd/lober`。

**架构：** 两个新包。`internal/console` 只做编排——从仓储取数据，交给第 1 层判定，把结果写回仓储；路由表是唯一事实源，OpenAPI 文档、权限矩阵与审计声明都由它生成。`cmd/lober` 是唯一知道每个接口由谁实现的包，用 wire 组合。控制台对第 4 层（ISO 来源）与第 7 层（任务控制）的全部需要，各压缩成一个自己声明的窄接口，未就绪时由装配点注入占位实现。

**技术栈：** Go 1.27.1，标准库；`gin-gonic/gin` v1.12.0（第 5 层引入的第一个 Web 框架）；`gopkg.in/yaml.v3`（只给 `cmd/lober` 读配置）；`github.com/goforj/wire/cmd/wire` v1.2.0 以 `tool` 指令钉住。HTTP 测试连测试环境的 PostgreSQL（经 `internal/store/pgstore/pgtest`，总 spec §6.4）。

**规格：** `docs/superpowers/specs/2026-10-11-layer5-console-design.md`

**依据：**
- 总 spec `docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`：§2.3–§2.7（主机、hook、模板、参数、任务）、§3（校验分四个时点）、§4.5（本层定义）、§5.2–§5.3（写模板与建任务）、§6.1（依赖方向）、§6.2 第 1、2、6、8、9、10、11 条、§6.3（切面与 wire）、§6.4（测试环境）、§7.2 第 5 条（出口判据）、§7.4（装配点在第 5 层首次出现）
- 第 3 层计划 `plans/2026-10-09-rewrite-layer3-render.md`「留给后续层的约定」第 1 条：预览调 `render.Render`、没指定主机或 ISO 时用示例值并标明、装配点调 `EnsureBuiltin*`、按 `builtin` 当前带的清单过滤向导里的旧身份
- 第 4 层设计 `specs/2026-10-11-layer4-iso-design.md` §4.5（`FreeSpace`）、§12（进度只读）
- 第 1 层清单 `reviews/2026-10-02-layer1-open-issues.md` L-8、S3-10（归第 5 层）

## 全局约束

每个任务的要求都隐含包含本节。

- **中文说明只在 `internal/core/model/codes.go`。** console 不写业务规则，也不在别处写中文说明；未识别的错误给通用文案，细节只进 `log/slog`。
- **层 1 与第 3 层不读时钟、不读库、不做文件与网络 IO。** console 读时钟，但一律经 `Deps.Clock` 注入，测试传固定值。
- **仓储不读时钟。** `Audit.At`、`User.CreatedAt` 由 console 用 `Deps.Clock` 算好传入，必须大于 0。
- **装结果的切片与 map 一律不为 nil**：第 5 层编成 JSON 是 `[]` 与 `{}`，nil 会编成 `null`，与 OpenAPI 对不上（`internal/store/doc.go`）。
- **新加的包必须同一次改动给守卫加规则。** 守卫全模块列包，归不到任何层的包一律报违规；常量与引用它的用例必须一起加。
- **只加新表，不动第 2 层已有的索引与约束。** 总 spec §4.2：改旧定义要先写一步删掉旧定义的升级动作。
- **HTTP 测试连测试环境的 PostgreSQL，找不到连接串就失败，不跳过**（总 spec §6.4）。
- **make check 六步全绿**：`fmt`、`vet`、`test`、`race`、`cross`，以及本层新增的 `gen`（重新生成 wire 并比对）。
- **提交信息用中文，Conventional Commits**，每个任务的最后一步都是提交。

---

## 文件结构

### `internal/console`

| 文件 | 职责 |
|---|---|
| `doc.go` | 包说明：只做编排，不写业务规则 |
| `envelope.go` | 响应信封 `{code, data, message}` 与 `ok`/`fail` |
| `errors.go` | 错误到状态码的映射、`problems`/`bind`/`get`/`write` 辅助、问题的 JSON 形状 |
| `server.go` | `Deps`、`New`、`server`、中间件链 |
| `routes.go` | `route` 类型、`allRoutes`、注册到 gin |
| `perm.go` | 权限码、角色表、权限中间件 |
| `openapi.go` | 由路由表生成 OpenAPI 文档 |
| `auth.go` | 登录、登出、用户信息、内存会话、口令散列 |
| `users.go` | 用户管理端点 |
| `hosts.go` | 主机端点与 CSV 解析 |
| `isos.go` | ISO 端点 |
| `isosource.go` | `ISOSource` 接口（控制台声明的第 4 层接缝） |
| `taskcontrol.go` | `TaskControl` 接口（控制台声明的第 7 层接缝） |
| `hooks.go` | hook 端点 |
| `templates.go` | 模板端点 |
| `wizard.go` | 向导五步 |
| `tasks.go` | 任务端点 |
| `sse.go` | SSE 与通知中心 |
| `audit.go` | 审计查询与 CSV 导出 |
| `preview.go` | 模板预览与任务预览，都调 `render.Render` |
| `slots.go` | 编辑器用的插槽清单，调 `scan.Scan` |

### `internal/store`、`internal/store/pgstore`

| 文件 | 职责 |
|---|---|
| `internal/store/users.go` | `Users` 接口、`User`、`Role`、`UserQuery`、`CheckUser` |
| `internal/store/users_test.go` | `CheckUser`、`Role.Valid` 的纯函数测试 |
| `internal/store/pgstore/users.go` | `Users` 的 PostgreSQL 实现 |
| `internal/store/pgstore/schema.go` | 加 `users` 表 |
| `internal/store/pgstore/contract_helpers_test.go` | `contents` 加 `Users`，被拒的写入才查得出来 |
| `internal/store/pgstore/contract_users_test.go` | 用户的契约用例 |

### `internal/core`（第 1 层补词表）

| 文件 | 职责 |
|---|---|
| `internal/core/catalog/vocab.go` | `AllFormats`、`AllSeries`、`AllTimings`、`AllInterpreters`、`AllSystemParts`、`AllSystemValues`、`AllArchs`、`AllFirmwares` |
| `internal/core/catalog/vocab_test.go` | 每张表与既有登记表逐项一致、返回副本 |
| `internal/core/model/subject.go` | `KindHost`…`KindBody` 八个常量与 `Kinds()` |
| `internal/core/model/subject_test.go` | 八个取值与 `Subject.Kind` 注释里列的一致 |
| `internal/core/model/authoring.go` | `HookKey.String()`、`TemplateKey.String()` |
| `internal/core/model/codes_align_test.go` | S3-10：架构清单改用 `catalog.AllArchs()` 比对全集 |

### `cmd/lober`

| 文件 | 职责 |
|---|---|
| `main.go` | 入口、配置、启动顺序、信号与优雅退出 |
| `config.go`、`config_test.go` | YAML 配置与解析 |
| `wire.go`、`wire_gen.go` | 注入函数与生成结果 |
| `builtin.go` | 装载内置内容，撞 `ErrBuiltinMismatch` 拒绝启动 |
| `isosource.go` | `ISOSource` 的占位实现（第 4 层替换） |
| `taskcontrol.go` | `TaskControl` 的占位实现（第 7 层替换） |
| `logging.go` | 存储层的调用日志装饰器（手写，不用 gowrap） |

### 修改的既有文件

| 文件 | 改动 |
|---|---|
| `internal/archtest/deps_test.go` | 加「控制台」与「装配点」两条规则 |
| `internal/archtest/guard_test.go` | 两条新规则的正反例 |
| `Makefile` | 加 `gen` 一步 |
| `go.mod`、`go.sum` | 加 gin、yaml.v3；`tool` 指令钉 wire |

---

## Task 1：骨架——信封、错误映射与守卫规则

**Files:**
- Create: `internal/console/doc.go`
- Create: `internal/console/envelope.go`
- Create: `internal/console/errors.go`
- Test: `internal/console/envelope_test.go`、`internal/console/errors_test.go`
- Modify: `internal/archtest/deps_test.go`
- Modify: `internal/archtest/guard_test.go`

**Interfaces:**
- Consumes: 无（第一个任务）
- Produces:
  - `console.ok(c *gin.Context, data any)`、`console.fail(c *gin.Context, status int, data any, message string)`
  - `console.respondError(c *gin.Context, err error)`
  - `console.judge(c *gin.Context, ps []model.Problem) bool`
  - `console.bind(c *gin.Context, v any) bool`
  - `console.get[T any](c *gin.Context, load func() (T, error)) (T, bool)`
  - `console.write[T any](c *gin.Context, do func() (T, error)) (T, bool)`
  - `console.statusOfProblems(ps []model.Problem) int`（纯函数，供测试）
  - 守卫的 `layerConsole`、`layerAssembly`、`ruleConsole`、`ruleAssembly`

- [x] **Step 1: 加依赖，确认能编**

Run:
```bash
go get github.com/gin-gonic/gin@v1.12.0
go mod tidy
```
Expected: `go.mod` 的 require 里出现 gin；`go build ./...` 通过。

- [x] **Step 2: 写失败的测试——信封**

Create `internal/console/envelope_test.go`:

```go
package console_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/console"
)

// testContext 给一个能写响应的 gin 上下文：没有路由，只测信封本身。
func testContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/console/v1/x", nil)
	return c, w
}

func Test成功响应包上code与data(t *testing.T) {
	c, w := testContext(t)
	console.Ok(c, map[string]string{"sn": "SN-1"})
	if w.Code != 200 {
		t.Fatalf("状态码是 %d，应为 200", w.Code)
	}
	var got struct {
		Code    int            `json:"code"`
		Data    map[string]any `json:"data"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON：%v\n%s", err, w.Body.String())
	}
	if got.Code != 0 {
		t.Errorf("code 是 %d，应为 0", got.Code)
	}
	if got.Message != "" {
		t.Errorf("message 是 %q，成功时应为空", got.Message)
	}
	if got.Data["sn"] != "SN-1" {
		t.Errorf("data 是 %v，应带上传入的数据", got.Data)
	}
}

func Test没有数据时成功响应的data是null(t *testing.T) {
	c, w := testContext(t)
	console.Ok(c, nil)
	var got struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON：%v", err)
	}
	if string(got.Data) != "null" {
		t.Errorf("data 是 %s，没有数据时应是 null", got.Data)
	}
}
```

Run: `go test ./internal/console/ -run 'Test成功|Test没有数据' -v`
Expected: FAIL，`undefined: console.Ok`。

- [x] **Step 3: 写失败的测试——错误映射**

Create `internal/console/errors_test.go`：

```go
package console_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 原因类别到状态码：读懂了但内容不合规矩的一律 422，逐条原因在 data.problems 里；
// 组合不兼容与状态不允许是 409。
func Test状态码按原因类别映射(t *testing.T) {
	cases := []struct {
		name string
		ps   []model.Problem
		want int
	}{
		{"invalid 是 422", []model.Problem{model.NewProblem("sn_invalid", model.Subject{Kind: "host"}, "为空")}, 422},
		{"incomplete 是 422", []model.Problem{model.NewProblem("hook_no_impl", model.Subject{Kind: "hook"})}, 422},
		{"incompatible 是 409", []model.Problem{model.NewProblem("series_not_ticked",
			model.Subject{Kind: "template"}, "rocky")}, 409},
		{"state 是 409", []model.Problem{model.NewProblem("template_not_approved",
			model.Subject{Kind: "template"}, "草稿")}, 409},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := console.StatusOfProblems(c.ps); got != c.want {
				t.Errorf("状态码是 %d，应为 %d", got, c.want)
			}
		})
	}
}

// 类别未登记时代码表里没有这个代码，按 422：宁可让前端拿到问题清单，也不要一个没有说明的 500。
func Test类别未登记按422(t *testing.T) {
	ps := []model.Problem{{Code: "not_registered", Message: "说明"}}
	if got := console.StatusOfProblems(ps); got != 422 {
		t.Errorf("状态码是 %d，应为 422", got)
	}
}

// 仓储的哨兵值各一例。带逐条原因的两个排在前面：它们要 422 与 problems。
func Test仓储错误映射到状态码(t *testing.T) {
	probs := []model.Problem{model.NewProblem("sn_invalid", model.Subject{Kind: "host"}, "为空")}
	batch := &store.BatchError{Result: admission.BatchResult{
		Common: []model.Problem{model.NewProblem("iso_not_ready", model.Subject{Kind: "iso"}, "获取中")},
		ByHost: map[string][]model.Problem{"SN-1": probs},
	}}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"找不到是 404", store.ErrNotFound, 404},
		{"Rev 对不上是 409", store.ErrConflict, 409},
		{"唯一键重复是 409", &store.DuplicateError{Field: "sn", Existing: "SN-1"}, 409},
		{"不能修改是 409", store.ErrImmutable, 409},
		{"仍被引用是 409", store.ErrReferenced, 409},
		{"入参不合法是 400", store.ErrInvalid, 400},
		{"要审计却没给是 500", store.ErrAuditRequired, 500},
		{"带问题的入参不合法是 422", &store.ProblemsError{Kind: store.ErrInvalid, Problems: probs}, 422},
		{"重跑判定没通过是 422", &store.ProblemsError{Kind: store.ErrRejected, Problems: probs}, 422},
		{"批量被拒是 422", batch, 422},
		{"不认识的错误是 500", errors.New("boom"), 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, w := testContext(t)
			console.RespondError(ctx, c.err)
			if w.Code != c.want {
				t.Errorf("状态码是 %d，应为 %d\n%s", w.Code, c.want, w.Body.String())
			}
		})
	}
}

// 失败的响应体：data 装结构化详情，message 装可展示的中文汇总。
func Test失败响应的data装结构化详情(t *testing.T) {
	probs := []model.Problem{model.NewProblem("sn_invalid", model.Subject{Kind: "host", ID: "SN-1", Path: "arch"},
		"含不可打印字符")}
	ctx, w := testContext(t)
	console.RespondError(ctx, &store.ProblemsError{Kind: store.ErrInvalid, Problems: probs})
	var got struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Problems []struct {
				Code    string `json:"code"`
				Subject struct {
					Kind, ID, Path string
				} `json:"subject"`
				Message, Next string
			} `json:"problems"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON：%v\n%s", err, w.Body.String())
	}
	if got.Code != 422 {
		t.Errorf("code 是 %d，应为 422", got.Code)
	}
	if got.Message != probs[0].Message {
		t.Errorf("message 是 %q，应为第一条问题的说明 %q", got.Message, probs[0].Message)
	}
	if len(got.Data.Problems) != 1 {
		t.Fatalf("problems 有 %d 条，应为 1 条", len(got.Data.Problems))
	}
	p := got.Data.Problems[0]
	if p.Code != "sn_invalid" || p.Subject.Kind != "host" || p.Subject.ID != "SN-1" || p.Subject.Path != "arch" {
		t.Errorf("问题与位置是 %+v，应原样来自第 1 层", p)
	}
	if p.Message == "" || p.Next == "" {
		t.Errorf("说明或下一步是空的：%+v", p)
	}
}

// 批量建任务被拒：data 与 admission.BatchResult 同形，逐台列出原因（C30）。
func Test批量被拒的data按主机分开(t *testing.T) {
	common := []model.Problem{model.NewProblem("iso_not_ready", model.Subject{Kind: "iso"}, "获取中")}
	byHost := map[string][]model.Problem{
		"SN-1": {model.NewProblem("host_has_open_task", model.Subject{Kind: "host", ID: "SN-1"})},
	}
	ctx, w := testContext(t)
	console.RespondError(ctx, &store.BatchError{Result: admission.BatchResult{Common: common, ByHost: byHost}})
	var got struct {
		Data struct {
			Common []struct {
				Code string `json:"code"`
			} `json:"common"`
			ByHost map[string][]struct {
				Code string `json:"code"`
			} `json:"byHost"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON：%v", err)
	}
	if len(got.Data.Common) != 1 || got.Data.Common[0].Code != "iso_not_ready" {
		t.Errorf("common 是 %+v，应带 ISO 的问题", got.Data.Common)
	}
	if len(got.Data.ByHost["SN-1"]) != 1 || got.Data.ByHost["SN-1"][0].Code != "host_has_open_task" {
		t.Errorf("byHost 是 %+v，应按 SN 放各台的问题", got.Data.ByHost)
	}
}

// 没有逐条原因的失败：data 是 null，message 是兜底文案。
func Test没有原因的失败data是null(t *testing.T) {
	ctx, w := testContext(t)
	console.RespondError(ctx, store.ErrNotFound)
	var got struct {
		Data    json.RawMessage `json:"data"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应体不是 JSON：%v", err)
	}
	if string(got.Data) != "null" {
		t.Errorf("data 是 %s，应为 null", got.Data)
	}
	if got.Message == "" {
		t.Error("message 不应为空")
	}
}

// 系与格式的字面量只为构造反例，用过后就不依赖了。
var _ = catalog.FileMenu
```

Run: `go test ./internal/console/ -v`
Expected: FAIL，`undefined: console.StatusOfProblems`、`undefined: console.RespondError`、`undefined: console.Ok`。

- [x] **Step 4: 写失败的测试——守卫两条新规则**

Modify `internal/archtest/guard_test.go`，在 `Test存储层的依赖规则` 之后加：

```go
// 第 5 层的两条规则（总 spec §6.1：实现模块之间互不引用；装配点知道每个接口由谁实现）。
// 控制台是白名单：标准库、gin、层 1、存储契约、渲染与内置内容，第三方只有 gin 一家。
func Test控制台与装配点的依赖规则(t *testing.T) {
	cases := []struct {
		name string
		pkg  string
		src  string
		want []finding
	}{
		{"console 用白名单里的包不报", pkgConsole,
			importing("context", "net/http", "net/http/httptest", gin,
				pkgModel, pkgCatalog, pkgStore, pkgRender, pkgBuiltin), nil},
		{"console import 装配点要报", pkgConsole, importing(pkgAssembly), []finding{{ruleConsole, pkgAssembly}}},
		{"console import 别的第三方模块要报", pkgConsole,
			importing("github.com/lib/pq", "gorm.io/gorm", "github.com/lober/lober/internal/iso"),
			[]finding{{ruleConsole, "github.com/lib/pq"}, {ruleConsole, "gorm.io/gorm"},
				{ruleConsole, "github.com/lober/lober/internal/iso"}}},
		{"console 下面新加的包按同一条查", pkgConsole + "/seed",
			importing(pkgPgstore, "github.com/lib/pq"),
			[]finding{{ruleConsole, pkgPgstore}, {ruleConsole, "github.com/lib/pq"}}},
		{"cgo 的 C 不算标准库", pkgConsole, importing("C"), []finding{{ruleConsole, "C"}}},

		{"装配点 import 本模块的任何包都不报", pkgAssembly,
			importing("context", "os/signal", pkgStore, pkgPgstore, pkgConsole, pkgRender, pkgBuiltin, pkgModel), nil},
		{"装配点 import gin 与 yaml 不报", pkgAssembly,
			importing(gin, yamlV3), nil},
		{"装配点 import 别的第三方模块要报", pkgAssembly,
			importing("github.com/lib/pq", "gorm.io/gorm"),
			[]finding{{ruleAssembly, "github.com/lib/pq"}, {ruleAssembly, "gorm.io/gorm"}}},
		{"别的包 import 装配点要报", pkgStore, importing(pkgAssembly), []finding{{ruleContract, pkgAssembly}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertFindings(t, checkConsoleOrAssemblySource(t, c.pkg, c.src), c.pkg, c.want)
		})
	}
}
```

同时把 `checkStoreSource` 旁加一个：

```go
// checkConsoleOrAssemblySource 按包归的层选规则：控制台与装配点各一条。
func checkConsoleOrAssemblySource(t *testing.T, pkg, src string) []violation {
	t.Helper()
	fset, f := parseSource(t, src)
	if strings.HasPrefix(pkg, pkgConsole) {
		return checkConsoleFile(fset, pkg, f)
	}
	return checkAssemblyFile(fset, pkg, f)
}
```

并在 `Test总检查接上了每一项` 之后加一条，钉住 `checkModule` 接上了这两层：

```go
// 真实的树是干净的，总检查漏接了控制台或装配点，Test全模块依赖守卫 照样通过。
func Test总检查接上了控制台与装配点(t *testing.T) {
	fsys := fstest.MapFS{
		"internal/console/x.go": {Data: []byte(importing(pkgAssembly))},
		"cmd/lober/y.go":        {Data: []byte(importing("github.com/lib/pq"))},
	}
	pkgs := []goPackage{
		{ImportPath: pkgConsole, GoFiles: []string{"x.go"}},
		{ImportPath: pkgAssembly, GoFiles: []string{"y.go"}},
	}
	got, checked := checkModule(fsys, []string{"internal/console", "cmd/lober"}, pkgs)
	assertFindings(t, got, "", []finding{
		{ruleConsole, pkgAssembly},
		{ruleAssembly, "github.com/lib/pq"},
	})
	if checked[layerConsole] != 1 || checked[layerAssembly] != 1 {
		t.Errorf("各层查到的包数是 %v，控制台与装配点应各 1 个", checked)
	}
}
```

Run: `go test ./internal/archtest/ -run 'Test控制台与装配点|Test总检查接上了控制台' -v`
Expected: FAIL，`undefined: pkgConsole`、`undefined: ruleConsole`、`undefined: checkConsoleFile`。

- [x] **Step 5: 实现信封与错误映射**

Create `internal/console/doc.go`：

```go
// Package console 是控制台（总 spec §4.5）：给人用的全部 HTTP 接口。它只做编排——从仓储取数据，
// 交给第 1 层判定，把结果写回仓储，自己不写业务规则。说明文字一律来自第 1 层的代码表，本包只做
// 两件映射：原因类别到 HTTP 状态码，仓储的哨兵错误到状态码与兜底文案。
package console
```

Create `internal/console/envelope.go`：

```go
package console

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// codeOK 是 vben-admin 默认响应拦截器约定的成功码：它的 codeField 是 'code'、successCode 是 0
// （第 5 层设计 §4.1）。照它做，第 6 层不必改前端框架的拦截器。失败时 code 就是 HTTP 状态码，
// 不另设一套 1000 段的业务码：前端只判 0 与非 0，展示的是 message，多一张表只多一处要同步。
const codeOK = 0

// envelope 是全部非 SSE 响应的外包装。失败时 data 装结构化详情（problems、批量建任务的
// common 与 byHost），message 装可展示的中文汇总；没有结构化详情时 data 是 nil，编成 null。
type envelope struct {
	Code    int  `json:"code"`
	Data    any `json:"data"`
	Message string `json:"message"`
}

// Ok 写出成功响应。
func Ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, envelope{Code: codeOK, Data: data})
}

// Fail 写出失败响应并中止后续处理函数：响应已经交付出去了，不该再有别的写入。
func Fail(c *gin.Context, status int, data any, message string) {
	c.JSON(status, envelope{Code: status, Data: data, Message: message})
	c.Abort()
}
```

Create `internal/console/errors.go`：

```go
package console

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 状态码与兜底文案。业务码就是状态码本身，见 envelope.go 的说明。
const (
	statusBadRequest    = http.StatusBadRequest      // 400：请求读不懂或入参违反前提
	statusUnauthorized  = http.StatusUnauthorized    // 401：未登录或会话失效
	statusForbidden     = http.StatusForbidden       // 403：权限码不够
	statusNotFound      = http.StatusNotFound        // 404：找不到
	statusConflict      = http.StatusConflict        // 409：状态、唯一键、引用
	statusUnprocessable = http.StatusUnprocessableEntity // 422：内容不合规矩，原因在 problems 里
	statusInternal      = http.StatusInternalServerError  // 500
)

// categoryStatus 是原因类别到状态码的映射（第 5 层设计 §4.2）。读懂了但内容不合规矩的一律
// 422，逐条原因在 data.problems 里；组合不兼容与状态不允许是 409。
var categoryStatus = map[model.Category]int{
	model.CategoryInvalid:      statusUnprocessable,
	model.CategoryIncomplete:   statusUnprocessable,
	model.CategoryIncompatible: statusConflict,
	model.CategoryState:        statusConflict,
}

// problemList 是带逐条原因的失败详情。说明与下一步原样来自第 1 层的代码表。
type problemList struct {
	Problems []model.Problem `json:"problems"`
}

// batchDetail 是批量建任务被拒时的详情，与 admission.BatchResult 同形（C30：一台不建，
// 逐台列出原因）。
type batchDetail struct {
	Common []model.Problem            `json:"common"`
	ByHost map[string][]model.Problem `json:"byHost"`
}

// RespondError 把一个 error 映射成状态码与响应体。判别的顺序即优先级：带逐条原因的两个排在
// 前面，它们要 422 与 problems；其余按仓储的哨兵值分；都不认识的是未预期的故障，500。
func RespondError(c *gin.Context, err error) {
	var batch *store.BatchError
	if errors.As(err, &batch) {
		detail := batchDetail{Common: batch.Result.Common, ByHost: batch.Result.ByHost}
		Fail(c, statusUnprocessable, detail, firstMessage(batch.Result.Common, batch.Result.ByHost))
		return
	}
	var probs *store.ProblemsError
	if errors.As(err, &probs) {
		Fail(c, statusUnprocessable, problemList{Problems: probs.Problems}, firstMessage(probs.Problems, nil))
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, statusNotFound, nil, "找不到这条记录")
	case errors.Is(err, store.ErrConflict):
		Fail(c, statusConflict, nil, "记录已经被改过，重读之后再试")
	case errors.Is(err, store.ErrDuplicate):
		Fail(c, statusConflict, nil, "已经有同键的记录了")
	case errors.Is(err, store.ErrImmutable):
		Fail(c, statusConflict, nil, "这个版本已经定稿，不能改")
	case errors.Is(err, store.ErrReferenced):
		Fail(c, statusConflict, nil, "还有别的记录引用着它，不能删")
	case errors.Is(err, store.ErrInvalid):
		Fail(c, statusBadRequest, nil, "请求的内容不合法")
	case errors.Is(err, store.ErrAuditRequired):
		// 路由声明了审计动作，处理函数却没把审计交给仓储：这是编程错误，不是用户的错。
		Fail(c, statusInternal, nil, "服务端内部错误")
	default:
		Fail(c, statusInternal, nil, "服务端内部错误")
	}
}

// Judge 把第 1 层判定给出的问题写成响应。没有问题时什么都不做并返回 true，调用方接着写库。
func Judge(c *gin.Context, ps []model.Problem) bool {
	if len(ps) == 0 {
		return true
	}
	Fail(c, StatusOfProblems(ps), problemList{Problems: ps}, ps[0].Message)
	return false
}

// StatusOfProblems 给出这组问题该用的状态码：按第一条的类别。类别未登记时代码表里没有这个
// 代码，按 422——宁可让前端拿到问题清单，也不要一个没有说明的 500。
func StatusOfProblems(ps []model.Problem) int {
	if len(ps) == 0 {
		return statusUnprocessable
	}
	if s, ok := categoryStatus[ps[0].Code.Category()]; ok {
		return s
	}
	return statusUnprocessable
}

// Bind 解析请求体，失败就写出 400 并返回 false。用法：
//
//	var req hostRequest
//	if !Bind(c, &req) { return }
func Bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		Fail(c, statusBadRequest, nil, "请求的内容不合法："+err.Error())
		return false
	}
	return true
}

// Get 取一个对象，取不到就把仓储的错误映射成响应。ok 为 false 时响应已经写出，调用方
// 直接 return。
func Get[T any](c *gin.Context, load func() (T, error)) (T, bool) {
	v, err := load()
	if err != nil {
		RespondError(c, err)
		return v, false
	}
	return v, true
}

// Write 做一个写操作，失败就按 RespondError 映射。ok 为 false 时响应已经写出；成功时
// 调用方自己 Ok(c, v)，因为每个端点要返回的形状不一样。
func Write[T any](c *gin.Context, do func() (T, error)) (T, bool) {
	v, err := do()
	if err != nil {
		RespondError(c, err)
		return v, false
	}
	return v, true
}

// firstMessage 取可展示的中文汇总：第一条问题的说明。批量被拒时先 Common 再各台，按 SN 的
// 顺序——ByHost 是 map，排过序同一次结果才写得一样。
func firstMessage(common []model.Problem, byHost map[string][]model.Problem) string {
	if len(common) > 0 {
		return common[0].Message
	}
	sns := make([]string, 0, len(byHost))
	for sn := range byHost {
		sns = append(sns, sn)
	}
	slices.Sort(sns)
	for _, sn := range sns {
		if ps := byHost[sn]; len(ps) > 0 {
			return ps[0].Message
		}
	}
	return "这批请求没有通过"
}
```

`errors.go` 的 import 里补 `"slices"`。

- [x] **Step 6: 加守卫的两条规则**

Modify `internal/archtest/deps_test.go`：

在 `pkgBuiltin` 之后加：

```go
// pkgConsole 是第 5 层的控制台；pkgAssembly 是装配点 cmd/lober，唯一可以依赖全部模块的包
// （总 spec §6.1）。gin 与 yaml.v3 是第 5 层引入的两个第三方模块，只在装配点与控制台用。
const (
	pkgConsole  = modulePath + "/internal/console"
	pkgAssembly = modulePath + "/cmd/lober"
)

const (
	gin   = "github.com/gin-gonic/gin"
	yamlV3 = "gopkg.in/yaml.v3"
)
```

在 `layerGuard layer = "守卫"` 之前加两个取值：

```go
	layerConsole  layer = "控制台"
	layerAssembly layer = "装配点"
```

`layers` 改成：

```go
var layers = []layer{layerCore, layerStore, layerRender, layerBuiltin, layerConsole, layerAssembly, layerGuard}
```

`minChecked` 改成：

```go
var minChecked = map[layer]int{layerCore: 7, layerStore: 3, layerRender: 1, layerBuiltin: 1,
	layerConsole: 1, layerAssembly: 1, layerGuard: 1}
```

在 `ruleLayer` 之前加：

```go
	// 第 5 层的两条规则，照第 5 层设计「包与文件」表。控制台是白名单：它只做编排，不 import
	// 装配点，也不依赖 gin 以外的第三方模块——gin 是它引入的第一个 Web 框架，放行别家就等于
	// 把任意依赖放进编排层。装配点相反，它知道每个接口由谁实现，本模块的包都能 import。
	ruleConsole rule = "console 只依赖标准库、github.com/gin-gonic/gin、internal/core/...、internal/store、" +
		"internal/render 与 internal/builtin：它只做编排，不 import 装配点，也不依赖白名单外的第三方模块"
	ruleAssembly rule = "cmd/lober 是装配点，可以依赖本模块的任何包以及 gin 与 yaml.v3（总 spec §6.1）；" +
		"白名单外的第三方模块不许用"
```

`layerOf` 的 switch 加两个 case：

```go
	case under(pkg, pkgConsole):
		return layerConsole
	case pkg == pkgAssembly:
		return layerAssembly
```

`checkModule` 的 switch 加两个 case：

```go
		case layerConsole:
			found = append(found, checkSources(fsys, p, checkConsoleFile)...)
		case layerAssembly:
			found = append(found, checkSources(fsys, p, checkAssemblyFile)...)
```

在 `storeRule` 之后加：

```go
// checkConsoleFile 查控制台一个非测试源文件的 import。
func checkConsoleFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	for _, imp := range f.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value) // 理由同 checkFile
		if r := consoleRule(pkg, importPath); r != "" {
			found = append(found, violation{rule: r, pkg: pkg, subject: importPath, pos: position(fset, imp.Pos())})
		}
	}
	return found
}

// consoleRule 返回控制台的包 pkg import imp 违反的规则，不违反时返回空串。白名单照第 5 层设计
// 「包与文件」表：标准库、gin、层 1、存储契约、渲染与内置内容。按整段比对，internal/coreutils
// 不算层 1，internal/rendery 不算渲染。
func consoleRule(pkg, imp string) rule {
	std := isStd(imp) && imp != "C"
	if std || inLayer1(imp) || imp == pkgStore || under(imp, pkgRender) || under(imp, pkgBuiltin) || imp == gin {
		return ""
	}
	return ruleConsole
}

// checkAssemblyFile 查装配点一个非测试源文件的 import。
func checkAssemblyFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	for _, imp := range f.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		if r := assemblyRule(pkg, importPath); r != "" {
			found = append(found, violation{rule: r, pkg: pkg, subject: importPath, pos: position(fset, imp.Pos())})
		}
	}
	return found
}

// assemblyRule 返回装配点的包 pkg import imp 违反的规则。装配点知道每个接口由谁实现，本模块的
// 包它都能 import；第三方只有 gin（它不起 HTTP，只是把 console.New 的引擎交给 http.Server）
// 与 yaml.v3（读配置）。
func assemblyRule(pkg, imp string) rule {
	std := isStd(imp) && imp != "C"
	if std || strings.HasPrefix(imp, modulePath+"/") || imp == gin || imp == yamlV3 {
		return ""
	}
	return ruleAssembly
}
```

- [x] **Step 7: 跑测试，确认绿**

Run: `go test ./internal/console/ ./internal/archtest/ -v`
Expected: PASS。`internal/archtest` 的日志里出现「控制台」与「装配点」两层各查到的包数。

- [x] **Step 8: 守卫的反例**

先确认守卫真的在查这两层：临时在 `internal/console/errors.go` 里加一行 `import _ "github.com/lib/pq"`，跑
`go test ./internal/archtest/ -run Test总检查`，应报 `ruleConsole`；删掉该行再跑一次，恢复绿。
（这是计划「防空跑」的例行步骤：守卫没有反例就等于没查。）

- [x] **Step 9: 提交**

```bash
git add internal/console go.mod go.sum internal/archtest
git commit -m "feat(console): 响应信封、错误到状态码的映射与守卫两条新规则"
```

---

## Task 2：第 1 层补全局词表（L-8、S3-10）

**Files:**
- Create: `internal/core/catalog/vocab.go`
- Test: `internal/core/catalog/vocab_test.go`
- Create: `internal/core/model/subject.go`
- Test: `internal/core/model/subject_test.go`
- Modify: `internal/core/model/authoring.go`
- Modify: `internal/core/model/authoring_test.go`
- Modify: `internal/core/model/codes_align_test.go`
- Modify: `internal/core/activation/hook.go`、`internal/core/admission/admission.go`、`internal/core/params/params.go`、`internal/core/snapshot/build.go`

**Interfaces:**
- Consumes: 无（与 Task 1 并行）
- Produces:
  - `catalog.AllFormats() []Format`、`catalog.AllSeries() []Series`、`catalog.AllTimings() []Timing`、`catalog.AllInterpreters() []Interpreter`、`catalog.AllSystemParts() []SystemPart`、`catalog.AllSystemValues() []SystemValue`、`catalog.AllArchs() []Arch`、`catalog.AllFirmwares() []Firmware`
  - `model.KindHost`/`KindISO`/`KindHook`/`KindImpl`/`KindTemplate`/`KindSlot`/`KindParam`/`KindBody`（八个 `string` 常量）、`model.Kinds() []string`
  - `(model.HookKey).String() string`、`(model.TemplateKey).String() string`

第 1 层清单 L-8 归第 5 层的部分：catalog 没有导出全局词表，第 5、6 层列下拉框只能经 `Families`、`SeriesOf` 反推；`Subject.Kind` 的八个取值是字面量；「ID@版本」在四个包各拼一份。S3-10 挂在词表上：`host_platform_invalid` 的说明列出的架构只能查到「列出的都合法」，目录新加了架构而说明没补，查不出来。

- [x] **Step 1: 写失败的测试——词表**

Create `internal/core/catalog/vocab_test.go`：

```go
package catalog

import (
	"slices"
	"testing"
)

// 每张 All* 表与既有的登记表逐项一致：加一个取值只改一处，两处对不上就红。
func Test全局词表与登记表逐项一致(t *testing.T) {
	formats := AllFormats()
	if !slices.Equal(formats, []Format{FormatKickstart, FormatPreseed, FormatAutoinstall,
		FormatAutoyast, FormatAgama, FormatUnattend, FormatESXiKS}) {
		t.Errorf("AllFormats 是 %v，与 formatTable 不一致", formats)
	}
	var series []Series
	for _, f := range formats {
		series = append(series, SeriesOfFormat(f)...)
	}
	if !slices.Equal(AllSeries(), series) {
		t.Errorf("AllSeries 是 %v，与 seriesTable 不一致", AllSeries())
	}
	if !slices.Equal(AllTimings(), []Timing{TimingPre, TimingPostInstaller, TimingPostTarget, TimingFirstboot}) {
		t.Errorf("AllTimings 是 %v，与 timingTable 不一致", AllTimings())
	}
	if !slices.Equal(AllSystemParts(), []SystemPart{PartSyslog, PartFirstboot, PartCleanup}) {
		t.Errorf("AllSystemParts 是 %v，与 systemPartTable 不一致", AllSystemParts())
	}
	if !slices.Equal(AllSystemValues(), []SystemValue{SysRepo, SysISO, SysSMB, SysGateway, SysTask, SysSN, SysLBrace}) {
		t.Errorf("AllSystemValues 是 %v，与 systemValueTable 不一致", AllSystemValues())
	}
	if !slices.Equal(AllArchs(), []Arch{ArchX8664, ArchAarch64}) {
		t.Errorf("AllArchs 是 %v，与 platformArchTable 不一致", AllArchs())
	}
	if !slices.Equal(AllFirmwares(), []Firmware{FirmwareBIOS, FirmwareUEFI}) {
		t.Errorf("AllFirmwares 是 %v，与 platformFirmwareTable 不一致", AllFirmwares())
	}
	var interpreters []Interpreter
	for _, row := range interpreterTable {
		if !slices.Contains(interpreters, row.id) {
			interpreters = append(interpreters, row.id)
		}
	}
	if !slices.Equal(AllInterpreters(), interpreters) {
		t.Errorf("AllInterpreters 是 %v，与 interpreterTable 不一致", AllInterpreters())
	}
}

// 每次返回新切片：调用方改了不能影响下一次查询（全局约定）。
func Test全局词表返回副本(t *testing.T) {
	got := AllFormats()
	got[0] = "篡改"
	if AllFormats()[0] == "篡改" {
		t.Error("AllFormats 返回了内部的切片，调用方改了就影响了别人")
	}
}
```

注意：上面的字面量（`FormatKickstart`、`PartSyslog`、`ArchX8664` 等）**现在还不存在**——L-8 的另一半正是「catalog 没有导出常量」。本任务一并补上：在 `vocab.go` 里为族、格式、系、时机、解释器、部件、系统值、架构、固件各导出一组常量，并让 `series.go`、`timing.go`、`platform.go`、`delivery.go` 的三张登记表改用常量书写。测试因此先因符号未定义而编译失败，补常量后转绿。

Run: `go test ./internal/core/catalog/ -run 'Test全局词表' -v`
Expected: FAIL，`undefined: AllFormats`。

- [x] **Step 2: 写失败的测试——Subject.Kind 常量**

Create `internal/core/model/subject_test.go`：

```go
package model

import (
	"slices"
	"strings"
	"testing"
)

// Subject.Kind 的注释列了八个取值，常量与它逐项一致：第 5、6 层按它解析 Path（problem.go 第 30 行）。
func TestSubject的Kind常量与注释一致(t *testing.T) {
	want := []string{KindHost, KindISO, KindHook, KindImpl, KindTemplate, KindSlot, KindParam, KindBody}
	if !slices.Equal(Kinds(), want) {
		t.Errorf("Kinds 是 %v，应为 %v", Kinds(), want)
	}
	if len(slices.Compact(slices.Clone(want))) != len(want) {
		t.Error("八个 Kind 常量有重复")
	}
	// problem.go 的注释把这八个写在一行里，常量漏一个或被改掉，这里对不上。
	doc := "Kind 取 host、iso、hook、impl、template、slot、param、body 之一"
	for _, k := range want {
		if !strings.Contains(doc, k) {
			t.Errorf("Subject.Kind 的注释里没有 %q", k)
		}
	}
}

// 「ID@版本」只在 model 拼一次（L-8）：activation、admission、params、snapshot 与第 2 层的
// store.VersionID 都调它。
func Test版本键的字符串形式(t *testing.T) {
	if got := (HookKey{ID: "lvm", Version: 3}).String(); got != "lvm@3" {
		t.Errorf("HookKey.String() 是 %q，应为 \"lvm@3\"", got)
	}
	if got := (TemplateKey{ID: "rhel-base", Version: 12}).String(); got != "rhel-base@12" {
		t.Errorf("TemplateKey.String() 是 %q，应为 \"rhel-base@12\"", got)
	}
}
```

Run: `go test ./internal/core/model/ -run 'TestSubject的Kind|Test版本键' -v`
Expected: FAIL，`undefined: KindHost`、`undefined: Kinds`。

- [x] **Step 3: 写失败的测试——S3-10 架构全集**

Modify `internal/core/model/codes_align_test.go`，在 `alignFirmwares` 之后加：

```go
// 架构查得到全集（L-8 补了 catalog.AllArchs 之后）：说明漏列一种架构也报得出来（S3-10）。
func alignArchs() []string {
	var out []string
	for _, a := range catalog.AllArchs() {
		out = append(out, string(a))
	}
	return out
}
```

把 `TestAlign主机平台的取值与目录一致` 里的架构循环从「逐个查合法」改成「比对全集」，区分大小写那条保留：

```go
	alignSame(t, "host_platform_invalid 列出的架构", arches, alignArchs())
	for _, a := range arches {
		// 说明还写了「区分大小写」：大写的写法不能也算合法。
		if !catalog.Arch(a).Valid() || catalog.Arch(strings.ToUpper(a)).Valid() {
			t.Errorf("host_platform_invalid 列出的架构 %q 应是目录里区分大小写的合法取值", a)
		}
	}
```

（`alignSame` 已存在，`alignFirmwares` 就是这么用的。）

Run: `go test ./internal/core/model/ -run 'TestAlign主机平台' -v`
Expected: FAIL，`undefined: catalog.AllArchs`。

- [x] **Step 4: 实现词表与常量**

Create `internal/core/catalog/vocab.go`：

```go
package catalog

import "slices"

// 本文件是全局词表（第 1 层清单 L-8 归第 5 层的部分）：第 5、6 层列下拉框、生成 OpenAPI 的
// 枚举都经它取，不靠 SeriesOf 反推，也不会在各包各写一份字面量。每张表按登记顺序返回新切片，
// 调用方改了不影响别人。目录内置在程序里（C1），加一个取值只改对应的登记表，这里的函数跟着改。

// AllFamilies 是全部族。
func AllFamilies() []Family { return families() }

// AllFormats 是全部应答格式，按登记顺序。
func AllFormats() []Format {
	out := make([]Format, 0, len(formatTable))
	for _, r := range formatTable {
		out = append(out, r.id)
	}
	return out
}

// AllSeries 是全部系，按登记顺序。
func AllSeries() []Series {
	out := make([]Series, 0, len(seriesTable))
	for _, r := range seriesTable {
		out = append(out, r.id)
	}
	return out
}

// AllTimings 是全部执行时机，按先后排。
func AllTimings() []Timing {
	out := make([]Timing, 0, len(timingTable))
	for _, r := range timingTable {
		out = append(out, r.id)
	}
	return out
}

// AllInterpreters 是全部解释器，按登记顺序、不重复。
func AllInterpreters() []Interpreter {
	var out []Interpreter
	for _, r := range interpreterTable {
		if !slices.Contains(out, r.id) {
			out = append(out, r.id)
		}
	}
	return out
}

// AllSystemParts 是全部系统部件。
func AllSystemParts() []SystemPart {
	out := make([]SystemPart, 0, len(systemPartTable))
	for _, r := range systemPartTable {
		out = append(out, r.id)
	}
	return out
}

// AllSystemValues 是全部系统值。
func AllSystemValues() []SystemValue {
	out := make([]SystemValue, 0, len(systemValueTable))
	for _, r := range systemValueTable {
		out = append(out, r.id)
	}
	return out
}

// AllArchs 是全部架构。
func AllArchs() []Arch {
	out := make([]Arch, 0, len(platformArchTable))
	for _, r := range platformArchTable {
		out = append(out, r.id)
	}
	return out
}

// AllFirmwares 是全部固件，按登记顺序（先 bios 后 uefi）。
func AllFirmwares() []Firmware {
	out := make([]Firmware, 0, len(platformFirmwareTable))
	for _, r := range platformFirmwareTable {
		out = append(out, r.id)
	}
	return out
}
```

同文件里加常量。族、格式、系各一组（系有 14 个，照 `seriesTable` 的顺序写）：

```go
// 族、格式、系的常量。字面量会进数据库与 URL，发布后不能改名（C1）。
const (
	FamilyLinux   Family = "linux"
	FamilyWindows Family = "windows"
	FamilyESXi    Family = "esxi"
)

const (
	FormatKickstart   Format = "kickstart"
	FormatPreseed     Format = "preseed"
	FormatAutoinstall Format = "autoinstall"
	FormatAutoyast    Format = "autoyast"
	FormatAgama       Format = "agama"
	FormatUnattend    Format = "unattend"
	FormatESXiKS      Format = "esxi-ks"
)

const (
	SeriesRHEL         Series = "rhel"
	SeriesRocky        Series = "rocky"
	SeriesAlmalinux    Series = "almalinux"
	SeriesOpeneuler    Series = "openeuler"
	SeriesOpencloudos  Series = "opencloudos"
	SeriesDebian       Series = "debian"
	SeriesUbuntu       Series = "ubuntu"
	SeriesSLES15       Series = "sles15"
	SeriesLeap15       Series = "leap15"
	SeriesSLES16       Series = "sles16"
	SeriesLeap16       Series = "leap16"
	SeriesWinServer    Series = "windows-server"
	SeriesWinDesktop   Series = "windows-desktop"
	SeriesESXi         Series = "esxi"
)
```

时机、解释器、部件、系统值、架构、固件各一组，字面量照 `timing.go`、`platform.go` 的登记表抄：

```go
const (
	TimingPre           Timing = "pre"
	TimingPostInstaller Timing = "post-installer"
	TimingPostTarget    Timing = "post-target"
	TimingFirstboot     Timing = "firstboot"
)

const (
	InterpSH        Interpreter = "sh"
	InterpBash      Interpreter = "bash"
	InterpCmd       Interpreter = "cmd"
	InterpPowershell Interpreter = "powershell"
)

const (
	PartSyslog   SystemPart = "syslog"
	PartFirstboot SystemPart = "firstboot"
	PartCleanup  SystemPart = "cleanup"
)

const (
	SysRepo    SystemValue = "repo"
	SysISO     SystemValue = "iso"
	SysSMB     SystemValue = "smb"
	SysGateway SystemValue = "gateway"
	SysTask    SystemValue = "task"
	SysSN      SystemValue = "sn"
	SysLBrace  SystemValue = "lbrace"
)

const (
	ArchX8664   Arch = "x86_64"
	ArchAarch64 Arch = "aarch64"
)

const (
	FirmwareBIOS Firmware = "bios"
	FirmwareUEFI Firmware = "uefi"
)
```

把 `series.go` 的 `familyTable`、`formatTable`、`seriesTable`，`timing.go` 的四张表，`platform.go` 的两张表改用上面的常量书写（机械替换，值不变）。`vocab_test.go` 的字面量同步改成常量。

Create `internal/core/model/subject.go`：

```go
package model

import "slices"

// Subject.Kind 的八个取值（problem.go 的注释列了它们）。第 5、6 层按它把错误标到具体的行或
// 单元格，也按它过滤，所以集中在这里，不在各包写字面量（L-8）。
const (
	KindHost     = "host"
	KindISO      = "iso"
	KindHook     = "hook"
	KindImpl     = "impl"
	KindTemplate = "template"
	KindSlot     = "slot"
	KindParam    = "param"
	KindBody     = "body"
)

// Kinds 是全部 Subject.Kind，顺序同上面的常量。每次返回新切片。
func Kinds() []string {
	return slices.Clone(kinds)
}

var kinds = []string{KindHost, KindISO, KindHook, KindImpl, KindTemplate, KindSlot, KindParam, KindBody}
```

在 `internal/core/model/authoring.go` 里给两个键加 `String()`：

```go
// String 是「ID@版本」的写法，四处判定与第 2 层的审计目标都用它（L-8）：以前每处各拼一份，
// 拼法漂了就对不上。
func (k HookKey) String() string    { return k.ID + "@" + strconv.Itoa(k.Version) }
func (k TemplateKey) String() string { return k.ID + "@" + strconv.Itoa(k.Version) }
```

（`authoring.go` 已 import `strconv`；没有就补。）

- [x] **Step 5: 四处判定改调 String()**

`internal/core/activation/hook.go`、`internal/core/admission/admission.go`、`internal/core/params/params.go`、`internal/core/snapshot/build.go` 里各有一处 `t.ID + "@" + strconv.Itoa(t.Version)`（或 k 的等价写法），改成调 `String()`。改完 `go build ./...`，确认 `strconv` 仍有别的用处，没有就删掉 import。

- [x] **Step 6: 跑测试**

Run: `go test ./internal/core/... -v`
Expected: PASS。

- [x] **Step 7: 提交**

```bash
git add internal/core
git commit -m "feat(core): 导出全局词表与 Subject.Kind 常量，「ID@版本」收成一份（L-8、S3-10）"
```

---

## Task 3：存储层的 Users 聚合

**Files:**
- Create: `internal/store/users.go`
- Test: `internal/store/users_test.go`
- Create: `internal/store/pgstore/users.go`
- Modify: `internal/store/pgstore/schema.go`（加 `users` 表）
- Modify: `internal/store/pgstore/contract_helpers_test.go`（`contents` 加 `Users`）
- Test: `internal/store/pgstore/contract_users_test.go`
- Modify: `internal/store/store.go`（`Store` 接口加 `Users`）

**Interfaces:**
- Consumes: 无（与 Task 1、2 并行）
- Produces:
  - `store.Users` 接口：`CreateUser`、`UpdateUser`、`GetUser`、`ListUsers`
  - `store.User{Username, DisplayName string; Role Role; Password string; Disabled bool; CreatedAt, Rev int64}`
  - `store.Role`，常量 `RoleViewer`、`RoleOperator`、`RoleHookAuthor`、`RoleHookApprover`、`RoleAdmin`，`Roles() []Role`，`(Role).Valid() bool`
  - `store.UserQuery{Cursor string; Limit int}`
  - `store.CheckUser(u User) error`
  - `store.Store` 接口多一个 `Users`

用户与角色落库，会话不放库（第 5 层设计 §5）：会话不是业务数据，单进程放内存就够，放进库里要额外处理过期清理，而 AutoMigrate 表达不了。

- [x] **Step 1: 写失败的测试——CheckUser 与 Role**

Create `internal/store/users_test.go`：

```go
package store

import (
	"errors"
	"testing"
)

func goodUser() User {
	return User{Username: "alice", DisplayName: "爱丽丝", Role: RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
}

func TestCheckUser每条前提各一个反例(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*User)
	}{
		{"用户名为空", func(u *User) { u.Username = "" }},
		{"用户名不是标识符", func(u *User) { u.Username = "Alice" }},
		{"用户名带连字符", func(u *User) { u.Username = "al-ice" }},
		{"用户名超过 32 个字符", func(u *User) {
			u.Username = ""
			for i := 0; i < 33; i++ {
				u.Username += "a"
			}
		}},
		{"显示名存不下", func(u *User) { u.DisplayName = "爱\x00丽丝" }},
		{"显示名不是合法 UTF-8", func(u *User) { u.DisplayName = "爱\xff丽丝" }},
		{"角色不合法", func(u *User) { u.Role = "root" }},
		{"角色大小写不同", func(u *User) { u.Role = "Admin" }},
		{"口令散列为空", func(u *User) { u.Password = "" }},
		{"口令散列存不下", func(u *User) { u.Password = "pbkdf2\x00" }},
		{"建档时间为 0", func(u *User) { u.CreatedAt = 0 }},
		{"建档时间为负", func(u *User) { u.CreatedAt = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := goodUser()
			c.mod(&u)
			if err := CheckUser(u); !errors.Is(err, ErrInvalid) {
				t.Errorf("CheckUser 返回 %v，应报 ErrInvalid", err)
			}
		})
	}
}

func TestCheckUser合规则的通过(t *testing.T) {
	if err := CheckUser(goodUser()); err != nil {
		t.Fatalf("CheckUser 返回 %v，合规则的不该报错", err)
	}
}

// 禁用与 Rev 不查：禁用是正当状态，Rev 由仓储维护。
func TestCheckUser不管禁用与Rev(t *testing.T) {
	u := goodUser()
	u.Disabled = true
	u.Rev = 7
	if err := CheckUser(u); err != nil {
		t.Errorf("CheckUser 返回 %v，禁用与 Rev 不该拦", err)
	}
}

func TestRole的取值与Valid(t *testing.T) {
	want := []Role{RoleViewer, RoleOperator, RoleHookAuthor, RoleHookApprover, RoleAdmin}
	if len(Roles()) != len(want) {
		t.Fatalf("Roles 有 %d 个，应为 %d 个", len(Roles()), len(want))
	}
	for _, r := range want {
		if !r.Valid() {
			t.Errorf("角色 %q 应合法", r)
		}
		if string(r) == "" {
			t.Error("有角色的字面量为空")
		}
	}
	for _, bad := range []Role{"", "root", "Admin", "viewer "} {
		if bad.Valid() {
			t.Errorf("角色 %q 不该合法", bad)
		}
	}
}

// Roles 返回副本：界面下拉框改了不能影响别人。
func TestRoles返回副本(t *testing.T) {
	got := Roles()
	got[0] = "篡改"
	if Roles()[0] == "篡改" {
		t.Error("Roles 返回了内部的切片")
	}
}
```

Run: `go test ./internal/store/ -run 'TestCheckUser|TestRole|TestRoles' -v`
Expected: FAIL，`undefined: CheckUser`、`undefined: User`、`undefined: RoleOperator`。

- [x] **Step 2: 写失败的测试——契约用例**

Create `internal/store/pgstore/contract_users_test.go`。先看一眼 `contract_hosts_test.go` 的用例写法（`runCases`、`must`、`wantIs`、`wantDuplicate`、`wantEqual`、`anAudit`、`wantUnchanged`、`pagesFrom`），照它写：

```go
package pgstore_test

import (
	"errors"
	"testing"

	"github.com/lober/lober/internal/store"
)

// Test契约用户 是用户这一组：建、改、查。会话不在库里（第 5 层设计 §5），所以这里没有登录与
// 登出的用例。要审计的写操作每个对象写一条审计，由审计那一组查。
func Test契约用户(t *testing.T) {
	runCases(t, []testCase{
		{"建成后读回来与写进去的相同，Rev 为 1", userCreateReadBack},
		{"显示名、角色、口令、禁用都能改成零值", userUpdateToZero},
		{"用户名不能改：审计要能一直指到这个人", userUpdateKeepsUsername},
		{"Rev 对不上报 ErrConflict，没登记的报 ErrNotFound", userUpdateConflict},
		{"记录不合法报 ProblemsError，没登记的同理", userCreateInvalid},
		{"用户名已登记报 DuplicateError，Existing 是那个用户名", userCreateDuplicate},
		{"ListUsers 按用户名的字节序，翻两页与一次全取相同", userListPaging},
		{"ListUsers 的 Limit 为 0 时每页 100 条", userListDefaultLimit},
		{"禁用的人照常读得回来，口令不经接口出去也不改", userDisabledReadBack},
	})
}

func userCreateReadBack(t *testing.T, s *pgstore.Store) {
	in := store.User{Username: "alice", DisplayName: "爱丽丝", Role: store.RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
	got, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "建用户")
	wantEqual(t, got, in, "读回来的与写进去的相同")
	if got.Rev != 1 {
		t.Errorf("Rev 是 %d，应为 1", got.Rev)
	}
	back, err := s.GetUser(t.Context(), "alice")
	must(t, err, "再读一次")
	wantEqual(t, back, got, "两次读到的相同")
}

func userUpdateKeepsUsername(t *testing.T, s *pgstore.Store) {
	in := store.User{Username: "alice", DisplayName: "爱丽丝", Role: store.RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
	created, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "建用户")
	// 用户名换成 bob：入参不合法，因为用户名是身份，CheckUser 只认标识符，而仓储按传入的用户名找行
	renamed := created
	renamed.Username = "bob"
	renamed.DisplayName = "鲍勃"
	if _, err := s.UpdateUser(t.Context(), renamed, anAudit("user.update")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("按不存在的用户名修改返回 %v，应报 ErrNotFound", err)
	}
	after, err := s.GetUser(t.Context(), "alice")
	must(t, err, "爱丽丝还在")
	wantEqual(t, after, created, "没被改动")
}

func userUpdateConflict(t *testing.T, s *pgstore.Store) {
	in := store.User{Username: "alice", DisplayName: "爱丽丝", Role: store.RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
	created, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "建用户")
	stale := created
	stale.Rev = 99
	stale.DisplayName = "爱丽丝二世"
	if _, err := s.UpdateUser(t.Context(), stale, anAudit("user.update")); !errors.Is(err, store.ErrConflict) {
		t.Errorf("Rev 对得上才写得进：返回 %v，应报 ErrConflict", err)
	}
	if _, err := s.UpdateUser(t.Context(), store.User{Username: "nobody"}, anAudit("user.update")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("改没登记的用户返回 %v，应报 ErrNotFound", err)
	}
}

func userCreateDuplicate(t *testing.T, s *pgstore.Store) {
	in := store.User{Username: "alice", DisplayName: "爱丽丝", Role: store.RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
	_, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "第一次建")
	_, err = s.CreateUser(t.Context(), in, anAudit("user.create"))
	wantDuplicate(t, err, "username", "alice", "再建同一个用户名")
}

func userListPaging(t *testing.T, s *pgstore.Store) {
	for _, name := range []string{"bob", "alice", "carol", "dave"} {
		u := store.User{Username: name, DisplayName: name, Role: store.RoleViewer,
			Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
		if _, err := s.CreateUser(t.Context(), u, anAudit("user.create")); err != nil {
			t.Fatalf("建 %s：%v", name, err)
		}
	}
	pages := pagesFrom(t, "", func(cursor string) (store.Page[store.User], error) {
		return s.ListUsers(t.Context(), store.UserQuery{Cursor: cursor, Limit: 2})
	})
	// 按用户名的字节序：alice、bob、carol、dave
	want := []string{"alice", "bob", "carol", "dave"}
	var got []string
	for _, page := range pages {
		for _, u := range page {
			got = append(got, u.Username)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("翻页得到 %v，应为 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("翻页得到 %v，应按字节序 %v", got, want)
		}
	}
}
```

把 `userCreateDuplicate` 改成真反例前，先跑一次看 `wantDuplicate` 的签名（`wantDuplicate(t, err, field, existing, what)`）：

```go
func userCreateDuplicate(t *testing.T, s *pgstore.Store) {
	in := store.User{Username: "alice", DisplayName: "爱丽丝", Role: store.RoleOperator,
		Password: "pbkdf2-sha256$600000$c2FsdHNhbHRzYWx0$abcdef", CreatedAt: 1750000000}
	_, err := s.CreateUser(t.Context(), in, anAudit("user.create"))
	must(t, err, "第一次建")
	_, err = s.CreateUser(t.Context(), in, anAudit("user.create"))
	wantDuplicate(t, err, "username", "alice", "再建同一个用户名")
}
```

在 `internal/store/pgstore/contract_helpers_test.go` 的 `contents` 里加一个字段，并在 `snapshot` 里填上：

```go
type contents struct {
	Hosts []model.Host
	ISOs  []model.ISO
	Users []store.User
	Audit []store.AuditEntry
}
```

`snapshot` 里加一行（`userPages` 照 `hostPages` 的写法写一个，或直接 `Limit: 500` 取一页）：

```go
		Users: slices.Concat(userPages(t, s, store.UserQuery{Limit: 500})...),
```

被拒的写入因此查得出「用户表也没动」。

Run: `go test ./internal/store/pgstore/ -run 'Test契约用户' -v`
Expected: FAIL，`undefined: s.CreateUser`、`undefined: store.RoleOperator`。

Step 2 里 `userCreateInvalid`、`userUpdateToZero`、`userListDefaultLimit`、`userDisabledReadBack` 四个用例照 `contract_hosts_test.go` 的同名用例改写：`userCreateInvalid` 逐条传 `TestCheckUser每条前提各一个反例` 里的坏记录，断言 `wantProblems(t, err, store.ErrInvalid, ...)`；`userUpdateToZero` 把显示名清空、角色置 viewer、禁用置 true，断言读回来就是这些零值；`userListDefaultLimit` 建 101 个用户，断言第一页 100 条、第二页 1 条；`userDisabledReadBack` 建一个禁用的，断言读得回来且 `Disabled` 为 true、`Password` 原样。

- [x] **Step 3: 实现契约与纯函数**

Create `internal/store/users.go`：

```go
package store

import (
	"errors"
	"fmt"
	"slices"

	"github.com/lober/lober/internal/core/model"
)

// Users 是用户与角色（总 spec §4.2：用户、角色与会话推迟到第 5 层）。会话不在库里，由控制台放在
// 内存里；这里只管人。要审计的方法最后一个参数是 Audit，审计在同一个事务里写（§6.2 第 8 条）。
type Users interface {
	// CreateUser 建一个用户，Rev 置 1，CreatedAt 由调用方给。记录要过 CheckUser；用户名已登记报
	// DuplicateError{Field: "username", Existing: 用户名}。
	CreateUser(ctx context.Context, u User, a Audit) (User, error)
	// UpdateUser 按用户名整条替换显示名、角色、口令散列与禁用标记，Rev 对不上报 ErrConflict，
	// 成功后 Rev 加 1。用户名本身不能改：它是身份，审计要能一直指到这个人，所以按用户名找行，
	// 换个用户名就是另一个人。
	UpdateUser(ctx context.Context, u User, a Audit) (User, error)
	// GetUser 按用户名取，找不到报 ErrNotFound。
	GetUser(ctx context.Context, username string) (User, error)
	// ListUsers 按用户名的字节序分页，规则同别的 List。
	ListUsers(ctx context.Context, q UserQuery) (Page[User], error)
}

// User 是一个能登录控制台的人。口令只存散列，永不经 HTTP 出去（第 5 层设计 §5）；散列由控制台
// 算好再交给仓储，仓储不认算法，也不查强度。
type User struct {
	Username    string // 标识符，1–32，是身份
	DisplayName string // 可存文本，可以用中文
	Role        Role
	Password    string // pbkdf2 散列
	Disabled    bool   // 禁用后会话立即失效，但记录留着：审计要能一直指到这个人
	CreatedAt   int64  // Unix 秒，由调用方传入：仓储不读时钟
	Rev         int64  // 每次写入加 1，由仓储维护
}

// Role 是角色，固定五个。权限码到角色的映射在控制台（第 5 层设计 §4.4）：角色是数据，权限是
// 界面规则，加一个权限码不该惊动存储层。
type Role string

const (
	RoleViewer       Role = "viewer"
	RoleOperator     Role = "operator"
	RoleHookAuthor   Role = "hook-author"
	RoleHookApprover Role = "hook-approver"
	RoleAdmin        Role = "admin"
)

// roles 是全部角色，顺序即界面下拉框的顺序。
var roles = []Role{RoleViewer, RoleOperator, RoleHookAuthor, RoleHookApprover, RoleAdmin}

// Roles 是全部角色，顺序同上。每次返回新切片。
func Roles() []Role { return slices.Clone(roles) }

// Valid 报告 r 是不是角色，只认原样的写法（同 catalog 的做法）。
func (r Role) Valid() bool { return slices.Contains(roles, r) }

// UserQuery 查用户。
type UserQuery struct {
	Cursor string
	Limit  int
}

// userMaxLen 是用户名与显示名的长度上限：用户名还要是标识符（更严），显示名按字算。
const userMaxLen = 32

// CheckUser 查一个用户记录合不合法，问题包在 ProblemsError(ErrInvalid) 里。用户名是不是标识符、
// 显不显示存得下、角色合不合法、口令散列非空且存得下、建档时间大不大于 0。用户名有没有被占用由
// 仓储查；Disabled 与 Rev 不查，禁用是正当状态，Rev 由仓储维护。
func CheckUser(u User) error {
	var ps []model.Problem
	if !model.ValidIdent(u.Username) || len(u.Username) > userMaxLen {
		ps = append(ps, model.NewProblem("text_not_storable", model.Subject{Kind: "user", ID: u.Username, Path: "username"}))
	}
	if !model.StorableText(u.DisplayName) || len([]rune(u.DisplayName)) > userMaxLen {
		ps = append(ps, model.NewProblem("text_not_storable", model.Subject{Kind: "user", ID: u.Username, Path: "display_name"}))
	}
	if !u.Role.Valid() {
		ps = append(ps, model.NewProblem("text_not_storable", model.Subject{Kind: "user", ID: u.Username, Path: "role"}))
	}
	if !model.StorableText(u.Password) {
		ps = append(ps, model.NewProblem("text_not_storable", model.Subject{Kind: "user", ID: u.Username, Path: "password"}))
	}
	if u.CreatedAt <= 0 {
		return fmt.Errorf("用户 %s 的建档时间 %d 不大于 0：%w", u.Username, u.CreatedAt, ErrInvalid)
	}
	if len(ps) > 0 {
		return &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return nil
}
```

`Subject.Kind` 用 `"user"`：`model.Kinds()` 里没有它，而 `Subject` 的注释说 Kind 取八个之一。**这里不新增 Kind**——第 5 层设计 §4.1 的问题 JSON 原样透传，`Kind` 是给前端定位用的，用户的问题落在整个用户上，用 `Path` 区分字段就够。若后续要把它纳入 `Kinds()`，只改 `model/subject.go` 一处。

`store.go` 的 `Store` 接口加一行 `Users`：

```go
type Store interface {
	Hosts
	ISOs
	Hooks
	Templates
	Tasks
	Events
	AuditLog
	Users
	Close() error
}
```

并在 `doc.go` 的能力清单里补一句「用户与角色（第 5 层加，会话不在库里）」。

- [x] **Step 4: 实现 pgstore**

Create `internal/store/pgstore/users.go`：

```go
package pgstore

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/store"
)

// userRow 是 users 表的一行。用户名是主键，没有代理键：审计要能一直指到这个人，而用户名不改。
type userRow struct {
	Username    string `gorm:"primaryKey"`
	DisplayName string `gorm:"not null"`
	Role        string `gorm:"not null;check:role IN ('viewer', 'operator', 'hook-author', 'hook-approver', 'admin')"`
	Password    string `gorm:"not null"`
	Disabled    bool   `gorm:"not null"`
	CreatedAt   int64  `gorm:"not null;check:created_at > 0"`
	Rev         int64  `gorm:"not null;check:rev >= 1"`
}

func (userRow) TableName() string { return "users" }

// userColumns 是修改用户时写的列：整条替换，把禁用改回 false、把显示名清空都照写。
var userColumns = []string{"display_name", "role", "password", "disabled", "rev"}

func toUserRow(u store.User) userRow {
	return userRow{Username: u.Username, DisplayName: u.DisplayName, Role: string(u.Role),
		Password: u.Password, Disabled: u.Disabled, CreatedAt: u.CreatedAt, Rev: u.Rev}
}

// user 把一行转回 store.User。
func (r userRow) user() store.User {
	return store.User{Username: r.Username, DisplayName: r.DisplayName, Role: store.Role(r.Role),
		Password: r.Password, Disabled: r.Disabled, CreatedAt: r.CreatedAt, Rev: r.Rev}
}

// CreateUser 见 store.Users。
func (s *Store) CreateUser(ctx context.Context, u store.User, a store.Audit) (store.User, error) {
	if err := store.CheckAudit(a); err != nil {
		return store.User{}, err
	}
	if err := store.CheckUser(u); err != nil {
		return store.User{}, err
	}
	u.Rev = 1
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return addAudits(tx, a, "user", u.Username)
	})
	if err != nil {
		return store.User{}, translate(err, false, func(string) string { return u.Username })
	}
	return row.user(), nil
}

// UpdateUser 见 store.Users。
func (s *Store) UpdateUser(ctx context.Context, u store.User, a store.Audit) (store.User, error) {
	if err := store.CheckAudit(a); err != nil {
		return store.User{}, err
	}
	if err := store.CheckUser(u); err != nil {
		return store.User{}, err
	}
	row := toUserRow(u)
	err := s.write(ctx, func(tx *gorm.DB) error {
		// 按用户名找行：换个用户名就是另一个人，找不到报 ErrNotFound
		res := tx.Model(&row).Where("username = ?", u.Username).Updates(map[string]any{
			"display_name": row.DisplayName, "role": row.Role, "password": row.Password,
			"disabled": row.Disabled, "rev": gorm.Expr("users.rev + 1"),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		// Rev 由库里加 1，读回来给调用方
		var back userRow
		if err := tx.First(&back, "username = ?", u.Username).Error; err != nil {
			return err
		}
		row = back
		return addAudits(tx, a, "user", u.Username)
	})
	if err != nil {
		return store.User{}, translate(err, false, func(string) string { return u.Username })
	}
	return row.user(), nil
}

// GetUser 见 store.Users。
func (s *Store) GetUser(ctx context.Context, username string) (store.User, error) {
	var row userRow
	err := s.read(ctx, func(tx *gorm.DB) error {
		return tx.First(&row, "username = ?", username).Error
	})
	if err != nil {
		return store.User{}, translate(err, false, func(string) string { return username })
	}
	return row.user(), nil
}

// ListUsers 见 store.Users。结构与 ListHosts 相同：按 username 的字节序（COLLATE "C"，
// pgstore 的库默认排序规则不是字节序）、多取一行判断有没有下一页、游标记的是最后一项的用户名。
func (s *Store) ListUsers(ctx context.Context, q store.UserQuery) (store.Page[store.User], error) {
	limit, err := store.CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return store.Page[store.User]{}, err
	}
	var after string
	if q.Cursor != "" {
		if err := store.DecodeCursor(q.Cursor, "users", &after); err != nil {
			return store.Page[store.User]{}, err
		}
	}
	db := s.read(ctx)
	if q.Cursor != "" {
		db = db.Where(`username COLLATE "C" > ?`, after)
	}
	var rows []userRow
	if err := db.Order(`username COLLATE "C"`).Limit(limit + 1).Find(&rows).Error; err != nil {
		return store.Page[store.User]{}, err
	}
	return page(rows, limit, func(r userRow) (store.User, error) { return r.user(), nil },
		func(r userRow) string { return store.EncodeCursor("users", r.Username) })
}

// userRow.user 返回的是值，conv 因此不会失败；包一层是为了用上既有的 page。
var _ = errors.Is
```

`s.read`、`s.write`、`translate`、`addAudits`、`page`、`storable` 都是 `pgstore` 既有的辅助，签名照 `hosts.go`、`open.go` 的用法。`errors` 只为 `var _ = errors.Is` 占一行，实现完删掉该行与 import。

- [x] **Step 5: 加表**

Modify `internal/store/pgstore/schema.go`，在 `tables` 末尾加：

```go
	{
		// 用户与角色（第 5 层加）。用户名是主键，没有代理键：审计要能一直指到这个人，而用户名不改。
		// 口令只存散列。禁用不建部分索引：禁用的人照常要读得出来（审计与历史操作都指得到）。
		name:    "users",
		primary: []string{"username"},
		checks: []check{
			{"chk_users_role", "CHECK (role = ANY (ARRAY['viewer'::text, 'operator'::text, 'hook-author'::text, " +
				"'hook-approver'::text, 'admin'::text]))"},
			{"chk_users_created_at", "CHECK (created_at > 0)"},
			{"chk_users_rev", "CHECK (rev >= 1)"},
		},
	},
```

- [x] **Step 6: 跑测试**

Run: `go test ./internal/store/... -v`
Expected: PASS。`Test表结构` 认得出新表；`Test契约用户` 全绿。

- [x] **Step 7: 提交**

```bash
git add internal/store
git commit -m "feat(store): 加 Users 聚合与 users 表，用户与角色落库、会话不进库（第 5 层）"
```


---

## Task 4：服务骨架、路由表、权限表与 OpenAPI

**Files:**
- Create: `internal/console/server.go`
- Create: `internal/console/routes.go`
- Create: `internal/console/perm.go`
- Create: `internal/console/openapi.go`
- Create: `internal/console/isosource.go`
- Create: `internal/console/taskcontrol.go`
- Create: `internal/console/notify.go`
- Create: `internal/console/sessions.go`
- Test: `internal/console/perm_test.go`、`internal/console/openapi_test.go`、`internal/console/server_test.go`、`internal/console/harness_test.go`、`internal/console/export_test.go`

**Interfaces:**
- Consumes: Task 1 的 `Ok`/`Fail`/`RespondError`/`Judge`/`Bind`/`Get`/`Write` 与各状态码；Task 3 的 `store.Users`、`store.Role`、`store.Roles`
- Produces:
  - `console.Clock`（`func() int64`）
  - `console.Deps{Hosts, ISOs, Hooks, Templates, Tasks, Events, Audit, Users store.*; ISOSource; TaskCtl; Notifier; Clock; SessionTTL int64}`
  - `console.New(d Deps) *Server`，`Server.Engine *gin.Engine`、`(*Server).Routes() []RouteInfo`、`(*Server).OpenAPI() map[string]any`
  - `(*server).auditFrom(c *gin.Context, detail string) store.Audit`、`(*server).clock() int64`
  - `console.RouteInfo{Method, Path, Perm, Audit, Tag string}`
  - `console.route` 类型与 `(*server).allRoutes() []route`（每个资源任务往里接一行）
  - `console.PermHostRead`…`console.PermAuditRead` 十六个权限码、`console.AllPerms()`、`console.PermsOf(r store.Role) []string`
  - `console.ISOSource`、`console.SourceKind`（`SourceURL`/`SourceUpload`/`SourcePath`）
  - `console.TaskControl`
  - `console.Problems(ps []model.Problem) error`（把一组问题包成 error，给 `TaskControl` 的实现用）
  - `console.Notifier`、`console.NewHub() Notifier`
  - `console.userOf(c) *store.User`

- [ ] **Step 1: 写失败的测试——权限表**

Create `internal/console/perm_test.go`：

```go
package console_test

import (
	"slices"
	"testing"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
)

// 角色表照第 5 层设计 §4.4：viewer 只读；operator 加主机写入与任务三个动作；hook-author 加
// hook 与模板写入；hook-approver 加两个审批；admin 拿全部。
func Test每个角色的权限码(t *testing.T) {
	want := map[store.Role][]string{
		store.RoleViewer: {console.PermHostRead, console.PermISORead, console.PermHookRead,
			console.PermTemplateRead, console.PermTaskRead},
		store.RoleOperator: {console.PermHostRead, console.PermISORead, console.PermHookRead,
			console.PermTemplateRead, console.PermTaskRead,
			console.PermHostWrite, console.PermTaskCreate, console.PermTaskCancel, console.PermTaskRetry},
		store.RoleHookAuthor: {console.PermHostRead, console.PermISORead, console.PermHookRead,
			console.PermTemplateRead, console.PermTaskRead, console.PermHookWrite, console.PermTemplateWrite},
		store.RoleHookApprover: {console.PermHostRead, console.PermISORead, console.PermHookRead,
			console.PermTemplateRead, console.PermTaskRead, console.PermHookApprove, console.PermTemplateApprove},
	}
	for role, codes := range want {
		got := console.PermsOf(role)
		if !slices.Equal(got, codes) {
			t.Errorf("角色 %s 的权限码是 %v，应为 %v", role, got, codes)
		}
	}
}

// admin 拿全部权限码，不逐条列：加码时忘了给 admin，这一条报出来。
func TestAdmin拿全部权限码(t *testing.T) {
	got := console.PermsOf(store.RoleAdmin)
	if !slices.Equal(got, console.AllPerms()) {
		t.Errorf("admin 的权限码是 %v，应为全部 %v", got, console.AllPerms())
	}
}

// 未知角色一个都没有：宁可全部拒绝，也不要因为角色写错而多放行。
func Test未知角色没有权限(t *testing.T) {
	for _, bad := range []store.Role{"", "root", "Admin", "viewer "} {
		if got := console.PermsOf(bad); len(got) != 0 {
			t.Errorf("角色 %q 的权限码是 %v，应一个都没有", bad, got)
		}
	}
}

// 每条路由的权限码都登记过。反方向（每个码都有路由用到）在 Task 13 补：那时全部资源都落地了。
// PermAnyUser 不是权限码，跳过。
func Test路由的权限码都登记过(t *testing.T) {
	s := newTestServer(t)
	for _, r := range s.Routes() {
		switch r.Perm {
		case "":
			if r.Path != "/console/v1/auth/login" {
				t.Errorf("%s %s 没有权限码，免登录的只该有登录本身", r.Method, r.Path)
			}
			continue
		case console.PermAnyUser:
			continue
		}
		if !slices.Contains(console.AllPerms(), r.Perm) {
			t.Errorf("%s %s 用了没登记的权限码 %q", r.Method, r.Path, r.Perm)
		}
	}
}

// PermsOf 返回副本，界面改了不影响别人。
func TestPermsOf返回副本(t *testing.T) {
	got := console.PermsOf(store.RoleViewer)
	got[0] = "篡改"
	if console.PermsOf(store.RoleViewer)[0] == "篡改" {
		t.Error("PermsOf 返回了内部的切片")
	}
}
```

Run: `go test ./internal/console/ -run 'Test每个角色|TestAdmin|Test未知角色|Test路由的权限码|TestPermsOf' -v`
Expected: FAIL，`undefined: console.PermsOf`、`undefined: console.AllPerms`、`undefined: newTestServer`。

- [ ] **Step 2: 写失败的测试——OpenAPI**

Create `internal/console/openapi_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// 每条注册的路由都在文档里，都有摘要与分组（总 spec §6.2 第 10 条）。
func Test每条路由都有OpenAPI描述(t *testing.T) {
	s := newTestServer(t)
	paths, ok := s.OpenAPI()["paths"].(map[string]any)
	if !ok {
		t.Fatalf("文档里没有 paths：%v", s.OpenAPI())
	}
	for _, r := range s.Routes() {
		item, ok := paths[r.Path].(map[string]any)
		if !ok {
			t.Errorf("%s %s 不在 OpenAPI 文档里", r.Method, r.Path)
			continue
		}
		op, ok := item[strings.ToLower(r.Method)].(map[string]any)
		if !ok {
			t.Errorf("%s %s 的方法不在文档里", r.Method, r.Path)
			continue
		}
		if summary, _ := op["summary"].(string); summary == "" {
			t.Errorf("%s %s 没有摘要", r.Method, r.Path)
		}
		if tags, _ := op["tags"].([]string); len(tags) == 0 || tags[0] == "" {
			t.Errorf("%s %s 没有分组", r.Method, r.Path)
		}
	}
}

// 一条不多：gin 注册的路由都在文档里。文档比路由表多一条，说明有路由忘了登记。
func TestOpenAPI一条不多(t *testing.T) {
	s := newTestServer(t)
	paths, _ := s.OpenAPI()["paths"].(map[string]any)
	for _, ri := range s.Engine.Routes() {
		if _, ok := paths[ri.Path]; !ok {
			t.Errorf("gin 注册了 %s，OpenAPI 文档里没有", ri.Path)
		}
	}
}

// 引用的 schema 名字都解析得过：req 与 resp 传的是结构体零值，名字取自 Go 的类型名。
func TestOpenAPI引用的schema都有名字(t *testing.T) {
	s := newTestServer(t)
	raw, err := json.Marshal(s.OpenAPI())
	if err != nil {
		t.Fatalf("文档编码失败：%v", err)
	}
	for _, ref := range refsIn(string(raw)) {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if name == ref || name == "" {
			t.Errorf("引用 %q 不是 components/schemas 下的名字", ref)
			continue
		}
		if name != firstUpper(name) {
			t.Errorf("schema 名 %q 应首字母大写（Go 的导出类型名）", name)
		}
	}
}

// 同样的路由表编码两次逐字节相同：文档是确定性的，第 6 层生成类型才不会无故 diff。
func TestOpenAPI编码两次相同(t *testing.T) {
	s := newTestServer(t)
	a, err := json.Marshal(s.OpenAPI())
	if err != nil {
		t.Fatalf("第一次编码失败：%v", err)
	}
	b, err := json.Marshal(s.OpenAPI())
	if err != nil {
		t.Fatalf("第二次编码失败：%v", err)
	}
	if string(a) != string(b) {
		t.Error("OpenAPI 文档编码两次不相同")
	}
}

// refsIn 取出 JSON 里全部 $ref 的值。
func refsIn(s string) []string {
	var out []string
	for {
		i := strings.Index(s, `"$ref":"`)
		if i < 0 {
			return out
		}
		s = s[i+len(`"$ref":"`):]
		j := strings.Index(s, `"`)
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j:]
	}
}

func firstUpper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
```

Run: `go test ./internal/console/ -run 'Test每条路由|TestOpenAPI' -v`
Expected: FAIL，`undefined: newTestServer`。

- [ ] **Step 3: 写失败的测试——中间件与夹具**

Create `internal/console/server_test.go`：

```go
package console_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// stubISOSource 与 stubTaskControl 是 Task 7、Task 12 之前的占位：本文件的用例不打这两个
// 端点，只为把 Deps 填满。Task 7 起换成一个记调用的 mock。
type stubISOSource struct{}

func (stubISOSource) Register(ctx context.Context, iso model.ISO, kind, addr, role string, size int64, sha256 string, a store.Audit) error {
	return nil
}
func (stubISOSource) Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a store.Audit) error {
	return nil
}
func (stubISOSource) Cancel(ctx context.Context, id string, a store.Audit) error   { return nil }
func (stubISOSource) Retry(ctx context.Context, id string, a store.Audit) error    { return nil }
func (stubISOSource) Reimport(ctx context.Context, id string, a store.Audit) error { return nil }
func (stubISOSource) FreeSpace(ctx context.Context) (int64, int64, error) {
	return 0, 0, nil
}

type stubTaskControl struct{}

func (stubTaskControl) Cancel(ctx context.Context, t model.Task, a store.Audit) error { return nil }
func (stubTaskControl) Retry(ctx context.Context, t model.Task, a store.Audit) error  { return nil }
func (stubTaskControl) CanCancel(t model.Task) []model.Problem { return nil }
func (stubTaskControl) CanRetry(t model.Task) []model.Problem  { return nil }

// 要权限的路由不带令牌一律 401。本任务只有登录一条路由，这条用例从 Task 5 起才有意义。
func Test没有令牌一律401(t *testing.T) {
	s := newTestServer(t)
	for _, r := range s.Routes() {
		if r.Perm == "" {
			continue
		}
		req := httptest.NewRequest(r.Method, samplePath(r.Path), nil)
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("%s %s 没有令牌返回 %d，应为 401", r.Method, r.Path, w.Code)
		}
	}
}

// panic 兜底：处理函数慌了也回 500 的信封，不是半截响应（总 spec §6.3）。
func TestPanic兜底回500(t *testing.T) {
	s := newTestServer(t)
	s.Engine.GET("/console/v1/_panic", func(c *gin.Context) { panic("boom") })
	req := httptest.NewRequest("GET", "/console/v1/_panic", nil)
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	if w.Code != 500 {
		t.Errorf("panic 之后返回 %d，应为 500", w.Code)
	}
	var got struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("panic 的响应不是信封：%v\n%s", err, w.Body.String())
	}
	if got.Code != 500 || got.Message == "" {
		t.Errorf("panic 的信封是 %+v，应为 500 加一句说明", got)
	}
}

// 没注册的路径是 gin 自己的 404，不带信封：它不在路由表里，控制台不包装它。
func Test没注册的路径是gin的404(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, httptest.NewRequest("GET", "/console/v1/nope", nil))
	if w.Code != 404 {
		t.Errorf("没注册的路径返回 %d，应为 404", w.Code)
	}
}
```

`server_test.go` 的 import 加 `"context"` 与 `"io"`（stub 的方法签名要用）。

Create `internal/console/harness_test.go`（后续每个资源任务的测试都从这里取夹具）：

```go
package console_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
)

// fixedAt 是测试用的固定时钟：会话到期、审计时间都按它算，不依赖真实时间。
const fixedAt = 1750000000

// newTestServer 建一个控制台。仓储留空：本任务的用例只打免登录与要权限的路由，不碰库。
// 连库与 mock 的夹具从 Task 5 起加在这里。
func newTestServer(t *testing.T) *console.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return console.New(console.Deps{
		Clock:      func() int64 { return fixedAt },
		SessionTTL: 12 * 3600,
		ISOSource:  stubISOSource{},
		TaskCtl:    stubTaskControl{},
		Notifier:   console.NewHub(),
	})
}

// samplePath 把路由模板里的 :param 换成打得出去的样例值。
func samplePath(path string) string {
	return strings.NewReplacer(
		"{sn}", "SN-1", "{id}", "iso-1", "{username}", "alice",
		"{version}", "1", "{taskID}", "task-1",
	).Replace(path)
}

// do 打一个请求，返回状态码与响应体。
func do(t *testing.T, s *console.Server, method, path, body string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// login 建一个会话并返回令牌。本任务还没有登录端点，Task 5 之后换成打登录接口。
func login(t *testing.T, s *console.Server, u *store.User) string {
	t.Helper()
	return s.TestLogin(u)
}
```

Create `internal/console/export_test.go`：

```go
package console

import "github.com/lober/lober/internal/store"

// TestLogin 是测试后门：直接建一个会话并返回令牌，不走口令校验。生产的入口只有 New。
func (s *Server) TestLogin(u *store.User) string { return s.server.sessions.put(u) }
```

Run: `go test ./internal/console/ -run 'Test没有令牌|TestPanic|Test没注册' -v`
Expected: FAIL，`undefined: console.New`、`undefined: console.Server`、`undefined: console.NewHub`、`undefined: (*console.Server).TestLogin`。

- [ ] **Step 4: 实现权限表**

Create `internal/console/perm.go`：

```go
package console

import (
	"slices"

	"github.com/lober/lober/internal/store"
)

// 权限码。每个码至少被一条路由用到（Task 13 的 Test每个权限码都有路由用到），加码时同时加进
// 角色表与 AllPerms。
const (
	PermHostRead       = "host:read"
	PermHostWrite      = "host:write"
	PermISORead        = "iso:read"
	PermISOWrite       = "iso:write"
	PermHookRead       = "hook:read"
	PermHookWrite      = "hook:write"
	PermHookApprove    = "hook:approve"
	PermTemplateRead   = "template:read"
	PermTemplateWrite  = "template:write"
	PermTemplateApprove = "template:approve"
	PermTaskRead       = "task:read"
	PermTaskCreate     = "task:create"
	PermTaskCancel     = "task:cancel"
	PermTaskRetry      = "task:retry"
	PermUserManage     = "user:manage"
	PermAuditRead      = "audit:read"
)

// allPerms 是全部权限码，顺序即界面列权限矩阵的顺序。
var allPerms = []string{
	PermHostRead, PermHostWrite, PermISORead, PermISOWrite,
	PermHookRead, PermHookWrite, PermHookApprove,
	PermTemplateRead, PermTemplateWrite, PermTemplateApprove,
	PermTaskRead, PermTaskCreate, PermTaskCancel, PermTaskRetry,
	PermUserManage, PermAuditRead,
}

// AllPerms 是全部权限码，每次返回新切片。
func AllPerms() []string { return slices.Clone(allPerms) }

// rolePerms 是每个角色的权限码（第 5 层设计 §4.4 的表）。admin 不列，它拿 allPerms。
var rolePerms = map[store.Role][]string{
	store.RoleViewer: {PermHostRead, PermISORead, PermHookRead, PermTemplateRead, PermTaskRead},
	store.RoleOperator: {PermHostRead, PermISORead, PermHookRead, PermTemplateRead, PermTaskRead,
		PermHostWrite, PermTaskCreate, PermTaskCancel, PermTaskRetry},
	store.RoleHookAuthor: {PermHostRead, PermISORead, PermHookRead, PermTemplateRead, PermTaskRead,
		PermHookWrite, PermTemplateWrite},
	store.RoleHookApprover: {PermHostRead, PermISORead, PermHookRead, PermTemplateRead, PermTaskRead,
		PermHookApprove, PermTemplateApprove},
}

// PermsOf 返回角色 r 的全部权限码，按 allPerms 的顺序。admin 拿 allPerms；未知角色一个都没有
// ——宁可全部拒绝，也不要因为角色写错而多放行。
func PermsOf(r store.Role) []string {
	if r == store.RoleAdmin {
		return slices.Clone(allPerms)
	}
	ps, ok := rolePerms[r]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(ps))
	for _, p := range allPerms {
		if slices.Contains(ps, p) {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 5: 实现服务骨架与路由表**

Create `internal/console/server.go`：

```go
package console

import (
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/store"
)

// Clock 取当前的 Unix 秒。装配点注入 time.Now，测试注入固定值：会话到期、审计时间都经它。
type Clock func() int64

// Deps 是控制台要的全部能力，由装配点用 wire 组合，测试逐项替身。仓储按聚合分开给，不整包
// 收 store.Store：调用方只声明自己用得到的那几个（总 spec §6.1），测试也就能只实现要用的几组。
type Deps struct {
	Hosts     store.Hosts
	ISOs      store.ISOs
	Hooks     store.Hooks
	Templates store.Templates
	Tasks     store.Tasks
	Events    store.Events
	Audit     store.AuditLog
	Users     store.Users
	// ISOSource 与 TaskCtl 是第 4、7 层的接缝，由控制台声明、装配点注入（见各自文件）。
	ISOSource ISOSource
	TaskCtl   TaskControl
	// Notifier 是进程内的通知中心，第 7 层的状态机拿同一个。
	Notifier Notifier
	Clock    Clock
	// SessionTTL 是会话的有效期，秒。
	SessionTTL int64
}

// Server 是一个控制台实例：引擎加路由表。New 返回它，调用方用 Engine 起服务，测试用
// Routes 与 OpenAPI 对账。
type Server struct {
	Engine *gin.Engine
	routes []RouteInfo
	server *server
}

// server 是内部状态，Server 只是它的门面。
type server struct {
	deps     Deps
	sessions *sessions
	hub      Notifier
	index    map[string]route // 「方法 + 路径」到路由表条目
}

// New 建一个控制台。返回的 Server 已经挂好全部路由与中间件，调用方只管 ListenAndServe。
func New(d Deps) *Server {
	gin.SetMode(gin.ReleaseMode)
	s := &server{deps: d, hub: d.Notifier}
	s.sessions = newSessions(d.SessionTTL, s.clock)
	out := &Server{server: s}
	r := gin.New()
	r.Use(recoverPanic(), requestLog(s), s.authenticate())
	out.Engine = r
	s.register(r, out)
	return out
}

// ctxUser 是当前登录的用户名，认证中间件放进去，authorize 与 auditFrom 从这儿取。
const ctxUser = "console.username"

// PermAnyUser 是「已登录即可」，不是角色权限码：用户信息与退出登录只问你是谁，不问你能做
// 什么。authorize 见到它就放行，权限码表里没有它，权限矩阵的用例跳过它。
const PermAnyUser = "@any"

// recoverPanic 是 panic 兜底：处理函数慌了也回 500 的信封，不是半截响应（总 spec §6.3）。
func recoverPanic() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if p := recover(); p != nil {
				slog.Error("控制台处理函数 panic", "path", c.FullPath(), "panic", p,
					"stack", string(debug.Stack()))
				Fail(c, statusInternal, nil, "服务端内部错误")
			}
		}()
		c.Next()
	}
}

// requestLog 记每个请求：方法、路径、状态码、耗时。它是控制台自己读时钟的少数几处之一——
// 日志不算业务时间，装配点不为它注入时钟。
func requestLog(s *server) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("请求", "method", c.Request.Method, "path", c.FullPath(),
			"status", c.Writer.Status(), "took", time.Since(start))
	}
}

// authenticate 按路由表决定要不要认证：perm 为空的路由（只有登录本身）免登录，其余要一个
// 没过期的会话。令牌来自 Authorization 头，SSE 那条路另收 access_token 查询参数
// （EventSource 发不了头）。本任务只认会话里有这个名字，Task 5 再补查仓储与禁用。
func (s *server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		r, ok := s.meta(c)
		if ok && r.perm == "" {
			c.Next()
			return
		}
		name, ok := s.sessions.lookup(bearerToken(c))
		if !ok {
			Fail(c, statusUnauthorized, nil, "没有登录，或者会话已经过期")
			return
		}
		c.Set(ctxUser, name)
		c.Next()
	}
}

// authorize 查权限码，逐条路由挂（见 register）。本任务只认上下文里有没有用户名，Task 5 补角色。
func (s *server) authorize(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if perm == PermAnyUser {
			c.Next() // 已登录即可：用户信息与退出登录只问你是谁，不问你能做什么
			return
		}
		if userOf(c) == nil {
			Fail(c, statusForbidden, nil, "没有做这件事的权限")
			return
		}
		c.Next()
	}
}

// bearerToken 从请求里取令牌。
func bearerToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return ""
		}
		return strings.TrimSpace(tok)
	}
	return c.Query("access_token")
}

// userOf 取当前登录的用户名。认证中间件之后一定有，免登录的路由没有。
func userOf(c *gin.Context) string {
	name, _ := c.Get(ctxUser)
	s, _ := name.(string)
	return s
}

// meta 按「方法 + 完整路径」查路由表里登记的那一条。gin 在跑中间件之前已经匹配好路由，
// c.FullPath() 就是登记时的路径模板（带 :param 的那种），所以查得回来。
func (s *server) meta(c *gin.Context) (route, bool) {
	r, ok := s.index[c.Request.Method+" "+c.FullPath()]
	return r, ok
}

// auditFrom 按路由表声明的动作和当前用户构造一条审计。detail 由处理函数给，比如驳回审批的
// 理由。路由没声明动作时返回零值：仓储收到零值审计会报 ErrAuditRequired，500，测试立刻红
// （总 spec §6.3「漏了声明，测试里会直接报错」）。
func (s *server) auditFrom(c *gin.Context, detail string) store.Audit {
	r, ok := s.meta(c)
	if !ok || r.audit == "" {
		return store.Audit{}
	}
	return store.Audit{Actor: userOf(c), Action: r.audit, Detail: detail, At: s.clock()}
}

// clock 是当前时间，测试注入固定值。
func (s *server) clock() int64 {
	if s.deps.Clock == nil {
		return time.Now().Unix()
	}
	return s.deps.Clock()
}
```

Create `internal/console/routes.go`：

```go
package console

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// route 是路由表的一条。perm 为空表示免登录（只有登录本身）；audit 为空表示不审计；
// summary 是 OpenAPI 的必填项，漏一条由 Test每条路由都有OpenAPI描述 报出来。
type route struct {
	method, path string
	perm         string
	audit        string
	tag          string
	summary      string
	req, resp    any
	handle       gin.HandlerFunc
}

// RouteInfo 是路由表对外可见的部分，给测试列权限矩阵、对账 OpenAPI 用。
type RouteInfo struct {
	Method, Path, Perm, Audit, Tag string
}

// Routes 返回登记的全部路由，按路径再按方法排序，测试因此稳定。
func (s *Server) Routes() []RouteInfo { return slices.Clone(s.routes) }

// allRoutes 是全部路由。新加一个资源就在它的文件里写一个 xxxRoutes，再接在这里一行。
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),
	)
}

// register 把路由表注册到 gin：每条要权限的路由额外挂上 authorize。路由表的顺序就是注册的
// 顺序，gin 不因为它换行为，列出来只为读得清。
func (s *server) register(r *gin.Engine, out *Server) {
	rs := s.allRoutes()
	s.index = make(map[string]route, len(rs))
	out.routes = make([]RouteInfo, 0, len(rs))
	for _, rt := range rs {
		s.index[rt.method+" "+rt.path] = rt
		out.routes = append(out.routes, RouteInfo{
			Method: rt.method, Path: rt.path, Perm: rt.perm, Audit: rt.audit, Tag: rt.tag})
		handlers := []gin.HandlerFunc{rt.handle}
		if rt.perm != "" {
			handlers = append([]gin.HandlerFunc{s.authorize(rt.perm)}, rt.handle)
		}
		r.Handle(rt.method, rt.path, handlers...)
	}
	slices.SortFunc(out.routes, func(a, b RouteInfo) int {
		return strings.Compare(a.Path+" "+a.Method, b.Path+" "+b.Method)
	})
}
```

Create `internal/console/openapi.go`：

```go
package console

import (
	"reflect"
	"slices"
	"strings"
)

// OpenAPI 是 GET /openapi.json 给出的文档，由路由表生成，不手写。schema 只登记引用的名字，
// 详细的字段定义留给第 6 层生成 TS 类型时补；本层保证的是「一条不漏、名字解析得过、编码两次
// 相同」三条，由 openapi_test 钉住（总 spec §6.2 第 10 条）。
func (s *Server) OpenAPI() map[string]any {
	rs := s.server.allRoutes()
	slices.SortFunc(rs, func(a, b route) int { return strings.Compare(a.path, b.path) })
	paths := map[string]any{}
	for _, r := range rs {
		item, _ := paths[r.path].(map[string]any)
		if item == nil {
			item = map[string]any{}
		}
		body := map[string]any{"description": "成功"}
		if r.resp != nil {
			body["content"] = map[string]any{"application/json": map[string]any{
				"schema": map[string]any{"$ref": refOf(r.resp)}}}
		}
		op := map[string]any{
			"summary": r.summary,
			"tags":     []string{r.tag},
			"responses": map[string]any{
				"200": body,
				"401": map[string]any{"description": "没有登录，或者会话已经过期"},
				"403": map[string]any{"description": "没有做这件事的权限"},
			},
		}
		if r.req != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{"$ref": refOf(r.req)}}}}
		}
		item[strings.ToLower(r.method)] = op
		paths[r.path] = item
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "lober 控制台", "version": "1.0.0"},
		"paths":   paths,
	}
}

// refOf 是 schema 的引用名，取自 Go 类型的名字：req 与 resp 传结构体的零值，比如 hostRequest{}。
func refOf(v any) string {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return "#/components/schemas/" + t.Name()
}
```

- [ ] **Step 6: 两个接缝、通知中心与会话桩**

Create `internal/console/isosource.go`：

```go
package console

import (
	"context"
	"io"

	"github.com/lober/lober/internal/core/model"
)

// ISOSource 是控制台对第 4 层（ISO 来源）的全部需要，由控制台声明、ISO 模块实现、装配点注入
// （总 spec §6.2 第 6 条、§6.1「实现模块之间互不引用」）。
//
// 参数只用 model.ISO 与原始类型，不用 Source 结构体：这样 internal/iso 不必 import console
// 就能满足这个接口，cmd/lober 里一行 var _ ISOSource = (*iso.Sources)(nil) 静态兜住。
// 第 4 层若坚持用 store.Source，在 cmd/lober 加一个几行的适配器，不要让 console 去 import
// internal/iso（第 5 层设计 §6.1）。
//
// 进度只读：控制台不自己写进度，字节进度由第 4 层经存储契约写，粗状态在 ISO 记录上
// （第 4 层设计 §12）。
//
// 四个写方法带 store.Audit（2026-10-12 定，第 5 层设计 §6.1）：总 spec §6.2 第 8 条要求
// 「登记来源」要审计，没有载体就写不进去。Register 与 Upload 另带 role：一个 ISO 可以有多条
// 来源——主介质加该系要求的附加来源，目前只有 Debian 要同版本的官方 netboot 包（netinst ISO
// 里的 initrd 是光驱版，不能网络装机）。role 为空串是主介质。Cancel、Retry、Reimport 按 ISO ID
// 寻址，作用于它的全部来源，不带 role。
type ISOSource interface {
	// Register 登记一个来源并开始获取。kind 是 url、upload 或 path；addr 是链接或服务器本地
	// 路径；role 是来源的角色，空串是主介质，附加来源传 catalog 给的附加来源名；size 是浏览器
	// 上传的总字节数，0 表示未知；sha256 是期望的摘要，可空（C3）。
	Register(ctx context.Context, iso model.ISO, kind, addr, role string, size int64, sha256 string, a store.Audit) error
	// Upload 接收浏览器上传的一段字节，offset 是它在文件里的起始字节。分片大小由客户端定，
	// 第 4 层按字节区间记，不收尾：字节齐不齐由它自己判。
	Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a store.Audit) error
	// Cancel 取消获取。取消不把 ISO 置为 failed：ISOFailed 的语义是导入失败、可以直接重导
	// （第 4 层设计 §4.1）。
	Cancel(ctx context.Context, id string, a store.Audit) error
	// Retry 失败后重试获取。
	Retry(ctx context.Context, id string, a store.Audit) error
	// Reimport 字节已到齐，重新导入。
	Reimport(ctx context.Context, id string, a store.Audit) error
	// FreeSpace 报告暂存区与制品目录的余量。建 ISO 前调它（总 spec §5.1 第 1 步、第 4 层设计
	// §4.5），不够就拒绝并说明还差多少。
	FreeSpace(ctx context.Context) (staging, artifacts int64, err error)
}

// SourceKind 是来源的三种：远程链接、浏览器上传、服务器本地路径（总 spec §4.4）。
type SourceKind string

const (
	SourceURL    SourceKind = "url"
	SourceUpload SourceKind = "upload"
	SourcePath   SourceKind = "path"
)

// Valid 报告 k 是不是三种来源之一。
func (k SourceKind) Valid() bool {
	switch k {
	case SourceURL, SourceUpload, SourcePath:
		return true
	}
	return false
}
```

Create `internal/console/taskcontrol.go`：

```go
package console

import (
	"context"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// TaskControl 是控制台对任务状态发起变更需要的能力，由控制台声明、第 7 层的状态机实现、
// 装配点注入。取消与重试经过状态机，C34 只在状态机里实现一次（总 spec §6.2 第 4 条）。
//
// 未就绪时装配点注入的占位实现一律返回「第 7 层未实现」，不写临时规则：model.Task 现在没有
// 「失败前有没有进过重启中」这个字段，第 7 层才会加时机进度，此刻写一个「failed 就能重试」
// 的版本会直接违反 C34（第 5 层设计 §6.2）。
type TaskControl interface {
	// Cancel 取消一个任务。不允许时返回 model.Problem 或仓储的错误。
	Cancel(ctx context.Context, t model.Task, a store.Audit) error
	// Retry 重试一个失败的任务，沿用原来的快照（C34）。不允许时同上。
	Retry(ctx context.Context, t model.Task, a store.Audit) error
	// CanCancel 与 CanRetry 报告这个任务能不能取消、能不能重试，给界面置灰用。
	CanCancel(t model.Task) []model.Problem
	CanRetry(t model.Task) []model.Problem
}
```

Create `internal/console/notify.go`：

```go
package console

import "sync"

// Notifier 是进程内的通知中心：写操作提交之后发一条「任务 X 有更新」，不带数据（总 spec
// §6.2 第 9 条）。控制台的推送连接收到后按事件序号从仓储补读，通知丢了也不会出错，下一次
// 补读会补齐。
type Notifier interface {
	// Publish 报告这个任务有更新。
	Publish(taskID string)
	// Subscribe 订阅更新，返回一个 channel 与退订函数。channel 缓冲 1：发布者不因为某个
	// 连接读得慢而阻塞，丢的那条由补读兜底。
	Subscribe(taskID string) (<-chan struct{}, func())
}

// hub 是 Notifier 的内存实现。单进程够用：装配点只有一个，第 7 层的状态机拿同一个。
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

// NewHub 建一个空的通知中心。
func NewHub() Notifier { return &hub{subs: map[string]map[chan struct{}]struct{}{}} }

func (h *hub) Publish(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[taskID] {
		select {
		case ch <- struct{}{}:
		default: // 缓冲满了：上一条还没读，丢就丢了，补读会补齐
		}
	}
}

func (h *hub) Subscribe(taskID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[taskID] == nil {
		h.subs[taskID] = map[chan struct{}]struct{}{}
	}
	h.subs[taskID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[taskID], ch)
		if len(h.subs[taskID]) == 0 {
			delete(h.subs, taskID)
		}
	}
}
```

Create `internal/console/sessions.go`（Task 5 补登录，这里只放查得到的桩）：

```go
package console

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// sessions 是内存会话表（第 5 层设计 §5）：token → 用户名，带 TTL。单进程、重启即重新登录，
// 不放库——会话不是业务数据，放进库里要额外处理过期清理，而 AutoMigrate 表达不了。
//
// 只记用户名，不存用户快照：角色改了、账号被禁用了，下一次请求就看得出来，不用等这一次会话
// 过期（第 5 层设计 §5「禁用后会话立即失效」）。用户名到用户由 authenticate 查仓储。
type sessions struct {
	ttl     int64   // 秒
	now     func() int64
	mu      sync.Mutex
	byToken map[string]string
	expiry  map[string]int64
}

func newSessions(ttl int64, now func() int64) *sessions {
	return &sessions{ttl: ttl, now: now, byToken: map[string]string{}, expiry: map[string]int64{}}
}

// lookup 查一个令牌，过期就算没有，返回用户名。
func (s *sessions) lookup(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name, ok := s.byToken[token]
	if !ok {
		return "", false
	}
	if s.expiry[token] <= s.now() {
		delete(s.byToken, token)
		delete(s.expiry, token)
		return "", false
	}
	return name, true
}

// put 给 username 建一个会话并返回令牌。时钟由调用方注入，测试因此能拨快时间。
func (s *sessions) put(username string) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand 失败时建不出可信的令牌，不能静默降级
	}
	tok := hex.EncodeToString(buf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[tok] = username
	s.expiry[tok] = s.now() + s.ttl
	return tok
}

// drop 让一个令牌作废，退出登录用。
func (s *sessions) drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, token)
	delete(s.expiry, token)
}
```

Create `internal/console/export_test.go`：

```go
package console

import "github.com/lober/lober/internal/store"

// TestLogin 是测试后门：直接建一个会话并返回令牌，不走口令校验。生产的入口只有 New。
func (s *Server) TestLogin(u *store.User) string { return s.server.sessions.put(u.Username) }
```

`server.go` 里 `New` 的构造改成两步，让 `newSessions` 拿到注入的时钟：

```go
	s := &server{deps: d, hub: d.Notifier}
	s.sessions = newSessions(d.SessionTTL, s.clock)
```

`authenticate` 在本任务只认会话里有这个名字，Task 5 再补查仓储：

```go
		name, ok := s.sessions.lookup(bearerToken(c))
		if !ok {
			Fail(c, statusUnauthorized, nil, "没有登录，或者会话已经过期")
			return
		}
		c.Set(ctxUser, name)
		c.Next()
```

`ctxUser` 的注释改成「当前登录的用户名」。`userOf` 与 `authorize` 在本任务因此拿不到角色，`authorize` 改成：用户名不在上下文里就 403（Task 5 再补角色）。

- [ ] **Step 7: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: `Test每个角色的权限码`、`TestAdmin拿全部权限码`、`Test未知角色没有权限`、`TestPermsOf返回副本`、`Test路由的权限码都登记过`、OpenAPI 四条、`TestPanic兜底回500`、`Test没注册的路径是gin的404` PASS；`Test没有令牌一律401` 空转通过（还没有要权限的路由）。

- [ ] **Step 8: 提交**

```bash
git add internal/console
git commit -m "feat(console): 服务骨架、路由表、权限表、OpenAPI 生成与两个接缝接口"
```

---

---

## Task 5：认证、会话与用户管理

**Files:**
- Create: `internal/console/auth.go`
- Create: `internal/console/users.go`
- Modify: `internal/console/server.go`（`authenticate` 补查仓储、`authorize` 补角色）
- Modify: `internal/console/routes.go`（`allRoutes` 接两行）
- Test: `internal/console/auth_test.go`、`internal/console/users_test.go`、`internal/console/store_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Deps`/`New`/`Routes`/`auditFrom`/`clock`/`PermAnyUser`/`userOf`；Task 3 的 `store.Users`、`store.User`、`store.Role`
- Produces:
  - `(*server).login`、`(*server).logout`、`(*server).userInfo`、`(*server).listUsers`、`(*server).createUser`、`(*server).updateUser`、`(*server).disableUser`
  - `console.hashPassword(pw string) (string, error)`、`console.checkPassword(pw, hashed string) bool`
  - `userResponse`、`sessionResponse`、`userRequest`、`userPage`（OpenAPI 的 schema 名）
  - 测试夹具 `newStoreServer(t) (*console.Server, store.Store)`、`seedUser(t, st, name, role) *store.User`

- [ ] **Step 1: 写失败的测试——口令散列**

Create `internal/console/auth_test.go`：

```go
package console_test

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/console"
)

// 散列的格式是 pbkdf2-sha256$<迭代>$<盐>$<散列>，盐与散列都是 base64.RawURLEncoding。
func Test口令散列的格式(t *testing.T) {
	got, err := console.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword：%v", err)
	}
	parts := strings.Split(got, "$")
	if len(parts) != 4 {
		t.Fatalf("散列是 %q，应分四段", got)
	}
	if parts[0] != "pbkdf2-sha256" {
		t.Errorf("算法是 %q，应为 pbkdf2-sha256", parts[0])
	}
	if parts[1] != "600000" {
		t.Errorf("迭代次数是 %q，应为 600000", parts[1])
	}
	if len(parts[2]) == 0 || len(parts[3]) == 0 {
		t.Errorf("盐或散列是空的：%q", got)
	}
}

// 每次用新的随机盐，同一个口令两次算出来不一样。
func Test同一个口令两次散列不同(t *testing.T) {
	a, err := console.HashPassword("pw")
	if err != nil {
		t.Fatalf("第一次：%v", err)
	}
	b, err := console.HashPassword("pw")
	if err != nil {
		t.Fatalf("第二次：%v", err)
	}
	if a == b {
		t.Error("两次散列相同，盐没有随机化")
	}
}

func Test比对口令(t *testing.T) {
	hashed, err := console.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("HashPassword：%v", err)
	}
	if !console.CheckPassword("correct horse", hashed) {
		t.Error("对的口令没比上")
	}
	for _, bad := range []string{"", "correct hors", "CORRECT HORSE", "correct horse "} {
		if console.CheckPassword(bad, hashed) {
			t.Errorf("错的口令 %q 比上了", bad)
		}
	}
}

// 散列被改坏时返回 false，不 panic：库里可能是旧格式，或者有人手工改过。
func Test比对一个改坏的散列(t *testing.T) {
	hashed, err := console.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword：%v", err)
	}
	for _, bad := range []string{
		"", "pbkdf2-sha256", "pbkdf2-sha256$600000$c2FsdA", "md5$600000$c2FsdA$aGFzaA",
		"pbkdf2-sha256$abc$c2FsdA$aGFzaA", "pbkdf2-sha256$600000$!!!$aGFzaA",
		"pbkdf2-sha256$600000$c2FsdA$!!!", "pbkdf2-sha256$0$c2FsdA$aGFzaA",
		"pbkdf2-sha256$-1$c2FsdA$aGFzaA",
	} {
		if console.CheckPassword("pw", bad) {
			t.Errorf("改坏的散列 %q 比上了", bad)
		}
	}
}
```

Run: `go test ./internal/console/ -run 'Test口令|Test同一个|Test比对' -v`
Expected: FAIL，`undefined: console.HashPassword`、`undefined: console.CheckPassword`。

- [ ] **Step 2: 写失败的测试——登录与会话**

接着写在 `internal/console/auth_test.go`：

```go
// 登录成功给一个令牌，带上它就能打要权限的路由。
func Test登录成功给令牌(t *testing.T) {
	s, st := newStoreServer(t)
	seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	code, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"alice","password":"correct horse"}`)
	if code != 200 {
		t.Fatalf("登录返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expiresAt"`
			User      struct {
				Username    string   `json:"username"`
				Role        string   `json:"role"`
				Permissions []string `json:"permissions"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v\n%s", err, body)
	}
	if got.Data.Token == "" {
		t.Error("没有给令牌")
	}
	if got.Data.ExpiresAt != fixedAt+12*3600 {
		t.Errorf("到期时间是 %d，应为 %d", got.Data.ExpiresAt, fixedAt+12*3600)
	}
	if got.Data.User.Username != "alice" || got.Data.User.Role != "operator" {
		t.Errorf("用户是 %+v，应为 alice/operator", got.Data.User)
	}
	if len(got.Data.User.Permissions) == 0 {
		t.Error("没有给权限码，前端列不出菜单")
	}
}

// 口令不对与用户名不存在同一句话：不让别人拿用户名试探哪个账号存在。
func Test登录失败的三种说法一样(t *testing.T) {
	s, st := newStoreServer(t)
	seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	cases := map[string]string{
		"口令不对":     `{"username":"alice","password":"wrong"}`,
		"用户不存在":   `{"username":"nobody","password":"correct horse"}`,
		"用户名为空":   `{"username":"","password":"correct horse"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			code, got := do(t, s, "POST", "/console/v1/auth/login", body)
			if code != 401 {
				t.Errorf("返回 %d，应为 401：%s", code, got)
			}
			var env struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(got), &env); err != nil {
				t.Fatalf("响应不是 JSON：%v", err)
			}
			if env.Message != "用户名或口令不对" {
				t.Errorf("说明是 %q，三种失败应同一句话", env.Message)
			}
		})
	}
}

// 禁用的账号登不进来。
func Test禁用的账号登不进来(t *testing.T) {
	s, st := newStoreServer(t)
	u := seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	u.Disabled = true
	if _, err := st.UpdateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
		t.Fatalf("禁用 alice：%v", err)
	}
	code, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"alice","password":"correct horse"}`)
	if code != 401 {
		t.Errorf("禁用的账号登录返回 %d，应为 401：%s", code, body)
	}
}

// 会话过期之后令牌就不好用了。
func Test会话过期后令牌作废(t *testing.T) {
	s, st := newStoreServer(t)
	seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	code, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"alice","password":"correct horse"}`)
	if code != 200 {
		t.Fatalf("登录返回 %d：%s", code, body)
	}
	token := tokenOf(t, body)
	// 把时钟拨到 13 小时之后，会话的 12 小时 TTL 已经过了
	s.AdvanceClock(13 * 3600)
	code, body = doWith(t, s, "GET", "/console/v1/user/info", "", token)
	if code != 401 {
		t.Errorf("过期之后返回 %d，应为 401：%s", code, body)
	}
}

// 退出登录之后同一个令牌立刻不好用。
func Test退出登录后令牌作废(t *testing.T) {
	s, st := newStoreServer(t)
	seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	_, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"alice","password":"correct horse"}`)
	token := tokenOf(t, body)
	if code, _ := doWith(t, s, "POST", "/console/v1/auth/logout", "", token); code != 200 {
		t.Fatalf("退出登录返回 %d", code)
	}
	if code, body := doWith(t, s, "GET", "/console/v1/user/info", "", token); code != 401 {
		t.Errorf("退出之后返回 %d，应为 401：%s", code, body)
	}
}

// 用户信息给出自己与自己的权限码，不给口令散列。
func Test用户信息不带口令(t *testing.T) {
	s, st := newStoreServer(t)
	seedUser(t, st, "alice", store.RoleOperator, "correct horse")
	_, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"alice","password":"correct horse"}`)
	code, got := doWith(t, s, "GET", "/console/v1/user/info", "", tokenOf(t, body))
	if code != 200 {
		t.Fatalf("用户信息返回 %d：%s", code, got)
	}
	if strings.Contains(got, "pbkdf2") {
		t.Errorf("响应里带了口令散列：%s", got)
	}
}
```

`tokenOf`、`doWith`、`AdvanceClock` 在 Step 3 的夹具里定义。`auth_test.go` 的 import 加 `"encoding/json"`。

Run: `go test ./internal/console/ -run 'Test登录|Test禁用的账号|Test会话过期|Test退出登录|Test用户信息' -v`
Expected: FAIL，`undefined: newStoreServer`、`undefined: seedUser`。

- [ ] **Step 3: 写失败的测试——用户管理与夹具**

Create `internal/console/store_test.go`（连真实 PostgreSQL 的夹具，总 spec §6.4）：

```go
package console_test

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

// newStoreServer 建一个连真实 PostgreSQL 的控制台。每个顶层测试开一个仓储，不每个子测试开：
// 第 2 层评审提过的连接数上限在这里同样适用（总 spec §6.4）。pgtest.Open 自己在测试结束时
// 关仓储、删 schema。
func newStoreServer(t *testing.T) (*console.Server, store.Store) {
	t.Helper()
	st := pgtest.Open(t)
	gin.SetMode(gin.TestMode)
	s := console.New(console.Deps{
		Hosts: st, ISOs: st, Hooks: st, Templates: st, Tasks: st, Events: st, Audit: st, Users: st,
		Clock:      func() int64 { return currentNow() },
		SessionTTL: 12 * 3600,
		ISOSource:  stubISOSource{},
		TaskCtl:    stubTaskControl{},
		Notifier:   console.NewHub(),
	})
	return s, st
}

// now 是夹具的时钟，AdvanceClock 拨快它。固定起点，测试因此不依赖真实时间。
var now atomic.Int64

func currentNow() int64 { return now.Load() }

func init() { now.Store(fixedAt) }

// AdvanceClock 是测试后门：把时钟拨快 sec 秒。会话过期按它验。
func (s *console.Server) AdvanceClock(sec int64) { now.Add(sec) }

// doWith 打一个带令牌的请求。
func doWith(t *testing.T, s *console.Server, method, path, body, token string) (int, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// tokenOf 从登录响应里取出令牌。
func tokenOf(t *testing.T, body string) string {
	t.Helper()
	var got struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("解析登录响应失败：%v\n%s", err, body)
	}
	if got.Data.Token == "" {
		t.Fatalf("登录响应里没有令牌：%s", body)
	}
	return got.Data.Token
}

// seedUser 直接经仓储建一个用户，返回带上散列的记录。口令统一用 "correct horse"。
func seedUser(t *testing.T, st store.Store, name string, role store.Role, pw string) *store.User {
	t.Helper()
	hashed, err := console.HashPassword(pw)
	if err != nil {
		t.Fatalf("算口令散列：%v", err)
	}
	u := store.User{Username: name, DisplayName: name, Role: role, Password: hashed, CreatedAt: fixedAt}
	created, err := st.CreateUser(t.Context(), u, store.Audit{Actor: "seed", Action: "user.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建用户 %s：%v", name, err)
	}
	return &created
}

// adminToken 建一个 admin 并登录，返回令牌。用户管理的用例都用它。
func adminToken(t *testing.T, s *console.Server, st store.Store) string {
	t.Helper()
	seedUser(t, st, "root", store.RoleAdmin, "correct horse")
	_, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"root","password":"correct horse"}`)
	return tokenOf(t, body)
}
```

Create `internal/console/users_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/store"
)

// 权限矩阵：五个角色各打一次用户管理，只有 admin 过得去。
func Test用户管理的权限矩阵(t *testing.T) {
	roles := []store.Role{store.RoleViewer, store.RoleOperator, store.RoleHookAuthor,
		store.RoleHookApprover, store.RoleAdmin}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			s, st := newStoreServer(t)
			seedUser(t, st, "alice", role, "correct horse")
			_, body := do(t, s, "POST", "/console/v1/auth/login",
				`{"username":"alice","password":"correct horse"}`)
			token := tokenOf(t, body)
			code, got := doWith(t, s, "GET", "/console/v1/users", "", token)
			want := 403
			if role == store.RoleAdmin {
				want = 200
			}
			if code != want {
				t.Errorf("角色 %s 列用户返回 %d，应为 %d：%s", role, code, want, got)
			}
		})
	}
}

func Test建用户(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	code, body := doWith(t, s, "POST", "/console/v1/users",
		`{"username":"bob","displayName":"鲍勃","role":"operator","password":"correct horse"}`, token)
	if code != 200 {
		t.Fatalf("建用户返回 %d：%s", code, body)
	}
	if strings.Contains(body, "pbkdf2") {
		t.Errorf("响应里带了口令散列：%s", body)
	}
	var got struct {
		Data struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if got.Data.Username != "bob" || got.Data.Role != "operator" {
		t.Errorf("建出来的是 %+v，应为 bob/operator", got.Data)
	}
	// 建的用户能登录
	code, _ = do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"bob","password":"correct horse"}`)
	if code != 200 {
		t.Errorf("新建的用户登录返回 %d，应为 200", code)
	}
}

// 每个要审计的写操作都留下审计记录（总 spec §6.2 第 8 条、§7.2 第 5 条）。
func Test用户管理写审计(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	code, _ := doWith(t, s, "POST", "/console/v1/users",
		`{"username":"bob","displayName":"鲍勃","role":"operator","password":"correct horse"}`, token)
	if code != 200 {
		t.Fatalf("建用户返回 %d", code)
	}
	code, _ = doWith(t, s, "PUT", "/console/v1/users/bob",
		`{"username":"bob","displayName":"小鲍","role":"viewer","password":"correct horse"}`, token)
	if code != 200 {
		t.Fatalf("改用户返回 %d", code)
	}
	code, _ = doWith(t, s, "DELETE", "/console/v1/users/bob", "", token)
	if code != 200 {
		t.Fatalf("禁用用户返回 %d", code)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	actions := map[string]int{}
	for _, e := range page.Items {
		if e.TargetKind == "user" {
			actions[e.Action]++
			if e.Actor != "root" {
				t.Errorf("审计的操作者是 %q，应为 root", e.Actor)
			}
			if e.TargetID != "bob" {
				t.Errorf("审计的对象是 %q，应为 bob", e.TargetID)
			}
		}
	}
	for _, want := range []string{"user.create", "user.update", "user.disable"} {
		if actions[want] == 0 {
			t.Errorf("没有 %s 的审计记录，实际有 %v", want, actions)
		}
	}
}

// 用户名不合法、角色不合法都在写库之前拦下，报 400 带一句说明。
func Test建用户的入参不合法(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	cases := map[string]string{
		"用户名大写":  `{"username":"Bob","role":"viewer","password":"x"}`,
		"用户名带点":  `{"username":"b.o.b","role":"viewer","password":"x"}`,
		"用户名为空":  `{"username":"","role":"viewer","password":"x"}`,
		"口令为空":   `{"username":"bob","role":"viewer","password":""}`,
		"角色不合法":  `{"username":"bob","role":"root","password":"x"}`,
		"角色大小写":  `{"username":"bob","role":"Admin","password":"x"}`,
		"不是 JSON": `{"username":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			code, got := doWith(t, s, "POST", "/console/v1/users", body, token)
			if code != 400 {
				t.Errorf("返回 %d，应为 400：%s", code, got)
			}
			var env struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(got), &env); err != nil {
				t.Fatalf("响应不是 JSON：%v", err)
			}
			if env.Message == "" {
				t.Error("没有给一句说明")
			}
		})
	}
}

// 用户名已登记报 409，说明是谁占着。
func Test建用户重名(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	seedUser(t, st, "bob", store.RoleViewer, "correct horse")
	code, got := doWith(t, s, "POST", "/console/v1/users",
		`{"username":"bob","displayName":"另一个鲍勃","role":"viewer","password":"x"}`, token)
	if code != 409 {
		t.Errorf("返回 %d，应为 409：%s", code, got)
	}
}

// 不能禁用自己的账号：否则一时兴起就把自己关在门外，而别人不一定在线。
func Test不能禁用自己的账号(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "DELETE", "/console/v1/users/root", "", token)
	if code != 409 {
		t.Errorf("禁用自己返回 %d，应为 409：%s", code, got)
	}
	u, err := st.GetUser(t.Context(), "root")
	if err != nil {
		t.Fatalf("查 root：%v", err)
	}
	if u.Disabled {
		t.Error("root 被禁用了")
	}
}

// 禁用是置标记，不删行：审计要能一直指到这个人。
func Test禁用不删行(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	seedUser(t, st, "bob", store.RoleViewer, "correct horse")
	if code, _ := doWith(t, s, "DELETE", "/console/v1/users/bob", "", token); code != 200 {
		t.Fatalf("禁用 bob 返回 %d", code)
	}
	u, err := st.GetUser(t.Context(), "bob")
	if err != nil {
		t.Fatalf("禁用之后查不到 bob 了：%v", err)
	}
	if !u.Disabled {
		t.Error("bob 没有被禁用")
	}
	// 禁用的人登不进来
	if code, _ := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"bob","password":"correct horse"}`); code != 401 {
		t.Errorf("禁用的人登录返回 %d，应为 401", code)
	}
}

// 改用户时口令可空：不传就不改。
func Test改用户可以不传口令(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	seedUser(t, st, "bob", store.RoleViewer, "correct horse")
	code, got := doWith(t, s, "PUT", "/console/v1/users/bob",
		`{"username":"bob","displayName":"小鲍","role":"operator"}`, token)
	if code != 200 {
		t.Fatalf("改用户返回 %d：%s", code, got)
	}
	if code, _ := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"bob","password":"correct horse"}`); code != 200 {
		t.Error("不传口令就把口令改坏了")
	}
}

// 已经禁用的用户改回来还能登录。
func Test恢复禁用的用户(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	u := seedUser(t, st, "bob", store.RoleViewer, "correct horse")
	u.Disabled = true
	if _, err := st.UpdateUser(t.Context(), *u, store.Audit{Actor: "seed", Action: "user.update", At: fixedAt}); err != nil {
		t.Fatalf("禁用 bob：%v", err)
	}
	code, got := doWith(t, s, "PUT", "/console/v1/users/bob",
		`{"username":"bob","displayName":"小鲍","role":"viewer","disabled":false}`, token)
	if code != 200 {
		t.Fatalf("恢复 bob 返回 %d：%s", code, got)
	}
	if code, _ := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"bob","password":"correct horse"}`); code != 200 {
		t.Errorf("恢复之后登录返回 %d，应为 200", code)
	}
}

// 列用户按用户名的字节序，游标翻页与一次全取相同。
func Test列用户分页(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, name := range []string{"bob", "alice", "carol", "dave"} {
		seedUser(t, st, name, store.RoleViewer, "correct horse")
	}
	code, body := doWith(t, s, "GET", "/console/v1/users?limit=2", "", token)
	if code != 200 {
		t.Fatalf("列用户返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				Username string `json:"username"`
			} `json:"items"`
			Next string `json:"next"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Fatalf("第一页有 %d 条，应为 2 条", len(got.Data.Items))
	}
	// root 是 adminToken 建的，按字节序排在最前：alice、bob、carol、dave、root
	if got.Data.Items[0].Username != "alice" || got.Data.Items[1].Username != "bob" {
		t.Errorf("第一页是 %s、%s，应按字节序 alice、bob", got.Data.Items[0].Username, got.Data.Items[1].Username)
	}
	if got.Data.Next == "" {
		t.Error("还有下一页却没给游标")
	}
}
```

Run: `go test ./internal/console/ -run 'Test用户管理|Test建用户|Test用户管理写审计|Test不能禁用|Test禁用不删行|Test改用户|Test恢复禁用|Test列用户' -v`
Expected: FAIL，`undefined: console.HashPassword`、`undefined: (*console.Server).AdvanceClock`、`undefined: s.login`。

- [ ] **Step 4: 实现口令散列与三个认证端点**

Create `internal/console/auth.go`：

```go
package console

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/store"
)

// 口令散列的格式：pbkdf2-sha256$<迭代>$<盐>$<散列>，盐与散列都是 base64.RawURLEncoding。
// 用标准库的 crypto/pbkdf2，不引 x/crypto：60 万次 SHA-256 在 2026 年的机器上是几十毫秒，
// 足够慢，也足够简单。
const (
	pbkdf2Algo     = "pbkdf2-sha256"
	pbkdf2Iter     = 600000
	pbkdf2KeyLen   = 32
	pbkdf2SaltLen  = 16
	pbkdf2MaxParts = 4
)

// HashPassword 算一个口令的散列。每次用新的随机盐，同一个口令两次算出来不一样。
func HashPassword(pw string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("读随机盐：%w", err)
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, pbkdf2KeyLen)
	if err != nil {
		return "", fmt.Errorf("算散列：%w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s", pbkdf2Algo, pbkdf2Iter,
		base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(key)), nil
}

// CheckPassword 比对口令与散列，恒定时间比较。散列被改坏时返回 false，不 panic：库里可能是
// 旧格式，或者有人手工改过，那是要查的事，不是让请求崩掉的事。
func CheckPassword(pw, hashed string) bool {
	parts := strings.Split(hashed, "$")
	if len(parts) != pbkdf2MaxParts || parts[0] != pbkdf2Algo {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// loginRequest 是登录的请求体。
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// sessionResponse 是登录成功后的响应：令牌、到期时间与用户本人。
type sessionResponse struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expiresAt"`
	User      userResponse `json:"user"`
}

// userResponse 是一个用户对外的形状。口令散列永不经 HTTP 出去。
type userResponse struct {
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        store.Role `json:"role"`
	Permissions []string   `json:"permissions"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   int64      `json:"createdAt"`
}

func toUserResponse(u store.User) userResponse {
	return userResponse{
		Username: u.Username, DisplayName: u.DisplayName, Role: u.Role,
		Permissions: PermsOf(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt,
	}
}

// authRoutes 是认证的三个端点。登录本身免登录：它没有权限码，authenticate 因此放行。
// 用户信息与退出登录只问你是谁，不问你能做什么，用 PermAnyUser。
func (s *server) authRoutes() []route {
	return []route{
		{"POST", "/console/v1/auth/login", "", "", "认证", "登录", loginRequest{}, sessionResponse{}, s.login},
		{"POST", "/console/v1/auth/logout", PermAnyUser, "", "认证", "退出登录", nil, nil, s.logout},
		{"GET", "/console/v1/user/info", PermAnyUser, "", "认证", "当前用户与权限", nil, userResponse{}, s.userInfo},
	}
}

func (s *server) login(c *gin.Context) {
	var req loginRequest
	if !Bind(c, &req) {
		return
	}
	u, err := s.deps.Users.GetUser(c.Request.Context(), req.Username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, statusUnauthorized, nil, "用户名或口令不对")
		return
	case err != nil:
		RespondError(c, err)
		return
	}
	// 两种失败同一句话：不让别人拿用户名试探哪个账号存在
	if u.Disabled || !CheckPassword(req.Password, u.Password) {
		Fail(c, statusUnauthorized, nil, "用户名或口令不对")
		return
	}
	Ok(c, sessionResponse{
		Token:     s.sessions.put(u.Username),
		ExpiresAt: s.clock() + s.deps.SessionTTL,
		User:      toUserResponse(u),
	})
}

func (s *server) logout(c *gin.Context) {
	s.sessions.drop(bearerToken(c))
	Ok(c, nil)
}

func (s *server) userInfo(c *gin.Context) {
	u, err := s.deps.Users.GetUser(c.Request.Context(), userOf(c))
	if err != nil {
		RespondError(c, err) // 会话里的人被删了：不该发生，发生了就照实说
		return
	}
	Ok(c, toUserResponse(u))
}
```

- [ ] **Step 5: 补认证与权限中间件**

Modify `internal/console/server.go`。`authenticate` 改成查仓储，`authorize` 改成查角色：

```go
// authenticate 按路由表决定要不要认证：perm 为空的路由（只有登录本身）免登录，其余要一个
// 没过期的会话，还要这个账号还在、没被禁用。令牌来自 Authorization 头，SSE 那条路另收
// access_token 查询参数（EventSource 发不了头）。
//
// 每个请求都查一次仓储，不把用户存进会话：角色改了、账号被禁用了，下一次请求就看得出来，
// 不用等这一次会话过期（第 5 层设计 §5）。
func (s *server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		r, ok := s.meta(c)
		if ok && r.perm == "" {
			c.Next()
			return
		}
		name, ok := s.sessions.lookup(bearerToken(c))
		if !ok {
			Fail(c, statusUnauthorized, nil, "没有登录，或者会话已经过期")
			return
		}
		u, err := s.deps.Users.GetUser(c.Request.Context(), name)
		if err != nil || u.Disabled {
			// 会话里的人被删掉或禁用了：当作没有登录，让他重新登
			s.sessions.drop(bearerToken(c))
			Fail(c, statusUnauthorized, nil, "没有登录，或者会话已经过期")
			return
		}
		c.Set(ctxUser, &u)
		c.Next()
	}
}

// authorize 查权限码，逐条路由挂（见 register）。
func (s *server) authorize(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if perm == PermAnyUser {
			c.Next() // 已登录即可：用户信息与退出登录只问你是谁，不问你能做什么
			return
		}
		u := userOf(c)
		if u == nil || !slices.Contains(PermsOf(u.Role), perm) {
			Fail(c, statusForbidden, nil, "没有做这件事的权限")
			return
		}
		c.Next()
	}
}
```

`ctxUser` 与 `userOf` 改回存 `*store.User`：

```go
// ctxUser 是当前登录的用户，认证中间件放进去，authorize 与 auditFrom 从这儿取。
const ctxUser = "console.user"

// userOf 取当前登录的用户。认证中间件之后一定有，免登录的路由没有。
func userOf(c *gin.Context) *store.User {
	u, _ := c.Get(ctxUser)
	up, _ := u.(*store.User)
	return up
}
```

`auditFrom` 里 `Actor: userOf(c).Username`。`server.go` 的 import 加回 `"slices"`。

`export_test.go` 的 `TestLogin` 改成把用户名放进会话：

```go
func (s *Server) TestLogin(u *store.User) string { return s.server.sessions.put(u.Username) }
```

- [ ] **Step 6: 实现用户管理端点**

Create `internal/console/users.go`：

```go
package console

import (
	"github.com/lober/lober/internal/store"
)

// userRequest 是新建与修改用户的请求体。口令可空：不传就不改。
type userRequest struct {
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        store.Role `json:"role"`
	Password    string     `json:"password"`
	Disabled    bool       `json:"disabled"`
}

// userPage 是一页用户。
type userPage struct {
	Items []userResponse `json:"items"`
	Next  string         `json:"next"`
}

// userRoutes 是用户管理，只有 admin 能用（user:manage）。删是禁用：记录留着，审计才一直
// 指得到这个人（第 5 层设计 §5）。
func (s *server) userRoutes() []route {
	return []route{
		{"GET", "/console/v1/users", PermUserManage, "", "用户", "列出用户", nil, userPage{}, s.listUsers},
		{"POST", "/console/v1/users", PermUserManage, "user.create", "用户", "新建用户", userRequest{}, userResponse{}, s.createUser},
		{"PUT", "/console/v1/users/{username}", PermUserManage, "user.update", "用户", "修改用户", userRequest{}, userResponse{}, s.updateUser},
		{"DELETE", "/console/v1/users/{username}", PermUserManage, "user.disable", "用户", "禁用用户", nil, nil, s.disableUser},
	}
}

func (s *server) listUsers(c *gin.Context) {
	var q store.UserQuery
	q.Cursor = c.Query("cursor")
	q.Limit = atoiDefault(c.Query("limit"), 0)
	page, ok := Get(c, func() (store.Page[store.User], error) {
		return s.deps.Users.ListUsers(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]userResponse, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, toUserResponse(u))
	}
	Ok(c, userPage{Items: items, Next: page.Next})
}

func (s *server) createUser(c *gin.Context) {
	var req userRequest
	if !Bind(c, &req) {
		return
	}
	// 这两条在写库之前拦下：报 400 带一句人看得懂的话，比让仓储报 text_not_storable 清楚
	if !validUsername(req.Username) {
		Fail(c, statusBadRequest, nil, "用户名要以小写字母开头，只含小写字母、数字和下划线，最长 32 个字符")
		return
	}
	if !req.Role.Valid() {
		Fail(c, statusBadRequest, nil, "角色只能是 viewer、operator、hook-author、hook-approver、admin 之一")
		return
	}
	hashed, err := HashPassword(req.Password)
	if err != nil {
		Fail(c, statusInternal, nil, "服务端内部错误")
		return
	}
	u := store.User{
		Username: req.Username, DisplayName: req.DisplayName, Role: req.Role,
		Password: hashed, Disabled: req.Disabled, CreatedAt: s.clock(),
	}
	created, ok := Write(c, func() (store.User, error) {
		return s.deps.Users.CreateUser(c.Request.Context(), u, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	Ok(c, toUserResponse(created))
}

func (s *server) updateUser(c *gin.Context) {
	var req userRequest
	if !Bind(c, &req) {
		return
	}
	if !validUsername(req.Username) || !req.Role.Valid() {
		Fail(c, statusBadRequest, nil, "用户名或角色不合法")
		return
	}
	cur, ok := Get(c, func() (store.User, error) {
		return s.deps.Users.GetUser(c.Request.Context(), c.Param("username"))
	})
	if !ok {
		return
	}
	// 用户名是身份，按路径里的那个找行，请求体里的用户名改了就是另一个人
	cur.Username = c.Param("username")
	cur.DisplayName = req.DisplayName
	cur.Role = req.Role
	cur.Disabled = req.Disabled
	if req.Password != "" {
		hashed, err := HashPassword(req.Password)
		if err != nil {
			Fail(c, statusInternal, nil, "服务端内部错误")
			return
		}
		cur.Password = hashed
	}
	updated, ok := Write(c, func() (store.User, error) {
		return s.deps.Users.UpdateUser(c.Request.Context(), cur, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	Ok(c, toUserResponse(updated))
}

func (s *server) disableUser(c *gin.Context) {
	username := c.Param("username")
	// 不能禁用自己的账号：一时兴起就把自己关在门外，而别人不一定在线
	if username == userOf(c).Username {
		Fail(c, statusConflict, nil, "不能禁用自己的账号")
		return
	}
	cur, ok := Get(c, func() (store.User, error) {
		return s.deps.Users.GetUser(c.Request.Context(), username)
	})
	if !ok {
		return
	}
	if cur.Disabled {
		Ok(c, nil) // 已经禁用了，再说一次也是成功
		return
	}
	cur.Disabled = true
	if _, ok := Write(c, func() (store.User, error) {
		return s.deps.Users.UpdateUser(c.Request.Context(), cur, s.auditFrom(c, ""))
	}); !ok {
		return
	}
	Ok(c, nil)
}

// validUsername 是用户名的规则：标识符，且不超过 32 个字符（store.CheckUser 也查，这里先拦
// 一道，为了一句人看得懂的话）。
func validUsername(name string) bool {
	return store.CheckUser(store.User{Username: name, Role: store.RoleViewer,
		Password: "x", CreatedAt: 1}) == nil
}

// atoiDefault 解析查询参数里的整数，空串或解析不了用 def。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}
```

Modify `internal/console/routes.go` 的 `allRoutes`：

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),
		s.userRoutes(),
	)
}
```

到此为止的形态。后面每个资源任务往里加一组，Task 13 收尾时是九组全上的形态。


`users.go` 的 import 加 `"github.com/lober/lober/internal/core/model"`（`validUsername` 不用它，改用 `model.ValidIdent` 更直接）：

```go
func validUsername(name string) bool {
	return model.ValidIdent(name) && len(name) <= 32
}
```

- [ ] **Step 7: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。日志里能看到连上了 PostgreSQL（`pgtest` 找不到连接串会直接失败，不跳过）。

- [ ] **Step 8: 提交**

```bash
git add internal/console
git commit -m "feat(console): 登录、会话与用户管理，权限矩阵与审计各有用例"
```

---

---

## Task 6：主机（含 CSV 两步导入）

**Files:**
- Create: `internal/console/hosts.go`
- Create: `internal/console/csv.go`
- Modify: `internal/console/routes.go`
- Test: `internal/console/hosts_test.go`、`internal/console/csv_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Deps`/`New`/`Bind`/`Get`/`Write`/`Judge`/`auditFrom`/`clock`；Task 5 的 `newStoreServer`、`seedUser`、`adminToken`、`do`/`doWith`/`tokenOf`
- Produces:
  - `hostResponse`、`hostRequest`、`hostPage`、`csvPlan`、`csvRow`（OpenAPI 的 schema 名）
  - `(*server).hostRoutes() []route`
  - `parseHostCSV(data []byte, known func(string) bool) (csvPlan, []model.Host)`

- [ ] **Step 1: 写失败的测试——CSV 解析**

Create `internal/console/csv_test.go`：

```go
package console_test

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/console"
)

// 表头必须有 sn；arch 与 firmware 可选；其余列都成为属性（总 spec §5.3）。
func Test解析主机CSV(t *testing.T) {
	csv := "sn,arch,firmware,rack,ip\r\nSN-1,x86_64,uefi,A01,10.0.0.1\r\nSN-2,,,A02,10.0.0.2\r\n"
	plan, hosts := console.ParseHostCSV([]byte(csv), func(string) bool { return false })
	if len(hosts) != 2 {
		t.Fatalf("解析出 %d 台，应为 2 台", len(hosts))
	}
	want := hosts[0]
	if want.SN != "SN-1" || want.Arch != "x86_64" || want.Firmware != "uefi" {
		t.Errorf("第一台是 %+v，SN、架构、固件应按列填", want)
	}
	if want.Attrs["rack"] != "A01" || want.Attrs["ip"] != "10.0.0.1" {
		t.Errorf("第一台的属性是 %v，其余列都该成为属性", want.Attrs)
	}
	if hosts[1].Arch != "" || hosts[1].Firmware != "" {
		t.Errorf("第二台的架构、固件应为空：%+v", hosts[1])
	}
	for _, r := range plan.Rows {
		if r.Action != "create" {
			t.Errorf("第 %d 行的动作是 %q，没登记过的应为 create", r.Row, r.Action)
		}
	}
	if plan.Rows[0].Row != 2 || plan.Rows[1].Row != 3 {
		t.Errorf("行号是 %d、%d，应从 2 起（第 1 行是表头）", plan.Rows[0].Row, plan.Rows[1].Row)
	}
}

// 已登记的 SN 标成 update（总 spec §5.3：CSV 导入先解析，逐行标出新建、更新或错误）。
func TestCSV按登记情况标新建或更新(t *testing.T) {
	csv := "sn,arch\nSN-1,x86_64\nSN-2,x86_64\n"
	known := map[string]bool{"SN-1": true}
	plan, _ := console.ParseHostCSV([]byte(csv), func(sn string) bool { return known[sn] })
	if plan.Rows[0].Action != "update" {
		t.Errorf("SN-1 的动作是 %q，已登记的应为 update", plan.Rows[0].Action)
	}
	if plan.Rows[1].Action != "create" {
		t.Errorf("SN-2 的动作是 %q，没登记过的应为 create", plan.Rows[1].Action)
	}
}

// SN 不合法或记录不合法标成 error，并把问题带上；其余行照常解析。
func TestCSV标出有错的行(t *testing.T) {
	csv := "sn,arch\n,x86_64\nSN-1,X86_64\nSN-2,x86_64\n"
	plan, hosts := console.ParseHostCSV([]byte(csv), func(string) bool { return false })
	if len(hosts) != 1 || hosts[0].SN != "SN-2" {
		t.Fatalf("应只解析出 SN-2，实际是 %+v", hosts)
	}
	if plan.Rows[0].Action != "error" || len(plan.Rows[0].Problems) == 0 {
		t.Errorf("空 SN 那行是 %+v，应标 error 并带问题", plan.Rows[0])
	}
	if plan.Rows[1].Action != "error" || len(plan.Rows[1].Problems) == 0 {
		t.Errorf("架构不合法那行是 %+v，应标 error 并带问题", plan.Rows[1])
	}
	if plan.Rows[2].Action != "create" {
		t.Errorf("SN-2 那行是 %q，应为 create", plan.Rows[2].Action)
	}
}

// 批内 SN 重复：几处都算错，只报一次 host_duplicate_in_batch（与 store.ImportHosts 一致）。
func TestCSV批内重复的SN(t *testing.T) {
	csv := "sn,arch\nSN-1,x86_64\nsn-1,x86_64\n"
	plan, _ := console.ParseHostCSV([]byte(csv), func(string) bool { return false })
	for i, r := range plan.Rows {
		found := false
		for _, p := range r.Problems {
			if p.Code == "host_duplicate_in_batch" {
				found = true
			}
		}
		if !found {
			t.Errorf("第 %d 行没报 host_duplicate_in_batch：%+v", r.Row, r)
		}
	}
}

// Windows 上存成 UTF-8 带 BOM、CRLF 换行的文件要能读（总 spec §3 总则）。
func TestCSV容忍BOM与CRLF(t *testing.T) {
	csv := "\xef\xbb\xbfsn,arch\r\nSN-1,x86_64\r\n"
	plan, hosts := console.ParseHostCSV([]byte(csv), func(string) bool { return false })
	if len(hosts) != 1 || hosts[0].SN != "SN-1" {
		t.Fatalf("解析出 %+v，BOM 应被剥掉", hosts)
	}
	if plan.Rows[0].Action != "create" {
		t.Errorf("动作是 %q，应为 create", plan.Rows[0].Action)
	}
}

// 表头缺 sn、或者文件根本不是 CSV：报一个说得清的错，不是panic。
func TestCSV的两种坏文件(t *testing.T) {
	if _, _, err := console.ParseHostCSVErr([]byte("arch,firmware\nx86_64,uefi\n"),
		func(string) bool { return false }); err == nil {
		t.Error("表头缺 sn 却没报错")
	}
	if _, _, err := console.ParseHostCSVErr([]byte("sn,arch\n\"SN-1,x86_64\n"),
		func(string) bool { return false }); err == nil {
		t.Error("引号没闭合却没报错")
	}
}

// 列数对不上：报错并说是第几行，不让操作员对着一堆错行猜。
func TestCSV列数对不上(t *testing.T) {
	_, _, err := console.ParseHostCSVErr([]byte("sn,arch\nSN-1\nSN-2,x86_64,y\n"),
		func(string) bool { return false })
	if err == nil {
		t.Fatal("列数对不上却没报错")
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("错误是 %v，应指出是第几行", err)
	}
}
```

`ParseHostCSVErr` 是 `ParseHostCSV` 的带错误版本：文件本身读不成（表头缺 `sn`、引号没闭合、列数对不上）时报错，行内的记录问题照常进 `plan.Rows`。

Run: `go test ./internal/console/ -run 'Test解析主机CSV|TestCSV' -v`
Expected: FAIL，`undefined: console.ParseHostCSV`、`undefined: console.ParseHostCSVErr`。

- [ ] **Step 2: 写失败的测试——主机端点**

Create `internal/console/hosts_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hostBody 是打主机端点共用的请求体。
func hostBody(sn, arch, fw string, attrs map[string]string) string {
	b, _ := json.Marshal(hostRequest{SN: sn, Arch: catalog.Arch(arch),
		Firmware: catalog.Firmware(fw), Attrs: attrs})
	return string(b)
}

func Test登记主机(t *testing.T) {
	s, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, body := doWith(t, s, "POST", "/console/v1/hosts",
		hostBody("SN-1", "x86_64", "uefi", map[string]string{"rack": "A01"}), token)
	if code != 200 {
		t.Fatalf("登记返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			SN      string            `json:"sn"`
			Arch    string            `json:"arch"`
			Attrs   map[string]string `json:"attrs"`
			Rev     int64             `json:"rev"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if got.Data.SN != "SN-1" || got.Data.Arch != "x86_64" {
		t.Errorf("登记出来的是 %+v", got.Data)
	}
	if got.Data.Rev != 1 {
		t.Errorf("Rev 是 %d，应为 1", got.Data.Rev)
	}
	if got.Data.Attrs["rack"] != "A01" {
		t.Errorf("属性是 %v，应为 rack=A01", got.Data.Attrs)
	}
}

// SN 没规范化就报 400，并说明要先规范化（store 的规则：带整条记录的写入查规范化）。
func Test登记主机SN没规范化(t *testing.T) {
	s, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/hosts",
		hostBody("sn-1", "x86_64", "", nil), token)
	if code != 400 {
		t.Errorf("返回 %d，应为 400：%s", code, got)
	}
	if !strings.Contains(got, "SN") {
		t.Errorf("说明是 %s，应提到 SN", got)
	}
}

// SN 不合法报 422，问题是 model 的 sn_invalid，说明与下一步原样来自代码表。
func Test登记主机SN不合法(t *testing.T) {
	s, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/hosts",
		hostBody("", "x86_64", "", nil), token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "sn_invalid") {
		t.Errorf("响应里没有 sn_invalid：%s", got)
	}
}

// 架构、固件、属性键不合法报 422，问题逐条带出（C7）。
func Test登记主机记录不合法(t *testing.T) {
	s, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/hosts",
		hostBody("SN-1", "amd64", "UEFI", map[string]string{"sn": "x"}), token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	for _, want := range []string{"host_platform_invalid", "attr_key_invalid"} {
		if !strings.Contains(got, want) {
			t.Errorf("响应里没有 %s：%s", want, got)
		}
	}
}

// 列主机按 SN 的字节序，属性过滤与前缀过滤各一例。
func Test列主机(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, sn := range []string{"SN-2", "SN-10", "SN-1"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64",
			Attrs: map[string]string{"rack": "A01"}},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	code, body := doWith(t, s, "GET", "/console/v1/hosts", "", token)
	if code != 200 {
		t.Fatalf("列主机返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				SN string `json:"sn"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	want := []string{"SN-1", "SN-10", "SN-2"} // 字节序：'1' < '2'
	if len(got.Data.Items) != 3 {
		t.Fatalf("列出 %d 台，应为 3 台", len(got.Data.Items))
	}
	for i, sn := range want {
		if got.Data.Items[i].SN != sn {
			t.Errorf("第 %d 台是 %s，应按字节序 %s", i, got.Data.Items[i].SN, sn)
		}
	}
	// 按属性过滤
	code, body = doWith(t, s, "GET", "/console/v1/hosts?attr_key=rack&attr_value=A01", "", token)
	if code != 200 {
		t.Fatalf("按属性过滤返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 3 {
		t.Errorf("按 rack=A01 过滤出 %d 台，应为 3 台", len(got.Data.Items))
	}
}

// 改主机是整条替换，Rev 加 1；Rev 对不上报 409。
func Test改主机(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	created, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "x86_64",
		Attrs: map[string]string{"rack": "A01"}},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建主机：%v", err)
	}
	code, got := doWith(t, s, "PUT", "/console/v1/hosts/SN-1",
		hostBody("SN-1", "aarch64", "bios", map[string]string{"rack": "A02"}), token)
	if code != 200 {
		t.Fatalf("改主机返回 %d：%s", code, got)
	}
	back, err := st.GetHost(t.Context(), "SN-1")
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.Arch != "aarch64" || back.Firmware != "bios" || back.Attrs["rack"] != "A02" {
		t.Errorf("改完之后是 %+v，整条替换没生效", back)
	}
	if back.Rev != created.Rev+1 {
		t.Errorf("Rev 是 %d，应为 %d", back.Rev, created.Rev+1)
	}
	// Rev 对不上：请求体里带的 Rev 是旧的
	code, got = doWith(t, s, "PUT", "/console/v1/hosts/SN-1",
		hostBodyRev("SN-1", "x86_64", "", created.Rev), token)
	if code != 409 {
		t.Errorf("Rev 对不上返回 %d，应为 409：%s", code, got)
	}
}

// 删主机：被任务引用的报 409（C26），引用检查在 Task 12 补用例，这里先钉没引用的能删。
func Test删主机(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	code, got := doWith(t, s, "DELETE", "/console/v1/hosts/SN-1?rev=1", "", token)
	if code != 200 {
		t.Fatalf("删主机返回 %d：%s", code, got)
	}
	if _, err := st.GetHost(t.Context(), "SN-1"); err == nil {
		t.Error("删之后还读得到")
	}
}

// 登记主机写一条审计，操作者是当前用户（总 spec §6.2 第 8 条）。
func Test登记主机写审计(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	if code, _ := doWith(t, s, "POST", "/console/v1/hosts",
		hostBody("SN-1", "x86_64", "", nil), token); code != 200 {
		t.Fatalf("登记返回 %d", code)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	found := false
	for _, e := range page.Items {
		if e.TargetKind == "host" && e.TargetID == "SN-1" {
			found = true
			if e.Action != "host.create" || e.Actor != "root" {
				t.Errorf("审计是 %+v，动作应为 host.create、操作者应为 root", e)
			}
		}
	}
	if !found {
		t.Error("没有 host/SN-1 的审计记录")
	}
}

// CSV 两步：dryRun 只解析并逐行标出新建、更新或错误，操作员确认后一次提交（总 spec §5.3）。
func TestCSV导入两步(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "x86_64"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建 SN-1：%v", err)
	}
	csv := "sn,arch,rack\r\nSN-1,aarch64,A02\r\nSN-2,x86_64,A01\r\n"
	// 第一步：dryRun，什么都不写
	code, body := doWith(t, s, "POST", "/console/v1/hosts/import?dryRun=true", csv, token)
	if code != 200 {
		t.Fatalf("dryRun 返回 %d：%s", code, body)
	}
	var plan struct {
		Data struct {
			Rows []struct {
				Row    int    `json:"row"`
				SN     string `json:"sn"`
				Action string `json:"action"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &plan); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(plan.Data.Rows) != 2 {
		t.Fatalf("解析出 %d 行，应为 2 行", len(plan.Data.Rows))
	}
	if plan.Data.Rows[0].Action != "update" || plan.Data.Rows[1].Action != "create" {
		t.Errorf("动作是 %s、%s，应为 update、create",
			plan.Data.Rows[0].Action, plan.Data.Rows[1].Action)
	}
	if _, err := st.GetHost(t.Context(), "SN-2"); err == nil {
		t.Error("dryRun 之后 SN-2 已经写进去了")
	}
	// 第二步：真导入
	code, got := doWith(t, s, "POST", "/console/v1/hosts/import", csv, token)
	if code != 200 {
		t.Fatalf("导入返回 %d：%s", code, got)
	}
	back, err := st.GetHost(t.Context(), "SN-1")
	if err != nil {
		t.Fatalf("读回 SN-1：%v", err)
	}
	if back.Arch != "aarch64" || back.Attrs["rack"] != "A02" {
		t.Errorf("SN-1 是 %+v，已登记的应整条替换", back)
	}
	if _, err := st.GetHost(t.Context(), "SN-2"); err != nil {
		t.Errorf("SN-2 没建出来：%v", err)
	}
}

// 一条不合法，整批都不写，逐条列出问题（store.ImportHosts 的语义）。
func TestCSV导入一条不合法整批不写(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	csv := "sn,arch\r\nSN-1,x86_64\r\nSN-2,amd64\r\nSN-3,x86_64\r\n"
	code, got := doWith(t, s, "POST", "/console/v1/hosts/import", csv, token)
	if code != 422 {
		t.Fatalf("导入返回 %d，应为 422：%s", code, got)
	}
	for _, sn := range []string{"SN-1", "SN-2", "SN-3"} {
		if _, err := st.GetHost(t.Context(), sn); err == nil {
			t.Errorf("%s 写进去了，整批都不该写", sn)
		}
	}
	if !strings.Contains(got, "host_platform_invalid") {
		t.Errorf("响应里没有 host_platform_invalid：%s", got)
	}
}

// 导入也写审计，每台主机一条（总 spec §6.2 第 8 条）。
func TestCSV导入写审计(t *testing.T) {
	s, st := newStoreServer(t)
	token := adminToken(t, s, st)
	csv := "sn,arch\r\nSN-1,x86_64\r\nSN-2,x86_64\r\n"
	if code, _ := doWith(t, s, "POST", "/console/v1/hosts/import", csv, token); code != 200 {
		t.Fatalf("导入返回 %d", code)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	seen := map[string]bool{}
	for _, e := range page.Items {
		if e.TargetKind == "host" && e.Action == "host.import" {
			seen[e.TargetID] = true
		}
	}
	for _, sn := range []string{"SN-1", "SN-2"} {
		if !seen[sn] {
			t.Errorf("没有 %s 的导入审计", sn)
		}
	}
}
```

`hostRequest`、`hostBodyRev` 在 Step 4 定义；`hostBodyRev` 与 `hostBody` 的差别只在一个 `rev` 字段。

Run: `go test ./internal/console/ -run 'Test登记主机|Test列主机|Test改主机|Test删主机|TestCSV导入' -v`
Expected: FAIL，`undefined: hostRequest`、`undefined: s.hostRoutes`。

- [ ] **Step 3: 实现 CSV 解析**

Create `internal/console/csv.go`：

```go
package console

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// csvRow 是 CSV 里一行的解析结果。Row 从 2 起：第 1 行是表头，操作员在编辑器里看到的就是这个行号。
type csvRow struct {
	Row      int              `json:"row"`
	SN       string           `json:"sn"`
	Action   string           `json:"action"` // create、update、error
	Problems []model.Problem  `json:"problems,omitempty"`
}

// csvPlan 是一次 CSV 解析的结果，dryRun 的响应体。Total 是数据行数。
type csvPlan struct {
	Rows  []csvRow `json:"rows"`
	Total int      `json:"total"`
}

// 三种动作。
const (
	csvCreate = "create"
	csvUpdate = "update"
	csvError  = "error"
)

// utf8BOM 是 Windows 上存成 UTF-8 的文件常带的字节序标记，剥掉它第一列才认得出来
// （总 spec §3 总则）。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseHostCSV 解析主机 CSV，返回逐行的计划与能写库的那些主机。表头必须有 sn 一列，arch 与
// firmware 可选，其余列都成为属性（总 spec §5.3）。known 报告一个 SN 是不是已登记，用来标
// 这一行是新建还是更新。
//
// 行内的记录问题（SN 不合法、架构不合法、属性键不合法）进 plan.Rows 的 Problems，这一行不进
// hosts；文件本身读不成（表头缺 sn、引号没闭合、列数对不上）由 ParseHostCSVErr 报错。
func ParseHostCSV(data []byte, known func(string) bool) (csvPlan, []model.Host) {
	plan, hosts, err := parseHostCSV(data, known)
	if err != nil {
		// 调用方用 ParseHostCSVErr 才能看到错；这里返回空，宁可不写也不要写错。
		return csvPlan{Rows: []csvRow{}}, nil
	}
	return plan, hosts
}

// ParseHostCSVErr 同 ParseHostCSV，文件本身读不成时报错。
func ParseHostCSVErr(data []byte, known func(string) bool) (csvPlan, []model.Host, error) {
	return parseHostCSV(data, known)
}

func parseHostCSV(data []byte, known func(string) bool) (csvPlan, []model.Host, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // 列数自己数，为了一句「第几行列数对不上」而不是 csv 的泛泛一句
	r.LazyQuotes = false
	records, err := r.ReadAll()
	if err != nil {
		return csvPlan{}, nil, fmt.Errorf("CSV 读不成：%w", err)
	}
	if len(records) == 0 {
		return csvPlan{Rows: []csvRow{}}, nil, fmt.Errorf("CSV 是空的，连表头都没有")
	}
	header := records[0]
	snCol, archCol, fwCol := -1, -1, -1
	attrCols := map[int]string{}
	for i, name := range header {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "sn":
			snCol = i
		case "arch":
			archCol = i
		case "firmware":
			fwCol = i
		default:
			attrCols[i] = strings.TrimSpace(name)
		}
	}
	if snCol < 0 {
		return csvPlan{}, nil, fmt.Errorf("表头里没有 sn 一列：%v", header)
	}
	width := len(header)
	plan := csvPlan{Rows: []csvRow{}}
	var hosts []model.Host
	seen := map[string]int{} // 规范化后的 SN → 第一次出现的行号
	for i, rec := range records[1:] {
		row := i + 2 // 操作员看到的行号
		if len(rec) != width {
			return csvPlan{}, nil, fmt.Errorf("第 %d 行有 %d 列，表头有 %d 列", row, len(rec), width)
		}
		sn := rec[snCol]
		normalized, ps := model.NormalizeSN(sn)
		if len(ps) > 0 {
			plan.Rows = append(plan.Rows, csvRow{Row: row, SN: sn, Action: csvError, Problems: ps})
			continue
		}
		h := model.Host{SN: normalized}
		if archCol >= 0 {
			h.Arch = catalog.Arch(strings.TrimSpace(rec[archCol]))
		}
		if fwCol >= 0 {
			h.Firmware = catalog.Firmware(strings.TrimSpace(rec[fwCol]))
		}
		for col, key := range attrCols {
			if h.Attrs == nil {
				h.Attrs = map[string]string{}
			}
			h.Attrs[key] = strings.TrimSpace(rec[col])
		}
		problems := h.Validate()
		if first, ok := seen[normalized]; ok {
			problems = append([]model.Problem{model.NewProblem("host_duplicate_in_batch",
				model.Subject{Kind: "host", ID: normalized})}, problems...)
			_ = first
		} else {
			seen[normalized] = row
		}
		if len(problems) > 0 {
			plan.Rows = append(plan.Rows, csvRow{Row: row, SN: normalized, Action: csvError, Problems: problems})
			continue
		}
		action := csvCreate
		if known != nil && known(normalized) {
			action = csvUpdate
		}
		plan.Rows = append(plan.Rows, csvRow{Row: row, SN: normalized, Action: action})
		hosts = append(hosts, h)
	}
	plan.Total = len(plan.Rows)
	return plan, hosts, nil
}

// atoiOr 解析查询参数里的整数，空串或解析不了用 def。
func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}
```

`csv.go` 里 `_ = first` 那行删掉，`first` 改成 `_`：`if _, ok := seen[normalized]; ok {`。

- [ ] **Step 4: 实现主机端点**

Create `internal/console/hosts.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hostRequest 是登记与修改主机的请求体。SN 必须已经是规范化之后的写法（C6）。
type hostRequest struct {
	SN       string            `json:"sn"`
	Arch     catalog.Arch      `json:"arch"`
	Firmware catalog.Firmware `json:"firmware"`
	Attrs    map[string]string `json:"attrs"`
	Rev      int64             `json:"rev"`
}

// hostResponse 是一台主机对外的形状。属性一律给 map，没有属性时是 {} 不是 null：第 6 层的
// 表单直接遍历它。
type hostResponse struct {
	SN       string            `json:"sn"`
	Arch     catalog.Arch      `json:"arch"`
	Firmware catalog.Firmware `json:"firmware"`
	Attrs    map[string]string `json:"attrs"`
	Rev      int64             `json:"rev"`
}

// hostPage 是一页主机。
type hostPage struct {
	Items []hostResponse `json:"items"`
	Next  string         `json:"next"`
}

func toHostResponse(h model.Host) hostResponse {
	attrs := h.Attrs
	if attrs == nil {
		attrs = map[string]string{}
	}
	return hostResponse{SN: h.SN, Arch: h.Arch, Firmware: h.Firmware, Attrs: attrs, Rev: h.Rev}
}

// hostRoutes 是主机的六个端点。导入单独一条：它按 dryRun 分两步（总 spec §5.3）。
func (s *server) hostRoutes() []route {
	return []route{
		{"GET", "/console/v1/hosts", PermHostRead, "", "主机", "列出主机", nil, hostPage{}, s.listHosts},
		{"POST", "/console/v1/hosts", PermHostWrite, "host.create", "主机", "登记主机", hostRequest{}, hostResponse{}, s.createHost},
		{"POST", "/console/v1/hosts/import", PermHostWrite, "host.import", "主机", "CSV 批量导入主机", nil, csvPlan{}, s.importHosts},
		{"GET", "/console/v1/hosts/{sn}", PermHostRead, "", "主机", "查一台主机", nil, hostResponse{}, s.getHost},
		{"PUT", "/console/v1/hosts/{sn}", PermHostWrite, "host.update", "主机", "修改主机", hostRequest{}, hostResponse{}, s.updateHost},
		{"DELETE", "/console/v1/hosts/{sn}", PermHostWrite, "host.delete", "主机", "删除主机", nil, nil, s.deleteHost},
	}
}

func (s *server) listHosts(c *gin.Context) {
	q := store.HostQuery{
		SNPrefix:  c.Query("prefix"),
		AttrKey:   c.Query("attr_key"),
		AttrValue: c.Query("attr_value"),
		Cursor:    c.Query("cursor"),
		Limit:     atoiOr(c.Query("limit"), 0),
	}
	page, ok := Get(c, func() (store.Page[model.Host], error) {
		return s.deps.Hosts.ListHosts(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]hostResponse, 0, len(page.Items))
	for _, h := range page.Items {
		items = append(items, toHostResponse(h))
	}
	Ok(c, hostPage{Items: items, Next: page.Next})
}

func (s *server) getHost(c *gin.Context) {
	h, ok := Get(c, func() (model.Host, error) {
		return s.deps.Hosts.GetHost(c.Request.Context(), c.Param("sn"))
	})
	if !ok {
		return
	}
	Ok(c, toHostResponse(h))
}

func (s *server) createHost(c *gin.Context) {
	var req hostRequest
	if !Bind(c, &req) {
		return
	}
	created, ok := Write(c, func() (model.Host, error) {
		return s.deps.Hosts.CreateHost(c.Request.Context(), hostOf(req), s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	Ok(c, toHostResponse(created))
}

func (s *server) updateHost(c *gin.Context) {
	var req hostRequest
	if !Bind(c, &req) {
		return
	}
	// 按路径里的 SN 找行：请求体里的 SN 改了就是另一台主机
	req.SN = c.Param("sn")
	updated, ok := Write(c, func() (model.Host, error) {
		return s.deps.Hosts.UpdateHost(c.Request.Context(), hostOf(req), s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	Ok(c, toHostResponse(updated))
}

func (s *server) deleteHost(c *gin.Context) {
	if _, ok := Write(c, func() (struct{}, error) {
		return struct{}{}, s.deps.Hosts.DeleteHost(c.Request.Context(), c.Param("sn"),
			atoiOr(c.Query("rev"), 0), s.auditFrom(c, ""))
	}); !ok {
		return
	}
	Ok(c, nil)
}

// importHosts 是 CSV 批量导入，分两步（总 spec §5.3）：带 dryRun 只解析并逐行标出新建、更新
// 或错误，什么都不写；不带就真导入。无状态，客户端把同一个文件发两遍。
func (s *server) importHosts(c *gin.Context) {
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		Fail(c, statusBadRequest, nil, "请求体读不出来")
		return
	}
	plan, hosts, err := ParseHostCSVErr(data, func(sn string) bool {
		_, err := s.deps.Hosts.GetHost(c.Request.Context(), sn)
		return err == nil
	})
	if err != nil {
		Fail(c, statusBadRequest, nil, err.Error())
		return
	}
	if c.Query("dryRun") == "true" {
		Ok(c, plan)
		return
	}
	imported, ok := Write(c, func() ([]model.Host, error) {
		return s.deps.Hosts.ImportHosts(c.Request.Context(), hosts, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	items := make([]hostResponse, 0, len(imported))
	for _, h := range imported {
		items = append(items, toHostResponse(h))
	}
	Ok(c, hostPage{Items: items})
}

// hostOf 把请求体转成 model.Host。
func hostOf(req hostRequest) model.Host {
	return model.Host{SN: req.SN, Arch: req.Arch, Firmware: req.Firmware, Attrs: req.Attrs, Rev: req.Rev}
}
```

`hosts.go` 的 import 加 `"io"` 与 `"github.com/gin-gonic/gin"`。

`deleteHost` 的 `Write[struct{}]` 写法别扭，改用 `Get` 同款的错误分流：

```go
func (s *server) deleteHost(c *gin.Context) {
	err := s.deps.Hosts.DeleteHost(c.Request.Context(), c.Param("sn"),
		atoiOr(c.Query("rev"), 0), s.auditFrom(c, ""))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}
```

`hostBodyRev` 在测试里定义：

```go
func hostBodyRev(sn, arch, fw string, rev int64) string {
	b, _ := json.Marshal(hostRequest{SN: sn, Arch: catalog.Arch(arch),
		Firmware: catalog.Firmware(fw), Rev: rev})
	return string(b)
}
```

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),,
		s.userRoutes(),,
		s.hostRoutes(),
	)
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/console
git commit -m "feat(console): 主机的登记、修改、删除与 CSV 两步导入"
```

---

---

## Task 7：ISO 与来源接缝

**Files:**
- Create: `internal/console/isos.go`
- Modify: `internal/console/routes.go`
- Modify: `internal/console/server_test.go`（`stubISOSource` 换成记调用的 mock）
- Test: `internal/console/isos_test.go`

**Interfaces:**
- Consumes: Task 4 的 `ISOSource`、`SourceKind`、`Deps`/`Bind`/`Get`/`Write`/`Judge`/`auditFrom`/`clock`；Task 5 的夹具；Task 6 的 `atoiOr`
- Produces:
  - `isoRequest`、`isoSourceRequest`、`isoResponse`、`isoPage`、`isoProgress`（OpenAPI 的 schema 名）
  - `(*server).isoRoutes() []route`
  - `console.ParseContentRange(header string) (start, end, total int64, ok bool)`

**上传分片不审计**（路由表的 `audit` 留空）：总 spec §6.2 第 8 条列的审计操作里没有它，一张 4 GB 的 ISO 按 8 MiB 分片是五百多片。`Upload` 仍然收 `store.Audit`，为的是签名与第 4 层的 `Sources` 一致；传进去的是零值，第 4 层对零值不写。

**本任务的接缝**：`ISOSource` 的实现在第 4 层。这里换成一个记调用的内存 mock，测试因此能断言控制台真的把活儿交给了第 4 层、交下去的参数对，而不用等第 4 层存在（第 5 层设计 §1「不依赖第 4 层」）。`cmd/lober` 里的占位实现Task 14 补。

- [ ] **Step 1: 写失败的测试——记调用的 mock**

Modify `internal/console/server_test.go`，把 `stubISOSource` 换成：

```go
// mockISOSource 记下调了它的哪些方法、参数是什么。测试因此能断言控制台把活儿交给了第 4 层，
// 以及交下去的参数对，而不用等第 4 层存在（第 5 层设计 §1）。
type mockISOSource struct {
	mu         sync.Mutex
	registered []string // isoID|kind|addr|size|sha256
	uploaded   []string // isoID|offset|字节数
	cancelled  []string
	retried    []string
	reimported []string

	staging, artifacts int64
	freeErr            error
	regErr             error
	uploadErr          error
}

func (m *mockISOSource) Register(ctx context.Context, iso model.ISO, kind, addr, role string, size int64, sha256 string, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered = append(m.registered, fmt.Sprintf("%s|%s|%s|%s|%d|%s|%s|%s",
		iso.ID, kind, addr, role, size, sha256, a.Action, a.Actor))
	return m.regErr
}

func (m *mockISOSource) Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a store.Audit) error {
	n, _ := io.Copy(io.Discard, r)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploaded = append(m.uploaded, fmt.Sprintf("%s|%s|%d|%d", id, role, offset, n))
	return m.uploadErr
}

func (m *mockISOSource) Cancel(ctx context.Context, id string, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelled = append(m.cancelled, id+"|"+a.Action+"|"+a.Actor)
	return nil
}

func (m *mockISOSource) Retry(ctx context.Context, id string, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retried = append(m.retried, id+"|"+a.Action+"|"+a.Actor)
	return nil
}

func (m *mockISOSource) Reimport(ctx context.Context, id string, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reimported = append(m.reimported, id+"|"+a.Action+"|"+a.Actor)
	return nil
}

func (m *mockISOSource) FreeSpace(ctx context.Context) (int64, int64, error) {
	return m.staging, m.artifacts, m.freeErr
}

// mockHub 记下发布过哪些通知。
type mockHub struct {
	mu    sync.Mutex
	fired []string
}

func (h *mockHub) Publish(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fired = append(h.fired, taskID)
}

func (h *mockHub) Subscribe(taskID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	return ch, func() {}
}
```

`server_test.go` 的 import 加 `"context"`、`"fmt"`、`"io"`、`"sync"`。`stubTaskControl` 留着（Task 12 换成记调用的）。

Modify `internal/console/harness_test.go`，`newStoreServer` 多返回两个替身：

```go
// newStoreServer 建一个连真实 PostgreSQL 的控制台。每个顶层测试开一个仓储，不每个子测试开：
// 第 2 层评审提过的连接数上限在这里同样适用（总 spec §6.4）。pgtest.Open 自己在测试结束时
// 关仓储、删 schema。
func newStoreServer(t *testing.T) (*console.Server, store.Store, *mockISOSource, *mockHub) {
	t.Helper()
	st := pgtest.Open(t)
	src := &mockISOSource{staging: 1 << 40, artifacts: 1 << 40}
	hub := &mockHub{}
	gin.SetMode(gin.TestMode)
	s := console.New(console.Deps{
		Hosts: st, ISOs: st, Hooks: st, Templates: st, Tasks: st, Events: st, Audit: st, Users: st,
		Clock:      func() int64 { return currentNow() },
		SessionTTL: 12 * 3600,
		ISOSource:  src,
		TaskCtl:    stubTaskControl{},
		Notifier:   hub,
	})
	return s, st, src, hub
}
```

**Task 5、Task 6 里已经写好的用例要把返回值改成四个**（`s, st := newStoreServer(t)` → `s, st, _, _ := newStoreServer(t)`）。这是一次机械改动，逐个文件跑 `go vet` 就能找出漏改的地方。

Run: `go test ./internal/console/ -v`
Expected: FAIL，`undefined: newStoreServer` 的返回值个数不对付（Task 5、6 的用例还没改）。

- [ ] **Step 2: 写失败的测试——ISO 端点**

Create `internal/console/isos_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// readyISO 直接经仓储建一个可用的 ISO，省得每个用例都走一遍导入。
func readyISO(t *testing.T, st store.Store, id, name string, series catalog.Series) model.ISO {
	t.Helper()
	iso, err := st.CreateISO(t.Context(), model.ISO{Name: name, Family: series.Family(), Series: series},
		store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建 ISO：%v", err)
	}
	iso.Version = "9.6"
	iso.Arch = catalog.ArchX8664
	iso.State = model.ISOReady
	ready, err := st.UpdateISO(t.Context(), iso)
	if err != nil {
		t.Fatalf("置为可用：%v", err)
	}
	return ready
}

// 登记一个远程链接的 ISO：先建记录，再把来源交给第 4 层（总 spec §5.1 第 1、2 步）。
func Test登记ISO(t *testing.T) {
	s, _, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, body := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"Rocky 9.6","family":"linux","series":"rocky","sources":`+
			`[{"kind":"url","addr":"http://mirror/rocky-9.6.iso","sha256":"`+
			strings.Repeat("a", 64)+`"}]}`, token)
	if code != 200 {
		t.Fatalf("登记返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			State  string `json:"state"`
			Series string `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if got.Data.Name != "Rocky 9.6" || got.Data.Series != "rocky" {
		t.Errorf("登记出来的是 %+v", got.Data)
	}
	if got.Data.State != "acquiring" {
		t.Errorf("状态是 %q，建出来应是 acquiring", got.Data.State)
	}
	// 来源真的交给了第 4 层，参数对
	if len(src.registered) != 1 {
		t.Fatalf("Register 被调了 %d 次，应为 1 次", len(src.registered))
	}
	// mock 记下的顺序是 isoID|kind|addr|role|size|sha256|动作|操作者
	want := got.Data.ID + "|url|http://mirror/rocky-9.6.iso||0|" + strings.Repeat("a", 64) +
		"|iso.create|root"
	if src.registered[0] != want {
		t.Errorf("Register 的参数是 %q，应为 %q", src.registered[0], want)
	}
}

// 一个 ISO 可以有多条来源：主介质加该系要求的附加来源。Debian 要同版本的官方 netboot 包
// （netinst ISO 里的 initrd 是光驱版，不能网络装机），它的 role 是 netboot。
func Test登记ISO带附加来源(t *testing.T) {
	s, _, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"Debian 13.6","family":"linux","series":"debian","sources":[`+
			`{"kind":"url","addr":"http://mirror/debian-13.6.iso"},`+
			`{"kind":"url","role":"netboot","addr":"http://mirror/debian-13.6-netboot.iso"}]}`, token)
	if code != 200 {
		t.Fatalf("登记返回 %d：%s", code, got)
	}
	if len(src.registered) != 2 {
		t.Fatalf("Register 被调了 %d 次，两条来源应各一次", len(src.registered))
	}
	// 第一条是主介质（role 为空），第二条带 netboot
	if !strings.Contains(src.registered[0], "|url|http://mirror/debian-13.6.iso||") {
		t.Errorf("主介质的参数是 %q，role 应为空", src.registered[0])
	}
	if !strings.Contains(src.registered[1], "|netboot|") {
		t.Errorf("附加来源的参数是 %q，应带 role", src.registered[1])
	}
	// 两条来源各写一条审计
	if !strings.Contains(got, "iso.create") {
		t.Errorf("响应里没有 iso.create：%s", got)
	}
}

// 主介质只能有一条：role 为空的给两条，分不出哪条是主介质。
func Test登记ISO主介质只能一条(t *testing.T) {
	s, _, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"x","family":"linux","series":"rocky","sources":[`+
			`{"kind":"url","addr":"http://a/x.iso"},`+
			`{"kind":"url","addr":"http://b/x.iso"}]}`, token)
	if code != 400 {
		t.Errorf("返回 %d，应为 400：%s", code, got)
	}
	if len(src.registered) != 0 {
		t.Error("入参不合法还把来源交给了第 4 层")
	}
	// 一条都没有也不行
	code, _ = doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"x","family":"linux","series":"rocky","sources":[]}`, token)
	if code != 400 {
		t.Errorf("没有来源返回 %d，应为 400", code)
	}
}

// 系不属于族报 400，并说是哪一项不对（C2）。
func Test登记ISO系不属于族(t *testing.T) {
	s, _, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"装错","family":"linux","series":"windows-server","sources":`+
			`[{"kind":"url","addr":"http://mirror/x.iso"}]}`, token)
	if code != 400 {
		t.Errorf("返回 %d，应为 400：%s", code, got)
	}
	if !strings.Contains(got, "windows-server") || !strings.Contains(got, "linux") {
		t.Errorf("说明是 %s，应指出系与族", got)
	}
}

// 来源种类不合法报 400。
func Test登记ISO来源不合法(t *testing.T) {
	s, _, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, kind := range []string{"ftp", "URL", "", "magnet"} {
		code, got := doWith(t, s, "POST", "/console/v1/isos",
			`{"name":"x","family":"linux","series":"rocky","sources":`+
				`[{"kind":"`+kind+`","addr":"http://mirror/x.iso"}]}`, token)
		if code != 400 {
			t.Errorf("来源种类 %q 返回 %d，应为 400：%s", kind, code, got)
		}
	}
}

// 磁盘余量不够就拒绝，并说明还差多少（总 spec §5.1 第 1 步、第 4 层设计 §4.5）。
func Test登记ISO磁盘不够就拒绝(t *testing.T) {
	s, _, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	src.staging = 100 // 只賸 100 字节
	code, got := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"Rocky 9.6","family":"linux","series":"rocky","sources":`+
			`[{"kind":"upload","addr":"rocky.iso","size":1000000}]}`, token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "999900") {
		t.Errorf("说明是 %s，应写出还差多少字节", got)
	}
	if len(src.registered) != 0 {
		t.Error("余量不够还把来源交给了第 4 层")
	}
}

// 第 4 层登记失败：原样带上它的说明，操作员才知道改什么。
func Test登记ISO来源失败(t *testing.T) {
	s, _, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	src.regErr = errString("回环地址不允许")
	code, got := doWith(t, s, "POST", "/console/v1/isos",
		`{"name":"x","family":"linux","series":"rocky","sources":`+
			`[{"kind":"url","addr":"http://127.0.0.1/x.iso"}]}`, token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "回环地址不允许") {
		t.Errorf("说明是 %s，应带上第 4 层的原话", got)
	}
}

// 列 ISO 按名字的字节序，可按族、系、状态过滤。
func Test列ISO(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	readyISO(t, st, "iso-2", "Debian 13.6", catalog.SeriesDebian)
	code, body := doWith(t, s, "GET", "/console/v1/isos", "", token)
	if code != 200 {
		t.Fatalf("列 ISO 返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Fatalf("列出 %d 个，应为 2 个", len(got.Data.Items))
	}
	// 字节序：D < R
	if got.Data.Items[0].Name != "Debian 13.6" || got.Data.Items[1].Name != "Rocky 9.6" {
		t.Errorf("顺序是 %s、%s，应按名字的字节序", got.Data.Items[0].Name, got.Data.Items[1].Name)
	}
	// 按系过滤
	code, body = doWith(t, s, "GET", "/console/v1/isos?series=rocky", "", token)
	if code != 200 {
		t.Fatalf("按系过滤返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 1 || got.Data.Items[0].Name != "Rocky 9.6" {
		t.Errorf("按 rocky 过滤出 %+v", got.Data.Items)
	}
}

// 取一个 ISO：状态、版本、架构、制品、提供的集合都在。进度只回粗状态（第 4 层设计 §12：
// 逐来源、逐分片的进度等 store.ISOSources 落地后补 GET /isos/{id}/progress）。
func Test取ISO(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	code, body := doWith(t, s, "GET", "/console/v1/isos/"+iso.ID, "", token)
	if code != 200 {
		t.Fatalf("取 ISO 返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Arch    string `json:"arch"`
			State   string `json:"state"`
			Offers  map[string][]struct {
				Value string `json:"value"`
			} `json:"offers"`
			Artifacts map[string]string `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if got.Data.Version != "9.6" || got.Data.Arch != "x86_64" || got.Data.State != "ready" {
		t.Errorf("取回来的是 %+v", got.Data)
	}
	if got.Data.Offers == nil || got.Data.Artifacts == nil {
		t.Errorf("offers 或 artifacts 是 null，应为 {}：%s", body)
	}
}

// 废弃只能对可用的 ISO 做，报 409 说清原因（C26）。
func Test废弃ISO(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	code, got := doWith(t, s, "POST", "/console/v1/isos/"+iso.ID+"/deprecate?rev=1", "", token)
	if code != 200 {
		t.Fatalf("废弃返回 %d：%s", code, got)
	}
	back, err := st.GetISO(t.Context(), iso.ID)
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.ISODeprecated {
		t.Errorf("状态是 %q，应为 deprecated", back.State)
	}
	// 再废弃一次报 409
	code, got = doWith(t, s, "POST", "/console/v1/isos/"+iso.ID+"/deprecate?rev=2", "", token)
	if code != 409 {
		t.Errorf("再废弃一次返回 %d，应为 409：%s", code, got)
	}
}

// 取消、重试、重新导入都只是把活儿交给第 4 层，控制台不自己碰字节（第 4 层设计 §12）。
func Test取消重试重新导入交给第4层(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   func(id string) string
		want   func(m *mockISOSource) int
	}{
		{"取消", "POST", func(id string) string { return "/console/v1/isos/" + id + "/cancel" },
			func(m *mockISOSource) int { return len(m.cancelled) }},
		{"重试获取", "POST", func(id string) string { return "/console/v1/isos/" + id + "/retry" },
			func(m *mockISOSource) int { return len(m.retried) }},
		{"重新导入", "POST", func(id string) string { return "/console/v1/isos/" + id + "/reimport" },
			func(m *mockISOSource) int { return len(m.reimported) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, st, src, _ := newStoreServer(t)
			token := adminToken(t, s, st)
			iso, err := st.CreateISO(t.Context(), model.ISO{Name: "x", Family: "linux", Series: "rocky"},
				store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
			if err != nil {
				t.Fatalf("建 ISO：%v", err)
			}
			code, got := doWith(t, s, c.method, c.path(iso.ID), "", token)
			if code != 200 {
				t.Fatalf("%s 返回 %d：%s", c.name, code, got)
			}
			if c.want(src) != 1 {
				t.Errorf("第 4 层没收到这一次 %s", c.name)
			}
		})
	}
}

// 分片上传：Content-Range 给出偏移，控制台把它交给第 4 层，自己不落盘。
func Test分片上传(t *testing.T) {
	s, st, src, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	iso, err := st.CreateISO(t.Context(), model.ISO{Name: "x", Family: "linux", Series: "rocky"},
		store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建 ISO：%v", err)
	}
	req := httptest.NewRequest("PUT", "/console/v1/isos/"+iso.ID+"/content",
		strings.NewReader("前半段"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Range", "bytes 0-8/19")
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("上传返回 %d：%s", w.Code, w.Body.String())
	}
	if len(src.uploaded) != 1 {
		t.Fatalf("Upload 被调了 %d 次，应为 1 次", len(src.uploaded))
	}
	// mock 记下的顺序是 isoID|role|offset|字节数
	if src.uploaded[0] != iso.ID+"||0|9" {
		t.Errorf("Upload 的参数是 %q，应为 %s||0|9", src.uploaded[0], iso.ID)
	}
}

// Content-Range 读不出偏移时报 400，不猜。
func Test分片上传的ContentRange不合法(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	iso, err := st.CreateISO(t.Context(), model.ISO{Name: "x", Family: "linux", Series: "rocky"},
		store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建 ISO：%v", err)
	}
	for _, h := range []string{"", "bytes", "bytes 0-", "bytes -8/19", "bytes 8-0/19", "chunk 0-8/19", "bytes 0-8"} {
		req := httptest.NewRequest("PUT", "/console/v1/isos/"+iso.ID+"/content", strings.NewReader("x"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Range", h)
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("Content-Range %q 返回 %d，应为 400", h, w.Code)
		}
	}
}

// 登记 ISO 写一条审计，动作是 iso.create。
func Test登记ISO写审计(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	body := `{"name":"Rocky 9.6","family":"linux","series":"rocky",` +
		`"sources":[{"kind":"url","addr":"http://mirror/x.iso"}]}`
	code, got := doWith(t, s, "POST", "/console/v1/isos", body, token)
	if code != 200 {
		t.Fatalf("登记返回 %d：%s", code, got)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	found := false
	for _, e := range page.Items {
		if e.TargetKind == "iso" && e.Action == "iso.create" && e.Actor == "root" {
			found = true
		}
	}
	if !found {
		t.Error("没有 iso.create 的审计记录")
	}
}

// 删 ISO 报 ErrReferenced 的用例在 Task 12 补：先得有任务。这里先钉没引用的能删。
func Test删ISO(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	code, got := doWith(t, s, "DELETE", "/console/v1/isos/"+iso.ID+"?rev=1", "", token)
	if code != 200 {
		t.Fatalf("删 ISO 返回 %d：%s", code, got)
	}
	if _, err := st.GetISO(t.Context(), iso.ID); err == nil {
		t.Error("删之后还读得到")
	}
}
```

`isos_test.go` 的 import 加 `"context"`、`"net/http/httptest"`，并定义：

```go
// errString 是一个只有错误信息的 error，用来冒充第 4 层的失败。
type errString string

func (e errString) Error() string { return string(e) }
```

Run: `go test ./internal/console/ -run 'Test登记ISO|Test列ISO|Test取ISO|Test废弃ISO|Test取消重试|Test分片上传|Test删ISO' -v`
Expected: FAIL，`undefined: s.isoRoutes`、`undefined: mockISOSource`。

- [ ] **Step 3: 写失败的测试——Content-Range 解析**

接着写在 `internal/console/isos_test.go`：

```go
// Content-Range 的写法是 bytes <起>-<止>/<总长>，控制台只取起始偏移交给第 4 层。
func Test解析ContentRange(t *testing.T) {
	cases := []struct {
		header         string
		start, total   int64
		ok             bool
	}{
		{"bytes 0-8/19", 0, 19, true},
		{"bytes 9-18/19", 9, 19, true},
		{"bytes 0-1048575/1048576", 0, 1048576, true},
		{"", 0, 0, false},
		{"bytes 0-8", 0, 0, false},
		{"bytes 0-8/", 0, 0, false},
		{"bytes -8/19", 0, 0, false},
		{"bytes 8-0/19", 0, 0, false},
		{"chunk 0-8/19", 0, 0, false},
		{"bytes a-b/19", 0, 0, false},
		{"bytes 0-8/x", 0, 0, false},
		{"bytes 0-8/*", 0, 0, true}, // 总长未知：上传分片允许，第 4 层自己收尾
	}
	for _, c := range cases {
		start, _, total, ok := console.ParseContentRange(c.header)
		if ok != c.ok {
			t.Errorf("Content-Range %q 的 ok 是 %v，应为 %v", c.header, ok, c.ok)
			continue
		}
		if ok && (start != c.start || total != c.total) {
			t.Errorf("Content-Range %q 解出 start=%d total=%d，应为 %d、%d",
				c.header, start, total, c.start, c.total)
		}
	}
}
```

Run: `go test ./internal/console/ -run 'Test解析ContentRange' -v`
Expected: FAIL，`undefined: console.ParseContentRange`。

- [ ] **Step 4: 实现 ISO 端点**

Create `internal/console/isos.go`：

```go
package console

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// isoRequest 是登记一个 ISO 的请求体：族、系、名字，加一个来源。字节此时可能还没到齐（C2），
// 所以版本、架构、制品都还不填，等第 4 层导入之后才有。
// isoRequest 是登记一个 ISO 的请求体：族、系、名字，加它的来源。来源是列表：一条主介质，
// 加该系要求的附加来源（目前只有 Debian 要同版本的官方 netboot 包）。字节此时可能还没到齐
// （C2），所以版本、架构、制品都还不填，等第 4 层导入之后才有。
type isoRequest struct {
	Name    string         `json:"name"`
	Family  catalog.Family `json:"family"`
	Series  catalog.Series `json:"series"`
	Sources []isoSourceReq `json:"sources"`
}

// isoSourceReq 是一条来源的请求体。addr 对 url 是链接，对 path 是服务器本地路径，对 upload
// 是文件名；size 只对 upload 有意义，0 表示未知；role 为空串是主介质，附加来源传 catalog 给的
// 附加来源名（Debian 是 netboot）。哪条附加来源是必须的由第 4 层在导入时判（C3），控制台只
// 保证空 role 的至多一条。
type isoSourceReq struct {
	Kind   SourceKind `json:"kind"`
	Addr   string     `json:"addr"`
	Role   string     `json:"role"`
	Size   int64      `json:"size"`
	SHA256 string     `json:"sha256"`
}

// isoResponse 是一个 ISO 对外的形状。进度只回粗状态：逐来源、逐分片的进度等第 4 层的
// store.ISOSources 落地之后补 GET /isos/{id}/progress（第 4 层设计 §6、§12）。
type isoResponse struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Family    catalog.Family             `json:"family"`
	Series    catalog.Series             `json:"series"`
	Version   string                     `json:"version"`
	Arch      catalog.Arch               `json:"arch"`
	State     model.ISOState             `json:"state"`
	SHA256    string                     `json:"sha256"`
	Artifacts map[string]string          `json:"artifacts"`
	Offers    map[string][]model.Option  `json:"offers"`
	Rev       int64                      `json:"rev"`
}

// isoPage 是一页 ISO。
type isoPage struct {
	Items []isoResponse `json:"items"`
	Next  string        `json:"next"`
}

func toISOResponse(iso model.ISO) isoResponse {
	artifacts, offers := iso.Artifacts, iso.Offers
	if artifacts == nil {
		artifacts = map[string]string{}
	}
	if offers == nil {
		offers = map[string][]model.Option{}
	}
	return isoResponse{
		ID: iso.ID, Name: iso.Name, Family: iso.Family, Series: iso.Series,
		Version: iso.Version, Arch: iso.Arch, State: iso.State, SHA256: iso.SHA256,
		Artifacts: artifacts, Offers: offers, Rev: iso.Rev,
	}
}

// isoRoutes 是 ISO 的九个端点。登记来源、上传分片、取消、重试、重新导入都只是把活儿交给
// 第 4 层，控制台不自己碰字节（第 4 层设计 §12）。
func (s *server) isoRoutes() []route {
	return []route{
		{"GET", "/console/v1/isos", PermISORead, "", "ISO", "列出 ISO", nil, isoPage{}, s.listISOs},
		{"POST", "/console/v1/isos", PermISOWrite, "iso.create", "ISO", "登记一个 ISO 与它的来源", isoRequest{}, isoResponse{}, s.createISO},
		{"GET", "/console/v1/isos/{id}", PermISORead, "", "ISO", "取一个 ISO", nil, isoResponse{}, s.getISO},
		{"DELETE", "/console/v1/isos/{id}", PermISOWrite, "iso.delete", "ISO", "删除 ISO", nil, nil, s.deleteISO},
		{"POST", "/console/v1/isos/{id}/deprecate", PermISOWrite, "iso.deprecate", "ISO", "废弃 ISO", nil, nil, s.deprecateISO},
		{"POST", "/console/v1/isos/{id}/cancel", PermISOWrite, "iso.cancel", "ISO", "取消获取", nil, nil, s.cancelISO},
		{"POST", "/console/v1/isos/{id}/retry", PermISOWrite, "iso.retry", "ISO", "重试获取", nil, nil, s.retryISO},
		{"POST", "/console/v1/isos/{id}/reimport", PermISOWrite, "iso.reimport", "ISO", "重新导入", nil, nil, s.reimportISO},
		{"PUT", "/console/v1/isos/{id}/content", PermISOWrite, "", "ISO", "上传一段字节", nil, nil, s.uploadChunk},
	}
}

func (s *server) listISOs(c *gin.Context) {
	q := store.ISOQuery{
		Family: catalog.Family(c.Query("family")),
		Series: catalog.Series(c.Query("series")),
		Cursor: c.Query("cursor"),
		Limit:  atoiOr(c.Query("limit"), 0),
	}
	if st := c.Query("state"); st != "" {
		q.States = []model.ISOState{model.ISOState(st)}
	}
	page, ok := Get(c, func() (store.Page[model.ISO], error) {
		return s.deps.ISOs.ListISOs(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]isoResponse, 0, len(page.Items))
	for _, iso := range page.Items {
		items = append(items, toISOResponse(iso))
	}
	Ok(c, isoPage{Items: items, Next: page.Next})
}

func (s *server) getISO(c *gin.Context) {
	iso, ok := Get(c, func() (model.ISO, error) {
		return s.deps.ISOs.GetISO(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	Ok(c, toISOResponse(iso))
}

func (s *server) createISO(c *gin.Context) {
	var req isoRequest
	if !Bind(c, &req) {
		return
	}
	if len(req.Sources) == 0 {
		Fail(c, statusBadRequest, nil, "至少要有一条来源：主介质")
		return
	}
	main := 0 // 空 role 的条数：只能有一条
	var total int64
	for _, src := range req.Sources {
		if !src.Kind.Valid() {
			Fail(c, statusBadRequest, nil, "来源的种类只能是 url、upload 或 path")
			return
		}
		if src.Role == "" {
			main++
		} else if !model.ValidIdent(src.Role) {
			// 角色会成为 iso_sources 表的键，也会进制品路径的推断，先按标识符收
			Fail(c, statusBadRequest, nil, "附加来源的角色要是以小写字母开头的标识符")
			return
		}
		if src.Kind != SourceUpload && src.Addr == "" {
			Fail(c, statusBadRequest, nil, "来源的地址是空的")
			return
		}
		if src.Size > total {
			total = src.Size
		}
	}
	if main != 1 {
		Fail(c, statusBadRequest, nil, "只能有一条主介质：它的 role 为空串，附加来源要带角色")
		return
	}
	// 系必须属于族（C2）：族给错了，后面每一句说明都会指错方向
	if req.Family.Valid() && req.Series.Valid() && req.Series.Family() != req.Family {
		Fail(c, statusBadRequest, nil,
			fmt.Sprintf("系 %s 属于族 %s，不属于 %s", req.Series, req.Series.Family(), req.Family))
		return
	}
	// 能事先知道大小的，先查磁盘余量，不够就拒绝，并说明还差多少（总 spec §5.1 第 1 步）。
	// 几条来源里取最大的那个：都要落到同一个暂存区。
	if total > 0 {
		staging, _, err := s.deps.ISOSource.FreeSpace(c.Request.Context())
		if err != nil {
			Fail(c, statusUnprocessable, nil, "查暂存区余量没成功："+err.Error())
			return
		}
		if staging < total {
			Fail(c, statusUnprocessable, nil, fmt.Sprintf(
				"暂存区只剩 %d 字节，还差 %d 字节：先清一阵，或者把来源换成服务器本地路径",
				staging, total-staging))
			return
		}
	}
	created, ok := Write(c, func() (model.ISO, error) {
		return s.deps.ISOs.CreateISO(c.Request.Context(),
			model.ISO{Name: req.Name, Family: req.Family, Series: req.Series},
			s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	audit := s.auditFrom(c, "")
	for _, src := range req.Sources {
		if err := s.deps.ISOSource.Register(c.Request.Context(), created,
			string(src.Kind), src.Addr, src.Role, src.Size, src.SHA256, audit); err != nil {
			// 第 4 层的说明是给运维看的（「回环地址」「不在允许的目录里」），原样带上
			Fail(c, statusUnprocessable, nil, "登记来源没成功："+err.Error())
			return
		}
	}
	Ok(c, toISOResponse(created))
}

func (s *server) deleteISO(c *gin.Context) {
	err := s.deps.ISOs.DeleteISO(c.Request.Context(), c.Param("id"),
		atoiOr(c.Query("rev"), 0), s.auditFrom(c, ""))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

func (s *server) deprecateISO(c *gin.Context) {
	err := s.deps.ISOs.DeprecateISO(c.Request.Context(), c.Param("id"),
		atoiOr(c.Query("rev"), 0), s.auditFrom(c, ""))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

// cancelISO、retryISO、reimportISO 都只是把活儿交给第 4 层。第 4 层没就绪时装配点注入的
// 占位实现返回「第 4 层未实现」，那句话原样带给操作员。
func (s *server) cancelISO(c *gin.Context)   { s.callSource(c, "取消获取", (*mockISOSource)(nil)) }
func (s *server) retryISO(c *gin.Context)    { s.callSource(c, "重试获取", nil) }
func (s *server) reimportISO(c *gin.Context) { s.callSource(c, "重新导入", nil) }
```

上面三行的写法不对，改成各调各的：

```go
func (s *server) cancelISO(c *gin.Context) {
	if err := s.deps.ISOSource.Cancel(c.Request.Context(), c.Param("id"), s.auditFrom(c, "")); err != nil {
		sourceFailed(c, "取消获取", err)
		return
	}
	Ok(c, nil)
}

func (s *server) retryISO(c *gin.Context) {
	if err := s.deps.ISOSource.Retry(c.Request.Context(), c.Param("id"), s.auditFrom(c, "")); err != nil {
		sourceFailed(c, "重试获取", err)
		return
	}
	Ok(c, nil)
}

func (s *server) reimportISO(c *gin.Context) {
	if err := s.deps.ISOSource.Reimport(c.Request.Context(), c.Param("id"), s.auditFrom(c, "")); err != nil {
		sourceFailed(c, "重新导入", err)
		return
	}
	Ok(c, nil)
}

// uploadChunk 接收浏览器上传的一个分片。偏移从 Content-Range 来，控制台把它和字节一起交给
// 第 4 层，自己不落盘。
func (s *server) uploadChunk(c *gin.Context) {
	start, _, _, ok := ParseContentRange(c.GetHeader("Content-Range"))
	if !ok {
		Fail(c, statusBadRequest, nil,
			"Content-Range 要写成 bytes <起始>-<结束>/<总长>，比如 bytes 0-1048575/5242880")
		return
	}
	role := c.Query("role")
	// 上传一片不审计：总 spec §6.2 第 8 条列的是登记来源、取消、重试这些，没有它。一张 4 GB
	// 的 ISO 按 8 MiB 分片是五百多片，一片一行审计会把审计表淹掉。路由表因此不声明动作，
	// auditFrom 返回零值，第 4 层对零值不写。哪天上传要审计了，只改路由表那一格。
	if err := s.deps.ISOSource.Upload(c.Request.Context(), c.Param("id"), role, start, c.Request.Body,
		s.auditFrom(c, "")); err != nil {
		sourceFailed(c, "上传分片", err)
		return
	}
	Ok(c, nil)
}

// sourceFailed 把第 4 层的失败写成响应：它的说明是给运维看的，原样带上，操作员才知道改什么。
func sourceFailed(c *gin.Context, what string, err error) {
	Fail(c, statusUnprocessable, nil, what+"没成功："+err.Error())
}

// ParseContentRange 解出上传分片的 Content-Range：bytes <起始>-<结束>/<总长>。返回起始偏移与
// 总长；总长是 * 表示未知。解不出来时 ok 为 false，调用方报 400，不猜偏移。
func ParseContentRange(header string) (start, end, total int64, ok bool) {
	rest, found := strings.CutPrefix(header, "bytes ")
	if !found {
		return 0, 0, 0, false
	}
	rangePart, totalPart, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, 0, false
	}
	startPart, endPart, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, 0, false
	}
	var err error
	if start, err = strconv.ParseInt(strings.TrimSpace(startPart), 10, 64); err != nil || start < 0 {
		return 0, 0, 0, false
	}
	if end, err = strconv.ParseInt(strings.TrimSpace(endPart), 10, 64); err != nil || end < start {
		return 0, 0, 0, false
	}
	totalPart = strings.TrimSpace(totalPart)
	if totalPart == "*" {
		return start, end, -1, true // 总长未知
	}
	if total, err = strconv.ParseInt(totalPart, 10, 64); err != nil || total <= 0 || end >= total {
		return 0, 0, 0, false
	}
	return start, end, total, true
}
```

`isos.go` 的 import 去掉 `context`、`io`（没有直接用），保留 `fmt`、`strconv`、`strings`、`gin`、`catalog`、`model`、`store`。

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),,
		s.userRoutes(),,
		s.hostRoutes(),,
		s.isoRoutes(),
	)
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/console
git commit -m "feat(console): ISO 的登记、列表、废弃、删除与来源接缝的内存 mock"
```

---

---

## Task 8：hook 与模板的读取、保存、版本与派生

**Files:**
- Create: `internal/console/hooks.go`
- Create: `internal/console/templates.go`
- Modify: `internal/console/routes.go`
- Test: `internal/console/hooks_test.go`、`internal/console/templates_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Deps`/`Bind`/`Get`/`Write`/`Judge`/`auditFrom`/`clock`；Task 5 的夹具与 `adminToken`；Task 6 的 `atoiOr`
- Produces:
  - `hookResponse`、`hookRequest`、`hookPage`、`versionPage`、`templateResponse`、`templateRequest`、`templatePage`、`deriveRequest`（OpenAPI 的 schema 名）
  - `(*server).hookRoutes() []route`、`(*server).templateRoutes() []route`
  - `console.ProblemsOf(h model.Hook) []model.Problem`（跑激活判定，给草稿的编辑器标红用）

**保存与激活的分工**：草稿存得下的都能存（总 spec §3 总则），完备性只在激活时要求。所以 `PUT .../versions/{version}` 只查仓储的前提（能不能改这份草稿、文本存不存得下），不跑激活判定；激活判定在提交、审批和 `POST .../validate` 三处跑，问题原样带给编辑器。

- [ ] **Step 1: 写失败的测试——hook 的读取与保存**

Create `internal/console/hooks_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// sampleHookBody 是一个能通过激活判定的 kickstart 片段：只写必需部件 syslog 与 cleanup，
// 时机只用 kickstart 支持的。
const sampleHookBody = "{{lober:syslog}}\n{{lober:cleanup}}\n"

// seedHook 直接经仓储建一个 hook 版本 1 的草稿。
func seedHook(t *testing.T, st store.Store, id, name string) model.Hook {
	t.Helper()
	h, err := st.CreateHook(t.Context(), model.Hook{
		Name: name, Description: "示例", TimeoutSec: 60, Required: true,
		Params: []model.ParamDecl{{Name: "disk", Type: "diskpath", Label: "系统盘"}},
		Impls: []model.Impl{{Family: catalog.FamilyLinux, Series: catalog.SeriesOfFormat(catalog.FormatKickstart),
			Kind: model.ImplFragment, Body: sampleHookBody}},
	})
	if err != nil {
		t.Fatalf("建 hook %s：%v", id, err)
	}
	return h
}

func Test建hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	body := hookBody("lvm", "LVM 分区")
	code, got := doWith(t, s, "POST", "/console/v1/hooks", body, token)
	if code != 200 {
		t.Fatalf("建 hook 返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
			Name    string `json:"name"`
			State   string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if res.Data.Version != 1 || res.Data.State != "draft" {
		t.Errorf("建出来的是 %+v，应为版本 1 的草稿", res.Data)
	}
}

// 名字用了保留前缀就拒（C12）：lober: 只给内置内容。
func Test建hook名字用保留前缀(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	code, got := doWith(t, s, "POST", "/console/v1/hooks", hookBody("lober:mine", "我的"), token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "name_reserved") {
		t.Errorf("响应里没有 name_reserved：%s", got)
	}
}

// 名字不合法（带空格、中文）报 422，问题原样来自第 1 层（C12）。
func Test建hook名字不合法(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, name := range []string{"LVM 分区", "lvm 分区", "lvm/分区", ""} {
		code, got := doWith(t, s, "POST", "/console/v1/hooks", hookBody(name, "x"), token)
		if code != 422 {
			t.Errorf("名字 %q 返回 %d，应为 422：%s", name, code, got)
		}
		if !strings.Contains(got, "name_invalid") {
			t.Errorf("名字 %q 的响应里没有 name_invalid：%s", name, got)
		}
	}
}

// 正文存不下（含 NUL）报 422，问题是 text_not_storable。
func Test建hook正文存不下(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	body := `{"name":"lvm","description":"x","impls":[{"family":"linux",` +
		`"series":["rocky"],"kind":"fragment","body":"a\u0000b"}]}`
	code, got := doWith(t, s, "POST", "/console/v1/hooks", body, token)
	if code != 422 {
		t.Errorf("返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "text_not_storable") {
		t.Errorf("响应里没有 text_not_storable：%s", got)
	}
}

// 列 hook：每个身份一项，可按名字前缀与状态过滤，ApprovedOnly 给当前的已审批版本（C23）。
func Test列hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	seedHook(t, st, "h1", "lvm")
	seedHook(t, st, "h2", "network")
	code, body := doWith(t, s, "GET", "/console/v1/hooks", "", token)
	if code != 200 {
		t.Fatalf("列 hook 返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Fatalf("列出 %d 个身份，应为 2 个", len(got.Data.Items))
	}
	// 按名字的 key 排：lvm、network
	if got.Data.Items[0].Name != "lvm" || got.Data.Items[1].Name != "network" {
		t.Errorf("顺序是 %s、%s，应按名字的 key", got.Data.Items[0].Name, got.Data.Items[1].Name)
	}
	// 只列已审批的：两个都还没审批，应为空
	code, body = doWith(t, s, "GET", "/console/v1/hooks?approved=true", "", token)
	if code != 200 {
		t.Fatalf("按已审批过滤返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 0 {
		t.Errorf("按已审批过滤出 %d 个，都还没审批，应为 0 个", len(got.Data.Items))
	}
}

// 取一个版本、列一个身份的全部版本。
func Test取hook与列版本(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, body := doWith(t, s, "GET", "/console/v1/hooks/"+h.ID+"/versions/1", "", token)
	if code != 200 {
		t.Fatalf("取版本返回 %d：%s", code, body)
	}
	if !strings.Contains(body, "系统盘") {
		t.Errorf("响应里没有参数的显示名：%s", body)
	}
	code, body = doWith(t, s, "GET", "/console/v1/hooks/"+h.ID+"/versions", "", token)
	if code != 200 {
		t.Fatalf("列版本返回 %d：%s", code, body)
	}
	var got struct {
		Data []struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data) != 1 || got.Data[0].Version != 1 {
		t.Errorf("列出的版本是 %+v，应只有版本 1", got.Data)
	}
}

// 保存草稿：能改说明、参数、实现；不能改已审批的版本（C23）。
func Test保存hook草稿(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	body := hookBodyRev("lvm", "改过的说明", 1)
	code, got := doWith(t, s, "PUT", "/console/v1/hooks/"+h.ID+"/versions/1", body, token)
	if code != 200 {
		t.Fatalf("保存返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.Description != "改过的说明" {
		t.Errorf("说明是 %q，保存没生效", back.Description)
	}
	if back.Rev != h.Rev+1 {
		t.Errorf("Rev 是 %d，应为 %d", back.Rev, h.Rev+1)
	}
}

// 开新版本：复制来源的内容，版本号是分配过的最大号加 1，不重发删掉的号。
func Test开hook新版本(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions?from=1", "", token)
	if code != 200 {
		t.Fatalf("开新版本返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if res.Data.Version != 2 {
		t.Errorf("新版本是 %d，应为 2", res.Data.Version)
	}
	// 作者是当前用户，不是来源那份的作者
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 2})
	if err != nil {
		t.Fatalf("读回版本 2：%v", err)
	}
	if back.Author != "root" {
		t.Errorf("作者是 %q，应为当前用户 root", back.Author)
	}
}

// 改名是单独的操作，要审计（§4.2「名字放在身份上」）。
func Test改hook名(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, got := doWith(t, s, "PUT", "/console/v1/hooks/"+h.ID+"/name",
		`{"name":"logical-volume"}`, token)
	if code != 200 {
		t.Fatalf("改名返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.Name != "logical-volume" {
		t.Errorf("名字是 %q，改名没生效", back.Name)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	found := false
	for _, e := range page.Items {
		if e.TargetKind == "hook" && e.Action == "hook.rename" {
			found = true
		}
	}
	if !found {
		t.Error("改名没有留下审计记录")
	}
}

// 内置的只读：保存、改名、删除都报 ErrImmutable（C25）。
func Test内置hook只读(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	builtin := model.Hook{ID: "builtin-packages", Version: 1, Name: "lober:packages",
		State: model.VersionApproved, Builtin: true, Author: "lober", Approver: "lober",
		Description: "内置", TimeoutSec: 60,
		Params: []model.ParamDecl{{Name: "packages", Type: "word", List: true, Default: "curl"}},
		Impls: []model.Impl{{Family: catalog.FamilyLinux, Series: []catalog.Series{catalog.SeriesRocky},
			Kind: model.ImplFragment, Body: "{{lober:syslog}}\n{{lober:cleanup}}\n"}}}
	if err := st.EnsureBuiltinHook(t.Context(), builtin); err != nil {
		t.Fatalf("装载内置 hook：%v", err)
	}
	if code, _ := doWith(t, s, "PUT", "/console/v1/hooks/builtin-packages/name",
		`{"name":"lober:packages2"}`, token); code != 409 {
		t.Errorf("内置的改名返回 %d，应为 409", code)
	}
	if code, _ := doWith(t, s, "DELETE", "/console/v1/hooks/builtin-packages/versions/1?rev=1",
		"", token); code != 409 {
		t.Errorf("删内置的返回 %d，应为 409", code)
	}
}

// 删草稿：已审批与已废弃的不能删（C23），删的是最后一个版本时身份一起删。
func Test删hook草稿(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, got := doWith(t, s, "DELETE", "/console/v1/hooks/"+h.ID+"/versions/1?rev=1", "", token)
	if code != 200 {
		t.Fatalf("删草稿返回 %d：%s", code, got)
	}
	if _, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1}); err == nil {
		t.Error("删之后还读得到")
	}
	// 身份一起删了：再列不到它
	page, err := st.ListHooks(t.Context(), store.HookQuery{Limit: 500})
	if err != nil {
		t.Fatalf("列 hook：%v", err)
	}
	for _, item := range page.Items {
		if item.ID == h.ID {
			t.Error("身份没有一起删掉")
		}
	}
}

// hookBody 与 hookBodyRev 是打 hook 端点共用的请求体。
func hookBody(name, desc string) string {
	return hookBodyRev(name, desc, 0)
}

func hookBodyRev(name, desc string, rev int64) string {
	b, _ := json.Marshal(hookRequest{
		Name: name, Description: desc, Rev: rev, TimeoutSec: 60, Required: true,
		Params: []model.ParamDecl{{Name: "disk", Type: "diskpath", Label: "系统盘"}},
		Impls: []model.Impl{{Family: catalog.FamilyLinux, Series: []catalog.Series{catalog.SeriesRocky},
			Kind: model.ImplFragment, Body: sampleHookBody}},
	})
	return string(b)
}
```

`hookRequest` 在 Step 3 定义。

Run: `go test ./internal/console/ -run 'Test建hook|Test列hook|Test取hook|Test保存hook|Test开hook|Test改hook名|Test内置hook|Test删hook' -v`
Expected: FAIL，`undefined: hookRequest`、`undefined: s.hookRoutes`。

- [ ] **Step 2: 写失败的测试——模板的读取、保存与派生**

Create `internal/console/templates_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// sampleTemplateBody 是一个能通过激活判定的 rocky kickstart 正文：一个片段插槽、一个执行插槽，
// 必需部件各一次。
const sampleTemplateBody = "{{lober:syslog}}\n{{slot:分区}}\n" +
	"{{slot:装后配置@post-target}}\n{{lober:cleanup}}\n"

// seedTemplate 直接经仓储建一个模板版本 1 的草稿，装配表引用 lvm@1。
func seedTemplate(t *testing.T, st store.Store, hook model.Hook) model.Template {
	t.Helper()
	tpl, err := st.CreateTemplate(t.Context(), model.Template{
		Name: "rhel-base", Family: catalog.FamilyLinux,
		Series: []catalog.Series{catalog.SeriesRocky}, Body: sampleTemplateBody,
		Params: []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "node"}},
		Bindings: map[string]model.Binding{"hostname": {Source: model.BindingDefault, Editable: true}},
		Assembly: map[string][]model.Instance{
			"分区": {{HookID: hook.ID, HookVersion: 1}},
		},
	})
	if err != nil {
		t.Fatalf("建模板：%v", err)
	}
	return tpl
}

func Test建模板(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, got := doWith(t, s, "POST", "/console/v1/templates",
		templateBody("rhel-base", h.ID), token)
	if code != 200 {
		t.Fatalf("建模板返回 %d：%s", code, got)
	}
}

// 族和系可以先留空，没勾系的模板不能激活（C13）：存得下，只是激活判定报 impl_series_invalid。
func Test建模板可以留空族与系(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	body := `{"name":"draft-only","body":"` + strings.ReplaceAll(sampleTemplateBody, "\n", `\n`) +
		`","assembly":{"分区":[{"hookID":"` + h.ID + `","hookVersion":1}]}}`
	code, got := doWith(t, s, "POST", "/console/v1/templates", body, token)
	if code != 200 {
		t.Fatalf("留空族与系的模板返回 %d，应存得下：%s", code, got)
	}
}

// 派生：整份复制来源的内容，记下 DerivedFrom，名字规则同新建（C24）。
func Test派生模板(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/derive",
		`{"from":1,"name":"rhel-base-copy"}`, token)
	if code != 200 {
		t.Fatalf("派生返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			ID          string `json:"id"`
			DerivedFrom *struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"derivedFrom"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if res.Data.ID == tpl.ID {
		t.Error("派生应建一个新身份")
	}
	if res.Data.DerivedFrom == nil || res.Data.DerivedFrom.ID != tpl.ID || res.Data.DerivedFrom.Version != 1 {
		t.Errorf("派生来源是 %+v，应为 %s@1", res.Data.DerivedFrom, tpl.ID)
	}
}

// 保存草稿与开新版本，规则同 hook。
func Test保存模板草稿与开新版本(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	code, got := doWith(t, s, "PUT", "/console/v1/templates/"+tpl.ID+"/versions/1",
		templateBodyRev("rhel-base", h.ID, tpl.Rev), token)
	if code != 200 {
		t.Fatalf("保存返回 %d：%s", code, got)
	}
	code, got = doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions?from=1", "", token)
	if code != 200 {
		t.Fatalf("开新版本返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if res.Data.Version != 2 {
		t.Errorf("新版本是 %d，应为 2", res.Data.Version)
	}
}

// 列模板可按系过滤：向导第 3 步只列勾了该 ISO 的系的（总 spec §5.3 第 3 步）。
func Test列模板按系过滤(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	seedTemplate(t, st, h)
	code, body := doWith(t, s, "GET", "/console/v1/templates?series=rocky", "", token)
	if code != 200 {
		t.Fatalf("列模板返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 1 {
		t.Errorf("按 rocky 过滤出 %d 个，应为 1 个", len(got.Data.Items))
	}
	code, body = doWith(t, s, "GET", "/console/v1/templates?series=debian", "", token)
	if code != 200 {
		t.Fatalf("列模板返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 0 {
		t.Errorf("按 debian 过滤出 %d 个，应为 0 个", len(got.Data.Items))
	}
}

// 内置模板也只读。
func Test内置模板只读(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	builtin := model.Template{ID: "builtin-kickstart", Version: 1, Name: "lober:kickstart",
		State: model.VersionApproved, Builtin: true, Author: "lober", Approver: "lober",
		Family: catalog.FamilyLinux, Series: []catalog.Series{catalog.SeriesRocky},
		Body: sampleTemplateBody,
		Params: []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "lober-node"}},
		Bindings: map[string]model.Binding{"hostname": {Source: model.BindingDefault, Editable: true}},
		Assembly: map[string][]model.Instance{}}
	if err := st.EnsureBuiltinTemplate(t.Context(), builtin); err != nil {
		t.Fatalf("装载内置模板：%v", err)
	}
	if code, _ := doWith(t, s, "PUT", "/console/v1/templates/builtin-kickstart/name",
		`{"name":"lober:kickstart2"}`, token); code != 409 {
		t.Errorf("内置的改名返回 %d，应为 409", code)
	}
}

// templateBody 与 templateBodyRev 是打模板端点共用的请求体。
func templateBody(name, hookID string) string {
	return templateBodyRev(name, hookID, 0)
}

func templateBodyRev(name, hookID string, rev int64) string {
	b, _ := json.Marshal(templateRequest{
		Name: name, Rev: rev, Family: catalog.FamilyLinux,
		Series:   []catalog.Series{catalog.SeriesRocky},
		Body:     sampleTemplateBody,
		Params:   []model.ParamDecl{{Name: "hostname", Type: "hostname", Default: "node"}},
		Bindings: map[string]model.Binding{"hostname": {Source: model.BindingDefault, Editable: true}},
		Assembly: map[string][]model.Instance{"分区": {{HookID: hookID, HookVersion: 1}}},
	})
	return string(b)
}
```

Run: `go test ./internal/console/ -run 'Test建模板|Test派生模板|Test保存模板|Test列模板|Test内置模板' -v`
Expected: FAIL，`undefined: templateRequest`、`undefined: s.templateRoutes`。

- [ ] **Step 3: 实现 hook 端点**

Create `internal/console/hooks.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// hookRequest 是新建与保存 hook 的请求体。Rev 只在保存时用得上。
type hookRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	TimeoutSec  int               `json:"timeoutSec"`
	Required    bool              `json:"required"`
	Params      []model.ParamDecl `json:"params"`
	Impls       []model.Impl      `json:"impls"`
	Rev         int64             `json:"rev"`
}

// hookResponse 是一个 hook 版本对外的形状。Problems 是激活判定的结果：草稿没过判定也能读，
// 编辑器照它标红（总 spec §3 总则：完备性只在激活时要求）。
type hookResponse struct {
	ID          string            `json:"id"`
	Version     int               `json:"version"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	State       model.VersionState `json:"state"`
	Author      string            `json:"author"`
	Approver    string            `json:"approver"`
	Builtin     bool              `json:"builtin"`
	TimeoutSec  int               `json:"timeoutSec"`
	Required    bool              `json:"required"`
	Params      []model.ParamDecl `json:"params"`
	Impls       []model.Impl      `json:"impls"`
	Problems    []model.Problem   `json:"problems"`
	Rev         int64             `json:"rev"`
}

// hookPage 是一页 hook，每个身份一项。
type hookPage struct {
	Items []hookResponse `json:"items"`
	Next  string         `json:"next"`
}

// versionPage 是一个身份的全部版本，按版本号升序。
type versionPage struct {
	Items []hookResponse `json:"items"`
}

func toHookResponse(h model.Hook) hookResponse {
	return hookResponse{
		ID: h.ID, Version: h.Version, Name: h.Name, Description: h.Description,
		State: h.State, Author: h.Author, Approver: h.Approver, Builtin: h.Builtin,
		TimeoutSec: h.TimeoutSec, Required: h.Required,
		Params: h.Params, Impls: h.Impls,
		Problems: activation.Hook(h), Rev: h.Rev,
	}
}

// hookRoutes 是 hook 的端点。保存草稿不跑激活判定（Step 的说明），提交、审批与 validate 才跑。
func (s *server) hookRoutes() []route {
	return []route{
		{"GET", "/console/v1/hooks", PermHookRead, "", "hook", "列出 hook", nil, hookPage{}, s.listHooks},
		{"POST", "/console/v1/hooks", PermHookWrite, "hook.create", "hook", "新建 hook", hookRequest{}, hookResponse{}, s.createHook},
		{"GET", "/console/v1/hooks/{id}/versions", PermHookRead, "", "hook", "列一个 hook 的全部版本", nil, versionPage{}, s.listHookVersions},
		{"POST", "/console/v1/hooks/{id}/versions", PermHookWrite, "", "hook", "开一个新版本的草稿", nil, hookResponse{}, s.newHookVersion},
		{"GET", "/console/v1/hooks/{id}/versions/{version}", PermHookRead, "", "hook", "取一个 hook 版本", nil, hookResponse{}, s.getHook},
		{"PUT", "/console/v1/hooks/{id}/versions/{version}", PermHookWrite, "", "hook", "保存 hook 草稿", hookRequest{}, hookResponse{}, s.saveHookDraft},
		{"DELETE", "/console/v1/hooks/{id}/versions/{version}", PermHookWrite, "hook.delete_draft", "hook", "删除 hook 草稿", nil, nil, s.deleteHookDraft},
		{"PUT", "/console/v1/hooks/{id}/name", PermHookWrite, "hook.rename", "hook", "给 hook 改名", nameRequest{}, nil, s.renameHook},
	}
}

func (s *server) listHooks(c *gin.Context) {
	q := store.HookQuery{
		NamePrefix:   c.Query("prefix"),
		ApprovedOnly: c.Query("approved") == "true",
		Cursor:       c.Query("cursor"),
		Limit:        atoiOr(c.Query("limit"), 0),
	}
	if st := c.Query("state"); st != "" {
		q.States = []model.VersionState{model.VersionState(st)}
	}
	page, ok := Get(c, func() (store.Page[model.Hook], error) {
		return s.deps.Hooks.ListHooks(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]hookResponse, 0, len(page.Items))
	for _, h := range page.Items {
		items = append(items, toHookResponse(h))
	}
	Ok(c, hookPage{Items: items, Next: page.Next})
}

func (s *server) listHookVersions(c *gin.Context) {
	versions, ok := Get(c, func() ([]model.Hook, error) {
		return s.deps.Hooks.ListHookVersions(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	items := make([]hookResponse, 0, len(versions))
	for _, h := range versions {
		items = append(items, toHookResponse(h))
	}
	Ok(c, versionPage{Items: items})
}

func (s *server) getHook(c *gin.Context) {
	h, ok := Get(c, func() (model.Hook, error) {
		return s.deps.Hooks.GetHook(c.Request.Context(), hookKeyOf(c))
	})
	if !ok {
		return
	}
	Ok(c, toHookResponse(h))
}

func (s *server) createHook(c *gin.Context) {
	var req hookRequest
	if !Bind(c, &req) {
		return
	}
	h := hookOf(req)
	h.Author = userOf(c).Username // 作者是当前用户，审批人不能是作者（C22）
	created, ok := Write(c, func() (model.Hook, error) {
		return s.deps.Hooks.CreateHook(c.Request.Context(), h)
	})
	if !ok {
		return
	}
	Ok(c, toHookResponse(created))
}

func (s *server) saveHookDraft(c *gin.Context) {
	var req hookRequest
	if !Bind(c, &req) {
		return
	}
	cur, ok := Get(c, func() (model.Hook, error) {
		return s.deps.Hooks.GetHook(c.Request.Context(), hookKeyOf(c))
	})
	if !ok {
		return
	}
	// 按路径里的 ID 与版本找行：请求体里改了就是另一份
	h := hookOf(req)
	h.ID, h.Version, h.Rev = cur.ID, cur.Version, cur.Rev
	h.Author, h.Approver, h.State, h.Builtin = cur.Author, cur.Approver, cur.State, cur.Builtin
	saved, ok := Write(c, func() (model.Hook, error) {
		return s.deps.Hooks.SaveHookDraft(c.Request.Context(), h)
	})
	if !ok {
		return
	}
	Ok(c, toHookResponse(saved))
}

func (s *server) newHookVersion(c *gin.Context) {
	from := atoiOr(c.Query("from"), 0)
	if from <= 0 {
		Fail(c, statusBadRequest, nil, "要带 from，说明从哪个版本开新的")
		return
	}
	created, ok := Write(c, func() (model.Hook, error) {
		return s.deps.Hooks.NewHookVersion(c.Request.Context(), c.Param("id"), from, userOf(c).Username)
	})
	if !ok {
		return
	}
	Ok(c, toHookResponse(created))
}

func (s *server) deleteHookDraft(c *gin.Context) {
	err := s.deps.Hooks.DeleteHookDraft(c.Request.Context(), hookKeyOf(c),
		atoiOr(c.Query("rev"), 0))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

func (s *server) renameHook(c *gin.Context) {
	var req nameRequest
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.Hooks.RenameHook(c.Request.Context(), c.Param("id"), req.Name,
		s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

// hookKeyOf 从路径里取出 hook 的 ID 与版本。
func hookKeyOf(c *gin.Context) model.HookKey {
	return model.HookKey{ID: c.Param("id"), Version: atoiOr(c.Param("version"), 0)}
}

// hookOf 把请求体转成 model.Hook。ID、版本、Rev、状态、作者、审批人、内置标记由仓储管，
// 这里一个都不填。
func hookOf(req hookRequest) model.Hook {
	return model.Hook{
		Name: req.Name, Description: req.Description, TimeoutSec: req.TimeoutSec,
		Required: req.Required, Params: req.Params, Impls: req.Impls,
	}
}

// nameRequest 是改名的请求体。名字规则同新建（C12）。
type nameRequest struct {
	Name string `json:"name"`
}
```

`hooks.go` 的 import 加 `"github.com/lober/lober/internal/core/activation"` 与 `"github.com/gin-gonic/gin"`。

`CreateHook` 与 `NewHookVersion` 不要 `Audit`（仓储的签名里没有），所以这两个处理器不调 `auditFrom`——新建与开新版本不写审计（总 spec §6.2 第 8 条：「编辑草稿（新建、保存、开新版本、派生、删除草稿）不写审计」）。

- [ ] **Step 4: 实现模板端点**

Create `internal/console/templates.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// templateRequest 是新建与保存模板的请求体。族与系可以留空（C13）。
type templateRequest struct {
	Name      string                    `json:"name"`
	Author    string                    `json:"author"`
	Family    catalog.Family            `json:"family"`
	Series    []catalog.Series          `json:"series"`
	Body      string                    `json:"body"`
	Params    []model.ParamDecl         `json:"params"`
	Bindings  map[string]model.Binding  `json:"bindings"`
	Assembly  map[string][]model.Instance `json:"assembly"`
	Rev       int64                     `json:"rev"`
}

// templateResponse 是一个模板版本对外的形状。Problems 是激活判定的结果，规则同 hookResponse。
type templateResponse struct {
	ID          string                     `json:"id"`
	Version     int                        `json:"version"`
	Name        string                     `json:"name"`
	State       model.VersionState         `json:"state"`
	Author      string                     `json:"author"`
	Approver    string                     `json:"approver"`
	Builtin     bool                       `json:"builtin"`
	DerivedFrom *model.TemplateKey         `json:"derivedFrom"`
	Family      catalog.Family             `json:"family"`
	Series      []catalog.Series           `json:"series"`
	Body        string                     `json:"body"`
	Params      []model.ParamDecl          `json:"params"`
	Bindings    map[string]model.Binding   `json:"bindings"`
	Assembly    map[string][]model.Instance `json:"assembly"`
	Problems    []model.Problem            `json:"problems"`
	Rev         int64                      `json:"rev"`
}

// templatePage 是一页模板，每个身份一项。
type templatePage struct {
	Items []templateResponse `json:"items"`
	Next  string             `json:"next"`
}

// deriveRequest 是派生的请求体：来源版本加新身份的名字（C24）。
type deriveRequest struct {
	From int    `json:"from"`
	Name string `json:"name"`
}

func toTemplateResponse(t model.Template) templateResponse {
	return templateResponse{
		ID: t.ID, Version: t.Version, Name: t.Name, State: t.State,
		Author: t.Author, Approver: t.Approver, Builtin: t.Builtin, DerivedFrom: t.DerivedFrom,
		Family: t.Family, Series: t.Series, Body: t.Body, Params: t.Params,
		Bindings: t.Bindings, Assembly: t.Assembly,
		Problems: activation.Template(t, nil), Rev: t.Rev,
	}
}

// templateRoutes 是模板的端点。与 hook 的对应关系：CreateTemplate 对 CreateHook，依此类推；
// 另有三条 hook 没有的——派生、预览、插槽清单（预览与插槽在 Task 10）。
func (s *server) templateRoutes() []route {
	return []route{
		{"GET", "/console/v1/templates", PermTemplateRead, "", "模板", "列出模板", nil, templatePage{}, s.listTemplates},
		{"POST", "/console/v1/templates", PermTemplateWrite, "template.create", "模板", "新建模板", templateRequest{}, templateResponse{}, s.createTemplate},
		{"POST", "/console/v1/templates/{id}/derive", PermTemplateWrite, "", "模板", "派生模板", deriveRequest{}, templateResponse{}, s.deriveTemplate},
		{"GET", "/console/v1/templates/{id}/versions", PermTemplateRead, "", "模板", "列一个模板的全部版本", nil, versionPage{}, s.listTemplateVersions},
		{"POST", "/console/v1/templates/{id}/versions", PermTemplateWrite, "", "模板", "开一个新版本的草稿", nil, templateResponse{}, s.newTemplateVersion},
		{"GET", "/console/v1/templates/{id}/versions/{version}", PermTemplateRead, "", "模板", "取一个模板版本", nil, templateResponse{}, s.getTemplate},
		{"PUT", "/console/v1/templates/{id}/versions/{version}", PermTemplateWrite, "", "模板", "保存模板草稿", templateRequest{}, templateResponse{}, s.saveTemplateDraft},
		{"DELETE", "/console/v1/templates/{id}/versions/{version}", PermTemplateWrite, "template.delete_draft", "模板", "删除模板草稿", nil, nil, s.deleteTemplateDraft},
		{"PUT", "/console/v1/templates/{id}/name", PermTemplateWrite, "template.rename", "模板", "给模板改名", nameRequest{}, nil, s.renameTemplate},
	}
}

func (s *server) listTemplates(c *gin.Context) {
	q := store.TemplateQuery{
		NamePrefix:   c.Query("prefix"),
		ApprovedOnly: c.Query("approved") == "true",
		Series:       catalog.Series(c.Query("series")),
		Cursor:       c.Query("cursor"),
		Limit:        atoiOr(c.Query("limit"), 0),
	}
	if st := c.Query("state"); st != "" {
		q.States = []model.VersionState{model.VersionState(st)}
	}
	page, ok := Get(c, func() (store.Page[model.Template], error) {
		return s.deps.Templates.ListTemplates(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]templateResponse, 0, len(page.Items))
	for _, t := range page.Items {
		items = append(items, toTemplateResponse(t))
	}
	Ok(c, templatePage{Items: items, Next: page.Next})
}

func (s *server) listTemplateVersions(c *gin.Context) {
	versions, ok := Get(c, func() ([]model.Template, error) {
		return s.deps.Templates.ListTemplateVersions(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	items := make([]templateResponse, 0, len(versions))
	for _, t := range versions {
		items = append(items, toTemplateResponse(t))
	}
	Ok(c, versionPage{Items: items})
}

func (s *server) getTemplate(c *gin.Context) {
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(t))
}

func (s *server) createTemplate(c *gin.Context) {
	var req templateRequest
	if !Bind(c, &req) {
		return
	}
	t := templateOf(req)
	t.Author = userOf(c).Username
	created, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.CreateTemplate(c.Request.Context(), t)
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(created))
}

func (s *server) deriveTemplate(c *gin.Context) {
	var req deriveRequest
	if !Bind(c, &req) {
		return
	}
	if req.From <= 0 {
		Fail(c, statusBadRequest, nil, "要带 from，说明从哪个版本派生")
		return
	}
	created, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.DeriveTemplate(c.Request.Context(),
			model.TemplateKey{ID: c.Param("id"), Version: req.From}, req.Name, userOf(c).Username)
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(created))
}

func (s *server) saveTemplateDraft(c *gin.Context) {
	var req templateRequest
	if !Bind(c, &req) {
		return
	}
	cur, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	})
	if !ok {
		return
	}
	t := templateOf(req)
	t.ID, t.Version, t.Rev = cur.ID, cur.Version, cur.Rev
	t.Author, t.Approver, t.State, t.Builtin, t.DerivedFrom = cur.Author, cur.Approver, cur.State, cur.Builtin, cur.DerivedFrom
	saved, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.SaveTemplateDraft(c.Request.Context(), t)
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(saved))
}

func (s *server) newTemplateVersion(c *gin.Context) {
	from := atoiOr(c.Query("from"), 0)
	if from <= 0 {
		Fail(c, statusBadRequest, nil, "要带 from，说明从哪个版本开新的")
		return
	}
	created, ok := Write(c, func() (model.Template, error) {
		return s.deps.Templates.NewTemplateVersion(c.Request.Context(), c.Param("id"), from, userOf(c).Username)
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(created))
}

func (s *server) deleteTemplateDraft(c *gin.Context) {
	err := s.deps.Templates.DeleteTemplateDraft(c.Request.Context(), templateKeyOf(c),
		atoiOr(c.Query("rev"), 0))
	if err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

func (s *server) renameTemplate(c *gin.Context) {
	var req nameRequest
	if !Bind(c, &req) {
		return
	}
	if err := s.deps.Templates.RenameTemplate(c.Request.Context(), c.Param("id"), req.Name,
		s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	Ok(c, nil)
}

// templateKeyOf 从路径里取出模板的 ID 与版本。
func templateKeyOf(c *gin.Context) model.TemplateKey {
	return model.TemplateKey{ID: c.Param("id"), Version: atoiOr(c.Param("version"), 0)}
}

// templateOf 把请求体转成 model.Template。ID、版本、Rev、状态、作者、审批人、内置标记与派生
// 来源由仓储管，这里一个都不填。
func templateOf(req templateRequest) model.Template {
	return model.Template{
		Name: req.Name, Family: req.Family, Series: req.Series, Body: req.Body,
		Params: req.Params, Bindings: req.Bindings, Assembly: req.Assembly,
	}
}
```

`templates.go` 的 import 加 `"github.com/lober/lober/internal/core/activation"` 与 `"github.com/gin-gonic/gin"`。

**`activation.Template(t, nil)` 传 nil 的 hooks**：激活判定要用装配表引用的 hook 版本才能查「实例的 hook 有没有覆盖所勾的系」这类问题，而列表接口不想为此逐条查库。传 nil 时那些检查都跳过（`admission` 与 `activation` 都按「hooks 里缺了的引用当作没废弃、也不看它的参数」处理），只报不依赖 hook 的问题。详情接口（Task 9 的 `validate`）才把 hook 查齐了再判。

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),,
		s.userRoutes(),,
		s.hostRoutes(),,
		s.isoRoutes(),,
		s.hookRoutes(),,
		s.templateRoutes(),
	)
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/console
git commit -m "feat(console): hook 与模板的读取、保存、开新版本、改名与派生"
```

---

---

## Task 9：hook 与模板的生命周期与 validate

**Files:**
- Create: `internal/console/lifecycle.go`
- Modify: `internal/console/hooks.go`、`internal/console/templates.go`（路由表各接四行）
- Modify: `internal/console/routes.go`
- Test: `internal/console/lifecycle_test.go`

**Interfaces:**
- Consumes: Task 8 的 `hookResponse`/`templateResponse`/`toHookResponse`/`toTemplateResponse`/`hookKeyOf`/`templateKeyOf`/夹具
- Produces:
  - `rejectRequest`、`validateResponse`（OpenAPI 的 schema 名）
  - `(*server).hookLifecycleRoutes() []route`、`(*server).templateLifecycleRoutes() []route`
  - `console.HookProblems(h model.Hook) []model.Problem`、`console.TemplateProblems(t model.Template) []model.Problem`（后者自己查齐引用的 hook）

- [ ] **Step 1: 写失败的测试——提交、审批、驳回、废弃**

Create `internal/console/lifecycle_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// approveAs 换一个用户登录并审批。审批人不能是作者（C22），所以每个用例都要第二个用户。
func approveAs(t *testing.T, s *console.Server, st store.Store, approver string) string {
	t.Helper()
	seedUser(t, st, approver, store.RoleHookApprover, "correct horse")
	_, body := do(t, s, "POST", "/console/v1/auth/login",
		`{"username":"`+approver+`","password":"correct horse"}`)
	return tokenOf(t, body)
}

// 提交：draft → review，写审计。
func Test提交hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token)
	if code != 200 {
		t.Fatalf("提交返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionReview {
		t.Errorf("状态是 %q，应为 review", back.State)
	}
}

// 审批：review → approved，审批人不能是作者（C22）。激活判定在事务里重跑，不过就拒。
func Test审批hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	if code, _ := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token); code != 200 {
		t.Fatal("提交失败")
	}
	// 作者是 root，换 bob 来审批
	approver := approveAs(t, s, st, "bob")
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	if code != 200 {
		t.Fatalf("审批返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionApproved || back.Approver != "bob" {
		t.Errorf("状态是 %q、审批人是 %q，应为 approved、bob", back.State, back.Approver)
	}
	// 自己批自己：C22
	h2 := seedHook(t, st, "h2", "network")
	if code, _ := doWith(t, s, "POST", "/console/v1/hooks/"+h2.ID+"/versions/1/submit", "", token); code != 200 {
		t.Fatal("提交 h2 失败")
	}
	code, got = doWith(t, s, "POST", "/console/v1/hooks/"+h2.ID+"/versions/1/approve", "", token)
	if code != 422 {
		t.Errorf("作者批自己返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "approver_is_author") {
		t.Errorf("响应里没有 approver_is_author：%s", got)
	}
}

// 激活判定不过的提交不了：审批时重跑，问题原样带出（C22）。
func Test审批激活不过的hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	// 一个空 hook：没有任何实现
	h, err := st.CreateHook(t.Context(), model.Hook{Name: "empty"})
	if err != nil {
		t.Fatalf("建空 hook：%v", err)
	}
	if code, _ := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token); code != 200 {
		t.Fatal("提交失败")
	}
	approver := approveAs(t, s, st, "bob")
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	if code != 422 {
		t.Errorf("审批返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "hook_no_impl") {
		t.Errorf("响应里没有 hook_no_impl：%s", got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionReview {
		t.Errorf("状态是 %q，没通过就应停在 review", back.State)
	}
}

// 驳回：review → draft，必须给理由（spec §5.2），理由写进审计的 Detail。
func Test驳回hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	if code, _ := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token); code != 200 {
		t.Fatal("提交失败")
	}
	approver := approveAs(t, s, st, "bob")
	// 没给理由：400
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/reject", "", approver)
	if code != 400 {
		t.Errorf("没给理由返回 %d，应为 400：%s", code, got)
	}
	// 给了理由
	code, got = doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/reject",
		`{"reason":"脚本里写了明文口令"}`, approver)
	if code != 200 {
		t.Fatalf("驳回返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionDraft {
		t.Errorf("状态是 %q，应为 draft", back.State)
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	found := false
	for _, e := range page.Items {
		if e.Action == "hook.reject" {
			found = true
			if e.Detail != "脚本里写了明文口令" {
				t.Errorf("审计的理由是 %q，应原样记下", e.Detail)
			}
		}
	}
	if !found {
		t.Error("没有 hook.reject 的审计记录")
	}
}

// 废弃：approved → deprecated。已废弃的不能再被新引用（C26）。
func Test废弃hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token)
	approver := approveAs(t, s, st, "bob")
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/deprecate", "", approver)
	if code != 200 {
		t.Fatalf("废弃返回 %d：%s", code, got)
	}
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionDeprecated {
		t.Errorf("状态是 %q，应为 deprecated", back.State)
	}
}

// 不合法的状态转换报 409，并说清现在是什么状态。
func Test不合法的状态转换(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	// 草稿就审批：409
	approver := approveAs(t, s, st, "bob")
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	if code != 409 {
		t.Errorf("草稿就审批返回 %d，应为 409：%s", code, got)
	}
	// 草稿就驳回：409
	code, got = doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/reject",
		`{"reason":"x"}`, approver)
	if code != 409 {
		t.Errorf("草稿就驳回返回 %d，应为 409：%s", code, got)
	}
	// 草稿就废弃：409
	code, got = doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/deprecate", "", approver)
	if code != 409 {
		t.Errorf("草稿就废弃返回 %d，应为 409：%s", code, got)
	}
}

// validate：把激活判定的问题逐条带给编辑器，不写任何东西。
func Test校验hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	// 一个勾了不属于本族的系的 hook
	h, err := st.CreateHook(t.Context(), model.Hook{
		Name: "bad", Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{catalog.SeriesRocky},
			Kind: model.ImplFragment, Body: ""}},
	})
	if err != nil {
		t.Fatalf("建 hook：%v", err)
	}
	code, got := doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/validate", "", token)
	if code != 200 {
		t.Fatalf("校验返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			OK       bool `json:"ok"`
			Problems []struct {
				Code string `json:"code"`
			} `json:"problems"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if res.Data.OK {
		t.Error("这个 hook 过不了激活判定，OK 却为 true")
	}
	if len(res.Data.Problems) == 0 {
		t.Error("没有问题清单")
	}
	// 校验什么都不写
	back, err := st.GetHook(t.Context(), model.HookKey{ID: h.ID, Version: 1})
	if err != nil {
		t.Fatalf("读回：%v", err)
	}
	if back.State != model.VersionDraft {
		t.Errorf("校验把状态改成了 %q", back.State)
	}
}

// 模板的 validate 要把装配表引用的 hook 查齐了再判（C16：实例的 hook 要覆盖所勾的系）。
func Test校验模板把hook查齐(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	// 模板勾了 rocky 与 debian，而 hook 的实现只覆盖 rocky：报 series 不对
	tpl.Series = []catalog.Series{catalog.SeriesRocky, catalog.SeriesDebian}
	tpl.Rev = 1
	saved, err := st.SaveTemplateDraft(t.Context(), tpl)
	if err != nil {
		t.Fatalf("改模板：%v", err)
	}
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+saved.ID+"/versions/1/validate", "", token)
	if code != 200 {
		t.Fatalf("校验返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Problems []struct {
				Code string `json:"code"`
			} `json:"problems"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	found := false
	for _, p := range res.Data.Problems {
		if p.Code == "series_not_ticked" || p.Code == "impl_missing_for_series" {
			found = true
		}
	}
	if !found {
		t.Errorf("问题里没有系的：%+v", res.Data.Problems)
	}
}

// 模板的生命周期与 hook 相同，另有一处：审批时连带它引用的 hook 版本一起判定。
func Test审批模板引用已废弃的hook(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	// 把 hook 审批后废弃
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token)
	approver := approveAs(t, s, st, "bob")
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/deprecate", "", approver)
	// 提交并审批模板：引用的 hook 已废弃，挡住它（C26）
	if code, _ := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/submit", "", token); code != 200 {
		t.Fatal("提交模板失败")
	}
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/approve", "", approver)
	if code != 422 {
		t.Errorf("审批返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "template_blocked_by_deprecated") {
		t.Errorf("响应里没有 template_blocked_by_deprecated：%s", got)
	}
}
```

`lifecycle_test.go` 的 import 加 `"github.com/lober/lober/internal/console"`、`"github.com/lober/lober/internal/core/catalog"`。

Run: `go test ./internal/console/ -run 'Test提交|Test审批|Test驳回|Test废弃hook|Test不合法|Test校验|Test审批模板' -v`
Expected: FAIL，`undefined: s.submitHook`（路由还没接）。

- [ ] **Step 2: 实现生命周期端点**

Create `internal/console/lifecycle.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/activation"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// rejectRequest 是驳回审批的请求体。理由必填：spec §5.2 要求驳回给出理由，第 2 层把它留给
// 本层收口（第 2 层清单的遗留条目）。
type rejectRequest struct {
	Reason string `json:"reason"`
}

// validateResponse 是 validate 的响应：过没过，加逐条问题。编辑器照它标红。
type validateResponse struct {
	OK       bool            `json:"ok"`
	Problems []model.Problem `json:"problems"`
}

// HookProblems 跑 hook 的激活判定（C8–C11）。列表接口也用它，那时传的是从仓储读出来的版本。
func HookProblems(h model.Hook) []model.Problem { return activation.Hook(h) }

// TemplateProblems 跑模板的激活判定（C13–C17、C19、C22）。它要把装配表引用的 hook 版本查齐
// 了再判，所以不是纯函数：调用方把仓储交给它。
func TemplateProblems(st store.Hooks, t model.Template) []model.Problem {
	hooks, err := st.GetHooks(context.Background(), store.HookRefs(t))
	if err != nil {
		// 查不齐就按没有判：缺的引用由 hook_ref_missing 报出来，比这里报一个内部错清楚
		return activation.Template(t, nil)
	}
	return activation.Template(t, hooks)
}

// hookLifecycleRoutes 是 hook 的五个生命周期端点。提交、审批、驳回、废弃都写审计；validate
// 什么都不写。
func (s *server) hookLifecycleRoutes() []route {
	const base = "/console/v1/hooks/{id}/versions/{version}"
	return []route{
		{"POST", base + "/submit", PermHookWrite, "hook.submit", "hook", "提交审批", nil, hookResponse{}, s.submitHook},
		{"POST", base + "/approve", PermHookApprove, "hook.approve", "hook", "审批", nil, hookResponse{}, s.approveHook},
		{"POST", base + "/reject", PermHookApprove, "hook.reject", "hook", "驳回审批", rejectRequest{}, hookResponse{}, s.rejectHook},
		{"POST", base + "/deprecate", PermHookApprove, "hook.deprecate", "hook", "废弃", nil, hookResponse{}, s.deprecateHook},
		{"POST", base + "/validate", PermHookWrite, "", "hook", "跑激活判定", nil, validateResponse{}, s.validateHook},
	}
}

func (s *server) submitHook(c *gin.Context) {
	k := hookKeyOf(c)
	if err := s.deps.Hooks.SubmitHook(c.Request.Context(), k, revOf(c), s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadHook(c, k)
}

func (s *server) approveHook(c *gin.Context) {
	k := hookKeyOf(c)
	if err := s.deps.Hooks.ApproveHook(c.Request.Context(), k, revOf(c),
		userOf(c).Username, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadHook(c, k)
}

func (s *server) rejectHook(c *gin.Context) {
	var req rejectRequest
	if !Bind(c, &req) {
		return
	}
	if req.Reason == "" {
		Fail(c, statusBadRequest, nil, "驳回要给出理由，作者照它改")
		return
	}
	k := hookKeyOf(c)
	if err := s.deps.Hooks.RejectHook(c.Request.Context(), k, revOf(c),
		s.auditFrom(c, req.Reason)); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadHook(c, k)
}

func (s *server) deprecateHook(c *gin.Context) {
	k := hookKeyOf(c)
	if err := s.deps.Hooks.DeprecateHook(c.Request.Context(), k, revOf(c),
		s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadHook(c, k)
}

func (s *server) validateHook(c *gin.Context) {
	h, ok := Get(c, func() (model.Hook, error) {
		return s.deps.Hooks.GetHook(c.Request.Context(), hookKeyOf(c))
	})
	if !ok {
		return
	}
	Ok(c, validateResponse{Problems: HookProblems(h)})
}

// reloadHook 是四个状态端点的收尾：再读一次，把新的状态与激活判定的问题带回界面。
func (s *server) reloadHook(c *gin.Context, k model.HookKey) {
	h, ok := Get(c, func() (model.Hook, error) {
		return s.deps.Hooks.GetHook(c.Request.Context(), k)
	})
	if !ok {
		return
	}
	Ok(c, toHookResponse(h))
}

// templateLifecycleRoutes 是模板的五个生命周期端点，规则与 hook 的相同。
func (s *server) templateLifecycleRoutes() []route {
	const base = "/console/v1/templates/{id}/versions/{version}"
	return []route{
		{"POST", base + "/submit", PermTemplateWrite, "template.submit", "模板", "提交审批", nil, templateResponse{}, s.submitTemplate},
		{"POST", base + "/approve", PermTemplateApprove, "template.approve", "模板", "审批", nil, templateResponse{}, s.approveTemplate},
		{"POST", base + "/reject", PermTemplateApprove, "template.reject", "模板", "驳回审批", rejectRequest{}, templateResponse{}, s.rejectTemplate},
		{"POST", base + "/deprecate", PermTemplateApprove, "template.deprecate", "模板", "废弃", nil, templateResponse{}, s.deprecateTemplate},
		{"POST", base + "/validate", PermTemplateWrite, "", "模板", "跑激活判定", nil, validateResponse{}, s.validateTemplate},
	}
}

func (s *server) submitTemplate(c *gin.Context) {
	k := templateKeyOf(c)
	if err := s.deps.Templates.SubmitTemplate(c.Request.Context(), k, revOf(c), s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadTemplate(c, k)
}

func (s *server) approveTemplate(c *gin.Context) {
	k := templateKeyOf(c)
	if err := s.deps.Templates.ApproveTemplate(c.Request.Context(), k, revOf(c),
		userOf(c).Username, s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadTemplate(c, k)
}

func (s *server) rejectTemplate(c *gin.Context) {
	var req rejectRequest
	if !Bind(c, &req) {
		return
	}
	if req.Reason == "" {
		Fail(c, statusBadRequest, nil, "驳回要给出理由，作者照它改")
		return
	}
	k := templateKeyOf(c)
	if err := s.deps.Templates.RejectTemplate(c.Request.Context(), k, revOf(c),
		s.auditFrom(c, req.Reason)); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadTemplate(c, k)
}

func (s *server) deprecateTemplate(c *gin.Context) {
	k := templateKeyOf(c)
	if err := s.deps.Templates.DeprecateTemplate(c.Request.Context(), k, revOf(c),
		s.auditFrom(c, "")); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadTemplate(c, k)
}

func (s *server) validateTemplate(c *gin.Context) {
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	})
	if !ok {
		return
	}
	Ok(c, validateResponse{Problems: TemplateProblems(s.deps.Hooks, t)})
}

func (s *server) reloadTemplate(c *gin.Context, k model.TemplateKey) {
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), k)
	})
	if !ok {
		return
	}
	Ok(c, toTemplateResponse(t))
}

// revOf 从查询参数里取期望的 Rev。四个状态端点都要它：状态推进是条件更新，Rev 对不上就写不
// 进去（§4.2「可变的记录都带乐观锁版本 Rev」）。
func revOf(c *gin.Context) int64 { return atoiOr(c.Query("rev"), 0) }
```

`lifecycle.go` 的 import 加 `"context"` 与 `"github.com/gin-gonic/gin"`。

`hooks.go` 与 `templates.go` 的路由表各接一行（用 `slices.Concat` 拼上生命周期那组）：

```go
func (s *server) hookRoutes() []route {
	return slices.Concat(s.hookBaseRoutes(), s.hookLifecycleRoutes())
}
```

把原来 `hookRoutes` 的函数体改名成 `hookBaseRoutes`，`templateRoutes` 同样改成 `slices.Concat(s.templateBaseRoutes(), s.templateLifecycleRoutes())`。两个文件的 import 都要加 `"slices"`。

`TemplateProblems` 里的 `context.Background()` 换成 `c.Request.Context()`：把它改成方法更合适——

```go
// templateProblems 跑模板的激活判定，把装配表引用的 hook 版本查齐了再判。
func (s *server) templateProblems(c *gin.Context, t model.Template) []model.Problem {
	hooks, err := s.deps.Hooks.GetHooks(c.Request.Context(), store.HookRefs(t))
	if err != nil {
		// 查不齐就按没有判：缺的引用由 hook_ref_missing 报出来，比这里报一个内部错清楚
		return activation.Template(t, nil)
	}
	return activation.Template(t, hooks)
}
```

包级的 `TemplateProblems(st store.Hooks, t model.Template)` 因此不需要，`validateTemplate` 改调 `s.templateProblems(c, t)`。**Interfaces 一节的 `TemplateProblems` 相应改成这个方法。**

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add internal/console
git commit -m "feat(console): hook 与模板的提交、审批、驳回、废弃与激活判定端点"
```

---

---

## Task 10：预览与编辑器用的插槽清单

**Files:**
- Create: `internal/console/preview.go`
- Create: `internal/console/slots.go`
- Modify: `internal/console/templates.go`（路由表接两行）
- Modify: `internal/console/routes.go`
- Test: `internal/console/preview_test.go`、`internal/console/slots_test.go`

**Interfaces:**
- Consumes: Task 8 的 `templateResponse`/`templateKeyOf`；Task 4 的 `Deps`/`Judge`；Task 9 的 `validateResponse`
- Produces:
  - `previewRequest`、`previewFile`、`previewResponse`、`slotList`、`slotInfo`（OpenAPI 的 schema 名）
  - `console.SampleISO(series catalog.Series) model.ISO`、`console.SampleHost() model.Host`
  - `(*server).previewTemplate`、`(*server).slotsOf`

**预览与真实下发走同一条路**（C35）：预览调 `render.Render`，与网关下发时用的是同一个函数、同一份快照构建。没指定主机或 ISO 时用示例值，并在响应里标出哪些是示例；预览的 token 是示例值（第 3 层交接、spec §6.2 第 2 条）。

- [ ] **Step 1: 写失败的测试——插槽清单**

Create `internal/console/slots_test.go`：

```go
package console_test

import (
	"encoding/json"
	"testing"
)

// 编辑器要的插槽清单：名字、是不是执行插槽、时机，都从正文扫出来（C14：插槽只来自正文）。
func Test插槽清单(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	code, got := doWith(t, s, "GET", "/console/v1/templates/"+tpl.ID+"/versions/1/slots", "", token)
	if code != 200 {
		t.Fatalf("取插槽清单返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Slots []struct {
				Name   string `json:"name"`
				Exec   bool   `json:"exec"`
				Timing string `json:"timing"`
				Line   int    `json:"line"`
				Col    int    `json:"col"`
			} `json:"slots"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Slots) != 2 {
		t.Fatalf("扫出 %d 个插槽，sampleTemplateBody 里有两个", len(res.Data.Slots))
	}
	// 按正文里出现的顺序：分区（片段）、装后配置（执行，post-target）
	if res.Data.Slots[0].Name != "分区" || res.Data.Slots[0].Exec {
		t.Errorf("第一个插槽是 %+v，应为片段插槽「分区」", res.Data.Slots[0])
	}
	if !res.Data.Slots[1].Exec || res.Data.Slots[1].Timing != "post-target" {
		t.Errorf("第二个插槽是 %+v，应为 post-target 的执行插槽", res.Data.Slots[1])
	}
	if res.Data.Slots[0].Line == 0 || res.Data.Slots[0].Col == 0 {
		t.Errorf("插槽没有行列，编辑器标不了位置：%+v", res.Data.Slots[0])
	}
}

// 正文有扫描错误时照常给出扫得出来的插槽，错误另外列出来（任务 8 的约定）。
func Test插槽清单带扫描错误(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl, err := st.CreateTemplate(t.Context(), model.Template{
		Name: "broken", Family: catalog.FamilyLinux, Series: []catalog.Series{catalog.SeriesRocky},
		Body: "{{slot:分区}}\n{{slot:坏名字 with space}}\n{{lober:syslog}}\n{{lober:cleanup}}\n",
		Assembly: map[string][]model.Instance{"分区": {{HookID: h.ID, HookVersion: 1}}},
	})
	if err != nil {
		t.Fatalf("建模板：%v", err)
	}
	code, got := doWith(t, s, "GET", "/console/v1/templates/"+tpl.ID+"/versions/1/slots", "", token)
	if code != 200 {
		t.Fatalf("取插槽清单返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Slots []struct {
				Name string `json:"name"`
			} `json:"slots"`
			Problems []struct {
				Code string `json:"code"`
			} `json:"problems"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Slots) != 1 || res.Data.Slots[0].Name != "分区" {
		t.Errorf("插槽是 %+v，坏占位符不该进来，分区该在", res.Data.Slots)
	}
	if len(res.Data.Problems) == 0 {
		t.Error("没有列出扫描错误")
	}
}
```

`slots_test.go` 的 import 加 `"github.com/lober/lober/internal/core/catalog"`、`"github.com/lober/lober/internal/core/model"`、`"github.com/lober/lober/internal/store"`。

Run: `go test ./internal/console/ -run 'Test插槽清单' -v`
Expected: FAIL，`undefined: s.slotsOf`。

- [ ] **Step 2: 写失败的测试——预览**

Create `internal/console/preview_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 预览给该格式要下发的每一个文件，BIOS 与 UEFI 各一份菜单（spec §7.2 第 3 条）。
func Test预览给出每种文件(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/preview",
		`{"firmware":"uefi"}`, token)
	if code != 200 {
		t.Fatalf("预览返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Files []struct {
				Kind   string `json:"kind"`
				Name   string `json:"name"`
				Sample bool   `json:"sample"`
				Body   string `json:"body"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	// kickstart 下发菜单、应答文件与运行脚本（catalog.DeliveryFiles）
	kinds := map[string]bool{}
	for _, f := range res.Data.Files {
		kinds[f.Kind] = true
		if f.Name == "" {
			t.Errorf("%s 没有文件名", f.Kind)
		}
	}
	for _, want := range []string{"menu", "answer", "script"} {
		if !kinds[want] {
			t.Errorf("预览里没有 %s，实际有 %v", want, kinds)
		}
	}
}

// 没指定 ISO 与主机时用示例值，并标出来（第 3 层交接、spec §6.2 第 2 条）。
func Test预览用示例值并标明(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/preview", `{}`, token)
	if code != 200 {
		t.Fatalf("预览返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Sample []string `json:"sample"`
			Files  []struct {
				Sample bool   `json:"sample"`
				Body   string `json:"body"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Sample) == 0 {
		t.Error("没有标出哪些是示例值")
	}
	for _, f := range res.Data.Files {
		if !f.Sample {
			t.Errorf("有一个文件没标示例：%q", f.Body)
			break
		}
	}
	// 预览的 token 是示例值（spec §6.2 第 2 条）
	if !strings.Contains(got, "sample-token") {
		t.Errorf("预览里没有示例 token：%s", got)
	}
}

// 指定了 ISO 与主机就用真的：响应里不再标示例。
func Test预览用真的ISO与主机(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "x86_64", Firmware: "uefi"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	body := `{"isoID":"` + iso.ID + `","hostSN":"SN-1","firmware":"uefi"}`
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/preview", body, token)
	if code != 200 {
		t.Fatalf("预览返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Sample []string `json:"sample"`
			Files  []struct {
				Sample bool `json:"sample"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Sample) != 0 {
		t.Errorf("指定了 ISO 与主机却还标示例：%v", res.Data.Sample)
	}
	for _, f := range res.Data.Files {
		if f.Sample {
			t.Error("有一个文件还标着示例")
			break
		}
	}
}

// 同一个请求预览两次，逐字节相同（C35）。
func Test预览两次逐字节相同(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	path := "/console/v1/templates/" + tpl.ID + "/versions/1/preview"
	_, a := doWith(t, s, "POST", path, `{}`, token)
	_, b := doWith(t, s, "POST", path, `{}`, token)
	if a != b {
		t.Error("同一个请求预览两次不相同")
	}
}

// 模板过不了快照构建的前提（勾的系与 ISO 的系不符）时，返回问题，不给半截文件。
func Test预览过不了前提时只给问题(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Debian 13.6", catalog.SeriesDebian)
	body := `{"isoID":"` + iso.ID + `","firmware":"uefi"}`
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/preview", body, token)
	if code != 422 {
		t.Fatalf("预览返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "series_not_ticked") {
		t.Errorf("响应里没有 series_not_ticked：%s", got)
	}
	var res struct {
		Data struct {
			Files []struct {
				Body string `json:"body"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Files) != 0 {
		t.Error("有问题却还给文件")
	}
}

// 草稿也能预览（C22）：过不了激活判定不要紧，快照构建守住渲染要用的前提。
func Test草稿也能预览(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	// 模板勾了 debian，hook 的实现只覆盖 rocky：激活判定不过，但预览照给
	tpl, err := st.CreateTemplate(t.Context(), model.Template{
		Name: "draft", Family: catalog.FamilyLinux, Series: []catalog.Series{catalog.SeriesDebian},
		Body: sampleTemplateBody,
		Assembly: map[string][]model.Instance{"分区": {{HookID: h.ID, HookVersion: 1}}},
	})
	if err != nil {
		t.Fatalf("建模板：%v", err)
	}
	code, got := doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/preview", `{}`, token)
	if code != 200 {
		t.Fatalf("草稿预览返回 %d：%s", code, got)
	}
}
```

`preview_test.go` 的 import 加 `"github.com/lober/lober/internal/console"`（用到 `SampleHost` 时）。

Run: `go test ./internal/console/ -run 'Test预览' -v`
Expected: FAIL，`undefined: s.previewTemplate`。

- [ ] **Step 3: 实现插槽清单**

Create `internal/console/slots.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/scan"
)

// slotInfo 是编辑器里的一个插槽。行列给编辑器标位置：按 rune 计，从 1 起（任务 8 的约定）。
type slotInfo struct {
	Name   string `json:"name"`
	Exec   bool   `json:"exec"`
	Timing string `json:"timing"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
}

// slotList 是正文的插槽清单加扫描错误。控制台与渲染用同一份扫描（spec §4.1），否则会出现
// 编辑器里有这个插槽、渲染时却找不到的情况。
type slotList struct {
	Slots    []slotInfo      `json:"slots"`
	Problems []model.Problem `json:"problems"`
}

func (s *server) slotsOf(c *gin.Context) {
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	})
	if !ok {
		return
	}
	result, problems := scan.Scan(t.Body)
	slots := make([]slotInfo, 0, len(result.Slots))
	seen := map[string]bool{}
	for _, seg := range result.Segments {
		if seg.Kind != scan.KindSlot || seen[seg.Slot.Name] {
			continue // 插槽重名由激活判定报（slot_duplicate），这里只按第一次出现的列
		}
		seen[seg.Slot.Name] = true
		slots = append(slots, slotInfo{
			Name: seg.Slot.Name, Exec: seg.Slot.Exec, Timing: string(seg.Slot.Timing),
			Line: seg.Line, Col: seg.Col,
		})
	}
	Ok(c, slotList{Slots: slots, Problems: problems})
}
```

`slots.go` 的 import 加 `"github.com/gin-gonic/gin"`。

- [ ] **Step 4: 实现预览**

Create `internal/console/preview.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/snapshot"
	"github.com/lober/lober/internal/render"
)

// previewRequest 是预览的请求体。主机与 ISO 都可省：省了就用示例值，响应里标出哪些是示例
// （第 3 层交接、spec §6.2 第 2 条）。Firmware 是本次按哪种固件渲染菜单：BIOS 与 UEFI 的菜单
// 不同（spec §7.2 第 3 条），主机登记的值只作参考，不选菜单。
type previewRequest struct {
	HostSN   string            `json:"hostSN"`
	ISOID    string            `json:"isoID"`
	Firmware catalog.Firmware  `json:"firmware"`
	Entered  map[string]string `json:"entered"`
}

// previewFile 是预览里的一个文件。Sample 标这个文件用到了示例值。
type previewFile struct {
	Kind   catalog.FileKind `json:"kind"`
	Name   string           `json:"name"`
	Sample bool             `json:"sample"`
	Body   string           `json:"body"`
}

// previewResponse 是预览的结果。Sample 列出用了示例值的地方，界面据此标一句「这是示例」。
type previewResponse struct {
	Files    []previewFile   `json:"files"`
	Problems []model.Problem `json:"problems"`
	Sample   []string        `json:"sample"`
}

// previewToken 是预览用的示例 token（spec §6.2 第 2 条）。它不是真令牌，签不出去。
const previewToken = "sample-token"

func (s *server) previewTemplate(c *gin.Context) {
	var req previewRequest
	if !Bind(c, &req) {
		return
	}
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	})
	if !ok {
		return
	}
	hooks, err := s.deps.Hooks.GetHooks(c.Request.Context(), store.HookRefs(t))
	if err != nil {
		RespondError(c, err)
		return
	}
	iso, sample := s.previewISO(c, req.ISOID)
	if sample != nil {
		defer func() { *sample = append(*sample, "ISO") }()
	}
	host := s.previewHost(c, req.HostSN)
	firmware := req.Firmware
	if firmware == "" {
		firmware = catalog.FirmwareUEFI
	}
	// 参数求值与快照构建各跑一次：预览要让操作员看到填错的值会怎样，所以错误照给，
	// 但只在快照构建给出问题时停下（草稿也要能预览，C22）。
	in := params.Input{Template: t, Hooks: hooks, ISO: iso, Host: host, Entered: enteredOf(req)}
	snap, problems := snapshot.Build(in)
	if len(problems) > 0 {
		Ok(c, previewResponse{Files: []previewFile{}, Problems: problems, Sample: sampleLabels(sample)})
		return
	}
	values := map[catalog.SystemValue]string{
		catalog.SysGateway: "https://gateway.example.invalid",
		catalog.SysTask:    snap.Template.ID,
		catalog.SysSN:      host.SN,
		catalog.SysSMB:     `\\smb.example.invalid\lober`,
		"syslog-addr":      "udp://syslog.example.invalid:514",
		"artifacts":        "https://artifacts.example.invalid",
		"token":            previewToken,
		"finalize-token":   previewToken + "-finalize",
	}
	files := make([]previewFile, 0, 4)
	for _, kind := range catalog.DeliveryFiles(snap.Format) {
		name, ok := catalog.DeliveryFileName(snap.Format, kind)
		if !ok {
			continue
		}
		body, ps, err := render.Render(render.Request{
			Snapshot: snap, Firmware: firmware, Values: values, Kind: kind,
			ESXiBootConfig: esxiBootCfgSample,
		})
		if err != nil {
			RespondError(c, err)
			return
		}
		if len(ps) > 0 {
			Ok(c, previewResponse{Files: []previewFile{}, Problems: ps, Sample: sampleLabels(sample)})
			return
		}
		files = append(files, previewFile{Kind: kind, Name: name, Body: string(body),
			Sample: sample != nil && len(*sample) > 0})
	}
	Ok(c, previewResponse{Files: files, Problems: []model.Problem{}, Sample: sampleLabels(sample)})
}

// previewISO 取预览用的 ISO。没指定就造一个示例的，并记下用了示例。
func (s *server) previewISO(c *gin.Context, id string) (model.ISO, *[]string) {
	if id != "" {
		iso, ok := Get(c, func() (model.ISO, error) {
			return s.deps.ISOs.GetISO(c.Request.Context(), id)
		})
		if !ok {
			return model.ISO{}, nil
		}
		return iso, nil
	}
	// 示例 ISO 的系取模板勾的第一个系；模板没勾系时预览无从谈起，由快照构建报 series_not_ticked
	t, _ := s.deps.Templates.GetTemplate(c.Request.Context(), templateKeyOf(c))
	series := catalog.Series("")
	if len(t.Series) > 0 {
		series = t.Series[0]
	}
	labels := []string{"ISO"}
	return SampleISO(series), &labels
}

// previewHost 取预览用的主机。没指定就用一个示例的。
func (s *server) previewHost(c *gin.Context, sn string) model.Host {
	if sn != "" {
		h, _ := s.deps.Hosts.GetHost(c.Request.Context(), sn)
		return h
	}
	return SampleHost()
}

// enteredOf 把请求体里的参数值转成 params.Input 要的形式。
func enteredOf(req previewRequest) map[params.FieldKey]string {
	if len(req.Entered) == 0 {
		return nil
	}
	out := make(map[params.FieldKey]string, len(req.Entered))
	for k, v := range req.Entered {
		out[params.FieldKey(k)] = v
	}
	return out
}

// sampleLabels 是「用了示例值」的清单，nil 时给空切片：第 6 层直接遍历它。
func sampleLabels(labels *[]string) []string {
	if labels == nil {
		return []string{}
	}
	return *labels
}

// esxiBootCfgSample 是 ESXi 附属文件要的原始 boot.cfg。预览时用一份示例，真实的那份由第 4 层
// 解出之后存起来（render.Request.ESXiBootConfig 的说明）。
const esxiBootCfgSample = "bootstate=0\ntitle=Loading ESXi installer\n"

// SampleISO 造一个用于预览的 ISO：版本与架构用占位，制品给每个角色一条占位路径。渲染只取它
// 要的那些角色，多给的不起作用；少给会让地址里缺一段，所以给齐。
//
// 角色名照第 4 层设计 §7.1 的表。第 4 层落地 catalog.RequiredRoles 之后，这里改成读它，
// 免得两份角色表走偏。
func SampleISO(series catalog.Series) model.ISO {
	roles := []string{
		"kernel", "initrd", "stage2", "repo", "iso", "live-root",
		"mboot", "esxi-tree", "wimboot", "bcd", "boot-sdi", "boot-wim",
		"winpeshl", "winpe-busybox", "winpe-curl",
	}
	artifacts := make(map[string]string, len(roles))
	for _, r := range roles {
		artifacts[r] = "sample/" + r
	}
	family := catalog.Family("")
	if series.Valid() {
		family = series.Family()
	}
	return model.ISO{
		ID: "sample-iso", Name: "示例 ISO", Family: family, Series: series,
		Version: "0.0", Arch: catalog.ArchX8664, State: model.ISOReady,
		Artifacts: artifacts, Offers: map[string][]model.Option{},
	}
}

// SampleHost 造一个用于预览的主机：SN 与平台用占位，没有属性——参数引用主机属性时取不到值，
// 预览里那一格就是空的，操作员据此知道真机上要登记什么。
func SampleHost() model.Host {
	return model.Host{SN: "SAMPLE-SN", Arch: catalog.ArchX8664, Firmware: catalog.FirmwareUEFI,
		Attrs: map[string]string{}}
}
```

`preview.go` 的 import 加 `"github.com/lober/lober/internal/store"`。

**`previewISO` 里把模板又读了一次**，为的是没指定 ISO 时知道该用哪个系。更省事的做法是让 `previewTemplate` 把 `t` 传进去：

```go
func (s *server) previewISO(c *gin.Context, t model.Template, id string) (model.ISO, *[]string)
```

调用处改成 `iso, sample := s.previewISO(c, t, req.ISOID)`，函数里就不再读仓储。

**`Sample` 的判定**：上面 `sample != nil && len(*sample) > 0` 在「指定了 ISO 但没指定主机」时不成立（`sample` 非 nil 但 `*sample` 为空），而主机其实也是示例。修正：`previewHost` 也回报一个 label，两处合起来判。

```go
func (s *server) previewHost(c *gin.Context, sn string) (model.Host, *[]string) {
	if sn != "" {
		h, ok := Get(c, func() (model.Host, error) {
			return s.deps.Hosts.GetHost(c.Request.Context(), sn)
		})
		if !ok {
			return model.Host{}, nil
		}
		return h, nil
	}
	labels := []string{"主机"}
	return SampleHost(), &labels
}
```

`previewTemplate` 里：

```go
	iso, isoSample := s.previewISO(c, t, req.ISOID)
	host, hostSample := s.previewHost(c, req.HostSN)
	sample := append(isoSample, hostSample...) // 两个都是 *[]string，解引用后拼
	...
	files = append(files, previewFile{..., Sample: len(sample) > 0})
```

`sampleLabels` 因此收 `[]string` 而不是 `*[]string`。

- [ ] **Step 5: 接路由**

Modify `internal/console/templates.go` 的 `templateBaseRoutes`，末尾加两条：

```go
		{"POST", "/console/v1/templates/{id}/versions/{version}/preview", PermTemplateRead, "", "模板", "预览渲染出来的文件", previewRequest{}, previewResponse{}, s.previewTemplate},
		{"GET", "/console/v1/templates/{id}/versions/{version}/slots", PermTemplateRead, "", "模板", "正文的插槽清单", nil, slotList{}, s.slotsOf},
```

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),,
		s.userRoutes(),,
		s.hostRoutes(),,
		s.isoRoutes(),,
		s.hookRoutes(),,
		s.templateRoutes(),,
		s.wizardRoutes(),
	)
}
```

- [ ] **Step 6: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/console
git commit -m "feat(console): 模板预览调 render.Render，与真实下发同一条路（C35）"
```

---

---

## Task 11：向导五步

**Files:**
- Create: `internal/console/wizard.go`
- Modify: `internal/console/routes.go`
- Test: `internal/console/wizard_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Deps`/`Judge`/`Bind`/`Get`/`Write`；Task 5 的夹具；Task 6 的 `atoiOr`；Task 7 的 `readyISO`；Task 8 的 `seedHook`/`seedTemplate`/`sampleTemplateBody`
- Produces:
  - `wizardHostItem`、`wizardHostPage`、`wizardISOResult`、`wizardTemplateResult`、`wizardParamsResult`、`wizardSubmitRequest`、`wizardSubmitResult`（OpenAPI 的 schema 名）
  - `(*server).wizardRoutes() []route`

向导五步严格照总 spec §5.3。第三步起服务端**不信任前端**的筛选结果：提交时整批重跑准入判定、逐台重跑参数求值、逐台构建快照，一次 `CreateTasks`（C30）。

- [ ] **Step 1: 写失败的测试——选主机**

Create `internal/console/wizard_test.go`：

```go
package console_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 第一步：按 SN 前缀或按属性搜主机。已有未结束任务的主机照常列出，但不能选，并写明原因（C31）。
func Test向导选主机(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, sn := range []string{"SN-1", "SN-2"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64",
			Attrs: map[string]string{"rack": "A01"}},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	code, got := doWith(t, s, "GET", "/console/v1/wizard/hosts?q=SN", "", token)
	if code != 200 {
		t.Fatalf("选主机返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Items []struct {
				SN         string `json:"sn"`
				Selectable bool   `json:"selectable"`
				Problems   []struct {
					Code string `json:"code"`
				} `json:"problems"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 2 {
		t.Fatalf("列出 %d 台，应为 2 台", len(res.Data.Items))
	}
	for _, h := range res.Data.Items {
		if !h.Selectable {
			t.Errorf("%s 不能选，没有未结束任务时应能选", h.SN)
		}
	}
	// 按属性搜：attr_key 与 attr_value 都要给，值完全相等
	code, got = doWith(t, s, "GET", "/console/v1/wizard/hosts?attr_key=rack&attr_value=A01", "", token)
	if code != 200 {
		t.Fatalf("按属性搜返回 %d：%s", code, got)
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 2 {
		t.Errorf("按 rack=A01 搜出 %d 台，应为 2 台", len(res.Data.Items))
	}
	// 值不相等：一台都不中
	code, got = doWith(t, s, "GET", "/console/v1/wizard/hosts?attr_key=rack&attr_value=A02", "", token)
	if code != 200 {
		t.Fatalf("按属性搜返回 %d：%s", code, got)
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 0 {
		t.Errorf("按 rack=A02 搜出 %d 台，应为 0 台", len(res.Data.Items))
	}
}

// 第二步：只列可用的 ISO，与某几台冲突的照常列出并写明是哪几台（C28）。
func Test向导选ISO(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	rocky := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	// 一台 BIOS 的 Windows 主机：ESXi 之外的 Linux ISO 与它不冲突，构造一台架构不符的
	bad, err := st.CreateISO(t.Context(), model.ISO{Name: "ARM 版", Family: "linux", Series: "rocky"},
		store.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatalf("建 ISO：%v", err)
	}
	bad.Arch = catalog.ArchAarch64
	bad.Version = "9.6"
	bad.State = model.ISOReady
	if _, err := st.UpdateISO(t.Context(), bad); err != nil {
		t.Fatalf("置为可用：%v", err)
	}
	for _, sn := range []string{"SN-1", "SN-2"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn, Arch: "x86_64", Firmware: "uefi"},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	code, got := doWith(t, s, "POST", "/console/v1/wizard/isos",
		`{"hostSNs":["SN-1","SN-2"]}`, token)
	if code != 200 {
		t.Fatalf("选 ISO 返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Items []struct {
				ID        string `json:"id"`
				Usable    bool   `json:"usable"`
				Problems  []struct {
					Code   string `json:"code"`
					Subject struct {
						ID string `json:"id"`
					} `json:"subject"`
				} `json:"problems"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 2 {
		t.Fatalf("列出 %d 个 ISO，应为 2 个", len(res.Data.Items))
	}
	byID := map[string]int{}
	for i, item := range res.Data.Items {
		byID[item.ID] = i
	}
	if !res.Data.Items[byID[rocky.ID]].Usable {
		t.Error("Rocky 与两台 x86_64 主机都相配，却标成不可用")
	}
	arm := res.Data.Items[byID[bad.ID]]
	if arm.Usable {
		t.Error("aarch64 的 ISO 与 x86_64 的主机不相配，却标成可用")
	}
	if len(arm.Problems) == 0 {
		t.Error("冲突了却没给原因")
	}
}

// 第三步：只列已审批、没引用已废弃 hook、勾了该 ISO 的系的模板，每个取当前的已审批版本（C23）。
func Test向导选模板(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	// 还没审批：列不出来
	code, got := doWith(t, s, "POST", "/console/v1/wizard/templates",
		`{"isoID":"`+iso.ID+`"}`, token)
	if code != 200 {
		t.Fatalf("选模板返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 0 {
		t.Errorf("没审批的模板列出来了：%+v", res.Data.Items)
	}
	// 审批 hook 与模板
	approver := approveAs(t, s, st, "bob")
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/submit", "", token)
	doWith(t, s, "POST", "/console/v1/hooks/"+h.ID+"/versions/1/approve", "", approver)
	doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/submit", "", token)
	doWith(t, s, "POST", "/console/v1/templates/"+tpl.ID+"/versions/1/approve", "", approver)
	code, got = doWith(t, s, "POST", "/console/v1/wizard/templates",
		`{"isoID":"`+iso.ID+`"}`, token)
	if code != 200 {
		t.Fatalf("选模板返回 %d：%s", code, got)
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Items) != 1 || res.Data.Items[0].ID != tpl.ID {
		t.Errorf("列出的模板是 %+v，应为 %s", res.Data.Items, tpl.ID)
	}
}

// 第四步：逐台一张参数表单，锁定的只显示，能改的已预填，必填没值的标出来（C20）。
func Test向导参数表单(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	body := `{"isoID":"` + iso.ID + `","templateID":"` + tpl.ID + `","templateVersion":1,` +
		`"hostSNs":["SN-1"]}`
	code, got := doWith(t, s, "POST", "/console/v1/wizard/params", body, token)
	if code != 200 {
		t.Fatalf("参数表单返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Hosts []struct {
				SN     string `json:"sn"`
				Fields []struct {
					Key      string `json:"key"`
					Label    string `json:"label"`
					Required bool   `json:"required"`
					Locked   bool   `json:"locked"`
					Prefill  string `json:"prefill"`
				} `json:"fields"`
			} `json:"hosts"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Hosts) != 1 || res.Data.Hosts[0].SN != "SN-1" {
		t.Fatalf("表单是 %+v，应逐台一张", res.Data.Hosts)
	}
	// hostname 是模板参数，取值来源是 default，可改，预填 node
	byKey := map[string]int{}
	for i, f := range res.Data.Hosts[0].Fields {
		byKey[f.Key] = i
	}
	hostname := res.Data.Hosts[0].Fields[byKey["tpl/hostname"]]
	if hostname.Prefill != "node" || hostname.Locked {
		t.Errorf("hostname 是 %+v，应预填 node 且可改", hostname)
	}
	// disk 是 hook 的参数，正文没引用它，按声明的 Required（false）算，没有 Binding 就可改
	disk := res.Data.Hosts[0].Fields[byKey["分区/1/disk"]]
	if disk.Label != "系统盘" {
		t.Errorf("disk 的列头是 %q，显示名应原样给出", disk.Label)
	}
}

// 第五步：提交。服务端不信任前端，整批重跑准入与参数求值，逐台建快照，一次建出全部任务（C30）。
func Test向导提交建任务(t *testing.T) {
	s, st, _, hub := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	for _, sn := range []string{"SN-1", "SN-2"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	body := `{"isoID":"` + iso.ID + `","templateID":"` + tpl.ID + `","templateVersion":1,` +
		`"hosts":[{"sn":"SN-1","entered":{"tpl/hostname":"node-1"}},` +
		`{"sn":"SN-2","entered":{"tpl/hostname":"node-2"}}]}`
	code, got := doWith(t, s, "POST", "/console/v1/wizard/tasks", body, token)
	if code != 200 {
		t.Fatalf("提交返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Tasks []struct {
				ID     string `json:"id"`
				HostSN string `json:"hostSN"`
				State  string `json:"state"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Tasks) != 2 {
		t.Fatalf("建出 %d 个任务，应为 2 个", len(res.Data.Tasks))
	}
	for _, task := range res.Data.Tasks {
		if task.State != "pending" {
			t.Errorf("%s 的状态是 %q，一建成就应是 pending", task.ID, task.State)
		}
	}
	// 快照固化了参数值（C29）
	back, err := st.GetTask(t.Context(), res.Data.Tasks[0].ID)
	if err != nil {
		t.Fatalf("读回任务：%v", err)
	}
	if back.Snapshot.Template.Params["hostname"].Items[0] != "node-1" {
		t.Errorf("快照里 hostname 是 %v，操作员填的值没固化进去",
			back.Snapshot.Template.Params["hostname"])
	}
	// 通知中心发了消息，前端据此补读（spec §6.2 第 9 条）
	if len(hub.fired) != 2 {
		t.Errorf("发布了 %d 条通知，应为 2 条", len(hub.fired))
	}
	// 每个任务一条审计（spec §6.2 第 8 条）
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	n := 0
	for _, e := range page.Items {
		if e.Action == "task.create" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("有 %d 条 task.create 审计，应为 2 条", n)
	}
}

// 一台不过，一台都不建，逐台列出原因（C30）。
func Test向导提交一台不过全不建(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	for _, sn := range []string{"SN-1", "SN-2"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	// SN-2 填一个不合法的 hostname
	body := `{"isoID":"` + iso.ID + `","templateID":"` + tpl.ID + `","templateVersion":1,` +
		`"hosts":[{"sn":"SN-1","entered":{"tpl/hostname":"node-1"}},` +
		`{"sn":"SN-2","entered":{"tpl/hostname":"bad host"}}]}`
	code, got := doWith(t, s, "POST", "/console/v1/wizard/tasks", body, token)
	if code != 422 {
		t.Fatalf("提交返回 %d，应为 422：%s", code, got)
	}
	if !strings.Contains(got, "SN-2") {
		t.Errorf("响应里没指出是哪一台：%s", got)
	}
	page, err := st.ListTasks(t.Context(), store.TaskQuery{Limit: 500})
	if err != nil {
		t.Fatalf("列任务：%v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("建出了 %d 个任务，一台都不该建", len(page.Items))
	}
	// 审计也没有：整批回滚
	audit, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	for _, e := range audit.Items {
		if e.Action == "task.create" {
			t.Error("回滚了却留下了 task.create 的审计")
		}
	}
}

// 提交的模板版本不是当前的已审批版本时报 409（C23：向导里每个模板只提供它当前的已审批版本）。
func Test向导提交旧版本(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	// 开一个版本 2 的草稿：版本 1 已不是「当前版本」
	if _, err := st.NewTemplateVersion(t.Context(), tpl.ID, 1, "root"); err != nil {
		t.Fatalf("开新版本：%v", err)
	}
	body := `{"isoID":"` + iso.ID + `","templateID":"` + tpl.ID + `","templateVersion":1,` +
		`"hosts":[{"sn":"SN-1"}]}`
	code, got := doWith(t, s, "POST", "/console/v1/wizard/tasks", body, token)
	if code != 409 {
		t.Errorf("提交旧版本返回 %d，应为 409：%s", code, got)
	}
}
```

Run: `go test ./internal/console/ -run 'Test向导' -v`
Expected: FAIL，`undefined: s.wizardRoutes`。

- [ ] **Step 2: 实现向导**

Create `internal/console/wizard.go`：

```go
package console

import (
	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/snapshot"
	"github.com/lober/lober/internal/store"
)

// wizardHostItem 是第一步里的一台主机。Selectable 为 false 时 Problems 说清为什么不能选。
type wizardHostItem struct {
	SN         string            `json:"sn"`
	Arch       catalog.Arch      `json:"arch"`
	Firmware   catalog.Firmware  `json:"firmware"`
	Attrs      map[string]string `json:"attrs"`
	Selectable bool              `json:"selectable"`
	Problems   []model.Problem   `json:"problems"`
}

// wizardHostPage 是第一步的一页主机。
type wizardHostPage struct {
	Items []wizardHostItem `json:"items"`
	Next  string           `json:"next"`
}

// wizardISOItem 是第二步里的一个 ISO。Usable 为 false 时 Problems 逐台指出是哪几台主机不相配。
type wizardISOItem struct {
	isoResponse
	Usable   bool            `json:"usable"`
	Problems []model.Problem `json:"problems"`
}

// wizardISOResult 是第二步的结果。
type wizardISOResult struct {
	Items []wizardISOItem `json:"items"`
}

// wizardTemplateItem 是第三步里的一个模板，取的是它当前的已审批版本（C23）。
type wizardTemplateItem struct {
	templateResponse
	Usable   bool            `json:"usable"`
	Problems []model.Problem `json:"problems"`
}

// wizardTemplateResult 是第三步的结果。
type wizardTemplateResult struct {
	Items []wizardTemplateItem `json:"items"`
}

// wizardParamsHost 是第四步里一台主机的一张表。
type wizardParamsHost struct {
	SN     string         `json:"sn"`
	Fields []params.Field `json:"fields"`
}

// wizardParamsResult 是第四步的结果：逐台一张表。
type wizardParamsResult struct {
	Hosts []wizardParamsHost `json:"hosts"`
}

// wizardSubmitHost 是第五步里一台主机的提交：SN 加操作员填的值。
type wizardSubmitHost struct {
	SN      string            `json:"sn"`
	Entered map[string]string `json:"entered"`
}

// wizardSubmitRequest 是第五步的请求体。
type wizardSubmitRequest struct {
	ISOID           string              `json:"isoID"`
	TemplateID      string              `json:"templateID"`
	TemplateVersion int                 `json:"templateVersion"`
	Hosts           []wizardSubmitHost  `json:"hosts"`
}

// wizardSubmitResult 是第五步的结果：建出来的任务。
type wizardSubmitResult struct {
	Tasks []model.Task `json:"tasks"`
}

// wizardRoutes 是向导的五步。每一步都只读，除了最后一步。
func (s *server) wizardRoutes() []route {
	return []route{
		{"GET", "/console/v1/wizard/hosts", PermTaskCreate, "", "向导", "第一步：选主机", nil, wizardHostPage{}, s.wizardHosts},
		{"POST", "/console/v1/wizard/isos", PermTaskCreate, "", "向导", "第二步：选 ISO", hostSNsRequest{}, wizardISOResult{}, s.wizardISOs},
		{"POST", "/console/v1/wizard/templates", PermTaskCreate, "", "向导", "第三步：选模板", isoRequest{}, wizardTemplateResult{}, s.wizardTemplates},
		{"POST", "/console/v1/wizard/params", PermTaskCreate, "", "向导", "第四步：填参数", paramsRequest{}, wizardParamsResult{}, s.wizardParams},
		{"POST", "/console/v1/wizard/tasks", PermTaskCreate, "task.create", "向导", "第五步：提交建任务", wizardSubmitRequest{}, wizardSubmitResult{}, s.wizardSubmit},
	}
}

// hostSNsRequest 是第二、三步的请求体：选中了哪些主机。
type hostSNsRequest struct {
	HostSNs []string `json:"hostSNs"`
}

// isoRequest 是第三步的请求体：选定了哪个 ISO。
type isoRequest struct {
	ISOID string `json:"isoID"`
}

// paramsRequest 是第四步的请求体：ISO、模板版本与选中了哪些主机。
type paramsRequest struct {
	ISOID           string   `json:"isoID"`
	TemplateID      string   `json:"templateID"`
	TemplateVersion int      `json:"templateVersion"`
	HostSNs         []string `json:"hostSNs"`
}

// wizardHosts 是第一步。按 SN 前缀搜，或者按属性键值搜：attr_key 与 attr_value 都要给，值
// 完全相等（总 spec §5.3 第 1 步「按 SN 或任意属性搜索」——「任意属性」指任意键，由 attr_key
// 指定，store.HostQuery 没有「任意键包含」这种查法）。
func (s *server) wizardHosts(c *gin.Context) {
	q := store.HostQuery{
		SNPrefix:  c.Query("q"),
		AttrKey:   c.Query("attr_key"),
		AttrValue: c.Query("attr_value"),
		Cursor:    c.Query("cursor"),
		Limit:     atoiOr(c.Query("limit"), 0),
	}
	page, ok := Get(c, func() (store.Page[model.Host], error) {
		return s.deps.Hosts.ListHosts(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]wizardHostItem, 0, len(page.Items))
	for _, h := range page.Items {
		// 有没有未结束的任务：FindOpenTask 的返回值就是它
		_, open, err := s.deps.Tasks.FindOpenTask(c.Request.Context(), h.SN)
		if err != nil {
			RespondError(c, err)
			return
		}
		problems := admission.HostSelectable(h, open)
		attrs := h.Attrs
		if attrs == nil {
			attrs = map[string]string{}
		}
		items = append(items, wizardHostItem{
			SN: h.SN, Arch: h.Arch, Firmware: h.Firmware, Attrs: attrs,
			Selectable: len(problems) == 0, Problems: problems,
		})
	}
	Ok(c, wizardHostPage{Items: items, Next: page.Next})
}

// wizardISOs 是第二步。只列可用的，与某几台冲突的照常列出并写明是哪几台（C28）。
func (s *server) wizardISOs(c *gin.Context) {
	var req hostSNsRequest
	if !Bind(c, &req) {
		return
	}
	hosts, ok := s.hostsOf(c, req.HostSNs)
	if !ok {
		return
	}
	page, ok := Get(c, func() (store.Page[model.ISO], error) {
		return s.deps.ISOs.ListISOs(c.Request.Context(), store.ISOQuery{Limit: 500})
	})
	if !ok {
		return
	}
	items := make([]wizardISOItem, 0, len(page.Items))
	for _, iso := range page.Items {
		problems := admission.ISOUsable(iso, hosts)
		items = append(items, wizardISOItem{
			isoResponse: toISOResponse(iso),
			Usable:      len(problems) == 0, Problems: problems,
		})
	}
	Ok(c, wizardISOResult{Items: items})
}

// wizardTemplates 是第三步。只列已审批、没引用已废弃 hook、勾了该 ISO 的系的模板，每个取当前的
// 已审批版本（C21、C22、C26、C27、C23）。
func (s *server) wizardTemplates(c *gin.Context) {
	var req isoRequest
	if !Bind(c, &req) {
		return
	}
	iso, ok := Get(c, func() (model.ISO, error) {
		return s.deps.ISOs.GetISO(c.Request.Context(), req.ISOID)
	})
	if !ok {
		return
	}
	page, ok := Get(c, func() (store.Page[model.Template], error) {
		return s.deps.Templates.ListTemplates(c.Request.Context(),
			store.TemplateQuery{Series: iso.Series, ApprovedOnly: true, Limit: 500})
	})
	if !ok {
		return
	}
	items := make([]wizardTemplateItem, 0, len(page.Items))
	for _, t := range page.Items {
		hooks, ok := s.hooksOf(c, t)
		if !ok {
			return
		}
		problems := admission.TemplateUsable(t, hooks, iso)
		items = append(items, wizardTemplateItem{
			templateResponse: toTemplateResponse(t),
			Usable:           len(problems) == 0, Problems: problems,
		})
	}
	Ok(c, wizardTemplateResult{Items: items})
}

// wizardParams 是第四步：逐台一张表。
func (s *server) wizardParams(c *gin.Context) {
	var req paramsRequest
	if !Bind(c, &req) {
		return
	}
	t, iso, ok := s.templateAndISO(c, req.TemplateID, req.TemplateVersion, req.ISOID)
	if !ok {
		return
	}
	hosts, ok := s.hostsOf(c, req.HostSNs)
	if !ok {
		return
	}
	hooks, ok := s.hooksOf(c, t)
	if !ok {
		return
	}
	out := make([]wizardParamsHost, 0, len(hosts))
	for _, h := range hosts {
		res := params.Evaluate(params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h})
		out = append(out, wizardParamsHost{SN: h.SN, Fields: res.Fields})
	}
	Ok(c, wizardParamsResult{Hosts: out})
}

// wizardSubmit 是第五步。服务端不信任前端的筛选结果：整批重跑准入判定、逐台重跑参数求值、
// 逐台构建快照，再一次建出全部任务（C29、C30）。任何一台不通过，一台都不建。
func (s *server) wizardSubmit(c *gin.Context) {
	var req wizardSubmitRequest
	if !Bind(c, &req) {
		return
	}
	if len(req.Hosts) == 0 {
		Fail(c, statusBadRequest, nil, "一台主机都没选")
		return
	}
	t, iso, ok := s.templateAndISO(c, req.TemplateID, req.TemplateVersion, req.ISOID)
	if !ok {
		return
	}
	// 提交的必须是当前的已审批版本（C23）：已被新版本取代的旧版本照样是已审批的，但向导里
	// 只提供当前那个，操作员选的不该是旧的
	current, err := s.deps.Templates.ListTemplateVersions(c.Request.Context(), t.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	// CurrentApproved 返回 (版本, 有没有)：它取的就是已审批里版本号最高的那个
	if approved, ok := model.CurrentApproved(current); !ok || approved.Version != t.Version {
		Fail(c, statusConflict, nil, "这个模板已经有新的已审批版本，用新的那个建任务")
		return
	}
	sns := make([]string, 0, len(req.Hosts))
	entered := make(map[string]map[string]string, len(req.Hosts))
	for _, h := range req.Hosts {
		sns = append(sns, h.SN)
		entered[h.SN] = h.Entered
	}
	hosts, ok := s.hostsOf(c, sns)
	if !ok {
		return
	}
	hooks, ok := s.hooksOf(c, t)
	if !ok {
		return
	}
	// 一、准入判定：整批重跑，问题按对象分开放
	open := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		_, has, err := s.deps.Tasks.FindOpenTask(c.Request.Context(), h.SN)
		if err != nil {
			RespondError(c, err)
			return
		}
		open[h.SN] = has
	}
	if result := admission.Batch(admission.BatchInput{Hosts: hosts, OpenTask: open, ISO: iso,
		Template: t, Hooks: hooks}); len(result.Common) > 0 || anyByHost(result.ByHost) {
		// 与仓储重跑不通过的形状一致，前端照一套渲染
		Fail(c, statusUnprocessable, batchDetail{Common: result.Common, ByHost: result.ByHost},
			firstMessage(result.Common, result.ByHost))
		return
	}
	// 二、逐台参数求值与快照构建
	tasks := make([]model.Task, 0, len(hosts))
	for _, h := range hosts {
		in := params.Input{Template: t, Hooks: hooks, ISO: iso, Host: h, Entered: enteredMap(entered[h.SN])}
		res := params.Evaluate(in)
		if len(res.Problems) > 0 {
			// 参数的问题落在这台主机上：一台不过，一台都不建
			Fail(c, statusUnprocessable, batchDetail{ByHost: map[string][]model.Problem{h.SN: res.Problems}},
				res.Problems[0].Message)
			return
		}
		snap, problems := snapshot.Build(in)
		if len(problems) > 0 {
			Fail(c, statusUnprocessable, batchDetail{ByHost: map[string][]model.Problem{h.SN: problems}},
				problems[0].Message)
			return
		}
		tasks = append(tasks, model.Task{
			HostSN: h.SN, ISOID: iso.ID, TemplateID: t.ID, TemplateVersion: t.Version,
			State: model.TaskPending, Snapshot: snap, CreatedAt: s.clock(),
		})
	}
	// 三、一次建出全部任务。仓储在事务里对加锁后的最新数据再跑一遍准入判定，不过就整批回滚
	created, ok := Write(c, func() ([]model.Task, error) {
		return s.deps.Tasks.CreateTasks(c.Request.Context(), tasks, s.auditFrom(c, ""))
	})
	if !ok {
		return
	}
	for _, task := range created {
		s.deps.Notifier.Publish(task.ID)
	}
	Ok(c, wizardSubmitResult{Tasks: created})
}

// templateAndISO 取模板版本与 ISO。第三、四、五步都要这两个。
func (s *server) templateAndISO(c *gin.Context, id string, version int, isoID string) (model.Template, model.ISO, bool) {
	t, ok := Get(c, func() (model.Template, error) {
		return s.deps.Templates.GetTemplate(c.Request.Context(),
			model.TemplateKey{ID: id, Version: version})
	})
	if !ok {
		return model.Template{}, model.ISO{}, false
	}
	iso, ok := Get(c, func() (model.ISO, error) {
		return s.deps.ISOs.GetISO(c.Request.Context(), isoID)
	})
	if !ok {
		return model.Template{}, model.ISO{}, false
	}
	return t, iso, true
}

// hostsOf 按 SN 批量取主机，只放存在的。少了谁由调用方决定怎么办：向导里少了就是没登记，
// 准入判定报 host_not_registered。
func (s *server) hostsOf(c *gin.Context, sns []string) ([]model.Host, bool) {
	m, ok := Get(c, func() (map[string]model.Host, error) {
		return s.deps.Hosts.GetHosts(c.Request.Context(), sns)
	})
	if !ok {
		return nil, false
	}
	out := make([]model.Host, 0, len(m))
	for _, sn := range sns {
		if h, ok := m[sn]; ok {
			out = append(out, h)
		}
	}
	return out, true
}

// hooksOf 取一个模板装配表引用的全部 hook 版本。
func (s *server) hooksOf(c *gin.Context, t model.Template) (map[model.HookKey]model.Hook, bool) {
	hooks, ok := Get(c, func() (map[model.HookKey]model.Hook, error) {
		return s.deps.Hooks.GetHooks(c.Request.Context(), store.HookRefs(t))
	})
	return hooks, ok
}

// anyByHost 报告有没有哪台主机有问题。ByHost 每台都有键，空切片算没问题。
func anyByHost(byHost map[string][]model.Problem) bool {
	for _, ps := range byHost {
		if len(ps) > 0 {
			return true
		}
	}
	return false
}

// enteredMap 把提交上来的参数值转成 params.Input 要的形式。
func enteredMap(in map[string]string) map[params.FieldKey]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[params.FieldKey]string, len(in))
	for k, v := range in {
		out[params.FieldKey(k)] = v
	}
	return out
}
```

`wizard.go` 的 import 加 `"github.com/gin-gonic/gin"`。

**`wizardISOItem` 嵌 `isoResponse`**：JSON 里它的字段与 `isoResponse` 平级，多出 `usable` 与 `problems`。嵌入匿名结构体在 `encoding/json` 里是展开的，正是要的形状。

**`wizardSubmitResult.Tasks` 直接给 `[]model.Task`**：任务带整份快照，向导第 5 步要列出每台的 SN 与登记属性（总 spec §5.3 第 5 步），快照里都有。第 6 层若要更瘦的形状，在这里加一个 `taskItem` 就行。

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add internal/console
git commit -m "feat(console): 向导五步，提交时整批重跑准入与参数求值（C30）"
```

---

---

## Task 12：任务、SSE 与任务预览

**Files:**
- Create: `internal/console/tasks.go`
- Modify: `internal/console/routes.go`
- Modify: `internal/console/server_test.go`（`stubTaskControl` 换成记调用的 mock）
- Test: `internal/console/tasks_test.go`

**Interfaces:**
- Consumes: Task 4 的 `TaskControl`、`Notifier`、`Deps`/`Bind`/`Get`/`Write`/`auditFrom`/`clock`；Task 11 的 `wizardSubmit`（建任务）；Task 10 的 `previewFile`/`previewResponse`/`previewToken`
- Produces:
  - `taskItem`、`taskPage`、`eventItem`、`eventPage`（OpenAPI 的 schema 名）
  - `(*server).taskRoutes() []route`

**取消与重试经过状态机**（总 spec §6.2 第 4 条）：`TaskControl` 由第 7 层实现。本任务的 mock 记下调了它，测试断言控制台把活儿交了过去了；`cmd/lober` 里的占位实现一律返回「第 7 层未实现」，不写临时规则（第 5 层设计 §6.2）。

- [ ] **Step 0: 接上任务路由**

Modify `internal/console/routes.go` 的 `allRoutes`，接上 `s.taskRoutes()`：

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),,
		s.userRoutes(),,
		s.hostRoutes(),,
		s.isoRoutes(),,
		s.hookRoutes(),,
		s.templateRoutes(),,
		s.wizardRoutes(),,
		s.taskRoutes(),
	)
}
```

- [ ] **Step 1: 写失败的测试——任务的列表、详情、事件**

Create `internal/console/tasks_test.go`：

```go
package console_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/store"
)

// seedTask 经向导建一个任务，省得每个用例都走一遍五步。
func seedTask(t *testing.T, s *console.Server, st store.Store, token, sn string) string {
	t.Helper()
	iso := readyISO(t, st, "iso-"+sn, "Rocky 9.6", catalog.SeriesRocky)
	h := seedHook(t, st, "h-"+sn, "lvm-"+sn)
	tpl := seedTemplate(t, st, h)
	body := `{"isoID":"` + iso.ID + `","templateID":"` + tpl.ID + `","templateVersion":1,` +
		`"hosts":[{"sn":"` + sn + `","entered":{"tpl/hostname":"node"}}]}`
	code, got := doWith(t, s, "POST", "/console/v1/wizard/tasks", body, token)
	if code != 200 {
		t.Fatalf("建任务返回 %d：%s", code, got)
	}
	var res struct {
		Data struct {
			Tasks []struct {
				ID string `json:"id"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(res.Data.Tasks) != 1 {
		t.Fatalf("建出 %d 个任务，应为 1 个", len(res.Data.Tasks))
	}
	return res.Data.Tasks[0].ID
}

func Test列任务(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	for _, sn := range []string{"SN-1", "SN-2"} {
		if _, err := st.CreateHost(t.Context(), model.Host{SN: sn},
			store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
			t.Fatalf("建 %s：%v", sn, err)
		}
	}
	seedTask(t, s, st, token, "SN-1")
	seedTask(t, s, st, token, "SN-2")
	code, body := doWith(t, s, "GET", "/console/v1/tasks", "", token)
	if code != 200 {
		t.Fatalf("列任务返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				ID     string `json:"id"`
				HostSN string `json:"hostSN"`
				State  string `json:"state"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Fatalf("列出 %d 个任务，应为 2 个", len(got.Data.Items))
	}
	// 按主机 SN 过滤
	code, body = doWith(t, s, "GET", "/console/v1/tasks?host_sn=SN-1", "", token)
	if code != 200 {
		t.Fatalf("按主机过滤返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 1 || got.Data.Items[0].HostSN != "SN-1" {
		t.Errorf("按 SN-1 过滤出 %+v", got.Data.Items)
	}
}

func Test取任务详情(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	id := seedTask(t, s, st, token, "SN-1")
	code, body := doWith(t, s, "GET", "/console/v1/tasks/"+id, "", token)
	if code != 200 {
		t.Fatalf("取详情返回 %d：%s", code, body)
	}
	// 快照整份带回：控制台从它读主机、ISO、模板与参数（C29）
	if !strings.Contains(body, "hostname") {
		t.Errorf("响应里没有参数：%s", body)
	}
	var got struct {
		Data struct {
			ID     string `json:"id"`
			State  string `json:"state"`
			Snapshot struct {
				Series string `json:"series"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if got.Data.Snapshot.Series != "rocky" {
		t.Errorf("快照的系是 %q，应为 rocky", got.Data.Snapshot.Series)
	}
}

func Test取任务事件(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	id := seedTask(t, s, st, token, "SN-1")
	if _, err := st.AppendEvents(t.Context(), id, []store.NewEvent{
		{At: fixedAt, Kind: "slot.start", Key: "s1", Fields: map[string]string{"slot": "分区"}},
		{At: fixedAt, Kind: "slot.exit", Key: "s2", Fields: map[string]string{"code": "0"}},
	}); err != nil {
		t.Fatalf("追加事件：%v", err)
	}
	code, body := doWith(t, s, "GET", "/console/v1/tasks/"+id+"/events", "", token)
	if code != 200 {
		t.Fatalf("取事件返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				Seq  int64  `json:"seq"`
				Kind string `json:"kind"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Fatalf("列出 %d 条事件，应为 2 条", len(got.Data.Items))
	}
	if got.Data.Items[0].Seq != 1 || got.Data.Items[1].Seq != 2 {
		t.Errorf("序号是 %d、%d，应按 Seq 升序且从 1 起", got.Data.Items[0].Seq, got.Data.Items[1].Seq)
	}
	// 续读：afterSeq 之后的
	code, body = doWith(t, s, "GET", "/console/v1/tasks/"+id+"/events?after_seq=1", "", token)
	if code != 200 {
		t.Fatalf("续读返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 1 || got.Data.Items[0].Seq != 2 {
		t.Errorf("续读出 %+v，应只有第 2 条", got.Data.Items)
	}
}

// 取消与重试经过 TaskControl（总 spec §6.2 第 4 条）。
func Test取消与重试交给状态机(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	ctl := &mockTaskControl{}
	s.SetTaskControl(ctl) // 测试后门，见 export_test.go
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	id := seedTask(t, s, st, token, "SN-1")
	if code, got := doWith(t, s, "POST", "/console/v1/tasks/"+id+"/cancel", "", token); code != 200 {
		t.Fatalf("取消返回 %d：%s", code, got)
	}
	if code, got := doWith(t, s, "POST", "/console/v1/tasks/"+id+"/retry", "", token); code != 200 {
		t.Fatalf("重试返回 %d：%s", code, got)
	}
	if len(ctl.cancelled) != 1 || ctl.cancelled[0] != id {
		t.Errorf("Cancel 收到 %v，应收到 %s 一次", ctl.cancelled, id)
	}
	if len(ctl.retried) != 1 || ctl.retried[0] != id {
		t.Errorf("Retry 收到 %v，应收到 %s 一次", ctl.retried, id)
	}
}

// 任务预览渲染固化了的那份快照，与真实下发同一条路（C35）。
func Test任务预览(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	id := seedTask(t, s, st, token, "SN-1")
	code, got := doWith(t, s, "POST", "/console/v1/tasks/"+id+"/preview", `{"firmware":"uefi"}`, token)
	if code != 200 {
		t.Fatalf("任务预览返回 %d：%s", code, got)
	}
	if !strings.Contains(got, "kickstart") {
		t.Errorf("预览里没有应答文件：%s", got)
	}
	// 预览不写任何东西
	back, err := st.GetTask(t.Context(), id)
	if err != nil {
		t.Fatalf("读回任务：%v", err)
	}
	if back.State != model.TaskPending || back.Rev != 1 {
		t.Errorf("预览把任务改成了 %+v", back)
	}
}

// SSE：连上先推已有事件，之后收到通知再增量推（spec §6.2 第 9 条）。
func Test任务事件流(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1"},
		store.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatalf("建主机：%v", err)
	}
	id := seedTask(t, s, st, token, "SN-1")
	if _, err := st.AppendEvents(t.Context(), id, []store.NewEvent{
		{At: fixedAt, Kind: "slot.start", Key: "s1"},
	}); err != nil {
		t.Fatalf("追加事件：%v", err)
	}
	// 起一个真的 HTTP 服务：SSE 要的是流，httptest.Recorder 等不到
	srv := httptest.NewServer(s.Engine)
	defer srv.Close()
	req, err := http.NewRequest("GET", srv.URL+"/console/v1/tasks/"+id+"/stream?access_token="+token, nil)
	if err != nil {
		t.Fatalf("建请求：%v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("连上来：%v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type 是 %q，应为 text/event-stream", ct)
	}
	// 第一条事件应该是已有的那条
	line := readLine(t, resp.Body)
	if !strings.Contains(line, "slot.start") {
		t.Errorf("第一行是 %q，应带上已有的事件", line)
	}
	// 再追加一条，通知中心发消息，连接应马上收到
	if _, err := st.AppendEvents(t.Context(), id, []store.NewEvent{
		{At: fixedAt, Kind: "slot.exit", Key: "s2"},
	}); err != nil {
		t.Fatalf("再追加事件：%v", err)
	}
	s.Notify(id) // 测试后门：模拟状态机的发布
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line = readLine(t, resp.Body)
		if strings.Contains(line, "slot.exit") {
			return
		}
	}
	t.Errorf("3 秒内没收到新事件，最后读到 %q", line)
}

// readLine 读一行，超时就失败：SSE 卡住不该让测试挂住。
func readLine(t *testing.T, r io.Reader) string {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := r.Read(buf)
		ch <- result{string(buf[:n]), err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("读事件流：%v", got.err)
		}
		return got.s
	case <-time.After(3 * time.Second):
		t.Fatal("3 秒没读到东西")
		return ""
	}
}
```

`tasks_test.go` 的 import 加 `"io"`、`"github.com/lober/lober/internal/console"`、`"github.com/lober/lober/internal/core/catalog"`、`"github.com/lober/lober/internal/core/model"`。

Modify `internal/console/server_test.go`，把 `stubTaskControl` 换成：

```go
// mockTaskControl 记下取消与重试交给了谁。第 7 层的状态机到位后由 cmd/lober 注入真的。
type mockTaskControl struct {
	mu        sync.Mutex
	cancelled []string
	retried   []string
	canCancel []model.Problem
	canRetry  []model.Problem
}

func (m *mockTaskControl) Cancel(ctx context.Context, t model.Task, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelled = append(m.cancelled, t.ID)
	return nil
}

func (m *mockTaskControl) Retry(ctx context.Context, t model.Task, a store.Audit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retried = append(m.retried, t.ID)
	return nil
}

func (m *mockTaskControl) CanCancel(t model.Task) []model.Problem { return m.canCancel }
func (m *mockTaskControl) CanRetry(t model.Task) []model.Problem  { return m.canRetry }
```

`harness_test.go` 的 `newStoreServer` 里 `TaskCtl: &mockTaskControl{}`。但 `Test取消与重试交给状态机` 要能换一个自己的，所以在 `export_test.go` 加后门：

```go
// SetTaskControl 是测试后门：换掉 TaskControl，测试因此能断言控制台把活儿交给了它。
func (s *Server) SetTaskControl(tc TaskControl) { s.server.deps.TaskCtl = tc }

// Notify 是测试后门：代替第 7 层的状态机发一条通知。
func (s *Server) Notify(taskID string) { s.server.hub.Publish(taskID) }
```

Run: `go test ./internal/console/ -run 'Test列任务|Test取任务|Test取消与重试|Test任务事件流' -v`
Expected: FAIL，`undefined: s.taskRoutes`、`undefined: (*console.Server).SetTaskControl`。

- [ ] **Step 2: 实现任务端点与 SSE**

Create `internal/console/tasks.go`：

```go
package console

import (
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// taskItem 是一个任务对外的形状。快照整份带回：界面要从它读主机、ISO、模板与参数（C29）。
type taskItem struct {
	ID              string          `json:"id"`
	HostSN          string          `json:"hostSN"`
	ISOID           string          `json:"isoID"`
	TemplateID      string          `json:"templateID"`
	TemplateVersion int             `json:"templateVersion"`
	State           model.TaskState `json:"state"`
	Snapshot        model.Snapshot  `json:"snapshot"`
	CreatedBy       string          `json:"createdBy"`
	CreatedAt       int64           `json:"createdAt"`
	Rev             int64           `json:"rev"`
	// CanCancel 与 CanRetry 给界面置灰。第 7 层没就绪时 TaskControl 一律说不能，并给出原因。
	CanCancel []model.Problem `json:"canCancel"`
	CanRetry  []model.Problem `json:"canRetry"`
}

// taskPage 是一页任务。
type taskPage struct {
	Items []taskItem `json:"items"`
	Next  string     `json:"next"`
}

// eventItem 是一条事件。Kind 与 Fields 的含义由第 7 层定，仓储不解释它们（store.NewEvent）。
type eventItem struct {
	TaskID string            `json:"taskID"`
	Seq    int64             `json:"seq"`
	At     int64             `json:"at"`
	Kind   string            `json:"kind"`
	Key    string            `json:"key"`
	Fields map[string]string `json:"fields"`
}

// eventPage 是一页事件，按 Seq 升序。
type eventPage struct {
	Items []eventItem `json:"items"`
}

func toTaskItem(t model.Task) taskItem {
	return taskItem{
		ID: t.ID, HostSN: t.HostSN, ISOID: t.ISOID, TemplateID: t.TemplateID,
		TemplateVersion: t.TemplateVersion, State: t.State, Snapshot: t.Snapshot,
		CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, Rev: t.Rev,
		CanCancel: s.canCancel(t), CanRetry: s.canRetry(t),
	}
}

// taskRoutes 是任务的七个端点。
func (s *server) taskRoutes() []route {
	return []route{
		{"GET", "/console/v1/tasks", PermTaskRead, "", "任务", "列出任务", nil, taskPage{}, s.listTasks},
		{"GET", "/console/v1/tasks/{id}", PermTaskRead, "", "任务", "取一个任务", nil, taskItem{}, s.getTask},
		{"GET", "/console/v1/tasks/{id}/events", PermTaskRead, "", "任务", "取任务的事件", nil, eventPage{}, s.listEvents},
		{"GET", "/console/v1/tasks/{id}/stream", PermTaskRead, "", "任务", "任务的事件流（SSE）", nil, nil, s.taskStream},
		{"POST", "/console/v1/tasks/{id}/cancel", PermTaskCancel, "task.cancel", "任务", "取消任务", nil, taskItem{}, s.cancelTask},
		{"POST", "/console/v1/tasks/{id}/retry", PermTaskRetry, "task.retry", "任务", "重试任务", nil, taskItem{}, s.retryTask},
		{"POST", "/console/v1/tasks/{id}/preview", PermTaskRead, "", "任务", "预览这个任务要下发的文件", previewRequest{}, previewResponse{}, s.previewTask},
	}
}

func (s *server) listTasks(c *gin.Context) {
	q := store.TaskQuery{
		HostSN:     c.Query("host_sn"),
		Cursor:     c.Query("cursor"),
		Limit:      atoiOr(c.Query("limit"), 0),
		CreatedFrom: atoiOr(c.Query("created_from"), 0),
		CreatedTo:   atoiOr(c.Query("created_to"), 0),
	}
	if st := c.Query("state"); st != "" {
		q.States = []model.TaskState{model.TaskState(st)}
	}
	page, ok := Get(c, func() (store.Page[model.Task], error) {
		return s.deps.Tasks.ListTasks(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]taskItem, 0, len(page.Items))
	for _, t := range page.Items {
		items = append(items, toTaskItem(t))
	}
	Ok(c, taskPage{Items: items, Next: page.Next})
}

func (s *server) getTask(c *gin.Context) {
	t, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	Ok(c, toTaskItem(t))
}

func (s *server) listEvents(c *gin.Context) {
	events, ok := Get(c, func() ([]store.Event, error) {
		return s.deps.Events.ListEvents(c.Request.Context(), c.Param("id"),
			atoiOr(c.Query("after_seq"), 0), atoiOr(c.Query("limit"), 0))
	})
	if !ok {
		return
	}
	items := make([]eventItem, 0, len(events))
	for _, e := range events {
		fields := e.Fields
		if fields == nil {
			fields = map[string]string{}
		}
		items = append(items, eventItem{TaskID: e.TaskID, Seq: e.Seq, At: e.At,
			Kind: e.Kind, Key: e.Key, Fields: fields})
	}
	Ok(c, eventPage{Items: items})
}

// taskStream 是任务的事件流。连上先按 Seq 补读全量推过去，之后收到「任务 X 有更新」的通知再
// 增量补读；另有 5 秒兜底轮询，通知丢了不错乱（spec §6.2 第 9 条）。
func (s *server) taskStream(c *gin.Context) {
	id := c.Param("id")
	if _, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), id)
	}); !ok {
		return // Get 已经写出 404
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Flush()
	ch, unsub := s.deps.Notifier.Subscribe(id)
	defer unsub()
	var last int64
	send := func() bool {
		events, err := s.deps.Events.ListEvents(c.Request.Context(), id, last, 200)
		if err != nil {
			return false // 任务被删了之类的：流到此为止
		}
		for _, e := range events {
			c.SSEvent("task", eventItem{TaskID: e.TaskID, Seq: e.Seq, At: e.At,
				Kind: e.Kind, Key: e.Key, Fields: e.Fields})
			last = e.Seq
		}
		c.Writer.Flush()
		return true
	}
	if !send() {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ch:
			if !send() {
				return
			}
		case <-ticker.C:
			if !send() {
				return
			}
		case <-c.Request.Context().Done():
			return // 客户端断开
		}
	}
}

func (s *server) cancelTask(c *gin.Context) {
	t, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	if err := s.deps.TaskCtl.Cancel(c.Request.Context(), t, s.auditFrom(c, "")); err != nil {
		controlFailed(c, "取消", err)
		return
	}
	back, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	s.deps.Notifier.Publish(back.ID)
	Ok(c, toTaskItem(back))
}

func (s *server) retryTask(c *gin.Context) {
	t, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	if err := s.deps.TaskCtl.Retry(c.Request.Context(), t, s.auditFrom(c, "")); err != nil {
		controlFailed(c, "重试", err)
		return
	}
	back, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	s.deps.Notifier.Publish(back.ID)
	Ok(c, toTaskItem(back))
}

// controlFailed 把状态机的拒绝写成响应。它给的是 model.Problem 时按类别映射；是仓储的错误时
// 按哨兵值映射；第 7 层没就绪时占位实现给一句「第 7 层未实现」，原样带给操作员。
func controlFailed(c *gin.Context, what string, err error) {
	var ps []model.Problem
	if errors.As(err, &ps) {
		Judge(c, ps)
		return
	}
	Fail(c, statusUnprocessable, nil, what+"没成功："+err.Error())
}

// previewTask 渲染这个任务固化了的那份快照。与网关下发走同一个 render.Render（C35），
// 预览的 token 是示例值（spec §6.2 第 2 条）。
func (s *server) previewTask(c *gin.Context) {
	var req previewRequest
	if !Bind(c, &req) {
		return
	}
	t, ok := Get(c, func() (model.Task, error) {
		return s.deps.Tasks.GetTask(c.Request.Context(), c.Param("id"))
	})
	if !ok {
		return
	}
	firmware := req.Firmware
	if firmware == "" {
		firmware = catalog.FirmwareUEFI
	}
	// 固件取任务快照里钉住的那个：预览看的该是这台机器实际要用的那份
	if t.Snapshot.Host.Firmware != "" {
		firmware = t.Snapshot.Host.Firmware
	}
	values := map[catalog.SystemValue]string{
		catalog.SysGateway: "https://gateway.example.invalid",
		catalog.SysTask:    t.ID,
		catalog.SysSN:      t.HostSN,
		catalog.SysSMB:     `\\smb.example.invalid\lober`,
		"syslog-addr":      "udp://syslog.example.invalid:514",
		"artifacts":        "https://artifacts.example.invalid",
		"token":            previewToken,
		"finalize-token":   previewToken + "-finalize",
	}
	files := make([]previewFile, 0, 4)
	for _, kind := range catalog.DeliveryFiles(t.Snapshot.Format) {
		name, ok := catalog.DeliveryFileName(t.Snapshot.Format, kind)
		if !ok {
			continue
		}
		body, ps, err := render.Render(render.Request{
			Snapshot: t.Snapshot, Firmware: firmware, Values: values, Kind: kind,
			ESXiBootConfig: esxiBootCfgSample,
		})
		if err != nil {
			RespondError(c, err)
			return
		}
		if len(ps) > 0 {
			Ok(c, previewResponse{Files: []previewFile{}, Problems: ps, Sample: []string{}})
			return
		}
		files = append(files, previewFile{Kind: kind, Name: name, Body: string(body)})
	}
	Ok(c, previewResponse{Files: files, Problems: []model.Problem{}, Sample: []string{}})
}
```

`tasks.go` 的 import 加 `"errors"`、`"github.com/lober/lober/internal/core/catalog"`、`"github.com/lober/lober/internal/render"`、`"github.com/gin-gonic/gin"`。

`toTaskItem` 上面引用了不存在的 `s`，改成方法：

```go
func (s *server) toTaskItem(t model.Task) taskItem {
	return taskItem{
		...
		CanCancel: s.deps.TaskCtl.CanCancel(t), CanRetry: s.deps.TaskCtl.CanRetry(t),
	}
}
```

所有 `toTaskItem(x)` 的调用处改成 `s.toTaskItem(x)`。

`errors.As(err, &ps)` 接 `[]model.Problem` 编译不过（`errors.As` 的第二个参数要指向实现 error 的类型）。改成：

```go
func controlFailed(c *gin.Context, what string, err error) {
	var ps problemError
	if errors.As(err, &ps) {
		Judge(c, ps.problems)
		return
	}
	Fail(c, statusUnprocessable, nil, what+"没成功："+err.Error())
}

// problemError 让状态机能返回一组 model.Problem：实现 error 的类型才能被 errors.As 认出来。
type problemError struct{ problems []model.Problem }

func (e problemError) Error() string {
	if len(e.problems) == 0 {
		return "不允许"
	}
	return e.problems[0].Message
}
```

`taskcontrol.go` 的注释里补一句：实现方要拒绝时返回 `console.ProblemError{...}`（包级构造函数）：

```go
// Problems 是把一组 model.Problem 包成 error 的构造函数，给 TaskControl 的实现用：C34 的
// 拒绝是有原因可说的，该走 problems 那条路，前端才标得出来。
func Problems(ps []model.Problem) error { return problemError{problems: ps} }
```

`cmd/lober` 的占位实现因此返回一个普通 error（`errors.New("第 7 层（状态机）尚未实现……")`），由 `controlFailed` 映射成 422 并把原话带给操作员。

**`CanCancel`/`CanRetry` 为什么也让占位实现返回 nil**：它们要返回 `[]model.Problem`，而代码表里没有「这个状态不许取消/重试」这一条——第 7 层实现 C34 时要先往 `codes.go` 追加，同步改附录总表与 `TestCode表与附录总表逐项一致`。本层不能替它加，所以占位实现返回 nil：界面上按钮是亮的，点下去得到 422 和一句「第 7 层未实现」。这比灰着一个其实能用的按钮诚实。

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add internal/console
git commit -m "feat(console): 任务的列表、详情、事件、SSE 与预览，取消重试交给状态机"
```

---

---

## Task 13：审计查询与 CSV 导出

**Files:**
- Create: `internal/console/audit.go`
- Modify: `internal/console/routes.go`
- Test: `internal/console/audit_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Deps`/`Get`；Task 5 的夹具与 `adminToken`；Task 6 的 `atoiOr`
- Produces:
  - `auditItem`、`auditPage`（OpenAPI 的 schema 名）
  - `(*server).auditRoutes() []route`

**本任务收尾两件事**：① 权限码的反方向——每个码都有路由用到；② 审计的反方向——路由表里声明了动作的每一条，打一次都真的留下审计记录。

- [ ] **Step 1: 写失败的测试**

Create `internal/console/audit_test.go`：

```go
package console_test

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/store"
)

func Test查审计(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	// 造几条审计：登记两台主机、建一个用户
	doWith(t, s, "POST", "/console/v1/hosts", hostBody("SN-1", "x86_64", "", nil), token)
	doWith(t, s, "POST", "/console/v1/hosts", hostBody("SN-2", "x86_64", "", nil), token)
	doWith(t, s, "POST", "/console/v1/users",
		`{"username":"bob","role":"viewer","password":"correct horse"}`, token)
	code, body := doWith(t, s, "GET", "/console/v1/audit", "", token)
	if code != 200 {
		t.Fatalf("查审计返回 %d：%s", code, body)
	}
	var got struct {
		Data struct {
			Items []struct {
				TargetKind string `json:"targetKind"`
				TargetID   string `json:"targetID"`
				Actor      string `json:"actor"`
				Action     string `json:"action"`
				At         int64  `json:"at"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 3 {
		t.Fatalf("列出 %d 条，应为 3 条", len(got.Data.Items))
	}
	// 按 ID 倒序：最新的一条在前（store.AuditQuery 的约定）
	if got.Data.Items[0].At < got.Data.Items[2].At {
		t.Error("不是按时间倒序")
	}
	// 四个过滤条件各一例
	for name, query := range map[string]string{
		"按对象种类": "?target_kind=host",
		"按对象":    "?target_kind=host&target_id=SN-1",
		"按操作者":  "?actor=root",
		"按动作":    "?action=host.create",
	} {
		t.Run(name, func(t *testing.T) {
			code, body := doWith(t, s, "GET", "/console/v1/audit"+query, "", token)
			if code != 200 {
				t.Fatalf("返回 %d：%s", code, body)
			}
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("响应不是 JSON：%v", err)
			}
			if len(got.Data.Items) == 0 {
				t.Errorf("%s 过滤出 0 条", name)
			}
		})
	}
	// 按动作过滤：host.create 恰好两条
	code, body = doWith(t, s, "GET", "/console/v1/audit?action=host.create", "", token)
	if code != 200 {
		t.Fatalf("返回 %d：%s", code, body)
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if len(got.Data.Items) != 2 {
		t.Errorf("host.create 有 %d 条，应为 2 条", len(got.Data.Items))
	}
}

// 导出 CSV：不带响应包裹，列齐全，一行一条（总 spec §4.5「审计」）。
func Test导出审计CSV(t *testing.T) {
	s, _, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	doWith(t, s, "POST", "/console/v1/hosts", hostBody("SN-1", "x86_64", "", nil), token)
	req := httptest.NewRequest("GET", "/console/v1/audit/export", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Engine.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("导出返回 %d：%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type 是 %q，应为 text/csv", ct)
	}
	// 内容不该是信封
	if strings.Contains(w.Body.String(), `"code"`) {
		t.Error("导出带了响应包裹")
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil {
		t.Fatalf("CSV 读不成：%v\n%s", err, w.Body.String())
	}
	if len(rows) < 2 {
		t.Fatalf("只有 %d 行，应有表头加至少一条", len(rows))
	}
	wantHeader := []string{"id", "at", "actor", "action", "target_kind", "target_id", "detail"}
	if strings.Join(rows[0], ",") != strings.Join(wantHeader, ",") {
		t.Errorf("表头是 %v，应为 %v", rows[0], wantHeader)
	}
}

// 权限码的反方向：每个码都至少被一条路由用到。前面的任务只钉了「路由用的码都登记过」。
func Test每个权限码都有路由用到(t *testing.T) {
	s := newTestServer(t)
	used := map[string]bool{}
	for _, r := range s.Routes() {
		if r.Perm != "" && r.Perm != console.PermAnyUser {
			used[r.Perm] = true
		}
	}
	for _, p := range console.AllPerms() {
		if !used[p] {
			t.Errorf("权限码 %q 没有任何路由用到：加码时忘了接路由，或者这条路其实不需要它", p)
		}
	}
}

// 审计的反方向：路由表里声明了动作的每一条，打一次都真的留下审计记录（总 spec §6.2 第 8 条、
// §7.2 第 5 条）。免登录与不要审计的跳过；需要前置数据的（建任务、审批）由各自的用例覆盖，
// 这里列个清单，漏一条就补。
func Test每条审计路由都留下记录(t *testing.T) {
	s, st, _, _ := newStoreServer(t)
	token := adminToken(t, s, st)
	// 先造出打得出去的前置：一台主机、一个 ISO、一个 hook、一个模板
	doWith(t, s, "POST", "/console/v1/hosts", hostBody("SN-1", "x86_64", "", nil), token)
	iso := readyISO(t, st, "iso-1", "Rocky 9.6", catalog.SeriesRocky)
	h := seedHook(t, st, "h1", "lvm")
	tpl := seedTemplate(t, st, h)
	approver := approveAs(t, s, st, "bob")
	// 逐个打一遍，记下期望的动作
	steps := []struct {
		method, path, body, action string
	}{
		{"POST", "/console/v1/users", `{"username":"carol","role":"viewer","password":"x"}`, "user.create"},
		{"PUT", "/console/v1/users/carol", `{"username":"carol","role":"operator"}`, "user.update"},
		{"DELETE", "/console/v1/users/carol", "", "user.disable"},
		{"PUT", "/console/v1/users/root/name", `{"name":"root2"}`, "user.rename"},
		{"POST", "/console/v1/hosts", hostBody("SN-9", "x86_64", "", nil), "host.create"},
		{"PUT", "/console/v1/hosts/SN-9", hostBodyRev("SN-9", "aarch64", "", 1), "host.update"},
		{"DELETE", "/console/v1/hosts/SN-9?rev=2", "", "host.delete"},
		{"POST", "/console/v1/hosts/import", "sn,arch\nSN-8,x86_64\n", "host.import"},
		{"POST", "/console/v1/isos",
			`{"name":"Rocky 9.5","family":"linux","series":"rocky","sources":[{"kind":"url","addr":"http://a/x.iso"}]}`,
			"iso.create"},
		{"POST", "/console/v1/isos/" + iso.ID + "/deprecate?rev=1", "", "iso.deprecate"},
		{"POST", "/console/v1/isos/" + iso.ID + "/cancel", "", "iso.cancel"},
		{"POST", "/console/v1/isos/" + iso.ID + "/retry", "", "iso.retry"},
		{"POST", "/console/v1/isos/" + iso.ID + "/reimport", "", "iso.reimport"},
		{"POST", "/console/v1/hooks", hookBody("net", "网络"), "hook.create"},
		{"POST", "/console/v1/templates", templateBody("tpl2", h.ID), "template.create"},
		{"PUT", "/console/v1/hooks/" + h.ID + "/name", `{"name":"lvm2"}`, "hook.rename"},
		{"PUT", "/console/v1/templates/" + tpl.ID + "/name", `{"name":"rhel-base2"}`, "template.rename"},
		{"POST", "/console/v1/hooks/" + h.ID + "/versions/1/submit?rev=1", "", "hook.submit"},
		{"POST", "/console/v1/hooks/" + h.ID + "/versions/1/approve?rev=2", "", "hook.approve"},
		{"POST", "/console/v1/hooks/" + h.ID + "/versions/1/reject?rev=3", `{"reason":"x"}`, "hook.reject"},
		{"POST", "/console/v1/templates/" + tpl.ID + "/versions/1/submit?rev=1", "", "template.submit"},
	}
	for _, step := range steps {
		code, got := doWith(t, s, step.method, step.path, step.body, tokenFor(step.path, token, approver))
		if code != 200 {
			t.Errorf("%s %s 返回 %d：%s", step.method, step.path, code, got)
		}
	}
	page, err := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatalf("查审计：%v", err)
	}
	seen := map[string]bool{}
	for _, e := range page.Items {
		seen[e.Action] = true
	}
	for _, step := range steps {
		if !seen[step.action] {
			t.Errorf("没有 %s 的审计记录", step.action)
		}
	}
	// 路由表里声明了动作的，每一条都在上面这个清单里：漏一条就补，别让声明了却没人验
	declared := map[string]bool{}
	for _, r := range s.Routes() {
		if r.Audit != "" {
			declared[r.Audit] = true
		}
	}
	for action := range declared {
		if !seen[action] {
			t.Errorf("路由表声明了 %s，却没有用例打下它：补进上面的 steps，或者把它从声明里去掉", action)
		}
	}
}

// tokenFor 给需要审批人身份的几步换令牌。审批人不能是作者（C22），所以 approve 与 reject 用
// approver 那个账号打。
func tokenFor(path, author, approver string) string {
	if strings.Contains(path, "/approve") || strings.Contains(path, "/reject") {
		return approver
	}
	return author
}
```

`audit_test.go` 的 import 加 `"encoding/json"`、`"net/http/httptest"`、`"github.com/lober/lober/internal/console"`、`"github.com/lober/lober/internal/core/catalog"`。

**`PUT /users/{username}/name` 这条端点本计划没有**：上面的清单里写了它，`Test每个权限码都有路由用到` 也会因为 `user:manage` 只有四条路由用到而过得去，但 `Test每条审计路由都留下记录` 的最后一段会查出「路由表声明了 `user.rename`，却没有用例打下它」。**实现时补上这条路由**（`PUT /console/v1/users/{username}/name`，权限 `user:manage`，审计 `user.rename`，调 `UpdateUser` 时只改用户名——用户名是身份不能改，所以这条路由其实应该删掉）。

**正确的做法**：把 `user.rename` 从清单里删掉，用户名不可改（Task 3 定下的：按用户名找行，换个用户名就是另一个人）。实现时把上面 steps 里那两行 `user.rename` 删掉，`declared` 的检查自然就没有 `user.rename` 了。

Run: `go test ./internal/console/ -run 'Test查审计|Test导出审计|Test每个权限码|Test每条审计路由' -v`
Expected: FAIL，`undefined: s.auditRoutes`。

- [ ] **Step 2: 实现审计端点**

Create `internal/console/audit.go`：

```go
package console

import (
	"encoding/csv"
	"strconv"

	"github.com/lober/lober/internal/store"
)

// auditItem 是一条审计。Detail 是驳回审批的理由这类补充说明。
type auditItem struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetID"`
}

// auditPage 是一页审计，按 ID 倒序（store.AuditQuery 的约定）。
type auditPage struct {
	Items []auditItem `json:"items"`
	Next  string      `json:"next"`
}

func toAuditItem(e store.AuditEntry) auditItem {
	return auditItem{ID: e.ID, At: e.At, Actor: e.Actor, Action: e.Action,
		Detail: e.Detail, TargetKind: e.TargetKind, TargetID: e.TargetID}
}

// auditRoutes 是审计的两个端点。导出不带响应包裹：它是给 Excel 打开的文件，不是给前端
// 拦截器处理的 JSON（总 spec §4.5）。
func (s *server) auditRoutes() []route {
	return []route{
		{"GET", "/console/v1/audit", PermAuditRead, "", "审计", "查审计记录", nil, auditPage{}, s.listAudit},
		{"GET", "/console/v1/audit/export", PermAuditRead, "", "审计", "导出审计记录 CSV", nil, nil, s.exportAudit},
	}
}

func (s *server) listAudit(c *gin.Context) {
	q := store.AuditQuery{
		TargetKind: c.Query("target_kind"),
		TargetID:   c.Query("target_id"),
		Actor:      c.Query("actor"),
		Action:     c.Query("action"),
		From:       atoiOr(c.Query("from"), 0),
		To:         atoiOr(c.Query("to"), 0),
		Cursor:     c.Query("cursor"),
		Limit:      atoiOr(c.Query("limit"), 0),
	}
	page, ok := Get(c, func() (store.Page[store.AuditEntry], error) {
		return s.deps.Audit.ListAudit(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	items := make([]auditItem, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, toAuditItem(e))
	}
	Ok(c, auditPage{Items: items, Next: page.Next})
}

// exportAudit 导出 CSV。过滤条件与 listAudit 相同，一次取完一页（500 条）：再要多就该让
// 操作员缩小时间范围，而不是让他等一个几 MB 的下载。
func (s *server) exportAudit(c *gin.Context) {
	q := store.AuditQuery{
		TargetKind: c.Query("target_kind"),
		TargetID:   c.Query("target_id"),
		Actor:      c.Query("actor"),
		Action:     c.Query("action"),
		From:       atoiOr(c.Query("from"), 0),
		To:         atoiOr(c.Query("to"), 0),
		Limit:      500,
	}
	page, ok := Get(c, func() (store.Page[store.AuditEntry], error) {
		return s.deps.Audit.ListAudit(c.Request.Context(), q)
	})
	if !ok {
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="audit.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"id", "at", "actor", "action", "target_kind", "target_id", "detail"})
	for _, e := range page.Items {
		_ = w.Write([]string{strconv.FormatInt(e.ID, 10), strconv.FormatInt(e.At, 10),
			e.Actor, e.Action, e.TargetKind, e.TargetID, e.Detail})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		slog.Error("写审计 CSV", "err", err)
	}
}
```

`audit.go` 的 import 加 `"log/slog"` 与 `"github.com/gin-gonic/gin"`。

```go
func (s *server) allRoutes() []route {
	return slices.Concat(
		s.authRoutes(),
		s.userRoutes(),
		s.hostRoutes(),
		s.isoRoutes(),
		s.hookRoutes(),
		s.templateRoutes(),
		s.wizardRoutes(),
		s.taskRoutes(),
		s.auditRoutes(),
	)
}
```

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/console/ -v`
Expected: 全部 PASS。`Test每个权限码都有路由用到` 此时才真绿——前面十二个任务里它一直是红的或空转的。

- [ ] **Step 4: 提交**

```bash
git add internal/console
git commit -m "feat(console): 审计查询与 CSV 导出，权限码与审计的正反对账"
```

---

## Task 14：装配点 cmd/lober

**Files:**
- Create: `cmd/lober/main.go`
- Create: `cmd/lober/config.go`、`cmd/lober/config_test.go`
- Create: `cmd/lober/wire.go`、`cmd/lober/wire_gen.go`（生成）
- Create: `cmd/lober/builtin.go`
- Create: `cmd/lober/isosource.go`（第 4 层的占位实现）
- Create: `cmd/lober/taskcontrol.go`（第 7 层的占位实现）
- Create: `cmd/lober/logging.go`（存储层的调用日志装饰器，手写）
- Modify: `go.mod`、`go.sum`（加 gin、yaml.v3；`tool` 指令钉 wire）
- Modify: `Makefile`（加 `gen` 一步）

**Interfaces:**
- Consumes: 前面十三个任务的全部导出面
- Produces: 一个能起来的二进製；`make check` 的第六步 `gen`

**本任务是唯一知道每个接口由谁实现的包**（总 spec §6.1）。两个占位实现都明确标注归谁替换，且一律拒绝而不是写临时规则（第 5 层设计 §6.1、§6.2）。

- [ ] **Step 1: 加依赖与 tool 指令**

Run:
```bash
go get github.com/gin-gonic/gin@v1.12.0
go get gopkg.in/yaml.v3
go get -tool github.com/goforj/wire/cmd/wire@v1.2.0
go mod tidy
```
Expected: `go.mod` 的 require 里有 gin 与 yaml.v3，并有一行 `tool github.com/goforj/wire/cmd/wire`。

若 `go get -tool` 报 `no such tool`，说明 goforj/wire v1.2.0 的主包不在 `cmd/wire`。用 `go list -m -json github.com/goforj/wire@v1.2.0` 看它的 Go 文件布局，把 tool 路径改成真实的那一个；改不动就照总 spec §8 的退路把 wire 也改成手写构造（`newApp` 一个函数，顺序写在注释里），并在设计文档 §8 记一笔。

- [ ] **Step 2: 写失败的测试——配置**

Create `cmd/lober/config_test.go`：

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func Test读配置(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lober.yaml")
	if err := os.WriteFile(path, []byte(`
listen: ":8080"
pg:
  dsn: "postgres://lober@localhost/lober"
log:
  level: "info"
tls:
  cert: "/etc/lober/tls.crt"
  key: "/etc/lober/tls.key"
sessionTTLHours: 12
seedAdmin: "admin"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("读配置：%v", err)
	}
	if got.Listen != ":8080" || got.PG.DSN != "postgres://lober@localhost/lober" {
		t.Errorf("读出来是 %+v", got)
	}
	if got.TLS.Cert == "" || got.TLS.Key == "" {
		t.Error("TLS 的两条路径没读到")
	}
	if got.SeedAdmin != "admin" {
		t.Errorf("种子管理员是 %q，应为 admin", got.SeedAdmin)
	}
}

// 环境变量覆盖 DSN：测试与生产只差连哪个库，不该靠改文件（总 spec §6.4）。
func Test环境变量覆盖DSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lober.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8080\"\npg:\n  dsn: \"文件里的\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOBER_PG_DSN", "环境里的")
	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("读配置：%v", err)
	}
	if got.PG.DSN != "环境里的" {
		t.Errorf("DSN 是 %q，环境变量应覆盖文件", got.PG.DSN)
	}
}

// listen 与 DSN 是必须的：缺了一个起不来，不如在读配置的时候就说出缺什么。
func Test配置缺必须项(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lober.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8080\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Error("缺 DSN 却没报错")
	}
}

// 文件不存在、或者不是 YAML：报错，不拿默认值糊弄。
func Test配置读不出来(t *testing.T) {
	if _, err := loadConfig(filepath.Join(t.TempDir(), "没有.yaml")); err == nil {
		t.Error("文件不存在却没报错")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "坏.yaml")
	if err := os.WriteFile(path, []byte("listen: [没闭合"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Error("YAML 坏了却没报错")
	}
}
```

Run: `go test ./cmd/lober/ -run 'Test读配置|Test环境变量|Test配置' -v`
Expected: FAIL，`undefined: loadConfig`。

- [ ] **Step 3: 实现配置**

Create `cmd/lober/config.go`：

```go
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// config 是装配点的全部配置。第 4 层要的五个目录（暂存、制品、SMB、Windows 工具、允许登记的
// 本地路径）等第 4 层再加：现在加了也没有人读（第 5 层设计 §8）。
type config struct {
	Listen         string       `yaml:"listen"`
	PG             pgConfig     `yaml:"pg"`
	Log            logConfig    `yaml:"log"`
	TLS            tlsConfig    `yaml:"tls"`
	SessionTTLHours int         `yaml:"sessionTTLHours"`
	SeedAdmin      string       `yaml:"seedAdmin"`
}

type pgConfig struct {
	DSN string `yaml:"dsn"`
}

type logConfig struct {
	Level string `yaml:"level"`
}

type tlsConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// loadConfig 读一份 YAML 配置。DSN 可被 LOBER_PG_DSN 覆盖：测试与生产只差连哪个库，
// 不该靠改文件（总 spec §6.4）。
func loadConfig(path string) (config, error) {
	var c config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("读配置 %s：%w", path, err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("解析配置 %s：%w", path, err)
	}
	if dsn := os.Getenv("LOBER_PG_DSN"); dsn != "" {
		c.PG.DSN = dsn
	}
	if c.Listen == "" {
		return c, fmt.Errorf("配置里没有 listen：控制台要监听哪个地址")
	}
	if c.PG.DSN == "" {
		return c, fmt.Errorf("配置里没有 pg.dsn，环境变量 LOBER_PG_DSN 也没给：连哪个库")
	}
	if c.SessionTTLHours <= 0 {
		c.SessionTTLHours = 12
	}
	if c.SeedAdmin == "" {
		c.SeedAdmin = "admin"
	}
	return c, nil
}
```

- [ ] **Step 4: 实现内置内容装载、两个占位实现与装饰器**

Create `cmd/lober/builtin.go`：

```go
package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/lober/lober/internal/builtin"
	"github.com/lober/lober/internal/store"
)

// loadBuiltin 把程序自带的内置 hook 与模板按「ID + 版本」写进仓储（总 spec §6.2 第 11 条）。
// 撞 ErrConflict 是两次同时装载同一个版本，等先到的提交之后内容相同，重试即可；撞
// ErrBuiltinMismatch 说明有人改了内置内容却没有升版本号，拒绝启动并说明原因。
func loadBuiltin(ctx context.Context, st store.Store) error {
	for _, h := range builtin.Hooks() {
		if err := ensure(ctx, func() error { return st.EnsureBuiltinHook(ctx, h) }); err != nil {
			return err
		}
	}
	for _, t := range builtin.Templates() {
		if err := ensure(ctx, func() error { return st.EnsureBuiltinTemplate(ctx, t) }); err != nil {
			return err
		}
	}
	return nil
}

// ensure 重试到成功或失败。ErrConflict 只重试：那是并发下的占位之争，等一等就有结果。
func ensure(ctx context.Context, do func() error) error {
	for attempt := 0; ; attempt++ {
		err := do()
		if err == nil {
			return nil
		}
		if !errors.Is(err, store.ErrConflict) || attempt >= 3 {
			return err
		}
		slog.Warn("装载内置内容撞上并发写入，重试", "attempt", attempt+1, "err", err)
	}
}
```

Create `cmd/lober/isosource.go`：

```go
package main

import (
	"context"
	"fmt"
	"io"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// isoSourceStub 是 console.ISOSource 的占位实现。第 4 层的 internal/iso 到位后，wire 的
// 提供者换成 (*iso.Sources)(nil) 那一个，本文件整个删掉。
//
// 一律返回「第 4 层未实现」，不写临时规则：此刻写一个「能登记但不能取」的版本，操作员会以为
// 来源登记成功，等到装机现场才发现字节从来没到过（第 5 层设计 §6.1）。
type isoSourceStub struct{}

func (isoSourceStub) Register(ctx context.Context, iso model.ISO, kind, addr, role string,
	size int64, sha256 string, a store.Audit) error {
	return fmt.Errorf("第 4 层（ISO 来源获取）尚未实现，登记不了来源")
}

func (isoSourceStub) Upload(ctx context.Context, id, role string, offset int64, r io.Reader,
	a store.Audit) error {
	return fmt.Errorf("第 4 层（ISO 来源获取）尚未实现，收不下分片")
}

func (isoSourceStub) Cancel(ctx context.Context, id string, a store.Audit) error {
	return fmt.Errorf("第 4 层（ISO 来源获取）尚未实现，取消不了")
}

func (isoSourceStub) Retry(ctx context.Context, id string, a store.Audit) error {
	return fmt.Errorf("第 4 层（ISO 来源获取）尚未实现，重试不了")
}

func (isoSourceStub) Reimport(ctx context.Context, id string, a store.Audit) error {
	return fmt.Errorf("第 4 层（ISO 导入）尚未实现，重新导入不了")
}

// FreeSpace 返回一个大得不会拦住人的数：占位实现不该凭一个假余量拒绝登记。
func (isoSourceStub) FreeSpace(ctx context.Context) (int64, int64, error) {
	return 1 << 62, 1 << 62, nil
}

// 编译期兜住：第 4 层的实现换上来的那一天，签名漂了这里先红。
var _ console.ISOSource = isoSourceStub{}
```

Create `cmd/lober/taskcontrol.go`：

```go
package main

import (
	"context"
	"errors"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// taskControlStub 是 console.TaskControl 的占位实现。第 7 层的状态机到位后整个删掉。
//
// 一律拒绝，不写临时规则：model.Task 现在没有「失败前有没有进过重启中」这个字段，第 7 层才会
// 加时机进度，此刻写一个「failed 就能重试」的版本会直接违反 C34——重启之后的失败一键重装，
// 数据可能已经在盘上（第 5 层设计 §6.2）。
type taskControlStub struct{}

func (taskControlStub) Cancel(ctx context.Context, t model.Task, a store.Audit) error {
	return errors.New("第 7 层（状态机）尚未实现，取消不了任务")
}

func (taskControlStub) Retry(ctx context.Context, t model.Task, a store.Audit) error {
	return errors.New("第 7 层（状态机）尚未实现，重试不了任务")
}

// CanCancel 与 CanRetry 返回 nil，也就是「不拦」。代码表里没有「这个状态不许取消/重试」这一条
// ——第 7 层实现 C34 时要先往 codes.go 追加，同步改附录总表与
// TestCode表与附录总表逐项一致。本层不能替它加，于是界面上按钮是亮的，点下去得到 422 和一句
// 「第 7 层未实现」。这比灰着一个其实能用的按钮诚实。
func (taskControlStub) CanCancel(t model.Task) []model.Problem { return nil }
func (taskControlStub) CanRetry(t model.Task) []model.Problem  { return nil }

var _ console.TaskControl = taskControlStub{}
```

上面最后一行是笔误，改成：

```go
var _ console.TaskControl = taskControlStub{}
```

并删掉 `fmt` 的 import。

Create `cmd/lober/logging.go`：

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// loggingStore 是存储层的调用日志装饰器（总 spec §6.3「模块接口」一行）。手写而不用
// gowrap：它的最新发布是 2024-12-11，早于 Go 1.25 的 go tool 用法，而这一层只有四十行，
// 没有代码生成步骤要守（总 spec §8 的退路）。
//
// 切面的契约：对被包的接口透明，不改入参、返回值和错误。错误尤其要原样透传，调用方靠
// errors.Is 识别「重复投递」这类情况。
type loggingStore struct {
	store.Store
}

func (l loggingStore) log(op string, start time.Time, err error, keys ...any) {
	attrs := append([]any{"op", op, "took", time.Since(start)}, keys...)
	if err != nil {
		attrs = append(attrs, "err", err)
		slog.Warn("仓储调用失败", attrs...)
		return
	}
	slog.Debug("仓储调用", attrs...)
}

func (l loggingStore) CreateHost(ctx context.Context, h model.Host, a store.Audit) (model.Host, error) {
	start := time.Now()
	got, err := l.Store.CreateHost(ctx, h, a)
	l.log("host.create", start, err, "sn", h.SN)
	return got, err
}

func (l loggingStore) CreateTasks(ctx context.Context, ts []model.Task, a store.Audit) ([]model.Task, error) {
	start := time.Now()
	got, err := l.Store.CreateTasks(ctx, ts, a)
	l.log("task.create", start, err, "count", len(ts))
	return got, err
}

func (l loggingStore) TransitionTask(ctx context.Context, id string, expect store.TaskExpect,
	to model.TaskState, events []store.NewEvent) (model.Task, []store.Appended, error) {
	start := time.Now()
	got, appended, err := l.Store.TransitionTask(ctx, id, expect, to, events)
	// 「重复投递、未推进」正是要靠这一行留记录的问题（总 spec §6.3）
	l.log("task.transition", start, err, "task", id, "from", string(expect.State), "to", string(to))
	return got, appended, err
}

var _ = errors.Is // 透传约定写在类型注释里，这里只为 import 不空
```

删掉最后那行 `var _ = errors.Is` 与 `errors` 的 import——装饰器不自己判断错误，原样返回即可。

**装饰器只包三个方法是个刻意的起点**：它证明切面的形状（透明、错误原样透传），其余方法经嵌入的 `store.Store` 直接走。第 7 层给状态机套日志时照这个样子加；要包全部方法，就把 `loggingStore` 改成手写全部十七个接口方法，或者换回 gowrap。

- [ ] **Step 5: 写 wire 与 main**

Create `cmd/lober/wire.go`：

```go
//go:build wireinject

package main

import (
	"github.com/gin-gonic/gin"
	"github.com/goforj/wire"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// app 是装配点凑出来的一个能跑的控制台。
type app struct {
	engine *gin.Engine
	store  store.Store
}

// newApp 是唯一的注入函数：构造顺序由 wire 生成，这里只声明要什么。
func newApp(c config) (*app, error) {
	wire.Build(
		pgstore.Open,
		loggingStoreOf,
		consoleDepsOf,
		console.New,
		isoSourceStub{},
		taskControlStub{},
		console.NewHub,
		wire.FieldsOf(new(config), "SessionTTLHours"),
		wire.Struct(new(app), "*"),
	)
	return nil, nil
}
```

Create `cmd/lober/main.go`：

```go
// Command lober 是装配点（总 spec §4.8）：唯一知道每个接口由谁实现的地方。它读配置、建实例、
// 注入依赖、管理启动和停止；别的模块只拿到自己用得到的值（§6.1）。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("lober 起不来", "err", err)
		os.Exit(1)
	}
}

func run() error {
	path := "/etc/lober/lober.yaml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	setupLog(cfg.Log.Level)
	a, err := newApp(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// 内置内容要在起服务之前装完：库里同一版本的内容和程序自带的不一致，就该拒绝启动
	if err := loadBuiltin(ctx, a.store); err != nil {
		return err
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: a.engine}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("控制台在听", "addr", cfg.Listen)
		if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
			return
		}
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("收到退出信号，正在收尾")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("收尾没干净", "err", err)
		}
		return a.store.Close()
	}
}

func setupLog(level string) {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}
```

`main.go` 里引用的三个注入辅助要在 `wire.go` 之外定义（wire 生成的文件只放生成结果）：

Create `cmd/lober/providers.go`：

```go
package main

import (
	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// pgstore.Open 的签名是 (ctx, dsn, opts...) 而 wire 要的是 (dsn) → store.Store，包一层。
func openStore(dsn string) (store.Store, error) {
	return pgstore.Open(context.Background(), dsn)
}

// loggingStoreOf 给仓储套上调用日志。
func loggingStoreOf(st store.Store) store.Store { return loggingStore{Store: st} }

// consoleDepsOf 把各实现凑成 console.Deps。仓储按聚合分开给，不整包收 store.Store：
// 调用方只声明自己用得到的那几个（总 spec §6.1）。
func consoleDepsOf(st store.Store, src console.ISOSource, ctl console.TaskControl,
	hub console.Notifier, cfg config) console.Deps {
	return console.Deps{
		Hosts: st, ISOs: st, Hooks: st, Templates: st, Tasks: st, Events: st, Audit: st, Users: st,
		ISOSource: src, TaskCtl: ctl, Notifier: hub,
		Clock:      func() int64 { return time.Now().Unix() },
		SessionTTL: int64(cfg.SessionTTLHours) * 3600,
	}
}
```

`providers.go` 的 import 加 `"context"` 与 `"time"`。

**`pgstore.Open` 的真实签名以代码为准**（`internal/store/pgstore/open.go`）：它收 ctx、DSN 与选项。`openStore` 照那个签名包；`pgtest.Open` 用的是 `pgstore.Open(t.Context(), dsn, pgstore.WithMaxConns(maxConns))`，所以还要不要限连接池由配置定，本任务先不限，第 7 层要限再加一个选项。

`wire.go` 的 `wire.Build` 里把 `pgstore.Open` 换成 `openStore`。

- [ ] **Step 6: 生成 wire 并加 Makefile 的一步**

Run:
```bash
go generate ./cmd/lober/    # 或者 go tool github.com/goforj/wire/cmd/wire ./cmd/lober
go build ./cmd/lober
```
Expected: 生成 `cmd/lober/wire_gen.go`，`go build ./...` 通过。

Modify `Makefile`：

```make
# make check 依次跑格式、vet、测试、竞态检测、交叉编译和生成代码比对，六步全过才算绿。
# 各目标只用 go、gofmt 和 shell 内建命令，Git Bash 与 Linux 上都能跑。

.PHONY: check fmt vet test race cross gen

check: fmt vet test race cross gen

# 只查 go list 列出的包目录：主目录的 web/node_modules 里混有第三方 .go 文件，
# gofmt -l . 会把它们也算进来。go list 一个包都没列出时，不带参数的 gofmt
# 会改读标准输入而卡住，所以先把这种情况挡掉。
fmt:
	@echo 'gofmt -l $$(go list -f {{.Dir}} ./...)'
	@dirs="$$(go list -f '{{.Dir}}' ./...)" || exit 1; \
	if [ -z "$$dirs" ]; then echo "fmt：go list 没有列出任何包"; exit 1; fi; \
	unformatted="$$(gofmt -l $$dirs)" || exit 1; \
	if [ -n "$$unformatted" ]; then echo "以下文件没有 gofmt："; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

# 旧系统出过在 Windows 上能编译、却编不出 Linux 版的问题（spec §7.1）。
cross:
	GOOS=linux GOARCH=amd64 go build ./...

# 生成代码与库里的一致：wire 重新生成一遍，不一致就说明有人改了 wire.go 却没重新生成，
# 或者手工改了生成结果。只对 wire_gen.go 做 diff：别的地方不许被这一步碰。
gen:
	@echo 'go tool wire ./cmd/lober'
	@go tool wire ./cmd/lober || exit 1
	@if [ -n "$$(git diff --name-only -- cmd/lober/wire_gen.go)" ]; then \
		echo "wire_gen.go 与 wire.go 不一致，重新生成之后重新提交："; \
		git diff -- cmd/lober/wire_gen.go; exit 1; \
	fi
	@if [ -n "$$(git status --porcelain -- cmd/lober)" ]; then \
		echo "生成之后 cmd/lober 下还有未跟踪或未提交的文件："; \
		git status --porcelain -- cmd/lober; exit 1; \
	fi
```

`go tool wire` 的路径以 Step 1 定下的 tool 指令为准；若 tool 名不是 `wire`，这里跟着改。

- [ ] **Step 7: 起一次，确认真的能跑**

Run: `go build -o /tmp/lober ./cmd/lober && /tmp/lober --help`
Expected: 打印用法或直接起不来（缺配置），但不该是 panic。

再写一份临时配置连测试库起一次（DSN 从 `.pgtest.dsn` 那个机制之外另给，别把测试库当生产库）：

```bash
/tmp/lober /tmp/lober-check.yaml &
sleep 2
curl -s -X POST localhost:18080/console/v1/auth/login -d '{"username":"admin","password":"改我"}'
kill %1
```
Expected: 登录返回 401（初始管理员的口令还没设）或 200；接口通了就说明装配点成了。这一**不在 make check 里**：它要一个真的 PostgreSQL，测试库的连接串由 `pgtest` 管，装配点不读它（总 spec §6.4）。

- [ ] **Step 8: 跑 make check**

Run: `make check`
Expected: 六步全过。`gen` 一步没有 diff。

- [ ] **Step 9: 提交**

```bash
git add cmd/lober go.mod go.sum Makefile
git commit -m "feat(lober): 装配点首次出现，能起控制台；ISO 来源与任务控制的占位实现"
```

---

## Task 15：文档同步与整体验收

**Files:**
- Modify: `docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`
- Modify: `docs/superpowers/plans/2026-10-11-layer5-console.md`（若本计划与实现有出入，改计划）
- Modify: `docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`（L-8、S3-10 标结果）
- Modify: `docs/superpowers/reviews/2026-10-07-layer2-open-issues.md`（归第 5 层的条目标结果）

- [ ] **Step 1: 总 spec 要改的三处**

1. §4.2「范围」：用户、角色与会话不再「推迟到第 5 层」，改成「第 5 层已加：`Users` 聚合与 `users` 表，会话在控制台的内存里」。
2. §6.4「生产环境」：配置从哪来已经定了——YAML 一份，路径由命令行参数给，DSN 可被 `LOBER_PG_DSN` 覆盖。
3. §7.1：make check 从五步变六步，补 `gen`（比对 wire 生成结果）。

- [ ] **Step 2: 两份清单归第 5 层的条目标结果**

- L-8：catalog 的全局词表（`AllFormats` 等八张表）、`Subject.Kind` 常量、`HookKey`/`TemplateKey` 的 `String()` 都已加；「ID@版本」收成一份，activation、admission、params、snapshot 四处改调 `String()`。
- S3-10：`host_platform_invalid` 的架构清单改成与 `catalog.AllArchs()` 比对全集，漏列也报得出。
- 第 2 层清单里归第 5 层的条目（RejectHook/RejectTemplate 的理由必填、`hostReservedAttrs` 的导出等）逐条标结果；没做的写明归谁。

- [ ] **Step 3: 整体验收**

逐条跑，每条都要有输出佐证，不许「应该过了」：

- [ ] `make check` 六步全过，退出码 0。PostgreSQL 的测试真的跑了，没有因为找不到 DSN 而跳过。
- [ ] `go list ./...` 列出 `internal/console` 与 `cmd/lober`，守卫覆盖到它们：构造一个 console import `cmd/lober` 的反例，`go test ./internal/archtest/` 报 `ruleConsole`；构造一个 console import `github.com/lib/pq` 的反例，同样报。
- [ ] 每条路由都有 HTTP 测试（`Test每条路由都有OpenAPI描述` 与 `go test ./internal/console/ -v` 的用例数对得上），权限矩阵覆盖五个角色。
- [ ] 每条声明了审计动作的路由都留下审计记录（`Test每条审计路由都留下记录` 全绿）。
- [ ] 批量建任务一台不过就一台都不建（`Test向导提交一台不过全不建` 全绿，`ListTasks` 与审计都为空）。
- [ ] 同一个预览请求两次逐字节相同（`Test预览两次逐字节相同`，C35）。
- [ ] `go test -race ./...` 全过：会话表与通知中心都有锁，竞态检测说得清。
- [ ] `GOOS=linux GOARCH=amd64 go build ./...` 过。
- [ ] 二进制能起来、能登录、能打一个要权限的接口（Step 7 手工验过一次，这里记录输出）。
- [ ] 第 1 层清单的 L-8、S3-10 标了结果；第 2 层清单归第 5 层的条目标了结果。
- [ ] 既有行为没变：`go test ./internal/core/... ./internal/store/... ./internal/render/... ./internal/builtin/...` 全绿。

- [ ] **Step 4: 提交**

```bash
git add docs
git commit -m "docs: 第 5 层落地后同步 spec、计划与两份清单"
```

---

## 交给实现者的三件事

1. **分支基点**：`feat/console` 从 `feat/render`（555afc1）开出，带着第 1/2/3 层。第 4 层在自己的分支上，两者都并入 dev 时只有两处会撞：`internal/store/store.go` 的 `Store` 接口嵌入列表、`internal/store/pgstore/schema.go` 的 `tables` 切片。各加各的，压提交时看一眼别互相盖。
2. **行尾**：本文件是 LF。仓库没有 `.gitattributes`、`core.autocrlf` 也没设，既有提交都是 LF。用编辑器改完之后提交前确认一遍，否则 diff 会是整文件重写。
3. **两个接缝的占位实现都在 `cmd/lober`**：`isoSourceStub` 与 `taskControlStub`。第 4 层换前者、第 7 层换后者，都只是删一个文件加一行 wire 提供者。它们现在一律拒绝而不写临时规则，这是刻意的（第 5 层设计 §6.1、§6.2）。
