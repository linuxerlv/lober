# 第 2 层遗留问题清单

日期：2026-10-07
对象：`feat/store` 上的 b61ba0a（第 2 层 10 个任务与验收修正）

10 个任务的实现者和评审者共留下 233 条记录。去重合并后剩 159 条，逐条对照 b61ba0a 的代码核实过，拿不准的在探针克隆里跑测试或做变异验证：

- 已被验收修正或后续任务解决，或属于合理偏差：68 条
- 纯表述问题，或没有实际影响：37 条
- 需要你拍板：4 条（第 1 节）
- 第 3 层开工前应修：28 条（第 2 节），其中 14 条是两份实现行为不一致
- 归后续层处理：15 条（第 3 节）

标「两份实现不一致」的条目要优先修：第 5 层的接口测试跑在内存实现上，不一致就会变成只在生产环境出现的问题，而这一层的契约测试本来就是为了防住它。

## 1. 需要你拍板

计划或 spec 本身有缺口、矛盾或取舍。每条列出可选项与代价，并给出建议和最晚什么时候要定。

### S10-2　保留名字前缀拦在哪没定，写进现有名字规则会让内置装不进

问题：spec §6.2 第 11 条和计划「留给后续层的约定」第 3 层都写了「先定一个只给内置内容用的名字前缀，并在名字规则里拦下用户用它」，但没定前缀是什么，也没定拦在哪一层。用户的新建、派生、改名（CheckNewHook、CheckNewTemplate、DeriveTemplate、RenameHook、RenameTemplate）和内置内容的装载（checkBuiltin）走的是同一条路：store.NormalizeName，再到 model.NormalizeName。

影响：照字面把拦截写进 model.NormalizeName，EnsureBuiltin 会对每份带前缀的内置内容报 ProblemsError(ErrInvalid)，启动被拒。契约测试里内置名字是 Builtin-Base 这类不带前缀的，测不出来。不定下来，第 3 层没法给内置内容起名。

选项：

A. 规则放第 1 层：model 给出「是不是保留前缀」的判定，外加一个新原因代码（如 name_reserved）。用户的几条路径经它拦下，内置内容的装载反过来要求必须带前缀（可以拆成用户与内置两个规范化入口）。代价：第 1 层加一条规则和一个代码；store 的共用检查、两份实现的派生与改名路径、契约测试都要跟着改。好处是判定规则仍只有第 1 层一份（spec §4.2），第 5 层预检时也能报出同样的原因。

B. 只在第 5 层控制台拦，仓储不管，EnsureBuiltin 撞名时报的 DuplicateError 当兜底。代价：改动最小；但规则不在名字规则里，与 spec 字面不符，不经控制台的调用方拦不住（眼下建用户内容的只有控制台）。

C. 不用前缀，内置与用户的名字各占一个空间，唯一索引改成 (builtin, name_key)。代价：表结构期望值要改（还没部署，不用写升级动作）；同名的内置与用户内容只能靠界面区分；C12 和 §4.2 的「名字唯一」要改写。

前缀本身写什么也要一起定（名字只收可打印 ASCII，1 到 64 个字符，不含空格）。最晚在第 3 层写第一份内置内容之前定。倾向 A。

出处：spec §6.2 第 11 条（第 330 行）；计划「留给后续层的约定」第 503 行；internal/store/check_authoring.go:37-47、94、111、222-237；pgstore/hooks.go:193、pgstore/templates.go:136、265（内存实现对应处同样）

合并自：S10 should：spec 第 330 行「并在名字规则里拦下用户用它」有歧义

### S4-2　pgstore 测试包 137 秒，超出计划的一分钟线

问题：每个契约用例都经 harness.Open 新建一个 schema，对 10 张表跑 AutoMigrate、做表结构断言、装失败注入触发器，而且用例全部串行（internal/store 下没有一处 t.Parallel）。我在 HEAD 的克隆里关掉缓存重跑了一次 pgstore 包，用时 137.1 秒，与验收时报的 137 秒一致。计划任务 7「风险」写的是单个包在一分钟以内就不用优化，超了要报告。

影响：make check 的 test 与 race 两步各跑一遍。只要 internal/core 或 internal/store 有改动，光 pgstore 一项就要四五分钟（-race 更慢）。第 4、5 层加表、加分组之后还会涨，几个代理同时跑会更慢。

选项：

A. 接受现状，把计划里的一分钟线改成新上限（比如 3 分钟）。代价：零改动；但每次改存储层或第 1 层都要多等，以后每层还会涨。

B. 契约用例并行：在 runCases、eachAudited、runAudited 的子测试里加 t.Parallel。每个用例本来就各用一个 schema，互不干扰。早先的报告说 Test数据库错误的翻译 会替换包级的 tables、不能并行，这在 HEAD 上已不成立（pgstore 包里只有 schema.go 一处给 tables 赋值）。代价：要限并发。每个测试仓储最多 4 个连接（maxTestConns），本机 max_connections 是 100，几个代理同时跑会撑满。可以在 pgstore 的 harness.Open 里加一个信号量，也可以在 Makefile 给 go test 加 -parallel。writeWhileHeld 这类 pg 专有测试保持串行；storetest 的每个用例都要确认没有共享状态（内存实现跑的是同一套）。

C. 复用 schema：每组只迁移一次，用例之间 TRUNCATE … RESTART IDENTITY CASCADE，或者预先建一池 schema 轮流用。代价：夹具变复杂；「每个用例一个全新的空仓储」改成靠清表来保证，漏清一张表，用例就会互相污染；表结构断言和失败注入只在建 schema 时跑一次。

最好在第 3 层开工前定，因为第 3 层改 internal/core 也会让它重跑；最晚在第 4 层加表之前定。倾向 B，用信号量限并发，不够再考虑 C。

出处：计划任务 7「风险」第 2 条（plans/2026-10-03-rewrite-layer2-store.md:440）；internal/store/storetest/harness.go:53-58；pgstore/pgstore_test.go:36-37、136-186；Makefile:21-25

合并自：S4 concern「The contract subtests run sequentially, about 70 Opens」；S4 should「可选：runCases 与 eachAudited 依次 Open 约 70 次」；S8 concerns：The pgstore test package now takes about 100 s…；S8 should：pgstore 测试包这次实测 101.9 s…；S8 should：测试耗时：make check 实测 pgstore 包 102 s…；S9 concerns：pgstore package test time on this machine is about 144s…；S9 should：pgstore 包的测试耗时约 147 秒…；S9 should：pgstore 包的测试耗时：我在 probe 里跑 make check…；S10 concern：make check 里 pgstore 包的测试用了 128s（加 -race 是 145s）；S10 should：实现者报的 pgstore 测试耗时属实：我复测（不带 -race）是 127.075s（前半）

### L-8b　主机被任务引用过就永远删不掉，又没有废弃状态

问题：tasks.host_sn 的外键是 RESTRICT；内存实现的 referencedBy 也把已结束的任务算进去（hostDeleteReferenced 钉住了「任务结束了也一样」）；任务又只增不删，契约里没有删任务的方法。结果是：一台主机只要建过一次任务，就永远删不掉。spec C26 说被引用的对象「只能废弃，不能删除」，可 model.Host 只有 SN、架构、固件、属性和 Rev，没有废弃状态，也就无从废弃。计划的错误表照 C26 写了 ErrReferenced，但没讨论这个后果。这一点是层间评审第 8 条（L-8）分类时顺带发现的，原条目没有单独列出，这里拆出来。

影响：退役、报废、换主板后换了 SN 的机器，只能一直留在主机列表和向导的候选里，列表只增不减。第 5 层做主机管理时一定会碰到。

选项：

A. 维持现状：被任务引用过的主机永远留着，删除只对没建过任务的主机有用。代价：零改动；运维清理不了退役的机器，只能靠界面过滤。

B. 给主机加「退役」状态，照 ISO 的废弃来做：退役的主机不能再被选来建任务，默认不在列表里显示，记录留给已建的任务查。代价：第 1 层 model.Host 加字段，准入判定加一个原因代码；第 2 层 hosts 表加列（AutoMigrate 能加列），两份实现各加一个要审计的方法，契约测试补用例；CSV 导入遇到已退役的 SN 怎么处理也要定。

C. 允许删除只被已结束任务引用的主机：任务建成时已把主机的 SN 和属性固化进快照（C29），渲染不再读主机表。做法是去掉 fk_tasks_host，或改成不约束删除，内存实现的 referencedBy 只看未结束的任务。代价：与 C26 字面冲突，要改 spec；表结构期望值要改（还没部署，不用写升级动作）；任务列表里的主机可能已经不存在，界面要能处理。

ISO 上也有同一个根子：ISO 能废弃，记录留着没问题，但被任务引用过的 ISO 永远删不掉，spec §5.1 第 5 步说「删 ISO 时一并删除」的原始字节也就一直占着磁盘，第 4 层设计导入与清理时要一起想。最晚在第 5 层写计划之前定；选 B 要动第 1、2 层，越早定返工越少。倾向 B。

出处：internal/store/store.go:33-34；memstore/hosts.go:91-106、memstore/tasks.go:196-209；pgstore/rows_tasks.go:30-33；pgstore/schema.go:129-144；storetest/hosts.go:340（hostDeleteReferenced）；internal/core/model/registry.go:12-24；spec C26（第 193 行）、§5.1 第 5 步（第 274 行）

合并自：L-8 分类时补充的「另外一点：主机没有废弃状态，被任何任务（包括已结束的）引用过，就永远删不掉」

### S10-3　spec 读着像 hook 也能派生，契约只有 DeriveTemplate

问题：§2.8 写「内置模板和 hook 只读……要改就先派生」，§5.2 又说「hook 的流程相同」（第 1 步就是新建或派生），读起来 hook 也能派生。但计划和存储契约只有 DeriveTemplate，model.Hook 没有 DerivedFrom；C24 只说「记下来源模板和版本」；§7.2 第 6 条的前端验收是「派生内置模板、换一个 hook 再审批」，又像是 hook 不派生。这几句是 spec 原有的，不是本层引入的，但它决定了内置 hook 怎么改。

影响：第 3 层写内置 hook 不受影响。第 5 层做 hook 编辑时，照哪种读法都能写下去，但两种读法对应的接口和界面不同。

选项：

A. hook 不能派生。§2.8 改成「要改模板就先派生；要改内置 hook，就另建一个 hook，在派生出的模板里换掉它」，§5.2 写明 hook 没有派生。控制台可以提供「复制为新 hook」，用 GetHook 加 CreateHook 实现，不记来源。代价：只改文档；hook 没有 C24 那种「来源出了新版本时提示差异」。

B. 加 DeriveHook：model.Hook 加 DerivedFrom（golden 要更新），store 契约、两份实现、契约测试各加一份，hook_versions 加一列（AutoMigrate 能加列）。代价：第 1、2 层再返工一轮。

最晚在第 5 层写计划之前定。倾向 A。

出处：spec §2.8（第 142 行）、§5.2（第 278、284 行）、C24（第 191 行）、§7.2 第 6 条（第 368 行）；internal/store/store.go:102-113；internal/core/model/authoring.go:129-131

合并自：S10 should：同文件 :142（§2.8）：「内置模板和 hook 只读……要改就先派生」可能被读成 hook 也能派生

## 2. 第 3 层开工前应修

实际的缺陷或保护缺口。按计划和 spec 能直接判断，不需要你拍板，只需你同意修。

### S7-1　写事务没固定 READ COMMITTED，改了默认隔离级别就报 40001　（两份实现不一致）

问题：pgstore 的 write 调的是 s.db.WithContext(ctx).Transaction(fn)，没带 TxOptions，隔离级别取连接的默认值。DSN 参数、ALTER DATABASE/ROLE … SET default_transaction_isolation、postgresql.conf 都能改这个默认值。主机与 ISO 的乐观锁、hook 与模板版本的写入、任务推进、事件 Seq 的分配、建任务锁住之后重跑准入，全都依赖 READ COMMITTED 的两条性质：FOR UPDATE 等对方提交后读到最新的行；每条语句都看得到刚提交的数据。

影响：S7 的探针在 DSN 上加了 default_transaction_isolation=repeatable read，24 个 pg 专有子测试失败，全部得到原样的 SQLSTATE 40001（其中追加事件本该成功）；建任务撞上主机上没提交的新任务时，只报出一台。同样的时序下，内存实现给 ErrConflict 或成功。第 5、7 层会把 40001 当成内部错误。运维为了「更安全」把库设成 serializable，就会触发。

建议：write 改成 Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})，并在 pgstore 包说明里写明这个前提。pgstore_test 加一例：用带 default_transaction_isolation=repeatable read 的 DSN 打开仓储，跑一个 writeWhileHeld 用例，断言仍报 ErrConflict。改动约三行。

出处：internal/store/pgstore/open.go:84-88（write）

合并自：S7 should：…open.go:84 write 没有固定隔离级别…

### S4-5　SHA256 不查写法，超长值在 pgstore 报原样 54000　（两份实现不一致）

问题：系统更新（UpdateISO）只查 SHA256 存不存得下（CheckISOText），不查格式和长度，ISO.Validate 也不看它。

影响：S4 的探针实测，4096 个字符的 SHA256，内存实现照收；pgstore 撞上部分唯一索引 idx_isos_sha256 的 btree 行长上限，返回 SQLSTATE 54000，不在翻译表里，第 4、5 层只能当内部错误。大写的 64 位十六进制、not-a-hash 这类值两边都照收，于是大小写不同的同一份内容会被当成两份，C4 的去重全靠第 4 层永远写小写。这个值不一定都是程序自己算出的：isoSHA256 的注释写明获取中的 ISO 可能在字节到齐之前就记下 SHA256，那时记下的多半是操作员填的期望值（C3）。

建议：在共用的 CheckISOText（或 CheckISOUpdateFrom）里规定 SHA256 只能是空串或 ^[0-9a-f]{64}$，否则报 ErrInvalid；isoUpdateUnstorable 或 isoSHA256 补大写、超长、非十六进制三个反例。夹具的 sha256Of 产出的已是 64 位小写十六进制，现有用例不受影响。

出处：internal/store/check_registry.go:91-145（CheckISOUpdateFrom、CheckISOText）；internal/core/model/registry.go:145、170-197；pgstore/rows_registry.go:36（idx_isos_sha256）；storetest/isos.go:373-408

合并自：S4 should「UpdateISO 不检查 SHA256 的写法」

### S8-1　同时首次装载同一份内置内容，后到的报原样主键 23505　（两份实现不一致）

问题：EnsureBuiltinHook 和 EnsureBuiltinTemplate 先不加锁地查版本在不在（hookVersionAt、templateVersionAt），查不到才在 ensureBuiltinIdent 里锁或建身份，最后插入版本。身份还不存在时，两路都会 createIdent，后到的撞上 hooks_pkey（templates_pkey）；身份已存在时，后到的等到锁之后不再查版本，直接插入，撞上 hook_versions_pkey（template_versions_pkey）。这几个主键都不在 duplicateFields 里，translate 原样返回 23505。S8 的探针在 READ COMMITTED 下两种情形都复现了。契约（store.go:96-98、spec §6.2 第 11 条）是「已存在并且内容相同，什么都不做」，内存实现在一把锁下第二次调用返回 nil。

影响：两个 lober 进程同时启动，或者第 5 层的装配点并发装载同一个 ID 的几个版本，启动就会失败。spec 的进程内通知中心意味着只跑一个实例，所以主要风险在装配点并发装载。

建议：先确定身份，再查版本。具体是 INSERT 身份 … ON CONFLICT (id) DO NOTHING，再 lockIdent（FOR UPDATE），锁住之后才查版本、按内容比对；后到的会等先到的提交，看得到它写下的版本，返回 nil。名字被用户的身份占了，仍由 idx_*_name_key 报 DuplicateError。补两例 pg 测试（新身份；已有身份的新版本），模板同理。若不想改代码，退一步在 EnsureBuiltin* 的接口注释写明不能并发调用，由第 5 层的装配点按顺序装载。

出处：internal/store/pgstore/hooks.go:320-353、500-516；pgstore/templates.go:364-398；pgstore/errors.go:19-26

合并自：S8 concerns：Race windows I did not translate…（Two EnsureBuiltin calls racing on the same new ID 那一处）；S8 should：EnsureBuiltinHook / EnsureBuiltinTemplate：两次同时首次装载同一个新的内置 ID 时…；S8 should：两处没翻译的竞态，我核对过都可以接受…（EnsureBuiltin 那一处）；layer_should 第 4 条：pgstore 的 EnsureBuiltinHook 与 EnsureBuiltinTemplate 先查版本在不在，之后才锁身份

### S4-4　元素为 null 的伪造游标，审计与任务列表两边结果不同　（两份实现不一致）

问题：游标的编解码已在 b61ba0a 收成共用的 store.DecodeCursor，但 json.Unmarshal 遇到 null 既不报错，也不改目标值，所以元素为 null 的游标照样被收下。这与 check.go 注释说的「改坏的……都认得出来，报 ErrInvalid」不符。两份实现给排序键的初值不同，结果也就不同：审计列表用 [audit, null] 编成的游标，内存实现保留初值 MaxInt64，从头取（探针得 9 条）；pgstore 初值是 0，查 id < 0，得 0 条。任务列表用 [tasks, null, null] 也一样，一边全部、一边 0 条。主机、ISO、hook、模板列表两边都静默地从第一页重来。

影响：游标经第 5 层原样来自客户端，伪造的游标在测试环境和生产环境给出不同的页。

建议：DecodeCursor 对值为 null 的元素直接报 ErrInvalid，一处改动两边生效；storetest 各 *ListInvalid 用例补一个含 null 的游标。

出处：internal/store/check.go:170-192（DecodeCursor）；memstore/audit.go:17-22；memstore/tasks.go:120-125；pgstore/audit.go:28-33；pgstore/tasks.go:222-228

合并自：S4 should「decodeCursor 会收下 JSON null」

### S7-6　撞唯一索引后查不到占用者，原样返回 23505　（两份实现不一致）

问题：撞上 idx_isos_sha256 或 idx_*_name_key 之后，事务已经中止，要在事务外查出占用者，才能翻成 DuplicateError。如果对方恰好在这几毫秒里改成了导入失败、被删掉、改了名，或者删掉了最后一个版本、身份随之删除，就查不到，translate 原样返回 23505（errors.go:45-50；Test数据库错误的翻译 里「查不到已有的那一个，原样返回」钉住的正是这一支）。涉及 UpdateISO，以及 nameHolder 的全部调用方：CreateHook、CreateTemplate、DeriveTemplate、RenameHook、RenameTemplate、EnsureBuiltin*。

影响：内存实现一把锁串行，结果只会是「写入成功」或「DuplicateError」二者之一；pgstore 在这个窗口里给出第三种结果，第 4、5 层只能当内部错误。窗口很窄。这条原先列为待拍板，我改成工程问题：计划写的「认不出的原样返回、不要猜」，说的是认不出的约束；这里的约束认得，只是撞上的那一方刚刚变了，按 ErrConflict 的本意（重读之后再判断）处理不算猜。

建议：查不到占用者时报 ErrConflict，调用方重试必然得到成功或 DuplicateError。改法是 translate 里加一个分支，单元测试把那一例的期望从「原样返回」改成 ErrConflict 即可，不用造竞态；ErrConflict 的注释、计划的错误表和这几个方法的接口注释各补一句。若想与内存实现完全一致，也可以在 pgstore 里把整笔写入重跑一次（事务已回滚），代价是六处调用的闭包都要确认能重入。

出处：internal/store/pgstore/errors.go:43-50；pgstore/isos.go:77-82（UpdateISO）；pgstore/hooks.go:538-548（nameHolder）；pgstore/schema_test.go:400；internal/store/errors.go:20-22

合并自：S7 concerns：The race fallback in UpdateISO is untested…（未测的一半已由 629c72e 的 Test同时写同一份内容由索引兜底报出对方 补上）；S7 should：UpdateISO 撞上 idx_isos_sha256 后会在事务外查出对方…；S8 should：两处没翻译的竞态，我核对过都可以接受…（nameHolder 那一处）

### S7-3　1000 行一批的分批路径没有用例走到第二批　（两份实现不一致）

问题：ImportHosts（写入和读回 Rev 各有一个分批循环）、GetHosts、addAudits，以及建任务的 lockHosts 与 openTasksOf，都按 batchSize=1000 分批。契约测试和 pgstore_test 一次最多 101 台（hostListDefaultLimit），所有分批循环都只走到第一批。内存实现不分批，不受影响。

影响：第二批起出的错（漏写、读回时漏读、跨批的加锁顺序错乱）只会在 PostgreSQL 上、一次导入上千台时出现。CSV 一次导入几千台是 open.go:31 的注释点名的场景，第 5 层跑在内存实现上的测试看不到。我读过这几处循环，眼下没看出错，但这段代码从没在测试里执行过。

建议：storetest 的主机组加一例，一次导入 1001 台以上，断言每台返回的 Rev 都对、GetHosts 全部取到、审计条数等于台数，两份实现都跑。建任务超过 1000 台的用例看耗时再定，可以先放在 pgstore_test。

出处：internal/store/pgstore/open.go:31-32；pgstore/hosts.go:100-122、171-189；pgstore/audit.go:13-19；pgstore/tasks.go:148-180；storetest/hosts.go:394-410

合并自：S7 should：ImportHosts、GetHosts 和 addAudits 都按 batchSize（1000）分批…

### S4-3　存不下的查找键与过滤条件只在 pgstore_test 里钉住　（两份实现不一致）

问题：PostgreSQL 的 text 不收 NUL 和非法 UTF-8。pgstore 在 GetHost、DeleteHost、GetHosts 和各个 List 里先过 storable()，把这类键和过滤条件当作没有，结果才与内存实现一致。钉住这一点的只有 pgstore 自己的 Test存不下的键与过滤条件当作没有，而且只覆盖 SNPrefix、AttrKey/AttrValue、ISO 的 States、审计的 Actor；storetest 一例都没有（我 grep 过，storetest 里的 NUL 和 0xFF 只出现在写入的反例和游标里）。S4 的变异验证：把内存实现的 GetHost、ListHosts、ListAudit 改成对这类串报 ErrInvalid，内存实现的契约测试照样全绿。S7 的探针把同一组断言写成 storetest 函数，两份实现今天都通过，缺的是契约层的约束。

影响：第 5 层的 HTTP 测试跑在内存实现上，/hosts/%FF、?prefix=%00 这类请求两边一致，只靠 pgstore 自觉；任何一边改了，都只在生产上暴露。

建议：把 pgstore_test 那一例搬进 storetest 并补全，然后删掉 pgstore_test 里那份。每组查询各放一次 NUL 和 0xFF（ISO 的 Family/Series、审计的 TargetKind/TargetID/Action、任务的 HostSN/States、hook 的 NamePrefix、模板的 Series 也算）；GetHost、DeleteHost 报 ErrNotFound 并 wantUnchanged；GetHosts 只给存在的；各 List 无错且 Items 为空；登记一台属性值恰好是 U+FFFD 的主机，按 0xFF 过滤不能选中；FindOpenTask 报 ErrInvalid。再在 doc.go 写一句「存不下的查找键与过滤条件当作没有」。

出处：internal/store/pgstore/pgstore_test.go:204-242；pgstore/open.go:98-107（storable）；storetest 各组的查询用例（hosts.go、isos.go、audit.go、tasks.go、hooks.go、templates.go）

合并自：S4 deferred「契约测试没有钉住这种情况：查询条件或查找键是存不下的串」；S4 should「读路径收到存不下的文本时，两份实现会走偏」；S7 deviations：Added behaviour the plan doesn't list: keys and filters that can't be stored…；S7 concerns：storetest has no cases for unstorable keys or filters…（键与过滤条件那一半）；S7 should：storetest 没有「存不下的键与过滤条件」和「游标里藏着 NUL」这两类用例…（键与过滤条件那一半）

### S6-2　空的非 nil 状态过滤没钉住，GORM 惯用写法会走偏　（两份实现不一致）

问题：两份实现都把长度为 0 当作不过滤。S6 的探针在 ISO、任务、hook、模板四个列表上传空切片，两边都返回全部，目前一致。但 types.go 只写了「过滤条件为零值表示不按它过滤」，空切片不是零值，也没有一个用例传它（我 grep 过）。

影响：pgstore 如果改成 GORM 的惯用写法（States 非 nil 才加 state IN ? 条件），会生成 IN (NULL)，一条都不返回，契约测试照样绿。第 5 层把 JSON 里的空数组解码出来，正好就是这种值，这就成了只在生产上出现的 bug。

建议：types.go 写明 nil 和空切片都表示不按它过滤；ISO、hook、模板、任务四组的过滤用例各补一条空切片。

出处：internal/store/types.go:63-64；memstore/isos.go:173-176、memstore/hooks.go:267、memstore/tasks.go:218-221；pgstore/isos.go:160-182、pgstore/hooks.go:566-585、pgstore/tasks.go:229-248

合并自：S6 should「空的非 nil 过滤切片没钉住」

### S9-1　finishedStates 是手写清单，没钉在 model 上　（两份实现不一致）

问题：pgstore 的 openTasksOf 和 FindOpenTask 用的是手写的 finishedStates，内存实现用的是 model.TaskState.Finished()。idx_tasks_open 的谓词由 Test任务状态的CHECK与C31的索引都与model一致 钉在 model 上，finishedStates 却没有钉住；那个测试的 candidates 也是手写的。今天三处一致，现有的三种结束状态也由契约用例 taskFindOpen 覆盖。

影响：model 以后加一个结束状态（最可能是第 7 层的状态机），pgstore 的 FindOpenTask 和建任务时查未结束任务的那条查询，就会同时和内存实现、和索引对不上；契约测试推进不到新状态时测不出来。

建议：在那个测试里加一行，断言 finishedStates 恰好是 candidates 里 Finished() 为真的那些。更稳的做法是由 model 导出全部任务状态的清单，candidates 和 finishedStates 都按 Finished() 从它算出来。

出处：internal/store/pgstore/tasks.go:17-19、171、203；pgstore/schema_test.go:318-362；memstore/tasks.go:169、189

合并自：S9 should：小问题：tasks.go:20 的 finishedStates 是「已结束的状态」的第三份手写清单…

### L-6　导入主机与建任务的加锁顺序各写一份，没有测试钉住　（两份实现不一致）

问题：ImportHosts 用 strings.Compare 排序后按顺序 upsert；CreateTasks 的 lockHosts 用 slices.Sorted，再加 ORDER BY sn COLLATE C FOR UPDATE。两处现在都是按 SN 字节序升序，行为正确，但排序各写了一份。并发测试只覆盖同一个方法自己并发（Test同时导入同一组主机不会互相等死、concurrentBatches 等），storetest 和 pgstore_test 里都没有主机重叠的 ImportHosts 与 CreateTasks 同时跑的用例。把 ImportHosts 改成倒序，现有测试照样全过。内存实现用一把锁，不涉及。

影响：两处的顺序一旦走岔，CSV 导入撞上批量建任务就会得到没翻译的 40P01。这只在 PostgreSQL 上出现，第 5 层只能报内部错误。

建议：在 pgstore 里抽一个给主机排加锁次序的函数，两处共用，注释写明凡是给多台主机加锁都按它。再加一条 pg 测试：多轮同时跑主机重叠的 ImportHosts（入参顺序打乱）与 CreateTasks，断言没有 40P01、最终结果正确。

出处：internal/store/pgstore/hosts.go:100-107；pgstore/tasks.go:148-163；pgstore/pgstore_test.go:300-376；storetest/concurrency.go

合并自：layer_should 第 6 条：跨方法的锁顺序没有测试钉住

### S2-15　Detail 与 CreatedBy 没有非法 UTF-8 的反例　（两份实现不一致）

问题：check_test 里 CheckAudit 的 Detail（第 66 行）和 CheckNewTasks 的 CreatedBy（第 389 行），以及 storetest 的 taskCreateInvalid（tasks.go:337），测「存不下」时都只用了 NUL，没有非法 UTF-8。S2 的探针把这两处改成只查 NUL，internal/store 和 memstore 全部通过。两份实现调用的是同一个函数，现在没有不一致。

影响：这里一旦改坏，内存实现会存下非法 UTF-8，pgstore 撞上 22021，返回没翻译的错误，第 5 层跑在内存实现上的 HTTP 测试测不出来。经 JSON 来的串已是合法 UTF-8，现实里不容易碰上，优先级低。

建议：TestCheckAudit各字段的规则 加「Detail 不是 UTF-8」，TestCheckNewTasks 的反例加「CreatedBy 不是 UTF-8」，各用一个 0xFF 字节，两行。

出处：internal/store/check.go:67、263；check_test.go:63-66、388-389；storetest/tasks.go:336-337

合并自：任务2 should：测试粒度（已在探针里验证）：CheckAudit 的 Detail 和 CheckNewTasks 的 CreatedBy 测「存不下」时只用了 NUL…；任务2 should：测试的边界缺口：…② CheckAudit 的 Detail、CheckNewTasks 的 CreatedBy 只测了 NUL…

### S4-7　SHA256 撞车没测写入的一方停在获取中　（两份实现不一致）

问题：isoSHA256 里写入的一方 B 只置为 importing 和 ready，获取中（acquiring）只出现在占位的一方；TestHoldsSHA256 也没有 acquiring 这一行。S4 的变异验证：让内存实现的 sha256Holder 对 acquiring 的写入方直接返回空串，契约测试全绿。

影响：pgstore 有部分唯一索引兜底，这类回退只会让内存实现比 PostgreSQL 宽松。第 5 层跑在内存实现上的测试看不到，到生产上才报 DuplicateError。优先级低。

建议：isoSHA256 加一例「B 停在 acquiring、写入 ready 那个的 SHA256」，断言 DuplicateError{sha256, 对方 ID} 并 wantUnchanged；TestHoldsSHA256 补一行 acquiring 为 true。

出处：internal/store/storetest/isos.go:373-408（isoSHA256）；internal/store/check_registry_test.go:209-225（TestHoldsSHA256）；memstore/isos.go:145-155

合并自：S4 should「测试缺口（小）：SHA256 撞键时」

### S7-5　表结构断言不比主键名，翻译却靠 hosts_pkey　（两份实现不一致）

问题：compareTable 只比主键的列，不比名字；translate 却靠 duplicateFields 里的 hosts_pkey 认出 SN 撞车。Test唯一约束的翻译表与期望值一致 只检查翻译表里的名字符合「表名_pkey」的写法，不查库里实际的名字。

影响：有人手工给主键改了名（ALTER TABLE hosts RENAME CONSTRAINT …），Open 照样通过；CreateHost 撞上已登记的 SN 时，pgstore 报原样的 23505，而内存实现报 DuplicateError。AutoMigrate 建的主键总是叫 <表>_pkey，只有手工改动才会触发，所以优先级低；但别的索引和约束都比名字，只漏了主键。

建议：在主键分支加一条判断：pk.Name 应为表名加 _pkey，对不上就报出来，并写明 RENAME CONSTRAINT 的改法；schema_test 补一个改名的反例。可以和 S7-4 一起做。

出处：internal/store/pgstore/schema.go:289-297；pgstore/errors.go:19-26；pgstore/schema_test.go:364-381

合并自：S7 should：…errors.go:22 翻译表靠 hosts_pkey 这个名字认出主键撞车…

### S6-1　推进被回滚之后事件 Seq 是否连续没有钉住　（两份实现不一致）

问题：pgstore 的计数列 last_seq 和状态在同一条 UPDATE、同一个事务里写，被 C31 挡下的推进连同带的事件、计数一起回滚，现在是对的。但 taskTransitionConflict 和 taskTransitionOpenTask 被拒之后都没有再追加事件、断言 Seq 接着已提交的那一条。last_seq 不是 model.Task 的字段，wantTasksUnchanged 看不见它；pgstore_test 的 Test推进撞上没提交的新任务由部分唯一索引兜底 也只断言「没写事件」。内存实现的 Seq 由事件个数算出，天然不会留空号。

影响：计数一旦改成在事务外分配，被回滚的推进就会在生产上留下空号，违反「每个任务从 1 起、连续」；现在只有 RunConcurrency 在概率上碰得到。优先级低。

建议：两处 wantTasksUnchanged 之后各追加一条事件，断言它的 Seq 等于该任务已有的事件数加 1（taskTransitionConflict 里为 1）。

出处：internal/store/storetest/tasks.go:549-567（taskTransitionConflict）、618-641（taskTransitionOpenTask）；pgstore/tasks.go:281-319；pgstore/pgstore_test.go:765-794

合并自：S6 should「Seq 在回滚之后是否仍然连续」

### S4-6　store 契约注释落后于实现与契约测试

问题：我逐条对过 HEAD，下面这些只写在实现注释、提交说明或用例里，store 包的接口注释没有写，或者写得不对：

① CreateHost 说 SN 没规范化报 ProblemsError(ErrInvalid)，实际上合法却没规范化的 SN 报的是不带问题的 ErrInvalid（CheckHosts 写了原因）。ImportHosts 没写空批报 ErrInvalid，也没写有一台没规范化时整批报不带问题的 ErrInvalid。

② UpdateISO 和 ErrImmutable 都没写「已废弃的 ISO 不再接受系统更新，报 ErrImmutable」，也没写文本存不下、状态不认识报 ErrInvalid。

③ types.go:66 说 ISO 按名字的 key 排。ISO 的名字不规范化，没有 key，两份实现和 isoListPaging 都是按 Name 的字节序、再按 ID。各列表按字节序比较（pgstore 用 COLLATE C）只写在 pgstore 包说明和 storetest 里；任务的「再按 ID」没写是升序。计划第 90 行是同一句。顺带一提：ISO 名称区分大小写，和 hook、模板不同；界面以后要不分大小写，得给 isos 加一列 key，AutoMigrate 能加列和新索引，推迟也安全。

④ 查询结果里的切片和 map（Page.Items、GetHosts 与 GetHooks 的结果、ListEvents 与 AppendEvents 的结果）不为 nil，只写在 storetest 的包说明里，doc.go 的总则没有；追加事件不改任务的 Rev，也只写在 pgstore 的行类型注释里。

⑤ NewHookVersion 只写了「下一个版本号」，没写「分配过的最大版本号加 1，删掉的草稿的号不再发」（deleteLast、deleteStale 钉住），spec 也没有；ListHookVersions、ListTemplateVersions 对不存在的身份报 ErrNotFound 没写。

⑥ CreateTasks 遇到 ISO 或模板版本不存在报 ErrNotFound；FindOpenTask 对没规范化的 SN 报 ErrInvalid（理由只写在 CheckSN 里）；ListEvents 对不存在的任务报 ErrNotFound、afterSeq 为负报 ErrInvalid；已结束的任务照样能追加事件；Key 区分大小写。这些都没写。

⑦ Templates 的说明（store.go:104-105），以及 check.go:337、pgstore/schema.go:118、pgstore/templates.go:401，都说建任务靠 template_hook_refs；实际上两份实现建任务都用 HookRefs 现算（任务 9 的偏离），这张表唯一的读者是 pgstore 的 DeleteHookDraft。

影响：第 4、5、7 层的计划要照接口注释写调用方，照现在的注释写会走错，比如以为 ISO 按 key 排、以为空批能过。

建议：第 3 层开工前一次补进 store.go、types.go、errors.go、doc.go 和上面那几处实现注释，计划第 90 行一并改准。只改注释，行为和测试都不动。S4-1 的 SN 口径和 L-7 的注释部分可以同一批改。

出处：internal/store/store.go:24-32、41-50、72-74、92-93、102-105、131-162；types.go:63-67；errors.go:25-28；doc.go；check.go:336-339；pgstore/schema.go:118；pgstore/templates.go:400-401；pgstore/rows_tasks.go:26-27；计划第 90 行

合并自：S4 should「契约包的注释和现在钉住的行为对不上，store.go 与 types.go 不在任务 4 的文件清单里」；S4 should「契约包的文档落后于 storetest 钉住的行为」；S5 should「版本号永不重发是本任务定下的契约」；S6 should「这一轮由测试钉住的几条新契约，只写在提交说明和用例里」；S6 should「契约文档没跟上：本任务定下、并已由契约测试钉住的语义」；任务2 should：types.go 的分页顺序照抄了计划，但没写按什么比较…（注释部分）；任务2 should：排序规则没写比较方式（types.go 62–65 行）…（注释部分）；任务2 should：返回的集合在没有结果时是 nil 还是空切片或空 map，契约没定…（doc.go 总则部分）；layer_should 第 2 条：契约注释与实现、契约测试对不上，有几处

### S5-1　内置内容的名字一经装载就不能改，spec 与注释没写

问题：名字记在身份上，内置身份不能改名（报 ErrImmutable），内置版本连废弃也不行。EnsureBuiltin 遇到同一个 ID 的新版本带了另一个名字，两份实现的 ensureBuiltinIdent 都报 ErrBuiltinMismatch（builtinEnsure 的 b3 用例钉住）。合起来就是：内置内容的名字一经在某个库里装载，以后的 lober 版本就改不了；要改只能换一个新 ID，旧身份和它的已审批版本永久留在库里，名字也一直占着。spec §6.2 第 11 条只写了两种拒绝启动的情形（改了内容没升版本号、名字被用户占了），ErrBuiltinMismatch 和 EnsureBuiltinHook 的注释也只说「同一个 ID 与版本的内容不同」。

影响：第 3 层正是写内置内容的一层，照现在的文字会以为改名时升个版本号就行。真改了的话，全新的库照常启动，装过旧版本的库升级时却拒绝启动，在新库上开发测不出来。

这条原先一组列为待拍板、另一组列为开工前修，我按开工前修处理：现状是契约测试钉住的行为；以后如果想让内置内容能改名，放宽成「装载更高版本时改身份上的名字」是向后兼容的，到时再定也不晚。

建议：§6.2 第 11 条和计划「留给后续层的约定」第 3 层补一句：「名字随身份定下，以后的版本不能换名，换了同样拒绝启动；要改名只能换一个新 ID」。ErrBuiltinMismatch 和 EnsureBuiltinHook 的注释同步。第 3 层另加一条用例，把发布过的各个内置版本依次装进同一个库。同一个根子还有另一面：内置版本不能废弃，以后哪个 lober 版本不再带某个内置模板，库里的旧身份仍是已审批，会一直出现在向导里，第 3 层设计装载时要一并想（比如由第 5 层按程序当前带的内置清单过滤）。这也是名字前缀必须在第一份内置内容之前定下来的原因（见 S10-2）。

出处：spec §6.2 第 11 条（第 330 行）；计划「留给后续层的约定」第 503 行；internal/store/errors.go:37-39；store.go:96-98；pgstore/hooks.go:500-516；memstore/hooks.go:520-537；storetest/hooks.go:1133-1138、1156-1157

合并自：S5 should「「留给后续层的约定」的第 3 层可以补一条」；S10 should：同文件 :330（§6.2 第 11 条）：契约测试 builtinEnsure 还钉住了另一种情况；S10 should：spec 第 330 行（§6.2 第 11 条）漏了一条拒绝启动的情形

### S3-14　model.Subject 写法表自相矛盾，缺身份级与新建时的写法

问题：problem.go 里 Subject 的注释写明「第 5、6 层按它解析，以后不要随手改」，但有几处不对：

① 第 31 行「系的列表与参数名只出现在说明里，不进 Path」与第 39-41 行的 tpl/<参数名>、<插槽名>/<n>/<参数名> 矛盾。应改成「参数声明与实现按位置写（param/<n>、impl/<n>），不写参数名与系的列表；取值来源的字段键例外，带参数名」。

② 模板一节的参数声明和模板取值来源没写 Kind（实际是 template），hook 一节写了。

③ 第 47 行「NormalizeSN 只给 Kind，SN 合法时另给 ID」不准：sn_invalid 只给 Kind，sn_placeholder 另给规范化之后的 SN 作 ID。

④ param_unknown_field 的 Path 是提交上来的原样键，不一定合字段键的写法，应注明。

⑤ hook 和模板的 ID 只列了「ID@版本」一种，仓储另外还给两种：改名（RenameHook、RenameTemplate）时，名字问题的 Subject 是 {hook 或 template, 身份 ID}，不带 @版本；新建与派生（CheckNewHook、CheckNewTemplate、DeriveTemplate）时身份还没有 ID，名字与文本问题的 ID 为空（text_not_storable 是 {impl, 空 ID, impl/<n>} 这类）。审计的 TargetID 已经写了「针对整个身份的操作是 ID」，这里没跟上。

影响：第 5、6 层照这张表解析，遇到不带 @ 或 ID 为空的 Subject 会对不上；矛盾留下去，就会被照着写错。

建议：只改注释。补齐上面几处，再补两行：针对整个身份的问题，ID 是身份 ID；新建与派生时 ID 为空，Kind 与 Path 照版本级的写法。

出处：internal/core/model/problem.go:25-47；model/ident.go:39-48；params/params.go:260-266；internal/store/check_authoring.go:94、111、243-253；pgstore/hooks.go:193；pgstore/templates.go:136、265（内存实现对应处同样）

合并自：任务3 should：internal/core/model/problem.go:31：Subject 注释写「系的列表与参数名只出现在说明里，不进 Path」…；任务3 should：internal/core/model/problem.go:42–47 的 Subject 文档（C-4）写明第 5、6 层要按它解析…；layer_should 第 9 条：model.Subject 的写法表（problem.go:30-44）里 hook 与模板只有版本级的「ID@版本」

### S4-1　按 SN 查找时没规范化的 SN 各方法报法不一，契约没写

问题：两份实现的结果完全相同（S4 的探针两边实测，我也对过代码）：SN-1 已登记时，GetHost、DeleteHost 收到 sn-1 报 ErrNotFound，GetHosts 给空 map，ListTasks 按 HostSN=sn-1 给空页；FindOpenTask 和 UpdateHost 报 ErrInvalid。规则其实是「带整条记录的写入和 FindOpenTask 查规范化，其余按 SN 查找的不查」，但 store.go 一句没写；FindOpenTask 为什么不同，也只写在 CheckSN 的注释里：网关不能把漏了规范化当成「没有任务」，否则机器会从本地盘启动，任务一直等着。

影响：第 5 层要记得在 /hosts/{sn} 这类路由上先经过 model.NormalizeSN，否则 sn-1 得到的是 404，而不是 400。

这条原先列为待拍板，我改成工程问题：统一报 ErrInvalid，会让 GetHosts 里一个坏 SN 就整批失败，与「结果只放存在的」冲突，S4-3 里存不下的 SN 当作「没有」的口径也要跟着改；统一当作找不到，又违背 CheckSN 写明的理由。保持现状最合适。

建议：在 store.go 的 Hosts 说明和 FindOpenTask 上写明这条规则和理由；在计划「留给后续层的约定」第 5 层写一句「按 SN 调仓储之前先规范化」。可以随 S4-6 一起改。

出处：internal/store/store.go:22-39、142-144；check.go:218-225（CheckSN）；check_registry.go:12-46（CheckHosts）；memstore/hosts.go:92-130；pgstore/hosts.go:136-189

合并自：S4 should「SN-1 已登记时，DeleteHost 与 GetHost 收到合法但没规范化的」；S6 should「按 SN 查询的约定不一致」

### S10-5　第 1 层清单 L-8 的字段键部分已随 C-3 修掉，条目没更新

问题：C-3（036f397）已经把 TemplateFieldKey、InstancePath、InstanceFieldKey 放进 model（problem.go:58-71），activation、params、snapshot 共用这一份。可第 1 层遗留问题清单的 L-8（第 730 行）还写着「字段键在 params 与 snapshot 里各拼一次」，并建议「第 5 层做接口时，params 导出字段键构造函数」；清单第 18 行和第 3 节开头（第 636 行）也还写着「其余 14 条没有动」。

影响：第 3 层写计划时要读这一节，照着做会在 params 里再造一份。L-8 的其余部分仍成立：「ID@版本」有五处各拼一份（activation/hook.go、admission.go、params.go、snapshot/build.go，外加 store.VersionID）；model 没有 HookKey、TemplateKey 的 String()；Subject.Kind 和 catalog 的标识仍是字面量。

建议：在 L-8 下补一段结果：字段键已随 C-3 修掉，放在 model 而不是 params；「ID@版本」可以改用 store.VersionID，或者在 model 加 String()；catalog 常量、全局词表和 Subject.Kind 常量仍留给第 3、5 层。第 18 行和第 636 行的说法一并改掉。只改文档。

出处：docs/superpowers/reviews/2026-10-02-layer1-open-issues.md:18、636、728-734

合并自：S10 should：清单 L-8（第 728-734 行）已部分过时

### S7-4　models 与 tables 一一对应只写在注释里，没有测试

问题：open.go:23 的注释说，models 里的表和 schema.go 的 tables 一一对应；但 checkSchema 只查 tables 列出的表，schema_test 里也没有比对两者的测试。

影响：往 models 加了行类型、却漏登 tables，那张表的主键、索引、外键、CHECK 就完全不受断言管，AutoMigrate 的两个盲区又没人守了。计划说断言是唯一的防线，而第 4 层（ISO 来源）和第 5 层（用户与会话）都要加表。

建议：schema_test 加一例：models 里每个行类型都有 TableName()（十个都有），断言这些表名的集合等于 tables 的表名集合。约十行，可以和 S7-5 一起做。

出处：internal/store/pgstore/open.go:23-29；pgstore/schema.go:58-60、251-269；pgstore/schema_test.go

合并自：S7 should：…open.go:27 models 与 schema.go 的 tables「一一对应」只写在注释里…

### S3-13　「所勾各系并起来看」的用例只钉住一个方向

问题：activation 里检查「所勾各系选中的片段要并起来看」的两例新用例，起决定作用的引用都在 rocky 的片段里，而 rocky 又是模板所勾的第一个系，所以只抓得住「只留最后一个系」这一种退化。S3 的探针把 template.go:224-227 改成「只留第一个有引用的系」（refs 为空时才取），internal/core 全部通过。

影响：退化之后，勾了几个系的模板，只要后面那个系的片段取了默认值给不出的子值，照样能批准，要到给那个系的 ISO 建任务时才逐台报错。第 3 层的内置模板多半一个模板勾几个系，正靠激活判定把关。

建议：把 inOneTicked 反过来写，即 splitPart 的第一份留空、第二份放 volgroup 那一行；gw 那例不动，两个方向就都钉住了（评审核对过：反过来的这例在当前代码上能过，在「只留第一个系」下会失败）。只改测试。

出处：internal/core/activation/template_test.go:522-546、599-614；activation/template.go:206-228

合并自：任务3 should：internal/core/activation/template_test.go:524-546、599-614：第 1 轮必须修改项已按要求修好…；任务3 should：internal/core/activation/template_test.go:600：新补的两例都只往 rocky…

### S2-11　存储层守卫不报被构建约束排除的文件

问题：checkStore 只解析被构建约束排除的文件的 import，不像第 1 层那样把这类文件本身报出来（ruleIgnored）；Test存储层总检查接上了每一项 还专门断言 x_linux.go、x_windows_test.go 不报。这是任务 2 有意的取舍，checkStore 的注释写了。

影响：pgstore 里出现一个 *_linux_test.go，或者给测试文件加一个 //go:build 标签，PostgreSQL 契约测试就会在 Windows 上静默不跑，make check 照样绿。这正是计划「开工前」一节要防的「跳过会让存储层零覆盖也变绿」。pgstore 包现在要跑 137 秒，有人想提速时最容易走到这一步。

建议：checkStore 也对 IgnoredGoFiles（含 _test.go）报违规，规则文字改成不限第 1 层（cgo 已经由「C 不算标准库」挡住，不用另加）；guard_test 的组合用例改成断言这两个文件被报出来。我用 go list 核过，存储层四个包现在都没有被排除的文件，改完即绿。

出处：internal/archtest/deps_test.go:177-188（checkStore）；archtest/guard_test.go:321-347

合并自：任务2 deviation：Two layer-1 rules are not applied to the store layer…；任务2 should：存储层守卫只解析被构建约束排除的文件的 import，不把这类文件本身报出来…

### S5-5　往返读写漏了参数声明与取值来源的四个字段

问题：storetest 的样例里，ParamDecl.List、ParamDecl.Required、ParamDecl.Offer（只出现在存不下的反例里）和 Binding.Editable 都是零值（我 grep 过），深拷贝、往返读写和 builtinEnsure 的比对都没覆盖到这四个字段。pgstore 把 model 类型整个编成 jsonb，内存实现按结构体整体拷贝，目前不会丢。

影响：cloneDecls 这类手写拷贝一旦改成逐字段构造，或者 pgstore 把哪个字段拆成单独的列，就会悄悄丢掉；而 Editable 决定向导里参数能不能改（C20）。优先级低。

建议：full 里给一个参数设 List 和 Required，一个 enum 改用 Offer，一条取值来源设 Editable 为 true，variants 各加一项。

出处：internal/store/storetest/hooks.go:32-84（sampleHook、variants）；storetest/templates.go:39-100（sampleTemplate、fullTemplate、variants）

合并自：S5 should「往返读写漏了四个嵌套字段（探针证实）」

### S5-4　开新版本失败之后版本号是否连续没钉住

问题：两份实现都在同一次写入里分配版本号（内存实现在暂存里，pgstore 在事务里），现在是对的。但 newVersionPending 两次 ErrConflict 之后开出的版本没断言版本号，newVersionBad 失败之后也不再开版本。S5 的变异验证：让内存实现的 NewHookVersion 在报 ErrConflict 或 ErrNotFound 之后另起一次写入把计数加 1，契约测试全绿。

影响：版本号会出现空洞，不涉及数据。优先级低。

建议：newVersionPending 断言开出的是版本 2；newVersionBad 末尾补一次成功的开新版本，并断言是版本 2。两行断言。

出处：internal/store/storetest/hooks.go:959-993（newVersionPending、newVersionBad）

合并自：S5 should「版本计数器的原子性没被钉住（探针证实）」

### S4-8　只有 ListHosts 钉住了 Limit 为 0 时取 100

问题：各列表都用共用的 CheckQuery 返回的条数，两份实现目前一致；但只有 hostListDefaultLimit 钉住了「Limit 为 0 时取 100」，ListISOs、ListAudit、ListHooks、ListTemplates、ListTasks 都没有「Limit 为 0、多于 100 条」的用例。

影响：某份实现写成 Limit(q.Limit+1) 这类回退，要到真有很多数据时才暴露。优先级低。

建议：各列表补一个最小用例：101 条，第一页 100 条且 Next 非空，第二页 1 条；可以写成各组共用的一个辅助函数。

出处：internal/store/storetest/hosts.go:394-410（hostListDefaultLimit）；isos.go、audit.go、tasks.go、hooks.go、templates.go 的列表用例

合并自：S4 should「「Limit 为 0 时取 100」只在 ListHosts 上钉住了」

### S6-3　同时有两类问题时报哪一类没有用例钉住

问题：两份实现的检查顺序都是：审计，入参（共用函数），按键取数（ISO 先于模板），重跑判定；推进的顺序是：CheckTransition，任务存在，期望值，C31。S6 的探针逐对核对过：零值审计加上入参不合格，两边都报 ErrAuditRequired；ISO 和主机都缺，两边都报 ErrNotFound；事件不合法加上期望值对不上，两边都报 ErrInvalid；身份有草稿且来源版本不存在，两边都报 ErrNotFound。当前一致，但没有用例钉住。

影响：第 5 层按错误类别映射状态码，哪天某份实现调了顺序，同一个请求在测试和生产上就会得到不同的状态码。优先级低。

建议：taskCreateInvalid、taskTransitionInvalid、taskCreateMissing 各补一例组合输入，断言报的是哪一类。

出处：internal/store/memstore/tasks.go:18-84、154-184；pgstore/tasks.go:26-146、281-319；storetest/tasks.go:309-395、571-596

合并自：S6 should「检查的先后顺序没有用例钉住」

### S5-6　改名之后单取与批量取的名字没有断言

问题：renameIdentity 只比了 ListHookVersions、ListTemplateVersions，templateNamespaces 比了列表；GetHook、GetHooks、GetTemplate 在改名之后都没断言名字。两份实现读版本时都从身份上带出名字（内存实现的 d.hook、pgstore 的 joined 连表），目前一致。

影响：哪天某条读取路径改成从版本行取名字，改名之后单取和批量取会给出旧名字，现在测不出来。优先级低。

建议：在 renameIdentity 里给 get 和 GetHooks 各补一处断言。

出处：internal/store/storetest/hooks.go:995-1015（renameIdentity）

合并自：S5 should「「返回的每个版本，Name 都是身份现在的名字」在改名之后只查了两处」

### S6-4　深拷贝用例没改动用户发起推进的返回值

问题：taskCopies 只改动了 TransitionTask 的返回值，没改 TransitionTaskByUser 的。两份实现的两种推进走的是同一个 transition，pgstore 的返回值每次都从 JSON 解出，天然是新副本，目前没有问题。

影响：以后哪份实现把两种推进拆开写，返回值共享底层存储也测不出来。优先级最低。

建议：在用例末尾补一次 TransitionTaskByUser，并 mutate 它的返回值。一行。

出处：internal/store/storetest/tasks.go:753-790（taskCopies）

合并自：S6 should「taskCopies（tasks.go 740–776 行）只改动了 TransitionTask 返回的任务」

## 3. 归后续层处理

属于后续层的事，或要等后续层才有真实用途。写那一层的计划时纳入即可。

### S1-6　「ValidID 合的都是安全词」只靠 6 个样本钉住

问题：ValidID 的注释保证「合它的都是安全词」，但这条保证只靠 6 个样本钉住。S1 的探针把 IsSafeWord 改成拒收结尾的 -，internal/core、internal/store、memstore 全部通过，此时 a- 合 ValidID，却不是安全词。

影响：眼下没有。{{sys:task}} 代入的任务 ID 都由 NewID 生成，TestNewID生成的ID互不相同且合规则 对 1000 个 ID 断言了 IsSafeWord。param.go:346 写明「要不要拒收以 - 或 @ 开头的取值，留到第 3 层写内置模板之前再定」。

建议：第 3 层动 IsSafeWord 时，在 TestValidID逐个字符 里，对 ValidID 为真的「字符加 a」和「a 加字符」同时断言 IsSafeWord（长度上限已由 64 个 a 的样本钉住），或者收窄 ValidID 注释里的这条保证。

出处：internal/core/model/ident_test.go:217-270；ident.go:142-149；param.go:346

合并自：任务1 should：…ident_test.go:217-270：ValidID 注释里「合它的都是安全词」这条保证，目前只靠 6 个样本…

### S2-12　「只有 pgstore 能用 gorm、pgx」只查 internal/store

问题：Test存储层依赖守卫 只列 internal/store 下的包，测试文件也不查。现在模块里只有 internal/core、internal/store、internal/archtest，go.mod 的直接依赖只有 gorm、driver/postgres、pgx v5，眼下没有缺口。

影响：第 3 层会加渲染与内置内容的包（spec §4.3；§6.1 要求它们只依赖层 1），守卫不扩就查不到它们。

建议：第 3 层起，守卫按全模块列包：第三方模块只许 pgstore 用 gorm 和 pgx，渲染与内置内容另按「只依赖层 1」查；第 5 层加控制台和 cmd/lober 时照这套延伸。S2-11 若到时还没做，可以一起做。

出处：internal/archtest/deps_test.go:161-175、451-476；计划「整体验收」第 4 条

合并自：任务2 concern：The guard checks only non-test files, as layer 1 does…；任务2 should：「只有 pgstore 可以依赖 gorm 与 pgx」现在只检查 internal/store 下的包…

### S2-13　存储层没有「不读时钟」的守卫

问题：「仓储不读时钟」是全局约定（doc.go），但存储层的守卫没有时钟检查。眼下没有缺口：internal/store 下的非测试文件都没 import time（我 grep 过，只有 pgstore_test.go 用到）；唯一叫 CreatedAt 的行字段已关掉 GORM 的 autoCreateTime，而且 CheckNewTasks 拒收 0，GORM 本来也不会去填。

影响：以后有人在仓储里读时钟，或者给行类型加了 GORM 会按字段名自动填时间的字段，import 检查看不出来。

建议：第 3 层本来就要给渲染加时钟检查（spec §6.1），届时把「只禁读时钟的九个函数（ruleClock），放行 time.Duration」这条一起套到存储层；第 4、5 层给仓储加表时，再加一条「行类型不开 autoCreateTime、autoUpdateTime」的断言。

出处：internal/archtest/deps_test.go:439-449（checkStoreFile）；internal/store/doc.go:15；pgstore/rows_tasks.go:23-24

合并自：任务2 concern：「仓储不读时钟」 has no mechanical guard for the store layer…；任务2 should：「仓储不读时钟」是全局约定，但存储层的守卫没有时钟检查…

### S2-22　内置内容「内容相同」按 DeepEqual 比，nil 与空切片算不同

问题：两份实现都把库里读回的版本与程序带的版本做 reflect.DeepEqual（Rev 置 1 后比）；jsonb 写进去什么就读出什么，两边结果一致，不是两份实现不一致的问题。

影响：风险在跨版本：程序升级后，同一个内置版本只是把 nil 写成了空切片（或者反过来），语义没变，也会报 ErrBuiltinMismatch、拒绝启动。契约测试都从空库起，只有老库升级时才冒出来，属于只在生产环境出现的那一类。

建议：第 3 层写内置内容时二选一。① 保持严格比较，另加一条测试，给每个内置版本的序列化记指纹（golden），同一版本的指纹变了就失败，逼着升版本号；代价是多一份随版本更新的 golden，第 2 层不用动。② 比较前把 nil 和空的切片、map 规范成一种，规范函数放进 store 两边共用；代价是改两份实现，builtinEnsure 加一例「nil 与空切片算相同」。倾向 ①。

出处：memstore/hooks.go:271-293、templates.go:312-335；pgstore/hooks.go:320-353、templates.go:364-398；storetest/hooks.go:1099-1139；spec §6.2 第 11 条

合并自：任务2 should：计划把共用纯函数限定为九个…（EnsureBuiltin* 的「内容相同」，特别是 nil 与空切片怎么算）

### S3-10　架构清单只查得到「列出的都合法」

问题：C-9 的架构清单对齐测试只能查到「列出的都合法、区分大小写」，catalog 没有导出全部架构的词表。

影响：目录新加了架构，而 host_platform_invalid 的说明没补，查不出来。

建议：随第 1 层遗留问题清单的 L-8「catalog 导出全局词表」一起做（清单写的是第 3、5 层），届时把这条对齐改成全集比对。

出处：internal/core/model/codes_align_test.go:275；第 1 层遗留问题清单 L-8

合并自：任务3 concern：C-9 的架构清单只能查到「列出的都合法、区分大小写」…

### L-7　建任务重跑准入用钉住的版本，与 admission 注释不符

问题：两份实现建任务时，按任务里钉住的 TemplateID/TemplateVersion 取版本交给准入；而 BatchInput.Template 和 TemplateUsable 的注释要求它是「调用方用 model.CurrentApproved 取出的版本」。结果是：向导停留期间新版本获批，提交旧的、仍是已审批的版本，照样建得成。两份实现一致。

影响：C23 的「向导里每个模板只提供它当前的已审批版本」，只靠向导的筛选守着。

这条原先列为待拍板，我改成工程问题：C23 和 §5.2 第 5 步「新版本批准之前，旧版本照常可用」都意味着批准之后旧版本不再用来建新任务；admission 的注释也写明不回落到更早的已审批版本，免得操作员选的是当前版本、装出来的却是旧内容；控制台是唯一建任务的入口，§5.3 第 6 步又要求服务端不信任前端的筛选。

建议：第 5 层在向导提交时用 CurrentApproved 比对，版本已被取代就整批拒绝，并提示换成新版本（需要时追加一个原因代码）。仓储保持现状，不另加锁，比对与提交之间的毫秒级窗口可以接受。注释现在就可以改：admission 的两处和 store.go 里 CreateTasks 第 3 步写明「重跑准入用的是任务里钉住的版本，它是不是当前版本，由第 5 层在提交时比对」，可以随 S4-6 一起改。如果你希望允许用旧的已审批版本建任务，就只改注释、不加比对。

出处：internal/core/admission/admission.go:50-51、92；memstore/tasks.go:62-67；pgstore/tasks.go:103-113；internal/store/store.go:131-139；spec C23（第 190 行）、§5.2 第 5 步（第 282 行）、§5.3 第 3、6 步（第 292、295 行）

合并自：layer_should 第 7 条：CreateTasks 把任务钉住的模板版本原样交给准入

### S5-3　GetHooks 遇到格式不对的键整批报 ErrInvalid

问题：模板草稿的 Instance.HookID 可以是任何存得下的串，HookRefs 照样列出；两份实现的 GetHooks 只要有一个键不合 ValidID，就整批报 ErrInvalid（hookGetHooks 钉住）。仓储自己的审批和建任务不受影响：探针里同一份引用了 Bad ID 的草稿，审批时两边都报 hook_ref_missing。

影响：影响在第 5 层。预览草稿或给编辑器跑激活判定时，自然写法 GetHooks(HookRefs(draft)) 两边都得到 ErrInvalid，多半映射成 400，而不是判定给出的 hook_ref_missing（C22：草稿可以预览）。

这条原先列为待拍板，我改成工程问题：按 doc.go「收到的 ID 先过 ValidID，不合报 ErrInvalid」的总则，GetHooks 保持严格就对了。

建议：第 5 层取引用的 hook 之前，先滤掉不合 ValidID 的引用，缺的由判定报 hook_ref_missing；到时如果有两处以上要这样做，就在 store 里加一个只给格式合法引用的辅助函数。写进「留给后续层的约定」第 5 层。

出处：internal/store/memstore/hooks.go:217-233；pgstore/hooks.go:254-276；storetest/hooks.go（hookGetHooks）；internal/store/check.go:336-351（HookRefs）；doc.go:16-17

合并自：S5 should「GetHooks 只要有一个键格式不对就报 ErrInvalid」

### S5-2　删掉派生来源的草稿后 DerivedFrom 悬空

问题：DeriveTemplate 的来源可以是任何状态，包括草稿；DeleteTemplateDraft 不看别的模板的 DerivedFrom，两份实现都放行（探针实测）。派生模板的 DerivedFrom 于是指向已不存在的版本；删的是最后一个版本时，连身份都不在了。版本号不再发、NewID 是随机的，所以不会对到别的内容上；内存实现的 DeleteTemplateDraft 注释写明这是有意的。spec C26 说「被模板或任务引用的对象只能废弃，不能删除」，计划的 ErrReferenced 只列了主机、ISO 与 hook 草稿，而草稿又不能废弃，两处说法有缺口。

影响：第 5 层编辑器的「来源出了新版本」提示（C24）要能处理来源已经不在的情况。

这条原先列为待拍板，我改成工程问题：按 C24 的精神，派生出的是一份独立副本，DerivedFrom 只记出处，不是 C26 那种要用到对方内容的引用。

建议：保持现状。在「留给后续层的约定」第 5 层写明：派生来源可能已删除，提示按来源身份的其余版本算，身份也没了就不提示。

出处：internal/store/memstore/templates.go:235-256；pgstore/templates.go:272-298；spec C24、C26（第 191、193 行）；计划「错误」一节 ErrReferenced（第 101 行）

合并自：S5 concern「Deleting a template draft that another template was derived from is not blocked」

### L-8　ErrReferenced 只带文字，两份实现写的不一样　（两份实现不一致）

问题：删除被任务引用的主机或 ISO 时，内存实现写的是「主机 SN-1 被任务 xxx 引用」；pgstore 走外键翻译，写的是「还有别的记录经外键 fk_tasks_host 引用它」。DeleteHookDraft 两边都写出引用它的模板 ID@版本，是一致的。错误文字两边不同，但契约写明错误文字只给日志和开发者看，类别（errors.Is）一致，契约测试也只比类别。所以不放在第 3 层之前修：只要第 5 层不把 err.Error() 交给前端，两边就没有差别。

影响：第 5 层拿不到结构化的「被谁引用」，没法照 spec 总则「说清下一步」，指出是哪个模板挡着 hook 草稿的删除。

建议：第 5 层做删除接口时，加一个带类型的 ReferencedError（Unwrap 为 ErrReferenced），带上引用方的类别和 ID，两份实现都填。pgstore 在锁住主机或 ISO 之后，先在事务里查引用它的任务：建任务要先给主机加写锁、给 ISO 加读锁，所以查到的不会漏；外键照旧兜底。对主机和 ISO 来说，引用它的任务本身无从处置，真正有用的是 hook 草稿那一处。主机永远删不掉的问题另见 L-8b。

出处：internal/store/memstore/hosts.go:100-102、memstore/isos.go:96-98；pgstore/errors.go:51-58；pgstore/hooks.go:221-230；internal/store/errors.go:27-28

合并自：layer_should 第 8 条：ErrReferenced 只带文字，两边还不一样

### S9-2　tasks 表缺 host_sn、iso_id、模板版本的全表索引

问题：计划定死的索引集合里，tasks 只有 created_at、state 两个普通索引，外加只收未结束任务的 idx_tasks_open。删主机、删 ISO、删模板草稿时，PostgreSQL 的 RESTRICT 外键检查要按 host_sn、iso_id、(template_id, template_version) 去查 tasks；ListTasks 按 HostSN 过滤（含已结束的任务）也一样。这些都会顺序扫描，而 tasks 只增不删。

影响：数据量小时感觉不到，任务多了以后，删主机、按主机看任务会越来越慢。

这条原先列为待拍板，我改成工程问题：按需要加索引即可。以后加索引对 AutoMigrate 是安全的（是新增索引，不是改已有的），只是 schema.go 的期望值要跟着改；首次部署之前加，也不用写升级动作。

建议：第 5 层做「按主机看任务」和删主机、删 ISO 的接口时，给 tasks 加 host_sn、iso_id、(template_id, template_version) 三个普通索引，改 rows_tasks.go 的 tag 和 schema.go 的期望值，重跑断言测试。如果 L-8b 选了允许删除主机，host_sn 的索引就更要有。

出处：计划「表结构」一节（docs/superpowers/plans/2026-10-03-rewrite-layer2-store.md:206-210）；internal/store/pgstore/schema.go:128-150；pgstore/rows_tasks.go:14-34

合并自：S9 concerns：Following the plan's fixed index set, there is no full-table index on tasks.host_sn…；S9 should：计划层面的观察，不是本任务的偏差：按计划定死的索引集合…；S9 should：计划层面：tasks 上没有 host_sn、iso_id、(template_id, template_version) 的全表索引…

### S2-24　DuplicateError.Field 与 TargetKind 没有导出常量

问题：DuplicateError.Field（sn、name、sha256）和 AuditEntry.TargetKind（host、iso、hook、template、task）仍是字面量，散在 memstore、pgstore、storetest 里。两份实现的取值由契约测试（wantDuplicate、审计对象的断言）钉住，不会走偏。

影响：第 5 层要按它们映射说明、拼审计查询参数时，只能再写一遍字面量。

建议：第 5 层用到时在 store 导出常量（纯追加），与第 1 层遗留问题清单 L-8 同一做法。

出处：internal/store/errors.go:42-47；types.go:25-29

合并自：任务2 should：小处：…DuplicateError.Field（sn、name、sha256）与 AuditEntry.TargetKind…可以导出常量

### S5-7　驳回审批不强制写理由，留给第 5 层

问题：两份实现的驳回只要求审计合规则，Detail 可以为空；spec §5.2 第 5 步写的是「驳回要写理由」。

影响：仓储不做业务判定（doc.go），这条要靠第 5 层，否则驳回可以不留理由。

建议：第 5 层的驳回接口要求非空理由，并放进 Audit.Detail；写进「留给后续层的约定」第 5 层。

出处：internal/store/store.go:80-81；memstore/hooks.go:115-121、pgstore/hooks.go:133-139；spec §5.2 第 5 步（第 282 行）

合并自：S5 concern「RejectHook and RejectTemplate do not require a reason」

### S5-8　开新版本与装载内置模板两条路径的引用记录没钉住

问题：两份实现在这两条路径上都记了引用（内存实现的 putTemplate 每次算 HookRefs，pgstore 两处都 insertRefs），只是没有用例，因为经接口看不出来：开新版本的来源只能是已审批或已废弃的版本，它引用的 hook 版本在审批时已是 approved（否则报 hook_ref_unapproved），永远删不掉；内置模板只能引用内置 hook，内置 hook 只以已审批装载，用户的 ID 由 NewID 随机生成，内置内容引用不到。建任务也不读这张表，两份实现都用 HookRefs 现算（任务 9）。如今 template_hook_refs 唯一的读者是 pgstore 的 DeleteHookDraft。

影响：眼下没有。等这张表有了第二个读者，这两条写入路径漏记就会出错。

建议：后续层（多半是第 5 层「哪些模板引用了这个 hook 版本」这类查询）给它加第二个读者时再补用例；或者把任务 8 评审在探针里验证过的不变式检查（每个模板版本在表里的行等于 refRows）放进 pgstore_test 的 harness。store.go「建任务都靠它」的说法已列在 S4-6。

出处：internal/store/pgstore/templates.go:119、395（refRows）；memstore/templates.go:356-361（putTemplate）；storetest/templates.go:427 起（templateRefs）

合并自：S5 should「引用表有两条写入路径没有测试钉住」

### S7-7　连接池写着由装配点调整，装配点却调不了

问题：Open 的注释写着「连接池的大小用 database/sql 的默认值，由装配点按部署调整」，但 Store.db 不导出，Open 只收 ctx 和 dsn，装配点没有入口可调。database/sql 默认的 MaxOpenConns 不设上限（测试夹具自己设成了 4）。

影响：到第 7 层大批主机同时回报时，可能把库的 max_connections（本机 100）撑满。在那之前，这句注释不准确。

建议：装配点在第 5 层第一次出现（spec §7.4），到时加一个选项，比如让 Open 收连接池参数，或给 Store 加一个设置方法。注释可以先改成「目前用默认值，第 5 层的装配点会加选项」。

出处：internal/store/pgstore/open.go:16-19、34-35；pgstore/pgstore_test.go:152

合并自：S7 should：…open.go:33 注释说连接池大小「由装配点按部署调整」…

### S6-5　cloneTask 逐字段手写，第 7 层加字段时要有守卫

问题：cloneTask 逐字段手写深拷贝。S6 对照 model.Task 和各快照类型核过，它覆盖了现有全部引用类型的字段；探针里把它改成浅拷贝，会被 taskCopies 抓到。

影响：第 7 层要往 Task 上加 token、引导次数、时机进度等执行字段（计划「留给后续层的约定」）。加了 map、切片或指针却忘了改 cloneTask，taskCopies 只改现有字段，查不出来。

建议：第 7 层加字段时，同时加一个反射测试：遍历 model.Task 里所有引用类型的字段，确认拷贝前后不共享底层存储；也可以现在就加。

出处：internal/store/memstore/tasks.go:227-262

合并自：S6 should「cloneTask（memstore/tasks.go:235）逐字段手写深拷贝」
