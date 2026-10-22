# 云服务器 Lober KVM 全链路装机 e2e 报告

日期：2026-10-21（UTC）
状态：`PASS`（Ubuntu 24.04.5，基础设施链路 + 系统级 autoinstall）；产品第七层网关/任务管道未接入，其余系统 `NOT_RUN`

> 2026-10-21/22 追加：产品 `test` 基线（bc3141f，含 `internal/machine` 网关与 `internal/execution` 状态机）已部署并按 REST API 流程完成三系统并发实装复验，发现 3 个产品级缺陷（Debian / AlmaLinux / openSUSE Leap 15.6 均未完成安装），详见「第 8 节 产品 API 全链路复验」。本节及以上段落记录的 Ubuntu 结论仅为基础设施级 e2e，不等于产品任务管道验收。

本报告记录一次在云服务器内 `lober` KVM 上完成的并发装机 e2e：3 台客户 VM 同时经 DHCP → TFTP → HTTP 链路启动 Ubuntu 24.04.5 安装器，无人值守安装并验证落盘到最小容量磁盘。产品代码未修改；本轮的 kea/nginx/tftp 与 seed 配置是测试编排，不是产品实现。

## 1. 环境

| 对象 | 配置 |
| --- | --- |
| 云服务器 | `110.42.44.11`，Ubuntu 24.04，16 vCPU / 31 GiB / 117 GiB |
| Lober KVM | `lober`，4 vCPU / 4 GiB / 50 GiB；`install0`（隔离装机网 192.168.200.0/24）与 `control0`（NAT 控制网 192.168.100.10/24） |
| DHCP | kea-dhcp4 2.4.1，仅监听 `install0`，子网 192.168.200.0/24，`next-server 192.168.200.10`，`boot-file-name boot.ipxe` |
| TFTP | tftpd-hpa，目录 `/srv/tftpboot`，提供 `boot.ipxe`（iPXE 脚本）与 iPXE 引导文件 |
| HTTP | nginx，`/srv/lober/www`：`boot.ipxe`、`ubuntu/vmlinuz`、`ubuntu/initrd`、`seed/{meta-data,user-data,vendor-data}` |
| 数据库 | PostgreSQL 16：`lober`（运行时）与 `lober_test`（测试）两个库/角色 |
| 工具链 | 二进制安装 Go 1.27.1（/usr/local/go）、Node 24.16.0 + pnpm 11.16.0（/usr/local/lib/node24） |
| 代码快照 | 工作区 `codex/layer4-render-integration`（含未提交 render 改动）导入为 VM 内 git 快照 `8170556` |

### 产品门禁基线（在同一 VM 上执行）

| 命令 | 结果 |
| --- | --- |
| `make fmt` | EXIT=0 |
| `make vet` | EXIT=0 |
| `make gen`（wire diff） | EXIT=0 |
| `make cross` | EXIT=0 |
| `make test`（含 archtest、console 150s、pgstore 真库契约 74s） | EXIT=0 |
| `make race` | EXIT=0 |

环境先决：`lober_test` 角色/库、`.pgtest.dsn`、`LOBER_TEST_BASH=/bin/bash`、`LOBER_TEST_BUSYBOX=/usr/bin/busybox`、`gcc`（-race 需 cgo）、`git`（archtest 增量门禁需仓库）、`python`（kickstart Python 解码测试，python-is-python3）。缺少这些时出现的失败均为环境失败，不是产品缺陷。

## 2. 客户 VM 规格与启动方式

| 项目 | 取值 |
| --- | --- |
| 数量 | 3 台并发（`lober-e2e-t1/2/3`，8 GiB × 3 + lober 4 GiB ≤ 31 GiB 宿主内存） |
| 内存/vCPU | 每台 8 GiB / 2 vCPU |
| 磁盘 | 每台 3 块：`vda` 9 GiB、`vdb` 8 GiB、`vdc` 7 GiB（共 24 GiB，按 9/8/7 顺序；目标盘为最小的 `vdc`） |
| 网卡 | e1000（`52:54:00:a1:20:11/12/13`），接 `lober-install` 隔离网 |
| 固件/引导 | SeaBIOS（BIOS PXE，QEMU iPXE option ROM）；`vdc` 显式 `<boot order='1'/>`，网卡 `order='2'` |
| 介质 | Ubuntu 24.04.5 live-server ISO，SHA256 `97f3d7ffb032c3eb3b23d2c8be9cc76e60c2c1f2c0146ba5ba9fe01cafae0fd8`；ISO 以只读 sata cdrom 挂载提供 live rootfs，kernel/initrd/seed 仍走 PXE HTTP 链 |

## 3. 全链路时序（3 台并发，2026-10-21 UTC）

1. 13:43:45 三台同时上电，SeaBIOS → iPXE ROM → DHCP。
2. DHCP 分配 `192.168.200.101/.102/.104`，返回 next-server 与 `boot-file-name=boot.ipxe`。
3. TFTP 下发 `boot.ipxe`（iPXE 脚本），脚本经 HTTP 取回二级脚本。
4. HTTP 下发 `boot.ipxe` → `vmlinuz`（15 MB）→ `initrd`（76 MB），内核参数含 `autoinstall ds=nocloud-net;s=http://192.168.200.10/seed/`。
5. 13:44:21 三台分别取回 `/seed/meta-data`、`/seed/user-data`、`/seed/vendor-data`，均 200。
6. curtin 安装阶段向 `vdc`（7 GiB）写入约 2.93 GiB；`vda`/`vdb` 写入 0。
7. 安装完成自动重启，按显式 boot order 从 `vdc` 引导进入已安装系统。
8. 验证：三台均 `Ubuntu 24.04.5 LTS`（6.8.0-139-generic），`/` = `/dev/vdc2`（ext4，6.8 GiB，已用 2.6 GiB），`vda`/`vdb` 无分区表签名，SSH 密钥登录正常。

## 4. 验证结果

| VM | IP | OS | 根文件系统 | vda (9G) | vdb (8G) | vdc (7G) | 结果 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| lober-e2e-t1 | 192.168.200.101 | Ubuntu 24.04.5 | `/dev/vdc2` | 无分区 | 无分区 | bios_grub + ext4 `/` | PASS |
| lober-e2e-t2 | 192.168.200.102 | Ubuntu 24.04.5 | `/dev/vdc2` | 无分区 | 无分区 | bios_grub + ext4 `/` | PASS |
| lober-e2e-t3 | 192.168.200.104 | Ubuntu 24.04.5 | `/dev/vdc2` | 无分区 | 无分区 | bios_grub + ext4 `/` | PASS |

## 5. 关键发现（本轮定位并验证）

1. **iPXE 传参不能转义 `;`**：内核参数写成 `ds=nocloud-net\;s=...` 时 NoCloud 数据源解析失败，安装器落入交互式界面且不来取 seed（对应 TODO 文档中 "autoinstall 未生效" 的现象）；改为原样 `ds=nocloud-net;s=http://192.168.200.10/seed/` 后立即修复。
2. **autoinstall `identity` 必须含 `password`**：缺少时 subiquity 报 `Malformed autoinstall in 'identity' section` 并停在错误屏（VNC 截图证据）。这是本次第一轮误判的直接原因。
3. **多盘客户机的 BIOS 引导顺序**：安装完成后 SeaBIOS 未落到 `vdc`（前两块空盘在前），导致重启后回到 PXE 反复重装。给目标盘配置 per-device `<boot order>` 后可正常进入系统。该配置属于测试编排/产品部署约束，需在产品侧固定策略。
4. 8 GiB 内存足够本场景：ISO 以本地只读 cdrom 提供 live rootfs，避免 `url=` 全量网络暂存导致的内存压力（旧结论中的 8 GiB 不足针对 ISO 暂存路径）。

## 6. 限制与未覆盖

- 产品第七层网关（任务 token、状态机、机器面回调）未交付，本链路未经过产品任务管道，`lober` 服务仅验证到控制台/API 与门禁级别。
- 仅验证 Ubuntu 24.04.5 + BIOS + x86_64；UEFI、其他发行版与 Windows 均 `NOT_RUN`。
- 客户 VM 登录凭据按项目约定不进入仓库文档，仅保存在测试机受限文件。
- 大日志与截图放在实验机证据目录，不提交仓库。

## 7. 复现要点

1. 在 `lober` VM 启动 kea（仅 `install0`）、tftpd-hpa（`/srv/tftpboot`）、nginx（`/srv/lober/www`），seed 目录含 `meta-data`、`user-data`、空 `vendor-data`。
2. `boot.ipxe`：`kernel ... ip=dhcp console=ttyS0,115200 autoinstall ds=nocloud-net;s=http://192.168.200.10/seed/ ---`，不得转义分号。
3. `user-data` 的 `identity` 必含 `password`（哈希）；`storage.layout.match.size: smallest` 选择最小盘。
4. 客户 VM 三盘 9/8/7 GiB、e1000、BIOS；目标盘设 `boot order 1`、网卡 `order 2`；ISO 以只读 cdrom 挂载。
5. 证据目录：`mgr@110.42.44.11:~/lober-e2e/evidence/`（domain XML、块统计、截图）；`dev@lober VM:~/lober-e2e/evidence/`（nginx/kea/tftp/seed、门禁日志）。

## 8. 产品 API 全链路复验（2026-10-21/22）

### 8.1 部署与驱动方式

- 产品基线：`test` 分支 `bc3141f`（合并 dev `f5c8ee9`，含机器网关 `internal/machine`、执行状态机 `internal/execution`）。二进制在 lober VM 构建并以 systemd 运行，`machine.enabled=true`，`preview.gateway=http://192.168.200.10:8080`、`preview.artifacts=http://192.168.200.10`（nginx 根 = `/var/lib/lober/artifacts`），`machine.syslogListen=0.0.0.0:10514`。
- 全部产品操作经 REST API：管理员改密 → 建 `hook-author`/`hook-approver` 用户 → `POST /isos` 导入介质（本地 path 源，产品负责校验/解包）→ 模板 `derive`→`submit`→`approve`（双人）→ `POST /hosts`（SN 为身份）→ `POST /wizard/params` 预检 → `POST /wizard/tasks`。
- 装机驱动：kea 按 MAC 下发 bootfile `http://192.168.200.10:8080/v1/boot?sn=<SN>&arch=x86_64&firmware=bios`；客户机 SMBIOS serial 设为同 SN。`/v1/boot` 返回产品渲染的 iPXE 菜单（内核/initrd 来自 artifacts，应答来自 `/v1/answer/{task}/{token}/{file}`），安装器信号推进任务状态机。
- 装机网补充：kea 下发 `routers=192.168.200.10` 与 DNS，lober VM 开启转发并对安装网做 NAT（Debian 需访问镜像源）。
- 三台并发：2 vCPU / 8 GiB；磁盘 9+8+7 GiB（目标盘 `vdc`，7 GiB）；e1000；三台均拉取到产品 artifacts 与 token 化应答。

### 8.2 结果

| 系统 | API 流程 | 引导/应答 | 安装结果 | 状态 |
| --- | --- | --- | --- | --- |
| Debian 13.7.0（preseed） | 通过 | DHCP/TFTP/HTTP 正常；d-i 把 DHCP option 67 的网关 boot URL 当 preseed 处理并失败 | 未写盘，落回交互式主菜单 | `FAIL`（产品缺陷 D1） |
| AlmaLinux 10.2（kickstart） | 通过 | 应答 `ks.cfg` HTTP 200 | anaconda 报 “Installation Destination (Kickstart insufficient)” 后终止；三盘均 0 写入 | `FAIL`（产品缺陷 D2） |
| openSUSE Leap 15.6（AutoYaST） | 通过 | 应答 `autoinst.xml` HTTP 200（多次重取） | YaST 报 “Extra element users … profile failed to validate” 后停在警告对话框 | `FAIL`（产品缺陷 D3） |

### 8.3 产品缺陷（含证据）

- **D1 — Debian preseed 与 DHCP filename 冲突**：d-i 提示 “Failed to process the preconfiguration file from `http://192.168.200.10:8080/v1/boot?sn=LOBER-E2E-DEB&...`”，即把 PXE 的 bootfile URL 当作 preseed；实际 `preseed/url` 未被消费（抓包中无 preseed.cfg 请求）。需产品确认 d-i auto 模式下 DHCP filename 回退行为，避免与网关 boot URL 冲突（例如为 Debian 客户端返回可解析的 preseed 或调整 DHCP filename 策略）。
- **D2 — kickstart 分区指令经 `%pre`+`%include` 不生效**：产品渲染（证据 kickstart.ks）为 `%pre` 写 `/tmp/lober-partition.ks` 后 `%include`，anaconda 在解析阶段先处理 `%include`（`%pre` 尚未执行），导致无 `part` 指令、目标盘必填项缺失而退出。需要把 `part` 指令直接内联渲染（或改用 anaconda 支持的方案），并补充真实装机回归。
- **D3 — AutoYaST `<users>` 重复**：渲染的 autoinst.xml 出现两个 `<users>` 元素（模板自带一个 + account 插槽再渲染一个），YaST 校验失败。需要修正 autoyast 模板组装，使 account/sshkey 插槽合并进单一 `<users>`。

### 8.4 证据目录

- 云服务器：`/home/mgr/lober-e2e/evidence/round3/`（三台串口日志、截图、块统计）。
- lober VM：`/home/dev/lober-e2e/evidence/round3/`（gateway-8080.pcap、抽取的 preseed.cfg/autoyast.xml/kickstart.ks、任务与事件 JSON、导入/轮询日志）。

以上三个缺陷修复并回归后，才能重跑本轮三系统并发验收；当前不得记为通过。

### 8.5 D1–D3 根因复核与修复（2026-10-22）

对 round3 证据做了逐包/逐条复核，三个缺陷的真实根因与第 8.3 节的初判有一处重要修正：

- **D1 复核确认（机制实锤）**：pcap 中 `/v1/boot?sn=...` 的请求按 User-Agent 分为两类——iPXE（`iPXE/1.21.1+git-...`，13 次）与 **`debian-installer`（t≈66s、t≈574s 两次）**。即 Debian 安装器在 auto 模式下把 DHCP option 67 的网关 boot URL 当作 preseed 下载（该行为覆盖内核命令行的 `preseed/url`；抓包中无任何 `/v1/answer/.../preseed.cfg` 请求，串口内核命令行确实带有 `preseed/url=`）。它拿到的是 iPXE 菜单（353 字节）或无任务脚本，无法作为 preseed 解析，遂落回交互式主菜单。**修复**：机器网关 `/v1/boot` 按 User-Agent 分流——`debian-installer` 客户端且任务为 preseed 格式时直接返回渲染后的 preseed 应答（与 `/v1/answer` 相同内容），并同步做 `Answer` 状态推进（d-i 不会再请求命令行里的应答地址，状态机若不在此推进会停在 booting）；其余 UA（含空/未知）一律维持原 iPXE 菜单行为，无任务/非 preseed 格式维持“不给安装”。
- **D2 复核确认**：串口日志（serial-t2.log）为 "Failed to save storage configuration" → "Installation Destination (Kickstart insufficient)" → cmdline 模式终止，三盘 0 写入；下发 ks.cfg 中 `part` 指令仅经 `%pre`+`%include` 生成。AlmaLinux 10（pykickstart 3.52.12-1.el10，已从制品内确认）在解析期处理 `%include`（`%pre` 未执行），无 `part` 指令。**修复**：partition hook v2 的 kickstart 片段在渲染期内联 `part` 指令，固件相关引导分区交给 anaconda 原生 `reqpart`（RHEL 10 文档 22.5.6：自动为 BIOS+GPT 建 `biosboot`、为 UEFI 建 `/boot/efi`）。
- **D3 根因修正（重要）**：网关每次 HTTP 响应单体（`--export-objects` 逐响应导出，5 份 autoinst.xml 均 11502 字节）**只含一个 `<profile>`、一个 `<users>`**；此前 23004 字节“双 profile”文件是抽取脚本把多次重取响应拼接的伪影（YaST 对 autoinst.xml 有多次重取）。真实缺陷是：**autoyast account 片段里的 `<create_home config:type="boolean">true</create_home>` 不是 Leap 15.6 AutoYaST schema（yast2-schema-default 4.6.3 的 users.rng）允许的 `<user>` 子元素**。用介质自带的 `profile.rng` 对实际下发字节验证可逐字复现串口报错（"Extra element users in interleave … Element profile failed to validate content"）；消融实验“仅去掉 create_home 行 → 整份真实 profile 校验通过”。**修复**：account hook v2 的 autoyast 片段去掉 `create_home` 行（YaST 默认即创建家目录）。
- **附带发现（环境，非缺陷）**：三台 KVM 实例 SMBIOS serial（`LOBER-E2E-DEB/ALM/SUS`）→ iPXE `sn=` 询问 → 网关任务匹配链路验证通过；无任务 SN 返回 `#!ipxe\nexit || sanboot --drive 0x80`（不给安装、回退本地盘）符合设计。

**修复落盘（分支 `codex/d1-d3-fixes`，基于 test bc3141f）**：

| 缺陷 | 修复位置 | 版本动作 |
| --- | --- | --- |
| D1 | `internal/machine/http.go`（UA 分流 + `installerBoot`）、`internal/execution/service.go`（新增 `Preseed` 按 SN 查 booting/installing 任务）、`internal/machine`/`internal/execution` 新增回归测试 | 无内置内容变更 |
| D2 | `internal/builtin/kickstart/partition.cfg`（reqpart + 内联 part） | partition hook v1→v2；kickstart/preseed 模板 v2→v3，其余五个模板 v1→v2（全部重绑 account/partition v2） |
| D3 | `internal/builtin/autoyast/account.xml`（去掉 create_home） | account hook v1→v2（同上重绑） |

内置内容按产品契约升版本并录制 release v4（`releases/v4.json` + 新身份/版本指纹，v1–v3 夹具与指纹逐字节保留）；answers golden 重录（未变格式字节一致，仅 kickstart/autoyast 变化）；原 `%pre` 分支执行测试替换为“固件无关 + 渲染期 内联 + 无安装期生成”断言。升级部署注意：DB 中引用 account/partition v1 的旧派生模板会被向导按“已过时装载内容”过滤，需从 v4 内置模板重新 derive（旧任务冻结快照不受影响）。
