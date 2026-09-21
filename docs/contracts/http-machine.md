# 机器面 HTTP 契约（装机 VLAN）

> 冻结文档，不是计划。M5（网关）据此实现，M9（集成）与实机验证据此断言。
> 客户端是 iPXE 固件与 installer ramdisk 里的 busybox `curl`——没有 JSON 解析能力，
> 也没有 cookie jar。**响应一律是纯文本裸响应，不包裹 JSON**（SPEC §6.2）。
>
> 依据：SPEC §4.2、§4.3、§4.4、§4.6、§5.5、§6.1、§6.2、§6.4、§8.1、§9.1、§9.2。
> 类型字段名对应 `internal/core/model`：`Task`（task.go）、`TaskState`/`Phase`/
> `FailureReason`（task.go）、`SignalKind`（signal.go）、`TokenPurpose`（token.go）、
> `Host`/`ArtifactSet`/`Arch`/`FirmwareMode`（inventory.go）。

## 端点总表

| # | 方法 | 路径 | 认证 | 成功响应 | SPEC |
|---|---|---|---|---|---|
| 1 | GET | `/boot.ipxe` | 无 | `#!ipxe` 静态引导脚本 | §4.1、§4.7 |
| 2 | GET | `/v1/boot` | 无 | 见「约定 1」，始终 200 | §4.2、§4.3、§4.6 |
| 3 | GET | `/v1/answer/{task}/{token}/{file}` | token（路径段） | 渲染后的应答文件正文 | §4.4、§4.5、§5.1 |
| 4 | POST | `/v1/signal/{task}/{token}/{phase}` | token（路径段） | `200 OK`，空 body | §6.1、§6.2、§6.4 |
| 5 | POST | `/v1/facts/{task}/{token}` | token（路径段） | `200 OK`，空 body | §5.5 |
| 6 | GET | `/v1/facts/{task}/{token}` | token（路径段） | `key=value\n` 逐行纯文本 | §5.5 |
| — | UDP | `:514` | 无 | 无响应（单向接收） | §6.5 |

`GET /artifacts/...` 不在本文档范围——它由 nginx 直接服务，不经 Go（SPEC §1.4、§8.1）。

---

## 1. `GET /boot.ipxe`

全网共用一份、静态文本，不因机器身份产生分叉（SPEC §4.7：分叉全部收敛在 `/v1/boot`）。
职责只有一件：把 iPXE 变量拼进下一跳 URL。

**认证：** 无。

**成功响应：** `200`，`Content-Type: text/plain`，固定文本（网关 base URL 从配置注入，
不写死）：

```
#!ipxe
chain ${gateway-base}/v1/boot?sn=${serial}&mac=${net0/mac}&uuid=${uuid}&arch=${buildarch}&plat=${platform}
```

**错误行为：** 无分支——这个端点不查库、不做任何判断，渲染失败在部署期就会被发现，
不存在运行期错误路径。

---

## 2. `GET /v1/boot`

引导决策的唯一入口，**默认拒绝**（SPEC §9.1 第一道闸门）。

**认证：** 无。第一次接触时机器手上不可能有 token（SPEC §4.2）。

**查询参数：**

| 参数 | 对应 model 字段 | 说明 |
|---|---|---|
| `sn` | `Host.SN` / `Task.HostSN` | iPXE `${serial}` |
| `mac` | `Host.MAC` / `Task.HostMAC` | iPXE `${net0/mac}`，网关内部按 `model.NormalizeMAC` 归一化后再比较 |
| `uuid` | `Host.UUID` / `Task.HostUUID` | iPXE `${uuid}` |
| `arch` | 经 `model.ParseIPXEBuildArch` 解析为 `model.Arch` | iPXE `${buildarch}` |
| `plat` | 经 `model.ParseIPXEPlatform` 解析为 `model.FirmwareMode`，即 `RenderContext.ActualFirmware` | iPXE `${platform}` |

**决策流程**（SPEC §4.2、§4.3、§4.6）：

1. 按 `HostSN → HostMAC → HostUUID` 优先级回退，查找该主机是否有 `TaskStatePending` 的任务。
2. 查不到 → 返回「约定 2：`no_task`」。
3. 查到 → 做 SN + MAC 双因子校验（两者都要对上，SPEC §4.3、§9.1 第二道闸门）与 §4.6
   三项一致性校验（arch 一致性、固件模式兼容、制品完整性）。
   - 任一不过 → 该任务置 `TaskStateFailed`，`FailureReason` 按判定分支写具体值，
     **不消耗 `AttemptCount`**，返回「约定 2：`no_task`」同一形态的载荷。
   - 全过 → 签发装机 token（`TokenPurpose = TokenPurposeInstall`，
     `TokenExpiresAt = now + model.InstallTokenTTL`），`AttemptCount++`；
     若 `AttemptCount > model.MaxBootAttempts` 则置 `TaskStateFailed`
     （`FailureReasonAttemptExceeded`）并返回同上的 `no_task` 载荷；
     否则任务状态推进为 `TaskStateBooting`，渲染 iPXE 菜单，token 内嵌进后续
     所有 URL 的路径段。

**成功响应：** **始终 `200`**，`Content-Type: text/plain`。这是本端点唯一且不可违反的
约定——iPXE 遇到非 200 会跳到 `|| shell`，把机器停在交互式提示符上等人；
无论有没有任务、校验通没通过，都必须给出一个能让机器继续走（回本地盘或继续 PXE
轮询）的合法 iPXE 脚本。

- 有任务且校验通过：渲染出的 iPXE 菜单（按 SPEC §4.5 各 OS 家族形态，内嵌 token）。
- 无任务，或有任务但校验不过：见下方「约定 2」的固定载荷。

**错误行为：** 本端点不存在「返回非 200」这个分支。查询参数缺失或格式错误时，视为
查不到匹配主机，走「查不到」分支，同样返回 200 的 `no_task` 载荷——**不返回 400**。

---

## 3. `GET /v1/answer/{task}/{token}/{file}`

**认证：** token 走路径段（约定 3）。校验：token 存在、`TokenPurpose == TokenPurposeInstall`、
未过期、与路径中的 `{task}` 匹配、且 `model.AnswerFileServable(task.State)` 为真
（即 `TaskStateBooting` 或 `TaskStateInstalling`，见 `internal/core/model/token.go`）。

**路径参数：**

| 参数 | 说明 |
|---|---|
| `{task}` | `Task.ID` |
| `{token}` | 装机 token 原文 |
| `{file}` | 应答文件名，见约定 4 |

**成功响应：** `200`，渲染后的应答文件正文。`Content-Type` 按家族：
`ks.cfg`/`preseed.cfg`/`user-data` 用 `text/plain`，`autoinst.xml`/`unattend.xml`
用 `application/xml`，`boot.cfg` 用 `text/plain`。

**错误行为：**

| 情形 | 响应 |
|---|---|
| token 不存在/过期/用途不符/task 不匹配 | `403`，body 为简短纯文本原因 |
| task 状态不满足 `AnswerFileServable` | `403` |
| `{file}` 与该任务 `RenderContext.AnswerFileName()` 不一致 | `404` |
| 渲染本身出错（模板/参数问题） | `500`，body 为简短纯文本原因；同时记一条 `TaskEvent`（level=error） |

`{file}` 不接受 handler 自行拼接的候选名单——见约定 4。

---

## 4. `POST /v1/signal/{task}/{token}/{phase}`

**认证：** token 走路径段。装机 token 或 finalize token 均可，但 finalize token 只能发
`model.FinalizeCanSend` 为真的信号（`booted`/`init-start`/`init-done`/`ready`/
`hook-report`）；发送范围外的信号视为认证失败。

**路径参数：**

| 参数 | 说明 |
|---|---|
| `{task}` | `Task.ID` |
| `{token}` | 装机 token 或 finalize token |
| `{phase}` | `model.SignalKind` 的字符串值之一（如 `pre-start`、`hook-report`），必须能被 `model.LookupSignal` 查到，否则见错误行为 |

**Query 参数：**

| 参数 | 必需 | 说明 |
|---|---|---|
| `rc` | 是 | 整数退出码 |
| `seq` | 是 | 客户端侧自增序号（SPEC §6.2 示例），**与 `model.SignalEntry.Seq`（服务端信号表序号）是两个不同的概念**，不要混用：客户端 `seq` 用于同一客户端会话内的请求排序展示，服务端推进/去重判据是 `model.LookupSignal(phase).Seq` 与 `Task.LastSeq` 的比较 |
| `digest` | 否，仅 `hook-report` 使用 | 见「约定 5」 |

**Body：** `Content-Type: text/plain`，最近 4 KB 日志尾部，可为空。

**处理流程：**

1. 校验 token（见上）。
2. `model.LookupSignal(SignalKind(phase))`：查不到 → 见错误行为。
3. 查到后按 `entry.Seq` 与 `Task.LastSeq` 比较：`entry.Seq <= Task.LastSeq` 时只记
   `TaskEvent`、不改状态（去重，SPEC §3.6）；否则推进 `Task.State`/`Task.CurrentPhase`
   为 `entry.State`/`entry.Phase`，更新 `LastSeq = entry.Seq`。
   `hook-report` 的 `entry.Seq == 0`，永不推进水位，只记事件（见 model 包作者说明第 3 点）。
4. 无论是否推进状态，都追加一条 `TaskEvent`（`Source = EventSourceSignal`，
   `RC` 取自 `rc` 参数，`Message` 取自 body，`HookDigest` 取自校验通过的 `digest` 参数）。
   更新 `Task.LastSignalAt = now`。

**成功响应：** `200`，空 body。

**错误行为：**

| 情形 | 响应 |
|---|---|
| token 不存在/过期/task 不匹配/finalize token 发范围外信号 | `403` |
| `{phase}` 不是合法 `SignalKind`（`LookupSignal` 返回 `ok=false`） | `400` |
| `rc` 或 `seq` 缺失或非整数 | `400` |
| task 已处于终态（`TaskState.IsTerminal()` 为真） | `200`，仅记事件不做任何状态变更（终态之后的信号视为迟到噪声，不是错误——机器侧不知道任务已被人工 cancel） |

---

## 5. `POST /v1/facts/{task}/{token}`

**认证：** 同信号端点。

**Body：** `Content-Type: text/plain`，`key=value` 逐行纯文本（SPEC §5.5 示例）。
每行一个 KV，`=` 后面到行尾都算 value（value 本身可含 `=`）。

**成功响应：** `200`，空 body。写入的 KV 覆盖式合并进该任务的 facts 存储
（同 key 后写覆盖先写）。

**错误行为：**

| 情形 | 响应 |
|---|---|
| token 校验失败 | `403` |
| body 中出现不含 `=` 的行 | 忽略该行，不拒绝整个请求（与「约定 5」digest 畸形不中断装机同一精神：一行格式错不该让整批 fact 都写不进去） |

---

## 6. `GET /v1/facts/{task}/{token}`

**认证：** 同信号端点。

**成功响应：** `200`，`Content-Type: text/plain`，该任务当前全部 fact 的 `key=value\n`
逐行纯文本（包含服务端无条件注入的 24 个 `LOBER_` 键与 hook 通过写端点存入的键）。

**错误行为：** 同信号端点的 token 校验失败情形，`403`。

---

## UDP `:514`（syslog 接收）

**认证：** 无——UDP 无法承载认证，且 SPEC §6.5 明确这是装机 VLAN 内的辅助通道，
不经 nginx。

**行为：** Go 进程直接监听（不经 nginx 反代），按 RFC 3164/5424 尽力解析，解析失败
的行仍整行落 `TaskEvent`（`Source = EventSourceSyslog`）与本地日志文件，不因解析
失败丢弹数据包。**这是三条辅助通道之一，接收失败或识别不出任务归属时静默丢弹，
绝不能反向影响装机流程或阻塞信号处理**（SPEC §6.5：当 installer 炸在依赖解析、
磁盘写坏时，信号完全静默，syslog 是唯一还在说话的通道，因此它必须比信号通道更
"皮实"，不能有拒绝逻辑）。

---

## 约定（五条，逐条冻结）

### 约定 1：`/v1/boot` 始终返回 200

不存在返回 4xx/5xx 的分支，包括：查询参数缺失、格式错误、查不到主机、双因子不符、
§4.6 三项校验不过、`AttemptCount` 超限。iPXE 对非 200 响应的处理是跳到 `|| shell`，
把机器停在交互式提示符上等人；本系统要的行为始终是「让机器回本地盘或继续 PXE
轮询」，这只能通过 200 + `sanboot` 达成（SPEC §4.2、§10 失败模式 1）。

### 约定 2：`no_task` 载荷逐字符如下

以下三行原文冻结，M5 实现与测试断言必须逐字符匹配（不含行首/行尾多余空白，
末行换行符可有可无）：

```
#!ipxe
echo lober: no task for ${serial}
sanboot --no-describe --drive 0x80
```

**使用场景：** 查不到 pending 任务、SN/MAC 双因子不符、§4.6 三项一致性校验任一不过、
`AttemptCount` 超限——这四种情形返回同一份载荷，不区分文案，机器侧不需要知道
具体原因（原因写进服务端的 `Task.FailureReason` 与 `TaskEvent`，供人面查看）。

**不做的事：** 这四种情形本身不建任务、不改与本次请求无关的其它任务的状态、
「查不到主机/任务」这一支不写 `TaskEvent`、不告警（SPEC §0.1、§4.2）——
唯一留痕是 nginx access log。而「有任务但校验/次数不过」这一支**要**写
`TaskEvent`（这是范围内失败，SPEC §4.6：机器已经取到 `/v1/boot` 并带来了真实
上报值，系统手上有任务、有实际值、能给出确切原因，必须记录）。

### 约定 3：token 走路径段，不用 query string

适用于 `/v1/answer`、`/v1/signal`、`/v1/facts` 三个端点。

**理由（SPEC §4.4）：** Ubuntu 的 `ds=nocloud-net;s=<url>` 要求 URL 是目录且结尾带
`/`，query string 会破坏这个形态；Debian 的 `preseed/url=` 里带 `&` 在内核命令行
上拼接时容易被截断或转义错误。两个发行版的引导参数格式都要求 URL 干净，因此
token 固定为路径段：`/v1/answer/{task}/{token}/{file}`、
`/v1/signal/{task}/{token}/{phase}`、`/v1/facts/{task}/{token}`。

### 约定 4：应答文件名按 OS 家族固定，handler 不自己拼

文件名由 `model.AnswerFileName(family)` 统一决定（`internal/core/model/render.go`），
`RenderContext.AnswerFileName()` 是任务级的便捷调用。六个家族对应表（与
`answerFileNames` 逐一对应，不另起名字）：

| OS 家族 | 文件名 |
|---|---|
| `rhel` | `ks.cfg` |
| `ubuntu` | `user-data` |
| `debian` | `preseed.cfg` |
| `suse` | `autoinst.xml`（**不是** `ks.cfg`——AutoYaST 与 Kickstart 是完全不同的语法） |
| `windows` | `unattend.xml` |
| `esxi` | `boot.cfg` |

`/v1/answer/{task}/{token}/{file}` 的 `{file}` 段必须与
`RenderContext.AnswerFileName()` 的返回值逐字符相等，handler 层不做「模糊匹配」
或「按扩展名猜测」——不一致直接 `404`（见端点 3 的错误行为表）。

### 约定 5：`hook-report` 信号的 `digest` query 参数

**背景（SPEC 自身缺口）：** SPEC §3.5 要求失败事件带 hook digest 以便诊断，
但 §6.3 的 `run_hook` 参考实现的 `signal "hook-report" "$rc" /tmp/h.log` 调用
没有把 digest 传给 `signal()` 函数。这是 SPEC 文档内部的缺口，补法是给
`hook-report` 的信号端点加一个 `digest` query 参数，客户端封装
（`run_hook` 函数）在调用 `signal` 时把自己已知的 `$digest` 变量带上。

**格式：** 64 位十六进制小写字符串（与 `model.HookDigest` 的输出格式一致）。

**校验行为：** 参数存在但格式不合格（长度不是 64、含非十六进制字符、含大写字母）时，
**忽略该参数，视为未提供**，不拒绝整个信号请求——`hook-report` 信号本身照常处理
（记事件、按 SPEC §6.1 语义不推进水位）。`TaskEvent.HookDigest` 在此情形下留空。

**理由：** 上报失败绝不能中断装机（SPEC §6.3 的核心原则），一个畸形的 digest
参数不该让机器装不完；诊断信息缺失好过装机中止。

---

## 认证矩阵小结

| 端点 | 认证载体 | 校验项 |
|---|---|---|
| `GET /boot.ipxe` | 无 | — |
| `GET /v1/boot` | 无 | SN → MAC → UUID 回退查找 + SN/MAC 双因子 + §4.6 三项 |
| `GET /v1/answer/...` | 路径段 token | 存在、`TokenPurposeInstall`、未过期、task 匹配、`AnswerFileServable` |
| `POST /v1/signal/...` | 路径段 token | 存在、未过期、task 匹配；finalize token 额外校验 `FinalizeCanSend` |
| `POST /v1/facts/...` | 路径段 token | 同信号端点 |
| `GET /v1/facts/...` | 路径段 token | 同信号端点 |
| `UDP :514` | 无 | — |

`clientIP` 判定（`X-Real-IP` 仅在直连 peer 为 loopback 时可信，SPEC §1.6、§9.2）
是 nginx 反代层的通用规则，适用于以上全部 Go 侧 HTTP 端点，但**不作为任何端点的
认证依据**——本文档所有认证判定只依赖 token 或 SN/MAC，不依赖客户端 IP
（SPEC §5.7：`booted` 之后信号源 IP 会变，服务端不得用源 IP 做校验）。
