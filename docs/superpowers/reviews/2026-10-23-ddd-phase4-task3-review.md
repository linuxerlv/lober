# Task3最终审查通过

初审发现一项重要问题，修复aef267b已通过独立复审。当前没有遗留严重或重要问题。以下保留两轮原始结论。

# Task 3 独立审查

## Spec Compliance

- ❌ Issues found：主体拆分符合任务要求，但错误转换会丢掉重试文件恢复失败，破坏原有错误保留行为。见 Important 1。
- 审查范围固定为 `3eb6647468c9f2c765a4b8951d5ca4d9f9e95005..9b88bd20c024772d92be33a4f6a128cffa928c82`，包括单列的旧导入测试夹具同步修复。
- ⚠️ Cannot verify from diff：未改动的数据库原子合并实现、全部网页身份检查、旧导入和删除的完整兼容性不能单靠本次差异证明。本次确认其调用边界与保留测试，未扩展为整分支审查。
- ⚠️ Cannot verify：可选真实镜像文件未提供；全库 `make check` 按任务安排留给 Task 6。任务测试通过不等于全部媒体格式已经验证。

## Strengths

- `cmd/lober/wire_gen.go:25`：新获取服务与旧导入对象收到同一组来源锁和登记闸门；删除日志继续收到同一闸门。`internal/infrastructure/media/iso.go:23` 将旧锁入口委托给共享能力；额外检查 `internal/infrastructure/media/import.go:48` 确认旧导入仍经过该入口。
- `internal/application/media/sources.go:44`：登记读锁移到应用入口；旧 supervisor 的重复读锁已删除。失败镜像补来源仍使用 `SaveSourceAndAcquire`，没有把保存与排队拆开。`internal/application/media/retry.go:76` 的重新导入只保存待办，`internal/application/media/control.go:39` 成功后才唤醒。
- `internal/application/media/fetch.go:59`：先登记取消函数，再重读状态；已同步位置与读取错误同时返回时，仍尝试保存位置。最终发布重读状态及版本；保存失败恢复 part；上下文结束不改成用户取消。`internal/infrastructure/media/coordination.go:25` 的注销只删除自己登记的取消函数。
- `internal/application/media/upload.go:34`、`internal/infrastructure/media/upload.go:41`：先完整暂存，按真实片长保存区间；检查重叠后才写确认文件。最后片按 Write/Sync、SaveRange、重读、一次 Close、发布的顺序执行。已完成重放只比较字节；关闭错误顺序符合根的裁定。
- `internal/infrastructure/media/fetch_session.go:63`、`internal/infrastructure/media/remote.go:52`、`internal/infrastructure/media/ssrf.go:33`：会话持有文件和响应，复制缓冲仍为 256 KiB；真实 part 长度、无摘要重来、200/206/416规则保留。每次请求重新验证所有地址，拨号固定到已验证地址，保留证书验证，禁用代理和透明解压。
- `internal/application/media/acquisition_order_test.go:1`、`internal/infrastructure/media/checkpoint_order_test.go:1`、`internal/application/media/upload_close_order_test.go:1`：新增测试结合真实数据库、文件和通道屏障，验证取消两方向竞争、重放、关闭次数及真实断点恢复。原上传、下载、重试审计失败、关闭恢复和排队原子性测试只调整消费者组装，所见断言未减弱。
- `internal/archtest/four_layer_test.go:55`、`internal/archtest/transition_manifest_test.go:40`、`internal/archtest/hosts_rules_test.go:129`：只为精确 media 包开放精确 application/media/port，包含拒绝子包及具体应用服务的例子；删除 74 项获取许可。原始清单摘要继续固定，未把删除项改成永久许可。
- `internal/infrastructure/media/import_review_test.go:27`：旧夹具的变更只为 Err 与完成观察加入同一把锁，原内容、失败状态和未发布断言保留；未借此迁移生产导入。

## Issues

### Critical（必须修正）

- 无。

### Important（应修正）

1. `internal/application/media/fetch.go:40`、`internal/application/media/fetch.go:55`：`problem()` 用 `errors.As` 找到错误树中的一个 `Fault` 后，直接返回新的业务错误，丢掉其余错误。真实调用链是 `internal/infrastructure/media/retry_files.go:39` 返回 `errors.Join(fault(InvalidFile), restore())`，再由 `internal/application/media/retry.go:56` 传给这里。若 part 已移到备份，随后发现 final 不是普通文件，同时恢复 part 的重命名也失败，调用方最终只能收到 `ErrInvalid`，不能再发现恢复失败或其原因。旧实现保留了这两个错误；新实现隐藏了“字节仍停在备份里”的故障。转换时应只替换对应 Fault，保留同一错误树中的其他原因及 `errors.Is`/`errors.As` 可见性，不能简单把整棵树替换掉。

   建议在 VM 补一个小测试：让 `ParkRetry` 返回 `errors.Join(&port.Fault{Kind: port.InvalidFile}, restoreFailure)`，调用真实应用 Retry，断言返回值同时匹配 `repository.ErrInvalid` 和 `restoreFailure`，来源仍为 failed/cancelled，未提交重置与审计。现有 `RetryAuditFailure` 覆盖的是 SaveSource 失败后的恢复，不能证明此处的 ParkRetry 失败分支。此次审查未运行该测试。

### Minor（可改进）

1. `.superpowers/sdd/2026-10-23-backend-ddd-phase4-media-jobs/vm-logs/t3-final-3.safe.log:406` 仍有框架调试模式警告；`:281` 有预期的拒绝测试证书握手日志。它们不是本次引入的功能故障，也不应通过削弱测试或更改原验证设置来换通过。但输出并非完全无噪声；后续可单独整理测试日志，保留错误场景的断言。

## Checks / Exceptions / Limitations

- 已读任务 brief、constraints（Global Constraints 与 Review Focus 均按原文约束审查）、contracts-context、handoff、report、`docs/contracts/media.md` 与指定审查模板。
- 差异按顺序分块读完，未重新运行 Git。首次 1–750 行输出被工具截断；仅补读被截断的 421–590 行，其中少量上下文重复。之后使用较小分块，未重读整个差异或整个改动文件。最初合并读取报告也被截断，随后按段补读报告，不据截断认定证据缺失。
- 差异中 recover 的两个上下文块截在函数中间。为确认 allFetched 与最后排序条件，仅补读当前 `internal/application/media/recover.go:26` 起18行及 `:84` 起20行，未重读整个文件。
- 外部检查 1：具体风险是 `CheckURL`/`CheckLocalPath` 改为 Fault 后，旧调用者可能直接暴露错误而未转换。仅对 `internal` 与 `cmd` 搜索这两个符号的调用，生产调用均落在已审查的 Transfer/网络实现中，没有发现遗留调用方漏转。
- 外部检查 2：具体风险是共享来源锁只接到新对象，旧 Import 仍可能走私有锁。仅检查未改 `import.go` 的锁调用，确认它调用 `s.lockSource(id, "@import")`，与本次替换后的委托相连。
- 读取安全证明：`t3-final-3.tree:1` 为 `d6fb356035da818fe34d1c0edb5813b7e69e9e2d`，`t3-final-3.exit:1` 为0；changes 与 untracked 文件大小均为0。安全日志普通四包成功位于62/404/2125/3600行，并发四包成功位于3662/4004/5725/7200行，console与架构成功位于9284/9285行。测试树与候选源码一致性采用根已核实的比较结果，未重复 Git 检查。
- 未运行本机 Go、格式化、Wire、Node、构建或测试；未重跑 VM 测试。未读取私有连接文档、运行配置、DSN、原始日志或其他计划草稿。未创建子代理。唯一写入为本审查报告。

## Assessment

**Task quality: Needs fixes**

拆分边界、共享锁、上传发布顺序和真实竞争测试整体扎实。先修复 Important 1，使文件恢复失败不会被错误转换隐藏，再审查该修复与针对性验证即可；此结论不要求扩大为全分支测试。


## 第1轮修复复审

- **错误转换丢失独立恢复失败** — **ADDRESSED**。原发现：`problem()用errors.As找到错误树中的一个Fault后，直接返回新的业务错误，丢掉其余错误。真实调用链ParkRetry返回errors.Join(fault(InvalidFile), restore())，Retry传给这里。part已移备份、final非法且恢复重命名失败时，调用方只得ErrInvalid，看不到恢复失败；旧实现保留两者。转换时只替换对应Fault，保留同树其他原因及errors.Is/errors.As可见性，不可整树替换。`
- **修复证据**：`internal/application/media/fetch.go:42` 递归处理错误树；`:50` 遍历合并分支，`:60` 仅重新合并已转换的结果。无 Fault 的独立分支在 `:45` 保留原对象。已知 Fault 在 `:70` 起转换为原业务错误，因此 InvalidFile 与恢复失败均能保留。
- **调用覆盖**：`internal/application/media/retry_error_test.go:30` 使用真实应用 Retry、数据库和部分上传文件，分别覆盖 failed/cancelled。`:68`、`:72` 检查业务错误、恢复错误对象、底层原因及类型；`:76`、`:79`、`:83`、`:87` 检查来源、审计、区间和字节未被重置。
- **安全兼容性**：`internal/application/media/fetch.go:48` 在直接遇到 Fault 时停止向其 Cause 递归，只返回安全业务错误；单层外包装在 `:64` 被去掉，未把内部提示重新带回。`internal/application/media/source_error_test.go:53` 覆盖六种已知 Fault 的直接、包装、合并形式，`:65` 同时检查文本、errors.Is 和 errors.As 不泄露内部原因。`:15` 另覆盖嵌套、多个 Fault 和第三独立原因。

### New Breakage in the Fix Diff

- **None**。未发现本次修复新引入的 Critical、Important 或 Minor 问题。普通、空、未知错误继续保留；多 Fault 均按原业务规则转换；独立恢复错误继续可识别。

### Checks

- 已按指定复审模板读取任务 brief、constraints、初审报告、执行报告末尾修复节，以及 `review-9b88bd2..aef267b.diff`。差异只读一次，未执行 Git。首次合并输出截断了执行报告中段，之后只补读末尾修复记录。
- **具名外部风险检查：隐藏原因是否通过 Fault.Unwrap 泄露**。仅补查 `internal/application/media/port/transfer.go:65` 的 Fault 定义及 `:77` 的 Unwrap；Cause 确实是可被遍历的内部原因。新转换在直接 Fault 处截断，保留独立分支的同时继续隐藏该原因。未重新审查其余未改代码。
- `task-3-report.md:411` 起列明三个源码/测试文件；`:455` 起列明应用媒体全包及基础设施相关用例的普通与并发命令，保留 `-race -timeout=20m`。`:428` 提供耗时与结果，`:466` 固定本轮提交和测试树比较结论。
- 已核对安全日志 `vm-logs/t3-fix1-final-1.safe.log:17`、`:28`、`:78` 与 `:172`、`:183`、`:233`：新增错误保留、隐藏内部原因和真实 Retry 用例在普通/并发两轮均 PASS。`:90`、`:155`、`:245`、`:310` 为四条包级成功结果，耗时与报告一致。定向搜索未见 FAIL、SKIP 或数据竞争标记。
- 已读取 `t3-fix1-final-1.exit:1` 为 0、`t3-fix1-final-1.tree:1` 为 `1ebbfa3b4e2f79e529c322fb261aa087aa56e177`；changes/untracked 文件均为 0 字节。源码与测试树一致性采用根已独立核实的结果，没有重复 Git 比较。
- 未重跑任何测试，未运行本机 Go、gofmt、Wire、Node。未读取私有连接文件、运行配置、DSN、原始日志或其他计划；未创建子代理。唯一写入为本复审报告。

### Out-of-Scope Observations

- **None（新增）**。初审提到的既有 GIN/TLS 测试日志噪声仍按原裁定延期，不阻塞本次修复，也未扩展检查。

### Verdict

- **Fix round: All findings addressed, no new Critical/Important breakage**。本轮可通过；没有遗留的本轮阻塞项。


## 根执行者逐项处理

1. 初审重要问题：旧错误转换丢掉合并错误中的文件恢复失败。修复后递归映射业务故障，保留独立分支和Is/As，同时不重新暴露故障内部细节。复审已确认解决，无新增问题。
2. 差异外保证：真实仓储普通与竞态检查已运行，网页全包通过。本项不替代Task4的导入迁移或Task5的删除与生命周期迁移；两项继续核对原完整测试和危险顺序。Task6再做全仓及整分支检查。
3. 真实镜像与全仓范围：TestIdentifyRealISO因未配置路径在普通/竞态各跳过一次，不能声称实物镜像兼容通过。make check按批准计划留Task6，尚未执行。
4. 测试与候选一致：根核对final-3和fix1-final-1的tree/exit/时间/changes/untracked及安全输出。最终修复树1ebbfa3b4e2f79e529c322fb261aa087aa56e177退出0；git比较与aef267b只差计划、执行记录两项Markdown，源码、测试和设置一致。
5. 初审轻微项：详细日志含既有框架调试警告和预期证书握手拒绝日志。未通过改设置隐藏；已登记延期，整分支审查继续看到此项，不称输出完全无噪声。
