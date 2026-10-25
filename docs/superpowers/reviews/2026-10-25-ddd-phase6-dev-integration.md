# 第六阶段 dev 集成与合并后验收

第六阶段已按用户本轮授权在本地合入 `dev`。合并后的原完整检查已在 lober VM 通过。独立集成审查通过，严重、重要、轻微问题均为0。原有两处未提交网页改动已保留，尚未推送或推进后续环境。

## 分支核对与合并

- 合并前 `dev` 为 `7c0edfdb67282ccfd25e9d2f31448cb0bb0faa2b`；来源 `codex/ddd-task-read-finalize` 为 `c8175f2dd69c884d8ae45515829d0d1237cf437b`，来源工作区干净。
- 共同起点就是原 dev，左右独有提交为0/15。来源八项任务、逐项审查、整分支代码审查和文档关闭检查已完成，见[模块交付记录](2026-10-24-ddd-phase6-final-review.md)。
- 读取远端dev失败，退出128。只确认本地分支关系，远端是否更新仍未知。
- 使用保留模块边界的合并，提交为 `7301128eabd2636d4e0e676730bdacafba08ab99`；两个父提交依次为原dev和来源，无冲突。
- 合并树 `27837a5530f34fcf1e6b21cb15c40daff204655e` 与来源完整内容树相同，没有额外修改业务、测试、配置或生成文件。
- dev原有 `index.html` 修改和占位文件删除均已备份、保留；两条路径不在模块改动清单中。未暂存或带入合并，未使用stash或reset。HTML摘要仍为 `02b7a719fdcb7299cda7a7005506c9a404849103c7885731e4d7d5eff6afc5f0`。

## 合并后的 lober VM 验收

被测对象是实际合并提交 `7301128eabd2636d4e0e676730bdacafba08ab99`。新目录 `/home/dev/lober-ddd-phase6-dev-integration-20261025/source` 从已有Git对象克隆并导入合并对象，实际分离检出该提交，保留两个真实父提交。旧阶段目录和证据未覆盖。用户未提交的网页改动未传入，被测网页来自提交对象。

本轮运行前实测主机lober、用户dev，4CPU、内存2111MiB、当时可用1481MiB、可用盘约20G。Go1.27.1、Node24.16.0、pnpm11.16.0。数据库按实际解析的忽略文件连接 `lober_test`，非恢复状态，角色 `lober_test`，PostgreSQL16.15；文件权限0600，未公开连接内容。

沿用 `GOMAXPROCS=2 GOFLAGS=-p=2 GOPROXY=off`、原Bash与BusyBox路径及 `WIRE_LOADER_MODE=fallback WIRE_LOADER_ARTIFACTS=0`。未改代码、测试设置、密码强度、会话缓存、超时或断言。开始前按原命令清测试结果缓存。

依次执行以下命令，退出均为0。前四条与模块验收原命令在去掉首尾空白后完全相同。

```sh
go clean -testcache
make check
pnpm --dir web check:console-types
go test -count=1 -v ./internal/application/ports ./internal/application/tasks ./internal/application/authoring ./internal/application/identity ./internal/application/execution ./internal/archtest ./internal/infrastructure/notification ./internal/infrastructure/machinekeys ./internal/infrastructure/events ./internal/infrastructure/persistence/pgstore ./internal/interfaces/console ./internal/interfaces/machine ./cmd/lober
git diff --check
git diff --exit-code
git diff --cached --exit-code
```

`make check` 于UTC `2026-10-24T16:23:32Z` 至 `2026-10-24T16:56:39Z` 执行，耗时1987秒；北京时间为2026年10月25日00:23:32至00:56:39。格式、静态检查、强制全模块架构检查、普通测试、竞态检查、Linux amd64构建和Wire漂移均通过。竞态保持原 `go test -race -timeout=20m ./...`，控制台包实际872.731秒。

| 检查 | 实际通过包 | 缓存包 | 无测试文件包 |
| --- | ---: | ---: | ---: |
| 普通测试 | 37 | 0 | 8 |
| 竞态检查 | 37 | 0 | 8 |
| 13包详细测试 | 13 | 0 | 0 |

详细测试于UTC16:56:56至17:03:27执行，487个顶层测试、2313个子测试通过；34个计划点名测试均有实际PASS，0明示跳过、0外层失败。两条缩进的失败输出来自架构守卫的故意失败探针，所属外层测试通过。普通和竞态输出不详细，隐藏跳过总数未知，不能写成零跳过。

## 测试对象与文件一致性

VM运行前后，HEAD均为合并提交，HEAD树和暂存树均为 `27837a5530f34fcf1e6b21cb15c40daff204655e`。全部2116个跟踪文件的提交字节、物理文件、运行前后内容与来源清单完全一致。45个项目包中的553个实际Go、测试和嵌入输入一致，没有被忽略的活动源码。

清单摘要为 `b6a4636fd127e82284a4a034e334fbfa619eab0d516e5c317c6c528ea898e13c`。生成类型摘要为 `08dc80a1e7c472e907d8f710845ce09f5999c0ec792f2aa78776bd407405941c`，前后不变。跟踪文件、暂存区和非忽略新增均无差异。该结论不扩大成所有被忽略缓存文件都没变化。

## 独立审查与范围

独立集成审查通过，严重、重要、轻微问题均为0。审查聚焦合并父关系、来源与合并内容相等、同一提交的新VM验收、用户文件保留和结论边界。来源业务已完成逐项和整分支审查，本次合并没有额外内容差异。

远端新鲜度未知；审查者未另连VM重测；普通和竞态隐藏跳过数未知。真实ISO开关原本未启用，不声称实物介质、实机安装、完整前端构建、企业套件、浏览器人工验收或test/preview/main通过。用户未提交网页效果不在受测对象中；已有Gin调试日志噪声仍为原记录的后续整理项。

文档收尾只修改本集成记录、总路线及第六阶段计划状态。收尾后另核对 dev 相对被测合并提交只含这三份Markdown，代码和测试设置不变；不重复已通过的完整套件。来源分支、全部工作区、原日志和用户文件均保留。没有推送或后续环境晋级。

## 证据位置

本地忽略证据位于 `E:/src/lober/.worktree/ddd-task-read-finalize/.superpowers/sdd/2026-10-24-backend-ddd-phase6-task-read-finalize/2026-10-25-dev-integration`。`preflight.json`、`merge.json`、`preflight-safe.json`、前后VM清单、`results.json`、`verification.json`、各命令与退出码、安全日志、独立审查报告可核对本轮事实。私有连接与原日志不提交。

## 根端对审查边界的处理

1. 接受远端新鲜度未知、审查者未现场重测及未读私人原日志的限制；不扩大为远端同步或二次重跑。根端保留原始输出与摘要，审查者独立核对安全记录、命令和文件清单。
2. 接受普通与竞态隐藏跳过数未知；仅13包详细测试的明确跳过为0。真实ISO及其他未验范围仍按上文记录。
3. 来源业务按已有逐项和整分支审查结论；本次整树一致，不重复业务细审。历史整分支原0/0/1与文档关闭过程保留，不改写历史结果；已有日志噪声仍为后续事项。
4. 只证明全部跟踪文件和实际活动输入一致，不声称所有忽略缓存未变。用户未提交网页效果不在受测提交中。
5. 文档提交后的三文件范围、553输入及用户文件保护，由根端另做精确Git核对，记录在私人 `final-proof.json`，不预写为审查者已核实。

## 独立集成审查原记录

以下为文档收尾前的独立报告。证据文件行号均指上面的本地忽略目录，不能当作公开网页链接。源码被测提交仍为7301128，后续只提交文档。

# 第六阶段本地 dev 集成独立审查

结论：本轮 **Critical 0、Important 0、Minor 0（C/I/M = 0/0/0）**。可以完成本地 dev 集成收尾。此结论只覆盖现有合并 `7301128eabd2636d4e0e676730bdacafba08ab99` 及其合并后测试证据，不代表推送、远端同步或后续环境验收通过。

审查日期：2026-10-25。审查者独立按 requesting-code-review 的要求核对既有结果。没有派子代理，没有修改源码或 Git 分支、提交、暂存内容；唯一写入是本报告。没有运行本机 Go、Node、构建或测试，没有连接 VM，也没有现场重测。

## 发现

| 等级 | 数量 | 证据、影响与修复 |
| --- | ---: | --- |
| Critical：会造成严重错误的问题 | 0 | 无发现，无需修复。 |
| Important：完成前必须处理的问题 | 0 | 无发现，无需修复。 |
| Minor：不阻止完成的小问题 | 0 | 无新增发现，无需修复。 |

## 独立核对结果

| 核对项 | 结果和证据位置 |
| --- | --- |
| 本地分支和父关系 | 直接在 dev 工作区读取 Git：当前分支是 `dev`，HEAD 为 `7301128eabd2636d4e0e676730bdacafba08ab99`。两个父提交依次为原 dev `7c0edfdb67282ccfd25e9d2f31448cb0bb0faa2b` 和来源 `c8175f2dd69c884d8ae45515829d0d1237cf437b`。与 `merge.json:2`、`review-source-merge.diff:4` 一致。 |
| 来源完整合入 | 直接读取共同起点和左右提交数，得到共同起点为原 dev，左右为 `0 / 15`。来源工作区 HEAD 为 `c8175f2` 且状态为空。来源与合并完整内容差异为空，两者树均为 `27837a5530f34fcf1e6b21cb15c40daff204655e`。与 `preflight.json:2`、`merge.json:7` 一致。没有额外源码、测试或配置变动。 |
| 用户网页文件保留 | 直接读取 dev 状态：只有 `index.html` 修改与 `_placeholder-keeps-embed-underscore-mode.js` 删除两项，暂存区为空。分别计算现有 HTML 和 `user-files/index.html` 备份的 SHA256，均为 `02b7a719fdcb7299cda7a7005506c9a404849103c7885731e4d7d5eff6afc5f0`。两路径不在来源改动列表。与 `preflight.json:99`、`merge.json:10`、`assemble-evidence.py:8` 一致。 |
| 新 VM 检出合并对象 | `vm.py:49` 检查主机、用户、新目录和旧目录归属；`vm.py:65` 克隆后导入 bundle，再分离检出合并对象；`vm.py:70` 核对 HEAD、树、暂存树、真实父关系和干净状态。`vm-create.exit`、`vm-checkout.exit` 均为 0。前后清单实测 HEAD 都为本次合并，树与暂存树也一致，见 `vm-before-manifest.json:14986`、`vm-after-manifest.json:14986`。这支持确实检出了新合并对象，而非仅替换标识。 |
| 全文件同源 | 直接读取本地合并树的 2116 个条目，逐个核对路径、模式及 Git 内容对象，全部与 reference 清单一致。独立逐字段比较 reference、VM 前后清单中每项的模式、内容对象、SHA256、字节数，并核对前后物理文件摘要，差异为 0。前后均 2116 文件、无物理文件差异，见 `reference-manifest.json:2`、`vm-before-manifest.json:14990`、`vm-after-manifest.json:25575`。 |
| 实际输入未改变 | 前后均为 45 个项目包、553 个实际 Go、测试和嵌入输入；独立比较全部输入摘要，无差异；被忽略的活动源码列表均空。见两份 VM 清单第 13–16、2233 行，以及 `manifest-vm.py:31`、`manifest-vm.py:36`、`manifest-vm.py:46`。全清单摘要均为 `b6a4636fd127e82284a4a034e334fbfa619eab0d516e5c317c6c528ea898e13c`。生成类型摘要均为 `08dc80a1e7c472e907d8f710845ce09f5999c0ec792f2aa78776bd407405941c`。 |
| 原命令和环境保持 | 独立比较本轮四条命令及前一轮 `task8-acceptance-*.command.json`：仅去掉首尾空白后完全相同，固定环境也相同。本轮 `.commands` 内容与对应 `.command.json` 完全相同。见 `clean-testcache.command.json:2`、`make-check.command.json:2`、`console-types.command.json:2`、`focused13.command.json:2`。当前 `vm.py` SHA256 与命令记录中的 helperSHA256 一致。未发现改断言、改缓存策略、提高竞态时限或加入工作区覆盖的证据。 |
| 原 make 检查完成 | 从当前安全日志独立核对格式、静态检查、强制全模块发现、普通测试、竞态、Linux amd64 构建及 Wire 漂移命令，最终 EXIT=0。竞态命令仍为 `go test -race -timeout=20m ./...`。见 `make-check.safe.log:3`、`make-check.safe.log:53`、`make-check.safe.log:99`。直接读取合并提交的 Makefile，命令和检查链吻合。 |
| 普通与竞态数量 | 独立按命令边界统计安全日志：普通测试 37 包通过、8 包无测试文件；竞态同样 37/8。外层失败包与缓存包均为 0。见 `make-check.safe.log:7`、`make-check.safe.log:53`、`results.json:2`。控制台竞态实际耗时 872.731 秒，见安全日志第 97 行。 |
| 13 包详细测试 | 独立统计安全日志：13 包通过、487 顶层 PASS、2313 子项 PASS；顶层 FAIL、外层失败包、明确 SKIP、缓存包均为 0。用原 `planned-named-tests.json` 的 34 个名称逐个匹配本轮顶层 PASS，无缺失。见 `focused13.safe.log:1`、`focused13.safe.log:5630`、`results.json:14`。 |
| 两条缩进 FAIL | `focused13.safe.log:1740` 两条缩进 FAIL 属于缓存守卫故意构造违规包后的子进程结果，外层 `TestIncrementalGateRejectsPackageInExistingRoot` 在第 1742 行 PASS。只读检查 `internal/archtest/cache_test.go:66–77`，它要求普通和竞态门检查拒绝该违规包。没有把它们误算为外层失败，也没有声称日志不存在 FAIL 字样。 |
| 时间与退出状态 | 五组 `.exit` 均为 0。make 从 UTC 2026-10-24 16:23:32 至 16:56:39，共 1987 秒；详细测试从 16:56:56 至 17:03:27。源清单取于 16:23:10 和 17:03:38，覆盖测试前后。见 `verification.json:18`、`vm-before-manifest.json:14988`、`vm-after-manifest.json:14988`。类型及最终差异检查也均退出 0。 |
| 数据库为实际探测 | `preflight-vm.py:27` 使用数据库原生库实际连接，并查询数据库名、是否处于恢复模式、角色和版本。安全结果是 `lober_test`、非恢复、角色 `lober_test`、PostgreSQL 16.15；连接文件被忽略且权限为 0600，见 `preflight-safe.json:3`。脚本只输出这些身份结果，没有输出连接内容。审查者没有读取连接文件。 |
| 安全证据可交叉核对 | 对五份 `.safe.log` 重新计算 SHA256，全部与当前 `verification.json:18` 对应 safeSHA256 相同；命令文件、退出码、日期、HEAD 和包结果也相互一致。阅读了 `verify.py:9` 的清单比较及 `verify.py:28` 的结果解析逻辑，另用独立 PowerShell 比较与计数核对，没有执行该脚本。 |

## 审查范围和无法核实项目

1. 本次先读 `integration-report.md`，再独立读取本地 Git、用户 HTML 与备份、命令记录、退出码、前后清单、`verification.json`、`verify.py`、当前目录的执行和取证脚本，以及五份安全日志。安全日志是按 `assemble-evidence.py:20` 白名单保留的输出，不含完整原始输出。
2. 没有读取任何原始 `*.log`（明确允许的 `*.safe.log` 除外）、`remote-check.private.log`、`docs/linux-test-environment.private.md`、任何 DSN 或上级 `vm.py` 的连接常量。没有比较原始日志字节与安全日志删选结果；原日志摘要只视为现有记录，不冒称自己独立重算过。
3. 没有现场重现 VM 执行或数据库连接。关于 VM 的判断来自已完成执行的命令、退出状态、安全日志、实测清单和取证脚本交叉一致；不等同于审查者亲自重跑。
4. 远端读取退出 128，最新远端状态未知，见 `preflight.json:107`。只确认本地 dev 和来源关系，不称为已与远端同步。
5. make 输出并非详细模式，因此全部隐藏跳过数未知。13 包详细输出明确跳过数为 0。真实 ISO 原开关未启用，见 `preflight-safe.json:27`、`results.json:128`。

## 暂不评判项目

1. 来源业务已有 `docs/superpowers/reviews/2026-10-24-ddd-phase6-evidence.md` 和 `2026-10-24-ddd-phase6-final-review.md`。只核对其已有结论及范围，没有重复 86 文件业务细审。本轮合并树与来源树相同，没有新增合并内容需要另行业务审查。历史整分支原 C/I/M=0/0/1 及后续文档关闭记录仍按原文保留，不改写成历史全零。
2. 用户未提交网页效果、完整前端构建、真实 ISO 和介质、实机安装、企业测试、浏览器人工验收、test/preview/main 晋级均不在通过结论。已有 Gin 调试噪声也未在本轮重新评判。
3. 只确认跟踪文件及实际活动输入不变，不声称所有被忽略的缓存文件都未变化。
4. 审查时三份 Markdown 收尾尚未提交，不算入受测源码。本地集成可收尾；收尾后仍应只读核对合并到文档提交只含获准的三份 Markdown，全部代码输入不变，原两处用户网页改动保留且暂存区为空。该后续核对不预先算通过，也不要求重跑本轮完整测试。

除上述明确边界外，没有其他无法核实或暂不评判事项。没有阻止本地 dev 集成完成的问题。

补充：已只读核对后补的 `public-integration-draft.md`。其中合并对象、命令、数量、时间、同源证明和范围与本轮证据一致；审查段保留“待补”，没有提前宣称本次独立审查通过。没有修改该草稿。最后复核 dev 仍停在合并 `7301128`，仅原两处用户网页改动。
