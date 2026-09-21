# M7 Provider 内置实现与 ISO 导入 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** M1 契约与地基必须已完成。本模块与 M2–M6、M8 并行，只通过
> `internal/core/contract` 通信。
>
> **本计划描述做什么与为什么，不给代码。**

**目标：** 实现七个 provider 接口的内置版本、建任务的同步决策编排、以及 ISO 导入 job。

**架构：** 两个包。`internal/provider` 放七个接口的内置实现、按配置构造的工厂、
生命周期订阅与同步决策编排；`internal/imports` 放导入 job。

**MVP 不做插件加载器**（§7.6）。接口已在 M1 定义好，本模块只做内置实现，
通过配置选择。exec / http / grpc / starlark 四种加载器是 Phase 2–3 的事 ——
**接口边界是真正有价值的部分，加载机制可以后补**。

**技术栈：** 标准库 `net`（TCP 探测）、`io/fs`、`testing/fstest`；
ICMP 需要 `golang.org/x/net/icmp`（见任务 6 的取舍）

**SPEC 依据：** §7 全节、§2.1 制品集与导入、§5.6 服务端决策、§6.6 心跳的局限、
§10 失败模式 5

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/provider/diskplanner.go` | 五个具名磁盘策略 |
| `internal/provider/resolver.go` | 模板解析规则表 |
| `internal/provider/ipam.go` | static IPAM |
| `internal/provider/dns.go` | noop DNS |
| `internal/provider/inventory.go` | 内置 Inventory（查本地清单） |
| `internal/provider/notify.go` | 内置 Notify（记日志） |
| `internal/provider/heartbeat.go` | tcp 与 icmp 两个探测器 |
| `internal/provider/eventbus.go` | 事件总线实现 |
| `internal/provider/factory.go` | 按配置构造七个 provider |
| `internal/provider/resolve.go` | 建任务的同步决策编排 |
| `internal/provider/lifecycle.go` | 生命周期订阅（只订阅两个事件，见任务 8） |
| `internal/imports/extract.go` | 抽取器接口与真实实现 |
| `internal/imports/identify.go` | OS 家族识别 |
| `internal/imports/job.go` | 导入 job 的状态机 |
| `internal/imports/manifest.go` | manifest 生成 |

---

### 任务 1：磁盘策略 first 与 largest

**产出文件：** `diskplanner.go`（第一部分）、对应测试

**为什么需要这一步：** 磁盘策略决定擦哪块盘 —— 这是全系统后果最严重的决策之一
（§10 失败模式 5）。

**`DiskPlanner` 是纯决策、无 IO**（§7.2）：这套系统会擦盘，
**磁盘规划函数若行为不完全确定则故障无法复现**。所以：不读文件、不查库、不看时钟、
不用随机数、**输入相同必须输出相同**。

**两个策略：**

- **first**：按输入顺序取第一块。用于「机器型号统一、盘位固定」的批量场景
- **largest**：取容量最大的。**同容量并列时按设备标识排序后取第一个** ——
  不能依赖输入顺序，因为那个顺序来自机器侧采集，不保证稳定

**所有策略的共同要求：**

- **返回的设备标识用 by-id / WWN，绝不用内核分配的名字**（§10 失败模式 5）。
  内核名会漂移 —— 同一台机器两次启动的 `/dev/sda` 可能是不同的物理盘
- **必须填「为什么这么选」的说明**，dry-run 端点要展示它（§8.2）
- **零块盘返回明确错误**，不要返回空布局 —— 空布局会让渲染器产出一份没有磁盘指令的
  kickstart，而 anaconda 遇到那种情况的行为是交互式提问，机器会挂在那里等人

**测试要锁住的行为（两个策略各一组）：**

- 正常多盘场景选对
- **单块盘**
- **零块盘返回错误**
- **同容量并列时结果确定**（largest：打乱输入顺序两次，结果相同）
- 说明非空且含判断依据（容量值或型号）

**验证：** `go test ./internal/provider/ -run 'TestDiskFirst|TestDiskLargest' -v` 全过。

**完成标志：** commit `feat(provider): first and largest disk policies`

---

### 任务 2：磁盘策略 by-model

**产出文件：** `diskplanner.go`（第二部分）、对应测试

**为什么需要这一步：** 混合盘机型上需要按型号锁定目标盘 ——
比如「装到 SATA SSD 上，不要碰那两块 NVMe 数据盘」。

**规则：** 匹配型号含指定子串的盘；多块匹配时按设备标识排序取第一个；
**一块都不匹配时返回明确错误**。

**型号串从配置来**（M1 的配置里有这个字段，且配置校验已保证选了这个策略时它非空）。

**匹配用子串而非精确相等：** 型号字符串里常带空格、修订号与厂商前缀
（如 `SAMSUNG MZ7LH480HAHQ-00005`），要求运维填精确值不现实。

**不匹配时返回错误而不是回落到其它策略。** 回落的后果是：运维以为装在了 SSD 上，
实际装在了随便一块盘上 —— 而这种错误装完才发现，那时数据已经被擦了。

**测试要锁住的行为：**

- 单块匹配
- 多块匹配时结果确定
- **零块匹配返回错误**（不回落）
- 子串匹配生效（填厂商名能匹配到完整型号）
- 大小写处理要明确（建议不敏感，并在说明里写清）

**验证：** `go test ./internal/provider/ -run TestDiskByModel -v` 全过。

**完成标志：** commit `feat(provider): by-model disk policy`

---

### 任务 3：磁盘策略 raid1-ssd

**产出文件：** `diskplanner.go`（第三部分）、对应测试

**为什么需要这一步：** 系统盘做软 RAID1 是常见要求。

**规则：** 从非旋转盘（SSD）里挑两块容量相同的，返回主盘与两个成员。

**不足两块 SSD 时返回明确错误，不退化为单盘。**

**这个决定的理由有两条：**

1. **静默退化会让同一个策略名在不同机器上做不同的事**，违反 §7.2 的纯函数确定性 ——
   而那正是「故障无法复现」的开始
2. **它会掩盖「这批机器 SSD 不够」这个应该被运维立即看到的事实。**
   运维以为装了 RAID1，实际是单盘 —— 那块盘坏掉时才发现没有冗余

**容量必须相同才组 RAID1**（不同容量会浪费到小的那块，且多数实现会拒绝）。
容量不同的多组 SSD 时，取容量最大的那一组。

**测试要锁住的行为：**

- 两块同容量 SSD 正常组对
- **只有一块 SSD 返回错误**
- **零块 SSD 返回错误**
- 有两块 SSD 但容量不同时返回错误
- 混合盘（两块 SSD 加两块 HDD）时只选 SSD
- 三块同容量 SSD 时选定的两块是确定的

**验证：** `go test ./internal/provider/ -run TestDiskRAID1 -v` 全过。

**完成标志：** commit `feat(provider): raid1-ssd disk policy, no silent degradation`

---

### 任务 4：磁盘策略 by-hook 与工厂

**产出文件：** `diskplanner.go`（第四部分）、对应测试

**为什么需要这一步：** 有些机型的选盘逻辑复杂到规则表表达不了，
需要在 `%pre` 阶段由 hook 动态决定（§5.6 承认这条路径，但同时指出它的代价）。

**规则：** 不做决策，返回一个空的主盘与一句说明「由 pre 阶段的 hook 决定」。

**在说明里写清这个策略的代价**（§5.6）：把逻辑推到机器上，
**失败时无日志可看、无法复现、无法 dry-run**。服务端决策可调试、可 dry-run、可审计 ——
所以 by-hook 是兜底而非首选。

**策略工厂：** 按策略名返回对应实现，未知名字返回错误。
五个策略名与 M1 的枚举一致。

**测试要锁住的行为：**

- by-hook 返回空主盘与非空说明
- 工厂对五个合法名字各返回正确的实现（用一个已知输入验证行为）
- **未知策略名返回错误**

**验证：** `go test ./internal/provider/ -run 'TestDiskByHook|TestDiskFactory' -v` 全过。

**完成标志：** commit `feat(provider): by-hook policy and disk planner factory`

---

### 任务 5：模板解析规则表与三个简单 provider

**产出文件：** `resolver.go`、`ipam.go`、`dns.go`、`inventory.go`、`notify.go`、对应测试

**为什么需要这一步：** 四个内置实现都很薄，放一个任务里。

**模板解析器的 MVP 形态是规则表**（§7.6）：按 OS、版本、厂商、型号匹配，
**自上而下第一条命中生效**。规则从配置的文件路径读；路径为空时用一条「全匹配到默认模板」
的兜底规则。

**「第一条命中生效」要有测试锁住** —— 多条都匹配时的行为若不确定，
运维调整规则顺序就不会有预期效果。

**一条都不匹配时返回明确错误**（失败语义是阻塞建任务，§7.4）——
不要回落到某个默认模板，那会让机器装上不该装的系统。

**引入脚本引擎的信号**（写进注释）：规则表开始长出嵌套条件，
或运维反复要求「这批机器特殊处理」（§7.6）。

**static IPAM：** 从请求与表单直接取，不访问外部系统。释放操作是空实现。

**为什么空实现也要写：** 接口完整性，以及将来换成真实 IPAM 时调用点不用改 ——
`task.failed` 之后释放 IP 这条路径届时自动生效。

**noop DNS：** 记日志即返回成功。失败语义是只告警（§7.4）——
一次 DNS 抖动不能把装好的机器标成失败。

**内置 Inventory：** 查本地主机清单（MVP 没有外部 CMDB）。
失败语义是阻塞但允许手工录入兜底 —— 而 MVP 阶段手工录入就是唯一路径。

**内置 Notify：** 记结构化日志。**这是 MVP 的告警出口** ——
日志里要含任务 id、状态、失败原因，便于运维用 journalctl 过滤。

**测试要锁住的行为：**

- 规则表：单条命中、**多条匹配时第一条生效**、零条匹配返回错误、空配置用兜底规则
- IPAM：从表单取到的值正确传出
- DNS/Notify：调用不返回错误
- Inventory：查到与查不到两种情形

**验证：** `go test ./internal/provider/ -run 'TestResolver|TestIPAM|TestDNS|TestInventory|TestNotify' -v` 全过。

**完成标志：** commit `feat(provider): rule-table resolver and four built-in providers`

---

### 任务 6：心跳探测 —— tcp 与 icmp

**产出文件：** `heartbeat.go`、对应测试

**为什么需要这一步：** 机器主动推送后，拉取不再承担状态推进，只作为「推送断了」时的
兜底判断（§6.6）。

**必须在注释里写清拉取的本质局限**（§6.6）：**ICMP/TCP 在你最关心的故障里全是正常的** ——
anaconda 卡在依赖解析、hook 死循环、仓库 404 重试时，内核活着、网络通着。
**它能替代存活判断，替代不了进展判断。** 进展靠信号加 syslog。

**两个探测器：**

- **tcp（默认）**：带超时的拨号。零权限要求
- **icmp**：需要特权套接字

**ICMP 的取舍决定：默认用 tcp，icmp 实现但优雅降级。**

理由：ICMP 需要 root 或 `CAP_NET_RAW` 或调整内核的 ping 组范围。
**心跳只是辅助通道，不值得为它给整个进程提权** —— 提权的影响面是全系统，
而收益只是「能 ping 通还没配业务 IP 的机器」。

**无权限时的行为要明确：不 panic、不让整批探测失败**，
而是对每个目标返回「探测失败」并在错误信息里写明「需要提权」。
这样运维在控制台看到的是「心跳不可用，原因是权限」而不是「所有机器都死了」。

**批量探测要并发但有上限**（避免几百个目标同时拨号耗尽文件描述符），
且单个目标的失败不影响其余。

**测试要锁住的行为：**

- tcp 探测：用本地监听的端口验证探活成功；用关闭的端口验证失败
- **单个目标超时不影响其余目标的结果**
- icmp 无权限时返回带「需要提权」说明的失败，**而不是 panic**
- 空目标列表返回空结果不报错
- 并发上限生效（用大量目标验证不会耗尽资源）

**验证：** `go test ./internal/provider/ -run TestHeartbeat -v` 全过。
**ICMP 的测试要能在无权限环境下通过**（它验证的是降级行为）。

**完成标志：** commit `feat(provider): tcp heartbeat with graceful icmp degradation`

---

### 任务 7：事件总线与工厂

**产出文件：** `eventbus.go`、`factory.go`、对应测试

**为什么需要这一步：** 生命周期通知的分发机制，加按配置构造七个 provider 的入口。

**事件总线的要点：**

- **fire-and-forget**：发布不返回错误，回调也不返回错误（M1 契约定的）
- **订阅者的 panic 不能带走发布者** —— 每个回调用 recover 包住并记日志。
  一个写 DNS 的回调 panic 不该让状态机崩溃
- **同步调用还是异步？选同步。** 异步的话「任务已 ready」的通知可能在进程退出时丢失；
  同步会让状态机等待订阅者，但订阅者都是记日志级别的操作。
  **要在注释里写明这个取舍** —— 将来接入真实的告警推送时需要改成异步加队列

**工厂的要点：** 按配置的选择项构造七个实现，**未知的选择项返回错误而不是回落到默认** ——
配置里写错一个 provider 名字应该在启动时被发现（M9 的校验模式会跑到这里）。

**工厂还要暴露同步决策编排器**（下一个任务的产出）供装配点取用。

**测试要锁住的行为：**

- 发布的事件到达订阅者
- **订阅者 panic 时发布者不受影响**，且 panic 被记录
- 多个订阅者都收到
- 工厂对合法配置构造成功
- **未知的 provider 名返回错误**

**验证：** `go test ./internal/provider/ -run 'TestEventBus|TestFactory' -v` 全过。

**完成标志：** commit `feat(provider): event bus with panic isolation and provider factory`

---

### 任务 8：同步决策编排与生命周期订阅

**产出文件：** `resolve.go`、`lifecycle.go`、对应测试

**为什么需要这一步：** §7.4 要求 IPAM、模板解析、磁盘规划三者失败时**阻塞建任务**，
而事件总线做不到阻塞。

**这是一处偏离 SPEC 图示的实现决定，理由要写进注释：**

SPEC §7.5 画的是「`task.created` → 申请 IP、分配主机名，结果写回决策参数」。
但**事件总线是 fire-and-forget 的，事件发出时任务已经落库了** ——
IPAM 此时失败已经来不及阻塞。那张图描述的是因果关系，不是实现机制。

**落地方式：**

- **同步决策编排器**（实现 M1 的对应接口）：依次调模板解析、IPAM、磁盘规划，
  把三者结果合并成一份参数。**任一失败即整体失败**，错误直接返回给 M6 的建任务端点
- **生命周期订阅只订两个事件**：`task.ready`（写 DNS、推通知）与
  `task.failed`（推告警）。这两个才真正匹配 fire-and-forget 语义

**编排顺序：模板解析 → IPAM → 磁盘规划。**
模板解析在最前，因为后两者可能需要知道装什么系统（比如不同 OS 的分区方案不同）。

**返回的参数要覆盖 fact 命名空间里的相应键**（业务 IP、网关、掩码、主盘），
M4 的渲染器会消费它们。

**决策结果被固化**（§2.6：决策要固化、IO 可以实时）—— 固化动作由 M6 在写库时完成，
本模块只负责产出。**这条要在注释里写明**，否则可能有人在渲染时再调一次编排器 ——
那会让同一任务前后两次拿到不同的 IP。

**测试要锁住的行为：**

- 三个 provider 都成功时参数合并正确，含全部预期键
- **任一 provider 失败时整体返回错误**（逐个验证三种失败）
- **失败时不产生部分结果** —— 返回的参数应为空，避免调用方误用
- 生命周期订阅只对两个事件生效（发布「已创建」事件时不触发 DNS 写入）
- ready 事件触发 DNS 注册与通知

**验证：** `go test ./internal/provider/ -run 'TestResolveTaskParams|TestLifecycle' -v` 全过。

**完成标志：** commit `feat(provider): synchronous param resolution and lifecycle hooks`

---

### 任务 9：抽取器隔离与 OS 家族识别

**产出文件：** `internal/imports/extract.go`、`identify.go`、对应测试

**为什么需要这一步：** 导入要读真 ISO（挂载或解压），但测试不能依赖几 GB 的下载。
用一个窄接口把「怎么读到文件」隔离开。

**抽取器接口的职责：** 把一个本地 ISO 路径变成一个可遍历的文件系统视图。
真实实现用解压或挂载；测试注入内存文件系统。

**为什么用 `fs.FS` 而不是自定义接口：** 标准库的抽象已经够用，
且 `testing/fstest` 直接提供内存实现 —— 不需要自己写替身。

**OS 家族识别按特征文件**（§2.1 的表）：

| 家族 | 特征文件 |
|---|---|
| RHEL 系 | `.treeinfo` |
| Ubuntu | `.disk/info` |
| Debian | `dists/` 目录 |
| ESXi | `boot.cfg` 加 `mboot.efi` |
| Windows | `sources/install.wim` |

**识别不出来要明确失败，不要猜。** 猜错的后果是按错误的家族抽取，
产出一个结构不对的制品集 —— 而那个错误要等机器引导失败才发现。

**识别顺序要考虑歧义：** Ubuntu 的 ISO 也含 `dists/`，所以 Ubuntu 的判定要在 Debian 之前。
**这条要写进注释** —— 否则将来有人「顺手整理」了顺序就会把 Ubuntu 识别成 Debian。

**MVP 支持闸门：** 识别出 Windows 或 ESXi 时**明确报「MVP 不支持」并把制品集置失败**，
不要装作成功。理由：一个 state=ready 但内容不完整的制品集会被建任务页列出来，
运维选了它才发现装不了。

**测试要锁住的行为：**

- 五个家族各自被正确识别（用内存文件系统造特征文件）
- **同时含 `dists/` 与 `.disk/info` 时识别为 Ubuntu**（歧义处理）
- 无特征文件时返回错误
- **Windows 与 ESXi 被识别但返回「MVP 不支持」**

**验证：** `go test ./internal/imports/ -run 'TestExtract|TestIdentify' -v` 全过。

**完成标志：** commit `feat(imports): extractor isolation and OS family identification`

---

### 任务 10：RHEL 抽取与 manifest

**产出文件：** `internal/imports/manifest.go`、抽取逻辑、对应测试

**为什么需要这一步：** ISO 是导入期制品，不是运行时制品（§2.1）——
iPXE 引导的是从 ISO 里取出的内核与 initrd，再用内核参数指向解压后的仓库树。
**因此解压与抽取在导入时一次完成，装机时零解压、nginx 直接发静态文件。**

**RHEL 系的抽取四步：**

1. 从特征文件里读内核与 initrd 的路径（那个文件是 INI 格式，含镜像段）
2. 把内核与 initrd 复制到制品目录，路径按 `docs/contracts/filesystem.md` 的布局
3. 建仓库树（复制或硬链接整个仓库目录）
4. 生成 manifest

**manifest 的内容：每个文件的相对路径与 sha256，加一个整体校验值。**
它服务于两处：§4.6 第三项的制品完整性校验，以及控制台的「校验完整性」端点。

**落盘路径必须严格按文件系统契约。** Go 签发 URL、nginx 按路径找文件，
**不一致即引导时 404**（§10 运维点一）—— 而那个故障的现场是「机器取内核时失败」，
从机器侧看不出是路径拼错了。

**抽取完成后写回两项：** OS 家族与路径结构（用 M1 的写回方法）。
状态更新是另一个方法 —— 两者分开是因为**导入失败时只改状态、不该碰已写入的路径**。

**测试要锁住的行为：**

- 从特征文件正确解析出内核与 initrd 路径
- 抽取后的文件在预期位置（路径与契约一致）
- manifest 含全部文件且 sha256 正确
- **特征文件格式异常时返回错误**（缺镜像段、路径为空）
- 写回的路径结构含内核、initrd 列表、仓库三项

**验证：** `go test ./internal/imports/ -run 'TestExtractRHEL|TestManifest' -v` 全过。

**完成标志：** commit `feat(imports): RHEL artifact extraction and manifest generation`

---

### 任务 11：导入 job 的状态机

**产出文件：** `internal/imports/job.go`、对应测试

**为什么需要这一步：** 导入是异步的（§2.1）—— 几 GB 的解压要几分钟，
不能阻塞 HTTP 请求。

**状态流转：** `importing` → `ready` 或 `failed`。

**失败时要把原因写进制品集的错误字段**，供 UI 展示（M8 的制品集页面显示导入进度与失败原因）。
**错误信息要具体**：「识别不出 OS 家族」「缺少内核文件」「目标目录无写权限」——
而不是「导入失败」。

**job 的执行流程：**

1. 打开抽取器得到文件系统视图
2. 识别 OS 家族；不支持则置失败
3. 按家族抽取
4. 生成 manifest
5. 写回家族与路径
6. 置 ready 并记录 manifest 校验值

**任何一步失败都置 failed 并写原因，且不继续后续步骤。**

**并发控制：** 同一时刻只允许一个导入 job 运行。理由：解压是 IO 密集的，
并发两个只会让两个都变慢；而运维一次导入一个 ISO 是常态。
**用一个带缓冲为 1 的信号量或互斥锁**，第二个请求排队而不是拒绝。

**测试要锁住的行为：**

- 成功路径：状态从 importing 到 ready，manifest 校验值被记录
- **每一步失败都置 failed 且错误原因具体**（逐步注入失败验证）
- 失败时**已写入的路径不被清空**（用 M1 的两个方法分工验证）
- 不支持的家族置 failed 且原因说明是 MVP 限制
- 并发提交两个 job 时串行执行

**验证：** `go test ./internal/imports/ -v` 全过。

**完成标志：** commit `feat(imports): async import job with specific failure reasons`

---

### 任务 12：接口断言与全量验证

**产出文件：** 各文件末尾的断言

**实现流程：** 给七个 provider 实现、编排器、事件总线各加编译期接口断言。

**验证：**

- `go build ./...` 全部断言成立
- `go test ./internal/provider/ ./internal/imports/ -race -v` 全过
- `make check` 全绿

**完成标志：** commit `test(provider): assert all provider interfaces satisfied`

---

## 交付验收

- [ ] **五个磁盘策略全部实现**，各有边界测试（单盘、零盘、并列）
- [ ] **raid1-ssd 不足两块盘时返回错误，不退化**
- [ ] 所有磁盘策略返回 by-id 标识与非空说明
- [ ] 磁盘策略是纯函数：代码中无 IO、无时钟、无随机数
- [ ] 模板规则表「第一条命中生效」有测试锁住
- [ ] **心跳默认 tcp；icmp 无权限时优雅降级而非 panic**
- [ ] **同步决策编排器任一步失败即整体失败，不产生部分结果**
- [ ] 生命周期订阅只订 ready 与 failed 两个事件
- [ ] 导入 job 的每种失败都有具体原因
- [ ] 抽取逻辑通过内存文件系统测试，**不依赖真 ISO**
- [ ] 只 import `model`、`contract`、`config`，由 `archtest` 保证

## 两处偏离 SPEC 的记录

1. **生命周期订阅不承担阻塞决策**（§7.5 的图示）：事件总线是 fire-and-forget 的，
   做不到阻塞。改为同步编排器在写库前调用。图示描述的是因果关系，不是实现机制。
2. **ICMP 探测默认不启用**：需要提权，而心跳只是辅助通道（§6.6）。
   实现保留并优雅降级。

