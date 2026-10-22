// Package repository 是仓储的契约（spec §4.2）：每个聚合一个接口（Hosts、ISOs、Hooks、Templates、Tasks、Events、
// AuditLog、Users），合成 Store；外加错误、审计、事件、分页这些共用类型，以及与存储无关的纯函数。
//
// 实现只有 PostgreSQL 一份（pgstore），测试也连 PostgreSQL。调用方（第 4、5、7 层与内置内容的装载）只依赖本包，
// 不依赖 pgstore。不用查库就能定下的规则（入参与版本状态的检查、生成 ID、游标的编解码、提取模板的 hook 引用、
// 在事务里重跑判定）写在本包的 check*.go，不随存储的写法走，单独测得了。实现里只留要查库里数据的那一半（键在
// 不在、Rev 对不对、名字被谁占着），判定的条件仍在这里。
//
// 下面的约定对每个方法都成立，各方法的注释不再重复：
//
//   - 第一个参数是 ctx，最后一个返回值是 error。
//   - 返回的切片与 map 都是深拷贝，调用方改了也不影响仓储里的数据；入参也先拷贝再存。
//   - 装结果的切片与 map 一律不为 nil，没有结果时是长度为 0 的切片或空 map：Page.Items、GetHosts 与 GetHooks 的
//     结果、ListEvents 的结果，以及 AppendEvents 与两种推进返回的 []Appended。第 5 层编成 JSON 是 [] 与 {}，nil 会
//     编成 null，与 OpenAPI 对不上。记录自己的字段不在此列：写进去什么就读出什么，nil 与空的 map、切片读回来不同。
//   - 改记录按整条替换，把字段改成零值也要存下：清空属性、把固件改回空都是正当的写入。pgstore 为此显式列出
//     要写的列，契约测试专门覆盖「改成零值」。
//   - 仓储不读时钟。Audit.At、Task.CreatedAt、NewEvent.At 都由调用方给，必须大于 0，否则报 ErrInvalid。架构守卫
//     （internal/archtest）查存储层的非测试文件：time.Now、time.Since、time.Sleep 这类读时钟的函数一律不许用，
//     time.Duration、time.Minute 照用。
//   - ID 由仓储用 NewID 生成，内置的 hook 与模板用自己带的固定 ID；收到的 ID 先过 model.ValidID（CheckID、
//     CheckVersionKey），不合规则报 ErrInvalid。
//   - 存不下的查找键与过滤条件当作没有：含 NUL 或不是合法 UTF-8 的串（model.StorableText 为假）不可能登记过。
//     GetHost、DeleteHost 按这样的 SN 报 ErrNotFound，GetHosts 的结果里没有它；各个 List 的过滤条件里有这样的串
//     时不报错，Items 为空；切片的过滤条件里有这样的元素，它谁都不是。不把数据库的编码错误交给调用方。FindOpenTask
//     例外，它要求 SN 已经规范化，存不下的 SN 同样报 ErrInvalid（CheckSN）。
//   - 可变的记录都带乐观锁版本 Rev：新建时为 1，每次写入加 1。调用方改记录时把读到的 Rev 原样带回，对不上报
//     ErrConflict，重读之后再判断（spec §6.2 第 4 条）。
//   - 要审计的方法最后一个参数是 Audit。审计在同一个事务里、在动作本身的写入之后写，审计写失败时动作一起
//     回滚（spec §6.2 第 8 条）。没给审计（零值）报 ErrAuditRequired，什么都不写；批量的操作每个对象写一条。
//   - 每个方法返回时事务已经提交（spec §6.2 第 9 条）。调用方接触不到事务；仓储也不发通知，通知由调用方在
//     拿到返回值之后发（spec §6.3「事务不做成切面」）。
//   - 仓储不做业务判定，只兜住并发：审批与建任务时，提交前对加锁后读到的最新数据重跑第 1 层的判定
//     （ApproveHookProblems、ApproveTemplateProblems、BatchProblems），判定规则仍然只有第 1 层那一份。
//   - 中文说明只在 model/codes.go。能用现有原因代码表达的问题返回 model.Problem，包在 ProblemsError 或
//     BatchError 里；其余返回本包的哨兵错误或带类型的错误，由第 5 层映射成说明。
package repository
