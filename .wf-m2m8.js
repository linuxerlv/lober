export const meta = {
  name: 'lober-m2m8-modules',
  description: 'Implement Lober M2-M8 in seven isolated git worktrees, each on its own feature branch, implement then adversarially verify',
  phases: [
    { title: 'Implement', detail: '七个模块在各自的 git worktree 与特性分支上并行实现' },
    { title: 'Verify', detail: '独立验证者在同一 worktree 里证伪并修复，然后提交' },
  ],
}

const WT = 'E:/src/lober-wt/'

const PREAMBLE = `你是 Lober 项目的模块实现者。Windows，bash shell，Go 1.26.4。

## 你的工作区是一个独立的 git worktree

**主仓库在 E:\\src\\lober（分支 dev），但你不在那里工作。** 你有一个专属的 git worktree 与特性分支，其它六个模块各有自己的 worktree，彼此完全隔离——所以你不会和任何人冲突，也看不到别人未合并的工作。

  你的 worktree：__WT__
  你的分支：    __BRANCH__

**每一个文件操作、每一条命令都必须在你的 worktree 里**。用绝对路径，或先 \`cd __WT__\`。绝对不要读写 E:\\src\\lober 下的任何东西——那是主仓库，改它会污染别人。

## M1（契约与地基）已完成并已 commit 到 dev，你的 worktree 里已经有它

  internal/core/model/**            领域类型（TaskState / FailureReason / Phase / 信号表 / 预算表 / 固件矩阵 / fact 键 / RenderContext …）
  internal/core/contract/**         跨模块接口（Store 窄接口族 / StateMachine / Renderer / Prechecker / Clock / provider 七接口 / sentinel error）
  internal/core/contract/conformance/store.go   Store 行为契约可执行套件（20 个子测试）
  internal/core/fake/**             内存 Store + 可控时钟（你的测试夹具）
  internal/store/sqlite/schema/001_init.sql + migrate.go   数据库 schema 与迁移
  internal/apierr/**                人面响应包裹与错误码
  internal/config/**                配置结构与启动校验
  internal/archtest/deps_test.go    依赖方向守卫
  Makefile                          build / test / race / vet / fmt / check
  docs/contracts/http-machine.md    机器面 HTTP 契约（冻结）
  docs/contracts/http-console.md    人面 HTTP 契约（冻结）
  docs/contracts/filesystem.md      磁盘布局契约（冻结）

必读（用 Read 工具，不要 cat）：
- 你自己的模块计划（路径见下）——逐任务实现，它是你的唯一实现依据
- M1 产出中与你相关的源码。**动手前一定先 Read 真实的接口定义**，不要凭计划里的描述猜签名。
- SPEC.md 里你的计划引用到的章节（按需精读，它有 1765 行，不要全文读）

## 铁律

1. **契约只读。** 禁止修改 internal/core/**、internal/apierr/**、internal/config/**、internal/store/sqlite/schema/**、internal/store/sqlite/migrate.go、internal/archtest/**、docs/contracts/**、Makefile、go.mod、go.sum。发现契约缺东西时**不要在本模块加变通**：在返回值的 contractGaps 里写清「缺什么、你临时怎么绕的、为什么这是最保守的绕法、要改哪个接口才算正解」，然后用不改契约的方式把本模块做完。契约漂移是并行开发唯一的致命风险。
2. **只写你拥有的文件**（见下）。你的 worktree 里虽然有全仓代码，但别的模块的目录归别人，你在自己分支上改了也会在合并时制造冲突。
3. **验证只跑你自己的包**：\`go test ./<你的包>/...\`。别的模块的目录在你的分支上是空的（它们的代码在别人的分支上），所以 \`go build ./...\` 不会报它们的错，但也不要指望能测到它们。**不要跑 make check**——它跑全仓测试，在合并前没有意义。
4. **git：只允许这三件事**——\`git add <你自己的文件>\`、\`git commit\`、\`git status\` / \`git diff\` / \`git log\`。**严禁**：checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 任何带 -f 或 --hard 的命令。你的分支只装你的模块，合并由编排方做。
5. **严禁 rm -rf，严禁删除任何目录。** 需要临时文件就用 \`go test\` 的 t.TempDir()，或在 \`__WT__/.tmp-<你的模块名>/\` 下建，用完只删你自己建的那个具体文件。**绝不要创建与真实模块同名的临时目录**（比如叫 internal/store、internal/render 的样例包）——上一轮有 agent 这样做，清理时连真实代码一起删掉了。
6. 禁止修改 go.mod / go.sum，禁止 \`go mod tidy\` / \`go get\`。可用依赖：modernc.org/sqlite、github.com/google/go-cmp、github.com/google/uuid、gopkg.in/yaml.v3、golang.org/x/crypto（bcrypt）、golang.org/x/net（icmp）。需要别的就改用标准库并在 contractGaps 里说明。
7. **import 白名单**：只允许 model、contract、标准库，加上你的计划明确允许的（测试可用 internal/core/fake）。**绝不 import 另一个实现模块**（store / statemachine / render / gateway / console / provider / imports 互不可见）——它们只通过 contract 的接口通信，这是七个模块能并行的全部保障，internal/archtest 会在合并后的 CI 里拦住违反。
8. 先写测试再写实现（TDD）。计划里每个任务的「测试要锁住的行为」逐条都要有对应测试，断言失败信息写明「为什么这条重要」加 SPEC 章节号。
9. 注释、文档、测试失败信息用中文；标识符、包名、文件名用英文。gofmt 必须干净。
10. 完成前必须真的执行验证命令并看到通过输出。不要声称通过。跑不了的验证在 verifyOutcome 里写明原因。
11. 你的返回值是数据，不是给人看的汇报。

`

const RESULT = {
  type: 'object',
  properties: {
    files: { type: 'array', items: { type: 'string' }, description: '创建或修改的文件（worktree 内相对路径）' },
    commits: { type: 'array', items: { type: 'string' }, description: '你做的 commit 的 sha 与标题；没提交就空数组' },
    verifyCommands: { type: 'array', items: { type: 'string' }, description: '实际执行过的验证命令原文' },
    verifyOutcome: { type: 'string', description: '逐条验证结果；哪条没过、哪条跑不了及原因' },
    acceptance: { type: 'array', items: { type: 'string' }, description: '计划「交付验收」清单逐条的达成情况，格式：✓/✗ 条目 —— 证据' },
    contractGaps: { type: 'array', items: { type: 'string' }, description: '发现的契约缺口与你的保守处理；没有就空数组' },
    unfinished: { type: 'array', items: { type: 'string' }, description: '没做完或没法做的任务点及原因；没有就空数组' },
    notes: { type: 'string', description: '合并与集成（M9）时必须知道的事' },
  },
  required: ['files', 'commits', 'verifyCommands', 'verifyOutcome', 'acceptance', 'contractGaps', 'unfinished', 'notes'],
}

const MODULES = [
  {
    key: 'M2-store',
    dir: 'm2-store',
    label: 'M2:sqlite-store',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-02-store.md',
    pkgs: './internal/store/sqlite/',
    owns: `internal/store/sqlite/store.go、errors.go、scan.go、task.go、event.go、fact.go、host.go、artifact.go、hook.go、template.go、tx.go、store_test.go、concurrency_test.go
  （**不要动同目录下 M1 的 schema/001_init.sql 与 migrate.go / migrate_test.go**——它们是数据契约）`,
    brief: `目标：用 SQLite 实现 contract.Store 与事务运行接口，**通过 M1 的 conformance 套件**——套件是你的验收标准。先写 store_test.go 调用它（会全红），再逐个接口实现让它变绿。

重点：
- **约束冲突按索引名匹配翻译成 sentinel error，不要匹配错误文本**（文本随驱动版本变）：SN partial unique → ErrActiveTaskExists、(task_id,seq) unique → ErrDuplicateSeq、外键 → ErrStillReferenced。
- CAS 用一条带 state 与 version 条件的 UPDATE，影响行数为 0 时重读并区分「不存在」与「冲突」。
- 改运行时字段的方法携带 state 必须报错（不留绕过 CAS 的路径）。
- 超时扫描的 SQL 必须排除无预算的阶段（pending 永不超时）。
- 身份查找 SN→MAC→UUID 回退、空键跳过、只匹配 pending。
- 制品集「改状态」与「写回 OSFamily+Paths」两个方法分开：导入失败只改状态、不清空 Paths。
- 事件 seq 非正表示非信号来源，由实现分配、不参与去重。
- **禁止 ALTER TABLE**：schema 是数据契约，改它会让内存实现与 SQLite 实现行为分叉，而 conformance 套件发现不了这种分叉。
- 时间列存 Unix 秒、结构化列存 JSON——JSON 往返容易掉字段，逐字段验。
- token 过期判断读真实时钟（conformance 套件的时间基准已锚定运行时的 now，所以你读真实时钟就与套件一致）。`,
  },
  {
    key: 'M3-statemachine',
    dir: 'm3-statemachine',
    label: 'M3:statemachine',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-03-statemachine.md',
    pkgs: './internal/statemachine/',
    owns: 'internal/statemachine/ 下全部文件：machine.go、token.go、precheck.go、boot.go、signal.go、lifecycle.go、scan.go 与各自的 _test.go',
    brief: `目标：实现 contract.StateMachine、TokenIssuer 与 Prechecker。你是 task.state 的唯一所有者——网关与控制台都只通过你改状态。

重点：
- **开始引导的顺序是硬约束**：先 precheck（不过则置 failed 且**不消耗 attempt**，因为重试必然同样失败）→ attempt++ → 超上限（3）则 failed → 签 token → CAS 到 booting。
- **禁止重装落在 retry 里**：reboot_timeout / init_timeout / init_failed 三种必须拒绝——这些机器已经装好、盘上可能有数据，重装会擦掉。拒绝在状态机里，不是靠 UI 隐藏按钮。六种可重装的要接受。
- 标记应答文件已取必须**幂等**（Ubuntu 的 cloud-init 会取两个文件）。
- 应用信号的三种幂等情形：seq 重复、序号不大于水位、终态任务收到信号——都只记事件不改状态。返回值把「已接受」与「改变了状态」分开。
- booting 超时：attempt 未达上限回 pending（机器可自行再来一次），达上限则 failed(attempt_exceeded)。计划已就这处 SPEC 张力做了决断，照它执行。
- 超时扫描**永不触碰 pending 任务**，返回本轮判失败的任务数；置失败时写带 §3.5 六个字段的诊断事件。
- **时间全部来自注入的 Clock，实现代码里不得出现 time.Now()**（用 \`grep -rn 'time.Now()' internal/statemachine/\` 自查，只允许出现在测试夹具里）。
- CAS 冲突要有重试骨架（重读 → 重新判定 → 再试），但绝不无条件覆盖。
- token 签发用 crypto/rand，不可预测。

测试用 internal/core/fake 的内存 Store 与可控时钟。`,
  },
  {
    key: 'M4-render',
    dir: 'm4-renderer',
    label: 'M4:renderer',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-04-renderer.md',
    pkgs: './internal/render/...',
    owns: 'internal/render/ 下全部文件（含 guard/validate.go 与 testdata/*.golden）',
    brief: `目标：实现 contract.Renderer——输入 RenderContext，输出 iPXE 菜单与 kickstart 应答文件。**纯函数：不查库、不碰时钟、不做 IO。**

重点：
- **注入防护**：form_data 的值会进 kickstart 脚本，必须按类型白名单校验（IP 格式、hostname 字符集、整数范围、枚举）。七类恶意输入逐一被拒绝且不泄漏进任何产出（\`; rm -rf /\`、换行注入、\`$(...)\`、反引号、%end 提前闭合、模板语法、超长串）。
- **产出中不含字面 http://**：所有 URL 从 RenderContext 的基址方法拼出来（\`grep -rn 'http://' internal/render/ --include=*.go\` 只允许在测试期望值里出现）。
- 三段结构：prologue（参数内联 + syslog 指向）、用户 hook、epilogue（firstboot 脚手架 + 凭据清理）。**系统必需动作由渲染器生成、不是 hook**，用户看不见、不可编辑、不可绕过。
- 注入的 \`signal()\` 函数**末尾必须返回成功**——遥测失败绝不杀装机；\`run_hook\` 用管道状态数组（PIPESTATUS）取退出码，不是 \$?，并用 sha256sum -c 校验下载的脚本。
- firstboot 脚手架用 ConditionPathExists + 成功后删标记文件保证只跑一次；用户的 firstboot hook 脚本与参数必须在 %post 就落进目标系统（重启后装机 VLAN 未必可达）。
- 磁盘设备只用 by-id/by-path/WWN，不用内核分配的名字。业务网**写配置不激活**（激活会让机器当场失去到网关的连接，后续信号全丢）。
- golden file 基础设施带 -update 开关；三份 golden 与输出一致且 -update 后无 diff。
- MVP 只真正实现 RHEL 家族（Rocky 9）；其余五个家族留分支返回「未实现」错误，**绝不把 suse 路由到 kickstart 渲染器**（AutoYaST 语法完全不同，产出的文件安装器根本不认）。

额外自查：代码中无 os. 与 net. 调用。`,
  },
  {
    key: 'M5-gateway',
    dir: 'm5-gateway',
    label: 'M5:gateway',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-05-gateway.md',
    pkgs: './internal/gateway/',
    owns: 'internal/gateway/ 下全部文件',
    brief: `目标：实现机器面六个 HTTP 端点与 syslog UDP 接收器。契约是 docs/contracts/http-machine.md——**先完整读它**，路径、响应体、状态码按它实现，不自己发明。

重点：
- **不 import internal/statemachine 与 internal/render**：只依赖 contract 的接口，测试用自己的桩（stub_test.go）。
- **/v1/boot 始终返回 200**：iPXE 对非 200 会跳到 \`|| shell\`，把机器停在交互式提示符上等人。
- **no_task 响应体逐字符等于 contract 里的 no_task 脚本**，那行 \`sanboot --no-describe --drive 0x80\` 必须在——BIOS/MBR 下没有 efibootmgr，装完的机器完全靠它回本地盘。断言逐字符相等。
- **无匹配任务的请求不产生任何 Store 记录**：不建任务、不改状态、不写事件、不告警。用桩断言「一次写操作都没发生」。
- **MAC 双因子**：SN 与 MAC 都对上才签发 token；MAC 不匹配时不签发。
- **客户端 IP 只在直连 peer 是 loopback 时才信任 X-Real-IP**（含 IPv6 回环 ::1）——网关端点本身无认证，不加此判断则任何人可伪造头部冒充其它机器请求装机脚本。
- finalize token 无法发送装机期信号（用 M1 的信号权限集合判定）；重复序号的信号仍回 204（机器侧重试三次是常态，不是错误路径）。
- 应答文件端点校验路径里的文件名等于渲染上下文算出的文件名（Ubuntu 额外接受 meta-data）；状态门控仅 booting/installing。
- hook-report 的 digest query 参数不合格时**忽略该参数而非拒绝整个信号**（上报失败绝不中断装机）。
- syslog 收 UDP 514 纯文本，落日志存储，不进状态机。

计划任务 9 的手工验证服务器若需要跑起真进程，改成 httptest 内跑并在 notes 里说明。`,
  },
  {
    key: 'M6-console',
    dir: 'm6-console',
    label: 'M6:console',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-06-console.md',
    pkgs: './internal/console/',
    owns: 'internal/console/ 下全部 .go 文件（**不含 internal/console/ui/**，那是 M8 的，不要创建它）',
    brief: `目标：实现 /console/v1 下全部端点、RBAC、SSE 与审计。契约是 docs/contracts/http-console.md——**先完整读它**，端点、字段名、响应形态按它实现。响应统一包裹用 internal/apierr。

重点：
- **不 import internal/statemachine / internal/render / internal/provider**：只依赖 contract 的接口，测试用自己的桩。
- **建任务是唯一的授权动作**：入参是主机清单 id，**必须拒绝请求体里的 SN 字段**（SN 从清单取，不接受自由文本——这是删掉 armed 后唯一的人工确认点）；服务端据 id 取出 SN 与 MAC 写进任务构成双因子。
- **参数决策（TaskParamsResolver）在写库之前同步调用，失败即整体失败、库里不出现任务**（IP 没申请到不能装）。决策结果固化进任务的 resolved_params。
- retry 端点对三种不可重装失败原因必须拒绝（靠状态机拒绝，handler 把 sentinel 映射成 1008 与那句「已完成安装，重装会擦除磁盘数据」）。
- RBAC：五个角色的权限码按 SPEC §12.5 表，跨角色越权被拒；审计中间件记操作人与动作。密码用 bcrypt 比对配置里的哈希。
- 表单 schema 由三段聚合（固定字段 + template.required_form_fields + 所有 hook 的 params_schema 合并去重），输出限定为 §12.7 的 JSON Schema 子集，超出子集在 hook 注册时就拒绝（不是等渲染时静默丢字段）。
- preview 用假数据渲染，dry-run 展示磁盘布局与「为什么这么选」——这两个端点把模板调试周期从「上电等十分钟」压到秒级。
- SSE：不套响应包裹；id 取事件序号；重连带 Last-Event-ID 续传不从头重放；终态服务端主动关流；三个响应头含 X-Accel-Buffering: no；上下文取消要能退出（否则列表与详情来回切会攒住一堆连接）。
- 列表端点响应形态统一：data 就是数组、不带总数，limit 默认上限 1000。
- 主机清单入库前归一化 MAC、非法值拒绝；arch 与 firmware 必填。`,
  },
  {
    key: 'M7-providers',
    dir: 'm7-providers',
    label: 'M7:providers+imports',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-07-providers.md',
    pkgs: './internal/provider/ ./internal/imports/',
    owns: 'internal/provider/ 与 internal/imports/ 下全部文件',
    brief: `目标：实现七个 provider 接口的内置版本、建任务的同步决策编排、ISO 导入 job。允许 import internal/config（工厂按配置构造）。

重点：
- **五个磁盘策略全部实现**（first / largest / by-model / raid1-ssd / by-hook），各有边界测试（单盘、零盘、容量并列）；**raid1-ssd 不足两块盘时返回错误、不退化**（退化会装到错误的盘上）；全部返回 by-id 标识与**非空的「为什么这么选」说明**（dry-run 端点要展示）。
- **磁盘策略是纯函数**：无 IO、无时钟、无随机数——这套系统会擦盘，行为不完全确定则故障无法复现。
- 模板解析规则表按 os+version+vendor+product 匹配，**自上而下第一条命中生效**，要有测试锁住。
- 心跳：**默认 tcp**（icmp 需要特权套接字，不值得为辅助通道给整个进程提权）；**icmp 无权限时优雅降级而非 panic**。
- 事件总线是 fire-and-forget，**只订阅 ready 与 failed 两个事件**；阻塞式决策（IPAM / resolver / diskplanner）走同步编排器**不走事件总线**——任务已创建事件发出时任务已落库，那时再报错为时已晚。
- **同步决策编排器任一步失败即整体失败，不产生部分结果**。
- 导入：抽取器用接口隔离、通过**内存文件系统**测试（不依赖真 ISO）；按特征文件识别 OS 家族（.treeinfo / .disk/info / dists/ / boot.cfg+mboot.efi / sources/install.wim）；导入 job 的每种失败都有具体原因；manifest 用 sha256；落盘路径严格遵守 docs/contracts/filesystem.md。
- 失败语义按 contract 里的失败语义表，不要自己发明。

额外自查：磁盘策略所在文件无 IO / 时钟 / 随机数调用。`,
  },
  {
    key: 'M8-embed',
    dir: 'm8-embed',
    label: 'M8:go-embed+docs',
    plan: 'docs/superpowers/plans/2026-09-20-lober-mvp-08-webui.md',
    pkgs: './internal/console/ui/',
    owns: `internal/console/ui/embed.go、internal/console/ui/embed_test.go、internal/console/ui/dist/（最小占位产物）
  web/UPSTREAM_PIN.md、web/README.md
  .gitignore（**本轮只有你一个 agent 改它**，且只为给占位 dist 开一个例外）`,
    brief: `**范围受限，先读这段再读计划。** 本机没有 Node 与 pnpm（\`node -v\` 与 \`pnpm -v\` 都是 command not found），所以 M8 计划里任务 2、3、5、6、7、8、9、10、11、12、13 以及任务 4 的前端构建部分**在本环境无法执行**——那些任务要 clone vue-vben-admin 5.x 的 monorepo 再 pnpm install/build。**不要尝试安装 Node、不要尝试 clone 仓库。**

你在本轮只做三件能做的事：

1. **任务 1：核对上游实际版本并 pin commit → web/UPSTREAM_PIN.md。**
   用 curl 取 raw.githubusercontent.com 与 api.github.com（WebFetch 对 github.com 常被挡，raw 与 api 可直连）。至少核对：monorepo 根 package.json 的 version 与 packageManager、apps/web-ele/package.json 的 version 与 element-plus/vxe-table/axios 版本、Node 与 pnpm 的版本要求、apps/ 下实际有哪几个应用、\`apps/web-ele/src/api/request.ts\`（或框架内 request 包）里响应拦截器的字段名（codeField/dataField/successCode/responseReturn）、preferences.ts 里 SPEC §12.4 要覆盖的项名是否还叫那个名字。取得当前 main 的 commit sha（\`curl -s https://api.github.com/repos/vbenjs/vue-vben-admin/commits/main\`）写进文档作为 pin 目标。**取不到的项如实标注「未能核实」，不要编。** 同时逐条核对 SPEC §12.2 删除清单里的目录名在当前上游是否还存在——SPEC 是按 2026-09-20 的 main 写的，可能已漂移。
2. **任务 14 的 Go 侧：internal/console/ui/embed.go + embed_test.go + 最小占位 dist。**
   go:embed 内嵌 dist 整棵树，**用能包含下划线开头文件的模式**（Vite 产物里有 _ 前缀文件，默认 embed 模式会漏掉）；提供一个 http.Handler，hash 路由不需要 SPA fallback；设置正确的 Content-Type 与缓存头（index.html 不缓存、带 hash 的资源长缓存）。
   **关键：仓库里必须有一个最小占位 dist（至少 internal/console/ui/dist/index.html，外加一个 _ 前缀文件用来锁住 embed 模式），否则 go:embed 编译不过，M9 的 cmd/lober 就无法装配。** .gitignore 里现有一行 \`internal/console/ui/dist/\` 会让占位文件进不了版本控制——加一条例外（例如 \`!internal/console/ui/dist/index.html\` 与 \`!internal/console/ui/dist/_placeholder*\`），并在占位 index.html 的注释里写明「这是占位文件，真实 dist 由前端构建产生并覆盖」。**提交前用 \`git status\` 确认占位文件确实不再被忽略**（被忽略的话 M9 拿不到它，装配就编译不过）。
   测试要锁住：handler 能返回占位 index.html、Content-Type 正确、下划线前缀文件确实被嵌进来了、未知路径的行为符合 hash 路由假设。
3. **任务 15：web/README.md。** 写清交付方式与气隙验收：构建必须在有外网的机器上做（内网装不上 npm 依赖，这不是能绕的事）、三种做法按推荐顺序（CI 构建只带 dist / pnpm fetch 离线 store / 内网私有 registry）、pnpm-lock.yaml 必须提交、dist 是否入库取决于交付方式、SPEC §12.4 那三处外网依赖必须处理（index.html 里的百度统计脚本、logo.source 与 defaultAvatar 指向 unpkg.com、iconify 在线图标解析）、以及 §12.9 的与上游同步纪律（定制只收敛在四处，packages/** 与 internal/** 保持原样）。
   **在 README 顶部用一节明确写出「本仓库当前状态」**：前端源码树尚未落地，原因是实施环境缺 Node/pnpm；已就位的是 Go 侧 embed 与占位 dist；接手的人要做什么（按 UPSTREAM_PIN.md pin 的 commit clone、按 §12.2 精简、按任务 4 把 vite 的 outDir 指向 internal/console/ui/dist、按 docs/contracts/http-console.md 对接 API）。

unfinished 数组里逐条列出计划里没做的任务号与原因（缺 Node/pnpm）。`,
  },
]

const results = await pipeline(
  MODULES,
  (mod) => {
    const wt = WT + mod.dir
    const pre = PREAMBLE.split('__WT__').join(wt).split('__BRANCH__').join('feat/' + mod.dir)
    return agent(pre +
      '## 你的模块\n\n计划：' + mod.plan + '\n\n你拥有的文件（只有这些）：\n  ' + mod.owns + '\n\n' + mod.brief +
      '\n\n## 验证与提交\n\n验证命令（在 ' + wt + ' 下跑）：\n' +
      '  go vet ' + mod.pkgs + '\n  go test ' + mod.pkgs + ' -v\n  go test ' + mod.pkgs + ' -race\n  gofmt -l <你的目录>\n' +
      '加上你的计划与上面「重点」里提到的各条自查 grep。\n\n' +
      '全绿之后**在你的 worktree 里 commit**：`git add <你的文件>` 然后 `git commit -m "<按你的计划末尾给的 commit 信息，或 feat(<模块>): ...>"`。' +
      '可以分成多个 commit（按计划的任务切分），也可以一个。**只 add 你自己的文件**，提交前用 `git status` 确认没有夹带别人的东西。\n\n' +
      '最后：把你的计划末尾「交付验收」清单逐条核对，结果填进 acceptance 数组（每条给证据，例如命令与输出要点）。清单里有做不到的条目，如实标 ✗ 并在 unfinished 里说明。',
      { label: mod.label, phase: 'Implement', schema: RESULT, effort: 'high' })
  },

  (impl, mod) => {
    if (!impl) return { impl: null, verify: null }
    const wt = WT + mod.dir
    return agent(`你是 Lober 项目 ${mod.key} 模块的独立验证者与修复者。实现者刚写完并提交，你要证伪它，把真问题修掉，然后提交修复。

## 你的工作区

  worktree：${wt}
  分支：    feat/${mod.dir}

**所有操作都在这个 worktree 里**（用绝对路径或先 cd 进去）。绝不碰 E:\\src\\lober 主仓库，也不碰其它 worktree。

先读：
- ${mod.plan} —— 特别是每个任务的「测试要锁住的行为」与末尾的「交付验收」清单
- 实现者实际写的代码（文件清单见下）
- 与本模块相关的契约：internal/core/contract/**、internal/core/model/**、docs/contracts/**（**只读，不许改**）

## 实现者的报告（不要相信这份自评，自己跑一遍）

  文件：${impl.files.join(', ')}
  提交：${(impl.commits || []).join(' | ') || '（未提交）'}
  验证：${impl.verifyOutcome}
  交付验收自评：${(impl.acceptance || []).join(' | ')}
  契约缺口：${(impl.contractGaps || []).join(' | ') || '（无）'}
  未完成：${(impl.unfinished || []).join(' | ') || '（无）'}
  说明：${impl.notes || ''}

## 按这个顺序做

1. **真的跑验证**：\`go vet ${mod.pkgs}\`、\`go test ${mod.pkgs} -v\`、\`go test ${mod.pkgs} -race\`、\`gofmt -l <本模块目录>\`。只跑本模块的包。
2. **对着计划逐任务查漏**：每个任务的「测试要锁住的行为」是否真有对应测试？有没有写了测试但断言得很弱（只断言不报错、没断言值）？有没有任务被整段跳过？有没有「测试通过但测的不是那件事」？
3. **重点证伪安全不变量**（按本模块适用的挑）：禁止重装的三种失败原因真的被拒绝？no_task 响应体真的逐字符相等且含 sanboot？无匹配任务真的零写入？MAC 不匹配真的不签 token？X-Real-IP 真的只在 loopback peer 时被信任（IPv6 回环 ::1 也要）？超时扫描真的永不返回 pending？改运行时字段带 state 真的报错？磁盘策略真的是纯函数？raid1-ssd 盘不够真的报错而不是退化？渲染产出真的不含字面 http://、七类恶意输入真的不泄漏？signal() 真的在失败时也返回成功？
   **写一个能证伪的测试再看它是红还是绿——不要靠读代码下结论。**
4. **修真问题**：你可以修改本模块的文件。**绝不修改契约**（internal/core/**、internal/apierr/**、internal/config/**、internal/archtest/**、docs/contracts/**、Makefile、go.mod/go.sum）；契约确有缺口就记进 contractGaps 让上层决策。也不要碰别的模块的目录。
5. 修完再跑一遍第 1 步确认全绿，然后 **commit 你的修复**（\`git add <你改的文件>\` + \`git commit -m "fix(<模块>): ..."\`；没改任何东西就不提交，commits 填空数组）。

**不要为了让测试变绿而削弱断言——那是最坏的结果。** 宁可留一条 ✗ 并说清楚。

## 铁律（同实现者）

- git 只允许 add / commit / status / diff / log。严禁 checkout / switch / branch / merge / rebase / reset / stash / clean / push / worktree / 任何带 -f 或 --hard 的命令。
- **严禁 rm -rf，严禁删除任何目录。** 临时文件用 t.TempDir()。**绝不创建与真实模块同名的临时目录**——上一轮有 agent 这样做，清理时把真实代码一起删了。
- 禁止改 go.mod / go.sum，禁止 go mod tidy / go get，禁止跑 make check 或全仓 go build ./... 与 go test ./...。
- 禁止 import 另一个实现模块。中文注释与断言信息。

返回值里：files 只列你改动的文件（没改就空数组）；acceptance 是**你验证后的**逐条结论，不是实现者的自评；verifyOutcome 写你亲自跑出来的结果；unfinished 写仍然没达成的条目与原因；notes 写合并与 M9 集成时必须知道的事。`,
      { label: 'verify:' + mod.key, phase: 'Verify', schema: RESULT, effort: 'high' })
      .then(function (v) { return { impl: impl, verify: v } })
  }
)

const out = {}
const allGaps = []
for (let i = 0; i < MODULES.length; i++) {
  const pair = results[i] || { impl: null, verify: null }
  const impl = pair.impl
  const ver = pair.verify
  const gaps = (impl ? impl.contractGaps || [] : []).concat(ver ? ver.contractGaps || [] : [])
  gaps.forEach(function (g) { allGaps.push(MODULES[i].key + ': ' + g) })
  out[MODULES[i].key] = {
    branch: 'feat/' + MODULES[i].dir,
    implFiles: impl ? impl.files.length : 0,
    implCommits: impl ? impl.commits : [],
    verifierFixed: ver ? ver.files : null,
    verifierCommits: ver ? ver.commits : [],
    verifiedOutcome: ver ? ver.verifyOutcome : '验证 agent 未返回',
    acceptance: ver ? ver.acceptance : (impl ? impl.acceptance : null),
    unfinished: (impl ? impl.unfinished || [] : []).concat(ver ? ver.unfinished || [] : []),
    notesForM9: (impl ? impl.notes || '' : '') + ' || ' + (ver ? ver.notes || '' : ''),
  }
}
return { modules: out, contractGaps: allGaps }
