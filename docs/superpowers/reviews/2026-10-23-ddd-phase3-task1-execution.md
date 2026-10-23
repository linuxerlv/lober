本记录保留执行或审查当时的状态。最终交付及验证见[第三阶段验收记录](2026-10-23-ddd-phase3-evidence.md)。本机临时目录清理被自动审批策略阻止，现保留在Git忽略范围中；原始运行日志也保留在 lober VM 的第三阶段测试目录。

# Task 1 执行报告

状态：Task 1 已完成。两条基线命令真实通过，格式与最终提交源码树一致；未合并或推送。

## 范围与源码

- 分支：`codex/ddd-hosts`。
- 起点：`69b3b46e3d9838c3421023b03ed1ef986a84a1e6`。
- 提交：`75f3ada55e901c3a1687c7f1c48649894dc2e39f`，`test(hosts): pin registry and CSV contracts`。
- 最终提交树及 VM 测试源码树：`c25688073818ffaca40889c4d68386eb312426d8`。
- 仅修改 `docs/contracts/hosts.md`、`internal/interfaces/console/hosts_contract_test.go`、`docs/contracts/backend-modules.md`。
- 没有生产代码、依赖、数据库结构、旧测试或权限规则改动。原真实登录和口令用例完整保留。
- Task 1 是旧行为刻画，按授权先取得真实通过；没有人为制造生产失败。

## 新增约束

1. 手写六条 method/path/perm/audit 集合，实际走五角色网页权限检查。两条读取检查返回内容，允许写入检查真实创建、替换、预演、删除内容；拒绝写入检查 403、提示、空明细及记录未变。检查实际审计动作、操作者、时间与空详情。
2. 固定坏 Rev、非 ASCII 路径编号、正文编号不匹配的错误先后；删除版本溢出优先；创建空编号拒绝，负数、零及任意正版本仍创建为 1。
3. 固定 SN-1/SN-10/SN-2 字节序，两种分页、总数、游标、空列表、空字段混用、重复页码与数字溢出。小写前缀与属性空白保持原文；属性键不新增格式检查。
4. 固定 dryRun 先于文件解析，后段列数错误整份 400/null；有效行和错误行混合时读取仓储失败优先为 500。正常预演 200，提交 422 且同计划，记录及审计均不变；空 dryRun 拒绝，重复参数保留首值。

## 执行记录

所有 Go 与格式化均只在 lober VM 执行。本机仅文本、Git 和 Python 传输。根代理确认实际主机 lober、实际测试库 lober_test；测试使用临时 schema。没有读取私有连接文件或凭据。

| 运行 | 暂存源码树 | 退出 | 结果 |
| --- | --- | --- | --- |
| `t1-format-1` | `ed265538504bf4f862b489250f9797a1d8a3e2ef` | 0 | VM gofmt；精确取回 Go 文件并重新暂存 |
| `t1-baseline-1` | `b18eee02b0b00d4864a2eca17dbd22661b4f8ccd` | 1 | 新增四测试停在账号夹具；用户名误用了连字符，未进入主机断言；第二条数据库命令未执行 |
| `t1-baseline-2` | `c25688073818ffaca40889c4d68386eb312426d8` | 0 | COMPLETE；console 12.031 秒，pgstore 20.388 秒；两条原命令均实际通过 |
| `t1-format-2` | `c25688073818ffaca40889c4d68386eb312426d8` | 0 | COMPLETE；VM gofmt 无变化，精确取回、重新暂存后树仍完全相同，复用同树测试证据 |

失败原因已按排错技能核对 `repository.CheckUser`、`model.ValidIdent` 和原 `users_test.go`。用户名只允许小写字母、数字及下划线，且字母开头。此次失败属于测试准备错误，不是业务兼容缺陷。原失败日志保留，不改生产规避失败。

指定命令：

```sh
go test ./internal/interfaces/console -run 'Test主机|TestCSV|TestOpenAPIOptionalHostArchitectureAndFirmware' -count=1
go test ./internal/infrastructure/persistence/pgstore -run 'Test契约主机|Test契约审计|Test契约并发' -count=1
```

证据位于本目录 `vm-logs/t1-format-1.*`、`vm-logs/t1-baseline-1.*`、`vm-logs/t1-baseline-2.*`、`vm-logs/t1-format-2.*`。每次保留 `.tree/.started/.finished/.exit/.log/.changes/.untracked`；最终以 COMPLETE 与真实退出码为准。

## 自审与边界

- 期望值为手写路由和固定样例，没有从被测路由生成期望。
- 网页权限通过真实当前账号读取；只复用会话夹具，旧真实登录测试未修改。
- 数据库失败使用真实关闭的独立仓储，鉴权仍用正常用户仓储；没有将读取失败伪装成不存在。
- 旧 CSV 物理行号、重复位置、OpenAPI 与仓储事务、审计、并发测试保留并在指定命令中运行。
- 全量 make check 按阶段计划留给 Task 4；本任务通过不能宣称全阶段完成。
- 最终 VM 格式复核无变化，重新暂存和提交后源码树仍为已通过的同一树。`git diff --cached --check` 与提交差异检查通过，提交后工作区干净。
- 未发现主机兼容缺陷。已知本次测试准备错误已修并留证；没有未解决失败或跳过的数据库验收。没有运行全量 make check，未合并、未推送。
- 独立审查由根代理接续；本报告为执行者自审记录。
