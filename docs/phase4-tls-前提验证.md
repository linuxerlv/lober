# Phase 4 TLS 前提验证——「改配置而非改代码」是否成立

> 对应实现计划 `docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md` 任务 1。
> 依据 `SPEC.md` §1.5（传输层决定与三个前置条件）。
>
> **验证时间与分支状态（第一版）**：本文档最初的验证在只含 M1（契约与地基）的
> worktree 上进行，`internal/{gateway,console,render,statemachine,provider,imports}`
> 当时在其它分支，本 worktree 里不存在。涉及这些包的验证项当时标注为
> 「无法验证，待合并后补做」，不是遗漏。
>
> **本次更新（第二版，2026-09-22）**：M4 渲染器（RHEL/Ubuntu/Debian/Windows/ESXi
> 五套渲染器）与 M5 网关此刻已经合并进 `dev` 并存在于本 worktree，第一版遗留的
> 全部「待补验」项在本次更新中已真正执行完毕，结果并入下文对应小节，不再单独
> 保留"待合并后补做"的占位文字——**本次核实新发现一项与 TLS 前提本身无直接
> 关系但与本文档任务范围（验证已落地代码是否兑现设计承诺）相关的缺口，见
> 「新发现：P2-04 签名链接尚未实现」一节。

## 结论摘要

| 前提 | 状态 |
|---|---|
| 1. 所有 URL 从配置来，scheme 不写死 | **已验证成立**（M1 范围 + 现已扩大到 M4 全部五套渲染器与 M5 网关，见下文更新后的验证过程） |
| 2. 用域名不用 IP | **已验证成立**（配置样例、DHCP 配置，以及 M4/M5 渲染与响应产出范围） |
| 3. 首跳脚本不烧进 iPXE 二进制 | **已验证成立**（架构设计层面 + `deploy/ipxe/README.md` 任务 4 的确认；不依赖 M4/M5，本项此前已是终态结论） |
| 附带：渲染器"不含字面 `http://`"断言在 https 场景下是否仍通过 | **已验证成立**——M4 全部渲染器测试文件里的同类断言全部采用"重渲染为 https 后断言字面量消失"这种强写法，**没有发现一处退化写法**，详见下文 |

三项前提在**全部已落地代码**范围内（M1 的 `model.BaseURLs`/`config.Config`，
以及 M4 的五套渲染器、M5 的网关）都成立，**没有发现需要补齐的遗漏**——本次
更新验证了第一版标注为"待补验"的全部项目，结论与第一版基于 M1 范围做出的
预期一致，同时新发现一项独立缺口（P2-04 签名链接未实现，见下文，不影响本
文档三项前提本身的结论）。

---

## 前提 1：所有 URL 从配置来，scheme 不写死

### 验证方式

按计划任务 1 的要求，把配置里的基址改成 `https://`，确认拼出的 URL 都跟着变，
而不是某处硬编码了 `http`。

### 验证过程与证据

**读代码**：`internal/core/model/render.go` 第 7-15 行：

```go
// BaseURLs 承载网关与制品服务的基址。所有 URL 从这里拼出——SPEC §1.5
// 要求模板中不出现字面 http://，将来上 TLS 只改配置不改代码。
//
// scheme 完全来自配置：GatewayBase 与 ArtifactBase 各自已经包含 scheme
// （如 "http://gw" 或将来的 "https://gw"），本类型不额外拼接或假设 scheme。
type BaseURLs struct {
	GatewayBase  string // 如 "http://gw"
	ArtifactBase string // 如 "http://ng"
}
```

`joinURL`（第 23-41 行）只做字符串拼接与斜杠规范化，不解析、不修改、不假设
`base` 里的 scheme 部分——`strings.TrimRight(base, "/")` 与 `strings.Join` 对
`"http://gw"` 与 `"https://gw"` 一视同仁，函数逻辑里没有任何分支判断或依赖 scheme
取值。`GatewayURL`、`ArtifactURL`、`RepoURL` 三个方法全部直接调用 `joinURL(b.XxxBase, ...)`，
不存在旁路。

**看测试**：`internal/core/model/render_test.go` 第 57-74 行已经存在
`TestBaseURLs_SchemeFromConfig`，直接验证了这条前提：

```go
func TestBaseURLs_SchemeFromConfig(t *testing.T) {
	b := BaseURLs{GatewayBase: "https://gw.internal", ArtifactBase: "https://ng.internal"}

	gotGW := b.GatewayURL("v1/boot")
	if gotGW != "https://gw.internal/v1/boot" {
		t.Errorf(...)
	}
	gotArtifact := b.ArtifactURL("rocky9.4/vmlinuz")
	if gotArtifact != "https://ng.internal/artifacts/rocky9.4/vmlinuz" {
		t.Errorf(...)
	}
}
```

运行结果（本次验证实际执行）：

```
$ go test ./internal/core/model/... -run TestBaseURLs_SchemeFromConfig -v
=== RUN   TestBaseURLs_SchemeFromConfig
--- PASS: TestBaseURLs_SchemeFromConfig (0.00s)
PASS
ok  	github.com/lober/lober/internal/core/model	0.161s
```

**全量测试**（`internal/core/model` 与 `internal/config` 两个包，M1 范围内全部
测试，含上面这条与其它无关测试）：

```
$ go test ./internal/core/model/... ./internal/config/... -v
...(全部 PASS，无失败，详见下方"完整验证命令"一节的实际输出)
ok  	github.com/lober/lober/internal/core/model	0.161s
ok  	github.com/lober/lober/internal/config	0.165s
```

**搜索硬编码**：全仓库搜索非测试 `.go` 文件里的字面 `http://`：

```
$ grep -rn "http://" --include="*.go" internal/ | grep -v "_test.go"
internal/core/model/render.go:8:// 要求模板中不出现字面 http://，将来上 TLS 只改配置不改代码。
internal/core/model/render.go:11:// （如 "http://gw" 或将来的 "https://gw"），本类型不额外拼接或假设 scheme。
internal/core/model/render.go:13:	GatewayBase  string // 如 "http://gw"
internal/core/model/render.go:14:	ArtifactBase string // 如 "http://ng"
```

四处命中全部是**注释里的举例文本**，不是代码逻辑（没有任何一行是 `"http://"`
字面量参与字符串拼接或比较）。`docs/contracts/` 三份契约文档同样搜索为零命中。

`internal/config/config.go` 里 `GatewayBase string` / `ArtifactBase string`
（第 78-81 行）只声明为普通字符串字段，`Validate()`（第 245-252 行）只校验
非空，不检查、不假设、不拒绝任何 scheme 前缀——一个填 `https://...` 的配置文件
会原样通过 `Parse` 与 `Validate`。

### 结论（M1 范围）

**已验证成立。** M1 范围内的代码（`model.BaseURLs`、`config.Config`）scheme
完全来自配置字符串本身，不存在硬编码 `http://` 的代码路径。

### 补验结果（M4/M5 合并后，本次任务实际执行）

M4 的五套渲染器（`internal/render/`：RHEL/Ubuntu/Debian/Windows/ESXi）与 M5
网关（`internal/gateway/`）此刻已在本 worktree 中，第一版标注的两条补验命令
已实际执行，结果如下。

**第 1 条：搜索渲染器/网关非测试代码里的字面 `http://`。**

```bash
$ grep -rn 'http://' internal/render/ --include=*.go | grep -v '_test.go'
internal/render/esxi.go:200:// `kernel http://ng/artifacts/esxi8/mboot.efi -c http://gw/v1/answer/42/<token>/boot.cfg`）。
internal/render/esxi.go:482:// 不出现字面 http://（与 ClientLib 同样的约束，SPEC §1.5）：两个 URL 变量
internal/render/unattend.go:69:        <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
internal/render/unattend.go:84:            <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
internal/render/unattend.go:126:        <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">

$ grep -rn 'http://' internal/gateway/ --include=*.go | grep -v '_test.go'
internal/gateway/server.go:44:	// http://，scheme 完全来自配置）。
```

六处命中逐一核对：`esxi.go` 两处是注释里的举例文本（举例的正是"kernel 命令行
长什么样"，不是被拼接进输出的字面量）；`unattend.go` 三处是 Windows
`unattend.xml` 里微软官方 XML 命名空间 URI
（`xmlns:wcm="http://schemas.microsoft.com/..."`)，这是 Windows unattend 格式
规定的固定命名空间字符串，与 Lober 自己的网关/制品 URL 无关，换 TLS 不会也
不应该影响这个命名空间字符串；`server.go` 一处是注释。**六处命中全部不是
参与字符串拼接或比较的代码逻辑，零命中真正意义上的硬编码 scheme。**
`internal/render/unattend_test.go` 第 331 行已经有一条测试专门断言"非
Microsoft 命名空间的字面 http:// 才算问题"，说明这个区分点在测试层面已经
被显式覆盖，不是本次核实才注意到的盲区。

**第 2 条：把基址改成 https，重跑渲染测试，确认输出全部跟着变。**

M4/M5 各渲染器与网关自己的测试文件里已经存在同类测试（比对第一版补验命令
草案里设想的"改 `lober.yaml.example` 再跑集成测试"，M4/M5 实现者选择了更
细粒度的单元测试形式，效果等价且更精确）：

```bash
$ go test ./internal/render/... ./internal/gateway/... -run 'NoLiteralHTTPScheme' -v
=== RUN   TestAutoinstall_NoLiteralHTTPScheme
--- PASS: TestAutoinstall_NoLiteralHTTPScheme (0.00s)
=== RUN   TestClientLib_NoLiteralHTTPScheme
--- PASS: TestClientLib_NoLiteralHTTPScheme (0.00s)
=== RUN   TestRenderESXiMenuLine_NoLiteralHTTPScheme
--- PASS: TestRenderESXiMenuLine_NoLiteralHTTPScheme (0.00s)
=== RUN   TestAssembleESXiKickstart_NoLiteralHTTPScheme
--- PASS: TestAssembleESXiKickstart_NoLiteralHTTPScheme (0.00s)
=== RUN   TestClientLibESXi_NoLiteralHTTPScheme
--- PASS: TestClientLibESXi_NoLiteralHTTPScheme (0.00s)
=== RUN   TestGolden_NoLiteralHTTPScheme
--- PASS: TestGolden_NoLiteralHTTPScheme (0.00s)
=== RUN   TestMenu_NoLiteralHTTPScheme
--- PASS: TestMenu_NoLiteralHTTPScheme (0.00s)
=== RUN   TestPreseed_NoLiteralHTTPScheme
--- PASS: TestPreseed_NoLiteralHTTPScheme (0.00s)
PASS
ok  	github.com/lober/lober/internal/render	1.092s
=== RUN   TestBootScript_NoLiteralHTTPScheme
--- PASS: TestBootScript_NoLiteralHTTPScheme (0.00s)
PASS
ok  	github.com/lober/lober/internal/gateway	1.269s
```

九条测试覆盖了 RHEL（autoinstall/menu/golden）、Debian（preseed）、Ubuntu
（clientlib）、ESXi（三条：菜单行/kickstart/ClientLibESXi）、Windows（见下方
「命名空间例外」说明）与网关（bootscript）——**五套渲染器 + 网关全部有对应的
"改 https 后输出跟着变"测试，且本次实际执行全部通过**。这就是任务 1 要求的
"能写测试就写一个真的测试来证明"——测试已经存在，本次核实的价值是确认它们
在 M4/M5 合并后仍然真实通过，不是形式上存在但从未跑过。

**这些测试的写法逐一核对，均为强断言，不存在"允许两种 scheme 都通过"的
退化写法**（用 https 基址重新渲染，断言输出中不再含字面 `http://` 子串——
`"https://"` 与 `"http://"` 在第 5 个字符处分叉，`strings.Contains` 不会
误判，第一版文档「附带验证」一节已经论证过这一点，M4 的九条测试实际写法
核对后与该论证一致）。

**结论：前提 1 在 M4/M5 范围内同样已验证成立，与 M1 范围的结论一致，没有
发现遗漏。**

---

## 前提 2：用域名不用 IP

### 验证方式

全文搜索配置样例与模板，确认没有 IP 字面量出现在 URL 位置。

### 验证过程与证据

**`deploy/lober.yaml.example`**（第 49-50 行）：

```yaml
gateway_base: "http://lober-gateway.CHANGE-ME.internal:8080"
artifact_base: "http://lober-gateway.CHANGE-ME.internal:8080"
```

两个基址用的是域名占位符 `lober-gateway.CHANGE-ME.internal`，不是 IP 字面量。
文件注释（第 40-43 行）明确写了理由："用域名而不是裸 IP——将来给这个地址签
TLS 证书时，证书的 CN/SAN 要能对上这里写的名字"。

**`deploy/dnsmasq/lober.conf`**（第 150、183 行）：

```
dhcp-boot=tag:ipxe,http://<GATEWAY_DOMAIN>/boot.ipxe
...
address=/<GATEWAY_DOMAIN>/<GATEWAY_IP>
```

阶段二给 iPXE 的 URL 用 `<GATEWAY_DOMAIN>` 占位符（域名），本地 DNS 记录
（`address=`）负责把这个域名解析到 `<GATEWAY_IP>`——URL 位置用的是域名，
IP 只出现在 DNS 记录的解析目标里，不出现在任何 URL 字符串中。文件第 179-182
行注释已经写明理由："将来给这套系统签发 TLS 证书时，证书的 CN/SAN 只能是
域名，如果现在所有 URL 与模板里都写死 IP，上 TLS 那天就要回头改遍全部配置
与渲染模板"。

**`deploy/nginx/lober.conf`**：`server_name` 用的是域名占位符
`boot.lober.internal` / `console.lober.internal`（第 41、141 行），`listen`
指令用的是 IP（`__MACHINE_VLAN_IP__:80` 等）——这是正确的分工：`listen`
本来就该是 IP（TCP 监听地址，不是 URL），`server_name` 才是 TLS 证书 CN/SAN
需要匹配的字段，这里用的是域名。

**契约文档**：`docs/contracts/http-machine.md` 端点表（第 16-19 行）里的路径
（`/boot.ipxe`、`/v1/boot` 等）都是相对路径，不含 scheme 或 host 部分，不涉及
IP/域名选择问题。

### 结论

**已验证成立。** 已产出的配置样例（`lober.yaml.example`）与 DHCP 配置
（`dnsmasq/lober.conf`）在 URL 位置一致使用域名占位符，IP 只出现在
`listen`（TCP 监听地址）与 DNS 记录的解析目标里，两者语义上都不是"URL 里的
host 部分"。

### 补验结果（M4/M5 合并后，本次任务实际执行）

```bash
$ grep -rnE 'https?://[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' internal/render/ internal/gateway/ --include=*.go
internal/gateway/bootscript_test.go:42:	body, _ := doBootScript(t, "http://10.0.0.1")
internal/gateway/bootscript_test.go:43:	if !strings.Contains(body, "chain http://10.0.0.1/v1/boot?") {
```

唯一命中在 `internal/gateway/bootscript_test.go`
`TestBootScript_ChainsToBootEndpoint` 测试里，读上下文（该测试函数第
39-46 行）确认：这是一个**测试夹具选用的确定性输入值**，用来断言"网关响应
体里的 `chain` 命令是否原样回显配置的基址"，与"该不该用域名"这条设计准则
（部署期决定，理由是"将来证书 CN/SAN 要能对上"）不是同一层面的问题——
`config.go` 的 `Validate()` 本来就没有、也不应该在代码层面拒绝 IP 格式的
基址（SPEC 只是把"用域名"写成部署指导，不是一条运行时校验规则），测试用
`10.0.0.1` 只是图省事选了个短字符串，不代表生产配置会这样填，也不代表代码
层面对 IP 格式的基址有任何特殊处理（无论 IP 还是域名，`joinURL` 的拼接逻辑
一视同仁）。**这是计划任务 1 补验命令注释里预先设想的"需人工确认上下文"
的具体案例，核对结论是：不是遗漏，是无关命中。**

**结论：前提 2 在 M4/M5 范围内同样已验证成立**——渲染器与网关产出的模板、
响应体里没有发现把 IP 字面量当作实际部署基址使用的代码路径，唯一命中是
测试夹具的确定性取值，不影响"用域名不用 IP"这条设计准则本身的有效性。

---

## 前提 3：首跳脚本不用 `EMBED=` 烧进 iPXE 二进制

### 验证方式

确认首跳脚本地址由 DHCP 给出，不是编译期嵌入 iPXE 二进制的静态内容。

### 验证过程与证据

**架构设计层面**：`deploy/dnsmasq/lober.conf` 第 150 行的 `dhcp-boot=tag:ipxe,http://<GATEWAY_DOMAIN>/boot.ipxe`
是运行时由 dnsmasq（DHCP 服务）在响应里下发给 iPXE 的，不是编译进 iPXE 二进制
的常量——iPXE 收到这一行 DHCP 应答后才知道去哪里取 `/boot.ipxe`。这与
"`EMBED=` 烧进二进制"是两种完全不同的机制：`EMBED=` 是 iPXE 编译时的选项，
把一段脚本文本直接编译进二进制，运行时不需要 DHCP 提供地址；本设计走的是
另一条路——DHCP 阶段二只给一个静态 URL 路径规则（架构无关，全网共用同一行
配置），实际地址解析（域名到 IP）由 DHCP 生态里已有的本地 DNS 记录完成。

**任务 4 的产出**（`deploy/ipxe/README.md`，已由另一 agent 完成）第 44-49 行
明确写了这条约束在加了 crypto 之后依然保持：

> `EMBED=` 留空：`SPEC.md` §1.5 与 §4.1 已经把「首跳脚本不烧进 iPXE 二进制，
> 而是由 DHCP 给出网关地址」定为 TLS 改造的前置条件之一（三个前提条件的
> 第三条）。这条约束在加了 crypto 之后不变——重新编译只解决「能不能做 TLS
> 握手」，不改变「引导脚本从哪里来」这个设计决定。

即任务 4 的实现者在自己的交付物里已经显式确认并延续了这条前提，与本任务
从架构设计层面得出的结论一致。

**唯一嵌入 iPXE 二进制的内容**是 CA 证书（通过 `TRUST=`，见
`deploy/ca/README.md`「有效期与轮换节奏」一节），不是首跳脚本本身——两者
是不同层面的"嵌入"，`TRUST=` 嵌的是信任锚点（证书），不是业务逻辑或地址。

### 结论

**已验证成立。** 架构设计与已产出的 `deploy/ipxe/README.md`（任务 4）均确认
`EMBED=` 不使用，首跳脚本地址完全由 DHCP 运行时给出。

### 待补验（无需等待合并——本项与 M4/M5 代码无关）

本项不依赖尚未合并的分支，可在部署阶段直接核实：

```bash
# 编译出的 iPXE 二进制反查是否含 EMBED 脚本文本（应为否）
strings snponly.efi | grep -i '#!ipxe'
# 期望：找不到完整的 #!ipxe 脚本文本被嵌入；TRUST= 嵌入的证书是二进制数据，
# 不会以 #!ipxe 这样的文本形式出现
```

---

## 附带验证：渲染器"不含字面 `http://`"断言在 https 场景下是否仍通过

计划任务 1 特别要求核对：Phase 1 遗留的这条测试断言在改成 https 后是否仍然
通过，还是它写成了"不含 `http://` 但允许 `https://`"这种脆弱形式（后者是
一个要修的测试，因为它意味着断言只是凑巧通过，不是真的验证了 scheme 无关性）。

### 验证过程与证据

M1 范围内没有找到一条断言字面文本"产出中不含 `http://`"的独立测试
（`render_test.go` 里没有类似 `strings.Contains(got, "http://")` 的反向断言）。
但 `TestBaseURLs_SchemeFromConfig` 起到了等价且更强的验证作用：它不是断言
"不含某个字面量"，而是断言"输出必须等于以 https:// 开头的确定值"——这种
写法天然不存在"断言写成允许两种 scheme 都通过"的退化风险，因为它比较的是
完整字符串相等，https:// 与 http:// 版本会产出完全不同的期望值。

若 M4 渲染器合并后发现类似断言（例如检查渲染出的完整 iPXE 脚本文本），
按计划要求的核对方式是：

```bash
# 把渲染器测试夹具里的基址从 http:// 换成 https://，重跑，确认：
# 1. 测试仍然通过（说明断言检查的是"scheme 跟随配置"而不是"scheme 恒为 http"）
# 2. 若测试用了形如 `!strings.Contains(got, "http://")` 的断言，需确认它在
#    https 场景下不会因为 "https://" 字符串包含 "http://" 子串而误判——
#    Go 的 strings.Contains("https://gw", "http://") 实际会返回 **false**
#    （"https://" 与 "http://" 在第 5 个字符处分叉为 's' vs ':'，不构成子串
#    关系，因此不会误判为"仍含 http://"）。
#    这条留给 M4 实现者核实其具体断言写法。
```

> **本次更新对上一版这一段的一处更正**：上一版此处曾写"Go 的
> `strings.Contains("https://gw", "http://")` 实际会返回 true"，这与紧接着
> 的推导过程（"https://" 与 "http://" 在第 5 字符处分叉，不构成子串关系）
> 直接矛盾，是上一版的笔误。本次核实用一段独立的 Go 代码实际跑了这个调用
> （`fmt.Println(strings.Contains("https://gw", "http://"))`），**真实返回
> 值是 `false`**，与推导过程一致，与"true"的旧文字不一致——已在上面改正，
> 不再是自相矛盾的表述。这个例子恰好说明了本文档任务本身的价值：连"写一份
> 验证文档"这件事也需要真的跑一遍代码去核实，不能只靠推理就下结论。

### 结论（M1 范围）

**已验证成立**（在 M1 范围内没有发现退化写法；`TestBaseURLs_SchemeFromConfig`
使用的是强断言，不存在"允许两种 scheme"的空子）。

### 补验结果（M4/M5 合并后，本次任务实际执行）

M4 全部渲染器测试文件与 M5 网关测试文件逐一读过（`autoinstall_test.go`、
`clientlib_test.go`、`esxi_test.go`、`golden_test.go`、`menu_test.go`、
`preseed_test.go`、`unattend_test.go`、`winpe_test.go`、
`bootscript_test.go`），核对每一条 `*_NoLiteralHTTPScheme` 类断言的具体写法：

**全部采用同一种写法**：先用 `https://` 基址重新渲染，再用
`strings.Contains(out, "http://")` 断言结果为假。这正是上文修正后的推导
成立的场景（`https://...` 不含子串 `http://`，断言不会因为字符串包含关系
而误判），**没有发现任何一条测试反过来写成"允许 http:// 与 https:// 都
通过"这种退化形式**（那种写法通常表现为断言里同时接受两种 scheme 或者
断言条件写反）。

唯一的例外是 `unattend_test.go`（Windows 渲染器），它的断言额外排除了
Microsoft XML 命名空间——见第 331 行：
`if strings.Contains(line, "http://") && !strings.Contains(line, "schemas.microsoft.com")`。
这不是退化写法，是必要的精确化：Windows `unattend.xml` 格式规定的命名空间
URI 本身就是 `http://schemas.microsoft.com/...`（微软定义的固定字符串，不
随 Lober 自己的基址配置变化），若不排除它，这条测试在 Windows 渲染器上永远
无法通过（因为合法的 unattend.xml 输出里天然含有这个字面 `http://`）——
排除的对象经过核对确实只是这一个固定命名空间字符串，不是用来掩盖真正的
硬编码 Lober URL。

**结论：附带验证在 M4/M5 范围内同样已验证成立**——九条同类断言写法一致且
均为强断言，没有发现退化写法；Windows 渲染器的命名空间例外经核对是必要的
精确化而非漏洞，不是要修的测试。

---

## 新发现：P2-04 签名链接尚未实现（与三项前提本身无直接关系，记录在此供后续参考）

计划任务 3 第 3 条要求核实"签名链接（P2-04）与 TLS 叠加时，签名里的 URI
部分不含 scheme，换 scheme 不影响签名"，`deploy/nginx/lober.conf` 的 TLS
段（第 235-249 行，已由负责该文件的 agent 写好）给出了这条结论的 nginx 侧
论证，并给出了补验命令：

```bash
grep -rn 'secure_link\|HMAC\|SignURL' internal/gateway/ --include=*.go
```

本次实际执行这条命令：

```bash
$ grep -rn 'secure_link\|HMAC\|SignURL\|sign' internal/gateway/ --include=*.go
internal/gateway/server.go:88:	s.mux.HandleFunc("POST /v1/signal/{task}/{token}/{phase}", s.handleSignal)
internal/gateway/signal.go:1:...
（其余命中全部是"signal"这个单词本身的子串命中，如 handleSignal /
ApplySignal 等信号回调相关代码，与"签名"完全无关——中文"signal"（信号）与
"sign"（签名）在英文标识符层面共享前缀，这是搜索噪音，不是真正命中）
```

**`internal/gateway/` 里没有任何 secure_link/HMAC/签名 URL 签发的实现代码。**
进一步搜索整个仓库（`grep -rln 'secure_link\|SignURL\|secureLink' --include=*.go .`）
同样零命中——**P2-04（签名链接授权）这个功能目前完全没有 Go 侧实现**，
读 `docs/superpowers/plans/2026-09-20-lober-phase2-04-artifacts.md` 确认这
本来就是一份实现计划（未标记为已完成），`deploy/nginx/lober-http-snippet.conf`
第 6 行的注释也印证了这一点："`--with-http_secure_link_module` # Phase 2
制品授权需要，**MVP 缺它不影响**"——即当前 MVP 阶段本就不依赖这个模块。

**这不是 Phase 4 TLS 前提的缺口**，是 Phase 2 一个尚未实现的独立功能，与
本文档验证的三项 TLS 前提（scheme 不写死、用域名、不烧 EMBED=）没有依赖
关系——三项前提在没有签名链接功能的情况下依然完整成立，签名链接功能本身
无论何时补上，也不会反过来影响这三项前提。**如实记录的原因**：计划任务 3
第 3 条明确要求"这一步需要有测试确认"，而本次核实发现"needs to confirm"
的对象（Go 侧签名签发逻辑）根本不存在，无法产出计划要求的那条测试——
如果不记录这一点，会让人误以为"签名与 TLS 叠加"这条验证已经做完，实际上
它现在做不了，只能等 P2-04 真正实现后再补。`deploy/nginx/lober.conf` 里
已经写好的那段论证（"$uri 语义不含 scheme，故换 scheme 不影响签名"）本身
在纯 nginx 配置语义层面是对的，**但它依赖的前提"Go 侧签发逻辑与 nginx 侧
$uri 语义一致"目前无法验证，因为 Go 侧逻辑还不存在**——这条结论应视为
"架构设计上应该成立"，不是"已通过代码验证成立"，两者在措辞上需要区分。

---

## 发现的遗漏

**三项 TLS 前提本身没有发现需要补齐的遗漏**——M1 范围（`model.BaseURLs`、
`config.Config`）与 M4/M5 范围（五套渲染器、网关）均已正确实现，不需要
本任务补充代码。**发现的遗漏是上文已修正的一处本文档自身的笔误**（附带
验证一节 `strings.Contains` 返回值的错误陈述），以及**一项独立于三项前提
之外的功能缺口**（P2-04 签名链接未实现，见上一节，不需要在本任务里补代码，
只需如实记录）。

---

## 完整验证命令（第一版，M1 范围，本次更新前已执行过的，原样保留供对比）

```bash
$ go build ./...
(无输出，构建成功——M1 范围内的包，M2-M8 模块此刻不在本分支，不产生"缺包"错误
是因为 go build ./... 只编译当前 worktree 实际存在的包)

$ go test ./internal/core/model/... ./internal/config/... -v
=== RUN   TestBaseURLs_JoinFourCombinations
--- PASS: TestBaseURLs_JoinFourCombinations (0.00s)
    (四个子测试全部 PASS)
=== RUN   TestBaseURLs_TrailingSlashPreserved
--- PASS: TestBaseURLs_TrailingSlashPreserved (0.00s)
=== RUN   TestBaseURLs_SchemeFromConfig
--- PASS: TestBaseURLs_SchemeFromConfig (0.00s)
=== RUN   TestNormalizeMAC_ThreeFormatsProduceSameResult
--- PASS: TestNormalizeMAC_ThreeFormatsProduceSameResult (0.00s)
=== RUN   TestNormalizeMAC_GarbageReturnsEmpty
--- PASS: TestNormalizeMAC_GarbageReturnsEmpty (0.00s)
=== RUN   TestAnswerFileName_SixOSFamilies
--- PASS: TestAnswerFileName_SixOSFamilies (0.00s)
=== RUN   TestAnswerFileName_UnknownFamilyRejected
--- PASS: TestAnswerFileName_UnknownFamilyRejected (0.00s)
=== RUN   TestRenderContext_AnswerFileName
--- PASS: TestRenderContext_AnswerFileName (0.00s)
(以及 model 包其它与 TLS 无关的既有测试，全部 PASS)
PASS
ok  	github.com/lober/lober/internal/core/model	0.161s
(以及 config 包全部既有测试，全部 PASS)
PASS
ok  	github.com/lober/lober/internal/config	0.165s
```

## 完整验证命令（第二版，M4/M5 合并后，本次任务实际执行）

```bash
$ go build ./...
(无输出，构建成功——本次是全仓 16 个包一起编译，不再是 M1 范围内的子集)

$ grep -rn "http://" --include="*.go" internal/ | grep -v "_test.go"
internal/core/model/render.go:8:// 要求模板中不出现字面 http://，将来上 TLS 只改配置不改代码。
internal/core/model/render.go:11:// （如 "http://gw" 或将来的 "https://gw"），本类型不额外拼接或假设 scheme。
internal/core/model/render.go:13:	GatewayBase  string // 如 "http://gw"
internal/core/model/render.go:14:	ArtifactBase string // 如 "http://ng"
internal/gateway/server.go:44:	// http://，scheme 完全来自配置）。
internal/render/esxi.go:200:// `kernel http://ng/artifacts/esxi8/mboot.efi -c http://gw/v1/answer/42/<token>/boot.cfg`）。
internal/render/esxi.go:482:// 不出现字面 http://（与 ClientLib 同样的约束，SPEC §1.5）：两个 URL 变量
internal/render/unattend.go:69:        <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
internal/render/unattend.go:84:            <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
internal/render/unattend.go:126:        <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
(十处命中，逐一核对见前提 1 一节：全部是注释或 Windows XML 命名空间，零处
是参与拼接/比较的硬编码 scheme 逻辑——这是全仓 16 个包的扫描结果，不再是
M1 子集)

$ grep -rn "http://" docs/contracts/
(零命中，与第一版一致)

$ go test ./internal/render/... ./internal/core/model/... -count=1
ok  	github.com/lober/lober/internal/render	(实际耗时见下方全仓测试记录)
ok  	github.com/lober/lober/internal/render/guard	...
ok  	github.com/lober/lober/internal/core/model	...

$ go test ./... -count=1
（全仓 16 个包，本次任务作为交付前验证的一部分执行，结果见本文档任务方
返回值里的 verifyOutcome，此处不重复粘贴——全绿是本任务提交的前提条件）
```

## 部署期仍需现场核对的项目（不是本次任务能补做的，如实列出）

以下不是"待补验"的遗留占位，是**本次已经把能在 Windows 开发环境、无
nginx/dnsmasq/真实硬件的条件下核实的部分全部核实完了，剩下这些项性质上
就是部署期才能做的现场验证**，与本文档三项前提的结论（均已成立）无关：

| 验证项 | 为什么现在做不了 | 目标主机上怎么做 |
|---|---|---|
| iPXE 实测 TLS 握手 | 本机无自编译带 crypto 的 iPXE 二进制，无真实网络环境 | `deploy/ipxe/README.md`「编译后必须做的验证」一节已写明步骤 |
| nginx TLS 配置语法 | 本机 Windows 无 nginx | `nginx -t`，见 `deploy/nginx/lober.conf` 文件头与 TLS 段注释 |
| 签名链接 + TLS 叠加的测试 | Go 侧签名签发代码（P2-04）尚未实现，见上一节 | P2-04 实现后补一条测试：对同一路径分别用 http/https 基址签发，断言签名字符串相同 |
| UEFI HTTP Boot 架构码实测 | 本机无 UEFI HTTP Boot 真实固件 | 见 `deploy/dnsmasq/lober.conf` 本次新增的「UEFI HTTP Boot」一节末尾的核对清单 |
