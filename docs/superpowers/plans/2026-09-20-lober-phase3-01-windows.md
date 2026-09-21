# P3-01 Windows 支持 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** Phase 2 全部完成。与 P3-02 至 P3-04 并行。
>
> **本计划描述做什么与为什么，不给代码。**

**目标：** 让 Windows Server 走通 —— wimboot 引导 WinPE，WinPE 内完成安装，
unattend.xml 驱动无人值守。

**架构：** 与三个 Linux 家族**不共用安装机制**。Linux 是 installer 驱动
（anaconda / debian-installer / curtin 读应答文件），Windows 是
**「引导一个最小系统，再由它执行安装」** —— 引导的是 WinPE，安装动作由 WinPE 内的
DISM 或安装程序执行。

**这个差异决定了整个计划的结构。** 三处具体后果：

| 后果 | 详情 |
|---|---|
| 制品要导两套 | `boot.wim` 的位宽须与固件匹配（§0.3、§2.1），**同一 Windows 版本按固件导两个制品集** |
| 引导菜单形态不同 | wimboot 加三个 initrd 项（§4.5），而非 kernel 加一个 initrd |
| 遥测通道要重做 | WinPE 里没有 bash、没有 curl（默认）、没有 syslog 客户端 |

**技术栈：** wimboot；WinPE；unattend.xml（用 XML 转义的模板引擎）；
WinPE 内的脚本用批处理或 PowerShell

**SPEC 依据：** §0.3、§1.2、§2.1、§4.5、§4.6、§5.2 末段、附录 A.4–A.6

---

## 三个必须先接受的事实

这三条来自附录 A.4–A.6，它们排除了几种看似可行的做法：

1. **wimboot 只能引导 WinPE，不能引导完整 Windows。** 完整 OS 需从 iSCSI 或 AoE 卷
   块级引导 —— 那是另一套东西，本计划不做
2. **标准流程是 HTTP 引导进 WinPE，再由它连文件服务器完成安装。**
   所以「Windows 的镜像封装」实质是维护安装镜像加应答文件，
   **真正的安装动作由 WinPE 内的工具执行**
3. **wimboot 走 HTTP 而非 TFTP**，千兆网下典型 WinPE 镜像数秒下完 ——
   传输不是瓶颈，不需要为它做缓存

**由第 2 条推出一个关键设计：应答文件的取用时机与 Linux 不同。**
Linux 的 installer 在启动时就取应答文件；Windows 是 WinPE 起来后由 `startnet.cmd`
去取（§4.5 注明「unattend 由 WinPE 内 startnet.cmd 去取，不烧进 WIM」）。

**不烧进 WIM 的理由要理解**：烧进去意味着每台机器一个 WIM，而 WIM 有几百 MB ——
per-machine 的 WIM 既浪费存储又让制品集失去「一次导入多次使用」的意义。

---

### 任务 1：制品集的双固件导入

**产出文件：** `internal/imports/extract_windows.go`、对应测试

**为什么需要这一步：** §2.1 的固件字段对 Windows 取具体值而非「通用」——
**同一 Windows 版本要导两个制品集**，因为 `boot.wim` 的位宽须与固件匹配。

**识别特征文件是安装镜像的路径**（§2.1 的表）。

**抽取要产出的四类文件**（§4.5 的菜单形态）：

| 文件 | 作用 |
|---|---|
| wimboot | iPXE 侧的引导器，**不在 ISO 里** —— 要单独获取并放进制品根目录 |
| 引导配置数据 | WinPE 的引导描述 |
| 引导镜像描述文件 | 同上 |
| WinPE 镜像 | 实际被引导的最小系统 |

**wimboot 不在 ISO 里这件事要在导入流程里处理好。** 它是一个独立的二进制
（与 iPXE 二进制同类），气隙环境下要从外网取得后带进内网。
**导入 Windows 制品集时若 wimboot 不存在，要明确报错而不是等引导时 404** ——
后者的现场是「机器取不到引导器」，而那条错误在 iPXE 里几乎看不出原因。

**WinPE 镜像的来源要写清：它不是 ISO 里现成的。** 标准 Windows ISO 里的
引导镜像可以用，但通常要定制（加驱动、加脚本）。**这是一个人工前置步骤**，
与「硬 RAID 需在装机前配好」（§5.6 末段）同类 —— 导入流程只负责登记与校验，
不负责构建 WinPE。

**要在导入时校验位宽与声明的固件一致。** 声明 UEFI 却导入了 32 位的 WinPE
会在引导时失败，而那时运维已经在机房了。**校验方式**：读镜像的架构标识，
与制品集声明的固件比对。

**测试要锁住的行为：**

- 识别出 Windows 家族
- 四类文件齐全时导入成功，缺任一则失败且原因具体
- **wimboot 缺失时报错**（不是等引导时才发现）
- **位宽与声明固件不符时导入失败**
- 同一版本导两个制品集（不同固件）互不冲突

**验证：** `go test ./internal/imports/ -run TestWindows -v` 全过。

**完成标志：** commit `feat(imports): Windows artifact set with per-firmware validation`

---

### 任务 2：引导菜单与固件选键

**产出文件：** `internal/render/menu.go`（补 Windows 分支）、对应测试

**为什么需要这一步：** Windows 的菜单形态与 Linux 完全不同 ——
wimboot 作为「内核」，三个文件作为 initrd 项且**各带一个名字标签**（§4.5）。

**菜单的四行：** wimboot 行，加三个带标签的 initrd 行。
**标签不能省** —— wimboot 靠它区分三个文件的用途。

**固件选键是本任务的核心**（§4.6 第二项）：机器上报的固件模式决定用哪个制品集。
Phase 1 的 precheck 已经会拒绝不兼容的组合，**但 Windows 的情况不是「拒绝」
而是「选对的那一套」** —— 同一个任务 pin 的制品集是固定的，
所以实际要做的是：

**建任务时就按机器记录的固件选制品集**（M6 的建任务已有固件兼容校验），
**引导时再按实际上报值核验一次**（§4.6 的存在理由：记录值可能与实际不符）。
不符时按 §4.6 硬拒绝并置失败 —— **不要试图在引导时换制品集**，
那会让任务 pin 的内容与实际下发的不一致，破坏 §2.6 的可复现性。

**测试要锁住的行为：**

- 菜单四行结构正确，三个 initrd 项各带标签
- URL 从基址拼出，不含字面 scheme
- **实际固件与制品集声明不符时拒绝下发并置失败**（不换制品集）
- 两种固件的制品集各自渲染出正确的菜单

**验证：** `go test ./internal/render/ -run TestWindowsMenu -v` 全过。

**完成标志：** commit `feat(render): Windows wimboot menu with firmware-matched artifacts`

---

### 任务 3：unattend.xml 渲染

**产出文件：** `internal/render/unattend.go`、golden file

**为什么需要这一步：** unattend.xml 是 Windows 的应答文件。它是 XML，
而表单值会进它 —— **注入防护的形式与 kickstart 不同**。

**用带 XML 转义的模板引擎**（§5.6 明确：`unattend.xml` 用它以获得 XML 转义），
而 kickstart 与 preseed 用不转义的那个。

**但转义不能替代校验。** Phase 1 的 guard 校验器仍要全部跑一遍 ——
转义能防「值破坏 XML 结构」，防不了「值是一个合法但恶意的主机名」。
**两道都要**，这条要写进注释，否则会有人觉得「有转义就够了」。

**unattend.xml 的几个段落**（按 Windows 的安装阶段划分）：
磁盘配置、镜像选择、区域与语言、账户、首次登录命令。

**两处敏感内容的处理**（§1.5 的缓解手段）：

1. **不放域管凭据。** 用最小权限的专用加域账号，**或把加域挪到装机后另一通道** ——
   MVP 无 TLS，应答文件在网络上是明文的
2. **本地管理员密码**要么用一次性的、要么装完立即轮换

**这两条在 Linux 侧的对应做法是「注入 SSH 公钥并关闭密码登录」，
但 Windows 没有等价手段** —— 所以要在文档里写明这是 Windows 路线的一个
实质性弱点，而缓解只能靠「凭据短时效」与「装机 VLAN 真正隔离」。

**测试要锁住的行为：**

- 输出是合法 XML（**用 XML 解析器解一遍**，不只对比文本）
- 含特殊字符的表单值被正确转义且不破坏结构
- **guard 校验器仍然生效**（恶意值被拒绝，而不是被转义后放行）
- golden file 覆盖：最小 unattend、带首次登录命令的完整版
- **输出里不含域管凭据的占位符被误填的情况**（模板里根本不该有那个字段）

**验证：** `go test ./internal/render/ -run TestUnattend -v` 全过。

**完成标志：** commit `feat(render): unattend.xml with XML escaping plus value validation`

---

### 任务 4：WinPE 内的遥测通道

**产出文件：** `internal/render/winpe.go`、对应测试

**为什么需要这一步：** **这是本计划最容易被低估的任务。**
Phase 1 注入的 `signal()` 与 `run_hook()` 是 bash 脚本 ——
WinPE 里没有 bash，默认也没有 curl。

**三个可用的上报手段，选一个：**

| 手段 | 取舍 |
|---|---|
| **PowerShell 的 Web 请求**（推荐） | WinPE 默认含 PowerShell（需在构建 WinPE 时加入对应组件）；语法清晰 |
| 批处理加 `bitsadmin` | 无需额外组件；但它是传输工具，POST 与 body 支持很弱 |
| 往 WinPE 里塞一个 curl 二进制 | 最接近 Linux 侧的行为；但要维护那个二进制 |

**选 PowerShell。** 理由：它能正常发 POST 与纯文本 body，
且 WinPE 加 PowerShell 组件是标准做法。**但要在文档里写明这是 WinPE 的构建前置** ——
不含 PowerShell 的 WinPE 会让整个遥测静默失效。

**上报函数要保持与 Linux 侧同样的四条行为**（§6.3）：
重试三次、退避、失败时落本地日志、**最后返回成功（遥测失败不杀装机）**。

**信号点的映射与 Linux 不同。** Windows 的安装阶段是：
WinPE 起来 → 分区 → 应用镜像 → 配置引导 → 重启 → 首次启动配置 → 完成。

| 信号 | Windows 的对应位置 |
|---|---|
| pre-start / pre-done | `startnet.cmd` 的开始与分区前 |
| packages-start / packages-done | **应用镜像的前后**（这一步对应 Linux 的装包） |
| post-start / post-done | 配置引导与写入首次启动脚本的前后 |
| rebooting | 重启前 |
| booted 及之后 | **首次登录前执行的脚本里**（见任务 5） |

**syslog 这条辅助通道在 Windows 下基本失效**（§6.5）：WinPE 没有 syslog 客户端。
**所以 Windows 的可观测性弱于 Linux** —— 要在文档里写明，
并建议把 WinPE 内的完整日志在装完后上传（用 facts 端点或一次专门的上报）。

**测试要锁住的行为：**

- 生成的 PowerShell 上报函数含四条行为
- 信号名都在 M1 的信号表内
- **上报失败时脚本继续**（用一个不可达的地址验证脚本不中止）
- 各阶段的信号点都在生成的脚本里出现

**验证：** `go test ./internal/render/ -run TestWinPE -v` 全过。
**另在实机核对：WinPE 里 PowerShell 可用且能发出请求。**

**完成标志：** commit `feat(render): WinPE telemetry via PowerShell`

---

### 任务 5：firstboot 的 Windows 对应机制

**产出文件：** `internal/render/winpe.go`（扩展）、对应测试

**为什么需要这一步：** §5.2 末段给了两个对应机制：
**首次登录前以系统身份执行的脚本**，或应答文件里的首次登录命令。

**选前者。** 三条理由：

1. **它以系统身份执行**，权限等同 Linux 的 root —— 而首次登录命令依赖有人登录
2. **它在首次登录前跑**，与 Linux 的 firstboot（systemd oneshot 在启动时跑）时机一致
3. **无人值守装机不该依赖登录动作** —— 那台机器可能永远没人交互登录

**要落进目标系统的四样**（与 Linux 的 firstboot 脚手架对应，§5.8）：
finalize token（**权限要限制到仅系统账号可读**）、参数文件、用户的 firstboot 脚本、
一个执行入口脚本。

**只跑一次的保证方式：** Linux 用标记文件加条件启动（§5.8）。
Windows 侧的等价做法是**脚本开头检查标记文件、成功后删除它** ——
而那个脚本本身在执行后会被系统自动忽略（它只在首次登录前跑一次）。
**两层保证都要**：系统的一次性机制加标记文件。理由与 Linux 侧相同 ——
脚本中途死掉时标记仍在，可以看出未跑完。

**凭据清理的 Windows 对应**（§9.5）：
安全删除 token 与参数文件；**另要清理安装日志里的应答文件路径**。
Windows 的安装过程会把应答文件复制到目标系统的一个固定位置 ——
**那个副本含明文密码，必须删除**。这条在 Linux 侧的对应是清理安装记录里的密码哈希。

**测试要锁住的行为：**

- 四样都落进目标系统，token 文件权限受限
- 一次性保证是两层（系统机制加标记文件）
- 凭据清理含**应答文件副本的删除**
- 信号顺序与 Linux 侧一致

**验证：** `go test ./internal/render/ -run TestWindowsFirstboot -v` 全过。

**完成标志：** commit `feat(render): Windows firstboot via system-account setup script`

---

### 任务 6：实机核对清单

**产出文件：** `deploy/verified-windows-<日期>.md`

**为什么需要这一步：** Windows 路线有六处只能实机确认的事，
且失败都发生在「机器已经在机房」的时刻。

**核对清单：**

- [ ] **两种固件的机器各自拿到位宽匹配的 WinPE 并成功引导**
- [ ] wimboot 的三个 initrd 标签正确（错了的表现是 WinPE 启动失败且信息模糊）
- [ ] WinPE 里 PowerShell 可用，且能向网关发出 POST
- [ ] `startnet.cmd` 能取到应答文件（**它是 WinPE 内取的，不是引导时取的**）
- [ ] 应答文件的磁盘配置用的是服务端决策的盘
      （Windows 的磁盘标识方式与 Linux 的 by-id 不同 —— **要确认决策结果能被正确表达**）
- [ ] 首次登录前的脚本以系统身份执行，且信号能发出
- [ ] 装完后应答文件副本已被清理（**手工检查目标系统上那个固定位置**）
- [ ] **故意用位宽不匹配的制品集建一个任务，确认引导时被拒绝**（§4.6 的验证）

**倒数第二条是安全项，不能跳过** —— 那个副本含明文密码。

**倒数第一条是对 §4.6 硬拒绝的主动验证** —— 与 Phase 2 主动验证
「Ubuntu 少一个斜杠确实会失败」同一性质：确认防护真的有效，而不只是代码里有那个分支。

**磁盘标识那条要特别注意**（第五条）：服务端的磁盘决策产出 by-id 路径，
而 Windows 的应答文件用磁盘序号或磁盘 ID。**两者的映射关系要实机确认** ——
这可能需要在 WinPE 里做一次转换，那是本任务可能发现的一个额外工作量。

**完成标志：** commit `docs: Windows on-hardware verification record`

---

## 交付验收

- [ ] 同一 Windows 版本能导两个制品集（两种固件），位宽校验生效
- [ ] wimboot 缺失时导入报错，不留到引导时
- [ ] 菜单的三个 initrd 项带正确标签
- [ ] **实际固件与制品集不符时拒绝下发，不换制品集**
- [ ] unattend.xml 是合法 XML，**转义与 guard 校验两道都在**
- [ ] 模板里没有域管凭据字段
- [ ] WinPE 的上报函数含四条行为，失败时不中止装机
- [ ] firstboot 用系统身份脚本，一次性保证是两层
- [ ] **凭据清理含应答文件副本的删除**
- [ ] 实机核对清单逐项勾完
- [ ] `make check` 全绿

## 边界提醒

**不要试图让 Windows 复用 kickstart 那套渲染框架。**
两者的机制不同（installer 驱动 vs 引导最小系统再安装）、
应答文件格式不同（脚本 vs XML）、遥测手段不同（bash vs PowerShell）、
firstboot 机制不同（systemd vs 系统账号脚本）。

**共用的只有三样**：fact 命名空间的键、信号名与信号表、以及「上报失败绝不中断装机」
这条原则。**这三样是跨机制的契约，其余都是各自实现。**

强行抽象的代价：每个抽象点都是两种机制在打架，而 Windows 侧的调试成本远高于
Linux（没有 syslog、WinPE 里工具少、失败时机器停在一个蓝屏或一个命令行上）。

