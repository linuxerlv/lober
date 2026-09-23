# Ubuntu 端到端验证（scripts/e2e-ubuntu.sh）

> 验证环境：Windows 11 Pro 10.0.26200 主机，Git Bash（MSYS）+ Go 1.26.4
> （go1.26.4 windows/amd64），2026-09-23。
>
> 验证对象：`internal/render/autoinstall.go`（Ubuntu autoinstall/cloud-init
> 渲染，21 条单元测试 + 2 份 golden file）与
> `internal/imports/extract_ubuntu.go`（Ubuntu 抽取逻辑，本轮 t03 加固）
> 串起来、真的经过网关/控制台/状态机走一遍完整链路——这条链路此前从未
> 端到端跑过。
>
> **本文档不重复展开渲染器/抽取器自身的单元测试覆盖**，只记录本轮新写的
> `scripts/e2e-ubuntu.sh` 脚本的验证方法、真实运行结果与发现。

---

## 0. 结论摘要

**九个阶段（含四条安全负面用例）全部跑完，脚本以退出码 0 结束。**
连续三次独立运行结果完全一致（19 条 `[PASS]`、0 条 `[FAIL]`）——不是"跑了
一次凑巧过"。

**发现一处真实的 Go 侧 bug**（详见第 3 节）：`GET
/v1/answer/{task}/{token}/meta-data` 对 Ubuntu 家族返回 404，而 SPEC §4.5
要求 cloud-init 能依次取到 `user-data` 与 `meta-data` 两个文件。渲染层
（`internal/render`）对 `meta-data` 的支持是完整且有测试覆盖的，但网关层
（`internal/gateway/answer.go`）的文件名校验没有对齐这个例外，导致
render 包那一侧的 `meta-data` 分支是死代码——单元测试测得到，真实 HTTP
请求永远走不到。**本脚本未修复此 bug**（超出本轮任务的文件归属），已记入
下方 findings，交由拥有 `internal/gateway/answer.go` 的轨处理。

脚本本身遇到并修正了两个自身逻辑问题（造假制品树的目录形状不对、meta-data
硬失败会掩盖后续阶段），详见第 4 节——记录在案供以后维护本脚本的人参考，
不是本次验证成果的一部分。

---

## 1. 验证方法

### 1.1 结构：照抄 `scripts/e2e.sh`（RHEL 基线），按 Ubuntu 家族调整

`scripts/e2e.sh` 是 RHEL 家族的端到端验证脚本，九阶段全过，是 MVP 的验收
基线——**本轮只读它、未做任何修改**。`scripts/e2e-ubuntu.sh` 复制它的九
阶段骨架（临时目录/端口/trap 清理、json_field 提取、poll_until 轮询、
pass/fail 打点），改动集中在与家族强相关的部分：

- 造假制品树用新写的 `scripts/e2e/mkfixture-ubuntu.sh`（不是复用
  `mkfixture.sh`，两个家族的目录结构完全不同）。
- 端口段与 RHEL 脚本不同（28180/28190/28614 vs. 28080/28090/28514），
  允许两个脚本理论上同机先后跑而不冲突。
- 阶段 7/8 增加了 Ubuntu 特有的两处坑（见 1.2、1.3）。

### 1.2 Ubuntu 特有点 1：`ds=nocloud-net;s=<url>` 结尾斜杠的端到端验证

不满足于"读代码确认 `model.joinURL` 保留了结尾斜杠"，脚本在阶段 7a 真的从
`/v1/boot` 返回的引导菜单里用 `grep -o` 抠出
`ds=nocloud-net;s=<url>` 的 `<url>` 部分，再用 `case ... in */) ;; *) fail`
断言它必须以 `/` 结尾——这是对**真实网络响应文本**的断言，不是对渲染函数
返回值的单元测试断言（那部分已经在 `internal/render` 的单元测试里覆盖）。

实测结果：

```
[PASS] 阶段7a: autoinstall 数据源 URL 是目录形式且结尾带斜杠（http://127.0.0.1:28180/v1/answer/2654703f-677e-48f7-b23d-8218180a2c70/eZd40o-0vEFbzaB6aOjxlnHtU077nzypOiGQdBbRFbM/）
```

结尾斜杠确实保留。

### 1.3 Ubuntu 特有点 2：`user-data` 与 `meta-data` 分别取

任务描述明确要求"分别取这两个文件并断言都成功——只测 user-data 会漏掉
cloud-init 实际会取两次这个事实"。脚本阶段 8 对两个 URL
（`.../user-data` 与 `.../meta-data`）各自单独发起 `curl` 请求并分别断言。

实测结果：`user-data` 取得成功（HTTP 200，含渲染器骨架三件套）；
`meta-data` 取得失败（HTTP 404）——这正是本轮发现的 bug（第 3 节展开）。

### 1.4 YAML 合法性校验：不满足于"看起来缩进对"

Ubuntu 的 `user-data` 是 YAML，缩进敏感，注入的 shell 片段若缩进错了
cloud-init 直接解析失败。脚本在阶段 8b 用 `go run` 临时跑一个几十行的
Go 程序，调用 `gopkg.in/yaml.v3`（已在 `go.mod` 里，未新增依赖）真实解析
一次从网关取到的 `user-data` 原文，并断言 `autoinstall` 顶层下
`early-commands`/`late-commands`/`identity`/`ssh` 四个必需大键均存在。

实测结果：

```
yaml OK: autoinstall 顶层结构与四个必需大键均已确认存在
[PASS] 阶段8b: 真实取到的 user-data 已用 yaml.v3 解析校验通过（不是"看起来缩进对"，是真的解析成功），early-commands/late-commands/identity/ssh 四个必需大键均存在
```

### 1.5 四条安全负面用例（§9.1 三道闸门 + §10 失败模式 1/2b）

与 RHEL 脚本要求的四条一条不少，全部在 Ubuntu 家族上重新触发并断言：

| 用例 | 断言内容 | 结果 |
|---|---|---|
| 陌生机器请求 `/v1/boot` | 响应体逐字符含三行 sanboot 脚本 | PASS（阶段6a） |
| SN 对但 MAC 不对 | 拒签 token，任务置 failed，`failureReason=precheck` | PASS（阶段6b） |
| 架构不符 | 硬拒绝，不消耗 attempt_count（用 retry 验证仍能拿回 pending） | PASS（阶段6c/6d） |
| 装完的机器再次引导 | 拿到 sanboot 而不是装机菜单 | PASS（阶段9b） |

### 1.6 hook 阶段映射验证（§5.2）

阶段 8 断言取到的 `user-data` 里同时含 `early-commands:` 段（pre 阶段
hook 映射目标）与 `late-commands:` 段（post-nochroot/post 阶段共用，靠
`curtin in-target` 前缀区分能力边界）——本次 fixture 没有配置任何
hook，所以这两段里只有信号打点脚手架本身，验证的是"这两个 YAML 键确实
存在"这一结构性事实，具体 hook 调用行的拼接正确性已经由
`internal/render` 的单元测试与两份 golden file
（`autoinstall_minimal.golden`/`autoinstall_full.golden`）覆盖，本脚本不
重复验证那一层。

---

## 2. 完整运行记录

```
=== Lober Ubuntu 端到端验证 ===
[PASS] 阶段1: Ubuntu 制品树、二进制、admin 用户、配置文件均已就位
[PASS] 阶段2: 服务已启动并就绪（gw=28180, console=28190, syslog=28614）
[PASS] 阶段3: 登录成功，取得 access token
[PASS] 阶段4: 主机=E2E-UBUNTU-SN-0001、制品集=710e8ba8-98d7-4ce7-b504-a31c392c1571（ready，Ubuntu 24.04）、模板=58e43f3e-2263-4d55-9e60-10a32edd1beb（active）均已就位
[PASS] 阶段5: preview 端点用假数据渲染成功（不建任务、不签 token、不落库）
[PASS] 阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot
[PASS] 阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck）
[PASS] 阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck，原因可读）
[PASS] 阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending
[PASS] 阶段6: 四条准入控制负面用例全部通过
[PASS] 阶段7a: autoinstall 数据源 URL 是目录形式且结尾带斜杠
[PASS] 阶段7b: /v1/boot 命中签发 token，任务进入 booting，从菜单里取得 install token
[KNOWN-ISSUE] 阶段8: GET /v1/answer/{task}/{token}/meta-data 返回 HTTP 404（want 200）
[部分通过] 阶段8a: user-data 取得成功并含渲染器骨架三件套；meta-data 未能取得
[PASS] 阶段8b: 真实取到的 user-data 已用 yaml.v3 解析校验通过
[PASS] 阶段8c: 装机期七个信号打点成功；延迟到达/重复 seq 均幂等；finalize token 越权发装机期信号被拒（403）
[PASS] 阶段8d: 重启后四个信号（booted/init-start/init-done/ready）打点成功
[PASS] 阶段9a: 任务达到终态 done
[PASS] 阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装）
[PASS] 阶段9c: 终态任务的 user-data 请求不再提供（403）——meta-data 因阶段8已记录的已知问题未纳入本项断言
[PASS] 阶段9d: 事件表留下完整时间线（23 条事件）

各阶段验证结果：
  state=done current_phase=done attempt_count=1 last_seq=110

=== 全部九个阶段（含四条负面用例）已跑完，但发现 1 处已知问题 ===
```

（完整逐字输出更长，此处摘录关键行；三次独立运行均为此结果，退出码 0，
`[PASS]` 计数均为 19，`[FAIL]` 计数均为 0。）

---

## 3. 发现的 bug：Ubuntu `meta-data` 端点在网关层被 404

### 3.1 现象

```
GET /v1/answer/{task}/{token}/meta-data → HTTP 404
```

而同一个任务的 `GET /v1/answer/{task}/{token}/user-data` 返回 200 且内容
正确。

### 3.2 根因（已读代码确认，不是猜测）

两处相关代码：

**`internal/render/renderer.go`**（`validateAnswerFileName` 与
`RenderAnswerFile`）对 `fileName == "meta-data"` 有专门分支，额外放行并
调用 `RenderMetaData`：

```go
// validateAnswerFileName
if ctx.Artifact.OSFamily == model.OSFamilyUbuntu && fileName == "meta-data" {
    return nil
}
```

```go
// RenderAnswerFile 的 Ubuntu 分支
case model.OSFamilyUbuntu:
    if fileName == "meta-data" {
        return RenderMetaData(ctx)
    }
    return AssembleAutoinstall(ctx)
```

**`internal/gateway/answer.go`** 的 `handleAnswer` 在调用
`RenderAnswerFile` **之前**，自己先做了一次独立的文件名校验：

```go
wantFile, ok := renderCtx.AnswerFileName()
if !ok || file != wantFile {
    w.WriteHeader(http.StatusNotFound)
    return
}
```

`renderCtx.AnswerFileName()` 对 Ubuntu 家族固定返回 `"user-data"`
（`model.answerFileNames` 表）。这一层校验**只知道
`AnswerFileName()` 返回的唯一值**，不知道 render 包内部还额外接受
`meta-data` 这个第二候选——`meta-data` 请求会在到达 `RenderAnswerFile`
之前就被这一层拦截成 404。

### 3.3 为什么现有测试没有拦住这个问题

`internal/render` 包对 `meta-data` 的支持有直接的单元测试覆盖
（`injection_test.go` 的 `TestRenderer_UbuntuAcceptsMetaData`）——但那个
测试直接构造 `RenderContext` 调用 `(*Renderer).RenderAnswerFile(ctx,
"meta-data")`，完全绕过了 `internal/gateway/answer.go` 这一层。
`internal/gateway` 包自己的 `answer_test.go` 里搜索
不到任何 `meta-data` 相关的测试用例。两个包各自的单元测试都是绿的，
但串起来的真实 HTTP 请求路径是断的——这正是本轮任务存在的理由：
"渲染层就绪但从没端到端跑过"。

### 3.4 影响

真实 Ubuntu autoinstall 场景下，cloud-init 的 NoCloud 数据源
（`ds=nocloud-net`）会依次请求该目录下的 `user-data` 与 `meta-data`
两个文件。`meta-data` 请求会收到 404，cloud-init 对此的行为取决于具体
版本实现（可能报错中止，也可能容忍缺失继续但缺少
`instance-id`/`local-hostname`），两种情况都不是预期行为。这与任务描述
里"两个坑"的第二条完全对应——本次验证把它从"理应验证"变成了"已确认存在
的真实故障"。

### 3.5 建议的正解方向（供 t08-dispatch 或拥有 `internal/gateway/answer.go`
的轨参考，不在本次改动范围内）

`internal/gateway/answer.go` 的文件名校验需要知道"该家族除了主文件名，
是否还有第二候选文件名"，而不能只依赖 `renderCtx.AnswerFileName()` 的
单一返回值。`internal/render/esxi.go` 文件末尾已经有一段几乎完全同构的
契约缺口说明（ESXi 的 `boot.cfg`/`ks.cfg` 双文件问题），并给出了正解方向
第 1 点："`renderer.go` 的 `validateAnswerFileName` 对 `OSFamilyESXi`
额外接受一个第二文件名"——这意味着**Ubuntu 的 `meta-data`
应该已经是这个模式的先例**，但先例本身在网关层没有被正确接上。正解
大致是：`internal/gateway/answer.go` 的文件名校验需要改成"file ==
wantFile，或者 file 是该家族的已知第二候选文件名"，第二候选文件名的
判断逻辑应该复用（或对齐）`render` 包内部已有的
`validateAnswerFileName` 逻辑，而不是网关层自己重新维护一份不完整的
判断。这处改动会触及 `internal/gateway/answer.go`，不在本轮
`scripts/e2e-ubuntu.sh` 任务的文件归属内，未做修改。

---

## 4. 脚本自身的两处调试记录（供以后维护 `e2e-ubuntu.sh` 的人参考）

### 4.1 造假制品树的目录形状：ISO 形状 vs. 抽取产物形状

第一版 `mkfixture-ubuntu.sh` 按 RHEL 脚本 `mkfixture.sh` 的思路，造了一棵
"ISO 内容形状"的树（`casper/vmlinuz`、`casper/initrd`、
`casper/filesystem.squashfs`）。这对 RHEL 是对的，因为
`ExtractRHEL` 基本原样保留 ISO 目录树。但 Ubuntu 的
`ExtractUbuntu`（`internal/imports/extract_ubuntu.go`）会把内核与
initrd **搬到目标目录根**（不带 `casper/` 前缀），只有 squashfs 保留
`casper/` 前缀——两种形状不同。

由于本脚本走的是"登记→sqlite3 直接写 ready"的绕法（第 5 节展开），从未
真正调用 `ExtractUbuntu`，因此第一版脚本造的 ISO 形状树与手写的
`paths_json`（`Kernel=vmlinuz`、`Initrd=[initrd]`，没有 `casper/`
前缀）不匹配——`internal/statemachine/precheck.go` 的
`checkArtifactFiles` 在真实文件系统上 stat 这些相对路径时全部落空，
导致 `/v1/boot` 命中时 `StartBoot` 内部的 precheck 失败，网关把这类
错误统一收敛成 `no_task`（与"完全陌生的机器"返回完全相同的载荷，符合
`handleBootGrant` 的错误处理策略：**不区分"查不到"与"precheck 失败"这两类
错误对机器的可见响应**），现场表现是一台身份完全匹配、刚建好的任务在
`/v1/boot` 上直接收到"no task"，与"陌生机器"的响应逐字节相同，一度让人
怀疑是任务系统本身的 bug。用一段独立的调试脚本手动逐步复现（先建
一个全新任务，跳过 retry 路径直接打 `/v1/boot`）后确认问题出在
`mkfixture-ubuntu.sh` 造的目录形状与 `paths_json` 不匹配，而不是
`FindPendingTaskByIdentity`/`StartBoot` 本身的问题。

修复：改造 `mkfixture-ubuntu.sh` 直接产出"抽取产物形状"（`vmlinuz`、
`initrd` 在根，`casper/filesystem.squashfs` 保留前缀），与手写的
`paths_json` 逐字段对齐。文件头注释里说明了这个选择：若未来
`RunUbuntu`（或等价调用点）真正接上、脚本改为走真实抽取路径，这份
fixture 需要换回 ISO 形状。

### 4.2 meta-data 硬失败曾一度掩盖阶段 9 的验证机会

第一版脚本对 `meta-data` 取失败用 `fail()` 硬中止整个脚本——这在发现
第 3 节的 bug 之后是不合适的处理方式：这是一个已确认的 Go 侧 bug，不是
"本脚本前提条件不满足"，硬中止会让阶段 8 剩余的信号打点与阶段 9 的
终态/安全负面用例全部失去验证机会，而它们并不依赖 `meta-data`
是否可取。改为用 `record_known_issue()` 记录后继续跑完剩余阶段，阶段
9c 对 `meta-data` 终态后是否 403 的断言也相应改为条件跳过（一个从未
成功过 200 的端点，判断它终态后是否变成 403 没有额外验证价值）。

---

## 5. 已知限制（脚本本身的绕法，不是本轮新发现的契约缺口）

**制品集登记→导入 job 之间仍然没有接线，本脚本绕过 HTTP 直接 seed
数据库**——任务描述提到"本轮 t01 已经把导入管线接上了，优先走真实路径"，
但实测确认这个说法对本 worktree 当前代码不成立，**本脚本按代码实际行为
执行，不是按任务描述的预期执行**：

- `internal/console/artifact.go` 的 `handleCreateArtifactSet`
  仍然只落一条 `state=importing` 的记录就返回。
- 全仓搜索 `imports.NewJob(` 与 `imports.Job{` 的调用点：唯一命中的是
  `internal/imports` 包自己的 `job_test.go`——没有任何控制台/网关/cmd
  层代码构造过一个 `imports.Job` 实例。
- `RunRHEL` 是 `imports.Job` 目前唯一的家族专属抽取入口，没有
  `RunUbuntu`；即使真的接上了调用点，也无法不改 `internal/imports`
  的代码就让 Ubuntu 走真实抽取路径。

结论：真实路径走不通不是本脚本"没试"，是这条路径客观上还不存在。绕法
与 `scripts/e2e.sh`（RHEL）完全相同——先用真实端点落一条 `importing`
记录取得真实分配的 ID，再用 `sqlite3` CLI 直接把
`state`/`paths_json` 推进到 `ready`，文件本身仍然是
`mkfixture-ubuntu.sh` 造出的真实文件、真实落在制品根目录下的真实 ID
子目录。

syslog 通道与 jq 依赖两点，理由与 `scripts/e2e.sh` 完全相同，不重复
展开（详见该脚本顶部注释第 1、3 条）。

---

## 6. 交付物

| 文件 | 说明 |
|---|---|
| `scripts/e2e-ubuntu.sh` | 新建，Ubuntu 家族端到端验证脚本 |
| `scripts/e2e/mkfixture-ubuntu.sh` | 新建，Ubuntu 假制品树构造脚本 |
| `docs/real-machine/13-e2e-ubuntu.md` | 新建（本文件） |

**未修改** `scripts/e2e.sh`、`scripts/e2e-debian.sh`、任何 `.go` 文件——
均在本轮任务的文件归属之外。
