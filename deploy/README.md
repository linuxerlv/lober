# Lober 部署指南（面向机房运维）

> **这份文档写给谁：** 手上有一台已装好 Linux 的服务器、一批待装的裸机、一个内网、
> 没有外网访问的运维。不是写给开发看的——代码层面的设计理由查
> `docs/superpowers/specs/SPEC.md` 与 `docs/contracts/`，本文档只回答
> 「先做什么、后做什么、哪里最容易配错、怎么确认部署对了」。
>
> 依据：`docs/superpowers/plans/2026-09-20-lober-mvp-09-integration.md` 任务 10、
> `SPEC.md` §0.1（范围边界）、§1.4（nginx）、§1.6（反向代理两个陷阱）、
> 附录 B（待实机核实项）；`docs/contracts/http-machine.md`、
> `docs/contracts/filesystem.md`。
>
> **本文档不重复展开各配置文件已有的注释**——每个配置文件顶部都写了自己的
> 部署前校验命令与占位符说明，本文档只负责顺序、决策点、和一份可勾选的
> 实机核对清单。文件内容如与本文档描述不一致，**以文件实际内容为准**
> （本文档写作时逐一 Read 过下方列出的全部文件，若后续这些文件被修改，
> 本文档可能需要同步更新）。

---

## 目录

1. [部署顺序（八步）](#1-部署顺序八步)
2. [三处必改的配置](#2-三处必改的配置)
3. [TFTP 二进制：放哪四个文件](#3-tftp-二进制放哪四个文件)
4. [选哪套 DHCP 配置](#4-选哪套-dhcp-配置)
5. [实机核对清单](#5-实机核对清单)
6. [验证部署成功的标志](#6-验证部署成功的标志)
7. [范围边界：出问题去哪查](#7-范围边界出问题去哪查)

---

## 0. 前提条件（动手前先确认这三条）

1. **双网卡（或双 VLAN 接口）。** 本机需要同时接入两个网络：
   - **装机 VLAN**——待装裸机所在的网络，无认证、机器面协议本身不设防线。
   - **管理网**——运维访问控制台的网络。
   两者监听地址必须是不同网段的具体 IP（见下方[第 2 节](#2-三处必改的配置)），
   不能靠同一个网卡的同一个地址区分。

2. **装机 VLAN 必须真正隔离，不路由到办公网或其它信任网络。** 这是 MVP
   阶段的威胁模型前提（SPEC §1.5：MVP 明文 HTTP，机器面端点无认证）。
   如果装机 VLAN 能路由到办公网，那么任何接入办公网的设备都能访问机器面
   的无认证端点——这不是本系统的 bug，是缺了这一条前提之后威胁模型直接
   不成立。**这条不是本系统能检测或校验的，需要运维在网络层面确认。**

3. **nginx 编译带线程支持（`--with-threads`）。** `deploy/nginx/lober.conf`
   的制品分发用 `aio threads` 做异步 IO（否则 4 GB 镜像的 sendfile 会阻塞
   worker 进程），这个编译选项必须在离线装包**之前**确认好——内网环境
   补装不上缺失的编译选项，届时只能整个重新编译或换发行版自带的包。
   核对命令见[第 5 节实机核对清单](#5-实机核对清单)。

---

## 1. 部署顺序（八步）

**为什么有顺序：** 主要的坑是 nginx 与 lober 服务谁先起——nginx 先起会对
所有请求返回 502（`deploy/systemd/nginx.service.d/after-lober.conf` 已经
处理了这个顺序问题，但装的时候仍要按下面顺序走，不要图快调换）。另一个
坑是 DHCP 配置里写死引用的 TFTP 文件，如果这一步之前没放好文件，机器上电
测试时会拿到「文件不存在」的 TFTP 错误——这个错误本身不影响后续步骤，
但会浪费一次上电测试的时间。

| 步骤 | 做什么 | 对应文件 |
|---|---|---|
| 1 | **建用户与目录（含权限）** | 见下方「步骤 1 详解」 |
| 2 | **装二进制与配置** | `cmd/lober` 的构建产物、`deploy/lober.yaml.example` 改名部署 |
| 3 | **跑校验模式** | `lober -config <路径> -check`，见下方「步骤 3 详解」 |
| 4 | **放 iPXE 二进制** | 见[第 3 节](#3-tftp-二进制放哪四个文件) |
| 5 | **装 DHCP 配置**（dnsmasq 或既有 dhcpd/Kea） | 见[第 4 节](#4-选哪套-dhcp-配置) |
| 6 | **装 nginx 配置** | `deploy/nginx/lober.conf`、`lober-http-snippet.conf` |
| 7 | **装 systemd 单元** | `deploy/systemd/lober.service`、`nginx.service.d/after-lober.conf` |
| 8 | **配防火墙** | 见下方「步骤 8 详解」 |

**顺序里的关键决策点：** 步骤 3（校验模式）必须排在改完配置**之后**、
起服务**之前**——这一步的价值是让配置错误在这里失败，而不是等 systemd
起服务后第一个机器请求打进来才发现。步骤 4/5（TFTP 二进制、DHCP）与
步骤 6/7（nginx、systemd）互相独立，可以并行准备，但**步骤 7 的
systemd 单元装完先不要 `systemctl enable --now`**，等步骤 8 防火墙配好、
且已确认[第 6 节](#6-验证部署成功的标志)的验证命令能跑之后再正式启用，
避免装机 VLAN 上的裸机在防火墙规则还没生效的窗口期里意外访问到管理网
监听地址。

### 步骤 1 详解：建用户与目录

按 `docs/contracts/filesystem.md`「约定：权限」一节：

```bash
# 专用系统用户，不用 root（对应 lober.service 的 User=/Group=）
useradd --system --no-create-home --shell /usr/sbin/nologin lober-svc

# 三个可写目录，权限按 filesystem.md 的约定表：
install -d -m 750 -o lober-svc -g lober-svc /var/lib/lober/db
install -d -m 750 -o lober-svc -g lober-svc /var/lib/lober/log
install -d -m 750 -o lober-svc -g lober-svc /srv/lober/artifacts
```

`/srv/lober/` 需要让 lober 进程与 nginx worker 两者都能访问——若两者以
不同系统用户运行（推荐），典型做法是把 nginx 的运行用户加入 `lober-svc`
组，目录保持 `750` 让组内可读。**nginx worker 不需要写权限**——制品是
只读静态文件，给 nginx 写权限没有功能收益，只会扩大攻击面。

### 步骤 3 详解：跑校验模式

```bash
/opt/lober/bin/lober -config /etc/lober/lober.yaml -check
```

这条命令等价于 `deploy/systemd/lober.service` 里 `ExecStartPre` 那一行
（校验模式是专门为 `ExecStartPre` 设计的运行模式）。**手动跑一次确认通过
之后才继续下一步**，不要假设配置写对了就直接起服务。

### 步骤 8 详解：配防火墙

配置文件层面的信任域隔离（nginx 的 `listen` 写具体 IP、Go 侧
`Validate()` 拒绝通配地址）是第一层。**防火墙是第二层，是纵深防御，
不是可选项**——即便配置文件全部正确，仍应在主机防火墙加规则：

```bash
# 示例（按实际防火墙工具调整；下面用 nftables 语法示意）：
# 拒绝装机 VLAN 网段访问管理网监听端口
nft add rule inet filter input ip saddr <装机VLAN网段> tcp dport <console.listen 的端口> drop
```

这条规则的价值在于：如果将来有人不小心把 `console.listen` 改成了装机
VLAN 上的地址（或 nginx 配置被改动），防火墙仍能拦住，不至于两层防线
同时失效。

---

## 2. 三处必改的配置

以下三处在 `deploy/lober.yaml.example` 里都标了 `CHANGE-ME`，但值得在
这里单独列出，因为它们的失败模式**都是「不报错，只在特定条件下才暴露」**
——这类配置错误最容易被忽略。

| # | 配置项 | 文件与字段 | 改错的后果 |
|---|---|---|---|
| 1 | **两个监听地址的 IP** | `gateway.listen`、`console.listen`（`internal/config/config.go`） | 写成通配地址（`0.0.0.0:port`）时，Go 侧 `Validate()` 会**直接拒绝启动**——这是好事，配置错误在这里就暴露。但如果两个监听地址虽然都是具体 IP、却选错了网段（例如把 `console.listen` 也绑到了装机 VLAN 的地址上），`Validate()` 校验不出这一层语义错误——它只查「是不是通配」，不查「是不是选对了网段」。后果是装机 VLAN 上的裸机能直接访问控制台 API，而控制台本身设计为有认证但暴露在错误网段就是信任域被合并。**这条错误只能靠人核对网段，机器测不出来**——见[第 6 节](#6-验证部署成功的标志)的交叉验证命令。 |
| 2 | **基址 URL 的域名** | `gateway.gateway_base`、`gateway.artifact_base` | 留空会被 `Validate()` 拒绝启动。但若填了值却与 DHCP 配置里 `dhcp-boot=tag:ipxe,http://<域名>/boot.ipxe` 的域名不一致，**两处都能通过各自的校验、服务也能正常启动**——错误只会在机器实际引导到 `/boot.ipxe` 之后的下一跳 `chain` 命令时才暴露：iPXE 拿到 `/boot.ipxe`，但文件内部渲染出的 `${gateway-base}/v1/boot?...` 指向了另一个地址，请求失败，现场表现是「iPXE 启动了，但卡在第一条 chain 命令上报错」。三处必须填同一个域名：`gateway.gateway_base`/`artifact_base`、`deploy/dnsmasq/lober.conf` 的 `<GATEWAY_DOMAIN>`（两处出现，DNS 记录与 dhcp-boot 各一次）、以及若走既有 DHCP 对接，`deploy/dhcpd/lober.conf` 与 `deploy/kea/lober-dhcp4.json` 里对应的域名占位符也要同步改成一致的值（见 `deploy/README-既有DHCP.md`「与 dnsmasq 版本的交叉核对」一节）。 |
| 3 | **控制台 token 密钥** | `console.token_secret` | 留空时 Go 进程会在**每次启动**生成一个随机密钥——这是本文件里唯一「留空也能正常启动，后果最隐蔽」的一类字段。后果：每次重启服务（无论是运维手动重启、systemd 因 `Restart=on-failure` 自动重启、还是滚动升级），全部已登录用户的 access token 立即失效，需要重新登录；若正好有运维在盯着任务详情页的 SSE 实时日志，连接会断。**生产部署必须显式填一个固定的随机密钥**（例如 `openssl rand -base64 32` 生成）并妥善保管，不要提交进版本控制。 |

**这三处任何一处配错，服务大概率仍然「看起来正常启动」**——这正是为什么
值得单独列出来反复强调，而不是假设运维看一眼配置样例的注释就会记住。

---

## 3. TFTP 二进制：放哪四个文件

TFTP 根目录需要放置 BIOS/IA32 UEFI/x64 UEFI/ARM64 UEFI 四个 iPXE 官方
预编译二进制。**完整清单、文件名对照表、气隙环境下的获取与 sha256 校验
流程见 `deploy/dnsmasq/README.md`「TFTP 根目录该放哪四个文件」一节**，
本文档不重复展开。

只强调两点决策性的信息：

- **ARM64 那个文件 MVP 阶段可以不放**，但 dnsmasq 配置里对应的匹配规则
  建议保留（缺文件时 TFTP 日志至少会留一条「请求了但文件不存在」的
  记录；缺规则则 DHCP 层面完全无响应，排查成本高一个数量级）。
- **四个文件的 sha256 必须在下载后立即计算并记录**（`deploy/dnsmasq/README.md`
  末尾有一张待填表格）——**在这四个值填入之前，不要认为 TFTP 二进制已经
  过校验**。带进内网前后各校验一次，确认传输过程没有损坏或被替换。

---

## 4. 选哪套 DHCP 配置

**新部署首选 `deploy/dnsmasq/lober.conf`。**

只有当机房已有在运的 DHCP 服务（ISC dhcpd 或 Kea）、且不接受再引入一个
DHCP 服务时，才改用 `deploy/dhcpd/lober.conf` 或 `deploy/kea/lober-dhcp4.json`
对接既有环境——**新部署不要选 dhcpd**，它已于 2022 年 10 月 EOL，官方
建议迁移到 Kea。三种实现的机制差异、为什么不能照抄 dnsmasq 配置到另
两种上、以及每种实现各自的校验命令，见 `deploy/README-既有DHCP.md`
（本文档不重复展开该文档已有的内容）。

**判断依据只有一条：** 机房是否已有不能替换的 DHCP 服务。是→查
`deploy/README-既有DHCP.md`；否→直接用 dnsmasq。

---

## 5. 实机核对清单

**这是本文档最有价值的部分。** SPEC 附录 B 列了一批「基于工程经验但
未取得来源验证」的判断，本节把其中属于**部署期一次性核对**（核完即
固定，不进 CI、不做运行期检测，SPEC §0.1）的项目变成可勾选的步骤。

> **核对完成后，把结果记进一份带日期的文件**（例如
> `docs/deploy-verified-<日期>.md`，本文档不预先创建这个文件，由首次
> 实际部署的运维在核对完成后建立）——各架构的 option 93 实测值、iPXE
> 变量的实际可用性、多网卡机器的引导网卡命名。**下次部署直接照这份走，
> 不用再核。**

### 5.1 option 93 各架构实机取值（最高优先级）

**调试第一步永远是抓包看固件实际上报什么，不要相信配置文件里的注释。**
SPEC 附录 A.15 记录过真实案例：有用户报告 UEFI 固件明确发了 option 93 = 7，
但 DHCP 侧仍然回了 legacy BIOS 的引导文件——原因是配置本身写错了，不是
固件行为异常。这条核对存在的意义就是防止同样的事发生在这套部署上。

- [ ] **抓包命令**（在跑 DHCP 服务的主机上，对着装机 VLAN 网卡）：

  ```bash
  tcpdump -i <装机VLAN网卡> -vvv -s0 -w /tmp/pxe-discover.pcap port 67 or port 68
  ```

- [ ] **让每种固件各上电一次**走网络引导（BIOS 引导顺序需已指向网络
  引导——这一步本身在 SPEC §0.1 划定的范围外，需运维在 BMC 或现场手动
  确认，不是本系统能推动的）。
- [ ] **中断抓包，回放**：`tcpdump -r /tmp/pxe-discover.pcap -vv` 或用
  Wireshark 打开，找 DHCP DISCOVER 报文，定位 `option 93`
  （Client System Architecture Type）字段的原始字节。
- [ ] **看什么输出**：核对实际数值是否落在配置里已配的 `0/6/7/9/11`
  集合内——
  - BIOS 机器应为 `0`
  - IA32 UEFI 应为 `6`
  - **x64 UEFI 应为 `7`**（若看到 `9` 也不算错，两者归同一 tag/class）
  - ARM64 UEFI 应为 `11`（**国产 ARM 平台如飞腾/鲲鹏是否规范地报 11，
    是 SPEC 附录 B 明确标注的待核实项，MVP 阶段若没有真实 ARM64 硬件，
    这一条留到有硬件时补做**）
- [ ] **不对怎么办**：如果某型号固件上报的值不在 `0/6/7/9/11` 集合内，
  需要在 dnsmasq（或 dhcpd/Kea）配置里补一条对应的 `dhcp-match`/class
  规则，并同步在 TFTP 目录准备好对应架构的二进制——**绝不要图省事把
  未知架构临时归到一个已有 tag 上**，那等价于给错架构的二进制，后果
  是固件加载失败或挂死，比「毫无反应」更难查（`deploy/dnsmasq/README.md`
  已详细说明这条权衡）。

### 5.2 引导链：两种固件各自拿到正确的二进制

- [ ] **BIOS 机器**上电后，TFTP 日志（`dnsmasq` 已开 `log-dhcp`；既有
  dhcpd/Kea 按各自的日志配置）显示它请求并成功取到 `undionly.kpxe`。
- [ ] **UEFI x64 机器**取到 `snponly.efi`（不是 `undionly.kpxe`——如果
  一台明确是 UEFI 的机器拿到了 BIOS 二进制，说明 option 93 判定或
  tag/class 配置有问题，回到[5.1](#51-option-93-各架构实机取值最高优先级)重新核对）。
- [ ] **iPXE 起来后第二次 DHCP 请求拿到的是脚本 URL（`http://<域名>/boot.ipxe`），
  而不是又一个二进制文件名。** 这一条如果没通过，现场表现是无限引导
  循环（SPEC §10 失败模式 3）——机器会反复加载 iPXE、反复重新走 DHCP，
  永远到不了网关。**不对怎么办**：检查 user-class（option 77）判定规则
  是否存在、且（若是 dhcpd）判定顺序是否确实排在架构判定**之前**——
  dhcpd 靠 `elsif` 顺序保证互斥，顺序颠倒是这条失败模式最常见的成因。
  **服务端判定逻辑本身（非真实固件）已在 WSL debian-13 上用隔离 veth +
  真实 dhclient 核对通过**（2026-09-22，dhcpd 与 Kea 均测试了
  `client-arch=7` 与 `user-class=iPXE` 同时出现的场景，均正确返回
  `boot.ipxe` URL 而非架构专属二进制，详见
  `docs/real-machine/01-配置语法核对.md` 第 2.1/3.2 节）——**这条勾选框
  仍不打勾**，因为它验证的是「真实固件」这一端，本轮测试用的是可控客户端
  模拟同样的 option 组合，两者不是同一件事，仍需真实 iPXE 固件跑一次
  完整验证。
- [ ] **未知架构的机器拿不到任何引导文件名，且日志里有这次 DISCOVER 的
  记录（含它上报的 client-arch 原始值）。** 这一条验证的是「静默失败」
  与「配置遗漏」的区别——如果连日志都没有记录到这次请求，问题可能在
  网络链路或 VLAN 划分，不在 DHCP 配置里。

### 5.3 iPXE 变量拼写与多网卡引导网卡

- [ ] **五个变量能正确展开**：在 `/boot.ipxe` 渲染出的第一条 `chain`
  命令里，`${serial}`、`${net0/mac}`、`${uuid}`、`${buildarch}`、
  `${platform}` 五个变量在实际请求到达网关时都有非空值（可在 nginx
  access log 的 `boot` 格式里看到完整 `$request_uri`，五个 query 参数
  应该都有值，不是变量名字面文本本身）。
- [ ] **多网卡机器上 `net0` 是否为实际引导网卡。** 这条如果不成立，
  影响会传导到 SN/MAC 双因子校验（`docs/contracts/http-machine.md`
  端点 2 的 SN+MAC 双因子硬要求）——如果 `net0` 对应的 MAC 不是机器
  实际用来发起 PXE 请求的那块网卡，Go 侧记录的 MAC 与固件实际上报的
  MAC 会不一致，机器可能拿不到任务或匹配到错误的任务。**不对怎么办**：
  若确认某型号机器的引导网卡不是 `net0`（比如某些多网卡服务器的固件
  枚举顺序与操作系统枚举顺序不一致），需要在首跳脚本
  （`GET /boot.ipxe` 的渲染，见 `docs/contracts/http-machine.md` 端点 1）
  里改用固件实际报告的那个网卡变量，这属于代码改动而非部署配置改动，
  需要反馈给负责网关渲染的模块。

### 5.3b nginx 编译带 --with-threads（离线装包前就要确认）

**这条必须在离线装包之前核对，不是装完再检查。** 内网环境一旦发现缺失
这个编译选项，补装不上——只能整个重新编译 nginx 或换一个自带此选项的
发行版包，返工成本远高于装机前花两分钟确认。

- [x] **已在 WSL debian-13 上核对通过**（2026-09-22，命令：`nginx -V`）：
  Debian trixie 的 `nginx` 核心包（`1.26.3-3+deb13u9`，`nginx-core`/
  `nginx-full`/`nginx-light`/`nginx-extras` 四个变体共享同一个核心二进制，
  与选哪个变体无关）**同时带有 `--with-threads` 与
  `--with-http_secure_link_module` 两个编译选项**，`sendfile`+`aio threads`
  与 Phase 2 的 `secure_link` 均可正常工作。详见
  `docs/real-machine/01-配置语法核对.md` 第 5 节。**此结论只对 Debian
  trixie 成立**——若目标机房用其它发行版或其它 Debian 版本，仍需按下面的
  命令重跑一次核对（秒级完成，不构成负担）。
- [ ] **命令**：

  ```bash
  nginx -V 2>&1 | grep -- '--with-threads'
  ```

- [ ] **看什么输出**：命令有输出（`grep` 找到匹配行）即通过——
  `nginx -V` 的输出会把全部编译参数打在同一行，能 grep 到
  `--with-threads` 说明这个选项确实编译进去了。
- [ ] **不对怎么办**：没有输出说明当前这个 nginx 二进制不支持
  `aio threads`，`deploy/nginx/lober.conf` 里制品分发 location 的
  `aio threads;` 指令会导致 `nginx -t` 报错或（视版本而定）静默不生效、
  退化为同步 IO 阻塞 worker 进程。此时需要在**离线装包之前**换一个
  带此编译选项的 nginx 包，或找发行版对应的完整功能构建——不要等
  装完在内网发现问题再回去找包。

### 5.4 内核参数

- [ ] Rocky（或其它 RHEL 系）应答文件里内嵌的两个内核参数（仓库地址、
  kickstart URL 相关参数）能被 anaconda 正确取用——用一次真实（或
  高保真本地环境如 QEMU）安装验证，不要只看应答文件渲染出的文本
  「看起来对」。

### 5.5 文件路径一致性

- [ ] **校验模式无告警**：

  ```bash
  /opt/lober/bin/lober -config /etc/lober/lober.yaml -check
  ```

  这条命令本身通过（退出码 0）只说明配置格式合法，**还需要看启动日志
  是否有制品缺文件的告警**（`docs/contracts/filesystem.md`「三条硬约束」
  第 3 条：启动时遍历全部 `ready` 状态的制品集，逐一确认文件真实存在，
  缺失只告警不阻止启动）——校验通过但日志里有告警，同样需要处理。

- [ ] **随机挑一个制品 URL，用 curl 实际取得到**：

  ```bash
  curl -sI http://<机器面域名>/artifacts/<某个制品集ID>/vmlinuz
  ```

  期望 `HTTP/1.1 200 OK` 且 `Accept-Ranges: bytes`。如果拿到 404，
  说明 nginx 的 `alias` 路径与 Go 侧签发 URL 用的相对路径不一致
  （`docs/contracts/filesystem.md` 硬约束第 1 条：两者必须逐字符一致，
  这类不一致在测试环境常因路径碰巧对上而不暴露，只在某个特殊命名的
  制品集上才炸）。

---

## 6. 验证部署成功的标志

按 `docs/contracts/http-machine.md`，运维能用 curl 模拟一次机器请求
确认链路通。**此时还没有任何任务**，预期响应是固定的三行 `no_task`
载荷（该文档「约定 2」冻结的原文，机器侧看到这个响应会执行
`sanboot` 回本地盘，或在空盘机器上继续 PXE 轮询——两种行为都正确）：

```bash
curl -s "http://<机器面域名>/v1/boot?sn=TEST-SN&mac=00:11:22:33:44:55&uuid=00000000-0000-0000-0000-000000000000&arch=x86_64&plat=efi"
```

（`arch`/`plat` 的取值必须是 iPXE 实际会发的字符串——`arch=x86_64` 对应
`${buildarch}`，`plat=efi` 对应 `${platform}`，两者由
`internal/core/model.ParseIPXEBuildArch`/`ParseIPXEPlatform` 解析，取值
域目前只认 `x86_64`/`arm64` 与 `pcbios`/`efi`。填错值不会导致这条命令
失败——解析失败会被网关当作「查不到匹配主机」处理，同样返回下面的
`no_task` 载荷，所以这条验证命令本身对 `arch`/`plat` 的具体取值不敏感，
但如果后续要用这条命令模拟真实机器请求做更深入的排查，取值就必须准确。）

**期望输出，逐字符**（不含行首/行尾多余空白，末行换行符可有可无）：

```
#!ipxe
echo lober: no task for TEST-SN
sanboot --no-describe --drive 0x80
```

如果看到别的内容（比如连接失败、超时、或非 200 状态码），说明网关
本身没起来或 nginx 反代配置有问题——回到[第 1 步部署顺序](#1-部署顺序八步)
检查 lober.service 是否已成功启动。**这个端点始终返回 200**（这是
`docs/contracts/http-machine.md`「约定 1」不可违反的约定），因此
「连接失败/超时」与「返回内容不对」是两类完全不同的问题，前者是网络
或进程没起来，后者是渲染逻辑或路由配置有问题。

**另一条必做的交叉验证：信任域没有被静默合并**——从装机 VLAN 的地址
访问控制台监听地址必须失败：

```bash
# 在装机 VLAN 内的一台机器（或该网段内的任意可达主机）上执行：
curl -sS --connect-timeout 3 "http://<console.listen 的 IP>:<端口>/" && echo "!!! 危险：装机VLAN能访问控制台，信任域被合并 !!!" || echo "OK：装机VLAN访问控制台被拒绝"
```

期望是连接被拒绝或超时（`curl` 非零退出码）。如果这条命令能连上，
说明[第 2 节](#2-三处必改的配置)第 1 条列出的风险已经发生，需要立即
检查 `console.listen` 绑的网段、nginx 人面 `server` 块的 `listen`
是否确实绑在管理网、以及防火墙规则是否已生效（[步骤 8](#步骤-8-详解配防火墙)）。

---

## 7. 范围边界：出问题去哪查

**SPEC §0.1 明确划定了系统范围：上电、DHCP、拿到 iPXE 二进制这一段
在系统范围外。** 这不是本系统的疏漏，是刻意的设计边界——这一段的
全部判据都在固件与 DHCP 里，本系统对这一段既无可观测性也无干预手段。

**这一点必须写清楚，因为不写清楚，运维会在错误的地方找问题：**

| 机器的现场症状 | 正确的排查入口 | **不是** 这里 |
|---|---|---|
| 机器上电后在 PXE 里转圈，或屏幕上毫无反应 | **BMC 控制台**（看固件层面的引导顺序、网络链路状态）与**DHCP/TFTP 日志**（看这台机器的 MAC 有没有发起过 DISCOVER、有没有收到 OFFER、TFTP 有没有收到取文件请求） | 控制台任务列表——这个阶段任务甚至可能还不存在，控制台里什么都看不到，不是「查不到」等于「系统坏了」 |
| 机器卡在 iPXE 的 `chain` 命令上报错 | 检查[第 2 节](#2-三处必改的配置)第 2 条的域名一致性问题，以及网络层面装机 VLAN 到网关地址的连通性 | 同上 |
| 机器已经拿到装机菜单、但选了某个选项后没反应 | 这已经在系统范围内了——查控制台的任务详情页与事件时间线 | （范围内，控制台是对的入口） |

**判断「在不在范围内」的简单标志：** 如果机器已经成功请求过
`/v1/boot` 并被系统记录（能在控制台任务列表或事件表里看到这台机器的
`SN`/`MAC`），问题就在范围内，查控制台。如果控制台里完全找不到这台
机器的任何痕迹，问题在范围外，查 BMC 与 DHCP/TFTP 日志——**这不是
系统的可观测性缺陷，是这段本就不产生任何系统内记录的设计决定**
（`no_task` 响应本身也不留任何系统内痕迹，唯一留痕是 nginx access
log，见 `docs/contracts/http-machine.md`「约定 2」）。

---

## 附：本文档交叉引用的文件（供快速定位，不代表本文档拥有这些文件）

- `deploy/nginx/lober.conf`、`deploy/nginx/lober-http-snippet.conf`
- `deploy/dnsmasq/lober.conf`、`deploy/dnsmasq/README.md`
- `deploy/dhcpd/lober.conf`、`deploy/kea/lober-dhcp4.json`、`deploy/README-既有DHCP.md`
- `deploy/systemd/lober.service`、`deploy/systemd/nginx.service.d/after-lober.conf`
- `deploy/lober.yaml.example`
- `scripts/mkuser/main.go`（`go run ./scripts/mkuser -user <名字>` 生成
  bcrypt 哈希；`-stdin` 开关仅供自动化测试非交互造用户）
- `internal/config/config.go`（配置结构的权威定义）
- `docs/contracts/http-machine.md`、`docs/contracts/filesystem.md`
- `docs/superpowers/specs/SPEC.md` §0.1、§1.4、§1.6、附录 B
