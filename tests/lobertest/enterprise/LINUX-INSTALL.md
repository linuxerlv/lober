# 实际 Linux 装机（脚本尚待 VM 验证）

`linux_install.py` 不属于 131 项控制面回归，也不发送模拟机器信号或直接写业务表。它操作本轮独占的 PVE VM102，检查 VM 名称、MAC、32GiB 磁盘序列号、SMBIOS 序列号和私有数据库 schema，拒绝其他目标。

本轮环境的私有状态位于 `/home/dev/lober-linux-20261018`。其中 instance.json、instance-private.json、candidate.json、owned-pve-resources.json、pve-session.json、guest-key / .pub 以及 import-SERIES.json 须先准备并校验。不得提交这些私有状态、认证材料、安装应答、serial 原文或数据库备份。

先完成产品验证并经 dev → test 集成，再冻结候选。真实 ISO 必须从发行方获取，校验实际字节并经产品 API 导入；Debian 使用含所需包的 DVD-1 与同版 mini。Leap16 必须从真实介质解析 Agama 产品选择，脚本遇到缺少或不唯一的产品列表会停止，不能手填猜测 ID。

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
