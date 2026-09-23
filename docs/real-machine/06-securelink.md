# 06 secure_link 制品授权：Go 只签发 URL，不在数据路径上

> 本文档记录 t11-securelink 这次实现：SPEC §9.3 Phase 2 项——大制品
> （kernel/initrd/镜像/WIM）走 nginx `secure_link`（HMAC + 过期），Go
> 只签发 URL、完全不在数据路径上。
>
> 依据：SPEC §9.3、§1.6（`X-Real-IP` 陷阱）；
> `internal/gateway/siglink.go` 文件头注释；
> `deploy/nginx/lober.conf` 的 `/artifacts/secure/` location；
> `docs/real-machine/01-配置语法核对.md` 第 5 节（nginx 编译选项实测）。

## 默认关闭，向后兼容

本次实现是一个**可选加固开关**（`gateway.secure_link_enabled`，默认
`false`），不是 MVP 的硬性要求：

- 未打开时，制品仍走 `deploy/nginx/lober.conf` 现有的开放 `/artifacts/`
  location，行为与本次改动之前完全一致——任何既有部署文件不需要任何
  改动即可继续通过 `internal/config.Config.Validate()`。
- 打开后，网关会对渲染出的菜单文本里的大制品 URL（kernel/initrd，及
  Windows/ESXi 家族接线完成后的 wimboot/BCD/boot.sdi/boot.wim/
  mboot.efi）附加 `secure_link` 签名查询参数。

## 一个真实的架构问题：为什么不能直接在渲染器里签名

`internal/render` 是纯函数，`model.RenderContext` 没有、也不该有密钥
字段——这与该包「渲染器不查库」的设计原则是同一条线：不该持有的东西
就不该出现在它的输入里。但 `secure_link` 的签名恰恰需要密钥。

折中方式（本次实现选择的做法）：渲染器继续用
`model.BaseURLs.ArtifactURL` 产出未签名 URL，完全不变；网关在拿到
`RenderMenu` 的产出文本之后，做一次**后处理**——用与渲染器完全相同的
纯函数调用方式（`ctx.Bases.ArtifactURL(相对路径)`）独立算出未签名 URL
字符串作为定位锚点，再用签名版本替换掉它。网关是唯一持有密钥的模块，
这个后处理步骤全部发生在 `internal/gateway/boot.go` 的
`handleBootGrant` 里，`internal/render` 的任何文件都没有被改动。

## 第二个真实的架构问题：为什么需要一个独立的 nginx location

SPEC §9.3 的表格把制品分三类——离线仓库（必须开放）、大制品
（`secure_link`）、少量敏感大文件（`X-Accel-Redirect`）。但
`docs/contracts/filesystem.md` 记录的磁盘布局显示，kernel/initrd/repo/
hooks 四类文件在 MVP 阶段实际共享**同一个** nginx location
（`/artifacts/`，同一个 `alias`），且 `model.BaseURLs.ArtifactURL` 与
`RepoURL` 当前实现完全相同（都拼同一个 `/artifacts/` 前缀）。

这意味着不能直接在现有 `/artifacts/` location 上加 `secure_link`
指令——那样离线仓库（几万个包，installer 按 repodata 自行构造 URL）与
hook 脚本（`/artifacts/hooks/<digest>/script.sh`）的请求会因为不带
签名一起被 403，直接违反 SPEC §9.3「离线仓库必须保持开放，签了会让
装机在装包阶段整体失败」这条硬性要求。

本次实现的做法：`deploy/nginx/lober.conf` 新增一个独立的
`/artifacts/secure/` location，用第二个 `alias` 指向与 `/artifacts/`
完全相同的物理目录（`/srv/lober/artifacts/`），只在这一个新 location
上启用 `secure_link`；原有 `/artifacts/` 与 `/repo/` 两个 location 的
行为完全不变。网关把需要签名的 URL 前缀从 `/artifacts/` 改写成
`/artifacts/secure/` 之后再签名——重写后的路径就是 nginx 侧
`secure_link_md5` 公式里 `$uri` 的值。

## `$remote_addr` 陷阱：必须用 `X-Real-IP`

SPEC §1.6/§9.3 点名的陷阱：nginx 校验时用真实客户端 IP
（`$remote_addr`，在 nginx 自己的进程里，看到的就是装机 VLAN 上机器的
真实 IP），而 Go 进程在 nginx 之后，它自己看到的 peer 是 loopback。若
Go 签发时直接用自己看到的 IP，两侧 HMAC 永远不一致，机器取制品即 403。

本次实现直接复用 `internal/gateway/clientip.go` 已有的 `clientIP(r)`
函数（仅当直连 peer 是 loopback 时才信任 `X-Real-IP` 头部），不重新
实现一份判断逻辑。`internal/gateway/boot.go` 的
`handleBootGrant` 在需要签名时调用 `clientIP(r)`，不是
`r.RemoteAddr`。

## DHCP 租约陷阱：签名 TTL 与租期是两件不同的事

SPEC §9.3 末段：若机器在「取引导脚本」与「取制品」之间换了 DHCP 租约
导致 IP 变化，签名会失效。`deploy/dnsmasq/lober.conf` 已经是 24h 租期
（本次任务**只读确认，未修改该文件**）——这个 24h 是用来避免"整个
装机窗口内 IP 不变"，而签名 URL 的 TTL（`gateway.secure_link_ttl`，
默认 30 分钟）是一次引导内部的时间窗口，两者是不同量级的两件事，不应该
取相同的值。30 分钟远长于同一次 PXE 引导内「取引导脚本」到「取内核/
initrd」之间的正常间隔（秒级到分钟级），又远短于 24h 租约窗口。

## nginx 编译选项：Debian trixie 已确认带 `--with-http_secure_link_module`

`docs/real-machine/01-配置语法核对.md` 第 5 节记录了实测结论：Debian
trixie 的 `nginx` 核心包（`nginx-core`/`nginx-full`/`nginx-light`/
`nginx-extras` 四个变体共享的同一个二进制包）同时带有 `--with-threads`
与 `--with-http_secure_link_module` 两个编译选项，不需要重新编译 nginx
或换发行版包。

若目标发行版缺这个编译选项：`/artifacts/secure/` location 里的
`secure_link`/`secure_link_md5` 两条指令会让 `nginx -t` 直接报错。此时
应保持 `gateway.secure_link_enabled` 为 `false`（不使用这个
location）——运维需重新编译 nginx 或换用带该模块的发行版包才能启用这
项加固；这不会影响其它任何功能，MVP 阶段制品本就是开放的。

## 部署核对清单

1. `deploy/nginx/lober.conf` 里 `/artifacts/secure/` location 的
   `__SECURE_LINK_SECRET__` 占位符，替换成与
   `deploy/lober.yaml.example`（或实际部署配置）的
   `gateway.secure_link_secret` **逐字节相同**的值。两侧但凡有一个字符
   不同，机器取制品即 403。
2. 确认目标主机 `nginx -V 2>&1 | grep -- '--with-http_secure_link_module'`
   有输出（见上一节）。
3. 确认 `gateway.secure_link_enabled: true` 与非空
   `gateway.secure_link_secret` 成对出现——`internal/config.Config.
   Validate()` 会在启动时拒绝「打开但密钥为空」的配置，但不会检查这个
   密钥是否与 nginx 侧一致（那是两个独立配置文件，Go 无法在启动时读到
   nginx 的配置内容）。
4. `nginx -t` 通过后 reload。

## 本机验证范围与边界（Windows 开发环境，无 nginx 进程）

本机无法启动真实 nginx，`nginx -t` 跑不了，也无法用真实 HTTP 请求
核实 `/artifacts/secure/` location 的实际拦截行为。本次验证做了两类
能在本机完成、且能提供实质证据的替代：

1. **静态核对**：`deploy/nginx/lober.conf` 手工核对 nginx 配置语法
   （location 嵌套、指令拼写、分号），并用 Python 脚本核对全文件花括号
   配对数量一致（11 对 11），排除明显的结构性错误。这不等价于
   `nginx -t`——`nginx -t` 还会检查指令是否在合法的上下文层级、模块是否
   编译进当前 nginx 二进制等本机无法模拟的项，**需在目标主机补做**。
2. **独立实现的往返测试**（`internal/gateway/siglink_test.go` 的
   `verifySecureLinkIndependent`）：不调用 `siglink.go` 的签名函数本身，
   而是在测试文件里独立重新写一遍 nginx `secure_link_md5` 公式与
   base64 变体编码规则，用它校验 `siglink.go` 签发的 URL。这是本机
   没有真实 nginx 时唯一能证明"Go 签发的签名与 nginx 会计算出的签名
   一致"的办法——若两份独立实现在理解 nginx 公式的方式上都错了，往返
   测试仍然会通过（一种双写同一个错误的情形），这是本方法固有的边界，
   不是执行上的疏漏；缓解手段是本文档与 `siglink.go` 都逐句对照了
   nginx 官方文档记录的具体公式与编码规则（含"标准 base64 后 + 换 -、
   / 换 _、去掉末尾 ="这条转换规则的逐字核对），不是凭记忆猜测。

**结论：签名算法本身（HMAC 公式、base64 变体、过期判定、IP 绑定）已经
用两种独立实现互相验证，可以确信正确；nginx 配置语法与编译选项依赖
的实际生效情况仍需在目标主机上用 `nginx -t` 与真实 HTTP 请求核实**，
这一条待办与 SPEC 附录 B「部署期核对项，不做运行期检测」的分类一致。
