# 第 2 层修补计划

执行者是代理。问题、影响、出处都以遗留问题清单为准（`docs/superpowers/reviews/2026-10-07-layer2-open-issues.md`），本计划只写决定和契约。清单里各条「出处」的行号指 b61ba0a，修补后会变。

**目标：** 落实 2026-10-08 的 6 条决定，修掉清单第 2 节 25 条（28 条里有 3 条理由随 memstore 删除而消失），并把第 4、5、7 层要用的 PostgreSQL 测试夹具抽成导出包。

**架构：** 存储层只剩一份实现 pgstore。契约包 internal/store 不变：接口、共用类型、共用纯函数都留着，它仍是第 4、5、7 层唯一依赖的包。storetest 的 4770 行并进 pgstore 的测试，换来对 last_seq、版本计数、template_hook_refs 的白盒断言。新增 pgstore/pgtest 导出测试夹具。

**技术栈：** Go、GORM、pgx v5、PostgreSQL 17。测试连测试环境的真实库，不跳过。

**依据：** spec `docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`；第 2 层原计划 `docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md`（原计划不改正文，只在任务 4–6 前加一条历史横幅，并在「已定的决定」表里追加本轮决定）。

## 用户的决定

| 条目 | 决定 | 落在 |
|---|---|---|
| 测试与环境 | 测试和生产都用 PostgreSQL，分成两个环境 | U1（spec §6.4）、U2 |
| memstore | 删掉，只留 pgstore；第 4、5、7 层的测试也连 PostgreSQL | U2 |
| storetest | 并进 pgstore 的 _test.go | U3 |
| 测试夹具 | 这轮就抽成导出包，供第 4、5、7 层用 | U4 |
| S10-2 | 选 A：保留前缀的判定放第 1 层 model，加原因代码 `name_reserved`；前缀定为 `lober:` | U5 |
| S4-2 | 选 B：契约用例并行，用信号量限并发 | U6 |
| L-8b | 不加退役状态。一台主机可以先后有多个安装任务，这就是装机历史 | U1（spec 补记录） |
| S10-3 | 选 A：hook 不能派生。换 hook 时 b 不存在就先建，再原位替换 | U1（spec 改条款） |
| 第 2 节 28 条 | 全部同意修 | U7–U12 |

前缀选 `lober:` 的理由：和正文里系统部件的命名空间 `{{lober:syslog}}` 是同一个词，用户一看就知道是系统给的；用户起名习惯是 `Rocky-Base` 这类，几乎不会撞。它占 6 个字符，内置名字还剩 58 个。不选 `builtin-`，因为用户复制内置模板时很可能顺手写出 `builtin-xxx`，而且它和内置 ID `builtin-kickstart` 是同一个写法。

## 清单第 2 节的重判结果

28 条在「删掉 memstore」这个新前提下逐条重判过。3 条理由消失，不修：

| 条目 | 为什么不修 |
|---|---|
| S6-3 | 「某份实现调了顺序，测试和生产给出不同状态码」——只剩一份实现，不存在两边不同 |
| S5-6 | 同上，读取路径只有一条 |
| S6-4 | 深拷贝用例在 pgstore 上永远是绿的：改 Go 里的值碰不到库里的行 |

另有 S6-5（memstore 的 cloneTask 是手写深拷贝）属第 3 节，随 memstore 删除整条作废。

其余 25 条都要修。14 条带「（两份实现不一致）」标记的条目里，13 条去掉标记（讲的是 pgstore 自己的缺陷），S9-1 的标记改写为「（pgstore 与 model 可能不一致）」。另有 5 条要改写理由或建议：S4-3、S4-4、S7-6、S5-5、S5-4 原来的论据是「两份实现会走偏」，重写成 pgstore 单独也需要这条约束。

## 并行执行

U1、U2 先做，其余都依赖它们。

- 第一批（可并行）：U1（spec 与计划的文档）、U2（删 memstore）
- 第二批（U2 之后，可并行）：U3（storetest 并包）、U5（保留前缀）
- 第三批（U3 之后，可并行）：U4（夹具包）、U6（并行化）、U7（pgstore 的事务与错误翻译）、U8（pgstore 的分支与索引覆盖）
- 第四批（可并行）：U9（契约注释）、U10（model 的状态清单）、U11（第 1 层的测试缺口）、U12（文档同步）

U7–U11 各自只碰自己的文件，互不重叠。

## 新增与删除

| 包或文件 | 动作 | 允许依赖 |
|---|---|---|
| `internal/store/memstore` | 删除（10 个文件、1948 行） | — |
| `internal/store/storetest` | 删除，内容并进 pgstore 的 _test.go | — |
| `internal/store/pgstore/pgtest` | 新建：导出测试夹具 | 标准库、pgx、`internal/store`、`internal/store/pgstore` |
| spec §6.4 | 新建一节「测试环境与生产环境」 | — |

## 任务 U1：改 spec 与原计划

**目标：** 把 6 条决定落进 spec，补一节环境划分，给原计划加历史横幅。（决定全部，以及 S10-2、S10-3、S5-1、L-8b 的文档部分）

**文件：**
- 修改：`docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`
- 修改：`docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md`

**契约：**

删掉内存实现，要改四处。§4.2（第 224 行）、§6.1（第 316 行）、§7.2 第 2 条（第 364 行）都写着「两份实现跑同一套契约测试」，改成只有 PostgreSQL 一份、测试连测试环境的真实库、找不到连接串就失败不跳过。§6.3（第 346 行）那处是拿「内存版仓储得能模拟回滚」当第二条理由，来论证事务不做成切面；这条理由随内存实现消失，但**结论不动**。换一条照样成立的理由：事务放进 ctx 传，加锁的先后会散到各个调用方，仓储在一个操作里定死的加锁顺序就守不住（L-6 正是实例），两条调用链按不同顺序锁同一批行会互相等死。另外两条理由（副作用先于提交执行、漏用 ctx 里的事务不会报错）保留。

§7.1 第 356 行的「存储契约测试」和 §7.2 第 5 条的「HTTP 测试」都要注明连测试环境的 PostgreSQL。

新增 §6.4「测试环境与生产环境」，放在 §6.3 之后——放进 §7 会让 §7.2、§7.4 重新编号，而计划、清单和代码注释引的就是这两个号。要写四件事：两个环境跑同一份代码（同一套 AutoMigrate 和表结构断言，区别只在连哪个库）；测试环境（非超级用户的专用角色与库 `lober_test`，每个用例一个临时 schema，连接串只从 `LOBER_TEST_PG_DSN` 或模块根的 `.pgtest.dsn` 读，两处都没有就失败）；生产环境（只定到「由装配点读配置」这一层，装配点第 5 层才出现，模块自己不读环境变量和文件，所以配错一个变量不会让生产连到测试库）；两边共同的约定（写事务固定 READ COMMITTED 由 pgstore 指定、同一个 PostgreSQL 大版本，因为表结构断言比对的是规范化后的定义文本）。

S10-2 要改两处。C12（第 173 行）在「大小写不同的算同名」之后补：以 `lober:` 开头的名字（比较不分大小写）留给内置内容，用户用了就拒绝；内置内容的名字必须带这个前缀。spec 里不写原因代码的名字。§6.2 第 11 条（第 330 行）把「先定一个前缀」改写成已经定了 `lober:`，并写明拦截在第 1 层、内置装载反过来要求必须带前缀，同时并入 S5-1 那一句：名字随身份定下，以后的版本不能换名，要改名只能换新 ID。

S10-3 要改三处。§2.8（第 142 行）写明只有模板能派生，hook 不能；换 hook 时先看 b 在不在，不在就新建，再按 §2.5 的「用 b 覆盖 a」原位替换。§5.2 第 284 行的「hook 的流程相同」不准确——hook 没有正文、装配和预览这三步，要改写，并说明换 hook 是在第 3 步装配时做。C24（第 191 行）开头加「只有模板能派生，hook 不能」。**C25 不动**：清单的 S10-3 决定段说 C25 含「hook……要改就先派生」，这一点不对，C25 原文里没有「派生」二字，引起误读的是 §2.8 和 §5.2，清单那句要一并更正（随 U12）。

L-8b 只补记录，不改模型。§2.7 补一句：一台主机同一时刻最多一个未结束的任务（C31），上一个结束后可以再建，按主机列出的任务就是它的装机历史；失败后重试沿用同一个任务。§8 风险表加两行，都标「接受（2026-10-08）」：主机没有废弃状态、被任务引用过就删不掉；被任务引用过的 ISO 删不掉，原始字节一直占磁盘（第 4 层设计导入与清理时处理）。§5.1 第 5 步（第 274 行）补一句指向后者。C26 可选小改，写明主机没有废弃状态，免得读者以为能废弃。

原计划不改正文。只做两件事：「已定的决定」表（第 37–48 行）追加本轮决定，由谁定写「用户，2026-10-08」；任务 4–6（第 322–409 行）前面加一条横幅，说明 memstore 已删除、这几个任务只作历史记录。

**步骤：**
- [ ] 改 spec 与原计划，逐处核对行号（spec 可能因新增 §6.4 而移位，改完要复查所有交叉引用）
- [ ] commit：`docs(spec): 只留 PostgreSQL 一份实现，补环境划分与四条拍板`

**风险：** spec 里 §7.2 第 2 条、§7.4 被计划、清单和 `storetest/harness.go:1` 引用。新增小节务必放在 §6.4，不要插进 §7。

## 任务 U2：删掉 memstore

**目标：** 删除内存实现，清掉守卫规则和注释里「两份实现」的说法。（决定）

**文件：**
- 删除：`internal/store/memstore/`（10 个文件）
- 修改：`internal/archtest/deps_test.go`、`internal/archtest/guard_test.go`
- 修改：`internal/store/doc.go`、`check.go`、`check_authoring.go`、`check_registry.go`、`store.go` 及三个 `*_test.go` 的注释
- 修改：`internal/store/pgstore/doc.go`、`open.go`、`pgstore_test.go` 的注释

**契约：**

生产代码里没有任何地方 import memstore，所以删目录不会让任何包编不过。守卫那边要同步：删掉 `pkgMemstore`、`ruleMemstore` 和 `storeRule` 里对应的 case，`ruleContract` 改成「不依赖 pgstore」，`rulePgstore` 去掉「memstore 与」，「四个包」「三条规则」改成「三个包」「两条规则」。**常量和引用它的用例必须在同一个提交里删**，否则 archtest 编不过。约束不会丢：以后真有人建了 `internal/store/memstore`，它会按 `ruleContract` 查，放行的集合和原来的 `ruleMemstore` 完全相同。

`guard_test.go` 里 7 个用例拿 memstore 当输入，改用 pgstore 或 storetest；`Test存储层总检查接上了每一项` 的夹具也拿 memstore 当「带 cgo 文件和被排除文件的包」，换成 pgstore（这件事和 S2-11 是同一处，放在 U8 一起做）。

要接住 memstore 独有测试的覆盖：`Test游标藏着存不下的串报ErrInvalid` 覆盖五个列表，而 pgstore 这边只测了主机和 ISO 两个。给 hook、模板、任务三个列表补上带 NUL 的游标。

注释里凡是用「与内存实现一致」来定义行为的，都要换成契约自己的说法，否则这些行为就没了出处：存不下的查找键与过滤条件当作没有、列表按字节序排、并发撞车只给成功或 DuplicateError、推进回滚后 Seq 仍连续、开新版本失败不跳号、哪些状态算已结束。这几条分别对应 S4-3、S4-6 ③、S7-6、S8-1、S6-1、S5-4、S9-1，由各自的任务补进契约文字，本任务只负责清掉旧说法。

注意不要误改：第 1 层和 activation、model 里的「两份实现」指的是一个 hook 的两份实现，不是存储实现。

**步骤：**
- [ ] **红：** 先改 archtest（删常量、规则和用例），`go test ./internal/archtest/` 应 FAIL，因为还引用着 memstore
- [ ] **绿：** 删目录，改注释，补三个列表的 NUL 游标用例，`make check` 应 PASS
- [ ] **重构 + commit：** `refactor(store)!: 删掉内存实现，只留 PostgreSQL`

**风险：** 只删目录不改守卫，守卫照样是绿的，`ruleMemstore` 会变成一条没人发现的死规则。

## 任务 U3：storetest 并进 pgstore

**目标：** 把契约测试包并进 pgstore 的测试，换来白盒断言能力。（决定）

**文件：**
- 删除：`internal/store/storetest/`（并入下列文件）
- 创建：`internal/store/pgstore/contract_*_test.go`（按聚合分文件）
- 修改：`internal/store/pgstore/pgstore_test.go`、`internal/archtest/deps_test.go`

**契约：**

storetest 现在只对外暴露 `Harness` 接口、`FaultActor` 常量、`RunAll` 和 8 个 `RunXxx`，只有 pgstore 一处调用 `RunAll`。并包之后：

- 测试文件属于 `package pgstore`，可以直接用未导出的 `s.db`。`Harness` 接口和 `RunAll` 不再需要，按聚合分文件、由 `go test` 直接发现。`RunXxx` 的导出收回。
- 原来黑盒看不到的三处补上白盒断言：`last_seq`（S6-1：推进被回滚后 Seq 仍连续）、版本计数（S5-4：开新版本失败后不跳号）、`template_hook_refs`（S5-8：两条写入路径都记了引用）。这三条清单里原本只能提议「追加一条事件再断言」这类间接做法，现在可以直接查列。
- archtest 的 `pkgStoretest` 和相关规则删掉。

**步骤：**
- [ ] **红：** 按聚合搬文件，先不改断言，`go test ./internal/store/pgstore/` 应 PASS（纯搬家，行为不变）
- [ ] **绿：** 给 S6-1、S5-4、S5-8 加白盒断言，应 PASS
- [ ] **重构 + commit：** `refactor(store)!: storetest 并进 pgstore，补三处白盒断言`

**风险：** 4770 行搬家，容易漏文件或漏用例。搬完要核对子测试总数：原来 250 个叶子子测试，加上新增的断言，不应变少。

## 任务 U4：抽出测试夹具包 pgtest

**目标：** 把 pgstore 测试里的夹具抽成导出包，供第 4、5、7 层用。顺带修 S7-7。（决定、S7-7）

**文件：**
- 创建：`internal/store/pgstore/pgtest/pgtest.go`
- 修改：`internal/store/pgstore/open.go`、`internal/store/pgstore/pgstore_test.go`、`internal/archtest/deps_test.go`

**契约：**

```go
package pgtest

// DSN 按 spec §6.4 读测试连接串：先 LOBER_TEST_PG_DSN，再从模块根逐级往上找
// .pgtest.dsn（并行克隆没有这个文件，靠往上找读到主目录那一份）。
// 两处都没有就 t.Fatal，不跳过。
func DSN(t testing.TB) string

// Open 新建一个临时 schema、跑 AutoMigrate 与表结构断言、装上失败注入，
// 返回连到这个 schema 的仓储。t.Cleanup 时连 schema 一起删掉。
// 并发数由包内的信号量限住，见 spec §6.4。
func Open(t testing.TB) store.Store

// FaultActor 是失败注入的触发条件：审计的 Actor 等于它时，审计写入失败。
const FaultActor = "..."
```

`Open` 要用到 pgstore 未导出的 `s.db` 装触发器和设连接池上限，所以 pgstore 要开一个导出入口。连接池这一半正是 S7-7：`Open` 现在只收 ctx 和 dsn，注释却说连接池「由装配点按部署调整」，装配点其实调不了。改法是给 pgstore.Open 加可选参数（或一个设置方法），让装配点能从配置传入连接池上限；注释同步改成当前由装配点传入。

守卫要为 pgtest 单加一条规则：它要 import pgx 和 pgstore，按现在的 `storeRule` 会被当成契约包而报违规；S2-12 打算加的「只有 pgstore 能用 gorm 和 pgx」也要给它放行。

**步骤：**
- [ ] **红：** 写 archtest 对 pgtest 的规则用例，应 FAIL（包还不存在）
- [ ] **绿：** 建包、改 pgstore.Open、pgstore_test 改用 pgtest，应 PASS
- [ ] **重构 + commit：** `feat(store): 导出 PostgreSQL 测试夹具 pgtest，Open 可配连接池`

**风险：** 夹具留在包内的 _test.go 里不解决，第 4 层一开工就只能复制一份，又回到「同一条规则写两遍」的老路。

## 任务 U5：保留前缀 lober:（S10-2 选 A）

**目标：** 第 1 层加保留前缀判定和原因代码，用户路径拦下，内置装载反过来要求必带前缀。（S10-2、S5-1、S3-14）

**文件：**
- 修改：`internal/core/model/ident.go`（或名字规则所在处）、`internal/core/model/codes.go`、`internal/core/model/problem.go`
- 修改：`internal/store/check_authoring.go`、`internal/store/pgstore/hooks.go`、`internal/store/pgstore/templates.go`
- 修改：pgstore 契约测试里的内置名字

**契约：**

```go
package model

// ReservedNamePrefix 是只给内置内容用的名字前缀（spec C12）。
const ReservedNamePrefix = "lober:"

// NameReserved 判断规范化后的 key 是不是用了保留前缀。
// 比较按 key（小写）做，所以 LOBER:x 也算；lober-base 这类不算。
func NameReserved(key string) bool
```

新增原因代码 `name_reserved`。规范化拆成两个入口：用户路径（CheckNewHook、CheckNewTemplate、DeriveTemplate、RenameHook、RenameTemplate）用了前缀就报 `ProblemsError(ErrInvalid)`，内置装载（checkBuiltin）反过来要求必须带前缀，不带就拒绝启动。判定只有第 1 层一份，第 5 层做预检时报的是同一个原因（spec §4.2）。

S5-1 的注释部分一起做：`ErrBuiltinMismatch` 和 `EnsureBuiltinHook` 的注释要写明名字随身份定下、以后的版本不能换名、要改名只能换新 ID。S3-14 的 Subject 写法表（problem.go）也在这一批：把 `name_reserved` 和新判定写进那张表，同时修掉表里原有的五处矛盾（参数名进不进 Path、模板一节漏写 Kind、NormalizeSN 的描述不准、param_unknown_field 的 Path 是原样键、改名与新建时 ID 的两种写法）。

**步骤：**
- [ ] **红：** 写判定与两个入口的测试，`go test ./internal/core/... ./internal/store/...` 应 FAIL
- [ ] **绿：** 实现，改现有用例里 `Builtin-Base` 这类不带前缀的内置名字，应 PASS
- [ ] **重构 + commit：** `feat(model): 保留名字前缀 lober: 与 name_reserved`

**风险：** 现有用例里的内置名字全都不带前缀，EnsureBuiltin 改成必带之后会整批失败，要一起改。第 5 层的派生对话框如果把来源名字预填进去，用户提交会撞上 `name_reserved`，预填时要去掉前缀——这一条写进「留给后续层的约定」。

## 任务 U6：契约用例并行（S4-2 选 B）

**目标：** 让契约用例并行跑，用信号量限并发。（S4-2）

**文件：**
- 修改：`internal/store/pgstore/pgtest/pgtest.go`、pgstore 的契约测试文件
- 修改：`docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md` 第 440 行

**契约：**

`runCases`、`eachAudited`、`runAudited` 的子测试加 `t.Parallel`。每个用例本来就各用一个 schema，彼此不干扰。并发数由 pgtest.Open 里的信号量限住：同时打开的测试仓储数乘以每个仓储的连接上限，要给几个代理同时跑测试留余量（本机 `max_connections` 是 100，`maxTestConns` 是 4）。`writeWhileHeld` 这类测锁等待的用例照旧串行。

早先有份报告说 `Test数据库错误的翻译` 会替换包级的 `tables`、所以不能并行——这个说法在 HEAD 上已不成立，给 `tables` 赋值的只有 `schema.go` 一处。

计划第 440 行的一分钟线改成：串行时 137 秒（S4-2 实测），所以并行加信号量；并行后仍超一分钟就报告耗时，再考虑复用 schema（选项 C）。要注明信号量只管一个测试进程，第 4、5、7 层也连库之后 `go test` 会同时跑好几个包。

**步骤：**
- [ ] **绿：** 加 t.Parallel 与信号量，`go test -count=1 ./internal/store/pgstore/` 应 PASS，并记录耗时
- [ ] **重构 + commit：** `perf(store): 契约用例并行，信号量限并发`

**风险：** 并发数定得太高会撑满 `max_connections`，表现为测试偶发失败。定数字时要留余量。

## 任务 U7：pgstore 的事务与错误翻译

**目标：** 修事务隔离级别、两处竞态和游标解码。（S7-1、S8-1、S7-6、S4-4）

**文件：**
- 修改：`internal/store/pgstore/open.go`、`errors.go`、`isos.go`、`hooks.go`、`templates.go`
- 修改：`internal/store/check.go`、`check_test.go`、`internal/store/errors.go`、`store.go`
- 修改：`internal/store/pgstore/schema_test.go`、pgstore 契约测试

**契约：**

S7-1：`write` 改成 `Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})`，pgstore 包说明写明这个前提。pgstore 全部写入都经 `s.write`（25 处调用），所以改这一处就覆盖全部。加一个测试：用带 `default_transaction_isolation=repeatable read` 的 DSN 打开仓储，跑一个 `writeWhileHeld` 用例，断言仍报 ErrConflict。

S8-1：`EnsureBuiltinHook`/`EnsureBuiltinTemplate` 的顺序倒过来——先 `INSERT 身份 … ON CONFLICT (id) DO NOTHING`，再 `lockIdent`，锁住之后才查版本并比对内容。这样后到的一路会等先到的提交，看得到它写下的版本，返回 nil，符合契约「已存在并且内容相同，什么都不做」。名字被用户身份占着时仍由 `idx_*_name_key` 报 DuplicateError。补两个测试：新身份、已有身份的新版本。**新顺序只在 READ COMMITTED 下成立，所以要在 S7-1 之后做。**

S7-6：translate 里约束认得、但 holder 查不到占用者时，改报 ErrConflict，调用方重试必然得到成功或 DuplicateError。`schema_test.go:400` 那一例的期望从「原样返回」改成 ErrConflict。要补注释的不止清单列的三处：`translate` 的注释、`isos.go` 里「查不到就原样返回」、`nameHolder` 的注释，现在都写着原样返回。`EnsureBuiltin*` 的接口注释还要写明：报 ErrConflict 时由装配点重试，否则程序会拒绝启动。

S4-4：`DecodeCursor` 遇到值为 null 的元素报 ErrInvalid——`json.Unmarshal` 对 null 既不报错也不改目标值，所以这种游标现在被静默收下，和 `check.go` 注释承诺的「改坏的都认得出来」不符。后果是审计和任务列表返回空页且 Next 为空，第 5 层会回 200 和空列表，调用方以为翻到头了；其余列表静默从头翻。测试主要加在 `check_test.go` 的 `TestCursor的编解码`（整个排序键为 null、第二个排序键为 null 两例），契约测试里给审计和任务各补一例。

**步骤：**
- [ ] **红：** 四条各写失败测试，`go test ./internal/store/...` 应 FAIL
- [ ] **绿：** 按上面的契约实现，应 PASS
- [ ] **重构 + commit：** `fix(store): 固定 READ COMMITTED，修两处竞态与 null 游标`

**风险：** S8-1 的新顺序依赖 READ COMMITTED。先做 S7-1。

## 任务 U8：pgstore 的分支与索引覆盖

**目标：** 让从没执行过的分支有用例，补上缺的断言和守卫。（S7-3、S4-3、L-6、S7-5、S7-4、S2-11、S4-5、S6-2、S4-8）

**文件：**
- 修改：`internal/store/pgstore/hosts.go`、`tasks.go`、`schema.go`、`schema_test.go`、契约测试
- 修改：`internal/store/check_registry.go`、`check_registry_test.go`、`types.go`、`doc.go`
- 修改：`internal/archtest/deps_test.go`、`guard_test.go`

**契约：**

S7-3：分批路径（batchSize=1000）从来没走到第二批。清单列的五处不全，grep `batchSize` 还有 `events.go:99`、`113`、`hooks.go:262`、`tasks.go:115`、`templates.go:186`、`436`。加一个用例一次导入 1001 台以上，断言每台的 Rev 都对、GetHosts 全部取到、审计条数等于台数。超过 1000 台建任务的用例看耗时再定。

S4-3：存不下的查找键与过滤条件当作没有——这条规则只有一个测试，且只覆盖四个条件。补全范围：ISO 的 Family/Series、审计的 TargetKind/TargetID/Action、任务的 HostSN/States、hook 的 NamePrefix、模板的 Series。预期：GetHost 和 DeleteHost 报 ErrNotFound 并 wantUnchanged；GetHosts 只返回存在的；各 List 不报错且 Items 为空；FindOpenTask 报 ErrInvalid。另登记一台属性值恰好是 U+FFFD 的主机，按 0xFF 过滤不能选中。`store/doc.go` 要加这句接口约定，那是给第 4、5、7 层看的。

L-6：ImportHosts 和 CreateTasks 各排一遍加锁顺序。抽一个给主机排加锁次序的函数，两处共用，注释写明凡给多台主机加锁都按它。加一个 PostgreSQL 测试：多轮同时跑主机重叠的 ImportHosts（入参顺序打乱）与 CreateTasks，断言没有 40P01、结果正确。这个测试不走 runCases，按 U6 的口径保持串行。

S7-5：`compareTable` 检查主键只比列不比名字，而 translate 恰恰靠 `hosts_pkey` 这个名字认出 SN 撞车。加一条判断：`pk.Name` 应等于表名加 `_pkey`，对不上就报出来并写明怎么用 RENAME CONSTRAINT 改回来。

S7-4：`models` 与 `tables` 一一对应只写在注释里。加一个用例，断言 models 里十个行类型的 `TableName()` 集合等于 tables 的表名集合。

S2-11：`checkStore` 只解析被构建约束排除的文件的 import，不把这类文件本身报出来。改成也对 `IgnoredGoFiles`（含 `_test.go`）报违规，规则文字改成不限第 1 层。`guard_test` 的组合用例改成断言这两个文件被报出来——这个夹具原来拿 memstore 当输入，U2 要换成 pgstore，两件事一起做。

S4-5：SHA256 只查存不存得下，不查格式。在 `CheckISOText` 规定它只能是空串或 `^[0-9a-f]{64}$`，否则报 ErrInvalid。超长值现在会撞上部分唯一索引的 btree 行长上限、报出没翻译的 54000；加了格式规则，请求在到达索引之前就被挡下。补大写、超长、非十六进制三个反例，`store.go` 里 UpdateISO 的注释写明这条格式要求。

S6-2：空的非 nil 过滤切片现在当作不过滤，但 `types.go` 只写了「零值表示不过滤」，空切片不是零值。写明 nil 和空切片都表示不过滤，四组过滤用例各补一条传空切片的。否则 pgstore 改成 GORM 惯用写法（非 nil 才加条件）会生成 `IN (NULL)`，一条都不返回，而第 5 层把 JSON 空数组解出来正好是这种值。

S4-8：「Limit 为 0 时取 100」只有 ListHosts 有用例。给 ListISOs、ListAudit、ListHooks、ListTemplates、ListTasks 各补一个：101 条数据，第一页 100 条且 Next 非空，第二页 1 条。写成共用的辅助函数。**要在 U6 之后做**，这几个用例各要 101 条数据。

**步骤：**
- [ ] **红：** 九条各写失败测试，应 FAIL
- [ ] **绿：** 实现，应 PASS
- [ ] **重构 + commit：** `test(store): 补分批、存不下的键、加锁顺序与索引覆盖`

## 任务 U9：契约注释补齐

**目标：** 把只写在实现注释、提交说明或用例里的行为补进 store 包的接口注释。（S4-6、S4-1、S5-1 的注释部分、L-7 的注释部分）

**文件：**
- 修改：`internal/store/store.go`、`types.go`、`errors.go`、`doc.go`、`check.go`
- 修改：`internal/store/pgstore/schema.go`、`templates.go`、`doc.go`
- 修改：`internal/core/admission/admission.go`
- 修改：`docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md` 第 90 行

**契约：**

S4-6 的七条照清单改，只有两处要调整措辞：③ 排序那条，「两份实现和 isoListPaging」改成「pgstore（COLLATE "C"）和 isoListPaging」，并把「字符串按字节序比较」写进 `types.go` 的契约；④ 非 nil 规则搬进 `doc.go` 时，理由只留「第 5 层编成 JSON 是 `[]` 与 `{}`，nil 会编成 null，与 OpenAPI 对不上」。⑦ 的「两份实现建任务时都不读这张表」改成「pgstore 建任务时用 HookRefs 现算」。

计划第 90 行一并改准：顺序固定，字符串一律按字节序比较（pgstore 用 `COLLATE "C"`）；ISO 按 Name 再按 ID（ISO 的名字不规范化，区分大小写）；任务按 CreatedAt 倒序、同一时刻按 ID 升序；事件不分页，由 ListEvents 按 Seq 升序续读。

S4-1：按 SN 查找的几个方法报法不一，契约一句没写。规则是「带整条记录的写入和 FindOpenTask 查规范化，其余按 SN 查找的不查」。在 `store.go` 的 Hosts 说明和 FindOpenTask 上写明这条规则和理由（网关不能把漏了规范化当成「没有任务」，否则机器会从本地盘启动、任务一直等着）。

本任务是唯一获准改原计划「留给后续层的约定」一节（第 512 行附近）的任务——U1 只改了「已定的决定」表，正文留给这里。要改和要补的：

- 第 3 层那条现在还写着「先定一个只给内置内容用的名字前缀」，已经过时，改成前缀已定为 `lober:`，并补上 S5-1 那句「名字随身份定下，以后的版本不能换名，要改名只能换新 ID」。
- 第 5 层补五条：按 SN 调仓储之前先规范化（S4-1）；派生对话框预填来源名字时要去掉 `lober:` 前缀，否则用户提交会撞上 `name_reserved`（U5 的风险）；派生来源可能已被删除，提示按同一身份下剩下的版本算（S5-2）；取草稿引用的 hook 之前先滤掉不合 ValidID 的引用（S5-3）；驳回接口要求非空理由并放进 Audit.Detail（S5-7）。
- 连接池的上限由装配点从配置读出来传给 pgstore（S7-7，代码部分在 U4 做）。

L-7 只改注释、不改行为：admission 的两处和 `store.go` 里 CreateTasks 第 3 步写明，重跑准入用的是任务里钉住的版本，它是不是当前版本由第 5 层在提交时比对。

**步骤：**
- [ ] **绿：** 只改注释与文档，行为和测试都不动，`make check` 应 PASS
- [ ] **重构 + commit：** `docs(store): 契约注释补齐实现与测试钉住的行为`

## 任务 U10：model 导出任务状态清单（S9-1）

**目标：** 让「哪些状态算已结束」只有一份出处。（S9-1）

**文件：**
- 修改：`internal/core/model/task.go`、`task_test.go`
- 修改：`internal/store/pgstore/tasks.go`、`schema_test.go`、契约测试

**契约：**

```go
package model

// TaskStates 是全部任务状态，顺序与状态机一致。
func TaskStates() []TaskState
```

`Valid` 改成按这份清单判断——否则 switch 里加了状态、清单漏了，照样逃得过。pgstore 的 `finishedStates` 由这份清单按 `Finished()` 算出来，`schema_test` 的 `candidates` 也用它，再加几个非法写法。契约测试里两处遍历结束状态的循环改成遍历清单。

这样 model 一加结束状态，CHECK 和索引谓词会由 `Test任务状态的CHECK与C31的索引都与model一致` 报红，`FindOpenTask` 和建任务时查未结束任务的那条查询会由契约用例报红。清单里「加一行断言」那个较弱的方案不采用。

标题的标记改写为「（pgstore 与 model 可能不一致）」，不是去掉——只剩一份实现，但 pgstore 和 model 之间仍有对不上的风险。

**步骤：**
- [ ] **红：** 写清单与 Valid 的测试，以及「finishedStates 由清单算出」的断言，应 FAIL
- [ ] **绿：** 实现，应 PASS
- [ ] **重构 + commit：** `refactor(model): 导出任务状态清单，结束状态只有一份出处`

## 任务 U11：第 1 层与往返读写的测试缺口

**目标：** 补上几处测试缺口。（S3-13、S2-15、S4-7、S6-1、S5-5、S5-4）

**文件：**
- 修改：`internal/core/activation/template_test.go`
- 修改：`internal/store/check_test.go`、`check_registry_test.go`
- 修改：pgstore 的契约测试

**契约：**

S3-13：检查「所勾各系并起来看」的两个用例，起决定作用的引用都在 rocky 的片段里，而 rocky 是模板勾的第一个系，所以只抓得住「只留最后一个系」这一种退化。把 `inOneTicked` 反过来写（splitPart 第一份留空、第二份放 volgroup 那一行），`gw` 那例不动，两个方向就都钉住了。

S2-15：`CheckAudit` 的 Detail 和 `CheckNewTasks` 的 CreatedBy 测「存不下」时只用了 NUL，各加一个非法 UTF-8 的反例（0xFF 字节）。

S4-7：`isoSHA256` 里写入的一方 B 只置过 importing 和 ready，acquiring 只出现在占位的一方。加一例：B 停在 acquiring，把 B 的 SHA256 写成那个 ready 占位 ISO 的 SHA256，断言 `DuplicateError{sha256, 对方 ID}` 并 wantUnchanged；`TestHoldsSHA256` 补一行 acquiring 为 true。

S6-1、S5-5、S5-4：S6-1 和 S5-4 在 U3 已经有了白盒断言（直接查 `last_seq` 和版本计数），本任务只补 S5-5：样例数据里 `ParamDecl.List`、`Required`、`Offer` 和 `Binding.Editable` 四个字段都是零值，深拷贝、往返读写和 builtinEnsure 的比对都没覆盖到。给一个参数设 List 和 Required，一个 enum 改用 Offer，一条取值来源设 Editable 为 true，variants 各加一项。

**步骤：**
- [ ] **红：** 各写失败测试，应 FAIL
- [ ] **绿：** 实现（S3-13、S2-15、S4-7、S5-5 都只改测试或样例），应 PASS
- [ ] **重构 + commit：** `test: 补激活判定方向、非法 UTF-8 反例与往返字段`

## 任务 U12：同步两份清单

**目标：** 把修补结果写回清单，更正过时的说法。（S10-5，以及两份清单的同步）

**文件：**
- 修改：`docs/superpowers/reviews/2026-10-07-layer2-open-issues.md`
- 修改：`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`

**契约：**

照第 1 层的先例：清单开头加一段「修补结果」（总述、提交表、逐条表），每条标题下加一行「结果：」，原有的问题、影响、选项、建议、出处、合并自都不动。

第 2 层清单还要改这些：开头和术语表里「两份实现」的说法；13 条去掉「（两份实现不一致）」标记，S9-1 改写为「（pgstore 与 model 可能不一致）」；S6-3、S5-6、S6-4 标注「理由随 memstore 删除而消失，不修」；S6-5 标注整条作废；L-8 的「两份实现写的文字不一样」这个前提消失，但 ReferencedError 的建议仍成立；S2-22 选项 ② 里的「两份实现共用」要改写；S10-3 决定段里「C25 含 hook 要改就先派生」这句不对，要更正（C25 原文没有「派生」二字）；各条出处里的 memstore 路径删掉。

S10-5 改第 1 层清单：在 L-8 下面补一段结果，写明字段键已随 C-3 修掉（放在 model 而不是 params），「ID@版本」有五处各拼一份、可以在 model 给 HookKey/TemplateKey 加 String()（**不是**改用 `store.VersionID`：第 1 层不能依赖 store，反过来引用还会成环），catalog 常量、全局词表和 Subject.Kind 常量仍留给第 3、5 层。清单第 18 行和第 636 行「其余 14 条没有动」的说法一并改掉。

**步骤：**
- [ ] 写修补结果与逐条结果，核对每条都标了
- [ ] commit：`docs: 第 2 层修补后同步 spec、计划与两份清单`

## 整体验收

- [ ] `make check` 全绿，交叉编译 linux/amd64 通过
- [ ] `internal/store` 下只剩三个包：契约、pgstore、pgtest
- [ ] 用户的 6 条决定每条都能用一个测试或一处文档证明
- [ ] 清单第 2 节 25 条都标了「已修」，3 条标了「理由消失，不修」
- [ ] 契约测试的叶子子测试数不少于搬家前的 250 个
- [ ] pgstore 包的测试耗时记录在报告里（并行前后各一个数）
- [ ] spec §6.4 写明两个环境的差别；§6.3 的结论没动，只换了第二条理由
- [ ] 第 4、5、7 层能直接 import `pgstore/pgtest` 起一个测试仓储
