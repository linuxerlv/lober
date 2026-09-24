export const meta = {
  name: 'lober-close-assembly-gaps',
  description: 'Close the three gaps that need assembly-point changes: ESXi boot.cfg never rendered, Windows external sources have no resolver, MountExtractor is a stub',
  phases: [{ title: 'Wire', detail: '三轨并行：ESXi boot.cfg 接线、Windows 外部文件解析器、真实 ISO 挂载抽取器' }],
}

const BASE = `你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：__WT__
  分支：    feat/__ID__

**每个文件操作、每条命令都在你的 worktree 里**（绝对路径，或先 \`cd __WT__\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 有 3 条轨在并行。

## 仓库现状

\`dev\` 分支：**17 个 Go 包 \`go test ./...\` 全绿**，\`gofmt\` 干净，\`go vet\` 无输出。

六个 OS 家族的渲染与分派都已接线（rhel / debian / ubuntu / suse / windows / esxi）。
五条端到端脚本 exit 0：\`e2e.sh\`(18 PASS)、\`e2e-debian.sh\`(18)、\`e2e-ubuntu.sh\`(20)、
\`e2e-windows.sh\`(19)、\`e2e-esxi.sh\`(20)。另有 \`e2e-suse.sh\` 正由另一轨补完阶段 6-9
（**那条脚本归那一轨，你绝不要碰它**）。

**范围决定（2026-09-23）**：机器面永久保持明文 HTTP，HTTPS 只用于浏览器 ↔ 控制台。
机器面 TLS 与 Secure Boot 已放弃，iPXE 不重编译。见 \`docs/tls-与安全启动-范围决定.md\`。

**你的改动不许打破这个状态。** 五条端到端脚本必须仍然全过。

## 铁律

1. **只写你拥有的文件**（下面明确列出）。三条轨并行，碰别人的文件就制造合并冲突。
2. **契约只读**：禁止修改 \`internal/core/contract/**\`、\`internal/core/model/**\`、
   \`internal/apierr/**\`、\`internal/archtest/**\`、\`internal/store/sqlite/schema/**\`、
   \`docs/contracts/**\`、\`Makefile\`、\`go.mod\`、\`go.sum\`（除非你的任务明确授权）。
   发现契约缺口时不要加变通：在 contractGaps 里写清「缺什么、你怎么保守绕的、
   正解要改哪个接口」。
3. **不要修改别的模块的既有行为。** 你是做增量。若必须改既有函数，改动要最小且
   **不得让任何现存测试变红**。
4. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 \`t.TempDir()\` 或写在你 worktree 的
   \`.tmp/\` 下，用完只删你自己建的那个具体文件。**绝不创建与真实模块同名的临时目录。**
5. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
6. **import 白名单**：model、contract、标准库，加上你的任务明确允许的。
   **实现模块之间绝不互相 import**（store/statemachine/render/gateway/console/provider/imports
   互不可见）—— archtest 会拦住，**唯一豁免是 cmd/lober 装配点**。
7. 先写测试再写实现。断言失败信息写明「为什么这条重要」加 SPEC 章节号。
8. Bash 的 PATH 快照不含某些目录：Node 用 \`export PATH="/c/Program Files/nodejs:$PATH"\`；
   pwsh 用绝对路径 \`/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe\`
   （**禁止 powershell 5.1**：输出 GB2312，中文全乱码连错误信息都乱，无法判断成败）。
   **本机没有 jq**（curl 与 sqlite3 有）。
9. **完成前必须真的执行验证并看到通过输出。不要声称通过。**
   **不要把「我读了代码觉得没问题」当成验证。**
10. 中文注释、文档与断言信息；标识符与文件名用英文。gofmt 干净。
11. 你的返回值是数据，不是给人看的汇报。

## 验证与提交

必跑：\`go vet <你的包>\`、\`go test <你的包> -v -count=1\`、\`go test <你的包> -race\`、
\`gofmt -l <你的目录>\`、**\`go test ./... -count=1\` 全仓**、以及**受影响的 e2e 脚本**。

全绿后 commit。**只 add 你自己的文件**，提交前 \`git status\` 确认没夹带别人的东西。

`

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    commits: { type: 'array', items: { type: 'string' } },
    verifyCommands: { type: 'array', items: { type: 'string' } },
    verifyOutcome: { type: 'string' },
    findings: { type: 'array', items: { type: 'string' } },
    contractGaps: { type: 'array', items: { type: 'string' } },
    unfinished: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'findings', 'contractGaps', 'unfinished', 'notes'],
}

const mk = (id) => BASE.split('__WT__').join('E:/src/lober-wt/' + id).split('__ID__').join(id)

phase('Wire')

const out = await parallel([
  // ── m1：ESXi 的 boot.cfg 真的被渲染 ──
  () => agent(mk('m1-esxi-bootcfg-wire') + `## 你的任务：让 ESXi 真的下发引导参数文件，而不是把 kickstart 正文当 boot.cfg

### 这是真实部署必然失败的缺口，两轮端到端都坐实了

\`internal/render/esxi.go\` 的 \`RenderESXiBootCfg(template, bootCfgURLBase, answerFileURL)\`
负责改写引导参数文件，**三处替换**：
- \`prefix=\` 改成 HTTP 路径
- \`kernelopt=ks=\` 填应答文件地址
- **模块路径前的 \`/\` 去掉**（这条最容易漏，漏了安装器找不到模块）

**它在生产代码里零调用点**（唯一调用在 \`esxi_test.go\`）。上一轮已经做了一半：
\`renderer.go\` 的分派现在对 \`fileName == "ks.cfg"\` 正确调用 \`AssembleESXiKickstart\`，
\`gateway/answer.go\` 也放行了 ESXi 的 \`ks.cfg\`（与 Ubuntu 的 \`meta-data\` 同构）。
**但 \`boot.cfg\` 那条路径仍然返回 kickstart 正文。**

真实部署下 \`mboot.efi\` 会把那段 kickstart 当引导参数文件解析，**失败发生在引导器
读引导参数这一步，比「装到一半停住」更早**，而且现场只看到引导器报错、看不出根因。

### 上一轮诊断出的根因链（已经查证过，你不必重查，但要读代码确认）

\`RenderESXiBootCfg\` 需要「导入时保存的原始 boot.cfg 模板**文本内容**」作为输入。
而渲染器能拿到的只有 \`model.RenderContext.Artifact.Paths.Extra["boot.cfg"]\`，
那**只是磁盘相对路径字符串**（值就是字面 \`"boot.cfg"\`），不是文件内容。

渲染器包的文件头注释明确要求**零 IO、纯函数**（preview 端点与 golden 测试都依赖
这个前提），不能在 \`RenderAnswerFile\` 内部插入磁盘读取。

上一轮给出的正解方向：**把读盘放在 gateway 层**（它本来就做 IO）。需要两处改动：
1. \`internal/gateway/server.go\` 的 \`Deps\` 加一个「制品根目录」字段
2. \`cmd/lober/wire.go\` 构造 \`gateway.Deps{...}\` 时把 \`cfg.Store.ArtifactPath\` 传进去
   （那个值在 wire.go 里确实存在、已被 importRunner 与 assemblyPrechecker 使用，
   只是从没传进 gateway.Deps）

有了这两处，\`handleAnswer\` 在 \`fileName == "boot.cfg"\` 时可以：
\`filepath.Join(deps.ArtifactRoot, renderCtx.Artifact.ID, renderCtx.Artifact.Paths.Extra["boot.cfg"])\`
读出原始模板，算出 \`bootCfgURLBase\` 与 \`answerFileURL\`，再调 \`render.RenderESXiBootCfg\`。

### 你拥有的文件（只有这些）

  internal/gateway/server.go（**授权加 Deps 字段**，不改既有字段）
  internal/gateway/answer.go 与 answer_test.go
  internal/gateway/esxi_bootcfg.go（新建，若你想把读盘与拼装逻辑单独放一个文件）
  cmd/lober/wire.go（接线，只允许加）
  cmd/lober/wire_test.go

**不要碰** \`internal/render/**\`（渲染器保持纯函数，\`RenderESXiBootCfg\` 的签名不动）、
\`internal/imports/**\`、\`internal/console/**\`、\`scripts/**\`、\`deploy/**\`。

### 几处要自己判断并论证的

- **\`answerFileURL\` 与 \`bootCfgURLBase\` 怎么算**：前者是 \`ks.cfg\` 的完整 URL
  （\`/v1/answer/<task>/<token>/ks.cfg\`），后者是制品的 HTTP 基址前缀。
  \`internal/render/menu.go\` 的 \`renderESXiMenu\` 已经算过类似的东西 ——
  **读它，保持拼法一致**（它现在用 \`ctx.SetArtifactURL\`，那个方法会补上制品集 ID
  那一层目录）。不一致的后果是菜单指向一个地址、boot.cfg 里的 prefix 指向另一个。
- **读盘失败怎么办**：文件缺失、权限不足、内容为空。按契约文档
  \`docs/contracts/http-machine.md\` 端点 3 的错误行为处理（渲染出错 → 500 + 简短纯文本
  + 一条 level=error 的 TaskEvent）。**不要静默返回 kickstart 正文兜底** ——
  那正是现在的错误行为。
- **\`ArtifactRoot\` 为空时的降级**：个别测试或部署形态可能不注入它。
  参考 \`importRunner\` 为 nil 时的处理方式（读 \`cmd/lober/importrunner.go\`），
  选一个明确的行为并论证 —— 但**绝不能降级成「返回 kickstart 正文」**。

### 测试要锁住

- **取 \`boot.cfg\` 拿到的是引导参数文件**：含 \`prefix=\` 与 \`kernelopt=\` 行，
  **不含 kickstart 指令**（比如 \`install \`、\`rootpw\`）
- **三处改写都正确**：\`prefix=\` 是 HTTP 路径、\`kernelopt=ks=\` 指向 \`ks.cfg\` 的完整 URL、
  **模块路径前没有前导 \`/\`**
- **\`prefix=\` 与菜单里的制品 URL 指向同一个基址**（含制品集 ID 那一层）
- 取 \`ks.cfg\` 仍然拿到 kickstart 正文（不要改坏上一轮的修复）
- 原始模板文件缺失时返回 500 并记 TaskEvent，**不返回 kickstart 正文**
- 非 ESXi 家族不受影响

**必跑 \`bash scripts/e2e-esxi.sh\`**。它当前阶段 8 可能断言的是「取到 kickstart 正文」
这个错误行为（用 \`finding()\` 记录而非硬失败）—— 修好之后那条说明就过期了。
但 \`scripts/\` **不属于你**：在 findings 里写清哪一行需要改成什么，由编排方接线。`,
    { label: 'm1:esxi-bootcfg-wire', phase: 'Wire', schema: R, effort: 'high' }),

  // ── m2：Windows 外部文件解析器 ──
  () => agent(mk('m2-win-sources') + `## 你的任务：给 Windows 的四个外部文件做生产解析器，让导入真的能跑通

### 当前状态

\`internal/imports/job.go\` 的 \`Job.ConfigureWindows(resolver, detector)\` 已经存在
（上一轮 n2 轨新增），但**生产代码里零调用点** —— 所以导入 Windows 时
\`runWindows\` 会明确报错：「识别到家族 windows，但导入 job 尚未调用 ConfigureWindows
完成配置」。这是刻意的设计（不静默成功、不 panic），但意味着 **Windows 的导入路径
实际走不通**。

缺的两个依赖：
1. **\`WindowsSourcesResolver\`** —— 按制品集 ID 返回
   \`WindowsExternalSources\`（wimboot / BCD / boot.sdi / boot.wim 四个文件的本地路径）。
   这四个文件**不在 ISO 里**，是运维单独准备的（读 \`extract_windows.go\` 的
   \`WindowsExternalSources\` 文档确认它期望什么）。
2. **\`WimArchDetector\`** —— 探测 \`boot.wim\` 实际的固件位宽。
   \`extract_windows.go\` 文件末尾记录了「真实 WIM 格式解析」是已知缺口。

### 你拥有的文件（只有这些）

  cmd/lober/winsources.go（新建：WindowsSourcesResolver 的生产实现）
  cmd/lober/winsources_test.go
  cmd/lober/wimarch.go（新建：WimArchDetector 的生产实现）
  cmd/lober/wimarch_test.go
  cmd/lober/importrunner.go（接线：构造 Job 后调 ConfigureWindows）
  cmd/lober/importrunner_test.go
  cmd/lober/wire.go（若需要传配置，只允许加）
  internal/config/config.go 与 config_test.go（**授权你加 Windows 外部文件目录的配置项**）
  deploy/lober.yaml.example
  deploy/README.md（说明运维要把那四个文件放哪）

**不要碰** \`internal/imports/**\`（\`ConfigureWindows\` 与 \`ExtractWindows\` 的签名不动 ——
你是给它们提供实现，不是改它们）、\`internal/render/**\`、\`internal/gateway/**\`、
\`internal/console/**\`、\`scripts/**\`。

### 设计要点

**Resolver 的「按什么约定找文件」是你要定的核心决定。** 几个方向与代价：
- **按目录约定**：配置里给一个基目录，四个文件按固定名字放在
  \`<基目录>/<制品集ID>/\` 或 \`<基目录>/\` 下。简单，运维好理解。
  但「每个制品集一份」与「全局共享一份」是两种不同的运维模型 —— **想清楚哪种对**：
  wimboot 是通用的（iPXE 项目提供，与 Windows 版本无关），
  而 BCD / boot.sdi / boot.wim 来自具体的 WinPE 制品、**按固件分位宽**
  （SPEC §2.1：同一 Windows 版本要按固件导两个 artifact_set）。
  这个区别可能意味着两类文件该放不同层级。
- 按配置显式列四个路径：最直白，但每个制品集都要配一遍，运维负担大。
- 从数据库读：需要新的表或字段，那要改 schema（**禁止**）。

**选一个并在文档与注释里论证。** 判据是「运维在气隙环境下真的能照着做」：
SPEC §2.1 说气隙下 ISO 以「登记服务器本地路径」为主，因为几 GB 走浏览器体验很差 ——
这四个文件同理，运维会 scp 上去。

**\`WimArchDetector\` 的现实取舍**：真实解析 WIM 格式（它是 MS 的压缩容器格式）
工作量不小。**判断 MVP 阶段需要做到什么程度**：
- 最保守：**信任运维在登记时填的 \`ArtifactSet.Firmware\`**，detector 只做一致性校验
  或直接返回声明值。理由：§2.1 已经要求 Windows 按固件导两个制品集，
  那个字段就是运维的显式声明。
- 更严：真读 WIM 头部。**若你判断做不到（或成本不值得），那就实现保守版本，
  并在 contractGaps 里写清「detector 目前信任声明值，真实位宽探测是已知缺口」** ——
  这比写一个半对的 WIM 解析器安全。

### 测试要锁住

- **四个文件都在时能解析出正确路径**
- **缺任一个文件时报明确错误**（指出缺的是哪个），\`runWindows\` 据此让整个导入失败
  —— **不能假设一组默认路径继续**
- 配置项缺失或目录不存在时的行为（启动校验拒绝？还是运行期报错？论证）
- detector 的行为与你选的方案一致，且**固件声明为 \`any\` 时被拒绝**
  （§2.1：Windows 必须取具体固件值）
- **\`ConfigureWindows\` 真的被调用了**：写一个测试确认 importRunner 构造出的 Job
  在遇到 Windows 时不再返回「尚未调用 ConfigureWindows」那个错误

**必跑 \`bash scripts/e2e-windows.sh\`**。它当前用 \`sqlite3\` 绕法造制品集
（因为导入走不通）—— 你修好之后**真实导入路径应该能走通了**，
但那条脚本不属于你：在 findings 里写清它可以改成走真实路径，由编排方接线。`,
    { label: 'm2:win-sources', phase: 'Wire', schema: R, effort: 'high' }),

  // ── m3：真实 ISO 挂载抽取器 ──
  () => agent(mk('m3-mount-extractor') + `## 你的任务：实现真实的 ISO 内容读取，让导入不再依赖 sqlite3 绕法

### 当前状态

\`internal/imports/extract.go\` 的 \`MountExtractor.Open\` 返回
「尚未实现真实挂载逻辑」错误 —— 它是骨架。后果是**全部端到端脚本都用
\`sqlite3\` CLI 直接写 \`artifact_sets\` 表**来造 ready 状态的制品集
（\`scripts/e2e.sh\` 的文件头注释写明了这个绕法）。

所以「导入管线已接上」目前只在单元测试层成立（内存文件系统），
**从没有一张真实 ISO 被读过**。

### 关键设计判断：不要真的 mount

原骨架的注释说真实 \`mount(8)\` 需要 Linux 特权与 loop 设备。**但读 ISO 内容
不需要挂载** —— ISO 9660 是一个有公开规范的文件系统格式，可以纯用户态解析。

**Go 标准库没有 ISO 9660 支持，而本项目禁止加新依赖**（铁律：禁止改 go.mod）。
所以你有几个方向，选一个并论证：

1. **自己实现最小的 ISO 9660 读取器**（只读、只需要 \`fs.FS\` 接口需要的那几个操作）。
   ISO 9660 的核心结构不复杂：2048 字节扇区、主卷描述符在第 16 扇区、
   目录记录是链式的。**Joliet 扩展**（长文件名）与 **Rock Ridge**（POSIX 属性）
   是两个常见扩展 —— 判断需不需要支持（提示：Windows ISO 用 Joliet，
   Linux 发行版 ISO 通常有 Rock Ridge，而我们要找的特征文件路径如
   \`.treeinfo\`、\`sources/install.wim\` 可能在不同扩展下的表现不同）。
   **这是本轨主要的工作量，但它是纯算法、可完整单测、不需要特权、跨平台。**
2. **调用外部工具**（\`7z\`、\`bsdtar\`、\`isoinfo\`）解压到临时目录再返回 \`os.DirFS\`。
   简单，但引入外部二进制依赖，气隙环境下要确保那个工具在。
   **若选这个，必须在文档里写清依赖哪个工具、气隙下怎么保证它在。**
3. **真的 mount**（Linux 专属，需要特权）。**本机是 Windows，你无法测试它** ——
   除非你只写代码不验证，那违反铁律 9。

**我倾向方向 1**（纯 Go、可测、无特权、跨平台），但你要自己判断工作量是否可控。
**若你判断方向 1 在本轮做不完，就实现一个可用的子集并在 unfinished 里写清
支持到什么程度、哪些 ISO 会失败** —— 一个「只支持无 Joliet 的基础 ISO 9660、
但有完整测试」的实现比一个没测过的完整实现有价值。

### 你拥有的文件（只有这些）

  internal/imports/extract.go（**授权你实现 MountExtractor.Open**，不改 Extractor 接口）
  internal/imports/extract_test.go
  internal/imports/iso9660.go（新建：ISO 读取器实现）
  internal/imports/iso9660_test.go
  internal/imports/testdata/（新建：测试用的最小 ISO 镜像，**你要自己造**，见下）
  docs/real-machine/17-iso读取实现.md（你的说明文档，新建）

**不要碰** \`internal/imports/\` 下其它文件（\`identify.go\`、\`job.go\`、
各 \`extract_*.go\`、\`manifest.go\`）、\`internal/render/**\`、\`cmd/**\`、\`scripts/**\`。

### 测试夹具：你需要一个真实的 ISO

**不能依赖外部下载**（气隙纪律，且 ISO 动辄几 GB）。做法：
**自己构造一个最小的 ISO 镜像字节流**作为测试夹具。
几百字节到几 KB 就够 —— 手工按 ISO 9660 规范写出扇区布局，
放进 \`testdata/\`（**注意它是二进制，git 会存它，控制在几 KB 内**）。

或者写一个「生成最小 ISO」的辅助函数，在测试里动态生成 —— 这样不入库二进制文件，
而且生成逻辑本身就是对规范理解的验证。**我倾向这个做法，但你判断。**

若本机有 \`mkisofs\` / \`genisoimage\` / \`xorriso\` 可用，也可以用它生成夹具 ——
**先检查有没有**（\`command -v\`），有就用，但要在测试里说明夹具怎么来的，
且**不能让测试运行时依赖那个工具**（夹具应该是预生成的或纯 Go 生成的）。

### 测试要锁住

- **能读出根目录下的文件**（比如 \`.treeinfo\`）
- **能读出嵌套目录下的文件**（比如 \`sources/install.wim\`、\`boot/x86_64/loader/linux\`
  —— 后者是 SUSE 的路径，三层嵌套）
- **\`fs.Stat\` 对文件与目录都正确**（\`identify.go\` 的 \`exists\` 与 \`existsDir\` 依赖它）
- **\`fs.WalkDir\` 能遍历整棵树**（\`manifest.go\` 的 \`GenerateManifest\` 依赖它）
- **文件内容字节完整**（读出来的内容与写入的一致）
- 不存在的路径返回 \`fs.ErrNotExist\`（不是别的错误 —— \`exists()\` 靠它判断）
- 畸形/截断的 ISO 报明确错误，**不 panic**
- 若支持 Joliet：长文件名能正确读出

**必跑全仓 \`go test ./... -count=1\` 与至少一条 e2e 脚本**确认没打破现状。
你的实现接上之后，端到端脚本**理论上可以不再用 sqlite3 绕法** ——
但那些脚本不属于你：在 findings 里写清可以怎么改，由编排方接线。`,
    { label: 'm3:mount-extractor', phase: 'Wire', schema: R, effort: 'high' }),
])

const ok = out.filter(Boolean)
return {
  done: ok.length + '/3',
  lanes: ok.map(r => ({
    files: (r.files || []).length, commits: r.commits,
    outcome: String(r.verifyOutcome || '').slice(0, 400),
    findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished,
  })),
  allFindings: ok.flatMap(r => r.findings || []),
}
