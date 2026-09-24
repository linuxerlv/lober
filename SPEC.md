# Lober — 裸金属自动化 OS 部署系统设计规格

| | |
|---|---|
| 版本 | 0.4（设计定稿，未实现） |
| 日期 | 2026-09-20 |
| 状态 | 待评审 |
| 目标环境 | 裸金属数据中心，内网完全离线（air-gapped） |

> **0.5 修订**（数据层落地情况，非设计变更）：**数据库已从 §1.1／§11 设想的
> 「SQLite（MVP）→ PostgreSQL」直接落地为 PostgreSQL + GORM**，未经过 SQLite
> 这一中间阶段——§1.1 组件表与 §11 Phase 1 范围表中的「数据库」行按此更新，
> 仅更正与实现不符的技术选型描述，不改变本节所处的设计层论证。
>
> **0.4 修订**（删除 armed，评审时重点看）：**`armed` 状态与 arm / disarm 动作整体删除** —— 建任务即授权，`pending` 任务直接接受引导请求（§3.2）。范围明确为 **`booting` 到 `done`**（§0.1）。`/v1/boot` 查不到任务时返回 `no_task`，不建任务、不记状态、不告警（§4.2）。连带改动：§9.1 四道闸门变三道、§8.2 删两个端点、§12.5 删 `task:arm` 权限码并把确认要求搬到建任务表单、§11 Phase 1 状态机列表、附录 C 换词条。
>
> 评审时请特别看 **§3.2 末段的残留风险**与 **§10 新增的失败模式 2b**：删掉 armed 把防误装全部压到「建任务时选对机器」这一个点上，因此 §4.3 的「SN 从清单选取 + MAC 双因子」由建议升为硬要求。
>
> **0.3 修订**（范围收窄）：**范围起点由「进入 PXE 引导之后」收窄为「iPXE 取得 token 之后」**（§0.1）—— 上电、DHCP、拿到 iPXE 二进制这一段明确划出范围外，其失败不进状态机、不告警、不追踪；§0.2 补显式排除项；§10 标注哪些失败模式属于部署期一次性核对而非运行期检测。
>
> **0.2 修订**（引导层修正）：修正 §1.3 中 option 93 的 x64 UEFI 码值（9 → 7 与 9 双匹配，附录 A.1b）；补 §1.3.1 ISC dhcpd（附 EOL 状态）与 §1.3.2 Kea 两份等价配置；收窄 §1.2「固件路径之后逻辑完全一致」的论断（Windows / ESXi 不成立）；§0.3 与 §2.1 引入固件模式维度；新增 §4.6 两层引导一致性校验与 §4.7 多机异构并发说明。

---

## 0. 范围与边界

### 0.1 系统职责：booting 到 done

**管理范围是状态机的 `booting` 到 `done`。** 起点是「iPXE 请求 `/v1/boot`、网关查到任务并签发 token」这一刻，终点是目标 OS 初始化完成。上电、DHCP、拿到并跑起 iPXE 二进制这一整段是**前置条件**，由运维用既有手段完成（BMC、机房现场、BIOS 引导顺序），本系统既不触发、也不监控、也不为其失败负责。

```
编号   动作                                            归属
─────────────────────────────────────────────────────────────────────────────
 0     安装任务已创建（建任务即授权，无需二次确认）      本系统（准备动作，§3.2）
 1     机器上电 → PXE 反复轮询 → DHCP 给地址 →
       DHCP 指向 iPXE 二进制 → iPXE 跑起来              ★ 范围外
 1.5   iPXE 取 /boot.ipxe（静态一行，全网共用）         边界：本系统提供，但不做判定
 2     iPXE chain 到 /v1/boot
         查到任务 → 签发 token → state = booting        ★ 范围起点
         查不到   → 返回 no_task，什么都不做（§4.2）    ★ 仍在范围外
 3     下发菜单 → 取应答文件 → 安装 → 重启 → 初始化      本系统
 4     ready，交付                                     本系统（终点）
─────────────────────────────────────────────────────────────────────────────

[上电/重启]  →  PXE 轮询 + DHCP + iPXE 起来  →  拿到 token  →  安装  →  重启  →  OS 初始化  →  [交付]
                                             └──── Lober 负责：booting → done ────┘
```

**token 是范围的分界符，也是唯一的执行许可。** 有任务才签发，签发了才能取应答文件、才能装机；查不到任务就不签发，机器也就无从装机 —— 不需要再叠一道人工开关（§3.2）。

> 第 2 步在协议上不是「客户端先认证再拿 token」—— 首次请求必然无认证（机器此刻只有固件，不可能持有凭据），网关按 SN/MAC/UUID 查到任务后**在响应中签发** token，之后所有 URL 才带 token。细节见 §4.2、§4.4。

第 1.5 步的 `/boot.ipxe` 由本系统提供（§8.1），但它是一份全网共用的静态文本、不看机器身份、不做任何决策（§4.7）—— 归属上算边界，真正的起点是第 2 步那次带身份的请求命中了任务。

第 1 步在范围外的理由：它的全部判据都在固件与 DHCP 里，而两者都看不到任务存在（§1.3、§4.7）—— 本系统对这一段既无可观测性也无干预手段。由此四条：

- **PXE 反复轮询是固件的正常行为，不是故障。** 机器在网络引导下拿不到地址、拿不到引导文件、或拿到了但没任务，都会退回固件的引导顺序继续轮询或重启。这是固件设计如此，本系统不干预、不计数、不告警。
- **第 2 步之前的失败不进状态机、不告警、不追踪。** 机器没上电、BIOS 里网络引导没开、option 93 报了个没配的架构、ARM 平台没放二进制 —— 现场症状统一是「机器在 PXE 里转圈或毫无反应」，处置手段是 BMC 看控制台或人到机房，不是在控制台里等出一个失败态。
- **没有任务的机器请求 `/v1/boot`，同样不产生任何记录。** 返回 `no_task` 即止：不建任务、不改状态、不写事件表、不告警（§4.2）。这类请求在数据中心里是常态 —— 任何一台网络引导优先的机器重启都会来一次。
- **本系统仍然提供 DHCP 配置**（§1.3），但那是一次性部署产物，配完不再变动 —— 与「运行期负责机器引导成功」是两件事。§1.3 与 §10 中关于 option 93 码值、无限引导循环、架构不匹配的内容是**部署期一次性核对清单**，不是运行期要系统检测的故障。

反过来，一旦签出 token，责任就全在本系统：一致性校验（§4.6）、菜单下发、应答文件、信号、超时判定、失败分类，都在范围内。**§4.6 的 arch / 固件模式硬拒绝属于范围内**，因为那时任务与机器实际上报值都已在手，能判且必须判。

### 0.2 显式排除项

以下能力**不做**，且是经过权衡的决定，不是待实现项：

| 排除项 | 理由 |
|---|---|
| 带外管理（Redfish/IPMI/iDRAC/iLO） | 自动开关机与强制引导顺序的误操作风险最高：一个错的 SN 配上自动开机即可擦掉生产机。人工上电是安全特性。 |
| **让机器走到 iPXE**（上电、改引导顺序、挂虚拟介质） | 范围起点是 `booting`（§0.1）。这些动作运维在 BMC 里或到机房做，比本系统隔着 DHCP 去推更快也更准。 |
| **第 2 步之前的失败检测、告警与重试** | 判据全在固件与 DHCP 里，本系统零可观测性（§0.1）。做成状态机里的失败态只会产出一个没有诊断信息的 `failed`，而运维真正要看的是 BMC 控制台。 |
| **`armed` 状态 / arm 与 disarm 动作 / 独立的装机前确认步骤** | 建任务本身就是授权动作，事后再要一次点击是重复确认，不增加安全性：两次动作的判据完全相同（这台 SN 是不是我要装的机器），错一次就会错两次。真正的防线是 token 只对有任务的机器签发（§3.2、§9.1）。**注意这排除的是「建完任务之后另起一个环节」，不是建任务表单的提交确认弹窗** —— 后者保留（§12.5）。 |
| **跟踪「没有任务的机器来过」** | 数据中心里任何网络引导优先的机器重启都会撞一次 `/v1/boot`，这是常态噪声。返回 `no_task` 即止，不建任务、不记状态、不告警（§4.2）。需要取证时 nginx access log 已经记下了（§1.4），零成本。 |
| 自研 DHCP 服务 | dnsmasq / Kea 是成熟生产实现，直接复用，由本系统直接配置。**ISC dhcpd 已 EOL（2022-10），仅作既有环境对接支持，不作新部署推荐**（§1.3.1）。 |
| ProxyDHCP 旁挂 | DHCP 由本系统直接配置，不存在「无权修改既有 DHCP」的前提。引入它会多一个服务、多一套引导决策路径、以及两个 DHCP 响应竞争这类难排查的故障。 |
| 自研 TFTP 服务 | dnsmasq 自带，且只用于传输几十 KB 的 iPXE 二进制。 |
| 传输层 TLS（MVP 阶段） | iPXE 走 HTTPS 需重新编译并用 `TRUST=` 嵌入内网 CA，叠加 Secure Boot 签名链后可自行吃掉一个 MVP 周期。见 §1.5。 |
| Go `buildmode=plugin` | 更新已加载插件必须重启进程，且无崩溃隔离，Windows 不支持。见 §7.2。 |
| 机器侧直连内部系统（IPAM/CMDB/DNS） | 全部收敛到服务端 provider。装机 VLAN 上的机器只与网关和 nginx 通信。 |

### 0.3 覆盖的目标 OS

| 家族 | 应答文件机制 | 引导形态 | 固件模式 |
|---|---|---|---|
| RHEL / Rocky / AlmaLinux | kickstart | vmlinuz + initrd + `inst.ks=` | BIOS / UEFI 通用 |
| Ubuntu | autoinstall（cloud-init NoCloud） | vmlinuz + initrd + `ds=nocloud-net;s=` | BIOS / UEFI 通用 |
| Debian | preseed | linux + initrd.gz + `preseed/url=` | BIOS / UEFI 通用 |
| SUSE | AutoYaST | linux + initrd + `autoyast=` | BIOS / UEFI 通用 |
| Windows Server | unattend.xml | wimboot + BCD/boot.sdi/boot.wim | **按固件分别维护 WinPE 制品** |
| ESXi | kickstart（`%firstboot` 原生） | mboot.efi + 渲染后的 boot.cfg | **仅 UEFI**（BIOS 需 syslinux 链，见 §1.2） |

固件模式一列是**制品维护成本**，不是可配置项：Linux 四家一套制品通吃，Windows 要两套 WinPE，ESXi 的 BIOS 路径直接排除在覆盖范围外。网关在下发前校验固件模式与目标 OS 是否兼容（§4.6）。

---

## 1. 技术栈与组件分工

### 1.1 组件清单

| 组件 | 职责 | 是否需要开发 |
|---|---|---|
| iPXE | 网络引导、脚本执行、动态变量（`${serial}` / `${uuid}` / `${net0/mac}`） | 否（离线/Secure Boot 场景需自行编译） |
| DHCP 服务器 | 按架构分发 iPXE 二进制、按 user-class 分发引导脚本 URL | 否（静态配置，一次性） |
| nginx | 制品传输（kernel/initrd/repo/WIM/hook 脚本）、API 反向代理 | 否（配置） |
| Go 网关 | 引导决策、应答文件渲染、信号接收、状态机 | 是 |
| Go 控制台 | 清单、模板、hook、ISO、任务管理，SSE 日志流 | 是 |
| 控制台 WebUI | 人面界面，vue-vben-admin 5.x 的 `apps/web-ele`（Element Plus）精简后内嵌进 Go 二进制 | 是（见 §12） |
| 数据库 | 清单、状态、事件、审计 | PostgreSQL（GORM + AutoMigrate，见 0.5 修订） |

网关与控制台是**同一个二进制的两个 listener**，不是两个进程。理由：状态机必须有唯一所有者，两个进程会立即撞上状态转换竞态与 reconcile 循环重复执行。代码层面按 `internal/gateway` 和 `internal/console` 分包，进程层面不分。

两个 listener 是必须的 —— `/boot.ipxe` 与 `/v1/boot` 天然无认证（裸机在获得身份前无法认证），该风险面需用监听地址与防火墙物理隔开。

```
internal/core/      领域层：清单、状态机、渲染器、provider 接口，不含 HTTP
internal/gateway/   机器面 handler（无认证 + token 授权）
internal/console/   人面 handler（RBAC + 审计）
internal/console/ui/dist/   前端构建产物，go:embed 目标（§12.3）
internal/provider/  provider 接口实现与加载器
cmd/lober/          起两个 listener，套 middleware
web/                前端源码：精简后的 vben-admin 5.x monorepo（§12）
```

### 1.2 引导链

三种固件路径殊途同归，汇合于 iPXE 启动那一刻；**之后网关不再关心机器走的是 TFTP 还是 HTTP Boot，但仍需知道固件模式（BIOS/UEFI）与架构** —— 见下方「固件模式的影响范围」。

```
BIOS PXE        → TFTP 拉 undionly.kpxe  ─┐
UEFI PXE        → TFTP 拉 snponly.efi    ─┼→ iPXE 运行 → chain 到网关 → 装机
UEFI HTTP Boot  → HTTP 拉 ipxe.efi       ─┘
```

**固件模式的影响范围。** 汇合之后并非所有 OS 都固件无关：

| OS 家族 | 固件模式是否影响引导形态 | 说明 |
|---|---|---|
| RHEL / Ubuntu / Debian / SUSE | 否 | iPXE 自带 Linux kernel loader，`kernel` + `initrd` 两条命令在 BIOS 与 UEFI 下写法完全相同 |
| Windows | 是 | `boot.wim` 位宽须与固件匹配，WinPE 制品需按固件模式分别维护 |
| ESXi | 是 | UEFI 用 `mboot.efi`；传统 BIOS 用 syslinux 的 `mboot.c32`，需 iPXE 的 COMBOOT 支持（仅存在于 BIOS 构建，且部分预编译二进制未开启）。**MVP 只支持 UEFI**（§0.3） |

因此 §4.1 首跳脚本中的 `arch=${buildarch}` 与 `plat=${platform}` 不是冗余留痕，而是第二层分流的必要输入，网关据此做一致性校验（§4.6）。

UEFI HTTP Boot 不是绕过 iPXE，而是用 HTTP 把 iPXE 本身载入内存，省掉 TFTP 那段慢传输。固件 HTTP Boot 直接拉 kernel 理论可行但只能取静态 URL，无变量、无脚本、无重试，等于把动态性还给 DHCP，与本设计目标相反。

**MVP 只实现 TFTP 两条路径**（BIOS + UEFI PXE）。HTTP Boot 是纯增量，实测出 TFTP 瓶颈后再加，届时网关代码无需改动。

### 1.3 DHCP 配置（静态，配完永不再动）

DHCP 只需知道两件事：非 iPXE 客户端按架构给对应二进制，已是 iPXE 的客户端给一个固定 HTTP URL。**唯一的耦合点是最后那行 URL。**

```
# 架构识别（option 93）
dhcp-match=set:bios,option:client-arch,0        # X86PC，传统 BIOS
dhcp-match=set:efi32,option:client-arch,6       # IA32 UEFI
dhcp-match=set:efi64,option:client-arch,7       # x64 UEFI —— 实测主流值，见下
dhcp-match=set:efi64,option:client-arch,9       # x64 UEFI 旧值，归同一 tag 兜底
dhcp-match=set:arm64,option:client-arch,11      # ARM64 UEFI（飞腾/鲲鹏）
# user-class 识别（option 77）
dhcp-userclass=set:ipxe,iPXE

# 阶段一：固件 → iPXE 二进制（按架构分流）
dhcp-boot=tag:bios,tag:!ipxe,undionly.kpxe,,<tftp-ip>
dhcp-boot=tag:efi32,tag:!ipxe,snponlyx32.efi,,<tftp-ip>
dhcp-boot=tag:efi64,tag:!ipxe,snponly.efi,,<tftp-ip>
dhcp-boot=tag:arm64,tag:!ipxe,snp-arm64.efi,,<tftp-ip>   # bin-arm64-efi/snp.efi

# 阶段二：iPXE → 网关 HTTP 端点（架构无关，一条通吃）
dhcp-boot=tag:ipxe,http://boot.lober.internal/boot.ipxe
```

**option 93 的 7 与 9 必须都匹配，且 7 才是实测主流值。** RFC 4578 原文将 7 定义为 `EFI BC`、9 定义为 `EFI x86-64`，但该 RFC 的勘误明确指出两者应当互换 —— 实际 UEFI 固件用 7 上报 x86-64，ipxe.org 将 9 标注为 obsolete，Microsoft DHCP 策略与主流实战配置一律按 `Arch:00007` 编写（附录 A.1）。**只配 9 的后果是 UEFI 机器匹配不上任何 tag、拿不到 `filename`、固件静默跳到下一个引导项**，现场表现为「机器毫无反应」，是本节最容易踩且最难查的错误。两个值归到同一 tag 即可，不需要两套制品。

**ARM64 那行 MVP 阶段可以不放二进制，但 tag 建议先留。** 缺失时国产 ARM 平台（附录 B）接入的首个症状同样是「机器毫无反应」；留着 tag 则 TFTP 会留下一条找不到文件的记录，排查成本差一个数量级。

> **本节整体属于部署期一次性核对，不是运行期功能（§0.1）。** 上面这些码值配错的后果都发生在第 1 步 —— 机器拿不到或拿错 iPXE 二进制，根本走不到 `/v1/boot`。本系统对此没有检测手段，也不承担告警责任：排查入口是 DHCP/TFTP 日志与 BMC 控制台。把这些细节写进文档是为了**配置那一次就配对**，不是为了将来在控制台里看它们失败。

UEFI HTTP Boot 的架构码（x64 疑为 16）与 option 60 `HTTPClient` 等做该路径时再加，不是补一行 `dhcp-match` 的事（附录 B）。

两阶段用 option 77 user-class 区分，防止 iPXE 启动后再次拿到 iPXE 二进制导致无限引导循环。Kea 用 client-classes 对 option 77 做 substring test，ISC dhcpd 用 conditional 改 filename/next-server，Microsoft DHCP 用 policy —— 四种实现的具体语法见 [tinkerbell/smee docs/DHCP.md](https://github.com/tinkerbell/smee/blob/main/docs/DHCP.md)（附录 A.2、A.3）。

**BIOS 与 UEFI 必须发不同的链载二进制**，同一实例内按架构分流，没有统一通吃的引导文件（附录 A.1）。

**不使用 ProxyDHCP。** DHCP 服务器由本系统直接配置，`dhcp-boot` / `filename` / `next-server` 直接写在主 DHCP 配置里。ProxyDHCP（option 43 + UDP 4011 旁挂）是为「无权修改既有 DHCP」准备的规避手段，本系统不适用 —— 引入它会多一个服务、多一套引导决策路径、多一类难排查的故障（两个 DHCP 响应竞争）。

**使用域名而非 IP**（dnsmasq 已在管 DNS）。将来签发证书时 CN 能对上，不必回头修改所有 URL 与模板。

### 1.3.1 ISC dhcpd 等价配置

> **ISC dhcpd 已于 2022 年 10 月 EOL**，最后维护版本为 4.4.3-P1 / 4.1-ESV-R16-P2，ISC 官方建议迁移到 Kea（附录 A.12）。新部署应选 dnsmasq 或 Kea；本节供必须对接既有 dhcpd 的场景使用。

dhcpd 无 tag 机制，两层判定靠 `if / elsif` 条件链表达。**条件顺序是硬要求：user-class 判定必须在架构判定之前**，否则已经是 iPXE 的客户端会再次匹配到架构分支、拿到 iPXE 二进制，直接构成无限引导循环（§10 失败模式 3）。

```
# 必须显式声明，dhcpd 无内置 option 93 名称
option client-arch code 93 = unsigned integer 16;

allow booting;
allow bootp;

subnet 10.10.0.0 netmask 255.255.255.0 {
    range 10.10.0.100 10.10.0.200;
    option routers 10.10.0.1;
    option domain-name-servers 10.10.0.1;

    # ── 阶段二优先：已是 iPXE 就给脚本 URL ──
    if exists user-class and option user-class = "iPXE" {
        filename "http://boot.lober.internal/boot.ipxe";
        # 无需 next-server：filename 以 http:// 开头时 iPXE 按 URI scheme 走 HTTP（附录 A.13）

    # ── 阶段一：按 option 93 给架构专属二进制 ──
    } elsif option client-arch = 00:00 {              # X86PC，传统 BIOS
        next-server 10.10.0.1;
        filename "undionly.kpxe";
    } elsif option client-arch = 00:06 {              # IA32 UEFI
        next-server 10.10.0.1;
        filename "snponlyx32.efi";
    } elsif option client-arch = 00:07
         or option client-arch = 00:09 {              # x64 UEFI，7 为实测主流值
        next-server 10.10.0.1;
        filename "snponly.efi";
    } elsif option client-arch = 00:0b {              # ARM64 UEFI
        next-server 10.10.0.1;
        filename "snp-arm64.efi";

    # ── 未知架构：显式不给 filename ──
    } else {
        log(info, concat("lober: unknown client-arch, mac=",
            binary-to-ascii(16, 8, ":", substring(hardware, 1, 6))));
    }
}
```

三个 dhcpd 专属注意点：

1. **未知架构必须落到显式 `else` 并打日志。** 不写 `else` 时客户端拿不到 `filename`、固件静默跳过网络引导，与「DHCP 没响应」现场表现完全一致。这条日志是区分二者的唯一依据。**绝不要在 `else` 里兜底发某个二进制** —— 给错架构的二进制会让固件加载失败或挂死，比不引导更难查（§10 失败模式 4）。
2. **`option client-arch` 声明为 `unsigned integer 16`，但比较字面量写 `00:07` 这种双字节形式**（dhcpd 按原始 option 数据逐字节比较）。这是社区通行写法，实施前需在实验环境用 `dhcpd -t` 与真机各验一遍，写成 `= 7` 的行为不要凭空假设（附录 B）。
3. **Kea 的等价配置见 §1.3.2** —— 它既不是 tag 也不是 if/elsif，机制第三种，不能照抄本节。

### 1.3.2 Kea 等价配置

Kea 用 `client-classes` 表达同一套逻辑，但**机制与前两者都不同，是本节唯一需要特别小心的一点**：

| | dnsmasq | ISC dhcpd | Kea |
|---|---|---|---|
| 机制 | tag 集合 | `if / elsif` 条件链 | `client-classes` 数组 |
| 判定是否互斥 | 否，靠 `tag:!ipxe` 显式排除 | **是**，`elsif` 天然互斥 | **否，所有 class 独立求值** |
| 阶段区分靠什么 | `tag:!ipxe` | 条件顺序 | 需在 test 表达式内显式排除 |

**危险在于「独立求值」**：一台 UEFI 机器跑起 iPXE 后再次发 DISCOVER，它同时满足 `ipxe`（option 77 是 iPXE）和 `efi64`（option 93 是 7）两个 class 的 test。若两个 class 都设了 `boot-file-name`，实际生效的是哪个取决于 Kea 的 class 求值与 option 合并顺序 —— **这条语义本设计不依赖**（未经文档确认，见附录 B）。做法是把 iPXE 的反条件直接 AND 进每个架构 class 的 test，让至多一个 class 能命中，与 dnsmasq 的 `tag:!ipxe` 语义完全对齐：

```json
{
"Dhcp4": {
  "client-classes": [
    {
      "name": "ipxe",
      "test": "substring(option[user-class].hex, 0, 4) == 'iPXE'",
      "boot-file-name": "http://boot.lober.internal/boot.ipxe"
    },
    {
      "name": "bios",
      "test": "option[client-system].hex == 0x0000 and not (substring(option[user-class].hex, 0, 4) == 'iPXE')",
      "next-server": "10.10.0.1",
      "boot-file-name": "undionly.kpxe"
    },
    {
      "name": "efi32",
      "test": "option[client-system].hex == 0x0006 and not (substring(option[user-class].hex, 0, 4) == 'iPXE')",
      "next-server": "10.10.0.1",
      "boot-file-name": "snponlyx32.efi"
    },
    {
      "name": "efi64",
      "test": "(option[client-system].hex == 0x0007 or option[client-system].hex == 0x0009) and not (substring(option[user-class].hex, 0, 4) == 'iPXE')",
      "next-server": "10.10.0.1",
      "boot-file-name": "snponly.efi"
    },
    {
      "name": "arm64",
      "test": "option[client-system].hex == 0x000b and not (substring(option[user-class].hex, 0, 4) == 'iPXE')",
      "next-server": "10.10.0.1",
      "boot-file-name": "snp-arm64.efi"
    }
  ],
  "subnet4": [
    {
      "id": 1,
      "subnet": "10.10.0.0/24",
      "pools": [ { "pool": "10.10.0.100 - 10.10.0.200" } ],
      "option-data": [
        { "name": "routers", "data": "10.10.0.1" },
        { "name": "domain-name-servers", "data": "10.10.0.1" }
      ]
    }
  ]
}
}
```

四个 Kea 专属注意点：

1. **反条件宁可写重复也不要省。** 上面那句 `not (...)` 在四个 class 里各写一遍，看着笨，但它换来的是「配置正确性不依赖 Kea 的 option 合并顺序」。若确认 `member()` 函数可用，可简化成 `not member('ipxe')`，前提是 `ipxe` class 的求值确实先于架构 class 完成（Kea 文档称 class 按配置中定义的顺序处理，故 `ipxe` 需放在数组首位）—— 这条简化**必须实测后再用**（附录 B）。
2. **option 93 在 Kea 里的符号名是 `client-system`，不是 `client-arch`**（后者是 dhcpd 配置里的自定义名）。写 `option[93].hex` 也可以，数值形式与符号形式等价。实测前建议先用 `option[93].hex` —— 符号名对不上时 Kea 直接启动失败，比静默不匹配好查。
3. **无需显式声明 option 93**，Kea 内置标准选项定义，这是与 dhcpd 的一个实质差异（后者必须先 `option client-arch code 93 = ...`）。
4. **未知架构不会命中任何 class，也就拿不到 `boot-file-name`** —— 行为与 §1.3.1 的显式 `else` 等效，但 Kea 不会为此留日志。需要日志时靠 `Dhcp4` 的 `hooks-libraries` 或调高日志级别观察未分类报文，否则「机器毫无反应」这一症状在 Kea 下比 dhcpd 更难定位。

> **`0x0007` 这条有实战踩坑记录**：有用户报告 UEFI 固件明确发 option 93 = `0x0007`，但 Kea 仍回了 legacy BIOS 的引导文件（附录 A.15）—— 与 §1.3 那条错误配置同源。调试第一步永远是抓 DISCOVER 报文看固件实际上报什么，不要相信配置文件里的注释。

**DHCP 保证不了「目标 OS 正确」，只能保证「固件模式正确」。** DHCP 视野里只有 option 93 与 option 77 两个字段，看不到任务要装什么 OS —— 目标 OS 的引导形态由网关在第二层决定（§4.5），两层的一致性由网关校验（§4.6）。三种 DHCP 实现的差异**全部止于本节**：不论用哪一种，第二段拿到的都是同一个 `http://boot.lober.internal/boot.ipxe`，网关对此零感知（§4.7）。

### 1.4 nginx 配置要点

Go 绑 `127.0.0.1`，nginx 作唯一入口。限速、连接数、访问日志、将来的 TLS 全部集中在一处。

**分界线：内容因主机而异的走 Go，对所有主机相同的走 nginx。** kernel / initrd / 镜像 blob / RPM 包 / hook 脚本属于后者；引导脚本与应答文件属于前者。

**制品必须走 `location` 直接发盘，不得 `proxy_pass`。** 否则 4 GB 镜像流经 Go 进程，`sendfile` 零拷贝、`aio threads`、Range 请求全部失效。

```nginx
# ── 机器面：只监听装机 VLAN ──
server {
    listen 10.10.0.1:80;              # 具体 IP，不是 0.0.0.0
    server_name boot.lober.internal;

    location /artifacts/ {
        alias /srv/lober/artifacts/;
        sendfile on;  aio threads;  tcp_nopush on;
        gzip off;                      # iPXE 不处理 Content-Encoding
        add_header Accept-Ranges bytes;   # wimboot / WinPE 依赖 Range
    }
    location /repo/ {                  # 离线仓库：几十万小文件
        alias /srv/lober/repo/;
        open_file_cache max=20000 inactive=5m;
        autoindex on;
    }
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header X-Real-IP $remote_addr;
        client_max_body_size 64m;      # 日志与 inspect 上报
    }
}

# ── 人面：只监听管理网 ──
server {
    listen 192.168.1.10:80;
    server_name console.lober.internal;

    # WebUI 与 API 都在 Go 里（UI 走 go:embed，hash 路由，无需 try_files 兜底）
    location / {
        proxy_pass http://127.0.0.1:8081;
        gzip on;                       # 与机器面相反：前端 JS/CSS 该压
        gzip_types application/javascript text/css application/json;
    }
    location ~ ^/console/v1/tasks/[^/]+/stream$ {   # SSE 必须关缓冲
        proxy_pass http://127.0.0.1:8081;
        proxy_buffering off;  proxy_cache off;
        proxy_read_timeout 3600s;
    }
}
```

必须注意的四点：

1. **`listen` 写具体 IP。** 写成 `0.0.0.0` 时一切功能正常、测试全过，只是装机 VLAN 上的裸机获得了访问控制台 API 的能力 —— 信任域被静默合并。加防火墙规则兜底。
2. **`gzip off`** 对制品与引导脚本都要关，否则 iPXE 会把压缩字节当内核加载。
3. **SSE 关缓冲。** 默认 `proxy_buffering on` 会让装机日志看起来完全冻住。Go 侧同时回 `X-Accel-Buffering: no` 兜底。
4. **`sendfile` + `aio threads` 需 nginx 编译带 `--with-threads`**，`secure_link` 需 `--with-http_secure_link_module`。离线装包前先确认发行版包是否带这两项。

日志格式带上 query args，nginx access log 是 iPXE 启动之后全链路的主要取证记录：

```nginx
log_format boot '$time_iso8601 $remote_addr "$request_uri" $status $body_bytes_sent $request_time';
```

`sn` / `mac` / `uuid` 都在 `$request_uri` 里，grep 单机可得完整引导时间线。制品下载的 `$body_bytes_sent` 与 `$request_time` 能直接看出传输是否中途断开。

### 1.5 传输层：MVP 明文 HTTP（显式决定）

MVP 不上 TLS。省下的不是零碎工作量 —— HTTPS 在这套栈里最贵的部分是 iPXE 需重新编译（带 crypto、用 `TRUST=` 嵌内网 CA），之后每次证书轮换都要重编重发引导文件，叠加 Secure Boot 签名链后风险与工期都显著上升。

为将来上 TLS 只需做到「部署变更而非代码变更」，三件事现在做，成本接近于零：

- 所有 URL 从配置来，scheme 不写死（`gateway_base_url` / `artifact_base_url`），模板中不出现字面 `http://`
- 用域名不用 IP（见 §1.3）
- 首跳脚本不用 `EMBED=` 烧进 iPXE 二进制，让 DHCP 给出网关地址

**代价与缓解**：没有传输层保护后，token 与默认拒绝从「纵深防御的一层」变成唯一的一层（见 §9）。应答文件中的敏感内容处理原则：

- root 不设可复用密码，注入 SSH 公钥并关闭密码登录 —— 截获也换不成持久访问
- `unattend.xml` 不放域管凭据，用最小权限专用加域账号，或把加域挪到装机后另一通道

威胁模型：交换网络上被动嗅探需要端口镜像或装机 VLAN 上已有失陷主机，风险面是内部人员与已失陷主机，不是任意旁观者。**前提是该 VLAN 真正隔离、不路由到办公网。**

### 1.6 反向代理引入的两个陷阱

**`$remote_addr` 变成 `127.0.0.1`。** 这会静默破坏三件事：审计日志记全 loopback、按客户端限速失效、`secure_link` 签名对不上（nginx 校验时用真实客户端 IP，Go 签发时看到 loopback，两边 HMAC 不一致，机器取制品即 403）。

Go 必须从 `X-Real-IP` 取客户端 IP，且**仅当直连 peer 是 loopback 时才信任该头部** —— 网关端点本身无认证，不加此判断则任何人可伪造 `X-Real-IP` 冒充其它机器请求装机脚本。

```go
func clientIP(r *http.Request) string {
    host, _, _ := net.SplitHostPort(r.RemoteAddr)
    if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
        if v := r.Header.Get("X-Real-IP"); v != "" {
            return v          // 只信任 nginx 注入值
        }
    }
    return host               // 其它情况一律用真实 peer
}
```

**启动顺序。** nginx 先起会返回 502。systemd 中给 nginx 加 `After=lober.service`，或接受重启窗口内的 502（iPXE 会重试，实际影响小）。

---

## 2. 数据模型

### 2.1 artifact_set — ISO 导入的产物

**ISO 是导入期制品，不是运行时制品。** iPXE 不引导安装 ISO，它引导从 ISO 中取出的 `vmlinuz` + `initrd`，再用内核参数指向解压后的仓库树。因此解压与抽取在导入时一次完成，装机时零解压、nginx 直接发静态文件。

```
id, name                 rocky-9.4-x86_64
os_family                rhel | debian | ubuntu | suse | windows | esxi
os_version, arch
firmware                 any | uefi | bios          ← 见下
source_iso_sha256        原始 ISO 校验值，留痕
state                    importing | ready | failed | deprecated
manifest_sha256          整个制品集的清单校验值
paths {
  kernel                 rocky9.4/vmlinuz
  initrd[]               rocky9.4/initrd.img
  repo                   rocky9.4/repo/
  extra{}                esxi: mboot.efi / boot.cfg 模板
                         windows: wimboot / BCD / boot.sdi / boot.wim
}
```

**`firmware` 字段承载 §0.3 的固件维度**，取值直接决定选集时的可匹配范围：

| os_family | firmware | 含义 |
|---|---|---|
| rhel / debian / ubuntu / suse | `any` | 一套制品 BIOS 与 UEFI 通吃 |
| windows | `uefi` 或 `bios` | **同一 Windows 版本需导两个制品集**，WinPE 位宽不同 |
| esxi | `uefi` | BIOS 路径 MVP 不覆盖（附录 A.14） |

把固件做成 artifact_set 的一个字段（而不是 `paths.extra` 里的分键）的理由：选集逻辑变成对 `(os_family, os_version, arch, firmware)` 四元组的一次查表，`any` 参与匹配时放宽固件项即可 —— 无需在渲染器里为 Windows 写特例分支。代价是 Windows 的导入操作要做两遍，这个代价是真实存在的，不该藏在数据结构里。

导入是异步 job：解压 → 按特征文件识别 OS 家族 → 抽 kernel/initrd → 建仓库树 → 算 manifest。

| 家族 | 识别特征文件 |
|---|---|
| RHEL 系 | `.treeinfo` |
| Ubuntu | `.disk/info` |
| Debian | `dists/` |
| ESXi | `boot.cfg` + `mboot.efi` |
| Windows | `sources/install.wim` |

**ESXi 特例**：需 `mboot.efi` 加一份 `boot.cfg`，而 `boot.cfg` 中 `prefix=` 必须改写为 HTTP 路径、`kernelopt=ks=` 填应答文件地址、模块路径前的 `/` 要去掉。因其含 per-task 的 ks URL，**这份 boot.cfg 由网关渲染，不放 nginx 静态**。

气隙环境下 ISO **以「登记服务器本地路径」为主**，WebUI 上传为辅 —— 几 GB 走浏览器体验很差，运维 scp 到指定目录再登记更实际。

### 2.2 hook — 只有用户 hook

**hook 表中只存用户 hook。** 系统必需的机器侧动作（遥测、firstboot 脚手架、凭据清理）由渲染器生成，不作为 hook 数据存在 —— 详见 §5.1。因此 order 区间全部归用户，UI 上不存在「系统 hook」这个概念。

```
id, name, description
stage                    pre | post-nochroot | post | firstboot
os_families[]            支持哪些家族
interpreter              bash | powershell | ash(esxi)
body, body_sha256        按 digest 存为 /artifacts/hooks/<sha256>/script.sh
params_schema            声明需要哪些参数 → 驱动动态表单与校验
requires {
  contract_version       ">=1"
  facts[]                要读哪些 fact 键
  capabilities[]         要哪些服务端 provider 能力
}
required, timeout_sec    默认值，模板可覆盖
version, state           draft | review | active | deprecated
author, approver, approved_at
```

**hook 本体是静态制品，参数单独下发。** 脚本按内容寻址放 nginx（可缓存、可校验、可签名），per-machine 参数由渲染器内联（见 §5.4）。

**`requires` 是把运行时意外提前为注册时错误的机制**（见 §7.5）：注册与激活时校验 fact 键是否存在、所需 capability 的 provider 是否已配置、contract_version 是否匹配；模板保存时再校验一遍所有 hook_refs。

附加静态校验：拿 `params_schema` 与脚本正文比对，脚本引用了 `$LOBER_FOO` 但既不在 schema 也不在 fact 命名空间中 → 警告。可拦掉大量拼写错误。

### 2.3 template — 组装单元，必须版本化

```
id, name
artifact_set_id          指定的制品集
answer_template          ks.cfg / user-data / preseed.cfg / unattend.xml 模板文本
hook_refs[] {
  hook_id, hook_digest   pin 住 digest，不是浮动引用
  stage, order, required, params_defaults
}
kernel_args              额外内核参数
disk_policy              first | largest | by-model | raid1-ssd | by-hook
on_failure               reboot | halt | shell | keep-network
required_form_fields[]
version, state
```

`on_failure` 是运维策略而非渲染器该定的事，放模板级配置：调试期用 `shell`（留住机器供人工登录），生产用 `reboot` 加上报。

### 2.4 task

```
id, created_at, created_by
host_sn, host_mac, host_uuid       三个查找键，按 SN → MAC → UUID 优先级回退
template_id, template_version      pin
artifact_set_id                    pin
hook_digests[]                     pin
form_data                          biz_ip / hostname / role / hook 参数
resolved_params                    form_data + provider 决策结果 + 系统生成，渲染上下文
state, state_updated_at, version   version 用于乐观锁 CAS
                                   无 armed 态、无 arm TTL 字段（§3.2）
current_phase, phase_entered_at    分阶段超时判定；pending 无预算（§3.3）
attempt_count                      菜单取用次数，> 3 → failed
last_seq                           信号去重水位
last_signal_at                     失败诊断用
token, token_expires_at            装机 token
finalize_token                     重启后阶段用，TTL 24h
failure_reason, failure_detail
```

### 2.5 task_event — append-only

```
task_id, seq, ts, phase, level, message
rc, hook_digest
source                   signal | syslog | nginx | heartbeat | provider
```

`(task_id, seq)` 唯一索引用于信号去重。

**日志正文不入库** —— 按 `task_id/phase` 落文件，事件表只存路径与尾部片段。数百台机器每台数十 MB 日志入库会很难看。

### 2.6 pin 语义

task 上 pin 住 `template_version` / `artifact_set_id` / `hook_digests`，解决两个具体问题：

1. **正在运行的任务不受改模板影响。** 运维在 A 机装到一半时改了模板，B 机用新版，A 机继续用启动时那版 —— 不 pin 会渲染出半新半旧的应答文件。
2. **可复现。** 数月后排查「这台机器当初装了什么」，靠 pin 能精确还原，不依赖零碎日志。

配套原则：**决策要固化，IO 可以实时。** 决策类 provider（DiskPlanner / TemplateResolver）的结果在建任务时固化进 `resolved_params`，之后不再重算，因此 provider 热更新不会让同一任务前后决策不一致。

### 2.7 引用完整性

- artifact_set / hook 被 active template 引用时只能 deprecate，不能删除
- template 存在非终态 task 时不能删除
- 因 task 已 pin 住内容，deprecate 不影响在跑的任务
- hook 正文不可变，修改即新版本 + 新 digest；模板 pin 的是 digest，故审批过的内容不会被偷换

---

## 3. 任务生命周期与状态机

### 3.1 状态转换

`pending → booting` 这一跳就是 §0.1 的范围起点。`pending` 只是「任务已建、等机器来」，没有 TTL、不需要人工推进，也不产出失败。

```
pending                无 TTL，可长期挂着；运维可 cancel
   │
   │  /v1/boot 命中此任务 + §4.6 校验通过 + 签发 token
   │  ═══════════ 范围起点：booting（§0.1）═══════════
   ▼
booting
   │  取 answer（installer 起来了）
   ▼
installing            pre / packages / post 逐阶段推进
   │  信号 rebooting
   ▼
rebooting             重启窗口
   │  信号 booted
   ▼
initializing
   │  信号 ready
   ▼
done                  ═══════════ 范围终点 ═══════════

任意范围内状态 ──> failed    attempt > 3 | 阶段超时 | required hook 失败
                            | §4.6 一致性校验不过 | 人工取消
```

`pending` 之前与之外的一切都不在状态机里：机器有没有上电、有没有走到 iPXE、有没有在 PXE 里反复轮询，状态机全不知道也不需要知道（§0.1）。**没有任务的机器请求 `/v1/boot` 不创建任何状态** —— 它拿到 `no_task`，状态机里什么都没发生（§4.2）。

### 3.2 建任务即授权 —— 删除 armed 与独立的装机前确认环节

**建任务这个动作本身就是授权，`pending` 任务直接接受引导请求。** 不存在「建好了但还不许装」这个中间态。

早期设计里有一个 `armed` 状态，要运维在装机前再点一次「允许装机」。**它是个假需求，已删除**，理由是：

- **两次确认的判据完全相同。** arm 时能核对的只有 SN、机架位、上次被发现时间 —— 这些在建任务时就已经摆在眼前了。同一个人、同一份信息、看两遍，第一遍选错 SN 的人第二遍照样点确认。它拦不住真正的风险（选错机器），只拦得住「建了任务又后悔」，而那是 cancel 该干的事。
- **TTL 制造协调负担却不换来安全。** `armed` 带 4 小时 TTL，于是「先 arm 再去机房」变成一件有时限的事：路上堵车、机器开机慢、一批机器分两天装，都会撞上 TTL 过期，运维回来发现要重新 arm 一遍。而 TTL 过期本身没有任何安全含义 —— 它只说明「机器还没来」。
- **批量装机下它是纯阻塞。** 一次上架几十台，逐台 arm 是几十次点击，且必须卡在「机器即将上电」这个窗口里做。运维的实际应对会是提前把整批都 arm 掉，于是这道闸门在最需要它的场景下自动退化成空操作。
- **真正的执行许可是 token，不是状态位。** 机器没有 token 就取不到应答文件、装不了机（§0.1、§4.4），而 token 只对查得到任务的机器签发。这道门是协议级的、无法绕过，不依赖人是否点了按钮。

**删掉 arm 之后，防误装全部集中在建任务的正确性上**，所以下面两条从「建议」升级为硬要求（§4.3、§9.1）：

| 要求 | 为什么现在是硬要求 |
|---|---|
| SN 必须从已发现主机清单中选取，**不接受自由文本** | 这是唯一一次人工输入机器身份的机会。自由文本打错字符、恰好撞上某台生产机 SN，那台机器某天网络启动就会被擦盘 |
| **MAC 双因子校验** —— SN 与 MAC 都要对上才下发 | 单靠 SN 不够：白牌机 SN 可能留空、填 `To Be Filled By O.E.M.`、甚至整批重复（§4.3） |

建任务界面需显示机器 SN、机架位、以及**上次被发现时间**，供运维核对是不是目标机 —— 这条要求从原 arm 界面搬到建任务界面，没有取消。

**残留风险要写明白，因为它是这个取舍换来的。** 建好任务后，那台机器一旦网络启动就会开始装机，而本系统不做带外（§0.2），没有远程关机手段可纠正。触发场景包括掉电恢复、他人重启、BIOS 本就网络优先。缓解手段是：建任务与实际装机尽量同期进行；不确定何时装的机器先别建任务；发现建错了立刻 cancel（`pending` 任务 cancel 后即不再响应引导）。**这个风险在有 armed 时同样存在** —— 提前 arm 整批机器是运维的必然选择，arm 只是把风险窗口从「建任务后」推到「arm 后」，没有消除它。

### 3.3 分阶段超时

只设总超时（如 90 分钟）意味着卡在 `pre` 也要等 90 分钟才知道。分阶段预算能在数分钟内报出，且**报出的是具体阶段**。

| 阶段 | 预算 | 超时后 |
|---|---|---|
| pending | **无** | 可长期挂着；机器没来不是失败（§0.1、§3.2） |
| booting | 15 min | 允许重试，attempt++ |
| pre | 10 min | failed |
| packages | 60 min | failed |
| post | 20 min | failed |
| rebooting | 8 min | failed，标记「装好但未起来」 |
| initializing | 15 min | failed，标记「已就绪但初始化未完成」 |

`pending` 无预算是刻意的：那台机器什么时候上电不在本系统控制范围内（§0.1），给它设超时等于替运维的排期做判断。`booting` 及之后每一行都在范围内，因此超时都是有诊断价值的失败 —— 系统知道机器活着、知道卡在哪个阶段、`last_signal_at` 还能给出距今多久（§3.5）。

扫描循环独立于请求路径，每 30 s 扫 `phase_entered_at + budget < now`。**不用 in-process timer** —— 进程重启定时器全丢。

### 3.4 失败态分类 — 哪些绝对不能触发重装

这是本节最重要的一条约束。

| 失败态 | 含义 | 允许重装 |
|---|---|---|
| booting 超时 / attempt > 3 | 已取到 token，但没走到 installer（取 kernel/initrd 或加载失败） | **是** |
| pre / packages / post 失败 | 安装中途失败 | **是** |
| rebooting 超时 | 系统已装好，但起不来（引导项或驱动问题） | **否** |
| initializing 超时或失败 | 系统已就绪，配置未完成 | **否** |

后两者的机器**已经装好且数据可能已在盘上**，重装会擦掉。状态机中必须显式禁止这两种失败态走回 `pending` —— 包括 `retry` 端点（§8.2）对这两种失败态直接拒绝，不是靠 UI 隐藏按钮。删掉 armed 后 `pending` 就意味着「接受引导」，这条禁止比原先更要紧。

`initializing` 失败的正确处理是人工登录 `touch /var/lib/lober/init.pending` 重跑脚本。告警文案要与「装机失败」明确区分 —— 两者处理方式完全不同。

### 3.5 失败事件必须带够信息

`state=failed` 本身无用。失败事件需直接携带：

| 字段 | 用途 |
|---|---|
| `phase` | 哪个阶段 |
| `rc` | 退出码 |
| `hook_digest` | 哪个 hook（若为 hook 失败） |
| `log_tail` | 最后 4 KB，直接可读 |
| `syslog_tail` | installer 自己的原始报错 |
| `last_signal_at` | **距今多久** |

`last_signal_at` 区分度很高：刚收到信号说明机器活着、是脚本或 installer 逻辑错误；很久没信号说明机器层面出事。两个排查方向完全不同。

### 3.6 并发与幂等

- 同一 SN 同时只能有一个非终态 task（partial unique index）
- 状态转换用 `(task_id, expected_state, version)` 做 CAS：
  `UPDATE tasks SET state=?, version=version+1 WHERE id=? AND state=? AND version=?`
- 信号去重靠 `(task_id, seq)` 唯一索引
- **阶段只能前进不能后退**：维护阶段序号表，收到的阶段序号小于当前值则记事件但不改状态 —— 否则延迟到达的 `packages-start` 会把已 `post-done` 的任务打回去

---

## 4. 引导决策

### 4.1 动态性来自两段 HTTP 链，不来自 DHCP

DHCP 给出的 URL 必须是静态的，所以第一个响应是极小的静态引导片，它负责把机器身份塞进第二跳的 query：

```
#!ipxe
chain http://gw/v1/boot?sn=${serial}&mac=${net0/mac}&uuid=${uuid}&arch=${buildarch}&plat=${platform} || shell
```

iPXE 原生暴露 `${serial}`（SMBIOS type 1）、`${uuid}`、`${asset}`、`${manufacturer}`、`${product}`，因此「按 SN/MAC/UUID/资产标签动态下发」所需的键固件全都给全了 —— 网关无需读 DHCP 租约表、无需 ARP 查询、对 DHCP 状态零可见性要求。

> 变量拼写需对照 ipxe.org/cfg 核实。多网卡时 `net0` 未必是引导网卡，需实测。

### 4.2 `/v1/boot` 契约 — 有任务才签 token，没任务返回 no_task

**第一次请求必须无认证，token 在响应中签发。** 机器刚上电只有固件，token 不可能先于首次接触存在。

```
GET /v1/boot?sn=&mac=&uuid=&arch=&plat=        ← 无认证
  → 按 SN → MAC → UUID 优先级查找主机
  → 查该主机是否有 pending 状态的 task

     ├ 无 → no_task：#!ipxe
     │      echo lober: no task for ${serial}
     │      sanboot --no-describe --drive 0x80     ← 回本地盘；无盘可启则固件继续 PXE 轮询
     │      ────────────────────────────────────
     │      不建任务、不改状态、不写 task_event、不告警（§0.1）
     │      唯一留痕是 nginx access log，零成本（§1.4）
     │
     └ 有 → §4.6 三项一致性校验
              不过 → state → failed，返回同上的 sanboot，不消耗 attempt_count
              通过 → 签发 token（token_expires_at = now + 90min）
                     attempt_count++，若 > 3 则置 failed 并返回同上的 sanboot
                     state → booting        ← 范围起点（§0.1）
                     渲染菜单，token 内嵌在后续所有 URL 中
```

**`no_task` 是个决策，不是一个需要被跟踪的事件。** 数据中心里任何网络引导优先的机器重启都会撞一次这个端点，量大且全是噪声 —— 建任务、记状态、发告警都会把噪声变成负担。`sanboot` 是这个决策的 iPXE 表达：盘上有系统就正常启动它，盘上没有系统则 `sanboot` 失败、固件继续走引导顺序、机器留在 PXE 轮询里。两种行为都正确，都不需要本系统再做什么。

> **为什么 `no_task` 的载荷是 `sanboot` 而不是 `exit` 或直接让它循环。** 已装好的机器如果 BIOS 里网络引导优先，每次重启都会来问一次；此时给 `sanboot` 它就去启动自己的系统，给别的它就永远起不来。**BIOS/MBR 模式下没有 `efibootmgr`**，installer 装完无法把本地盘置顶引导顺序，这类机器完全依赖这一行（§10 失败模式 1）。对真正的空盘机器，`sanboot` 自然失败、效果与「继续循环」完全一致 —— 一行 `sanboot` 同时覆盖两种情况，没有取舍。

**没有 token 就没有装机能力，这是协议级的。** 应答文件、信号、facts 全部要求路径里带有效 token（§8.1），拿不到 token 的机器在协议上无从进入安装流程 —— 不需要额外的人工开关来保证这件事（§3.2）。

### 4.3 主机识别

**SN 校验即准入控制**，那个无认证端点是唯一的门。删掉 armed 之后（§3.2）它也是唯一一次人工确认机器身份的机会，因此下面前两条是硬要求，不是建议：

- **SN 不能是表单自由文本**，必须从已发现的机器清单中选取。否则运维打错字符、恰好撞上某台生产机 SN，而那台机器某天网络启动了，就会被擦盘。
- **MAC 必须一并校验，构成双因子** —— SN 与 MAC 都对得上才签发 token。只校 SN 不够：下一条说明了为什么 SN 本身不可信。
- **`${serial}` 不总可靠。** 白牌机与部分 ODM 机器留空、填 `To Be Filled By O.E.M.`、甚至整批重复；SMBIOS UUID 同样有垃圾值。因此查找键按 `SN → MAC → UUID` 回退，且在 discovery 阶段就记录三者实际值，装机时比对。
- **discovery 还须记录 `arch` 与固件模式（BIOS/UEFI）**，否则建任务时无法过滤出该机器可装的 OS 列表，固件不兼容的组合要等到机器实际引导时才被 §4.6 拦下 —— 那时运维已经在机房了。这两项不作为查找键，只作为建任务时的约束与 §4.6 的比对基准。

### 4.4 token

| | |
|---|---|
| 作用 | 阻止非法客户端、避免错装；**删掉 armed 后它是唯一的执行许可**（§3.2） |
| 粒度 | 一主机一 token |
| TTL | 90 分钟 |
| 计时起点 | **首次 `/v1/boot` 匹配成功时**，不是建任务时 |
| 重试 | 换新 token，旧的立即失效 |
| 位置 | **URL 路径段，不用 query string** |

TTL 从实际装机开始算，因为运维可能上午建任务、下午才上电 —— `pending` 无 TTL（§3.3），任务可以挂很久，而 token 的 90 分钟必须是装机窗口本身。

token 走路径段的理由很具体：Ubuntu 的 `ds=nocloud-net;s=<url>` 要求 URL 是目录且结尾带 `/`，query string 会坏掉；Debian `preseed/url=` 中带 `&` 在内核命令行上也容易出事。

**应答文件的暴露控制用状态门控，不用一次性失效** —— 仅当 `state ∈ {booting, installing}` 时提供，靠任务窗口与 attempt 计数限制暴露面。一次性失效会直接掐死正常重试。

### 4.5 各 OS 家族菜单形态

```
# RHEL / Rocky / AlmaLinux
kernel http://ng/artifacts/rocky9.4/vmlinuz \
  inst.repo=http://ng/artifacts/rocky9.4/repo/ \
  inst.ks=http://gw/v1/answer/42/<token>/ks.cfg inst.text ip=dhcp
initrd http://ng/artifacts/rocky9.4/initrd.img

# Ubuntu（注意结尾斜杠，该目录下需有 user-data 与 meta-data）
kernel .../vmlinuz autoinstall ds=nocloud-net;s=http://gw/v1/answer/42/<token>/ ---
initrd .../initrd

# Debian
kernel .../linux auto=true priority=critical \
  preseed/url=http://gw/v1/answer/42/<token>/preseed.cfg
initrd .../initrd.gz

# Windows（wimboot；unattend 由 WinPE 内 startnet.cmd 去取，不烧进 WIM）
kernel http://ng/artifacts/wimboot/wimboot
initrd http://ng/artifacts/winpe2022/BCD       BCD
initrd http://ng/artifacts/winpe2022/boot.sdi  boot.sdi
initrd http://ng/artifacts/winpe2022/boot.wim  boot.wim

# ESXi（boot.cfg 由网关渲染；仅 UEFI，plat != efi 时不下发此菜单）
kernel http://ng/artifacts/esxi8/mboot.efi -c http://gw/v1/answer/42/<token>/boot.cfg
```

> 这些参数拼写需对照各发行版官方文档核实（见附录 B）。

**Windows 与 ESXi 的菜单依赖固件模式，不能只按 `os_family` 渲染**（§1.2）。Windows 的 `boot.wim` 位宽须与固件匹配，故按固件导成两个 artifact_set（§2.1 的 `firmware` 字段）；ESXi 的 `mboot.efi` 是 UEFI 专用，传统 BIOS 需经 syslinux 的 `mboot.c32`（附录 A.14），MVP 不覆盖 —— 网关对 `plat=pcbios` + `os_family=esxi` 的组合直接拒绝下发（§4.6）。

Windows 路线的关键事实（附录 A.4–A.6）：wimboot 通过 HTTP 而非 TFTP 引导 WinPE，千兆网下典型 WinPE 镜像数秒下完；标准流程是 HTTP 引导进 WinPE 后连标准 Windows 文件服务器完成安装；**wimboot 只能引导 WinPE，不能引导完整 Windows**。因此 Windows 的「镜像封装」实质是维护 `install.wim` 加 `unattend.xml`，真正的安装动作由 WinPE 内的 DISM 或 setup.exe 执行 —— 与 Linux 的 installer-driven 是两套机制。

ESXi 路线的关键事实（附录 A.7–A.8）：kickstart 脚本可从 HTTP/HTTPS/FTP/NFS/CD-ROM/USB 获取，安装器自身可走 PXE 或 CD/DVD 引导；脚本位置靠 `ks=filepath` 引导选项指定，不设则脚本化安装无法启动。

### 4.6 两层引导的一致性校验

DHCP 按固件分流（第一层，§1.3），网关按任务分流（第二层，§4.5）。两层各自正确不等于组合正确 —— **建任务时选的 OS 与眼前机器的实际固件可能不匹配，而 DHCP 对此毫无判断能力**（它看不到任务）。校验只能在网关做，且必须做成硬拒绝：

```
/v1/boot 命中 pending task 之后，签发 token 与下发菜单之前：

1. arch 一致性    ${buildarch} 与 artifact_set.arch 必须相符
                  不符 → 拒绝（x86_64 制品发给 arm64 机器，kernel 直接加载失败）
2. 固件模式兼容   查 os_family 的固件要求（§0.3）
                  esxi + pcbios        → 拒绝（MVP 不覆盖 BIOS 路径）
                  windows              → 按 plat 选对应位宽的 WinPE 制品键
                  linux 四家            → 通过，固件无关
3. 制品完整性     paths 中所有文件在盘上真实存在（§10 运维点一）

任一不过：
  → state → failed，失败原因写事件表（区分于「装机失败」，是「不该开始装」）
  → 不签发 token
  → 返回 sanboot（与 no_task 同一形态，§4.2）
  → 不消耗 attempt_count —— 这不是可重试的偶发故障，重试必然同样失败
```

**为什么是 failed 而不是留在 `pending`**：这类不匹配是建任务时的人工错误，重试不会自愈，留在 `pending` 只会让机器在下一次网络启动时再撞一次 —— 而删掉 armed 之后 `pending` 就意味着「接受引导」，机器每转一圈都会再来问一次，噪声无穷。置 `failed` 并把原因显式呈现给运维，是唯一能收敛的处理。

**这是范围内的失败，与 §0.1 说的「范围外不追踪」不冲突**：机器已经取到了 `/v1/boot` 并带来了真实的 `${buildarch}` / `${platform}`，系统手上有任务、有实际上报值、能给出确切原因（「这台机器是 BIOS，任务要装 ESXi」），所以必须记录并呈现。区别在于有没有可诊断的信息，不在于发生的早晚。

**为什么不在建任务时校验就够**：建任务时机器还没上电，`${buildarch}` / `${platform}` 无从得知 —— 库里存的是 discovery 阶段的记录值，而运维可能选错机器（§4.3 的 SN 打错场景）。建任务时校验库内记录值可以提前拦掉大部分错误，是有价值的一道，但**不能替代引导时按固件实际上报值的校验**。两道都要。

### 4.7 多机异构并发 — 六种引导形态如何共存

§0.3 的六个 OS 家族引导形态互不相同，但**引导链上没有任何一处按 OS 配置**。分流点只有三个，且职责严格不重叠：

| 层 | 判据来源 | 分几路 | 配置形态 |
|---|---|---|---|
| DHCP | option 93 / option 77 | 按固件架构 4 路 + 阶段 2 路 | 静态，配完永不再动（§1.3） |
| `/boot.ipxe` | 无 | **1 路，全网共用一份** | 静态文本，机器身份靠 iPXE 变量自带（§4.1） |
| `/v1/boot` | 主机身份 → task → artifact_set | 按 `os_family` N 路 | 动态渲染，加新 OS 只加模板（§4.5） |

**关键在中间那层是 1 路。** 所有机器拿到完全相同的 `/boot.ipxe`，它只做一件事：把 `${serial}` / `${net0/mac}` / `${uuid}` / `${buildarch}` / `${platform}` 拼进 URL 请求下一跳。机器身份由固件自报，网关据此查到**这台机器的那个 task**，再决定给什么。因此「装不同 OS」这件事在引导链上不产生任何分叉 —— 分叉全部收敛到网关内部的一次查表加一次模板渲染。

五台机器同时上电装五种 OS，实际发生的是：

```
机器 A (UEFI, x86_64)  ─┐                        ┌→ task#1 rocky-9.4   → kernel/initrd + inst.ks=
机器 B (UEFI, x86_64)  ─┤                        ├→ task#2 ubuntu-24.04 → kernel/initrd + ds=nocloud-net
机器 C (BIOS, x86_64)  ─┼→ 同一份 DHCP 配置      ├→ task#3 debian-12    → linux/initrd.gz + preseed/url=
机器 D (UEFI, x86_64)  ─┤  同一份 /boot.ipxe  ──→┼→ task#4 win2022-uefi → wimboot + BCD/boot.sdi/boot.wim
机器 E (UEFI, x86_64)  ─┘  （仅 iPXE 二进制      └→ task#5 esxi-8       → mboot.efi -c boot.cfg
                            按 C 的 BIOS 分流）
```

DHCP 只把 C 和其余机器分开（BIOS 给 `undionly.kpxe`，其余给 `snponly.efi`），**它不知道也不需要知道 A 装 Rocky、D 装 Windows**。五种引导形态的差异全部体现在网关渲染出的那几行 iPXE 脚本文本里。

三条由此而来的约束：

1. **加一个新 OS 家族 = 加一个模板 + 导一个制品集，DHCP 与 nginx 零改动。** 这是两段链载设计的主要收益，也是不用 ProxyDHCP、不做多套 DHCP scope 的理由（§1.3）。
2. **一台机器同时只能有一个非终态 task**（§3 的 partial unique index）。这条约束在多 OS 场景下的作用比单 OS 时大得多：没有它，网关查到同一主机的两个 `pending` task 时无法决定渲染哪种引导形态，而两种形态可能是 `wimboot` 与 `mboot.efi` 这种完全不同的东西。删掉 armed 后这条索引承担的责任更重 —— 原先还能靠「只 arm 其中一个」蒙混过关，现在它是唯一的裁决依据。
3. **BIOS 机器不能被分配 ESXi 任务**（§0.3、§4.6）。这个组合的拦截点在网关而非 DHCP —— DHCP 那层还不知道任务存在。建任务时按记录的固件模式过滤可选 OS 列表是更早也更友好的一道拦截，前提是 **discovery 阶段除 SN/MAC/UUID 外还要记录 `arch` 与固件模式**（§4.3 当前只列了前三项，需补）。引导时仍要按实际上报值再校一遍（§4.6）。

---

## 5. 应答文件渲染与 hook

### 5.1 渲染器三段结构 — 系统 hook 不存在

系统必需的机器侧动作**不是 hook**，而是渲染器直接生成的模板文本，与 `%packages` 段同级。用户看不见、不可编辑、不可绕过。

```go
func (r *Renderer) RenderKickstart(t *Task) string {
    return kickstartTemplate(t,
        prologue:  inlineParams(t) + syslogDirective(t),
        preHooks:  t.UserHooks(StagePre),
        postHooks: t.UserHooks(StagePost),
        epilogue:  firstbootScaffold(t) + cleanup(t),
    )
}
```

这样做的收益：

- hook 表中只有用户 hook，order 区间全归用户，无需预留保留区间
- UI 上不存在「系统 hook」概念，用户不会困惑、不会试图删除
- 协议升级只改渲染器代码，不必批量改数据库中的 hook 记录
- 消除一个隐患：系统 hook 作为数据存在时，有权限者可编辑或删除它，会静默废掉整个遥测

**物理约束（不可绕过）**：服务端 provider 跑在服务器上，无法在目标机器上执行代码。以下四件事必须是目标机器上执行的代码，因此只能是渲染器骨架：

| | 为什么服务端做不到 |
|---|---|
| 信号函数注入 | 那是机器本地文件，服务端只能提供内容，写入动作必须机器执行 |
| firstboot 脚手架 | 重启后目标系统里需有 unit、脚本、token、参数 —— 只有 `%post` 能写进目标盘 |
| 凭据清理 | 擦 finalize token 与 `anaconda-ks.cfg` 中的密码哈希 |
| syslog 指向 | kickstart 内的 `logging --host=` 一行 |

### 5.2 hook 阶段与跨家族映射

**阶段不是排序，是能力边界。** 装机过程中有几个能力完全不同的执行环境，hook 放错阶段不是顺序错了，是根本跑不起来。

| 阶段 | 环境 | 能做什么 |
|---|---|---|
| `pre` | installer ramdisk，分区之前 | 看硬件、配 RAID、动态生成分区方案 |
| `post-nochroot` | 目标盘挂在 `/mnt/sysroot`，用宿主工具 | 往目标系统塞文件 |
| `post` | chroot 进目标系统 | 装包、写配置、建用户、`systemctl enable` |
| `firstboot` | 真正启动后的 systemd oneshot | 需完整 boot 才能做的事 |

`%post` 虽已 chroot，但跑的还是 installer 的内核，目标系统 systemd 未启动。因此以下在 `%post` 中全部失败：`systemctl start/restart`（无 running systemd）、`nmcli` / `hostnamectl` / `timedatectl`（需 dbus）、依赖服务运行的初始化（initdb、集群 join、agent 注册）、业务网通信。

反直觉的细节：**`systemctl enable` 在 `%post` 中可用**，因其本质只是建符号链接。这正是 firstboot 机制的基础 —— `%post` 负责埋点，firstboot 负责执行。

跨家族映射（保持一套 hook 库通用的关键）：

| 阶段 | RHEL kickstart | Ubuntu autoinstall | Debian preseed | ESXi ks |
|---|---|---|---|---|
| pre | `%pre` | `early-commands` | `preseed/early_command` | `%pre` |
| post-nochroot | `%post --nochroot` | `late-commands`（curtin 目标） | `preseed/late_command` | — |
| post | `%post` | `late-commands` in-target | `late_command` + chroot | `%post` |
| firstboot | systemd oneshot | cloud-init `runcmd` | systemd oneshot | `%firstboot`（原生） |

Windows 的 firstboot 对应 `%WINDIR%\Setup\Scripts\SetupComplete.cmd`（以 SYSTEM 身份在首次登录前执行）或 `oobeSystem` pass 的 `FirstLogonCommands`。

### 5.3 参数内联，不用机器去取

参数在渲染时直接内联为变量赋值，因此无需 `/v1/params` 端点、无需机器下载 params 文件：

```bash
%pre
LOBER_TASK_ID=42
LOBER_SN=ABC123
LOBER_HOSTNAME=web-01
LOBER_BIZ_IP=10.20.0.15
LOBER_DISK_PRIMARY=/dev/disk/by-id/wwn-0x5000c500...
# 用户 hook 从这里开始
```

### 5.4 fact 命名空间是对外 API

服务端无条件注入一大包事实，不管 hook 用不用。**一旦有数十个 hook 在用就不能改名**，因此需 `LOBER_CONTRACT_VERSION`，hook 声明 `min_contract_version`，服务端只增不删，废弃字段需双写一个版本周期。

```
LOBER_CONTRACT_VERSION=1
LOBER_TASK_ID / SN / MAC / UUID / HOSTNAME
LOBER_OS_FAMILY / OS_VERSION / ARCH
LOBER_DISKS              by-id 列表（inspect 采集）
LOBER_DISK_PRIMARY       服务端决策结果
LOBER_NICS               eth0:aa:bb:cc:...:up,eth1:...:down
LOBER_RAM_MB / CPU_CORES / VENDOR / PRODUCT
LOBER_BIZ_IP / BIZ_GW / BIZ_MASK
LOBER_ARTIFACT_BASE / LOBER_GATEWAY_BASE
LOBER_RACK / ROW / DC
```

宁可多注入，也别让 hook 去发明自己的取数方式 —— 一旦某个 hook 开始 curl 控制台 API 拿数据，耦合就失控了。

### 5.5 facts KV — 吃掉「需要新端点」这类需求的通用原语

一个读写端点同时解决 hook 间通信与结构化上报，跨阶段、跨重启都能传，服务端零改动：

```
POST /v1/facts/{task}/{token}     raid_model=PERC-H755
                                  chosen_disk=/dev/disk/by-id/wwn-0x...
GET  /v1/facts/{task}/{token}     早先 hook 写入的都在这里
```

典型用法：`%pre` 的 hook 探测出 RAID 控制器型号写入，`%post` 的 hook 读出后决定装哪个驱动；firstboot 的 hook 读到 `%pre` 选中的盘。

### 5.6 动态化：先采集，再服务端决策

**不要在模板里硬编码 `/dev/sda` 或 `eth0`。** 正确顺序：

1. `pre` 阶段采集 `lsblk` / `lspci` / `dmidecode` / `ethtool`，上报为 fact
2. 服务端用采集结果决策：按容量/型号/rotational/by-path 选盘，按 MAC/PCI 路径/link 状态选网卡
3. 再渲染应答文件，把具体设备标识写死进去 —— 用 by-id / by-path / WWN，不用内核分配的名字

也可在 `%pre` 中动态计算磁盘，但那把逻辑推到机器上，失败时无日志可看、无法复现。服务端决策可调试、可 dry-run、可审计。

**硬 RAID 需在装机前配好**（本系统不做带外，因此这是前置人工步骤或独立流程），不要指望在 kickstart 中配置。

模板渲染的注入防护：`unattend.xml` 用 `html/template`（获得 XML 转义），kickstart / preseed 用 `text/template` 但对注入值做严格白名单校验。form_data 的值会进 ks 脚本，**必须按类型白名单校验**（IP 格式、hostname 字符集、整数范围）—— 否则填入 `; rm -rf /` 会真的执行。

### 5.7 业务 IP 写配置但不激活

若某个 `%post` hook 把业务网配上并激活，而业务网在另一 VLAN 或另一组 bond 上，机器当场失去到网关的连接 —— 后续信号全部发不出，装机成功但系统显示超时失败。

因此：

- `%post` 只写 `nmconnection` / `netplan` / `ifcfg` 文件，**不执行 `nmcli up`**
- 装机 VLAN 的 DHCP 保持到 `rebooting` 信号发出
- 重启后业务网生效，`booted` 及后续信号走业务网发出

**`booted` 之后的信号源 IP 会变** —— 服务端不得用源 IP 做校验，只认 token。

### 5.8 firstboot 脚手架

`%post` 阶段由渲染器生成：

```bash
%post
mkdir -p /var/lib/lober
install -m 600 /dev/null /var/lib/lober/finalize.token
echo "<finalize_token>" > /var/lib/lober/finalize.token

cat > /etc/systemd/system/lober-init.service <<'EOF'
[Unit]
Description=Lober OS init
After=network-online.target
Wants=network-online.target
ConditionPathExists=/var/lib/lober/init.pending
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/lober-init.sh
StandardOutput=journal+console
[Install]
WantedBy=multi-user.target
EOF

systemctl enable lober-init.service
touch /var/lib/lober/init.pending
%end
```

用 `ConditionPathExists` + 成功后删标记文件保证只跑一次，比脚本末尾 `systemctl disable` 好 —— 脚本中途死掉时标记仍在，可看出未跑完，重启会重试，也可人工 `touch` 重新挂上。

`lober-init.sh` 流程：发 `booted` → 发 `init-start` → 顺序跑用户 firstboot hook → 发 `init-done` → 发 `ready` → 清理凭据 → 删 `init.pending`。

**用户的 firstboot hook 脚本与参数必须在 `%post` 阶段就落进目标系统**，不能等重启后再去 nginx 拉取 —— 重启后业务网可能已生效，装机 VLAN 未必还可达。firstboot 对网络的唯一依赖应只剩「把信号发出去」。

```
/var/lib/lober/
├── init.pending
├── finalize.token       600
├── params.env           600
└── hooks/
    ├── 010-<digest>.sh
    ├── 020-<digest>.sh
    └── manifest         digest + required + timeout + order
```

即使信号发不出，也要把本地工作做完 —— 不能因为报不上状态就放弃配置。

---

## 6. 信号协议与遥测

**一步一个信号，状态机由信号驱动。** 这是机器侧主动推送，比服务端拉取精确 —— 尤其 `rebooting` 信号消除了「装机成功重启」与「装机崩溃」在拉取模式下无法区分的问题。

### 6.1 信号点

只在**状态有意义变化**处发，不是每行都发。

```
installer 阶段
  pre-start          %pre 开始（installer 起来了）
  pre-done           %pre 结束
  packages-start     开始装包
  packages-done      装包结束
  post-start         %post 开始
  post-done          %post 结束
  rebooting      ★   即将重启

目标 OS 阶段
  booted         ★   重启成功，系统起来了
  init-start         OS 初始化开始
  init-done          初始化完成
  ready              全部完成

逐 hook
  hook-report        每个用户 hook 的 rc + 日志尾部
```

`rebooting` 与 `booted` 配成一对，中间的空窗即明确的重启窗口：收到 `rebooting` 后启动 8 分钟计时，超时未收到 `booted` 即「重启失败」—— 独立失败态，**不可重装**（§3.4）。

另有几个阶段由请求本身隐含，无需额外代码：`/v1/boot` 被取 = 开始引导，`/v1/answer` 被取 = installer 起来了，nginx 上 kernel/initrd 被取 = 引导成功。

### 6.2 协议 — 极简零依赖

installer ramdisk 中没有 python、没有 jq，`curl` 也可能只有 busybox 版。因此**结构化信息全走 URL 路径与 query，body 只放纯文本日志**：

```
POST /v1/signal/{task}/{token}/{phase}?rc=0&seq=7
     Content-Type: text/plain
     body: 最近 4 KB 日志
```

一行 `curl --data-binary @file` 即可，服务端解析零成本。用 JSON 需在 shell 中手工拼转义，日志带引号或换行就会坏掉。

### 6.3 客户端封装（渲染器注入）

```bash
SEQ=0
signal() {
  local phase=$1 rc=${2:-0} log=${3:-/dev/null}
  SEQ=$((SEQ+1))
  for i in 1 2 3; do
    curl -fsS -m 10 -X POST \
      "$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ" \
      -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
    sleep $((i*3))
  done
  logger -t lober "signal failed: $phase rc=$rc"   # 落 syslog 兜底
  return 0    # ★ 上报失败绝不中断装机
}

run_hook() {
  local digest=$1 required=$2
  curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
  echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
  ( set -euo pipefail; timeout "$HOOK_TIMEOUT" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
  local rc=${PIPESTATUS[0]}
  signal "hook-report" "$rc" /tmp/h.log
  [ "$rc" -eq 0 ] || [ "$required" != "true" ]
}
```

最后那个 `return 0` 是刻意的：**遥测失败不能杀装机。** 服务器临时不可达而把正在装的机器搞挂，是最没必要的失败。上报失败退化为 syslog，装机继续。

hook 失败语义：`required: true` 失败即中止装机并上报 failed；`false` 记日志继续。

### 6.4 token 跨重启

装机 token TTL 90 分钟从引导算起。若装机用了 70 分钟、重启又停在 BIOS 数分钟，`booted` 发出时 token 可能刚好过期。

因此 `%post` 阶段写入**独立的 finalize token**（TTL 24 h，权限只够发 `booted` / `init-*` / `ready` 与 hook-report）。装机 token 保持紧，重启后阶段有自己的凭据，两者互不牵连。

### 6.5 辅助通道

三条通道互为冗余 —— 单一通道在某个阶段一定会瞎。

| 通道 | 内容 | 不可替代性 |
|---|---|---|
| 信号（主） | 阶段边界、hook rc、错误详情 | 最可靠、结构化 |
| syslog（UDP 514） | installer 自己的全量日志 | **当 anaconda 炸在依赖解析、磁盘写坏、仓库 404 时，你的 hook 一行都没跑到，信号完全静默，而 syslog 里有确切错误行** |
| nginx access log | 请求流即进展 | 零成本；但对镜像式安装（Ubuntu curtin、Windows DISM）很弱 —— 大文件下完后的长时间静默是正常的 |

kickstart 中一行即开 syslog：`logging --host=10.10.0.1 --port=514 --level=info`。Ubuntu/Debian 在 `early-commands` 配 rsyslog forward，ESXi 用 `esxcli system syslog config set --loghost=`。

**syslog 是 UDP 514，不经过 nginx** —— Go 直接监听，或本地 rsyslog 收后转发。注意防火墙。

### 6.6 心跳插件 — 辅助通道，非主通道

机器主动推送后，拉取不再承担状态推进，只作为「推送断了」时的兜底判断（机器是还活着还是真死了）。

```go
type HeartbeatProvider interface {
    Probe(ctx context.Context, targets []Target) []ProbeResult
}
```

内置实现两种（Redfish 探测按 §0.2 不做，但接口保留，将来可由插件接入）：

| 探测器 | 目标地址来源 | 用于阶段 |
|---|---|---|
| `icmp` | `/v1/boot` 请求的 `X-Real-IP` | installing |
| `tcp` | 表单中的业务 IP + 指定端口 | initializing |

拉取范围：`state ∈ {booting, installing, rebooting, initializing}` 且已知可探测地址。

**拉取的本质局限**：ICMP/TCP 在你最关心的故障里全是正常的 —— anaconda 卡在依赖解析、hook 死循环、仓库 404 重试时，内核活着、网络通着。因此拉取能完整替代存活判断，替代不了进展判断。进展靠信号 + syslog。

若使用业务 IP 探测 `ready`，需在建任务时校验**业务 IP 对服务端可达**。若业务网与管理网不通，探测永远失败、任务永远卡在 `initializing`。表单校验时即告知，不要等装完才发现；或允许配置「不探测，靠人工确认完成」。

---

## 7. Provider 插件体系

### 7.1 接口清单

**不做通用插件，做窄接口的 provider。** 接口窄且稳才能版本化；定义成「插件可以做任何事」则一年后没人敢改。每个接口独立版本化（`IPAMProviderV1`），provider 声明实现哪一版。

```go
type IPAMProvider interface {
    Allocate(ctx, req AllocRequest) (AllocResult, error)
    Release(ctx, ip string) error
}
type DNSProvider interface {
    Register(ctx, fqdn, ip string) error
    Deregister(ctx, fqdn string) error
}
type InventoryProvider interface {
    LookupBySN(ctx, sn string) (*Host, error)
}
type NotifyProvider interface {
    Emit(ctx, ev Event) error
}
type HeartbeatProvider interface {
    Probe(ctx, targets []Target) []ProbeResult
}
type TemplateResolver interface {          // 纯决策，无 IO
    Resolve(ctx, form FormData, facts HWFacts) (TemplateRef, error)
}
type DiskPlanner interface {               // 纯决策，无 IO
    Plan(ctx, disks []Disk, intent DiskIntent) (Layout, error)
}
```

### 7.2 加载器：三种，同一套接口

```yaml
providers:
  ipam:   { type: grpc, path: /etc/lober/plugins/ipam/current }
  dns:    { type: exec, command: /etc/lober/providers/dns.sh, timeout: 10s }
  notify: { type: http, url: http://alert.internal/api/v1/events }
```

| 类型 | 形态 | 加新的 | 改已有的 | 崩溃隔离 |
|---|---|---|---|---|
| `exec` / `http` | 子进程 / webhook，JSON 进 JSON 出 | 不重启 | **不重启**（下次调用即生效） | 完整 |
| `grpc`（hashicorp/go-plugin） | 独立子进程 + gRPC | 不重启 | **不重启**（杀子进程重拉） | 完整 |
| `starlark`（可选，Phase 3） | 嵌入式脚本，纯决策 | 不重启 | 不重启 | 完整（默认无 IO） |

**Go `buildmode=plugin` 明确排除。** 它要求主程序与插件编译期完全对齐（同一 Go 版本含补丁号、所有共享依赖同版本、同构建标志），且：无 `Close()` 无 unload、同路径重复 `Open` 返回缓存对象（改文件无效）、**更新已加载插件必须重启进程**、panic 直接带走网关、**Windows 完全不支持**（本地开发环境即过不去）。它同时是唯一需要重启且唯一无崩溃隔离的方案。

选型判据：

- **外部系统集成** → `exec` / `http`。气隙环境下改 shell 脚本是 30 秒的事，改 Go 插件要在外网编译、打包、走审批、传入内网。DNS 注册、告警推送这类几行 curl 的集成用 gRPC 是纯负担。
- **有真逻辑的集成**（IPAM、CMDB） → `grpc`。
- **纯决策**（DiskPlanner / TemplateResolver） → 内置策略（MVP），后续按需上 Starlark。选 Starlark 而非 JS 的理由是确定性：这套系统会擦盘，磁盘规划函数若行为不完全确定则故障无法复现。Starlark 默认无 IO、无时钟、无随机、值冻结。

### 7.3 热更新要求（grpc 类型）

| 要求 | 做法 |
|---|---|
| 换二进制原子 | `plugins/ipam/v2/ipam` + `current` 符号链接，reload 时切链接。不用 `cp` 覆盖 —— 运行中进程可能读到半个文件 |
| in-flight drain | 停止新请求进入 → 等在途调用结束（带超时）→ 再 kill。用带计数的 gate 包住调用点 |
| 健康检查与回滚 | 新插件拉起后先探活（接口留 `Ping()`），失败则保留旧进程、拒绝本次 reload |
| 崩溃自动重启 + 熔断 | 指数退避，连续失败 N 次标记 provider 不可用，再按 §7.4 的失败语义处理 |
| 协议版本协商 | `HandshakeConfig.ProtocolVersion` 对不上直接拒绝加载 |

### 7.4 失败语义按扩展点分别定义

统一成「失败即中止」会让一次 DNS 抖动把装好的机器标成失败；统一成「失败即忽略」会让 IP 没申请到的机器带着空配置装上去。

| provider | 调用时机 | 失败时 |
|---|---|---|
| IPAM | 建任务 | **阻塞建任务** —— IP 没申请到不能装 |
| TemplateResolver / DiskPlanner | 建任务 | **阻塞** —— 决策出不来无法继续 |
| Inventory | 建任务 / 清单同步 | 阻塞建任务，但允许手工录入兜底 |
| DNS / Notify | `ready` 之后 | **只告警**，不影响任务状态 |
| Heartbeat | 周期轮询 | 记录探测失败，不改任务状态 |

### 7.5 hook 与 provider 的耦合分层

hook 的能力需求分六层，设计目标是**把 3/4/5 层压到 0/1/2 层**：

| 层 | hook 需要什么 | 服务端改动 |
|---|---|---|
| 0 | 只做本地事 | 无 |
| 1 | 新的用户输入参数 | 无 —— `params_schema` 让表单自己长 |
| 2 | 服务端已有的数据 | 无 —— 从 fact 命名空间取 |
| 3 | 服务端没有的数据（IPAM 取 IP、命名服务取主机名） | provider |
| 4 | 服务端做事（写 DNS、注册 CMDB、通知监控） | provider |
| 5 | 新协议（结构化上报、多机协同） | facts KV 已吃掉大部分 |

第 3/4 层的正确解法是**服务端生命周期 hook**（不是给每个 hook 加专属端点，那是通往一百个 `/v1/hook/xxx` 的路）：

```
task.created     → 申请 IP、分配主机名，结果写回 resolved_params
phase.changed    → 推送进度到外部系统
task.ready       → 写 DNS、注册 CMDB、加监控
task.failed      → 告警、开工单
```

于是「hook 需要 IPAM」被拆成两件解耦的事：provider 在 `task.created` 时申请好 IP 写进 params，机器侧 hook 只消费 `LOBER_BIZ_IP` —— 退回第 1 层。

**判断规则**：hook 只消费内联参数与 facts KV 即为纯用户态；一旦需要服务器去外部系统取数或做事，就是第 3/4 层，做成 provider，让机器侧 hook 退回纯消费者。

搬走的额外收益：机器侧不再需要访问 IPAM/CMDB/DNS，装机 VLAN 上的机器只与网关和 nginx 通信，网络策略干净得多。

### 7.6 MVP 不做插件系统

在 Go 中把接口定义出来，实现内置版本（`ipam: static` 从表单直接取、`dns: noop`、`resolver: rule-based` 规则表、`diskplanner` 具名策略），通过配置选择。

**接口边界是真正有价值的部分，加载机制可以后补** —— 只要接口对，后续加 exec / grpc / starlark 三种加载器都不影响已有代码。过早做插件体系是典型过度设计：会花两周做加载、版本协商、隔离，然后发现实际只需要三个 shell 脚本。

`DiskPlanner` 的 MVP 形态是具名内置策略：

```yaml
disk_policy: raid1-ssd-else-largest   # first | largest | by-model | raid1-ssd | by-hook
```

`TemplateResolver` 的 MVP 形态是规则表，按 `os + version + vendor + product` 匹配，自上而下第一条命中生效。

引入脚本引擎的信号：规则表开始长出嵌套条件，或运维反复要求「这批机器特殊处理」。

---

## 8. API

### 8.1 机器面（网关，装机 VLAN）

| 端点 | 认证 | 说明 |
|---|---|---|
| `GET /boot.ipxe` | 无 | DHCP 指向的静态引导片，chain 到 `/v1/boot` |
| `GET /v1/boot` | 无 | 引导决策，**默认拒绝**，签发 token |
| `GET /v1/answer/{task}/{token}/{file}` | token | 渲染 ks / user-data / preseed / unattend / boot.cfg |
| `POST /v1/signal/{task}/{token}/{phase}` | token | 阶段信号，body 为纯文本日志 |
| `POST /v1/facts/{task}/{token}` | token | hook 写 fact |
| `GET /v1/facts/{task}/{token}` | token | hook 读 fact |
| `GET /artifacts/...` | 见 §9.3 | **由 nginx 直接服务，不经 Go** |
| `UDP :514` | 无 | syslog 接收 |

### 8.2 人面（控制台，管理网）

```
# 认证（端点名与 UI 框架默认约定对齐，见 §12.5）
POST   /console/v1/auth/login                 → { accessToken }
GET    /console/v1/user/info                  → UserInfo（含 roles[]）
GET    /console/v1/auth/codes                 → 权限码 string[]
POST   /console/v1/auth/logout

# ISO / 制品集
POST   /console/v1/artifact-sets              登记（本地路径或上传）→ 异步 import job
GET    /console/v1/artifact-sets
POST   /console/v1/artifact-sets/{id}/verify  重算 sha256 校验完整性
DELETE /console/v1/artifact-sets/{id}         仅 deprecated 且无引用

# hook
POST   /console/v1/hooks
GET    /console/v1/hooks?stage=&os_family=
PUT    /console/v1/hooks/{id}                 建新版本，不改旧版本
POST   /console/v1/hooks/{id}/validate        bash -n / shellcheck / requires 校验
GET    /console/v1/hooks/{id}/versions
POST   /console/v1/hooks/{id}/approve         draft → review → active

# 模板
POST   /console/v1/templates
PUT    /console/v1/templates/{id}             建新版本
GET    /console/v1/templates/{id}/form-schema 聚合出的 JSON Schema
POST   /console/v1/templates/{id}/preview     ★ 假数据渲染，看生成的 ks 与菜单

# 任务（无 arm / disarm —— 建任务即授权，§3.2）
POST   /console/v1/tasks                      建任务 → pending，即刻接受引导
GET    /console/v1/tasks?state=&sn=
GET    /console/v1/tasks/{id}/events
GET    /console/v1/tasks/{id}/stream          SSE
POST   /console/v1/tasks/{id}/retry           重置 attempt + 换 token → pending
                                              ★ 对 rebooting / initializing 超时的
                                                失败态必须拒绝（§3.4）
POST   /console/v1/tasks/{id}/cancel          → 终态，之后不再响应该主机的引导请求

# 决策 dry-run
POST   /console/v1/disk-policy/dryrun         拿录制的硬件 facts 跑策略，输出布局不落地
```

**人面响应统一包裹 `{"code":0,"data":...,"message":""}`**，`code != 0` 即失败、`message` 可直接展示。这是 UI 框架默认响应拦截器的约定（§12.5），照它做可以不改框架代码。机器面不适用 —— 那边是 iPXE 与 busybox curl，保持纯文本与裸响应（§6.2）。

**`preview` 与 `dryrun` 是开发期性价比最高的两个端点。** 装机模板的错误反馈周期极长（建任务 → 上电 → 等数分钟 → 失败 → 翻日志），这两个端点把它压到秒级。

### 8.3 表单 schema 由三段聚合

```
固定字段        sn（从已发现清单选取，非自由文本）、os / version → 定位 template、hostname、biz_ip
template 声明   template.required_form_fields
hook 聚合       所有 hook_refs 的 params_schema 合并去重
```

`GET /console/v1/templates/{id}/form-schema` 返回 JSON Schema，前端动态渲染。新增一个需要填 VLAN ID 的 hook 时表单自动多一项，UI 代码不动。

---

## 9. 安全

### 9.1 风险面与准入控制

**核心风险**：一个无认证的 HTTP 端点，有权告诉机器「擦盘重装」。

因此准入控制不是可选项，三道闸门叠加：

1. **默认拒绝** —— `/v1/boot` 查不到 `pending` task 即返回 `no_task`（载荷为 `sanboot`），只有有任务的主机才签发 token 并下发装机脚本（§4.2）
2. **SN 从清单选取 + MAC 双因子** —— 不接受自由文本，两个键都对上才签发（§4.3）
3. **attempt 计数封顶** —— 同一任务窗口内菜单最多取 3 次，超过即 failed 并返回 `sanboot`

**原第 2 道「armed 显式确认」已删除**（§3.2）。它与建任务的判据完全相同，拦不住选错机器，却在批量装机时被运维整批提前 arm 掉而自动失效。删除的直接后果是**第 2 道闸门从「建议」变成硬要求**：SN 自由文本与「只校 SN 不校 MAC」这两种实现都不再可接受，因为建任务是唯一一次人工确认机器身份的机会。

MVP 无 TLS，故 token 与默认拒绝是唯一的防线（§1.5），不能降级实现。

### 9.2 `X-Real-IP` 只在 loopback peer 时可信

见 §1.6 的代码。不加此判断则任何人可伪造该头部冒充其它机器。

### 9.3 制品授权分三类，不要试图统一

| 类别 | 授权方式 | 理由 |
|---|---|---|
| 离线仓库（RPM/deb） | **开放** | installer 按 repodata 自行构造 URL 拉取几万个包，无法让它带签名；内容本就是公开的 |
| 大制品（kernel/initrd/镜像/WIM） | nginx `secure_link`（HMAC + 过期） | Go 只签发 URL，完全不在数据路径上 |
| 少量敏感大文件 | `X-Accel-Redirect` | Go 校验后交 nginx 发字节；无需额外模块但每文件多一次往返 |

```nginx
location /artifacts/ {
    secure_link $arg_md5,$arg_expires;
    secure_link_md5 "$secure_link_expires$uri$remote_addr SECRET";
    if ($secure_link = "")  { return 403; }
    if ($secure_link = "0") { return 410; }
    sendfile on;  aio threads;
}
```

`$remote_addr` 进签名可让 URL 绑定到具体机器，防止 token 横向传播 —— 但需确认路径上无 NAT，否则签名恒不匹配。Go 签发时必须用 `X-Real-IP`（§1.6）。

边缘情况：若机器在「取引导脚本」与「取制品」之间更换了 DHCP 租约导致 IP 变化，签名失效。租约时间设得长于装机窗口可避开。

### 9.4 权限分级

| 操作 | 权限 | 说明 |
|---|---|---|
| 建任务 | 装机操作员 | **建任务即授权装机**（§3.2）—— 这一个动作就能让目标机器下次网络启动时被擦盘，权限分配按此理解，不要当成「只是填个表」 |
| 创建/编辑 hook | **独立权限，与建任务分开** | hook 以 root 身份在分区之前运行，能力等同「擦掉这台机器所有盘」 |
| 激活 `pre` 阶段 hook | 需审批（`draft → review → active`） | `pre` 能碰分区 |
| 配置 provider | **最高权限，走代码评审** | provider 跑在服务端，能读凭据、访问内网、决定哪台机器装什么 —— 影响面是全部机器而非一台 |

每个 hook 版本记作者、审批人、时间；hook 正文不可变，改即新版本 + 新 digest。

### 9.5 凭据清理

firstboot 成功后：

```bash
shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token
rm -f /var/lib/lober/init.pending
sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null
```

`inst.ks=` 带 token 的完整 URL 会进入目标机的 `/proc/cmdline`、anaconda 日志、`/root/anaconda-ks.cfg`，同时也进 nginx access log。因 token 短时效且装完即废，影响有界，但顺手清理值得做。

### 9.6 镜像签名：气隙下 sigstore keyless 走不通

cosign 的 keyless 模式依赖 Fulcio（需外网 OIDC）与 Rekor（透明日志），气隙环境均不可达。

用 key-based 签名：`cosign sign-blob --key`，公钥预置在 discovery image 与网关中；配 sha256 清单做完整性校验。**不要指望默认流程能跑。**

---

## 10. 关键失败模式

按杀伤力排序，前三条能造成机房级事故。

| # | 失败模式 | 规避手段 |
|---|---|---|
| 1 | **装完又从网络启动 → 循环重装** | 任务已进终态，`/v1/boot` 查不到 `pending` task 即返回 `no_task`；载荷 `sanboot` 让机器回本地盘（§4.2）。installer 自身也会调 `efibootmgr` 置顶本地盘（主流发行版标准行为），但 **BIOS/MBR 模式下无 efibootmgr** —— 那时 `no_task` 的 `sanboot` 载荷是唯一保护，是硬依赖 |
| 2 | **准入控制缺失 → 任意上电机器被重装** | §9.1 三道闸门。删掉 armed 后（§3.2）第 2 道「SN 从清单选取 + MAC 双因子」是硬要求 |
| 2b | **建任务时选错 SN → 生产机某天网络启动后被擦盘** | 这是删掉 armed 后的主要残留风险（§3.2）。SN 只能从已发现清单选 + MAC 双因子（§4.3）；建任务界面显示 SN、机架位、上次被发现时间；不确定何时装的机器先别建任务；建错立刻 cancel |
| 3 | **无限引导循环**（iPXE 起来后又拿到 iPXE 二进制） | DHCP 用 option 77 user-class 区分两段（§1.3）。**部署期核对项，运行期不检测（§0.1）** |
| 4 | **引导架构不匹配**（UEFI 机器收到 `undionly.kpxe`，或匹配不上任何 tag 导致机器毫无反应） | 按 option 93 分流，**7 与 9 都要匹配、7 是实测主流值**（§1.3、附录 A.1）；未知架构显式不发 filename 并打日志，绝不兜底发某个二进制（§1.3.1）。**部署期核对项，运行期不检测（§0.1）** |
| 4b | **固件模式与目标 OS 不兼容**（BIOS 机器收到 ESXi 的 `mboot.efi`、x86_64 制品发给 ARM64 机器） | DHCP 看不到任务、无法校验；网关在下发菜单前按 `${buildarch}` / `${platform}` 硬拒绝并置 failed（§4.6） |
| 5 | **磁盘设备名漂移 → 装到错误的盘** | by-id / by-path / WWN；inspect 后服务端决策（§5.6） |
| 6 | **网卡命名漂移 → 装完网络不通，机器失联需人到现场** | MAC 绑定 + 固化 `net.ifnames` 策略 |
| 7 | **业务网在 `%post` 被激活 → 后续信号全丢** | 写配置不激活（§5.7） |
| 8 | **TFTP 吞吐瓶颈** | TFTP 只发最小 iPXE 二进制，之后全走 HTTP |
| 9 | **气隙下镜像源缺包 → installer 中途失败** | 完整仓库镜像 + 锁定快照，不依赖「应该有」 |
| 10 | **时间不同步 → 日志时序错乱** | 装机网提供本地 NTP（dnsmasq 可兼任 DNS） |
| 11 | **并发装机打满 HTTP** | 状态机设 provisioning 并发上限与排队 |
| 12 | **BMC 被高频轮询假死** | 不适用 —— 本系统不做带外（§0.2）。若将来以插件接入 Redfish，间隔需 ≥ 60 s、全局并发 ≤ 8、按 BMC 串行 |

**两类失败模式要分清（§0.1）：**

| | 3、4、8 —— 范围外 | 1、2、2b、4b、5、6、7、9、11 —— 范围内 |
|---|---|---|
| 发生位置 | 签出 token 之前：固件、DHCP、TFTP | 签出 token 之后：网关、渲染、hook、信号 |
| 谁能看见 | DHCP/TFTP 日志、BMC 控制台 | 本系统的状态机与事件表 |
| 本系统做什么 | **配对那一次，之后不检测、不告警** | 检测、拒绝、置失败态、带诊断信息上报 |

左列的现场症状是机器在 PXE 里转圈或毫无反应，而本系统连一次请求都没收到，也就没有任何可上报的内容 —— 排查从 BMC 控制台与 DHCP/TFTP 日志开始，不是从控制台任务列表开始。**失败模式 4 与 4b 长得像但分在两列**：4 是机器拿错或拿不到 iPXE 二进制（范围外），4b 是机器已经跑起 iPXE 并请求了 `/v1/boot`、网关据实际上报值硬拒绝（范围内，§4.6）。

失败模式 1 横跨两侧：它的表现在范围外（机器反复网络启动），但拦截点在范围内（`/v1/boot` 返回 `no_task` + `sanboot`）—— 这正是 §4.2 里那一行 `sanboot` 不能省的原因。失败模式 10（时间不同步）与 12（BMC 轮询）不进这个二分：前者是装机网的基础设施项，配一次 NTP 即可；后者本系统不适用（§0.2）。

其它需注意的运维点：

- **nginx 与 Go 必须对制品路径达成一致**。Go 签发 URL、nginx 按路径找文件，不一致即引导时 404。做法：制品发布时写 manifest，Go 启动时校验文件真实存在，别等机器来了才发现。
- **心跳与信号回调量不小**（数百台 × 每 30 s）。nginx 侧给 `/v1/signal` 单独设宽松 `limit_req`，别与引导端点共用限流桶。
- **ESXi 与 Windows 装到一半失败可能留下半个引导项**。任务此时已是 `failed`（终态），这类机器下次网络启动会拿到 `no_task` + `sanboot`，然后本地盘引导失败、回到 PXE 轮询或停住等人工 —— 这是正确行为，比自动重装安全。要重装必须运维显式建新任务。

---

## 11. 实施路线

### Phase 1 — MVP（4–6 周）

目标：一台机器全自动装完，且全过程可见。

| 项 | 范围 |
|---|---|
| DHCP | dnsmasq 静态配置，TFTP + BIOS/UEFI PXE 两条路径；option 93 映射按 §1.3 全配（含 7/9 双值与未知架构不兜底）。**一次性部署动作，属范围外（§0.1）** —— 现场逐架构实测一遍即固定，不进 CI、不做运行期检测 |
| nginx | 制品静态服务 + API 反代，两个 server 块绑具体 IP |
| Go | 单二进制两 listener，绑 127.0.0.1 |
| 数据库 | PostgreSQL（实际落地未经过 SQLite 阶段，见 0.5 修订） |
| OS | 单一发行版（建议 Rocky 9），安装器驱动 + kickstart |
| 状态机 | `pending → booting → installing → rebooting → initializing → done` + failed。**无 armed**（§3.2）；`/v1/boot` 查不到任务返回 `no_task`（§4.2） |
| 引导校验 | §4.6 的 arch / 固件模式 / 制品完整性三项硬拒绝。**单 OS 阶段就要做** —— 它属于安全闸门（§10 失败模式 4b），不是多 OS 才需要的功能 |
| 信号 | `/v1/signal` 全部阶段点 + 去重 + 只进不退 + 分阶段超时 |
| 辅助通道 | syslog receiver |
| provider | 只定接口 + 内置实现，无加载器 |
| hook | 用户 hook（pre / post / firstboot）+ 渲染器骨架 |
| UI | vben-admin 5.x `web-ele` 精简后内嵌（§12）：登录、主机清单、建任务、任务详情 + SSE 日志、模板编辑与 preview |
| 验证 | `curl` 模拟机器打点，整条链本地跑通，不碰硬件 |

MVP 明确不做：TLS、UEFI HTTP Boot、插件加载器、镜像直写、Windows、ESXi、多播、增量传输。

### Phase 2 — 多 OS 与扩展点（4–6 周）

- Ubuntu autoinstall + Debian preseed（复用同一模板框架与 hook 阶段映射）
- `exec` / `http` provider 加载器 —— 此时已知哪些扩展点真需要外部集成
- inspect 阶段硬件采集 + 服务端磁盘/网卡决策 + facts KV
- 心跳插件（icmp / tcp）
- `secure_link` 制品授权
- 每机架 HTTP cache（实测出带宽瓶颈后）
- 控制台 UI 补齐 §12.6 中标 P2 的页面：hook 审批、制品集导入、磁盘 dry-run（MVP 期这三件用 API 直接顶）

### Phase 3 — 镜像通道与异构 OS

- 镜像封装流水线：Packer（Windows / ESXi，必须引导真 installer）+ mkosi 或 osbuild（Linux）+ virt-customize（增量定制）
- 镜像直写通道（与安装器驱动并存：**安装器驱动作兜底**，覆盖新 OS 版本、特殊机型、调试；**镜像直写作主力**，覆盖批量与一致性要求）
- zstd 传输 + sha256/cosign key-based 校验
- Windows（wimboot + WinPE + unattend.xml）—— **同一版本按固件导两个 artifact_set**（§2.1），导入工作量是 Linux 的两倍
- ESXi（`ks=` + 渲染 boot.cfg）—— **仅 UEFI**；若届时确有 BIOS 机器需装 ESXi，先做 `mboot.c32` + COMBOOT 的独立 PoC（附录 B）
- `grpc` provider 加载器（带 drain / 健康检查 / 回滚 / 熔断）
- Starlark 决策引擎（若规则表已长出嵌套条件）

### Phase 4 — 生产化

- TLS + mTLS（iPXE 重编译带 `TRUST=`）
- Secure Boot 方案落地（shim 链载自签 iPXE，建议**先做独立 PoC**，技术风险最高）
- UEFI HTTP Boot
- 并发限流、失败重试、告警接入
- desync 增量传输（仅在实测有收益时）
- 审计报表

---

## 12. 控制台 WebUI

### 12.1 选型：vue-vben-admin 5.x 的 `apps/web-ele`

前端不自研，套 [vue-vben-admin](https://github.com/vbenjs/vue-vben-admin) 5.x 的 Element Plus 应用（`apps/web-ele`），MIT 许可。

| | |
|---|---|
| 上游版本 | monorepo `5.7.0` / `@vben/web-ele` `5.8.0`（2026-09-20 的 main） |
| 框架栈 | Vue 3.5.41 · Vite 8.3 · TypeScript 6.0 · Tailwind CSS 4.3 · vue-router 5.2 · pinia 4.0 |
| 组件与库 | Element Plus 2.14.3 + `unplugin-element-plus`；表格 vxe-table 4.21；请求 axios 1.18；i18n vue-i18n 11.4 |
| 构建环境 | Node `^22.18.0 \|\| ^24.12.0`、pnpm ≥ 11（`packageManager: pnpm@11.16.0`，根 `preinstall` 用 `only-allow pnpm` 卡住包管理器） |
| 文档 | <https://doc.vben.pro/>，精简方法见「简介 → 精简版本」与「其他 → 移除代码」 |

上游有五个 UI 应用（`web-antd` / `web-antdv-next` / `web-ele` / `web-naive` / `web-tdesign`），差异只在 `src/adapter/*` 与组件库依赖，框架层（`packages/@core/*`、`packages/effects/{layouts,access,request,common-ui}`、`internal/*`）完全共用。选 element 不会把自己锁死 —— 将来换库的代价集中在 adapter 层与页面里直接写的 `El*` 组件。

**5.0 起官方不再提供精简仓库或精简分支**（官网原话，理由是减少维护成本、保持开发体验一致）。精简方式就是克隆全仓再删，下一节是照官网步骤走完后本项目的确定结论。

### 12.2 精简：删什么、留什么

仓库级：

| 对象 | 处置 | 说明 |
|---|---|---|
| `apps/web-antd` `apps/web-antdv-next` `apps/web-naive` `apps/web-tdesign` | 删 | 只留 `web-ele` |
| `apps/backend-mock` | 删 | 同时把 `.env.development` 的 `VITE_NITRO_MOCK` 置 `false`。我们有真后端，Nitro mock 只会掩盖契约不一致 |
| `playground` | 删 | 演示代码 |
| `docs` | 删 | 官网文档源码 |
| `.changeset` `.github` `scripts/deploy` | 删 | changeset 版本管理、上游 CI、docker 部署脚本都不适用 |
| `packages/**` `internal/**` | **全留** | 框架本体与构建配置，删了跑不起来 |
| 根 `package.json` 的 `scripts` | 改 | 只留 `dev:ele` / `build:ele` 与 lint/typecheck，其余 `dev:*` `build:*` 删掉 |

应用内部（官网「精简版本 → 应用精简」）：

| 对象 | 处置 |
|---|---|
| `src/router/routes/core.ts` | 只留 `Login` 与 fallback；删 `CodeLogin` / `QrCodeLogin` / `ForgetPassword` / `Register` |
| `src/router/routes/modules/{dashboard,demos,vben}.ts` | 删，换成 §12.6 的业务路由 |
| `src/views/_core/authentication/{code-login,qrcode-login,forget-password,register}.vue` | 删 |
| `src/views/_core/{about,profile}` | 删 —— MVP 无个人中心，改密码给一个接口就够，不需要整个 profile 模块 |
| `src/views/_core/fallback` | 留 `not-found` / `forbidden` / `internal-error`，删 `coming-soon` / `offline` |
| `src/views/{dashboard,demos}` | 删。`dashboard/analytics` 是唯一用 echarts 的地方，删掉它 echarts 自然出局 |
| `packages/effects/common-ui/src/ui/{about,dashboard,profile}` | 删（删前确认路由已无引用） |

**不必为了体积去删 `packages/effects/plugins` 的代码。** 该包用子路径导出（`./echarts` `./tiptap` `./vxe-table` `./motion`），没 import 就不进 bundle，tiptap 和 echarts 留在源码树里不产生构建成本。

### 12.3 集成：源码进仓，dist 内嵌进二进制

```
web/                        精简后的 vben monorepo（根 package.json 在这里）
└── apps/web-ele/
internal/console/ui/dist/   前端构建产物，go:embed 目标
```

**`go:embed` 不接受 `..` 路径**，产物必须落在 embed 所在包的目录树内。因此把 `apps/web-ele/vite.config.ts` 的 `build.outDir` 直接指向 `../../../internal/console/ui/dist`，而不是构建完再 `cp` —— 少一个会被忘掉的步骤。

**构建必须在有外网的机器上做。** 气隙内网装不上 npm 依赖，这不是能绕的事。三种做法，按推荐顺序：

1. 开发机或 CI 构建，只把 `dist`（或含 dist 的 release tar）带进内网 —— MVP 采用
2. 外网 `pnpm fetch` 灌满离线 store，整个 store 带进内网后 `pnpm install --offline`
3. 内网起私有 registry（verdaccio / Nexus）镜像上游 —— 只有当内网确实需要改前端时才值得

`pnpm-lock.yaml` 必须提交。`dist` 是否入库取决于交付方式：内网只能拿到 git 仓库就必须提交，走 release tar 则不必。

### 12.4 环境变量与偏好设置（确定值）

`apps/web-ele/.env*`：

| 变量 | 值 | 理由 |
|---|---|---|
| `VITE_APP_TITLE` | `Lober 控制台` | |
| `VITE_APP_NAMESPACE` | `lober-console` | 缓存与 store 的前缀 |
| `VITE_APP_STORE_SECURE_KEY` | **必须改** | 上游默认值是 `please-replace-me-with-your-own-key`，store 持久化加密用它 |
| `VITE_BASE` | `/` | |
| `VITE_GLOB_API_URL` | `/console/v1` | 同源，不跨域，也不需要 `_app-config-*.js` 那套打包后改接口地址的机制 |
| `VITE_ROUTER_HISTORY` | `hash` | 保持上游默认。hash 路由不需要 SPA fallback，Go 的静态 handler 与 nginx 都不必配 history rewrite |
| `VITE_COMPRESS` | `none` | 内嵌的 `http.FS` 不会自动发 `.gz`，压缩交给 nginx 现场做（§1.4） |
| `VITE_ARCHIVER` | `false` | 不要 `dist.zip` |
| `VITE_PWA` / `VITE_DEVTOOLS` / `VITE_NITRO_MOCK` | `false` | |

`apps/web-ele/src/preferences.ts` 的覆盖项（只覆盖需要改的，其余走框架默认）：

| 项 | 值 | 理由 |
|---|---|---|
| `app.accessMode` | `frontend` | 菜单写死在前端路由，省掉一个后端菜单接口（§12.5） |
| `app.defaultHomePath` | `/tasks` | 任务列表是主战场 |
| `app.locale` / `widget.languageToggle` | `zh-CN` / `false` | 只发中文；`src/locales/langs/en-US` 可一并删 |
| `app.enableCheckUpdates` | `false` | 默认按分钟轮询检查前端更新，离线控制台没必要 |
| `app.defaultAvatar` / `logo.source` | 换成 `public/` 下的本地文件 | **默认值指向 unpkg.com** |
| `copyright.enable` | `false` | 默认带 vben 的公司名与站点链接 |
| `app.watermark` | `true` | 水印带操作人，误装追溯用 |
| `tabbar` | 保留 | 多台机器并行排查时来回切页面，标签页有用 |

**气隙下必须处理的三处外网依赖** —— 前两条官网点明了，第三条要自己盯：

1. `apps/web-ele/index.html` 在生产构建下注入 `hm.baidu.com` 的百度统计脚本，删掉（官网「其他 → 移除代码」）
2. `logo.source` 与 `app.defaultAvatar` 默认指向 `unpkg.com/@vbenjs/static-source`，覆盖为本地资源
3. 图标：`@vben/icons` 建立在 `@iconify/vue` 上，`@lucide/vue` 是本地依赖。只用 `@vben/icons` 导出的 lucide 组件，或用它同时导出的 `addCollection` 预注册离线图标集；别在路由 `meta.icon` 里写需要在线解析的 iconify 图标名（列入附录 B 待核实）

### 12.5 认证、权限与响应约定

`accessMode` 取 `frontend`（框架另支持 `backend` 动态菜单与 `mixed`）：角色写在路由 `meta.authority`，按钮级用权限码。代价是加页面要改前端，收益是不用实现菜单接口、菜单结构与页面代码不会漂移。本系统页面数量固定（§12.6），这个取舍明显划算。

需要 Go 侧实现的端点，名字与框架默认的 `src/api/core/*` 对齐，挂在 `/console/v1` 下（§8.2）：

| 端点 | 返回 |
|---|---|
| `POST /auth/login` | `{ accessToken }` |
| `GET /user/info` | `UserInfo`，含 `roles: string[]` |
| `GET /auth/codes` | 权限码 `string[]` |
| `POST /auth/logout` | — |

不做 refresh token（`enableRefreshToken` 默认 false），单 access token 即可。

**响应包裹格式是选型带来的约束。** `src/api/request.ts` 的默认响应拦截器按 `codeField: 'code'` / `dataField: 'data'` / `successCode: 0` 解包，且 `responseReturn: 'data'`。因此人面 API 统一返回 `{"code":0,"data":...,"message":""}`（已写入 §8.2）。反向做法是改拦截器（约五行，换成 `responseReturn: 'body'` 并去掉 `defaultResponseInterceptor`）让 API 保持裸 REST —— 选包裹是因为它不碰框架代码，升级上游时无冲突。

角色与权限码，对应 §9.4 的权限分级：

| 角色 | 可见路由 | 权限码 |
|---|---|---|
| `viewer` | 全部，只读 | — |
| `operator` | `/tasks` `/hosts` | `task:create` `task:retry` `task:cancel` |
| `hook-author` | `/hooks` | `hook:write` |
| `hook-approver` | `/hooks` | `hook:approve` |
| `admin` | 全部 + `/settings` | `provider:write` `artifact:import`，以及以上全部 |

**`task:arm` 权限码已删除**（§3.2 删掉了 armed 状态），UI 上不存在 arm / disarm 按钮。危险动作的确认要求整体搬到**建任务**这一步 —— 那才是真正的授权动作：

- 提交前弹二次确认，确认框内显示 SN、机架位、**上次被发现时间**（§3.2 的硬要求，从原 arm 界面搬来）
- SN 字段用 `host-select` 组件从 `/hosts` 取值，**不是 `input`**（§12.7 的 `x-widget` 子集里有这个类型，§4.3 是硬要求）
- 建任务后即接受引导，因此 `/tasks` 列表里 `pending` 状态要标成「等待机器引导，随时可能开始装机」，不要显示成中性的「未开始」

### 12.6 页面清单（就这些，不扩张）

| 路由 | 页面 | 主要端点 | 阶段 |
|---|---|---|---|
| `/auth/login` | 登录 | `POST /auth/login` | P1 |
| `/tasks` | 任务列表：按 state 过滤、按 SN 搜索、retry / cancel | `GET /tasks` | P1 |
| `/tasks/:id` | 任务详情：状态机时间线、事件表、SSE 实时日志、失败详情（§3.5 的六个字段） | `/tasks/{id}/events`、`/stream` | P1 |
| `/tasks/new` | 建任务：选主机 → 选模板 → 动态表单 → **二次确认**（§12.7、§12.5）。这是唯一的授权动作，按危险动作做 | `/templates/{id}/form-schema`、`POST /tasks` | P1 |
| `/hosts` | 已发现主机清单，SN 的唯一来源（§4.3） | 清单同步接口 | P1 |
| `/templates` | 模板列表、编辑、版本；右侧常驻 preview 面板 | `POST /templates/{id}/preview` | P1 |
| `/hooks` | hook 列表、版本、`validate`、审批流转 | `/hooks/*` | P2 |
| `/artifact-sets` | ISO 登记与导入进度（异步 job） | `/artifact-sets/*` | P2 |
| `/tools/disk-dryrun` | 磁盘策略 dry-run：贴 facts，出布局 | `POST /disk-policy/dryrun` | P2 |

P2 的三个页面在 MVP 期直接用 API 顶（§11 Phase 2 已列）。

`preview` 与 `dryrun` 要做成一等公民、给显眼入口，别埋进二级菜单 —— §8.2 说明了它们把模板调试周期从「上电等十分钟」压到秒级，UI 不给入口等于白做。

列表页统一用 `useVbenVxeGrid`（适配器已在 `src/adapter/vxe-table.ts`），不要每个页面各写一套 `el-table` 加分页。

### 12.7 表单：JSON Schema → VbenFormSchema 的转换层

§8.3 的 `form-schema` 端点返回 JSON Schema，而框架表单吃的是自己的 schema 数组（`{ component, fieldName, label, rules, componentProps, dependencies }`，5.7 起内部换成 TanStack Form + Zod 4）。中间必须有一层映射，位置定在 `src/adapter/form-schema.ts`。

为了让映射是全函数、而不是「大部分 schema 能渲染」，**服务端输出的 JSON Schema 限定为一个子集**：

```
type     string | integer | number | boolean | array（array 仅用于多选）
校验      enum / pattern / minimum / maximum / minLength / maxLength / required
展示      title / description / default
扩展      x-widget      input | textarea | password | number | switch | select | ip | host-select
         x-order       排序
         x-depends-on  { field, value } → 映射到框架表单的 dependencies
```

超出子集的 schema 在 hook 注册时就拒绝 —— §2.2 已有 `params_schema` 的注册期校验，这条并进去，而不是等渲染时静默丢字段。

**前端校验只是体验，不是防线。** 映射出的规则给即时反馈，服务端仍按 §5.6 对 `form_data` 做类型白名单强校验 —— 那些值会进 kickstart 脚本。

### 12.8 SSE：用 fetch 流，不用 EventSource

`@vben/request` 是 axios 封装，不含 SSE，这块要自己补。补的时候有个陷阱：**`EventSource` 不能设请求头**，用它就只能把 token 塞进 query string，而 token 进 query 会落进 nginx access log —— §9.5 刚要求清理带 token 的记录。

因此 `/console/v1/tasks/{id}/stream` 用 `fetch` + `ReadableStream` 读，`Authorization` 头照常带。四个要点：

- 断线用指数退避重连，重连带 `Last-Event-ID`，服务端按 `task_event.seq`（§2.5）续传，不从头重放
- 组件卸载必须 `AbortController.abort()`，否则列表与详情来回切会攒住一堆连接
- nginx 侧 §1.4 已关 `proxy_buffering`，Go 侧同时回 `X-Accel-Buffering: no`
- 任务进终态（done / failed）由服务端主动关流，前端收到终态即停止重连

### 12.9 与上游同步

**不做侵入式修改。** 定制全部收敛在四处：`apps/web-ele/src/**`、`apps/web-ele/.env*`、`apps/web-ele/index.html`、根 `package.json` 的 `scripts`。`packages/**` 与 `internal/**` 保持原样（删文件不算改），升级就是覆盖这两棵树。

**pin 住一个上游 commit，升级当独立任务做。** 5.x 仍在快速迭代 —— 5.7 这一带已经换过 Vite 8、Tailwind 4、TypeScript 6、oxlint/oxfmt，表单底层换成 TanStack Form + Zod 4。在装机功能开发中途跟版，会把「装机逻辑坏了」和「前端框架升级坏了」两类问题混在一起排查。




---

## 附录 A — 已验证的技术事实

以下论断经三票对抗式验证通过（2026-09-20 检索）。**注意**：验证环境的 WebFetch 对 ipxe.org / github.com / docs.vmware.com 等域名受限，多数验证依据搜索引擎索引内容而非原文，故标注为弱确认者需自行复核。

| # | 论断 | 票数 | 来源 |
|---|---|---|---|
| A.1 | 按客户端架构类型分发不同引导文件：`X86PC`→`undionly.kpxe`、`IA32_EFI`→`snponlyx32.efi`、`BC_EFI`/`X86-64_EFI`→`snponly.efi`。同一 DHCP 实例内 BIOS 与 UEFI 必须发架构专属二进制，无统一通吃文件 | 3-0 | [ipxe.org/appnote/proxydhcp](https://ipxe.org/appnote/proxydhcp) |
| A.1b | **RFC 4578 中 option 93 的 7 与 9 定义有误、勘误要求互换**：实际 UEFI 固件用 **7** 上报 EFI x86-64，而非 9；ipxe.org 将 9 标为 obsolete，Microsoft DHCP 策略与主流实战配置均按 `Arch:00007` 编写。故 §1.3 早期版本中只匹配 9 的配置是错的，已修正为 7 与 9 同 tag | 3-0 | [RFC 4578 勘误](https://www.rfc-editor.org/errata/rfc4578)、[ipxe.org/cfg/platform](https://www.ipxe.org/cfg/platform)、[serverfault 讨论](https://serverfault.com/questions/349693/what-is-the-pxe-client-system-architecture-type-bc-efi-7-from-rfc-4578) |
| A.2 | Smee/Tinkerbell 可不接管二层 DHCP，通过两段链载对接 DHCP（dnsmasq/Kea/ISC/Microsoft 四种配置语法齐备）：第一段 DHCP 给出 `ipxe.efi` 位置，第二段 iPXE 再广播、DHCP 给出 `auto.ipxe` 脚本位置 | 3-0 | [tinkerbell/smee docs/DHCP.md](https://github.com/tinkerbell/smee/blob/main/docs/DHCP.md) |
| A.3 | 两段之间用 DHCP option 77（user class）区分：值等于 `"Tinkerbell"` 则发脚本，否则发二进制 | 2-1 | 同上 |
| A.4 | wimboot 使 iPXE 可通过 HTTP 而非默认 TFTP 引导 WinPE，千兆网下典型 WinPE 镜像数秒下完 | 2-1 | [ipxe.org/wimboot](https://www.ipxe.org/wimboot) |
| A.5 | 标准流程为 HTTP 引导进 WinPE，再连标准 Windows 文件服务器（或 iSCSI target）将完整 Windows 安装到本地盘 | 2-1 | 同上 |
| A.6 | **wimboot 只能引导 WinPE 镜像**，不能引导完整 Windows；完整 OS 需从 iSCSI 或 AoE 卷块级引导 | 3-0（弱确认） | 同上 |
| A.7 | ESXi 脚本化安装支持从 HTTP/HTTPS/FTP/NFS/CD-ROM/USB 获取 kickstart 脚本，安装器自身可走 PXE 或 CD/DVD 引导，与脚本位置无关 | 3-0 | [vSphere 8.0 docs](https://docs.vmware.com/en/VMware-vSphere/8.0/vsphere-esxi-upgrade/GUID-341A83E4-2A6C-4FB9-BE30-F1E19D12947F.html) |
| A.8 | 脚本位置靠引导命令行 `ks=filepath` 指定，不设该选项则脚本化安装/升级无法启动 | 2-1 | 同上 |
| A.9 | 用 USB 引导时 ks.cfg 不能放在同一 U 盘上，必须在独立设备 | 2-1 | 同上 |
| A.10 | pin/tftp 被其维护者定位为「特性集足以支持 PXE 引导」 | 3-0 | [github.com/pin/tftp](https://github.com/pin/tftp) |
| A.11 | desync 是 casync 格式的 Go 实现，与 casync 互操作（同样的 index/archive/chunk store），带并行分块、更多存储后端与 **Go 库 API**；命令行非 drop-in 替换 | 3-0 | [github.com/folbricht/desync](https://github.com/folbricht/desync) |
| A.12 | **ISC dhcpd 已 EOL**：4.4.3-P1 与 4.1-ESV-R16-P2（2022-10-05 发布）是最后的维护版本，ISC 不再计划发布新版，且不知有任何完整维护的分支，官方建议迁移到 Kea | 3-0 | [isc.org/blogs/isc-dhcp-eol](https://www.isc.org/blogs/isc-dhcp-eol/)、[kb.isc.org isc-dhcp-eol-dates](https://kb.isc.org/docs/isc-dhcp-eol-dates) |
| A.13 | iPXE 始终按 `bootfile-name` 中指定的 URI scheme 行事：值以 `http://` 开头即执行 HTTP 引导，不受 legacy UEFI HTTP Boot 的约束 | 2-1（弱确认） | [ipxe/ipxe discussions#271](https://github.com/ipxe/ipxe/discussions/271) |
| A.14 | **ESXi 传统 BIOS 引导器依赖旧版 Syslinux（v3.86）**，且 GRUB2 无法链载 ESXi 的 EFI 引导器 —— BIOS 与 UEFI 两条 ESXi 引导路径机制不同，不可互换 | 2-1 | [theforeman.org 部署 ESXi](https://theforeman.org/2018/08/deploying-esxi-through-foreman.html)、[Cobbler ESXi 文档](https://cobbler.readthedocs.io/en/latest/user-guide/esxi.html) |
| A.15 | Kea 中 option 93 用符号名 `client-system` 测试，形如 `option[client-system].hex == 0x0006`；**带匹配表达式的 class 按配置中定义的顺序处理**。实战中存在「UEFI 固件确实发 `0x0007` 但 Kea 仍回 legacy BIOS 引导文件」的踩坑记录 | 2-1（弱确认） | [Using iPXE with KEA DHCP server](https://gist.github.com/robinsmidsrod/90f158524535f78437cd3f5602a387d1)、[Kea 客户端分类文档](https://kea.readthedocs.io/en/latest/arm/classify.html)、[Netgate 论坛案例](https://forum.netgate.com/topic/184301/kea-dhcp-uefi-pxe-boot-sends-wrong-boot-file/12) |

---

## 附录 B — 待验证项

以下内容是基于工程经验的设计判断，**本轮研究未能取得来源验证**（检索环境的 WebFetch 对关键域名受限，27 个来源中 19 个返回空）。实施前需自行核实。

| 项 | 需核实什么 |
|---|---|
| iPXE 变量拼写 | `${serial}` / `${uuid}` / `${asset}` / `${buildarch}` / `${platform}` 的准确名称与可用性，对照 ipxe.org/cfg；多网卡时 `net0` 是否为引导网卡 |
| 各 OS 内核参数 | §4.5 的 `inst.ks=` / `ds=nocloud-net;s=` / `preseed/url=` / `autoyast=` 拼写与语义，对照各发行版官方文档 |
| UEFI HTTP Boot | option 93 的 HTTP 变体码值（x64 为 16？）、option 60 vendor-class `HTTPClient` 的必要性，对照 RFC 5970。注：x64 UEFI PXE 用 7、ARM64 UEFI PXE 用 11 已在 A.1b 确认，此行仅剩 HTTP 变体待核 |
| option 93 实机取值 | §1.3 的码值表需在实验环境逐架构实测：抓 DHCP DISCOVER 看固件实际上报什么。**尤其是国产 ARM 平台（飞腾/鲲鹏）是否规范地报 11**。属部署期一次性核对（§0.1），核完即固定 |
| dhcpd 条件语法 | §1.3.1 中 `option client-arch = 00:07` 这种双字节字面量比较的实际行为，用 `dhcpd -t` 加真机各验一遍；`or` 在 `elsif` 条件中的求值是否如预期 |
| Kea 多 class 命中 | **§1.3.2 的核心风险**：多个匹配的 class 都定义 `boot-file-name` 时，哪个生效（首个命中 / 末个命中 / 报错）。当前配置用显式反条件绕开了这条语义，但需确认绕开是否真的必要 —— 若 Kea 语义明确且符合预期，配置可大幅简化 |
| Kea `member()` 与符号名 | `not member('ipxe')` 能否替代 §1.3.2 中重复的 `not (substring(...))`，以及其求值时机是否依赖 class 定义顺序；`option[client-system]` 与 `option[user-class]` 符号名在目标 Kea 版本上是否存在，对照 [ISC 标准选项表](https://kb.isc.org/docs/standard-dhcp-options)。均用 `kea-dhcp4 -t` 加真机验证 |
| option 77 的 RFC 3004 结构 | DHCPv4 option 77 规范上是带长度前缀的字符串列表，而 iPXE 发的是裸字符串 `iPXE`。§1.3.1 的 `= "iPXE"` 与 §1.3.2 的 `substring(...,0,4)` 两种写法都基于后者，需抓包确认 iPXE 实际发的字节形态 |
| ESXi BIOS 引导 | A.14 的 `mboot.c32` 路径是否真能经 iPXE COMBOOT 走通、预编译 iPXE 二进制是否开启 COMBOOT。**MVP 已明确排除（§0.3），此项仅在将来要覆盖 BIOS + ESXi 时才需 PoC** |
| Secure Boot 链载 | `shim.efi → ipxe.efi → kernel` 三级独立签名的具体机制与 MOK 注册流程。**技术风险最高，建议独立 PoC** |
| nginx 模块可用性 | 发行版 nginx 包是否带 `--with-threads`（aio）与 `--with-http_secure_link_module` |
| Go plugin 行为 | §7.2 关于 `buildmode=plugin` 的约束（无 unload、同路径缓存、需重启、Windows 不支持），对照 pkg.go.dev |
| desync 增量收益 | 实际场景下的 chunk 复用率与流量节省量级，需自行实测（本轮相关论断被否决） |
| 开源方案对比 | Cobbler、Foreman/Katello + Discovery、matchbox、Ironic、MAAS、FOG、Warewulf、Digital Rebar 的架构与适用边界 —— 本轮检索几乎零产出，选型前建议直接读官方文档 |
| 镜像构建工具边界 | osbuild/images、mkosi、KIWI 的实际能力与 OS 覆盖范围 |
| 国产 OS 适配 | openEuler / 麒麟 / UOS / Anolis 的应答文件机制差异，及 ARM64（飞腾/鲲鹏）平台的引导差异 |
| 控制台 UI 离线图标 | `@iconify/vue` 对未本地注册的图标名是否确实回落到在线 Iconify API、`addCollection` 预注册离线图标集的正确用法（§12.4 第 3 条） |
| 前端离线构建 | `pnpm fetch` + `pnpm install --offline` 在这套依赖树（Vite 8 / Tailwind 4，含原生二进制依赖）下能否真的装上，需实测（§12.3 方案 2） |
| vben 版本漂移 | §12 按 2026-09-20 的上游 main（monorepo 5.7.0 / web-ele 5.8.0）核实。pin commit 前重新对一遍 §12.2 的目录名与 §12.5 的拦截器字段名 |

---

## 附录 C — 术语

| 术语 | 含义 |
|---|---|
| 制品集（artifact_set） | 一个 ISO 导入后产出的 kernel/initrd/仓库树/manifest 的集合 |
| 装机模板（template） | 制品集 + 应答文件模板 + hook 组合 + 磁盘策略的版本化组装单元 |
| hook | 用户编写的、在目标机器上执行的脚本，分 pre / post-nochroot / post / firstboot 四阶段 |
| 渲染器骨架 | 系统必需的机器侧动作，由渲染器生成的模板文本，非 hook、用户不可见 |
| provider | 服务端扩展点实现，可热更新，分 exec / http / grpc 三种加载方式 |
| fact | 服务端注入给 hook 的事实数据，构成对外契约（`LOBER_CONTRACT_VERSION`） |
| 信号（signal） | 目标机器主动推送的阶段事件，驱动状态机转换 |
| `no_task` | `/v1/boot` 查不到 `pending` 任务时的返回，载荷是 `sanboot`；不建任务、不记状态、不告警（§4.2） |
| 装机 token | TTL 90 分钟，首次 `/v1/boot` 匹配时开始计时。**没有它就无从装机**，是范围起点也是唯一执行许可（§0.1） |
| finalize token | TTL 24 小时，`%post` 写入目标系统，供重启后阶段使用 |

