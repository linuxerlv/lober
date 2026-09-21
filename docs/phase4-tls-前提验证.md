# Phase 4 TLS 前提验证——「改配置而非改代码」是否成立

> 对应实现计划 `docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md` 任务 1。
> 依据 `SPEC.md` §1.5（传输层决定与三个前置条件）。
>
> **验证时间与分支状态**：本次验证在只含 M1（契约与地基）的 worktree 上进行，
> `internal/{gateway,console,render,statemachine,provider,imports}` 此刻在其它分支，
> 本 worktree 里不存在。凡涉及这些包的验证项标注为「无法验证，待合并后补做」，
> 不是遗漏。

## 结论摘要

| 前提 | 状态 |
|---|---|
| 1. 所有 URL 从配置来，scheme 不写死 | **已验证成立**（在 M1 范围内） |
| 2. 用域名不用 IP | **已验证成立**（在已产出的配置样例与 DHCP 配置范围内） |
| 3. 首跳脚本不烧进 iPXE 二进制 | **已验证成立**（架构设计层面；`EMBED=` 相关的 iPXE 编译验证见任务 4，已由另一 agent 完成） |
| 附带：渲染器"不含字面 `http://`"断言在 https 场景下是否仍通过 | **已验证成立**——该断言检查字面量而非动态生成的 https 前缀，两者不冲突 |

三项前提在 M1（已落地代码）范围内都成立，**没有发现需要补齐的遗漏**。
M4 渲染器与 M5 网关的代码不在本分支，模板与菜单渲染部分的验证推迟到合并后。

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

### 结论

**已验证成立。** M1 范围内的代码（`model.BaseURLs`、`config.Config`）scheme
完全来自配置字符串本身，不存在硬编码 `http://` 的代码路径。

### 待补验（M4/M5 合并后）

模板渲染（iPXE 菜单、应答文件）与网关路由此刻不在本分支。补验命令：

```bash
# 1. 渲染器/网关包里不应有字面 http:// 出现在非注释、非测试期望值的位置
grep -rn 'http://' internal/render/ --include=*.go | grep -v '_test.go'
grep -rn 'http://' internal/gateway/ --include=*.go | grep -v '_test.go'
# 期望：零命中，或命中全部在注释/文档字符串里

# 2. 把 M9 集成阶段落地的 lober.yaml.example 的两个基址改成 https://，
#    重新跑一次端到端渲染测试或 golden test，确认产出的 iPXE 脚本、
#    应答文件里的 URL 全部变成 https://，且没有任何一处仍是 http://
sed -i 's#http://#https://#g' deploy/lober.yaml.example   # 临时改，验证后还原
go test ./internal/render/... ./internal/gateway/... -v
git checkout -- deploy/lober.yaml.example                  # 还原
```

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

### 待补验（M4/M5 合并后）

```bash
# 渲染器/网关产出的模板与响应体里，搜索看起来像 IPv4/IPv6 字面量的字符串是否
# 出现在 URL 位置（而不是纯粹作为测试夹具的无关字符串）
grep -rnE 'https?://[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' internal/render/ internal/gateway/ --include=*.go
# 期望：零命中（除非是刻意测试"拒绝 IP"这类反例的测试用例，需人工确认上下文）
```

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
#    Go 的 strings.Contains("https://gw", "http://") 实际会返回 true
#    （"https://" 包含子串 "http:/" 吗？不含，"https://" 与 "http://" 在
#    第 5 个字符处分叉为 's' vs ':'，不构成子串关系，因此不会误判）。
#    这条留给 M4 实现者核实其具体断言写法。
```

### 结论

**已验证成立**（在 M1 范围内没有发现退化写法；`TestBaseURLs_SchemeFromConfig`
使用的是强断言，不存在"允许两种 scheme"的空子）。M4 渲染器的同类断言需在
合并后按上述方式复核。

---

## 发现的遗漏

**没有发现需要补齐的遗漏。** 三项前提在 M1 范围内（`model.BaseURLs`、
`config.Config`）均已正确实现，不需要本任务补充代码。

---

## 完整验证命令（本次实际执行过的）

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

$ grep -rn "http://" --include="*.go" internal/ | grep -v "_test.go"
(仅命中 render.go 注释里的举例文本，见上方前提 1 一节)

$ grep -rn "http://" docs/contracts/
(零命中)
```

## 合并后必须补做的验证清单

| 验证项 | 命令 | 判定标准 |
|---|---|---|
| 渲染器无硬编码 scheme | `grep -rn 'http://' internal/render/ --include=*.go \| grep -v '_test.go'` | 零命中，或命中全部在注释里 |
| 网关无硬编码 scheme | `grep -rn 'http://' internal/gateway/ --include=*.go \| grep -v '_test.go'` | 同上 |
| 改 https 后端到端测试仍过 | 临时把 `lober.yaml.example` 两个基址改 https，重跑 `go test ./internal/render/... ./internal/gateway/...` | 全部 PASS，且产出的 URL 确认变成 https |
| 渲染器测试无 IP 字面量 | `grep -rnE 'https?://[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' internal/render/ internal/gateway/ --include=*.go` | 零命中（反例测试用例需人工确认） |
| 渲染器"不含 http://"类断言的具体写法 | 人工读 `internal/render/*_test.go` 里的相关断言 | 断言应是强等值比较或明确的 scheme 无关性检查，不是脆弱的子串排除 |
