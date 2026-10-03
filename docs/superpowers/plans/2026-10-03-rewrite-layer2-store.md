# 重写第 2 层：存储 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 在第 1 层之上写出仓储：一份契约，一套契约测试，以及内存与 PostgreSQL 两份实现，两份都跑通同一套契约测试，守住 spec §4.2 列出的约束。

**架构：** `internal/store` 是契约包：每个聚合一个接口，外加错误、审计、事件、分页这些共用类型，以及两份实现共用的纯函数。`internal/store/storetest` 是契约测试套件，`memstore`（内存）与 `pgstore`（PostgreSQL，GORM + AutoMigrate）各自跑这一套。并发情况的兜底在事务里完成：提交前，对加锁后读到的最新数据重跑一遍第 1 层的判定。判定规则仍然只有第 1 层那一份。

**技术栈：** Go 1.27.1；gorm.io/gorm v1.31.2、gorm.io/driver/postgres v1.6.3（底层是 pgx v5）；PostgreSQL 17。

**依据：**
- spec：§4.2、§6.1；§6.2 第 4、8、9、11 条；§6.3「事务不做成切面」；§7.2 第 2 条。
- 第 1 层原计划与修补计划。
- 遗留问题清单（`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`）里「修补轮之后新发现的问题」一节的 C-1 到 C-10。

---

## 范围

本层要做的：主机；ISO（记录与状态，不含获取来源）；hook；模板；任务（批量建，以及状态的条件推进）；事件（任务的追加式日志）；审计。

推迟到第一个用到它的层（与第 1 层「由第一个用到它的层加进来」是同一个做法）：
- ISO 的获取来源（下载断点、上传分片、本地路径）：第 4 层。
- 用户、角色与会话：第 5 层。本层的审计里，操作者只是一个字符串。

推迟是安全的：AutoMigrate 能给已有的库加新表、新列、新索引，做不到的是修改已存在的索引和约束。所以本层建的表，索引和约束必须一次定对，由表结构断言守住（任务 7）。

不在本层：P-1、P-2 两条待拍板项；T5-3、T6-3 两处待澄清项。

## 开工前

- [ ] 从 dev（8484e3a）开分支：`git -C /e/src/lober checkout -b feat/store dev`。
- [ ] 本机 PostgreSQL 17 在跑（服务名 postgresql-x64-17）。测试角色 lober_test 能连上库 lober_test：它不是超级用户，不能建库，但拥有这个库，可以在里面建 schema。
- [ ] 测试用的连接串（DSN）有两种给法：环境变量 `LOBER_TEST_PG_DSN`，或模块根目录下的 `.pgtest.dsn` 文件（一行 DSN，任务 7 把它加进 .gitignore）。两处都没有时，PostgreSQL 的测试直接失败，不跳过：跳过会让 make check 在存储层零覆盖的情况下变绿。

## 已定的决定

| 决定 | 由谁定 |
|---|---|
| 用 GORM + AutoMigrate 建表，每次启动断言表结构 | 用户，2026-10-03 |
| 名字放在身份上：每个 hook、每个模板各一行身份，名字唯一，各版本共用；改名是单独的操作，要审计 | 本计划 |
| 已审批和已废弃的版本永不删除，只能删草稿。这比 C26 严：C26 只禁止删被引用的 | 本计划 |
| 每个身份同时最多一个未审批的版本（草稿或待审批） | 本计划 |
| 审批和建任务时，在事务里对加锁后的最新数据重跑第 1 层的判定（activation、admission），作为并发兜底 | 本计划 |
| 审计按对象写：批量建任务、CSV 导入时，每个对象一条。spec §5.3 写的是「每次提交记一条审计」 | 本计划 |
| 可变的记录都带乐观锁版本 Rev；仓储不读时钟，时间都由调用方给；ID 由仓储生成 | 本计划 |
| 测试用的失败注入：审计的操作者是保留值 `lober-test-fault` 时，测试夹具让这条审计写入失败 | 本计划 |

## 包与文件

| 包 | 职责 | 可以依赖 |
|---|---|---|
| `internal/store` | 契约：每个聚合的接口、共用类型、错误，以及两份实现共用的纯函数（输入校验、生成 ID、提取模板的 hook 引用、在事务里重跑判定的封装） | 标准库、`internal/core/...` |
| `internal/store/storetest` | 契约测试套件：Harness 接口，以及按聚合分组的测试函数 | 标准库、`internal/core/...`、`internal/store` |
| `internal/store/memstore` | 内存实现：契约测试的参照实现，第 5 层的 HTTP 测试也用它 | 标准库、`internal/core/...`、`internal/store` |
| `internal/store/pgstore` | PostgreSQL 实现：GORM 的行类型、AutoMigrate、表结构断言、数据库错误的翻译 | 上一行，加 `gorm.io/...` 与 `github.com/jackc/pgx/v5/...` |

为什么这样切：
- 契约单独成包，两份实现与将来的调用方（第 4、5、7 层）都只依赖契约，不依赖任何一份实现，换实现不改调用方。
- 共用的纯函数放在契约包里，两份实现调用同一份。规则写两遍就会走偏，旧系统的 fake 与 SQLite 排序不一致（fc71387）就是这样出的。
- 行类型（带 gorm tag）只在 pgstore 里。第 1 层的 model 类型保持不带 tag，第 1 层修补时 T7-4 定过：字段改名由 model 的 golden 测试守住。
- 每个聚合在实现里各占一个文件，任务可以按聚合并行。

## 全局约定

- 每个方法的第一个参数是 ctx，最后一个返回值是 error。
- 返回的切片与 map 都是深拷贝，调用方改了也不影响仓储里的数据；入参也先拷贝再存。
- 仓储不读时钟。时间（`Audit.At`、`Task.CreatedAt`、事件的 `At`）都由调用方给，必须大于 0，否则报 ErrInvalid。
- ID 由仓储用 `store.NewID` 生成：128 位随机数，编成不带填充的小写 base32，共 26 个字符。它满足 `model.ValidID` 与 `model.IsSafeWord`（L-13b）。内置的 hook 与模板用自己带的固定 ID。
- 要审计的操作，审计在同一个事务里、在动作本身的写入之后写，所以审计写失败时，动作一起回滚（spec §6.2 第 8 条）。
- 仓储的每个操作返回时事务已经提交（spec §6.2 第 9 条）；仓储不发通知，通知由调用方在拿到返回值之后发。
- pgstore 的更新一律显式列出要写的列（GORM 的 `Select(...)`），不用不带 Select 的 `Updates(struct)`：后者会跳过零值字段，把属性清空、把固件改回空这类写入会被静默丢掉。契约测试专门覆盖「改成零值」。
- 中文说明仍只在 `model/codes.go`。仓储能用现有代码表达的问题，返回 `model.Problem`（比如建任务时重跑准入）；其余返回带类型的错误，由第 5 层映射成说明。

## 仓储契约

各任务都按这一节实现与测试。这一节是本计划最主要的决定，评审时请先看这里。

### 共用类型

| 类型 | 字段与约定 |
|---|---|
| `Audit` | 调用方给 `Actor`、`Action`、`Detail`、`At`。Actor 是可存文本，1–128 字节；Action 匹配 `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`，不超过 64 字节，如 `host.import`；Detail 是可存文本，不超过 4096 字节，可空；At 大于 0。零值 Audit 表示没给审计 |
| `AuditEntry` | 存下来的一条：仓储分配的 `ID`（递增整数）、调用方给的 Audit，以及仓储填的 `TargetKind`（host、iso、hook、template、task 之一）和 `TargetID`（SN，或「ID」，或「ID@版本」） |
| `NewEvent` | `At`；`Kind`，规则同 Action；`Key`，可空，非空时是 1–128 字节的安全词，同一个任务里唯一，用来去重；`Fields`，键是标识符、值是可存文本，合计不超过 64 KiB |
| `Event` | `TaskID`、仓储分配的 `Seq`（每个任务从 1 起、连续），以及 NewEvent 的字段 |
| `Appended` | 追加一条事件的结果：`Seq`，以及 `Duplicate`。Key 已存在时 Duplicate 为 true，Seq 是原来那一条的 |
| `Page[T]` | `Items` 与 `Next`。Next 是不透明的游标，空串表示没有下一页 |
| 各查询类型 | 都带 `Cursor string` 与 `Limit int`：Limit 为 0 时取 100，1–500 照用，超过 500 报 ErrInvalid。顺序固定：主机按 SN；ISO、hook、模板按名字的 key，再按 ID；任务按 CreatedAt 倒序，再按 ID；审计按 ID 倒序；事件按 Seq |
| `TaskExpect` | 条件推进的期望值：`State` 与 `Rev` |

### 错误

| 错误 | 什么时候返回 |
|---|---|
| `ErrNotFound` | 按键找不到 |
| `ErrConflict` | Rev 或期望的状态对不上；或者已有一个未审批的版本，又要新开一个。调用方重读之后再判断（spec §6.2 第 4 条） |
| `ErrDuplicate`，包在 `*DuplicateError{Field, Existing}` 里 | 唯一键撞了：Field 取 sn、name、sha256；Existing 是已有的那一个（SN 或 ID） |
| `ErrImmutable` | 改已审批、已废弃或内置的版本；改内置的身份 |
| `ErrReferenced` | 删除被引用的对象：主机或 ISO 被任务引用，hook 草稿被模板版本引用 |
| `ErrAuditRequired` | 要审计的操作没收到审计（Audit 是零值）。一条都不写 |
| `ErrInvalid`，可以包在 `*ProblemsError` 里 | 入参违反仓储的前提：ID 格式、时间、快照与任务字段不一致、一批里的 ISO 或模板不同等等；记录本身没过 model 的校验时，问题列在 `Problems` 里 |
| `ErrRejected`，包在 `*ProblemsError` 或 `*BatchError{Result admission.BatchResult}` 里 | 在事务里重跑第 1 层的判定没通过：审批时重跑激活判定，建任务时重跑准入判定 |
| `ErrBuiltinMismatch` | 装载内置内容时，库里同一个 ID 与版本的内容和程序自带的不同（spec §6.2 第 11 条） |

带类型的错误都实现 `Unwrap`，返回上表左列的那个哨兵值，调用方用 `errors.Is` 判断类别、用 `errors.As` 取细节。`*ProblemsError` 有两个字段：`Kind` 是它包着的哨兵值（ErrInvalid 或 ErrRejected），`Problems` 是 model.Problem 列表；`*BatchError` 的 `Result` 与准入判定的返回值同形，第 5 层可以原样逐台返回。

### 接口

`store.Store` 由下面七个接口组成，外加 `Close() error`。表里省略了 ctx 与返回的 error；「审计」一列为「要」的方法，最后一个参数是 `Audit`。

**Hosts**

| 方法 | 入参 → 返回 | 语义 | 审计 |
|---|---|---|---|
| `CreateHost` | `model.Host` → `model.Host` | SN 必须已经规范化（与 NormalizeSN 的结果相同），并且 Host.Validate 通过，否则报 ProblemsError(ErrInvalid)；SN 已登记报 DuplicateError；Rev 置 1 | 要 |
| `UpdateHost` | 带 Rev 的 `model.Host` → `model.Host` | 按 SN 整条替换架构、固件与属性；Rev 对不上报 ErrConflict；成功后 Rev 加 1 | 要 |
| `ImportHosts` | `[]model.Host` → `[]model.Host` | CSV 批量导入：已登记的 SN 整条替换，不比 Rev；没登记的新建。一条不合法，整批都不写，ProblemsError 列出每一条的问题（Subject 为 host 与 SN）；批里 SN 重复报 host_duplicate_in_batch。每台主机写一条审计 | 要 |
| `DeleteHost` | SN、Rev | 有任务引用报 ErrReferenced | 要 |
| `GetHost` / `GetHosts` | SN → Host；SN 列表 → map | 前者找不到报 ErrNotFound；后者只放存在的 | — |
| `ListHosts` | `HostQuery{SNPrefix, AttrKey, AttrValue}` → `Page[model.Host]` | AttrKey 与 AttrValue 要么都给，要么都不给；给了就要求属性值完全相等 | — |

**ISOs**

| 方法 | 入参 → 返回 | 语义 | 审计 |
|---|---|---|---|
| `CreateISO` | 只填 Name、Family、Series 的 `model.ISO` → `model.ISO` | 系必须属于族；Name 是可存文本；生成 ID；State 为 acquiring，Rev 为 1 | 要 |
| `UpdateISO` | 带 Rev 的 `model.ISO` → `model.ISO` | 第 4 层导入用的系统更新，不审计：可以改 Version、Arch、State、SHA256、Artifacts、Offers；Family、Series、Name 不能改（ErrInvalid）。要置为 ready 时，必须通过 ISO.Validate，否则报 ProblemsError(ErrInvalid)；不能经它置为 deprecated。SHA256 撞上另一个不是导入失败的 ISO，报 DuplicateError，Existing 是对方的 ID（C4） | — |
| `DeprecateISO` | ID、Rev | 只能从 ready 改成 deprecated，其他状态报 ErrConflict | 要 |
| `DeleteISO` | ID、Rev | 有任务引用报 ErrReferenced | 要 |
| `GetISO` / `ListISOs` | ID；`ISOQuery{Family, Series, States}` | | — |

**Hooks**（版本的状态：draft → review → approved → deprecated；review 可以退回 draft）

| 方法 | 入参 → 返回 | 语义 | 审计 |
|---|---|---|---|
| `CreateHook` | 不带 ID 与版本的 `model.Hook` → `model.Hook` | 新身份加版本 1 的草稿。名字按 NormalizeName 规范化，不合法报 ProblemsError(ErrInvalid)；key 被别的身份占了，报 DuplicateError。说明与每份实现的正文都要是可存文本，否则报 text_not_storable | — |
| `SaveHookDraft` | 带 ID、版本、Rev 的 `model.Hook` → `model.Hook` | 只能改草稿，否则报 ErrImmutable；Name 必须等于身份现在的名字，否则报 ErrInvalid（改名用 RenameHook）；Rev 对不上报 ErrConflict | — |
| `NewHookVersion` | ID、来源版本号、作者 → `model.Hook` | 复制来源版本的内容，建下一个版本号的草稿。已有草稿或待审批的版本报 ErrConflict；内置的报 ErrImmutable | — |
| `SubmitHook` | `HookKey`、Rev | draft → review | 要 |
| `ApproveHook` | `HookKey`、Rev、审批人 | review → approved，Approver 置为审批人。事务里对最新数据重跑 activation.Hook 与 activation.Approver，有问题报 ProblemsError(ErrRejected)，状态不变 | 要 |
| `RejectHook` | `HookKey`、Rev | review → draft；理由写在审计的 Detail 里 | 要 |
| `DeprecateHook` | `HookKey`、Rev | approved → deprecated | 要 |
| `RenameHook` | ID、新名字 | 规则同 CreateHook；内置的报 ErrImmutable | 要 |
| `DeleteHookDraft` | `HookKey`、Rev | 只能删草稿；被任何模板版本引用（包括草稿）报 ErrReferenced。删的是这个身份的最后一个版本时，身份一起删掉，名字随之空出来 | — |
| `GetHook` / `GetHooks` / `ListHookVersions` | `HookKey`；`[]HookKey` → map，只放存在的；ID → 全部版本，按版本号升序 | | — |
| `ListHooks` | `HookQuery{NamePrefix, States, ApprovedOnly}` → `Page[model.Hook]` | 每个身份一项：ApprovedOnly 时给当前的已审批版本（与 model.CurrentApproved 同一规则），没有就跳过；否则给最新的版本 | — |
| `EnsureBuiltinHook` | `model.Hook`（Builtin 为 true，State 为 approved） | 同一个 ID 与版本不存在就写入；已存在并且内容相同，什么都不做；内容不同报 ErrBuiltinMismatch；名字被用户建的身份占了，报 DuplicateError | — |

返回的每个版本，Name 都是身份现在的名字。

**Templates**：与 Hooks 一一对应（CreateTemplate、SaveTemplateDraft、NewTemplateVersion、SubmitTemplate、ApproveTemplate、RejectTemplate、DeprecateTemplate、RenameTemplate、DeleteTemplateDraft、GetTemplate、ListTemplateVersions、ListTemplates、EnsureBuiltinTemplate），另有四处不同：
- `DeriveTemplate`：来源的 `TemplateKey`、新名字、作者 → `model.Template`。来源可以是任何状态。复制族、系、正文、参数、取值来源与装配表，建一个新身份的版本 1 草稿，DerivedFrom 记下来源（C24）。不审计。
- `ApproveTemplate`：事务里锁住它引用的 hook 版本，对最新数据重跑 activation.Template 与 activation.Approver。引用的 hook 刚被废弃时，会因此报 hook_ref_deprecated（C26）。
- `ListTemplates` 的查询多一个 `Series`：只列勾了这个系的。
- 保存模板的任何一个版本时，仓储同时维护这个版本引用了哪些 hook 版本，DeleteHookDraft 与建任务都靠它。

**Tasks**

| 方法 | 入参 → 返回 | 语义 | 审计 |
|---|---|---|---|
| `CreateTasks` | `[]model.Task` → `[]model.Task` | 见下面「批量建任务」 | 要，每个任务一条 |
| `GetTask` / `FindOpenTask` | ID → Task；SN → (Task, 是否找到) | 后者找这台主机未结束的那个任务（C31 保证最多一个），给第 7 层引导时用 | — |
| `ListTasks` | `TaskQuery{HostSN, States, CreatedFrom, CreatedTo}` → `Page[model.Task]` | | — |
| `TransitionTask` | ID、`TaskExpect`、目标状态、`[]NewEvent` → (`model.Task`, `[]Appended`) | 条件推进：当前的状态与 Rev 都等于期望值才写入，否则报 ErrConflict；成功后 Rev 加 1。事件与状态在同一个事务里写，事件的规则同 AppendEvents。目标状态不合法报 ErrInvalid。仓储不判断这一步推进合不合规矩，那是第 7 层状态机的事 | — |
| `TransitionTaskByUser` | 同上 | 用户发起的推进（取消、重试），其余与 TransitionTask 相同 | 要 |

推进到未结束的状态时，如果这台主机已有另一个未结束的任务（比如失败的任务重试时，期间又建了新任务），会撞上「一台主机一个未结束任务」的约束，报 ProblemsError(ErrRejected)，问题是 host_has_open_task。

**批量建任务（C29、C30、C31）**

1. 先查入参，不合格报 ErrInvalid：每个任务的 ID、State、Rev 都是零值；CreatedAt 大于 0，CreatedBy 不为空；Snapshot.Schema 等于 model.SnapshotSchema；HostSN、ISOID、TemplateID、TemplateVersion 与快照里的一致；同一批的 ISO 与模板版本都相同（spec §2.7）；批里 SN 不重复。
2. 开事务，按 SN 升序给涉及的主机加写锁，对 ISO、模板版本和它引用的 hook 版本加读锁。拿锁的顺序固定，两批同时提交时就不会互相等死。
3. 用锁住之后读到的数据组出 admission.BatchInput 并调用 admission.Batch。没登记的 SN 在 ByHost 里报 host_not_registered（任务 1 新增的代码）。任何一处有问题，整批回滚，报 BatchError(ErrRejected)，不写任务，也不写审计。
4. 全部通过才写：ID 用 NewID 生成，State 为 pending，Rev 为 1。每个任务写一条审计。部分唯一索引（任务 9）是最后一道防线：撞上它时，同样回滚并报 BatchError，问题是 host_has_open_task。

**Events**

| 方法 | 入参 → 返回 | 语义 | 审计 |
|---|---|---|---|
| `AppendEvents` | 任务 ID、`[]NewEvent` → `[]Appended` | Seq 由仓储按任务连续分配；Key 已存在的那一条跳过，报告为 Duplicate，不是错误（第 7 层收到重发的信号时据此去重）；任务不存在报 ErrNotFound；事件不合法报 ErrInvalid，整批不写 | — |
| `ListEvents` | 任务 ID、afterSeq、limit → `[]Event` | 返回 Seq 大于 afterSeq 的，按 Seq 升序；limit 的规则同分页 | — |

**AuditLog**

| 方法 | 入参 → 返回 | 语义 |
|---|---|---|
| `ListAudit` | `AuditQuery{TargetKind, TargetID, Actor, Action, From, To}` → `Page[AuditEntry]` | 各过滤条件都是完全相等，From、To 是含两端的时间范围 |

要审计的操作：上表里「审计」一列为「要」的那些。不审计的：草稿的新建、保存、开新版本、派生和删除（spec §6.2 第 8 条：编辑草稿不写审计）；ISO 的系统更新；系统发起的任务推进；事件；装载内置内容。

## 表结构（pgstore）

列名由 GORM 按字段名转成 snake_case。嵌套的 model 值（属性、参数、取值来源、装配表、实现、制品、提供的集合、快照、事件字段）存成 jsonb。不用 gorm.Model，没有自动时间戳，也没有软删除。

| 表 | 主键 | 唯一与部分唯一索引 | 外键（删除时） | CHECK |
|---|---|---|---|---|
| hosts | sn | — | — | rev ≥ 1 |
| isos | id | sha256，WHERE sha256 <> '' AND state <> 'failed'（C4） | — | state 在五个取值里；rev ≥ 1 |
| hooks | id | name_key | — | — |
| hook_versions | (hook_id, version) | hook_id，WHERE state IN ('draft', 'review')（每个身份最多一个未审批版本） | hook_id → hooks（RESTRICT） | state 在四个取值里；version ≥ 1；rev ≥ 1 |
| templates | id | name_key | — | — |
| template_versions | (template_id, version) | template_id，WHERE state IN ('draft', 'review') | template_id → templates（RESTRICT） | 同 hook_versions |
| template_hook_refs | (template_id, template_version, hook_id, hook_version) | — | (template_id, template_version) → template_versions（CASCADE） | — |
| tasks | id | host_sn，WHERE state NOT IN ('done', 'failed', 'cancelled')（C31） | host_sn → hosts、iso_id → isos、(template_id, template_version) → template_versions，都是 RESTRICT | state 在八个取值里；rev ≥ 1；created_at > 0 |
| task_events | (task_id, seq) | (task_id, key)，WHERE key <> '' | task_id → tasks（RESTRICT） | seq ≥ 1；at > 0 |
| audit_log | id（自增） | — | — | at > 0 |

另有几个非唯一索引，供查询用：template_hook_refs (hook_id, hook_version)；tasks (state)、tasks (created_at)；audit_log (target_kind, target_id)、audit_log (actor)、audit_log (at)。

- template_hook_refs 对 hook_versions 刻意不建外键：草稿可以引用不存在的 hook 版本（激活时才报 hook_ref_missing），而外键会让这样的草稿存不进去。删除 hook 草稿时，由 DeleteHookDraft 在事务里查这张表。
- 模板的系列表存成 jsonb 数组，ListTemplates 按 `series @> '["rocky"]'` 这样的包含关系过滤。
- 任务的每条事件，Seq 由 tasks 表上一个计数列在同一事务里加 1 分配，所以同一个任务并发追加时，Seq 仍然连续。

**表结构断言。** Open 在 AutoMigrate 之后，查 pg_indexes 与 pg_constraint（都限定 `schemaname = current_schema()`），逐项比对上表：每个索引比是否唯一、列的集合与顺序、WHERE 子句；每个外键比列、被引用的表与列、删除时的动作；每个 CHECK 比定义。我们的表上多出来的索引或约束也算不一致（主键除外）。不一致就拒绝启动，并逐条写出差在哪里、该怎么改。期望值按 PostgreSQL 规范化之后的输出来写，例如 `<>` 而不是 `!=`、`''::text`、`<> ALL (ARRAY[...])`。先建一次表，把真实的定义打出来，再照着写。

为什么非断言不可：AutoMigrate 有两个不报错的盲区，旧系统都踩过。一是复合索引的每个列都要标同名的 tag，漏标一个就静默建成单列索引（旧系统的事件表因此把 (task_id, seq) 建成了 (seq)，并发任务的事件被当成重复丢掉）；二是已存在的同名索引永远不会被更新，改了定义的库原地不动。两者在编译、测试、启动时都不报错，断言是唯一的防线。

## 并行执行

做法同第 1 层：每个任务在 `.tmp/wf/s<N>` 的本地克隆里做，评审通过后由串行的并入步骤压成一个提交，快进并入主目录的 `feat/store`。

依赖：任务 2、3 依赖 1；任务 4 依赖 2；任务 5 依赖 4；任务 6 依赖 3、5；任务 7 依赖 4；任务 8 依赖 5、7；任务 9 依赖 6、8；任务 10 在其余全部并入之后做。任务 3 只改第 1 层，与存储层的任务并行。

- 只改本任务「文件」一节列出的文件。
- 新的原因代码只有 host_not_registered 一个，由任务 1 登记。确需计划外的代码时，照第 1 层的做法追加在 `codes.go` 与 `problem_test.go` 的 extraCodes 末尾，并在报告里说明。
- 契约测试按分组写成可导出的函数，每份实现的测试文件调用自己已经支持的分组。任务 6 与任务 9 把调用改成跑全部分组的 `storetest.RunAll`。在那之前，还没做的方法先返回一个未导出的「尚未实现」错误；整体验收要求这种占位一个不剩。
- PostgreSQL 的测试每次 Open 都新建一个随机名字的 schema，用完就删，所以几个代理同时跑测试不会互相干扰。

---

### 任务 1：第 1 层的模型增补

**目标：** 给仓储要用的三样东西在 model 里落地：乐观锁版本、ID 规则，以及建任务时要用的 host_not_registered。

**文件：**
- 修改：`internal/core/model/registry.go`（Host、ISO）、`authoring.go`（Hook、Template）、`task.go`（Task）—— 各加 `Rev int64` 与注释
- 修改：`internal/core/model/ident.go`、`ident_test.go` —— ValidID
- 修改：`internal/core/model/codes.go`、`problem_test.go` —— 登记 host_not_registered
- 修改：`internal/core/model/testdata/` 下的 golden 文件 —— 加了 Rev 之后用 `-update` 重写，并在提交说明里写明是因为这个

**接口契约：**

```go
func ValidID(s string) bool // ^[a-z0-9][a-z0-9-]{0,63}$
```

- Rev 的注释写明：仓储的乐观锁版本，每次写入加 1，由仓储维护；调用方改记录时原样带回，对不上就写不进去。第 1 层的判定都不看它，快照里也不放它。
- Task.ID 的注释补一句：由仓储用 store.NewID 生成。
- host_not_registered：类别 state，不带参数，Subject 为 {host, SN}。说明大意是「这台主机还没登记，或者已经被删了」，下一步是「到主机列表里重新登记它，再建任务」。

- [ ] **红：** 写失败测试：ValidID 的合法取值（26 个字符的小写 base32、builtin-kickstart、单个字符）与非法取值（空串、大写、下划线、@、点、以 - 开头、65 个字符）；代码表与附录加 extraCodes 逐项一致，共 70 个；golden 比对因为多了 Rev 而失败。运行 `go -C <克隆> test ./internal/core/model/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现后，用 `go -C <克隆> test ./internal/core/model/ -run TestGolden -update` 重写 golden 文件。用 `git -C <克隆> diff` 确认 host、iso、hook、template、task 五个文件只多了 Rev、snapshot 没变，再跑全部测试，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model): 仓储要用的乐观锁版本、ID 规则与 host_not_registered`）。

**风险：** 改 golden 之前先读 golden_test.go 的文件头，那里写明了什么时候可以用 `-update`。

### 任务 2：仓储契约包与依赖守卫

**目标：** 把「仓储契约」一节写成 `internal/store` 包，加上两份实现共用的纯函数；并把依赖守卫扩到存储层。

**文件：**
- 创建：`internal/store/doc.go`（包说明：契约的总则，即「全局约定」一节）、`types.go`（共用类型与各查询类型）、`errors.go`（哨兵错误与带类型的错误）、`store.go`（七个接口与 Store）
- 创建：`internal/store/check.go`、`check_test.go` —— 共用的纯函数
- 修改：`internal/archtest/deps_test.go`、`guard_test.go` —— 存储层的依赖规则

**接口契约：** 类型、错误与接口照「仓储契约」一节。共用的纯函数：

```go
func NewID() string
func CheckAudit(a Audit) error              // nil，ErrAuditRequired，或 ErrInvalid
func CheckEvents(es []NewEvent) error       // ErrInvalid
func CheckQuery(cursor string, limit int) (int, error)
func CheckNewTasks(ts []model.Task) error   // ErrInvalid：批量建任务的第 1 步
func HookRefs(t model.Template) []model.HookKey // 去重，按 ID、版本排序
func ApproveHookProblems(h model.Hook, approver string) error
func ApproveTemplateProblems(t model.Template, hooks map[model.HookKey]model.Hook, approver string) error
func BatchProblems(in admission.BatchInput, missing []string) error
```

- 三个 *Problems 函数没有问题时返回 nil，有问题时返回 ProblemsError 或 BatchError（都包着 ErrRejected）。两份实现都在事务里、用加锁后的数据调用它们，判定规则因此只有第 1 层一份。
- BatchProblems 的 missing 是没登记的 SN，每个在 ByHost 里报一条 host_not_registered。
- NewID 用 crypto/rand，生成的 ID 满足 model.ValidID 与 model.IsSafeWord，不含 -，所以不会和内置内容那种带连字符的 ID 撞。

依赖守卫加三条规则：`internal/store` 与 `storetest` 只依赖标准库、`internal/core/...` 和 `internal/store`；`memstore` 不依赖任何第三方模块；只有 `pgstore` 可以依赖 `gorm.io/...` 与 `github.com/jackc/pgx/v5/...`，并且两份实现互不依赖。第 1 层的标准库白名单不套用到存储层。

- [ ] **红：** 写失败测试：CheckAudit 的零值、各字段的合法与非法取值、Action 的正则边界；CheckEvents 的 Key、Kind、Fields 规则与 64 KiB 上限；CheckQuery 的 0、1、500、501 与负数；CheckNewTasks 的每一条前提各一个反例；HookRefs 的去重与排序；三个 *Problems 函数各一例通过、一例不通过（不通过的问题与直接调用第 1 层判定得到的完全相同）；NewID 生成 1000 个互不相同、都合规；守卫的三条新规则，各用内存源码一正一反。运行 `go -C <克隆> test ./internal/store/ ./internal/archtest/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): 仓储契约与两份实现共用的纯函数`）。

**风险：** 契约包会被三层调用，接口名以「仓储契约」一节为准，不要另起名字。接口里的方法多，测试只覆盖纯函数；接口本身由任务 4–9 的契约测试覆盖。

### 任务 3：第 1 层清理（C-1 到 C-10）

**目标：** 修掉修补轮之后跨任务评审留下的十条建议。问题、影响和出处都写在遗留问题清单「修补轮之后新发现的问题」一节，以它为准。

**文件：**
- 修改：`internal/core/admission/admission.go`、`admission_test.go` —— C-1、C-6
- 修改：`internal/core/scan/scan.go`、`scan_test.go`，`activation/hook.go`、`template.go`，`params/params.go`，`snapshot/build.go` —— C-2、C-3
- 修改：`internal/core/model/problem.go`、`codes.go`、`registry.go`、`param.go`、`codes_align_test.go` —— C-4、C-5、C-7、C-8、C-9
- 修改：`docs/superpowers/plans/2026-10-02-layer1-fixups.md` —— C-10

**做法：**
- C-1：准入时 ISO 的系不在模板勾的系里，就只报 series_not_ticked，跳过集合检查，与参数求值、快照构建一致；改 TestTemplateUsable几个原因一起报 的期望。
- C-2：「整理正文里的参数引用与子值」收进 scan，作为扫描结果的一个方法（activation 与 params 都已依赖 scan），两边改为调用它，删掉各自的那一份；「被引用即必填」跟着放进这个方法的结果里。「哪种实现的正文要展开」做成 model.Impl 的一个方法，activation、params、snapshot 都用它。
- C-3：字段键（`tpl/<参数名>`、`<插槽名>/<序号>/<参数名>`）的构造收进 model，activation、params、snapshot 都改用它。依赖守卫不许 activation 依赖 params，所以不能放在 params 里。另加一条测试：同一格在激活判定与参数求值里得到的 Subject.Path 相同。
- C-4：model.Subject 的注释列全 Path 的全部写法，与修补计划 U6 的表一致，并补上 ISO 与主机的字段。
- C-5：codes.go 里 iso_offer_missing、series_not_ticked、impl_kind_mismatch、impl_timing_mismatch 的注释，补上新的产出方与各自的 Subject。
- C-6：准入时，主机登记的架构或固件不合法，先报 Host.Validate 给出的 host_platform_invalid，不再拿它去比平台。
- C-7：状态为空串时，说明里的称呼回落到 codes.go 登记的「未知状态」，不再显示成「现为「」」。
- C-8：「选项值是互不重复的安全词，显示名过 ValidLabel」抽成 model 里的一个函数，参数声明与 ISO 提供的集合都用它。
- C-9：host_platform_invalid、placeholder_unknown_kind、script_placeholder_not_expanded 说明里写的取值与种类，补进 codes_align_test。
- C-10：修补计划第 34 行改成 T13-8 在清单第 2 节。

- [ ] **红：** C-1、C-3、C-6、C-7、C-8、C-9 先各写一个失败的测试。C-2 是重构，先确认现有测试全绿，作为基线。运行 `go -C <克隆> test ./internal/core/... -count=1`，预期新测试 FAIL。
- [ ] **绿：** 实现，预期 PASS；守卫测试也要通过，新的共用函数不能破坏层内的依赖方向。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`refactor(core): 第 1 层修补轮之后的十条清理`）。

**风险：** 这个任务和任务 4–9 同时在跑，但它只改 internal/core 与一份文档，不会冲突。C-1、C-6 改了准入的输出，任务 6 的契约测试会断言建任务时的问题，所以任务 6 排在本任务之后。

### 任务 4：主机、ISO 与审计——契约测试与内存实现

**目标：** 搭起契约测试套件与内存实现，先让主机、ISO、审计三组测试在内存实现上跑通。

**文件：**
- 创建：`internal/store/storetest/harness.go` —— Harness、FaultActor、RunAll
- 创建：`internal/store/storetest/hosts.go`、`isos.go`、`audit.go` —— 三组测试
- 创建：`internal/store/memstore/memstore.go` —— Store 结构、New、事务的暂存与提交、审计写入
- 创建：`internal/store/memstore/hosts.go`、`isos.go`、`audit.go`
- 创建：`internal/store/memstore/memstore_test.go`、`export_test.go` —— 跑契约测试；失败注入的开关

**接口契约：**

```go
const FaultActor = "lober-test-fault"
type Harness interface {
	// Open 返回一个全新的空仓储，测试结束时由 t.Cleanup 收拾。审计的 Actor 为 FaultActor 时，
	// 这条审计的写入必须在动作本身写完之后、同一个事务里失败。
	Open(t *testing.T) store.Store
}
func RunHosts(t *testing.T, h Harness) // RunISOs、RunAudit 同形
func RunAll(t *testing.T, h Harness)
func New() *Store // memstore 包
```

- 内存实现用一把锁让所有操作串行；每个写操作先在暂存的副本上改，全部成功再一次提交，中途出错就丢掉副本，所以一次写要么全做，要么全不做。
- 失败注入只在测试里打开：export_test.go 给 memstore_test 一个打开开关的构造函数，生产代码里的 New 没有这条路。
- 还没做的方法返回未导出的 errNotImplemented，由任务 5、6 替换。

覆盖清单：
- 主机：SN 没规范化、是占位值、属性键或平台不合法，都报 ProblemsError，问题与 model 的校验相同；SN 重复报 DuplicateError，Existing 是那个 SN；Rev 对不上报 ErrConflict；把架构、固件、属性改成空值能存下；导入时新旧混合可以，一条不合法整批不写；批内 SN 重复报 host_duplicate_in_batch；导入每台写一条审计；GetHosts 只放存在的；ListHosts 翻三页以上，与一次全取的结果相同，两种过滤都对；改返回值里的 Attrs 不影响仓储。
- ISO：系不属于族报 ErrInvalid；生成的 ID 合规，State 为 acquiring；系统更新不要审计；改 Family、Series、Name 报 ErrInvalid；置为 ready 时 ISO.Validate 的问题原样带出；SHA256 撞上一个未失败的 ISO 报 DuplicateError，对方导入失败之后就能写；只有 ready 才能废弃；三种过滤都对；Artifacts、Offers 是深拷贝。
- 审计：每个要审计的方法，零值审计都报 ErrAuditRequired，什么都不写；审计不合法报 ErrInvalid；Actor 为 FaultActor 时报错，并且动作没有落库、审计也没有写；ListAudit 新的在前，各种过滤与翻页都对，条目的 TargetKind、TargetID 由仓储填对。

- [ ] **红：** 先写三组测试与 harness，内存实现只有空壳，运行 `go -C <克隆> test ./internal/store/... -count=1`，预期三组 FAIL。
- [ ] **绿：** 实现内存版的这三部分，预期 PASS。memstore_test 调用 RunHosts、RunISOs、RunAudit。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): 契约测试套件与内存实现：主机、ISO、审计`）。

**风险：** RunAll 现在只能列出已有的三组，任务 5、6 往里加；harness.go 会被这两个任务改动，并入时按机械合并处理。

### 任务 5：hook 与模板——契约测试与内存实现

**目标：** hook 与模板两组契约测试在内存实现上跑通：版本的生命周期、名字、内置内容、引用关系，以及审批时在事务里重跑激活判定。

**文件：**
- 创建：`internal/store/storetest/hooks.go`、`templates.go`；修改 `harness.go`，把两组加进 RunAll
- 创建：`internal/store/memstore/hooks.go`、`templates.go`；修改 `memstore_test.go`

**接口契约：** 照「仓储契约」一节的 Hooks 与 Templates 两张表，不新增对外接口。分组函数是 RunHooks 与 RunTemplates。

覆盖清单：
- 名字：按 NormalizeName 规范化；不合法报 ProblemsError；大小写不同也算同名，报 DuplicateError，Existing 是已有的身份 ID；改名要审计，内置的不能改；删掉一个身份的最后一个草稿之后，名字可以再用。
- 生命周期：每个状态转换都对，不合规矩的转换报 ErrConflict；只有草稿能保存，其余报 ErrImmutable；保存时 Name 与身份不同报 ErrInvalid；Rev 对不上报 ErrConflict；已有草稿或待审批的版本时再开新版本，报 ErrConflict；新版本复制来源的内容，版本号是下一个；说明或正文存不下报 text_not_storable。
- 审批：激活判定有问题时（比如 hook 没有实现，模板缺必需部件）报 ProblemsError(ErrRejected)，问题与直接调用 activation 得到的相同，状态不变；审批人与作者相同报 approver_is_author；模板审批前，它引用的 hook 版本刚被废弃，报 hook_ref_deprecated；Actor 为 FaultActor 时，审批不生效。
- 派生：复制来源的全部内容，DerivedFrom 记对；来源是什么状态都可以；新名字照样要唯一。
- 引用：模板的任一版本（包括草稿）引用了某个 hook 草稿，这个草稿就删不掉（ErrReferenced）；模板改了装配表、不再引用它之后，就能删了。
- 内置：不存在时写入，状态 approved，Builtin 为 true；内容相同再装一次什么都不做；内容不同报 ErrBuiltinMismatch；名字被用户的身份占了报 DuplicateError；内置的身份不能开新版本，内置的版本不能保存。
- 查询：GetHooks 只放存在的；ListHooks 与 ListTemplates 每个身份一项，ApprovedOnly 时给当前的已审批版本，与 model.CurrentApproved 一致；ListTemplates 按系过滤；Impls、Params、Bindings、Assembly、Series 都是深拷贝。

- [ ] **红：** 写两组测试，运行 `go -C <克隆> test ./internal/store/... -count=1`，预期两组 FAIL。
- [ ] **绿：** 实现，预期 PASS。memstore_test 加上 RunHooks、RunTemplates。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): 内存实现的 hook 与模板`）。

**风险：** 激活判定要用模板引用的 hook 版本。审批模板时，内存实现与 pgstore 都用 HookRefs 取引用、用 ApproveTemplateProblems 判定，不要各自另写一份。

### 任务 6：任务与事件——契约测试与内存实现

**目标：** 任务与事件两组契约测试在内存实现上跑通：批量建任务的原子性与并发兜底、条件推进、事件的序号与去重；内存实现从此实现了完整的契约。

**文件：**
- 创建：`internal/store/storetest/tasks.go`、`events.go`、`concurrency.go`；修改 `harness.go`、`hosts.go`、`isos.go`
- 创建：`internal/store/memstore/tasks.go`、`events.go`；修改 `memstore_test.go`，改成调用 RunAll

**接口契约：** 照「仓储契约」一节的 Tasks、Events 两张表与「批量建任务」。分组函数是 RunTasks、RunEvents、RunConcurrency。

覆盖清单：
- 建任务：成功时 ID 合规且是安全词，State 为 pending，Rev 为 1，读回来的快照与写进去的完全相同，每个任务一条审计；入参的每条前提各一个反例，都报 ErrInvalid；在事务里重跑准入：ISO 被废弃、模板版本不是已审批、模板引用了被废弃的 hook 版本、主机没登记、主机已有未结束的任务，各报对应的问题，都包在 BatchError 里，整批不写，也不写审计；Actor 为 FaultActor 时报错，一个任务都不落库。
- 引用：主机或 ISO 有任务引用之后，删除报 ErrReferenced（主机与 ISO 两组里留到这里的用例，加在 hosts.go、isos.go）。
- 推进：成功时 Rev 加 1；Rev 或期望状态不对报 ErrConflict；目标状态不合法报 ErrInvalid；事件不合法时整个推进不生效；事件和状态一起提交；失败的任务重试回 pending 时，如果这台主机已有别的未结束任务，报 host_has_open_task；TransitionTaskByUser 少了审计报 ErrAuditRequired，Actor 为 FaultActor 时状态不变、事件不写。
- 查询：FindOpenTask 在有、没有、任务刚结束三种情况下都对；ListTasks 按主机、状态、时间范围过滤，翻页的结果与一次全取相同。
- 事件：Seq 每个任务从 1 起连续，不同任务互不影响；同一个 Key 再追加，报告 Duplicate，Seq 是原来那条的，不新增；同一个 Key 在不同任务里可以各用一次；任务不存在报 ErrNotFound；ListEvents 的 afterSeq 与 limit 都对。
- 并发（RunConcurrency，用多个 goroutine 同时调用）：同一台主机的几批任务同时提交，恰好一批成功，其余报 host_has_open_task；同一个任务用同一个期望值同时推进，恰好一次成功；同一个任务同时追加事件，Seq 连续、不重、不缺；同一个 Key 同时追加，只落一条。

- [ ] **红：** 写三组测试，运行 `go -C <克隆> test ./internal/store/... -count=1 -race`，预期 FAIL。
- [ ] **绿：** 实现，去掉内存实现里全部的 errNotImplemented，memstore_test 改成 RunAll，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): 内存实现的任务与事件，契约测试全部分组`）。

**风险：** 并发用例在内存实现上靠一把锁就能过，真正考验的是任务 9 的 PostgreSQL 实现。所以并发用例要按「最终结果」断言（恰好一个成功、Seq 连续），不要依赖 goroutine 的调度顺序。

### 任务 7：PostgreSQL 实现的骨架，以及主机、ISO 与审计

**目标：** 搭起 pgstore：连接、AutoMigrate、表结构断言、数据库错误的翻译、测试夹具，并让主机、ISO、审计三组契约测试在 PostgreSQL 上跑通。

**文件：**
- 修改：`go.mod`、新增 `go.sum` —— 加 gorm.io/gorm v1.31.2 与 gorm.io/driver/postgres v1.6.3，版本写死
- 修改：`.gitignore` —— 加 `/.pgtest.dsn`
- 创建：`internal/store/pgstore/doc.go`（包说明，包括测试库怎么准备）、`open.go`、`schema.go`、`errors.go`
- 创建：`internal/store/pgstore/rows_registry.go`、`rows_audit.go`、`hosts.go`、`isos.go`、`audit.go`
- 创建：`internal/store/pgstore/pgstore_test.go`（夹具，跑契约测试）、`schema_test.go`

**接口契约：**

```go
func Open(ctx context.Context, dsn string) (*Store, error)
func (s *Store) Close() error
```

- Open：连库，对本层的全部行类型跑 AutoMigrate，再做表结构断言；断言不过就关闭连接并返回错误，错误逐条写出差异。GORM 的日志关掉（logger.Discard），切面日志是第 5 层的事。
- schema.go 里放「表结构」一节的期望值，Open 与 schema_test 共用这一份。
- errors.go 把数据库错误翻成契约的错误：23505（唯一约束）按约束名分成 DuplicateError，或者 host_has_open_task 的问题；23503（外键）在删除时翻成 ErrReferenced，在插入时翻成 ErrNotFound；23514（CHECK）翻成 ErrInvalid。认不出的约束名照原样返回，不要猜。
- 夹具：DSN 先读 LOBER_TEST_PG_DSN，没有再从模块根往上找 `.pgtest.dsn`，都没有就 t.Fatal 并写明怎么准备（不跳过）。每次 Open 新建一个 `lober_t_` 加随机串的 schema，在 DSN 里设好 search_path，用完删掉这个 schema。失败注入：在这个 schema 的审计表上建一个触发器，Actor 为 FaultActor 的插入一律抛错。

- [ ] **红：** 先写 schema_test：全新的 schema 断言通过；人为删掉一个索引、把 (task_id, seq) 那样的复合索引改建成单列、改掉一个 WHERE、去掉一个外键，各自都被断言查出来，并且报出那一项的名字（本任务只涉及主机、ISO、审计三张表，其余的表在任务 8、9 加）。再让 pgstore_test 调用 RunHosts、RunISOs、RunAudit。运行 `go -C <克隆> test ./internal/store/pgstore/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。还没做的方法返回 errNotImplemented。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): PostgreSQL 实现的骨架：建表、表结构断言、主机、ISO、审计`）。

**风险：**
- `go mod tidy` 之后检查 go.sum，确认没有带进计划外的模块；pgx 的版本由 gorm 的驱动决定，在提交说明里写明解析出来的是哪个版本。
- 每次 Open 都要建 schema 和跑 AutoMigrate，测试会慢一些。只要单个包在一分钟以内就不用优化；超过了，在报告里写明耗时。
- 本机的 PostgreSQL 跑在 Windows 上，Linux 上的实测由主会话在任务 10 之后做。

### 任务 8：PostgreSQL 实现的 hook 与模板

**目标：** hook 与模板两组契约测试在 PostgreSQL 上跑通。

**文件：**
- 创建：`internal/store/pgstore/rows_authoring.go`、`hooks.go`、`templates.go`
- 修改：`internal/store/pgstore/schema.go`、`schema_test.go`、`pgstore_test.go`

**接口契约：** 照「仓储契约」与「表结构」两节，不新增对外接口。

- 加锁：审批、废弃、保存、删除都先对版本行加写锁；审批模板时，再对它引用的 hook 版本加读锁，所以 hook 的废弃要等这次审批提交。
- 名字的 key 存在身份行上，由唯一索引兜底；用户建的身份撞上内置的名字，也是由它报出来。
- 保存模板的任一版本时，在同一个事务里重写这个版本在 template_hook_refs 里的行。

- [ ] **红：** schema_test 加上 hooks、hook_versions、templates、template_versions、template_hook_refs 五张表的断言，以及「部分唯一索引被改成全表唯一」这个反例；pgstore_test 加上 RunHooks、RunTemplates。运行 `go -C <克隆> test ./internal/store/pgstore/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): PostgreSQL 实现的 hook 与模板`）。

**风险：** jsonb 字段反序列化回 model 时，nil 与空切片、空 map 要和内存实现一致，否则同一个契约测试在两份实现上结果不同。读回来的值以「写进去什么就读出什么」为准；两边不一致时，修 pgstore，不改契约测试。

### 任务 9：PostgreSQL 实现的任务与事件

**目标：** 全部契约测试在 PostgreSQL 上跑通，包括并发分组；pgstore 从此实现了完整的契约。

**文件：**
- 创建：`internal/store/pgstore/rows_tasks.go`、`tasks.go`、`events.go`
- 修改：`internal/store/pgstore/schema.go`、`schema_test.go`、`pgstore_test.go`（改成调用 RunAll）

**接口契约：** 照「仓储契约」「批量建任务」「表结构」三节，不新增对外接口。

- 建任务的加锁顺序照「批量建任务」第 2 步；ISO、模板版本、引用的 hook 版本用读锁（FOR SHARE），主机用写锁（FOR UPDATE），主机按 SN 升序锁。
- 事件的 Seq 由 tasks 表上的计数列分配，追加事件时先锁这个任务行。

- [ ] **红：** schema_test 加上 tasks、task_events 两张表的断言，以及「C31 的部分唯一索引不见了」「(task_id, key) 的唯一索引变成单列」两个反例；pgstore_test 改成 RunAll。运行 `go -C <克隆> test ./internal/store/pgstore/ -count=1 -race`，预期 FAIL。
- [ ] **绿：** 实现，去掉 pgstore 里全部的 errNotImplemented，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(store): PostgreSQL 实现的任务与事件，契约测试全部分组`）。

**风险：** 并发用例在这里第一次真正受考验。用例失败时，先查是不是没加锁、加锁的顺序不一致，或者在事务外读了数据。不要靠重试或加大超时让它变绿。

### 任务 10：同步文档

**目标：** spec 与遗留问题清单反映本层定下的事。

**文件：**
- 修改：`docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`
- 修改：`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`

**改什么：**
- spec §4.2：写明本层仓储的范围，以及推迟到第 4、5 层的部分（ISO 的获取来源；用户、角色与会话）；写明审批与建任务时在事务里重跑第 1 层的判定作为兜底；写明名字放在身份上；写明已审批与已废弃的版本永不删除。
- spec §5.3 第 6 步：「每次提交记一条审计」改成「每个任务一条审计」。
- spec §6.2 第 8 条：补上审计按对象写，以及要审计的方法清单以存储契约为准。
- 清单的「修补轮之后新发现的问题」一节：C-1 到 C-10 标成已修，写上提交号。

- [ ] **红/绿：** 文档任务没有测试。改完后用 Grep 核对：spec 里不再有与本层决定相反的说法，清单里 C-1 到 C-10 都标了结果。
- [ ] **commit：** commit（`docs: 第 2 层落地后同步 spec 与遗留问题清单`）。

---

## 留给后续层的约定

- 第 3 层：内置 hook 与模板的名字，和用户建的共用一个名字空间。用户先用了某个名字，以后的版本又带了同名的内置内容，启动时 EnsureBuiltin 会报 DuplicateError、拒绝启动。写内置内容之前，先定一个只给内置内容用的名字前缀，并在名字规则里拦下用户用它。
- 第 4 层：ISO 的获取来源加新表；导入期间的状态与字段，只通过 UpdateISO 改。
- 第 5 层：用户、角色与会话加新表；把仓储的错误映射成 HTTP 状态码与说明，需要新的原因代码时照例追加；保存草稿前做的规范化（L-4b），在调用仓储之前完成。
- 第 7 层：token、引导次数、时机进度这些执行字段，加在 tasks 表上（AutoMigrate 加列是安全的），并扩展 TransitionTask，让它们和状态在同一个条件写入里提交（spec §6.2 第 4 条：token 和「进入引导中」一起写入）。事件的 Kind 与 Fields 由第 7 层定，仓储不解释它们。
- 任何一层要修改本层已有的索引或约束，都得自己写一步先删掉旧定义的升级动作：AutoMigrate 不会改。表结构断言会在启动时把没做这一步的库拦下来。

## 整体验收

- [ ] 在主目录（`feat/store`）跑 `make check`，五步全部通过，退出码为 0。PostgreSQL 的测试真的跑了，没有因为找不到 DSN 而失败或跳过。
- [ ] `go list ./...` 列出 internal/core 下的七个包、internal/archtest，以及 internal/store 下的四个包。
- [ ] 两份实现都调用 `storetest.RunAll`；`git grep errNotImplemented` 没有结果。
- [ ] 依赖守卫通过：存储层四个包的依赖符合任务 2 的三条规则；第三方模块只有 gorm 与 pgx 两家，只出现在 pgstore 里。
- [ ] 代码表共 70 个，每个都由某个非测试源文件实际产出。
- [ ] spec §4.2 列的六条约束，每条都能指出 RunAll 里的一个测试：SN 唯一、模板名唯一（C6、C12）；已审批的版本不可修改（C23）；被引用的对象不能删除（C26）；批量建任务原子提交（C30）；一台主机只有一个未结束的任务（C31）；状态推进不被并发写覆盖（C33）。逐条写出测试名。
- [ ] §7.2 第 2 条：写操作与审计在同一个事务里，FaultActor 用例在两份实现上都证明了「审计写失败时动作也不落库」；表结构断言能查出「表结构」一节列的各类偏差，逐条写出 schema_test 里的测试名。
- [ ] Linux 实测（主会话在工作流结束后做）：在 QEMU 的 Debian 13 虚拟机里装 postgresql，建好 lober_test 角色与库，写好 `.pgtest.dsn`，导入 HEAD 跑 `make check`，五步全部通过。

