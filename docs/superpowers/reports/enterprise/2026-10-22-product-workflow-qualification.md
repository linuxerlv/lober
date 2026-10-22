# SN、任务、模板与主机属性参数：产品流程验收

日期：2026-10-22。状态：三系统经产品 API 安装、任务完成、公钥登录及再次重启验收通过。范围为 x86_64／BIOS；不扩展到 Ubuntu、Rocky、Leap 16 或 UEFI 实装。

## 已确认的规则

用户已确认实施：登记主机 SN、IP、名称、MAC 列表；向导选择已注册 ISO 和装机模板；模板装配多个 hook；任务冻结介质、模板与 hook 版本。iPXE 读取 SMBIOS Type 1 Serial Number，按 SN 领取任务，无任务不进入安装。hook 的动态参数只能绑定登记的主机属性，禁止 fixed/default、可编辑绑定和任务手填覆盖；缺少属性拒绝创建任务。

模板自身的非 hook 参数保留兼容性。hook 可选属性允许显式登记空值，但不能省略属性键；运行时实际必需的 hostname、SSH 用户名已在新内置版本中声明必填。

## 代码与验证

- ISO 读取器优先有效 Rock Ridge：根 SUSP 中必须存在有效、可识别的 ER 声明，支持有界 CE 延续；仅 SP 或其他扩展继续使用 Joliet。保留 Unicode、长文件名和路径安全检查。并发读取不修改文件系统状态。
- hook 参数在模板激活、参数求值和任务入库处检查。控制台 hook 绑定入口只选择主机属性。真实 API 已拒绝手填及空值 entered 覆盖（422），缺少可选属性键报告 param_attr_missing；fixed/default、隐式默认及 editable 绑定无法审批（422）。不合规草稿仍可保存供作者修订。
- Debian 网络名称动作改为 firstboot。短主机名用于内核及 /etc/hostname；完整 FQDN 保留在 /etc/hosts，并可由 hostname -f 读回。隔离 mount/UTS 空间内对短名称、FQDN、超过 64 字节 FQDN 执行两次的实测均通过。
- 新的内置 hook／模板版本及 release v5/v6/v7 追加保存，既有版本指纹和历史 release 文件保留。v5 为本轮先前候选，后续 hostname 必填等审查修正另发新版本；AutoYaST 阶段门槛在 release v7 中发布模板 v5，未覆盖已冻结版本。
- 云端前端 27 文件、95 项测试、vue-tsc 及生产构建通过。hook 属性输入为可搜索的已登记属性选择器，遍历主机分页后去重；不存在自由输入创建属性选项。旧绑定引用缺失属性时提示并阻止提交／审批。hook 详情读取失败时显示诊断及重试，不能将读取失败当作零参数 hook 放行。
- 云端完整 `go test -p 2 ./...` 已通过，包括 PostgreSQL、控制台和架构门禁。旧夹具已改用显式主机属性；架构门禁的临时 Git 克隆补齐实际构建的、被 Git 忽略的 UI 制品，保留禁止缓存污染探针的检查。
- 精确原始 Leap ISO 的 6373 个条目读取通过。扩大的 race 检查曾发现测试辅助器 mutateImportContext.Err 的竞态；仅修复测试辅助器同步后，`go test -race ./internal/iso/iso9660 ./internal/iso -count=1 -v` 两包均通过（1.603s／40.198s），随后完整普通测试再次通过。红／绿日志保留。
- 入库检查同时拒绝重复插槽名称和执行序号，包括空插槽前置／后置遮蔽；对应 PostgreSQL 事务负例验证拒绝后无写入。
- 最终整合源码的完整云端 `go test -p 2 ./...` 退出码为 0；cmd 27.526s、archtest 83.417s、builtin 4.448s、console 204.258s、ISO 30.895s、pgstore 102.992s，其余通过或复用相同输入的 Go 测试缓存。

## 云端运行与真实安装

为保留历史任务和只读制品，新增独立运行库 lober_e2e_round6，制品目录 /var/lib/lober/round6-artifacts。通过 REST API 完成管理员初始改密、创建不同作者／审批人、介质导入、主机登记、模板派生与双人审批、向导建任务。未手改数据库状态或绕过产品导出器复制安装树。

验收时新运行库 API／机器网关使用 8081，制品 HTTP 使用 8082。最终通过后，将同一二进制、签名密钥、配置和验收运行库部署到标准入口 8080，8081 由 nginx 兼容代理至 8080，保证已冻结的引导 URL 可用。原运行库、原二进制和配置保留，另有完整备份 `/home/dev/bin/backups/round6-promote-20261022T044407Z`；未覆盖旧库内容。管理员沿用原测试环境密码，凭据未写入报告。

DHCP 三台预留共用 lober-round6.ipxe，脚本从实际 SMBIOS 读取 serial；MAC 不代填 SN。固定 DHCP 地址对应主机登记的 ip 属性：Debian .111、AlmaLinux .112、Leap .113（192.168.200.0/24）。本轮地址由测试网 DHCP 预留下发，并非宣称内置网络 hook 已提供静态 IP 设置。

原 Leap ISO 已重新导入为 ztmidlolz3levjtp5gpt7tszte。20 份仓库元数据均经 HTTP 读取并核对哈希，primary/filelists 等 143～145 字符的完整文件名均返回 200。

一次中间 Leap 任务 zaiposhdhvd3sn7hxray34le6i 被取消：运行中调整 DHCP 预留后，客户机地址从 .104 变成 .113，而既有下载 TCP 仍绑定 .104，导致下载暂停。串口、地址／连接状态和 nginx 部分传输记录已保留。这是本轮环境变更干扰，不能作为产品安装通过证据。新任务在地址固定后重新启动。

| 系统 | 最终复装任务 | 当前验收状态 |
| --- | --- | --- |
| Debian 13.7 | dimiisfkpajdr54zzwfkkggm5a | done rev16；SSH 公钥登录、名称 lober-debian、IP .111、SN、vdc 根分区及两块非目标盘无签名均通过 |
| AlmaLinux 10.2 | z6644oadxc4um5v7sehefotuha | done rev8；SSH 公钥登录、名称 lober-alma、IP .112、SN、vdc 根分区及两块非目标盘无签名均通过 |
| Leap 15.6 | qbi23njffdyn2cmcduapzie6nm | done rev8；SSH 公钥登录、名称 lober-suse、IP .113／eth0、SMBIOS、vdc2 btrfs 根分区及两块非目标盘零写入均通过 |

Leap 前一任务 ei72pm6wvvf3p7u4syba4syrsu 因登记的 interface=ens1 与真实系统网卡 eth0 不一致而取消；经 API 将主机属性改为 eth0 后重建上述任务。SN 和 MAC 不变。

新增发现：上一任务 snu3paj7atf6yt3jsqmmwu4wze 的 AutoYaST chroot-script 提前启用了通用 firstboot 服务。在 04:44:50 UTC，该服务已发送 booted/init_start/init_done/ready，但 YaST 二阶段仍在刷新仓库，`/run/nologin` 仍存在，sshd 记录 PAM account access denied。因此该轮虽 API done，也未计为安装通过。

新版 AutoYaST v5 在 chroot staging 时写入专用 service drop-in 并删除旧完成标记；原生 init-scripts 标记 YaST 完成后异步排队启动 firstboot。服务还等待 autoyast-initscripts、systemd-user-sessions 和 sshd，并检查不存在 `/run/nologin`。未修改通用 renderer 或历史模板。真实 systemd 测试先复现旧模板提前 ready（RED），修复后顺序为 native → ssh/users → ready；缺标记不运行、有 nologin 仍拒绝运行；完整 builtin 及 PostgreSQL 历史回放通过。阶段契约见 [Leap 15.6 AutoYaST 官方指南 §4.31.5](https://doc.opensuse.org/documentation/leap/autoyast/html/book-autoyast/cha-configuration-installation-options.html#ay-scripts-init)。

新任务 05:11:15 UTC 的真实系统证据：YaST2-Second-Stage 正 activating；firstboot 和 autoyast-initscripts 均 inactive、ConditionResult=no；完成标记不存在、`/run/nologin` 存在；API 保持 rebooting rev4（仅到 installed），没有提前 ready。

另外，测试网访问 cdn.opensuse.org 的 Fastly 地址超时。将测试 DNS 中该名称解析到已实测可达的官方源站 195.135.223.226，源站 repomd 与官方镜像列表列出的 NJU 镜像逐字一致（SHA256 596d1915e2ba94522c742e0a73a9ab5cba9ca1c50b8f60be81a81995d5201095）。旧 hosts 已备份；未修改客户机安装状态、仓库内容或签名策略。新任务中仓库元数据实际下载已恢复。

完成后通过 SSH 读取真实 systemd 单调时间：YaST 二阶段退出 162012389 μs → native init 启动 162183006、退出 162683295 → user-sessions 退出 162723439 → Lober firstboot 启动 162777856、退出 162960989；相关服务 Result 均 success，SSH 已先启动。任务随后 ready，公钥登录通过。独立 RNG 离线校验未运行；实际 YaST 已读取新版 profile、完成两阶段安装。

最终登录时还发现 Kea 默认匹配 Client ID，iPXE、linuxrc 和 Wicked 的不同 Client ID 将同一 MAC 当作不同客户，日志明确显示 `.113` reservation conflict，并临时分配 `.106/.107`。在安装网子网配置 `match-client-id=false` 后通过 Kea 配置校验并重启服务；未改租约文件或客户机配置。三台再次重启后实际 DHCP 和 SSH 地址为 `.111/.112/.113`。这只调整 DHCP 租约识别，Lober 任务身份仍为硬件 SN。参考 [Kea DHCPv4 文档](https://kea.readthedocs.io/en/kea-2.6.2/arm/dhcp4-srv.html)。

## 最终重启、无任务及运行状态

- 三台都在 PXE 优先的原有定义下经 ACPI 重启，再用注入公钥登录。系统版本、名称、地址和 vdc 根分区保持正确，任务仍分别 done rev16／rev8／rev8，没有再次安装。
- `final-no-task.pcap` 按请求与单次响应关联：来自 `.111/.112/.113` 的三条真实 iPXE 请求各带正确 Type 1 serial，均返回 HTTP 200、50 字节退出脚本，无 kernel/initrd 安装指令。另三条 localhost 记录是兼容代理的第二跳，未当成另外三台客户机。
- 标准 8080 和兼容 8081 对三台已完成任务的 SN 及 UNKNOWN SN 均返回相同退出脚本；标准入口实际服务的 UI bootstrap 哈希与构建制品一致。
- 运行中 SMBIOS 直接从三台 KVM 内存解析：Type1.System.SerialNumber 分别为 LOBER-E2E-DEB／ALM／SUS。Debian 和 Alma 另经 guest sudo 读取；Leap 用普通用户登录和独立 hypervisor SMBIOS 证据交叉核对，未为取证放开 root 登录或新增 sudo 权限。
- 三台 vda/vdb 自当前安装及最终重启以来的累计写入均为 0；客体 lsblk 均无这两盘的分区。三台保持运行，Lober、Kea、nginx、PostgreSQL 和 systemd-resolved active；两次抓包服务均已停止并保留证据。
- 已知独立遗留项：Leap 的 kdump-early／kdump 服务失败；读取的 kexec_crash_size=0，内核参数无 crashkernel。未以装机验收代替内核崩溃转储验收，也未手工修改客体掩盖失败。此项不阻塞本轮系统安装、重启和登录，但不能报告系统所有服务健康。

最终整合二进制 SHA256：ef5481b634a2ab21cfb31a38ca63de607922176a47515d68935aa311ed2e75d1；上传的本地源码归档 SHA256：b7d74cc36669d9e9a304ad4830f33adfb002ef9f7d26b9c0f4805a75b2a214e4。云端逐一核对归档中的 496 个源码文件与构建目录完全一致，运行二进制与候选一致。包含云端新构建的 UI bootstrap-_NyruB9d.js。

## 证据索引

- Lober VM：/home/dev/lober-e2e/evidence/round6-install/。包含介质状态、任务／事件、绑定映射、仓库 HTTP／哈希检查、API 负例、源码／二进制部署清单及网关 pcap。
- 宿主机：/home/mgr/lober-e2e/evidence/round5-install/，按时间归档各轮串口；同目录 tN-current.log/png 为最近一次快照。
- 云端测试：full-go-suite.log（首次集成失败日志）、console-fixtures-focused.log、console-fixtures-full.log 等；最终全套通过日志另存。
- 最终汇总：`final-qualification.json`；三台 `*-final-ssh.json`；`leap15-firstboot-order.json`；`final-host-state.json`；`gateway-summary.json`／`final-no-task.pcap`；`canonical-http-validation.json`；`canonical-promotion.json`；`final-source-verification.json`。
- 最终测试：`final-go-suite.log`／`.exit`（0）、`hook-boundary-web-full-green.log`（95 tests）、`hook-boundary-web-types-green.log`、`hook-boundary-web-build-green.log`、`autoyast-gate-{red,green}.log`、`import-context-race-{red,green}.log`，均集中于 VM 的 round6-install 证据目录。
- 凭据与签名材料只保存在服务器受限文件中，不写入本报告。
