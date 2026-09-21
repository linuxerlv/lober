export const meta = {
  name: 'lober-wave3-m9-phase23',
  description: 'M9 assembly and end-to-end, plus Phase 2/3 module increments, partitioned by file ownership so no two agents touch the same file',
  phases: [
    { title: 'Build', detail: 'M9 装配与 Phase 2/3 增量并行，按文件归属分区' },
    { title: 'Verify', detail: '各轨独立验证者证伪并修复' },
  ],
}

const PRE = `你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区是一个独立的 git worktree

  worktree：__WT__
  分支：    __BRANCH__

**每个文件操作、每条命令都在你的 worktree 里**（用绝对路径，或先 \`cd __WT__\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 那里有别的 agent 在并行工作。

## 仓库现状：MVP 七个模块已全部合并到 dev 并验证通过

你的 worktree 里有完整的 MVP：

  internal/core/model/**         领域类型（TaskState/FailureReason/Phase/信号表/预算表/固件矩阵/24 个 fact 键/RenderContext）
  internal/core/contract/**      跨模块接口（Store 窄接口族/StateMachine/Renderer/Prechecker/Clock/七个 provider/sentinel error）
  internal/core/contract/conformance/store.go   Store 行为契约套件
  internal/core/fake/**          内存 Store + 可控时钟
  internal/store/sqlite/**       SQLite 实现（通过 conformance 套件）
  internal/statemachine/**       状态机、token 签发、Prechecker
  internal/render/**             渲染器（RHEL/kickstart + iPXE 菜单 + guard 注入防护）
  internal/gateway/**            机器面六端点 + syslog UDP
  internal/console/**            人面全端点 + RBAC + SSE + 审计
  internal/console/ui/**         go:embed handler + 占位 dist
  internal/provider/**           七个 provider 内置实现 + 同步决策编排 + 事件总线
  internal/imports/**            ISO 导入（识别/抽取/manifest/job）
  internal/apierr/** internal/config/** internal/archtest/** Makefile
  deploy/** pipeline/** docs/secureboot-poc/**   部署产物与调研（另一波刚产出，可能还在写）
  docs/contracts/{http-machine,http-console,filesystem}.md   冻结的契约
  docs/superpowers/plans/**      全部实现计划

全仓 \`go test ./...\` 与 \`-race\` 当前全绿（16 个包），\`gofmt -l .\` 干净，archtest 三条依赖方向守卫通过。**你的改动不许打破这个状态。**

必读（用 Read 工具，不要 cat）：
- 你的计划（路径见下）—— 逐任务实现，它是你的唯一实现依据
- 你要扩展的那个包的现有代码。**动手前必读**：你是在别人写好的代码上做增量，不是从零写。要复用它已有的骨架与命名，不要另起一套。
- SPEC.md 里你的计划引用到的章节（按需精读，1765 行，不要全文读）

## 铁律

1. **只写你拥有的文件**（下面明确列出）。本轮七个 agent 按文件归属分区，你碰了别人的文件就会制造合并冲突。**特别注意 internal/render/menu.go 与 internal/provider/factory.go 这类被多个计划同时点名的文件 —— 只有被明确分配到它的 agent 能改。**
2. **契约只读**：禁止修改 internal/core/**、internal/apierr/**、internal/config/**（除非你被明确分配）、internal/archtest/**、docs/contracts/**、Makefile、go.mod、go.sum、internal/store/sqlite/schema/**。发现契约缺口时不要加变通，在返回值 contractGaps 里写清「缺什么、你怎么保守绕的、正解要改哪个接口」。
3. **不要修改别的模块的既有行为。** 你是做增量：加新文件、在既有文件里加新分支/新函数。若必须改既有函数，改动要最小且**不得让任何现存测试变红**。
4. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir()。**绝不创建与真实模块同名的临时目录** —— 之前有 agent 这样做，清理时把真实代码一起删了。
5. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 任何带 -f 或 --hard 的命令。合并由编排方做。
6. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。可用依赖：modernc.org/sqlite、go-cmp、uuid、yaml.v3、golang.org/x/crypto、golang.org/x/net。需要别的就用标准库并在 contractGaps 说明。
7. **import 白名单**：model、contract、标准库，加上你的计划明确允许的。**实现模块之间绝不互相 import**（store/statemachine/render/gateway/console/provider/imports 互不可见）——archtest 会拦住，**唯一豁免是 cmd/lober 装配点**。
8. 先写测试再写实现。计划里「测试要锁住的行为」逐条要有对应测试，断言失败信息写明「为什么这条重要」加 SPEC 章节号。
9. 中文注释、文档与断言信息；标识符与文件名用英文。gofmt 干净。
10. 完成前必须真的执行验证并看到通过输出。**验证要包含你改动的包 + 你可能影响到的包**。不要声称通过。
11. 你的返回值是数据，不是给人看的汇报。

`

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    commits: { type: 'array', items: { type: 'string' } },
    verifyCommands: { type: 'array', items: { type: 'string' } },
    verifyOutcome: { type: 'string', description: '逐条验证结果；哪条没过、哪条跑不了及原因' },
    acceptance: { type: 'array', items: { type: 'string' }, description: '计划交付验收清单逐条：✓/✗ 条目 —— 证据' },
    contractGaps: { type: 'array', items: { type: 'string' } },
    unfinished: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string', description: '后续阶段与合并时必须知道的事' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'acceptance', 'contractGaps', 'unfinished', 'notes'],
}

const TRACKS = [
  {
    key: 'M9-assembly',
    dir: 'm9-assembly',
    label: 'M9:assembly+e2e',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-09-integration.md',
    pkgs: './cmd/...',
    owns: `cmd/lober/{wire.go,preflight.go,scanner.go,server.go,main.go} 与各自的 _test.go
  scripts/e2e.sh、scripts/e2e/mkfixture.sh
  **注意：deploy/ 下的配置（nginx/dnsmasq/dhcpd/kea/systemd/lober.yaml.example）与 scripts/mkuser/ 已由另一波 agent 产出，你的 worktree 里可能已经有了。读它们、用它们，但不要重写。若发现缺失或有错，在 contractGaps 里记录，不要自己补一份。**`,
    brief: `## 你的任务：M9 装配与端到端（计划任务 1-5 与 9-10，跳过 6/7/7b/8）

**计划任务 6（nginx）、7（dnsmasq）、7b（dhcpd/Kea）、8（systemd+配置样例+mkuser）由另一波 agent 负责，已经或正在产出到 deploy/ 与 scripts/mkuser/。不要做这四个任务。** 你做任务 1-5（Go 装配）与任务 9-10（端到端脚本与部署文档的装配相关部分；如果 deploy/README.md 已存在就不要覆盖它，只在 contractGaps 里指出需要补充什么）。

**你是唯一能 import 所有实现模块的包**（archtest 对 cmd/lober 豁免）。这是装配点的职责。

任务 1（装配）：把七个模块接到一起。构造顺序有依赖：先 Store（sqlite.Open 拿 *sql.DB → sqlite.New 包成 *Store）、再 Clock/TokenIssuer/Prechecker、再 StateMachine、Renderer、provider 工厂与同步决策编排器、最后两个 HTTP Server。**M2 留的装配说明**：dsn 要形如 \`file:/path/to/db.sqlite\`（不要漏 file: 前缀，Open 内部会拼四个必需 pragma）。构造失败要给出能定位到具体依赖的错误。

任务 2（启动自检）：**别等机器来了才发现制品缺文件**（SPEC §10 运维点一）。校验 ready 状态制品集的 paths 里每个文件真实存在于盘上，路径按 docs/contracts/filesystem.md。缺文件要指出是哪个制品集的哪个路径。
**这里有一个已知缺口要你处理**：M3 的 Prechecker 需要一个指向制品根目录的 io/fs.FS，但 Machine 的依赖里没这一项，M3 用空文件系统占位（\`artifactFilesFS()\` 返回 emptyFS），后果是**经 StartBoot 触发的制品完整性校验目前对任何非空路径都判定为缺失、直接拒绝引导**。先读 internal/statemachine/boot.go 与 precheck.go 确认现状，然后在装配点用 os.DirFS(制品根目录) 注入真实文件系统 —— 如果 Machine 的构造函数不接受这个参数，**这是一个真实的契约缺口**：在 contractGaps 里写清，并采取最保守的可行做法（例如装配点自己构造 Prechecker 并注入，而不改 Machine 的签名；若连这也做不到就如实记录「MVP 的引导路径在装配后仍会因此拒绝所有引导」——这是必须让上层知道的阻塞级问题，不许悄悄放过）。

任务 3（超时扫描循环）：独立于请求路径，每 30 秒扫一次（间隔从配置来）。**不用 in-process timer**——进程重启定时器全丢（SPEC §3.3）。要能被 context 取消并干净退出。测试要用 -race 跑。

任务 4（两个 listener 加一个 UDP listener 的生命周期）：机器面与人面绑各自的具体 IP（配置已有通配地址守卫），syslog 绑 UDP 514。优雅关闭：收到信号后停止接受新连接、等在途请求结束（带超时）、关扫描循环。**两个 listener 必须是同一个进程**（SPEC §1.1：状态机必须有唯一所有者，两个进程会立即撞上状态转换竞态与 reconcile 循环重复执行）。

任务 5（入口与运行模式）：main.go 带 -version 与配置校验模式（systemd 的 ExecStartPre 要用它）。版本信息用 -ldflags 注入。

任务 9（端到端验证脚本 scripts/e2e.sh）：**这是整个 MVP 的验收标准**（SPEC §11 验证行：curl 模拟机器打点，整条链本地跑通，不碰硬件）。九个阶段逐个打印通过标记，最后输出完整状态链。要覆盖：建任务 → /v1/boot 命中签 token（断言 state=booting）→ 取应答文件（断言 installing）→ 逐个信号打点（pre-start/pre-done/packages-*/post-*/rebooting）→ booted/init-*/ready → 断言终态 done。另外必须覆盖三条安全路径：**无任务的机器请求 /v1/boot 得到 no_task 且响应体逐字符含那三行 sanboot**、**MAC 不匹配不签 token**、**rebooting/initializing 超时的失败态 retry 被拒绝**。脚本在 Windows 的 bash 下要能跑（用 curl 与 go run，不要依赖 Linux 专有工具）。

验证：\`go build ./cmd/...\`、\`go test ./cmd/... -v\`、\`go test ./cmd/... -race\`、\`gofmt -l cmd scripts\`、**\`go test ./... \` 全仓全绿（你是装配点，必须确认没打破任何模块）**、\`go test ./internal/archtest/ -v\`（装配点那条断言此刻应该从 SKIP 变成真正执行并通过）、最后真的跑一遍 \`bash scripts/e2e.sh\` 并把输出要点写进 verifyOutcome。`,
  },
  {
    key: 'P2-multios',
    dir: 'p2-multios',
    label: 'P2:multi-os',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase2-01-multi-os.md',
    pkgs: './internal/render/... ./internal/imports/',
    owns: `internal/render/{autoinstall.go,preseed.go} 与测试与 golden file
  internal/render/menu.go（**本轮只有你能改它** —— Windows 与 ESXi 两轨被告知不许碰）
  internal/render/renderer.go（只允许在分派处加分支）
  internal/render/skeleton.go（若需要把 RHEL 专用逻辑抽成跨家族共用，改动要最小且不让 RHEL 的 golden 变化）
  internal/imports/{extract_ubuntu.go,extract_debian.go} 与测试`,
    brief: `## 你的任务：Ubuntu autoinstall 与 Debian preseed（phase2-01 全部任务）

**先读 internal/render/ 的全部现有代码**，尤其 kickstart.go、skeleton.go、params.go、clientlib.go、firstboot.go、menu.go 与 guard/。M4 已经把 RHEL 家族做完了，你要复用它的三段骨架、参数内联、注入的 signal()/run_hook、firstboot 脚手架与注入防护 —— **不要另起一套**。

核心是 SPEC §5.2 的跨家族阶段映射（这张表是保持一套 hook 库通用的关键）：

| 阶段 | RHEL kickstart | Ubuntu autoinstall | Debian preseed |
|---|---|---|---|
| pre | %pre | early-commands | preseed/early_command |
| post-nochroot | %post --nochroot | late-commands（curtin 目标） | preseed/late_command |
| post | %post | late-commands in-target | late_command + chroot |
| firstboot | systemd oneshot | cloud-init runcmd | systemd oneshot |

要点：
- **阶段不是排序，是能力边界**（§5.2）。放错阶段的 hook 不是顺序错了，是根本跑不起来。
- Ubuntu 是 YAML（autoinstall / cloud-init NoCloud），**缩进敏感**——注入的 shell 片段必须正确缩进进 YAML 块，这与 kickstart 的纯文本拼接是两种活。用 YAML 的块标量语法，不要手工拼缩进。
- Ubuntu 的菜单要 \`ds=nocloud-net;s=<url>\`，**URL 必须是目录且结尾带 \`/\`**（§4.4）——model.BaseURLs 的拼接已保留结尾斜杠，别再手工处理掉。那个目录下需要有 user-data 与 meta-data 两个文件（M5 的 answer 端点已经额外接受 meta-data）。
- Debian 的 \`preseed/url=\` 里**带 & 在内核命令行上容易出事**（§4.4），所以 token 走路径段。
- 注入防护对三个家族都要生效：form_data 的值会进应答文件，必须走 guard 的类型白名单。**YAML 与 kickstart 的转义规则不同**，要各自验证恶意输入不逃逸（至少覆盖换行注入与 YAML 结构破坏）。
- 抽取器：extract_ubuntu.go 按 \`.disk/info\` 识别、extract_debian.go 按 \`dists/\` 识别（§2.1 的特征文件表，identify.go 已有识别逻辑，读它）。抽取逻辑**通过内存文件系统测试，不依赖真 ISO**（这是 M7 已立的规矩）。
- golden file：三个家族各自的 golden，**改动不得让 RHEL 的现有 golden 变化**（若变化说明你动了共用骨架的行为，那是回归）。

验证：\`go test ./internal/render/... ./internal/imports/ -v\`、\`-race\`、\`gofmt -l internal/render internal/imports\`、**\`grep -rn 'http://' internal/render/ --include=*.go\` 只允许在测试期望值里出现**、以及 \`go test ./... \` 确认没打破别的包（尤其 gateway 与 console 会调渲染器）。`,
  },
  {
    key: 'P2-inspect',
    dir: 'p2-inspect',
    label: 'P2:inspect+facts',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase2-02-inspect.md',
    pkgs: './internal/provider/ ./internal/render/',
    owns: `internal/provider/{hwfacts.go,nicplanner.go} 与测试
  internal/provider/resolve.go（只允许加新决策步骤，不改既有步骤行为）
  internal/render/inspect.go 与测试
  **不要改 internal/render/menu.go（归 P2-multios）、不要改 internal/provider/factory.go（归 P2-loaders，虽然本轮没跑，但保持归属）、不要改 internal/gateway/facts.go 与 internal/console/dryrun.go（计划提到它们，但它们归别人；需要它们配合时在 contractGaps 里写清）**`,
    brief: `## 你的任务：inspect 阶段硬件采集与服务端决策（phase2-02，限于 provider 与 render 两个包）

**先读**：internal/provider/ 的全部现有代码（尤其 diskplanner.go 的五个策略与 resolve.go 的同步决策编排）、internal/render/ 的 params.go 与 kickstart.go（fact 注入在那里）、internal/core/model/facts.go（24 个 fact 键是冻结的对外契约）。

核心原则（SPEC §5.6）：**不要在模板里硬编码 /dev/sda 或 eth0。** 正确顺序是：
1. pre 阶段采集 lsblk / lspci / dmidecode / ethtool，上报为 fact
2. **服务端**用采集结果决策：按容量/型号/rotational/by-path 选盘，按 MAC/PCI 路径/link 状态选网卡
3. 再渲染应答文件，把具体设备标识写死进去 —— **用 by-id / by-path / WWN，不用内核分配的名字**（§10 失败模式 5：磁盘设备名漂移会装到错误的盘）

**为什么决策在服务端而不在 %pre 里算**：推到机器上则失败时无日志可看、无法复现；服务端决策可调试、可 dry-run、可审计。这是 §5.6 的明确要求，不要为了省事把逻辑写进 shell。

你的三块产出：
- **render/inspect.go**：渲染注入到 %pre（以及 Ubuntu/Debian 对应阶段，若 P2-multios 的骨架已合并就复用，否则只做 RHEL）的采集脚本。它要把采集结果 POST 到 facts 端点（路径见 docs/contracts/http-machine.md 的 facts 端点，**不要自己发明路径**）。脚本在 installer ramdisk 里跑，**没有 python、没有 jq，curl 可能只有 busybox 版**（§6.2）——只能用 shell 内建与 coreutils，输出形态要让服务端零成本解析。
- **provider/hwfacts.go**：把上报的原始文本解析成结构化硬件事实（磁盘列表带容量/型号/rotational/by-id、网卡列表带 MAC/PCI 路径/link 状态）。**解析要对畸形输入健壮**——机器侧的采集脚本在各种硬件上会产出意外格式，解析失败要给出能定位的错误而不是 panic。
- **provider/nicplanner.go**：按 MAC / PCI 路径 / link 状态选网卡，与 diskplanner 同构（纯函数、无 IO、无时钟、无随机数、返回选择理由）。**网卡命名漂移会让装完的机器网络不通、需要人到现场**（§10 失败模式 6），所以要 MAC 绑定 + 固化 net.ifnames 策略。

已知契约缺口（M7 已记录，你会撞上）：contract.HWFacts 只有 OSFamily/Vendor/Product/Arch 四个字段，没有 Version；TaskParamsResolver.Resolve 的 facts 是扁平 map[string]string 而结构化硬件事实不是扁平的。**用最保守的方式绕（例如在 provider 包内部定义自己的结构化类型，只在跨接口边界处扁平化），并在 contractGaps 里写清正解。**

验证：\`go test ./internal/provider/ ./internal/render/ -v\`、\`-race\`、\`gofmt -l\`、纯函数自查（nicplanner 所在文件无 IO/时钟/随机数）、\`go test ./... \` 确认没打破别的包。`,
  },
  {
    key: 'P3-windows',
    dir: 'p3-windows',
    label: 'P3:windows',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase3-01-windows.md',
    pkgs: './internal/render/ ./internal/imports/',
    owns: `internal/render/{unattend.go,winpe.go} 与测试与 golden
  internal/imports/extract_windows.go 与测试
  **绝对不要改 internal/render/menu.go** —— 它归 P2-multios 这一轨。Windows 的菜单形态你写在 winpe.go 里作为独立函数导出，并在 notes 里写明「menu.go 的分派需要加一行调用它」，由编排方在合并后接线。这是为了避免三个计划抢同一个文件。`,
    brief: `## 你的任务：Windows 装机通道（phase3-01）

**先读**：internal/render/ 的现有代码（kickstart.go 的三段结构、menu.go 的菜单形态、firstboot.go）、internal/imports/identify.go 与 extract.go、internal/core/model/inventory.go 的 ArtifactSet 与固件矩阵、SPEC §4.5 的 Windows 菜单、§0.3 与 §2.1 的固件维度、附录 A.4-A.6。

**Windows 与 Linux 是两套机制，不要套用 kickstart 的思路：**
- wimboot 通过 HTTP 而非 TFTP 引导 WinPE，千兆网下典型 WinPE 镜像数秒下完（附录 A.4）
- 标准流程是 HTTP 引导进 WinPE，再由 WinPE 内的 DISM 或 setup.exe 完成安装（附录 A.5）
- **wimboot 只能引导 WinPE，不能引导完整 Windows**（附录 A.6）。所以 Windows 的「镜像封装」实质是维护 install.wim 加 unattend.xml，真正的安装动作由 WinPE 内执行 —— 与 Linux 的 installer-driven 是两套机制。
- 菜单要四个 initrd 行（BCD / boot.sdi / boot.wim / 加 wimboot 作 kernel），形态见 §4.5
- **unattend 由 WinPE 内 startnet.cmd 去取，不烧进 WIM** —— 这样同一份 WIM 能服务所有机器

**固件维度是硬约束**（§0.3、§2.1）：boot.wim 位宽须与固件匹配，所以**同一 Windows 版本需导两个 artifact_set**（firmware 取 uefi 或 bios 的具体值，不是 any）。model.ArtifactSet.Firmware 已经承载这个维度，渲染时按机器**实际上报**的固件模式（RenderContext.ActualFirmware，不是任务记录值）选对应制品键 —— 这是 §4.6 校验的一部分。

**unattend.xml 用 html/template 而非 text/template**（§5.6）：它是 XML，需要 XML 转义。form_data 的值会进 unattend，仍要走 guard 的类型白名单 —— 但 XML 转义与 shell 转义是两回事，**两层都要**。

firstboot 对应 \`%WINDIR%\\Setup\\Scripts\\SetupComplete.cmd\`（以 SYSTEM 身份在首次登录前执行）或 oobeSystem pass 的 FirstLogonCommands（§5.2 末段）。信号上报要用 PowerShell 或 certutil 之类 Windows 自带的东西，**不能假设有 curl**。

抽取：按 \`sources/install.wim\` 识别（§2.1 特征文件表）。内存文件系统测试，不依赖真 ISO。

验证：\`go test ./internal/render/ ./internal/imports/ -v\`、\`-race\`、\`gofmt -l\`、\`grep -rn 'http://' internal/render/ --include=*.go\` 只在测试期望值出现、\`go test ./... \` 确认没打破别的包（**尤其不要让 RHEL 的 golden 变化**）。`,
  },
  {
    key: 'P3-esxi',
    dir: 'p3-esxi',
    label: 'P3:esxi',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase3-02-esxi.md',
    pkgs: './internal/render/ ./internal/imports/',
    owns: `internal/render/esxi.go 与测试与 golden
  internal/imports/extract_esxi.go 与测试
  **绝对不要改 internal/render/menu.go** —— 归 P2-multios。ESXi 的菜单形态写在 esxi.go 里作为独立导出函数，在 notes 里写明「menu.go 的分派需要加一行调用它」，由编排方合并后接线。`,
    brief: `## 你的任务：ESXi 装机通道（phase3-02）

**先读**：internal/render/ 的现有代码、internal/imports/identify.go、internal/core/model/inventory.go 的固件兼容矩阵（**FirmwareCompatible(esxi, bios) 已经返回 false**，这是 M1 冻结的）、SPEC §4.5 的 ESXi 菜单、§0.3、§2.1 的 ESXi 特例、附录 A.7/A.8/A.14。

**ESXi 只支持 UEFI，这是 MVP 的明确边界**（§0.3、附录 A.14）：UEFI 用 mboot.efi；传统 BIOS 需要 syslinux 的 mboot.c32 加 iPXE 的 COMBOOT 支持（仅存在于 BIOS 构建，且部分预编译二进制未开启），两条路径机制不同、不可互换。**网关对 plat=pcbios + os_family=esxi 的组合直接硬拒绝**（§4.6，M3 的 Prechecker 已经实现了这条，读 precheck.go 确认）。你的渲染器在收到 BIOS 固件时也要拒绝，不要产出一个注定失败的菜单。

**boot.cfg 必须由网关渲染，不能放 nginx 静态**（§2.1）：因为它含 per-task 的 ks URL。渲染时三处改写：
1. \`prefix=\` 必须改写为 HTTP 路径
2. \`kernelopt=ks=\` 填应答文件地址（§4.5 的 \`mboot.efi -c <boot.cfg URL>\`）
3. **模块路径前的 \`/\` 要去掉** —— 这条最容易漏，漏了安装器找不到模块

kickstart 脚本可从 HTTP/HTTPS/FTP/NFS/CD-ROM/USB 获取（附录 A.7）；**脚本位置靠引导命令行 \`ks=filepath\` 指定，不设则脚本化安装无法启动**（附录 A.8）。

ESXi 的 kickstart 与 RHEL 的 kickstart **语法不同**（它是 ESXi 自己的方言，且 \`%firstboot\` 是原生支持的 —— 见 §5.2 的阶段映射表：ESXi 的 post-nochroot 一栏是「—」，即不存在这个阶段）。**不要把 RHEL 的 kickstart 渲染器直接套过来**，那是 SPEC 反复警告的错误（同样的警告也适用于 SUSE：都被口头叫「ks」但语法完全不同）。

syslog 用 \`esxcli system syslog config set --loghost=\`（§6.5）。信号上报在 ESXi 的 ash shell 里跑，hook 的 interpreter 是 \`ash\`（model.Hook.Interpreter 已有这个取值）。

抽取：按 \`boot.cfg\` + \`mboot.efi\` 识别（§2.1）。内存文件系统测试。

验证：\`go test ./internal/render/ ./internal/imports/ -v\`、\`-race\`、\`gofmt -l\`、\`grep -rn 'http://'\` 只在测试期望值出现、\`go test ./... \` 确认没打破别的包（尤其 RHEL golden 不变）、以及一条明确断言 **BIOS + ESXi 组合被拒绝**。`,
  },
  {
    key: 'P2-console',
    dir: 'p2-console',
    label: 'P2:console-p2',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase2-05-console-p2.md',
    pkgs: './internal/console/',
    owns: `internal/console/ 下与 P2 页面相关的新增 .go 文件与测试
  **不要改 internal/console/ui/**（归 M8）、不要改 internal/console/dryrun.go 的既有行为（只能加）**`,
    brief: `## 你的任务：控制台 P2 端点补齐（phase2-05）

**先读**：internal/console/ 的全部现有代码（M6 刚做完 MVP 的全部端点，你要在它之上补 P2 的部分）、docs/contracts/http-console.md（**冻结契约，端点路径与响应形态按它，不自己发明**）、internal/apierr（错误码的真实定义 —— 注意 M6 已记录：契约文档的错误码表与 apierr 实际实现不一致，**以 apierr 为准**，文档自己声明了这一点）。

SPEC §11 Phase 2 列的三个 P2 页面对应的服务端能力：hook 审批流转、制品集导入进度、磁盘 dry-run（MVP 期这三件用 API 直接顶，现在要补齐）。读 phase2-05 计划确认具体任务清单。

要点：
- **hook 审批是安全动作**（§9.4）：hook 以 root 身份在分区之前运行，能力等同「擦掉这台机器所有盘」。所以创建/编辑 hook 是独立权限（与建任务分开），**激活 pre 阶段 hook 需审批**（draft → review → active）。每个版本记作者、审批人、时间；**hook 正文不可变，改即新版本 + 新 digest**（§2.7：模板 pin 的是 digest，故审批过的内容不会被偷换）。
- 导入进度是异步 job 的状态查询（M7 的 imports 包已有 job 状态机，但**你不能 import 它** —— 通过 contract 的接口或 Store 读状态）。
- dry-run 要展示磁盘布局**与「为什么这么选」**（M7 的 DiskPlanner 结果已带说明字段）。§8.2 说 preview 与 dryrun 是开发期性价比最高的两个端点，把模板调试周期从「上电等十分钟」压到秒级。
- RBAC 按 §12.5 的五个角色与权限码表。**跨角色越权必须被拒绝**，要有测试。

验证：\`go test ./internal/console/ -v\`、\`-race\`、\`gofmt -l internal/console\`、\`go test ./... \` 确认没打破别的包、以及路由核对测试（契约文档里的端点都实现了）。`,
  },
  {
    key: 'P4-concurrency',
    dir: 'p4-concurrency',
    label: 'P4:concurrency',
    plan: 'docs/superpowers/plans/2026-09-20-lober-phase4-01-concurrency.md',
    pkgs: './internal/statemachine/ ./internal/provider/',
    owns: `internal/statemachine/admission.go 与测试
  internal/statemachine/scan.go（只允许加并发上限相关逻辑，**不得改既有的超时判定行为**）
  internal/provider/notify.go（只允许加告警接入相关内容）
  **不要改 internal/console/task.go（归 P2-console）、不要改 deploy/nginx/lober.conf（归 ops 那一波）**`,
    brief: `## 你的任务：并发限流与失败重试（phase4-01，限于 statemachine 与 provider 两个包）

**先读**：internal/statemachine/ 的全部现有代码（**尤其 machine.go 里 M3 的验证者加的「按 taskID 分片的进程内锁」—— 它是为了修一个真实的水位回退竞态，你的改动不得破坏它**）、scan.go 的超时扫描、internal/provider/notify.go、SPEC §10 失败模式 11（并发装机打满 HTTP）与 §3.3。

核心是 §10 失败模式 11 的规避手段：**状态机设 provisioning 并发上限与排队**。要点：
- 上限是「同时处于 installing 等消耗带宽阶段的任务数」，超过上限的引导请求要**排队而不是拒绝** —— 拒绝会让机器回到 PXE 轮询，运维看不出发生了什么。排队的表达方式要想清楚：iPXE 那边是一次 HTTP 请求，不能让它挂很久（§4.2 要求 /v1/boot 始终 200 且快速响应）。**一个可行方向是：超限时返回一个让机器稍后重试的 iPXE 脚本（sleep + chain 回 /v1/boot），而不是签 token。这不消耗 attempt_count** —— 因为它不是失败，是排队。**你要自己判断这个方向对不对，并在 notes 里说明理由。**
- 并发上限从配置来（config 已冻结，若没有这个字段就在 contractGaps 里记录，用一个包级默认值并留出注入口）。
- **失败自动重试**要与 §3.4 的禁止重装严格分开：reboot_timeout / init_timeout / init_failed 三种**绝不能自动重试**（机器已装好、盘上可能有数据）。自动重试只能作用于可重装的那六种，而且要有次数上限（attempt 上限已是 3）。**这条搞错会擦掉生产机的数据，是全系统最危险的逻辑。**
- 告警接入（notify.go）：按 §7.4 的失败语义，Notify 的失败**只告警、不影响任务状态**。§3.5 要求失败事件带六个字段（阶段/退出码/hook digest/日志尾部/syslog 尾部/距上次信号多久），告警文案要带上它们 —— \`state=failed\` 本身无用。另外 §3.4 明确要求：**initializing 失败的告警文案要与「装机失败」明确区分**，两者处理方式完全不同（前者是人工登录 touch 一个文件重跑，后者可以重装）。

验证：\`go test ./internal/statemachine/ ./internal/provider/ -v\`、**\`go test ./internal/statemachine/ -race -count=20\`（M3 的竞态是 1% 概率复现的，低 count 测不出来）**、\`gofmt -l\`、\`grep -rn 'time.Now()' internal/statemachine/\` 确认实现代码里仍然零处真实调用（时间全部来自注入的 Clock）、\`go test ./... \` 确认没打破别的包。`,
  },
]

const results = await pipeline(
  TRACKS,
  (t) => {
    const wt = 'E:/src/lober-wt/' + t.dir
    const pre = PRE.split('__WT__').join(wt).split('__BRANCH__').join('feat/' + t.dir)
    return agent(pre + '## 你的计划\n\n' + t.plan + '\n\n## 你拥有的文件（只有这些）\n\n  ' + t.owns + '\n\n' + t.brief +
      '\n\n## 提交\n\n全绿之后在你的 worktree 里 commit：`git add <你的文件>` 然后 `git commit -m "..."`（按计划给的 commit 信息，或 feat(<包>): ...）。可分多个 commit。**只 add 你自己的文件**，提交前用 `git status` 确认没夹带别人的东西。\n\n' +
      '最后把计划末尾的「交付验收」清单逐条核对填进 acceptance（每条给证据）。做不到的如实标 ✗ 并在 unfinished 说明。',
      { label: t.label, phase: 'Build', schema: R, effort: 'high' })
  },

  (impl, t) => {
    if (!impl) return { impl: null, verify: null }
    const wt = 'E:/src/lober-wt/' + t.dir
    return agent(`你是 Lober 项目 ${t.key} 这一轨的独立验证者与修复者。实现者刚写完并提交，你要证伪它，把真问题修掉，然后提交修复。

## 你的工作区

  worktree：${wt}
  分支：    feat/${t.dir}

**所有操作都在这个 worktree 里**（绝对路径或先 cd 进去）。绝不碰 E:\\src\\lober 主仓库或其它 worktree。

先读：
- ${t.plan} —— 尤其每个任务的「测试要锁住的行为」与末尾「交付验收」清单
- 实现者写的代码（文件清单见下）
- 它所扩展的那个包的既有代码 —— **判断实现者有没有破坏既有行为，这是本轮最重要的检查项之一**
- 相关契约：internal/core/contract/**、internal/core/model/**、docs/contracts/**（**只读，不许改**）

## 实现者的报告（不要相信这份自评，自己跑一遍）

  文件：${impl.files.join(', ')}
  提交：${(impl.commits || []).join(' | ') || '（未提交）'}
  验证：${impl.verifyOutcome}
  交付验收自评：${(impl.acceptance || []).join(' | ')}
  契约缺口：${(impl.contractGaps || []).join(' | ') || '（无）'}
  未完成：${(impl.unfinished || []).join(' | ') || '（无）'}
  说明：${impl.notes || ''}

## 按这个顺序做

1. **真的跑验证**：\`go vet ${t.pkgs}\`、\`go test ${t.pkgs} -v -count=1\`、\`go test ${t.pkgs} -race\`、\`gofmt -l <改动的目录>\`，**加上 \`go test ./... -count=1\` 全仓跑一遍** —— 这一轨是在已经全绿的 MVP 上做增量，**打破任何既有包的测试就是回归，必须发现并修掉**。
2. **对着计划逐任务查漏**：「测试要锁住的行为」是否真有对应测试？断言是否够强（不是只断言「不报错」）？有没有任务被整段跳过？有没有「测试通过但测的不是那件事」？
3. **重点证伪本轨适用的安全不变量**：
   - 自动重试是否严格排除三种不可重装的失败原因（reboot_timeout / init_timeout / init_failed）？**这条错了会擦掉生产机数据。**
   - 渲染产出是否真的不含字面 http://？恶意输入（换行注入、YAML/XML 结构破坏、\`$(...)\`、反引号）是否真的不逃逸？
   - RHEL 的既有 golden file 是否**没有变化**？变了说明改了共用骨架的行为，是回归。
   - 固件不兼容组合（BIOS + ESXi）是否真的被拒绝？
   - 时间是否全部来自注入的 Clock（statemachine 里实现代码零处 time.Now()）？
   - 纯函数（磁盘/网卡策略）是否真的无 IO / 无时钟 / 无随机数？
   - archtest 的依赖方向守卫是否仍然通过（没有引入跨模块 import）？
   **写一个能证伪的测试再看它红还是绿 —— 不要靠读代码下结论。**
4. **修真问题**：可以改本轨拥有的文件（见实现者的清单）。**绝不改契约**（internal/core/**、internal/apierr/**、internal/config/**、internal/archtest/**、docs/contracts/**、Makefile、go.mod/go.sum、schema/**），也不要碰别的轨的文件 —— 本轮七轨并行，碰了就制造合并冲突。契约确有缺口就记进 contractGaps。
5. 修完再跑第 1 步确认全绿（**含全仓 go test ./...**），然后 commit 修复（没改就不提交，commits 填空数组）。

**不要为了让测试变绿而削弱断言 —— 那是最坏的结果。** 宁可留一条 ✗ 并说清楚。

## 铁律（同实现者）

- git 只允许 add / commit / status / diff / log。严禁 checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 任何带 -f 或 --hard。
- **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir()。**绝不创建与真实模块同名的临时目录**。
- 禁止改 go.mod / go.sum，禁止 go mod tidy / go get。
- 禁止 import 另一个实现模块（cmd/lober 除外）。中文注释与断言信息。

返回值：files 只列你改动的文件（没改就空数组）；acceptance 是**你验证后的**结论不是实现者的自评；verifyOutcome 写你亲自跑出来的结果（**必须包含全仓 go test ./... 的结果**）；unfinished 写仍未达成的条目与原因。`,
      { label: 'verify:' + t.key, phase: 'Verify', schema: R, effort: 'high' })
      .then(function (v) { return { impl: impl, verify: v } })
  }
)

const out = {}
const gaps = []
for (let i = 0; i < TRACKS.length; i++) {
  const p = results[i] || { impl: null, verify: null }
  const im = p.impl, ve = p.verify
  const g = (im ? im.contractGaps || [] : []).concat(ve ? ve.contractGaps || [] : [])
  g.forEach(function (x) { gaps.push(TRACKS[i].key + ': ' + x) })
  out[TRACKS[i].key] = {
    branch: 'feat/' + TRACKS[i].dir,
    implFiles: im ? im.files.length : 0,
    implCommits: im ? im.commits : [],
    verifierFixed: ve ? ve.files : null,
    verifierCommits: ve ? ve.commits : [],
    verifiedOutcome: ve ? ve.verifyOutcome : '验证 agent 未返回',
    acceptance: ve ? ve.acceptance : (im ? im.acceptance : null),
    unfinished: (im ? im.unfinished || [] : []).concat(ve ? ve.unfinished || [] : []),
    notes: (im ? im.notes || '' : '') + ' || ' + (ve ? ve.notes || '' : ''),
  }
}
return { tracks: out, contractGaps: gaps }
