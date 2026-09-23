export const meta = {
  name: 'lober-finish-suse-e2e',
  description: 'Finish the SUSE e2e script: stages 6-9 were never written because the dispatch was blocked at stage 5, and that block is now cleared',
  phases: [{ title: 'Finish', detail: '补完 SUSE 端到端脚本的阶段 6-9' }],
}

const WT = 'E:/src/lober-wt/n9-suse-e2e-finish'

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

phase('Finish')

const r = await agent(`你是 Lober 项目的端到端验证脚本实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：${WT}
  分支：    feat/n9-suse-e2e-finish

**所有操作都在这个 worktree 里**（绝对路径，或先 \`cd ${WT}\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录。**

## 背景：脚本只写到阶段 5 就停了，因为当时被真实阻断卡住

\`scripts/e2e-suse.sh\`（357 行）由上一轮一位 agent 写成。它跑到阶段 5（preview）
发现 **SUSE 的渲染分派没接上**：\`renderer.go\` 与 \`menu.go\` 对 \`OSFamilySUSE\`
仍返回 \`unimplementedFamilyError\`，而 \`AssembleAutoYaST\` / \`RenderAutoYaSTMenu\`
早已实现。

它的判断是对的，也如实报告了：「阶段 6-9 全部依赖菜单/应答文件渲染成功，
在这个阻断解除之前无法继续验证，继续硬跑只会把断言改宽来掩盖这个真实缺口，
价值为负」。所以**阶段 6-9 从来没被写出来**，脚本在阶段 5 的 else 分支里 \`fail\` 掉。

**那个阻断现在已经解除了**（编排方接上了两处分派，新增测试锁住「SUSE 用 autoyast=
而非 inst.ks=」）。实测确认：\`bash scripts/e2e-suse.sh\` 现在阶段 1-5 全 PASS、exit 0，
阶段 5 的 pass 文案还写着「意外——需要人工复核 menu.go/renderer.go 是否已经改动」。

**你的任务就是把阶段 6-9 补完，并把阶段 5 那句过期的「意外」文案改正。**

## 仓库现状

\`dev\` 分支：17 个 Go 包 \`go test ./...\` 全绿，\`gofmt\` 干净。
四条端到端脚本全部 exit 0：\`e2e.sh\`(18 PASS)、\`e2e-debian.sh\`(18)、
\`e2e-ubuntu.sh\`(20)，另有 \`e2e-windows.sh\` 与 \`e2e-esxi.sh\`。

## 你拥有的文件（只有这些）

  scripts/e2e-suse.sh
  scripts/e2e/mkfixture-suse.sh（若存在；不存在且你需要就新建）
  docs/real-machine/16-e2e-suse.md（你的报告；若已存在就更新它）

**不要碰任何 Go 代码、不要碰其它 e2e 脚本**（\`scripts/e2e.sh\` 是 RHEL 基线与回归基准，
其余四条都已跑通）。**发现 Go 侧的 bug 就写进 findings 与 contractGaps**，由编排方处理
—— 你是验证轨。上一轮 Ubuntu 与 SUSE 脚本都是这样发现真 bug 的（cloud-init 的
meta-data 404、SUSE 分派漏接），编排方据此修好了 Go 侧。**诚实报告失败比让脚本变绿有价值。**

## 要补的阶段 6-9（照 \`scripts/e2e.sh\` 与 \`e2e-debian.sh\` 的形状写）

  6 四条准入负面用例
  7 /v1/boot 命中签发 token，下发 SUSE 菜单
  8 取 autoinst.xml 应答文件 + 装机期七信号 + 重启后四信号
  9 终态 done + 装完再引导拿 sanboot + 应答文件不再提供 + 事件时间线

**四条安全负面用例一条都不能省**（SPEC §9.1 三道闸门 + §10 失败模式 1/2b）：
- 陌生机器请求 \`/v1/boot\` → \`no_task\`，响应体**逐字符含那三行 sanboot 脚本**
- **SN 对但 MAC 不对 → 拒签 token**（双因子，§4.3 硬要求）
- 架构/固件不符 → 硬拒绝且**不消耗 attempt_count**（§4.6：重试必然同样失败）
- **装完的机器再次引导拿到 sanboot 而不是装机菜单**（§10 失败模式 1：
  BIOS/MBR 下没有 efibootmgr，装完的机器完全靠这一行回本地盘）

## SUSE 特有的断言

- **应答文件是 \`autoinst.xml\`，不是 \`ks.cfg\`**（\`model.AnswerFileName\` 已规定）。
  引导参数是 \`autoyast=\` 而非 \`inst.ks=\`。
  **SPEC 反复警示：绝不能因为都被口头叫「ks」就把 SUSE 当 kickstart** ——
  AutoYaST 是独立的 XML 格式，语法完全不同，安装器根本不认。
- **断言菜单里有 \`autoyast=\` 且没有 \`inst.ks=\`**（编排方刚在
  \`internal/render/menu_test.go\` 加了同名的单元测试，端到端再验一次）。
- **断言取到的应答文件是合法 XML**：用 \`go run\` 临时跑一个调 \`encoding/xml\` 的
  小程序解析它（\`go.mod\` 里的依赖可用；临时文件写在 \`${WT}/.tmp/\` 下，用完删）。
  **同时断言它不含 kickstart 特征**（比如 \`%packages\`）。
- AutoYaST 的 hook 阶段对应 \`<scripts>\` 下的 \`pre-scripts\` / \`chroot-scripts\` /
  \`post-scripts\` / \`init-scripts\`（与 kickstart 的四阶段不是一一对应）。
  **验证渲染出的 XML 里这几段真的都在。**
- 引导制品是 \`linux\` + \`initrd\`（openSUSE 命名，不是 RHEL 的 \`vmlinuz\` + \`initrd.img\`）。

## 两件本轮已经变了的事

1. **制品 URL 现在带制品集 ID**：\`/artifacts/<制品集ID>/<裸文件名>\`。
   \`RenderContext.SetArtifactURL\` 负责拼这一层，抽取器写回的是裸文件名。
   造假制品树时目录结构要对上（**读 \`scripts/e2e.sh\` 看 RHEL 是怎么造的**）。
2. **ESXi 与 Windows 的导入封锁已解除**，SUSE 本来就在 \`familyExtractors\` 表内 ——
   所以**优先走真实的登记→导入→ready 路径**。若走不通（比如 \`MountExtractor\`
   仍是「未实现真实挂载逻辑」的骨架），退回 \`sqlite3\` CLI 绕法，
   **并在报告里明确写出走不通的确切原因** —— 那是一个真实的发现。
   现有脚本的阶段 4 已经这么做了，读它确认当前状态。

## 铁律

1. **严禁 rm -rf，严禁删除任何目录。** 临时文件写在 \`${WT}/.tmp/\` 下，用完只删你建的那个。
2. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
3. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。
4. **本机没有 jq**（curl 与 sqlite3 有），用 shell 文本处理解析 JSON —— 现有脚本已经
   这么做了（它有 \`json_field\` 辅助函数），照它的方式。
5. Bash 的 PATH 快照不含某些目录：Node 用
   \`export PATH="/c/Program Files/nodejs:$PATH"\`；pwsh 用绝对路径
   \`/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe\`
   （**禁止 powershell 5.1**，它输出 GB2312，中文全乱码连错误信息都乱）。
6. 中文注释与输出。

## 验证

**\`bash scripts/e2e-suse.sh\` 必须 exit 0 且九阶段全 PASS**（当前只到阶段 5）。
另跑一次 \`bash scripts/e2e.sh\` 确认你没碰坏 RHEL 基线。
把两次运行的 PASS 计数与 exit code 写进 verifyOutcome。

**跑不通也要如实交付**：写清卡在哪一步、什么报错、排查到什么程度。
**绝不要为了让脚本变绿而削弱断言** —— 那是最坏的结果。

全绿后 commit。`, { label: 'n9:finish-suse-e2e', phase: 'Finish', schema: R, effort: 'high' })

return r ? {
  files: r.files, commits: r.commits, outcome: r.verifyOutcome,
  findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished, notes: r.notes,
} : '失败'
