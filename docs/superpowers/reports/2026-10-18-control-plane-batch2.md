# 第二批企业控制面测试

本批范围为已批准计划 Tasks 3/4/6 的增量：真实浏览器完整业务、权限刷新与资源旧响应隔离、事件传输及控制面故障恢复。产品基线仍为 dev `88bce232fc633ff5dc06c0285748d17f4f47b184`，测试工具冻结提交为 `fc6d827f1a21afed5430febc25a41756ad6d37b9`。本批 control-complete 汇总 PASS：131 项企业子集测试，0 失败、0 跳过；后端与前端门禁通过。release 汇总保持 FAIL。

本批只修改 test 分支的测试工具和文档。所有被测进程、浏览器、PostgreSQL schema、网络及磁盘故障均在指定测试 VM 上，没有操作原有 QA 服务或 PVE。最终工作目录 `/home/dev/lober-control-20261018-reviewed`。候选二进制沿用上轮生产构建，逐项核对 1643 个产品/构建输入完全一致，SHA256 为 `be7630aac1787701ad9abb5dd3a230a90abe0d329e458f0abfbc6329efab7970`。

后端和前端门禁本批在第一次冻结 `078117f` 上重新执行。审查修正只改 Python/JS 测试，不改产品；最终冻结再次核对完整产品输入映射一致，结果保留命令实际 run_id 与 manifest 摘要，不把第二次归档视为重新执行了相同 Go/前端命令。5 个企业子集则全部在 `fc6d827` 冻结归档上重跑。

最终 run_id 为 `vm-control-20261018-9400840715ba9728`，源码归档摘要 `a38e15fd376aba45833d9ecb51c019f71973da83a82f8c4b58e1022064802631`，manifest 摘要 `f7f8c1d831249f013e48087853c2bf4207af1cdec42bfbcc9597175de53d10eb`。二进制嵌入前端的来源归档摘要 `56534e8e1992851122d4abc41ccc0eff200bce06402d5736026e6d3b4d489089`。证据文件保留原始字节，配置、口令、session、私钥、数据库备份不进入报告。

## 结果范围

| 套件 | 数量 | 覆盖 |
| --- | --- | --- |
| backend | 18 个含测试 Go 包 | make check：格式、vet、普通测试、race、Linux 构建、Wire 漂移；另跑 go test -count=1 ./... |
| frontend | 25 文件 / 88 测试 | 类型、lint、Vitest、11/11 构建任务；沿用前一批 frozen lockfile 安装，未重新安装依赖 |
| gate | 30 | 既有身份/证据门禁、新子集源码和二进制绑定、活动服务与故障依赖清理顺序 |
| control | 88 | 既有真实 API、五角色矩阵、并发、快照、备份恢复及浏览器基础路由 |
| flows | 4 测试 / 8 浏览器场景及 1 项脱敏回归 | 四条完整业务、真实旧资源响应、角色撤销、空菜单恢复、同页退出与切换账号 |
| events | 4 | 205 条分页回放、Last-Event-ID 优先、持久化去重、无通知补读、账号停用断流、浏览器 TCP 真断线与回放去重 |
| recovery | 5 | 独立 DB 连接中断、8 MiB 临时盘 ENOSPC、TCP 下载截断、SIGKILL 与断点续传、真实连接超时 |

完整业务场景使用真实表单和独立登录：主机登记及 CSV 预览零写入再确认；浏览器上传合成 ISO 并核实可用和摘要；派生模板、替换 hook、不同账号审批；两台主机各自渲染预览并原子创建任务。任务保持 pending，不代表开始或完成装机。

事件探针通过编译自受控测试源码的 store helper 产生，只允许随机 enterprise schema、ENTERPRISE 前缀主机和 pending 任务，事件类型为 test.transport。验证真实持久化与 SSE，未实现的机器信号链不借此算通过。浏览器网络故障实际关闭本测试 TCP 代理的连接，不能只设置浏览器 offline 标记后假设已有 SSE 已断。去重测试还延迟一条真实 REST 响应，先确认同一 seq 5 经 SSE 显示，再放行该 REST 响应，等待响应及界面更新后断言仍只有序号 1–5。

故障场景均有“已发生”和“恢复不变量”两组断言：DB 代理关闭已建立连接后写入拒绝，恢复后旧记录不变且新记录只有一条；临时盘真实 ENOSPC 后不发布半成品，释放空间再上传摘要一致；下载截断记录 failed/error 后显式重试；SIGKILL 时确认已有持久化断点，重启后实际发出非零 Range、摘要一致，旧 session 失效；TCP 超时仅 DROP 独立来源端口，确认请求未到 HTTP 服务、来源持久化超时，再删除规则重试。故障结束清理 schema、配置、挂载与专用规则，不填满 VM 宿主盘，不停共享 PostgreSQL。

## 未覆盖与发布限制

- 导入发布文件与 DB 确认边界、删除日志边界的精确进程崩溃；DB 账本超过落盘字节；只读文件系统；浏览器上传刷新后补传，尚无本批进程级资格。
- 旧资源请求隔离已测；跨账号切换同时存在旧请求的专门竞争场景尚未单独资格。
- TCP 建连超时已测；已连接后无限停顿的读取超时策略未在本批资格化。
- 真实介质、七格式真实安装、第七层网关/机器状态机、PXE 链路、容量及 24 小时长稳、受支持升级及完整异机恢复仍待完成。
- 前一批保留的 node-forge、braces、smol-toml 三条依赖告警本批未重新审计、未豁免；ISO/WIM fuzz 与 Go 漏洞扫描未重复运行，不把前一轮结果改签为本轮 PASS。

control-complete 只是 profiles.json 列出的明确集合，不代表上述缺口已经完成。release 必须继续失败。测试代码留在 test；dev 和主开发工作区保持原状态，未推送远程或合入 main。

## 实施决定与审查

本批实施决定（Rulings I made）：

- 沿用已建立的标准库 runner，避免旧 pytest fixture 和过期协议混入；若后续框架约定不同，代价是迁移入口。
- transport-only 探针只证明存储和传输，第七层交付后另跑机器信号；若误作机器资格，代价是缺少真实执行链验证，因此本报告明确排除。
- 恢复断言遵循当前明确契约：terminal source failure 经显式 Retry 清零重取，崩溃 fetching 从断点续传；契约改变时需要更新并重跑这些测试。
- control-complete 指明示集合，未具备可控时刻的崩溃点记录为未执行；代价是仍需补充精确发布/删除断点资格。

按 executing-plans / requesting-code-review 执行一次独立只读审查，范围 `9a4d43d..078117f`。Critical 0，Important 2：失败日志只处理 query token，可能泄露 Playwright 请求头中的 Bearer；原浏览器去重场景没有确定性地将同一事件交给 REST 与 SSE 两条 UI 回调。另有一项原评 Minor 的清理顺序，因失败测试可能留下挂载，将其按 Important 一并处理。

一次修正批次 `fc6d827` 解决三项：对 Bearer 与已知随机秘密统一脱敏；新增真实 REST/SSE 重叠投递并等待回调；先终止候选进程（含超时强杀后备），再卸载临时盘与关闭 DB 代理。脱敏、原场景缺重叠、原清理顺序均留存先失败后通过证据；修正后重跑最终套件。未声称进行第二次独立审查。Deferred minors：无。

审查未判断最终 VM 输出、manifest 或真实网络超时；随后由本轮实际冻结运行补证。精确发布/删除崩溃、跨账号旧请求竞争、真实介质/装机/容量/升级不在本批资格内，仍列于未覆盖清单。

最终源码、归档、二进制、运行标识和日志摘要均通过门禁核对。故障清理后，专用超时规则与 enterprise 临时盘挂载残留均为 0。公共证据包传输 SHA256 为 f426708dc32d98a5fd9695b4de2910b2f29dc57ff1ccbfe3c99f78b2d2f236ed。

本批证据索引：

- [候选清单](control-plane/20261018/results/candidate.json)、[后端/前端命令原始候选清单](control-plane/20261018/results/baseline-candidate.json)、[运行环境](control-plane/20261018/results/runtime.json)。
- [backend](control-plane/20261018/results/backend.json)、[frontend](control-plane/20261018/results/frontend.json)、[gate](control-plane/20261018/results/gate.json)、[control](control-plane/20261018/results/control.json)、[flows](control-plane/20261018/results/flows.json)、[events](control-plane/20261018/results/events.json)、[recovery](control-plane/20261018/results/recovery.json)；各 JSON 指向同目录原始日志及摘要。
- [control-complete 门禁](control-plane/20261018/results/control-complete-gate.json)、[release 门禁](control-plane/20261018/results/release-gate.json)、[故障资源清理](control-plane/20261018/results/cleanup.json)。
- [脱敏回归先失败](control-plane/20261018/results/redaction-red.log)、[脱敏通过](control-plane/20261018/results/redaction-green.log)、[缺少真实重叠先失败](control-plane/20261018/results/overlap-red.log)、[旧清理顺序先失败](control-plane/20261018/results/cleanup-red.log)。最终 events 和 gate 日志分别是重叠与清理修正的通过证据。

此前的依赖告警和 fuzz/漏洞扫描记录见[依赖安全报告](2026-10-18-dependency-security.md)；本报告不将旧运行改签为新运行通过。执行原计划与各项待补资格见[企业测试计划](../plans/2026-10-17-enterprise-testing.md)。
