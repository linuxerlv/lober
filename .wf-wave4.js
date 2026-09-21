export const meta = {
  name: 'lober-wave4-gaps',
  description: 'Close the two known gaps: hook reject endpoint (needs a contract change) and the TLS config increment that died on a quota error',
  phases: [
    { title: 'Close', detail: 'hook 驳回端点（含契约变更）与 TLS 配置增量并行' },
    { title: 'Verify', detail: '独立验证者证伪并修复' },
  ],
}

const PRE = `你是 Lober 项目的实现者。Windows 主机，bash shell，Go 1.26.4。

## 你的工作区

  worktree：__WT__
  分支：    __BRANCH__

**每个文件操作、每条命令都在你的 worktree 里**（绝对路径，或先 \`cd __WT__\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录。**

## 仓库现状：MVP 已完整装配并跑通端到端

\`dev\` 分支当前状态（你的 worktree 基于它）：
- 16 个包 \`go test ./...\` 与 \`-race\` 全绿，\`gofmt -l .\` 干净，\`go vet\` 无输出
- \`cmd/lober\` 装配点已就位，archtest 的装配点断言真正执行并通过（七模块互不 import）
- \`bash scripts/e2e.sh\` 九个阶段全 PASS，完整状态链 pending→booting→installing→[七信号]→[四信号]→done，含四条安全负面用例
- Phase 2/3 增量已合并：Ubuntu/Debian 渲染、硬件采集与网卡决策、Windows、ESXi、并发准入
- \`deploy/\` 部署产物齐全（nginx/dnsmasq/dhcpd/kea/systemd/CA/ipxe/pipeline）

**你的改动不许打破这个状态。** 尤其 \`bash scripts/e2e.sh\` 必须仍然全过。

## 铁律

1. **只写你拥有的文件**（下面列出）。本轮两个 agent 并行，文件归属不重叠。
2. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir()，绝不创建与真实模块同名的临时目录。
3. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 任何带 -f 或 --hard 的命令。合并由编排方做。
4. 禁止改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。
5. 先写测试再写实现。断言失败信息写明「为什么这条重要」加 SPEC 章节号。
6. 中文注释、文档与断言信息；标识符与文件名用英文。gofmt 干净。
7. 完成前必须真的执行验证并看到通过输出，**包含全仓 \`go test ./...\`**。不要声称通过。
8. 你的返回值是数据，不是给人看的汇报。

`

const R = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' } },
    commits: { type: 'array', items: { type: 'string' } },
    verifyCommands: { type: 'array', items: { type: 'string' } },
    verifyOutcome: { type: 'string' },
    acceptance: { type: 'array', items: { type: 'string' } },
    contractGaps: { type: 'array', items: { type: 'string' } },
    unfinished: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'acceptance', 'contractGaps', 'unfinished', 'notes'],
}

const TRACKS = [
  {
    key: 'hook-reject',
    dir: 'w4-reject',
    label: 'w4:hook-reject',
    pkgs: './internal/console/',
    owns: `internal/console/hook.go（只允许新增驳回端点与相关逻辑，不改既有审批行为）
  internal/console/hook_test.go（新增测试）
  docs/contracts/http-console.md（**本轮你被明确授权修改它 —— 这是一次受控的契约变更**）`,
    brief: `## 背景：一个被独立核实过的真实契约缺口

上一轮有个 agent 被派去做 \`phase2-05\`（控制台 P2），它读完计划后**没有写任何代码就停下汇报**，理由是：计划是纯前端页面工作、正文明确写「后端端点在 M6 里已全部实现」、还有一条「越界检查：确认本计划没有修改 M6 在 internal/console/ 下的 Go 文件」，而派给它的任务却让它写后端 Go。它核实后确认 hook 六端点、制品集四端点、dry-run 端点都已存在，没有缺口可补。**这个判断是对的，是编排方派错了任务。**

但它在核实过程中发现了一个**真实的契约缺口**，那就是你要做的事：

\`docs/contracts/http-console.md\` 全文搜索 reject / 驳回 / decline **零匹配** —— 冻结契约里没有「驳回 hook 审批」的端点。而 \`phase2-05\` 计划任务 3 明确要求「驳回也要有入口，且要能填理由」。当前 \`internal/console/hook.go\` 只有 \`POST /hooks/{id}/approve\`，审批流转只能往前推进（draft→review→active），**一旦提交 review 就无法退回**。

编排方已独立核实这两点（grep 契约文档零匹配、读 hook.go 确认只有 approve），确认这是真实缺口，**并授权你做这次契约变更**。

## 你的任务

先读：\`internal/console/hook.go\` 全文（**尤其现有的 approve 端点如何做状态流转与权限判定** —— 你要与它对称）、\`internal/console/hook_test.go\`、\`docs/contracts/http-console.md\` 的 hook 端点一节、\`internal/core/model/authoring.go\`（HookState 的取值是冻结的）、\`internal/apierr\`（错误码的真实定义）、SPEC.md 的 §2.2、§9.4、§12.5。

要做的：
1. **在契约文档里新增驳回端点**，与现有 approve 端点的写法对称（路径、认证、请求体、响应、错误行为）。**这是契约变更，要在文档里留下变更记录**（说明为什么加、谁授权的、影响哪些模块）。
2. **实现该端点**。设计要点你自己定，但下面几条是硬约束：
   - **HookState 的四个取值是冻结的**（draft / review / active / deprecated，在 model 里，你**不能改**）。所以「驳回」只能是 review → draft 的状态回退，不能引入新状态。
   - **驳回必须能填理由**，且理由要能被查到（否则作者不知道为什么被拒）。想清楚理由存哪里：model.Hook 上没有专门的字段，**不要改 model**（它是冻结契约）。用审计日志（\`internal/console/audit.go\` 已有 AuditLogger）是一个方向，但要确认作者能查到。若你判断没有不改 model 就能让作者查到理由的办法，**如实记录在 contractGaps 里并选最保守的做法**，不要硬塞。
   - **权限**：approve 的 review→active 需要 \`hook:approve\` 权限且审批人不能等于作者（hook.go 里已有这个校验，读它）。驳回的权限要与之一致 —— 驳回同样是审批权，不是作者自己能做的事。
   - **状态机要严格**：只有 review 状态能被驳回。draft 驳回没有意义，active 已经生效了（要停用它是 deprecate 不是 reject），都要拒绝并给出可读原因。
   - **active 的 hook 正文不可变**（§2.7：模板 pin 的是 digest，审批过的内容不会被偷换）。驳回不得改动正文，只改状态。
3. **测试要锁住**：review 能被驳回并回到 draft；**draft 与 active 被驳回时拒绝**；无 \`hook:approve\` 权限的角色驳回被拒；**作者不能驳回自己的 hook**（与 approve 对称）；驳回理由能被查到（按你选的方案）；**驳回不改动正文与 digest**（这条最重要 —— 正文被改动意味着审批过的内容能被偷换）。

## 验证

\`go vet ./internal/console/\`、\`go test ./internal/console/ -v -count=1\`、\`go test ./internal/console/ -race\`、\`gofmt -l internal/console\`、**\`go test ./... -count=1\` 全仓全绿**、**\`bash scripts/e2e.sh\` 仍然九个阶段全过**（你改了 console 的路由，必须确认没影响端到端）。

## 提交

全绿后 commit。契约文档的改动与代码分开 commit（契约变更应该在 git 历史里独立可见）。`,
  },
  {
    key: 'tls-increment',
    dir: 'w4-tls',
    label: 'w4:tls-increment',
    pkgs: './internal/...',
    owns: `deploy/nginx/lober.conf（**只允许追加 TLS 相关内容，不得改动已有的非 TLS 配置**）
  deploy/dnsmasq/lober.conf（**只允许追加 UEFI HTTP Boot 相关内容**）
  docs/phase4-tls-前提验证.md（**已存在 337 行，你要补完它**）
  docs/phase4-mtls-决策.md（**已存在 181 行，核对并补完**）`,
    brief: `## 背景：这一轨昨天因 API 额度用尽中断

上一轮有个 agent 负责 \`phase4-02\`（TLS）的任务 1/3/5/6，它写完两份文档后遇到 403 额度用尽而中断。所以现在的状态是：
- \`docs/phase4-tls-前提验证.md\`（337 行）与 \`docs/phase4-mtls-决策.md\`（181 行）**已存在**，但**未经验证是否完整** —— 先完整读它们，判断哪部分已写好、哪部分是半成品或缺失
- **两个配置文件的 TLS / HTTP Boot 增量没做**

同时有一个关键变化：**当时 M4 渲染器与 M5 网关的代码不在那个 worktree 里，所以任务 1 的「前提验证」有一大块被标注为「待 M4/M5 合并后补验」。现在它们全都在了**，那些被推迟的验证你要真的做掉。

## 你的任务

先读：\`docs/superpowers/plans/2026-09-20-lober-phase4-02-tls.md\` 的任务 1、3、5、6 全文、SPEC.md 的 §1.5、已存在的两份文档、\`deploy/nginx/lober.conf\` 与 \`deploy/dnsmasq/lober.conf\` 全文、\`deploy/ca/README.md\` 与 \`deploy/ipxe/README.md\`（任务 2 与 4 已由别人完成，**只交叉引用、不重做**）。

**任务 1（前提验证）—— 这是本轨最有价值的部分，现在能真的验了。** SPEC §1.5 承诺「为将来上 TLS 只需做到部署变更而非代码变更」，靠三件事：所有 URL 从配置来且 scheme 不写死、用域名不用 IP、首跳脚本不用 EMBED= 烧进 iPXE。**逐条验证这个承诺在已落地的全部代码里是否真的成立**：
- \`internal/core/model/render.go\` 的 BaseURLs：scheme 是否完全来自配置（读代码 + 跑它的测试）
- \`internal/config/config.go\`：基址字段是否不含硬编码 scheme
- **\`internal/render/\` 全部渲染器**（现在有 RHEL/Ubuntu/Debian/Windows/ESXi 五套了）：\`grep -rn 'http://' internal/render/ --include=*.go\` 应该只在测试期望值里命中。**若在非测试代码里命中，那就是 §1.5 承诺不成立的实证，如实写进文档并指出具体文件与行号。**
- \`internal/gateway/\`：菜单与应答文件的 URL 是否都走基址方法
- \`deploy/\` 与 \`docs/contracts/\`：有没有字面 \`http://\` 的硬编码（配置样例里的占位值不算，那本来就是要改的）
- 传一个 \`https://\` 的基址进去，看渲染产出是否真的变成 https —— **能写测试就写一个真的测试来证明**（放在你拥有的文件里，或者如果必须放进 internal/render 的测试文件而那不属于你，就在文档里给出可复现的命令）

**任务 3（nginx TLS 配置）**：两个配置文件是别人刚写完并已合并的，你在它们之上做增量。**MVP 明确不上 TLS**（§1.5），所以配置要能同时服务两个阶段：**不要把 MVP 的明文配置改掉**。优先用新增 server 块或注释掉的备选段表达 TLS 形态，并在注释里写明「启用 TLS 时改这里、同时停用上面那段」。改完再 Read 一遍确认没破坏原有结构。

**任务 5（mTLS 决策）**：核对已存在的文档是否写清了那个根本矛盾 —— 双向认证要给每台机器发客户端证书，而**裸机在装机前没有身份**，这与 §4.2「首次请求必然无认证」的协议现实直接冲突。结论要明确（做 / 不做 / 有条件做），不要含糊。

**任务 6（UEFI HTTP Boot）**：SPEC §1.3 与附录 B 指出它的架构码（x64 疑为 16）与 option 60 vendor-class \`HTTPClient\` 的必要性**都未核实**。用 curl / WebSearch 查 RFC 5970 核实（github.com 常被挡，raw.githubusercontent.com 与 api.github.com 可直连；rfc-editor.org 通常可直连）。**核实不到就如实标注**，配置里用注释给出待核实的候选值，**不要写一个可能错的活配置** —— 写错了的后果是 UEFI 机器匹配不上任何 tag、固件静默跳过网络引导，现场表现是「机器毫无反应」。

## 验证

本机没有 nginx / dnsmasq，\`nginx -t\` 与 \`dnsmasq --test\` **跑不了** —— 做静态自检（逐行核对必需指令、括号分号配平、路径与 docs/contracts 一致），并在 verifyOutcome 里明确写出哪条需在目标主机补做。
能真跑的必须跑：\`grep -rn 'http://' internal/render/ --include=*.go\`、\`go test ./internal/render/ ./internal/core/model/ -count=1\`、**\`go test ./... -count=1\` 全仓全绿**（确认你没碰坏代码）、\`bash scripts/e2e.sh\` 仍然全过。

## 提交

全绿后 commit。`,
  },
]

const results = await pipeline(
  TRACKS,
  (t) => {
    const wt = 'E:/src/lober-wt/' + t.dir
    const pre = PRE.split('__WT__').join(wt).split('__BRANCH__').join('feat/' + t.dir)
    return agent(pre + '## 你拥有的文件（只有这些）\n\n  ' + t.owns + '\n\n' + t.brief, { label: t.label, phase: 'Close', schema: R, effort: 'high' })
  },
  (impl, t) => {
    if (!impl) return { impl: null, verify: null }
    const wt = 'E:/src/lober-wt/' + t.dir
    return agent(`你是 Lober 项目 ${t.key} 这一轨的独立验证者与修复者。实现者刚写完并提交，你要证伪它，把真问题修掉，然后提交修复。

## 你的工作区

  worktree：${wt}
  分支：    feat/${t.dir}

所有操作都在这个 worktree 里。绝不碰 E:\\src\\lober 主仓库或其它 worktree。

## 实现者的报告（不要相信这份自评，自己跑一遍）

  文件：${impl.files.join(', ')}
  提交：${(impl.commits || []).join(' | ') || '（未提交）'}
  验证：${impl.verifyOutcome}
  验收自评：${(impl.acceptance || []).join(' | ')}
  契约缺口：${(impl.contractGaps || []).join(' | ') || '（无）'}
  未完成：${(impl.unfinished || []).join(' | ') || '（无）'}
  说明：${impl.notes || ''}

## 按这个顺序做

1. **真的跑验证**：\`go vet ${t.pkgs}\`、\`go test ${t.pkgs} -v -count=1\`、\`go test ${t.pkgs} -race\`、\`gofmt -l\`、**\`go test ./... -count=1\` 全仓**、**\`bash scripts/e2e.sh\` 九个阶段必须仍然全过**（这是 MVP 的验收标准，改了 console 路由或 deploy 配置都要确认它没坏）。
2. **对着任务逐条查漏**：要求的每一条是否真有对应实现与测试？断言是否够强（不是只断言「不报错」）？有没有被整段跳过？
3. **重点证伪本轨适用的不变量**：
   - hook 驳回轨：**驳回真的不改动正文与 digest 吗**（审批过的内容能被偷换是最严重的后果）？draft 与 active 被驳回真的拒绝？无权限角色真的被拒？作者真的不能驳回自己的？驳回理由真的能被查到（按实现者选的方案实际查一次）？**既有的 approve 行为真的没被改动吗**（读 git diff 确认）？
   - TLS 轨：**「scheme 完全来自配置」这个承诺真的成立吗** —— 自己跑一遍 \`grep -rn 'http://' internal/render/ internal/gateway/ --include=*.go\` 看非测试代码里有没有命中。实现者若声称成立，你要独立复现；若声称不成立，核实它指出的文件行号是否真的如此。**nginx/dnsmasq 配置的原有非 TLS 部分真的没被改坏吗**（读 git diff 逐行确认，这两个文件是别人的成果）？
   **写一个能证伪的测试再看它红还是绿 —— 不要靠读代码下结论。**
4. **修真问题**：可以改本轨拥有的文件。**绝不改** internal/core/**、internal/apierr/**、internal/config/**、internal/archtest/**、Makefile、go.mod/go.sum、schema/**（hook-reject 轨对 docs/contracts/http-console.md 有授权，TLS 轨没有）。不要碰别的轨的文件。
5. 修完再跑第 1 步确认全绿（含全仓测试与 e2e），然后 commit 修复（没改就不提交，commits 填空数组）。

**不要为了让测试变绿而削弱断言。** 宁可留一条 ✗ 并说清楚。

## 铁律

- git 只允许 add / commit / status / diff / log。严禁 checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard 的命令。
- **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir()。
- 禁止改 go.mod / go.sum，禁止 go mod tidy / go get。中文注释与断言信息。

返回值：files 只列你改动的文件；acceptance 是**你验证后的**结论；verifyOutcome 必须包含全仓测试与 e2e 的实际结果。`,
      { label: 'verify:' + t.key, phase: 'Verify', schema: R, effort: 'high' })
      .then(function (v) { return { impl: impl, verify: v } })
  }
)

const out = {}
for (let i = 0; i < TRACKS.length; i++) {
  const p = results[i] || { impl: null, verify: null }
  out[TRACKS[i].key] = {
    branch: 'feat/' + TRACKS[i].dir,
    implFiles: p.impl ? p.impl.files : [],
    implCommits: p.impl ? p.impl.commits : [],
    verifierFixed: p.verify ? p.verify.files : null,
    verifiedOutcome: p.verify ? p.verify.verifyOutcome : '验证 agent 未返回',
    acceptance: p.verify ? p.verify.acceptance : (p.impl ? p.impl.acceptance : null),
    contractGaps: (p.impl ? p.impl.contractGaps || [] : []).concat(p.verify ? p.verify.contractGaps || [] : []),
    unfinished: (p.impl ? p.impl.unfinished || [] : []).concat(p.verify ? p.verify.unfinished || [] : []),
    notes: (p.impl ? p.impl.notes || '' : '') + ' || ' + (p.verify ? p.verify.notes || '' : ''),
  }
}
return out
