export const meta = {
  name: 'lober-close-known-gaps',
  description: 'Close the gaps the 16-lane round surfaced: ESXi boot.cfg never rendered, Windows/ESXi import blocked, preseed firstboot missing, resolve key mismatch, verify endpoint stub, import failure reason, SUSE e2e',
  phases: [
    { title: 'Fix', detail: '八轨并行，按文件归属分区' },
  ],
}

const BASE = `你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：__WT__
  分支：    feat/__ID__

**每个文件操作、每条命令都在你的 worktree 里**（绝对路径，或先 \`cd __WT__\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 有 7 条轨在并行。

## 仓库现状

\`dev\` 分支当前：**17 个 Go 包 \`go test ./...\` 与 \`-race\` 全绿**，\`gofmt\` 干净，
\`go vet\` 无输出，archtest 依赖方向守卫通过。

**五条端到端脚本全部 exit 0、95 个 PASS、0 FAIL**：
  scripts/e2e.sh（RHEL 基线）、e2e-debian.sh、e2e-ubuntu.sh、e2e-windows.sh、e2e-esxi.sh

**六个 OS 家族的渲染与分派都已接线**（rhel / debian / ubuntu / suse / windows / esxi），
ISO 导入管线已接上（\`POST /artifact-sets\` 异步触发导入 job，\`familyExtractors\` 查表分派，
RHEL/Debian/Ubuntu/SUSE 已在表内）。

**你的改动不许打破这个状态。** 尤其五条 e2e 脚本必须仍然全过。

## 铁律

1. **只写你拥有的文件**（下面明确列出）。8 条轨并行，碰别人的文件就制造合并冲突。
2. **契约只读**：禁止修改 \`internal/core/contract/**\`、\`internal/apierr/**\`、
   \`internal/archtest/**\`、\`docs/contracts/**\`、\`Makefile\`、\`go.mod\`、\`go.sum\`
   （除非你的任务明确授权）。发现契约缺口时不要加变通：在 contractGaps 里写清
   「缺什么、你怎么保守绕的、正解要改哪个接口」。
3. **不要修改别的模块的既有行为。** 你是做增量：加新文件、在既有文件里加新分支/新函数。
   若必须改既有函数，改动要最小且**不得让任何现存测试变红**。
4. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir() 或写在你 worktree 的
   \`.tmp/\` 下，用完只删你自己建的那个具体文件。**绝不创建与真实模块同名的临时目录。**
5. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
6. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。
7. **import 白名单**：model、contract、标准库，加上你的任务明确允许的。
   **实现模块之间绝不互相 import**（store/statemachine/render/gateway/console/provider/imports
   互不可见）—— archtest 会拦住，**唯一豁免是 cmd/lober 装配点**。
8. 先写测试再写实现。断言失败信息写明「为什么这条重要」加 SPEC 章节号。
9. Bash 的 PATH 快照不含某些目录，需要时自己加：
   Node \`export PATH="/c/Program Files/nodejs:$PATH"\`；
   pwsh 用绝对路径 \`/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe\`
   （**禁止 powershell 5.1**：输出 GB2312，中文全乱码，连错误信息都乱，无法判断成败。
   pwsh 首句先设 \`[Console]::OutputEncoding=[Text.UTF8Encoding]::new()\`）。
   **本机没有 jq**（curl 与 sqlite3 有），shell 里解析 JSON 用文本处理。
10. **完成前必须真的执行验证并看到通过输出。不要声称通过。**
    **不要把「我读了代码觉得没问题」当成验证。**
11. 中文注释、文档与断言信息；标识符与文件名用英文。gofmt 干净。
12. 你的返回值是数据，不是给人看的汇报。

## 验证与提交

必跑：\`go vet <你的包>\`、\`go test <你的包> -v -count=1\`、\`go test <你的包> -race\`、
\`gofmt -l <你的目录>\`、**\`go test ./... -count=1\` 全仓**（你在全绿代码上做增量，
打破任何既有包就是回归）。改了 Go 代码的轨**还要跑受影响的 e2e 脚本**确认没坏。

全绿后 commit。**只 add 你自己的文件**，提交前用 \`git status\` 确认没夹带别人的东西。

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

phase('Fix')

const out = await parallel([
  // ── n1：ESXi 的 boot.cfg 从没被渲染过（真实部署必失败） ──
  () => agent(mk('n1-esxi-bootcfg') + `## 你的任务：让 ESXi 真的下发 boot.cfg，而不是把 kickstart 正文当 boot.cfg

### 这是一个真实部署必然失败的缺口，端到端脚本已坐实

\`internal/render/esxi.go\` 有两个函数：
- \`RenderESXiBootCfg(template, bootCfgURLBase, answerFileURL)\` —— 改写引导参数文件，
  三处替换：\`prefix=\` 改成 HTTP 路径、\`kernelopt=ks=\` 填应答文件地址、
  **模块路径前的 \`/\` 去掉**
- \`AssembleESXiKickstart(ctx)\` —— 产出 ESXi kickstart 方言正文（install/rootpw/network/
  reboot/%firstboot 等）

**\`RenderESXiBootCfg\` 在生产代码里零调用点**（全仓 grep 确认，唯一调用在 esxi_test.go）。
\`renderer.go\` 的 \`RenderAnswerFile\` 对 ESXi 请求 \`boot.cfg\` 时分派到
\`AssembleESXiKickstart\` —— 于是机器取到的 \`boot.cfg\` 是一段 kickstart 正文。

真实部署下 \`mboot.efi\` 会尝试把那段 kickstart 当引导参数文件解析，**解析失败发生在
引导器读引导参数这一步，比「装到一半停住」更早**，而且现场只看到引导器报错、
看不出根因。

\`esxi.go\` 文件末尾自己记录了这个缺口的根因链：\`model.AnswerFileName(esxi)\` 固定返回
\`boot.cfg\`，没有像 Ubuntu 的 \`meta-data\` 那样的第二候选文件名机制，于是无处安放 \`ks.cfg\`。
它给出的正解方向是：\`renderer.go\` 的文件名校验对 ESXi 额外接受 \`ks.cfg\`，
\`RenderAnswerFile\` 按 \`fileName\` 分别调用两个函数。

### 你拥有的文件（只有这些）

  internal/render/renderer.go（分派与文件名校验）
  internal/render/esxi.go（若需要调整导出函数签名以便接线）
  internal/render/esxi_test.go、renderer_test.go
  internal/render/testdata/esxi_*.golden
  internal/gateway/answer.go 与 answer_test.go（文件名放行）

**不要碰** internal/render/{menu,kickstart,autoinstall,preseed,autoyast,unattend,winpe}.go、
internal/imports/**、internal/console/**、scripts/**、docs/contracts/**。

### 参考：Ubuntu 的 meta-data 是同类问题的已有解法

上一轮修过一个同构的 bug：cloud-init 要取 \`user-data\` 与 \`meta-data\` 两个文件，
而网关只放行 \`AnswerFileName()\` 的单一返回值，导致 \`meta-data\` 返回 404、
渲染器里那个专门分支成了死代码。修法是 \`internal/gateway/answer.go\` 的
\`answerFileNameAllowed(ctx, file, wantFile)\`：默认逐字符相等，**只对 Ubuntu 家族额外
放行 \`meta-data\` 这一个名字**。**读那个函数，照它的形状加 ESXi 的放行**，
放行必须同样窄（只对 ESXi、只多放行 \`ks.cfg\`）—— 路径段来自机器请求，
放宽成模糊匹配等于让机器自己指定要取哪种应答文件。

### 要点

- **\`boot.cfg\` 必须由网关渲染，不能放 nginx 静态**（SPEC §2.1）：它含 per-task 的 ks URL。
- \`RenderESXiBootCfg\` 需要「导入时保存的原始 boot.cfg 模板」。读
  \`internal/imports/extract_esxi.go\` 确认它把原始模板存到了 \`ArtifactPaths\` 的哪个键
  （**只读那个文件，不改**）。若模板不在 RenderContext 可达的地方，那是一个真实的
  契约缺口 —— 在 contractGaps 里写清，并选最保守的可行做法。
- **ESXi 只支持 UEFI**（§0.3、附录 A.14），BIOS + ESXi 的拒绝逻辑已在 \`precheck.go\` 与
  分派处实现，**不要改动它**。
- 菜单形态是 \`mboot.efi -c <boot.cfg URL>\`（§4.5），\`RenderESXiMenuLine\` 已实现。

### 测试要锁住

- **取 \`boot.cfg\` 拿到的是引导参数文件**（含 \`prefix=\` 与 \`kernelopt=\` 行），
  **不是 kickstart 正文**（不含 \`install \` 这类 kickstart 指令）
- **取 \`ks.cfg\` 拿到的是 kickstart 正文**
- **三处改写都正确**：\`prefix=\` 是 HTTP 路径、\`kernelopt=ks=\` 指向应答文件 URL、
  **模块路径前没有前导 \`/\`**（这条最容易漏，漏了安装器找不到模块）
- 非 ESXi 家族请求 \`ks.cfg\` 仍然 404（放行只对 ESXi 生效）
- golden file 覆盖两种产出

跑 \`bash scripts/e2e-esxi.sh\` 确认仍全过 —— **它当前断言的是「取到 kickstart 正文」这个
错误行为吗？** 读它确认；若是，那条断言需要跟着改，但 \`scripts/\` 不属于你 ——
在 findings 里写清哪一行需要改成什么，由编排方接线。`, { label: 'n1:esxi-bootcfg', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n2 / n3：Windows 与 ESXi 的导入放行 ──
  () => agent(mk('n2-win-import') + `## 你的任务：解除 Windows 的导入封锁，让它能走真实的登记→导入→ready 路径

### 当前状态

\`internal/imports/identify.go\` 的 \`mvpUnsupportedFamilies\` 把 Windows 与 ESXi 标为
不支持，导入会被挡。所以 Windows 的端到端脚本只能用 \`sqlite3\` CLI 直接写
\`artifact_sets\` 表来造 ready 状态的制品集。

**但 \`internal/imports/extract_windows.go\` 的 \`ExtractWindows\` 已经实现了**（有测试）。
挡它的是两件事：
1. \`mvpUnsupportedFamilies\` 这道闸门
2. **它的签名比别的家族多参数**（\`WindowsExternalSources\` / \`WimArchDetector\`），
   放不进 \`familyExtractors\` 查表（表的行类型固定为
   \`func(fs.FS, string) (ArtifactPaths, error)\`）—— 这是 t01 记录的真实缺口

### 你拥有的文件（只有这些）

  internal/imports/identify.go 与 identify_test.go（**只允许改 Windows 相关，
    绝不动 ESXi 的封锁** —— ESXi 归 n3 轨，两轨同时改这个文件必冲突。
    具体说：你只从 \`mvpUnsupportedFamilies\` 里移除 Windows 这一行，ESXi 那行留着）
  internal/imports/extract_windows.go 与 extract_windows_test.go
  internal/imports/job.go 与 job_test.go（**只允许加 Windows 的分派，
    不改既有家族的分派行为** —— 注意 n3 也要改这个文件加 ESXi 分派，
    所以你的改动要尽量局部：优先用「给查表加一行 + 一个适配函数」的方式，
    避免重构整个分派结构）

**不要碰** internal/imports/extract_{rhel,debian,ubuntu,suse,esxi}.go、manifest.go、
internal/render/**、internal/console/**、cmd/**、scripts/**。

### 核心设计问题：额外参数从哪来

\`ExtractWindows\` 要 \`declaredFirmware\`（或等价的固件信息）。而**固件维度是 Windows 的硬约束**
（SPEC §0.3、§2.1）：\`boot.wim\` 位宽须与固件匹配，所以**同一 Windows 版本要按固件导两个
artifact_set**，\`model.ArtifactSet.Firmware\` 取具体值（\`uefi\` 或 \`bios\`）而不是 \`any\`。

所以固件信息的正确来源是**登记时运维填的 \`ArtifactSet.Firmware\` 字段**。
但 \`familyExtractors\` 的行类型只接受 \`(fs.FS, string)\` 两个参数，拿不到 ArtifactSet。

**你要自己判断怎么解，下面是几个方向与各自的代价，选一个并论证：**
- 让查表的行类型改成接受一个「导入上下文」结构体（含 fs.FS、destDir、ArtifactSet）——
  改动面大，会碰到别的家族的分派（而那些归别的轨/既有代码）
- 给 Windows 单独一条分派路径（不进查表），在 \`Run\` 里按家族先判断一次 ——
  局部，但让分派结构不再统一
- 用闭包在建表时把 ArtifactSet 绑进去 —— 需要 \`Run\` 能拿到 ArtifactSet

**判断的依据是「改动面最小且不破坏别的轨」**，因为 n3 轨正在同一个文件里做 ESXi 的同类改动。
在 notes 里写清你选了什么、为什么，以及 n3 若选了不同方向会不会冲突。

### 测试要锁住

- **Windows 的 ISO 树能被识别并走到 \`ExtractWindows\`**（内存文件系统造 \`sources/install.wim\`）
- **固件信息真的传到了抽取器**：UEFI 与 BIOS 两个制品集各自抽取出对应位宽的 WinPE 制品键
- **固件字段为 \`any\` 时被拒绝**（§2.1：Windows 必须取具体固件值，
  \`any\` 意味着运维没想清楚要导哪个位宽，产出的制品集装机时才会暴露不匹配）
- 缺 \`sources/install.wim\` 报明确错误
- **ESXi 仍然被 \`mvpUnsupportedFamilies\` 挡着**（你不动它，加一条测试确认没误伤）`, { label: 'n2:win-import', phase: 'Fix', schema: R, effort: 'high' }),

  () => agent(mk('n3-esxi-import') + `## 你的任务：解除 ESXi 的导入封锁

### 当前状态

与 Windows 同构的问题：\`mvpUnsupportedFamilies\` 挡着 ESXi，而
\`internal/imports/extract_esxi.go\` 的 \`ExtractESXi(root, destDir, declaredFirmware)\`
已经实现（有测试），签名比别的家族多一个 \`declaredFirmware\` 参数，
放不进 \`familyExtractors\` 查表。

### 你拥有的文件（只有这些）

  internal/imports/extract_esxi.go 与 extract_esxi_test.go
  internal/imports/esxi_dispatch.go（**新建** —— 把 ESXi 的分派适配写在这个新文件里，
    而不是直接改 job.go。理由见下）

**不要碰 internal/imports/identify.go 与 job.go** —— n2 轨正在改那两个文件
（解除 Windows 封锁 + 加 Windows 分派）。**两轨同时改同一个文件必然冲突。**

所以你的交付方式是：
1. 在新文件 \`esxi_dispatch.go\` 里写好 ESXi 的分派适配函数（签名与
   \`familyExtractors\` 的行类型兼容，或提供一个能被 \`Run\` 调用的入口）
2. **在 \`wiringNeeded\` 里写清两行精确的接线指示**：
   - \`identify.go\` 的 \`mvpUnsupportedFamilies\` 要移除 ESXi 那一行
   - \`job.go\` 的分派要加什么（给出确切的代码行）
   由编排方在两轨都完成后接线。

### 要点

- **ESXi 只支持 UEFI**（SPEC §0.3、附录 A.14）：UEFI 用 \`mboot.efi\`；传统 BIOS 需要
  syslinux 的 \`mboot.c32\` 加 iPXE 的 COMBOOT 支持，MVP 明确不覆盖。
  \`model.FirmwareCompatible(esxi, bios)\` 已经返回 false（M1 冻结的）。
  **所以 \`declaredFirmware\` 不是 \`uefi\` 时，抽取就该直接拒绝** ——
  在导入这一步拦住比等到机器上电引导时才被 precheck 拒绝要好得多。
- ESXi 的识别特征是 \`boot.cfg\` + \`mboot.efi\`（§2.1）。
- **原始 \`boot.cfg\` 必须被保存下来**：它是 \`RenderESXiBootCfg\` 改写的输入
  （n1 轨正在做那件事）。确认 \`ExtractESXi\` 把它存到了 \`ArtifactPaths\` 的哪个键，
  **在 notes 里写清那个键名** —— n1 轨需要知道从哪读。
- 全部测试通过**内存文件系统**，不依赖真 ISO。

### 测试要锁住

- ESXi 的 ISO 树能被正确抽取（\`mboot.efi\` 与原始 \`boot.cfg\` 的路径都对）
- **\`declaredFirmware\` 是 bios 时被拒绝**，错误信息说清「ESXi 仅支持 UEFI，
  BIOS 路径需 mboot.c32 + COMBOOT，MVP 不覆盖」
- \`declaredFirmware\` 是 \`any\` 时的行为（判断该拒绝还是当 uefi，论证并测试）
- 缺 \`mboot.efi\` 或缺 \`boot.cfg\` 各自报明确错误`, { label: 'n3:esxi-import', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n4：preseed 的 firstboot 脚手架 ──
  () => agent(mk('n4-preseed-firstboot') + `## 你的任务：Debian preseed 补上 firstboot 脚手架与缺失的两个信号

### 当前状态（Debian 端到端脚本诚实标注的两个缺口）

1. **\`internal/render/preseed.go\` 没有调用 \`FirstbootScaffold\`**（grep 确认零处）。
   后果：Debian 装完重启后，目标系统里没有 systemd oneshot、没有 hook 脚本、
   没有 finalize token —— **重启后阶段的四个信号（booted / init-start / init-done /
   ready）没有任何机制会发出**，任务会卡在 \`rebooting\` 直到超时。
   而 \`rebooting\` 超时是**不可重装**的失败态（§3.4：机器已装好、盘上可能有数据），
   所以这不只是「少几个信号」，是**每台 Debian 机器装完都会进一个不可重装的失败态**。
2. **\`packages-start\` / \`packages-done\` 两个信号渲染器没生成**（§6.1 的信号点表里有它们）。
   后果：装包阶段（预算 60 分钟，是最长的一个阶段）完全没有进度可见性 ——
   卡在装包与卡在别处无法区分。

### 你拥有的文件（只有这些）

  internal/render/preseed.go 与 preseed_test.go
  internal/render/testdata/preseed_*.golden

**不要碰** internal/render/{firstboot,skeleton,clientlib,kickstart,autoinstall,autoyast,
menu,renderer,esxi,unattend,winpe}.go —— 那些是共享骨架或别的家族，
改它们会影响别的家族的 golden（那就是回归）。
若发现共享骨架缺东西，**在 contractGaps 里写清**，用不改它们的方式做完。

### 要点

- **读 \`internal/render/firstboot.go\` 与 \`kickstart.go\`**，看 RHEL 是怎么把
  \`FirstbootScaffold\` 接进 \`%post\` 段的，照同样的意图接进 preseed 的对应位置。
- **preseed 的阶段映射**（SPEC §5.2）：
  pre → \`preseed/early_command\`、post-nochroot → \`preseed/late_command\`、
  post → \`late_command\` + chroot、firstboot → systemd oneshot。
  **firstboot 用 systemd oneshot，与 RHEL 相同**（不像 Ubuntu 用 cloud-init runcmd）。
- **用户的 firstboot hook 脚本与参数必须在 post 阶段就落进目标系统**（SPEC §5.8），
  不能等重启后再去 nginx 拉 —— 重启后业务网可能已生效、装机 VLAN 未必还可达。
- firstboot 脚手架用 \`ConditionPathExists\` + 成功后删标记文件保证只跑一次（§5.8）：
  比脚本末尾 \`systemctl disable\` 好 —— 脚本中途死掉时标记仍在，可看出未跑完，
  重启会重试，也可人工 \`touch\` 重新挂上。
- **\`packages-start\`/\`packages-done\` 的插入位置要想清楚**：preseed 的装包阶段
  由 debian-installer 的 \`pkgsel\` 驱动，不像 kickstart 有明确的 \`%packages\` 段边界。
  查 preseed 有没有合适的钩子点（\`pkgsel/include\` 之后？\`late_command\` 之前？），
  **查不到就在 contractGaps 里如实写「preseed 没有对应的钩子点」并说明你的近似做法**，
  不要为了让信号齐全而放到一个语义不对的位置 —— 那比缺信号更糟（运维会以为装包
  已开始而实际没有）。
- 注入的 \`signal()\` 复用 \`clientlib.go\`（**只读**）。**遥测失败绝不杀装机**（§6.3）。

### 测试要锁住

- **产出的 preseed 里有 firstboot 脚手架**：systemd unit、\`ConditionPathExists\`、
  标记文件、finalize token 写入
- **用户的 firstboot hook 脚本在 post 阶段就落盘**（不是重启后才下载）
- 四个重启后信号有触发机制
- \`packages-start\`/\`packages-done\`（或你论证后的近似做法）
- **RHEL / Ubuntu / SUSE 的既有 golden file 不得变化** —— 变了说明你动了共享骨架
- 产出不含字面 \`http://\`（grep 自查）

跑 \`bash scripts/e2e-debian.sh\` 确认仍全过。它当前有两条 \`[PASS]\` 的说明文字提到
「渲染器未生成 packages-*」「preseed.go 未调用 FirstbootScaffold」——
你修好之后那两条说明文字就过期了，但 \`scripts/\` 不属于你：
**在 findings 里写清哪两行说明需要更新**，由编排方接线。`, { label: 'n4:preseed-firstboot', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n5：resolve 的键名偏差（装配点在兜） ──
  () => agent(mk('n5-resolve-keys') + `## 你的任务：修正 provider/resolve.go 的表单键读取，让装配点的 fixup 不再必要

### 当前状态：一个被装配点兜着的真实偏差

\`internal/provider/resolve.go\` 的 \`taskParamsResolver.Resolve\` 读表单值用的是
\`formData[model.FactHostname]\`（即 \`formData["LOBER_HOSTNAME"]\`），而**表单实际传的是
字面 \`"hostname"\`**（\`scripts/e2e.sh\` 的建任务请求体是
\`"formData":{"hostname":"e2e-host-01"}\`，SPEC §8.3 的表单字段也是裸键名）。

所以它读不到，写回 \`ResolvedParams\` 的是**空串**。

同时它写回的键用的是 fact 命名空间常量（\`model.FactHostname\` 等），而
\`internal/render/params.go\` 的 \`InlineParams\` 只认裸键白名单
\`{"hostname","biz_ip","role"}\` —— 六个家族的渲染器全都把 \`ResolvedParams\` 喂给它。

**\`cmd/lober/wire.go\` 的 \`resolvedParamsKeyFixup\` 在装配点做键名转换兜住了这两件事**
（\`factKeyToInlineParamKey\` 映射表）。M9 那轮做过证伪实验：摘掉这个 fixup，
e2e 阶段 8 立刻 HTTP 500。

**兜住不等于修好**：装配点多了一层只为修补语义不一致而存在的包装，
而且任何新增的表单字段都要在两处各改一次。

### 你拥有的文件（只有这些）

  internal/provider/resolve.go 与 resolve_test.go
  internal/provider/lifecycle.go 与 lifecycle_test.go（它读 \`ResolvedParams[model.FactHostname]\`，
    你改了写入键之后这里要跟着改）
  cmd/lober/heartbeat.go（同样读 \`ResolvedParams[model.FactHostname]\`）
  cmd/lober/wire.go（**授权你简化或移除 \`resolvedParamsKeyFixup\`** ——
    但必须先确认它兜的两件事都真的不需要了）
  cmd/lober/wire_test.go、paramsresolver_fixup_test.go

**不要碰** internal/render/**（\`InlineParams\` 的白名单是裸键，那是全系统一致的既有约定，
**以它为准**）、internal/console/**、internal/core/**、scripts/**。

### 要点

- **方向是「让 resolve.go 对齐裸键约定」，不是「让渲染器改成认常量键」**。
  理由：六个家族的渲染器都用裸键，\`InlineParams\` 的白名单是裸键，SPEC §8.3 的表单
  字段也是裸键 —— 裸键是多数、也是对外契约（运维填表单时写的就是 \`hostname\`）。
  而 fact 命名空间（\`LOBER_*\`）是**注入给机器侧 hook 的变量名**（§5.4），
  与表单键是两个不同的命名空间，resolve.go 把两者混用了。
- **注意区分三类键**，改动时别搞混：
  1. **表单键**（运维填的）：\`hostname\` / \`biz_ip\` / \`role\` —— 裸键
  2. **决策结果键**（写进 \`ResolvedParams\` 供渲染器消费）：也该是裸键，
     因为 \`InlineParams\` 认的是裸键
  3. **fact 变量名**（注入给机器侧 hook 的环境变量）：\`LOBER_HOSTNAME\` 等 ——
     这些由 \`InjectFacts\` 产出，与 \`ResolvedParams\` 无关
- \`lifecycle.go\` 读 hostname 是为了拼 FQDN 做 DNS 注册；\`heartbeat.go\` 读它是为了
  探测目标。两处都要跟着改成裸键。
- **\`template_id\` / \`template_version\` 两个 bookkeeping 键**不在 \`InlineParams\` 白名单里
  —— 装配点的 fixup 是怎么处理它们的？读 \`wire.go\` 确认，你的改动要保持那个行为
  （或给出更好的做法并论证）。
- **移除 fixup 前必须确认**：\`go test ./cmd/lober/ -run Fixup -v\` 那几条测试
  （\`TestResolvedParamsKeyFixup_*\`）在验什么？它们会因为你的修改而变成验证一个
  不再存在的包装 —— 相应调整，但**不要削弱断言**：那几条测试守的行为
  （渲染器能拿到合法的表单值）仍然要有测试守着，只是守的位置从 fixup 挪到 resolve.go。

### 测试要锁住

- **\`Resolve\` 真的读到了表单里的 hostname**（传 \`formData{"hostname":"web-01"}\` 进去，
  断言 \`ResolvedParams\` 里拿得到 \`web-01\`）—— 这条是本轨的核心，
  修之前它会红（因为读的是 \`LOBER_HOSTNAME\` 键）
- 写回的键是裸键（\`InlineParams\` 认的那些）
- \`lifecycle.go\` 与 \`heartbeat.go\` 仍能读到 hostname
- **\`bash scripts/e2e.sh\` 九阶段全过** —— 这是最重要的验证：
  M9 那轮的证伪实验证明了摘掉 fixup 会让阶段 8 返回 500，
  你的改动必须让 e2e 在**没有 fixup**（或 fixup 简化后）的情况下仍然全过
- 另跑 \`bash scripts/e2e-debian.sh\` 与 \`e2e-ubuntu.sh\` 确认没坏`, { label: 'n5:resolve-keys', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n6：verify 端点是空壳 ──
  () => agent(mk('n6-verify-manifest') + `## 你的任务：让 \`POST /artifact-sets/{id}/verify\` 真的重算 manifest 校验完整性

### 当前状态

\`internal/console/artifact.go\` 的 \`handleVerifyArtifactSet\` **只校验制品集存在后就返回接受**，
不真的重算 manifest（handler 自己的注释承认了这个行为）。

而 SPEC §8.2 的端点表写明这个端点的职责是「重算 sha256 校验完整性」。
docs/contracts/http-console.md 也是这么定义的。

**为什么这个端点重要**（SPEC §10 运维点一）：nginx 按路径找文件、Go 按同一套路径签发 URL，
不一致即引导时 404。制品落盘后被误删、被覆盖、传输截断，都只能靠校验发现 ——
否则要等机器上电取不到内核才暴露，而那时 attempt 已经被消耗、运维已经在机房了。

### 你拥有的文件（只有这些）

  internal/console/artifact.go 与 artifact_test.go
  internal/console/server.go（**只允许加 Deps 字段，不改既有字段**）
  cmd/lober/wire.go（接线，只允许加）
  cmd/lober/verifier.go（**新建** —— manifest 校验的生产实现）

**不要碰** internal/imports/**（\`GenerateManifest\` 在那里，**console 不许 import imports**
—— archtest 会拦。走 \`Deps\` 窄接口，实现放 cmd/lober，与 t01 的 \`importRunner\` 同一个模式，
**读 \`cmd/lober/importrunner.go\` 照它的形状做**）、internal/store/**、internal/core/**。

### 要点

- **校验是长耗时操作**（要重算几 GB 文件的 sha256）。不要在 HTTP handler 里同步跑完 ——
  与导入 job 同样的纪律：异步执行，端点立即返回「已接受」。
  但**结果要能查到**，否则运维不知道校验通过没有。
  \`model.ArtifactSet\` 有哪些字段能承载结果？（**不许改 model**）
  能表达的最大程度做到，不够的写进 contractGaps。
- **manifest 的算法必须与导入时一致**，否则校验必然失败。
  \`internal/imports/manifest.go\` 的 \`GenerateManifest\` 是导入时用的 ——
  你不能 import 那个包，所以**生产实现放 \`cmd/lober/verifier.go\`**（装配点可以 import imports），
  在那里调用 \`imports.GenerateManifest\` 并与 \`ArtifactSet.ManifestSHA256\` 比对。
- **校验失败时的状态处理要想清楚**：把制品集置 \`failed\`？还是只报告不改状态？
  考虑一个场景：制品集正被一个非终态任务 pin 着（§2.6），此时校验失败说明那台机器
  接下来会取不到文件。**论证你的选择并写进文档注释。**
- 权限：按 §9.4，这个操作影响面是「一个制品集」，不是全局。

### 测试要锁住

- **校验通过时**：制品集状态与 manifest 不变
- **文件被改动时校验失败**（造一个假的制品树，改一个字节，确认校验发现了）
- **文件缺失时校验失败**且错误指出是哪个路径
- 端点立即返回（不阻塞等校验跑完）
- 不存在的制品集返回 1004（\`apierr.CodeNotFound\`）
- **\`verifier\` 为 nil 时的降级行为**（个别测试或部署形态可能不注入它，
  参考 \`importRunner\` 为 nil 时的处理）`, { label: 'n6:verify-manifest', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n7：导入失败原因无处承载 ──
  () => agent(mk('n7-artifact-failreason') + `## 你的任务：让导入失败的原因能被运维看到

### 当前状态（t01 记录的契约缺口）

导入 job 失败时只能把 \`ArtifactSet.State\` 置 \`failed\`，**失败原因无处承载**：
- \`model.ArtifactSet\` 没有「失败原因」字段
- \`contract.ArtifactSetStore.UpdateArtifactSetState\` 的签名只接受
  \`(state, manifestSHA256)\`，没有第三个「原因」参数

后果：运维在 \`/artifact-sets\` 列表里只看到一个 \`failed\`，**不知道为什么** ——
是 ISO 路径写错了？识别不出家族？某个文件抽不出来？磁盘满了？
每一种的处置完全不同，而现在全都长一样。

导入是运维最常出错的一步（几 GB 的 ISO、手填的本地路径、气隙环境），
这个信息缺失的代价很高。

### 这是一次受控的契约变更，编排方授权你做

按 M1 定的契约变更五步走：**改接口 → 改 conformance 断言 → 改内存实现 →
改 SQLite 实现 → 接上调用方**。这个流程刻意做得麻烦，因为契约漂移是并行开发唯一的
致命风险。**五步都要走完，不许只改一半。**

### 你拥有的文件（只有这些）

  internal/core/model/inventory.go（**授权你给 ArtifactSet 加字段**）
  internal/core/model/inventory_test.go
  internal/core/contract/store.go（**授权你改 ArtifactSetStore 的方法签名**）
  internal/core/contract/conformance/store.go（**加断言，这是执行机制**）
  internal/core/fake/store.go（内存实现补齐）
  internal/store/sqlite/artifact.go、scan.go（SQLite 实现）
  internal/imports/job.go（**只允许改「写回失败原因」这一处，不改分派逻辑**
    —— n2 轨正在改同一个文件的分派部分，你的改动要局部）
  internal/console/artifact.go 与 artifact_test.go（响应里带上原因）
  docs/contracts/http-console.md（**授权你改** —— 响应字段变了要同步契约文档并留变更记录）
  internal/console/routes_test.go（若端点响应形态变了）

**不要碰** internal/store/sqlite/schema/**（\`artifact_sets\` 表已经建好了 ——
**先读它，看有没有现成的列能用**。若没有，那是一个真实的两难：schema 是数据契约，
禁止 \`ALTER TABLE\`（改它会让内存实现与 SQLite 实现行为分叉，而 conformance 套件
发现不了这种分叉）。**读 schema 确认后在 contractGaps 里写清你的判断与做法** ——
如果确实需要新列，说明为什么这是唯一的正解，由编排方决定要不要开这个例外）。

### 测试要锁住

- **conformance 断言**（两个 Store 实现跑同一套，这是契约没漂移的证明）：
  写入失败原因后能读回来、原因原文完整往返（含中文、引号、换行）、
  **导入失败只改状态与原因、不清空已写入的 Paths**（这是 ArtifactSetStore 两个方法
  分开的全部理由，既有断言里应该已经有类似的一条，读它）
- 导入 job 的每种失败都带**具体**原因（不是「导入失败」这种没信息的话）：
  路径打不开、识别不出家族、抽取失败、manifest 生成失败，逐个验
- 控制台响应里带上原因
- **既有的 conformance 断言全部仍然通过**（你改了接口签名，两个实现都要跟着改）`, { label: 'n7:artifact-failreason', phase: 'Fix', schema: R, effort: 'high' }),

  // ── n8：SUSE 端到端 ──
  () => agent(mk('n8-suse-e2e') + `## 你的任务：SUSE 端到端验证（新家族，渲染与导入都就绪但从没端到端跑过）

### 当前状态

上一轮新增了 SUSE 家族：
- \`internal/render/autoyast.go\` —— AutoYaST 渲染器（27 条测试 + 2 个 golden）
- \`internal/imports/extract_suse.go\` —— SUSE 抽取器（7 条测试）
- \`identify.go\` 的识别判据（\`repodata/repomd.xml\` + \`media.1/products\` 组合）
- \`familyExtractors\` 表里有 \`OSFamilySUSE → ExtractSUSE\`
- \`menu.go\` / \`renderer.go\` 的分派已接线

**但 SUSE 从没端到端跑过** —— 「单元测试全绿」与「能从 ISO 装出一台 openSUSE」之间
隔着导入、分派、菜单、应答文件供给、信号全链路。

### 你拥有的文件（只有这些）

  scripts/e2e-suse.sh（新建）
  scripts/e2e/mkfixture-suse.sh（新建，若需要）
  docs/real-machine/16-e2e-suse.md（你的报告，新建）

**不要碰** 任何 Go 代码、其它 e2e 脚本（\`scripts/e2e.sh\` 是 RHEL 基线与回归基准，
\`e2e-debian.sh\` / \`e2e-ubuntu.sh\` / \`e2e-windows.sh\` / \`e2e-esxi.sh\` 都已跑通）。
**发现 Go 侧的 bug 就写进 findings 与 contractGaps**，由编排方处理 —— 你是验证轨。
上一轮 Ubuntu 脚本就是这样发现了一个真 bug（cloud-init 要取 user-data 与 meta-data
两个文件，而网关只放行前者），编排方据此修好了 Go 侧。**诚实报告失败比让脚本变绿有价值。**

### 九阶段结构（照 \`scripts/e2e.sh\` 与 \`e2e-debian.sh\` 的形状写）

  1 造制品树/二进制/admin 用户/配置  2 起服务  3 登录  4 造主机+制品集+模板
  5 preview 渲染  6 四条准入负面用例  7 /v1/boot 命中签 token
  8 取应答文件 + 装机期七信号 + 重启后四信号  9 终态 done + 装完再引导拿 sanboot

**四条安全负面用例一条都不能省**（§9.1 三道闸门 + §10 失败模式 1/2b）：
- 陌生机器请求 \`/v1/boot\` → \`no_task\`，响应体**逐字符含那三行 sanboot 脚本**
- **SN 对但 MAC 不对 → 拒签 token**（双因子，§4.3 硬要求）
- 架构/固件不符 → 硬拒绝且**不消耗 attempt_count**（§4.6）
- **装完的机器再次引导拿到 sanboot 而不是装机菜单**（§10 失败模式 1）

### SUSE 特有的要点

- **应答文件是 \`autoinst.xml\`，不是 \`ks.cfg\`**（\`model.AnswerFileName\` 已规定）。
  引导参数是 \`autoyast=\` 而非 \`inst.ks=\`。
  **SPEC 反复警示：绝不能因为都被口头叫「ks」就把 SUSE 当 kickstart** ——
  语法完全不同，安装器根本不认。**端到端要断言取到的是 AutoYaST XML**
  （能被 XML 解析器解析、有 AutoYaST 的元素结构），**不是 kickstart 正文**
  （不含 \`%packages\` 这类 kickstart 特征）。用 \`go run\` 临时跑 \`encoding/xml\` 验一次。
- **引导制品是 \`linux\` + \`initrd\`**（openSUSE 的命名），路径在 \`boot/x86_64/loader/\` 下。
  造假制品树时文件名要与 \`extract_suse.go\` 抽取出的路径键一致（读那个文件确认）。
- AutoYaST 的 hook 阶段对应 \`<scripts>\` 下的 \`pre-scripts\` / \`chroot-scripts\` /
  \`post-scripts\` / \`init-scripts\`（与 kickstart 的四阶段不是一一对应）。
  **端到端要验证渲染出的 XML 里这几段真的都在。**
- **优先走真实的登记→导入→ready 路径**（SUSE 已在 \`familyExtractors\` 表内，
  导入应该能走通）。走不通再退回 \`sqlite3\` CLI 绕法，并**在报告里写明为什么走不通**
  —— 那是一个真实的发现。

### 交付

\`bash scripts/e2e-suse.sh\` **必须 exit 0 且九阶段全 PASS**。
另跑一次 \`bash scripts/e2e.sh\` 确认你没碰坏 RHEL 基线。
报告写清每阶段的实际命令与输出要点，以及仍需真机验证的项
（AutoYaST 是否能被真实 SUSE 安装器解析执行，需要 SUSE 安装介质）。

**跑不通也要如实交付**：写清卡在哪一步、什么报错、排查到什么程度。
一份「卡在 X，证据是 Y」的报告比一份把断言改宽让它变绿的脚本有用得多。`, { label: 'n8:suse-e2e', phase: 'Fix', schema: R, effort: 'high' }),
])

const ok = out.filter(Boolean)
return {
  done: ok.length + '/8',
  lanes: ok.map(r => ({
    files: (r.files || []).length, commits: r.commits,
    outcome: String(r.verifyOutcome || '').slice(0, 400),
    findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished,
  })),
  allFindings: ok.flatMap(r => r.findings || []),
  allGaps: ok.flatMap(r => r.contractGaps || []),
}
