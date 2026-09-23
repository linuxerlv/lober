export const meta = {
  name: 'lober-fix-win-esxi-e2e',
  description: 'Rewrite the Windows and ESXi e2e scripts to verify the success path now that dispatch is wired',
  phases: [{ title: 'Fix', detail: '两条脚本的断言方向过期，改成验证成功路径' }],
}

const WT = 'E:/src/lober-wt/t17-fix-e2e'

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

phase('Fix')

const r = await agent(`你是 Lober 项目的端到端验证脚本修复者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：${WT}
  分支：    feat/t17-fix-e2e

**所有操作都在这个 worktree 里**（绝对路径，或先 \`cd ${WT}\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录。**

## 背景：两条脚本的断言方向过期了，这是并行协作的时序产物

上一轮 16 路并行时，\`scripts/e2e-windows.sh\` 与 \`scripts/e2e-esxi.sh\` 与
「分派接线」那一轨（t08）**同在一波**，所以它们写脚本时看到的是「Windows 与 ESXi 在
\`menu.go\`/\`renderer.go\` 的分派 switch 里仍返回未实现」。它们据此把脚本写成了
**断言失败路径**：

- \`e2e-windows.sh\` 阶段 7 断言「分派未接线，应回退到 sanboot」
- \`e2e-esxi.sh\` 阶段 5 断言「preview 应报 'MVP 范围内未实现'」、阶段 7 断言「应回退 sanboot」

**而 t08 真的把分派接上了**，所以现在这些断言反过来失败。编排方已核实分派确实可用：
\`go test ./internal/render/ -run Dispatch -v\` 的 17 条断言全过，六个家族
（rhel/debian/ubuntu/suse/windows/esxi）的 \`RenderMenu\` 与 \`RenderAnswerFile\` 都成功，
另外还锁住了 BIOS+ESXi 被拒绝、Windows 双固件各自选对制品键、SUSE 不被路由进 kickstart。

**这两条脚本本身的结构与九阶段框架是好的，不要重写整个脚本** —— 只把建立在
「未接线」前提上的断言改成验证成功路径。

## 你拥有的文件（只有这两个）

  scripts/e2e-windows.sh
  scripts/e2e-esxi.sh

**不要碰** \`scripts/e2e.sh\`（RHEL 基线，九阶段全过，是回归基准）、
\`scripts/e2e-debian.sh\`、\`scripts/e2e-ubuntu.sh\`（这两条已经跑通）、
**任何 Go 代码**、任何 deploy/ 或 docs/ 文件。

**发现 Go 侧的 bug 就写进 findings 与 contractGaps**，由编排方处理 —— 你是验证轨。
这一点很重要：上一轮 Ubuntu 脚本就是这样发现了一个真 bug（cloud-init 要取
user-data 与 meta-data 两个文件，而网关只放行前者，导致 meta-data 返回 404、
渲染器里那个专门分支成了死代码），编排方据此修好了 Go 侧。**诚实报告失败比让脚本
变绿有价值得多。**

## 要做的事

把两条脚本改成验证**成功路径**的九阶段，与 \`scripts/e2e.sh\` / \`e2e-debian.sh\` /
\`e2e-ubuntu.sh\` 同构：

  1 造制品树/二进制/admin 用户/配置  2 起服务  3 登录  4 造主机+制品集+模板
  5 preview 渲染**成功**（不再断言报错）  6 四条准入负面用例
  7 /v1/boot 命中**签发 token 并下发该家族的装机菜单**（不再断言回退 sanboot）
  8 取应答文件 + 信号打点  9 终态 done + 装完再引导拿 sanboot + 应答文件不再提供

**四条安全负面用例一条都不能省**（§9.1 三道闸门 + §10 失败模式 1/2b）：
- 陌生机器请求 \`/v1/boot\` → \`no_task\`，响应体**逐字符含那三行 sanboot 脚本**
- **SN 对但 MAC 不对 → 拒签 token**（双因子，§4.3 硬要求）
- 架构/固件不符 → 硬拒绝且**不消耗 attempt_count**（§4.6：重试必然同样失败）
- **装完的机器再次引导拿到 sanboot 而不是装机菜单**（§10 失败模式 1）

### Windows 特有的断言（改成成功路径后要验的）

- 菜单里有 \`wimboot\` 作 kernel 与**四个 initrd 行**（BCD / boot.sdi / boot.wim，加 wimboot 本身），§4.5
- **固件维度**：\`boot.wim\` 位宽须与固件匹配，同一版本要按固件导两个 artifact_set
  （\`firmware\` 取具体值不是 \`any\`，§2.1）。**UEFI 与 BIOS 各自选到对应的制品键** ——
  \`TestDispatch_WindowsMenu_UEFIAndBIOSSelectOwnArtifactKeys\` 已在单元层锁住，
  端到端也要验一次。你原有的那条「Windows 双固件负面用例」（阶段 6）是对的，保留。
- 应答文件是 \`unattend.xml\`，**是 XML** —— 取到后用 XML 解析器验一次能解析
  （\`go run\` 临时跑 \`encoding/xml\`，go.mod 里有的依赖可用）。
- **unattend 由 WinPE 内 startnet.cmd 去取，不烧进 WIM**。
- 信号上报**不能假设有 curl**：WinPE 里用 PowerShell 或 certutil。读
  \`internal/render/winpe.go\` 的 \`WinPEClientLib\` 看它生成什么，端到端用等价方式模拟打点。

### ESXi 特有的断言

- **ESXi 只支持 UEFI**（§0.3、附录 A.14）。**必须保留一条用例验证 BIOS + ESXi 被硬拒绝**
  且错误信息可读（说清「这台机器是 BIOS，任务要装 ESXi」）——
  \`precheck.go\` 与分派处都做了这个拒绝，端到端要确认。
- **\`boot.cfg\` 由网关渲染，不放 nginx 静态**（§2.1，它含 per-task 的 ks URL）。三处改写要验：
  \`prefix=\` 改写成 HTTP 路径、\`kernelopt=ks=\` 填应答文件地址、
  **模块路径前的 \`/\` 要去掉**（这条最容易漏，漏了安装器找不到模块）。
- 菜单形态是 \`mboot.efi -c <boot.cfg URL>\`（§4.5）。
- **ESXi 的 kickstart 与 RHEL 的语法不同**（ESXi 自己的方言，\`%firstboot\` 是原生的；
  §5.2 的阶段映射表里 ESXi 的 post-nochroot 一栏是「—」，即不存在那个阶段）。
  **断言产出的不是 RHEL kickstart**（比如不含 RHEL 特有的段落）。
- hook 的 interpreter 是 \`ash\`；syslog 用 \`esxcli system syslog config set --loghost=\`。

## 一件已经变了的事：导入管线接上了

上一轮 t01 把 ISO 导入管线真正接上了（在那之前 \`internal/imports\` 整个包零调用点）。
现在 \`POST /artifact-sets\` 会异步触发导入 job，\`familyExtractors\` 查表分派
（RHEL/Debian/Ubuntu/SUSE 已在表内）。

**但 Windows 与 ESXi 仍被 \`identify.go\` 的 \`mvpUnsupportedFamilies\` 挡着**（导入会被
判定为不支持）。而且它们的抽取器签名比别的家族多参数（\`WindowsExternalSources\` /
\`declaredFirmware\`），放不进那张查表 —— 这是 t01 记录过的真实缺口。

所以你的脚本**仍然需要用 \`sqlite3\` CLI 直接写 \`artifact_sets\` 表**来造 ready 状态的
制品集（\`scripts/e2e.sh\` 的文件头注释写明了这个绕法）。**在脚本注释与报告里明确写出
这个事实**：Windows/ESXi 的导入路径尚未打通，端到端验证的是「制品就绪之后」的链路。

## 铁律

1. **严禁 rm -rf，严禁删除任何目录。** 临时文件写在 \`${WT}/.tmp/\` 下，用完只删你建的那个。
2. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
3. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。
4. Bash 的 PATH 快照不含某些目录，需要时自己加：
   Node \`export PATH="/c/Program Files/nodejs:$PATH"\`；
   pwsh 用绝对路径 \`/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe\`
   （**禁止 powershell 5.1**，它输出 GB2312，中文全乱码连错误信息都乱）。
5. **本机没有 jq**（curl 与 sqlite3 有），用 shell 文本处理解析 JSON —— 现有脚本已经
   这么做了，照它的方式。
6. 中文注释与输出。
7. **完成前必须真的把两条脚本跑到 exit 0 并看到九阶段全 PASS。不要声称通过。**

## 验证

\`bash scripts/e2e-windows.sh\` 与 \`bash scripts/e2e-esxi.sh\` **都必须 exit 0 且九阶段全 PASS**。
另外跑一次 \`bash scripts/e2e.sh\`（RHEL 基线）确认你没碰坏它。
跑不通就如实报告卡在哪一步、什么报错、排查到什么程度 —— 一份「卡在 X，证据是 Y」的
报告比一份把断言改宽让它变绿的脚本有用得多。**绝不要为了让脚本变绿而削弱断言。**

全绿后 commit。`, { label: 't17:fix-win-esxi-e2e', phase: 'Fix', schema: R, effort: 'high' })

return r ? {
  files: r.files, commits: r.commits, outcome: r.verifyOutcome,
  findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished, notes: r.notes,
} : '失败'
