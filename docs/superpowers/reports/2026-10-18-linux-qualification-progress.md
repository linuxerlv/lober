# Linux 装机验收进度（未完成）

更新时间：2026-10-18 21:15，Asia/Shanghai。构建、检查和装机均在指定测试 VM `192.168.137.21` 及独占 PVE VM102 上执行。

**当前结论：第二候选完整后端检查、前端检查及 131 项企业控制面回归通过；Linux 实际安装尚无 PASS。Debian 已进入真实安装器，发现默认 preseed 仍会询问镜像地址，DVD 解包目录还缺 APT 仓库签名。新的 Debian 软件源策略待用户选择，Ubuntu 介质继续获取。**

## 已集成并冻结的产品

- 产品 / dev：`d3014e535f1f2a3366cc726b5f30247bd750c8a9`，完整 VM `make check` 通过后按 dev → test 集成。
- 候选二进制的 test 源码：`8051bc1c50824711e5b7c7537b31fe7aaefcb952`。
- 二进制 SHA256：`79713e6ee6ad70dbf38b7ea13fd6836a4568d3027d89b4d6d81ee45b327ac92b`。
- 原始 test Git archive SHA256：`fe0d7553ab88b13720e8174b1513f9dd37d411dd434973eb20c82a4dedc7542b`。
- 前端 213 个实际生成文件，清单 SHA256：`c43f495a3f9bbcb609e3cbdefb8b6d576f42a6e411ece2b759f510d34783f2d2`。
- `candidate-2/candidate-2.json` 绑定源码、实际资源、二进制、run_id；291 个后端源码/模块文件逐一匹配 VM 已验证输入。
- 验收工具独立从 test `13e129b11b7838f3b1a20415aa723b913dd71676` 冻结，结果分别记录构建 test_sha 和 harness_test_sha。QA 修正没有改写已经完成的产品测试证据。
- 没有 push、main 合并；主工作区已有修改未动。

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
| Debian 13.7.0 | DVD SHA256 与官方 CD 签名通过；mini SHA256 通过；真实导入 ready，kernel/initrd/repo 齐全 | BLOCKED：两次真实 PXE 启动，第二次读取应答进入 installing，但仍询问镜像地址；未完成安装、未到 RC41 场景 |
| Ubuntu 24.04.5 | 单连接限速下载中；官方 SHA256SUMS 签名验证通过，ISO 整文件哈希尚待完成 | NOT RUN |
| openSUSE Leap 15.6 | 部分下载，未完成整文件验证 | NOT RUN |
| openSUSE Leap 16.0 | 官方版本和校验值已固定，未完成下载 | NOT RUN |
| AlmaLinux 9.8 | 部分下载，未完成整文件验证 | NOT RUN |
| Rocky Linux 9.8 | 部分下载，未完成整文件验证 | NOT RUN |

Debian DVD 的实际只读检查确认包含 curl、openssh-server、qemu-guest-agent。全文件 SHA256 为 `347b6c67a3cc0b7ddb60b178f683470c4e2b7ac426c996d9337a2ff36c1a32d2`，官方 CD 签名 key 指纹为 `DF9B9C49EAA9298432589D76DA87E80D6294BE9B`，参照 [Debian 官方校验说明](https://www.debian.org/CD/verify)。没有把 CD 签名验证混同为 APT 仓库签名验证。

Ubuntu 校验文件签名指纹 `843938DF228D22F7B3742BC0D94AA3F0EFE21092` 对照 [Ubuntu 官方说明](https://ubuntu.com/tutorials/how-to-verify-ubuntu)。签名通过不代表尚未下载完整的 ISO 哈希已通过。

## Debian 当前阻塞与待确认方案

1. 第一次原生安装器提示没有默认路由，尚未获取应答文件。保留 serial-attempt1 的脱敏片段，通过正常 API 取消并重试。第二次未改 DHCP 设置；抓包确认来自 192.168.56.2 的 ACK 包含 /24 掩码及默认网关 192.168.56.2。首次原因仍未确认，不称作已修复。
2. 第二次获取应答后停在镜像 hostname 确认页。从实际 mini initrd 提取的 debconf-set 只调用 db_set，没有设置 seen；当前 early_command 动态写值无法满足无人值守。
3. 实际 HTTP 检查确认 DVD 目录的 InRelease 和 Release.gpg 均为 404。Debian 安装器使用签名 Release 链校验安装源，参见 [Debian SecureApt](https://wiki.debian.org/SecureApt)。完整 ISO 校验通过不会自动补出这些仓库签名。
4. 当前任务通过正常 API 取消，VM102 已停止；原生失败、正常 cancel/retry 事件和定向 DHCP 抓包保留。没有伪造 installed/done，也没有把其他失败当作必需 hook 的 RC41。

建议的局部设计：新增 preseed v2，显式预置可配置的镜像 host/path，默认 Debian 官方签名网络源；保留旧 v1 的冻结内容和严格签名检查。完成回归、dev → test 集成后再试装。若要求必须离线，则需要内部签名仓库路径，不能用 allow_unauthenticated 绕过。

此软件源策略选择已向用户提出，尚未据此修改产品代码。

## 环境与后续

- 用户恢复 PVE 后，确认宿主机 wbt_lat_usec 为原值 75000；测试盘完成 ext4 日志恢复。原有 PostgreSQL、DHCP、nginx 正常。没有再次改变宿主机全局参数，先前失联原因仍未确认。
- 测试 VM101 使用独占新增数据盘；只有 VM102 的 MAC/IP 使用新增 PXE 保留和来源受限 NAT，保留了原 Kea 配置备份。VM100 未改动。
- PVE 的 SMBIOS 字符串需要 base64 标记；已配置并验证解码后的 SN 为 LOBER-LINUX-102。目标只有 32GiB 专用磁盘 LOBER_LINUX_TARGET。
- 原始 serial、口令、票据、应答和私有数据库状态留在受限目录；公共报告只保存脱敏片段及校验值。
- Ubuntu 实际引导需 8GiB RAM。启动前还需核对 PVE 内存预算，当前未调整其他 VM 的资源。
- 继续完成各 Linux 的实际安装、SSH、root 级凭据清理和重复经网关启动不重装。容量长稳和受支持升级仍是未完成项目；control-complete 不是 release 通过。
- ESXi7/8 与 Windows Server2022 均未启动，等待 Linux 结果汇报后用户另行确认。
