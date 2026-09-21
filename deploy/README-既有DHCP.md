# 对接既有 DHCP —— ISC dhcpd 与 Kea 等价配置

> 部署前必跑的校验命令：
>
> ```bash
> dhcpd -t -cf deploy/dhcpd/lober.conf
> kea-dhcp4 -t deploy/kea/lober-dhcp4.json
> ```
>
> **光跑通配置测试模式不够。** 两者都还要在实验环境用真机验证两阶段各走对
> 分支，并抓一次 DHCP DISCOVER 报文确认固件实际上报的 option 93 值与配置里
> 的判定条件一致。本文档标注的每一处「待实测」在部署前都必须核实，不能凭
> 配置测试通过就认为行为正确。

## 这份文档什么时候用

**只在无法使用 dnsmasq 时才用本目录下的配置。** 判据：目标机房已有在运的
DHCP 服务（ISC dhcpd 或 Kea），且不接受再引入一个 DHCP 服务（避免两个 DHCP
响应竞争这类难排查的故障）。

**新部署一律用 dnsmasq 或 Kea，不要选 dhcpd。** ISC dhcpd 已于 2022 年 10 月
EOL，最后维护版本是 4.4.3-P1 / 4.1-ESV-R16-P2，ISC 不再计划发布新版，且不知
有任何完整维护的分支，官方建议迁移到 Kea（SPEC.md 附录 A.12）。本目录对
dhcpd 的支持是「对接既有环境」，不是推荐方案。

三份配置的关系：

| 配置 | 适用场景 |
|---|---|
| `deploy/dnsmasq/lober.conf`（另一 agent 维护，本文档不改动它） | 默认推荐，新部署首选 |
| `deploy/dhcpd/lober.conf`（本目录） | 机房已有在运的 ISC dhcpd，不能引入新 DHCP |
| `deploy/kea/lober-dhcp4.json`（本目录） | 机房已有在运的 Kea，或需要 Kea 的其它特性 |

## 为什么不能照抄

SPEC.md §1.3 的两阶段引导逻辑——非 iPXE 客户端按架构给二进制，已是 iPXE 的
客户端给固定 URL——在三种 DHCP 实现里的**表达机制完全不同**。照抄 dnsmasq
的配置到另两种上不会工作，而失败的表现是「机器毫无反应」，本系统对此零可
观测性（SPEC §0.1）：DHCP 层的失败发生在机器拿到 `/v1/boot` 之前，网关连
请求都收不到，日志里什么都不会留下。

| | dnsmasq | ISC dhcpd | Kea |
|---|---|---|---|
| 机制 | tag 集合 | 条件链（`if` / `elsif`） | `client-classes` 数组 |
| 判定是否互斥 | 否，靠显式反 tag（`tag:!ipxe`）排除 | **是**，`elsif` 天然互斥 | **否，所有 class 独立求值** |
| 阶段区分靠什么 | 反 tag | **条件顺序** | 需在每个 `test` 表达式内显式排除 |
| option 93 的引用方式 | `dhcp-match=set:xxx,option:client-arch,N`，内置支持 | 必须先 `option client-arch code 93 = unsigned integer 16;` 自定义声明 | 内置标准选项，符号名 `client-system` 或数值 `option[93]` |
| 未知架构落空时的可观测性 | 与 dhcpd 相同问题（需自行核实是否留日志，本轮未覆盖 dnsmasq 细节） | 显式 `else` 分支打日志，**能查** | 不命中任何 class，**不留日志**，比前两者更难定位 |

**第三列（Kea）的「独立求值」是三者中唯一需要特别小心的机制。** dnsmasq 靠
显式反 tag、dhcpd 靠 `elsif` 顺序都能保证「至多一个分支命中」；Kea 的
`client-classes` 默认没有这层保证——一台 UEFI 机器跑起 iPXE 后再次发
DISCOVER，它可能同时满足 `ipxe` class（option 77 是 iPXE）与 `efi64` class
（option 93 是 7）两者的 `test`，若两者都设了 `boot-file-name`，实际生效的
是哪个取决于 Kea 的 class 求值与 option 合并顺序——这条语义 SPEC 设计不依赖
（附录 B 待核实项），`deploy/kea/lober-dhcp4.json` 用重复反条件规避了它。

## ISC dhcpd 的三个要点

1. **条件顺序是硬要求：`user-class` 判定必须在架构判定之前。**
   顺序颠倒的后果是已经跑起 iPXE 的客户端再次匹配到架构分支、又拿到一次
   iPXE 二进制——**直接构成无限引导循环**（SPEC §10 失败模式 3）。dhcpd 没
   有 tag 机制，两阶段互斥完全依赖 `elsif` 的书写顺序，所以这条比 dnsmasq
   那边写反 `tag:!ipxe` 更致命：dnsmasq 写反顺序至多是多一层 tag 判断，
   dhcpd 写反顺序是直接吞掉分支进入无限循环。

2. **`option 93` 必须显式声明**（`option client-arch code 93 = unsigned
   integer 16;`），dhcpd 没有该选项名的内置定义，不声明就无法在
   `if`/`elsif` 里引用它。**但比较字面量要写 `00:07` 这种双字节形式，不要
   写成十进制整数 `= 7`**——dhcpd 按原始 option 数据逐字节比较，双字节形式
   是社区通行写法。**这一点在 `deploy/dhcpd/lober.conf` 里明确标注为待实测
   项**：实施前必须用 `dhcpd -t` 与真机各验一遍，写成十进制的行为不要凭空
   假设。同样待验的是 `elsif` 条件里的 `or`（用在 x64 UEFI 分支，7 与 9 都
   要匹配）求值是否如预期。

3. **未知架构必须落到显式 `else` 并打日志。** 不写 `else` 时客户端拿不到
   `filename`、固件静默跳过网络引导，与「DHCP 没响应」的现场表现完全一
   致——那条日志是区分二者的唯一依据。**绝不要在 `else` 里兜底发某个二
   进制**：给错架构的二进制会让固件加载失败或挂死，比不引导更难查（SPEC
   §10 失败模式 4）。

## Kea 的四个要点

1. **反条件宁可写重复也不要省。** 把「不是 iPXE」的反条件（`not
   (substring(option[user-class].hex, 0, 4) == 'iPXE')`）直接 AND 进每个
   架构 class 的 `test`，让至多一个 class 能命中——与 dnsmasq 的
   `tag:!ipxe` 语义对齐。看着笨，但它换来的是**配置正确性不依赖 Kea 的
   class 求值与 option 合并顺序**。**若确认 `member()` 函数可用，可以简化
   成 `not member('ipxe')`**，前提是 `ipxe` class 的求值确实先于架构 class
   完成（Kea 文档称 class 按配置中定义的顺序处理，故 `ipxe` 需放在数组首
   位，`deploy/kea/lober-dhcp4.json` 里已经这样排列）——**这条简化必须先用
   `kea-dhcp4 -t` 加真机验证再启用，不能直接照抄**（SPEC 附录 B）。

2. **option 93 在 Kea 里的符号名是 `client-system`，不是 dhcpd 配置里的自
   定义名 `client-arch`。** `deploy/kea/lober-dhcp4.json` 建议先用数值形式
   `option[93].hex` 引用——符号名对不上时 Kea 直接启动失败，比静默不匹配
   更容易发现；确认目标 Kea 版本支持 `client-system` 符号名后，可换成
   `option[client-system].hex` 提升可读性，但这不是必须的。

3. **无需显式声明 option 93。** Kea 内置标准选项定义（RFC 4578
   client-architecture），这是与 dhcpd 的实质差异：dhcpd 必须先自定义声明
   才能引用，Kea 在 `client-classes` 的 `test` 表达式里直接写 `option[93]`
   就能用。

4. **未知架构不会命中任何 class，也就拿不到 `boot-file-name`**——行为与
   dhcpd 的显式 `else` 等效，**但 Kea 不会为此留日志**。需要日志时要靠
   `Dhcp4.hooks-libraries` 挂钩子库，或把 `Logging.loggers` 的
   `severity` 调到 `DEBUG` 观察未分类的 DISCOVER 报文，否则「机器毫无反
   应」这一症状在 Kea 下比 dhcpd 更难定位。`deploy/kea/lober-dhcp4.json`
   未预置钩子库配置，因为具体钩子库路径依赖目标系统的 Kea 安装方式，部署
   时按需补充。

## 0x0007 的实战踩坑记录

**有用户报告 UEFI 固件明确发 option 93 = `0x0007`，但 Kea 仍回了 legacy
BIOS 的引导文件**（SPEC.md 附录 A.15，2-1 弱确认）——与 SPEC §1.3 描述的
「只配 9 漏配 7」是同一类错误的另一种表现形式。

**调试第一步永远是抓 DISCOVER 报文看固件实际上报什么，不要相信配置文件里
的注释。** 配置文件里写着 `# x64 UEFI, arch=7` 不代表这台机器真的发了 7；
也不代表 Kea/dhcpd 真的按你以为的方式匹配到了这一行。核实手段：

```
tcpdump -i <装机VLAN网卡> -n port 67 or port 68 -w discover.pcap
```

抓到后用 Wireshark 或 `tcpdump -vv` 展开 DHCP option 93 的原始字节，与配置
文件里的判定值逐一核对。

## 三种实现的共同收敛点

**不论用哪一种 DHCP 实现，第二阶段拿到的都是同一个 `http://boot.lober.
internal/boot.ipxe`，网关对此零感知**（SPEC §4.7）。三份配置的差异**全部
止于本文档覆盖的范围**——它们不影响 Go 代码的任何一行。换 DHCP 实现只需换
这一层配置，不需要改网关。

**另一条边界：DHCP 保证不了「目标 OS 正确」，只能保证「固件模式正确」。**
DHCP 视野里只有 option 93（架构）与 option 77（user-class）两个字段，看不
到任务要装什么 OS——目标 OS 的引导形态由网关在第二层决定（SPEC §4.5），两
层的一致性由网关校验（SPEC §4.6）。

## 与 dnsmasq 版本的交叉核对

`deploy/dhcpd/lober.conf` 与 `deploy/kea/lober-dhcp4.json` 的以下取值必须
与 `deploy/dnsmasq/lober.conf`（另一 agent 维护，本文档只交叉引用，不修改
该文件）保持一致——若 dnsmasq 版本改了这些值，本目录两份配置要同步改：

- **阶段二 URL 的路径与形态一致，但占位符写法不同，部署时需统一**：
  `deploy/dnsmasq/lober.conf` 用的是 `http://<GATEWAY_DOMAIN>/boot.ipxe`
  （`<GATEWAY_DOMAIN>` 是留给部署时填的占位符）；本目录两份配置为便于
  阅读，直接借用 SPEC.md §1.3 例子里的示例域名写成
  `http://boot.lober.internal/boot.ipxe`。**两者本质上是同一件事**——路径
  都是 `/boot.ipxe`，都不带查询参数（真正带 `sn`/`mac`/`uuid`/`arch`/
  `plat` 五个变量的是网关渲染出的下一跳 `/v1/boot` URL，不是这一行）。
  **部署时三份配置必须填同一个真实域名**，不能 dnsmasq 填了真域名、
  本目录两份配置还留着示例域名没改。
- 架构分流的四个 option 93 判定值与对应二进制文件名，三份配置完全一致：
  `0 → undionly.kpxe`、`6 → snponlyx32.efi`、`7 与 9 → snponly.efi`、
  `11(0x0b) → snp-arm64.efi`
- 装机 VLAN 网段、网关、地址池：`deploy/dnsmasq/lober.conf` 全部用
  `<PXE_VLAN_SUBNET_START>` 等占位符表达；本目录两份配置为便于阅读直接
  借用 SPEC.md §1.3 例子里的示例网段 `10.10.0.0/24`。**三份配置在部署时
  必须填同一个真实网段**，不能各写各的。

## 验证清单

部署前逐项确认：

- [ ] `dhcpd -t -cf deploy/dhcpd/lober.conf` 通过
- [ ] `kea-dhcp4 -t deploy/kea/lober-dhcp4.json` 通过
- [ ] 真机验证：dhcpd 环境下，非 iPXE 客户端拿到架构专属二进制，已是 iPXE
      的客户端拿到 `boot.ipxe` URL，两阶段都不出现无限循环
- [ ] 真机验证：Kea 环境下，一台 UEFI 机器不会同时命中 `efi64` 与 `ipxe`
      两个 class（即两阶段各只有一个 `boot-file-name` 生效）
- [ ] 两个环境各抓一次 DISCOVER 报文，确认固件上报的 option 93 值与配置里
      的判定条件一致（尤其核实是 7 还是 9，或两者都需要）
- [ ] `deploy/dhcpd/lober.conf` 中 `00:07` 双字节字面量的匹配行为已用真机
      验证（不是凭本文档假设）
- [ ] 若启用了 Kea 的 `member('ipxe')` 简化写法，已用真机验证其行为与重复
      反条件写法一致
- [ ] 本目录两份配置里的网段、网关、域名、TFTP 二进制文件名等占位值已全部
      改为目标机房真值，且与 `deploy/dnsmasq/lober.conf`、
      `deploy/nginx/lober.conf` 一致

> `dhcpd -t` 与 `kea-dhcp4 -t` 在本次编写环境（Windows 主机）里都不可用，
> 以上校验命令未在本机执行，均需在目标 Linux 主机上补做。本文档与两份配置
> 文件已完成的是逐条对照 SPEC.md §1.3.1/§1.3.2 与计划任务 7b 的**静态自
> 检**：必需指令是否齐全、option 93 判定值是否覆盖 0/6/7/9/11、条件顺序
> （dhcpd 的 user-class 分支是否确实在架构分支之前）、Kea 的反条件是否
> 在每个架构 class 里都存在、JSON 括号是否配平（已用脱注释后的 JSON 解析
> 验证）。
