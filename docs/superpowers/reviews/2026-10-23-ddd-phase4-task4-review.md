# Task4 独立审查

## 需求符合性

- ✅ **符合本项要求。** 固定范围为 `fae02dcbfe5150b17988acb0fe5c9862834cc5a9..55f8309a1b1be87a7ba83a3c14ff33d9063b2b9a`，两个提交。未发现缺失、额外业务功能或阻断本项的行为变化。
- ✅ 应用的同一个 Sources 接管获取与导入；后台导入接收该对象，旧文件层 Import 业务删除。依据：`cmd/lober/app.go:103`、`cmd/lober/app.go:108`、`cmd/lober/wire_gen.go:38`、`internal/application/media/import.go:31`，以及固定差异中旧 `internal/infrastructure/media/import.go` 的删除块。
- ✅ 主来源仍先于额外来源打开；编号、取消、锁、状态、来源完成、摘要、识别、系匹配、额外介质、importing、临时根、Debian 检查、复制、复核、发布、清错、ready 的顺序保留。依据：`internal/application/media/import.go:31`、`:131`、`:143`、`:152`；文件层 `internal/infrastructure/media/import_session.go:48` 没有新增会提前盖过旧错误的取消检查。
- ✅ 两项接口补充只提供目录事实与临时根准备能力，未把文件句柄交给上层。平台目录和必需角色由应用传入；识别、版本、版本标签与 Debian 可用性判断位于应用/领域。依据：`internal/application/media/port/import.go:10`、`internal/application/media/import.go:130`、`internal/application/media/repository_media.go:14`、`internal/domain/media/rules.go:99`。按格式复制和相对仓库目录解析留在文件层，符合主控给定范围。
- ✅ 删除了本项 40 条活动许可，51→11；总数 158→118。原指纹保留，删除项只进入禁止恢复的历史检查。新领域包加入实际分层及禁止外部操作检查。依据：`internal/archtest/transition_manifest_test.go:4`、`internal/archtest/hosts_rules_test.go:129`、`:267`、`:296`，以及 `domain_rules_test.go`、`four_layer_test.go` 的对应差异。
- ⚠️ **可见证据以外无法验证：** 实物 ISO、真实 Windows 工具及完整机器安装兼容性；整仓库 make check 和 Task6 后整分支审查。本项通过不代替这些检查。测试树与候选的非 Markdown 一致性采用主控已经完成的比较，本席按约束未重跑 Git 比较。

## 做得好的部分

- `internal/application/media/import.go:186` 保留独立于请求取消的五秒确认读取。只有同摘要、同制品且 ready 才消除提交错误；`:191` 在结果未知、ready、deprecated 时禁止回滚。`:196` 保留失败写入前的状态检查和 4096 字节保存边界。
- `internal/infrastructure/media/import_session.go:166` 在提取和必需制品检查之后才创建 Windows 共享临时树；`:210` 在全部消费者完成后重核所有已打开来源的大小、修改时间与摘要；`:226` 保持共享树先发布，并在部分失败时返回已发布事实。
- `internal/infrastructure/media/import_session.go:248` 同时核对调用方和会话记录的“本次新建”标志，并检查 owner 后才删除。`:271` 只关闭来源与清理临时目录。已存在的完整同内容树仍复用，其他内容仍冲突，不扩大删除权限。
- `internal/application/media/identify.go:36` 保留 SUSE 优先顺序；文本上限仍为 `:18` 的 1MiB。版本、架构、Windows 标签条件迁入 `internal/domain/media/rules.go:99`；恢复和重新导入共用 `:63` 的 AllFetched。WIM 头与 XML 的顺序读取器仍在文件层，没有把整个镜像读进内存。
- `internal/application/media/import_error.go:14` 按错误分支转换已知故障，保留独立并列错误；新增 `import_error_test.go:12` 同时检查独立错误可追踪、已知故障内部原因隐藏。Task3 既有错误转换没有被改写。
- 关键新测试使用真实数据库、真实文件和明确的执行边界。`internal/application/media/import_order_test.go:86`、`:109` 检查临时目录与 Debian 错误顺序；`:135` 检查复制后篡改额外源；`:206` 检查提交未知时两种发布内容都保留；`:232` 检查废弃不会被失败标记覆盖。`import_publication_test.go:14` 检查共享树先发布且仅撤销本次内容；`import_cancel_order_test.go:24` 检查额外源打开时取消。
- 固定差异中的旧导入、官方介质与复核测试重命名保留原正文。识别测试中的版本、架构和标签断言仍在应用；三十二位 WIM 事实断言拆到 `internal/infrastructure/media/wim_test.go:224`，业务拒绝断言仍在应用，未用删断言换通过。原受互斥保护的复核测试只改包名，没有恢复此前的并发问题。

## 问题

### Critical：必须修复

无。

### Important：应修复后再通过

无。

### Minor：可选改进

无本项需要单列的问题。

## 检查、例外与限制

- 阅读了 task-4-brief、constraints、contracts-context、handoff、执行报告和 `docs/contracts/media.md`。固定差异按顺序分块读完，共 5223 行；首块输出中只有执行文档中段被截断，补取该段。未重新生成差异，未重读整份改动文件。
- 唯一补读源码例外：差异在 `internal/infrastructure/media/identify.go` 的 WIM 读取函数中途结束。为核对“不缓存整张镜像”的具体风险，读取该文件第 15 行起的函数尾部及其顺序读取器。确认使用 ReaderAt，或向前读取并丢弃间隔字节；没有整文件缓冲。除此之外只用一次定点名称搜索取得报告行号，没有扩查其他模块。
- 独立读取最终 `t4-final-verify-2` 的安全日志、tree、exit、started、finished、commands、snapshot.json 和 changes/untracked 文件大小。树为 `b696ddc307f72e10511d6e7097fa9639224bdcfb`；UTC 2026-10-23 18:07:58 至 18:17:25；退出码 0；changes/untracked 均 0 字节。
- 安全日志的结构化结果显示普通与 race 各 9 个包通过，合计 534 个顶层测试通过事件，0 个失败事件。port 包无测试。命令保留 `-count=1` 和 race 的 `-timeout=20m`，并包含 Wire diff、工作区 diff 与 status 检查。未运行本机 Go、gofmt、Wire、Node 或任何测试，也未重跑虚拟机套件。
- 两轮各跳过 `TestIdentifyRealISO` 与 `TestRealISO`。日志明确原因为 `LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in`。这些是未提供实物输入的覆盖限制，不应写成完整实机通过。Windows 工具样本也不能证明真实工具兼容性。
- 执行报告明确记录此前 Wire 文件未同步导致的构建失败；该旧轮没有作为本次通过证据。最终通过只采用完整重跑的 `t4-final-verify-2`，没有拼接旧轮结果。架构反例内部的缓存输出及暂存快照的 2019 条 `A  ` 已在报告解释；不将它们描述为全链路无缓存或空 status。
- 此次没有由代码产生、且现有执行记录无法回答的具体测试疑点，因此未建议额外套件重跑。整分支与完整构建检查继续按任务安排留给 Task6。

## 代码质量结论

**Approved。** 职责拆分明确，关键错误顺序和发布保护可从实际代码追踪；新增测试覆盖了会造成内容损失或状态误写的边界。此结论仅覆盖固定 Task4 差异和上述可见证据。


## 根执行者逐项处理

1. 实物镜像与真实工具：测试输入未提供，普通和竞态各两个实物测试明确跳过。工具样本不能证明真实工具和整机安装兼容；本项按批准计划如实保留限制，没有宣称完成这些验证。
2. 全仓检查与整分支审查：Task6尚未实施，继续按原计划执行。本项通过仅代表导入与识别迁移，不代替本阶段完成。
3. 源码一致：根重新比较最终测试树b696ddc307f72e10511d6e7097fa9639224bdcfb与55f8309候选，只列计划和执行记录两项Markdown，非文档内容完全相同。根也实际读取安全JSON与tree/exit/时间/changes/untracked证明，退出0且没有未暂存或未跟踪漂移。
4. 契约补充：Entries只返回目录事实；BeginPrepare只创建临时根，保证创建失败仍先于Debian扫描。格式复制和相对元数据解析留文件层，平台政策仍由应用/领域决定。独立审查已确认实际边界；这些补充随本次根计划提交保存。
