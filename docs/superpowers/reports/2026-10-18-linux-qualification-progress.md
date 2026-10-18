# Linux 装机验收进度（未完成）

更新时间：2026-10-18 21:57，Asia/Shanghai。构建、检查和装机均在指定测试 VM `192.168.137.21` 及独占 PVE VM102 上执行。

**当前结论：第三候选完整后端检查及 131 项企业控制面回归通过，control-complete 通过；Linux 实际安装尚无 PASS。Debian preseed v2 与 VM102 的 DHCP 隔离已在原生安装器越过默认路由和镜像地址确认页，正在安装；Ubuntu ISO 整文件哈希及官方签名验证通过。**

## 第二候选产品（保留历史证据）

- 产品 / dev：`d3014e535f1f2a3366cc726b5f30247bd750c8a9`，完整 VM `make check` 通过后按 dev → test 集成。
- 候选二进制的 test 源码：`8051bc1c50824711e5b7c7537b31fe7aaefcb952`。
- 二进制 SHA256：`79713e6ee6ad70dbf38b7ea13fd6836a4568d3027d89b4d6d81ee45b327ac92b`。
- 原始 test Git archive SHA256：`fe0d7553ab88b13720e8174b1513f9dd37d411dd434973eb20c82a4dedc7542b`。
- 前端 213 个实际生成文件，清单 SHA256：`c43f495a3f9bbcb609e3cbdefb8b6d576f42a6e411ece2b759f510d34783f2d2`。
- `candidate-2/candidate-2.json` 绑定源码、实际资源、二进制、run_id；291 个后端源码/模块文件逐一匹配 VM 已验证输入。
- 验收工具独立从 test `13e129b11b7838f3b1a20415aa723b913dd71676` 冻结，结果分别记录构建 test_sha 和 harness_test_sha。QA 修正没有改写已经完成的产品测试证据。
- 没有 push、main 合并；主工作区已有修改未动。

## 第三候选：官方签名网络源与再次验证

- 产品 / dev：`668a512849895ddd00310aca399d0589c9e43cf1`；test 构建及验收工具源码：`09bc87d49b808df830996bdadf98905d84f0ba94`。
- 二进制 SHA256：`96b986c43885e51e38174835122517286cdc084055e9b229c4df0774eab1fdf7`；原始 Git archive SHA256：`00a29fa98c38d9ae1372e97d7971257c4cec6b2256e6551beb441fd0fb7f1edc`。
- VM 完整 make check 退出码 0，21 个测试包、race、Linux 构建、Wire 检查均通过；446 个后端文件逐一匹配已验证输入后才集成 dev → test。真实前端嵌入后的 cmd/lober 与 console 检查及新二进制构建再次通过。
- 本候选重新执行 gate30 / control88 / flows4 / events4 / recovery5，合计 **131 PASS、0 failed、0 skipped**；control-complete 的 problems 为空。
- 前端检查与依赖审计明确复用第二候选结果：1226 个已提交 web 文件及 213 个实际嵌入产物逐一 SHA256 相同，`frontend-reuse-3.json` 保存证明。没有把复用记录称作本轮重新执行的前端测试。
- 验收工具自身 4 项回归通过，新增 cancelled 状态立即终止等待的 RED → GREEN，仍不等于实际安装通过。
- 窄范围独立审查未发现阻塞问题；公共证据保存在 `linux-qualification/20261018/candidate-3/`。历史候选与失败证据保留。

## 第二候选的验证

证据目录：`linux-qualification/20261018/candidate-2/`，每套结果包含退出码、原始日志及证据 SHA256。

| 范围 | 实际结果 |
| --- | --- |
| 完整后端 make check | PASS：21 个测试包，格式、vet、普通测试、race、Linux 构建、Wire 漂移检查通过 |
| 嵌入真实前端后的检查/构建 | PASS：cmd/lober 8.492s、console 57.231s，二进制构建成功 |
| 前端完整检查 | PASS：26 文件、91 测试；11 个构建任务通过 |
| 依赖消费者回归 | PASS：16 项真实 stylelint / glob / CSpell / Knip / shell-quote 等消费者检查 |
| 官方 npm 生产依赖审计 | PASS：1408 项依赖，所有级别告警为 0；不是完整代码安全审计 |
| 企业 gate | 30 PASS，0 failed，0 skipped |
| 企业 control | 88 PASS，0 failed，0 skipped |
| 企业 flows | 4 PASS，0 failed，0 skipped |
| 企业 events | 4 PASS，0 failed，0 skipped |
| 企业 recovery | 5 PASS，0 failed，0 skipped，含真实 TCP 超时、ENOSPC、SIGKILL 和数据库连接丢失 |
| control-complete profile | PASS，缺项列表为空 |
| Linux 验收工具自身检查 | 3 PASS：拒绝磁盘优先冒充 PXE、精确 RC41 失败归因、PVE base64 序列号解码；不算装机 PASS |

第一候选的 flows 超时和 events 启动失败原始证据保留在 candidate-1；第二候选重新执行通过，未修改旧结果。braces 修复分支的来源及消费者回归已记录，仍需跟踪其维护风险。

## Debian 导入修复

真实 netinst + 同版 mini 导入曾被标为 ready，但只有 kernel/initrd，缺少内置 preseed 引用的 repo。netinst 还缺 curl 包，因此实际试装改用官方 DVD-1 + 同版 mini，探针通过正常 API 删除。

已提交的补充修复发布主介质仓库、补充 Debian 必需角色，并拒绝旧 ready 记录缺 repo 时创建新任务。最低仓库结构检查从介质发现 suite，要求 Release、匹配架构的 Packages 索引及 pool 包文件，不猜测 codename。新增回归在 VM 上观察到 RED → GREEN，最终完整检查退出码为 0。

补充审查的 4 项 Important 均已处理：上述旧记录与仓库结构检查，以及验收负例核对冻结 hook 的 slot/instance/digest/firstboot/RC41、retry 实际 HTTP422 + task_retry_after_reboot；重复启动要求 net0 → scsi0，并抓取网关 local boot 响应、比较文件系统 UUID。

## 真实 Linux 矩阵

| 系统 | 介质 / 导入 | 实际安装结论 |
| --- | --- | --- |
| Debian 13.7.0 | DVD SHA256 与官方 CD 签名通过；mini SHA256 通过；真实导入 ready，kernel/initrd/repo 齐全 | IN PROGRESS：第三候选正常 cancel/retry 后获得正确 DHCP 地址与网关，自动通过官方镜像校验并下载安装组件；尚未完成磁盘安装与 RC41 验收 |
| Ubuntu 24.04.5 | 实际 4080486400 字节 ISO 整文件 SHA256 与官方签名校验均通过，尚未导入/安装 | NOT RUN |
| openSUSE Leap 15.6 | 部分下载，未完成整文件验证 | NOT RUN |
| openSUSE Leap 16.0 | 官方版本和校验值已固定，未完成下载 | NOT RUN |
| AlmaLinux 9.8 | 部分下载，未完成整文件验证 | NOT RUN |
| Rocky Linux 9.8 | 部分下载，未完成整文件验证 | NOT RUN |

Debian DVD 的实际只读检查确认包含 curl、openssh-server、qemu-guest-agent。全文件 SHA256 为 `347b6c67a3cc0b7ddb60b178f683470c4e2b7ac426c996d9337a2ff36c1a32d2`，官方 CD 签名 key 指纹为 `DF9B9C49EAA9298432589D76DA87E80D6294BE9B`，参照 [Debian 官方校验说明](https://www.debian.org/CD/verify)。没有把 CD 签名验证混同为 APT 仓库签名验证。

Ubuntu 校验文件签名指纹 `843938DF228D22F7B3742BC0D94AA3F0EFE21092` 对照 [Ubuntu 官方说明](https://ubuntu.com/tutorials/how-to-verify-ubuntu)。签名通过不代表尚未下载完整的 ISO 哈希已通过。

## Debian 原生阻塞与已批准修复

1. 第一次原生安装器提示没有默认路由，尚未获取应答文件。保留 serial-attempt1 的脱敏片段，通过正常 API 取消并重试。第二次未改 DHCP 设置；抓包确认来自 192.168.56.2 的 ACK 包含 /24 掩码及默认网关 192.168.56.2。旧候选当时尚未确认原因；第三候选随后抓到安装器选择 PVE 上 192.168.56.1 的租约，PVE 原有 Kea 的 192.168.56.100–200 地址池没有 routers 选项。新增桥接过滤仅匹配出口 tap102i0 的 DHCP 回复，允许 .56.2，拒绝竞争回复，不修改或停止原有 Kea。重新正常 cancel/retry 后，原生 d-i Request/ACK 证明获得 .56.50 和网关 .56.2，过滤计数及 PCAP 已保存。
2. 第二次获取应答后停在镜像 hostname 确认页。从实际 mini initrd 提取的 debconf-set 只调用 db_set，没有设置 seen；当前 early_command 动态写值无法满足无人值守。
3. 实际 HTTP 检查确认 DVD 目录的 InRelease 和 Release.gpg 均为 404。Debian 安装器使用签名 Release 链校验安装源，参见 [Debian SecureApt](https://wiki.debian.org/SecureApt)。完整 ISO 校验通过不会自动补出这些仓库签名。
4. 当前任务通过正常 API 取消，VM102 已停止；原生失败、正常 cancel/retry 事件和定向 DHCP 抓包保留。没有伪造 installed/done，也没有把其他失败当作必需 hook 的 RC41。

建议的局部设计：新增 preseed v2，显式预置可配置的镜像 host/path，默认 Debian 官方签名网络源；保留旧 v1 的冻结内容和严格签名检查。完成回归、dev → test 集成后再试装。若要求必须离线，则需要内部签名仓库路径，不能用 allow_unauthenticated 绕过。

用户已明确选择「采用官方签名网络源」。新增 preseed v2 使用 mirror_host / mirror_path，默认 deb.debian.org/debian，显式预置原生 debconf 参数并强制保留 APT 认证。真实 debconf 读取器的回归在 VM 上观察到 RED → GREEN；官方与内部签名仓库配置均验证 seen=true。历史 v1/v2 发布快照不变，新增 v3 发布快照和 preseed v2 指纹，升级重放检查通过。

产品 / dev 为 `668a512849895ddd00310aca399d0589c9e43cf1`；完整 make check 通过，21 个测试包、race、Linux 构建及 Wire 检查退出码为 0。446 个后端文件（包含实际模板、嵌入内容、测试数据）与已验证 VM 输入完全一致后，按 dev → test 集成到 `09bc87d49b808df830996bdadf98905d84f0ba94`。第三候选已重新执行 131 项企业回归全部通过；真实安装仍在进行，未据此宣称 Linux 或完整 release 通过。

已通过 VM 直接访问官方源，并使用已验签 DVD 内的 Debian 归档密钥验证实际 trixie InRelease。模板参数方式参照 [Debian 安装指南](https://www.debian.org/releases/trixie/amd64/apbs04.en.html#preseed-mirror)。

## 环境与后续

- 用户恢复 PVE 后，确认宿主机 wbt_lat_usec 为原值 75000；测试盘完成 ext4 日志恢复。原有 PostgreSQL、DHCP、nginx 正常。没有再次改变宿主机全局参数，先前失联原因仍未确认。
- 测试 VM101 使用独占新增数据盘；只有 VM102 的 MAC/IP 使用新增 PXE 保留和来源受限 NAT，保留了原 Kea 配置备份。VM100 未改动。
- PVE 的 SMBIOS 字符串需要 base64 标记；已配置并验证解码后的 SN 为 LOBER-LINUX-102。目标只有 32GiB 专用磁盘 LOBER_LINUX_TARGET。
- 原始 serial、口令、票据、应答和私有数据库状态留在受限目录；公共报告只保存脱敏片段及校验值。
- PVE 的临时 `bridge lober_linux_20261018` 表只过滤 VM102 的 DHCP；Linux 阶段结束后执行 `nft delete table bridge lober_linux_20261018` 恢复。它仍是本轮测试所需隔离，尚未移除。
- Ubuntu 实际引导需 8GiB RAM。启动前还需核对 PVE 内存预算，当前未调整其他 VM 的资源。
- 继续完成各 Linux 的实际安装、SSH、root 级凭据清理和重复经网关启动不重装。容量长稳和受支持升级仍是未完成项目；control-complete 不是 release 通过。
- ESXi7/8 与 Windows Server2022 均未启动，等待 Linux 结果汇报后用户另行确认。
