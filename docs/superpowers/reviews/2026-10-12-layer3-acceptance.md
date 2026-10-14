# 第 3 层验收与证据归档

日期：2026-10-12。下列完整检查的实际受测代码：`a4cc3b0d0e6982b330cf9d082119f5a14ff701de`（Task10 测试断言补漏之前，仍是当前生产代码验证依据）。检查地点：隔离克隆 `/mnt/e/src/lober/.tmp/wf/r10`，保留共享主目录中另一代理的工作。开发/检查使用 WSL Ubuntu、`go1.27.2 linux/amd64`；服务程序只部署 Linux。用户已安装的 nginx、Kea、TFTP 配置未改动。

控制器在 Task9 最终修正集成后执行实际代码检查；Task10 自审发现 T8-8 的扫描能力对齐断言实际漏做，控制器追加授权，仅在 scan_test.go 补三条断言；除此只同步文档与归档证据，无生产代码改动。该测试修正只跑 scan focused 与受控负例，没有重复全套产品检查。下列历史检查已通过；Task10 独立评审随后通过，818fb667 整分支评审提出 I1–I3 和 M1–M2。五项在 2026-10-13 单次修正中处理，见下方追加记录；**修正后控制器完整检查、一次定域复审与本地 dev 集成仍待完成**。控制器将在 Task10 提交后刷新 make check，并在本地 dev 集成提交中保存这些后续报告、最终验证和完整更新台账，再清理本次 SDD；本记录不提前声明最终评审通过。

## 最终实际代码检查

在 Ubuntu 中执行：

```sh
source /mnt/e/src/lober/.tmp/l3-env-ubuntu.sh
cd /mnt/e/src/lober/.tmp/wf/r10
go version
make check
go test -json ./internal/store/pgstore -run '^Test契约' -count=1 > "$evidence_dir/contract-leaves.jsonl"
```

`evidence_dir` 是本次 SDD 的证据目录，不是数据库连接配置。私有 DSN 未打印、未归档。

| 检查 | 实际结果 | 原始证据 |
|---|---|---|
| `make check` | 五步 gofmt、vet、普通测试、race、Linux amd64 build 全通过，明确 `exit_code=0` | [完整输出](evidence/2026-10-12-layer3/final-ubuntu-make-check.log) |
| PostgreSQL 普通/race | 真正连 Ubuntu 测试库执行；普通 35.360s，race 51.982s | 同上，含 builtin 升级重放 |
| 既有仓储契约 | 262 个唯一、实际 PASS 的叶子；0 fail、0 skip；满足不少于 262 的底线 | [原始 Go JSON](evidence/2026-10-12-layer3/contract-leaves.jsonl)、[叶子全名](evidence/2026-10-12-layer3/contract-leaves.txt) |
| 完整内置清单/激活/快照 | 7 模板、5 hook；13 系构建；14 份分开的 BIOS/UEFI 应答 golden（ESXi BIOS 仅渲染 fixture，菜单拒绝 BIOS） | [Task9 报告](evidence/2026-10-12-layer3/task-9-report.md)、[功能 GREEN](evidence/2026-10-12-layer3/task9/green.log) |
| 不可变历史与真实升级 | v1 原始字节与两个历史指纹不变；Kickstart/packages 改动升 v2，新身份从 v1 起；完整 v2 清单与 12 当前指纹固定；真实 PG 重放 v1→v2→v2，比较历史内容及存储 Rev | [PG 重放](evidence/2026-10-12-layer3/task9/pg-replay.log)、[Q1 修正后 focused 检查](evidence/2026-10-12-layer3/task9/q1-focused.log) |
| 可选 native parser | Jsonnet 0.21.0、Agama v17 commit `6d218f662791a49c7ded902da619558448ef08b8` 的四份固定 SHA schema、Ubuntu YAML 结构/值通过 | [原始输出](evidence/2026-10-12-layer3/task9/native-final.log)、[依据与重现](../../../internal/builtin/testdata/native-content.md) |

契约计数来自控制器的 JSON 事件收集：先拒绝任意 `Action=fail/skip`，只保留实际 `Action=pass` 且名称以 `Test契约` 开头的用例；若存在以 `name + '/'` 开头的通过后代，则父项不是叶子。没有用源码 grep、缓存 PASS 名或父用例数推算 262。原始事件和全名均已归档，足以复算。

## 任务和评审依据

| 任务 | 最终提交/范围 | 证据与最终任务门禁 |
|---|---|---|
| 1 | `28a55e6` | 五条 Build 不变式、SubType/Sub/Join/EnvValue、T6-3 B/S1-6；前次控制器检查与最终核心测试覆盖 |
| 2 | `6d339f2..2e50c23` | catalog 文件/地址/制品布局、扫描能力对齐；前次控制器检查与最终 catalog 测试覆盖；T8-8 漏项在 Task10 另补，不能归作 Task2 已做 |
| 3 | `fcdf5ea..c3c16bd` | 全模块白名单、未知层拒绝、仓储/渲染时钟检查及合成反例；前次控制器检查与最终 archtest 覆盖 |
| 4 | `268def9` | [报告](evidence/2026-10-12-layer3/task-4-report.md)、[评审](evidence/2026-10-12-layer3/task-4-review.md)：通过 |
| 5 | `09de4da` | [报告](evidence/2026-10-12-layer3/task-5-report.md)、[评审](evidence/2026-10-12-layer3/task-5-review.md)：通过 |
| 6 | `b5ca3ce` | [报告及两轮修正](evidence/2026-10-12-layer3/task-6-report.md)、[原评审](evidence/2026-10-12-layer3/task-6-review.md)、[fix1](evidence/2026-10-12-layer3/task-6-fix1-review.md)、[最终 fix2](evidence/2026-10-12-layer3/task-6-fix2-review.md)：最终无未处理 Important |
| 7 | `c284faf` | [报告](evidence/2026-10-12-layer3/task-7-report.md)、[评审](evidence/2026-10-12-layer3/task-7-review.md)：通过 |
| 8 | `555afc1` | [报告](evidence/2026-10-12-layer3/task-8-report.md)、[评审](evidence/2026-10-12-layer3/task-8-review.md)：通过，两个 Minor 留最终门禁裁定 |
| 9 | `a4cc3b0` | [报告含 Q1 修正](evidence/2026-10-12-layer3/task-9-report.md)、[原评审](evidence/2026-10-12-layer3/task-9-review.md)、[fix1](evidence/2026-10-12-layer3/task-9-fix1-review.md)：共享 SSH embed 已修，最终通过 |
| 10 | 本提交的文档/证据与 scan_test.go | T8-8 的三条缺失断言已补，受控负例各自 RED、真实 scan 全包 GREEN；[证据](evidence/2026-10-12-layer3/task10/) |
| Task10 评审/整分支 | 本提交后的控制器门禁 | 尚未运行，不记为通过；刷新 fullcheck、后续报告与结论保存于 dev 集成提交 |

任务 1–3 原始逐任务 RED 输出不在本次 SDD 资料内，计划中相应 RED 框保持未勾。任务 4–9 的 RED/GREEN、提交与任务评审以完整报告和保留输出为据；早期 Go 1.27.1/Windows 目标数据是历史证据，不能冒充当前 Ubuntu 检查或原生安装认证。

已定六条分别落在：重名插槽/P-2（任务 1 的 Build 用例），T6-3 B（IsSafeWord 与逐字符 ValidID 用例），全模块守卫（任务 3 合成反例），按旧 golden 分批（任务 6/7 逐行对照与任务 8 来源审计），S2-22 选 ①（全部历史/当前指纹与 PG 升级重放）。完整 [控制器台账](evidence/2026-10-12-layer3/progress.md) 原样保留 ALL Ruling、依据、cost if wrong、环境变更与延期裁定，没有改写为成功摘要。


Task10 的负例使用 Go overlay 只改测试内真实扫描文本及其手写期望，保持真实 Scan 行为：去掉 Kickstart syslog、加入私有 sys:token、把 unattend pre 改为不支持的 post-target。旧断言三例都 PASS，证明原缺口；加入三条能力对齐断言后三例分别因 RequiredParts、SystemValues、Timings 报 FAIL/exit1，而非编译错误或无关期望。取消 overlay 后 `go test ./internal/core/scan -count=1 -v` PASS 0.023s，gofmt 无输出。所有 tracked fixture 字节未改。原始 [baseline](evidence/2026-10-12-layer3/task10/baseline-parts.log)、[parts RED](evidence/2026-10-12-layer3/task10/red-parts.log)、[sys RED](evidence/2026-10-12-layer3/task10/red-sys.log)、[timing RED](evidence/2026-10-12-layer3/task10/red-timing.log)、[GREEN](evidence/2026-10-12-layer3/task10/green.log) 及 [受控 mutation 工具](evidence/2026-10-12-layer3/task10/scan-capability-probe.py) 一同保存。最终完整检查须由控制器在该测试修正提交之后刷新，不能把上面的 a4cc3b0 日志冒充新提交的完整验证。

## 失败证据与来源归档

归档目录 [evidence/2026-10-12-layer3](evidence/2026-10-12-layer3/) 保存任务 4–9 完整报告、原评审与全部修正评审，所引用的原 SDD/克隆绝对路径保留为历史原文；同名证据迁移至此目录，Task9 的证据位于其 `task9/` 子目录。未归档秘密 DSN、重复 review diff 包；已评审代码保存在 Git 历史中。

关键失败没有丢弃：Task5 DB 不在线时明确失败；Task6 golden RED、3916 字符 FirstLogonCommand 超限修正；Task7 原始介质约束 RED 与当时完整检查失败；Task8 缺角色/ESXi 归属/Windows 字节和状态桥接 RED，尤其 [watchdog cancellation RED](evidence/2026-10-12-layer3/task-8-watchdog-red.log)、[原 fullcheck 失败](evidence/2026-10-12-layer3/task-8-fullcheck-red.log)、[修正 GREEN](evidence/2026-10-12-layer3/task-8-watchdog-green.log)；Task9 清单/native/lifecycle/AutoYaST/golden-release RED 与对应 GREEN。早期 native Windows 的既有数据仅作为被撤回评审发现的历史依据保存，此次没有继续 Windows 主机适配或原生检查。

来源审计保留 [format-sources](evidence/2026-10-12-layer3/format-sources.md)、[Task8 source-notes](evidence/2026-10-12-layer3/task-8-source-notes.md)、[Debian cached-source 取回地址/哈希](evidence/2026-10-12-layer3/task9/debian-source-provenance.txt) 与两份原始 recipe。Debian 当时取 master 未记录上游 commit，只证明所读字节的内容，不能证明某 ISO 携带相同 recipe。Agama 四份 [schema 原始字节](evidence/2026-10-12-layer3/task9/agama-schema/) 已归档，由 tracked native_validate.py 固定 SHA 与局部引用解析，实际 ISO 版本仍须资格确认。

完整 Task7 旧版差异已复制到 [menu-legacy-line-diff.md](../../../internal/render/testdata/menu-legacy-line-diff.md)，Task6 [旧版应答审计](../../../internal/render/testdata/legacy-line-diff.md) 与 Task8 [runtime-contract](../../../internal/render/testdata/runtime-contract.md) 已在库内。旧差异与新 golden 来源逐项有解释；没有把真实安装待验证项算成 golden 的无法解释差异。

## 后续层义务与未裁定事项

- 第 4 层：固定全部媒体角色；验证相对安全路径、实际文件/仓库完整性、版本/架构与目标 ABI；Debian netboot、Agama schema/product、Windows PE/最终 OS 双环境 helper、ESXi 原始树/boot.cfg 都来自实际介质。渲染信任冻结输入，不读文件。
- 第 5 层：先 EnsureBuiltin hooks 后 templates；预览与下发同一 Render，明确 token/finalize 示例；按当前 7/5 清单和 CurrentApproved 过滤旧身份。L-8 全局词表/Subject.Kind、S3-10 全集对齐仍属第 5 层。
- 第 7 层：SignalURL POST/text/plain 的 slot/instance/timing/digest/rc 与 4096 字节 hook tail 的解析、鉴权与幂等；独立 24 小时 finalize 凭据的签发/权限，不复用 90 分钟 boot token；实际固件/Secure Boot 校验与真正 BIOS/UEFI 安装。
- 目标安装：Linux systemd 和阶段根路径；Agama 单一 post/systemd 路径；Windows OEM/Setup/单条 FirstLogonCommand/AutoLogon、CMD/PowerShell 字节/原生全宽状态/后代终止；ESXi encoded native firstboot、绝对 owned-$0 文件及 applet/进程组 ABI。Secure Boot 不支持 ESXi 此生命周期。Server2016/2019 静态 edition 数据不是当前 UTF8 helper 的 firstboot 支持；Windows SSH 仅保证 2025。T4-3 桌面版是否限 UEFI 留第 7 层。
- Task8 两个延期 Minor 最终确认为 M1/M2，已纳入 2026-10-13 单次修正：有效 root_password 与显式加密 fixture，ESXi watchdog 取消时终止并回收 sleep。历史 Task8/Task10 文档原字节保留，不能据其旧延期状态认定当前仍未修正。

Ubuntu 的 shell/进程/字节检查、Jsonnet/schema、XML/YAML 和 golden 只证明其各自边界；本层没有声称真实 Agama/AutoYaST/Windows/ESXi 或任何安装介质已完成真机验收。


## 2026-10-13 最终评审单次修正

基线 `818fb667f9d746cdce30e7b0cb5cfafb35b522aa`。全部五项：I1 通用 NoCloud metadata 不再要求作者参数名 hostname，真实 activation/Build 验证字面量、改名参数与 builtin identity.hostname；I2 Debian installer-time POST 使用已确认路径的真实 GNU wget-udeb，受限 PATH 的真实 HTTP 验证空请求及含 NUL 的4096字节日志，target curl 保留；I3 Makefile 正式门禁显式无缓存发现模块包，并回归既有根目录新增未知包；M1 修正 ESXi 参数/加密密码 fixture；M2 ESXi watchdog 明确取消并回收 sleep，保留超时/必选可选与进程组策略。

修正作者只执行覆盖包与针对性回归，证据见 [命令与来源](evidence/2026-10-13-layer3-final-fix/provenance.md)、[GREEN](evidence/2026-10-13-layer3-final-fix/covering-green.log)、[增量门禁](evidence/2026-10-13-layer3-final-fix/green-i3.log)、[真实 HTTP/runtime](evidence/2026-10-13-layer3-final-fix/green-runtime.log)。控制器提交后完整 make check、一次定域复审和 dev merge/清理闭环仍待执行；不以历史绿色结果冒充修正后的验证。没有重写 builtin 模型/发行快照/指纹；没有 Windows host adaptation/native checks 或实际媒体安装声明。
