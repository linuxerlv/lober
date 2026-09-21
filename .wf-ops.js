export const meta = {
  name: 'lober-ops-wave-a',
  description: 'Deploy artifacts, Secure Boot PoC research, image pipeline tree and TLS prep — all additive files with zero overlap on the running M2-M8 module work',
  phases: [
    { title: 'Artifacts', detail: '部署产物、PoC 调研、流水线定义、CA 与 iPXE 文档并行产出' },
    { title: 'Wrap', detail: '部署总文档与 TLS 增量，依赖前一阶段的产物' },
  ],
}

const WT = 'E:/src/lober-wt/ops'

const PRE = `你是 Lober 项目的部署与调研实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：${WT}
  分支：    feat/ops-deploy

**所有文件操作都在这个 worktree 里**（用绝对路径，或先 \`cd ${WT}\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 那里有七个模块的 agent 正在并行工作。

## 本轮有多个 agent 与你共用这一个 worktree

所以两条硬约束：
1. **只创建/修改属于你的文件**（下面列出）。别人的文件即使看着不对也不要动。
2. **完全不要执行任何 git 命令。** 不 add、不 commit、不 status。编排方会在本轮结束后统一提交。你只管把文件写对。

## 仓库现状

M1（契约与地基）已完成并合并到 dev，你的 worktree 里有它：
  internal/core/model/**、internal/core/contract/**、internal/core/fake/**
  internal/store/sqlite/{schema/001_init.sql,migrate.go}
  internal/apierr/**、internal/config/**（★ 配置结构的权威定义，写配置样例必须读它）
  internal/archtest/、Makefile
  docs/contracts/{http-machine,http-console,filesystem}.md（冻结的契约）

**M2–M8 七个模块的 Go 代码此刻在别的分支上，你的 worktree 里没有。** 所以：
- \`go build ./...\` 只能看到 M1 的包，这是正常的，不是错误
- 不要引用 internal/{gateway,console,render,statemachine,provider,imports} 里的东西
- 不要跑 make check

## 铁律

1. **严禁 rm -rf，严禁删除任何目录。** 需要临时文件就用 t.TempDir() 或写在 \`${WT}/.tmp/\` 下，用完只删你建的那个具体文件。**绝不创建与真实模块同名的目录。**
2. 禁止修改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。可用依赖：modernc.org/sqlite、go-cmp、uuid、yaml.v3、golang.org/x/crypto（含 bcrypt）、golang.org/x/net。
3. 禁止修改 internal/core/**、internal/apierr/**、internal/config/**、internal/archtest/**、docs/contracts/**、Makefile、.gitignore。
4. 禁止任何 git 命令（见上）。
5. **本机是 Windows，没有 nginx / dnsmasq / dhcpd / kea / systemd / systemd-analyze。** 计划里写的 \`nginx -t\`、\`dnsmasq --test\`、\`systemd-analyze verify\` **在这里跑不了**。不要假装跑过，也不要为了「能验证」而改变配置的正确形态。你要做的是：
   - 做**静态自检**：用 Read 逐行核对必需指令是否都在、值是否正确、括号与分号是否配平、引用到的文件路径是否与 docs/contracts/filesystem.md 一致
   - 在返回值的 verifyOutcome 里**明确写出「哪条验证因缺少工具而无法执行，需在目标主机上补做」**
   - 在产出文件自身的注释顶部写一行部署前必跑的校验命令，供运维照做
6. 配置文件里所有占位值（IP、域名、路径、密钥）用醒目的占位形式并在注释里说明必须改，不要写一个看起来能直接用的假值。
7. 中文注释与文档；标识符与文件名用英文。Go 代码 gofmt 干净。
8. 你的返回值是数据，不是给人看的汇报。

`

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' }, description: '创建或修改的文件（worktree 内相对路径）' },
    verifyCommands: { type: 'array', items: { type: 'string' }, description: '实际执行过的命令原文' },
    verifyOutcome: { type: 'string', description: '静态自检结果 + 明确列出哪条验证因缺工具无法执行、需在目标主机补做' },
    acceptance: { type: 'array', items: { type: 'string' }, description: '计划要求逐条达成情况，格式：✓/✗ 条目 —— 证据' },
    openQuestions: { type: 'array', items: { type: 'string' }, description: '需要实机核实或需要编排方决策的点；没有就空数组' },
    notes: { type: 'string', description: '合并与后续阶段必须知道的事' },
  },
  required: ['files', 'verifyCommands', 'verifyOutcome', 'acceptance', 'openQuestions', 'notes'],
}

phase('Artifacts')

const first = await parallel([
  () => agent(PRE + `## 你的任务：nginx 配置（M9 计划任务 6）

先 Read：docs/superpowers/plans/2026-09-20-lober-mvp-09-integration.md 的任务 6 全文、SPEC.md 的 §1.4 与 §1.6、docs/contracts/filesystem.md（制品路径必须与它逐字符一致）、docs/contracts/http-machine.md 与 http-console.md（端点路径）。

你拥有的文件（只有这些）：
  deploy/nginx/lober.conf
  deploy/nginx/lober-http-snippet.conf

两个 server 块：机器面只监听装机 VLAN 的具体 IP，人面只监听管理网的具体 IP。

必须配对的六项：
1. **listen 写具体 IP，不是 0.0.0.0。** 写成通配时一切功能正常、测试全过，只是装机 VLAN 上的裸机获得了访问控制台 API 的能力 —— 信任域被静默合并。注释里要写明还需加防火墙规则兜底。
2. **制品走 location 直接发盘，不得 proxy_pass。** 否则 4 GB 镜像流经 Go 进程，sendfile 零拷贝、aio threads、Range 请求全部失效。
3. **机器面全部关 gzip**（制品与引导脚本都要），否则 iPXE 会把压缩字节当内核加载。人面相反 —— 前端 JS/CSS 该压。
4. 制品要开 \`Accept-Ranges: bytes\`：wimboot 与 WinPE 依赖 Range 请求。
5. 仓库 location 要开 open_file_cache：离线仓库是几十万小文件。
6. **SSE 的 location 必须关 proxy_buffering 与 proxy_cache、放长读超时**。默认缓冲会让装机日志看起来完全冻住。SSE 路径按 http-console.md 写。

两处 SPEC 骨架之外要加的：
- **信号端点单独设限流桶**（SPEC §10 运维点二）：信号回调量不小（数百台 × 每 30 秒），与引导端点共用限流桶会让一批机器的信号把另一批的引导挤掉。\`limit_req_zone\` 只能声明在 http 层，所以放进 lober-http-snippet.conf，并在主配置顶部注释写明它必须先被 include（以及 include 的位置怎么写）。
- **access log 格式带上 query 与耗时**：nginx access log 是 iPXE 启动之后全链路的主要取证记录。SN/MAC/UUID 都在请求 URI 里，grep 单机可得完整引导时间线；制品下载的字节数与耗时能直接看出传输是否中途断开。

另外在注释里写明两项部署前必须确认的编译选项：\`--with-threads\`（sendfile + aio 需要）与 \`--with-http_secure_link_module\`（Phase 2 的制品授权需要，MVP 缺它不影响）—— **离线装包前就要确认，内网补装不上**。

自检：逐行核对上面六项 + 两项；核对每个 location 的路径与两份 HTTP 契约、以及制品路径与 filesystem.md 一致；核对括号与分号配平。\`nginx -t\` 本机没有 nginx 跑不了，在 verifyOutcome 里写明需在目标主机补做。`,
    { label: 'ops:nginx', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：dnsmasq 配置（M9 计划任务 7）

先 Read：M9 计划的任务 7 全文、SPEC.md 的 §1.3 与附录 A.1/A.1b、以及 §10 失败模式 3/4/10。

你拥有的文件（只有这些）：
  deploy/dnsmasq/lober.conf
  deploy/dnsmasq/README.md

**本任务整体属于部署期一次性核对，不是运行期功能**（SPEC §0.1）。这些码值配错的后果都发生在机器取 iPXE 二进制那一步 —— 根本走不到网关，本系统没有检测手段。把细节写进配置是为了**配置那一次就配对**。

**最容易踩且最难查的错误：option 93 的 7 与 9 必须都匹配，且 7 才是实测主流值**（附录 A.1b）。RFC 4578 原文把 7 定义为 EFI BC、9 定义为 EFI x86-64，但勘误明确指出两者应当互换 —— 实际 UEFI 固件用 7 上报 x86-64，ipxe.org 把 9 标为 obsolete。**只配 9 的后果是 UEFI 机器匹配不上任何 tag、拿不到引导文件名、固件静默跳到下一个引导项**，现场表现为「机器毫无反应」。两个值归到同一 tag 即可，不需要两套制品。在配置里就这一点写足注释，它值得。

必须配对的七项：
1. **只在装机 VLAN 的网卡上提供服务**，绝不写监听所有接口
2. 五条架构匹配规则（option 93 的 0/6/7/9/11），**7 与 9 同 tag**
3. user-class 匹配规则（option 77）区分两个阶段 —— 没有这一层就是无限引导循环（§10 失败模式 3）
4. 阶段一按架构给二进制，**每条都带「不是 iPXE」的反条件**
5. 阶段二给固定的 HTTP URL，架构无关一条通吃 —— 这是与网关的唯一耦合点
6. **未知架构不给任何引导文件名**，绝不兜底发某个二进制 —— 给错架构的二进制会让固件加载失败或挂死，比不引导更难查（§10 失败模式 4）
7. **租期长于装机窗口**：若机器在取引导脚本与取制品之间换了 IP，将来上 secure_link 后签名会失效（§9.3）

另外三项：DNS 记录（**用域名不用 IP** —— 将来签证书时 CN 能对上，不必回头改所有 URL 与模板）、**本地 NTP**（时间不同步会让日志时序错乱，§10 失败模式 10）、开启 DHCP 日志。

**ARM64 那行 MVP 阶段可以不放二进制，但 tag 建议先留**：缺失时国产 ARM 平台（飞腾/鲲鹏）接入的首个症状同样是「机器毫无反应」；留着 tag 则 TFTP 会留下一条找不到文件的记录，排查成本差一个数量级。

README 要写清 TFTP 目录该放哪四个文件（BIOS/IA32/x64/ARM64 各一个）以及它们在 ipxe.org 的对应路径；气隙环境下这些二进制需从外网取得后带进内网，**下载后校验 sha256 并把校验值记进仓库**。README 另附「抓 DHCP DISCOVER 看固件实际上报什么」的排查步骤 —— SPEC 附录 B 把 option 93 实机取值列为待核实项，尤其国产 ARM 平台是否规范地报 11。

自检：逐条核对七项 + 三项；确认阶段二的 URL 与 docs/contracts/http-machine.md 的 /boot.ipxe 一致。\`dnsmasq --test\` 本机没有 dnsmasq 跑不了，在 verifyOutcome 里写明需在目标主机补做。`,
    { label: 'ops:dnsmasq', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：对接既有 DHCP —— ISC dhcpd 与 Kea 等价配置（M9 计划任务 7b）

先 Read：M9 计划的任务 7b 全文、SPEC.md 的 §1.3.1（dhcpd）与 §1.3.2（Kea）、附录 A.12/A.13/A.15、附录 B 里 dhcpd 与 Kea 相关的待核实项。

你拥有的文件（只有这些）：
  deploy/dhcpd/lober.conf
  deploy/kea/lober-dhcp4.json
  deploy/README-既有DHCP.md

**这套配置只在无法使用 dnsmasq 时用**（目标机房已有在运的 DHCP 且不接受再引入一个）。README 开头就要写明这个判据，以及：**新部署一律用 dnsmasq 或 Kea，不要选 dhcpd** —— 它已于 2022 年 10 月 EOL（附录 A.12），最后维护版本是 4.4.3-P1 / 4.1-ESV-R16-P2，ISC 官方建议迁移到 Kea。

**三种实现的机制完全不同，不能互相照抄**，README 里要有这张对照表：dnsmasq 是 tag 集合靠 \`tag:!ipxe\` 显式排除；dhcpd 是 if/elsif 条件链天然互斥；**Kea 是 client-classes 数组、所有 class 独立求值**。

dhcpd 的三个要点：
1. **条件顺序是硬要求：user-class 判定必须在架构判定之前**，否则已经是 iPXE 的客户端会再次匹配到架构分支、拿到 iPXE 二进制，直接构成无限引导循环（§10 失败模式 3）。
2. **必须显式声明 option 93**（\`option client-arch code 93 = unsigned integer 16;\`），dhcpd 无内置名称。但比较字面量写 \`00:07\` 这种双字节形式（dhcpd 按原始 option 数据逐字节比较）—— 这是社区通行写法，**实施前需用 \`dhcpd -t\` 与真机各验一遍，写成 \`= 7\` 的行为不要凭空假设**（附录 B），在注释里标明这一点。
3. **未知架构必须落到显式 else 并打日志**：不写 else 时客户端拿不到 filename、固件静默跳过网络引导，与「DHCP 没响应」现场表现完全一致，这条日志是区分二者的唯一依据。**绝不要在 else 里兜底发某个二进制。**

Kea 的四个要点：
1. **反条件宁可写重复也不要省**：把 iPXE 的反条件直接 AND 进每个架构 class 的 test，让至多一个 class 能命中。危险在于「独立求值」—— 一台 UEFI 机器跑起 iPXE 后再次发 DISCOVER，同时满足 ipxe 与 efi64 两个 class 的 test，若两者都设了 boot-file-name，实际生效的是哪个取决于 Kea 的 class 求值与 option 合并顺序，**这条语义本设计不依赖**（附录 B 列为待核实）。若确认 \`member()\` 可用可简化成 \`not member('ipxe')\`，但**必须实测后再用**。
2. **option 93 在 Kea 里的符号名是 client-system，不是 client-arch**（后者是 dhcpd 配置里的自定义名）。建议先用 \`option[93].hex\` —— 符号名对不上时 Kea 直接启动失败，比静默不匹配好查。
3. 无需显式声明 option 93，Kea 内置标准选项定义（与 dhcpd 的实质差异）。
4. **未知架构不会命中任何 class、也拿不到 boot-file-name**，行为与 dhcpd 的显式 else 等效，但 **Kea 不会为此留日志** —— 「机器毫无反应」这一症状在 Kea 下比 dhcpd 更难定位，README 要给出调高日志级别观察未分类报文的做法。

README 还要写：**0x0007 有实战踩坑记录**（有用户报告 UEFI 固件明确发 option 93 = 0x0007，Kea 仍回了 legacy BIOS 的引导文件，附录 A.15）—— **调试第一步永远是抓 DISCOVER 报文看固件实际上报什么，不要相信配置文件里的注释**。另写明三种实现的共同收敛点：不论用哪一种，第二段拿到的都是同一个 \`/boot.ipxe\`，网关对此零感知（§4.7）。

自检：两份配置逐条核对上面的要点；确认两者的阶段二 URL 与 dnsmasq 版本一致（README 里交叉引用 deploy/dnsmasq/lober.conf，但**不要修改那个文件**，它是另一个 agent 的）。\`dhcpd -t\` 与 \`kea-dhcp4 -t\` 本机都没有，在 verifyOutcome 里写明需在目标主机补做。`,
    { label: 'ops:dhcpd-kea', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：systemd 单元、配置样例与密码哈希工具（M9 计划任务 8）

先 Read：M9 计划的任务 8 全文、**internal/config/config.go（配置样例必须与它的结构逐字段对应，这是权威定义，先读它再写样例）**、SPEC.md 的 §1.6（启动顺序）与 §9.4（权限分级的五个角色与权限码）、docs/contracts/filesystem.md（三个可写路径）。

你拥有的文件（只有这些）：
  deploy/systemd/lober.service
  deploy/systemd/nginx.service.d/after-lober.conf
  deploy/lober.yaml.example
  scripts/mkuser/main.go

systemd 单元的六项要点：
1. **ExecStartPre 跑一次校验模式** —— 配置写错时在这里失败，比起服务后第一个请求才崩好查
2. **TimeoutStopSec 要长于装配点的优雅关闭宽限期**，否则 systemd 会在收尾完成前强杀进程
3. **需要绑定特权端口的能力**（syslog 在 514），但**只给这一项能力，不给 root**（CAP_NET_BIND_SERVICE）
4. **文件系统加固**（ProtectSystem、PrivateTmp、ProtectHome），但**可写路径要包含数据库、制品、日志三处** —— 漏一处的表现是运行到一半才失败。三个路径取自 filesystem.md
5. 日志交给 journald
6. 失败自动重启，但有重启间隔

**nginx 的启动顺序 drop-in**（SPEC §1.6 第二条）：nginx 先起会返回 502。加 \`After=lober.service\` 的 drop-in，或接受重启窗口内的 502（iPXE 会重试，实际影响小）—— **两种都可接受，但要选一个而不是不管**，在文件注释里说明选了哪个与为什么。

**配置样例要带注释说明每项的后果**，尤其三处必改项：两个监听地址的 IP（写成通配会静默合并信任域）、基址 URL 的域名（用域名不用 IP，将来签证书 CN 能对上）、**控制台 token 密钥**（空值时启动生成随机值，后果是重启即全员登出）。另要在注释里列出五个角色与各自的权限码（SPEC §9.4 与 §12.5 的表），运维配用户时需要。样例必须能被 internal/config 真正解析 —— **写完用一个临时 Go 测试或 \`go run\` 小程序调用 config 的解析与校验函数验证它能通过**，这是本任务唯一能真跑的验证，务必跑。临时文件放 \`${WT}/.tmp/\` 并在结束前只删你建的那个文件。

密码哈希工具的三项要点：
- **独立的小工具而不是主二进制的子命令**：它需要从终端读密码，而主二进制跑在 systemd 下不该有任何交互式代码路径
- **默认交互式读两次并比对**，另提供一个从标准输入读一行的开关供自动化使用（端到端脚本要非交互地造用户）。默认交互是为了不把密码留在 shell history 里
- **bcrypt cost 用 12 而非默认 10**：比默认慢约 4 倍，对个位数用户的登录频率毫无影响，但离线爆破成本高 4 倍
- **哈希打到标准输出、提示打到标准错误** —— 这样可以直接管道进配置文件
- **拒绝短密码**，并在提示里说明理由（这个账号能让机器被擦盘）

自检与验证：\`go build ./scripts/mkuser/\` 必须通过；用非交互开关跑一次，确认输出是 60 字符的 bcrypt 哈希且以 \`$2\` 开头；\`gofmt -l scripts/mkuser\` 无输出；配置样例的解析验证（见上）。\`systemd-analyze verify\` 本机没有 systemd 跑不了，在 verifyOutcome 里写明需在目标主机补做。`,
    { label: 'ops:systemd-mkuser', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：Secure Boot 链载 PoC 调研（phase4-03 全部五个任务）

先 Read：docs/superpowers/plans/2026-09-20-lober-phase4-03-secureboot-poc.md **全文**（它只有 206 行，全读）、SPEC.md 的 §11 Phase 4 与附录 B 里 Secure Boot 那一行。

你拥有的文件（只有这些，五份）：
  docs/secureboot-poc/01-机器事实.md
  docs/secureboot-poc/02-中间引导器.md
  docs/secureboot-poc/03-签名与注册.md
  docs/secureboot-poc/04-内核签名.md
  docs/secureboot-poc/05-结论.md

**这是本项目技术风险最高的一项，SPEC 明确建议做独立 PoC。** 你在本环境没有裸金属、没有 BMC、没有开启 Secure Boot 的机器，所以**你产出的不是实验结果，是一份能让运维照着做实验的方案 + 一份基于公开资料的事实核实**。这个区分必须在每份文档顶部写清楚：哪些是查证到的事实（给来源链接），哪些是待实机验证的步骤（给具体命令与预期输出）。

调研手段：用 curl 取 raw.githubusercontent.com 与各发行版文档站（WebFetch 对 github.com 等域名常被挡，raw 与 api.github.com 可直连；被挡时改用 curl，或用 WebSearch 找可直连的镜像）。**取不到来源的论断一律标注「未能核实」，绝不编造链接或版本号。**

五份文档按计划的五个任务组织，核心要回答的是那条三级签名链 \`shim.efi → ipxe.efi → kernel\` 每一级的机制：
- 机器侧事实：怎么查固件是否开启 Secure Boot、db/dbx/MOK 各是什么、怎么看已注册的密钥
- 中间引导器：shim 从哪来（发行版包 vs 自建）、它凭什么被固件信任、怎么验证取到的 shim 是微软签过的
- iPXE 的签名与密钥注册：iPXE 需要自行编译（带 TRUST= 嵌内网 CA）、自签后怎么让 shim 信任它、MOK 注册流程与它的交互式代价（**这一步是整条链最可能卡住的地方**，要写清为什么）
- 目标 OS 内核的签名链：发行版内核自带签名 vs 自建内核需自签、initrd 是否在验证范围内
- 结论：给出「值得做 / 不值得做 / 有条件做」的判断与依据，并明确列出哪几步必须在真机上验证才能定论

同时必须写清与本项目的衔接：SPEC §1.5 决定 MVP 不上 TLS 的理由之一就是「叠加 Secure Boot 签名链后风险与工期都显著上升」，而 iPXE 重编译（Phase 4 TLS 那边也需要）与这里是同一件事 —— 在 05-结论.md 里指出这个交集，避免两边各做一遍。

自检：五份文档都产出；每条论断都标了「已核实（带来源）」或「未能核实」；每个待实机验证的步骤都给了具体命令与预期输出（而不是「检查一下固件设置」这种没法执行的话）。verifyCommands 填你实际用来取证的 curl / WebSearch 命令。openQuestions 里列出必须在真机上才能定论的问题清单。`,
    { label: 'ops:secureboot-poc', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：镜像封装流水线的构建定义（phase3-03 计划任务 2）

先 Read：docs/superpowers/plans/2026-09-20-lober-phase3-03-image-pipeline.md 的**任务 2 全文**（其余任务是别人的，不要做：任务 1/3/4/5 会改 internal/core/model、internal/imports、internal/render、internal/console，那些包在别的分支上）、SPEC.md 的 §11 Phase 3 与附录 B 里「镜像构建工具边界」那一行。

你拥有的文件（只有这些）：
  pipeline/README.md
  pipeline/ 目录下的构建定义文件（Packer 模板、mkosi 或 osbuild 配置等，按计划任务 2 的要求命名）

**注意：\`pipeline/sign.sh\` 与 internal/imports/signature.go 是计划任务 3 的产出，不是你的，不要写。**

计划任务 2 要的是「封装流水线与交付物格式」。核心是三件事：
1. **哪个工具对应哪类 OS**：Packer 用于 Windows / ESXi（**它们必须引导真 installer**，这是 Packer 不可替代的地方）；mkosi 或 osbuild 用于 Linux；virt-customize 用于增量定制。SPEC 附录 B 把这三者的「实际能力与 OS 覆盖范围」列为待核实项 —— 你要用 curl / WebSearch 去查它们的官方文档核实，**核实不到的如实标注，不要凭印象写**。
2. **交付物格式**：镜像直写通道要的是什么形态的产物（原始块设备镜像？压缩格式？分块？），以及它与 Phase 1 的 artifact_set 数据模型如何对应。SPEC §11 Phase 3 提到 zstd 传输 + sha256/cosign key-based 校验。
3. **气隙约束**：构建必须在有外网的机器上做，产物带进内网；**cosign 的 keyless 模式在气隙下走不通**（依赖 Fulcio 的外网 OIDC 与 Rekor 透明日志，SPEC §9.6），必须用 key-based 签名，公钥预置。README 要把这条写在显眼处 —— 「不要指望默认流程能跑」是 SPEC 的原话。

README 还要写明这个通道与安装器驱动通道的分工（SPEC §11 Phase 3）：**安装器驱动作兜底**，覆盖新 OS 版本、特殊机型、调试；**镜像直写作主力**，覆盖批量与一致性要求。两者并存，不是替代关系。

自检：构建定义的语法正确（若装不上 packer/mkosi 就做静态核对并在 verifyOutcome 说明）；每个工具能力论断都标了核实状态与来源；气隙三条约束都写了。openQuestions 里列出需要实测的项（例如某工具能否覆盖某个 OS 家族）。`,
    { label: 'ops:pipeline', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：内网 CA 与 iPXE 重编译文档（phase4-02 计划任务 2 与任务 4）

先 Read：docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md 的**任务 2 与任务 4 全文**（任务 1/3/5/6 是别人的，不要做 —— 它们要改 deploy/nginx 与 deploy/dnsmasq 的配置，那两个文件正被别的 agent 写）、SPEC.md 的 §1.5（MVP 明文 HTTP 的显式决定与将来上 TLS 的三个前置条件）。

你拥有的文件（只有这些）：
  deploy/ca/README.md
  deploy/ca/ 下的证书签发脚本（按计划任务 2 的要求命名）
  deploy/ipxe/README.md
  deploy/ipxe/ 下的构建脚本（按计划任务 4 的要求命名）

任务 2（内网 CA 与证书签发）：气隙环境不可能用公网 CA，必须自建。要写清：CA 私钥怎么产生与保管、签发脚本怎么用、证书里 **CN/SAN 必须是域名而不是 IP**（SPEC §1.3 与 §1.5 都强调用域名的理由正是「将来签发证书时 CN 能对上，不必回头修改所有 URL 与模板」）、有效期与轮换节奏、以及**轮换时哪些地方要同步更新**（这一条最容易漏）。脚本要能在只有 openssl 的环境下跑。

任务 4（iPXE 重新编译）：这是 TLS 在这套栈里**最贵的部分**（SPEC §1.5）。要写清：为什么必须重编译（iPXE 默认不带 crypto，走 HTTPS 需要）、\`TRUST=\` 怎么把内网 CA 嵌进二进制、编译产物对应 §1.3 的哪四个文件（BIOS/IA32/x64/ARM64）、以及**每次证书轮换都要重编重发引导文件**这个运维代价。

**两处必须交叉引用、不要各写一遍**：
1. iPXE 重编译与 Secure Boot PoC（docs/secureboot-poc/，另一个 agent 正在写）是同一件事的两面 —— 自签 iPXE 之后还要让 shim 信任它。在 deploy/ipxe/README.md 里指出这个交集并指向那份 PoC，不要在这里重复展开 Secure Boot 的内容。
2. nginx 与 dnsmasq 的 TLS 侧改动是本计划任务 3 与任务 6，由另一个 agent 在本轮稍后做 —— 你只需在 README 里说明「配置侧改动见 deploy/nginx/lober.conf 的 TLS 段」，**不要自己去改那两个文件**。

自检：证书脚本用 \`openssl\` 能跑通就真跑一遍（本机若有 openssl）；CN/SAN 用域名这条在文档与脚本里都体现了；轮换时的同步清单完整（至少覆盖 nginx 证书、iPXE 二进制、模板里的 URL scheme）。verifyOutcome 里写明哪条因缺工具无法执行。`,
    { label: 'ops:ca-ipxe', phase: 'Artifacts', schema: R }),

  () => agent(PRE + `## 你的任务：Provider 加载器的需求依据文档（phase2-03 计划任务 1）—— 这是一道开工闸门，不是形式

先 Read：docs/superpowers/plans/2026-09-20-lober-phase2-03-loaders.md 的**任务 1 全文**与开头的「这个计划有一个开工前提」那段、SPEC.md 的 §7.2、§7.4、§7.5、**§7.6（MVP 不做插件系统的理由）**、internal/core/contract/provider.go（七个接口与失败语义表的真实定义）。

你拥有的文件（只有这一份）：
  docs/phase2-loader-需求依据.md

**你的任务不是实现加载器，是产出「要不要做、做哪几个」的判据。** 计划原文：「**任务 1 是收集依据，依据不足时应当推迟整个计划**」。SPEC §11 把加载器放在 Phase 2 并注明「此时已知哪些扩展点真需要外部集成」；§7.6 进一步警告过早做插件体系会「花两周做加载、版本协商、隔离，然后发现实际只需要三个 shell 脚本」。

按计划任务 1 要回答的四个问题逐条写。但有一个你必须如实处理的现实：**Phase 1 此刻还没有运行期**（MVP 七个模块正在并行开发，尚未集成、未上过真机）。所以计划要求的「Phase 1 运行期的实际依据」**现在不存在**。

因此这份文档的结论部分必须明确写出：
1. 四个问题里，哪些能凭 SPEC 与契约层的既有信息回答（例如 §7.4 的失败语义表已经规定了哪些 provider 失败会阻塞建任务 —— 阻塞式的那几个对加载器的可靠性要求明显更高）
2. 哪些**必须等 Phase 1 上真机之后才有依据**，以及具体要观测什么（要收集哪些数据、从哪里看、什么样的观测结果支持做 exec、什么样支持做 http、什么样说明三个 shell 脚本就够了）
3. **一个明确的开工判据**：满足什么条件才启动 phase2-03 的任务 2 及之后
4. 按 §7.2 的选型判据给出**倾向性建议**（外部系统集成用 exec/http —— 气隙环境下改 shell 脚本是 30 秒的事，改 Go 插件要在外网编译打包走审批传入内网；有真逻辑的集成用 grpc；纯决策用内置策略后续按需上 Starlark），并说明这个倾向是基于 SPEC 的推理而非实测。

**不要为了「有个结论」而假装依据充分。** 这份文档的价值恰恰在于它可能得出「现在不该做」——那也是一个有用的结论，而且是 SPEC §7.6 预期的默认结论。

自检：四个问题逐条有回答且标注了依据来源（SPEC 条款 / 契约代码 / 待实测）；开工判据是可判定的（不是「等情况明朗」这种没法判定的话）；结论与 §7.6 的警告没有冲突、或者说清为什么这次例外。openQuestions 里列出需要 Phase 1 运行期数据的具体观测项。`,
    { label: 'ops:loader-gate', phase: 'Artifacts', schema: R }),
])

const done = first.filter(Boolean)
log('第一批完成 ' + done.length + '/8，产出文件 ' + done.reduce((n, r) => n + (r.files || []).length, 0) + ' 个')

phase('Wrap')

const nginxR = first[0]
const dnsmasqR = first[1]
const dhcpR = first[2]
const sysdR = first[3]
const caR = first[6]

const ctx = '\n\n## 前一批已产出的文件（动手前先 Read 它们，以真实内容为准）\n' +
  done.flatMap(r => r.files || []).map(f => '  ' + f).join('\n') +
  '\n\n前一批记录的待核实项与开放问题：\n' +
  done.flatMap(r => r.openQuestions || []).map((q, i) => '  ' + (i + 1) + '. ' + q).join('\n')

const wrap = await parallel([
  () => agent(PRE + ctx + `

## 你的任务：部署文档与实机核对清单（M9 计划任务 10）

先 Read：M9 计划的任务 10 全文，以及上面列出的前一批全部产出文件（nginx、dnsmasq、dhcpd/kea、systemd、配置样例、mkuser、CA、iPXE）。

你拥有的文件（只有这一份）：
  deploy/README.md

**这份文档是给到机房去的人看的，不是给开发看的。** 写作对象是运维：他手上有一台装好 Linux 的服务器、一批待装的裸机、一个内网、没有外网。

要写清的内容：
1. **部署顺序**：先装什么、后装什么，以及为什么这个顺序（例如 nginx 与 lober 的启动顺序问题，SPEC §1.6）
2. **三处必改的配置**：两个监听地址的 IP、基址 URL 的域名、控制台 token 密钥 —— 每处写明改错的后果
3. **TFTP 目录要放哪四个 iPXE 二进制**，从哪取、怎么校验 sha256（交叉引用 deploy/dnsmasq/README.md，不要重复展开）
4. **选哪套 DHCP 配置**：dnsmasq（新部署首选）还是对接既有 dhcpd/Kea（交叉引用 deploy/README-既有DHCP.md）
5. **实机核对清单**：这是本文档最有价值的部分。SPEC 附录 B 列了一批待验证项，其中属于部署期一次性核对的要在这里变成可勾选的步骤 —— 尤其 **option 93 各架构的实机取值**（抓 DHCP DISCOVER 看固件实际上报什么，不要相信配置注释）、**nginx 是否编译带 --with-threads**（离线装包前就要确认，内网补装不上）、**iPXE 变量拼写**（\${serial}/\${uuid}/\${buildarch}/\${platform} 与多网卡时 net0 是否为引导网卡）。每条给出「跑什么命令、看什么输出、不对怎么办」。
6. **验证部署成功的标志**：按 docs/contracts/http-machine.md，运维能用 curl 模拟一次机器请求确认链路通 —— 给出具体命令（注意此时还没有任务，预期响应是 no_task 的那三行 sanboot 脚本）
7. **范围边界要写给运维**：SPEC §0.1 明确上电、DHCP、拿到 iPXE 二进制这一段在系统范围外 —— 机器在 PXE 里转圈或毫无反应时，排查入口是 BMC 控制台与 DHCP/TFTP 日志，**不是控制台任务列表**。这条不写清，运维会在错误的地方找问题。

**不要重复展开各配置文件已有的注释**，用交叉引用。这份文档的职责是顺序、决策点与核对清单。

自检：七项都写了；每条核对清单项都是可执行的（有命令、有预期输出、有不对时的处置）；交叉引用的文件名与前一批的实际产出一致（用 Read 核对，不要凭记忆写文件名）。`,
    { label: 'ops:deploy-readme', phase: 'Wrap', schema: R }),

  () => agent(PRE + ctx + `

## 你的任务：TLS 的配置侧增量与两份决策文档（phase4-02 计划任务 1、3、5、6）

先 Read：docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md 的任务 1、3、5、6 全文、SPEC.md 的 §1.5，以及前一批产出的 deploy/nginx/lober.conf、deploy/dnsmasq/lober.conf、deploy/ca/README.md、deploy/ipxe/README.md（**任务 2 与 4 已由另一个 agent 完成，不要重做，只交叉引用**）。

你拥有的文件：
  docs/phase4-tls-前提验证.md      （任务 1）
  docs/phase4-mtls-决策.md          （任务 5）
  deploy/nginx/lober.conf           （任务 3：**只允许追加 TLS 相关内容，不得改动已有的非 TLS 配置**）
  deploy/dnsmasq/lober.conf         （任务 6：**只允许追加 UEFI HTTP Boot 相关内容，不得改动已有内容**）

**两个配置文件是别人刚写完的，你是在它们之上做增量。** 动手前先完整 Read，改完再 Read 一遍确认你没破坏原有结构。若 TLS 段与原有配置有冲突（例如同一个 server 块要改 listen），**优先用新增 server 块或注释掉的备选段来表达，并在注释里写明「启用 TLS 时改这里、同时停用上面那段」** —— 不要直接把 MVP 的明文配置改掉，MVP 明确不上 TLS（SPEC §1.5），配置要能同时服务两个阶段。

任务 1（前提验证）：SPEC §1.5 承诺「为将来上 TLS 只需做到部署变更而非代码变更」，做法是三件事：所有 URL 从配置来且 scheme 不写死、用域名不用 IP、首跳脚本不用 EMBED= 烧进 iPXE。**你的任务是验证这个承诺在已落地的代码里是否成立。** 能验的部分现在就验：
- \`internal/core/model/render.go\` 的 BaseURLs 是否真的让 scheme 完全来自配置（读代码 + 看它的测试）
- \`internal/config/config.go\` 的基址字段是否不含硬编码 scheme
- 已产出的 deploy/ 配置与 docs/contracts/ 是否有字面 \`http://\` 的硬编码
**M4 渲染器与 M5 网关的代码此刻在别的分支上、你的 worktree 里没有**，所以模板与菜单渲染那部分**无法验证** —— 如实写明「待 M4/M5 合并后补验」，并给出补验的具体命令（例如 \`grep -rn 'http://' internal/render/ --include=*.go\` 应只在测试期望值里出现）。

任务 5（mTLS 决策）：写清范围与取舍 —— 双向认证要给每台机器发客户端证书，而裸机在装机前没有身份，这与 SPEC §4.2「首次请求必然无认证」的协议现实直接冲突。给出明确结论（做 / 不做 / 有条件做）与依据，不要含糊。

任务 6（UEFI HTTP Boot）：SPEC §1.3 与附录 B 指出它的架构码（x64 疑为 16）与 option 60 vendor-class \`HTTPClient\` 的必要性**都未核实**。用 curl / WebSearch 查 RFC 5970 核实，**核实不到就如实标注**，配置里以注释形式给出待核实的候选值而不是写一个可能错的活配置。

自检：两份文档产出；两个配置文件的增量不破坏原有结构（改后重读确认）；任务 1 明确区分了「已验证成立」「已验证不成立」「因代码不在本分支无法验证」三类;
verifyOutcome 里写明哪些前提验证被推迟到合并后、补验命令是什么。`,
    { label: 'ops:tls-increment', phase: 'Wrap', schema: R }),
])

const all = done.concat(wrap.filter(Boolean))
return {
  agents: all.length + '/10',
  files: all.flatMap(r => r.files || []),
  outcomes: all.map(r => r.verifyOutcome),
  acceptance: all.flatMap(r => r.acceptance || []),
  openQuestions: all.flatMap(r => r.openQuestions || []),
  notes: all.map(r => r.notes).filter(Boolean),
}
