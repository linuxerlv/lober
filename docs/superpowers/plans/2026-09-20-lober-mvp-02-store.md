# M2 SQLite 存储层 实现计划

> **数据层迁移说明**：本文是 SQLite 存储层的实现计划，数据层已于
> 2026-09-24 迁移到 GORM + PostgreSQL（见
> `docs/superpowers/specs/2026-09-24-gorm-postgres-migration-design.md`），
> 正文保留当时原貌，未作修改。

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** M1 契约与地基必须已完成。本模块与 M3–M8 并行，只通过 `internal/core/contract` 通信。
>
> **本计划描述做什么与为什么，不给代码。** SQL 语句、结构体定义、测试代码都由你在实现时写。

**目标：** 用 SQLite 实现 `contract.Store` 与 `contract.TxRunner`，通过 M1 的 conformance 套件。

**架构：** 一个类型持有数据库连接并实现全部七个窄接口。按接口族分文件，每个文件只管自己
那几张表。错误翻译集中在一处 —— SQLite 的约束冲突要变成 contract 的 sentinel error，
否则调用方只能比较字符串。

**验收标准已经存在：** M1 的 `conformance.RunStoreSuite`。本计划的第一个任务就是写那个
调用套件的测试（它会全红），之后每个任务让其中一部分变绿。**不要另写一套断言** ——
套件就是契约。

**技术栈：** `database/sql` + `modernc.org/sqlite`（纯 Go，无 CGO）· schema 与迁移由 M1 提供

**SPEC 依据：** §2 数据模型、§2.6 pin 语义、§2.7 引用完整性、§3.6 并发与幂等

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/store/sqlite/store.go` | Store 类型、构造、编译期接口断言 |
| `internal/store/sqlite/errors.go` | SQLite 约束冲突到 sentinel error 的翻译 |
| `internal/store/sqlite/scan.go` | 行扫描辅助：时间与 JSON 列的编解码 |
| `internal/store/sqlite/task.go` | TaskStore：CRUD、身份查找、CAS、超时扫描、token 查询 |
| `internal/store/sqlite/event.go` | EventStore：追加与去重、按序号查询 |
| `internal/store/sqlite/fact.go` | FactStore：合并写入与读取 |
| `internal/store/sqlite/host.go` | HostStore |
| `internal/store/sqlite/artifact.go` | ArtifactSetStore：状态更新与导入写回 |
| `internal/store/sqlite/hook.go` | HookStore：版本、digest 反查、审批 |
| `internal/store/sqlite/template.go` | TemplateStore：版本与 hook 引用 |
| `internal/store/sqlite/tx.go` | 事务运行，拒绝嵌套 |
| `internal/store/sqlite/store_test.go` | conformance 套件入口 |
| `internal/store/sqlite/concurrency_test.go` | 竞态与并发 CAS |

`migrate.go` 与 `schema/001_init.sql` 已由 M1 产出，**本模块不改它们**。
若发现 schema 缺列，作为契约变更上报，不要在本模块里加一条 `ALTER TABLE`。

---

### 任务 1：Store 骨架与 conformance 入口

**产出文件：** `store.go`、`store_test.go`

**为什么需要这一步：** 先让验收标准可执行且全红，之后每个任务的「变绿」都有客观依据。

**实现流程：**

1. Store 类型持有一个 `*sql.DB`；构造函数只做赋值，不做初始化（开库与迁移是调用方的事）
2. 文件末尾加**编译期接口断言** —— 先注释掉，等方法齐了再解开
3. 测试文件里写 conformance 套件的调用：工厂每次建一个临时目录下的新库、跑迁移、
   返回 Store，并用测试清理钩子关闭连接

**设计要点：**

- **工厂必须每次给全新的库。** 套件的子测试之间不能共享状态，否则「同一 SN 只能有一个
  非终态任务」这类断言会互相干扰。用测试临时目录而不是内存库 ——
  内存库在连接池下的行为与文件库不同，测出来的不算数。
- **测试里开库要走 M1 提供的打开函数**，而不是自己 `sql.Open` —— 那些 pragma
  （WAL、外键、忙等超时、连接池上限 1）是行为的一部分，绕过它们等于测了另一个东西。

**测试要锁住的行为：** 本任务只要求套件能编译并运行（全红是预期）。

**验证：** `go test ./internal/store/sqlite/ -run Conformance` —— 预期大量失败，
失败信息应是各个未实现方法的 panic 或 nil 返回，而不是编译错误。

**完成标志：** commit `feat(store): sqlite store skeleton with conformance entrypoint`

---

### 任务 2：错误翻译与行扫描辅助

**产出文件：** `errors.go`、`scan.go`

**为什么需要这一步：** 两件事会在每个接口实现里重复出现。不先做，后面十个文件会各写一份。

**错误翻译要覆盖三种情形：**

| SQLite 情形 | 翻译成 |
|---|---|
| 唯一约束冲突，索引是任务的 SN partial unique index | `ErrActiveTaskExists` |
| 唯一约束冲突，索引是事件的 `(task_id, seq)` | `ErrDuplicateSeq` |
| 查询无行（`sql.ErrNoRows`） | `ErrNotFound` |

**按索引名区分，不要按错误文本猜。** `modernc.org/sqlite` 的约束错误里带索引名，
用它精确匹配；靠字符串包含判断会在驱动升级或 schema 改索引名时静默失效 ——
而失效的表现是「重复信号被当成内部错误返回 500」，机器侧会重试三次然后放弃上报。

**行扫描辅助要处理三类列：**

- **时间列存 Unix 秒。** 零值时间与 SQL NULL 的互转要明确：任务的「上次信号时间」
  在没收到任何信号时应是 NULL 而不是 1970 年 —— 后者会让 §3.5 的「距今多久」
  算出五十多年，那条诊断信息就废了
- **JSON 列**：制品路径、参数、hook requires、params_schema。编码时 nil 与空值都
  写成合法 JSON（`{}` 或 `[]`），不要写空串 —— 解码空串会报错
- **可空文本列**：两个 token。空串与 NULL 要统一，否则 token 的唯一索引
  （条件是非空）行为会不一致

**设计要点：**

- **翻译函数接收原始错误与一个上下文标签**，返回翻译后的错误；无法识别时原样包装返回
  而不是吞掉 —— 未知的数据库错误应该冒泡成 500 并进服务端日志。

**测试要锁住的行为：**

- 三种情形各自翻译正确（用真实的约束冲突触发，不要手工构造错误值）
- **零值时间往返后仍是零值**，不变成 1970
- JSON 列的 nil、空、有内容三种情况往返一致

**验证：** `go test ./internal/store/sqlite/ -run 'TestTranslate|TestScan' -v` 全过。

**完成标志：** commit `feat(store): error translation and column codec helpers`

---

### 任务 3：任务的建、取、列与身份查找

**产出文件：** `task.go`（第一部分）

**为什么需要这一步：** 身份查找是引导路径的第一步，它的正确性直接决定「哪台机器会被
装机」。这是全系统最需要小心的查询。

**实现流程：**

1. **建任务**：一条 INSERT。撞 SN 的 partial unique index 时翻译成
   `ErrActiveTaskExists` —— 不要先 SELECT 再 INSERT，那有竞态窗口，让索引做判定
2. **取任务**：按主键
3. **列任务**：状态集合与 SN 两个可选过滤，加 limit/offset。**不传 limit 时用默认上限**
   （M1 契约定的 1000），不返回无界结果集
4. **按身份查 pending 任务**：三条独立查询依次尝试

**身份查找的三条硬约束：**

- **优先级是 SN → MAC → UUID**（§4.3）。SN 不总可靠（白牌机留空、填
  `To Be Filled By O.E.M.`、整批重复），所以要能回退，但顺序不能变 ——
  SN 是最强的标识，有它就不该用弱标识去猜
- **空键必须跳过，不能拿空串去查。** 否则任何不报 SN 的机器都会撞上第一个 SN 为空的
  任务，那台机器会被装上不属于它的系统。这是本模块最危险的一个 bug
- **只匹配 `pending` 状态。** installing 中的任务再来一次引导请求说明机器在装机途中
  重启了 —— 此时网关应回 `no_task`，任务由所在阶段的超时预算判失败（§3.3），
  不该重新签 token 走一遍

**设计要点：**

- **MAC 入库与查询前都走 M1 的归一化函数。** 库里存归一化形式，查询也归一化 ——
  两边不一致会让 §4.3 的双因子校验假阴性，表现是「建了任务但机器一直装不上」。
- **列任务的排序要稳定**（按创建时间倒序，同值时按 id），否则分页会漏行或重复。

**测试要锁住的行为：** 由 conformance 套件的这几个子测试定义 ——
`OneActiveTaskPerSN`、`IdentityFallback`（含空 SN 不匹配空 SN 任务那条）、
`PendingOnlyLookup`。本任务不另写断言。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/(OneActiveTask|Identity|PendingOnly)' -v` 全过。

**完成标志：** commit `feat(store): task creation, listing and identity lookup with fallback`

---

### 任务 4：CAS 状态转换与运行时更新

**产出文件：** `task.go`（第二部分）

**为什么需要这一步：** 所有状态转换都走这一个方法（§3.6）。它是并发保护的全部内容 ——
扫描循环与信号请求会同时想改同一个任务。

**实现流程：**

1. **CAS**：一条 UPDATE，WHERE 子句带三个条件（主键、期望状态、期望版本），
   SET 里版本自增。**用受影响行数为 0 判定冲突**并返回 `ErrConflict`
2. 成功后重新 SELECT 返回更新后的任务 —— 调用方需要新版本号做下一次 CAS
3. **运行时更新**：只改不涉及状态转换的字段
4. 两个方法都从「可选字段更新集」构造 SET 子句：只为非 nil 的字段生成赋值

**两条不能妥协的约束：**

- **受影响行数为 0 必须返回 `ErrConflict`，不能返回 nil。** 静默失败的后果是调用方
  以为转换成功了 —— 状态机会据此继续推进，而库里的状态没变。
- **运行时更新携带状态时必须报错。** 留一条不走 CAS 的改状态路径，迟早有人用它绕过
  「禁止重装」的判定（§3.4）。这条是 conformance 套件断言的。

**设计要点：**

- **动态 SET 子句要用参数占位符，绝不字符串拼接值。** 参数来自 HTTP 请求
  （token、失败原因文本），拼接就是注入。
- **只有状态真的变了才更新「状态变更时间」**，否则那个字段会被每次信号刷新，
  失去「这个状态持续多久」的含义。
- **区分「没找到任务」与「冲突」**：前者返回 `ErrNotFound`。实现上要么先查一次，
  要么在 0 行时查一次确认存在性 —— 后者少一次查询但两次查询之间有窗口，
  对这个场景无害（都返回错误，只是错误类型可能不精确），选简单的。

**测试要锁住的行为：** conformance 套件的 `TaskCAS` 与 `RuntimeUpdateRejectsStateChange`。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/(TaskCAS|RuntimeUpdate)' -v` 全过。

**完成标志：** commit `feat(store): CAS state transitions and runtime field updates`

---

### 任务 5：超时扫描与 token 查询

**产出文件：** `task.go`（第三部分）

**为什么需要这一步：** 超时扫描是「机器卡住了」的唯一发现途径；token 查询是三个机器面
端点的认证入口。

**超时扫描的实现方式，两个选项，选一个并在注释里说明理由：**

| 方式 | 取舍 |
|---|---|
| SQL 里用 CASE 表达式按阶段算预算 | 一次查询出结果，但预算值在 SQL 与 Go 里各存一份，会漂移 |
| **取出非终态任务后在 Go 里用 M1 的预算表过滤**（推荐） | 多取一些行，但预算只有一个来源。任务表是百级，这个代价可忽略 |

推荐第二个。**预算值必须只有一个来源** —— M1 的预算表已经有测试锁住「pending 无预算」，
在 SQL 里复制一份会绕过那道保护。

无论哪种方式，**必须排除无预算的阶段**（`pending`），且要排除终态任务。

**token 查询的四条要求：**

- 以用途为参数：装机 token 查一个列，finalize token 查另一个列
- **过期即视为不存在**：比较对应的过期时间列与传入的当前时间
- **不存在、过期、用途不符三种情况统一返回 `ErrNotFound`** —— 不泄露「token 存在但
  过期了」，这个区别对攻击者有用、对正常客户端无用
- 空 token 直接返回 `ErrNotFound`，不要拿空串去查（两个唯一索引的条件是非空，
  但空串未必是 NULL）

**设计要点：**

- **两种 token 各有独立的过期时间列**（M1 schema 已有）。finalize token 活得比装机
  token 长是 §6.4 的全部意义 —— 装机用了 70 分钟、重启又停在 BIOS 数分钟时，
  `booted` 信号发出时装机 token 可能刚好过期。

**测试要锁住的行为：** conformance 套件的 `TokenLookup`（四种情形加 finalize 的
长短对比）与 `TimeoutScanSkipsPending`。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/(TokenLookup|TimeoutScan)' -v` 全过。

**完成标志：** commit `feat(store): timeout scan query and purpose-scoped token lookup`

---

### 任务 6：事件追加与去重

**产出文件：** `event.go`

**为什么需要这一步：** `(task_id, seq)` 唯一索引是信号去重的唯一依据（§2.5、§3.6）。
机器侧的 `signal()` 会重试三次（§6.3），所以重复投递是常态而非异常。

**实现流程：**

1. **序号为正**：直接 INSERT，撞唯一索引时翻译成 `ErrDuplicateSeq`
2. **序号非正**（非信号来源：扫描循环、人工操作、provider）：分配一个不与信号序号
   冲突的值。**用递减的负数** —— 与正序号天然不冲突，且同一任务内单调，排序仍然正确
3. **查询**：按任务过滤，支持「序号大于某值」以供 SSE 续传（§12.8），
   按插入顺序（自增主键）排序而非按序号 —— 负序号的事件也要出现在正确的时间位置

**设计要点：**

- **负序号的分配要在同一事务里取当前最小值再减一**，否则并发写两条非信号事件会撞。
  或者用一个独立的自增序列列 —— 两种都行，但要想清楚并发情形。
- **`ErrDuplicateSeq` 不是错误路径。** 在本模块它是一个返回值，但要在注释里写明：
  handler 收到它应回成功（M5 的契约要求），不要让它变成 500。
- **日志正文不进这张表**（§2.5）：本方法只写路径与尾部片段。正文由 `LogStore` 落文件。

**测试要锁住的行为：** conformance 套件的 `EventDedup`（含非信号事件可连续写两条）
与 `EventsAfterSeq`。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/Event' -v` 全过。

**完成标志：** commit `feat(store): event append with signal dedup`

---

### 任务 7：facts、主机与模板

**产出文件：** `fact.go`、`host.go`、`template.go`

**为什么需要这一步：** 三个相对简单的接口族，放在一个任务里。

**facts 的关键点：写入是合并而非替换**（§5.5）。同键覆盖、不同键保留 ——
`%pre` 的 hook 写入 RAID 型号，`%post` 的 hook 再写入选中的盘，两者都要在。
用 UPSERT（`ON CONFLICT DO UPDATE`）逐键写，不要先删后插。

**主机的关键点：**

- **按身份查主机与任务查询用同一套回退优先级**，保证「建任务时选的主机」与
  「引导时匹配的主机」判据一致。可以把回退逻辑抽成一个共用函数
- **MAC 归一化后入库**
- **`arch` 与 `firmware` 是必填列**（§4.3 末条），空值应在写入时报错而不是存进去 ——
  存进去的后果是建任务时过滤不出可装的 OS，而那个错误要等机器引导时才被 §4.6 拦下

**模板的关键点：**

- **取 pin 版本时要把 hook 引用一起取回来**（它们在单独一张表里，按模板 id 加版本关联）。
  §2.6 的可复现性要求还原出的是那一版的完整内容
- **版本号传非正数表示取最新** —— 取 `MAX(version)` 那一行
- **删除时若存在非终态任务引用该模板，返回 `ErrStillReferenced`**（§2.7）。
  判定看任务表而不是看模板状态

**设计要点：**

- **hook 引用的排序交给 model 的方法，不在 SQL 里 ORDER BY。** M1 已经把「按 order
  升序」做成模板类型的方法，SQL 里再排一次会出现两个来源。取回来放进切片即可。

**测试要锁住的行为：** conformance 套件的 `FactsUpsert` 与 `TemplatePinnedVersion`。
主机部分由 `IdentityFallback` 间接覆盖。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/(Facts|Template)' -v` 全过。

**完成标志：** commit `feat(store): facts upsert, host lookup and pinned template versions`

---

### 任务 8：hook 版本与 digest 反查

**产出文件：** `hook.go`

**为什么需要这一步：** §2.7 的防偷换机制：hook 正文不可变，改正文即新版本新 digest，
模板 pin 的是 digest。所以**按 digest 反查正文**是渲染路径的必需查询。

**实现流程：**

1. **建 hook**：主键是 (id, version)。digest 列有唯一索引 —— 同一正文重复注册应被拒绝
   还是复用，选一个：**建议拒绝**（返回错误），因为「两个 hook 记录指向同一正文」
   会让审批状态产生歧义（一个 active 一个 draft 时该信哪个？）
2. **按 id 取**：返回最高版本
3. **按 digest 取**：唯一索引直接命中
4. **列版本**：按版本升序
5. **审批**：改状态、审批人、审批时间三列

**设计要点：**

- **digest 由 M1 的函数从正文算出，写入时要校验传入的 digest 与算出的一致。**
  不校验的后果是库里的 digest 与正文对不上，而机器侧的 `sha256sum -c` 会失败 ——
  那个故障的现场是「所有 hook 都执行失败」，从机器侧日志很难追溯到库里的数据错了。
- **`os_families` 存成逗号分隔的文本列**（schema 已定），编解码时空串对应空切片
  （表示不限家族），不要变成含一个空字符串的切片。

**测试要锁住的行为：** conformance 套件的 `HookByDigest`（含「改一个字节即查不到」）。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/HookByDigest' -v` 全过。

**完成标志：** commit `feat(store): hook versions and content-addressed lookup`

---

### 任务 9：制品集与导入写回

**产出文件：** `artifact.go`

**为什么需要这一步：** 导入是异步 job，登记时只知道名字与本地路径，家族与路径要等识别与
抽取完成后才知道（§2.1）。

**两个写方法，各改各的列，互不覆盖：**

| 方法 | 改哪些列 |
|---|---|
| 状态更新 | state、manifest sha256、导入错误原因 |
| 导入写回 | os_family、paths JSON |

**分开的理由要写进注释：导入失败时只改状态、不该碰已写入的 Paths。** 合成一个大方法的
后果是失败路径必须传空 Paths，于是已经抽取好的路径信息被清掉 —— 而运维想看的恰恰是
「抽到哪一步失败的」。这条是 conformance 套件断言的。

**删除的关键点：被 active 状态的模板引用时返回 `ErrStillReferenced`**（§2.7）。
判定要看模板状态 —— deprecated 的模板引用它不阻止删除。

**设计要点：**

- **paths JSON 的往返要覆盖切片与 map**：initrd 是切片（Windows 有多个），
  extra 是 map（ESXi 与 Windows 的特殊文件）。这两类最容易在序列化时掉字段，
  而掉字段的表现是引导菜单少一行 initrd —— 机器加载内核后立刻 panic。

**测试要锁住的行为：** conformance 套件的 `ArtifactImportWriteback`
（含「导入失败不清空 Paths」与切片、map 的往返）。

**验证：** `go test ./internal/store/sqlite/ -run 'Conformance/ArtifactImport' -v` 全过。

**完成标志：** commit `feat(store): artifact set state and import writeback`

---

### 任务 10：事务与接口断言

**产出文件：** `tx.go`、`store.go`（解开断言）

**为什么需要这一步：** 建任务时要同时写任务与首条事件，两者必须原子。另外要确认
全部接口方法都已实现。

**事务的实现流程：**

1. 开事务，构造一个「事务内的 Store」交给回调
2. 回调返回 nil 则提交，返回错误则回滚并原样返回该错误
3. **拒绝嵌套调用** —— 检测到已在事务中时返回错误

**为什么必须拒绝嵌套：** SQLite 是单写者，且 M1 把连接池上限设为 1。嵌套开事务会
等待自己持有的连接释放 —— **死锁，且表现为整个进程卡住无响应**，比报错难查得多。

**实现方式：** 事务内的 Store 是同一个类型但持有 `*sql.Tx` 而非 `*sql.DB`。
让两者都满足一个「能执行查询」的小接口，各方法针对那个接口编程 ——
这样不必为事务版本复制一遍所有方法。

**接口断言：** 解开任务 1 里注释掉的两行编译期断言。**断言放在文件末尾而不是随手写在
某个函数里** —— 它的作用是「接口有新方法时这里先报错」，位置固定才容易找到。

**测试要锁住的行为：**

- 回调返回错误时**两张表都没有新行**（不是只回滚了一张）
- **嵌套调用返回错误而不是卡住** —— 这条测试要设超时，否则测试本身会挂死

**验证：** `go build ./...`（断言成立）；`go test ./internal/store/sqlite/ -run TestTx -v` 全过。

**完成标志：** commit `feat(store): transaction runner rejecting nesting, assert interfaces`

---

### 任务 11：conformance 全绿与并发验证

**产出文件：** `concurrency_test.go`

**为什么需要这一步：** 套件全绿说明功能正确；并发测试说明它在真实使用下正确。
状态机的扫描循环与信号请求是并发的，没有竞态检测的通过不算数。

**三个并发场景：**

1. **多个 goroutine 同时 CAS 同一任务的同一转换** —— 恰好一个成功，其余全部得到
   `ErrConflict`。不是「大部分成功」，是精确的一个
2. **多个 goroutine 同时写同一任务的不同事件序号** —— 全部成功，事件数等于 goroutine 数
3. **多个 goroutine 同时用同一序号写事件** —— 恰好一个成功，其余得到 `ErrDuplicateSeq`

**设计要点：**

- **用 `WaitGroup` 而非 sleep 同步**，并用一个屏障让所有 goroutine 尽量同时发起 ——
  否则它们会自然串行，测不出竞态。
- **统计成功与各类错误的次数，断言精确值。** 断言「至少一个成功」是没用的 ——
  那在有 bug 时也成立。
- **SQLite 在连接池上限 1 下会串行化写**，所以这些测试更多是验证错误翻译在竞争下
  仍然正确，而不是验证锁。但它们能抓到一类真实 bug：把 `SQLITE_BUSY`
  误翻译成 `ErrConflict`，那会让调用方以为是版本冲突而重读重试，
  掩盖真正的连接池配置问题。

**测试要锁住的行为：** 上面三个场景的精确计数。

**验证：**

- `go test ./internal/store/sqlite/ -run Conformance -v` —— **套件全部子测试通过**
- `go test ./internal/store/sqlite/ -race -v` —— 全部通过且无竞态报告
- `make check` 全绿

**完成标志：** commit `test(store): concurrent CAS and event dedup under race detector`

---

## 交付验收

- [ ] conformance 套件的全部子测试在 SQLite 实现上通过
- [ ] `go test ./internal/store/sqlite/ -race` 无竞态报告
- [ ] 编译期接口断言已解开注释并通过
- [ ] `internal/store/sqlite` 只 import `model`、`contract` 与标准库加驱动
      （由 `archtest` 保证）
- [ ] 未修改 M1 产出的 schema 与迁移文件
- [ ] 三种 SQLite 约束冲突各有对应的 sentinel error 翻译，按索引名匹配而非错误文本

## 如果发现契约缺口

不要在本模块加变通。停下，按 M1 的契约变更流程处理：改接口 → 改 conformance 套件 →
改内存实现 → 通知并行模块。**特别是不要加 `ALTER TABLE`** —— schema 是数据契约，
改它会让内存实现与 SQLite 实现的行为分叉，而 conformance 套件是发现不了这种分叉的
（它测的是行为，不是存储形态）。

