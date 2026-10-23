本记录保留执行或审查当时的状态。最终交付及验证见[第三阶段验收记录](2026-10-23-ddd-phase3-evidence.md)。本机临时目录清理被自动审批策略阻止，现保留在Git忽略范围中；原始运行日志也保留在 lober VM 的第三阶段测试目录。

# Task 2 实施报告（已提交，待独立审查）

范围：仅 Task 2 brief 的15条路径；根代理负责进度记录及独立审查。
起点：75f3ada55e901c3a1687c7f1c48649894dc2e39f，分支 codex/ddd-hosts。
提交：cfc8ddfb2b71f41f98694109ac7889d486cf815d（refactor(hosts): extract registry application）。
最终提交树：b526e88097fc962e6bc6b1020f6f3ed1a8875d99；本地工作区干净。

## 变更

- 新建 hosts.Registry/Deps 与五方法能力接口，五个方法先授权，再保留原查询、编号和版本检查顺序。
- 固定可信审计 host.create/update/delete，Detail为空。只有调用写仓储时读取一次 Clock。
- 网页五入口改用 HostRegistry；保留公开请求、响应、queryRev数字解析与CSV旧路径。
- app/provider/test helper 显式组装；console.New 没有新增默认服务。
- 删除CRUD仓储7项许可与normalizedHostSN规则1项，总数277→269；原阶段2/向导清零断言保留。
- 新增纯应用替身测试；真实HTTP/应用/数据库测试覆盖创建、替换、删除审计失败及全部任务状态的删除引用保护。
- 原Task1所有断言与原有冻结任务预览用例未改动。

## RED 和迭代

全部 Go 测试、gofmt、Wire 都经 vm-check.py 在 lober 运行。本机只做文本、Git和传输。
日志均相对本报告所在目录。

| 标签 | 记录的源树 | 实际退出 | 执行与结果 |
| --- | --- | --- | --- |
| t2-red-1 | f9b02a909e2f9d444cad5464319b74ccb95dcf48 | 1 | `go test ./internal/application/hosts -count=1`，缺 Registry、ListRequest、UpdateRequest、DeleteRequest、NewRegistry；符合缺应用入口的预期 |
| t2-format-1 | 4381d6691d8bc6ccd5b304609dc5f4e7cc37207f | 0 | task2-format.commands 的精确文件 gofmt 与 Wire gen；已取回全部实际变化文件 |
| t2-green-2 | 8da159a9b8440fffefc32a5a80023434d67079e5 | 1 | task2-green-2.commands；应用包通过，console编译发现hosts.go中迁移后未使用的repository导入，后续命令未执行 |
| t2-green-3 | 25a9748425cef114843d4e3e0fd79834d7c2e3fd | 1 | task2-green-3.commands；应用包与cmd/lober通过；console新增审计回滚夹具创建失败，后续架构/数据库/Wire diff均未执行 |
| t2-green-4 | 662c99383140d0231d231c08740d5e81db16c089 | 0 | task2-green-4.commands；应用、console/cmd、架构、数据库契约、Wire diff全部实际执行并通过 |
| t2-final-1 | 7315d8a1462e0a678cb3d8761ea50783e12fece5 | 0 | task2-final.commands；只查格式，全聚焦与Wire diff通过，VM无任何源码差异 |
| t2-assembly-format-1 | 2c8a43ea458131f3adf5dbcb630b1991e89f9f4a | 0 | 独立gofmt cmd/lober/assembly_test.go，实际格式变化已取回、重暂存 |
| t2-assembly-final-1 | b526e88097fc962e6bc6b1020f6f3ed1a8875d99 | 0 | 只查格式，cmd主机聚焦0.018秒，Wire diff通过，VM无源码差异；提交后verify-proof确认同树 |

`t2-green-1` 未启动Go：传输要求先取回所有VM变化（server.go），取回后去掉一次本地文本重试造成的重复字段/导入，再以t2-green-2运行。
这是传输失败，没有Go测试退出码。t2-green-2的编译失败只删除了已不用的导入，没有改断言。

RED日志：vm-logs/t2-red-1.log/.exit/.tree。
格式日志：vm-logs/t2-format-1.log/.exit/.tree/.changes；Wire生成的 cmd/lober/wire_gen.go 从VM实际取回。
GREEN迭代日志：vm-logs/t2-green-2.log/.exit/.tree/.changes、vm-logs/t2-green-3.log/.exit/.tree/.changes、vm-logs/t2-green-4.log/.exit/.tree/.changes。

t2-green-3的失败原因：pgtest.FaultActor含连字符，不满足网页用户名称规则。没有更改账号或生产审计规则；复用password_test.go既有包装方法，在测试仓储边界将审计Actor替换为故障标记。应用仍使用真实已验证网页身份。修正放在hosts_contract_test.go。

## 自审

- 应用测试只有边界替身，不反向导入基础设施；没有复制Host.Validate/CheckHosts。
- NormalizeSN错误仍为原model.Problem；Rev和身份不匹配为原ErrInvalid，稳定仓储错误原样透传。
- prefix和属性原文不规范化、不裁剪；替换省略字段清空；新建Rev不加门槛。
- 更新检查保持Rev→路径编号→正文编号→身份比对→仓储记录/版本；删除Rev→编号→原仓储。
- 接口仅五个真实方法，无Import空实现；Deps.Hosts暂只供生产CSV，测试读取口按brief保留。
- SQL、表结构、依赖、配置和任务创建锁代码没有修改。

## 最终证明

t2-green-4记录的是格式前源树662c99383140d0231d231c08740d5e81db16c089；命令先执行gofmt，实际被测源码已格式化，vm-logs/t2-green-4.changes列出hosts_contract_test.go。因此本轮通过不冒充最终同树证明。

green4各包实际结果：应用0.011秒、console33.644秒、cmd/lober2.319秒、archtest156.602秒、pgstore17.906秒，整段脚本exit=0。没有跳过测试。已将该格式文件从VM取回并重新暂存，最终固定源树7315d8a1462e0a678cb3d8761ea50783e12fece5。

task2-final.commands只检查格式、不写文件，在固定源树7315d8a1462e0a678cb3d8761ea50783e12fece5全聚焦通过，实际exit=0，changes与untracked均为空。各包：应用0.011秒、console33.456秒、cmd/lober1.964秒、架构146.560秒、数据库18.508秒，Wire diff通过。

随后按根代理裁定，仅将assembly_test.go的新真实数据库用例换为无数据库的provider契约替身测试。最终新树b526e88097fc962e6bc6b1020f6f3ed1a8875d99，t2-assembly-final-1实际exit=0；cmd主机聚焦0.018秒及Wire diff通过，VM changes/untracked均为空。
`git diff 7315d8a1462e0a678cb3d8761ea50783e12fece5 HEAD --name-only`仅输出cmd/lober/assembly_test.go。其余生产代码、应用/网页/数据库测试及架构规则与全聚焦通过树完全相同，遵根裁定不重复不受影响的套件。
完整make check按控制要求留给Task4；本任务不合并、不推送。

## 精确文件清单

- `internal/application/hosts/contract.go`
- `internal/application/hosts/registry.go`
- `internal/application/hosts/registry_test.go`
- `internal/application/hosts/port/repository.go`
- `internal/interfaces/console/hosts.go`
- `internal/interfaces/console/server.go`
- `internal/interfaces/console/harness_test.go`
- `internal/interfaces/console/hosts_contract_test.go`
- `cmd/lober/app.go`
- `cmd/lober/wire.go`
- `cmd/lober/wire_gen.go`
- `cmd/lober/assembly_test.go`
- `internal/archtest/migration_test.go`
- `internal/archtest/transition_manifest_test.go`
- `internal/archtest/authoring_rules_test.go`

## 本任务核对

- 权限矩阵共5方法×9种身份×合法/坏输入；无身份、无效角色、禁用同时带改密标记，用来固定身份检查优先。强制改密使用viewer，固定其优先于写权限不足。
- 未授权共享断言使用brief指定的repoCalls和clockCalls，二者均须0。
- 应用顺序测试覆盖坏Rev与坏路径并存、坏路径与正文不匹配并存、坏正文编号、身份不匹配、正反向规范化、空字段清空和三种创建Rev。
- 错误透传用同一错误实例核对五方法，并核对请求context保持原样。坏Arch仍交给仓储，不新增记录内容校验。
- 列表测试核对原文prefix、原文属性值、未知属性键、cursor/next/total；无效分页或属性配对在应用中拒绝。
- 三种审计失败经HTTP→真实Registry→故障注入仓储→PostgreSQL；完整对象、Rev、审计前后对比。
- 删除保护覆盖pending、booting、installing、rebooting、initializing、done、failed、cancelled八种状态；拒绝后对象和审计不变。
- 组装测试用嵌入Store并只实现五方法的替身，直接验证hostRegistryOf传递仓储、请求上下文和Clock；实际Wire生成/diff及独立审查验证生产接线。

未发现需要改变设计的事项；没有扩展任务范围。独立审查由根代理安排。

## 实际命令原文


task2-red.commands：

```sh
go test ./internal/application/hosts -count=1
```

task2-format.commands：

```sh
gofmt -w internal/application/hosts/contract.go internal/application/hosts/registry.go internal/application/hosts/registry_test.go internal/application/hosts/port/repository.go internal/interfaces/console/hosts.go internal/interfaces/console/server.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts_contract_test.go cmd/lober/app.go cmd/lober/wire.go cmd/lober/assembly_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/archtest/authoring_rules_test.go
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire gen ./cmd/lober
```

task2-green-2.commands：

```sh
set -euo pipefail
gofmt -w internal/interfaces/console/server.go internal/application/hosts/registry_test.go
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/archtest -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发|Test默认隔离' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

task2-green-3.commands：

```sh
set -euo pipefail
gofmt -w internal/interfaces/console/hosts.go internal/application/hosts/registry_test.go
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/archtest -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发|Test写事务不随连接默认的隔离级别变' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

task2-green-4.commands：

```sh
set -euo pipefail
gofmt -w internal/interfaces/console/hosts_contract_test.go internal/application/hosts/registry_test.go
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/archtest -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发|Test写事务不随连接默认的隔离级别变' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

task2-final.commands：

```sh
set -euo pipefail
test -z "$(gofmt -l internal/application/hosts/contract.go internal/application/hosts/registry.go internal/application/hosts/registry_test.go internal/application/hosts/port/repository.go internal/interfaces/console/hosts.go internal/interfaces/console/server.go internal/interfaces/console/harness_test.go internal/interfaces/console/hosts_contract_test.go cmd/lober/app.go cmd/lober/wire.go cmd/lober/wire_gen.go cmd/lober/assembly_test.go internal/archtest/migration_test.go internal/archtest/transition_manifest_test.go internal/archtest/authoring_rules_test.go)"
go test ./internal/application/hosts -count=1
go test ./internal/interfaces/console ./cmd/lober -run 'Test主机|TestCSV|Test向导|Test任务预览|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/archtest -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发|Test写事务不随连接默认的隔离级别变' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```

## 根代理裁定

context明确所有新增真实数据库用例放hosts_contract_test.go。最初新增的Test主机应用生产组装复用了真实数据库，虽然在允许修改的assembly_test.go中，仍不满足这个全局限制。
根代理裁定删除该新增真实数据库用例，改为无数据库的provider契约替身测试；生产Wire接线通过实际生成/diff和独立审查验证。Task2文件列表不形成例外。
此次只调整assembly_test.go中的新测试，不改生产代码；等原final完整结束并保留日志后，再独立format/pull/restage并只跑受影响cmd主机聚焦和Wire diff。

## 提交后核对

- `git diff --cached --check`通过；准确暂存且提交上述15条拥有路径，没有其他路径。
- `git show -s --format=%T HEAD`输出b526e88097fc962e6bc6b1020f6f3ed1a8875d99。
- `verify-proof.py t2-assembly-final-1 --commit cfc8ddfb2b71f41f98694109ac7889d486cf815d`实际exit=0；报告tested tree与commit tree均b526e88097fc962e6bc6b1020f6f3ed1a8875d99，VM命令后工作区干净。
- `git status --short`无输出。未合并、未推送。
- 日志/证明：vm-logs/t2-final-1.{log,exit,tree,started,finished,changes,untracked}；vm-logs/t2-assembly-format-1同名证据；vm-logs/t2-assembly-final-1同名证据。

## 仍需的工作与顾虑

Task2未发现尚未解决的问题；独立审查由根代理接续。本任务没有执行完整make check，按控制要求由Task4执行。Import与删除最后的Deps.Hosts字段属于Task3，未预先增加空实现或临时许可。

task2-assembly-format.commands：

```sh
set -euo pipefail
gofmt -w cmd/lober/assembly_test.go
```

task2-assembly-final.commands：

```sh
set -euo pipefail
test -z "$(gofmt -l cmd/lober/assembly_test.go)"
go test ./cmd/lober -run 'Test主机' -count=1
WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0 go tool wire diff ./cmd/lober
```
