export const meta = {
  name: 'lober-win-sources-retry',
  description: 'Retry: write the production WindowsSourcesResolver and WimArchDetector so Windows import actually works',
  phases: [{ title: 'Implement', detail: 'Windows 外部文件解析器与位宽探测器的生产实现' }],
}

const WT = 'E:/src/lober-wt/m2-win-sources'

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

phase('Implement')

const r = await agent(`你是 Lober 项目的实现者。Windows 主机，bash shell。

## 这是一次重跑，上一轮同任务零产出

上一轮派了同样的任务，那个 agent **一行代码都没写**，只返回了一份「进度检查报告」
（内容是「任务尚未开始」「四个文件全部不存在」「working tree clean」）。
它把任务读成了「去检查进度」，而不是「去实现」。

**你的任务是实现，不是检查。** 完成的标准是：写出代码、写出测试、跑通验证、提交 commit。

## 工具链（重要，会让你误判成代码坏了）

**跑任何 go 命令前先设 PATH**：

    export PATH="/d/golang/bin:$PATH"

本机有两份 Go：\`/d/golang/bin/go\` 是 1.27.1（与它的 \`pkg/tool\` 一致，可用），
而 PATH 默认命中 \`/d/msys/ucrt64/bin/go\`（1.26.4）。后者会去调 \`D:\\golang\` 的
compile（1.27.1），于是**每个包都报**
\`compile: version "go1.27.1" does not match go tool version "go1.26.4"\`，
看起来像代码全崩了，实际一行代码都没问题。**先设 PATH 再跑。**

## 你的工作区

  worktree：${WT}
  分支：    feat/m2-win-sources

**所有操作都在这个 worktree 里**（绝对路径，或先 \`cd ${WT}\`）。
**绝不读写 E:\\src\\lober 主仓库或 E:/src/lober-wt/ 下其它目录** —— 另有一条轨在并行。

## 仓库现状

\`dev\` 17 个包全绿。五条端到端脚本 exit 0（RHEL 18 PASS、Debian 18、Ubuntu 20、
Windows 19、ESXi 21）。你的 worktree 基于稍早的 dev，但 Windows 相关部分没变。

## 要解决的缺口

\`internal/imports/job.go\` 的 \`Job.ConfigureWindows(resolver, detector)\` 已经存在，
但**生产代码里零调用点** —— \`runWindows\` 因此明确报错：
「识别到家族 windows，但导入 job 尚未调用 ConfigureWindows 完成配置」。
这个报错是刻意设计（不静默成功、不 panic），但意味着 **Windows 的导入路径走不通**。

缺两个依赖的生产实现：

1. **\`WindowsSourcesResolver\`** —— 按制品集 ID 返回 \`WindowsExternalSources\`
   （wimboot / BCD / boot.sdi / boot.wim 四个文件的本地路径）。
   这四个文件**不在 ISO 里**，是运维单独准备的。
   **先 Read \`internal/imports/extract_windows.go\`**，看 \`WindowsExternalSources\`
   这个结构体到底要什么字段、\`ExtractWindows\` 怎么用它们。
2. **\`WimArchDetector\`** —— 探测 \`boot.wim\` 的固件位宽。

## 你拥有的文件（只有这些）

  cmd/lober/winsources.go 与 winsources_test.go（新建）
  cmd/lober/wimarch.go 与 wimarch_test.go（新建）
  cmd/lober/importrunner.go 与 importrunner_test.go（接线）
  cmd/lober/wire.go（只允许加）
  internal/config/config.go 与 config_test.go（**授权加 Windows 外部文件目录配置项**）
  deploy/lober.yaml.example
  deploy/README.md

**不要碰** \`internal/imports/**\`（\`ConfigureWindows\` 与 \`ExtractWindows\` 的签名不动 ——
你是给它们提供实现）、\`internal/render/**\`、\`internal/gateway/**\`、
\`internal/console/**\`、\`scripts/**\`、\`internal/core/**\`、\`go.mod\`、\`go.sum\`。

## 两处要你判断并论证的设计

### Resolver 按什么约定找文件

几个方向与代价：
- **按目录约定**：配置给一个基目录，四个文件按固定名字放在
  \`<基目录>/<制品集ID>/\` 或 \`<基目录>/\` 下。简单、运维好理解。
  **但要想清楚哪一层**：wimboot 是通用的（iPXE 项目提供，与 Windows 版本无关），
  而 BCD / boot.sdi / boot.wim 来自具体 WinPE 制品、**按固件分位宽**
  （SPEC §2.1：同一 Windows 版本要按固件导两个 artifact_set）。
  这个区别可能意味着两类文件该放不同层级。
- 配置里显式列四个路径：最直白，但每个制品集配一遍，运维负担大。
- 从数据库读：要改 schema（**禁止**）。

**选一个并在注释与 deploy/README.md 里论证。** 判据是「运维在气隙环境下真能照着做」：
SPEC §2.1 说气隙下 ISO 以「登记服务器本地路径」为主（几 GB 走浏览器体验很差），
这四个文件同理 —— 运维会 scp 上去。

### WimArchDetector 做到什么程度

真实解析 WIM 格式（MS 的压缩容器）工作量不小。两个方向：
- **最保守**：信任运维登记时填的 \`ArtifactSet.Firmware\`，detector 只做一致性校验或
  直接返回声明值。理由：§2.1 已经要求 Windows 按固件导两个制品集，那个字段就是
  运维的显式声明。
- 真读 WIM 头部。

**若判断真读做不到或不值得，就实现保守版本，并在 contractGaps 里写清
「detector 目前信任声明值，真实位宽探测是已知缺口」** —— 这比写一个半对的
WIM 解析器安全得多。

## 测试要锁住

- **四个文件都在时能解析出正确路径**
- **缺任一个时报明确错误**（指出缺的是哪个）—— \`runWindows\` 据此让整个导入失败，
  **不能假设一组默认路径继续**
- 配置项缺失或目录不存在时的行为（启动校验拒绝？运行期报错？论证并测）
- detector 的行为与你选的方案一致，且**固件声明为 \`any\` 时被拒绝**
  （§2.1：Windows 必须取具体固件值）
- **\`ConfigureWindows\` 真的被调用了**：写一个测试确认 importRunner 构造出的 Job
  遇到 Windows 时**不再返回「尚未调用 ConfigureWindows」那个错误**。
  这条是本轮成败的判据。

## 铁律

1. **严禁 rm -rf，严禁删除任何目录。** 临时文件用 \`t.TempDir()\` 或写在
   \`${WT}/.tmp/\` 下，用完只删你建的那个。
2. git 只允许 \`add\` / \`commit\` / \`status\` / \`diff\` / \`log\`。**严禁** checkout / switch /
   branch / merge / rebase / reset / stash / clean / push / worktree / 带 -f 或 --hard。
3. 禁止改 go.mod / go.sum。
4. 先写测试再写实现。断言失败信息写明「为什么这条重要」加 SPEC 章节号。
5. 中文注释与文档；标识符与文件名用英文。gofmt 干净。
6. **完成前必须真的跑验证并看到通过输出。不要声称通过。**

## 验证（先 \`export PATH="/d/golang/bin:$PATH"\`）

\`go vet ./cmd/lober/ ./internal/config/\`、\`go test ./cmd/lober/ ./internal/config/ -v -count=1\`、
\`go test ./cmd/lober/ -race\`、\`gofmt -l cmd internal/config\`、
**\`go test ./... -count=1\` 全仓**、**\`bash scripts/e2e-windows.sh\`**。

\`e2e-windows.sh\` 当前用 \`sqlite3\` 绕法造制品集（因为导入走不通）。你修好之后
**真实导入路径应该能走通了**，但那条脚本不属于你：在 findings 里写清它可以怎么改，
由编排方接线。

全绿后 commit。**只 add 你自己的文件**，提交前 \`git status\` 确认没夹带别人的东西。`,
  { label: 'm2:win-sources-retry', phase: 'Implement', schema: R, effort: 'high' })

return r ? {
  files: r.files, commits: r.commits, outcome: r.verifyOutcome,
  findings: r.findings, gaps: r.contractGaps, unfinished: r.unfinished, notes: r.notes,
} : '失败'
