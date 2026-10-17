# 2026-10-17：dev 集成复验与 test 企业测试

结论：指定 VM 上的 dev 集成门禁通过，随后已将该 dev 合入 test。当前控制面冒烟通过；企业发布资格未通过。依赖扫描存在发现，真实介质、机器面、真实装机、容量持续运行和受支持升级仍未取得资格，不能据此合入 main 或宣布可发布。

## 候选与环境

- 产品基线：`f68432abd0a7960b6b96c4a98ef42901ea44fb48`（dev）。
- 合入 test：`87b4aaffb4e2f386f9dcc3b24ca661bdc238cc97`，双亲为旧 test `1c8185f` 和上述 dev；保留原测试历史。
- 最终测试源码：`eb22f6e6d326971289faa6cce4abfdc4ec0f197e`。后续报告提交不改变此次测试源码。
- 最终运行：`vm-20261017-603abacb65350d84`。
- 二进制 SHA256：`8a058d13e62a9fdf9d9939aed23905325a55699119f271fafdb40c0b3f4086e7`。
- 清单 SHA256：`8ca67b6f9af784676932b4027e3401172a2567a27bfbdfcf06e5fc464afc650b`，包含 1829 个冻结文件的摘要、源码归档摘要、二进制摘要、产品与测试 SHA。
- 全部资格执行及被测服务均在用户指定 VM `192.168.137.21`。Ubuntu/Linux 6.8、Go 1.27.2、Node 24.22.0、Python 3.12.3、PostgreSQL 16 系列、4 vCPU；具体版本见运行日志。
- VM 工作目录：`/home/dev/lober-enterprise-20261017-f68432a`。连接串从受限文件加载，不写入仓库。测试使用独立随机 schema、随机账号口令、回环监听地址和专用进程；没有重启既有 QA 服务。

实际二进制由 test `6405b58cca314bd6577f2755e52fa46bac231da1` 构建，嵌入 VM 上锁文件构建的真实前端。之后提交仅修改测试工具、文档和门禁配置，产品目录与 dev 一致。已用 `git diff --quiet dev test -- cmd internal web go.mod go.sum Makefile` 核对。Go 后续检查继续使用冻结的 test 产品源码；前端门禁复用同产品树的 dev VM 结果，清单中明确记录来源。

## 执行结果与证据

下表证据位于 [本轮证据目录](enterprise/20261017/final-logs/)。JSON 将结果绑定到运行 ID、清单摘要及实际日志摘要；门禁会重新验证冻结源码、归档和二进制，拒绝零用例、跳过、缺失、旧结果及被替换的日志。

| 检查 | 结果 | 范围与证据 |
| --- | --- | --- |
| 合并前 dev：`make check` | PASS | 格式、vet、全量普通测试、race、Linux 构建、Wire；[日志](enterprise/20261017/final-logs/dev-backend.log) |
| 合并前 dev：前端门禁 | PASS | 锁文件安装、workspace stub、OpenAPI 类型、Vue 类型、ESLint、25 文件/88 Vitest 用例、11/11 构建任务；[日志](enterprise/20261017/final-logs/frontend.log) |
| test：`make check` 与 `go test -count=1 ./...` | PASS | 18 个含测试的 Go 包；[结果](enterprise/20261017/final-logs/backend.json) |
| 当前控制面 HTTP/浏览器/恢复 | PASS | 88 项、0 失败、0 跳过，含下述 4 条浏览器场景；[结果](enterprise/20261017/final-logs/control.json) |
| 门禁回归 | PASS | 25 项；[结果](enterprise/20261017/final-logs/gate.json) |
| ISO9660 fuzz | PASS | 10 分钟，3,899,719 次输入；[结果](enterprise/20261017/final-logs/fuzz_iso.json) |
| WIM fuzz | PASS | 10 分钟，4,334,970 次输入；[结果](enterprise/20261017/final-logs/fuzz_wim.json) |
| Go 依赖漏洞扫描 | FAIL | govulncheck v1.9.0，7 条符号/调用路径发现，涉及 3 个模块；[原始日志](enterprise/20261017/final-logs/security_scan.log) |
| 前端 workspace 生产依赖审计 | FAIL | 48 条：严重 2、高危 25、中危 18、低危 3；详见[结果](enterprise/20261017/final-logs/frontend_audit.json)及[原始日志](enterprise/20261017/final-logs/frontend_audit.log)；不等同于浏览器运行时可利用性结论 |
| `control-smoke` 汇总 | PASS | 仅汇总 backend/frontend/gate/control；[门禁结果](enterprise/20261017/final-logs/control-smoke-gate.json) |
| `release` 汇总 | FAIL | 安全发现及必测阻塞/未执行项使退出码非零；[门禁结果](enterprise/20261017/final-logs/release-gate.json) |

控制面覆盖五角色 × 13 条路由、匿名拒绝、首次改密、会话失效、禁用/角色变更、最后管理员保护、菜单 CAS、资料字段注入、主机 CAS/同 SN 并发、CSV 预演与原子失败、分页、审计、来源边界及坏介质。合成 Rocky 介质经真实上传、识别和导入后，走不同账号模板审批、批量任务全成全败、同主机唯一活动任务、快照固化和预览；未直接写业务表伪造 ready/done。

浏览器通过实际 UI 登录，使用生产 hash 路由及独立 admin/viewer 上下文，验证管理员菜单页、主机页深链接刷新、viewer 隐藏写入口、viewer 管理路由拒绝。没有注入 token 或 mock 后端。备份恢复覆盖专用 schema 和 staging/artifacts/SMB 内容及摘要；进程重启检查持久化和旧会话失效。该小数据集恢复不代表完整灾备 SLA 或升级资格。

## 安全发现

以下版本和建议修复下界来自此次工具输出，尚未做人工可利用性确认，也未验证升级兼容性。扫描包含测试辅助包，例如部分 x/text 路径来自 pgtest；HTTP/2 路径亦含接口/错误格式化的保守分析。必须逐条确认实际部署可达性后修复或形成有依据的处置结论，不能直接把调用图发现等同于攻击复现。

| 模块 | 当前版本 | 工具报告的修复下界 | 发现 |
| --- | --- | --- | --- |
| golang.org/x/text | v0.34.0 | v0.41.0（覆盖本轮两项） | [GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629)、[GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) |
| golang.org/x/net | v0.51.0 | v0.60.0 | [GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617)、[GO-2026-6612](https://pkg.go.dev/vuln/GO-2026-6612)、[GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611)、[GO-2026-6603](https://pkg.go.dev/vuln/GO-2026-6603) |
| github.com/quic-go/quic-go | v0.59.0 | v0.59.1 | [GO-2026-5676](https://pkg.go.dev/vuln/GO-2026-5676) |

govulncheck 另报告 3 条导入包级、24 条仅模块级发现而未观察到调用，与上述 7 项分开记录。包装命令 `go run ...@latest` 本轮解析为 v1.9.0，退出 1，工具底层退出 3；后续重现应固定 v1.9.0，漏洞库更新可能改变结果。前端使用 `pnpm audit --prod --json --registry=https://registry.npmjs.org`，workspace 分类包括构建工具，不能把所有条目视为已部署浏览器漏洞。此次没有在 test 中修改冻结产品依赖；修复应进入 dev 后重新验证并提升新候选。

## 计划覆盖和剩余资格

完整目标见 [企业测试计划](../plans/2026-10-17-enterprise-testing.md)。本轮新增入口集中在 `tests/lobertest/enterprise`，外层旧 pytest 框架尚未整体迁移；没有以旧版默认过滤后的绿灯代替当前测试，也没有将计划中的十项实施任务全部勾完。

| 计划套件 | 本轮状态与缺口 |
| --- | --- |
| Q01 基础/架构 | 本轮候选门禁通过 |
| Q02 数据一致性 | 产品 PostgreSQL 测试及上述 HTTP 并发、CAS、批次原子性通过；完整计划矩阵尚未逐条映射 |
| Q03 API | 本轮用例通过；完整事件补读、导出、所有生命周期与独立持久化审计核对未覆盖 |
| Q04 权限/安全 | 权限回归通过；漏洞扫描失败，完整资源耗尽/输入安全矩阵未完成 |
| Q05 介质 | 合成夹具及损坏输入通过；无本轮合格真实介质清单，真实发行版资格 BLOCKED |
| Q06 渲染 | 现有产品回归通过；目标解释器和七格式实际执行资格不由本轮合成介质证明 |
| Q07 PXE/外部服务 | BLOCKED：当前重写版第七层未交付；未动用 PVE 创建装机目标 |
| Q08 机器身份/状态机 | BLOCKED：机器面未实现，未伪造安装信号 |
| Q09 真实装机 | BLOCKED：依赖真实介质和机器面，未执行任何真实装机 |
| Q10 故障恢复 | 进程重启及小数据集备份恢复通过；导入关键断点、DB/网络/磁盘故障完整矩阵 NOT_RUN |
| Q11 容量/稳定性 | NOT_RUN：10k 主机/100k 任务等计划负载、吞吐及 24 小时持续运行未执行 |
| Q12 发布/升级/灾备 | 空库初始化、实际前端嵌入、小数据集恢复通过；受支持升级起点、完整异机恢复、离线包与回滚资格 BLOCKED/NOT_RUN |

`control-smoke` 是明确命名的当前子集，不是完整控制面资格；release 的任何适用项为 FAIL、BLOCKED 或 NOT_RUN 均不得发布。计划其余用例仍需实施，不能只补齐当前 JSON 文件就认定全部 Q01–Q12 已完成。

## 测试工具审查与失败处置

完成一次最终代码审查：无 Critical，发现三项 Important——结果未绑定实际源码/二进制、失败重跑可能残留旧 PASS、授权 GET 可错误接受 400/422。已集中修复：逐文件/归档/二进制摘要、启动即原子写 RUNNING、结束及日志摘要校验、GET 必须成功。新增回归先失败再通过；[审查修复 RED](enterprise/20261017/final-logs/review-fixes-red.log)、[GREEN](enterprise/20261017/final-logs/review-fixes-green.log)。没有声称修复后又做了第二轮审查。

实际 VM 运行又发现 libpq 环境参数错误：URI 直接放 PGDATABASE 导致零业务用例。新增四项连接转换回归后修复，秘密保留在环境而非参数/输出；[RED](enterprise/20261017/final-logs/db-env-red.log)、[GREEN](enterprise/20261017/final-logs/db-env-green.log)。RED 归档中的测试用 URI 字面量已脱敏，原日志留在受限 VM 目录。最终门禁 25 项通过。初版门禁也保留 [RED](enterprise/20261017/final-logs/gate-red.log) 和 [GREEN](enterprise/20261017/final-logs/gate-green.log)。

浏览器初版断言与实际 UI 不符：登录按钮 aria-label、生产 hash 路由、原生 checkbox 及页面默认 viewer 角色。根据源码和实际渲染诊断修正，保留失败日志及受限截图于 VM；最终 88 项全量重跑通过。ISO 用例修正为契约规定的 400/同步 422，并保留拒绝后无业务写入或无来源入队的断言；没有通过放宽成功 GET 错误码消除失败。

环境处理：原生 worktree 工具缺少其配置的 Git 可执行文件，确认已登记 test 路径不存在后以现有 Git 恢复，没有清理其他 worktree。早期宿主机/WSL 结果已被指定 VM 的新结果替代。VM 无法直连包仓库，经临时 SSH 回环代理安装锁定依赖/浏览器；镜像站缺少 audit 接口后改用官方接口，原失败日志保留。一次补充 Go 检查的包装脚本被更新打断，未生成有效退出码，故保留原日志并完整重跑，未以日志尾部的成功行判通过。

## 分支决策

按用户要求直接在 test 增加测试代码及证据，未采用计划里可选的中间分支。dev 未改变，原开发工作区未提交改动保留。企业 release 失败，因此停留 test，不合入 main、不推送远程。下一步先在 dev 处理依赖发现并复验；第七层及介质/容量/升级条件就绪后，继续完成剩余资格测试。
