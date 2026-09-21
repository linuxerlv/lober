# M3 状态机与超时扫描 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** M1 契约与地基必须已完成。本模块与 M2、M4–M8 并行，只通过
> `internal/core/contract` 通信。测试用 M1 的内存 Store 与可控时钟，**不等 M2**。
>
> **本计划描述做什么与为什么，不给代码。**

**目标：** 实现 `contract.StateMachine`、`TokenIssuer` 与 `Prechecker`，
成为 `task.state` 的唯一所有者。

**架构：** 一个类型持有六个依赖（Store、时钟、token 签发器、precheck 器、日志存储、
事件总线），全部通过构造注入。所有状态转换走 CAS，冲突时有限次重读重试。
时间全部来自注入的时钟 —— 60 分钟的 packages 预算没法真等。

**这个模块决定哪台机器会被擦盘。** §3.4 的「禁止重装」判定落在这里，不是落在 UI 上。
每个涉及状态转换的方法都要想清楚：如果它在错误的时机被调用，最坏后果是什么。

**技术栈：** 标准库 `crypto/rand`、`encoding/base64`、`io/fs`、`testing/fstest`

**SPEC 依据：** §3 全节、§4.2 引导契约、§4.4 token、§4.6 一致性校验、§6.1 信号点、§6.4 token 跨重启

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/statemachine/machine.go` | Machine 类型、构造、CAS 重试骨架、编译期接口断言 |
| `internal/statemachine/token.go` | TokenIssuer 实现 |
| `internal/statemachine/precheck.go` | Prechecker 实现：§4.6 三项校验 |
| `internal/statemachine/boot.go` | 开始引导、标记应答文件已取 |
| `internal/statemachine/signal.go` | 应用信号、签发 finalize token |
| `internal/statemachine/lifecycle.go` | 置失败、取消、重试 |
| `internal/statemachine/scan.go` | 超时扫描 |
| `internal/statemachine/*_test.go` | 各自的测试 |

---

### 任务 1：CAS 重试骨架

**产出文件：** `machine.go`

**为什么需要这一步：** 八个方法里有六个要改状态，每个都要处理 CAS 冲突。不先统一，
六处会各写一份重试逻辑，且大概率有一处漏掉。

**决策：CAS 冲突时重读后有限次重试，而不是直接把冲突返回给调用方。**

理由：状态转换的竞争主要来自扫描循环与信号请求撞车 —— 两者都在改同一个任务，
但改的往往是不同的转换（扫描要判超时，信号要推进阶段）。重读一次通常就能成功。
把冲突直接暴露给 HTTP 层只会变成一次无谓的失败：机器侧会重试，但那多花了一个网络往返，
而装机过程中的每次往返都可能撞上网络抖动。

**重试骨架的形态：** 接收任务 id、一个「基于当前任务算出目标转换」的回调、
最大重试次数。每轮重读任务、调回调得到目标状态与更新集、执行 CAS；
撞冲突则重试，重试用尽则返回冲突。

**三条必须处理的情形：**

- **回调可以决定「不需要转换」**（比如幂等方法发现已经是目标状态）—— 此时跳过 CAS
  直接返回成功
- **回调可以拒绝**（比如 retry 发现失败原因不可重装）—— 错误原样返回，不重试
- **重试次数用尽返回 `ErrConflict`** —— 不要无限重试，那会在真有 bug 时挂死请求

**设计要点：**

- **重试上限取 3。** 再多的意义不大：连续三次都撞冲突说明有持续的竞争源，
  那是设计问题不是偶发。
- **每轮必须重读任务。** 用上一轮读到的版本号重试必然再次冲突 —— 这是最容易写错的地方。
- **回调必须是纯函数式的**（输入当前任务，输出目标转换），不要在回调里做 IO ——
  它会被执行多次。

**测试要锁住的行为：**

- 首次成功时回调只被调用一次
- **注入一个「第一次 CAS 前被别人改掉」的场景，验证重试后成功** ——
  内存 Store 可以在回调里偷偷改任务来模拟
- 回调返回「不需要转换」时不产生 CAS
- 回调返回错误时错误原样返回且不重试

**验证：** `go test ./internal/statemachine/ -run TestCASRetry -v` 全过。

**完成标志：** commit `feat(statemachine): CAS retry skeleton shared by all transitions`

---

### 任务 2：token 签发器

**产出文件：** `token.go`、`token_test.go`

**为什么需要这一步：** token 是唯一的执行许可（§0.1、§4.4）。MVP 无 TLS，
所以它与「默认拒绝」是唯一的防线（§1.5、§9.1），不能降级实现。

**实现要点：**

- **32 字节的 `crypto/rand`，base64url 编码。** 不要用 `math/rand`，
  不要用时间戳或任务 id 派生 —— 可预测的 token 等于没有 token。
- **两种用途用同一个生成逻辑**，用途参数当前不影响生成方式。保留这个参数是为了
  将来可能需要区分（比如给 finalize token 加前缀便于日志识别）。
- **base64url 而非标准 base64**：token 走 URL 路径段（§4.4），
  标准 base64 的 `+` 与 `/` 在路径里需要转义。

**测试要锁住的行为：**

- **连续生成一千个无重复** —— 抓的是「误用了非加密随机源」或「忘了每次重新读随机字节」
- 输出只含 URL 安全字符（无 `+` `/` `=`）
- 长度稳定

**验证：** `go test ./internal/statemachine/ -run TestToken -v` 全过。

**完成标志：** commit `feat(statemachine): cryptographically random URL-safe token issuer`

---

### 任务 3：Prechecker —— §4.6 的三项硬拒绝

**产出文件：** `precheck.go`、`precheck_test.go`

**为什么需要这一步：** DHCP 按固件分流、网关按任务分流，两层各自正确不等于组合正确 ——
**建任务时选的 OS 与眼前机器的实际固件可能不匹配，而 DHCP 对此毫无判断能力**（它看不到任务）。
校验只能在这里做（§4.6）。

**三项校验，按这个顺序：**

1. **架构一致性**：机器上报的 `${buildarch}` 与制品集的架构必须相符。
   不符则拒绝 —— x86_64 制品发给 arm64 机器，内核直接加载失败
2. **固件模式兼容**：用 M1 的固件兼容矩阵判 OS 家族能否在该固件下装，
   再用制品集的固件匹配方法判这套制品能否发给该机器。
   典型拒绝是「这台机器是 BIOS，任务要装 ESXi」
3. **制品完整性**：路径中所有文件在盘上真实存在

**每项失败都要带「是哪一项」与可读的细节**，因为这个原因会写进事件表并显示给运维
（§4.6）。「precheck 失败」是没用的，「这台机器是 BIOS，任务要装 ESXi」才有用。

**实现要点：**

- **文件存在性检查通过 `fs.FS` 注入**，不直接调 `os.Stat`。测试用内存文件系统 ——
  不需要真造文件，也不会因为开发机上恰好有同名文件而假通过。
- **仓库路径是目录，检查要落到它下面的仓库元数据文件。** 空目录能 stat 成功但对
  installer 毫无用处。判定方式：路径以斜杠结尾则视为目录，检查其下的 `repodata/repomd.xml`。
- **机器上报的架构与平台是原始字符串**，要用 M1 的两个解析函数转成枚举。
  解析失败本身就是一种拒绝 —— 未知的 `${buildarch}` 说明是没见过的平台，
  不该猜着往下走。

**测试要锁住的行为：**

- 架构不符被拒绝，且错误里能读出「是架构那一项」
- **ESXi + BIOS 被拒绝**（§0.3 MVP 不覆盖）
- Linux 四家在两种固件下都通过
- 缺一个 initrd 时被拒绝，错误细节里含那个路径
- **仓库目录存在但缺元数据文件时被拒绝** —— 这条抓的是「检查了目录本身」那个 bug
- 无法解析的架构或平台字符串被拒绝

**验证：** `go test ./internal/statemachine/ -run TestPrecheck -v` 全过。

**完成标志：** commit `feat(statemachine): boot consistency prechecker with three hard rejects`

---

### 任务 4：开始引导 —— 顺序是硬约束

**产出文件：** `boot.go`、`boot_test.go`

**为什么需要这一步：** 这是 §0.1 的范围起点。这一个方法里的顺序错了，
后果是「不该装的机器被装了」或「该装的机器三次机会被白白消耗」。

**五步，顺序不能变：**

| 步 | 动作 | 为什么必须在这个位置 |
|---|---|---|
| 1 | 确认任务仍是 `pending` | 见下方的防御性检查 |
| 2 | 跑 precheck | 必须在 attempt++ 之前 —— 不匹配不是可重试的偶发故障 |
| 3 | precheck 不过 → 置 failed，**不消耗 attempt** | 重试必然同样失败，消耗 attempt 只会让运维少三次真正的机会 |
| 4 | attempt++；超上限 → 置 failed | 上限判定要在签 token 之前，否则第四次也拿到了 token |
| 5 | 签 token、CAS 到 `booting` | 状态转换与 token 写入必须在同一次 CAS 里 —— 分两次的话中间崩溃会留下「已 booting 但无 token」的任务，那台机器取不到应答文件也不会失败，只能等超时 |

**第 1 步的防御性检查不能省。** 调用方（M5）会先查 pending 任务再调这个方法，
但两步之间有窗口：并发的第二个请求可能已经把任务推进到 `booting`，或者扫描循环
刚把它判成 failed。**不检查的后果是对已终态任务重复累加 attempt 并重复写失败事件** ——
那个计数会无界增长，而 §9.1 第三道闸门依赖它。

**precheck 失败要写的事件：** 失败原因是 precheck，细节含「哪一项不过」。
**这是范围内的失败**（§4.6）—— 机器已经取到了引导端点并带来了真实的上报值，
系统手上有任务、有实际值、能给出确切原因，所以必须记录并呈现。

**返回值：** 成功时返回任务、token 与过期时间。token 的 TTL 从**现在**算起
（§4.4：计时起点是首次匹配成功时，不是建任务时）。

**设计要点：**

- **precheck 需要制品集，所以这个方法要查库取它。** 取不到制品集本身就是一种失败
  （置 failed，原因 precheck）。
- **attempt 超限的失败原因用专门的那个**（不是 boot_timeout），因为两者的诊断含义
  不同：超限说明机器反复来取菜单但走不到 installer，超时说明取了菜单之后没动静。

**测试要锁住的行为：**

- 五步的顺序：用一个总是失败的 precheck 验证 **attempt 没有增加**
- attempt 达到上限时置 failed 且不签 token
- 成功路径：状态变 booting、token 非空、过期时间是现在加 90 分钟（用可控时钟验证精确值）
- **对已经是 booting 的任务再调一次不会累加 attempt**
- **对终态任务调用返回错误且不写任何事件** —— 这条对应上面说的无界增长 bug

**验证：** `go test ./internal/statemachine/ -run TestBeginBoot -v` 全过。

**完成标志：** commit `feat(statemachine): begin boot with precheck-before-attempt ordering`

---

### 任务 5：标记应答文件已取

**产出文件：** `boot.go`（追加）

**为什么需要这一步：** 「installer 起来了」这个事实由应答文件被取走隐含（§6.1），
不需要机器额外发信号。

**实现要点：幂等。** 已经是 `installing` 或更后的状态时直接返回成功，不报错。

**为什么必须幂等：** Ubuntu 的 cloud-init 会取 `user-data` 与 `meta-data` 两个文件
（§4.5），每次都会触发这个方法。第二次报错的后果是应答文件请求返回 500，
而 cloud-init 拿不到 `meta-data` 就不会继续 —— 装机停住。

**同时要做的：** 把阶段推进到 `pre` 并重置阶段进入时间 —— 这样 `pre` 的 10 分钟预算
从 installer 真正起来时开始算，而不是从签发 token 时。

**测试要锁住的行为：**

- booting → installing 的正常转换，且阶段进入时间被重置
- **重复调用返回成功且不改变状态** —— 对应 cloud-init 取两个文件的场景
- 对 pending 任务调用返回错误（installer 不可能在签 token 之前起来）

**验证：** `go test ./internal/statemachine/ -run TestMarkAnswer -v` 全过。

**完成标志：** commit `feat(statemachine): idempotent answer-fetched transition`

---

### 任务 6：应用信号 —— 幂等的三种情形

**产出文件：** `signal.go`、`signal_test.go`

**为什么需要这一步：** 状态机由信号驱动（§6.1）。机器侧的网络不可靠、会重试、
信号可能乱序到达，所以这个方法必须对三种异常输入都正确。

**三种情形，都是「记事件但不改状态」：**

| 情形 | 判据 | 为什么不能改状态 |
|---|---|---|
| 序号重复 | Store 返回重复序号错误 | 机器侧重试三次（§6.3），重复投递是常态 |
| 阶段后退 | 信号的表序号不大于任务的水位 | 延迟到达的早期信号会把已推进的任务打回去（§3.6） |
| 任务已终态 | 任务状态是 done 或 failed | 终态任务不该被信号复活 |

**实现流程：**

1. 查表得到信号的序号与目标状态阶段。**表里没有的信号名直接返回错误** ——
   它来自 URL 路径段
2. 读任务；终态则只写事件，返回「未接受」
3. 写事件（带序号）；撞重复序号则返回「未接受」，**不是错误**
4. 比较信号序号与任务水位；不大于则返回「未接受」
5. 走 CAS：更新状态、阶段、水位、上次信号时间
6. 状态真的变了则发布生命周期事件

**关键细节：**

- **`hook-report` 的表序号是 0，永不推进水位。** 它与阶段正交 ——
  一个阶段内有几个 hook 就来几次。实现上它走第 3 步写事件后就返回「已接受但未改状态」。
- **`ready` 信号推进到 done，要发布「任务就绪」生命周期事件**（§7.5：那时才写 DNS、
  注册 CMDB）。
- **日志正文交给 LogStore 落文件，事件里只存路径与尾部**（§2.5）。
  尾部取最后 4 KB（§3.5）。
- **上次信号时间必须更新，即使信号未被接受。** §3.5 的「距今多久」是失败诊断的
  关键区分度 —— 刚收到信号说明机器活着、是脚本或 installer 逻辑错误；很久没信号
  说明机器层面出事。这两个排查方向完全不同。

**返回值要分别表达「已接受」与「改变了状态」** —— handler 据此决定回什么。

**测试要锁住的行为：**

- 正常序列推进状态与阶段
- **重复序号返回「未接受」而不是错误**
- **延迟到达的早期信号不改变阶段** —— 用「先 post-done 再 packages-start」验证
- 终态任务收到信号只记事件
- **`hook-report` 来三次都被接受，但水位不变**
- 未知信号名被拒绝
- `ready` 之后任务是 done，且生命周期事件被发布
- **上次信号时间在「未接受」的情形下也被更新**

**验证：** `go test ./internal/statemachine/ -run TestApplySignal -v` 全过。

**完成标志：** commit `feat(statemachine): idempotent signal application with monotonic phases`

---

### 任务 7：finalize token

**产出文件：** `signal.go`（追加）

**为什么需要这一步：** 装机 token TTL 90 分钟从引导算起。若装机用了 70 分钟、
重启又停在 BIOS 数分钟，`booted` 发出时装机 token 可能刚好过期（§6.4）。

**实现要点：**

- 在渲染 `%post` 之前被调用（M5 构造渲染上下文时），所以任务应处于 `installing`
- TTL 24 小时，**有独立的过期时间字段**，不复用装机 token 的
- **幂等：已有未过期的 finalize token 时返回同一个**。不幂等的后果是重复渲染
  （比如 Ubuntu 取两个文件）会换掉 token，而先前写进目标系统的那个就失效了

**权限限制在别处实现**（M5 按 M1 的 finalize 信号集合判定），本方法只负责签发。

**测试要锁住的行为：**

- 签发后任务上有 token 且过期时间是现在加 24 小时
- **重复调用返回同一个 token**
- 已过期时重复调用换发新的

**验证：** `go test ./internal/statemachine/ -run TestFinalizeToken -v` 全过。

**完成标志：** commit `feat(statemachine): finalize token with independent 24h TTL`

---

### 任务 8：置失败、取消与重试 —— 禁止重装落在这里

**产出文件：** `lifecycle.go`、`lifecycle_test.go`

**为什么需要这一步：** §3.4 是全系统最重要的安全约束。**拒绝必须在状态机里，
不是靠 UI 隐藏按钮** —— UI 可以被绕过（直接调 API），状态机不能。

**置失败的要点：** 写一条带 §3.5 六个字段的事件。

`TaskEvent` 上没有 syslog 尾部与「距今多久」的专属列，所以这两项要格式化进事件正文。
**「距今多久」用注入的时钟算，不用 `time.Now()`** —— 否则这个字段在测试里不可控，
而它恰恰是失败诊断里区分度最高的一项。

**取消的要点：** 置 failed，原因是取消。**`pending` 任务取消后即不再响应引导请求** ——
这是 §3.2 承认的残留风险的唯一缓解手段（建错了立刻取消）。要记审计（操作人）。

**重试的要点，这是本任务的核心：**

1. 读任务，确认是 `failed`
2. **检查失败原因是否可重装** —— 用 M1 的判定方法。不可重装则返回专门的拒绝错误
3. 重置 attempt 计数、清空 token、清空失败原因与细节
4. CAS 回 `pending`、阶段回 `pending`

**三种必须拒绝的失败原因：** 重启超时、初始化超时、初始化失败。
这三种的机器**已经装好且数据可能已在盘上**，重装会擦掉。

**不要扩大拒绝范围。** precheck 失败是允许 retry 的 —— §4.6 说「重试必然同样失败」，
但 §8.2 只显式要求拒绝上面三类。运维改正任务配置后重试是合理路径，
由 UI 提示「需先修正配置」（M8 的职责）。**自行扩大拒绝范围会让运维失去一条正当的
恢复途径，而他们的应对会是删任务重建 —— 那绕过了所有审计。**

**测试要锁住的行为：**

- **三种不可重装原因逐一被拒绝，返回的是专门的拒绝错误**（调用方要能识别它以回
  正确的错误码）
- **六种可重装原因逐一被接受**，特别是 precheck —— 这条防止将来误扩大拒绝范围
- retry 后 attempt 归零、token 清空、状态回 pending
- 取消后 pending 任务不再能被身份查找命中
- 置失败写的事件含六个字段，且「距今多久」来自注入的时钟

**验证：** `go test ./internal/statemachine/ -run 'TestFail|TestCancel|TestRetry' -v` 全过。

**完成标志：** commit `feat(statemachine): fail, cancel and retry with reinstall guard`

---

### 任务 9：超时扫描

**产出文件：** `scan.go`、`scan_test.go`

**为什么需要这一步：** 分阶段超时能在数分钟内报出卡在哪个阶段（§3.3）。
这是「机器卡住了」的唯一发现途径 —— 机器不会主动报告自己挂了。

**实现流程：**

1. 从 Store 取超时候选（Store 已排除无预算阶段）
2. 对每个候选按阶段决定处置
3. 返回**判失败**的任务数

**`booting` 超时的处置需要一个决断，SPEC 两处措辞不同：**

§3.3 的表写「允许重试，attempt++」，§3.4 的表把「booting 超时」列为失败态。

**采用：`attempt_count < 上限` 则回 `pending`（机器可自行再来一次），
已达上限则置 failed（原因是 attempt 超限）。**

理由：两处措辞都能满足 —— 回 pending 就是「允许重试」，达上限后确实成为失败态；
且符合 §9.1 第三道闸门「同一任务窗口内菜单最多取 3 次」的表述。
**回 pending 时不要增加 attempt** —— attempt 是在实际签发 token 时增加的（任务 4），
在这里再加一次会让每次超时消耗两个额度。

**其余阶段的处置：** 按 M1 的超时原因映射置 failed。
**`rebooting` 与 `initializing` 的失败原因必须落在不可重装那一组** ——
M1 已有测试锁住这条映射，本模块只需正确使用它，不要自己判断。

**失败事件要写清可诊断信息：** 哪个阶段、预算是多少、实际过了多久、上次收到信号是何时。
`rebooting` 超时要额外标记「装好但未起来」，`initializing` 超时标记
「已就绪但初始化未完成」—— **告警文案必须与「装机失败」明确区分**（§3.4），
两者处理方式完全不同（前者不能重装）。

**设计要点：**

- **返回值只计真正落到 failed 的任务数**，不计回 pending 的 —— 契约文档写的是
  「本轮判失败的任务数」，而回 pending 不是判失败。调用方（M9 的循环）用它决定
  是否打告警日志。
- **单个任务处置失败不中断整轮。** 一个任务撞 CAS 冲突不该让其余任务本轮不被处理 ——
  记日志继续。
- **不用 in-process timer**（§3.3），本方法只做一轮；周期调度是 M9 的事。

**测试要锁住的行为：**

- **pending 任务永不出现在处置结果里**（一年之后再扫也不动它）
- `booting` 超时且 attempt 未达上限 → 回 pending，且 **attempt 未增加**
- `booting` 超时且 attempt 已达上限 → failed，原因是 attempt 超限
- 三个安装阶段超时 → failed，原因是安装阶段失败
- **`rebooting` 超时 → 失败原因不可重装**，且事件文案含「装好但未起来」的标记
- **`initializing` 超时 → 失败原因不可重装**
- 返回值等于判失败数，不含回 pending 的
- 一个任务处置失败时其余仍被处理

**验证：** `go test ./internal/statemachine/ -run TestScanTimeouts -v` 全过。

**完成标志：** commit `feat(statemachine): per-phase timeout scan with booting retry window`

---

### 任务 10：并发验证与接口断言

**产出文件：** `concurrency_test.go`、`machine.go`（解开断言）

**为什么需要这一步：** 网关与扫描循环并发调用同一个状态机。没有竞态检测的通过不算数。

**三个并发场景：**

1. **多个 goroutine 对同一个 pending 任务同时开始引导 —— 恰好一个拿到 token。**
   不是「最多三个」（那是基于 attempt 上限的错误预期），是精确的一个：
   第一个成功后任务已是 `booting`，其余的防御性检查会拦住
2. **并发应用不同序号的信号 —— 全部被处理，最终水位是最大那个**
3. **扫描循环与信号并发 —— 不出现「已 done 又被判超时失败」**

**接口断言：** 解开编译期断言，确认八个方法全部实现。

**设计要点：**

- **用屏障让 goroutine 尽量同时发起**，否则它们会自然串行，测不出竞态。
- **断言精确计数**，不要写「至少一个成功」—— 那在有 bug 时也成立。

**测试要锁住的行为：** 上面三个场景的精确结果。

**验证：**

- `go test ./internal/statemachine/ -race -v` 全部通过且无竞态报告
- `go build ./...` 断言成立
- `make check` 全绿

**完成标志：** commit `test(statemachine): concurrent boot and signal under race detector`

---

## 交付验收

- [ ] 八个 `StateMachine` 方法全部实现，编译期断言通过
- [ ] `go test ./internal/statemachine/ -race` 无竞态报告
- [ ] 三种不可重装的失败原因在 retry 时被拒绝，六种可重装的被接受
- [ ] 超时扫描永不触碰 pending 任务
- [ ] 时间全部来自注入的时钟，代码中无 `time.Now()` 调用
      （用 `grep -rn 'time.Now()' internal/statemachine/` 确认只在测试夹具里出现）
- [ ] 只 import `model` 与 `contract`（测试可用 `fake`），由 `archtest` 保证

## 两处 SPEC 张力的决断记录

1. **`booting` 超时回 `pending` 还是直接 `failed`：** 采用「attempt 未达上限则回
   pending，达上限则 failed」。两处措辞都能满足，且符合第三道闸门的表述。
2. **`FailPrecheck` 是否允许 retry：** 允许。按 §8.2 的显式要求只拒绝三类超时，
   不自行扩大范围 —— 扩大的后果是运维改用「删任务重建」，那绕过了所有审计。

