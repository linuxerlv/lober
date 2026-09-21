# iPXE 重新编译（带 crypto，嵌入内网 CA）

> 对应实现计划 `docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md` 任务 4。
> 依据 `SPEC.md` §1.5（传输层决定与三个 TLS 前置条件）、§1.2/§1.3（引导链与
> 四种固件架构）。

## 为什么必须重新编译

**这是 HTTPS 在这套栈里最贵的部分**（`SPEC.md` §1.5 原话）。

Lober 官方分发的预编译 iPXE 二进制（`undionly.kpxe`、`snponly.efi` 等）
默认**不带 crypto 支持**——它们能做明文 HTTP 引导（MVP 现状），但不能做
HTTPS：iPXE 的 TLS 实现是编译时选配的功能，不在默认构建里，因为它会显著
增大二进制体积（见下方「坑 2」）而多数部署不需要它。

要让 iPXE 走 HTTPS 取 `/boot.ipxe` 与后续的 `/v1/boot`，必须：

1. 编译时开启 crypto 支持。
2. 把内网 CA 证书嵌进二进制，否则 iPXE 会因为「无法验证服务端证书链」而
   拒绝连接（自建 CA 签发的证书对任何未预置该 CA 的 TLS 客户端都是不可信的，
   iPXE 没有操作系统级的证书信任存储可以依赖）。

这两件事都只能在编译时完成，运行时无法后补——这正是「重新编译」这个词
在本任务里的具体含义，不是配置调整。

## `TRUST=` 怎么把内网 CA 嵌进二进制

iPXE 的构建系统支持在 `make` 时传入 `TRUST=<证书文件路径>`，把一个或多个
PEM 格式的证书嵌入编译出的二进制。编译完成后，iPXE 在做 TLS 握手校验服务端
证书链时，只信任由 `TRUST=` 指定的证书（或它们签发的证书）——这就是让
iPXE 信任内网 CA 的机制。

**嵌入的是 CA 证书，不是某一张具体的服务端证书。** 这条决定的理由详见
`deploy/ca/README.md`「有效期与轮换节奏」一节：只要 CA 不换，服务端证书
的常规轮换（nginx 侧换证书文件）不需要重新编译 iPXE——iPXE 信任的是
「任何由这个 CA 签发的证书」，而不是「这一张特定的证书」。本目录的
`build-ipxe.sh` 因此要求传入 `--ca-cert`，接受 `deploy/ca/gen-ca.sh` 产出的
CA 证书文件，不接受服务端证书。

配套的 `CERT=none` 编译选项关掉 iPXE 官方默认嵌入的一组公网信任锚点——
气隙内网场景下那些证书永远用不到，没有理由让二进制隐式信任一组我们不管理
的证书颁发机构。

`EMBED=` 留空：`SPEC.md` §1.5 与 §4.1 已经把「首跳脚本不烧进 iPXE 二进制，
而是由 DHCP 给出网关地址」定为 TLS 改造的前置条件之一（三个前提条件的
第三条）。这条约束在加了 crypto 之后不变——重新编译只解决「能不能做 TLS
握手」，不改变「引导脚本从哪里来」这个设计决定。`/boot.ipxe` 仍然是全网
共用的一行静态文本（`SPEC.md` §4.7），二进制本身不含任何机器身份或路由
信息。

## 四个编译产物对应哪四个文件

对应 `SPEC.md` §1.3 的四条 `dhcp-boot` 配置、四种固件架构：

| 架构 | option 93 | make 目标 | TFTP 文件名 |
|---|---|---|---|
| BIOS PXE | 0 | `bin/undionly.kpxe` | `undionly.kpxe` |
| IA32 UEFI | 6 | `bin-i386-efi/snponly.efi` | `snponlyx32.efi` |
| **x64 UEFI** | **7（主流值）/ 9（旧值，同一 tag 兜底）** | `bin-x86_64-efi/snponly.efi` | `snponly.efi` |
| ARM64 UEFI | 11 | `bin-arm64-efi/snponly.efi` | `snp-arm64.efi` |

四个都要编，即使 MVP 阶段 ARM64 那行 DHCP 配置可以不放二进制
（`SPEC.md` §1.3 原文：「ARM64 那行 MVP 阶段可以不放二进制，但 tag 建议
先留」）——一旦要支持 TLS，就应该四个架构一起做，避免将来单独为 ARM64
补一次编译流程。

`build-ipxe.sh` 一次运行产出全部四个文件，加一份 `sha256sums.txt`。

## 用法

**必须在一台有外网访问、装好编译工具链（gcc、binutils、perl 等）的机器
上运行**——要拉取 iPXE 源码，气隙内网里没有源码也没有工具链。这与
`SPEC.md` §12.3 前端构建「必须在有外网的机器上做」是同一类约束：产物
（这里是四个二进制文件）编完之后带入内网，内网本身不编译。

```bash
# 先跑 dry-run 核对命令与路径：
bash build-ipxe.sh --dry-run --ca-cert /path/to/ca.cert.pem

# 确认无误后正式构建（--ipxe-ref 必须先锁定到具体版本，见下）：
bash build-ipxe.sh \
  --ca-cert /path/to/ca.cert.pem \
  --ipxe-ref <锁定的 commit 或 tag> \
  --out-dir ./out
```

`--ipxe-ref` 默认是占位值 `CHANGE-ME-PIN-A-COMMIT-OR-TAG`，脚本在非
`--dry-run` 模式下会拒绝以占位值执行真实构建。这不是脚本的限制，是刻意的
保护：锁定具体版本（而不是跟随分支头）是为了让编译产物可复现——
「今天编的」与「下次任何人再编一遍」应该拿到完全相同的源码，这与
`SPEC.md` §12.9「pin 住一个上游 commit，升级当独立任务做」是同一个原则，
应用在 iPXE 而不是前端框架上。选定版本前，建议先看一遍 iPXE 上游的
release 说明，确认没有已知的 TLS 相关缺陷。

## 记录二进制的 sha256

**编译出的二进制必须记录 sha256 校验值。**`SPEC.md` 的部署文档已经要求
记录官方 iPXE 二进制的校验值；自己编译出来的更要记，因为**它们不能从
官方下载处复核**——官方发布的哈希值对应的是官方构建，我们自己编的二进制
哪怕源码版本相同，只要构建环境（工具链版本、编译选项）有差异，产出的
字节就可能不同。`build-ipxe.sh` 编译完成后自动生成 `sha256sums.txt`，
连同 `ipxe-source-commit.txt`（记录本次构建对应的 iPXE 源码 commit）一起
带入内网，作为将来核对「TFTP 目录里的这个文件到底是哪次构建产出的」的
唯一依据。

## 编译后必须做的验证，不要只看编译成功

**两个可能的坑：**

| 坑 | 表现 | 应对 |
|---|---|---|
| 嵌入的 CA 格式或路径不对 | iPXE 报证书验证失败（TLS 握手阶段），但 `make` 本身可能编译成功、不报任何错 | **编译后用一次实际 HTTPS 请求验证**，不要只看 `make` 的退出码。找一台机器（或用 iPXE 的 shell 模式手工执行 `chain https://...`）实际发一次请求到已配好 TLS 的 nginx，确认能成功建立连接并取到内容 |
| **二进制体积变大** | crypto 支持会让二进制大出可观比例；TFTP 传输这个更大的文件会变慢 | 编译后对比加 crypto 前后的文件体积，把增幅记录下来——这个数字是判断「要不要做 UEFI HTTP Boot」的依据（`SPEC.md` §11 Phase 4 把 UEFI HTTP Boot 列为附属任务，动机正是这个体积增幅；具体的 DHCP 配置改动属于本计划任务 6，本目录不涉及） |

第二个坑的量级必须实测：拿 `sha256sums.txt` 旁边的文件体积（`ls -l` 或
`stat`）与之前未加 crypto 的官方二进制体积做对比，记录下增幅百分比。
如果 TFTP 传输这个更大的文件在实测中仍然是秒级（`SPEC.md` §10 失败模式 8
的原有判断：「TFTP 只发最小 iPXE 二进制」这个前提在体积增大后是否还成立），
就不必现在就做 UEFI HTTP Boot；如果实测出可感知的延迟，再启动任务 6。

## 证书轮换时的重发步骤

**运维流程，写进本文档：**证书轮换分两种，触发的动作完全不同（详细清单
见 `deploy/ca/README.md`「轮换时哪些地方要同步更新」一节，本节只讲
iPXE 侧要做什么）：

- **服务端证书轮换**（常规节奏，数年一次）：**不涉及 iPXE**，不需要重新
  编译、不需要重发任何文件。因为嵌入 iPXE 的是 CA 证书而不是服务端证书。
- **CA 证书轮换**（罕见，或私钥泄露的应急响应）：
  1. 用新 CA 的证书重新运行 `build-ipxe.sh`，产出新的四个二进制。
  2. 用新二进制替换 TFTP 目录里的同名文件（`undionly.kpxe` /
     `snponlyx32.efi` / `snponly.efi` / `snp-arm64.efi`，路径见
     `SPEC.md` §1.3 的四条 `dhcp-boot` 配置）。
  3. **正在装机中的机器不受影响**——它们在开机时已经把旧二进制加载进了
     内存并跑起来，替换 TFTP 目录里的文件不会影响已经在内存中运行的
     实例。只有下一次重新走 PXE 引导流程的机器才会拿到新二进制。
  4. 记录新二进制的 sha256（重复上面「记录二进制的 sha256」一节的步骤）。

## 与 Secure Boot PoC 的交集——不在这里重复展开

**自签的 iPXE 二进制之后，还要让 shim 信任它才能在启用 Secure Boot 的机器
上引导。** 这是同一件事（「让 iPXE 在这套栈里被信任」）的两个独立层面：

- **本文档解决的是「iPXE 信任谁」**——编译时嵌入内网 CA，让 iPXE 在做
  TLS 握手时信任我们自己签发的证书。这是应用层（HTTPS）的信任问题。
- **Secure Boot PoC 解决的是「固件信任谁来执行」**——`shim.efi` 是否
  信任我们自签名的 `ipxe.efi` 二进制本身可以被执行。这是引导链完整性
  （代码签名）的信任问题，与上面的 TLS 证书信任是两条独立的信任链，
  只是恰好都因为「我们自己编译/签名了东西」而需要建立。

两者的交集在于：一旦本文档产出了自编译的 iPXE 二进制，如果目标环境启用
了 Secure Boot，这个二进制就需要走 Secure Boot PoC 里描述的
`shim.efi → ipxe.efi` 签名与 MOK 注册流程才能被执行——**否则编出来的
二进制在开了 Secure Boot 的机器上根本起不来，与 TLS 是否配置正确无关**。

Secure Boot 具体怎么做（`shim.efi` 链载机制、MOK 注册流程、签名工具链）
不在本文档展开，详见 `docs/secureboot-poc/`（由另一位 agent 负责编写，
`SPEC.md` §11 Phase 4 标注该项「技术风险最高，建议独立 PoC」）。若目标
环境不启用 Secure Boot，本文档产出的二进制可以直接使用，不需要额外步骤。

## 与 nginx/dnsmasq 的边界

本文档与 `build-ipxe.sh` 只负责编译 iPXE 二进制本身。nginx 侧的 TLS
配置（协议版本、密码套件按 iPXE 实际支持收紧）是本实现计划的**任务 3**，
由另一位 agent 负责修改 `deploy/nginx/lober.conf`——本目录不修改那个
文件。具体来说：

- **机器面的 TLS 协议版本与密码套件要按本文档编译出的 iPXE 实际支持来定**
  （`SPEC.md` 任务 3 原文：「先配一个宽松的，实测后收紧」）——这一步需要
  先有本文档产出的带 crypto 的 iPXE 二进制，实测它能协商上什么密码套件，
  再回去收紧 nginx 配置。这是本文档产出与任务 3 之间的依赖顺序：
  **先编译，后收紧 nginx 配置**。
- 配置侧的具体改动见 `deploy/nginx/lober.conf` 的 TLS 段，本 README 不
  重复展开。

dnsmasq 侧的 UEFI HTTP Boot 相关改动（若上面「坑 2」的实测结果触发了
任务 6）同理，改的是 `deploy/dnsmasq/lober.conf`，本目录不涉及。
