# M4 渲染器 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** M1 契约与地基必须已完成。本模块与 M2、M3、M5–M8 并行，只通过
> `internal/core/contract` 通信。
>
> **本计划描述做什么与为什么，不给代码。** kickstart 模板文本与注入的 shell 函数也由你写 ——
> 但它们的每一条行为要求都在本计划里列明了，逐条落实。

**目标：** 实现 `contract.Renderer`，把任务渲染成 iPXE 菜单与 kickstart 应答文件。
MVP 只要求 Rocky 9 走通。

**架构：** 纯函数 —— 输入渲染上下文，输出文本，不查库、不碰时钟、不做 IO。
这是控制台 preview 端点能用假数据跑的前提，也是 golden 测试的前提。

**这个模块生成的文本会以 root 身份在目标机器上执行。** 注入防护是头号职责 ——
表单里填进 `; rm -rf /` 必须被**拒绝**，不是被转义后仍然执行。

**技术栈：** `text/template`（kickstart 与 preseed）、`html/template`（unattend.xml 获得
XML 转义）、golden file 测试

**SPEC 依据：** §4.5 菜单形态、§5 全节、§6.3 客户端封装、§9.5 凭据清理、§1.5 URL 从配置来

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/render/renderer.go` | Renderer 类型、两个入口方法的分派、编译期接口断言 |
| `internal/render/guard/validate.go` | 注入防护：按类型白名单校验表单值 |
| `internal/render/params.go` | 参数内联与 fact 注入 |
| `internal/render/clientlib.go` | 注入到机器上的 shell 函数（signal 与 run_hook） |
| `internal/render/firstboot.go` | firstboot 脚手架与凭据清理 |
| `internal/render/kickstart.go` | kickstart 三段结构组装 |
| `internal/render/menu.go` | iPXE 菜单 |
| `internal/render/testdata/*.golden` | 期望输出 |

---

## 渲染器的三段结构（§5.1）

**系统必需的机器侧动作不是 hook，而是渲染器直接生成的模板文本**，与 `%packages` 段同级。
用户看不见、不可编辑、不可绕过。

```
序言    内联参数 + fact 注入 + syslog 指向 + 注入 signal/run_hook 函数
用户段  pre / post-nochroot / post 三个阶段的用户 hook 调用
尾声    firstboot 脚手架 + 凭据清理
```

**为什么系统动作不做成 hook 数据：** 作为数据存在时，有权限者可以编辑或删除它 ——
会静默废掉整个遥测。另外 hook 表里只有用户 hook 意味着 order 区间全归用户，
UI 上不存在「系统 hook」这个让人困惑的概念。

**物理约束：以下四件事只能是渲染器骨架**，因为服务端 provider 跑在服务器上，
无法在目标机器上执行代码：

| 动作 | 为什么服务端做不到 |
|---|---|
| 信号函数注入 | 那是机器本地文件，服务端只能提供内容，写入动作必须机器执行 |
| firstboot 脚手架 | 重启后目标系统里需有 unit、脚本、token、参数 —— 只有 `%post` 能写进目标盘 |
| 凭据清理 | 擦 finalize token 与安装记录里的密码哈希 |
| syslog 指向 | kickstart 内的一行配置 |

---

### 任务 1：注入防护 —— 按类型白名单校验

**产出文件：** `guard/validate.go`、`guard/validate_test.go`

**为什么需要这一步：** 表单值会进 kickstart 脚本（§5.6），而那个脚本以 root 身份执行。
**这是本模块最高优先级的任务，所以放在第一个** —— 后面所有渲染逻辑都建立在
「值已经校验过」这个前提上。

**三个校验器：**

| 校验器 | 规则 | 为什么这样定 |
|---|---|---|
| IP | 解析为 IP 后收紧到 IPv4 | 拒绝子网掩码写法、IPv6、任何附加字符。装机网是 IPv4 内网，放宽没有收益 |
| 主机名 | RFC 1123 单标签：字母数字与连字符，不以连字符首尾，长度上限 63 | 拒绝下划线、空格、换行、点号。点号要拒是因为 FQDN 由主机名加域名拼出，让用户填点号会产生歧义 |
| 整数范围 | 解析为十进制整数后检查闭区间 | 天然拒绝十六进制、小数、前后空格 |

**核心原则：走拒绝路径，不走转义路径。** 校验失败立即整体返回错误，
不要尝试「清洗」值 —— 清洗逻辑的每个漏洞都是一次 root 执行。

**必须验证被拒绝的恶意输入**（至少这七类）：分号、反引号、`$(...)`、换行、
kickstart 的段落结束标记、段落开始标记、以及看起来合法但含尾部空格的值。

**设计要点：**

- **校验器不返回「清洗后的值」，只返回是否合法。** 返回清洗值会让调用方产生
  「用哪个值」的选择，而选错就是漏洞。
- **`guard` 是独立子包**，因为 M6（控制台）也要用它做服务端强校验（§12.7：
  前端校验只是体验，不是防线）。放子包避免 M6 import 整个渲染器。

**测试要锁住的行为：**

- 三个校验器各自的合法值通过
- **七类恶意输入逐一被拒绝**，每类一个断言（合并成一个循环也行，
  但失败信息要能指出是哪一类）
- 边界值：主机名恰好 63 字符通过、64 字符拒绝；整数恰好在区间端点通过

**验证：** `go test ./internal/render/guard/ -v` 全过。

**完成标志：** commit `feat(render): type-whitelist validators rejecting injection`

---

### 任务 2：参数内联与 fact 注入

**产出文件：** `params.go`、`params_test.go`

**为什么需要这一步：** 参数在渲染时直接内联为 shell 变量赋值（§5.3），
因此无需额外端点、无需机器下载参数文件。

**两件事：**

**一、内联表单参数。** 每个参数按键名选对应的校验器（IP 类的用 IP 校验器、
主机名用主机名校验器、数值用整数校验器）。**键名到校验器的映射是一张显式的表，
未知键一律拒绝** —— 不在表里的键说明有人加了新表单字段却没想过它的合法取值范围，
那正是该拦住的时刻。

**二、注入 fact 命名空间。** 遍历 M1 的全部 fact 键，**无条件注入**，
不管 hook 用不用（§5.4）。

**缺失的键要注入空值而不是跳过。** 理由：hook 里 `$LOBER_RACK` 未定义与为空是两种
不同的调试体验 —— 未定义时 `set -u` 会让脚本直接退出且错误信息指向 hook 自己，
而为空只是条件判断走了另一支。后者好查得多。

**设计要点：**

- **值要用单引号包裹并转义其中的单引号**，即使已经校验过 —— 两道防护的成本接近于零。
- **变量赋值的顺序要稳定**（按键名排序），否则 golden file 每次渲染都不同。

**测试要锁住的行为：**

- 全部 fact 键都出现在输出里，缺值的赋成空
- 未知的表单键被拒绝
- 每类参数走了对应的校验器（用一个非法 IP 验证 IP 类字段被拒）
- 输出顺序稳定（渲染两次结果相同）

**验证：** `go test ./internal/render/ -run TestParams -v` 全过。

**完成标志：** commit `feat(render): parameter inlining and unconditional fact injection`

---

### 任务 3：注入的客户端函数 —— signal 与 run_hook

**产出文件：** `clientlib.go`、`clientlib_test.go`

**为什么需要这一步：** installer ramdisk 里没有 python、没有 jq，curl 也可能只是
busybox 版（§6.2）。所以上报能力要由渲染器注入两个 shell 函数。

**`signal()` 函数的七条行为要求，逐条落实：**

1. 接收阶段名、退出码、日志文件路径三个参数，**另加第四个可选的 hook digest**
2. 自增一个序号变量，拼进 query（`(task, seq)` 是去重键，§3.6）
3. **digest 非空时才拼进 query**，为空时 query 里完全不出现那个参数
4. **重试三次**，每次之间退避（第 i 次等 3i 秒）
5. 用 `curl --data-binary @文件` 发纯文本 body —— 一行即可，服务端解析零成本。
   用 JSON 需在 shell 里手工拼转义，日志带引号或换行就会坏掉
6. 三次都失败则落 syslog 兜底
7. **最后返回成功码**

**第 7 条是刻意的，不能改：遥测失败不能杀装机。** 服务器临时不可达而把正在装的机器
搞挂，是最没必要的失败。上报失败退化为 syslog，装机继续。

**`run_hook()` 函数的六条行为要求：**

1. 按 digest 从制品服务器取脚本（hook 正文是静态制品，按内容寻址，§2.2）
2. **用 `sha256sum -c` 校验下载到的内容** —— digest 目录名就是正文的 sha256（§6.3）
3. 用 `timeout` 包裹执行，超时值来自 hook 定义
4. 用 `tee` 同时留日志与输出
5. **用管道状态数组取真实退出码** —— 直接取 `$?` 拿到的是 `tee` 的退出码，永远是 0
6. 上报 hook-report（带 digest），然后**按 required 决定是否中止**：
   required 失败即中止装机并上报，非 required 记日志继续（§6.3）

**第 5 条是个经典陷阱。** 取错退出码的后果是所有 hook 都被认为成功 ——
required hook 失败也不会中止装机，机器带着半配置的状态装完。

**设计要点：**

- **两个函数的正文作为字符串常量或模板输出**，其中的服务器地址、任务 id、token
  从渲染上下文填入 —— **不要出现字面的 `http://`**（§1.5）。
- **注入位置在序言段**，早于任何用户 hook 的调用。

**测试要锁住的行为：**

不执行 shell（那需要真环境），改为**对生成的文本做断言** —— 逐条验证上面十三条要求
各自对应的文本特征存在：

- `signal` 的重试循环、退避表达式、syslog 兜底行、**末尾的返回成功**
- digest 参数的条件拼接（给 digest 与不给 digest 各渲染一次，对比 query）
- `run_hook` 的校验命令、`timeout` 调用、**管道状态数组的取值**、required 判断
- 两个函数里都不出现字面 `http://`

**验证：** `go test ./internal/render/ -run TestClientLib -v` 全过。

**完成标志：** commit `feat(render): inject signal and run_hook shell helpers`

---

### 任务 4：firstboot 脚手架与凭据清理

**产出文件：** `firstboot.go`、`firstboot_test.go`

**为什么需要这一步：** `%post` 虽已 chroot，但跑的还是 installer 的内核，
目标系统 systemd 未启动（§5.2）。所以需要完整 boot 才能做的事必须推到首次启动后。

**`%post` 阶段要生成的五样东西：**

1. 目标系统里的工作目录
2. **finalize token 文件，权限 0600**
3. 参数文件，权限 0600
4. **用户的 firstboot hook 脚本与参数，落进目标系统**
5. 一个 systemd oneshot unit 与它的执行脚本，并 enable 它

**第 4 条不能省也不能改成「重启后再去取」**（§5.8）：重启后业务网可能已生效，
装机 VLAN 未必还可达。firstboot 对网络的唯一依赖应只剩「把信号发出去」。

**unit 的存在条件用标记文件，不用脚本末尾的 disable。** 理由：脚本中途死掉时标记仍在，
可以看出未跑完，重启会重试，也可以人工重新挂上。用 disable 的话脚本死掉就永远不再跑，
且看不出来。

**反直觉但关键的事实：`systemctl enable` 在 `%post` 中可用**（它本质只是建符号链接），
而 `systemctl start` 不可用（无 running systemd）。这正是 firstboot 机制的基础 ——
`%post` 负责埋点，firstboot 负责执行。

**firstboot 执行脚本的流程：** 发 `booted` → 发 `init-start` → 顺序跑用户 firstboot hook
→ 发 `init-done` → 发 `ready` → 清理凭据 → 删标记文件。

**即使信号发不出，也要把本地工作做完**（§5.8）—— 不能因为报不上状态就放弃配置。

**凭据清理的三条**（§9.5）：

1. 安全擦除参数文件与 finalize token（用 `shred -u` 而非 `rm`）
2. 删标记文件
3. **从安装记录里删掉密码哈希与带 token 的引导参数行**

第 3 条的背景：带 token 的完整 URL 会进目标机的内核命令行、安装器日志与安装记录。
因 token 短时效且装完即废，影响有界，但顺手清理值得做。

**另一条容易忘的：业务 IP 写配置但不激活**（§5.7）。若某个 `%post` hook 把业务网配上
并激活，而业务网在另一 VLAN，机器当场失去到网关的连接 —— 后续信号全部发不出，
**装机成功但系统显示超时失败**。所以骨架里只写网络配置文件，不执行激活命令；
装机 VLAN 的地址保持到重启。

**测试要锁住的行为：**

- 五样东西都出现在 `%post` 输出里，两个凭据文件带 0600 权限
- unit 用标记文件作存在条件
- **用户 firstboot hook 的正文出现在 `%post` 里**（不是一条下载命令）
- firstboot 脚本的信号顺序正确
- 凭据清理的三条都在，且用的是安全擦除
- **输出里不含激活网络的命令**

**验证：** `go test ./internal/render/ -run TestFirstboot -v` 全过。

**完成标志：** commit `feat(render): firstboot scaffold and credential cleanup`

---

### 任务 5：hook 阶段到 kickstart 段落的映射

**产出文件：** `kickstart.go`（第一部分）、对应测试

**为什么需要这一步：** **阶段不是排序，是能力边界**（§5.2）。hook 放错阶段不是顺序错了，
是根本跑不起来。

**四个阶段的能力差异：**

| 阶段 | kickstart 段落 | 环境 | 能做什么 |
|---|---|---|---|
| pre | `%pre` | installer ramdisk，分区之前 | 看硬件、配 RAID、动态生成分区方案 |
| post-nochroot | `%post --nochroot` | 目标盘挂在挂载点，用宿主工具 | 往目标系统塞文件 |
| post | `%post` | chroot 进目标系统 | 装包、写配置、建用户、enable 服务 |
| firstboot | 不是段落，落进脚手架 | 真正启动后的 systemd oneshot | 需完整 boot 才能做的事 |

**以下在 `%post` 中全部失败**，注释里要写明以便后来者不踩：启动或重启服务
（无 running systemd）、网络配置命令与主机名命令（需 dbus）、依赖服务运行的初始化
（数据库初始化、集群加入、agent 注册）、业务网通信。

**实现要点：** 从模板取某阶段的 hook 引用（M1 已按 order 排好序，**不要再排一遍**），
为每个生成一次 `run_hook` 调用，参数是 digest、required 标志与超时值。

**测试要锁住的行为：**

- 三个 installer 阶段的 hook 落进对应段落
- firstboot 阶段的 hook **不出现在任何 kickstart 段落里**（它们进脚手架）
- 同阶段多个 hook 按 order 顺序出现
- 空阶段不产生空段落（`%pre` 里没东西就不要写 `%pre`）

**验证：** `go test ./internal/render/ -run TestStageMapping -v` 全过。

**完成标志：** commit `feat(render): hook stage to kickstart section mapping`

---

### 任务 6：kickstart 组装与 golden file 基础设施

**产出文件：** `kickstart.go`（第二部分）、`golden_test.go`、`testdata/*.golden`

**为什么需要这一步：** 把前五个任务的产出按三段结构拼成完整应答文件。
golden file 让「输出变了」这件事可见 —— 渲染器的输出是要喂给 anaconda 的，
一个字符的意外改动可能让装机失败，而单元断言很难覆盖全文。

**组装顺序：** 基础指令（语言、键盘、时区、引导加载器、磁盘、网络、root 密码策略）→
`logging --host=` 一行（§6.5 的 syslog 指向）→ 包组 → 三个阶段的 hook 段 →
firstboot 脚手架段。

**基础指令里的两条要点：**

- **root 不设可复用密码**：注入 SSH 公钥并关闭密码登录（§1.5 的缓解手段）——
  MVP 无 TLS，应答文件被截获也换不成持久访问
- **磁盘用 by-id 标识**，来自服务端决策（§5.6），**不要在模板里硬编码 `/dev/sda`**

**golden file 的做法：**

- 用一个 `-update` flag 控制「重新生成期望文件」
- 至少三份：无 hook 的最小 kickstart、带三阶段 hook 的完整 kickstart、Rocky 的 iPXE 菜单
- **渲染上下文的构造要用固定的假数据**（固定 token、固定时间、固定 SN），
  否则 golden 每次都变

**更新 golden 时必须人工审阅 diff，不能盲目接受。**
这句话要写进测试文件的注释 —— golden 测试的价值全在于「有人看了那个 diff」。

**测试要锁住的行为：**

- 三份 golden 与实际输出逐字节一致
- **输出里不含字面 `http://`**（§1.5，URL 全从基址来）—— 单独一条断言
- 应答文件名校验：传入的文件名与上下文算出的不一致时返回错误
- **SUSE 走「未实现」分支而不是 kickstart 分支**（见下方警示）

> **SUSE 用 AutoYaST，不是 kickstart。** M1 的应答文件名方法对 SUSE 返回
> `autoinst.xml`，引导参数是 `autoyast=`，格式是独立的 XML。
> **不要因为文件名对上了就放行到 kickstart 渲染器** —— 语法完全不同，
> 产出的文件安装器根本不认。MVP 范围内 SUSE 应明确返回「未实现」。

**验证：** `go test ./internal/render/ -run TestGolden -v` 全过；
`go test ./internal/render/ -run TestGolden -update` 后 `git diff` 应为空。

**完成标志：** commit `feat(render): kickstart assembly with golden file coverage`

---

### 任务 7：iPXE 菜单

**产出文件：** `menu.go`、`menu_test.go`

**为什么需要这一步：** 菜单是引导链的第二层分流（§4.7）—— 所有机器拿到同一份首跳脚本，
差异全部体现在这里渲染出的几行文本。

**Rocky（MVP 唯一要求）的菜单四行：** 内核行带仓库地址与应答文件地址与文本模式与
网络配置，initrd 行，两行之间的续行处理要正确。

**其余五个家族的处置：**

| 家族 | MVP 处置 |
|---|---|
| Ubuntu / Debian | 留分支，返回「未实现」（Phase 2） |
| SUSE | 同上（AutoYaST，见任务 6 的警示） |
| Windows | 留分支，返回「未实现」。它需要 wimboot 加三个 initrd，且**按固件分两套制品**（Phase 3） |
| ESXi | 留分支，返回「未实现」。**仅 UEFI**，BIOS 路径 MVP 不覆盖（§0.3） |

**留分支而不是直接 panic 的理由：** 分支位置就是将来实现的锚点，而错误信息能让
「有人给 MVP 建了个 Ubuntu 任务」这件事在引导时有明确反馈，而不是渲染出一份空菜单。

**Windows 与 ESXi 的菜单依赖固件模式，不能只按家族渲染**（§1.2）。
渲染上下文里有机器实际上报的固件模式 —— 将来实现时用它选制品键，
不要用任务记录的值（那可能与实际不符，§4.6 的存在理由）。

**设计要点：**

- **所有 URL 从渲染上下文的基址方法拼**，不出现字面 scheme。
- **token 在路径段里**，不在 query（§4.4）。

**测试要锁住的行为：**

- Rocky 菜单的四行结构，且三个 URL 都指向正确的基址
- 五个未实现的家族各自返回错误（不是空字符串）
- 菜单里不含字面 `http://`

**验证：** `go test ./internal/render/ -run TestMenu -v` 全过。

**完成标志：** commit `feat(render): iPXE boot menu for RHEL family`

---

### 任务 8：入口分派与端到端注入防护

**产出文件：** `renderer.go`、`injection_test.go`

**为什么需要这一步：** 两个入口方法的分派逻辑，加一个专门的端到端注入测试 ——
前者是接口实现，后者是把任务 1 的校验器与最终产物连起来验证。

**两个入口的职责：**

- **渲染菜单**：按家族分派
- **渲染应答文件**：**先校验传入的文件名等于上下文算出的文件名**，再按家族分派。
  Ubuntu 额外接受 `meta-data`（NoCloud 要求该目录下两个文件都存在，§4.5）

**为什么必须校验文件名：** 那个参数来自机器请求的 URL 路径段 ——
不能用它去选模板，否则机器可以通过改文件名让服务端渲染另一种格式的应答文件。

**端到端注入测试的做法：** 用一批恶意表单值走完整的渲染流程，断言两件事：

1. **渲染整体返回错误**（不是部分成功）
2. **恶意内容不出现在任何产出里** —— 包括菜单与应答文件。这一条抓的是
   「校验器漏了某个字段」或「某处绕过了校验器直接内插」

恶意值至少覆盖：命令分隔符、命令替换的两种写法、换行加 kickstart 段落标记、
超长值、含引号的值。

**编译期接口断言**放文件末尾。

**测试要锁住的行为：**

- 应答文件名不匹配时返回错误
- Ubuntu 的 `meta-data` 被接受
- **恶意值导致整体失败且不泄漏进产出**（逐类验证）
- 接口断言成立

**验证：**

- `go test ./internal/render/ -v` 全过
- `go build ./...` 断言成立
- `make check` 全绿

**完成标志：** commit `feat(render): entry dispatch with end-to-end injection guard`

---

## 交付验收

- [ ] `contract.Renderer` 两个方法实现，编译期断言通过
- [ ] 三份 golden file 与输出一致，且 `-update` 后 `git diff` 为空
- [ ] **产出中不含字面 `http://`**：`grep -rn 'http://' internal/render/ --include=*.go`
      只在测试的期望值里出现
- [ ] **渲染器不做 IO**：代码中无 `os.` 与 `net.` 调用
- [ ] 七类恶意输入逐一被拒绝，且不泄漏进任何产出
- [ ] `signal` 函数末尾返回成功（遥测失败不杀装机）
- [ ] `run_hook` 用管道状态数组取退出码，不是 `$?`
- [ ] 只 import `model` 与 `contract`，由 `archtest` 保证

## MVP 范围提示

只有 RHEL 家族（Rocky 9）需要真正实现。其余五个家族留分支返回「未实现」——
分支位置就是 Phase 2/3 的锚点。**不要为了「看起来完整」而实现半个 Ubuntu 渲染器**：
没有 golden file 覆盖、没有真机验证的渲染逻辑比明确的「未实现」更危险。

