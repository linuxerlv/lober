// Package store 是仓储的契约（spec §4.2）：每个聚合一个接口（Hosts、ISOs、Hooks、Templates、Tasks、Events、
// AuditLog），合成 Store；外加错误、审计、事件、分页这些共用类型，以及内存与 PostgreSQL 两份实现共用的纯函数。
//
// 调用方（第 4、5、7 层与内置内容的装载）只依赖本包，不依赖任何一份实现，换实现不改调用方。两份实现跑
// storetest 里的同一套契约测试。两份实现都要用的规则（入参校验、生成 ID、提取模板的 hook 引用、在事务里重跑
// 判定）写在本包，两边调用同一份：规则写两遍就会走偏。
//
// 下面的约定对每个方法都成立，各方法的注释不再重复：
//
//   - 第一个参数是 ctx，最后一个返回值是 error。
//   - 返回的切片与 map 都是深拷贝，调用方改了也不影响仓储里的数据；入参也先拷贝再存。
//   - 改记录按整条替换，把字段改成零值也要存下：清空属性、把固件改回空都是正当的写入。pgstore 为此显式列出
//     要写的列，契约测试专门覆盖「改成零值」。
//   - 仓储不读时钟。Audit.At、Task.CreatedAt、NewEvent.At 都由调用方给，必须大于 0，否则报 ErrInvalid。
//   - ID 由仓储用 NewID 生成，内置的 hook 与模板用自己带的固定 ID；收到的 ID 先过 model.ValidID，不合规则报
//     ErrInvalid。
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
package store
