# 实际 Linux 装机验收

## WSL 预检（2026-10-19）

用户允许在 WSL 满足条件时测试，并要求每次只运行一个系统。当前 WSL 的 KVM 实际 vCPU 探测通过，但 Windows 物理内存不足，真实装机尚未启动。详见 [WSL 预检报告](../../../docs/superpowers/reports/2026-10-19-wsl-qualification-preflight.md)。

`wsl_safety.py` 是新增的资源保护工具，尚未接入下述 PVE 验收脚本，也不是 WSL 装机运行器。它提供持有至目标退出的单实例文件锁、独占 32GiB qcow2 校验和重启后的新抓包检查。后续 WSL 运行器必须接入这些保护并验证后，才能启动实际测试机；不能直接在 WSL 运行下面的 PVE start 命令。

内存准入同时要求 Windows 实际可用内存与 WSL MemAvailable。Windows 观察值须在 30 秒内；预留宿主 1536MiB、QEMU 开销 512MiB。因此 2GiB Debian 目标要求 Windows 至少可用 4GiB，WSL 至少可用 3GiB；WSL 的 10GiB 配置上限不代表 Windows 有这么多空闲内存。内存值异常、过期或不足均拒绝。示例只检查内存，不启动或验收系统：

```bash
python3 tests/lobertest/enterprise/wsl_safety.py \
  --host-memory /mnt/e/src/lober/.tmp/linux-qualification/wsl-host-memory.json \
  --guest-memory-mib 2048
```

Windows 观察文件由本轮受控 PowerShell 采样生成，包含 Unix 时间、可用和总物理字节数。不可在采样停止后复用旧值。退出码 0 只表示内存准入，不是环境或安装 PASS。

以下 PVE 流程及其原有资源约束保留。

`linux_install.py` 不属于 131 项控制面回归，也不发送模拟机器信号或直接写业务表。它操作本轮独占的 PVE VM102，检查 VM 名称、MAC、32GiB 磁盘序列号、SMBIOS 序列号和私有数据库 schema，拒绝其他目标。

本轮环境的私有状态位于 `/home/dev/lober-linux-20261018`。其中 instance.json、instance-private.json、candidate.json、owned-pve-resources.json、pve-session.json、guest-key / .pub 以及 import-SERIES.json 须先准备并校验。不得提交这些私有状态、认证材料、安装应答、serial 原文或数据库备份。

先完成产品验证并经 dev → test 集成，再冻结候选。真实 ISO 必须从发行方获取，校验实际字节并经产品 API 导入；Debian 使用含所需包的 DVD-1 与同版 mini。Leap16 必须从真实介质解析 Agama 产品选择，脚本遇到缺少或不唯一的产品列表会停止，不能手填猜测 ID。

Debian 新任务派生自 preseed v2。该版本使用显式镜像参数，默认从 `deb.debian.org/debian` 下载并执行 Debian 原生归档签名校验；安装网络须可访问该源。`mirror_host` / `mirror_path` 可改为已受安装器信任的内部签名仓库。DVD 整体签名通过不代表其解包目录具备 HTTP APT 仓库签名，不能启用 `allow_unauthenticated` 来绕过。旧 preseed v1 的冻结版本保留，不能通过修改旧版本修复已创建的任务。

工具自身的回归测试验证取消状态、精确 hook 失败归因、网络优先引导和序列号识别；这些检查通过不代表任何 Linux 已完成实际安装。真实结果仅以绑定候选源码和二进制的逐系统验收证据为准。

先应用本轮独占网络配置、确认专用制品服务器及机器网关可达、配置已停止的 VM102 的 SMBIOS 序列号 LOBER-LINUX-102。每个命令均在测试 VM 执行：

```bash
python3 tests/lobertest/enterprise/linux_install.py prepare --state-dir /home/dev/lober-linux-20261018 --os debian
python3 tests/lobertest/enterprise/linux_install.py start --state-dir /home/dev/lober-linux-20261018 --os debian
# 同时启动独占目标的受限 serial 捕获，原始输出可能含凭据，不得公开。
python3 tests/lobertest/enterprise/linux_install.py wait --state-dir /home/dev/lober-linux-20261018 --os debian
python3 tests/lobertest/enterprise/linux_install.py qualify --state-dir /home/dev/lober-linux-20261018 --os debian
```

支持的 --os：debian、ubuntu、leap15、leap16、almalinux、rocky。Ubuntu 本轮 ISO 约 4GB，目标须配置 8GiB RAM；其他系统本轮使用 4GiB。顺序复用专用目标磁盘之前须保存前一轮证据并停止目标。不得对其他 VM / 磁盘运行装机。

prepare 经两名不同角色用户审批派生模板和必需 firstboot hook，添加 qemu-guest-agent 包用于 root 级验收，不增加 root SSH 密钥。该 hook 记录运行次数并启动 guest agent；常规 SSH 使用内置账号与公钥链。不能用派生模板的结果替代未经相应配置的其他模板兼容性结论。

只有 qualify 全部通过，result.json 才写 PASS：真实服务器 done、正确发行版/版本、根文件系统来自专用磁盘、SSH 网络可用、必需 hook 一次执行、运行及安装器/Cloud-init 残留凭据检查、再次经网络引导后回到已安装磁盘且不重跑 hook。证据绑定产品/test/binary/harness 哈希与两次 boot_id。TERMINAL 仅表示服务端终态，不能当作装机 PASS。

负例须另跑 prepare/start/wait，添加 --required-failure；验证冻结 hook 的 slot、instance、digest、firstboot 时机和 RC41 报告，以及 retry 的实际 HTTP422 和 task_retry_after_reboot 诊断码。单纯超时不能满足此负例。其结果永不记作正向装机 PASS。负例完成后保留证据，通过正常产品 API 建新的正向任务再重装独占目标，不能强改旧任务状态。

重复启动必须保持 net0 → scsi0 的引导顺序，使用只匹配本目标 IP/机器网关端口的受限 tcpdump 证明实际 PXE 请求及网关 local boot 响应；还比较重启前后的根文件系统 UUID。VM 需要 tcpdump 和免密 sudo。抓包受限保存，不得公开可能含凭据的原文。

当前脚本未在指定 VM 上运行验证。PVE 存储/连接恢复、补充产品修复完整验证、脚本语法与实际 API/guest agent 协议检查均完成后，才能开始用其结果作验收依据。

验收脚本单独从 test 的已提交 Git archive 导出，在脚本旁生成 qualification-source.json，绑定该 archive 的 test SHA、脚本 SHA256、候选 run_id 和二进制 SHA256。该生成文件不含秘密。结果分别记录构建候选的 test_sha 与验收脚本的 harness_test_sha；修改 QA 工具时不能改写已经完成的产品测试证据。PVE 对 SMBIOS 字符串使用 base64 标记时，脚本按标记解码后检查实际 SN。
