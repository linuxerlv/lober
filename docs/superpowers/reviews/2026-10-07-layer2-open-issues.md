# 第 2 层遗留问题清单

日期：2026-10-07
对象：`feat/store` 分支在提交 b61ba0a 时的代码，涵盖第 2 层的 10 个任务和之后的验收修正。

在第 2 层的 10 个任务里，实现者和评审者一共留下了 233 条记录。我把重复的记录合并以后，还剩 159 条。这 159 条我都逐条对照 b61ba0a 的代码核实过。光看代码拿不准的，我就到探针克隆里跑测试，或者做变异验证。核实的结果是：

- 68 条已经被验收修正或后续任务解决了，或者属于合理的偏差。
- 37 条只是表述上的问题，或者没有实际影响。
- 4 条需要你拍板，放在第 1 节。
- 28 条应该在第 3 层开工前修掉，放在第 2 节。其中 14 条当时标着「（两份实现不一致）」，见下一段。
- 15 条归到后续的层去处理，放在第 3 节。

写这份清单时（b61ba0a），internal/store 的接口有两份实现：内存实现 memstore 和 PostgreSQL 实现 pgstore。第 2 节有 14 条当时在标题末尾标着「（两份实现不一致）」，说的是同样的调用，两份实现可能给出不同的结果，或者眼下一致、只是没有测试防着。它们原本排在前面，因为原计划让第 5 层的接口测试跑在内存实现上，两边一旦不一致，问题要到生产环境才会出现。2026-10-08 你定了删掉内存实现，只留 pgstore（见「你的决定」一节），这条排序理由随之消失。修补时 13 条去掉了这个标记，它们讲的其实是 pgstore 自己的缺陷；S9-1 的标记改成「（pgstore 与 model 可能不一致）」，因为 pgstore 和 model 之间仍可能对不上。

## 修补结果

2026-10-08 按你的决定修补，计划见 `docs/superpowers/plans/2026-10-08-layer2-fixups.md`。第 1 节 4 条都已落实。第 2 节 28 条在「删掉 memstore」的新前提下逐条重判过：25 条已修；S6-3、S5-6、S6-4 的理由随 memstore 删除而消失，不修。第 3 节顺带做了几条：S7-7 与 S5-8 已修，L-7 的注释部分已改，S5-2、S5-3、S5-7 写进了原计划「留给后续层的约定」的第 5 层；S6-5 讲的是 memstore 的 cloneTask，整条作废；其余仍归后续层。每条的做法和测试名写在条目标题下的「结果」一行。

正文各条的问题、影响、选项、建议写的是 b61ba0a 时的情形，那时 memstore 和 storetest 都还在。正文里说「内存实现」「两份实现」「storetest」的地方，都指那时的代码；修补后的情形以「结果」为准。出处里的行号指 b61ba0a，修补后已经变了。出处里 memstore 的路径已经删掉；storetest 各文件的内容按聚合并进了 pgstore 的 contract_*_test.go（比如 storetest/hosts.go 对应 contract_hosts_test.go），属于 package pgstore。

修补轮的提交，都在 `feat/store` 上：

| 提交 | 任务 | 内容 |
|---|---|---|
| 0b7cf57 | — | 修补前：本清单改写成正常中文，记下 10-08 的决定 |
| fda1c88 | — | 修补计划：6 条决定、25 条应修项、12 个任务 |
| 73ac12c | U1 | spec 只留 PostgreSQL 一份实现，新增 §6.4「测试环境与生产环境」；S10-2、S10-3、L-8b、S5-1 的 spec 部分；原计划追加本轮决定，任务 4–6 加历史横幅 |
| 5dcfe69 | U2 | 删掉 memstore 与守卫里对应的规则，清掉「与内存实现一致」的注释，补 hook、模板、任务三个列表的 NUL 游标用例 |
| 8e7e9be | U3 | storetest 并进 pgstore 的 contract_*_test.go，补 last_seq、last_version、template_hook_refs 三处白盒断言 |
| dbea587 | U5 | 保留前缀 `lober:` 与原因代码 name_reserved；S5-1 的注释；S3-14 的 Subject 写法表 |
| f7d05d8 | U4 | 导出测试夹具 pgtest（DSN、Open、FaultActor）；pgstore.Open 加 WithMaxConns（S7-7） |
| 5e58281 | U7 | 写事务固定 READ COMMITTED（S7-1）；S8-1（修了两轮）、S7-6、S4-4 |
| 7b9de91 | U8 | S7-3、S4-3、L-6、S7-5、S7-4、S2-11、S4-5、S6-2、S4-8 |
| f0f30eb | U6 | 契约用例并行，信号量限并发（S4-2） |
| 7829943 | U8 补漏 | L-6 的死锁测试 hostlock_test.go：新建的文件，7b9de91 用 git add -u 没带上 |
| 1770b24 | U9 | 契约注释补齐：S4-6、S4-1、L-7 的注释；原计划「各查询类型」那一行与「留给后续层的约定」 |
| 38a2a4e | U10 | model 导出 TaskStates，结束状态只有一份出处（S9-1） |
| d2652fe | U11 | S3-13、S2-15、S4-7、S5-5 的测试与样例 |
| 与本节同一个提交 | U12 | 本清单与第 1 层清单同步（含 S10-5） |

各条「结果」里写的是任务名（U1 到 U12），对应的提交号查上面这张表。

逐条：

| 条目 | 结果 | 提交 |
|---|---|---|
| S10-2 | 选 A，前缀定为 `lober:` | 73ac12c、dbea587，后续层的约定随 U9 |
| S4-2 | 选 B，同时打开的测试仓储限在 4 个 | f7d05d8、f0f30eb |
| L-8b | 不加退役状态，spec 补装机历史和两条接受的风险 | 73ac12c |
| S10-3 | 选 A，hook 不能派生 | 73ac12c |
| S7-1 | 已修 | 5e58281 |
| S4-5 | 已修 | 7b9de91 |
| S8-1 | 已修（修了两轮） | 5e58281 |
| S4-4 | 已修 | 5e58281 |
| S7-6 | 已修 | 5e58281 |
| S7-3 | 已修，12 处分批循环覆盖了 9 处 | 7b9de91 |
| S4-3 | 已修 | 7b9de91 |
| S6-2 | 已修 | 7b9de91 |
| S9-1 | 已修，model 导出状态清单 | U10 |
| L-6 | 已修 | 7b9de91、7829943 |
| S2-15 | 已修（测试） | U11 |
| S4-7 | 已修（测试） | U11 |
| S7-5 | 已修 | 7b9de91 |
| S6-1 | 已修（白盒断言） | 8e7e9be |
| S4-6 | 已修（注释） | 5dcfe69 清掉旧说法，U9 |
| S5-1 | 已修（文档与注释） | 73ac12c、dbea587，后续层的约定随 U9 |
| S3-14 | 已修（注释） | dbea587 |
| S4-1 | 已修（注释） | U9 |
| S10-5 | 已修（文档） | U12 |
| S7-4 | 已修 | 7b9de91 |
| S3-13 | 已修（测试） | U11 |
| S2-11 | 已修 | 5dcfe69 换夹具，7b9de91 |
| S5-5 | 已修（样例） | U11 |
| S5-4 | 已修（白盒断言） | 8e7e9be |
| S4-8 | 已修 | 7b9de91 |
| S6-3 | 不修：理由随 memstore 删除而消失 | — |
| S5-6 | 不修：理由随 memstore 删除而消失 | — |
| S6-4 | 不修：理由随 memstore 删除而消失 | — |
| S7-7（第 3 节） | 已修 | f7d05d8，装配点的约定随 U9 |
| S5-8（第 3 节） | 已修（白盒断言） | 8e7e9be |
| L-7（第 3 节） | 注释部分已改，比对仍归第 5 层 | U9 |
| S5-2、S5-3、S5-7（第 3 节） | 写进原计划「留给后续层的约定」，仍归第 5 层 | U9 |
| S6-5（第 3 节） | 整条作废 | 5dcfe69 |
| L-8（第 3 节） | 前提消失，建议仍成立，仍归第 5 层 | — |

### 和计划设想不同的几件事

下面几件事以提交说明为准，这里只摘要。

1. 测试夹具的规则现在有两份（U4，f7d05d8）。计划要 pgstore_test 改用 pgtest，做不到：pgtest 依赖 pgstore，pgstore 包内的测试再 import 它，会报 import cycle not allowed in test（执行者用 go vet -overlay 验证过）。所以 pgstore 包内的夹具留着，只改成经 WithMaxConns 限连接数；U6 加的信号量也是两份，pgtest.Open 一份，pgstore 包内的 newSchema 一份。两边靠注释互相提醒同步。要去掉这份重复，得新加一个不依赖 pgstore 的低层包，这和整体验收的「internal/store 下只剩三个包：契约、pgstore、pgtest」冲突，留待你拍板。另外，pgtest.Open 装失败注入改用另开的 pgx 连接，所以 pgstore 不必像计划设想的那样导出数据库句柄。
2. S8-1 修了两轮（U7，5e58281）。第一轮照计划用 ON CONFLICT (id) DO NOTHING，只解决了「先到的已插入、还没提交」这一种交错。复查时用探针发现另一种：两条 INSERT 都在对方插入之前通过了预检查时，因为 id 是唯一的仲裁索引，后到的一路会在 idx_*_name_key 上撞车报 23505，而 nameHolder 查到的占用者正是自己那个 ID，于是返回 DuplicateError{name, 自己的 ID}，装配点只能拒绝启动。探针 300 轮里重现 53 轮（17.7%）。第二轮改成不带目标的 ON CONFLICT DO NOTHING，让所有唯一索引都当仲裁；锁不到身份时在事务内按 name_key 查占用者，查到报 DuplicateError{name, 对方 ID}，查不到报 ErrConflict。回归用例跑 200 轮，改回旧写法时「同一个 ID」那组报红（27%–31%）；另有「不同 ID 同名」一组，证明不带目标的 DO NOTHING 没把该报的名字冲突吞掉。独立验证另写探针跑了 3600 轮，0 失败。
3. 分批循环没有全部覆盖（U8，7b9de91）。S7-3 列的不全，grep 出来一共 12 处分批循环，现在覆盖了 9 处。剩下 3 处与 hook 引用有关，要凑上千个 hook 版本，成本高，暂不覆盖。
4. pgstore 整包的耗时。U6 的提交说明记的是并行前 142 秒、并行后 90 秒，其中契约部分从 117 秒降到 53 秒，其余串行的测试约 37 秒；U8 在这期间还新增了一次导入 2001 台、建任务 1002 台、五个列表各 101 条数据这类用例。并发数定在 4 是按实测曲线：串行 117 秒、2 路 73、4 路 53、8 路 44、16 路 49，瓶颈在数据库这边的 DDL，过了 4 收益不大，最坏情况的连接数却要翻倍。U12 写这一节时，在含 U9–U11 改动的工作区里复测过一次整包：86 秒。
5. 契约测试的叶子子测试，搬家时 250 个，与搬家前逐条对应，一个没少（8e7e9be）；现在是 261 个（U12 时用 go test -v 数的）。

## 你的决定（2026-10-08）

第 1 节的 4 条都定了，第 2 节的 28 条你整节同意。这些决定都已在 2026-10-08 的修补轮落地：修补计划照第 1 层的先例（`docs/superpowers/plans/2026-10-02-layer1-fixups.md`）另写了一份，是 `docs/superpowers/plans/2026-10-08-layer2-fixups.md`，结果见上面的「修补结果」。这一节只记决定本身。

| 条目 | 决定 |
|---|---|
| S10-2 | 选 A：保留前缀的判定放在第 1 层的 model 里，另加一个原因代码。前缀定为 `lober:`（见修补计划） |
| S4-2 | 选 B：契约用例并行跑，用信号量限制并发 |
| L-8b | 不加退役状态，等于选 A。一台主机可以先后有多个安装任务，这就是它的装机历史 |
| S10-3 | 选 A：hook 不能派生。模板派生之后要换 hook，b 不存在就先建一个，再在原位把 a 换成 b |
| 第 2 节 28 条 | 全部同意修 |

还有两条决定不属于清单里的条目：

- 测试和生产都用 PostgreSQL，分成两个环境。内存实现 memstore 删掉，只留 pgstore。已落地：memstore 在 5dcfe69 删除，storetest 在 8e7e9be 并进 pgstore。
- 「分环境」具体怎么分，当时还无据可依。spec 没有讲部署形态或环境划分的章节，装配点 cmd/lober 也还不存在，生产那边的 DSN 从哪来、配置什么格式都没定。测试这边已经分开了：专用角色和库 lober_test（角色不是超级用户），每个用例一个临时 schema。所以要补的是生产侧，spec 里加一节，写明两边各自的 DSN 来源、角色权限，以及库或实例怎么隔开。装配点要到第 5 层才出现（spec §7.4），连接池（S7-7）和事务隔离级别（S7-1）可以一并写进那一节。已落地：spec 新增 §6.4「测试环境与生产环境」（73ac12c），写了四件事：两个环境跑同一份代码，区别只在连哪个库；测试环境的角色、库、临时 schema 与连接串只从 `LOBER_TEST_PG_DSN` 或 `.pgtest.dsn` 读；生产环境只定到「连接串由装配点读配置得到」，模块自己不读环境变量和文件，所以配错一个变量不会让生产连到测试库；两边都固定 READ COMMITTED，并用同一个 PostgreSQL 大版本。生产侧的角色权限、配置格式没有写，留到装配点在第 5 层出现时再定。连接池没写进 spec：pgstore.Open 加了 WithMaxConns（f7d05d8），装配点从配置读出上限再传进来这一条，写进了原计划「留给后续层的约定」（U9）。

### 删掉 memstore 之后，这份清单有几处要跟着改

这一小节是修补前写的，下面各处在修补轮都处理过了，怎么处理的写在每一处后面的括号里。

这条决定推翻的是第 2 层计划第 56 行自己加的前提，也就是「第 5 层的 HTTP 测试也用 memstore」，不是 spec 的规定。spec §7.2 第 5 条只要求每个接口都有 HTTP 测试，没说用哪份存储。但 spec 有四处写着两份实现跑同一套契约测试，都要改：§4.2（第 224 行）、§6.1（第 316 行）、§6.3（第 346 行，那里拿内存实现能模拟回滚当论据）、§7.2 第 2 条（第 364 行）。（已改，73ac12c：§4.2、§6.1、§7.2 第 2 条，连同 §7.1、§7.2 第 5 条，都改成只有 pgstore 一份、测试连测试环境的真实库、找不到连接串就失败不跳过；§6.3 的结论「事务不做成切面」没动，只把第二条理由换成加锁顺序：事务放进 ctx 传，加锁的先后会散到调用方，仓储定死的顺序就守不住。）

清单这边要改的地方：

- 开头那一段给「（两份实现不一致）」排优先级的理由不再成立。只剩一份实现，就谈不上两边走偏。（开头那一段已改写。）
- 带这个标记的 14 条里，大多数讲的其实是 pgstore 自己的缺陷：没翻译的 SQLSTATE、从没走到的分批路径、手写的状态清单、两处各写一份的加锁顺序。删掉 memstore 不影响这些条目该不该修，只是标记和「影响」段的措辞要改。（13 条去掉了标记，S9-1 改成「（pgstore 与 model 可能不一致）」；「影响」段保留原文，修补后的说法写在各条的「结果」里。）
- 有两条的理由会消失或大幅削弱。S4-3 要把用例从 pgstore_test 搬进 storetest，图的就是让两份实现都跑。S4-7 的变异验证做在 memstore 上，而 pgstore 本来有部分唯一索引兜底。这两条修补时要重新判断还值不值得做。（重判后两条都照修：S4-3 在 7b9de91，S4-7 随 U11。为什么仍值得做，见各条的「结果」。）
- 各条「建议」里凡是写着「两份实现都跑」「两边各加一份」的，都要改写。（各条的「建议」保留原文，只有 S2-22 的选项 ② 改了措辞；修补时按什么口径做，写在各条的「结果」里。）
- storetest 这个包的定位也要重写。它不再是跨两份实现的契约测试，而是 pgstore 的测试集。（storetest 已在 8e7e9be 并进 pgstore 的 contract_*_test.go，属于 package pgstore；术语表跟着改了。）
- 第 4、5 层的 HTTP 测试改用 PostgreSQL 之后，测试耗时会在 S4-2 已经实测的 137 秒上继续涨。你给 S4-2 选的 B（并行加信号量）正好是对着这件事的。（U6 并行之后，pgstore 整包从 142 秒降到 90 秒，见「修补结果」。）

### 装机历史：现在的代码已经支持，缺三样

一个任务就是一次装机。C31 只禁止同一台主机同时有两个未结束的任务（spec 第 201 行），部分唯一索引 idx_tasks_open 的谓词写的就是这个条件，所以上一个任务一结束，就能给同一台主机建下一个。ListTasks 支持按 HostSN 过滤，按 created_at 倒序列出。每个任务的 State 就是那一次装机的最终状态，Snapshot.Host 里还留着建任务那一刻的登记信息。所以这条决定不用改模型。

真正缺的有三样，都不在第 2 层的范围里：

- 没有结束时间。任务只有 CreatedAt 一个时间字段（model/task.go:57），既没有结束时间，也没有最后一次推进的时间。想知道某次装机什么时候结束，只能去翻事件的 At，可契约并不保证推进时一定带事件。
- 按主机查会越来越慢。tasks.host_sn 上没有覆盖全表的普通索引，这就是归在第 3 节的 S9-2。任务多了以后，按主机列装机历史会退化成顺序扫描。
- 「一次」和「一个任务」对不上。失败的任务是在原任务上重试，从 failed 推回 pending，用的是同一个任务 ID（storetest/tasks.go:616-628；spec 第 304 行写的是重试沿用原来的快照）。所以一个任务里可能装过不止一次，而历史列表上只看得到它当前的状态。要让每一次尝试都单独占一行，就得改成重试另建一个任务。这一条等第 5 层做历史视图时再定。

L-8b 原来的问题还留着，这是选项 A 的代价：主机只要建过一次任务就永远删不掉，又没有废弃状态可用。退役、报废、换过主板的机器会一直留在主机列表和向导的候选里，只能靠界面过滤筛掉。ISO 那边同根的问题也还在：被任务引用过的 ISO 删不掉，§5.1 第 5 步要求「删 ISO 时一并删除」的原始字节也就一直占着磁盘。第 4 层设计 ISO 的导入与清理时要一起想。这两条代价已作为接受的风险记进 spec §8（73ac12c）。

因为不加退役状态，另外两个问题也就不存在了：主机还有未结束任务时能不能退役，以及退役的主机遇到 CSV 导入怎么算。后面那条你说待会再聊，等真要加状态时再定。

## 这份清单怎么读

正文分三节，每节的性质不一样：

- 第 1 节是需要你拍板的事。这些条目的毛病出在计划或 spec 本身：有缺口、有矛盾，或者需要做取舍。
- 第 2 节是第 3 层开工前应该修的事。它们是实际的缺陷，或者防护上的缺口。该怎么修，按计划和 spec 就能判断出来，所以不需要你拍板，只需要你同意修。
- 第 3 节是归后续层处理的事。它们属于后面的层，或者要等后面的层做出来才真正用得上。写那一层的计划时把它们纳入就行。

每一条下面都用固定的几个标签展开：

- 问题：说明现在哪里不对，或者计划和 spec 哪里没有定。
- 影响：说明这个问题会造成什么后果。
- 选项：只在第 1 节出现。它列出几种可选的做法和各自的代价，最后写明倾向哪一个、最晚什么时候要定。
- 建议：第 2、3 节用它代替「选项」，写的是建议怎么修。
- 出处：列出这一条的依据，包括代码的文件路径和行号，以及计划和 spec 里的条目。
- 合并自：列出这一条是由哪几条原始记录合并来的，引的是记录的原文。
- 结果：修补轮（2026-10-08）之后加的，写在条目标题的下面，说明这一条最后怎么处理、在哪个提交、测试叫什么。上面几个标签的内容都保留原文。

## 文中反复出现的几个说法

- 两份实现：b61ba0a 时，internal/store 定义的接口有两份实现。一份是内存实现 memstore，另一份是 PostgreSQL 实现 pgstore。memstore 已在修补轮删除（5dcfe69），现在只有 pgstore 一份；正文里说「两份实现」「内存实现」的地方，都指 b61ba0a 时的代码。
- 契约、契约测试：契约指 internal/store 包本身，pgstore 要遵守它。它包括四部分：每个聚合一个接口（合成 Store）；错误、审计、事件、分页这些共用类型；共用的纯函数，写在 check*.go 里，比如 CheckQuery、DecodeCursor、HookRefs，正文说的「共用函数」「共用检查」指的就是它们（b61ba0a 时由两份实现共用，现在由 pgstore 调用）；以及注释里写明的行为约定，这些注释主要在 store.go、types.go、errors.go、doc.go 里。正文里说「存储契约」「store 契约」，指的也是它。契约测试在 b61ba0a 时是 storetest 包里的一套测试，两份实现跑的是同一套；修补轮把它并进了 pgstore 的 contract_*_test.go（8e7e9be），属于 package pgstore，入口是 Test契约主机、Test契约ISO 这类 Test契约Xxx，可以直接查库做白盒断言。
- pg 专有测试：写在 pgstore 包里、只对 PostgreSQL 实现跑的测试，比如 schema_test.go 里的测试，以及 pgstore_test.go 里除 Test契约 以外的测试。它们不属于契约测试，内存实现不跑。Test契约 也写在 pgstore_test.go 里，但它是在 pgstore 上跑契约测试的入口。这是 b61ba0a 时的分法；契约测试并进 pgstore 之后，两类测试都在 pgstore 包里，正文里的说法仍按当时的分法。
- 探针、探针克隆：评审和核实时另开的一份代码克隆，专门用来跑实验。比如在 DSN 上加参数，或者补几条断言看结果。
- 变异验证：故意把代码改坏，看测试能不能发现。如果改坏以后测试照样全绿，就说明这个行为没有被测试钉住。
- 存储层守卫：internal/archtest 里的测试 Test存储层依赖守卫，用来检查存储层的依赖规则。具体的检查写在它调用的 checkStore 函数里，checkStore 靠解析 import 来判断。正文里单说「守卫」，指的也是它。
- 钉住：某个行为被测试固定下来了，谁改坏它，就会有测试失败。「没钉住」就是改坏了也没有测试报错。L-7 那一条里说的「任务里钉住的版本」「任务钉住的模板版本」是另一个意思，指任务固定用某一个模板版本。
- 条目编号：S 开头的条目以某个任务的记录为主，S 后面的数字就是那个任务的任务号，比如 S4 是任务 4。有些 S 条目还并进了别的任务或层间评审的记录，比如 S4-2 并了 S8、S9、S10 的记录，S8-1 并了层间评审第 4 条，具体看各条的「合并自」。L 开头的来自层间评审，比如 L-6 对应层间评审的第 6 条。「合并自」里的 layer_should 就是层间评审的记录。
- C 编号：正文里还有两种 C 编号，都不是本清单的条目。不带连字符的（C3、C4、C23、C26 等）是 spec 里的约束编号。带连字符的（C-3、C-4、C-9）是第 1 层遗留问题清单（docs/superpowers/reviews/2026-10-02-layer1-open-issues.md）里的条目。两边的号码会撞，比如 spec 的 C3 和第 1 层清单的 C-3 说的不是一回事。
- 第 N 层：重写是一层一层推进的。第 2 层是存储层，代码在 internal/store 下，也就是这份清单的对象。第 3 层是下一个要开工的层。

## 1. 需要你拍板的事

这一节的问题出在计划或 spec 本身。有的是留了缺口，有的是前后矛盾，有的需要在几种做法之间取舍。每一条都列出了可选的做法和各自的代价，给出了建议，也写明了最晚什么时候必须定下来。

### S10-2　内置专用的名字前缀该在哪一层拦，还没定；直接把拦截写进现有的名字规则，内置内容就装不进去了

结果：选 A，已落实，前缀定为 `lober:`。model 导出 ReservedNamePrefix 与 NameReserved，按规范化后的 key 比前缀，所以 LOBER:x 也算保留，lober-base、lobertest 不算；新增原因代码 name_reserved。规范化拆成两个方向：用户的五条路径（CheckNewHook、CheckNewTemplate、DeriveTemplate、RenameHook、RenameTemplate）用了前缀报 ProblemsError(ErrInvalid)；内置装载反过来要求必带前缀，不带或只是像前缀（lober-xxx）都报 ErrInvalid。契约用例里不带前缀的内置名字一并改掉（dbea587）。spec 的 C12 与 §6.2 第 11 条已改（73ac12c）；原计划「留给后续层的约定」里第 3 层那条随 U9 改成前缀已定，第 5 层补了「派生对话框预填来源名字时去掉前缀」。测试：TestNameReserved按key判定保留前缀、TestReservedNamePrefix是合规则的key、TestAlign保留名字前缀与ReservedNamePrefix一致，契约用例「内置：入参不是规范的内置已审批版本，或名字不带保留前缀，报 ErrInvalid」。

决定：选 A（2026-10-08）。判定放在第 1 层的 model 里，另加一个原因代码。用户的几条路径经它拦下，内置内容的装载反过来要求名字必须带前缀。前缀本身用什么字符串，要在第 3 层写第一份内置内容之前一起定。要改的是 spec §6.2 第 11 条（第 330 行）和 C12（第 173 行）；§4.2 第 226 行「判定规则仍然只有层 1 一份」正是选 A 的依据。

问题：spec §6.2 第 11 条，和计划里「留给后续层的约定」一节的第 3 层条目，提的是同一个要求。它们都写了「先定一个只给内置内容用的名字前缀，并在名字规则里拦下用户用它」。内置内容指的是程序自带、启动时由 EnsureBuiltin 装进仓储的那部分内容。但这两处都没写前缀具体是什么，也没写要在哪一层拦。而现在的代码里，用户的操作和内置内容的装载走的是同一条路。用户这边的操作是新建、派生和改名，对应 CheckNewHook、CheckNewTemplate、DeriveTemplate、RenameHook、RenameTemplate。内置内容的装载由 checkBuiltin 负责。两边都是先调 store.NormalizeName，再由它调到 model.NormalizeName。

影响：如果照字面意思把拦截写进 model.NormalizeName，内置内容也会被一起拦下。EnsureBuiltin 会对每一份带前缀的内置内容报 ProblemsError(ErrInvalid)，结果程序拒绝启动。契约测试（pgstore 和内存实现共用的那套测试）发现不了这个问题，因为契约测试里的内置名字都是 Builtin-Base 这类不带前缀的。这件事不定下来，第 3 层就没法给内置内容起名字。

选项：

A. 把规则放在第 1 层。第 1 层的 model 提供一个判定，用来回答某个名字是不是用了保留前缀。另外再加一个新的原因代码，比如 name_reserved。用户的几条路径都经过这个判定，用了前缀就被拦下。内置内容的装载正好反过来，要求名字必须带前缀。实现上可以把规范化拆成两个入口，一个给用户，一个给内置内容。代价：第 1 层要多一条规则和一个原因代码。store 里的共用检查要跟着改，两份实现（pgstore 和内存实现）里的派生与改名路径要跟着改，契约测试也要跟着改。不过好处是，判定规则仍然只在第 1 层有一份（spec §4.2），第 5 层做预检时也能报出同样的原因。

B. 只在第 5 层的控制台里拦，仓储层不管。EnsureBuiltin 装内置内容时如果撞了名，会报 DuplicateError，就拿它当兜底。代价：这个方案改动虽然最小，但拦截不在名字规则里做，和 spec 的字面要求不符。不经过控制台的调用方也拦不住，不过眼下创建用户内容的只有控制台。

C. 不用前缀，让内置内容和用户内容的名字各占一个空间。唯一索引相应地改成 (builtin, name_key)。代价：期望的表结构要跟着改，不过系统还没部署，所以不用写升级动作。内置内容和用户内容同名时，只能靠界面来区分。C12 和 §4.2 里的「名字唯一」也要改写。

前缀本身用什么字符串，也要一起定下来。定的时候要遵守名字规则：名字只收可打印 ASCII，长度 1 到 64 个字符，不能含空格。这件事最晚在第 3 层写第一份内置内容之前定。我倾向 A。

出处：spec §6.2 第 11 条（第 330 行）；计划「留给后续层的约定」第 503 行；internal/store/check_authoring.go:37-47、94、111、222-237；pgstore/hooks.go:193、pgstore/templates.go:136、265

合并自：S10 should：spec 第 330 行「并在名字规则里拦下用户用它」有歧义

### S4-2　pgstore 包的测试要跑 137 秒，超过了计划定的一分钟上限

结果：选 B，已落实。runCases、eachAudited、runAudited 的子测试加了 t.Parallel；同时打开的测试仓储由信号量限在 4 个，pgtest.Open 一份（f7d05d8），pgstore 包内的 newSchema 一份（f0f30eb）。测锁等待和本身测并发的用例照旧串行：writeWhileHeld 那一类、L-6 的死锁测试、EnsureBuiltin 的并发装载用例。排查过共享状态：测试里没有给包级变量赋值，也没有 os.Setenv、os.Chdir。pgstore 整包从 142 秒降到 90 秒，-count=3 连跑三遍稳定；没有走到选项 C。原计划的一分钟线改写成实测数据与信号量的理由，并注明信号量只管一个测试进程，第 4、5、7 层也连库之后 go test 会同时跑好几个包。并发数为什么定 4、连接数怎么算，见「修补结果」与 f0f30eb 的提交说明。

决定：选 B（2026-10-08）。契约用例并行跑，用信号量限制并发，不够再考虑 C。这一条和「测试也用 PostgreSQL」是连在一起的：第 4、5 层的测试改用 PostgreSQL 之后，耗时还会在 137 秒上继续涨。只涉及计划第 440 行和测试代码，spec 条款不受影响。

问题：每个契约用例都要经过 harness.Open 新建一个 schema。接着要对这个 schema 里的 10 张表跑 AutoMigrate，做表结构断言，再装上用来注入失败的触发器。而且这些用例全部串行执行，internal/store 下面没有一处用到 t.Parallel。我在 HEAD 的克隆里关掉缓存重跑了一次 pgstore 包，用时 137.1 秒。这和验收时报告的 137 秒一致。计划任务 7 的「风险」写的是：单个包在一分钟以内就不用优化，超过了要报告。

影响：make check 里的 test 和 race 两步会把这个包各跑一遍。所以只要 internal/core 或 internal/store 有改动，光是 pgstore 这一个包就要四五分钟。两遍里带 -race 的那遍更慢。等第 4、5 层加了表、加了新的测试分组，耗时还会继续涨。如果几个代理同时跑测试，还会更慢。

选项：

A. 接受现状，把计划里的一分钟线改成一个新的上限，比如 3 分钟。代价：眼下虽然不用做任何改动，但以后每次改存储层或第 1 层都要多等，而且每加一层，耗时还会再涨。

B. 让契约用例并行跑，做法是在 runCases、eachAudited、runAudited 的子测试里加上 t.Parallel。每个用例本来就各用一个 schema，彼此不会干扰。早先有份报告说，测试 Test数据库错误的翻译 会替换包级的 tables，所以不能并行。这个说法在 HEAD 上已经不成立了，因为 pgstore 包里给 tables 赋值的只有 schema.go 一处。代价：并发数得限制。每个测试仓储最多开 4 个连接（maxTestConns），而本机的 max_connections 是 100，几个代理同时跑，连接就会被撑满。限并发有两种办法：可以在 pgstore 的 harness.Open 里加一个信号量，也可以在 Makefile 里给 go test 加 -parallel。writeWhileHeld 这类只在 PostgreSQL 上跑的测试仍要保持串行。另外还得逐个确认 storetest 里的用例没有共享状态，因为内存实现跑的也是这套用例。

C. 复用 schema。一种做法是每组用例只迁移一次，用例之间执行 TRUNCATE … RESTART IDENTITY CASCADE 清表。另一种做法是预先建好一池 schema，轮流使用。代价：测试夹具会变复杂。现在每个用例拿到的都是一个全新的空仓储，改了以后，这一点要靠清表来保证。只要漏清一张表，用例之间就会互相污染。另外，表结构断言和失败注入只会在建 schema 时跑一次。

这件事最好在第 3 层开工前定下来，因为第 3 层要改 internal/core，这同样会让 pgstore 的测试重跑。最晚要在第 4 层加表之前定。我倾向 B，用信号量限制并发；还不够的话，再考虑 C。

出处：计划任务 7「风险」第 2 条（plans/2026-10-03-rewrite-layer2-store.md:440）；internal/store/storetest/harness.go:53-58；pgstore/pgstore_test.go:36-37、136-186；Makefile:21-25

合并自：S4 concern「The contract subtests run sequentially, about 70 Opens」；S4 should「可选：runCases 与 eachAudited 依次 Open 约 70 次」；S8 concerns：The pgstore test package now takes about 100 s…；S8 should：pgstore 测试包这次实测 101.9 s…；S8 should：测试耗时：make check 实测 pgstore 包 102 s…；S9 concerns：pgstore package test time on this machine is about 144s…；S9 should：pgstore 包的测试耗时约 147 秒…；S9 should：pgstore 包的测试耗时：我在 probe 里跑 make check…；S10 concern：make check 里 pgstore 包的测试用了 128s（加 -race 是 145s）；S10 should：实现者报的 pgstore 测试耗时属实：我复测（不带 -race）是 127.075s（前半）

### L-8b　主机只要被任务引用过就永远删不掉，而主机又没有废弃状态

结果：按决定不加退役状态，模型和代码都没改，只补了 spec 的记录（73ac12c）。§2.7 补上装机历史：一台主机同一时刻最多一个未结束的任务（C31），上一个结束后可以再建，按主机列出的任务就是它的装机历史，失败后重试沿用同一个任务。§8 风险表加了两行，都标「接受（2026-10-08）」：主机没有废弃状态，被任务引用过就删不掉；被任务引用过的 ISO 删不掉，原始字节一直占着磁盘，由第 4 层设计导入与清理时处理。§5.1 第 5 步补了一句指向后者，C26 写明主机没有废弃状态。

决定：不加退役状态（2026-10-08），等于选 A，模型不用改。你定的是：一台主机可以在不同时间先后有多个安装任务，一个任务就是一次装机，每个任务的状态就是那一次的最终状态；这就是它的装机历史。因为不涉及给机器停机重启，只是新建任务，所以多个历史并存没问题。这一点现在的代码已经支持，另见上面「装机历史：现在的代码已经支持，缺三样」。选 A 的代价也在那里：主机永远删不掉的问题留着，ISO 那边同根的问题留给第 4 层。下面的选项 B 和 C 保留，只作记录。

问题：tasks.host_sn 上的外键是 RESTRICT。内存实现的 referencedBy 也一样，它把已经结束的任务也算作引用。契约测试 hostDeleteReferenced 还专门测了这一点：任务结束了，主机照样删不掉。另一方面，任务只增不删，存储契约里没有删除任务的方法。结果是，一台主机只要建过一次任务，就永远删不掉。spec C26 规定，被引用的对象「只能废弃，不能删除」。可是 model.Host 只有 SN、架构、固件、属性和 Rev 这几项，没有废弃状态。所以就算想按 C26 把主机废弃掉，也做不到。计划的错误表照着 C26 写了 ErrReferenced，但是没有讨论这个后果。这个问题是在给层间评审第 8 条（L-8）分类时顺带发现的。原来的条目没有把它单独列出来，所以这里拆成单独一条。

影响：有的机器退役了，有的报废了，还有的换了主板、SN 也跟着变了。这些机器的记录只能一直留在主机列表里，也一直出现在向导的候选里。换句话说，主机列表只增不减。第 5 层做主机管理时一定会碰到这个问题。

选项：

A. 维持现状。被任务引用过的主机永远留着，删除操作只对没建过任务的主机有用。代价：什么都不用改。但是运维没法清理退役的机器，只能靠界面上的过滤把它们筛掉。

B. 给主机加一个「退役」状态，做法参照 ISO 的废弃。退役的主机不能再被选来建任务，列表默认也不显示它。不过它的记录还留着，已经建好的任务仍然能查到它。代价：第 1 层要给 model.Host 加字段，准入判定要加一个原因代码。第 2 层要给 hosts 表加列，这一步 AutoMigrate 能做到。两份实现还要各加一个需要审计的方法，契约测试也要补用例。另外还得定下来：CSV 导入时遇到已经退役的 SN，该怎么处理。

C. 允许删除只被已结束任务引用的主机。这样做的依据是：任务建成时，主机的 SN 和属性就已经固化进快照了（C29），之后渲染不会再读主机表。具体做法是去掉 fk_tasks_host，或者把它改成不约束删除。同时，内存实现的 referencedBy 改成只看还没结束的任务。代价：这和 C26 的字面意思冲突，所以要改 spec。表结构的期望值也要改，不过因为还没部署，不用写升级动作。另外，任务列表里的主机可能已经不存在了，界面要能处理这种情况。

ISO 也有根源相同的问题。ISO 虽然可以废弃，记录留着本身也没问题，但是被任务引用过的 ISO 同样永远删不掉。spec §5.1 第 5 步说，ISO 的原始字节要「删 ISO 时一并删除」。既然 ISO 删不掉，这些原始字节也就一直占着磁盘。第 4 层设计 ISO 的导入与清理时，要把这件事一起考虑进去。

这件事最晚要在第 5 层写计划之前定下来。选 B 要改第 1、2 层，所以越早定，返工越少。我倾向 B。

出处：internal/store/store.go:33-34；pgstore/rows_tasks.go:30-33；pgstore/schema.go:129-144；storetest/hosts.go:340（hostDeleteReferenced）；internal/core/model/registry.go:12-24；spec C26（第 193 行）、§5.1 第 5 步（第 274 行）

合并自：L-8 分类时补充的「另外一点：主机没有废弃状态，被任何任务（包括已结束的）引用过，就永远删不掉」

### S10-3　spec 读起来像是 hook 也能派生，但存储契约里只有 DeriveTemplate

结果：选 A，已落实，只改文档（73ac12c）。§2.8 写明只有模板能派生，hook 不能；要把其中的 hook a 换成 b，先看 b 在不在，不在就新建一个，再在派生出的模板里按 §2.5 的「用 b 覆盖 a」原位替换。§5.2 原来的「hook 的流程相同」改写成 hook 的流程不同：不能派生，也没有正文、装配和预览这三步；在模板里换 hook 是第 3 步装配时做的。C24 开头加了「只有模板能派生，hook 不能」。C25 没动，见下面决定段里的更正。§7.2 第 6 条本来就符合这个读法，也没动。

决定：选 A（2026-10-08）。你说从来没设计过 hook a 派生出 hook b。要表达的是另一件事：对一个模板，如果要把其中的 hook a 换成 b，先检查 b 在不在，不在就先建一个，在就直接把 a 从模板里移出、把 b 插进来。所以 hook 没有派生，不加 DeriveHook。spec §2.5（第 116 行）已经定义了「用 b 覆盖 a」：在副本里原位替换，插槽和顺序不变，同名参数的取值沿用。改文档时要照这条写明是原位替换，别写成「移出再插入」，免得读成位置可以变。要改的是 §2.8（第 142 行）里「内置模板和 hook 只读……要改就先派生」，以及 §5.2 第 284 行「hook 的流程相同」，因为那个流程的第 1 步含派生。§7.2 第 6 条（第 368 行）本来就符合这个读法，不用改。

更正（2026-10-08）：这一段原先说 C25（第 192 行）里也有「hook……要改就先派生」，要一起改。这一点不对。C25 的原文是「内置模板和 hook 只读，出厂即已审批，随 lober 版本发布新版本，旧版本保留」，没有「派生」二字；引起误读的是 §2.8 和 §5.2。所以修补时 C25 没有动。

问题：spec §2.8 写的是「内置模板和 hook 只读……要改就先派生」。§5.2 又说「hook 的流程相同」，而这个流程的第 1 步就是新建或派生。两处连起来读，hook 好像也能派生。但是计划和存储契约里只有 DeriveTemplate，model.Hook 也没有 DerivedFrom 字段。C24 只说要「记下来源模板和版本」，提到的只有模板。§7.2 第 6 条的前端验收写的是「派生内置模板、换一个 hook 再审批」，照这句话看，hook 又像是不派生的。这几句话都是 spec 原来就有的，不是这一层引入的。但是它们决定了内置 hook 要怎么改。

影响：第 3 层写内置 hook 时不受影响。到了第 5 层做 hook 编辑，照哪一种读法都能写下去。问题在于，两种读法对应的接口和界面不一样。

选项：

A. hook 不能派生。spec §2.8 改成这样一句话：「要改模板就先派生；要改内置 hook，就另建一个 hook，在派生出的模板里换掉它」。§5.2 则写明 hook 没有派生。控制台可以提供「复制为新 hook」，用 GetHook 加 CreateHook 实现，不记来源。代价：只需要改文档。但这样一来，hook 就没有 C24 那种提示了：来源出了新版本时，不会提示差异。

B. 加一个 DeriveHook。model.Hook 要加 DerivedFrom 字段，golden 也要跟着更新。store 契约、两份实现和契约测试里，都要各加一份。hook_versions 表要加一列，这一步 AutoMigrate 能做到。代价：第 1、2 层要再返工一轮。

这件事最晚要在第 5 层写计划之前定下来。我倾向 A。

出处：spec §2.8（第 142 行）、§5.2（第 278、284 行）、C24（第 191 行）、§7.2 第 6 条（第 368 行）；internal/store/store.go:102-113；internal/core/model/authoring.go:129-131

合并自：S10 should：同文件 :142（§2.8）：「内置模板和 hook 只读……要改就先派生」可能被读成 hook 也能派生

## 2. 这些要在第 3 层开工前修好

这一节列的是实际存在的缺陷，或者缺了的保护。这些问题按计划和 spec 就能判断该怎么修，所以不需要你拍板。你只要同意修就行。

决定：28 条你全部同意修（2026-10-08）。修补计划在删掉 memstore 的前提下把 28 条逐条重判过：25 条已修；S6-3、S5-6、S6-4 的理由随 memstore 删除而消失，不修。标着「（两份实现不一致）」的 14 条，13 条去掉了标记，S9-1 改成「（pgstore 与 model 可能不一致）」。原来拿「两份实现会走偏」当论据的几条（S4-3、S4-4、S7-6、S5-5、S5-4，以及理由被削弱的 S4-7），为什么只剩 pgstore 也照样要修，写在各条的「结果」里。汇总见开头的「修补结果」。

### S7-1　写事务没有固定为 READ COMMITTED，谁改了默认隔离级别就会冒出 40001

结果：已修（5e58281）。write 改成 Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})；pgstore 的写入全都经 s.write（25 处调用），改这一处就覆盖全部。pgstore 的包说明和 spec §6.4 都写明了这个前提。测试：Test写事务不随连接默认的隔离级别变，在 DSN 上设 repeatable read，先 SHOW 确认参数生效，再跑一个 writeWhileHeld 用例，断言仍报 ErrConflict。

问题：pgstore 的 write 方法开事务时调的是 s.db.WithContext(ctx).Transaction(fn)，没有传 TxOptions。也就是说，事务用哪一级隔离，完全取决于连接的默认值。能改这个默认值的地方有好几处：DSN 参数，ALTER DATABASE/ROLE … SET default_transaction_isolation，还有 postgresql.conf。可是 pgstore 里有一串操作都依赖 READ COMMITTED 的两条性质。第一条是，FOR UPDATE 会等对方提交，然后读到最新的行；第二条是，每条语句都看得到刚提交的数据。依赖这两条性质的操作有：主机与 ISO 的乐观锁、hook 与模板版本的写入、任务推进、事件 Seq 的分配，以及建任务时加锁之后重跑的准入判定。

影响：S7 的探针在 DSN 上加了 default_transaction_isolation=repeatable read，结果有 24 个只在 PostgreSQL 上跑的子测试失败了，失败的这些返回的全都是没有翻译过的 SQLSTATE 40001。这里的「翻译」，指的是 pgstore 把数据库错误换成 DuplicateError、ErrReferenced 这类契约里的错误。这 24 个子测试里，测追加事件的那些本来应该成功。另外，批里有几台主机上都有别人还没提交的新任务时，建任务返回的错误里只列出了先撞上的那一台，没有逐台列出原因。同样的时序放在内存实现上，得到的是 ErrConflict 或者成功。第 5 层和第 7 层拿到 40001，会把它当成内部错误。运维只要为了「更安全」把库设成 serializable，就会触发这个问题。

建议：把 write 改成 Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})，并在 pgstore 的包说明里写明这个前提。另外在 pgstore_test 里加一个测试：用带 default_transaction_isolation=repeatable read 的 DSN 打开仓储，跑一个 writeWhileHeld 用例，断言它仍然报 ErrConflict。改动大约三行。

出处：internal/store/pgstore/open.go:84-88（write）

合并自：S7 should：…open.go:84 write 没有固定隔离级别…

### S4-5　没有人检查 SHA256 的格式，值太长时 pgstore 会原样报出 54000

结果：已修（7b9de91）。CheckISOText 规定 SHA256 只能是空串，或是 64 位小写十六进制，否则报 ErrInvalid，不论置为什么状态都查；超长的值在到达部分唯一索引之前就被挡下，不会再报出没翻译的 54000。store.go 里 UpdateISO 的注释写明了这条格式要求。测试：TestCheckISOText 补了大写、超长、非十六进制三个反例，契约用例 isoUpdateBadSHA256 同样是这三个。

问题：系统更新 ISO 时走的是 UpdateISO。UpdateISO 只用 CheckISOText 检查 SHA256 存不存得下，不检查它的格式和长度。ISO.Validate 也不看这个字段。

影响：S4 的探针用一个 4096 个字符的 SHA256 实测过。结果内存实现照收不误，pgstore 则撞上了部分唯一索引 idx_isos_sha256 的 btree 行长上限，返回 SQLSTATE 54000。54000 不在翻译表里，所以第 4 层和第 5 层只能把它当成内部错误。另外，大写的 64 位十六进制，或者 not-a-hash 这类值，两份实现都照收。于是，同一份内容只要 SHA256 的大小写不同，就会被当成两份。换句话说，C4 的去重全靠第 4 层永远写小写来保证。而且这个值不一定都是程序自己算出来的。isoSHA256 的注释写明，正在获取的 ISO 可能在字节到齐之前就记下 SHA256。那时记下的多半是操作员填的期望值（C3）。

建议：在两份实现共用的 CheckISOText（或 CheckISOUpdateFrom）里加一条规定：SHA256 只能是空串，或者符合 ^[0-9a-f]{64}$，否则报 ErrInvalid。测试方面，在 isoUpdateUnstorable 或 isoSHA256 里补三个反例：大写、超长、非十六进制。夹具里的 sha256Of 产出的本来就是 64 位小写十六进制，所以现有用例不受影响。

出处：internal/store/check_registry.go:91-145（CheckISOUpdateFrom、CheckISOText）；internal/core/model/registry.go:145、170-197；pgstore/rows_registry.go:36（idx_isos_sha256）；storetest/isos.go:373-408

合并自：S4 should「UpdateISO 不检查 SHA256 的写法」

### S8-1　两路同时首次装载同一份内置内容时，后到的一路会原样报出主键冲突 23505

结果：已修（5e58281），修了两轮，经过见「修补结果」。EnsureBuiltinHook 与 EnsureBuiltinTemplate 改成先确定并锁住身份，再查版本、比对内容：先用不带目标的 INSERT 身份 … ON CONFLICT DO NOTHING，再用 lockIdent 锁住；锁不到身份时在事务内按 name_key 查占用者，查到报 DuplicateError{name, 对方 ID}，查不到报 ErrConflict。后到的一路会等先到的提交，看得到它写下的版本，于是返回 nil。装载路径不会再撞名字索引，translate 的 holder 因此传 nil。新顺序只在 READ COMMITTED 下成立，所以排在 S7-1 之后做。测试：Test同时装载同一份内置内容后到的等先到的提交之后什么都不做、Test同时首次装载同一个新身份两路都没看到对方（跑 200 轮，另有「不同 ID 同名」一组）。

问题：EnsureBuiltinHook 和 EnsureBuiltinTemplate 的步骤是这样的。它们先不加锁地查一下版本在不在，用的是 hookVersionAt 和 templateVersionAt。查不到，才进 ensureBuiltinIdent 去锁住身份（也就是 hooks 或 templates 表里的那一行），身份不存在就新建。最后才插入版本。这样就会有两种撞车的情形。身份还不存在时，两路都会调 createIdent，后到的一路撞上 hooks_pkey（模板是 templates_pkey）。身份已经存在时，后到的一路等到锁以后，不再查一遍版本，而是直接插入。于是它撞上 hook_versions_pkey（模板是 template_versions_pkey）。这几个主键都不在 duplicateFields 里，所以 translate 把 23505 原样返回。S8 的探针在 READ COMMITTED 下把两种情形都复现了。契约（store.go:96-98、spec §6.2 第 11 条）写的是「已存在并且内容相同，什么都不做」。而内存实现是在一把锁下做的，第二次调用会返回 nil。

影响：有两种情况会让启动直接失败。一种是两个 lober 进程同时启动。另一种是第 5 层的装配点并发装载同一个 ID 的几个版本。不过按 spec 的设计，通知中心在进程内，也就是说只会跑一个实例。所以主要的风险在装配点并发装载这一边。

建议：把顺序倒过来，先确定身份，再查版本。具体做法分三步。先执行 INSERT 身份 … ON CONFLICT (id) DO NOTHING。再用 lockIdent（FOR UPDATE）锁住身份。锁住之后，才查版本并按内容比对。这样一来，后到的一路会等先到的一路提交，然后就看得到它写下的版本，于是返回 nil。如果名字已经被用户的身份占了，仍然由 idx_*_name_key 报 DuplicateError。测试方面，补两个 PostgreSQL 上的测试：一个针对新身份，一个针对已有身份的新版本。EnsureBuiltinTemplate 那边同样处理。如果不想改代码，可以退一步：在 EnsureBuiltin* 的接口注释里写明不能并发调用，由第 5 层的装配点按顺序装载。

出处：internal/store/pgstore/hooks.go:320-353、500-516；pgstore/templates.go:364-398；pgstore/errors.go:19-26

合并自：S8 concerns：Race windows I did not translate…（Two EnsureBuiltin calls racing on the same new ID 那一处）；S8 should：EnsureBuiltinHook / EnsureBuiltinTemplate：两次同时首次装载同一个新的内置 ID 时…；S8 should：两处没翻译的竞态，我核对过都可以接受…（EnsureBuiltin 那一处）；layer_should 第 4 条：pgstore 的 EnsureBuiltinHook 与 EnsureBuiltinTemplate 先查版本在不在，之后才锁身份

### S4-4　伪造的游标里有 null 元素时，审计列表和任务列表在两份实现上翻出的页不一样

结果：已修（5e58281）。DecodeCursor 遇到值为 null 的排序键报 ErrInvalid。只剩一份实现之后，「两份实现翻出的页不一样」这个论据不在了，但 pgstore 单独就是错的：审计和任务列表收下这种游标，会返回空页且 Next 为空，第 5 层回 200 和空列表，调用方以为翻到头了；其余列表静默从头翻，和 check.go 注释承诺的「改坏的都认得出来」不符。测试：TestCursor的编解码 补了整个排序键为 null、排序键全为 null、第二个排序键为 null 三例，契约用例 auditListInvalid 与 taskListInvalid 各补一例。

问题：提交 b61ba0a 已经把游标的编码和解码收拢到一处，也就是共用的 store.DecodeCursor。但是 json.Unmarshal 遇到 null 时既不报错，也不改动目标值。所以元素为 null 的游标照样会被收下。check.go 的注释说「改坏的……都认得出来，报 ErrInvalid」，实际行为和这句话不符。

而两份实现给排序键设的初值不一样，所以收下这种游标之后，结果也不一样。拿审计列表来说，用 [audit, null] 编出一个游标传进去。内存实现（memstore）会保留排序键的初值 MaxInt64，于是从头开始取，在探针克隆里实测取到 9 条。PostgreSQL 实现（pgstore）的初值是 0，查的就成了 id < 0，结果是 0 条。任务列表用 [tasks, null, null] 编出的游标也一样：一边返回全部，另一边返回 0 条。至于主机、ISO、hook、模板这几个列表，两份实现都会静默地从第一页重新开始。

影响：游标来自客户端，第 5 层原样往下传，不做任何改动。第 5 层的测试跑在内存实现上，生产环境跑的是 pgstore。所以同一个伪造的游标，在测试环境和生产环境里会翻出不同的页。

建议：让 DecodeCursor 遇到值为 null 的元素就直接报 ErrInvalid。两份实现共用这个函数，所以只改一处，两边同时生效。另外，storetest（两份实现共用的契约测试）里的各个 *ListInvalid 用例，都补一个含 null 的游标。

出处：internal/store/check.go:170-192（DecodeCursor）；pgstore/audit.go:28-33；pgstore/tasks.go:222-228

合并自：S4 should「decodeCursor 会收下 JSON null」

### S7-6　撞上唯一索引后，如果查不到是谁占着这个值，pgstore 就把 23505 原样返回

结果：已修（5e58281）。translate 里约束认得、holder 却查不到占用者时，改报 ErrConflict，调用方重试必然得到成功或 DuplicateError。只剩一份实现之后，这条不再是「对照内存实现只给两种结果」，而是契约自己的要求：并发撞车只给成功或 DuplicateError，第三种结果第 4、5 层只能当内部错误。translate、isos.go 两处、nameHolder、ErrConflict、UpdateISO、CreateHook、EnsureBuiltinHook 的注释都改了，EnsureBuiltin* 的注释写明报 ErrConflict 时由装配点重试。测试：Test数据库错误的翻译 里原来「查不到已有的那一个，原样返回」那一例，换成「查不到占着 SHA256 的那一个报 ErrConflict」「查不到占着名字的那一个报 ErrConflict」两例。

问题：pgstore 的一笔写入撞上 idx_isos_sha256 或 idx_*_name_key 之后，所在的事务就已经中止了。要把这个错误翻成 DuplicateError，pgstore 得在事务外面再查一次，找出占着这个值的那一方。可是对方可能恰好在这几毫秒里变了：它可能被改成了导入失败，可能被删掉了，也可能改了名。还有一种情况是对方的最后一个版本被删掉了，身份也跟着被删除。在这几种情况下，pgstore 都查不到对方，于是负责翻译数据库错误的 translate 就把 23505 原样返回了（errors.go:45-50）。

单元测试 Test数据库错误的翻译 里有一个用例叫「查不到已有的那一个，原样返回」，它固定下来的正是这个分支的现有行为。受影响的方法有 UpdateISO，以及 nameHolder 的全部调用方：CreateHook、CreateTemplate、DeriveTemplate、RenameHook、RenameTemplate、EnsureBuiltin*。

影响：内存实现用一把锁把写入串行起来，所以结果只会是两种之一：「写入成功」或者「DuplicateError」。pgstore 在这几毫秒的窗口里却会给出第三种结果。第 4、5 层拿到这种结果，只能当内部错误处理。不过这个窗口很窄。

这一条原先列为待拍板，我把它改成了工程问题。理由是这样的：计划里写的「认不出的原样返回、不要猜」，说的是认不出的约束。这里的约束是认得的，只是撞上的那一方刚刚变了。ErrConflict 的本意就是重读之后再判断，所以在这里报 ErrConflict 不算猜。

建议：查不到占用者时，pgstore 改报 ErrConflict。这样调用方重试之后，必然会得到成功或者 DuplicateError。改法是在 translate 里加一个分支。单元测试只需把那一例的期望从「原样返回」改成 ErrConflict，不用专门制造竞态。此外，还有三处要各补一句：ErrConflict 的注释、计划里的错误表、这几个方法的接口注释。

如果想和内存实现完全一致，也可以在 pgstore 里把整笔写入重跑一次，重跑时原来那笔事务已经回滚了。这样做的代价是，六处调用传进去的闭包都得先确认可以重入。

出处：internal/store/pgstore/errors.go:43-50；pgstore/isos.go:77-82（UpdateISO）；pgstore/hooks.go:538-548（nameHolder）；pgstore/schema_test.go:400；internal/store/errors.go:20-22

合并自：S7 concerns：The race fallback in UpdateISO is untested…（未测的一半已由 629c72e 的 Test同时写同一份内容由索引兜底报出对方 补上）；S7 should：UpdateISO 撞上 idx_isos_sha256 后会在事务外查出对方…；S8 should：两处没翻译的竞态，我核对过都可以接受…（nameHolder 那一处）

### S7-3　代码按 1000 行一批分批处理，但没有一个用例走到过第二批

结果：已修（7b9de91），12 处分批循环覆盖了 9 处。这里列的五个函数不全，grep batchSize 还有事件、hook、任务与模板里的几处。契约用例 hostImportManyBatches 一次导入 2001 台，走到第三批，断言每台的 Rev 都对、GetHosts 全部取到、审计条数等于台数；taskCreateManyBatches 用 1002 台主机建任务，走到第二批；另补 hookGetHooksManyBatches（GetHooks 传 1001 个键）与 eventManyBatches（一次追加 1001 条带 Key 的事件，走到查已有 Key 的第二批）。剩下 3 处与 hook 引用有关，要凑上千个 hook 版本，成本高，暂不覆盖。变异验证：让循环只跑第一批，只有对应的用例报红。

问题：pgstore 里有好几处代码都按 batchSize=1000 分批处理。它们是 ImportHosts、GetHosts、addAudits，以及建任务时用到的 lockHosts 和 openTasksOf。其中 ImportHosts 有两个分批循环，写入用一个，读回 Rev 用一个。契约测试和 pgstore_test 一次最多只用到 101 台主机（hostListDefaultLimit），所以所有分批循环都只走到了第一批。内存实现不分批，不受这个问题影响。

影响：第二批及以后可能出这几类错：写入时漏写，读回时漏读，跨批加锁时顺序错乱。只有在 PostgreSQL 上一次导入上千台时，这些错才会出现。open.go:31 的注释里专门提到了这个场景：用 CSV 一次导入几千台。但第 5 层的测试跑在内存实现上，看不到这类错。我读过这几处循环，眼下没看出错，只是这段代码从来没有在测试里执行过。

建议：在 storetest 的主机组里加一个用例，一次导入 1001 台以上，两份实现都跑。这个用例要断言三件事：每台返回的 Rev 都对，GetHosts 能全部取到，审计条数等于台数。至于一次给超过 1000 台主机建任务的用例，先看看耗时再定，暂时可以放在 pgstore_test 里。

出处：internal/store/pgstore/open.go:31-32；pgstore/hosts.go:100-122、171-189；pgstore/audit.go:13-19；pgstore/tasks.go:148-180；storetest/hosts.go:394-410

合并自：S7 should：ImportHosts、GetHosts 和 addAudits 都按 batchSize（1000）分批…

### S4-3　GetHost、DeleteHost、GetHosts 和各个 List 把存不进数据库的查找键和过滤条件当作没有，这条规则只在 pgstore_test 里有测试

结果：已修（7b9de91）。清单当时说删掉 memstore 后要重判这条，因为搬进 storetest 图的是让两份实现都跑。重判后照修：这条规则是第 4、5、7 层看得见的接口约定，/hosts/%FF、?prefix=%00 这类请求要得到「没有」，而不是没翻译的编码错误，所以要钉在契约用例里，并写进 store/doc.go；storetest 并进 pgstore 之后，契约用例本来就在 pgstore 包里。补全了 ISO 的 Family/Series、审计的 TargetKind/TargetID/Action、任务的 HostSN/States、hook 的 NamePrefix、模板的 Series；pgstore_test 里和契约用例重复的那半段删掉，只留契约用例没有的游标部分。测试：契约用例 hostUnstorableKeys（含属性值恰好是 U+FFFD 的一台），以及各组的列表过滤用例（isoListFilters、auditListFilters、taskListFilters、templateListSeries 等）。

问题：PostgreSQL 的 text 类型存不了 NUL 字符，也存不了非法的 UTF-8。所以 pgstore 在 GetHost、DeleteHost、GetHosts 和各个 List 方法里，会先用 storable() 检查一遍。遇到这类存不下的键和过滤条件，pgstore 一律当作没有。正因为这样，pgstore 返回的结果才和内存实现一致。

目前把这个行为钉住（也就是用测试固定下来）的，只有 pgstore 自己的一个测试：Test存不下的键与过滤条件当作没有。而且这个测试只覆盖了 SNPrefix、AttrKey/AttrValue、ISO 的 States 和审计的 Actor。storetest 是两份实现共用的契约测试，里面一个这样的用例都没有。我 grep 过，storetest 里的 NUL 和 0xFF 只出现在两类地方：写入时的反例，以及游标。

S4 做过一次变异验证，也就是故意改坏代码，看测试能不能发现。S4 把内存实现的 GetHost、ListHosts、ListAudit 改成遇到这类串就报 ErrInvalid。结果内存实现的契约测试照样全绿。S7 则在探针里把同一组断言写成了 storetest 函数，两份实现目前都能通过。换句话说，两份实现眼下的行为没问题，缺的是契约层的约束。

影响：像 /hosts/%FF、?prefix=%00 这样的请求，两份实现现在给出的结果一致，但这只靠 pgstore 自觉。第 5 层的 HTTP 测试又是跑在内存实现上的，所以任何一边改了这个行为，问题都只会在生产环境暴露出来。

建议：把 pgstore_test 里的那个用例搬进 storetest 并补全，然后删掉 pgstore_test 里的那份。补全时，每组查询都要各传一次 NUL 和 0xFF，下面这些过滤条件也要算进去：ISO 的 Family/Series、审计的 TargetKind/TargetID/Action、任务的 HostSN/States、hook 的 NamePrefix、模板的 Series。各个方法的预期结果如下：GetHost 和 DeleteHost 要报 ErrNotFound，并且要用 wantUnchanged 确认数据没变；GetHosts 只返回存在的那些主机；各个 List 不报错，Items 为空；FindOpenTask 则要报 ErrInvalid。另外，用例里要登记一台属性值恰好是 U+FFFD 的主机，按 0xFF 过滤时不能选中它。最后在 doc.go 里写上一句「存不下的查找键与过滤条件当作没有」。

出处：internal/store/pgstore/pgstore_test.go:204-242；pgstore/open.go:98-107（storable）；storetest 各组的查询用例（hosts.go、isos.go、audit.go、tasks.go、hooks.go、templates.go）

合并自：S4 deferred「契约测试没有钉住这种情况：查询条件或查找键是存不下的串」；S4 should「读路径收到存不下的文本时，两份实现会走偏」；S7 deviations：Added behaviour the plan doesn't list: keys and filters that can't be stored…；S7 concerns：storetest has no cases for unstorable keys or filters…（键与过滤条件那一半）；S7 should：storetest 没有「存不下的键与过滤条件」和「游标里藏着 NUL」这两类用例…（键与过滤条件那一半）

### S6-2　状态过滤传入空切片（不是 nil）时该怎么算，没有测试钉住，pgstore 一旦换成 GORM 的惯用写法就会出错

结果：已修（7b9de91）。types.go 写明切片的过滤条件（States）为 nil 与为空切片相同，都表示不按它过滤，不是「状态在空集合里」，理由是第 5 层把 JSON 里的空数组解出来就是空切片。ISO、hook、模板、任务四组过滤用例各补一条「状态给空切片等于不过滤」，hook 与模板另有 ApprovedOnly 时的一条。

问题：过滤条件是长度为 0 的切片时，两份实现都当作不过滤。S6 在探针里试过：在 ISO、任务、hook、模板四个列表上传空切片，两边都返回了全部记录。所以目前两边是一致的。但是 types.go 只写了「过滤条件为零值表示不按它过滤」。空切片并不是零值，所以这句话管不到它。我还 grep 过，没有一个用例传过空切片。

影响：GORM 的惯用写法是 States 不为 nil 时才加上 state IN ? 条件。pgstore 如果改成这种写法，传入空切片时会生成 IN (NULL)，结果一条记录都不返回。可是契约测试照样是绿的。而第 5 层把 JSON 里的空数组解码出来，得到的正好就是这种空的非 nil 切片，于是这会变成一个只在生产上出现的 bug。

建议：在 types.go 里写明，nil 和空切片都表示不按这个条件过滤。然后在 ISO、hook、模板、任务这四组过滤用例里，各补一条传空切片的用例。

出处：internal/store/types.go:63-64；pgstore/isos.go:160-182、pgstore/hooks.go:566-585、pgstore/tasks.go:229-248

合并自：S6 should「空的非 nil 过滤切片没钉住」

### S9-1　finishedStates 是一份手写的清单，没有测试保证它和 model 一致　（pgstore 与 model 可能不一致）

结果：已修（U10），用的是这里「更稳的做法」，没用「加一行断言」。model 导出 TaskStates，按状态机的顺序列出全部任务状态，每次返回副本；Valid 改成按它判断，免得 switch 里加了状态、清单漏了照样逃过。pgstore 的 finishedStates 由它按 Finished 算出，schema_test 的 candidates 也从它出发，另加了几个非法写法；契约用例 taskCreateAfterFinished、taskFindOpen 里遍历结束状态的循环改成遍历清单。这样 model 一加结束状态，CHECK 和索引谓词由 Test任务状态的CHECK与C31的索引都与model一致 报红，FindOpenTask 和建任务时查未结束任务的那条查询由契约用例报红。标题的标记改成「（pgstore 与 model 可能不一致）」而不是去掉：只剩一份实现，但 pgstore 和 model 之间仍可能对不上。测试：TestTaskStates按状态机的顺序列出全部状态、TestTaskStates列出了全部TaskState常量（从源码里找出全部 TaskState 常量来比对）、TestTaskStates每次给的是副本、TestTaskStateValid只认清单里的状态、Test查未结束任务用的结束状态由model的清单算出。

问题：pgstore 的 openTasksOf 和 FindOpenTask 要知道哪些状态算已结束，用的是一份手写的清单 finishedStates。内存实现用的则是 model.TaskState.Finished()。索引 idx_tasks_open 的谓词倒是有测试钉在 model 上（Test任务状态的CHECK与C31的索引都与model一致），finishedStates 却没有被钉住。而且那个测试里的 candidates 也是手写的。目前 finishedStates、idx_tasks_open 的谓词和 model.TaskState.Finished() 这三处是一致的。现有的三种结束状态，也都有契约用例 taskFindOpen 覆盖。

影响：model 以后可能会加一个结束状态，最可能加它的是第 7 层的状态机。到那时，pgstore 里有两处会同时和内存实现、和索引对不上。一处是 FindOpenTask，另一处是建任务时查未结束任务的那条查询。如果契约测试没法把任务推进到新状态，就测不出这个问题。

建议：在那个测试里加一行断言：finishedStates 恰好等于 candidates 里 Finished() 为真的那些状态。更稳的做法是让 model 导出一份全部任务状态的清单。candidates 和 finishedStates 都从这份清单出发，按 Finished() 算出来。

出处：internal/store/pgstore/tasks.go:17-19、171、203；pgstore/schema_test.go:318-362

合并自：S9 should：小问题：tasks.go:20 的 finishedStates 是「已结束的状态」的第三份手写清单…

### L-6　导入主机和建任务各自排了一遍加锁顺序，没有测试保证两边一致

结果：已修（7b9de91，测试文件补在 7829943）。抽出 hostLockOrder 决定给多台主机加锁的次序（按 SN 的字节序升序），ImportHosts 与 CreateTasks 都调它，注释写明凡在一个事务里给多台主机加锁都按它。测试：Test同时导入主机与建任务不会互相等死（hostlock_test.go），多轮同时跑主机重叠的 ImportHosts（入参顺序打乱）与 CreateTasks，断言没有 40P01、最终结果正确；它本身在测并发，保持串行。变异验证：把 ImportHosts 改成倒序加锁，3 次都在第 2 轮报 40P01。

问题：ImportHosts 先用 strings.Compare 给主机排序，再按排好的顺序 upsert。CreateTasks 加锁走的是 lockHosts。lockHosts 先用 slices.Sorted 排序，再在 SQL 里写 ORDER BY sn COLLATE C FOR UPDATE。现在两处都按 SN 的字节序升序加锁，所以行为是正确的。但这套排序逻辑是两边各写了一份。现有的并发测试只覆盖同一个方法自己跟自己并发，例如 Test同时导入同一组主机不会互相等死、concurrentBatches 等。storetest 和 pgstore_test 里都没有这样的用例：ImportHosts 和 CreateTasks 同时跑，而且两边涉及的主机有重叠。所以就算把 ImportHosts 改成倒序，现有测试也照样全部通过。内存实现只用一把锁，不涉及这个问题。

影响：两处的加锁顺序一旦走岔，而 CSV 导入又恰好撞上批量建任务，pgstore 就会返回没有翻译过的 40P01（死锁）。这个错误只在 PostgreSQL 上出现。第 5 层拿到它，只能报内部错误。

建议：在 pgstore 里抽出一个函数，专门决定给多台主机加锁时按什么次序。ImportHosts 和 CreateTasks 都改成调用它。这个函数的注释要写明：凡是给多台主机加锁，都按它排序。另外再加一条只在 PostgreSQL 上跑的测试。这条测试让 ImportHosts 和 CreateTasks 同时跑上多轮，两边用到的主机要有重叠。ImportHosts 的入参顺序也要打乱。测试要断言两点：一是没有出现 40P01，二是最终结果正确。

出处：internal/store/pgstore/hosts.go:100-107；pgstore/tasks.go:148-163；pgstore/pgstore_test.go:300-376；storetest/concurrency.go

合并自：layer_should 第 6 条：跨方法的锁顺序没有测试钉住

### S2-15　Detail 和 CreatedBy 的测试只拿 NUL 做反例，没有用非法 UTF-8

结果：已修（U11），只改测试。TestCheckAudit各字段的规则 加了「Detail 不是 UTF-8」，TestCheckNewTasks每条前提各一个反例 加了「CreatedBy 不是 UTF-8」，各用一个 0xFF 字节；原来的「CreatedBy 存不下」改名为「CreatedBy 含 NUL」。只剩一份实现之后，「内存实现照存、pgstore 撞上 22021」这个对照不在了，但校验一旦被改坏，pgstore 照样会撞上 22021、报出没翻译的错误，所以仍要补。

问题：有三处测试专门测「存不下」的值，也就是数据库存不进去的值。可它们的反例只用了 NUL，没有用非法 UTF-8。这三处是：check_test 里 CheckAudit 的 Detail（第 66 行）和 CheckNewTasks 的 CreatedBy（第 389 行），以及 storetest 的 taskCreateInvalid（tasks.go:337）。S2 的探针试过一次：把 Detail 和 CreatedBy 这两处的校验改成只查 NUL，结果 internal/store 和 memstore 的测试照样全部通过。不过两份实现目前调用的是同一个函数，所以眼下还没有出现不一致。

影响：这个校验一旦被改坏，内存实现会把非法 UTF-8 照样存下来，pgstore 却会撞上 22021，返回一个没有翻译过的错误。第 5 层的 HTTP 测试跑在内存实现上，所以测不出这个差别。不过，经过 JSON 传进来的字符串已经是合法的 UTF-8，这种情况在现实里不容易碰上，所以这条优先级低。

建议：在 TestCheckAudit各字段的规则 里加一例「Detail 不是 UTF-8」，在 TestCheckNewTasks 的反例里加一例「CreatedBy 不是 UTF-8」。两例各用一个 0xFF 字节，一共两行。

出处：internal/store/check.go:67、263；check_test.go:63-66、388-389；storetest/tasks.go:336-337

合并自：任务2 should：测试粒度（已在探针里验证）：CheckAudit 的 Detail 和 CheckNewTasks 的 CreatedBy 测「存不下」时只用了 NUL…；任务2 should：测试的边界缺口：…② CheckAudit 的 Detail、CheckNewTasks 的 CreatedBy 只测了 NUL…

### S4-7　SHA256 撞车的测试没覆盖写入的一方还停在「获取中」的情况

结果：已修（U11），只改测试。契约用例 isoSHA256 加了一例：B 停在获取中，SHA256 写成那个 ready 占位 ISO 的 SHA256，断言 DuplicateError{sha256, 对方 ID}，紧接着的 wantUnchanged 断言数据没变；TestHoldsSHA256 补了一行：acquiring 为 true。清单当时说这条在删掉 memstore 后要重判：原来的变异验证做在内存实现上，而 pgstore 有部分唯一索引兜底。重判后照修，成本只有几行：pgstore 预检查用的 sha256Holder 调的就是共用的 HoldsSHA256，新加的那一行直接钉住这个判定，契约那一例钉住对外给出的结果。

问题：isoSHA256 测的是 SHA256 撞车。撞车涉及两方，一方是占位的，另一方是写入的 B。测试里 B 的状态只设成过 importing 和 ready。获取中（acquiring）这个状态只出现在占位的一方。TestHoldsSHA256 的表里也没有 acquiring 这一行。S4 做过一次变异验证：让内存实现的 sha256Holder 遇到处于 acquiring 的写入方时，直接返回空串。结果契约测试照样全部通过。

影响：因为 pgstore 有部分唯一索引兜底，所以内存实现即使退化成上面那样，也只会比 PostgreSQL 宽松。但第 5 层的测试跑在内存实现上，看不到这个差别。要等上了生产环境，pgstore 才会报出 DuplicateError。这条优先级低。

建议：在 isoSHA256 里加一例，让 B 停在 acquiring，并把 B 的 SHA256 写成那个处于 ready 状态的占位 ISO 的 SHA256。这一例要断言返回 DuplicateError{sha256, 对方 ID}，并用 wantUnchanged 断言数据没有变。TestHoldsSHA256 再补一行：acquiring 为 true。

出处：internal/store/storetest/isos.go:373-408（isoSHA256）；internal/store/check_registry_test.go:209-225（TestHoldsSHA256）

合并自：S4 should「测试缺口（小）：SHA256 撞键时」

### S7-5　表结构检查不核对主键的名字，可翻译数据库错误时恰恰要靠 hosts_pkey 这个名字来认

结果：已修（7b9de91）。compareTable 检查主键时加了一条：主键的名字应等于表名加 _pkey，对不上就报出来，并写明怎么用 ALTER TABLE … RENAME CONSTRAINT 改回来。测试：Test主键改了名被查出来并拒绝启动。

问题：Open 时核对表结构用的是 compareTable。它检查主键时只比列，不比名字。而 translate 负责把 PostgreSQL 的错误翻译成 DuplicateError 这类错误，它恰恰要靠 duplicateFields 里登记的 hosts_pkey，才能认出这是 SN 撞车。Test唯一约束的翻译表与期望值一致 这个测试也只检查一件事：翻译表里的名字符合「表名_pkey」的写法，并不去查数据库里的主键实际叫什么。

影响：假如有人手工给主键改了名（ALTER TABLE hosts RENAME CONSTRAINT …），Open 照样能通过。之后 CreateHost 撞上已登记的 SN 时，pgstore 会原样返回 23505，内存实现返回的却是 DuplicateError。AutoMigrate 建出来的主键总是叫 <表>_pkey，只有手工改过名才会触发这个问题，所以这条优先级低。不过别的索引和约束都核对了名字，唯独漏了主键。

建议：在 compareTable 检查主键的那段里加一条判断：pk.Name 应该等于表名加 _pkey。名字对不上就报出来，并且写明怎么用 RENAME CONSTRAINT 改回来。schema_test 里再补一个主键被改过名的反例。这件事可以和 S7-4 一起做。

出处：internal/store/pgstore/schema.go:289-297；pgstore/errors.go:19-26；pgstore/schema_test.go:364-381

合并自：S7 should：…errors.go:22 翻译表靠 hosts_pkey 这个名字认出主键撞车…

### S6-1　任务推进被回滚以后，事件的 Seq 还连不连续，现在没有测试钉住

结果：已修（8e7e9be），用并包换来的白盒断言，比这里建议的「追加一条事件再断言」更直接。wantSeqCounter 直接查 tasks.last_seq，连同事件条数与 max(seq) 三者比对，用在推进被 C31 挡下、期望值对不上、撞上部分唯一索引三处。变异验证：把 last_seq 改成在事务外分配，新断言报红，原有的 wantTasksUnchanged 等一个都抓不到。

问题：pgstore 用计数列 last_seq 给任务的事件分配 Seq（序号）。last_seq 和任务状态写在同一条 UPDATE 里，也在同一个事务里。所以一次推进（把任务推到下一个状态）如果被 C31 挡下，它带的事件和计数会一起回滚。这个行为现在是对的，但没有测试钉住它。storetest 里的两个契约测试 taskTransitionConflict 和 taskTransitionOpenTask，在推进被拒以后都没有再追加事件，自然也就没有断言新事件的 Seq 紧接在已提交的那一条后面。last_seq 不是 model.Task 的字段，因此 wantTasksUnchanged 检查不到它。pgstore_test 里还有一个测试，名叫 Test推进撞上没提交的新任务由部分唯一索引兜底，它也只断言了「没写事件」。内存实现的情况不同。它的 Seq 是按事件个数算出来的，天然不会留下空号。

影响：如果 pgstore 以后改成在事务外分配计数，被回滚的推进就会留下空号。也就是说，Seq 中间会断掉一个号。这种空号会出现在生产环境上，违反「每个任务从 1 起、连续」这条约定。现有的测试里，只有 RunConcurrency 有一定概率碰上这种情况。这条的优先级低。

建议：在两个测试调用 wantTasksUnchanged 之后，各追加一条事件。然后断言这条事件的 Seq 等于该任务已有的事件数加 1。在 taskTransitionConflict 里，这个值是 1。

出处：internal/store/storetest/tasks.go:549-567（taskTransitionConflict）、618-641（taskTransitionOpenTask）；pgstore/tasks.go:281-319；pgstore/pgstore_test.go:765-794

合并自：S6 should「Seq 在回滚之后是否仍然连续」

### S4-6　store 包的接口注释没有跟上实现和契约测试

结果：已修（U9），只改注释和原计划「共用类型」表里「各查询类型」那一行（b61ba0a 时的第 90 行），行为和测试都没动；「与内存实现一致」这类旧说法先由 5dcfe69 清掉。七条逐条补进 store.go、types.go、errors.go、doc.go 和几处实现注释：① CreateHost 分清 ProblemsError 与不带问题清单的 ErrInvalid，ImportHosts 补上空批和没规范化的 SN；② UpdateISO 与 ErrImmutable 写明已废弃的 ISO 报 ErrImmutable，状态不认识或文本存不下报 ErrInvalid；③ types.go 写明字符串一律按字节序比较（pgstore 用 COLLATE "C"），ISO 按 Name 再按 ID、区分大小写，任务同一时刻按 ID 升序；④ doc.go 写明装结果的切片与 map 不为 nil，理由只留「第 5 层编成 JSON 是 [] 与 {}，nil 会编成 null」，Events 的说明写明追加事件不改任务的 State 与 Rev；⑤ NewHookVersion 写明新版本号是分配过的最大版本号加 1、删掉的草稿用过的号不再发，ListHookVersions 补上身份不存在报 ErrNotFound；⑥ CreateTasks、FindOpenTask、ListEvents、AppendEvents 补上各自的报法，已结束的任务照样能追加事件，事件的 Key 区分大小写；⑦ 那四处注释改成只有 DeleteHookDraft 读 template_hook_refs，审批模板与建任务用 HookRefs 现算。⑤ 里「spec 里也没写」那一半没有改 spec。

问题：我逐条对过 HEAD 的代码。下面这些行为只写在实现注释、提交说明或测试用例里。store 包的接口注释要么没写，要么写得不对：

① CreateHost 的注释说，SN 没有规范化时报 ProblemsError(ErrInvalid)。但实际上，合法却没规范化的 SN 报的是不带问题清单的 ErrInvalid，也就是没有包成 ProblemsError，这样做的原因写在 CheckHosts 里。ImportHosts 的注释漏了两点：一是空批会报 ErrInvalid；二是批里只要有一台主机的 SN 没规范化，整批就报不带问题清单的 ErrInvalid。

② UpdateISO 和 ErrImmutable 的注释都没写这条规则：已废弃的 ISO 不再接受系统更新，这时报 ErrImmutable。它们也没写另外两种情况：文本存不下，或者状态不认识，这时报 ErrInvalid。

③ types.go:66 的注释说，ISO 按名字的 key 排序。key 是名字规范化以后的形式。可是 ISO 的名字不做规范化，所以 ISO 没有 key。两份实现和 isoListPaging 实际上都是先按 Name 的字节序排，再按 ID 排。不只是 ISO，各个列表都按字节序比较，pgstore 里靠的是 COLLATE C，但这一点只写在 pgstore 的包说明和 storetest 里。任务列表的注释写了「再按 ID」，却没写明是升序。计划第 90 行写的也是 types.go 里这句话。顺带一提：ISO 的名称区分大小写，这一点和 hook、模板不同。如果界面以后要做成不分大小写，就得给 isos 表加一列 key。AutoMigrate 能加列，也能加新索引，所以推迟到那时再做也安全。

④ 查询结果里的切片和 map 不会是 nil。这条规则适用于 Page.Items、GetHosts 与 GetHooks 的结果，以及 ListEvents 与 AppendEvents 的结果。但它只写在 storetest 的包说明里，doc.go 的总则里没有。另外，追加事件不会改任务的 Rev。这一条也只写在 pgstore 的行类型注释里。

⑤ NewHookVersion 的注释只写了「下一个版本号」。它没写具体的规则：新版本号是分配过的最大版本号加 1，草稿删掉以后，它用过的号也不再发。deleteLast 和 deleteStale 这两个测试已经断言了这条规则，可是 spec 里和注释一样没有写。另外，ListHookVersions 和 ListTemplateVersions 遇到不存在的身份会报 ErrNotFound，注释里也没写。

⑥ 下面这几条行为，接口注释里都没写：CreateTasks 遇到 ISO 或模板版本不存在时，报 ErrNotFound；FindOpenTask 收到没规范化的 SN 时，报 ErrInvalid，这样做的理由只写在 CheckSN 里；ListEvents 查不存在的任务时报 ErrNotFound，afterSeq 为负数时报 ErrInvalid；已经结束的任务照样可以追加事件；事件的 Key 区分大小写。

⑦ 有四处注释都说，建任务时靠的是 template_hook_refs 这张表。第一处是 Templates 的说明（store.go:104-105），另外三处是 check.go:337、pgstore/schema.go:118 和 pgstore/templates.go:401。但实际上，两份实现建任务时都不读这张表，而是用 HookRefs 现场算，这是任务 9 偏离计划的地方。现在唯一读这张表的，是 pgstore 的 DeleteHookDraft。

影响：第 4、5、7 层的计划要照着接口注释来写调用方。如果照现在的注释写，写调用方的人就会被带偏，比如以为 ISO 按 key 排序，或者以为空批能通过。

建议：第 3 层开工前，把这些内容一次补进 store.go、types.go、errors.go、doc.go，以及上面提到的那几处实现注释。计划第 90 行也一并改准。这次只改注释，行为和测试都不动。S4-1 讲的 SN 口径，以及 L-7 里和注释有关的那部分，可以放在同一批里改。

出处：internal/store/store.go:24-32、41-50、72-74、92-93、102-105、131-162；types.go:63-67；errors.go:25-28；doc.go；check.go:336-339；pgstore/schema.go:118；pgstore/templates.go:400-401；pgstore/rows_tasks.go:26-27；计划第 90 行

合并自：S4 should「契约包的注释和现在钉住的行为对不上，store.go 与 types.go 不在任务 4 的文件清单里」；S4 should「契约包的文档落后于 storetest 钉住的行为」；S5 should「版本号永不重发是本任务定下的契约」；S6 should「这一轮由测试钉住的几条新契约，只写在提交说明和用例里」；S6 should「契约文档没跟上：本任务定下、并已由契约测试钉住的语义」；任务2 should：types.go 的分页顺序照抄了计划，但没写按什么比较…（注释部分）；任务2 should：排序规则没写比较方式（types.go 62–65 行）…（注释部分）；任务2 should：返回的集合在没有结果时是 nil 还是空切片或空 map，契约没定…（doc.go 总则部分）；layer_should 第 2 条：契约注释与实现、契约测试对不上，有几处

### S5-1　内置内容的名字一旦装进库里就再也改不了，但 spec 和注释都没写这一点

结果：已修，只改文档与注释。spec §6.2 第 11 条补上「名字随身份定下，以后的版本不能换名，换了名同样拒绝启动；要改名，只能换一个新 ID」（73ac12c）；ErrBuiltinMismatch、EnsureBuiltinHook、ensureBuiltinIdent 的注释同样写明（dbea587）；原计划「留给后续层的约定」的第 3 层那条随 U9 补上这一句。建议里「第 3 层要再加一条用例，把发布过的各个内置版本依次装进同一个库」，以及内置版本不能废弃、旧身份会一直出现在向导里的问题，仍归第 3 层。

问题：内置内容的名字记在身份上。这里的「身份」指一个 hook 或模板本身，它的各个版本都挂在身份下面。内置身份不能改名，改了会报 ErrImmutable。内置版本则更严，连废弃都不允许。

EnsureBuiltin 装载内置内容时，可能会遇到同一个 ID 的新版本换了一个名字。这种情况下，PostgreSQL 实现和内存实现里的 ensureBuiltinIdent 都会报 ErrBuiltinMismatch。契约测试 builtinEnsure 的 b3 用例专门钉住了这个行为。

把这几条合起来看，结论是：某个内置内容的名字，一旦在某个库里装载过，以后的 lober 版本就改不了它。要改名，只能换一个新 ID。结果是，旧身份和它已审批的版本会永久留在库里，旧名字也会一直被占着。

但是 spec §6.2 第 11 条只写了两种会让 lober 拒绝启动的情形：一是改了内容却没升版本号，二是名字已经被用户占了。ErrBuiltinMismatch 和 EnsureBuiltinHook 的注释也只提到一种情况，就是同一个 ID 与版本的内容不同。

影响：第 3 层的工作正是编写内置内容。写第 3 层的人照着现在的文字理解，会以为改名时升个版本号就行。如果真这么改了，全新的库可以照常启动，但装过旧版本的库在升级时会拒绝启动。也就是说，只在新库上开发，是测不出这个问题的。

这一条原先有两种归类：一组列为待拍板，另一组列为第 3 层开工前应修。我按开工前应修来处理。原因有两点。一是现在的行为已经由契约测试钉住了。二是以后如果想让内置内容能改名，可以把规则放宽，允许在装载更高的版本时改身份上的名字；这种放宽是向后兼容的，所以到时候再定也不晚。

建议：在 spec §6.2 第 11 条里补一句话。计划里「留给后续层的约定」一节的第 3 层部分，也补上同样一句。这句话要写明：名字随身份定下，以后的版本不能换名；换了名，同样会拒绝启动；要改名，只能换一个新 ID。ErrBuiltinMismatch 和 EnsureBuiltinHook 的注释也要跟着改。另外，第 3 层要再加一条用例，把发布过的各个内置版本依次装进同一个库。

同一个根源还带出了另一个问题：内置版本不能废弃。以后如果某个 lober 版本不再带某个内置模板，库里的旧身份仍然是已审批状态。结果是，它会一直出现在向导里。第 3 层设计装载流程时，要把这件事一并考虑进去，比如由第 5 层按程序当前带的内置清单来过滤。保留的名字前缀必须在第一份内置内容之前定下来，原因也在这里（见 S10-2）。

出处：spec §6.2 第 11 条（第 330 行）；计划「留给后续层的约定」第 503 行；internal/store/errors.go:37-39；store.go:96-98；pgstore/hooks.go:500-516；storetest/hooks.go:1133-1138、1156-1157

合并自：S5 should「「留给后续层的约定」的第 3 层可以补一条」；S10 should：同文件 :330（§6.2 第 11 条）：契约测试 builtinEnsure 还钉住了另一种情况；S10 should：spec 第 330 行（§6.2 第 11 条）漏了一条拒绝启动的情形

### S3-14　model.Subject 的写法表前后矛盾，还漏了两种情况：针对整个身份时和新建时该怎么填

结果：已修（dbea587），只改注释。problem.go 的 Subject 写法表修掉五处：① 参数声明与实现按位置写，取值来源的字段键是例外、带参数名；② 模板一节补上 Kind 为 template；③ sn_placeholder 另给规范化之后的 SN；④ param_unknown_field 的 Path 是原样的键；⑤ 补上改名时 ID 是身份 ID、新建与派生时 ID 为空这两种写法。S10-2 新加的 name_reserved 也写进了这张表：由仓储的用户入口产出，Subject 同 hook 与模板里名称的问题。

问题：model.Subject 由 Kind、ID、Path，以及正文里的行号 Line、列号 Col 几项组成，用来标明一条问题落在哪个对象的哪个位置。problem.go 里 Subject 的注释列了一张写法表，说明各种情况下这几项怎么填。这段注释还写明「第 5、6 层按它解析，以后不要随手改」。但是这张表有几处不对：

① 第 31 行说，系的列表和参数名只出现在说明里，不进 Path。可是第 39-41 行给出的写法是 tpl/<参数名> 和 <插槽名>/<n>/<参数名>，Path 里明明带着参数名，两处互相矛盾。第 31 行应该改成这样：参数声明和实现按位置写，即 param/<n>、impl/<n>，不写参数名，也不写系的列表；取值来源的字段键是例外，要带参数名。

② 模板一节讲到参数声明和模板取值来源时，没有写 Kind 是什么，实际的 Kind 是 template。而 hook 一节在对应的地方是写了的。

③ 第 47 行说，NormalizeSN 只给 Kind，SN 合法时另给 ID。这句不准确。实际情况是：sn_invalid 只给 Kind；sn_placeholder 还会另给一个 ID，这个 ID 就是规范化之后的 SN。

④ param_unknown_field 的 Path 是原样照搬提交上来的键。这个键不一定符合字段键的写法，注释里应该注明这一点。

⑤ 表里 hook 和模板的 ID 只列了一种写法，就是「ID@版本」。但是仓储（internal/store 这一层）实际上还会给出另外两种。

第一种出现在改名时，也就是 RenameHook 和 RenameTemplate。这时报出的名字问题，Subject 是 {hook 或 template, 身份 ID}，不带 @版本。

第二种出现在新建与派生时，也就是 CheckNewHook、CheckNewTemplate 和 DeriveTemplate。这时身份还没有 ID，所以名字问题和文本问题的 Subject 里，ID 都是空的。比如 text_not_storable 的 Subject 就是 {impl, 空 ID, impl/<n>} 这一类。

审计记录的 TargetID 已经写明：操作针对整个身份时，TargetID 就是 ID。Subject 的这张写法表却没有跟上。

影响：第 5、6 层要照这张表解析 Subject。它们如果遇到不带 @ 的 Subject，或者 ID 为空的 Subject，就会对不上。表里的矛盾如果一直留着，后面的人就会照着写错。

建议：只改注释。先把上面几处改过来，再补两行说明。一行写：针对整个身份的问题，ID 是身份 ID。另一行写：新建与派生时 ID 为空，Kind 与 Path 照版本级的写法填，也就是和针对单个版本时一样。

出处：internal/core/model/problem.go:25-47；model/ident.go:39-48；params/params.go:260-266；internal/store/check_authoring.go:94、111、243-253；pgstore/hooks.go:193；pgstore/templates.go:136、265

合并自：任务3 should：internal/core/model/problem.go:31：Subject 注释写「系的列表与参数名只出现在说明里，不进 Path」…；任务3 should：internal/core/model/problem.go:42–47 的 Subject 文档（C-4）写明第 5、6 层要按它解析…；layer_should 第 9 条：model.Subject 的写法表（problem.go:30-44）里 hook 与模板只有版本级的「ID@版本」

### S4-1　传进来的 SN 没规范化时，按 SN 查找的几个方法结果各不相同，契约里也没写

结果：已修（U9），只改注释，行为不变。store.go 的 Hosts 说明写明这条规则：带整条记录的写入（CreateHost、UpdateHost、ImportHosts，以及 CreateTasks）和 FindOpenTask 查 SN 规范化了没有，其余按 SN 查找的照原样去找，找不到就是没有；为什么写入和 FindOpenTask 要查、其余的不查，理由一并写明。FindOpenTask 的注释另写了网关不能把漏了规范化当成「没有任务」。原计划「留给后续层的约定」的第 5 层补了「按 SN 调仓储之前先经 model.NormalizeSN 规范化」。

问题：两份实现（内存版 memstore 和 PostgreSQL 版 pgstore）在这件事上的表现完全相同。S4 的探针在两份实现上都实测过，我也对过代码。假设 SN-1 已经登记过，再拿没规范化的 sn-1 去调用这几个方法，结果是这样的：GetHost 和 DeleteHost 会报 ErrNotFound；GetHosts 返回一个空 map；ListTasks 按 HostSN=sn-1 查，返回的是空页；FindOpenTask 和 UpdateHost 则报 ErrInvalid。

这些差异背后其实有一条规则。传入整条记录来写入的方法会检查 SN 有没有规范化，FindOpenTask 也会检查。其余按 SN 查找的方法都不检查。但是 store.go 里对这条规则一句都没写。FindOpenTask 为什么和别的查找方法不同，理由也只写在 check.go 里 CheckSN 的注释中。那段注释给的理由是：网关不能把漏了规范化当成「没有任务」，否则机器就会从本地盘启动，那个任务也就一直等下去。

影响：第 5 层的 HTTP 接口处理 /hosts/{sn} 这类路由时，要记得先用 model.NormalizeSN 把 SN 规范化。如果漏了这一步，请求里传的是 sn-1，拿到的就会是 404，而不是 400。

这一条原先列在待拍板里，我把它改成了工程问题。原因是把行为统一起来的两种办法都有毛病。一种是统一报 ErrInvalid。这样一来，GetHosts 收到的一批 SN 里只要有一个坏的，整批就会失败。这和 GetHosts 的约定冲突：按约定，它的结果里只放存在的那些。S4-3 里定下的口径是把存不下的 SN 当作「没有」，这个口径也得跟着改。另一种是统一当作找不到，但这又违背了 CheckSN 注释里写明的理由。所以保持现状最合适。

建议：在 store.go 里，给 Hosts 的说明和 FindOpenTask 都写明这条规则和背后的理由。另外，在计划的「留给后续层的约定」里，给第 5 层补一句「按 SN 调仓储之前先规范化」。这件事可以和 S4-6 一起改。

出处：internal/store/store.go:22-39、142-144；check.go:218-225（CheckSN）；check_registry.go:12-46（CheckHosts）；pgstore/hosts.go:136-189

合并自：S4 should「SN-1 已登记时，DeleteHost 与 GetHost 收到合法但没规范化的」；S6 should「按 SN 查询的约定不一致」

### S10-5　第 1 层清单里 L-8 提到的字段键问题已经随 C-3 修掉，但条目还没更新

结果：已修（U12），只改文档。第 1 层清单的 L-8 下面补了结果，第 18 行和第 3 节开头「其余 14 条没有动」的说法一并改掉。和这里的建议有一处不同：拼「ID@版本」的那几处不能改用 store.VersionID。第 1 层不许依赖 store，而 store 本来就依赖第 1 层，反过来引用会成环；所以结果里只写了在 model 给 HookKey、TemplateKey 加 String() 这一种做法。

问题：C-3（提交 036f397）已经把 TemplateFieldKey、InstancePath、InstanceFieldKey 放进了 model（problem.go:58-71）。现在 activation、params、snapshot 共用这一份。但是第 1 层遗留问题清单的 L-8（第 730 行）还写着「字段键在 params 与 snapshot 里各拼一次」。L-8 还建议「第 5 层做接口时，params 导出字段键构造函数」。另外，清单第 18 行和第 3 节开头（第 636 行）也还写着「其余 14 条没有动」。

影响：第 3 层写计划时要读第 1 层清单里 L-8 所在的这一节。如果照着 L-8 的建议做，就会在 params 里把字段键再造一份。不过 L-8 的其余部分仍然成立。有五处代码各自拼一份「ID@版本」（用 @ 把 ID 和版本连起来）：activation/hook.go、admission.go、params.go、snapshot/build.go，外加 store.VersionID。model 里的 HookKey 和 TemplateKey 都没有 String()。Subject.Kind 和 catalog 的标识也仍然是字面量。

建议：在 L-8 下面补一段处理结果。这段结果要写明三点。一是字段键已经随 C-3 修掉，放在 model 里，而不是 params 里。二是拼「ID@版本」的那几处，可以改用 store.VersionID，也可以在 model 里加 String()。三是 catalog 常量、全局词表和 Subject.Kind 常量，仍然留给第 3 层和第 5 层。清单第 18 行和第 636 行的说法也一并改掉。这一条只改文档。

出处：docs/superpowers/reviews/2026-10-02-layer1-open-issues.md:18、636、728-734

合并自：S10 should：清单 L-8（第 728-734 行）已部分过时

### S7-4　models 和 tables 应该一一对应，但这条约定只写在注释里，没有测试检查

结果：已修（7b9de91）。测试：Test行类型与表结构的期望值一一对应，断言 models 里十个行类型的 TableName() 集合等于 tables 的表名集合。

问题：open.go:23 的注释说，models 里的表和 schema.go 里的 tables 一一对应。models 是 open.go 里登记的行类型，tables 是 schema.go 里列出的表。checkSchema 负责断言表结构，但它只检查 tables 里列出的表。schema_test 里也没有测试去比对这两份清单。

影响：假设有人往 models 里加了一个行类型，却忘了在 tables 里登记。那么那张表的主键、索引、外键和 CHECK，就完全不在断言的检查范围内。这样一来，AutoMigrate 的两个盲区又没人守了，因为计划里说过，这些断言是唯一的防线。偏偏后面的第 4 层（ISO 来源）和第 5 层（用户与会话）都要加表。

建议：在 schema_test 里加一个用例。models 里一共十个行类型，每个都有 TableName()。这个用例取出这些表名，断言它们组成的集合和 tables 里的表名集合相等。改动大约十行，可以和 S7-5 一起做。

出处：internal/store/pgstore/open.go:23-29；pgstore/schema.go:58-60、251-269；pgstore/schema_test.go

合并自：S7 should：…open.go:27 models 与 schema.go 的 tables「一一对应」只写在注释里…

### S3-13　两个用例检查勾选的各系有没有并起来看，但只抓得住一个方向的退化

结果：已修（U11），只改测试。Test锁在默认值上却没有默认值 里的 inOneTicked 反过来写：splitPart 的第一份留空，volgroup 那一行放进第二份，引用落在模板勾的第二个系上，抓「只留第一个有引用的系」的退化；Test取值来源取不出正文或片段要的子值 里引用落在 rocky 的那一例（gw）不动，抓「只留最后一个系」的退化。两例的注释互相指明了方向。

问题：activation 里新加了两个用例，检查的是模板勾了几个系时，会不会把这几个系各自选中的片段并起来看。这里的「系」是模板勾选的对象，rocky 就是一个系；每个系会从每个 hook 里选中覆盖它的那份片段，而一份片段可以同时覆盖几个系。这两个用例里起决定作用的引用都在 rocky 的片段里，而 rocky 恰好是模板勾的第一个系。所以这两个用例只抓得住一种退化，就是代码只留最后一个系。S3 的探针试过另一个方向：把 template.go:224-227 改成只留第一个有引用的系（只在 refs 为空时才取）。结果 internal/core 的测试照样全部通过。

影响：假如代码真的退化成这样，勾了几个系的模板就可能带着错误通过批准。出错的情形是：排在后面的那个系，在片段里取了一个默认值给不出的子值。这个错误要等到给那个系的 ISO 建任务时才会暴露，而且是一台一台地报。第 3 层的内置模板多半是一个模板勾好几个系，这种情况正需要激活判定来把关。

建议：把 inOneTicked 这个用例反过来写，也就是 splitPart 的第一份留空，第二份放 volgroup 那一行。gw 那个用例不用动。这样两个方向就都钉住了。评审者核对过：反过来写的这个用例在当前代码上能通过，代码一换成只留第一个系的写法，它就会失败。这条只需要改测试。

出处：internal/core/activation/template_test.go:522-546、599-614；activation/template.go:206-228

合并自：任务3 should：internal/core/activation/template_test.go:524-546、599-614：第 1 轮必须修改项已按要求修好…；任务3 should：internal/core/activation/template_test.go:600：新补的两例都只往 rocky…

### S2-11　存储层的守卫不会把被构建约束排除掉的文件报出来

结果：已修（7b9de91）。checkStore 对 IgnoredGoFiles（含 _test.go）也报违规，规则的说明文字不再限于第 1 层；cgo 没有另加规则，「C 不算标准库」那一条已经挡住它。Test存储层总检查接上了每一项 的夹具在 5dcfe69 由 memstore 换成了 pgstore，现在断言 x_linux.go 和 x_windows_test.go 都被报出来。

问题：遇到被构建约束排除的文件，存储层守卫的检查函数 checkStore 只解析它们的 import，却不像第 1 层那样把这类文件本身当成违规报出来（第 1 层靠的是 ruleIgnored）。测试也是照这个行为写的：Test存储层总检查接上了每一项 这个测试专门断言，x_linux.go 和 x_windows_test.go 不会被报出来。这是任务 2 有意做的取舍，checkStore 的注释里写明了。

影响：假如 pgstore 里出现一个 *_linux_test.go，或者有人给测试文件加了一个 //go:build 标签，PostgreSQL 契约测试在 Windows 上就会静默地不跑，make check 却照样是绿的。计划「开工前」一节要防的正是这种情况，它的说法是「跳过会让存储层零覆盖也变绿」。pgstore 包现在跑一遍要 137 秒，所以有人想提速的时候，最容易走到的就是这一步。

建议：让 checkStore 也对 IgnoredGoFiles 报违规，_test.go 文件也算在内。对应规则的说明文字改成不只限于第 1 层。cgo 不用另外加规则，因为「C 不算标准库」这一条已经把它挡住了。guard_test 里的组合用例改成断言 x_linux.go 和 x_windows_test.go 这两个文件会被报出来。我用 go list 核过，存储层的四个包现在都没有被排除的文件，所以改完直接就是绿的。

出处：internal/archtest/deps_test.go:177-188（checkStore）；archtest/guard_test.go:321-347

合并自：任务2 deviation：Two layer-1 rules are not applied to the store layer…；任务2 should：存储层守卫只解析被构建约束排除的文件的 import，不把这类文件本身报出来…

### S5-5　往返读写的测试漏掉了参数声明和取值来源里的四个字段

结果：已修（U11），只改样例。做法和建议略有不同：sampleHook 没动，因为建任务的夹具用它，而 Rocky 的 ISO 不提供 profiles、必填的参数又没有值，参数求值会报错；另加了 fullHook，在 sampleHook 上多两个参数，一个列表参数声明为必填，一个 enum 的选项取自 ISO 提供的集合（Offer）。hookOps 的 full 和 hookCopies 改用它；fullTemplate 的取值来源设了 Editable 为 true。variants 里加了参数是否列表、是否必填、选项集合，以及取值来源可不可改四项。只剩一份实现之后，这条的理由从「两份实现的拷贝方式」换成 pgstore 自己：以后要是把其中哪个字段拆成单独的列，或者内置内容的比对改成逐个字段，这些字段会悄悄丢掉或漏比。

问题：ParamDecl 是参数声明，Binding 是取值来源。storetest 的样例数据里，ParamDecl.List、ParamDecl.Required、ParamDecl.Offer 和 Binding.Editable 这四个字段都是零值，这一点我 grep 过。其中 ParamDecl.Offer 只在那个存不下的反例里出现过。因为都是零值，深拷贝、往返读写（写进去再读出来比对）和 builtinEnsure 的比对，三处都没有覆盖到这四个字段。目前这四个字段还不会丢，因为 pgstore 把 model 类型整个编成 jsonb 来存，内存实现也是按结构体整体拷贝。

影响：将来如果 cloneDecls 这类手写的拷贝改成逐个字段构造，这些字段就会悄悄丢掉。pgstore 要是把其中哪个字段拆成单独的列，也会这样。这四个字段里，Editable 决定了向导里的参数能不能改（C20）。不过这条优先级低。

建议：在 full 里给一个参数设上 List 和 Required，把一个 enum 改成用 Offer，再给一条取值来源把 Editable 设为 true。variants 里也各加一项。

出处：internal/store/storetest/hooks.go:32-84（sampleHook、variants）；storetest/templates.go:39-100（sampleTemplate、fullTemplate、variants）

合并自：S5 should「往返读写漏了四个嵌套字段（探针证实）」

### S5-4　开新版本失败以后，版本号是否还连续，现在没有用例检查

结果：已修（8e7e9be），用并包换来的白盒断言。wantLastVersion 直接查身份表的 last_version，断言开新版本失败以后计数没跳号。只剩一份实现之后，不再拿内存实现的暂存作对照，直接看 pgstore 自己的计数列。变异验证：让版本号在失败以后仍加 1，新断言报红。

问题：两份实现都是在同一次写入里分配版本号的。内存实现在暂存里分配，pgstore（PostgreSQL 实现）在事务里分配，所以现在的行为是对的。但是有两个用例没有把这一点钉住，也就是没有用断言把它固定下来。第一个是 newVersionPending。这个用例里，开新版本先报了两次 ErrConflict，之后才开出一个版本，可用例并没有断言这个版本的版本号。第二个是 newVersionBad。这个用例在开新版本失败以后，就没有再开过新版本。S5 做过一次变异验证，也就是故意把实现改错，看测试能不能发现。这次的改法是：让内存实现的 NewHookVersion 在报出 ErrConflict 或 ErrNotFound 之后，另起一次写入，把计数加 1。结果契约测试（两份实现共用的那套测试）仍然全部通过。

影响：这种错误的后果是版本号中间会出现空洞，也就是跳过几个号。它不涉及数据本身。优先级低。

建议：在 newVersionPending 里断言开出来的是版本 2。在 newVersionBad 的末尾补一次成功的开新版本，同样断言开出来的是版本 2。一共只需要加两行断言。

出处：internal/store/storetest/hooks.go:959-993（newVersionPending、newVersionBad）

合并自：S5 should「版本计数器的原子性没被钉住（探针证实）」

### S4-8　Limit 传 0 时应该默认取 100 条，但只有 ListHosts 有用例检查这一点

结果：已修（7b9de91）。共用的辅助函数 wantDefaultLimit：准备 101 条数据，Limit 传 0，第一页 100 条且 Next 非空，第二页 1 条。ListISOs、ListAudit、ListHooks、ListTemplates、ListTasks 各补一例（isoListDefaultLimit、auditListDefaultLimit、taskListDefaultLimit，hook 与模板共用一份）。

问题：各个列表方法每次取多少条，用的都是共用函数 CheckQuery 返回的条数，所以两份实现目前是一致的。但是只有 hostListDefaultLimit 这个用例钉住了「Limit 为 0 时取 100」。ListISOs、ListAudit、ListHooks、ListTemplates、ListTasks 都缺一个对应的用例。这个用例要做的是：Limit 传 0，同时数据多于 100 条。

影响：假如某份实现出现了回退，比如写成了 Limit(q.Limit+1) 这样，现有用例发现不了。要等到库里真有很多数据时，问题才会暴露出来。优先级低。

建议：给每个列表方法补一个最小用例。用例里准备 101 条数据。第一页应该返回 100 条，并且 Next 不为空；第二页应该返回 1 条。这个用例可以写成一个辅助函数，让各组测试共用。

出处：internal/store/storetest/hosts.go:394-410（hostListDefaultLimit）；isos.go、audit.go、tasks.go、hooks.go、templates.go 的列表用例

合并自：S4 should「「Limit 为 0 时取 100」只在 ListHosts 上钉住了」

### S6-3　一个请求同时有两类问题时应该报哪一类错误，没有用例检查

结果：理由随 memstore 删除而消失，不修。这条怕的是某份实现调了检查顺序，同一个请求在测试里和在生产上拿到不同的状态码；现在只剩 pgstore 一份实现，第 5 层的测试也连 PostgreSQL，不存在两边不同。

问题：两份实现做检查的先后顺序是一样的。它们都是先查审计，再查入参，其中入参检查用的是共用函数。接着用键去取要用的数据，先取 ISO，再取模板。最后做重跑判定。推进任务时，两边的顺序也一样：先过 CheckTransition，再确认任务存在，然后核对期望值，最后检查 C31。S6 的探针把下面几种组合逐一核对过。传入的审计是零值、入参又不合格时，两边都报 ErrAuditRequired。ISO 和主机都找不到时，两边都报 ErrNotFound。事件不合法、期望值又对不上时，两边都报 ErrInvalid。身份（版本所属的那个带名字的对象）上有草稿、来源版本又不存在时，两边都报 ErrNotFound。也就是说，两份实现目前是一致的，但是没有用例把这些情况钉住。

影响：第 5 层的 HTTP 接口是按错误类别来映射状态码的。可是第 5 层的接口测试跑在内存实现上，生产环境用的却是 pgstore。所以哪天某份实现调整了检查顺序，同一个请求在测试里和在生产上就会拿到不同的状态码。优先级低。

建议：在 taskCreateInvalid、taskTransitionInvalid、taskCreateMissing 这三个用例里，各补一个组合输入的例子，并断言报出来的是哪一类错误。

出处：internal/store/pgstore/tasks.go:26-146、281-319；storetest/tasks.go:309-395、571-596

合并自：S6 should「检查的先后顺序没有用例钉住」

### S5-6　改名以后，单个取和批量取返回的名字对不对，测试没有断言

结果：理由随 memstore 删除而消失，不修。这条比的是两份实现各自的读取路径；现在只剩 pgstore，读版本的地方都经 joined 连上身份表带出现在的名字，读取路径只有一条。

问题：renameIdentity 这个用例在改名之后，只比对了 ListHookVersions 和 ListTemplateVersions 的结果。templateNamespaces 这个用例比对的也是列表。至于 GetHook、GetHooks、GetTemplate 这三个方法在改名之后返回什么名字，用例都没有断言。两份实现在读版本时，名字都是从身份上带出来的。内存实现靠的是 d.hook，pgstore 靠的是 joined 连表。所以两边目前是一致的。

影响：哪天某条读取路径改成从版本行里取名字，改名以后单个取和批量取就会返回旧名字。问题在于，现在的用例测不出这种情况。优先级低。

建议：在 renameIdentity 里，给 get 和 GetHooks 各补一处断言。

出处：internal/store/storetest/hooks.go:995-1015（renameIdentity）

合并自：S5 should「「返回的每个版本，Name 都是身份现在的名字」在改名之后只查了两处」

### S6-4　检查深拷贝的用例只改了 TransitionTask 的返回值，没有改用户发起推进的返回值

结果：理由随 memstore 删除而消失，不修。深拷贝用例在 pgstore 上永远是绿的：返回值每次从库里读出、从 JSON 解出，改 Go 里的值碰不到库里的行，两种推进以后拆开来写也一样。

问题：taskCopies 是检查深拷贝的用例，它会去改动方法返回的任务。但它只改动了 TransitionTask 的返回值，没有改 TransitionTaskByUser 的返回值。目前这样没有问题，原因有两个。一是在两份实现里，这两种推进走的都是同一个 transition。二是 pgstore 的返回值每次都是从 JSON 解出来的，天生就是一份新副本。

影响：以后哪份实现要是把两种推进拆开来写，就可能出现返回值和底层存储共用数据的情况。到那时，现有用例也测不出来。优先级最低。

建议：在 taskCopies 的末尾补调一次 TransitionTaskByUser，再 mutate（改动）它的返回值。只需要加一行。

出处：internal/store/storetest/tasks.go:753-790（taskCopies）

合并自：S6 should「taskCopies（tasks.go 740–776 行）只改动了 TransitionTask 返回的任务」

## 3. 这些问题归到后续各层去处理

这一节的条目，要么本来就是后续某一层的事，要么要等到后续那一层才真正用得上。写到那一层的计划时，把对应的条目纳入就行。

修补轮顺带做了几条：S7-7、S5-8 已修；L-7 的注释部分已改；S5-2、S5-3、S5-7 写进了原计划「留给后续层的约定」的第 5 层；S6-5 整条作废；L-8 的前提消失、建议仍成立；S2-22 只改了选项 ② 的措辞。这几条的标题下都有「结果」。S1-6、S2-12、S2-13、S3-10、S9-2、S2-24 没有动。

### S1-6　ValidID 的注释保证通过它的都是安全词，但这条保证只有 6 个样本在守

问题：ValidID 的注释里有一条保证，原话是「合它的都是安全词」。也就是说，凡是 ValidID 判为合法的字符串，IsSafeWord 也应该判为安全词。但测试里只有 6 个样本在钉住这条保证。S1 的探针试过：把 IsSafeWord 改成拒收以 - 结尾的词，结果 internal/core、internal/store、memstore 的测试全部照样通过。可这时 a- 仍然能通过 ValidID，却已经不是安全词了。

影响：眼下没有实际影响。原因是 {{sys:task}} 代入的任务 ID 都是 NewID 生成的，而测试 TestNewID生成的ID互不相同且合规则 已经拿 1000 个 ID 断言过 IsSafeWord。另外，param.go:346 那里已经写明：要不要拒收以 - 或 @ 开头的取值，留到第 3 层写内置模板之前再定。

建议：第 3 层改 IsSafeWord 的时候，有两种做法。一种是在测试 TestValidID逐个字符 里补断言：对每个字符拼出「字符加 a」和「a 加字符」两个串，只要 ValidID 判某个串为真，就同时断言 IsSafeWord 对这个串也为真。长度上限不用另外管，因为已经有一个 64 个 a 的样本把它钉住了。另一种是收窄 ValidID 注释里的这条保证。

出处：internal/core/model/ident_test.go:217-270；ident.go:142-149；param.go:346

合并自：任务1 should：…ident_test.go:217-270：ValidID 注释里「合它的都是安全词」这条保证，目前只靠 6 个样本…

### S2-12　守卫要求只有 pgstore 能用 gorm 和 pgx，但它只检查了 internal/store 下的包

问题：存储层守卫 Test存储层依赖守卫 只列出了 internal/store 下的包，而且不检查测试文件。不过现在模块里只有 internal/core、internal/store、internal/archtest，go.mod 的直接依赖也只有 gorm、driver/postgres、pgx v5。所以眼下这条守卫没有漏掉什么。

影响：第 3 层会新加渲染和内置内容的包（见 spec §4.3）。spec §6.1 要求这些包只依赖层 1。如果守卫不扩大检查范围，就查不到这些新包。

建议：从第 3 层开始，守卫改成把整个模块的包都列进来。对第三方模块，只允许 pgstore 使用 gorm 和 pgx。渲染和内置内容的包另外按「只依赖层 1」来检查。到第 5 层加控制台和 cmd/lober 时，照这套规则往下延伸。如果到那时 S2-11 还没做，可以一起做掉。

出处：internal/archtest/deps_test.go:161-175、451-476；计划「整体验收」第 4 条

合并自：任务2 concern：The guard checks only non-test files, as layer 1 does…；任务2 should：「只有 pgstore 可以依赖 gorm 与 pgx」现在只检查 internal/store 下的包…

### S2-13　「仓储不读时钟」是全局约定，但存储层没有守卫来检查它

问题：「仓储不读时钟」是写在 doc.go 里的全局约定。但是存储层的守卫里没有检查时钟的规则。不过眼下这还不算缺口：我 grep 过，internal/store 下的非测试文件都没有 import time，只有 pgstore_test.go 用到了它。另外，行类型里只有一个字段叫 CreatedAt，它的 GORM autoCreateTime 已经关掉了。而且 CheckNewTasks 本来就拒收值为 0 的 CreatedAt，所以就算不关，GORM 也不会去填它。

影响：以后可能有人在仓储里读时钟。也可能有人给行类型加一个字段，而这个字段名恰好会让 GORM 自动给它填时间。这两种情况，现在的 import 检查都看不出来。

建议：按 spec §6.1，第 3 层本来就要给渲染加时钟检查。到时候把同一条规则一起套到存储层上。这条规则只禁止读时钟的九个函数（也就是 ruleClock），time.Duration 照常放行。之后第 4、5 层给仓储加表时，再加一条断言，要求行类型不开 autoCreateTime 和 autoUpdateTime。

出处：internal/archtest/deps_test.go:439-449（checkStoreFile）；internal/store/doc.go:15；pgstore/rows_tasks.go:23-24

合并自：任务2 concern：「仓储不读时钟」 has no mechanical guard for the store layer…；任务2 should：「仓储不读时钟」是全局约定，但存储层的守卫没有时钟检查…

### S2-22　判断内置内容是否「内容相同」用的是 DeepEqual，所以 nil 和空切片会被当成不同

结果：没有动，仍归第 3 层。memstore 删除之后只剩 pgstore 的两处比对（EnsureBuiltinHook、EnsureBuiltinTemplate），选项 ② 原来写的「两份实现共用」「两份实现都要改」跟着改了措辞。

问题：内置内容指程序自带的那份版本，启动时要和库里的版本比对。两份实现（memstore 和 pgstore）的做法一样：先把 Rev 置为 1，再用 reflect.DeepEqual 比较库里读回的版本和程序自带的版本。jsonb 写进去什么，读出来就是什么，所以两份实现的比较结果是一致的。也就是说，这不是两份实现不一致的问题。

影响：风险出在跨版本升级上。假设程序升级以后，同一个内置版本只是把 nil 写成了空切片，或者反过来。这时语义并没有变，但比较照样会报 ErrBuiltinMismatch，程序会拒绝启动。契约测试都是从空库开始跑的，所以测不出这种情况。它只会在老库升级时冒出来，属于只在生产环境出现的那一类问题。

建议：第 3 层写内置内容时，在下面两种做法里选一种。

① 保持严格比较，另外加一条测试。这条测试给每个内置版本的序列化结果记一份指纹（golden）。同一个版本的指纹一旦变了，测试就失败，逼着人去升版本号。代价是多出一份要随版本更新的 golden，不过第 2 层不用动。

② 比较之前，先把 nil 和空的切片、map 规范成同一种形式。规范函数放在 store 里，pgstore 的 EnsureBuiltinHook 与 EnsureBuiltinTemplate 共用。代价是这两处比对都要改，而且 builtinEnsure 要加一个例子，断言「nil 与空切片算相同」。

我倾向 ①。

出处：internal/store/pgstore/hooks.go:320-353、templates.go:364-398；storetest/hooks.go:1099-1139；spec §6.2 第 11 条

合并自：任务2 should：计划把共用纯函数限定为九个…（EnsureBuiltin* 的「内容相同」，特别是 nil 与空切片怎么算）

### S3-10　架构清单的对齐测试只能确认「列出的都合法」，漏列的架构查不出来

问题：第 1 层清单的 C-9 给 host_platform_invalid 说明里列出的架构加了一个对齐测试，可它只能查到「列出的都合法、区分大小写」。换句话说，这个测试只确认说明里列出的每种架构都合法，并且比对时区分大小写，大写的写法不算合法。至于说明有没有漏列架构，它查不了，因为 catalog 没有导出全部架构的词表。

影响：如果目录里新加了一种架构，host_platform_invalid 的说明却没有跟着补上，这个测试是发现不了的。

建议：这条可以和第 1 层遗留问题清单 L-8 里「catalog 导出全局词表」那一项一起做，那份清单把 L-8 整条归在第 3、5 层。到时候 catalog 有了全局词表，就把这条对齐检查改成拿全集来比对。

出处：internal/core/model/codes_align_test.go:275；第 1 层遗留问题清单 L-8

合并自：任务3 concern：C-9 的架构清单只能查到「列出的都合法、区分大小写」…

### L-7　建任务时重跑准入，用的是任务里钉住的模板版本，和 admission 的注释说的不一样

结果：注释部分已改（U9），比对仍归第 5 层。admission 的 TemplateUsable 与 BatchInput.Template、store.go 里 CreateTasks 的第 3 步都写明：重跑准入用的是任务里钉住的版本，不另取当前版本，已被新版本取代、但仍是已审批的旧版本照样通过；它是不是当前版本，由第 5 层在提交时用 model.CurrentApproved 比对。仓储没有加锁，行为不变。

问题：两份实现（memstore 和 pgstore）在建任务时，都会重跑一遍准入。重跑时，它们按任务里钉住的 TemplateID/TemplateVersion 取出模板版本，交给准入检查。「钉住」指的是任务上记下了具体用哪个模板、哪个版本。但是 BatchInput.Template 和 TemplateUsable 的注释要求，交给准入的应该是调用方用 model.CurrentApproved 取出的版本。这样一来，如果操作员还停在控制台的建任务向导里，期间有新版本获批，他提交上来的仍是旧版本。旧版本此时还处于已审批状态，所以任务照样建得成。在这一点上，两份实现是一致的。

影响：C23 规定，向导里每个模板只提供它当前的已审批版本。现在守住这条规定的，只有向导自己的筛选。

这条原先列为待拍板，我把它改成了工程问题，理由如下。C23 和 §5.2 第 5 步都意味着，新版本批准之后，旧版本就不再用来建新任务，其中 §5.2 第 5 步写的是：新版本批准之前，旧版本照常可用。admission 的注释也写明，准入不会回落到更早的已审批版本，为的是免得操作员选的是当前版本，装出来的却是旧内容。另外，控制台是建任务的唯一入口，而 §5.3 第 6 步又要求服务端不信任前端的筛选。

建议：第 5 层在处理向导提交时，用 CurrentApproved 取出当前版本来比对。如果提交的版本已经被取代，就整批拒绝，并提示操作员换成新版本。需要的话，可以为此追加一个原因代码。仓储（也就是 store 这一层）保持现状，不另外加锁。比对和提交之间虽然会留下一个毫秒级的窗口，但这个窗口可以接受。注释现在就可以改，要改的是 admission 里的两处，以及 store.go 里 CreateTasks 的第 3 步。改后的注释要写明：重跑准入用的是任务里钉住的版本，它是不是当前版本，由第 5 层在提交时比对。这几处注释可以随 S4-6 一起改。如果你希望允许用旧的已审批版本建任务，那就只改注释，不加比对。

出处：internal/core/admission/admission.go:50-51、92；pgstore/tasks.go:103-113；internal/store/store.go:131-139；spec C23（第 190 行）、§5.2 第 5 步（第 282 行）、§5.3 第 3、6 步（第 292、295 行）

合并自：layer_should 第 7 条：CreateTasks 把任务钉住的模板版本原样交给准入

### S5-3　GetHooks 只要碰到一个格式不对的键，就整批报 ErrInvalid

结果：仍归第 5 层，GetHooks 保持严格。建议里要写进约定的那一条已随 U9 写进原计划「留给后续层的约定」的第 5 层：取草稿引用的 hook 之前，先滤掉不合 model.ValidID 的引用，缺的 hook 交给激活判定报 hook_ref_missing；两处以上要这样做时，再在 store 里加辅助函数。

问题：模板草稿里的 Instance.HookID 可以是任何存得下的字符串。HookRefs 负责列出草稿引用了哪些 hook，这样的 ID 它也照样列出来。可是两份实现的 GetHooks 只要遇到一个不符合 ValidID 的键，就整批报 ErrInvalid。storetest 里的 hookGetHooks 专门测了这个行为，把它固定了下来。仓储自己的审批和建任务倒不受影响：在探针克隆里，拿同一份引用了 Bad ID 的草稿去审批，两份实现报的都是 hook_ref_missing。

影响：问题会出在第 5 层。C22 规定草稿可以预览。第 5 层预览草稿，或者给编辑器跑激活判定时，最自然的写法是 GetHooks(HookRefs(draft))。可这样一写，只要草稿里有格式不对的 ID，两份实现都会返回 ErrInvalid。而 ErrInvalid 多半会被映射成 400，不会是激活判定给出的 hook_ref_missing。

这条原先列为待拍板，我改成了工程问题，因为 doc.go 里有一条总则：收到的 ID 先过 ValidID，不合就报 ErrInvalid。按这条总则，GetHooks 保持严格是对的。

建议：第 5 层在取草稿引用的 hook 之前，先滤掉不符合 ValidID 的引用。这样一来，缺失的 hook 就交给激活判定，由它报 hook_ref_missing。到时候如果有两处以上的代码要这样做，就在 store 里加一个辅助函数，只返回格式合法的引用。这个做法要写进「留给后续层的约定」里第 5 层那部分。

出处：internal/store/pgstore/hooks.go:254-276；storetest/hooks.go（hookGetHooks）；internal/store/check.go:336-351（HookRefs）；doc.go:16-17

合并自：S5 should「GetHooks 只要有一个键格式不对就报 ErrInvalid」

### S5-2　一份草稿被别的模板当作派生来源，删掉它以后，那些模板的 DerivedFrom 就悬空了

结果：按建议保持现状，仍归第 5 层。要写进约定的那一条已随 U9 写进原计划「留给后续层的约定」的第 5 层：派生来源可能已被删除，「来源出了新版本」的提示（C24）这时按同一来源身份下剩下的版本算，连来源的身份也没了就不提示。

问题：DeriveTemplate 可以拿任何状态的版本当来源，草稿也可以。DeleteTemplateDraft 删草稿时，不会去看别的模板的 DerivedFrom。探针实测过，两份实现都会放行这样的删除。于是，派生模板的 DerivedFrom 就指向了一个已经不存在的版本。如果删的是来源模板的最后一个版本，连来源模板的身份都不在了。不过 DerivedFrom 不会因此错指到别的内容上，因为用过的版本号不会再发，NewID 又是随机的。内存实现在 DeleteTemplateDraft 的注释里写明，这样做是有意的。另一方面，spec C26 规定，被模板或任务引用的对象只能废弃，不能删除；可草稿又不能废弃，计划里的 ErrReferenced 也只列了主机、ISO 和 hook 草稿。所以 spec 和计划这两处说法之间有缺口。

影响：第 5 层的编辑器有一个「来源出了新版本」的提示，出自 C24。这个提示要能处理来源已经不在的情况。

这条原先列为待拍板，我改成了工程问题，因为按 C24 的精神，派生出来的是一份独立的副本，DerivedFrom 只是记下出处。C26 说的引用是要用到对方内容的，DerivedFrom 不属于这一种。

建议：保持现状。在「留给后续层的约定」里第 5 层那部分写明：派生来源可能已经被删除；这时，提示改按同一来源身份下剩下的其他版本来算；如果连来源的身份也没了，就不提示。

出处：internal/store/pgstore/templates.go:272-298；spec C24、C26（第 191、193 行）；计划「错误」一节 ErrReferenced（第 101 行）

合并自：S5 concern「Deleting a template draft that another template was derived from is not blocked」

### L-8　ErrReferenced 只带一段文字，两份实现写的文字还不一样

结果：前提随 memstore 删除而消失，建议仍成立，仍归第 5 层；标题末尾的「（两份实现不一致）」也一并去掉了。现在只有 pgstore 一份实现，删被任务引用的主机或 ISO 时，报的文字都是从外键报错翻译来的，谈不上「两份实现写的文字不一样」。但第 5 层仍拿不到结构化的「被谁引用」，hook 草稿被模板挡着删不掉时指不出是哪个模板，所以带类型的 ReferencedError（Unwrap 之后是 ErrReferenced，带上引用方的类别和 ID）照旧在第 5 层做删除接口时加，由 pgstore 来填。

问题：删除一台被任务引用的主机，或者一个被任务引用的 ISO 时，两份实现都会返回 ErrReferenced，但附带的文字不同。内存实现写的是「主机 SN-1 被任务 xxx 引用」。pgstore 的这个错误是从数据库的外键报错翻译过来的，所以写的是「还有别的记录经外键 fk_tasks_host 引用它」。DeleteHookDraft 这一处两边是一致的：两份实现都会写出引用它的模板 ID@版本。

错误文字虽然两边不同，但契约里写明了，错误文字只给日志和开发者看。两份实现的错误类别是一致的，用 errors.Is 判断结果相同。契约测试也只比较类别，所以这条不放在第 3 层之前修。只要第 5 层不把 err.Error() 交给前端，两份实现就没有差别。

影响：第 5 层拿不到结构化的「被谁引用」信息。spec 总则要求「说清下一步」，第 5 层照这条做不到。具体来说，hook 草稿被模板挡着删不掉时，第 5 层没法指出是哪个模板。

建议：第 5 层做删除接口时，加一个带类型的 ReferencedError，它 Unwrap 之后是 ErrReferenced。这个错误里带上引用方的类别和 ID，两份实现都要填。pgstore 可以这样实现：先锁住主机或 ISO，再在同一个事务里查引用它的任务。因为建任务时要先给主机加写锁、给 ISO 加读锁，所以这样查到的任务不会有遗漏。外键照旧留着兜底。不过对主机和 ISO 来说，引用它的任务本身没有办法处置。所以这个新错误真正有用的地方是 hook 草稿那一处。主机永远删不掉的问题另见 L-8b。

出处：internal/store/pgstore/errors.go:51-58；pgstore/hooks.go:221-230；internal/store/errors.go:27-28

合并自：layer_should 第 8 条：ErrReferenced 只带文字，两边还不一样

### S9-2　tasks 表没有按 host_sn、iso_id、模板版本建的全表索引

问题：计划把索引的集合定死了。按这个集合，tasks 表只有 created_at 和 state 两个普通索引，外加一个 idx_tasks_open。idx_tasks_open 只收未结束的任务，不是覆盖全表的索引。删主机、删 ISO、删模板草稿时，PostgreSQL 要做 RESTRICT 外键检查。这个检查会按 host_sn、iso_id 和 (template_id, template_version) 去查 tasks 表。ListTasks 按 HostSN 过滤时也要这样查，而且查的范围包括已结束的任务。这些查询都会走顺序扫描，偏偏 tasks 表只增不删。

影响：数据量小的时候感觉不到。任务多了以后，删主机和按主机看任务都会越来越慢。

这条原先列在待拍板里，我把它改成了工程问题：按需要加索引就行。以后再加索引，对 AutoMigrate 来说是安全的，因为这是新增索引，不是改已有的索引。只是 schema.go 里的期望值要跟着改。如果在首次部署之前加，连升级动作都不用写。

建议：第 5 层做「按主机看任务」的接口，以及删主机、删 ISO 的接口时，给 tasks 表加三个普通索引，分别建在 host_sn、iso_id 和 (template_id, template_version) 上。要改的地方是 rows_tasks.go 里的 tag 和 schema.go 里的期望值，改完重跑断言测试。如果 L-8b 选了允许删除主机，host_sn 上的索引就更需要了。

出处：计划「表结构」一节（docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md:206-210）；internal/store/pgstore/schema.go:128-150；pgstore/rows_tasks.go:14-34

合并自：S9 concerns：Following the plan's fixed index set, there is no full-table index on tasks.host_sn…；S9 should：计划层面的观察，不是本任务的偏差：按计划定死的索引集合…；S9 should：计划层面：tasks 上没有 host_sn、iso_id、(template_id, template_version) 的全表索引…

### S2-24　DuplicateError.Field 和 TargetKind 的取值都没有导出成常量

问题：DuplicateError.Field 的取值是 sn、name、sha256，AuditEntry.TargetKind 的取值是 host、iso、hook、template、task。这些值现在还是直接写的字面量，散落在 memstore、pgstore 和 storetest 里。不过契约测试用 wantDuplicate 和对审计对象的断言钉住了两份实现的取值，所以两边不会走偏。

影响：第 5 层要按这些值映射出对应的说明，还要用它们拼审计查询的参数。到那时第 5 层只能把这些字面量再写一遍。

建议：等第 5 层用到时，再在 store 包里导出这些常量。这是纯追加的改动，做法和第 1 层遗留问题清单里的 L-8 一样。

出处：internal/store/errors.go:42-47；types.go:25-29

合并自：任务2 should：小处：…DuplicateError.Field（sn、name、sha256）与 AuditEntry.TargetKind…可以导出常量

### S5-7　驳回审批时不强制写理由，这件事留给第 5 层来管

结果：仍归第 5 层，仓储照旧不查 Detail。建议里要写进约定的那一条已随 U9 写进原计划「留给后续层的约定」的第 5 层：驳回接口要求非空的理由，并放进 Audit.Detail。

问题：两份实现在驳回时只检查审计记录是否合规，并不要求 Detail 有内容。也就是说，驳回时 Detail 可以是空的。但是 spec §5.2 第 5 步写的是「驳回要写理由」。

影响：doc.go 里约定了仓储层不做业务判定，所以这条规则只能靠第 5 层来保证。如果第 5 层也不管，驳回时就可以不留理由。

建议：第 5 层的驳回接口应该要求调用方给出非空的理由，并把理由放进 Audit.Detail。这条约定要写进「留给后续层的约定」里第 5 层的那部分。

出处：internal/store/store.go:80-81；pgstore/hooks.go:133-139；spec §5.2 第 5 步（第 282 行）

合并自：S5 concern「RejectHook and RejectTemplate do not require a reason」

### S5-8　「开新版本」和「装载内置模板」这两条路径都会写引用记录，但没有测试钉住

结果：已修（8e7e9be），用并包换来的白盒断言，没等第二个读者出现，走的是这里说的「另一个办法」。wantRefsRecorded 读整张 template_hook_refs，和每个模板版本按 store.HookRefs 算出的引用逐行比对，是全表的不变式，比只查单个版本更严；开新版本、装载内置模板这两条路径都在覆盖之内。变异验证：让 insertRefs 少记一条，新断言报红，原有的断言一个都抓不到。

问题：两份实现在这两条路径上其实都记下了模板对 hook 版本的引用。内存实现的 putTemplate 每次都会重新算一遍 HookRefs，pgstore 在这两处都调了 insertRefs。缺的只是用例。之所以缺，是因为从接口上看不出这两条路径有没有记下引用。如今读 template_hook_refs 这张表的只有一处，就是 pgstore 的 DeleteHookDraft。建任务时也不读这张表，两份实现都是用 HookRefs 当场算出引用（任务 9）。而这两条路径记下的引用，指向的 hook 版本都已经审批，永远删不掉，DeleteHookDraft 碰不到它们：

- 开新版本时，新版本的来源只能是已审批或已废弃的版本。来源版本引用的 hook 版本，在来源版本审批时就已经是 approved 了，否则审批会报 hook_ref_unapproved。而这样的 hook 版本永远删不掉。
- 装载内置模板时，内置模板只能引用内置 hook，而内置 hook 只会以已审批的状态装载。用户建的 hook，ID 由 NewID 随机生成，所以内置内容引用不到它们。

影响：眼下没有影响。但是等这张表有了第二个读者，这两条写入路径一旦漏记引用，就会出错。

建议：等后续层给这张表加第二个读者时，再补上用例。这个读者多半是第 5 层的一类查询，比如「哪些模板引用了这个 hook 版本」。另一个办法是在 pgstore_test 的 harness 里加一项不变式检查，检查每个模板版本在表里的行都等于 refRows 的结果。任务 8 的评审已经在探针里验证过这项检查。至于 store.go 里「建任务都靠它」这个说法，已经单独列在 S4-6。

出处：internal/store/pgstore/templates.go:119、395（refRows）；storetest/templates.go:427 起（templateRefs）

合并自：S5 should「引用表有两条写入路径没有测试钉住」

### S7-7　注释说连接池由装配点来调，但装配点其实调不了

结果：已修（f7d05d8），没等到第 5 层。pgstore.Open 改成 Open(ctx, dsn, opts ...Option)，新增 WithMaxConns：选函数式可选参数，是因为现有调用不用改，而且上限在用到第一个连接之前就设好了。注释改成连接池的上限由装配点从配置读出来、经 WithMaxConns 传入，不传就不设上限。装配点那一侧的约定随 U9 写进了原计划「留给后续层的约定」；装配点本身仍在第 5 层。测试：TestOpen按选项设连接池上限。

问题：pgstore 的 Open 上有一句注释，原文是：「连接池的大小用 database/sql 的默认值，由装配点按部署调整」。装配点就是 spec 里负责把各个模块接起来的 cmd/lober。可是 Store.db 没有导出，Open 也只接收 ctx 和 dsn 两个参数，所以装配点根本没有入口去调连接池。而 database/sql 默认的 MaxOpenConns 是不设上限的，测试夹具是自己另外把它设成了 4。

影响：到了第 7 层，会有大批主机同时回报。那时连接数可能把数据库的 max_connections 撑满，本机上这个值是 100。就算还没到第 7 层，这句注释眼下也已经不准确了。

建议：装配点要到第 5 层才第一次出现（spec §7.4），到时候再加一个调连接池的选项。做法可以是让 Open 接收连接池参数，也可以是给 Store 加一个设置方法。在那之前，注释可以先改成这一句：「目前用默认值，第 5 层的装配点会加选项」。

出处：internal/store/pgstore/open.go:16-19、34-35；pgstore/pgstore_test.go:152

合并自：S7 should：…open.go:33 注释说连接池大小「由装配点按部署调整」…

### S6-5　cloneTask 是逐个字段手写的，第 7 层往 Task 上加字段时需要有测试把关

结果：整条作废。它讲的是 memstore 的 cloneTask，随 memstore 删除（5dcfe69）一起没了。pgstore 没有手写的深拷贝：返回的任务每次从库里读出，改 Go 里的值碰不到库里的行（同 S6-4）。

问题：memstore 的 cloneTask 是逐个字段手写的深拷贝。S6 对照 model.Task 和各个快照类型核对过，确认它覆盖了现有的全部引用类型字段。如果在探针里把它改成浅拷贝，契约测试 taskCopies 能抓出来。

影响：按计划里「留给后续层的约定」，第 7 层要往 Task 上加一批执行字段，比如 token、引导次数和时机进度。如果新加的字段是 map、切片或指针，而 cloneTask 没有跟着改，taskCopies 是查不出来的，因为它只改动现有的字段。

建议：第 7 层加字段时，同时加一个反射测试。这个测试遍历 model.Task 里所有引用类型的字段，确认拷贝前后的两份数据不共享底层存储。这个测试也可以现在就加。

出处：原出处是内存实现里的 cloneTask，那个文件已随包删除（5dcfe69）

合并自：S6 should「cloneTask（memstore/tasks.go:235）逐字段手写深拷贝」

