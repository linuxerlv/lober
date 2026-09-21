# P2-01 多 OS 支持：Ubuntu 与 Debian 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）
> 或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
>
> **前置：** Phase 1 全部完成，一台 Rocky 在实机装通过。
> 与 P2-02 至 P2-05 并行，**不改 Phase 1 的契约**。
>
> **本计划描述做什么与为什么，不给代码。**

**目标：** 让 Ubuntu（autoinstall）与 Debian（preseed）两个家族走通，
复用同一套 hook 阶段映射与 fact 注入。

**架构：** 渲染器从「一个家族一个分支」变成「三个家族三个渲染函数 + 共用的骨架生成器」。
共用部分是 hook 阶段映射、参数内联、fact 注入、客户端函数注入、firstboot 脚手架；
不共用的是应答文件的语法与引导菜单的参数。

**这个计划的主要风险不是代码，是参数拼写。** SPEC 附录 B 第二项明确列出
「各 OS 内核参数」为待验证项。**三个家族的引导参数各有一处极易出错的细节**，
本计划逐一标出并要求实机核对。

**技术栈：** `text/template`（preseed）、YAML 生成（autoinstall）；沿用 Phase 1 的
guard 校验器与 golden file 基础设施

**SPEC 依据：** §0.3、§2.1、§4.5、§5.2、附录 B

---

## 三个家族的差异总览

这张表是本计划的骨架，每一列差异对应后面的一到两个任务。

| | RHEL（已有） | Ubuntu（新） | Debian（新） |
|---|---|---|---|
| 应答文件 | kickstart，脚本式 | autoinstall，**YAML** | preseed，**debconf 键值** |
| 文件名 | `ks.cfg` | `user-data` **加 `meta-data`** | `preseed.cfg` |
| 引导参数 | `inst.ks=<url>` | `ds=nocloud-net;s=<url>` **必须是目录且结尾带斜杠** | `preseed/url=<url>` |
| 内核与 initrd 名 | `vmlinuz` / `initrd.img` | `vmlinuz` / `initrd` | `linux` / `initrd.gz` |
| 识别特征文件 | `.treeinfo` | `.disk/info` | `dists/` 目录 |
| pre 阶段 | `%pre` | `early-commands` | `preseed/early_command` |
| post-nochroot | `%post --nochroot` | `late-commands`（curtin 目标） | `preseed/late_command` |
| post | `%post` | `late-commands` 里走 in-target | `late_command` 加 chroot |
| firstboot | systemd oneshot | **cloud-init 的 runcmd** 或 systemd oneshot | systemd oneshot |
| 安装机制 | installer 驱动（anaconda） | **镜像式**（curtin 解包） | installer 驱动（debian-installer） |

**最后一行的差异会影响可观测性**（§6.5）：Ubuntu 的 curtin 是镜像式解包，
**大文件下完后有一段长时间静默是正常的** —— nginx access log 那条辅助通道对它很弱。
所以 Ubuntu 的阶段超时判定要格外依赖信号，而信号点的位置与 RHEL 不同（见任务 4）。

---

### 任务 1：渲染器的家族分派重构

**产出文件：** `internal/render/renderer.go`、`skeleton.go`（新）

**为什么需要这一步：** Phase 1 的渲染器只有一个真实分支，骨架生成与 kickstart 组装
是揉在一起的。加两个家族前先把共用部分抽出来，否则会变成三份复制粘贴。

**抽出为共用骨架的五样**（它们与应答文件语法无关）：

1. 参数内联与 fact 注入的**内容**（不含如何嵌入 —— 那随语法而变）
2. 客户端 shell 函数的正文
3. firstboot 脚手架要写入目标系统的文件清单
4. 凭据清理的动作清单
5. hook 阶段到「该阶段要执行哪些 digest」的解析

**不抽出的：如何把这五样嵌进应答文件。** 三种语法的嵌入方式本质不同：
kickstart 直接写 shell，preseed 要塞进 `late_command` 的一行里（且要处理转义），
autoinstall 要放进 YAML 的列表项。**强行抽一个统一的「嵌入」抽象会让三种都别扭** ——
这是本计划的第一条「不要过度设计」边界。

**设计要点：**

- **骨架生成器返回结构化的片段**（比如「这是要在 pre 阶段执行的 shell 行列表」），
  由各家族的渲染函数决定怎么放进自己的语法。这样共用的是内容，不是形式。
- **Phase 1 的 golden file 必须保持不变。** 重构后 RHEL 的输出应逐字节一致 ——
  这是重构正确性的唯一客观判据。若 golden 变了，先确认是重构引入的还是刻意的改动。

**测试要锁住的行为：**

- **RHEL 的三份 golden 逐字节不变**（重构的验收标准）
- 骨架生成器的输出含五样内容
- 三个家族的分派各自路由到正确的渲染函数

**验证：** `go test ./internal/render/ -v` 全过，**且 `git diff` 显示 testdata 无变化**。

**完成标志：** commit `refactor(render): extract family-agnostic skeleton generator`

---

### 任务 2：Debian preseed

**产出文件：** `internal/render/preseed.go`、golden file

**为什么需要这一步：** 先做 Debian 而不是 Ubuntu，因为 preseed 是 installer 驱动的
（与已有的 kickstart 同类），差异集中在语法而非机制。Ubuntu 的镜像式安装引入的
新问题更多，放后面。

**preseed 的语法要点：** 每行是「所有者 键 类型 值」四段的 debconf 预置项。
要生成的段落分五组：本地化、网络、镜像源、分区、账户与软件包。

**三处必须小心的细节：**

1. **`late_command` 是单行的。** 所有 post 阶段的 hook 调用要压进一行
   （用分号或 `&&` 连接），或者写成「先落一个脚本文件再执行它」——
   **推荐后者**：单行的可读性极差，且任何一个 hook 正文里有换行就会坏掉
2. **in-target 前缀**：在 `late_command` 里要对目标系统执行命令必须加 `in-target`，
   否则命令在 installer 环境里跑 —— 那是 §5.2 说的「阶段是能力边界」在 Debian 下的形式
3. **`preseed/url=` 在内核命令行上，值里带 `&` 会出事**（§4.4 已指出）。
   token 走路径段，这条 Phase 1 已经解决 —— **但要有测试确认生成的 URL 里没有 query**

**分区要用服务端决策的盘**（§5.6），设备标识用 by-id。preseed 的分区配方语法
（`partman-auto/disk`）接受设备路径。

**测试要锁住的行为：**

- 五组段落都生成，格式是四段式
- **post 阶段的 hook 走「落脚本再执行」而不是压进单行**
- **对目标系统的命令带 in-target 前缀**
- 生成的应答文件 URL 里**无 query string**
- 分区配方用 by-id 路径
- golden file 覆盖：无 hook 的最小 preseed、带三阶段 hook 的完整 preseed

**验证：** `go test ./internal/render/ -run TestPreseed -v` 全过。

**完成标志：** commit `feat(render): Debian preseed with in-target hook execution`

---

### 任务 3：Ubuntu autoinstall

**产出文件：** `internal/render/autoinstall.go`、golden file

**为什么需要这一步：** Ubuntu 是 Phase 2 的主要目标（它在国内机房的占有率高于 Debian）。

**autoinstall 是 YAML**，顶层有版本号与几个大键（本地化、身份、存储、网络、
早期命令、晚期命令、包）。

**四处必须小心的细节：**

1. **生成 YAML 要用序列化库，不要用文本模板。** 理由：YAML 对缩进敏感，
   而 hook 正文里的多行 shell 会破坏缩进。**用模板拼 YAML 是本任务最容易踩的坑** ——
   一个 hook 里的换行就能让整份文件语法错误，而 cloud-init 的报错信息很难定位到
   是哪个 hook
2. **`meta-data` 文件必须一起提供。** NoCloud 数据源要求那个目录下两个文件都存在 ——
   Phase 1 的渲染器接口已经为此留了「额外接受 `meta-data`」的口子。
   `meta-data` 的内容极简（实例 id 与主机名）
3. **`ds=nocloud-net;s=<url>` 的 URL 必须是目录且结尾带斜杠**（§4.4）。
   Phase 1 的 URL 拼接函数已保证保留结尾斜杠，**本任务要有测试确认菜单里那个 URL
   确实带斜杠** —— 少一个斜杠的现场表现是「机器装到一半停住」，极难定位
4. **早期与晚期命令是列表，每项是一条命令。** 对目标系统执行要用
   `curtin in-target --` 前缀

**firstboot 的两个选择：** cloud-init 的 `runcmd` 或 systemd oneshot。
**选 systemd oneshot，与 RHEL 保持一致** —— 理由是 Phase 1 的 firstboot 脚手架
（标记文件加条件启动，§5.8）已经验证过，且它的「只跑一次且可人工重挂」语义
比 `runcmd` 清楚。**共用一套脚手架也意味着共用一套排查经验。**

**测试要锁住的行为：**

- 输出是合法 YAML（**用 YAML 解析器解一遍**，不要只对比文本）
- **含多行 shell 的 hook 不破坏 YAML 结构** —— 这条是对第 1 点的直接验证
- `meta-data` 也能渲染，内容含实例 id 与主机名
- **菜单里的数据源 URL 结尾带斜杠**
- 对目标系统的命令带 in-target 前缀
- firstboot 走 systemd oneshot（与 RHEL 同一套脚手架）
- golden file 两份

**验证：** `go test ./internal/render/ -run TestAutoinstall -v` 全过。

**完成标志：** commit `feat(render): Ubuntu autoinstall via YAML serialization`

---

### 任务 4：信号点的家族适配

**产出文件：** `internal/render/skeleton.go`（追加）、对应测试

**为什么需要这一步：** §6.1 的信号点是按 kickstart 的阶段划分的。
Ubuntu 的镜像式安装（curtin 解包）没有「装包」这个阶段 —— 硬套会产出一个永不发出的
`packages-start`，而它的后果是**阶段超时判定永远停在 booting**。

**三个家族的信号点映射：**

| 信号 | RHEL | Debian | Ubuntu |
|---|---|---|---|
| pre-start / pre-done | `%pre` 首尾 | `early_command` 首尾 | `early-commands` 首尾 |
| packages-start | `%packages` 前 | 安装基础系统前 | **解包前**（curtin 阶段） |
| packages-done | `%packages` 后 | 安装基础系统后 | **解包后** |
| post-start / post-done | `%post` 首尾 | `late_command` 首尾 | `late-commands` 首尾 |
| rebooting | `%post` 末尾 | `late_command` 末尾 | `late-commands` 末尾 |

**Ubuntu 的 packages 两个信号只能近似。** curtin 的解包过程不提供钩子，
所以这两个信号实际是「晚期命令开始前」与「晚期命令里第一条」——
**这意味着 Ubuntu 的 packages 阶段耗时统计不准**。

**这个不准要不要处理？不处理，但要写进注释。** 理由：那个耗时统计的用途是
分阶段超时判定，而 Ubuntu 的整体安装时长与 RHEL 同量级，60 分钟的 packages 预算
对它同样适用。**为了统计精度去改超时预算表是不值得的** —— 预算的作用是
「卡住了要能报出来」，不是精确计量。

**测试要锁住的行为：**

- 三个家族各自生成全部信号点，**且信号名都在 M1 的信号表里**
- Ubuntu 不生成永不发出的信号（**遍历生成的文本，确认每个信号名都实际出现**）
- 三个家族的 `rebooting` 都在最后一个阶段的末尾

**验证：** `go test ./internal/render/ -run TestSignalPoints -v` 全过。

**完成标志：** commit `feat(render): per-family signal point mapping`

---

### 任务 5：导入 job 支持两个新家族

**产出文件：** `internal/imports/extract_debian.go`、`extract_ubuntu.go`、对应测试

**为什么需要这一步：** 两个家族的 ISO 结构与 RHEL 不同，抽取逻辑要各写一份。

**Debian 的抽取：** 内核与 initrd 在 installer 目录下（按架构分子目录），
名字是 `linux` 与 `initrd.gz`。仓库树是 `dists/` 与 `pool/` 两棵。

**Ubuntu 的抽取：** 内核与 initrd 在 casper 目录下。**Ubuntu 的「仓库」不是 deb 仓库
而是一个 squashfs 镜像** —— curtin 解包它。所以抽取要把那个镜像文件也搬到制品目录，
并在路径结构的额外文件里记下它的位置。

**识别顺序的歧义要处理**（M1 的识别任务已提到）：**Ubuntu 的 ISO 也含 `dists/`**，
所以 Ubuntu 的判定必须在 Debian 之前。Phase 1 已实现这个顺序，本任务要有测试确认
加入 Ubuntu 的真实特征后顺序仍正确。

**制品完整性校验要覆盖新的必需文件**（§4.6 第三项）：Ubuntu 少了 squashfs 镜像
就装不了，而那个缺失在 Phase 1 的校验逻辑里不会被发现（它只查内核、initrd、仓库）。
**路径结构的额外文件也要纳入存在性检查** —— Phase 1 的 precheck 已经遍历额外文件，
确认这条仍然生效。

**测试要锁住的行为：**

- 两个家族的抽取各自产出正确的路径结构
- **Ubuntu 的 squashfs 镜像被记入额外文件且被完整性校验覆盖**
- **同时含 Debian 与 Ubuntu 特征时识别为 Ubuntu**
- 缺关键文件时导入置失败且原因具体

**验证：** `go test ./internal/imports/ -v` 全过。

**完成标志：** commit `feat(imports): Debian and Ubuntu artifact extraction`

---

### 任务 6：引导菜单与实机核对清单

**产出文件：** `internal/render/menu.go`（补两个分支）、
`deploy/verified-phase2-<日期>.md`

**为什么需要这一步：** 菜单是引导链的最后一环，也是本计划风险最集中的地方 ——
SPEC 附录 B 明确把各 OS 内核参数列为待验证项。

**两个新菜单的参数：**

- **Debian**：内核行带自动应答开关、优先级设置与应答文件地址，加 initrd 行
- **Ubuntu**：内核行带 autoinstall 开关与数据源地址（**结尾带斜杠**），加 initrd 行

**两处易错点已在前面标出**（Ubuntu 的斜杠、Debian 的 query），这里再加一条：
**Ubuntu 的内核参数里有一个三连横线的分隔符**，它之后的参数传给目标系统而非 installer。
位置写错的后果是参数被错误的一方消费。

**实机核对清单**（写成带勾选框的文件，与 Phase 1 的核对清单同样性质 ——
核完即固定）：

- [ ] Debian 的应答文件地址参数能被 debian-installer 正确取用
- [ ] Debian 的 `late_command` 里 in-target 前缀生效（装完后目标系统里有预期的文件）
- [ ] **Ubuntu 的数据源 URL 少一个斜杠时确实会失败** —— 主动验证一次，
      确认这个陷阱真实存在且我们的防护有效
- [ ] Ubuntu 的三连横线分隔符位置正确
- [ ] Ubuntu 的 squashfs 能被 curtin 取到
- [ ] 三个家族的 hook 在各自阶段都实际执行（用一个写标记文件的 hook 验证）
- [ ] 三个家族的信号都能收到，任务走到 done

**倒数第二条要用同一个 hook 在三个家族上跑** —— 这是「复用同一套 hook 库」这个
设计目标的唯一真实验证。

**验证：** 在实验环境各装一台 Ubuntu 与 Debian，核对清单逐项勾完。

**完成标志：** commit `feat(render): Ubuntu and Debian boot menus, plus verification record`

---

## 交付验收

- [ ] 三个家族的应答文件与菜单各有 golden file 覆盖
- [ ] **RHEL 的 golden 在重构后逐字节未变**
- [ ] Ubuntu 的输出用 YAML 解析器验证过，含多行 hook 时仍合法
- [ ] Ubuntu 的数据源 URL 结尾带斜杠（单独测试）
- [ ] Debian 的应答文件 URL 无 query string
- [ ] 三个家族的信号点都在 M1 的信号表内，且无永不发出的信号
- [ ] 两个新家族的导入与完整性校验覆盖各自的必需文件
- [ ] 实机核对清单逐项勾完并记录
- [ ] **同一个 hook 在三个家族上都能执行**（复用目标的验证）
- [ ] `make check` 全绿

## 边界提醒

**不要为了对称而抽象。** 三种应答文件格式的差异是本质的 ——
kickstart 是脚本、autoinstall 是 YAML、preseed 是 debconf 键值。
共用的只有 hook 阶段映射、参数内联、fact 注入、firstboot 脚手架这几样与语法无关的东西。
**若发现自己在写一个「通用应答文件抽象层」，停下来** —— 那一层的每个分支判断
都是三种格式在打架，最终会比三份独立实现更难维护。

