# WSL Linux 装机预检与串行测试进度（未完成）

时间：2026-10-19 09:56，Asia/Shanghai。用户允许在 WSL 满足条件时继续测试，要求每次只运行一个系统、避免 OOM。

**09:55 的预检结果：WSL2 的实际 KVM vCPU 和存储探测通过，但 Windows 物理内存余量不足；未启动任何真实操作系统装机目标。当时停止本轮下载、前端依赖安装、Go 检查、代理和独立 PostgreSQL 16，保留进度。**

**09:57 复核更新：本轮负载及会话清理、空闲内存回收后，Windows 实际可用恢复到约 4.4GiB，1/2GiB 目标当时通过内存准入。未主动执行 WSL 重启或修改共享服务配置；相同内核 boot_id 不能证明发行版用户空间未退出，现使用专用会话保持测试期间的发行版运行。下面的低内存、失败和超时记录作为历史保留，不能改记为 PASS。**

## 10:41 更新

截至本次更新，**没有启动 WSL 真实装机目标，没有任何 Linux 实际安装 PASS**。公开增量证据见 [wsl-progress](linux-qualification/20261019/wsl-progress/)。每个重试保留独立日志，旧预检快照未覆盖。

| 范围 | 当前实际结果 |
| --- | --- |
| Debian 介质 | DVD-1 完整 3,992,977,408 字节，SHA256 `347b6c67a3cc0b7ddb60b178f683470c4e2b7ac426c996d9337a2ff36c1a32d2`；官方校验文件的签名通过，签名指纹 `DF9B9C49EAA9298432589D76DA87E80D6294BE9B`。mini 64MiB 的独立 SHA256 通过，未声称其 GPG 签名通过 |
| 前端依赖 | 冻结 lockfile 的 1427 项依赖已安装；串行运行，峰值约 1.6GiB、swap 0，Windows 最低可用约 2.42GiB。真实前端检查及构建尚未执行 |
| 检查器与 KVM 探针 | **45 PASS、0 failed、0 skipped**：门禁 30、原安装检查器 4、WSL 保护 6、作用域 3、真实暂停状态 KVM 探针 2。探针不是操作系统安装 |
| 第二次完整 Go 检查 | **ABORT_RESOURCE，未通过完整门禁**。普通测试全部通过；race 的 console 包通过（625.583 秒），另发现权限测试夹具依赖 umask；随后 Windows 物理内存低于 1536MiB 保留线，保护程序终止本轮自有进程。未完成后续 race/cross/gen，不能把部分通过算作完整通过 |
| 权限测试夹具修正 | 仅修改 test 分支 `cmd/lober/machine_test.go`，显式 chmod 0644 并校验夹具前置条件；产品代码及 dev 未改。相关完整用例在 umask 077 和 022 下分别运行 race、count=1，均通过；测试进程峰值约 352.5MiB、swap 0。完整 Go 门禁仍须重跑 |
| WSL 安装运行器 | 已实现专用 SLIRP 网络、32GiB 文件磁盘、贯穿目标生命周期的单实例锁、宿主实时内存保护及受限 systemd 进程组。监督程序 TERM/KILL 后无运行中 QEMU 的探针通过；独立审查所发现问题已修复。尚未冻结实际装机候选或完成真实运行验收 |

本轮只回收独占目录中的干净文件缓存，没有执行全局 drop_caches、调整全局 WSL 内存或停止共享服务。10:40 Windows 可用约 2.1GiB，仍低于 2GiB Debian 的 4GiB 启动预算，也低于完整 Go 重试的 3GiB、前端构建的 4GiB 预算；后续必须重新检查实时值，不能沿用 09:57 的短暂准入。

10:43 已停止本轮专用 PostgreSQL 16 和代理，数据、介质及日志保留；实际进程检查无运行中的 QEMU，内核日志未发现本轮 OOM 记录。保留轻量宿主采样及维持发行版运行的会话，当前没有装机、构建或浏览器任务运行。尚缺宿主物理内存余量，不能继续启动实际系统；不通过降低保留线或停用共享服务绕过。

Debian 采用 2GiB 目标预算，其余 4GiB 目标要求宿主至少可用 6GiB，8GiB Ubuntu 要求至少 10GiB。准备、构建、浏览器回归和实际装机均分阶段串行，前一个系统停止并保存证据后才能进入下一个系统。真实 Debian 仍按必需 hook RC41 负例、正向安装、SSH/root/凭据清理、重复 PXE 启动的顺序验收。

## 09:55 原始预检（历史记录）

公开证据：[wsl-preflight](linux-qualification/20261019/wsl-preflight/)。该目录的 PASS 仅适用于标明的探测或检查器，不能替代完整产品、Linux 安装或 release 验收。

## 环境与准入

| 项目 | 实际检查 |
| --- | --- |
| WSL 系统 | Ubuntu 26.04.1，WSL2，内核 6.6.114.1；QEMU 10.2.1 |
| KVM | 默认用户可打开 /dev/kvm、创建 VM；另运行真实 QEMU vCPU 探针，QMP query-kvm 返回 enabled=true、present=true 后退出；该探针没有安装操作系统 |
| 存储 | 独占目录 32MiB 写入、fsync、校验、删除探针通过，用时 0.05 秒；WSL VHD 虚拟剩余约 907GiB，不能据此忽略实际 Windows 承载盘空间 |
| 数据库 | 独立 PostgreSQL 16.15 容器、127.0.0.1:15432、256MiB 上限；受限测试角色连库成功，随后停止；既有 PostgreSQL 18 未改 |
| 工具 | Go 1.27.2；独占目录安装 Node 24.22.0（官方 SHA256 已核对）、pnpm 11.16.0 |
| 网络 | Debian 官方校验文件直连成功；Node 直连超时，经本轮独立回环代理完成下载，没有关闭 TLS 校验。尚未验证 WSL 来宾 PXE 网络 |
| 内存 | WSL 上限 10GiB、swap 2GiB，宿主实际约 15.84GiB；准备期间 Windows 可用内存一度不足 1GiB，停止负载后仍仅约 2GiB |
| 真实 Linux 安装 | **NOT RUN**：Debian、Ubuntu、Leap15/16、AlmaLinux、Rocky 均未在 WSL 启动 |

2GiB 的 Debian 目标按本轮保守预算要求 Windows 实际可用至少 4GiB（目标 2GiB + QEMU 开销 512MiB + 宿主保留 1536MiB），并要求 WSL 可用至少 3GiB。即便将 Debian 目标降为 1GiB，也须 Windows 至少可用 3GiB；本次观察仍不足。8GiB Ubuntu 目标要求 Windows 至少可用 10GiB，目前不能启动。

`memory-admission.json` 保存实时 Windows 与 WSL 观察、1/2/4/8GiB 目标的逐项拒绝结果；`host-memory.ndjson` 保存连续观察。Windows 采样须 30 秒内有效，不能把 WSL 虚拟 MemAvailable 当作宿主物理余量。

## 本轮实际测试与限制

本轮使用与第三候选一致的 Git archive：产品 `668a512849895ddd00310aca399d0589c9e43cf1`、test 源码 `09bc87d49b808df830996bdadf98905d84f0ba94`，归档 SHA256 `00a29fa98c38d9ae1372e97d7971257c4cec6b2256e6551beb441fd0fb7f1edc`。本轮没有构建新的交付二进制。

| 范围 | 实际结果 |
| --- | --- |
| 门禁检查器 | 30 PASS，无 skip；仅检查器回归 |
| 原 PVE 安装验收检查器 | 4 PASS，无 skip；没有连接或启动 PVE 目标 |
| 新 WSL 资源保护检查器 | 4 PASS，无 skip；内存异常/不足/过期拒绝、第二实例拒绝、独占 qcow2 及外部数据文件拒绝、新抓包边界检查 |
| 完整 make check 首次运行 | **FAIL，退出码 2**。fmt/vet/基础架构守卫通过，普通测试中 20 个包通过；TestIncrementalGateRejectsPackageInExistingRoot 因复制出的测试目录缺少前端嵌入文件失败；race/cross/gen 未执行 |
| 架构用例定向重试 | 临时 Git index 补入归档中原有的两个占位文件后，在 384MiB/单 CPU 限制下重试；嵌套 Go 测试编译超过 180 秒，**FAIL_TIMEOUT**，不能声称该用例或完整后端已通过 |
| 前端 | 依赖安装部分完成后因宿主内存压力停止；未执行本轮完整检查或真实前端构建 |
| Debian 介质 | WSL 仅下载 DVD 的部分字节，未完成整文件 SHA256 / 签名验证，mini 尚未下载；不得复用 PVE 上整文件已验证的结论作为这份 WSL 文件的验证 |

本轮 38 项检查器回归通过，不是重新执行了原有的 131 项企业控制面套件。第三候选在 PVE 测试 VM 上已完成的 131 项历史结果仍保留在 [原进度报告](2026-10-18-linux-qualification-progress.md)，不能把 WSL 的准备失败改记为产品成功，也不能把历史 PVE 结果称为新的 WSL 执行结果。

此阶段保护代码已独立审查；发现并修复了 NaN 内存值可能误放行、qcow2 嵌套 external data-file 未拒绝的问题，均先观察回归失败再修复通过。当时尚无 WSL 装机运行器，原 `linux_install.py` 是 PVE 专用；后续适配进展见上方更新。

## 继续条件与顺序

1. 释放宿主内存后重新采样，达到目标预算才继续；不通过强占物理内存或停止用户共享服务来获得通过。准备、前端构建、浏览器回归与来宾装机分阶段执行，避免叠加峰值。
2. 单独完成真实前端构建、测试目录准备和完整后端检查，冻结 WSL 二进制及哈希。已有准备错误与超时记录保留。
3. 完成 WSL 本地 QEMU 适配：独占 32GiB 文件磁盘、实际网络优先 PXE、串口和 guest agent、宿主内存持续监测、贯穿目标生命周期的单实例锁。使用独占网络配置，避免与既有 DHCP / Docker 网络竞争。
4. 先 Debian：实际官方介质校验及 API 导入，必需 hook RC41 负例，再正向安装、SSH/root 检查、凭据清理及重复经网关启动不重装。
5. 保存前一系统证据并停止目标，再按内存准入依次执行其余 Linux。任何系统结果未通过都明确保留，不能用后一个系统的结果覆盖。

本轮未修改主工作区、dev 产品代码、WSL 全局配置、原有 SSH/DHCP/NAT 或共享数据库服务。新增本轮资源在 `/home/mgr/lober-wsl-20261019`，凭据和私钥未导出。ESXi 7/8、Windows Server 2022 仍等待 Linux 结果汇报后用户另行确认。
