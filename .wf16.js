export const meta = {
  name: 'lober-16-lanes',
  description: 'Sixteen lanes: wire the ISO import pipeline, add Debian/Ubuntu/SUSE support, wire Windows and ESXi dispatch, secure_link, inspect, loaders, heartbeat, concurrency and audit UI',
  phases: [
    { title: 'Wave1', detail: '导入管线接线、Debian/Ubuntu/SUSE 抽取与渲染、secure_link、inspect、loaders、heartbeat、并发、审计 UI —— 12 轨并行' },
    { title: 'Wave2', detail: '分派接线（单轨串行，它要改被多家族抢的 menu.go/renderer.go）' },
    { title: 'Wave3', detail: '四条端到端验证轨：Debian、Ubuntu、Windows、ESXi' },
  ],
}

const BASE = `你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：__WT__
  分支：    feat/__ID__

**每个文件操作、每条命令都在你的 worktree 里**（绝对路径，或先 \`cd __WT__\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 有 15 条轨在并行。

## 仓库现状

\`dev\` 分支（你的 worktree 基于它）：16 个 Go 包 \`go test ./...\` 与 \`-race\` 全绿、
\`gofmt\` 干净、\`bash scripts/e2e.sh\` 九阶段全 PASS、archtest 依赖方向守卫通过、
前端已构建内嵌、部署产物齐全、五份真机核对报告在 \`docs/real-machine/\`。

**已实现的 OS 家族**：RHEL（kickstart，端到端与 QEMU 真机 PXE 都验过）、
Debian（preseed）、Ubuntu（autoinstall）—— 后两者渲染层就绪但未端到端验证。
**Windows 与 ESXi**：\`internal/render/{unattend,winpe,esxi}.go\` 代码与测试都在，
但**分派处（menu.go / renderer.go）仍返回「未实现」**，没接线。
**SUSE**：完全未做，渲染器显式落「未实现」分支。

**已知的关键断口（多条轨围绕它工作）**：\`internal/imports\` 包在整个仓库里
**零个非测试调用点** —— 制品集登记端点把状态写成 \`importing\` 就结束了，没有任何
东西触发导入 job，也没有端点能把它推到 \`ready\`，而 \`/v1/boot\` 只认 \`ready\`。
\`scripts/e2e.sh\` 的绕法是用 \`sqlite3\` CLI 直接写表。另外 job 只有 \`RunRHEL\` 一个入口。

## 铁律

1. **只写你拥有的文件**（下面明确列出）。16 条轨并行，碰别人的文件就制造合并冲突。
   **特别注意**：\`internal/render/menu.go\` 与 \`renderer.go\` 的分派 switch
   **归 t08-dispatch 一轨独占** —— 别的家族轨只写自己的家族文件并导出函数，
   在 notes 里写明「分派处需要加哪一行调用」，由 t08 接线。
2. **契约只读**：禁止修改 \`internal/core/contract/**\`、\`internal/core/model/**\`、
   \`internal/apierr/**\`、\`internal/archtest/**\`、\`internal/store/sqlite/schema/**\`、
   \`docs/contracts/**\`、\`Makefile\`、\`go.mod\`、\`go.sum\`（除非你的任务明确授权）。
   发现契约缺口时不要加变通：在 contractGaps 里写清「缺什么、你怎么保守绕的、
   正解要改哪个接口」。
3. **不要修改别的模块的既有行为。** 你是做增量：加新文件、在既有文件里加新分支/新函数。
   若必须改既有函数，改动要最小且**不得让任何现存测试变红**。
4. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir() 或写在你 worktree 的
   \`.tmp/\` 下，用完只删你自己建的那个具体文件。**绝不创建与真实模块同名的临时目录**
   —— 之前有 agent 这样做，清理时把真实代码一起删了。
5. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
6. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。可用依赖：modernc.org/sqlite、
   go-cmp、uuid、yaml.v3、golang.org/x/crypto、golang.org/x/net。
7. **import 白名单**：model、contract、标准库，加上你的任务明确允许的。
   **实现模块之间绝不互相 import**（store/statemachine/render/gateway/console/provider/imports
   互不可见）—— archtest 会拦住，**唯一豁免是 cmd/lober 装配点**。
8. 先写测试再写实现。断言失败信息写明「为什么这条重要」加 SPEC 章节号。
9. Bash 的 PATH 快照不含某些目录，需要时自己加：
   Node：\`export PATH="/c/Program Files/nodejs:$PATH"\`
   QEMU：\`export PATH="/d/msys/ucrt64/bin:$PATH"\`
   pwsh：绝对路径 \`/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe\`
   （**禁止 powershell 5.1**：输出是 GB2312，中文全乱码，连错误信息都乱，
   无法判断命令是否成功。pwsh 首句先设 \`[Console]::OutputEncoding=[Text.UTF8Encoding]::new()\`。）
10. **完成前必须真的执行验证并看到通过输出。不要声称通过。** 跑不了的在 verifyOutcome
    写明原因。**不要把「我读了代码觉得没问题」当成验证。**
11. 中文注释、文档与断言信息；标识符与文件名用英文。gofmt 干净。
12. 你的返回值是数据，不是给人看的汇报。

## 验证与提交

必跑：\`go vet <你的包>\`、\`go test <你的包> -v -count=1\`、\`go test <你的包> -race\`、
\`gofmt -l <你的目录>\`、以及 **\`go test ./... -count=1\` 全仓**（你在已经全绿的代码上做增量，
打破任何既有包就是回归）。改了 Go 代码的轨还要跑 \`bash scripts/e2e.sh\` 确认九阶段仍全过。

全绿后 commit（\`git add <你的文件>\` + \`git commit\`）。可分多个 commit。
**只 add 你自己的文件**，提交前用 \`git status\` 确认没夹带别人的东西。

`

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    commits: { type: 'array', items: { type: 'string' } },
    verifyCommands: { type: 'array', items: { type: 'string' } },
    verifyOutcome: { type: 'string' },
    exports: { type: 'array', items: { type: 'string' }, description: '你新增的导出函数签名，供下游接线使用' },
    wiringNeeded: { type: 'array', items: { type: 'string' }, description: '需要别的轨（尤其 t08-dispatch）替你接线的具体位置与那一行代码' },
    findings: { type: 'array', items: { type: 'string' } },
    contractGaps: { type: 'array', items: { type: 'string' } },
    unfinished: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'exports', 'wiringNeeded', 'findings', 'contractGaps', 'unfinished', 'notes'],
}

const mk = (id) => BASE.split('__WT__').join('E:/src/lober-wt/' + id).split('__ID__').join(id)
phase('Wave1')

const w1 = await parallel([
  // ── t01：导入管线接线 —— 本轮最关键的一轨 ──
  () => agent(mk('t01-import-wire') + `## 你的任务：把 ISO 导入管线真正接上（本轮最关键的一轨）

**当前状态**：\`internal/imports\` 包写得很完整（识别、抽取、manifest、job 状态机、
内存文件系统测试），但**整个仓库零个非测试调用点**。制品集登记端点把状态写成
\`importing\` 就返回了，没有任何东西触发导入，也没有端点能把它推到 \`ready\`，
而 \`/v1/boot\` 只认 \`ready\` 的制品集。结果是：**没人能通过产品本身把一张 ISO 变成可装的制品集**，
e2e 靠 \`sqlite3\` CLI 直接写表绕过（脚本注释自己写明了）。

你拥有的文件（只有这些）：
  internal/imports/job.go 与 job_test.go（加家族分派入口）
  internal/console/artifact.go 与 artifact_test.go（登记后触发导入、加进度查询）
  internal/console/server.go（Deps 加导入触发接缝，**只允许加字段，不改既有字段**）
  cmd/lober/importrunner.go（新建：导入 job 的生产装配与异步执行）
  cmd/lober/wire.go（接线，只允许加）
  docs/contracts/http-console.md（**授权你改** —— 若新增端点必须同步契约文档并留变更记录）
  internal/console/routes_test.go（**新增端点必须同时加进那张路由核对表并更新计数注释**
    —— 上一轮漏过一次，那张表会静默少测一个端点而不会变红）

**不要碰** internal/imports/extract_*.go（Debian/Ubuntu 的抽取器归 t02/t03，
SUSE 归 t07；你只改 job.go 的分派）、internal/render/**、internal/store/**、
internal/core/**、scripts/e2e.sh（e2e 归 t04/t05）。

### 要做的事

1. **job.go 加家族分派入口**。现在只有 \`RunRHEL\`（硬编码 \`if family != OSFamilyRHEL\` 就 fail）
   与 \`RunUnsupported\`。改成一个按识别结果自动分派的入口（比如 \`Run\`），内部按
   \`Identify\` 的结果选对应的 \`ExtractXxx\`：RHEL → \`ExtractRHEL\`、Debian → \`ExtractDebian\`、
   Ubuntu → \`ExtractUbuntu\`（这三个都已存在）。**Windows/ESXi/SUSE 的抽取器由别的轨
   在本轮并行添加**，所以你的分派要写成「查表 + 表里没有就落到明确的未实现错误」，
   让别的轨只需往表里加一行、不必改你的分派逻辑。在 exports 里写清那张表的形状与加法。
   **保留 \`RunRHEL\` 作为兼容入口**（它有测试）或让它委托给新的 \`Run\`，别让既有测试变红。
2. **控制台登记端点触发导入**。登记后异步跑导入 job（\`importing\` → \`ready\`/\`failed\`）。
   注意契约纪律：console **不许 import internal/imports**（archtest 会拦）。
   做法是在 \`console.Deps\` 加一个窄接口（比如 \`ArtifactImporter\`，一个方法：
   「异步导入这个制品集」），装配点用 \`cmd/lober/importrunner.go\` 里的实现注入。
   **接口定义放哪里要想清楚**：\`internal/core/contract\` 是只读的，所以定义在
   \`internal/console\` 包内（调用方定义接口是 Go 的惯例，也避免改契约层）。
3. **导入进度可查**。SPEC §11 Phase 2 与 §12.6 都要求「ISO 登记与导入进度（异步 job）」。
   现有 \`GET /artifact-sets\` 列表带 state，判断这够不够 —— 若需要失败原因，
   看 \`model.ArtifactSet\` 有没有承载它的字段（**不许改 model**），没有就在 contractGaps
   里写清，并用现有字段能表达的最大程度实现。
4. **异步执行的纪律**：导入是长耗时（解压几 GB）。不要在 HTTP handler 里同步跑。
   \`importSemaphore\`（job.go 里已有）限制并发。进程退出时在途导入会中断 —— 制品集会
   卡在 \`importing\`，在 notes 里写清这个行为与运维该怎么办。

### 测试要锁住

- 家族分派：RHEL/Debian/Ubuntu 三个家族各自走到对应的抽取器（用内存文件系统造特征文件）
- **未实现家族落明确错误而非静默成功** —— 静默成功会产出一个 \`ready\` 但没有 kernel 的
  制品集，而那要等机器上电取不到内核才暴露
- 登记端点返回后制品集是 \`importing\`，**导入完成后变 \`ready\`**（用可控的假导入器测，
  不要真解压）
- **导入失败时状态是 \`failed\` 且带原因**，不是卡在 \`importing\`
- 路由核对表包含所有新增端点（若有）`, { label: 't01:import-wire', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t02 / t03：Debian 与 Ubuntu 抽取器补全 ──
  () => agent(mk('t02-import-deb') + `## 你的任务：Debian 抽取器补全与 import 侧加固

\`internal/imports/extract_debian.go\` 已有 \`ExtractDebian\`（3 条测试）。本轨把它做到
**能真的处理一张 Debian netinst/DVD ISO 的目录树**，并补齐测试覆盖。

你拥有的文件（只有这些）：
  internal/imports/extract_debian.go 与 extract_debian_test.go
  internal/imports/identify.go 与 identify_test.go（**只允许加 Debian 相关的识别加固，
    不改 RHEL/Ubuntu 分支的既有行为**）

**不要碰** job.go（归 t01）、extract_ubuntu.go（归 t03）、extract_windows/esxi.go、
internal/render/**。

### 要点

- Debian 的特征文件是 \`dists/\`（§2.1 的表）。但**识别要稳**：Ubuntu 的 ISO 里也有
  \`dists/\`（它是 Debian 衍生），所以识别顺序与判据要想清楚 —— Ubuntu 有
  \`.disk/info\`，Debian 也可能有 \`.disk/\` 目录。**读 identify.go 的现有实现，
  确认当前顺序不会把 Ubuntu 误判成 Debian**，若会，那是一个真 bug，修它并写测试。
  这条最重要：误判的后果是给 Ubuntu 机器渲染 preseed，安装器直接不认。
- Debian 的引导制品是 \`linux\` + \`initrd.gz\`（不是 RHEL 的 \`vmlinuz\` + \`initrd.img\`），
  路径通常在 \`install.amd/\` 下。抽取要找对，并且**抽不到时报明确错误**而不是产出空路径。
- 仓库树：Debian 的 \`pool/\` 与 \`dists/\` 都要能被 nginx 直接发（§1.4 的 repo location）。
  落盘路径严格遵守 \`docs/contracts/filesystem.md\`。
- 全部测试通过**内存文件系统**（\`fs.FS\`），不依赖真 ISO —— 这是 M7 立的规矩。

### 测试要锁住

- 完整 Debian 目录树能被正确识别与抽取（kernel/initrd/repo 三项路径都对）
- **Ubuntu 的目录树不会被误判成 Debian**（这条是安全关键）
- 缺 kernel、缺 initrd、缺 dists/ 各自报明确错误
- 路径是相对路径、不含 scheme 与主机名（拼 URL 是渲染器的事，§1.5）`, { label: 't02:import-debian', phase: 'Wave1', schema: R, effort: 'high' }),

  () => agent(mk('t03-import-ubuntu') + `## 你的任务：Ubuntu 抽取器补全与 import 侧加固

\`internal/imports/extract_ubuntu.go\` 已有 \`ExtractUbuntu\`（5 条测试）。本轨把它做到
**能真的处理一张 Ubuntu Server live ISO 的目录树**，并补齐测试覆盖。

你拥有的文件（只有这些）：
  internal/imports/extract_ubuntu.go 与 extract_ubuntu_test.go

**不要碰** identify.go（归 t02，它在那里做识别加固）、job.go（归 t01）、
extract_debian.go、extract_windows/esxi.go、internal/render/**。
若你发现识别逻辑有问题，**写进 findings 让 t02 或编排方处理**，不要自己改 identify.go。

### 要点

- Ubuntu Server 24.04 的 live ISO 结构与 Debian 不同：引导制品在 \`casper/\` 下
  （\`vmlinuz\` + \`initrd\`），\`.disk/info\` 是识别特征。**注意现代 Ubuntu 用
  subiquity/casper 而不是 debian-installer**，抽取要按 live ISO 的实际布局找。
- Ubuntu 的 autoinstall 用 cloud-init NoCloud，内核参数是 \`ds=nocloud-net;s=<url>\`，
  **URL 必须是目录且结尾带 \`/\`**，那个目录下要有 \`user-data\` 与 \`meta-data\` 两个文件
  —— 渲染器已经处理了这部分，你只需保证抽取出的制品路径能让菜单拼对（看
  \`internal/render/menu.go\` 的 \`renderUbuntuMenu\` 期望哪些路径键，**只读，不改**）。
- 仓库/squashfs：live ISO 的软件源在 \`casper/*.squashfs\` 里，与 Debian 的 \`pool/\` 机制
  不同。判断 MVP 阶段抽取到什么程度够用（至少 kernel/initrd 要对），
  拿不准的在 contractGaps 里写清并选最保守的做法。
- 全部测试通过**内存文件系统**，不依赖真 ISO。

### 测试要锁住

- 完整 Ubuntu live ISO 目录树能被正确抽取（casper 下的 kernel/initrd 路径对）
- 缺 casper/vmlinuz、缺 initrd 各自报明确错误
- 路径是相对路径、不含 scheme 与主机名`, { label: 't03:import-ubuntu', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t06 / t07：SUSE 渲染与导入 ──
  () => agent(mk('t06-suse-render') + `## 你的任务：SUSE AutoYaST 渲染器（新家族）

**SUSE 完全没做**，渲染器显式落「未实现」分支。本轨实现它。

你拥有的文件（只有这些）：
  internal/render/autoyast.go 与 autoyast_test.go（新建）
  internal/render/testdata/autoyast_*.golden（新建）

**绝对不要改 internal/render/menu.go 与 renderer.go** —— 那两处的分派 switch
归 t08-dispatch 一轨独占（本轮有四个家族同时要接线，抢同一个文件必然冲突）。
你把入口写成**独立的导出函数**，在 wiringNeeded 里写清「menu.go 的 switch 要加
\`case model.OSFamilySUSE: return RenderAutoYaSTMenu(ctx)\`、renderer.go 要加
\`case model.OSFamilySUSE: return AssembleAutoYaST(ctx)\`」这样的精确指示。

### 最重要的一条（SPEC 反复警示）

**SUSE 用 AutoYaST，不是 kickstart。** 引导参数是 \`autoyast=\` 而非 \`inst.ks=\`，
应答文件是 \`autoinst.xml\`（\`model.AnswerFileName\` 已经这么规定），格式是**独立的 XML**。
**绝不要因为两者都被口头称作「ks」就复用 kickstart 的任何模板逻辑** —— 语法完全不同，
产出的文件安装器根本不认。

### 要点

- **XML 用 \`html/template\` 而非 \`text/template\`**（§5.6）：它需要 XML 转义。
  但注意 \`html/template\` 的转义是面向 HTML 的，对 XML 有些场景不完全等价 ——
  判断是否需要自己做 XML 转义，把理由写进注释。form_data 的值仍要走
  \`internal/render/guard\` 的类型白名单（**只读那个包，不改它**）。
- **三段结构要与既有家族一致**：prologue（参数内联 + syslog 指向）、用户 hook、
  epilogue（firstboot 脚手架 + 凭据清理）。读 \`kickstart.go\` 与 \`skeleton.go\`
  看现有骨架怎么组织的，**复用它们的公共部分**而不是另起一套。
- **hook 阶段映射**（§5.2）：AutoYaST 的对应段落是 \`<scripts>\` 下的
  \`pre-scripts\` / \`chroot-scripts\` / \`post-scripts\` / \`init-scripts\`，
  与 kickstart 的 \`%pre\`/\`%post --nochroot\`/\`%post\`/firstboot 不是一一对应。
  **查 SUSE 官方文档核实**（用 curl / WebSearch；github.com 常被挡，
  raw.githubusercontent.com 与 doc.opensuse.org 通常可直连）。
  **核实不到的拼写标注「未核实」并在 contractGaps 里列出**，不要凭印象写 —— SPEC
  附录 B 就把「各 OS 内核参数拼写」列为待核实项。
- **root SSH 公钥**：从 \`ctx.RootSSHPublicKeys\` 取（这是刚补的契约通路），
  **对空值必须硬拒绝**，与另外三个家族一致 —— 写占位公钥会静默产出一台没人能登录的机器。
- 注入的 \`signal()\` 与 \`run_hook\` 复用现有实现（\`clientlib.go\`，**只读**）。
- 产出中**不得含字面 \`http://\`**，URL 从 \`ctx.Bases\` 的方法拼。
- golden file 带 \`-update\` 开关，与现有家族一致。

### 测试要锁住

- 完整 AutoYaST XML 结构正确（能被 XML 解析器解析 —— 用 \`encoding/xml\` 验一次）
- 四个 hook 阶段各自落到正确的 AutoYaST 段落
- **恶意输入不逃逸**：XML 结构字符（\`<\` \`>\` \`&\` \`"\`）、换行、\`$(...)\`、反引号、
  超长串，逐个验证要么被拒绝要么被正确转义。**XML 与 shell 是两层转义，都要验**
- 空 RootSSHPublicKeys 被硬拒绝
- 产出不含字面 \`http://\`（\`grep\` 自查）
- **RHEL/Debian/Ubuntu 的既有 golden file 不得变化** —— 变了说明你动了共用骨架的行为`, { label: 't06:suse-render', phase: 'Wave1', schema: R, effort: 'high' }),

  () => agent(mk('t07-suse-import') + `## 你的任务：SUSE 抽取器与识别（新家族）

你拥有的文件（只有这些）：
  internal/imports/extract_suse.go 与 extract_suse_test.go（新建）

**不要碰** identify.go（归 t02）、job.go（归 t01）、其它 extract_*.go。
SUSE 的识别特征需要加进 identify.go 与 job.go 的分派表，但**那两个文件不属于你** ——
在 wiringNeeded 里写清「identify.go 要加什么特征文件判据」与「job.go 的分派表要加
\`OSFamilySUSE: ExtractSUSE\` 这一行」，由那两轨或编排方接线。

### 要点

- openSUSE Leap / SLES 的 ISO 特征：\`content\` 文件（描述产品元数据）与
  \`boot/x86_64/loader/\` 下的 \`linux\` + \`initrd\`。**SPEC §2.1 的特征文件表里没有
  SUSE 这一行**（表里只有 RHEL/Ubuntu/Debian/ESXi/Windows），所以你要**自己查证**
  并把依据写进注释与 contractGaps —— 这是 SPEC 的一处空白，不是你可以随便填的。
  用 curl / WebSearch 查 openSUSE 官方文档（doc.opensuse.org 通常可直连）。
  **查不到的标注「未核实」，不要编。**
- 引导制品路径与 RHEL 不同（\`boot/x86_64/loader/linux\` 而非 \`images/pxeboot/vmlinuz\`），
  抽取要找对，抽不到报明确错误。
- 仓库树：SUSE 用 zypper 仓库（\`repodata/\` 与 \`suse/\` 目录），落盘路径遵守
  \`docs/contracts/filesystem.md\`。
- 全部测试通过**内存文件系统**，不依赖真 ISO。

### 测试要锁住

- 完整 openSUSE 目录树能被正确抽取（kernel/initrd/repo 路径都对）
- 缺各个关键文件时报明确错误
- **不会把 RHEL 或 Debian 的目录树误判成 SUSE**（写一个反向测试）
- 路径是相对路径、不含 scheme 与主机名`, { label: 't07:suse-import', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t11：secure_link 制品授权 ──
  () => agent(mk('t11-securelink') + `## 你的任务：secure_link 制品授权（SPEC §9.3，Phase 2 项）

**当前状态**：MVP 阶段制品是开放的（\`deploy/nginx/lober.conf\` 的 \`/artifacts/\`
location 直接发盘，无授权）。SPEC §9.3 要求大制品（kernel/initrd/镜像/WIM）走
nginx \`secure_link\`（HMAC + 过期），**Go 只签发 URL、完全不在数据路径上**。

你拥有的文件（只有这些）：
  internal/gateway/siglink.go 与 siglink_test.go（新建：签发带签名的制品 URL）
  internal/gateway/server.go（**只允许加 Deps 字段与接线，不改既有字段**）
  internal/config/config.go 与 config_test.go（加 secure_link 密钥配置项）
  cmd/lober/wire.go（接线，只允许加）
  deploy/nginx/lober.conf（**只允许加 secure_link 相关指令，不得破坏既有配置**）
  deploy/lober.yaml.example
  docs/real-machine/06-securelink.md（你的说明文档，新建）

**不要碰** internal/render/**（渲染器拼 URL 的地方 —— 若签名 URL 需要在渲染器里生成，
那是一个真实的架构问题：渲染器是纯函数、不该持有密钥。**在 contractGaps 里写清你的判断**，
并选最保守的做法：让网关在构造 RenderContext 时把已签名的基址传进去，
或者只对 kernel/initrd 之外的路径签名）、internal/console/**、internal/imports/**。

### 要点

- **nginx 的 \`secure_link_md5\` 公式必须与 Go 的签名逐字节一致**，否则机器取制品即 403。
  SPEC §9.3 给的公式是 \`"$secure_link_expires$uri$remote_addr SECRET"\`。
  **\`$remote_addr\` 进签名有一个陷阱**（§9.3 与 §1.6）：nginx 校验时用真实客户端 IP，
  而 Go 签发时看到的是 loopback（因为它在 nginx 后面），两边 HMAC 不一致 →
  机器取制品 403。所以 Go 必须用 \`X-Real-IP\`（§1.6 的 \`clientIP\` 已经实现了这个逻辑，
  **读它，复用它**）。
- **另一个陷阱**：若机器在「取引导脚本」与「取制品」之间换了 DHCP 租约导致 IP 变化，
  签名失效（§9.3 末段）。租期设得长于装机窗口可避开 —— \`deploy/dnsmasq/lober.conf\`
  已经是 24h 租期（**只读确认，不改那个文件**）。
- **离线仓库（RPM/deb）必须保持开放**（§9.3 的表）：installer 按 repodata 自行构造 URL
  拉几万个包，无法让它带签名。**只对大制品签名，不要把仓库也签了** —— 签了会让装机在
  装包阶段整体失败。
- **nginx 需要编译带 \`--with-http_secure_link_module\`**。WSL 那轮已经核实过 Debian 的
  nginx 包带不带这个选项（见 \`docs/real-machine/01-配置语法核对.md\`，**读它**）。
  若不带，在文档里写清运维要怎么办。
- 本机没有 nginx，\`nginx -t\` 跑不了 —— 做静态核对并在 verifyOutcome 写明需在目标主机补做。
  但**签名算法本身必须有单元测试**，并且**用 Go 实现一遍 nginx 的校验逻辑做往返测试**
  （这是唯一能在本机证明两边一致的办法）。

### 测试要锁住

- 签名 URL 的往返：Go 签发 → 用独立实现的校验逻辑验证通过
- **过期时间生效**：过期的 URL 校验失败
- **IP 绑定生效**：换一个 IP 校验失败
- **密钥不同则签名不同**（防止配置了空密钥还能用）
- 空密钥配置被启动校验拒绝（否则等于没有授权）`, { label: 't11:securelink', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t12：inspect 接线 ──
  () => agent(mk('t12-inspect-wire') + `## 你的任务：inspect 硬件采集接线 —— 让服务端决策真的用上采集数据

**当前状态**：\`internal/provider/{hwfacts,nicplanner}.go\` 与 \`internal/render/inspect.go\`
都已实现（有测试），但**采集脚本渲染出来之后，上报的 facts 有没有被服务端决策真正消费，
是本轨要打通并证明的**。

你拥有的文件（只有这些）：
  internal/provider/resolve.go 与 resolve_test.go（**只允许加新决策步骤，不改既有步骤行为**）
  internal/provider/hwfacts.go 与 hwfacts_test.go
  internal/provider/nicplanner.go 与 nicplanner_test.go
  internal/render/inspect.go 与 inspect_test.go
  internal/console/dryrun.go 与 dryrun_test.go（**只允许加，不改既有行为**）
  docs/real-machine/07-inspect-闭环.md（你的说明文档，新建）

**不要碰** internal/render/{menu,renderer,kickstart,autoinstall,preseed}.go、
internal/gateway/**（facts 端点已实现）、internal/imports/**。

### 要点（SPEC §5.6 的正确顺序）

1. \`pre\` 阶段采集 \`lsblk\`/\`lspci\`/\`dmidecode\`/\`ethtool\`，上报为 fact
2. **服务端**用采集结果决策：按容量/型号/rotational/by-path 选盘，
   按 MAC/PCI 路径/link 状态选网卡
3. 再渲染应答文件，把具体设备标识写死进去 —— **用 by-id / by-path / WWN，
   不用内核分配的名字**（§10 失败模式 5：磁盘设备名漂移会装到错误的盘）

**为什么决策必须在服务端**：推到机器上（在 \`%pre\` 里算）则失败时无日志可看、无法复现；
服务端决策可调试、可 dry-run、可审计。这是 §5.6 的明确要求。

- 采集脚本在 installer ramdisk 里跑，**没有 python、没有 jq，curl 可能只有 busybox 版**
  （§6.2）。只能用 shell 内建与 coreutils，输出形态要让服务端零成本解析。
- **解析要对畸形输入健壮**：真实硬件上 \`lsblk\`/\`lspci\` 的输出千奇百怪，
  解析失败要给出能定位的错误而不是 panic。
- **网卡命名漂移会让装完的机器网络不通、需要人到现场**（§10 失败模式 6），
  所以要 MAC 绑定 + 固化 \`net.ifnames\` 策略。
- **磁盘/网卡策略必须是纯函数**：无 IO、无时钟、无随机数（这套系统会擦盘，
  行为不完全确定则故障无法复现）。用 grep 自查。
- dry-run 端点要能**用采集到的真实 facts 跑一遍策略并展示「为什么这么选」**
  （§8.2 说 preview 与 dryrun 是开发期性价比最高的两个端点）。

### 测试要锁住

- 采集 → 解析 → 决策 → 渲染的**完整闭环**：给一份真实形态的 \`lsblk\`/\`lspci\` 输出文本，
  确认最终渲染出的应答文件里写的是 by-id 路径而不是 \`/dev/sda\`
- 畸形采集输出不 panic，报可定位的错误
- 磁盘/网卡策略是纯函数（grep 自查无 IO/时钟/随机）
- dry-run 能吃真实 facts 并返回非空的选择理由`, { label: 't12:inspect-wire', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t13：exec / http provider 加载器 ──
  () => agent(mk('t13-loaders') + `## 你的任务：exec 与 http provider 加载器（SPEC §7.2，Phase 2 项）

**开工前提已满足**：\`docs/phase2-loader-需求依据.md\`（上一轮产出）给了开工判据。
**先读它**，按它的结论决定实现范围。若它明确说「现在不该做」，你要在 notes 里
写清你为什么仍然做（用户已明确要求「除了 TLS 和安全启动，其他都做」）。

你拥有的文件（只有这些）：
  internal/provider/loader/ 整个新目录（protocol.go / exec.go / http.go / breaker.go / reload.go 与测试）
  internal/provider/factory.go（**只允许加新 case 分支，不改既有 case 的行为**）
  internal/config/config.go 与 config_test.go（加 loader 类型与参数配置项）
  deploy/lober.yaml.example
  docs/real-machine/08-loaders.md（你的说明文档，新建）

**不要碰** internal/provider 下的其它实现文件（diskplanner/resolver/ipam/dns/
inventory/notify/heartbeat/eventbus/resolve/lifecycle 归别的轨或保持原样）、
internal/core/contract/**（七个 provider 接口是冻结的 —— 加载器是那些接口的**另一种实现**，
不需要改接口，这正是 §7.6「接口边界是真正有价值的部分，加载机制可以后补」的意思）。

### 要点

- **加载器是接口的另一种实现，对调用方透明**（§7.2）。\`exec\` 类型启动子进程、
  标准输入给 JSON、标准输出读 JSON；\`http\` 类型发 POST、body 是 JSON。
  两者共用一套请求/响应编解码，以及一套超时、重试与熔断策略。
- **失败语义按 §7.4 的表**，不要自己发明：IPAM/TemplateResolver/DiskPlanner 失败
  **阻塞建任务**；DNS/Notify 失败**只告警**；Heartbeat 失败只记录。
  加载器不改变这个语义，它只是换了执行方式。
- **为什么 exec/http 而不是 grpc**（§7.2 选型判据）：气隙环境下改 shell 脚本是 30 秒的事，
  改 Go 插件要在外网编译、打包、走审批、传入内网。DNS 注册、告警推送这类几行 curl
  的集成用 gRPC 是纯负担。
- **超时是硬要求**：子进程与 webhook 都可能挂住。超时后要杀掉子进程（不是只放弃等待 ——
  僵死子进程会累积）。
- **熔断**：连续失败 N 次标记 provider 不可用，之后按 §7.4 的失败语义处理。
  指数退避重试。
- **不做 grpc 与 starlark**（那是 Phase 3，且用户本轮没要求）。

### 测试要锁住

- exec 加载器：子进程正常返回 → 结果被正确解码；子进程非零退出 → 错误；
  **子进程超时 → 被杀掉且报超时**（用一个 sleep 脚本验，确认进程真的没了）
- http 加载器：200 正常解码；非 2xx → 错误；超时 → 报超时
- 熔断：连续失败达阈值后标记不可用，不再发起调用
- **失败语义与 §7.4 一致**：阻塞式 provider 的加载器失败真的阻塞，
  告警式的真的只告警
- 配置里选了 exec 但没给 command → 启动校验拒绝（否则运行期才发现）`, { label: 't13:loaders', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t14：心跳探测补全 ──
  () => agent(mk('t14-heartbeat') + `## 你的任务：心跳探测补全与装配（SPEC §6.6）

**当前状态**：\`internal/provider/heartbeat.go\` 有 tcp 与 icmp 两个探测器，
但**探测循环有没有真的在生产里跑起来、探测结果有没有被消费，是本轨要打通的**。

你拥有的文件（只有这些）：
  internal/provider/heartbeat.go 与 heartbeat_test.go
  cmd/lober/heartbeat.go（新建：探测循环的生产装配）
  cmd/lober/wire.go（接线，只允许加）
  cmd/lober/server.go（**只允许加探测循环的启停，不改既有 listener 逻辑**）
  internal/config/config.go 与 config_test.go（若需要补配置项）
  docs/real-machine/09-heartbeat.md（你的说明文档，新建）

**不要碰** internal/provider 下其它文件、internal/statemachine/**、internal/console/**。

### 要点（§6.6 的定位）

**心跳是辅助通道，不是主通道。** 机器主动推送信号后，拉取不再承担状态推进，
只作为「推送断了」时的兜底判断（机器是还活着还是真死了）。

- 拉取范围：\`state ∈ {booting, installing, rebooting, initializing}\` 且已知可探测地址
- \`icmp\` 探测器的目标地址来自 \`/v1/boot\` 请求的 \`X-Real-IP\`；
  \`tcp\` 用表单中的业务 IP + 指定端口
- **默认 tcp 而非 icmp**：ICMP 需要特权套接字（root / CAP_NET_RAW / \`ping_group_range\`），
  而心跳只是辅助通道，不值得为它给整个进程提权。**icmp 无权限时必须优雅降级而非 panic**。
- **拉取的本质局限要写进文档**：ICMP/TCP 在你最关心的故障里全是正常的 ——
  anaconda 卡在依赖解析、hook 死循环、仓库 404 重试时，内核活着、网络通着。
  所以拉取能完整替代存活判断，**替代不了进展判断**。进展靠信号 + syslog。
- **探测失败不改任务状态**（§7.4 的表），只记录。这一条搞错会让一次网络抖动
  把正在装的机器标成失败。
- **间隔与并发**：按配置（默认 60s）。§10 失败模式 12 提到若将来接 Redfish 需要
  间隔 ≥60s、全局并发 ≤8、按 BMC 串行 —— 现在不做 Redfish，但接口留着。
- **探测循环要能被 context 取消并干净退出**（与超时扫描循环同样的纪律，
  读 \`cmd/lober/scanner.go\` 看现有模式）。
- **业务 IP 可达性**：§6.6 末段要求若用业务 IP 探测 \`ready\`，需在建任务时校验
  业务 IP 对服务端可达 —— 若业务网与管理网不通，探测永远失败、任务永远卡在
  \`initializing\`。判断这条要不要在本轨做（它涉及 console 建任务路径，而那不属于你），
  拿不准写进 contractGaps。

### 测试要锁住

- 探测循环按间隔跑、能被 context 取消并退出（\`-race\` 下跑）
- **icmp 无权限时优雅降级**（不 panic，记录并继续）
- 探测失败**不改任务状态**
- 只探测 \`{booting, installing, rebooting, initializing}\` 状态的任务
- tcp 探测的端口来自配置`, { label: 't14:heartbeat', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t15：并发限流与失败重试 ──
  () => agent(mk('t15-concurrency') + `## 你的任务：并发装机限流与自动重试（SPEC §10 失败模式 11，Phase 4 项）

**当前状态**：\`internal/statemachine/admission.go\` 在之前一轮做过一部分（读它确认现状）。
本轨把它做完并接进生产路径。

你拥有的文件（只有这些）：
  internal/statemachine/admission.go 与 admission_test.go
  internal/statemachine/scan.go 与 scan_test.go（**只允许加并发上限相关逻辑，
    不得改既有的超时判定行为**）
  internal/statemachine/boot.go（**只允许在 StartBoot 里接入准入检查，不改既有顺序约束**）
  internal/provider/notify.go 与 notify_test.go（告警文案）
  internal/config/config.go 与 config_test.go（并发上限配置项）
  cmd/lober/wire.go（接线，只允许加）
  docs/real-machine/10-concurrency.md（你的说明文档，新建）

**不要碰** internal/console/**、internal/gateway/**、internal/render/**、
deploy/nginx/**（nginx 的限流归 t11 或保持原样）。

### 最危险的一条（搞错会擦掉生产机数据）

**自动重试必须严格排除三种不可重装的失败原因**：\`reboot_timeout\`、\`init_timeout\`、
\`init_failed\`。这些机器**已经装好、盘上可能有数据**，重装会擦掉（§3.4）。
自动重试只能作用于可重装的那六种，且要有次数上限（attempt 上限已是 3）。
**这条是全系统最危险的逻辑**，必须有测试逐个原因验证。

### 并发限流要点

- 上限是「同时处于消耗带宽阶段（installing）的任务数」，超过上限的引导请求
  **要排队而不是拒绝** —— 拒绝会让机器回到 PXE 轮询，运维看不出发生了什么。
- **排队怎么表达要想清楚**：iPXE 那边是一次 HTTP 请求，不能让它挂很久
  （§4.2 要求 \`/v1/boot\` 始终 200 且快速响应）。一个可行方向是超限时返回一个
  让机器稍后重试的 iPXE 脚本（sleep + chain 回 \`/v1/boot\`），而不是签 token。
  **这不消耗 attempt_count** —— 它不是失败，是排队。
  **你要自己判断这个方向对不对，并在 notes 里说明理由。**
- 上限从配置来（默认值自己定并说明依据）。
- **§10 运维点二**：信号回调量不小（数百台 × 每 30s），nginx 侧给 \`/v1/signal\`
  单独设限流桶（已在 \`deploy/nginx/lober-http-snippet.conf\` 做了，**只读确认**）。

### 告警文案要点（§3.5 与 §3.4）

- \`state=failed\` 本身无用，告警必须带 §3.5 的六个字段（阶段、退出码、hook digest、
  日志尾部、syslog 尾部、**距上次信号多久**）。\`last_signal_at\` 区分度最高：
  刚收到信号说明机器活着、是脚本或 installer 逻辑错误；很久没信号说明机器层面出事，
  两个排查方向完全不同。
- **\`initializing\` 失败的告警文案必须与「装机失败」明确区分**（§3.4 末段）：
  前者的正确处理是人工登录 \`touch /var/lib/lober/init.pending\` 重跑脚本，
  后者可以重装。两者处理方式完全不同，文案混了会让运维对一台装好的机器执行重装。
- Notify 失败**只告警、不影响任务状态**（§7.4）。

### 测试要锁住

- **三种不可重装原因的自动重试被拒绝**（逐个验，这是最重要的测试）
- 六种可重装原因的自动重试被接受，且受 attempt 上限约束
- 并发上限生效：超限时不签 token、**不消耗 attempt_count**
- 超时扫描的既有行为未被改变（\`pending\` 永不超时等）
- **时间全部来自注入的 Clock**（\`grep -rn 'time.Now()' internal/statemachine/\`
  确认实现代码里仍然零处真实调用）
- \`-race -count=20\` 跑（之前 M3 有个 1% 概率复现的水位回退竞态，低 count 测不出来）
- initializing 失败与装机失败的告警文案可区分`, { label: 't15:concurrency', phase: 'Wave1', schema: R, effort: 'high' }),

  // ── t16：审计 UI 与前端补齐 ──
  () => agent(mk('t16-audit-ui') + `## 你的任务：审计查询 UI 与前端补齐

**当前状态**：后端刚补了审计持久化与 \`GET /hooks/{id}/audit\` 查询端点
（上一轮的契约变更，5 条 conformance 断言在两个 Store 实现上都绿）。
**但前端还没有用上它** —— hook 页面的驳回理由仍然只在驳回当次的响应里可见。

你拥有的文件（只有这些）：
  web/apps/web-ele/src/api/console/audit.ts（新建）
  web/apps/web-ele/src/views/hooks/index.vue（只允许加审计相关 UI，不改既有审批流转逻辑）
  web/apps/web-ele/src/components/audit-timeline.vue（新建，若需要）
  docs/real-machine/11-audit-ui.md（你的说明文档，新建）

**不要碰** 任何 Go 文件、\`web/packages/**\` 与 \`web/internal/**\`（§12.9：定制只收敛在
apps/web-ele 四处，升级上游就是覆盖那两棵树）、其它 \`src/views/\` 下的页面。

### 工具链

\`export PATH="/c/Program Files/nodejs:$PATH"\`（Bash 的 PATH 快照里没有 node）。
pnpm 用 corepack 供给的 11.x，不是 nodejs 自带的 9.12。
前端在 \`web/\` 下，构建脚本是 \`pnpm build:ele\`，类型检查是 \`pnpm check:type\`
（**注意不是 \`pnpm typecheck\`** —— 那个脚本在精简时被删了，实际叫 \`check\` / \`check:type\`）。
构建产物落在 \`internal/console/ui/dist\`（\`go:embed\` 目标）。

### 要点

- **先读 \`docs/contracts/http-console.md\` 里审计端点那一节**（上一轮新增的，编号 29），
  路径、请求参数、响应形态逐字段照它写，不要自己发明。
- **驳回理由的可见性有一个已知限制**：\`AuditLogger\` 原本是只写接口，上一轮补了查询路径，
  但 UI 要明确告知作者「理由从审计记录里查」。**不要让界面暗示理由会自动通知作者** ——
  那是做不到的。
- hook 审批是危险动作（§9.4）：hook 以 root 身份在分区之前运行，能力等同「擦掉这台机器
  所有盘」。审计时间线要显示**谁在什么时候做了什么**，这是追溯的全部价值。
- 权限：审计记录含操作人信息，按 §9.4 的分级 —— 至少不能让无权限角色看到全部审计流。
  后端已经做了权限检查，**前端要正确处理 403**（不要显示成「加载失败」那种无信息的错误）。
- **气隙纪律**（§12.4）：不要引入任何需要在线解析的资源。图标用 \`@vben/icons\` 导出的
  lucide 组件，不要在 \`meta.icon\` 里写 iconify 字符串名。

### 验证

\`pnpm check:type\` 无错误、\`pnpm lint\` 通过、\`pnpm build:ele\` 成功。
**气隙 grep**：构建产物里不得新增 \`hm.baidu.com\` / \`unpkg.com\` / \`api.iconify.design\` 的命中
（\`unpkg.com\` 与 \`api.iconify.design\` 当前各有 1-2 处命中，那是框架默认值里的死字符串、
被应用层覆盖掉了，**你只要确认没有新增**）。
另外跑 \`cd <worktree> && go test ./internal/console/ui/ -count=1\` 确认 embed 仍然可用。

**注意 dist 不入库**（交付方式是 release tar，\`.gitignore\` 只放行两个占位文件）——
所以你构建出的产物不要 \`git add\`，只提交 \`web/\` 下的源码。`, { label: 't16:audit-ui', phase: 'Wave1', schema: R, effort: 'high' }),
])

const ok1 = w1.filter(Boolean)
log('Wave1 完成 ' + ok1.length + '/11')

// 汇总各轨的接线请求，交给 t08 独占改分派处
const WIRING = ok1.flatMap(r => r.wiringNeeded || [])
const EXPORTS = ok1.flatMap(r => r.exports || [])
const GAPS1 = ok1.flatMap(r => r.contractGaps || [])

phase('Wave2')

const t08 = await agent(mk('t08-dispatch') + `## 你的任务：分派接线 —— 把 Windows / ESXi / SUSE 接进渲染分派（本轮唯一能改分派处的轨）

**为什么单独一轨**：\`internal/render/menu.go\` 与 \`renderer.go\` 的分派 switch 会被
四个家族同时抢。上一轮我让 Windows 与 ESXi 两轨「写成独立函数、由编排方接线」，
**然后漏了接线** —— 结果是 \`unattend.go\`/\`winpe.go\`/\`esxi.go\` 连测试带 golden 都在，
但分派处仍返回「未实现」，那些代码一行都跑不到。本轨专门修这件事。

你拥有的文件（只有这些）：
  internal/render/menu.go（分派 switch）
  internal/render/renderer.go（分派 switch）
  internal/render/dispatch_test.go（新建：分派覆盖测试）

**不要碰** 各家族的实现文件（kickstart.go / autoinstall.go / preseed.go / autoyast.go /
unattend.go / winpe.go / esxi.go），也不要碰 internal/imports/**、internal/console/**。
若某个家族的导出函数签名不合用，**不要改那个文件** —— 在 contractGaps 里写清，
用适配的方式在分派处解决。

### 已就位的导出函数（Wave1 各轨与既有代码）

Windows（\`internal/render/{unattend,winpe}.go\`，已存在）：
  RenderUnattend(ctx model.RenderContext) (string, error)
  RenderWindowsMenu(ctx model.RenderContext) (string, error)
  另有 WinPESignalPoints / WinPEClientLib / WinPEInstallScript / WindowsFirstbootScaffold

ESXi（\`internal/render/esxi.go\`，已存在）：
  AssembleESXiKickstart(ctx model.RenderContext) (string, error)
  RenderESXiBootCfg(template, bootCfgURLBase, answerFileURL string) (string, error)
  RenderESXiMenuLine(ctx model.RenderContext, mbootEFIPath, answerFileURL string) (string, error)
  RenderESXiStageSection / ClientLibESXi

Wave1 新增的导出（以仓库真实代码为准，**动手前先 Read 那些文件确认签名**）：
${EXPORTS.length ? EXPORTS.map((e, i) => '  ' + (i + 1) + '. ' + e).join('\n') : '  （无）'}

### 各轨提出的接线请求（逐条核对是否合理，不合理的在 findings 里说明并按你的判断处理）

${WIRING.length ? WIRING.map((w, i) => (i + 1) + '. ' + w).join('\n') : '（无）'}

### 三条硬约束

1. **ESXi 只支持 UEFI**（§0.3、附录 A.14）：UEFI 用 \`mboot.efi\`；传统 BIOS 需要
   syslinux 的 \`mboot.c32\` 加 iPXE 的 COMBOOT 支持，两条路径机制不同、不可互换。
   **\`model.FirmwareCompatible(esxi, bios)\` 已经返回 false**（M1 冻结的），
   \`internal/statemachine/precheck.go\` 也已经硬拒绝这个组合。
   **你的分派在收到 BIOS + ESXi 时也要拒绝**，不要产出一个注定失败的菜单。
2. **Windows 的菜单依赖固件模式**（§1.2、§4.5）：\`boot.wim\` 位宽须与固件匹配，
   所以同一 Windows 版本要按固件导两个 artifact_set。渲染时按
   \`ctx.ActualFirmware\`（机器**实际上报**的固件模式，不是任务记录值）选对应制品键。
3. **SUSE 绝不能路由进 kickstart**（SPEC 反复警示）：AutoYaST 是独立的 XML 格式，
   引导参数是 \`autoyast=\` 而非 \`inst.ks=\`。\`model.AnswerFileName\` 已规定 SUSE 是
   \`autoinst.xml\`。分派处必须是独立的 case。

### 测试要锁住（dispatch_test.go）

- **六个家族的分派各自命中正确的实现**（rhel/debian/ubuntu/suse/windows/esxi），
  用最小的 RenderContext 分别调 \`RenderMenu\` 与 \`RenderAnswerFile\`，
  断言**不返回「未实现」错误**且产出非空
- **BIOS + ESXi 被拒绝**且错误信息可读（说清「这台机器是 BIOS，任务要装 ESXi」）
- **SUSE 的产出不是 kickstart**：断言产出里有 AutoYaST 的 XML 特征、
  没有 kickstart 的特征（比如 \`%packages\`）
- Windows 的 UEFI 与 BIOS 各自选到对应的 WinPE 制品键
- **未知家族仍然报明确错误**（不要因为加了 case 就把 default 分支弄丢）
- **既有四个家族的 golden file 不得变化**（跑一次 \`go test ./internal/render/ -run Golden\`
  确认，变了说明你改动了实现而不只是分派）

### 另外

\`model.AnswerFileName\` 规定了六个家族各自的应答文件名
（rhel→ks.cfg、ubuntu→user-data、debian→preseed.cfg、**suse→autoinst.xml**、
windows→unattend.xml、esxi→boot.cfg）。\`RenderAnswerFile\` 要校验传入的文件名
等于上下文算出的文件名（Ubuntu 额外接受 \`meta-data\`）—— 读现有实现，
**新家族要一并纳入这个校验**，否则机器能用路径段指定自己的应答文件类型。`,
  { label: 't08:dispatch', phase: 'Wave2', schema: R, effort: 'high' })

phase('Wave3')

const W3CTX = '\n\n## 前两波已完成的事（以仓库真实代码为准，动手前先 Read）\n' +
  'Wave1 十一轨的产出摘要：\n' +
  ok1.map((r, i) => '  轨' + (i + 1) + '：' + (r.files || []).slice(0, 4).join(', ') +
    ((r.files || []).length > 4 ? ' 等 ' + r.files.length + ' 个文件' : '')).join('\n') +
  '\n\nWave2（分派接线）：' + (t08 ? (t08.files || []).join(', ') + '\n结果：' + String(t08.verifyOutcome || '').slice(0, 300) : '失败') +
  (GAPS1.length ? '\n\n各轨记录的契约缺口（可能影响你）：\n' + GAPS1.slice(0, 8).map((g, i) => '  ' + (i + 1) + '. ' + String(g).slice(0, 200)).join('\n') : '')

const E2E = `### 你的验证方式：写一个该家族的端到端脚本

现有 \`scripts/e2e.sh\` 是 RHEL 的，九阶段全过（**只读它、照它的结构写，不要改它** ——
它是 MVP 的验收基线，改坏了会掩盖回归）。它的做法与两处已知绕法：
1. **不依赖 jq**（本机没有；curl 与 sqlite3 有），用 shell 文本处理解析 JSON
2. **用 \`sqlite3\` CLI 直接写 artifact_sets 表**绕过导入管线 —— **但本轮 t01 已经把导入
   管线接上了**，所以你应该**优先走真实的登记→导入→ready 路径**，走不通再退回 sqlite3
   绕法并在报告里写明为什么走不通（那是一个真实的发现）。

九个阶段的形状（照抄结构，内容按你的家族调整）：
  1 造制品树/二进制/admin 用户/配置  2 起服务  3 登录  4 造主机+制品集+模板
  5 preview 渲染  6 四条准入负面用例（陌生机器/MAC 不匹配/架构不符/retry 拒绝）
  7 /v1/boot 命中签 token  8 取应答文件 + 装机期七信号 + 重启后四信号
  9 终态 done + 装完再引导拿 sanboot + 应答文件不再提供 + 事件时间线

**四条安全负面用例一条都不能省**（§9.1 三道闸门 + §10 失败模式 1/2b）：
- 陌生机器请求 \`/v1/boot\` → \`no_task\`，响应体**逐字符含那三行 sanboot 脚本**
- **SN 对但 MAC 不对 → 拒签 token**（双因子，§4.3 硬要求）
- 架构/固件不符 → 硬拒绝且**不消耗 attempt_count**（§4.6：重试必然同样失败）
- **装完的机器再次引导拿到 sanboot 而不是装机菜单**（§10 失败模式 1：BIOS/MBR 下
  没有 efibootmgr，装完的机器完全靠这一行回本地盘）

`

const w3 = await parallel([
  () => agent(mk('t04-e2e-deb') + W3CTX + `## 你的任务：Debian 端到端验证（渲染层就绪但从没端到端跑过）

**当前状态**：\`internal/render/preseed.go\` 有 20 条测试与 golden file，
\`internal/imports/extract_debian.go\` 本轮由 t02 加固，但 **Debian 从没端到端跑过** ——
「渲染器测试全绿」与「能从 ISO 装出一台 Debian」之间隔着导入、分派、菜单、
应答文件供给、信号全链路。

你拥有的文件（只有这些）：
  scripts/e2e-debian.sh（新建）
  scripts/e2e/mkfixture-debian.sh（新建，若需要）
  docs/real-machine/12-e2e-debian.md（你的报告，新建）

**不要碰** scripts/e2e.sh（RHEL 的基线）、任何 Go 代码。
**发现 Go 侧的 bug 就写进 findings 与 contractGaps**，由编排方处理 —— 本轨是验证轨，
不是修复轨。这条很重要：你若顺手改了 Go 代码，会与另外三条 e2e 轨抢文件。

${E2E}### Debian 特有的要点

- 应答文件是 \`preseed.cfg\`，内核参数是 \`preseed/url=\`。**token 走路径段不走 query**
  （§4.4：\`preseed/url=\` 里带 \`&\` 在内核命令行上容易出事）。
- 引导制品是 \`linux\` + \`initrd.gz\`（不是 RHEL 的 \`vmlinuz\` + \`initrd.img\`）。
- hook 阶段映射（§5.2）：pre → \`preseed/early_command\`、
  post-nochroot → \`preseed/late_command\`、post → \`late_command\` + chroot、
  firstboot → systemd oneshot。**验证渲染出的 preseed 里这四段真的都在**。
- 造假制品树时，文件名要与 \`extract_debian.go\` 抽取出的路径一致
  （读那个文件确认它产出什么路径键）。`, { label: 't04:e2e-debian', phase: 'Wave3', schema: R, effort: 'high' }),

  () => agent(mk('t05-e2e-ubuntu') + W3CTX + `## 你的任务：Ubuntu 端到端验证（渲染层就绪但从没端到端跑过）

**当前状态**：\`internal/render/autoinstall.go\` 有 21 条测试与 golden file，
\`internal/imports/extract_ubuntu.go\` 本轮由 t03 加固，但 **Ubuntu 从没端到端跑过**。

你拥有的文件（只有这些）：
  scripts/e2e-ubuntu.sh（新建）
  scripts/e2e/mkfixture-ubuntu.sh（新建，若需要）
  docs/real-machine/13-e2e-ubuntu.md（你的报告，新建）

**不要碰** scripts/e2e.sh、scripts/e2e-debian.sh（归 t04）、任何 Go 代码。
发现 Go 侧 bug 写进 findings，不要自己改。

${E2E}### Ubuntu 特有的要点（两个坑）

1. **\`ds=nocloud-net;s=<url>\` 要求 URL 是目录且结尾带 \`/\`**（§4.4）。
   \`model.BaseURLs\` 的拼接已保留结尾斜杠 —— **端到端要真的验证这一点**：
   取应答文件的请求路径结尾有没有那个斜杠，斜杠被吃掉的现场表现是
   「机器装到一半停住」，极难定位到一个斜杠。
2. **那个目录下需要有 \`user-data\` 与 \`meta-data\` 两个文件**。
   M5 的 answer 端点已经额外接受 \`meta-data\`（读 \`internal/gateway/answer.go\` 确认）。
   **端到端要分别取这两个文件并断言都成功** —— 只测 \`user-data\` 会漏掉
   cloud-init 实际会取两次这个事实。

- hook 阶段映射（§5.2）：pre → \`early-commands\`、
  post-nochroot → \`late-commands\`（curtin 目标）、post → \`late-commands\` in-target、
  firstboot → cloud-init \`runcmd\`。**验证渲染出的 YAML 里这四段真的都在**。
- **Ubuntu 是 YAML，缩进敏感**：注入的 shell 片段若缩进错了，cloud-init 直接解析失败。
  端到端要把取到的 \`user-data\` 用一个 YAML 解析器验一次（Python 的 yaml 模块可能没有，
  可以用 Go 写个小程序，或用 \`go run\` 临时跑 yaml.v3 —— 它在 go.mod 里）。`,
    { label: 't05:e2e-ubuntu', phase: 'Wave3', schema: R, effort: 'high' }),

  () => agent(mk('t09-win-e2e') + W3CTX + `## 你的任务：Windows 端到端验证（分派刚接上，代码从没跑到过）

**当前状态**：\`internal/render/{unattend,winpe}.go\` 连测试带 golden 都在，
但**分派处直到本轮 Wave2 才接上** —— 在那之前那些代码一行都跑不到。本轨验证它真的能用。

你拥有的文件（只有这些）：
  scripts/e2e-windows.sh（新建）
  scripts/e2e/mkfixture-windows.sh（新建，若需要）
  docs/real-machine/14-e2e-windows.md（你的报告，新建）

**不要碰** scripts/e2e.sh、其它 e2e 脚本、任何 Go 代码。发现 Go 侧 bug 写进 findings。

${E2E}### Windows 特有的要点（与 Linux 是两套机制，不要套用）

- **wimboot 只能引导 WinPE，不能引导完整 Windows**（附录 A.6）。所以 Windows 的
  「镜像封装」实质是维护 \`install.wim\` 加 \`unattend.xml\`，**真正的安装动作由 WinPE 内的
  DISM 或 setup.exe 执行** —— 与 Linux 的 installer-driven 是两套机制。
- 菜单形态（§4.5）：\`wimboot\` 作 kernel，**四个 initrd 行**（BCD / boot.sdi / boot.wim，
  加 wimboot 自己）。端到端要断言菜单里这几行都在。
- **unattend 由 WinPE 内 startnet.cmd 去取，不烧进 WIM** —— 这样同一份 WIM 能服务所有机器。
- **固件维度是硬约束**（§0.3、§2.1）：\`boot.wim\` 位宽须与固件匹配，
  所以同一 Windows 版本要按固件导**两个** artifact_set（\`firmware\` 取具体值不是 \`any\`）。
  **端到端要验证 UEFI 与 BIOS 各自选到对应的制品键**，以及固件不匹配时的行为。
- **信号上报不能假设有 curl**：WinPE 里用 PowerShell 或 certutil。
  读 \`WinPEClientLib\` 看它生成什么，端到端用等价的方式模拟打点。
- firstboot 对应 \`%WINDIR%\\Setup\\Scripts\\SetupComplete.cmd\` 或 oobeSystem 的
  \`FirstLogonCommands\`（§5.2 末段）。
- **unattend.xml 是 XML**：验证取到的应答文件能被 XML 解析器解析
  （\`encoding/xml\`，用 \`go run\` 临时跑）。恶意输入的 XML 转义是否正确也要验一次。

**注意**：\`internal/imports/extract_windows.go\` 存在但 \`identify.go\` 把 Windows 列在
\`mvpUnsupportedFamilies\` 里（导入会被挡）。若本轮 t01/t02 没有解除这个限制，
你的端到端就走不通真实导入路径 —— 那时用 sqlite3 绕法造制品集，并**在报告里明确写出
「Windows 的导入路径仍被 mvpUnsupportedFamilies 挡着」这个发现**。`,
    { label: 't09:e2e-windows', phase: 'Wave3', schema: R, effort: 'high' }),

  () => agent(mk('t10-esxi-e2e') + W3CTX + `## 你的任务：ESXi 端到端验证（分派刚接上，代码从没跑到过）

**当前状态**：\`internal/render/esxi.go\` 连测试带 golden 都在，
但**分派处直到本轮 Wave2 才接上**。本轨验证它真的能用。

你拥有的文件（只有这些）：
  scripts/e2e-esxi.sh（新建）
  scripts/e2e/mkfixture-esxi.sh（新建，若需要）
  docs/real-machine/15-e2e-esxi.md（你的报告，新建）

**不要碰** scripts/e2e.sh、其它 e2e 脚本、任何 Go 代码。发现 Go 侧 bug 写进 findings。

${E2E}### ESXi 特有的要点

- **ESXi 只支持 UEFI，这是 MVP 的明确边界**（§0.3、附录 A.14）：UEFI 用 \`mboot.efi\`；
  传统 BIOS 需要 syslinux 的 \`mboot.c32\` 加 iPXE 的 COMBOOT 支持，两条路径机制不同。
  **端到端必须有一条用例验证 BIOS + ESXi 被硬拒绝**（§4.6，\`precheck.go\` 已实现），
  且错误信息可读（说清「这台机器是 BIOS，任务要装 ESXi」）。
- **\`boot.cfg\` 由网关渲染，不放 nginx 静态**（§2.1）：因为它含 per-task 的 ks URL。
  三处改写要验证：\`prefix=\` 改写成 HTTP 路径、\`kernelopt=ks=\` 填应答文件地址、
  **模块路径前的 \`/\` 要去掉**（这条最容易漏，漏了安装器找不到模块）。
- 菜单形态（§4.5）：\`mboot.efi -c <boot.cfg URL>\`。
- **ESXi 的 kickstart 与 RHEL 的 kickstart 语法不同**（它是 ESXi 自己的方言，
  且 \`%firstboot\` 是原生支持的 —— §5.2 的阶段映射表里 ESXi 的 post-nochroot 一栏是「—」，
  即不存在那个阶段）。**端到端要验证产出的不是 RHEL kickstart**。
- 脚本位置靠 \`ks=filepath\` 指定，不设则脚本化安装无法启动（附录 A.8）。
- hook 的 interpreter 是 \`ash\`（\`model.Hook.Interpreter\` 已有这个取值）。
- syslog 用 \`esxcli system syslog config set --loghost=\`（§6.5）。

**注意**：\`identify.go\` 把 ESXi 列在 \`mvpUnsupportedFamilies\` 里（导入会被挡）。
若本轮没解除，用 sqlite3 绕法造制品集，并**在报告里明确写出这个发现**。`,
    { label: 't10:e2e-esxi', phase: 'Wave3', schema: R, effort: 'high' }),
])

const ok3 = w3.filter(Boolean)

return {
  wave1: ok1.map(r => ({ files: (r.files || []).length, commits: r.commits, outcome: String(r.verifyOutcome || '').slice(0, 400), gaps: r.contractGaps, unfinished: r.unfinished })),
  wave2_dispatch: t08 ? { files: t08.files, commits: t08.commits, outcome: t08.verifyOutcome, findings: t08.findings, gaps: t08.contractGaps, unfinished: t08.unfinished } : '失败',
  wave3_e2e: ok3.map(r => ({ files: r.files, commits: r.commits, outcome: String(r.verifyOutcome || '').slice(0, 500), findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished })),
  allGaps: GAPS1.concat(t08 ? t08.contractGaps || [] : []).concat(ok3.flatMap(r => r.contractGaps || [])),
}


