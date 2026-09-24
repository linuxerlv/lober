# store 层迁移到 GORM + PostgreSQL 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 把 `internal/store/sqlite`（手写 SQL + modernc.org/sqlite）替换为 `internal/store/postgres`（GORM + PostgreSQL），按聚合拆成八个子包 1:1 对应 `contract.Store` 组合的八个窄接口。

**架构：** 八个聚合包各自持有 GORM 行结构体与 `internal/core/model` 的双向映射，组合器用结构体嵌入把它们拼成 `contract.Store`；`pgtype` 与 `pgerr` 两个共享小包分别提供 JSONB/可空时间的 Scanner-Valuer 与 pgconn 错误到 sentinel 的翻译。schema 由 GORM model tag + AutoMigrate 生成，启动时用 `pg_indexes` 校验四个唯一索引没有静默退化。

**技术栈：** Go 1.26 / `gorm.io/gorm v1.31.2` / `gorm.io/driver/postgres v1.6.3` / `github.com/jackc/pgx/v5 v5.11.0` / PostgreSQL（本机 5432）

**设计依据：** [2026-09-24-gorm-postgres-migration-design.md](../specs/2026-09-24-gorm-postgres-migration-design.md)。本计划的任何决策与该文档冲突时，以该文档为准。

---

## 环境前提

**Go 命令必须用 `/d/golang/bin/go`。** PATH 上的 `go` 是 msys 的 1.26.4，但 `GOROOT` 指向 `D:\golang`（1.27.1 工具链），版本不匹配会让每个包都报
`compile: version "go1.27.1" does not match go tool version "go1.26.4"`。本计划所有验证命令都写成绝对路径。`make check` 在 PATH 修好之前跑不通，用等价的 go 命令替代。

**测试需要可达的 PostgreSQL。** 环境变量 `LOBER_TEST_PG_DSN` 指向本机 PG，账号需有 `CREATE SCHEMA` / `DROP SCHEMA` 权限。连不上时测试**失败**而不是 skip（设计文档 §9）。

**基线：** 迁移开始前 `/d/golang/bin/go test ./...` 全绿（17 个包，2026-09-24 已确认）。每个任务结束都要回到全绿。

## 文件结构

```
internal/store/postgres/
  store.go          组合器 Store 结构体（八个嵌入）、New、编译期接口断言
  open.go           Open：连库 + 连接池 + AutoMigrate + 索引校验
  schemacheck.go    pg_indexes 校验四个唯一索引
  tx.go             RunInTx，拒绝嵌套
  pgtype/           jsonb.go（map/slice/结构体的 Scanner-Valuer）、nulltime.go
  pgerr/            translate.go（pgconn 错误 → contract sentinel）
  task/             tasks 表 → contract.TaskStore（8 个方法）
  event/            task_events 表 → contract.EventStore（2 个方法）
  host/             hosts 表 → contract.HostStore（4 个方法）
  hook/             hooks 表 → contract.HookStore（5 个方法）
  template/         templates + template_hook_refs → contract.TemplateStore（5 个方法）
  artifact/         artifact_sets 表 → contract.ArtifactSetStore
  fact/             task_facts 表 → contract.FactStore（2 个方法）
  audit/            audit_log 表 → contract.AuditStore（2 个方法）
```

**为什么这样切。** 切分依据是 `contract/store.go:342-351` 的八个窄接口，不是表数量 —— 每个包恰好实现一个接口，职责能用一句话说完，也不需要判断某个方法该放哪。`template` 包管两张表，因为 `TemplateStore` 这一个接口同时覆盖模板与它的 hook 引用列表。

**不设共享 row 包。** 行结构体归各自的聚合包所有。一个知道所有表的共享包会变成上帝包，与拆聚合的目的相反。`pgtype` 与 `pgerr` 是真正共享的（每个聚合都要用），复制八遍才是问题。

**archtest 不用改规则。** `deps_test.go:238` 把 `"store"` 映射到前缀 `internal/store`，`moduleOf` 按前缀匹配且同模块子包互引明确允许，新包全部落在这个前缀下。

---

### 任务 1：依赖与测试库夹具

**目标：** 拿到能连上 PG、每个测试独立 schema 的测试夹具，后续所有任务都靠它验证。

**文件：**
- 修改：`go.mod` —— 加三个直接依赖，暂不删 modernc.org/sqlite（旧包还在跑，任务 12 才删）
- 创建：`internal/store/postgres/testdb_test.go` —— 测试夹具，仅测试可见

**前提：** `gorm.io/gorm v1.31.2` 已经是直接依赖，靠 `internal/deps/deps.go` 的空白 import 锚定（打了 `//go:build tools` tag，不参与常规编译）。本任务只需补 `gorm.io/driver/postgres@v1.6.3` 与 `github.com/jackc/pgx/v5@v5.11.0`。

**接口契约：**

```go
// newTestDB 建一个独立 schema 并返回连到该 schema 的句柄。
// 已跑完 AutoMigrate 与索引校验。t.Cleanup 里 DROP SCHEMA ... CASCADE。
func newTestDB(t *testing.T) *gorm.DB
```
- schema 名取 `test_` 前缀 + 随机后缀，保证并行测试互不干扰
- `LOBER_TEST_PG_DSN` 未设或连不上时 `t.Fatal`，**不 skip**；失败信息要写明需要什么（可达的 PG、建 schema 权限、环境变量名）
- 本任务里 AutoMigrate 还没有 model 可迁移，先留空调用，任务 3 起逐步接上

- [ ] **红：** 写一个只调用 `newTestDB` 并查 `current_schema()` 的测试，断言返回的 schema 名是刚建的那个（证明 `search_path` 真的生效，而不是静默落在 public 上 —— 落在 public 会让所有测试共享一张表，隔离失效且症状是随机失败）。运行 `/d/golang/bin/go test ./internal/store/postgres/ -run TestTestDB -v`，预期 FAIL（包不存在）。

- [ ] **绿：** `/d/golang/bin/go get gorm.io/driver/postgres@v1.6.3 github.com/jackc/pgx/v5@v5.11.0` 后实现夹具。预期 1 passed。再故意 unset `LOBER_TEST_PG_DSN` 跑一次，确认是 FAIL 而不是 SKIP。

- [ ] **重构 + commit：** `/d/golang/bin/go build ./... && /d/golang/bin/go vet ./...` 通过后 commit（`test(store): PG 测试夹具，每测试独立 schema`）。

**风险：** `go get` 会顺带升级间接依赖。commit 前用 `git diff go.mod` 确认三个直接依赖的版本与本计划一致，没有被解析成别的版本。

---

### 任务 2：pgtype 与 pgerr 两个共享包

**目标：** 把 JSONB 序列化与 pgconn 错误翻译各收敛到一处，八个聚合包直接用。

**文件：**
- 创建：`internal/store/postgres/pgtype/jsonb.go` —— JSONB 列的 Scanner/Valuer
- 创建：`internal/store/postgres/pgtype/nulltime.go` —— Unix 秒 0 ↔ SQL NULL 的互转
- 创建：`internal/store/postgres/pgerr/translate.go` —— pgconn 错误 → contract sentinel
- 测试：三者各自的 `_test.go`（纯单元测试，不需要数据库）

**接口契约：**

```go
// pgtype
type StringMap map[string]string   // 实现 sql.Scanner 与 driver.Valuer
type StringSlice []string          // 同上

func NullTimeFromUnix(sec int64) *time.Time  // sec == 0 → nil
func UnixFromNullTime(t *time.Time) int64    // nil → 0

// pgerr
func Translate(err error, ctxLabel string) error
```
- `StringMap`/`StringSlice` 的 nil 值序列化成 JSON `null`；从 SQL NULL 读回得到 nil，不是空 map/slice
- `NullTimeFromUnix` 产出的时刻必须是 UTC
- `Translate`：`gorm.ErrRecordNotFound` → `contract.ErrNotFound`；`*pgconn.PgError` 且 `Code == "23505"` 时按 `ConstraintName` 映射（`idx_tasks_sn_active` → `ErrActiveTaskExists`，`idx_task_events_task_seq` → `ErrDuplicateSeq`）；`err == nil` 返回 nil；其余用 `%w` 包装 `ctxLabel` 后返回，**不吞**

- [ ] **红：** 为 `pgtype` 写往返测试，覆盖四类输入：非空 map/slice、空但非 nil（`map[string]string{}`）、nil、从 SQL NULL 读回。空与 nil 必须能区分 —— 混淆它们会让 `form_data_json` 这类列的「没填」和「填了空表单」变成同一个值。为 `NullTimeFromUnix`/`UnixFromNullTime` 写往返测试，覆盖 0、正数、以及 `nil` 输入。为 `pgerr.Translate` 写表驱动测试，覆盖上述五类错误（含 nil）。运行 `/d/golang/bin/go test ./internal/store/postgres/pgtype/ ./internal/store/postgres/pgerr/ -v`，预期 FAIL。

- [ ] **绿：** 实现三个文件直到测试通过。`Translate` 的未知错误分支要保留原错误可被 `errors.Is`/`errors.As` 取出。

- [ ] **重构 + commit：** commit（`feat(store): JSONB 与错误翻译的共享包`）。

**风险：** `23505` 是 unique_violation 的字面码值，抄成常量并在注释里写明来源。`pgerr` 依赖的两个索引名与任务 3、4 的 tag 里写的必须字面一致 —— 任务 11 有一条测试专门锁这个。

---

### 任务 3：task 聚合包（行结构体与三个唯一索引）

**目标：** tasks 表的行结构体、索引 tag、与 `model.Task` 的双向映射落地，`TaskStore` 八个方法先立签名。

**文件：**
- 创建：`internal/store/postgres/task/row.go` —— 行结构体、tag、双向映射、`Models()`
- 创建：`internal/store/postgres/task/store.go` —— `Store` 类型与八个方法（本任务内可 panic 桩）
- 测试：`internal/store/postgres/task/row_test.go`

**接口契约：**

```go
func New(db *gorm.DB) *Store
func Models() []any   // 供组合器收集给 AutoMigrate
```

行结构体的字段与列一一对应 `schema/001_init.sql` 的 tasks 表，类型按设计文档 §5：

- `created_at`、`state_updated_at`、`phase_entered_at` → `time.Time`（恒有值）
- `last_signal_at`、`token_expires_at`、`finalize_expires_at` → `*time.Time`（model 的 0 ↔ 库里 NULL）
- `hook_digests`、`form_data`、`resolved_params` → `pgtype` 的 JSONB 类型
- `failure_detail` → `*FailureDetail` 的 JSONB，nil ↔ NULL
- 表名固定为 `tasks`（用 `TableName()`，不依赖 GORM 的复数推导）

三个唯一索引用 tag 表达。**只有 `idx_tasks_sn_active` 的 WHERE 含逗号，必须反斜杠转义**（GORM 按逗号切 tag 选项，`schema/utils.go:23-26` 支持尾部反斜杠跨过分隔符）：

```go
HostSN string `gorm:"column:host_sn;index:idx_tasks_sn_active,unique,where:state NOT IN ('done'\,'failed') AND host_sn != ''"`
```

另两个：`idx_tasks_token` where `token != ''`、`idx_tasks_finalize_token` where `finalize_token != ''`。

- [ ] **红：** 写映射往返测试：`model.Task` → 行 → `model.Task` 必须完全相等（用 `go-cmp`，项目已依赖）。覆盖四类输入：全字段填满、四个可空时间列为 0、`FailureDetail` 为 nil、JSON 字段为空 map。运行 `/d/golang/bin/go test ./internal/store/postgres/task/ -v`，预期 FAIL。

- [ ] **绿：** 实现 `row.go`。`store.go` 的八个方法此刻可以是 panic 桩 —— 本任务只交付映射正确性，方法实现是任务 4、5。

- [ ] **重构 + commit：** commit（`feat(store/task): tasks 行结构体与 model 双向映射`）。

**风险：** 往返测试能抓住列的漏写与类型错配，但**抓不住 index tag 写错** —— tag 只在 AutoMigrate 时生效，纯映射测试不碰数据库。索引正确性由任务 6 的校验器负责，两者不能互相替代。

---

### 任务 4：TaskStore 的查询与建任务

**目标：** `CreateTask`、`GetTask`、`FindPendingTaskByIdentity`、`ListTasks`、`GetTaskByToken` 五个读路径与建任务可用。

**文件：**
- 修改：`internal/store/postgres/task/store.go` —— 替换五个 panic 桩
- 测试：`internal/store/postgres/task/store_test.go`

**接口契约：** 签名与语义完全照抄 `contract.TaskStore`（`contract/store.go:87-141`），不重新设计。三处容易做错的语义：

- `FindPendingTaskByIdentity(ctx, sn, mac, uuid)`：按 SN → MAC → UUID 优先级**依次尝试非空的键**，空键跳过，三者全空直接 `ErrNotFound`；只匹配 `state = pending`
- `GetTaskByToken(ctx, token, purpose)`：token 不存在、已过期、purpose 不符，**统一**返回 `ErrNotFound`，不区分（§4.4 不泄露 token 曾存在）
- `CreateTask`：撞 `idx_tasks_sn_active` 时经 `pgerr.Translate` 返回 `ErrActiveTaskExists`

- [ ] **红：** 用任务 1 的夹具写测试。`FindPendingTaskByIdentity` 覆盖：只有 SN 命中、SN 空而 MAC 命中、三键全空、命中的任务不是 pending、传入空 SN 时不能匹配到 host_sn 也为空的任务。`GetTaskByToken` 覆盖：有效、已过期、purpose 不符、不存在 —— 四者的返回必须无法区分。`CreateTask` 覆盖：正常、同 SN 第二个非终态任务（期望 `ErrActiveTaskExists`）、同 SN 但前一个已 done（期望成功）。运行 `/d/golang/bin/go test ./internal/store/postgres/task/ -v`，预期 FAIL。

- [ ] **绿：** 实现五个方法。`ListTasks` 按 `TaskFilter` 的两个字段过滤，零值不过滤。

- [ ] **重构 + commit：** commit（`feat(store/task): 建任务与五个查询路径`）。

**风险：** 「同 SN 已 done 可再建」这一条直接验证 partial index 的 WHERE 生效 —— 如果 WHERE 丢了，这条测试会失败。这是 tag 转义写错的**第一个**捕获点，比任务 6 的校验器更早暴露。

---

### 任务 5：TaskStore 的 CAS 与写路径

**目标：** `CASTaskState`、`MutateTaskFields`、`ListTimedOutTasks` 三个写/扫描路径可用，TaskStore 八个方法齐活。

**文件：**
- 修改：`internal/store/postgres/task/store.go` —— 替换余下三个 panic 桩
- 测试：`internal/store/postgres/task/store_test.go`

**接口契约：** 照抄 `contract.TaskStore`。三处关键语义：

- `CASTaskState`：条件是 `id AND state AND version` **三者同时成立**，不是只比版本号；成功时 `version + 1` 并返回更新后的任务；`RowsAffected == 0` → `contract.ErrConflict`。不用 `optimisticlock` 插件（设计文档 §2 说明理由）
- `MutateTaskFields`：`model.TaskMutation` 的每个字段是指针，nil 表示不改；类型本身不含 `State` 字段，实现不得新增任何能写 `state` 列的路径
- `ListTimedOutTasks(ctx, asOf)`：查询范围必须以 `model.BudgetedPhases()` 为准，**不能**遍历全部 Phase 再跳过 pending（后者在 Phase 新增取值时容易漏改）

- [ ] **红：** `CASTaskState` 覆盖：正常转换且 version 自增、version 过期、from-state 不符、两者都不符。`MutateTaskFields` 覆盖：改单个字段时其余字段不变、四个可空时间列从 0 改成非 0（验证 NULL → 值）、以及从非 0 改回 0（验证值 → NULL）。`ListTimedOutTasks` 覆盖：超预算的非终态任务被返回、pending 任务永不被返回、未超预算的不返回。运行 `/d/golang/bin/go test ./internal/store/postgres/task/ -v`，预期新增用例 FAIL。

- [ ] **绿：** 实现三个方法。

- [ ] **重构 + commit：** commit（`feat(store/task): CAS 状态转换与运行时字段更新`）。

**风险：** 从非 0 改回 0 要真的写成 SQL NULL 而不是被 GORM 当零值跳过 —— GORM 的 `Updates` 默认忽略零值字段，`*time.Time` 的 nil 与「不更新」在 GORM 里都表现为「不写」。实现时需要用 `Select` 显式指定要更新的列，或用 map 形式的 `Updates`。这是本任务最容易静默出错的一点。

---

### 任务 6：开库、AutoMigrate 与索引校验

**目标：** `Open` 一次性完成连库、迁移、索引校验；索引静默退化在启动时就被拦住。

**文件：**
- 创建：`internal/store/postgres/open.go` —— `Open`
- 创建：`internal/store/postgres/schemacheck.go` —— 索引校验
- 修改：`internal/store/postgres/testdb_test.go` —— 夹具改为走 `Open` 的迁移与校验路径
- 测试：`internal/store/postgres/schemacheck_test.go`

**接口契约：**

```go
func Open(cfg config.Postgres) (*gorm.DB, error)
func verifyIndexes(db *gorm.DB) error   // 包内可见
```

`Open` 的顺序：连库 → 设连接池 → AutoMigrate（收集八个聚合包的 `Models()`）→ `verifyIndexes`。任何一步失败都返回能定位到具体步骤的错误。

`verifyIndexes` 查 `pg_indexes.indexdef`，对四个索引断言存在、是 unique、且 WHERE 与下表字面一致：

| 索引 | 表与列 | WHERE |
|---|---|---|
| `idx_tasks_sn_active` | tasks (host_sn) | `state NOT IN ('done','failed') AND host_sn != ''` |
| `idx_tasks_token` | tasks (token) | `token != ''` |
| `idx_tasks_finalize_token` | tasks (finalize_token) | `finalize_token != ''` |
| `idx_task_events_task_seq` | task_events (task_id, seq) | 无（普通唯一索引） |

- [ ] **红：** 写测试：正常 AutoMigrate 后 `verifyIndexes` 通过；手工 `DROP INDEX idx_tasks_sn_active` 再建一个无 WHERE 的同名唯一索引后，`verifyIndexes` 必须报错并指出是哪个索引、期望与实际的 WHERE 各是什么。运行 `/d/golang/bin/go test ./internal/store/postgres/ -v`，预期 FAIL。

- [ ] **绿：** 实现两个文件。PG 会把 WHERE 条件规范化（加括号、改写字面量），比较时要容忍这种规范化差异 —— 先查一次真实的 `indexdef` 输出，按它的实际形态定比较方式，不要凭猜。

- [ ] **重构 + commit：** commit（`feat(store): 开库、AutoMigrate 与索引校验`）。

**风险：** 这是全计划风险最高的任务。索引退化不会让任何业务测试失败，症状是**装完的机器再也不能重装**（终态任务开始占用 host_sn 唯一约束）或**第二个未签发 token 的任务建不出来**（空串撞唯一约束）。校验器本身写松了等于没有，所以红灯步骤必须真的构造一个退化的索引来验证校验器会报错。

---

### 任务 7：event 与 fact 两个聚合包

**目标：** `EventStore` 与 `FactStore` 可用；信号去重与 facts 合并语义正确。

**文件：**
- 创建：`internal/store/postgres/event/{row.go,store.go}`、`internal/store/postgres/fact/{row.go,store.go}`
- 测试：两个包各自的 `store_test.go`

**接口契约：** 照抄 `contract.EventStore`（`contract/store.go:248-262`）与 `contract.FactStore`（`contract/store.go:326-338`）。两个包各自导出 `New(db *gorm.DB) *Store` 与 `Models() []any`，与任务 3 同一形态。四处关键语义：

- `AppendEvent`：`event.Seq > 0` 是信号来源事件，撞 `(task_id, seq)` 唯一索引返回 `ErrDuplicateSeq`；`Seq <= 0` 表示非信号事件，由实现分配一个**不参与去重**的序号并写进返回值，调用方传入的 Seq 被忽略
- `ListEvents`：按 Seq **升序**；`filter.AfterSeq` 非零时只返回 Seq 大于该值的
- `MergeFacts`：**合并而非替换** —— 已存在的键覆盖、新键新增、本次未出现的既有键保持不变。用 `clause.OnConflict` 做 UPSERT，不要先删后插
- `task_events.id` 用 `bigserial`（`conformance/store.go:1173` 有断言依赖 id 单调递增）

- [ ] **红：** `AppendEvent` 覆盖：正常追加、同 (task_id, seq) 第二次（期望 `ErrDuplicateSeq`）、`Seq = 0` 连续两次都成功且分配到不同序号。`ListEvents` 覆盖：升序、`AfterSeq` 过滤、空结果返回空切片。`MergeFacts` 覆盖：首次写入、第二次写入不同键（验证旧键仍在）、第二次写入同键（验证被覆盖）。运行 `/d/golang/bin/go test ./internal/store/postgres/event/ ./internal/store/postgres/fact/ -v`，预期 FAIL。

- [ ] **绿：** 实现两个包。确认 AutoMigrate 为 `task_events.id` 生成的是 `bigserial` 而不是普通 `bigint` —— 查一次 `information_schema.columns` 的 `column_default` 确认，不要假设。

- [ ] **重构 + commit：** commit（`feat(store): event 与 fact 两个聚合包`）。

**风险：** 非信号事件（`Seq <= 0`）分配的序号不能与信号事件的 seq 空间冲突，否则后续信号会误撞去重索引。实现前先读 `sqlite/event.go` 现有的分配方式，保持同样的语义。

---

### 任务 8：host、artifact、audit 三个聚合包

**目标：** 三个相对独立的聚合可用，UPSERT 与倒序查询语义正确。

**文件：**
- 创建：`internal/store/postgres/host/{row.go,store.go}`、`artifact/{row.go,store.go}`、`audit/{row.go,store.go}`
- 测试：三个包各自的 `store_test.go`

**接口契约：** 照抄 `contract.HostStore`（4 个方法，`store.go:143-161`）、`contract.ArtifactSetStore`（**6 个**方法：`CreateArtifactSet`、`GetArtifactSet`、`ListArtifactSets`、`UpdateArtifactSetState`、`SetArtifactSetContent`、`DeleteArtifactSet`，`store.go:168-199`）、`contract.AuditStore`（2 个方法，`store.go:310-324`）。三个包各自导出 `New(db *gorm.DB) *Store` 与 `Models() []any`，与任务 3 同一形态。四处关键语义：

- `UpsertHost`：按 mac 做 `clause.OnConflict` UPSERT（hosts 表主键是 mac；sn **故意不唯一** —— 白牌机 SN 可能空、可能整批重复）
- `FindHostByIdentity`：与 `FindPendingTaskByIdentity` 用**同一套** SN → MAC → UUID 回退优先级，保证建任务与引导时判据一致
- `UpdateArtifactSetState` 与 `SetArtifactSetContent` 是两个独立方法：前者改状态并带 `manifestSHA256`/`reason`，后者写回 `osFamily` 与 `ArtifactPaths`。导入失败时只调前者，**不碰已写入的 Paths**（Paths 是抽取完成后才知道的产物）
- `ListAudit`：按 TS **倒序**；查不到返回**空切片而非 ErrNotFound**；`AuditFilter.SinceTS > UntilTS` 时返回空结果而不是报错

- [ ] **红：** `UpsertHost` 覆盖：首次插入、同 mac 第二次（验证更新而非插入，且行数仍是 1）、两台不同 mac 但 SN 相同（必须都能存在）。`FindHostByIdentity` 覆盖 SN 命中、SN 空 MAC 命中、全空。`ListAudit` 覆盖：倒序、Target 精确匹配（不做子串）、时间区间、SinceTS > UntilTS、无匹配返回空切片。`ArtifactSetStore` 覆盖：登记、按 id 查、改状态后 Paths 不变。运行三个包的测试，预期 FAIL。

- [ ] **绿：** 实现三个包。`audit_log.id` 同样用 `bigserial`。

- [ ] **重构 + commit：** commit（`feat(store): host、artifact、audit 三个聚合包`）。

**风险：** hosts 表主键是 mac 不是 sn。给 sn 建唯一索引会让白牌机整批入库时失败，症状是 discovery 同步静默丢主机。

---

### 任务 9：hook 与 template 两个聚合包

**目标：** 两个复合主键聚合可用；版本语义与引用完整性拦截正确。

**文件：**
- 创建：`internal/store/postgres/hook/{row.go,store.go}`、`template/{row.go,store.go}`
- 测试：两个包各自的 `store_test.go`

**接口契约：** 照抄 `contract.HookStore`（5 个方法，`store.go:202-224`）与 `contract.TemplateStore`（5 个方法，`store.go:226-246`）。两个包各自导出 `New(db *gorm.DB) *Store` 与 `Models() []any`，与任务 3 同一形态。五处关键语义：

- 两张表都是**复合主键** `(id, version)`：GORM 用多个 `gorm:"primaryKey"` 表达。`template_hook_refs` 无主键，归 `template` 包
- `GetHook` / `GetTemplate` 的 `version` 参数**非正数表示取该 id 下最新版本**，正数表示取确切那一版
- `GetHookByDigest`：按 `body_sha256` 反查（渲染时只有 digest 可用，task 与 template pin 的是 digest 不是 id）
- `UpdateHookState`：正文不可变，本方法不接受修改 Body
- `DeleteTemplate`：存在引用该模板的**非终态** task 时返回 `ErrStillReferenced`

- [ ] **红：** `GetHook`/`GetTemplate` 覆盖：取确切版本、version 传 0 取最新、version 传 -1 取最新、id 不存在。`GetHookByDigest` 覆盖命中与不命中。`DeleteTemplate` 覆盖：无引用可删、有非终态 task 引用（期望 `ErrStillReferenced`）、引用的 task 已终态（可删）。`template` 包要覆盖 hook_refs 随模板一起写入与读出，且 `ref_order` 保持顺序。运行两个包的测试，预期 FAIL。

- [ ] **绿：** 实现两个包。

- [ ] **重构 + commit：** commit（`feat(store): hook 与 template 两个复合主键聚合`）。

**风险：** 「version 非正数取最新」容易只实现 0 而漏掉负数。契约写的是非正数，两者都要覆盖。

---

### 任务 10：组合器与事务

**目标：** 八个聚合拼成 `contract.Store`，`RunInTx` 让跨聚合写入原子，嵌套调用被拒绝。

**文件：**
- 创建：`internal/store/postgres/store.go` —— `Store` 结构体（八个嵌入）、`New`、编译期断言
- 创建：`internal/store/postgres/tx.go` —— `RunInTx`
- 修改：`internal/store/postgres/open.go` —— AutoMigrate 接上八个包的 `Models()`
- 测试：`internal/store/postgres/tx_test.go`

**接口契约：**

```go
type Store struct {
    *task.Store; *event.Store; *host.Store; *hook.Store
    *template.Store; *artifact.Store; *fact.Store; *audit.Store
    db   *gorm.DB
    inTx bool
}

func New(db *gorm.DB) *Store
func (s *Store) RunInTx(ctx context.Context, fn func(context.Context, contract.Store) error) error

var (
    _ contract.Store    = (*Store)(nil)
    _ contract.TxRunner = (*Store)(nil)
)
```
- 结构体嵌入让八个窄接口的方法集自动提升，`contract.Store` 免费满足；断言放文件末尾（接口加方法时这里先报错）
- `RunInTx` 用 `db.Transaction(...)`，在事务句柄上**重新组合**一个 `*Store`（`inTx = true`）交给回调 —— 八个聚合跑在同一个 tx 上
- **拒绝嵌套**，判据是 `inTx`。理由与 SQLite 时代不同：PG 下嵌套不死锁，而是静默开出一个**独立**事务，外层回滚时内层已提交，原子性无声失效
- 不需要 `querier` 接口（GORM 事务句柄仍是 `*gorm.DB`，同一个类型）

- [ ] **红：** 覆盖：回调返回 nil 则提交（事务外能读到）、回调返回错误则整个回滚（含跨两个聚合的写入，如 task + event 同时写后回滚，两者都不存在）、回调内再调 `RunInTx` 返回错误且不提交任何东西。运行 `/d/golang/bin/go test ./internal/store/postgres/ -v`，预期 FAIL。

- [ ] **绿：** 实现两个文件。嵌套拒绝用的错误不导出为 contract 层 sentinel —— 嵌套是调用方的编程错误，不是业务分支（与 `internal/core/fake` 的同名判断保持一致）。

- [ ] **重构 + commit：** commit（`feat(store): 组合器与事务，拒绝嵌套`）。

**风险：** 八个嵌入指针若有任一为 nil，调用其方法会 panic 而不是编译错误。`New` 必须把八个全部构造出来，别漏。

---

### 任务 11：conformance 套件接入与一致性守卫

**目标：** 新实现跑通 M1 的 conformance 全套断言 —— 这是整个迁移的验收标准；同时锁住索引名的两处一致性。

**文件：**
- 创建：`internal/store/postgres/conformance_test.go` —— 套件入口
- 创建：`internal/store/postgres/indexname_test.go` —— 索引名一致性守卫
- 测试：以上两者本身就是测试

**接口契约：**

```go
func TestConformance(t *testing.T)   // 调 conformance.RunConformanceSuite
```
- `RunConformanceSuite(t, newStore func() contract.Store)`：`newStore` 每次调用必须返回**全新、彼此隔离的空 Store**（「同一 SN 只能有一个非终态任务」这类断言依赖库是空的）
- 套件的断言逻辑**一行都不改** —— fake 与 postgres 两个实现跑同一套，这是安全网的全部价值所在
- 索引名守卫：断言 `pgerr` 里映射用的索引名与 AutoMigrate 实际建出的索引名字面一致

- [ ] **红：** 写套件入口，每个子测试用任务 6 的 `Open` 路径造独立 schema。运行 `/d/golang/bin/go test ./internal/store/postgres/ -run TestConformance -v`，预期部分子测试 FAIL（这是真实的实现缺口，逐个修）。

- [ ] **绿：** 修到套件全绿。子测试数量应与 `internal/core/fake` 跑出的一致 —— 数量对不上说明入口写漏了。索引名守卫：从 `pg_indexes` 取实际索引名集合，断言 `pgerr` 依赖的两个都在其中。

- [ ] **重构 + commit：** commit（`test(store): conformance 套件在 PG 实现上全绿`）。

**风险：** `conformance/store.go:1173` 那条断言依赖「同一秒内后写入的记录排前面」，即 id 单调递增。若任务 7、8 的 `bigserial` 没落实，这里会失败 —— 这是它的第二个捕获点。另：套件里的时间断言以 `fixedNow = time.Now().Unix()` 为基准，PG 的 timestamptz 往返必须不丢秒级精度，否则「未过期」用例会飘。

---

### 任务 12：装配点切换与旧包删除

**目标：** `cmd/lober` 改用 PG，`internal/store/sqlite` 整体删除，go.mod 清掉 modernc 系。

**文件：**
- 修改：`internal/config/config.go:217-221` —— `Store.DBPath` 换成 `Store.Postgres` 结构体与校验
- 修改：`cmd/lober/wire.go:419-451` —— 删 db_path 建目录、换开库与构造、`buildAppWithDB` 入参改 `*gorm.DB`
- 修改：`cmd/lober/wire_test.go:528` —— 内存 SQLite 换成测试 schema
- 修改：`deploy/lober.yaml.example:176` —— db_path 换成 postgres 段
- 删除：`internal/store/sqlite/` 整个目录（21 个 Go 文件 + `schema/001_init.sql`）
- 修改：`internal/deps/deps.go` —— 删掉 gorm 的空白 import（真实代码已 import，锚点不再需要）；**保留 gin 那一行**，HTTP 层迁移不在本计划范围内，整个文件与目录留给那件事收尾时删
- 修改：`go.mod` / `go.sum` —— 删 modernc 系

**接口契约：**

```go
// internal/config
type Postgres struct {
    Host, User, Password, DBName, SSLMode string
    Port int
}
```
- 配置校验与 config.go 现有风格一致，逐项给出能定位到具体字段的错误
- `artifact_path` 与 `log_path` 的 `ensureDir` **保留**，只删 db_path 那一步（PG 不是本地文件）
- 生产环境密码应走环境变量，示例配置里留空并注释说明

- [ ] **红：** 先只改 config 与 wire，不删旧包。运行 `/d/golang/bin/go build ./...`，预期 FAIL（config 字段变了，wire 引用不上）。这个红灯是编译错误而不是测试失败，因为本任务的变更是接线而非行为。

- [ ] **绿：** 改完 config、wire、wire_test、deploy 示例，`/d/golang/bin/go build ./... && /d/golang/bin/go test ./cmd/... ./internal/config/ -v` 通过。再 `git rm -r internal/store/sqlite` 并 `/d/golang/bin/go mod tidy`，确认 modernc 系从 go.mod 消失、全量测试仍绿。

- [ ] **重构 + commit：** commit（`feat(store)!: 数据层切到 GORM + PostgreSQL，删除 SQLite 实现`）。

**风险：** 删旧包前先确认没有别的包还 import 它 —— `grep -rn "store/sqlite" --include=*.go .` 应该只剩注释。`internal/console`、`internal/gateway` 等七个模块按架构规则不该直接 import store 实现，但要实测确认而不是假设。

---

### 任务 13：文档同步

**目标：** 代码与文档不再互相矛盾。

**文件：**
- 修改：`internal/core/contract/store.go:356` —— 嵌套事务的理由从「SQLite 单写者」改成「PG 下静默开独立事务、原子性无声失效」
- 修改：`internal/core/contract/store.go:9` —— 引用 `001_init.sql` 那句改指向新的 schema 真源（GORM model tag）
- 修改：`internal/archtest/deps_test.go:222` —— 注释里的 `store/sqlite` 改 `store/postgres`
- 修改：`internal/store/postgres/store.go` —— 注释里「七个窄接口」改「八个」（旧注释漏了后加的 AuditStore）
- 修改：`docs/superpowers/specs/SPEC.md` 与 `docs/contracts/*.md` —— 提到 SQLite 的地方改 PG
- 修改：`docs/real-machine/*.md` 六份 —— **不改写历史记录**，只在文件顶部加一句说明数据层已迁移

**接口契约：** 无新接口。`contract` 层只改文档注释，**不改任何签名**。

- [ ] **红：** `grep -rn -i "sqlite" --include=*.go --include=*.md . | grep -v "^./web/"` 列出全部残留，逐条判断是「该改」还是「历史记录保留」。这一步的输出就是待办清单。

- [ ] **绿：** 按清单改完，再跑一次 grep 确认只剩有意保留的历史记录。`/d/golang/bin/go test ./internal/archtest/ ./internal/core/contract/ -v` 通过。

- [ ] **重构 + commit：** commit（`docs: 同步数据层迁移到 PG 的表述`）。

**风险：** `real-machine` 下的实测记录写的是当时真实发生的事，改写它们等于伪造实验记录。只加说明，不动正文。

---

## 整体验收

全部任务完成后，逐条确认：

- [ ] `/d/golang/bin/go build ./...` 退出码 0，无输出
- [ ] `/d/golang/bin/go vet ./...` 退出码 0，无输出
- [ ] `/d/golang/bin/gofmt -l .` 无输出（有输出即有文件未格式化）
- [ ] `/d/golang/bin/go test ./...` 全部包 ok，包数不少于基线的 17 个（新增八个聚合子包后应更多）
- [ ] `/d/golang/bin/go test ./... -race` 全绿 —— PG 下连接池放开后这是真并发的首次验证，CAS 与 partial index 从「第二道防线」变成唯一防线
- [ ] `internal/store/postgres` 的 conformance 套件子测试数量与 `internal/core/fake` 跑出的一致
- [ ] `grep -rn "store/sqlite" --include=*.go .` 无结果
- [ ] `grep -rn "modernc" go.mod` 无结果
- [ ] `grep -n "gorm" internal/deps/deps.go` 无结果，但 gin 那一行还在（gorm 锚点已由真实 import 取代，gin 的留给 HTTP 层迁移）
- [ ] `grep -rniE "SELECT |INSERT INTO|UPDATE .* SET|DELETE FROM" internal/store/postgres/ --include=*.go` 只剩 `Where` 条件片段与 `pg_indexes` 校验查询，无拼接式的数据操作 SQL
- [ ] 手工验证索引未退化：连到一个刚 `Open` 过的库，`SELECT indexdef FROM pg_indexes WHERE tablename IN ('tasks','task_events')`，肉眼确认四个唯一索引的 WHERE 与设计文档 §6 的表一致
- [ ] 手工验证「装完的机器能重装」：建任务 → CAS 到 done → 同 SN 再建任务，第二次必须成功。这是 partial index 生效的端到端证据，也是索引退化后唯一的业务症状
- [ ] 既有行为基线：`internal/core/fake` 的测试、`internal/statemachine`、`internal/gateway`、`internal/console`、`internal/render` 四个模块的测试**一个都没改过**且全绿 —— 它们没被改动是「只换实现不改行为」的证据
- [ ] `git log --oneline` 能看到 13 个（或以上）任务粒度的 commit，没有一个 commit 同时动两个以上聚合包

