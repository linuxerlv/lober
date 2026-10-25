# 第三阶段合入 dev 的记录

用户已明确集成目标是 `dev`。本轮已合入本地 `dev`，合并后完整检查在 lober VM 通过，独立合并审查未发现问题。原模块分支、历史 `develop` 和用户原有网页文件均保留。

## 分支和实际合并

| 项目 | 提交或结果 |
| --- | --- |
| 合并前 dev | `a06d0db6745b438692f29f34cb6f0588ff19f6e6` |
| 历史 develop | `a9d5434fc507f8857c5fb36b252a41de1cce0ab3`，本轮不修改 |
| 已验收主机模块分支 | `codex/ddd-hosts`，`d146fb5d99f29d3993ee186837a5a15d0818cdc4` |
| dev 实际合并 | `f6a20d0f959d87f687d2045622ae2c6186dcaafc` |
| 合并第一、第二父提交 | 上述原 dev、主机模块提交，顺序一致 |
| 合并树 | `34a9d39aca131598c11574d629147416ffdca5cb` |

合并前，`dev...develop` 为 `0/23`，`dev...codex/ddd-hosts` 为 `0/29`；原 dev 是主机模块提交的祖先，没有 dev 独有提交被覆盖。主机分支已经包含第一、二阶段依赖，因此此次把前三阶段一起带入正确的集成分支。

实际使用非快进合并，提交说明为 `refactor(backend): integrate DDD migration into dev`。合并无代码冲突；合并树与已审查主机模块分支树完全相同。此前的逐项及整分支审查见[第三阶段证据](2026-10-23-ddd-phase3-evidence.md)。

## 保留原有未提交网页文件

合并前 dev 存在两项未暂存改动：旧路径 `internal/console/ui/dist/index.html` 修改，以及同目录 `_placeholder-keeps-embed-underscore-mode.js` 删除。该目录还有被忽略的网页构建文件。

合并前完整复制 214 个文件，共 4,243,554 字节，保存逐文件 SHA256 和大小清单，并仅暂存这两项已跟踪改动到具名 stash。合并后，在迁移后的 `internal/interfaces/console/ui/dist/` 恢复原文件；全部 214 个文件的内容和大小与原清单一致，两项改动仍未暂存。

恢复 stash 时，占位文件因路径迁移产生删除冲突，已严格按原先的删除状态恢复；这不是代码合并冲突。原、新路径的 index.html 和占位文件在提交中的内容本来就相同，没有用重新生成的网页文件替代用户文件。

原文件副本、清单和 stash 均保留在本机，未提交。stash 为 `f4b5c87ac92eb529206ce8102a37e33a6b6befee`，说明为 `codex-ddd-dev-ui-20261023-919750aaeb`。本轮不清理这些保留物，也不提交网页构建文件。

## 合并后验证

验证仅在 lober VM 进行，使用精确合并提交的归档，不含用户未提交的网页文件。运行标签为 `dev-integration-1`，重建 Git 索引后核对完整树等于 `34a9d39aca131598c11574d629147416ffdca5cb`。沿用第三阶段独立测试目录及依赖缓存，不覆盖运行服务、前两阶段记录或既有第三阶段日志。

准备检查确认没有其他 Go 任务；实际测试库为 `lober_test`，未在恢复。Go 1.27.1、Node 24.16.0、pnpm 11.16.0。数据库启动时间为 `2026-10-23T06:23:06.383579Z`；可用磁盘约 37 GiB。

环境保持 `GOMAXPROCS=2`、`GOFLAGS=-p=2`、`GOPROXY=off`，测试 shell 与 busybox 沿用原路径。数据库连接从已有私有文件读取，不记录连接内容。

按原顺序执行：

```sh
make check
pnpm --dir web check:console-types
go test ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$' -v -count=1
git diff --exit-code
```

`make check` 保持原格式检查、静态检查、普通测试、`go test -race -timeout=20m ./...`、Linux amd64 构建和 Wire 差异检查。全模块依赖发现继续强制 `-count=1`。不改密码强度、测试准备缓存、时限、断言或失败包；允许复用相同源码与环境的有效 Go 测试缓存。

实际运行结果：

| 检查 | 结果 |
| --- | --- |
| 运行树 | `34a9d39aca131598c11574d629147416ffdca5cb`，与实际 dev 合并树一致 |
| 时间 | UTC `2026-10-23T14:46:17Z` 至 `14:52:27Z`，共 370 秒；北京时间 22:46:17 至 22:52:27 |
| 总退出码 | `0` |
| 格式、静态检查、强制全模块发现 | 通过；强制发现实际 8.292 秒 |
| 普通测试 | 29 个有测试的包结果成功，其中 24 条为有效缓存；架构包实际 154.066 秒 |
| 并发测试 | 29 个有测试的包结果成功，其中 24 条为有效缓存；架构包实际 184.698 秒 |
| 数据库、网页及启动组装包 | 普通和并发结果均成功，本轮使用缓存，未宣称实际重新运行这些包 |
| Linux amd64 构建、Wire 差异检查 | 通过 |
| 网页类型生成检查 | `node scripts/generate-console-types.mjs --check` 通过 |
| Debian 预置配置验证 | `TestPreseedMirrorSelectionsAreSeenAndAuthenticated` 实际通过，1.56 秒 |
| 可选跳过 | `TestRecordCurrentRelease` 为一次性发布记录，未启用；`TestIdentifyRealISO`、`TestRealISO` 未设置 `LOBER_TEST_REAL_ISO`，共 3 项跳过 |
| 最终文件核对 | `git diff --exit-code` 通过，修改与未跟踪文件列表均为空 |

原日志及 `tree/exit/started/finished/changes/untracked` 已保存在虚拟机的第三阶段日志目录，标签为 `dev-integration-1`；本机也保留本次证据副本。公开文档不收录连接文件或敏感原日志。

此次没有代码或测试设置修复。数据库和网页包复用此前同源码、同环境的成功缓存；原始数据库真实执行、失败恢复及完整重跑的证据仍见[第三阶段模块验收记录](2026-10-23-ddd-phase3-evidence.md)。本次没有用旧结果替代原完整命令，原 `make check` 已从头至尾成功退出。

## 独立审查和交付范围

本轮[独立合并审查](2026-10-23-ddd-phase3-dev-merge-review.md)核对了分支历史、合并树、用户文件保护及验证范围，严重、重要和次要问题均无发现。不重复前三阶段已经完成的实现审查。

首轮独立审查时虚拟机尚在运行，根执行者随后核实本次退出码、完整阶段结果、精确文件树、缓存及跳过项，完成上表。独立代理再核对安全结果文件与最终文档，确认两个待项可关闭。审查中未作判定的五项已逐一记录处理方式，未把它们默认为通过。

合并后的收尾只更新 `docs/` 下 Markdown。提交前以显式文件清单核对文档差异，不纳入用户网页文件；生产代码、测试、生成文件、依赖与 Makefile 均保持被测合并提交内容。仅补记结果不重复运行相同套件。

本轮没有推送，也没有推进 `test`、`preview` 或 `main`。没有执行企业套件、完整前端构建、浏览器或整机安装验收，不把本轮自动检查替代这些验收。
