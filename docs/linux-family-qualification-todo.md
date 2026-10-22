# Linux 五类系统适配与验收待办

更新日期：2026-10-22  
状态：本轮 Debian 13.7、AlmaLinux 10.2、openSUSE Leap 15.6 产品 API 装机、重启、公钥登录验收通过（2026-10-22）；Ubuntu 仅基础设施级通过，Rocky／Leap 16 仍未运行。详见 [最终报告](superpowers/reports/enterprise/2026-10-22-product-workflow-qualification.md)。

本文跟踪 Debian、Ubuntu、SUSE、AlmaLinux、Rocky Linux 五类系统的适配与真实安装验收。它是执行清单，不代表测试已经通过。版本、介质和配置以本轮冻结的产品提交及实际 ISO 为准。

## 当前已知情况

- 2026-10-21 已在云服务器 `lober` KVM 内完成 3 台并发 e2e：DHCP → TFTP → HTTP → autoinstall 全链路通过，Ubuntu 24.04.5 无人值守安装到最小容量盘（7 GiB）并成功引导进系统。证据见 [e2e 报告](reports/enterprise/2026-10-21-lober-kvm-e2e.md)。
- 已定位并验证三个根因：iPXE 内核参数 `ds=nocloud-net;s=` 不能转义分号（否则 NoCloud 不解析、退回交互式安装器）；autoinstall `identity` 必须含 `password`（否则报 `Malformed autoinstall in 'identity' section`）；多盘客户机需给目标盘配置显式 boot order，否则装完重启回到 PXE 反复重装。这些目前是测试编排配置，尚未产品化。
- 旧结论“8 GiB 内存不足”针对 ISO 全量暂存路径；以只读 cdrom 提供 live rootfs、kernel/initrd 走 HTTP 时 8 GiB 足够。
- 2026-10-21/22 产品 API 首轮发现 D1／D2／D3；双 users 后证实是抽取拼接伪影，实际 schema 缺陷为 create_home。后续又定位 ISO 长文件名、Debian hostname 时序与 AutoYaST 提前 ready，均已修复并通过本轮三系统实装。历史失败记录保留，最终结果见新报告。
- 曾委托 Claude Code 检查，但 Sonnet 映射到 `deepseek-v4.1-flash[1M]` 后网关报告 `unrecognized_model`，未产生代码更改或测试结果。修复模型路由后再继续委托。
- Linux 隔离工作树 `codex/ubuntu-initramfs-diagnosis` 基于 dev 的 `f5c8ee9`，目前干净；此工作区没有 Ubuntu 修复提交。
- 企业测试套件的总体方案见 [企业级测试计划](superpowers/plans/2026-10-17-enterprise-testing.md)。

## 执行约束

- 客户 VM 可在云服务器内并发：按 31 GiB 宿主内存预算，每台建议 8 GiB，已验证 3 台并发（3×8 + lober 4 = 28 GiB）；超出预算时先关机再启动下一台，避免 OOM。
- `lober-test-server` 是运行 Lober 和程序的服务 VM，保持运行及现有 2.5 GiB 内存；客户安装测试在云服务器内新建/使用独立客户 VM。
- 测试期间保留启动串口日志、安装器结果、服务端请求/任务日志和最终系统信息；证据脱敏后存入本轮报告目录。
- 真实安装通过必须同时证明无人值守配置被获取、安装流程完成、目标系统可登录且版本/网络/磁盘状态符合预期。仅能启动 ISO、渲染配置或单元测试通过都不算真实装机通过。
- 遇到介质、容量、网络或硬件条件不足时，标记 `BLOCKED`/`NOT_RUN` 并记录原因，不得标成通过。

## 待办

### A. Ubuntu 修复与本地验证

- [ ] 恢复可用的 Claude Code 模型路由；确认 Sonnet 到 `deepseek-v4.1-flash` 的请求可以完成，而不是只启动后报 `unrecognized_model`。
- [ ] 在隔离工作树中核对 Ubuntu kernel command line、NoCloud seed 生成 URL 和服务端 answer 路由，确定可复现的具体不匹配。
- [ ] 先增加失败的回归测试，再做最小修复；限制改动在 Ubuntu 安装路径及相邻测试，不混入其他系统重构。
- [ ] 运行相关 `internal/render` 测试及 `go test ./...`；保存实际命令和退出码。通过后提交到隔离分支，不直接改当前有未提交改动的工作区。
- [ ] 确认 ISO 暂存所需内存/临时文件系统容量。已有 8 GiB 容量不足、10 GiB 才能继续启动的现象需要解释；不得把单纯增加内存当成 autoinstall 修复。

### B. Ubuntu 24.04.5 真实安装重试

- [x] 本地测试通过后，在客户 VM 上只运行 Ubuntu 一台；保留服务 VM 不动。本轮扩大到 3 台并发，lober 服务 VM 保持运行。
- [x] 从串口确认安装器自动选择 unattended/autoinstall 流程，不停留在交互式语言选择界面。以 VNC 截图和 seed 请求 200 证据确认；串口未落盘。
- [x] 确认客户 VM 成功获取 user-data 与 meta-data，服务端返回成功状态，安装任务未因 seed 404/网络错误失败。
- [x] 等待安装完成并重启；通过控制台或 SSH 验证系统版本、主机名、网卡、地址、磁盘分区和预期账号/凭据注入结果。
- [x] 记录镜像 SHA256、VM CPU/内存/磁盘/网卡配置、测试开始/结束时间及脱敏日志；通过后关机再开始下一系统。三台已关机，证据目录见报告。

> 说明：以上为基础设施链路与系统级 autoinstall 的 e2e 结果；产品第七层任务管道（token、状态机、机器面回调）未接入，不构成产品任务验收。三个根因需要产品化并补回归测试。

### C. Debian、SUSE、AlmaLinux、Rocky Linux 顺序验收

每种系统均按同一流程逐台、串行执行；每完成并关停一台后才进入下一台。

- [x] Debian 13.7：已通过签名网络源完成产品 API 安装、任务 done、重启和公钥登录；hostname／SN／IP／vdc 根分区与非目标盘检查通过。
- [x] SUSE/openSUSE Leap 15.6：AutoYaST v5 等待二阶段完成后 ready；实际安装、重启、公钥登录、名称／SN／IP／vdc 检查通过。kdump 未验收且两个服务失败，单独记录。
- [ ] SUSE/openSUSE Leap 16.0：验证 Agama 配置获取、安装、首次启动及网络/磁盘结果；与 Leap 15.6 分开记录，不能互相代替。
- [x] AlmaLinux 10.2：Kickstart 产品 API 安装、任务 done、重启、公钥登录与名称／SN／IP／vdc 检查通过。
- [ ] Rocky Linux 10.2：验证 Kickstart 获取、安装、首次启动及网络/磁盘结果。
- [ ] 若本轮介质版本或系列与上述目标不同，先更新支持矩阵并记录实际版本，不用相近版本结果推断覆盖。

每个系统的验收证据至少包括：

1. ISO 文件名、来源、SHA256、版本和架构。
2. 客户 VM 资源与固件/启动方式。
3. 安装配置请求及服务端结果（脱敏）、串口安装日志和最终任务状态。
4. 首次启动后的版本、架构、主机名、网络连通性、分区/引导方式及登录验证。
5. 明确的 `PASS`、`FAIL`、`BLOCKED` 或 `NOT_RUN`，以及失败缺陷编号/日志位置。

### D. 汇总与放行

- [ ] 建立五类系统支持矩阵；SUSE 的 Leap 15.6 与 16.0 单列为不同安装路径。
- [ ] 逐项复核介质完整性、自动安装请求、任务终态和目标系统证据；不得以模板存在或渲染成功代替真实安装结果。
- [ ] 汇总代码提交、测试退出码、VM 测试证据、已知缺陷和未覆盖项，写入 `docs/superpowers/reports/enterprise/` 下的本轮报告。
- [ ] 只有全部声明支持的系统均通过各自验收，才报告 Linux 家族测试通过；部分通过时列出通过项和阻塞项，不扩大结论。

## 本轮状态记录

| 系统 | 当前状态 | 下一步 |
| --- | --- | --- |
| Ubuntu 24.04.5 | `PASS`（基础设施级 e2e，2026-10-21）；产品 API 全链路尚未复验 | 用产品 API 导入 ISO 并走 wizard 任务复验 autoinstall |
| Debian 13.7 | `PASS`（产品 API、2026-10-22、任务 dimiisfkpajdr54zzwfkkggm5a） | 保留证据；不扩展为其他版本／UEFI 实装通过 |
| openSUSE Leap 15.6 | `PASS`（产品 API、2026-10-22、任务 qbi23njffdyn2cmcduapzie6nm） | 安装／重启／登录通过；kdump 两服务失败单独留项 |
| openSUSE Leap 16.0 | `NOT_RUN`（本轮无真实装机证据） | 串行测试 Agama |
| AlmaLinux 10.2 | `PASS`（产品 API、2026-10-22、任务 z6644oadxc4um5v7sehefotuha） | 保留证据；Rocky 不据此推断通过 |
| Rocky Linux 10.2 | `NOT_RUN`（本轮无真实装机证据） | 串行测试 Kickstart |

> 2026-10-22 复验结论：产品 API 与网关链路可用（引导菜单、token 化应答、状态机、事件均正常），失败点在产品内置模板/介质下发逻辑（D1–D3）。同日完成逐包根因复核并在 `codex/d1-d3-fixes`（基于 test bc3141f）落盘修复：D1 网关 UA 分流 + `Preseed` 查询；D2 reqpart 内联分区；D3 去除 create_home；内置内容按契约升版本并录制 release v4。修复后须在 lober VM 构建部署并重跑同一 API 流程的三系统并发验收，通过前不得将 Debian / AlmaLinux / openSUSE 记为通过。注意：升级部署后引用 account/partition v1 的旧派生模板会被向导过滤，需从 v4 内置模板重新 derive。

## 2026-10-22 最终产品流程复验

三系统实际装完且重启后再次登录，三条真实 iPXE 请求均携带 SMBIOS Type 1 Serial Number；已完成任务均返回退出脚本，无再次安装。全局 hook 属性绑定、空／非空 entered 覆盖、缺失属性和非法绑定审批负例均通过。云端 Go 全套 exit 0，前端 95 项、类型检查与构建通过。

标准入口 8080 已使用本地整合版本，8081 保留兼容代理；原运行库／配置／二进制已备份。完整证据、实际范围、历史误判纠正及 Leap kdump 遗留项见 [最终报告](superpowers/reports/enterprise/2026-10-22-product-workflow-qualification.md)。
