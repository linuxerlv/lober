# 2026-10-18：依赖安全修复及 VM 企业测试复验

本批已执行依赖安全修复，并按修复分支 → dev → test 提升候选。Go 原有 7 项调用路径告警已清除；前端 workspace 审计由 48 项降为 3 项，其中严重级别由 2 项降为 0。当前控制面检查通过，企业发布资格仍未通过，剩余依赖告警和未完成资格继续阻断发布。

## 修复与分支身份

| 项目 | 身份 |
| --- | --- |
| 修复起点 | dev `f68432abd0a7960b6b96c4a98ef42901ea44fb48` |
| 修复分支 | `codex/dependency-security`，工作区 `E:/src/lober/.worktree/dependency-security` |
| 依赖升级 | `78065b6c7b65ea5a72fd8ce58a829506a7b3f47a` |
| 审查后 Tiptap 同组版本修复 / 当前 dev | `88bce232fc633ff5dc06c0285748d17f4f47b184` |
| 冻结 test 候选 | `932b1375a3d5d8bf6fec3a20097071f9f8c3d608`；保留此前 test `f4afffa` 的测试及报告 |
| VM 运行 ID | `vm-security-20261018-1c71a62e06991542` |
| 实际二进制 SHA256 | `be7630aac1787701ad9abb5dd3a230a90abe0d329e458f0abfbc6329efab7970` |
| 候选清单 SHA256 | `4e24a1d6ced5e949c8065b37bd90033c521d48e4b45466e614feeb4889c9ccfd` |

产品变化仅涉及 `go.mod`、`go.sum`、`web/pnpm-workspace.yaml` 和 `web/pnpm-lock.yaml`。test 与 dev 的 cmd/internal/web、Go 清单及 Makefile 已核对一致。报告与证据提交晚于冻结候选，只增加文档，不改变已验证产品或测试工具。

Go 升级 quic-go `0.59.0 → 0.59.1`、x/net `0.51.0 → 0.60.0`；后者要求 x/text `0.42.0`，因此采用该兼容依赖组合，同时更新所要求的 x/crypto、x/sys、x/sync、x/mod、x/tools。清单和校验和通过 VM 上 `go get`/`go mod tidy` 生成。

前端升级 axios、qs、Vue、Tiptap，并针对告警增加同主版本依赖约束。Tailwind typography 的 selector-parser 单独切换至 `7.1.6` 以上，避免旧版解析器残留；其余包的约束保持各自主版本范围。所有 Tiptap 包最终统一为 `3.32.0`。锁文件在 VM 上由 pnpm 生成，安装使用 frozen lockfile；没有使用 audit 忽略清单，也没有修改门禁接受残留告警。

## 验证环境和证据

全部测试及被测服务在用户指定 VM `192.168.137.21` 上运行：4 vCPU、Linux 6.8、Go 1.27.2、Node 24.22.0、Python 3.12.3、PostgreSQL 服务端 16.15；详见 [runtime.json](dependency-security/20261018/results/runtime.json)。

第一轮依赖门禁目录为 `/home/dev/lober-security-20261017`；最终冻结候选目录为 `/home/dev/lober-security-20261018-final`。最终清单逐文件核对第一轮通过门禁的 419 个后端输入、1,224 个前端输入与 test 候选一致。二进制从最终 test 源码构建，嵌入本批 VM 生产前端；运行器再次验证归档、源码、二进制及日志摘要。构建使用 `-buildvcs=false`，因此提交身份由外部清单记录，二进制模块版本另保留 [buildinfo](dependency-security/20261018/results/binary-buildinfo.log)。

| 检查 | 结果 | 证据及适用范围 |
| --- | --- | --- |
| Go `make check` 与 test 上 `go test -count=1 ./...` | PASS | [backend.json](dependency-security/20261018/results/backend.json)：18 个含测试包，格式、vet、普通测试、race、Linux 构建、Wire 漂移，以及最终候选不缓存测试全部通过 |
| 前端 locked install/stub/check:console | PASS | [frontend.json](dependency-security/20261018/results/frontend.json)：OpenAPI 类型、Vue 类型、lint、25 文件/88 测试、11/11 构建任务；包含 Tiptap 版本对齐后的复测 |
| Tiptap 依赖族一致性 | PASS | [RED](dependency-security/20261018/results/tiptap-peer-red.json) 复现 3.30.3/3.32.0 混用；[GREEN](dependency-security/20261018/results/tiptap-peer-green.json) 35 个锁定包条目均为 3.32.0 |
| Typography 解析器兼容性 | PASS | [css-compatibility.json](dependency-security/20261018/results/css-compatibility.json)：41 个生产 CSS 文件名称及字节摘要与替换前一致 |
| test 控制面 HTTP/浏览器/恢复 | PASS | [control.json](dependency-security/20261018/results/control.json)：88 项、0 失败、0 跳过，运行实际候选二进制和前端 |
| 门禁自身回归 | PASS | [gate.json](dependency-security/20261018/results/gate.json)：25 项、0 失败、0 跳过 |
| ISO9660 / WIM fuzz | PASS | 各运行 10 分钟；[ISO](dependency-security/20261018/results/fuzz_iso.json) 6,498,964 次，[WIM](dependency-security/20261018/results/fuzz_wim.json) 5,146,385 次，共 11,645,349 次输入 |
| Go govulncheck v1.9.0 | PASS（符号级门禁） | [security_scan.json](dependency-security/20261018/results/security_scan.json)：0 调用路径、0 导入包级告警；1 条模块级提示完整保留 |
| 前端 workspace 依赖审计 | FAIL | [frontend_audit.json](dependency-security/20261018/results/frontend_audit.json)：3 项，2 高危、1 中危、0 严重；审计退出码仍为非零 |
| 控制面汇总 | PASS / exit 0 | [control-smoke-gate.json](dependency-security/20261018/results/control-smoke-gate.json)，仅代表现有子集 |
| 企业 release 汇总 | FAIL | [release-gate.json](dependency-security/20261018/results/release-gate.json)，依赖残留及必要资格阻塞均未豁免 |

本次控制面仍覆盖五角色 HTTP 权限矩阵、认证及会话撤销、CAS、同 SN 并发唯一性、CSV 原子性、模板审批、合成介质真实导入后的批量建任务、快照、生产浏览器登录/路由/只读限制、独立 schema 与配套文件恢复、进程重启。合成介质不能证明真实发行版可安装。

## 仍需处理的告警

前端原 48 条记录中，本批移除 45 条，剩余以下 3 条。它们位于 workspace 的构建或检查依赖链；是否存在实际攻击路径需要进一步核对，当前没有将其豁免为通过。

| 包 / 级别 | 依赖来源 | 现状与后续处置 |
| --- | --- | --- |
| node-forge 1.4.0 / 高危 | vite-config → nitropack → listhen | [GHSA-86w9-cpqp-85rv](https://github.com/advisories/GHSA-86w9-cpqp-85rv) 当前公开通告显示未有修复版本。审计响应建议 1.4.1，但 npm 该版本返回 404；等待上游可信修复，或单独设计移除不需要的 Nitro mock 工具链 |
| braces 3.0.3 / 高危 | stylelint/国际化构建工具 → micromatch | [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)，建议版本 3.0.4 在 npm 返回 404；后续升级采用可获取的修复版本或经过验证的上游补丁 |
| smol-toml 1.8.0 / 中危 | vsh → knip | [GHSA-r4xh-jqrq-34v2](https://github.com/advisories/GHSA-r4xh-jqrq-34v2)，建议版本 1.8.1 在 npm 返回 404；保留为检查工具链待解决项 |

版本可获取性由 VM 直接访问官方 registry 核对，记录于 [published-versions.log](dependency-security/20261018/results/published-versions.log)。没有改写不存在的版本号、用不明来源包替代、隐藏告警或擅自引入本地密码学补丁。上述候选版本只是本次审计返回值，不能视为已发布事实。

Go 扫描保留 [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)：x/crypto 模块包含不再维护的 OpenPGP 包。当前扫描未发现产品导入或调用该包，故它仅列于模块结果中。报告保留其完整输出，不把“0 调用路径”写成“依赖中没有任何安全提示”。

## 审查及处理过程

按 requesting-code-review 技能执行一次独立只读审查，审查范围为 `f68432a..78065b6`。无 Critical；一项 Important：八个 Tiptap 扩展仍锁定 3.30.3，要求核心/pm 的精确旧版本，而解析结果绑定了 3.32.0。现有控制台未使用富文本组件，因此常规控制台测试不能替代依赖族核对。

处理：统一所有 Tiptap catalog，重新生成锁文件；先在旧锁文件复现不一致，再在新锁文件确认单一版本，随后重新执行完整 VM 前端门禁。修复提交 `88bce23` 在提升 dev/test 前完成。没有声称做了第二轮独立审查。

审查未判断残余漏洞的实际可利用性、源码到 VM 逐文件身份、完整浏览器行为或发布资格；本轮随后用冻结清单和实际接口/浏览器测试补充了身份与行为证据，残余漏洞与发布缺口仍按未解决记录。

实施中原生 worktree 工具仍因配置的 `D:/msys/usr/bin/git.exe` 不存在而失败，使用已安装 Git 建立隔离 worktree，未改动原开发区。一次复测脚本缺少 Go 的 PATH，OpenAPI 类型生成未启动；修正环境后完整重跑通过，原失败日志保留在 VM。包下载使用临时 SSH 回环代理；现有 QA 服务及 PVE 未变更。

## 发布资格与分支决定

本批是已经有功能门禁支持的依赖安全改进，允许按既定流程进入 dev/test；它不构成完整企业发布资格。此前 [企业测试计划](../plans/2026-10-17-enterprise-testing.md) 中的以下项目仍未完成：

- 合格真实介质与附加来源清单、七格式真实安装验证。
- 第七层网关、机器身份、安装状态机、超时与防重复装机。
- 完整浏览器业务流程、事件补读、导入关键断点及 DB/网络/磁盘故障矩阵。
- 计划容量档、下载吞吐及 24 小时混合负载。
- 明确升级起点的迁移/回滚、完整异机恢复和离线发布包资格。

release 门禁继续返回非零；main 未合入、远程未推送。后续应先为 3 条残余依赖告警确定可信修复路径，再分批完成现有控制面覆盖和第七层实现及系统资格测试。
