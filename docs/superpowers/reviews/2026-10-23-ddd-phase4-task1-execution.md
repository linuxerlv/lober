# 第四阶段任务一执行记录

本次只固定镜像与后台作业的现有行为，补兼容测试。不改产品流程、权限、表结构、配置或生成代码。

## 改动

- 新增 `docs/contracts/media.md`，按八类行为说明身份、输入、结果、错误顺序、提交范围、重复调用、并发、审计和取消，列明十一入口。
- 新增 `console/media_contract_test.go`，检查十一入口、权限与审计描述、删除和补源的错误顺序、上传范围/键/存在性/种类/边界顺序，以及真实网络安全拒绝晚于全部输入预检。
- `console/isos_test.go` 用真实来源实现和真实 PostgreSQL 检查：第二来源失败返回422并带 Location；镜像与主上传来源保留；失败来源不存在；只留下两条已提交审计。
- 加强组装、排队失败回滚、删除清理失败后的审计、后台异常资源释放和退避、拒绝分片时版本与区间不变等断言。新增真实数据库排队事务与旧版本重放测试。

核对发现计划初稿把 operator 列为可写角色。真实权限和原五角色测试均只有 admin 可写。根执行者已用 `a5b02a8` 更正文档，本项不改权限。

## 虚拟机验证

全部 Go、格式处理和测试只在 lober 虚拟机执行。本机只读写文件、安全传输和 Git。未修改口令强度、超时、缓存准备或测试断言来换通过。

首轮候选树 `0430d5f408594d0d818444556cfc3c9c0e4a38a7`；开始于 `2026-10-23T15:35:20Z`，结束于 `15:36:12Z`，退出0。先对本项8个测试文件运行 gofmt，再执行：

```sh
go test ./internal/interfaces/console ./cmd/lober ./internal/infrastructure/media ./internal/infrastructure/persistence/pgstore -run 'Test(ISO|镜像|Source|ISOSources|契约ISO|契约审计|删除快照|补来源|重新导入|后台|Import|Upload|Remote|Worker|Recover)' -count=1
go test ./cmd/lober -run '^Test装配后台获取导入及启动恢复$' -count=1
```

四包分别通过：14.933秒、5.130秒、31.283秒、14.038秒。补充组装用例通过，1.152秒。原筛选不会选中该组装用例，因此单独执行。

首轮格式只改变 `cmd/lober/supervision_test.go` 和 `internal/interfaces/console/media_contract_test.go`。按明确白名单取回后，用新候选树 `8974c068d76469f19117211d77eaa39a439a91e5` 验证：

```sh
go test ./internal/interfaces/console -run '^Test镜像(十一入口描述及审计契约|多错仍按原先顺序返回|非法输入先于网络安全拒绝|创建后来源失败保留位置及已提交事实)$' -count=1
go test ./cmd/lober -run '^Test后台' -count=1
```

最终轮开始于 `2026-10-23T15:37:14Z`，结束于 `15:37:23Z`，退出0。两包分别通过，2.968秒和1.877秒。虚拟机 changes/untracked 证据均空。本项8个 Go 文件逐个内容编号与最终测试树一致。此后只补契约的审计字段说明和执行记录。

所有测试命令带 `-count=1`，没有使用测试结果缓存；没有清空构建缓存。安全日志没有 `(cached)` 或 skip 行，但命令未开详细输出，因此不声称已统计逐例跳过数量。真实数据库夹具缺少连接时会失败，不会跳过。

本项没有执行全仓 `make check`、全仓竞态检查、构建或 Wire 生成；按计划由末项统一验收。没有人为制造红灯，也没有发现需要单列修复的旧缺陷。

## 自查和后续

已对照源代码、路由描述、仓储接口及数据库实现检查契约。保留网页已确认 ranges 与旧能力 Missing 的不同含义，以及多来源非原子、提交结果未知时不删文件、后台唤醒只提示扫描的边界。`git diff --check` 通过。

独立审查由根执行者在本项提交后安排；本项没有派出子代理。Task2—Task6 的应用迁移、并发屏障、删除双向竞争和完整生命周期补测尚未执行。
