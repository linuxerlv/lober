本记录保留执行或审查当时的状态。最终交付及验证见[第三阶段验收记录](2026-10-23-ddd-phase3-evidence.md)。本机临时目录清理被自动审批策略阻止，现保留在Git忽略范围中；原始运行日志也保留在 lober VM 的第三阶段测试目录。

Spec compliance verdict: PASS（Task 3 范围）。未发现需要修改的规格偏离。阶段总验收仍待 Task 4。

审查范围：`cfc8ddfb2b71f41f98694109ac7889d486cf815d..1eecac5ef8800435a259fe6cd0f6c1b61578af9b`。按任务说明、上下文、执行报告及完整差异逐项核对。没有修改源码、索引或 HEAD，没有运行 Go、测试或连接虚拟机。

## 规格逐项核对

| 要求 | 核对结果与证据 |
| --- | --- |
| Registry 增加真实 Import；仓储口增加两方法 | `application/hosts/contract.go:11` 增加 Import；`port/repository.go:11` 明确声明 GetHosts、ImportHosts。原五方法保留，最终七方法，没有通用事务口。请求、计划、结果及两种指针错误符合约定，不带网页标签或调用者可填写的审计字段。 |
| 授权、解析、查询、拒绝、写入顺序 | `application/hosts/import.go:10` 先授权，再完整解析；有效且唯一编号只查一次，空集合也调用。查询错误先于预演或行拒绝。只有正式有效请求调用一次 ImportHosts 和一次 audit。原 context 直传，没有重试、逐条保存或额外取消检查。 |
| 应用测试先红及重要断言 | `import_test.go:56/79/87/103/121` 覆盖身份与权限错误、后段 CSV 格式错误不查库、空集合仍查、读取失败优先、预演/拒绝无时钟与写入、可信审计和仓储实际返回版本。失败先行的树和退出码有保存；具体编译失败原因的独立确认见下方限制。 |
| CSV 完整兼容 | `csv.go:17` 与被删除的网页解析器逐段对应，保留 encoding/csv、BOM、表头规则、FieldPos 物理行号、属性原文、领域校验、全部重复行及问题顺序。没有引入 CSV 旧版本拒绝。原三组解析断言迁移至 `csv_test.go:10/30/39`，update 断言改用真实 Import 预演并记录查询编号；`:53` 又固定重复问题后原问题次序。 |
| 网页职责和错误映射 | `console/hosts.go:157` 只调用应用及转换结果。dryRun 解析和读取正文的原顺序保留。`console/csv.go:16` 仅作字段转换，保留旧公开响应结构及空数组。`console/errors.go:53` 按两种错误字段映射 400/null 和 422/完整计划，没有调用 Error() 重算业务。 |
| 预演后覆盖最新数据库记录 | `hosts_contract_test.go:406` 使用两个仓储实例。预演 create 后另一实例先创建；正式导入分别断言 Rev=2、Rev=3、完整替换属性、空 next、无 total，并由另一实例读回比较。 |
| 真实数据库整批回滚、取消与成功审计 | `hosts_contract_test.go:609` 在 SN-2 主机写入失败、SN-1 导入审计失败、等待 SN-2 锁时取消三种情形下，完整比较三台主机和全部审计前后值。失败后成功批次按 SN-2/SN-1 返回；按审计自增编号核对顺序、操作者、时间及空明细。SN-1 排在 SN-2 前，因而写入失败和取消发生在实际批次中途。 |
| 两种确定的并发交错 | `hosts_contract_test.go:432/447/502` 使用独立连接、写入口信号、事务审计触发器和门闩表。观察连接要求看到 pg_blocking_pids、Lock 等待与未授予锁后才放行。主机先提交时，任务进入真实创建后等待主机锁，最终 422/arch_mismatch，任务及 task.create 审计均为零。任务先提交时，导入等待主机锁；最终任务响应及持久化快照均为 x86_64，登记主机为 aarch64/Rev=2。没有 sleep 或新进程锁代替数据库等待。 |
| 删除 Deps.Hosts；测试组装与冻结预览 | `console/server.go` 删除字段，`cmd/lober/app.go` 删除传入值。`harness_test.go:39` 的 store 显式含 repository.Hosts；Registry、Wizard、AuthoringEditor 分别直接取得所需口。`:50` 保留 CurrentTaskCreator 覆盖，Wizard 与 TemplateAuthoring 继续使用同一个 guard。关闭仓储测试改注入真实 Registry；`tasks_test.go:240` 同时注入关闭的主机与编写读取口，完整冻结结果比较保留。 |
| 精确删除临时放行 | 本差异仅删除阶段 3 的 GetHosts、ImportHosts、Deps.Hosts 三项，以及 CSV 五项领域调用，共八项。`authoring_rules_test.go:97` 将 269 改为 261；阶段 2 和向导清零检查保留。没有新增、改名、转移或扩大其他阶段放行。累计 16 项另见下方范围限制。 |
| 指定聚焦检查及提交 | 保存的 t3-green-1.tree 为 bae3110fecae7ad0b388fea189ad2dff2e73f55f，退出码 0；t3-final-1.tree 为 96eea3ffa3efaacfbdd5daabd818b8600a85b036，退出码 0。控制者已确认四组指定检查、最终应用/格式检查及两树差异仅为两份应用测试增强。差异包提交名称符合约定。未将未运行的完整 make 算作已通过。 |

## Strengths

- `application/hosts/import.go:10` 保留原流程和错误先后。应用只组织步骤，校验继续使用原领域方法，事务继续交给原仓储。
- `application/hosts/csv_test.go:10` 不仅搬测试，还把“已存在则 update”放到实际预演流程验证。属性空白、真实换行及物理行 2/4 的断言完整保留。
- `console/hosts_contract_test.go:447` 的等待条件由数据库报告。进入信号只证明到了写入入口，没有冒充锁等待；两个竞争方向都检查持久化结果。
- `console/hosts_contract_test.go:609` 回滚比较包含记录内容、版本和审计，成功后也核对输入顺序，避免只检查 HTTP 状态。
- 架构许可删除与实际消费者消失相对应，没有通过新增例外掩盖越界调用。

## Critical / Important / Minor findings

- Critical：无。
- Important：无。
- Minor：无。

## Cannot verify from diff

1. Task 1 的完整旧行为基线通过、Task 2 的五方法交付及之前八项许可删除不在本次差异中。控制者应结合前两份审查和证据，确认累计按两类删除 10+6=16；本次可确认的增量为 3+5=8、269→261。
2. 全分支检查、完整 make check、Wire 漂移、所有路由及其他模块的全量回归不属于本次聚焦审查。Task 4 应在最终候选树完成并记录实际结果；不能用本报告替代阶段验收。
3. 已读取安全的 `.tree`/`.exit` 文件，未读取原始日志或任何私有配置。退出码能证明命令结束情况，不能单独证明每项数据库测试没有跳过，或 RED 确因缺契约编译失败。执行报告给出了这些结论，控制者也确认了最终检查；控制者应保留对应经过脱敏的命令与运行证明。此项是独立审查的证据边界，不是测试失败发现。

## 定点读取说明

- 第一次差异输出被工具截断，只补读未展示的中段；没有重新读取整份差异。
- 具体风险：harness 差异在 CurrentTaskCreator 覆盖分支中间截断，可能丢失覆盖。只补读 `harness_test.go:48-60`，确认 override[0] 仍被采用。
- 具体风险：新增回滚测试声称“中间失败”，实际仓储可能先操作故障行。定点查看未修改的 `pgstore/hosts.go` 中 ImportHosts 和 hostLockOrder，确认按编号排序写入、SN-1 在 SN-2 前，以及同事务写记录和审计。读取工具附带的邻接内容未用于扩大审查范围。
- 具体风险：网页从分开的 Get/Write 改为统一 Write，可能改变预演错误或带入副作用。只定点查看未修改的 `console/errors.go:193-201`，确认 Write 直接委托 Get，无新增副作用。

Task quality: Approved
