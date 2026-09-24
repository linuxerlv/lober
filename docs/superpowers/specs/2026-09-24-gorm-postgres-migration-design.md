# Lober 数据层迁移设计 — 手写 SQL + SQLite → GORM + PostgreSQL

| | |
|---|---|
| 版本 | 1.0（设计定稿，未实现） |
| 日期 | 2026-09-24 |
| 状态 | 待评审 |
| 影响模块 | M2（store），连带 cmd/lober 装配点与 internal/config |

本文档记录「禁止裸 SQL、数据层全面改用 ORM」这一要求的设计结论。要求本身与
引擎选择由项目负责人决定，本文档只负责把决策落成可实现的设计，并写明每个
决策的代价。

---

## 0. 决策摘要

| 决策项 | 选择 | 关键理由 |
|---|---|---|
| ORM | GORM | 生态最大；PG 驱动走 pgx，纯 Go 无 cgo |
| 数据库引擎 | PostgreSQL | 本机已部署；partial index 与 pgx 错误码是 SQLite 做不到的 |
| schema 真源 | GORM model tag + AutoMigrate | 零手写 DDL，model 与 schema 不会漂移 |
| 列类型 | PG 原生：timestamptz + JSONB | 时间可读、JSON 可查可索引 |
| 包结构 | 按聚合拆子包，1:1 对应八个窄接口 | 包边界与契约边界一致 |
| 测试数据库 | 本机 PG，每测试独立 schema | 无 Docker 依赖；CREATE SCHEMA 比 CREATE DATABASE 便宜 |
| 数据迁移 | 全新空库，不搬旧数据 | 现有 SQLite 库无需保留的生产数据 |

## 1. 目标与非目标

**目标。** `internal/store` 内不再出现查询、写入、扫描用途的 SQL 字符串；
数据库换成 PostgreSQL；`internal/core/model` 与 `internal/core/fake` 不变；
`internal/core/contract/conformance` 的断言逻辑不变，继续对 fake 与新实现跑
同一套，作为迁移全程的安全网。

**非目标。** 不改 `contract` 的接口签名（八个窄接口与 `TxRunner` 原样保留）。
不改 M3-M8 七个模块的任何代码。不做与本次迁移无关的重构。不引入外部二进制
依赖（Atlas CLI 等），Makefile 现有的「规则体一律用 go 自带命令」偏好保持。

**能达到的程度要说清。** 查询、写入、扫描三类 SQL 全部消除；DDL 由
AutoMigrate 生成，也不再手写。剩下两处必要的 SQL 片段：CAS 与复合条件的
`Where` 子句（GORM 的条件表达方式本身就是 SQL 片段字符串），以及 §6 的索引
校验查询 `pg_indexes`。两者都不是「拼 SQL 操作数据」，而是条件表达与结构断言。

## 2. 依赖版本

全部锁精确版本，不用版本范围。版本取自 proxy.golang.org，日期为发布时间。

| 模块 | 版本 | 发布 | 用途 |
|---|---|---|---|
| `gorm.io/gorm` | v1.31.2 | 2026-06-22 | ORM 核心 |
| `gorm.io/driver/postgres` | v1.6.3 | 2026-09-14 | PG 方言，内含 partial index 支持 |
| `github.com/jackc/pgx/v5` | v5.11.0 | 2026-09-07 | 驱动，纯 Go |

**移除** `modernc.org/sqlite` 及其连带的 `modernc.org/libc`、`modernc.org/memory`、
`modernc.org/mathutil`、`github.com/remyoudompheng/bigfft`、`github.com/dustin/go-humanize`、
`github.com/ncruces/go-strftime`。

**不引入的依赖与理由。** `gorm.io/datatypes`（v1.2.7，2025-09）只为 JSONB 一个
功能，自己写 Scanner/Valuer 更短；`gorm.io/plugin/optimisticlock`（v1.1.3，
2024-07）模型与本项目的 (state, version) 双条件 CAS 不匹配，且 GORM 原生的
`RowsAffected` 已经够用；`gorm.io/gen` 仍是 0.x 且与 AutoMigrate 真源职责重叠。

## 3. 包结构

```
internal/store/postgres/            组合器：Store、New、Open、RunInTx
internal/store/postgres/pgtype/     JSONB 与可空 timestamptz 的 Scanner/Valuer
internal/store/postgres/pgerr/      pgconn 错误 → contract sentinel error
internal/store/postgres/task/       tasks                          → TaskStore
internal/store/postgres/event/      task_events                    → EventStore
internal/store/postgres/host/       hosts                          → HostStore
internal/store/postgres/hook/       hooks                          → HookStore
internal/store/postgres/template/   templates + template_hook_refs  → TemplateStore
internal/store/postgres/artifact/   artifact_sets                  → ArtifactSetStore
internal/store/postgres/fact/       task_facts                     → FactStore
internal/store/postgres/audit/      audit_log                      → AuditStore
```

八个聚合包 1:1 对应 `contract.Store` 组合的八个窄接口（`contract/store.go:342-351`）。
`template` 包管两张表，因为 `TemplateStore` 这一个接口同时覆盖模板与它的 hook
引用列表。切分依据是**契约边界而不是表数量** —— 这样每个包恰好实现一个接口，
包的职责可以用一句话说完，也不需要判断某个方法该放哪。

每个聚合包自己拥有：GORM 行结构体、列与索引 tag、与 `internal/core/model` 的
双向映射函数、以及导出的 `Models() []any` 供组合器收集给 AutoMigrate。**不设
共享的 row 包** —— 那会变成一个知道所有表的上帝包，与拆聚合的目的相反。

`pgtype` 与 `pgerr` 是两个真正共享的小包：JSON 序列化与错误判定每个聚合都要
用，复制八遍是纯粹的重复。

**archtest 不需要改规则。** `deps_test.go` 的 `moduleGroups` 把 `"store"` 映射
到前缀 `internal/store`（不是 `internal/store/sqlite`），`moduleOf` 按前缀匹配，
且同模块子包互相 import 明确允许。全部新包落在这个前缀下。只有一处提到
`store/sqlite` 的文档注释要更新。

## 4. 事务与组合

按聚合拆包带来的唯一真问题：`TxRunner.RunInTx` 的回调签名是
`fn(ctx, store contract.Store)`，回调拿到的必须是**全部八个窄接口**的能力 ——
建任务要在一个事务里原子写 tasks 与 task_events，跨两个聚合包。

解法是组合器用 Go 结构体嵌入，接口满足免费成立：

```go
// internal/store/postgres
type Store struct {
    *task.Store
    *event.Store
    *host.Store
    *hook.Store
    *template.Store
    *artifact.Store
    *fact.Store
    *audit.Store

    db   *gorm.DB
    inTx bool
}

func New(db *gorm.DB) *Store                    // 顶层构造，inTx = false
func Open(cfg config.Postgres) (*gorm.DB, error) // 连库 + AutoMigrate + 索引校验
func (s *Store) RunInTx(ctx context.Context, fn func(context.Context, contract.Store) error) error

var (
    _ contract.Store    = (*Store)(nil)
    _ contract.TxRunner = (*Store)(nil)
)
```

每个聚合包的构造函数统一收 `*gorm.DB`：`task.New(db *gorm.DB) *task.Store`。
`RunInTx` 用 `db.Transaction(...)`，在事务句柄上重新组合一个 `*Store`（`inTx = true`）
交给回调 —— 八个聚合跑在同一个 tx 上，原子性成立。

现有 `sqlite/store.go` 里的 `querier` 接口**删除**。它存在的唯一理由是统一
`*sql.DB` 与 `*sql.Tx` 两个不同类型；GORM 的事务句柄仍是 `*gorm.DB`，同一个
类型，不需要接口去抹平。

**嵌套调用仍然拒绝，但理由变了。** SQLite 时代的理由是「单写者 + 连接池上限 1，
嵌套开事务会等待自己持有的连接 → 死锁，进程卡死」。PG 下连接池放开后不会死锁，
但后果更坏：嵌套调用会静默开出一个**独立**事务，外层回滚时内层已经提交，
原子性悄悄失效且不报任何错。判定依据从 `rawDB == nil` 换成显式的 `inTx` 标记位。
`contract/store.go:356` 那段引用「SQLite 是单写者模型」的接口文档要改写成这个
新理由 —— 这是本次迁移**唯一**需要改 contract 层的地方，且只改文档注释不改签名。

## 5. 类型映射

`internal/core/model` 不动。时间戳在 model 侧是 `int64` Unix 秒，JSON 载荷是
`map[string]string` 与 `[]string`；PG 侧要 timestamptz 与 JSONB。转换全部发生在
聚合包的行结构体与映射函数里，domain 层看不见 GORM，也不 import 它。

**时间列分两类。** 四个列现在用 `0` 表示「未设置」：`last_signal_at`、
`token_expires_at`、`finalize_expires_at`、`approved_at`。把 0 写成
`time.Unix(0, 0)`（1970-01-01）会在库里留下假数据，查询和人工排查都会被误导。
这四列映射成 `*time.Time`：**model 的 0 ↔ 库里的 NULL**，读回 NULL 时还原成 0。
`created_at`、`state_updated_at`、`phase_entered_at`、`ts`、`last_seen_at` 恒有
值，用 `time.Time`。

时区统一 UTC：写入 `time.Unix(sec, 0).UTC()`，读出取 `t.Unix()`。Unix 秒本身
无时区，`timestamptz` 存的是绝对时刻，往返不丢信息。

**JSON 列。** `pgtype` 包提供两个命名类型（对应 `map[string]string` 与
`[]string`），各自实现 `sql.Scanner` 与 `driver.Valuer`，字段上标 `type:jsonb`。
`tasks.failure_detail_json` 是 `*FailureDetail`，nil 映射 NULL。

**被删除的东西。** `taskColumns` 常量与 `scanTaskRow` 函数一起消失。现有
`task.go:17-21` 的注释自己点出了它们的病：INSERT 与 SELECT 两份列名列表一旦
不一致，症状是运行期列数不匹配或静默漏写字段，只能靠共享常量人工兜住。行
结构体把列名、类型、顺序收敛到一处声明，这个问题从根上不存在。

## 6. Schema 与索引校验

AutoMigrate 从八个聚合包收集的 model 建表建索引。`001_init.sql` 里有四个唯一
索引，其中**三个是 partial**（带 WHERE 条件），一个是普通唯一索引：

| 索引 | 表与列 | WHERE 条件 |
|---|---|---|
| `idx_tasks_sn_active` | tasks (host_sn) | `state NOT IN ('done','failed') AND host_sn != ''` |
| `idx_tasks_token` | tasks (token) | `token != ''` |
| `idx_tasks_finalize_token` | tasks (finalize_token) | `finalize_token != ''` |
| `idx_task_events_task_seq` | task_events (task_id, seq) | 无，普通唯一索引 |

partial 索引靠 index tag 表达 —— PG 驱动的 `CreateIndex` 会输出 `WHERE` 子句
（`postgres/migrator.go`），GORM 核心的通用 migrator 不会，所以这条只在 PG 上成立。

**只有 `idx_tasks_sn_active` 的条件里含逗号**，需要用反斜杠转义。GORM 的 tag
解析按逗号切选项（`schema/index.go` 的 `parseFieldIndexes`），但 `ParseTagSetting`
支持用尾部反斜杠跨过分隔符（`schema/utils.go:23-26`）：

```go
HostSN string `gorm:"column:host_sn;index:idx_tasks_sn_active,unique,where:state NOT IN ('done'\,'failed') AND host_sn != ''"`
```

tag 写在被索引的字段上，WHERE 里可以引用其他列。

**这里有个静默失效风险，必须用测试锁住。** 转义写错时 GORM 不报错，只是把
WHERE 截断成半句或整条丢掉，索引退化成无条件唯一索引。症状不是任何测试失败，
而是**装完的机器再也不能重装** —— 终态任务开始占用 `host_sn` 的唯一约束，而
原设计里终态任务不占用这条索引（`001_init.sql` 的设计注释写明了这一点）。
`idx_tasks_token` 与 `idx_tasks_finalize_token` 退化的后果类似：两个 token 列的
默认值是空字符串，条件丢掉后第二个「尚未签发 token」的任务就会撞唯一约束，
建任务直接失败。

所以 `Open` 在 AutoMigrate 之后跑一次 schema 校验：查 `pg_indexes.indexdef`，
对上表四个索引断言存在、是 unique、且 WHERE 条件与上表字面一致（第四个断言
没有 WHERE）。启动时与测试里都跑。

## 7. 错误映射

`pgerr` 包判 `*pgconn.PgError`，`Code == "23505"`（unique_violation）时读
`ConstraintName`：

| 来源 | sentinel |
|---|---|
| `ConstraintName == "idx_tasks_sn_active"` | `contract.ErrActiveTaskExists` |
| `ConstraintName == "idx_task_events_task_seq"` | `contract.ErrDuplicateSeq` |
| `gorm.ErrRecordNotFound` | `contract.ErrNotFound` |
| 其他 | 原样 `%w` 包装冒泡，不吞 |

现有 `sqlite/errors.go` 整个 150 行删除。那个文件顶部的长注释记录了一处契约
缺口：实现计划要求「按索引名区分，不要按错误文本猜」，但 modernc.org/sqlite
不暴露索引名，只能退到匹配「表名 + 列名集合」。PG 上 `ConstraintName` 直接给
索引名，**原计划的意图能实现了**，连带删掉的还有 `parseUniqueConstraintColumns`、
`isUniqueViolationOnColumns` 和那个抄来的错误码常量 `2067`。

## 8. CAS 与 UPSERT

**CAS** 不引插件。`Model(&taskRow{}).Where("id = ? AND state = ? AND version = ?", ...).Updates(...)`
取返回的 `RowsAffected`，0 即 CAS 失败，翻成 `contract.ErrConflict`。这与现有
`task.go:300-305` 的语义完全一致 —— 条件是 (state, version) 同时成立，不是只
比版本号。

**UPSERT** 用 `clause.OnConflict{Columns:..., DoUpdates: clause.AssignmentColumns(...)}`，
替换现有两处手写的 `ON CONFLICT DO UPDATE`（`fact.go` 的 task_facts 按键写、
`host.go` 的 hosts 按 mac 写）。

`task_events.id` 与 `audit_log.id` 用 `bigserial`。`conformance/store.go:1173`
有一处断言绑定了「同一秒内后写入的记录排前面」这个语义，依赖 id 单调递增；
`bigserial` 同样单调递增，断言不用改，但实现时要确认 AutoMigrate 真的生成了
`bigserial` 而不是普通 `bigint`。

## 9. 测试

**每测试独立 schema**，不是独立 database。PG 的 `CREATE DATABASE` 不能在事务里
执行、开销也大；`CREATE SCHEMA` 加 DSN 里的 `search_path` 便宜得多，隔离性一样。
测试结束 `DROP SCHEMA ... CASCADE`。

DSN 从环境变量 `LOBER_TEST_PG_DSN` 读。**连不上就让测试失败，不是 skip** ——
skip 会让 `make check` 在 store 层零覆盖的情况下变绿，比报错危险得多。失败信息
要写明需要什么（可达的 PG、建 schema 的权限、环境变量怎么设）。

`conformance` 套件的断言逻辑不动，继续对 `fake` 与 `postgres` 两个实现跑同一套。
这是迁移全程的安全网：每迁完一个聚合包就能跑一次，行为对不上立刻暴露。

Makefile 不改。`make check` 的四步（fmt / vet / test / race）语义不变，只是 test
与 race 从此需要一个可达的 PG。

## 10. 配置与装配

`config.Store.DBPath` 删除，换成结构化的 PG 连接配置，与 config.go 现有风格
一致（结构化字段便于逐项校验并给出精确错误）：

```yaml
store:
  postgres:
    host: localhost
    port: 5432
    user: lober
    password: ""      # 生产环境应走环境变量，不写进 lober.yaml
    dbname: lober
    sslmode: disable
  artifact_path: /var/lib/lober/artifacts
  log_path: /var/lib/lober/logs
```

`wire.go` 的改动：`buildApp` 里给 `db_path` 建目录那一步删掉（PG 不是本地文件），
`artifact_path` 与 `log_path` 的 `ensureDir` 保留；`sqlite.Open(dsn)` 换成
`postgres.Open(cfg.Store.Postgres)`；`sqlite.New(db)` 换成 `postgres.New(db)`。
`buildAppWithDB` 的入参从 `*sql.DB` 换成 `*gorm.DB`，它「测试可以传已打开的连接、
不碰文件系统」的用途不变。

## 11. 影响面清单

**删除**：`internal/store/sqlite/` 下 21 个 Go 文件（约 3700 行，含测试）与
`schema/001_init.sql`。

**新建**：`internal/store/postgres/` 下组合器 1 个包 + 共享 2 个包 + 聚合 8 个包。

**修改**：

| 文件 | 改动 |
|---|---|
| `cmd/lober/wire.go` | 开库、构造、删 db_path 建目录 |
| `cmd/lober/wire_test.go` | 内存 SQLite 换成测试 schema |
| `internal/config/config.go` | `Store.DBPath` → `Store.Postgres` 与校验 |
| `deploy/lober.yaml.example` | 第 176 行 db_path 换成 postgres 段 |
| `go.mod` / `go.sum` | 加三个依赖，删 modernc 系 |
| `internal/core/contract/store.go` | 两处注释：§4 的嵌套理由、引用 001_init.sql 那句 |
| `internal/archtest/deps_test.go` | 一处注释提到 `store/sqlite` |
| `internal/core/fake/store.go` | 只看是否有注释提到 SQLite，逻辑不动 |
| SPEC.md 与 docs/ | 约 20 份文件提到 sqlite，含 6 份 real-machine 实测记录 |

**不动**：`internal/core/model`、`internal/core/fake` 的实现逻辑、
`internal/core/contract` 的接口签名、`conformance` 的断言、以及 statemachine /
render / gateway / console / provider / imports 七个模块的全部代码。

历史实测记录（`docs/real-machine/*.md`）记的是当时的真实过程，**不改写历史**，
只在受影响的文件顶部加一句说明数据层已迁移到 PG。

## 12. 已知代价与风险

**AutoMigrate 不可版本化。** 它不删列、不改列类型、不记已应用版本、不能回退。
schema 演进时只能加列；改列型或删列要手工介入。评审时已提出用版本化迁移的
替代方案，决策是选 AutoMigrate，此处仅记录后果：对装机系统而言 schema 升级
出错的后果是整个机房装不了机，因此后续任何改列型或删列的变更都应当单独评审，
不能依赖 AutoMigrate 自动完成。

**部署形态变了。** 原来是单文件 SQLite，零外部服务依赖，符合 air-gapped 内网
的部署预期。现在 lober 多一个 PostgreSQL 服务依赖，部署文档与运维手册需要补
PG 的安装、备份、连接凭据管理。这是引擎选择的固有代价，不是设计缺陷。

**真并发首次出现。** SQLite 时代连接池上限是 1，写入天然串行化。PG 下池子放开
后，CAS 与 partial unique index 从「第二道防线」变成「唯一防线」。`make race`
与 conformance 的并发子测试是主要防护，实现时要确认它们在 PG 下仍然覆盖到
同一批场景。

**静默失效的索引**（§6）是本次迁移风险最高的一点，必须靠 schema 校验兜住，
不能只依赖 tag 写对。

**pgx 错误码与约束名耦合。** `pgerr` 依赖 `ConstraintName` 与索引名字面一致；
索引改名会让错误映射静默失效（冲突变成 500 而不是业务错误）。索引名在行结构体
tag 与 pgerr 两处出现，需要一个测试断言两者一致。





