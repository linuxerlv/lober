# Debian 端到端验证报告

对应分支 `feat/t04-e2e-deb`。验证方式：`scripts/e2e-debian.sh`（curl 模拟机器
打点，本地跑通全链路，不碰硬件），结构照抄 `scripts/e2e.sh`（RHEL 基线，
本轮未修改它一个字符）。已连续跑两次，结果稳定：九个阶段（含四条准入负面
用例）全部 PASS。

## 一句话结论

**Debian 的渲染层单元测试全绿，但装机链路目前会在真机上 100% 卡死在
`/v1/boot` 这一步**——不是偶发，是无条件必然：`internal/statemachine/
precheck.go` 用来判定"制品仓库真正可用"的硬编码文件名
（`repodata/repomd.xml`）是 RHEL/yum 专属产物，Debian 的仓库树从不会有这
个文件。这条检查挡在签发装机 token 之前，因此**任何** Debian 任务在机器
真正引导时都会被判定为"制品文件不存在"而拒绝，与该制品集导入得多完整、
多正确无关。这是本次验证发现的最严重问题，写在最前面。

## 环境与方法

本地起服务（`go build ./cmd/lober` 后 `-config ... -check` 再直接跑），
`curl` 模拟 iPXE 客户端与真实机器发出的 HTTP 请求，`sqlite3` CLI 直接查库
核对状态与断言依据（不依赖 jq，本机没有）。假 Debian ISO 内容树由新建的
`scripts/e2e/mkfixture-debian.sh` 生成：`install.amd/{vmlinuz,initrd.gz}` +
`dists/stable/{Release,main/binary-amd64/Packages}` +
`pool/main/b/bash/bash_5.2-5_amd64.deb`——这是 `internal/imports/
identify.go`（`dists/` 目录即识别为 Debian）与 `extract_debian_test.go` 的
`debianFakeRoot()` 同款最小夹具，不是凭空编的目录形状。

## 真实发现（按严重程度排序）

### 发现 1（阻断性 bug）：precheck 的仓库完整性检查硬编码了 RHEL 专属文件名

**位置**：`internal/statemachine/precheck.go` 第 104 行
`const repoMetadataFile = "repodata/repomd.xml"`，被 `checkPathExists`
（第 112-121 行）用于判定任何结尾带斜杠的 `ArtifactPaths` 字段（当前只有
`Repo` 会触发）是否"真正可用"——不管 OS 家族，一律拼 `<repo目录>/
repodata/repomd.xml` 去 `fs.Stat`。同一常量与逻辑在
`cmd/lober/preflight.go` 第 137 行原样复制了一份（启动自检用，只打警告
日志不阻断启动）。

**复现步骤**（本次验证脚本实测，未加任何绕过时）：
1. 用 `mkfixture-debian.sh` 造真实的 Debian ISO 内容树（`install.amd/` +
   `dists/` + `pool/`）
2. 手动按 `ExtractDebian` 的产出结构搬运到制品集目录：`linux` +
   `initrd.gz` + `repo/dists/` + `repo/pool/`（与 `internal/imports/
   extract_debian.go` 的 `ExtractDebian` 函数产出逐字节一致的目录形状，
   只是搬运动作用 shell 代替了 Go 抽取器——因为导入 job 没有接线，见发现 3）
3. `paths_json` 写 `{"Kernel":"linux","Initrd":["initrd.gz"],"Repo":"repo/",...}`，
   `state='ready'`——与 `docs/contracts/filesystem.md` 硬约束 1 完全一致，
   磁盘上的文件真实存在
4. 机器发 `GET /v1/boot?sn=...&mac=...&arch=x86_64&plat=efi` 命中该任务
5. `internal/statemachine`（`Machine.Check` → `checkArtifactFiles` →
   `checkPathExists("repo/")`）尝试 `fs.Stat("repo/repodata/repomd.xml")`
   ——这个文件从未存在（Debian 用 `dists/`+`pool/`，不用 yum repodata）
6. 任务被状态机置为 `failed`，`failure_detail.LogTail` 逐字符为：
   ```
   制品文件 "repo/" 在盘上不存在: GetFileAttributesEx repo/repodata/repomd.xml: The system cannot find the path specified.
   ```
7. 机器收到 `sanboot`（no_task 载荷），永远拿不到装机菜单

**影响范围核对**（读代码逐一确认，不是猜测）：
- **RHEL**：`ExtractRHEL` 产出的 `repo/` 下真的有 `repodata/repomd.xml`
  （yum 仓库真实结构）——precheck 对 RHEL 是对的，这也是它为什么在
  `scripts/e2e.sh` 里从未暴露过。
- **Ubuntu**：`internal/imports/extract_ubuntu.go` 的 `ExtractUbuntu` 从
  未设置 `ArtifactPaths.Repo`（该字段保持零值空字符串，只用
  `Extra["squashfs"]` 承载镜像路径）；`artifactCheckPaths`/
  `checkArtifactFiles` 对空字符串直接跳过（`if p == "" { continue }`）
  ——Ubuntu 不会撞上这条硬编码，但这是"因为它的 Repo 字段本来就是空的"
  这个偶然事实，不是这条检查本身家族感知。
- **Debian**：`ExtractDebian` 产出 `Repo: "repo/"`（非空、带结尾斜杠）
  ——precheck 对每一个 Debian 任务都会执行这条注定失败的检查。
- **SUSE**：目前完全未实现抽取，若未来实现时 `Repo` 字段指向非 yum 树
  （AutoYaST/zypper 的仓库元数据文件名不是 `repodata/repomd.xml`），会
  撞上同一个问题。

**正解方向**：`repoMetadataFile` 需要按 `model.OSFamily` 分支（或者
`model.ArtifactPaths`/`model.ArtifactSet` 携带一个"仓库完整性判据"字段，
由抽取器按家族填入，precheck 不再自己猜文件名）。两处硬编码
（`precheck.go` 与 `preflight.go`）需要同步修，否则会出现"运行期拒绝了但
启动自检没警告"或反过来的不一致。这两个文件都不在本轮任务的可写文件列表
内（`internal/statemachine/**`、`cmd/lober/**` 都不归 t04 轨），故本报告
只记录发现，不动手修。

**本次验证的应对方式**：为了不让这一个阻断性 bug 挡住阶段 7-9（菜单/
preseed 内容/信号机制）本应验证的内容，`scripts/e2e-debian.sh` 在阶段 4
里手动补了一个 `repo/repodata/repomd.xml` 占位文件，让 precheck 误判为
"存在"从而放行——这是一处**为绕过已确认 bug 而加的诊断性 workaround**，
脚本里用大段注释明确标注了这一点，不代表真实 Debian ISO 会有这个文件。
删掉这三行、直接继续跑，就是该 bug 未修复时的真实生产行为：Debian 装机
100% 卡在 precheck。

### 发现 2：`AssemblePreseed` 从未渲染 firstboot 阶段、也从未安装重启后触发机制

**位置**：`internal/render/preseed.go`。

对 `StageHookCallLines` 的调用只有三处（`earlyCommandValue` 用
`HookStagePre`，`lateCommandValue` 用 `HookStagePostNoChroot` 与
`HookStagePost`），全文件搜索 `model.HookStageFirstboot` 零匹配。对比
`FirstbootScaffold`（`internal/render/firstboot.go`，负责在目标系统里写
`systemd oneshot` unit 并 enable 它，真正执行的载体）的调用点：只有
`kickstart.go`（RHEL）与 `autoinstall.go`（Ubuntu）两处引用它，
`preseed.go`（Debian）里零次引用。

**后果**（两层）：
1. 挂在 `firstboot` 阶段的用户 hook，对 Debian 任务会被**静默丢弃**——不
   报错、不执行、渲染出的 preseed.cfg 里根本不含这个 hook 的任何痕迹。
   建模板时选了一个 firstboot hook 挂给 Debian 家族，运维会以为它会跑，
   实际永远不会跑，且没有任何报错提示这一点。
2. 更根本的：`late_command` 的脚本以 `signal "rebooting" 0` 结尾
   （已在本次验证阶段 8a 解码确认这三个信号点真实存在：`post-start`/
   `post-done`/`rebooting`），之后没有任何东西在目标系统里落地"重启后要
   跑什么"这件事本身——没有 systemd unit、没有 `booted`/`init-start`/
   `init-done`/`ready` 四个信号的发送逻辑，也没有 finalize token 文件的
   落盘动作（这些都是 `FirstbootScaffold` 的职责）。

本次验证脚本的阶段 8c 仍然打了这四个信号，但已在脚本输出与本报告里明确
标注：那只验证了"网关的信号端点能用 finalize token 认证并接受这四个
phase 值"（家族无关的通用能力，已被 RHEL `scripts/e2e.sh` 覆盖过一次），
**不代表验证了"Debian 装完的机器会自动发出这些信号"**——按当前渲染器的
产出，真机重启后什么都不会发生，任务会永远卡在 `installing`（除非运维
手动 curl 四个信号，那已经不是"自动装机"）。

**正解方向**：`preseed.go` 需要在合适的位置（`late_command` 脚本正文里，
`post` 阶段 hook 之后、`rebooting` 信号之前）调用 `FirstbootScaffold(ctx)`
并把其产出（写 unit 文件、写 finalize token 文件等一系列 `%post` 风格的
落盘动作）转换成 preseed 的 `in-target` 命令形式——这需要类似 `postLines`
的处理方式（整体包进 `in-target bash -c '...'`，因为 `FirstbootScaffold`
产出的是假定已在 chroot 内执行的文本）。`FirstbootScaffold` 本身按读代码
确认是家族无关的纯函数（接收同一个 `model.RenderContext`，不分支判断
`OSFamily`），理论上可以直接复用，不需要另写一份 Debian 专属版本。

`internal/render/**` 不在本轮任务的可写文件列表内（归其它家族轨与
t08-dispatch），本报告只记录发现。

### 发现 3：制品集登记 → 导入 job 之间确实没有接线（与 RHEL 已知缺口相同，验证了"未接线"这一结论本身）

任务描述要求本脚本**先尝试真实的登记→导入→ready 路径**，走不通再退回
`sqlite3` 绕法，且要在报告里写明为什么走不通。以下是验证过程与结论：

- `internal/console/artifact.go` 的 `handleCreateArtifactSet`
  （第 86-112 行）只构造 `model.ArtifactSet{State: model.
  ArtifactSetImporting}` 并调用 `CreateArtifactSet` 落库，函数体内从未
  出现 `internal/imports` 包的任何符号引用——对该文件 grep
  `imports\.` 零匹配。
- `internal/imports/job.go` 的 `Job` 类型只有 `RunRHEL` 一个"真正执行
  抽取"的入口（另有 `RunUnsupported` 处理"识别出但不支持"/"识别不出"两类
  场景，但同样不执行任何家族特定抽取）。**没有 `RunDebian`，也没有按
  家族分派的通用 `Run` 方法**——即使 `handleCreateArtifactSet` 接上了
  `imports.Job`，也没有入口可调用来跑 Debian 的抽取。
- 对整个仓库（含 `cmd/lober` 装配点）grep `"imports\."`，只命中
  `internal/imports` 包自己的测试文件与 `extract_windows.go`/
  `extract_esxi.go`（它们是被同包内其它逻辑引用，不是被外部调用）——
  `cmd/lober` 从未 `import` `internal/imports` 包，这条链路在 HTTP 层
  完全不可达。

**脚本内的自动化验证**：阶段 4 在用真实端点拿到 `importing` 状态的制品
集 ID 后，轮询 3 秒观察 `state` 是否会自行推进——若某次编排把这条链路
接上了，这里会检测到并主动 `fail` 提示"判断已过时，需要人工复核"，而不是
静默让绕法失效却装作成功。两次实际运行，state 在等待期间始终原样保持
`importing`，与读代码分析一致。

**结论**：确实走不通，退回与 `scripts/e2e.sh` 相同的绕法——用真实端点
落一条 `importing` 记录取得真实分配的 ID，再用 `sqlite3` CLI 推进到
`ready` 并写回 `paths_json`。文件本身仍是 `mkfixture-debian.sh` 造出的
真实文件、真实落在制品根目录下；绕过的只是"谁调用抽取逻辑"这个缺失的
调用点，不是伪造磁盘内容本身。这与 RHEL 侧已记录的缺口是同一个缺口，
本次验证只是在 Debian 家族上又确认了一遍，且额外确认了"Job 没有
`RunDebian` 入口"这一具体缺失点（RHEL 侧的报告没有提到这一点，因为
RHEL 恰好是唯一有对应入口的家族）。

### 发现 4：渲染出的十一信号点中，`packages-start`/`packages-done` 两个 Debian 未生成

`internal/render/preseed.go` 通篇搜索 `packages-start`/`packages-done`
零匹配。装机期七个信号里，`AssemblePreseed` 实际生成的只有五个：
`pre-start`/`pre-done`（`early_command`）、`post-start`/`post-done`/
`rebooting`（`late_command`）——已在本次验证阶段 8a 解码 `late_command`
的 base64 载荷、直读 `early_command` 明文后逐一确认这五个信号点真实存在
于渲染出的 preseed.cfg 里。

本次验证脚本的阶段 8b 仍然对全部七个装机期 phase 值打点（包括
`packages-start`/`packages-done`），但这验证的是"网关侧信号端点能否
接受这两个 phase 值"（家族无关的通用协议能力），**不代表"确认了 Debian
渲染器会生成这两个信号点"**——它们确实不会。真实机器上，Debian 装机
在 d-i 的软件包安装阶段不会有任何东西上报这两个信号，控制台的任务
时间线在这个区间会是空白（不影响任务最终能否推进到 `done`，因为
状态机对这两个信号点没有强制要求，但会影响运维通过时间线判断"装机
卡在哪一步"这个可观测性目标）。

**正解方向**：若十一信号协议要求每个家族都生成全部信号点，`preseed.go`
需要在 `late_command` 脚本的 hook 调用前后（`packages` 概念在 d-i 里
对应"从仓库拉包并安装"这一段，没有独立的 debconf 钩子可挂）补上这两个
信号——可能需要包在 `pkgsel/include`/`pkgsel/upgrade` 一类 debconf 键
对应的动作附近，或者接受"Debian 就是没有这两个信号点"作为家族差异并
更新协议文档，两种都是设计决策，不是本报告该下结论的范围。

## 九个阶段逐一核对（家族特有点）

| 阶段 | 内容 | Debian 特有断言 |
|---|---|---|
| 1 | 造 ISO 内容树、二进制、admin、配置 | 用新建的 `mkfixture-debian.sh`，`dists/` 特征目录 |
| 2 | 起服务 | 与家族无关 |
| 3 | 登录 | 与家族无关 |
| 4 | 主机+制品集+模板 | 先验证真实导入路径走不通（发现 3），退回绕法；补 precheck workaround（发现 1） |
| 5 | preview 渲染 | 与家族无关（渲染器纯函数） |
| 6 | 四条准入负面用例 | 全部通过，与家族无关（状态机逻辑不分家族） |
| 7 | `/v1/boot` 命中签 token | 断言菜单形状为 `kernel .../linux auto=true priority=critical` + `preseed/url=`（路径段不带 `?`/`&`） + `initrd .../initrd.gz`，与 RHEL 的 `vmlinuz`/`inst.ks=`/`initrd.img` 逐一核对不同 |
| 8 | 取应答文件 + 信号 | 断言 `early_command`/`late_command` 两键非空；解码 `late_command` 的 base64 载荷核对 hook/信号/SSH 公钥注入命令真实存在（发现 2、4） |
| 9 | 终态 + 防循环重装 | 与家族无关 |

四条安全负面用例（§9.1 三道闸门 + §10 失败模式 1/2b）逐一确认：
- 陌生机器 → `no_task`，响应体逐字符含三行 `sanboot` 脚本：**通过**
- SN 对但 MAC 不对 → 拒签 token，任务 `failureReason=precheck`：**通过**
- 架构不符 → 硬拒绝，且允许后续 `retry`（验证"不消耗 attempt_count 的
  必然后果是重试被允许，不是拒绝重试"这条语义）：**通过**
- 装完的机器再次引导 → `sanboot` 而非装机菜单：**通过**

## 本次验证未覆盖的部分

- syslog 通道（UDP，curl 发不出，同 `scripts/e2e.sh` 的已知限制）。
- 真实 debian-installer 是否真的能解析本次渲染出的 preseed.cfg 语法
  （本脚本只做 HTTP 层与内容结构断言，不起真实 QEMU 虚拟机跑一次真装机
  ——那是另一类验证工作，本轮任务范围是"整条链路本地跑通"而非"真机
  d-i 兼容性"）。
- 发现 1 的 workaround 掩盖了"真实 Debian 装机会在这一步卡死"这一事实，
  阶段 7-9 的 PASS 只代表"precheck 放行之后的链路是通的"，不代表"precheck
  本身对 Debian 是对的"——这一点在本报告开头与脚本输出里都已反复强调，
  避免被误读为"Debian 端到端已经跑通可以上线"。

## 交付文件

- `scripts/e2e-debian.sh`（新建，可重复执行，已连续验证两次）
- `scripts/e2e/mkfixture-debian.sh`（新建）
- 本报告
