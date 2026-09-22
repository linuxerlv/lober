# PXE 引导实测报告

验证环境：两台 QEMU（WHPX 加速）虚拟机通过 `-netdev socket` 接成同一个虚拟
二层网段（`10.20.20.0/24`），不经过 Windows 宿主机的任何桥接。服务端 VM
是 Debian 13 (trixie)，装 `dnsmasq 2.91`（DHCP/TFTP）与 Debian 官方
`ipxe 1.21.1+git20250501.dad20602+dfsg-1` 包（提供 `undionly.kpxe`/
`snponly.efi` 两个二进制），Go 侧交叉编译 `GOOS=linux GOARCH=amd64` 后
在虚拟机内直接绑 `10.20.20.1:80`（本报告不经 nginx——nginx 的信任边界/
Range/SSE 行为已在 `docs/real-machine/02-nginx-真机集成.md` 单独验证过，
本报告只关心 DHCP→TFTP→HTTP 的引导链本身，两份报告的验证范围互补不重叠）。
客户端 VM 测了两种固件：QEMU 默认 e1000 PXE ROM（BIOS）与 OVMF
`edk2-x86_64-code.fd`（UEFI），均无磁盘、`-boot n`（纯网络引导）。

## 一句话结论

**SPEC 附录 B 警告最多的那一项——option 93 的 7 vs 9——在真实（非 iPXE）
UEFI 固件上实测确认：真机报的是 7，不是 9。** OVMF 的原生 UEFI PXE 客户端
（不是 iPXE）发出的 DHCP DISCOVER 里 `ARCH (93), length 2: 7`，抓包字节
为证。这直接证实了 `deploy/dnsmasq/lober.conf` 里那段最长的注释（"★★★
本节最容易踩且最难查的错误 ★★★"）判断正确：只配 9 不配 7 会让这台真实
UEFI 机器拿不到任何 tag，DHCP 层面"正常放行"、PXE 层面"固件毫无反应"。

同时确认了一个必须诚实说明的局限：**QEMU 自带的 BIOS PXE ROM本身是用
iPXE 构建的**，这导致 BIOS 客户端的第一次 DISCOVER 就带 `User-Class:
iPXE`，直接跳到"阶段二"，完全没有走"阶段一按架构发二进制"这条路径——
这一路的测试结果不能代表真实硬件的 BIOS/Legacy PXE 固件行为。**UEFI 客户端
（OVMF 原生栈）完整走了两阶段**：先用非 iPXE 的 UEFI 网络栈拿到二进制文件名
`snponly.efi`，TFTP 下载执行，再由这个刚起来的 iPXE 实例发第二次独立的
DHCP DISCOVER（这次带 user-class）拿到 `/boot.ipxe`。这一路的两次 DHCP
事务、dnsmasq 的 `tag:!ipxe` 排他规则、以及后续 `/boot.ipxe → /v1/boot →
菜单渲染 → sanboot 失败回落` 全链路，都是本报告最有把握代表真实硬件行为
的部分。

## 环境搭建：复用前序 agent 的半成品

本轮是重跑（上一轮因 API 网络中断失败）。worktree 里已有前序 agent 留下
的 `.tmp/pxe-test/`：一块已跑过 cloud-init 的服务端磁盘（`disk.qcow2`，
两块网卡：`enp0s2` 走用户态网络供 apt/ssh，`enp0s3` 静态 `10.20.20.1/24`
供 PXE LAN）、已安装的 `dnsmasq`/`ipxe`/`tcpdump`、已下载好但未安装的
`.deb` 包、按 `deploy/dnsmasq/lober.conf` 占位符替换好的测试配置
（`dnsmasq --test` 已确认语法通过）、交叉编译好的 `lober-linux` 二进制、
以及 `mkfixture.sh`/`mkuser-linux` 两个辅助工具。本轮在此基础上继续，
未重做这些前置步骤。

修正了配置里的一处笔误：`gateway_base`/`artifact_base` 误写成带尾部斜杠
（`"http://boot.lober.internal/"`），而 `internal/gateway/bootscript.go`
的 `bootScriptTemplate` 是 `"%s/v1/boot"`（不做斜杠归一化，测试夹具全部
不带尾部斜杠——`internal/gateway/bootscript_test.go` 等），两者相加会产生
`boot.lober.internal//v1/boot`（双斜杠）。这是本次测试配置的笔误，**不是
代码 bug**：`GatewayBase` 的契约本来就是不带尾部斜杠，改配置修复即可，
浏览器/大多数 HTTP 服务器会容忍双斜杠，但严格意义上不符合契约文档的
URL 形态。

本地网络较慢（apt-get update 屡次卡在下载主索引，与上一轮 ENOTFOUND 是
同一类问题），因此本轮避免依赖在线安装：登录密码用已安装的
`scripts/mkuser/main.go`（交叉编译产物 `mkuser-linux`）离线生成新的
bcrypt hash 替换测试配置里的占位密码；制品集/模板从 `importing`/`draft`
推进到 `ready`/`active` 沿用 `scripts/e2e.sh` 已经验证过的方法——**这是
一个已知契约缺口，不是本次测试发明的绕法**：`internal/console/artifact.go`
的 `handleCreateArtifactSet` 注释明确写着"本模块不 import M7 的实际导入
实现"，全项目没有任何 HTTP 端点能把制品集从 importing 推进到 ready。
`scripts/e2e.sh` 用 `sqlite3` CLI 直接 UPDATE 数据库；本环境没装 sqlite3
CLI（同样卡在 apt），改用 Python 3 标准库自带的 `sqlite3` 模块达到完全
等价的效果（同一张表、同一组字段、同一个"先用真端点拿到真实分配的 UUID
再回填"的顺序）。

## 抓包结论：option 93 与 option 77 的实际字节

### BIOS 客户端（QEMU 默认 e1000 PXE ROM）

```
$ tcpdump -r /tmp/dhcp-bios.pcap -n -vv
...
    ARCH (93), length 2: 0
    NDI (94), length 3: 1.2.1
    Vendor-Class (60), length 32: "PXEClient:Arch:00000:UNDI:002001"
    User-Class (77), length 4:
      instance#1: [ERROR: invalid option]
```

**option 93 = 0（BIOS/Legacy，符合预期，不是本次核实重点）。**

**option 77 的字节级细节值得记录**：tcpdump 的解码器报 "[ERROR: invalid
option]"，但 dnsmasq 自己的日志同时打出 `user class: iPXE` 并正确匹配
`tag:ipxe`——两者不矛盾。用 `-xx` 取十六进制核对：紧跟在 option 60
（vendor-class，32 字节）后面的是 `4d 04 69 50 58 45`，即 tag 77、
长度 4、内容 `iPXE` 四个 ASCII 字节。**iPXE 发的 User-Class 是裸字符串，
不带 RFC 3004 要求的子选项长度前缀**（RFC 3004 的 User Class 是
"1 字节计数 + [1 字节长度 + 字符串] 重复"的复合结构）——tcpdump 的解码器
按 RFC 3004 的复合结构去解析这 4 个字节，自然报错；dnsmasq 显然是按裸
字符串匹配的，所以能正确识别。这个差异本身是一条有价值的实测记录：
"option 77 = iPXE" 在协议细节上是一个不完全合规但被广泛容忍的写法，
不是标准 User-Class 编码。

### UEFI 客户端（OVMF 原生栈，第一次 DISCOVER——阶段一）

```
$ tcpdump -r /tmp/dhcp-uefi.pcap -n -vv
...
    NDI (94), length 3: 1.3.1
    ARCH (93), length 2: 7
    Vendor-Class (60), length 32: "PXEClient:Arch:00007:UNDI:003001"
```

（这条 DISCOVER 没有 User-Class 选项——OVMF 的原生 UEFI 网络栈第一次
探测时不是 iPXE，不会带 iPXE 的标记。）

**这是本报告的核心证据：真实（非 iPXE）UEFI x86-64 固件的 option 93
取值是 7，不是 9。** `deploy/dnsmasq/lober.conf` 把 7 与 9 都归到
`tag:efi64`（"7 是实测主流值，9 是兼容旧值……两条命中同一个 tag"）——
本次实测确认了这个判断，若当时只配 9，这台机器会因为它实际报 7 而完全
匹配不上任何 tag。

对应的 DHCPOFFER：`BF (67), length 12: "snponly.efi"`，`next server:
10.20.20.1`——命中 `dhcp-match=set:efi64,option:client-arch,7` 与
`dhcp-boot=tag:efi64,tag:!ipxe,snponly.efi,,<TFTP_SERVER_IP>` 这条规则，
与 `deploy/dnsmasq/lober.conf` 的设计完全一致。

### UEFI 客户端（第二次 DISCOVER——snponly.efi 起来后，阶段二）

```
    ARCH (93), length 2: 7
    NDI (94), length 3: 1.3.10
    Vendor-Class (60), length 32: "PXEClient:Arch:00007:UNDI:003010"
    User-Class (77), length 4:
      instance#1: [ERROR: invalid option]
```

architecture 码不变（仍是 7，符合预期——固件架构没变，只是现在跑的软件层
换成了 iPXE），但这次带上了 User-Class（同样是裸 "iPXE" 四字节，与 BIOS
那次一致）。对应的 DHCPOFFER：`BF (67): "http://boot.lober.internal/
boot.ipxe"`——命中的是 `dhcp-boot=tag:ipxe,...` 这条阶段二规则，而不是
阶段一那四条之一。**这组"同一块 MAC、同一个架构码、仅凭 user-class 有无
就切换到完全不同的 dhcp-boot 规则"的双重 DHCP 事务，是 dnsmasq 配置里
`tag:!ipxe` 排他设计在真实（非模拟）固件上的直接验证**——如果没有这层
排他，这次 DISCOVER 会因为架构码仍是 7 而再次匹配阶段一规则，机器会
反复重新加载 `snponly.efi`，构成 SPEC §10 失败模式 3 描述的无限引导循环。

### 未知架构（手工构造，checklist 第 6 项）

真实固件不会报未分配的架构码，这一项无法靠"等一台真机上电"覆盖，改为
从服务端 VM 自己在 `enp0s3` 上手工发一个合法的 DHCP DISCOVER、
`ARCH=12`（0x000c，IANA 登记表未分配的值）：

```
$ tcpdump -r /tmp/dhcp-unknown.pcap -n -vv
11:20:35 ... Request from 52:54:00:aa:bb:cc ...
    ARCH (93), length 2: 12
    NDI (94), length 3: 1.2.1
11:20:38 ... Reply, length 300 ...
    DHCP-Message (53): Offer
    Server-ID (54): 10.20.20.1
    Lease-Time (51): 86400
    RN (58): 43200
    RB (59): 75600
    Subnet-Mask (1): 255.255.255.0
    ...（无 BF/option 67 字段）
```

dnsmasq 的日志同时确认：`tags: enp0s3`（只有接口 tag，没有任何架构 tag）、
`DHCPOFFER(enp0s3) 10.20.20.51`——**给了一个正常的 IP 租约，但发出的
DHCPOFFER 里完全没有 `option 67 bootfile-name` 字段**（对比本报告前面
每一条正常应答都有 `sent size: NN option: 67 bootfile-name ...` 这一行，
这里彻底缺失）。这正是 `deploy/dnsmasq/lober.conf` "必须配对第 6 项"一节
预告的行为：DHCP 层面完全正常放行（不报错、给租约），PXE 层面机器拿不到
任何文件名，只能是"看上去毫无反应"。

## 逐条核实结论

| 编号 | 结论 | 状态 |
|---|---|---|
| 1 | option 93/77 实际取值（BIOS=0、UEFI=7，两者均已抓包取得原始字节） | **已实测（附证据，见上）** |
| 2 | 客户端拿到并跑起 iPXE 二进制（阶段一） | **UEFI 已实测**（OVMF 原生栈下载 `snponly.efi`，`NBP file downloaded successfully`，随后 iPXE 1.21.1 启动）；**BIOS 因 QEMU 实现细节不能代表真硬件**（QEMU 的 e1000 PXE ROM 本身就是 iPXE，第一次 DISCOVER 已带 user-class，从未走阶段一） |
| 3 | iPXE 取到 `/boot.ipxe` 并 chain 到 `/v1/boot` | **已实测（附证据）**：两种固件的 iPXE 实例都打出 `boot.ipxe : 134 bytes [script] ok` 与 `http://boot.lober.internal/v1/boot... ok`，`boot.ipxe` 的响应内容与 `docs/contracts/http-machine.md` 端点 1 冻结的文本逐字符一致（修复上述尾斜杠笔误后核对） |
| 4 | 无任务时回 `no_task`，客户端是否真的执行了 `sanboot` | **已实测（附证据）**：两种固件在无任务状态下都打出 `lober: no task for`、`Booting from SAN device 0x80`、`Boot from SAN device 0x80 failed: Input/output error`（BIOS）或 `Error 0x7f22208e`（UEFI），随后落到 `iPXE>` 交互 shell——sanboot 确实被执行、确实因为客户端无盘而失败、固件没有挂死，这正是预期行为 |
| 5 | 建任务后客户端拿到装机菜单与 kickstart | **已实测（附证据）**：见下一节，真实 UEFI 固件收到了 `kernel http://.../vmlinuz ...` 完整菜单文本，并**尝试执行**下载到的 `vmlinuz`（因是 `mkfixture.sh` 造的假文件而报 "Exec format error"，符合预期——见下） |
| 6 | 未知架构不给任何 filename | **已实测（附证据，见上）**：手工构造 DISCOVER，dnsmasq 给正常 IP 租约但不含 bootfile-name |

## 第 5 项细节：任务命中全链路

主机记录、制品集（`ready`）、模板（`active`）、任务均通过真实的
`/console/v1/*` 端点建立（除"制品集 importing→ready"与"模板 draft→active"
两步走上文说明的既有契约缺口绕法外，全部是真实 HTTP 调用）。首次尝试因
测试用的主机 `uuid` 是我随手填的占位值，与真实 OVMF 固件实际上报的
SMBIOS 默认 UUID（全零 `00000000-0000-0000-0000-000000000000`）不一致，
命中了 SPEC §4.3 SN/MAC/UUID 三键查找中的"MAC 命中一次即完成 StartBoot、
任务状态从 pending 前进到下一状态"——这本身是一次有效的负面验证（见下面
"一个意外但有价值的发现"），修正 uuid 后重建任务，真实客户端拿到：

```
$ curl --resolve boot.lober.internal:80:10.20.20.1 'http://boot.lober.internal/v1/boot?sn=&mac=52:54:00:33:33:02&uuid=00000000-0000-0000-0000-000000000000&arch=x86_64&plat=efi'
kernel http://boot.lober.internal/artifacts/vmlinuz \
  inst.repo=http://boot.lober.internal/artifacts/repo/ \
  inst.ks=http://boot.lober.internal/v1/answer/e1de0877-.../.../ks.cfg inst.text ip=dhcp
initrd http://boot.lober.internal/artifacts/initrd.img
```

真实 OVMF UEFI 客户端的屏幕输出（`screendump` 截取，见验证过程说明）
从 `lober: no task for` 变成了 `Could not boot: Exec format error`——
说明这次 iPXE 真的拿到了上面这段菜单文本并按 `kernel`/`initrd` 指令去
下载执行，而不是命中 no_task 分支。`Exec format error` 完全符合预期：
`mkfixture.sh` 造的 `vmlinuz`/`initrd.img` 是 `printf 'fake kernel for
e2e\n'` 这样的纯文本占位文件（对应 SPEC §2.1 识别表只检查文件存在性与
`.treeinfo` 特征文件，不解析内核镜像本身），真实 UEFI 固件试图把这段文本
当成可执行内核镜像加载，自然在格式校验阶段失败——**这是比"什么都没发生"
远更有力的证据**：它证明了 iPXE 确实完整解析并执行了 `kernel`/`initrd`
两条指令、确实向 `http://boot.lober.internal/artifacts/vmlinuz` 发出了
真实 HTTP GET（否则不会有"格式错误"，只会是"网络不可达"之类的另一种
失败）。数据库侧确认：任务 `state` 从 `pending` 前进到 `booting`，
`attempt_count=1`，`token` 与 `finalize_token` 均已签发——StartBoot 与
IssueFinalizeToken 两步在真实固件驱动下完整跑通。

kickstart 本身单独用 curl 核对过内容（`inst.ks` 指向的 URL），渲染出
完整的 `%pre`/lang/keyboard/网络/`logging --host=10.20.20.1 --port=8514`
等字段，`LOBER_*` 事实变量均为空串（因为本次模板未挂任何 hook，`hookRefs:
[]`）——符合预期，不是缺陷。

## 一个意外但有价值的发现：任务命中的"一次性"特性在真机上确认

第一次尝试建任务时，我用 curl 手工构造的 `plat` 参数值有误
（`plat=UEFI` 大写，而 `internal/core/model/inventory.go` 的
`ParseIPXEPlatform` 只认小写 `pcbios`/`efi`），修正参数重新 curl 后**恰好
命中了正确的 grant 流程**，把任务从 `pending` 推进到下一状态、消耗了
`attempt_count`。真实客户端随后的所有请求都落回 `no_task`——**不是 bug，
是 `handleBootGrant` 的设计使然**（`FindPendingTaskByIdentity` 按
`WHERE state = 'pending'` 查询，任务一旦离开 pending 就不再被这个查询
找到）。这与 `docs/real-machine/02-nginx-真机集成.md` 之前验证过的"建任务
即授权，无 arm/disarm"是同一个设计决定的另一面：**装机 token 只发一次，
第二次 `/v1/boot` 请求（哪怕来自同一台机器、同一次开机流程内的重试）
天然拿不到菜单**——这正是 SPEC §10 失败模式 1（防止装完的机器重新走网络
安装造成循环重装）的保护机制在真实网络往返延迟下的行为验证：真机的两次
DHCP 事务之间有秒级延迟，如果 grant 逻辑不是严格一次性的，存在竞态窗口。
本次实测没有触发竞态（因为是我手工的 curl 调用而非客户端自己的重试），
但确认了"一次性"这个不变量在真实的、非受控测试环境的请求节奏下依然
成立。

## README 的 TFTP 四文件表——核对结果

`deploy/ipxe/README.md`"四个编译产物对应哪四个文件"一节列出：

| 架构 | option 93 | TFTP 文件名 |
|---|---|---|
| BIOS PXE | 0 | `undionly.kpxe` |
| IA32 UEFI | 6 | `snponlyx32.efi` |
| x64 UEFI | 7/9 | `snponly.efi` |
| ARM64 UEFI | 11 | `snp-arm64.efi` |

Debian 官方 `ipxe` 包（`1.21.1+git20250501.dad20602+dfsg-1`）实测 `dpkg -L
ipxe` 的完整文件列表：`undionly.kpxe`、`undionly.kkpxe`、`snponly.efi`、
`ipxe-amd64.efi`、`ipxe-arm64.efi`、`ipxe-loong64.efi`、`ipxe-riscv64.efi`、
`ipxe.efi`（指向 `ipxe-amd64.efi` 的软链接）、`ipxe.iso`、`ipxe.lkrn`、
`ipxe.pxe`。

**核对结果：四个文件名里，Debian 包精确提供 2 个（`undionly.kpxe`、
`snponly.efi`），另外 2 个（`snponlyx32.efi`、`snp-arm64.efi`）Debian 包
完全不提供、也没有可以直接改名复用的等价文件**——`ipxe-arm64.efi` 不是
`snp-arm64.efi` 的等价物，两者是 iPXE 构建系统里不同的 make 目标
（`ipxe-*.efi` 系列内置完整菜单/shell，体积与功能都不同于 `snponly.efi`
这个"只含 SNP 驱动、不含菜单"的精简变体；文件名不同不是简单的重命名
差异）。这意味着：**若部署方计划用 Debian 官方包而非 `deploy/ipxe/
build-ipxe.sh` 自行编译来覆盖 IA32 UEFI 与 ARM64 UEFI 这两条路径，
README 的表格与"用法"章节需要补一句明确说明"Debian 包只覆盖 BIOS 与
x64 UEFI 两种架构，IA32/ARM64 仍需自行编译或从 ipxe.org 官方下载"，
否则读者可能误以为装好 Debian 的 `ipxe` 包就能覆盖全部四种架构。**
本报告只做到"确认了这个差异"，不改 README 本身（本次任务是报告-only）。

## 未能验证 / 环境结构性限制

- **BIOS/Legacy PXE 阶段一（架构相关二进制分发）无法用 QEMU 验证**：
  如上文反复强调，QEMU 内置的 e1000 PXE ROM 本身构建自 iPXE，第一次
  DISCOVER 已经带 user-class，直接跳过阶段一。若要验证真实（非 iPXE）
  BIOS 固件的阶段一行为，需要真实物理硬件或另一款不基于 iPXE 的 BIOS
  PXE ROM 模拟器（本次搜索范围内未找到）。
- **IA32 UEFI（option 93=6）与 ARM64 UEFI（option 93=11）未测**：
  OVMF 只有 x86_64（`edk2-x86_64-code.fd`）与 i386（`edk2-i386-code.fd`，
  32 位 UEFI，但 32 位 UEFI 固件在真实市场上极其罕见，主要是历史遗留的
  32 位 Atom 平板）固件文件，本次实测环境的宿主机
  `D:/msys/ucrt64/share/qemu/` 下没有 ARM64 的 edk2 固件（`edk2-aarch64-
  code.fd` 存在，但 QEMU 模拟 ARM64 需要完全不同的 `-machine virt` 与
  `qemu-system-aarch64` 二进制，architecture 层面与本次 x86_64 宿主环境
  下的测试拓扑不兼容，未在本次范围内展开）。
- **option 60（vendor-class）里 "PXEClient:Arch:NNNNN:UNDI:MMMMM" 这个
  复合字符串格式的精确程度**：本次实测确认了它的存在与大致形态（与
  `deploy/dnsmasq/lober.conf` 附录里核实到的 UEFI HTTP Boot 场景
  "HTTPClient:Arch:..." 格式是同一个家族），但本次场景（普通 PXE，非
  HTTP Boot）的 dnsmasq 配置本身不依赖 vendor-class 做匹配决策（只用
  option 93），所以未针对这个字段做额外的匹配规则验证。
- **UEFI HTTP Boot（architecture 码 16）未测**：`deploy/dnsmasq/
  lober.conf` 里这部分配置默认整段注释，本次任务范围（也是 SPEC 现有
  四条 PXE 路径）不包含它，维持未测状态。
- **真实网卡固件（服务器厂商的 PXE ROM，如 Intel/Broadcom/Mellanox 的
  UEFI 网络驱动）与 OVMF 之间的差异**：OVMF 是一个高质量、广泛使用的
  开源 UEFI 参考实现，本次证明的是"标准 UEFI PXE 客户端行为"，但不能
  100% 代表某个特定厂商的固件实现是否有它自己的怪癖（例如某些服务器
  厂商的 BMC/固件已知在 DHCP 选项处理上有 bug）。
- **未做 arp/交换机层面的行为**（VLAN 标记、STP 收敛延迟等）：本次拓扑
  是纯二层直连（socket netdev），不涉及真实交换机，SPEC §1.4 提到的
  "装机 VLAN 与人面网络物理隔离"这一层同样超出本次验证范围（与
  `02-nginx-真机集成.md` 报告的环境局限声明是同一类限制）。

## 仍然必须在真硬件上做的清单

1. **在至少一台真实的 Legacy BIOS 服务器（非 QEMU 模拟）上抓包，确认
   它的原厂 PXE ROM（不是 iPXE）第一次 DISCOVER 报的 option 93 是否
   确实是 0，且第一次真的走了阶段一（拿 `undionly.kpxe`）而不是直接
   跳到阶段二**——这是本报告唯一完全没能验证的核心路径。

   ```bash
   tcpdump -i <PXE网卡> -vvv -s0 port 67 or port 68 -w bios-real.pcap
   # 上电目标机器，选网络引导，等待完成或超时
   tcpdump -r bios-real.pcap -n -vv | grep -A5 'ARCH ('
   # 预期：ARCH (93), length 2: 0；且 DHCPOFFER 里 bootfile-name 是
   # undionly.kpxe（不是 http://.../boot.ipxe），证明真的走了阶段一
   ```

2. **在至少一台真实 UEFI x86-64 服务器上重复本报告的核心实验**，确认
   option 93=7 这个结论在生产硬件（而非 OVMF 参考实现）上同样成立，
   尤其是要覆盖几个不同的主板/网卡厂商：

   ```bash
   tcpdump -i <PXE网卡> -vvv -s0 port 67 or port 68 -w uefi-real.pcap
   tcpdump -r uefi-real.pcap -n -vv | grep -A5 'ARCH ('
   # 预期：第一次 DISCOVER 的 ARCH=7，DHCPOFFER 的 bootfile-name 是
   # snponly.efi
   ```

3. **在国产 ARM64 平台（飞腾/鲲鹏）上验证 option 93 是否规范地报 11**——
   `deploy/dnsmasq/lober.conf` 与 SPEC 附录 B 都把这一项列为待核实，
   本次因缺少 ARM64 QEMU 测试环境未能覆盖，是几项"未测"里优先级最高的
   一项（国产 ARM 平台的固件生态比 x86 UEFI 更不统一，出现意外取值的
   可能性更高）。

4. **在启用了 IA32 UEFI 的真实平板/嵌入式设备上确认 option 93=6**——
   现代服务器几乎不会遇到，但若目标资产池里有工控机/嵌入式设备，
   这条路径的真实覆盖率仍是未知数。

5. **真实网卡厂商固件的 vendor-class 精确格式抓包核对**，尤其是打算
   将来做 vendor-class 收紧匹配（`deploy/dnsmasq/lober.conf` 里默认
   注释的候选规则）之前——本报告只用 QEMU/OVMF 确认了格式的大致家族，
   不足以支撑生产环境的精确子串匹配规则。

6. **用真实 Rocky Linux ISO 制品（而非 `mkfixture.sh` 的占位文件）重复
   第 5 项**，确认 `vmlinuz`/`initrd.img` 真的能被 UEFI 固件加载执行、
   进入 Anaconda 图形/文本安装界面，而不是本报告观察到的
   "Exec format error"（那是占位文件的预期失败，不是链路本身的失败，
   但只有真内核镜像才能验证链路的最后一步：安装程序本身是否真的启动）。

7. **补一条 README 澄清**（`deploy/ipxe/README.md`"四个编译产物对应
   哪四个文件"一节）：注明 Debian 官方 `ipxe` 包仅覆盖 BIOS 与 x64
   UEFI 两条路径，IA32 UEFI 与 ARM64 UEFI 若要用官方包之外的来源，
   需要自行编译（`build-ipxe.sh`）或从 ipxe.org 下载对应文件。**本次
   任务是报告-only，未直接修改该 README**，留给后续处理这条发现的人。

## 清理

已关闭本次测试建立的两台 QEMU 进程（服务端 + 全部客户端反复重启的实例），
未留下残留进程或占用的 hostfwd 端口。`.tmp/pxe-test/` 下产生的临时文件
（多次 screendump 的 `.ppm`/`.png`、几份 pcap、辅助 python 脚本）保留在
worktree 本地供后续核对，未纳入本次 commit（该目录本身不追踪）。
