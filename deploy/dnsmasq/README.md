# dnsmasq 部署说明 —— DHCP + TFTP（装机 VLAN）

依据：`SPEC.md` §1.3、§1.3.1、§1.3.2、§10 失败模式 3/4/10、附录 A.1、A.1b、附录 B；
`docs/superpowers/plans/2026-09-20-lober-mvp-09-integration.md` 任务 7；
`docs/contracts/http-machine.md` 端点 1。

本任务整体属于**部署期一次性核对，不是运行期功能**（SPEC §0.1）。这里列出的码值配错的
后果都发生在机器取 iPXE 二进制那一步——根本走不到网关，本系统没有检测手段。把细节写
清楚是为了**配置那一次就配对**。

---

## 部署前必跑的校验命令

```bash
# 1. 语法校验
dnsmasq --test --conf-file=/etc/dnsmasq.d/lober.conf

# 2. 抓包核对固件实际上报的 option 93 值（每种固件各上电一次）
tcpdump -i <lober.conf 里 interface= 指定的网卡> -vvv -s0 port 67 or port 68
```

**第 1 条已在 WSL debian-13 上核对通过**（2026-09-22，占位符替换成测试值后
运行，`dnsmasq: syntax check OK.`），详见
`docs/real-machine/01-配置语法核对.md` 第 1 节。

`dnsmasq --test` 只能验证语法，验证不了「这台 UEFI 机器真的报 7 而不是别的值」——
这一点只能靠第 2 条命令现场核对，**仍需真实固件，WSL 无法模拟**。SPEC 附录 B
把 option 93 实机取值列为待核实项，尤其**国产 ARM 平台（飞腾/鲲鹏）是否规范地
报 11**，MVP 阶段没有真实 ARM64 硬件可测时这一条留待有硬件时补做。

---

## `lober.conf` 里的占位符清单

部署前必须替换的占位符（配置文件内每处都有对应注释）：

| 占位符 | 含义 |
|---|---|
| `<PXE_VLAN_IFACE>` | 本机接在装机 VLAN 上的网卡名 |
| `<PXE_VLAN_SUBNET_START>` / `<PXE_VLAN_SUBNET_END>` / `<PXE_VLAN_NETMASK>` | 装机 VLAN 的地址池范围与子网掩码 |
| `<TFTP_SERVER_IP>` | 本机在装机 VLAN 上的地址（TFTP `next-server`） |
| `<GATEWAY_DOMAIN>` | 网关对外域名，必须与 Go 侧配置的 `gateway_base` 域名部分一致 |
| `<GATEWAY_IP>` | 网关（nginx 机器面入口）在装机 VLAN 上的真实 IP |
| `<NTP_SERVER_IP>` | 装机 VLAN 内可达的 NTP 服务地址 |
| `<TFTP_ROOT_DIR>` | TFTP 根目录路径（见下一节） |

**不要按占位符原样部署。** 这些值故意写成醒目的 `<...>` 形式，不是一个「看起来能跑」的
假值——如果留一个能通过语法校验但语义错误的假 IP，问题会在机器上电的那一刻才暴露，
而那时人已经在机房了。

---

## TFTP 根目录该放哪四个文件

`<TFTP_ROOT_DIR>` 指向的目录下必须放置以下四个 iPXE 官方预编译二进制，文件名与
`lober.conf` 里 `dhcp-boot` 各条规则的 filename 逐一对应：

| 架构 | 文件名 | 对应 dnsmasq tag | ipxe.org 对应路径 |
|---|---|---|---|
| BIOS（X86PC） | `undionly.kpxe` | `bios` | `ipxe.org` 官方发布的 `undionly.kpxe`（通用 legacy BIOS 网卡驱动构建） |
| IA32 UEFI | `snponlyx32.efi` | `efi32` | `ipxe.org` 官方发布的 32 位 UEFI SNP-only 构建 |
| x64 UEFI | `snponly.efi` | `efi64` | `ipxe.org` 官方发布的 64 位 UEFI SNP-only 构建 |
| ARM64 UEFI | `snp-arm64.efi` | `arm64` | `ipxe.org` 官方发布的 ARM64 UEFI SNP-only 构建（SPEC 原文标注为 `bin-arm64-efi/snp.efi`，具体子路径以下载时 ipxe.org 官网当前目录结构为准，命名可能随上游发布调整） |

**ARM64 那个文件 MVP 阶段可以不放**（SPEC §1.3、计划任务 7）——`lober.conf` 里对应的
`dhcp-match` 与 `dhcp-boot` 规则仍然保留。缺文件与缺规则是两种不同的失败：

- **缺文件、留规则**：ARM64 机器请求 `snp-arm64.efi` 时 TFTP 会记一条「文件不存在」的
  日志，排查者能立刻定位到「规则配了，二进制没放」。
- **缺规则**：ARM64 机器的 option 93（值 11）匹配不上任何 tag，DHCP 层面完全没有响应，
  现场症状是「机器毫无反应」，且没有任何日志留痕——这是 SPEC §1.3 反复强调的最难排查
  的一类失败，排查成本比前者高一个数量级。

选 SNP-only（`snponly*.efi`）而不是完整驱动集构建的理由：SNP（Simple Network
Protocol）构建体积小、兼容性好，是目前网络社区与 ipxe.org 官方文档里最常见的 UEFI
链载推荐构建；若现场某型号网卡的 UEFI 固件不支持 SNP，需要换成对应厂商驱动内置的
完整构建，此时文件名需相应调整，`lober.conf` 里的 filename 也要跟着改。

### 气隙环境下的获取与校验流程

内网无法直接从 `ipxe.org` 下载，标准流程是：

1. 在**有外网的机器**上从 `ipxe.org` 下载页获取以上四个构建（或用 iPXE 源码自行
   编译——离线/Secure Boot 场景通常需要自行编译，见 SPEC §1.1 组件清单）。
2. 下载后立即计算 sha256：

   ```bash
   sha256sum undionly.kpxe snponlyx32.efi snponly.efi snp-arm64.efi
   ```

3. **把校验值记进本仓库**（追加到本文件下方的「已记录的 sha256 校验值」一节，附上
   下载日期与来源 URL）。
4. 把文件与校验记录一并带进内网，落地前用同一条 `sha256sum` 命令核对一遍，
   确认传输过程没有损坏或被替换。
5. 放进 `<TFTP_ROOT_DIR>`。

### 已记录的 sha256 校验值

> 本节留空，等首次实际下载后由部署人员填写。**在这四个值填入之前，不要认为
> TFTP 二进制已经过校验**——留空本身就是「尚未核实」的诚实标记，不要用假值占位。

| 文件 | sha256 | 下载日期 | 来源 URL |
|---|---|---|---|
| `undionly.kpxe` | *(待填写)* | | |
| `snponlyx32.efi` | *(待填写)* | | |
| `snponly.efi` | *(待填写)* | | |
| `snp-arm64.efi` | *(待填写，MVP 阶段可延后)* | | |

---

## 排查步骤：抓 DHCP DISCOVER 看固件实际上报什么

SPEC 附录 B 把 option 93 实机取值列为待核实项，尤其国产 ARM 平台是否规范地报 11。
**调试第一步永远是抓包看固件实际上报什么，不要相信配置文件里的注释**——SPEC 附录
A.15 记录过一个真实案例：有用户报告 UEFI 固件明确发了 option 93 = 7，但 DHCP 侧仍然
回了 legacy BIOS 的引导文件，原因是配置本身写错了而不是固件行为异常。

### 步骤

1. 在跑 dnsmasq 的主机上，对着装机 VLAN 网卡抓包：

   ```bash
   tcpdump -i <PXE_VLAN_IFACE> -vvv -s0 -w /tmp/pxe-discover.pcap port 67 or port 68
   ```

2. 让待测机器上电，走网络引导（BIOS 或 UEFI 引导顺序需已经指向网络引导，这一步本身
   是 SPEC §0.1 划定的范围外前置条件，需运维在 BMC 或现场手动确认）。

3. 中断抓包（`Ctrl-C`），用 Wireshark 或 `tcpdump -r` 回放，找 DHCP DISCOVER 报文，
   在 `Bootp/DHCP` 选项列表里定位：

   - **option 93（Client System Architecture Type）**：核对实际数值是否落在
     `lober.conf` 已配的 `0 / 6 / 7 / 9 / 11` 集合内。BIOS 机器应为 `0`，
     UEFI x86-64 应为 `7`（若看到 `9` 也不算错，两者归同一 tag），ARM64 UEFI
     应为 `11`。
   - **option 77（User Class）**：iPXE 自身发起的第二次 DISCOVER 里，这个选项的值
     应为 `iPXE`（或其字节形态，SPEC 附录 B 提示 RFC 3004 定义的是带长度前缀的字符串
     列表，而 iPXE 实际发的是裸字符串——抓包时留意选项数据的原始字节，不要只看
     Wireshark 的解码结果）。

4. **记录结果**：把这次抓包核对出的实测值（各架构的 option 93 数值、
   `net0` 是否为实际引导网卡）追加进部署时按 M9 计划任务 10 要求建立的、带日期的
   核对结果文件——那份文件覆盖全系统的实机核对项，不只是 dnsmasq 一项，因此不在本
   README 里重复建立，本 README 只负责说明抓包本身的步骤。

### 常见误判

- **「机器毫无反应」不等于「DHCP 没收到请求」**。先看 dnsmasq 日志（`lober.conf` 已开
  `log-dhcp`）确认 DISCOVER 到底有没有到——到了但没回应，问题在 tag 匹配；
  没到，问题在网络链路或 VLAN 划分，不在这份配置里。
- **同一台机器第一次报的 client-arch 与第二次（iPXE 起来后）可能相同**——这正是为什么
  必须靠 `option 77 = iPXE` 而不是靠架构值本身来区分两个阶段（本目录 `lober.conf`
  里 `tag:!ipxe` 反条件的作用）。抓包时如果看到同一台机器两次 DISCOVER 的 option 93
  一样但 option 77 不同，这是预期行为，不是异常。

---

## 与网关配置的耦合点

`lober.conf` 里唯一与 Go 网关产生耦合的一行是：

```
dhcp-boot=tag:ipxe,http://<GATEWAY_DOMAIN>/boot.ipxe
```

这个域名必须同时满足两点：

1. 与 Go 侧配置文件（`deploy/lober.yaml.example` 的 `gateway.gateway_base`）里
   域名部分一致——两处不一致的后果是 iPXE 能拿到 `/boot.ipxe`，但该文件内部
   `chain` 出的下一跳 `${gateway-base}/v1/boot?...` 指向了另一个地址，请求失败。
2. 能被本文件上方「本地 DNS 记录」一节（`address=/<GATEWAY_DOMAIN>/<GATEWAY_IP>`）
   解析到——两个域名占位符必须替换成完全相同的值。

`/boot.ipxe` 本身的响应内容（含 `chain` 目标 URL 的五个变量拼接形态）由
`docs/contracts/http-machine.md` 端点 1 冻结，本文件与本 README 都不重复定义那份
契约，只保证 DHCP 层面把机器正确指向这一个 URL。
