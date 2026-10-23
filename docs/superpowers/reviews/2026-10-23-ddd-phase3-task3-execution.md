本记录保留执行或审查当时的状态。最终交付及验证见[第三阶段验收记录](2026-10-23-ddd-phase3-evidence.md)。本机临时目录清理被自动审批策略阻止，现保留在Git忽略范围中；原始运行日志也保留在 lober VM 的第三阶段测试目录。

# Task 3 执行报告

## 范围

CSV 解析与预演/导入编排迁入 application/hosts。Registry 增加 Import；读写口明确增加 GetHosts/ImportHosts。console.Deps.Hosts 删除，网页通过 HostRegistry 调用。数据库仓储、锁顺序、任务快照、模板选择锁均未修改。

## 失败先行

- `t3-red-1`，源码树 `6cf3f40fb0117c3561898e324fe9d09a1b1904e5`。
- 命令 `go test ./internal/application/hosts -count=1`。
- 实际退出 1；编译失败定位在新增测试：Registry.Import、ImportRequest、CSVFileError 等任务要求的契约尚不存在。
- 原始日志 `vm-logs/t3-red-1.log` 保留。没有把缺工具、环境问题或其他失败作为此红色证据。

## 检查记录

- `t3-format-1`：源码树 `fce7148e528bd27480dc6a225e5767910ae228c3`，实际退出 0。
- 格式化只在 lober 虚拟机进行；取回 14 个被工具修改的文件后重新暂存。
- `t3-green-1`：源码树 `bae3110fecae7ad0b388fea189ad2dff2e73f55f`。实际退出 0；应用 0.014s，网页 40.325s，cmd/lober 0.014s，架构 153.676s，仓储 17.379s。运行开始 2026-10-23T05:48:24Z，结束 05:52:08Z。
- `t3-format-2`：仅补强两份应用测试的格式化，源码树 `e2413577b5389ff800318a7597373f16741d9d2c`，实际退出 0；取回两份格式化文件并重新暂存。
- 自审后仅加强 `csv_test.go` / `import_test.go` 两份应用测试：授权错误精确匹配，重复问题后的原问题次序，以及保留仓储实际返回版本。按控制者指示，四组检查通过后不重复未受影响套件，只在最终树复跑应用包和 gofmt -l，附两树精确差异。
- `t3-final-1`：最终源码树 `96eea3ffa3efaacfbdd5daabd818b8600a85b036`，实际退出 0；应用包 0.016s，格式断言通过。05:54:59Z 开始，05:55:00Z 完成，源码 changes/untracked 为空。
- 提交 `1eecac5ef8800435a259fe6cd0f6c1b61578af9b`，`refactor(hosts): move CSV import behind contract`；提交树与上述最终源码树相同。
- 本地 `git diff --cached --check` 通过；提交后工作区干净。没有在本机运行 Go、gofmt、Wire、构建或测试。
- `git diff --name-only bae3110fecae7ad0b388fea189ad2dff2e73f55f 96eea3ffa3efaacfbdd5daabd818b8600a85b036` 只有以下两份测试文件，生产、console、架构和仓储源码与四组通过树完全相同：
  - `internal/application/hosts/csv_test.go`
  - `internal/application/hosts/import_test.go`


`task3-green.commands` 在 green-1 执行的完整检查如下：

```sh
test -z "$(gofmt -l cmd/lober/app.go internal/application/hosts/contract.go internal/application/hosts/csv.go internal/application/hosts/csv_test.go internal/application/hosts/import.go internal/application/hosts/import_test.go internal/application/hosts/port/repository.go internal/archtest/authoring_rules_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/interfaces/console/csv.go internal/interfaces/console/csv_test.go internal/interfaces/console/errors.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts.go internal/interfaces/console/hosts_contract_test.go internal/interfaces/console/hosts_test.go internal/interfaces/console/server.go internal/interfaces/console/tasks_test.go internal/interfaces/console/wizard_test.go)"
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware'
go test ./internal/archtest
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test同时导入主机与建任务不会互相等死|Test同时导入同一组主机不会互相等死|Test写事务不随连接默认的隔离级别变' -count=1
```

原始日志与证明全部保留在 `vm-logs/`：`t3-red-1`、`t3-format-1`、`t3-green-1` 的 `.log`、`.tree`、`.exit`、`.started`、`.finished`、`.changes`、`.untracked` 文件，后续格式与补强检查同样按独立运行标签保存。


## 自审

- 先授权，再完整解析文件。格式错误不读数据库。有效且唯一编号一次 GetHosts，空集合也调用。查询错误先于预演或整批拒绝。
- 预演、坏行拒绝均不调用时钟或写入。正式导入只调用一次原子 ImportHosts，操作者来自 Actor，固定 host.import，共用一次时钟值。
- CSV 保留原 encoding/csv 解析：开头 BOM、原物理行号、属性空白、引号内真实换行、所有重复行及问题顺序。三个旧解析测试迁入应用，存在主机的 update 断言通过预演和可记录读口确认。
- 文件错误与整批拒绝使用指定指针错误；网页只读字段映射为原 400/null 与 422/计划，没有通过 Error() 重算业务。
- 测试组装显式包含 repository.Hosts。Registry、Wizard、AuthoringEditor 各自从 store 获得读写口；CurrentTaskCreator 覆盖和共用 SelectionGuard 原样保留。
- 关闭仓储测试注入真实 Registry；冻结任务预览同时把 Registry 和编写读口指向关闭仓储，原完整比较保留。
- 新增真实数据库测试全部在 console/hosts_contract_test.go；应用测试只有契约替身。
- 预演 create 后，第二连接先创建，正式导入实际返回 Rev=2，再次 Rev=3。
- 中间主机写入失败、中间审计失败、等待第二台主机锁时取消，都比较整批原记录、版本及全部审计不变。成功以 SN-2/SN-1 返回，并按审计递增编号验证输入顺序、操作者、时间及空明细。
- 两种竞争顺序使用独立仓储连接池。审计触发器访问测试 schema 门闩表，在写事务内暂停提交；独立观察连接必须看到 pg_blocking_pids 和未授予锁。进入信号只报告真实写入口，不代替数据库阻塞。
- 主机先锁：任务已读旧事实，进入真实创建后等待主机锁。主机提交后任务 422，SN-1 首项 arch_mismatch，任务及 task.create 审计 0。
- 任务先锁：导入等主机锁，任务先提交再导入。任务成功且持久化快照 x86_64，登记主机 aarch64/Rev=2。
- 删除阶段 3 剩余 3 项仓储/字段许可和 5 项 CSV 规则；总数 269→261。其他阶段未删项。

## 范围外

本任务不运行完整 make check；Task 4 负责完整验收。未合并、推送或推进环境。未改变表结构、配置、生成代码、依赖、进程锁或当前任务创建逻辑。

## 修改文件

- `cmd/lober/app.go`
- `internal/application/hosts/contract.go`
- `internal/application/hosts/csv.go`
- `internal/application/hosts/csv_test.go`
- `internal/application/hosts/import.go`
- `internal/application/hosts/import_test.go`
- `internal/application/hosts/port/repository.go`
- `internal/archtest/authoring_rules_test.go`
- `internal/archtest/migration_test.go`
- `internal/archtest/transition_manifest_test.go`
- `internal/interfaces/console/csv.go`
- `internal/interfaces/console/csv_test.go`
- `internal/interfaces/console/errors.go`
- `internal/interfaces/console/harness_test.go`
- `internal/interfaces/console/hosts.go`
- `internal/interfaces/console/hosts_contract_test.go`
- `internal/interfaces/console/hosts_test.go`
- `internal/interfaces/console/server.go`
- `internal/interfaces/console/tasks_test.go`
- `internal/interfaces/console/wizard_test.go`

## 最终树补强检查命令

```sh
test -z "$(gofmt -l cmd/lober/app.go internal/application/hosts/contract.go internal/application/hosts/csv.go internal/application/hosts/csv_test.go internal/application/hosts/import.go internal/application/hosts/import_test.go internal/application/hosts/port/repository.go internal/archtest/authoring_rules_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/interfaces/console/csv.go internal/interfaces/console/csv_test.go internal/interfaces/console/errors.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts.go internal/interfaces/console/hosts_contract_test.go internal/interfaces/console/hosts_test.go internal/interfaces/console/server.go internal/interfaces/console/tasks_test.go internal/interfaces/console/wizard_test.go)"
go test ./internal/application/hosts -count=1
```

## 实际结果与限制

所有本任务指定聚焦检查实际通过，没有跳过或未解释的失败。唯一失败运行是上述契约尚不存在的 RED。两次格式化单独执行、取回、重新暂存后才运行对应检查。完整 make check 与独立任务审查仍由后续 Task 4 / 控制者执行；本报告不宣称阶段总验收已通过。未发现未解决的事务、错误顺序或测试注入问题。
