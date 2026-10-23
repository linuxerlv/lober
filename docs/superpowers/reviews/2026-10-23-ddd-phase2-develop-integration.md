# 四层迁移第二阶段：develop本地集成记录

状态：第二阶段已完成本地develop集成验收。合并树的lober虚拟机完整检查和独立合并核对均通过；不代表后续发布环境验收。

## 分支核对

用户授权先核对dev与develop，再把第二阶段合入develop；合并后测试只在lober虚拟机执行。

| 分支 | 合并前提交 | 核对 |
| --- | --- | --- |
| dev | a06d0db6745b438692f29f34cb6f0588ff19f6e6 | 已被develop完整包含；无独有提交 |
| develop | bdc6371749a9122680a18383561567e89a05e9c2 | 比dev多12提交，保留第一阶段及dev原有功能 |
| codex/ddd-authoring | 8eeb4d72e5d140648db852aae43ac0faed94a6ee | 包含develop全部提交，新增10提交 |

远端`ls-remote --heads origin dev develop`因缺少登录凭据失败，不能把本地缓存当成最新远端。本次只进行已授权的本地集成。

原主目录有用户文档修改，原dev工作区有未提交网页产物。两者均未修改；没有暂存、重置或清理这些改动。

## 合并结果

在干净的第二阶段工作区切到develop，执行`git merge --no-ff --no-commit codex/ddd-authoring`，无冲突。暂存树为`f246ce6ced19bd7acfe498b034d32d0f4fce6d56`，逐字节等于第二阶段分支的完整树。

第二阶段的32条入口、83项临时许可删除及数据库保护已在[模块整分支审查](2026-10-23-ddd-phase2-final-review.md)通过。本次没有新的源码冲突处理或额外业务改动。

最终合并提交的两个父节点为上述develop和模块分支提交。以第一父节点为界可识别整个模块的合入范围，保留第一阶段保护。

## 验证环境与命令

验证目录为`/home/dev/lober-ddd-phase2-integration-20261023`，与此前模块测试目录分开。通过Git归档传输精确暂存树，在虚拟机重建索引并核对同一树值；测试连接不入索引，权限为600。

主机名实际为lober，Go1.27.1、Node24.16.0、pnpm11.16.0。开跑前35G磁盘可用，无其他Go测试进程。通过只读查询确认实际数据库为lober_test；没有修改产品数据库、服务、配置、安装制品或网络。

固定`GOMAXPROCS=2`、`GOFLAGS=-p=2`、`GOPROXY=off`、Bash与BusyBox原路径。沿用原测试强度、断言和`-race -timeout=20m`。不安装网页依赖。

实际顺序：

```sh
make check
pnpm --dir web check:console-types
go test -v ./internal/infrastructure/builtin ./internal/infrastructure/media ./internal/infrastructure/media/iso9660 -run '^(TestRecordCurrentRelease|TestPreseedMirrorSelectionsAreSeenAndAuthenticated|TestIdentifyRealISO|TestRealISO)$'
git diff --exit-code
```

被测树为`f246ce6ced19bd7acfe498b034d32d0f4fce6d56`。实际开始2026-10-22T23:56:25Z，结束2026-10-23T00:21:10Z，最终退出0。工具后源码无修改，也没有额外未跟踪文件。

| 检查 | 结果 |
| --- | --- |
| 根目录make check | 格式、静态检查、强制全模块架构、普通与并发全包、Linux amd64构建、Wire差异全部通过 |
| 普通网页接口 / 数据库 | 183.741s / 109.174s |
| 并发网页接口 / 数据库 | 844.374s / 155.267s |
| 完整架构普通 / 并发 | 141.142s / 198.125s |
| 网页公开类型生成检查 | 通过，无生成漂移 |
| Debian镜像选择补充用例 | official和internal-signed均通过 |
| 一次性发布记录、真实ISO识别与读取 | 按未开启的可选条件跳过3项；不记通过 |

没有前端源码变更，本轮不重复全量前端类型、构建或浏览器验收。真实ISO和整机安装没有执行。

[独立合并核对](2026-10-23-ddd-phase2-develop-merge-review.md)确认分支祖先、预期双父输入、无冲突、索引与模块分支完全相同、批准源码之后只有文档改动。新增严重/重要/轻微问题均0；沿用模块验收时已有的1项日志噪声。

最终提交前再次核对原始退出码、树、工具改动和精确文档差异。提交后核对实际双父对象及dev保持不变。

## 准备中的问题

首次私有连接预检将整段连接地址放入psql的PGDATABASE环境变量，导致库名校验失败；当时尚未启动Go检查。预检改为分别传递私有PG环境字段，实际查询确认lober_test。Go测试继续使用原始测试连接，数据库和配置未改。

准备脚本统一以UTF-8/LF字节传输，避免Windows文本输入换行影响远程shell。准备错误保留原始记录，不当成测试红灯或通过。

## 交付边界

本次交付只到本地develop。test企业套件、preview人工验收、main发布和远程推送均不在本次执行范围。真实ISO与整机安装没有新增验收结论。

模块分支与工作区保留，原dev保持不变。完整检查通过后，仅补本记录、独立核对报告和文档状态；源码、测试、构建、生成结果与被测树相同。
