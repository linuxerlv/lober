# Repository Guidelines

## Project Structure & Module Organization

The Go service lives in `cmd/lober` and `internal/` (core, storage, rendering, ISO, console, and architecture checks); tests sit beside packages. `web/` is a pnpm workspace; its Vue app is `web/apps/web-ele` and shared packages are in `web/packages/`. Engineering documentation is kept locally under `docs/`; that directory is git-ignored and never pushed.

## Build, Test, and Development Commands

Run root `make check` for Go formatting, vet, tests, race checks, Linux amd64 build, and Wire drift detection. Use `go test ./internal/core/...` for focused tests. PostgreSQL tests require `LOBER_TEST_PG_DSN` or the untracked `.pgtest.dsn`.

From `web/`, use `pnpm install`, `pnpm dev:ele`, and `pnpm build:ele` to install, run, and build the app. Check types with `pnpm --filter @vben/web-ele typecheck`; run frontend tests with `pnpm exec vitest run <path>`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; name tests `*_test.go`. Follow Vue 3/TypeScript patterns and use `.vue` single-file components in `web/apps/web-ele/src`. Use the workspace ESLint/formatter and consult `web/UPSTREAM_PIN.md` before changing shared upstream code.

## Testing Guidelines

Add Go `Test<Behavior>` cases beside changes, including relevant golden, contract, or architecture coverage. Run `make check` for backend changes; for frontend changes run typecheck, focused Vitest tests, and build as relevant.

### Lober 测试地点与执行规则

- 使用 VS Code 在本地编写代码和测试；单元测试、集成测试和企业测试都在指定的 Linux Lober 测试机器上执行。测试代码和执行入口保存在本仓库，不把测试结果只留在聊天记录里。
- 单元测试跟随代码存放。Go 测试放在对应包内，文件名以 `_test.go` 结尾；前端测试放在相关功能附近。功能变化覆盖正常情况、边界和失败情况，修复缺陷先复现再验证。
- 功能分支先在 Linux 上运行受影响的单元测试和回归测试；后端改动按需运行 `go test ./internal/core/...` 等相关测试，合入前运行仓库根目录的 `make check`。前端改动按需运行类型检查、相关测试和构建，具体命令见上文。
- 面向 `dev` 的功能分支提交或发起合并时，执行代码格式和规范检查、静态安全扫描、构建检查、相关单元测试及关键集成测试。功能代码和对应测试代码一同提交，并随功能分支合入 `dev`。
- 合入 `dev` 后，对合并后的版本执行全量单元测试、全量集成测试和接口自动化测试；通过后由内部环境的部署服务自动部署到开发环境，再执行冒烟测试，检查服务启动和关键功能。GitHub 只存储代码并运行基础检查，不直接部署或连接内部环境。
- `test` 接收已通过上述 `dev` 检查的固定候选版本，在此基础上增加企业测试集。保留候选版本对应的 `dev` 检查记录；候选代码变化后重新执行受影响的检查。
- 集成测试验证合并后的 `dev` 版本，按改动覆盖服务、数据库和前端之间的实际配合；需要 PostgreSQL 时使用测试专用数据库和 `LOBER_TEST_PG_DSN` 或机器上的私有 `.pgtest.dsn`。缺少必要环境时记录未执行，不能算通过。
- 企业测试在云服务器的 `lobervm` Linux 测试实例上运行。测试已固定的 `test` 分支提交，从 GitHub 获取对应代码，不使用本地未提交文件。使用 `tests/lobertest/enterprise/README.md` 中当前测试入口和准备要求；外层旧测试不能代替企业验收。
- 每轮记录测试机器、代码提交、完整命令、结果和跳过原因；需要构建产物时记录其校验和。失败或必测项未执行时不得记为通过。真实设备测试、远程上传、分支晋级及部署仍需事先获得明确授权；上述开发环境自动部署须先获得授权并配置，规则本身不构成执行授权。
- 本节适用于 Lober 的 Go 服务和 Vue 前端。下文若出现 Python／Flutter 桌面程序、Windows 打包或 `master` 工作流的描述，不能当作本项目的测试命令或发布分支规则；Lober 的分支和发布规则以上文 Branch & Release Workflow 为准。

## Commit & Pull Request Guidelines

Use concise conventional commits such as `feat(console): ...`, `fix(iso): ...`, or `docs: ...`. PRs should describe the change, report validation, link related issues, include UI screenshots, and call out schema, config, or generated-code changes.

## Branch & Release Workflow

Develop each feature or module on its own named branch, then merge it into `dev` for integration testing. Merge with a merge commit, never squash or rebase, so a faulty feature or module branch can be reverted as one unit. If integration testing finds a defect, roll back the problematic feature or module branch; unaffected branches may be submitted again. Fix the failing branch and rerun integration testing until the combined changes pass. Then sync the integrated code to `test` and run the enterprise test suite; the `lobervm` instance on the cloud server pulls the latest `test` branch from GitHub and runs tests there, never local uncommitted code. After it passes, sync to `preview` for manual acceptance and internal testing. Promote to `main` only after that review passes. GitHub stores code and runs basic checks only; it never deploys or reaches internal test environments. For a bug found on `main`, create a `fix/<name>` branch from `main` and follow the patch release flow through testing and preview before merging the fix into `main`. Keep branch names to `feat/<name>`, `fix/<name>`, `dev`, `test`, `preview`, and `main` only. Every promotion requires green CI, one approval, and resolved conversations. Tag releases on `main` as `vYYYY.MM.PATCH` and never move or delete a published tag.

## Security & Configuration

Never commit credentials or local DSNs. Keep PostgreSQL test credentials in `LOBER_TEST_PG_DSN` or ignored `.pgtest.dsn`; document new required configuration.

## Agent 规则：说人话

### 核心原则

所有回复都要让没学过专业知识的普通人看懂。

### 语言规则

1. **禁止黑话。** 不用专业术语；必须使用时，紧跟一句大白话解释。不用英文缩写；KPI、ROI、MVP 等词要翻译或换成中文。
2. **禁止废话。** 不说“综上所述”“值得注意的是”“需要指出的是”等填充语，不重复用户已经说过的内容，开头直接给答案。
3. **短句优先。** 一句话只说一件事。一段不超过 3 行。能用 10 个字说清的，不用 30 个字。
4. **结构清晰。** 超过 3 个要点时用列表或编号；有步骤就按顺序编号；对比内容用表格。
5. **先说结论，再说原因。** 第一句给答案、结论或建议，解释放在后面。
6. **具体不空洞。** 给例子、数字和可以直接照做的动作。比如，不只说“注意性能”，要说“图片先压缩再上传，不然加载要 5 秒”。
7. **诚实直说。** 不知道就说“不知道”，不编造；有风险就直接指出；用户方案有问题就明确说明。

### 语气规则

像朋友聊天，不像写公文。可以适度口语化，但别刻意卖萌。少用表情符号，最多在关键处点缀。

### 长度规则

简单问题用几句话回答。复杂问题先用一句话总结，再展开细节。用户说“详细讲讲”时再写长篇。

### 代码相关

先说代码的作用和用法，再贴代码。改动说明用一两句话讲清改了什么、为什么改。代码注释也用中文大白话写。



## Testing Guidelines

测试采用“按改动选择范围、按分支设置门禁、按候选版本保存证据”的规则。以下规定细化后文 Git 分支管理规则中的测试要求；测试通过不自动授权晋级、远程操作、部署或发布。

### 1. 测试范围与基础要求

- Python 使用 pytest，文件命名为 `test_*.py`；Flutter 使用单元／Widget 测试，文件命名为 `*_test.dart`，Windows 工作流放在 `desktop/integration_test/`。
- 功能或行为变化必须补充或更新对应测试，覆盖正常路径、关键边界和失败路径。缺陷修复先确认原问题可以复现，再验证修复有效；测试应验证可观察行为，避免只重复实现逻辑。
- 仅修改文档、注释且不改变行为时，检查命令、路径、链接和格式即可，不强制新增测试或运行应用测试。
- 不设置任意的覆盖率百分比；核心业务必须有明确场景覆盖。相关测试失败时不得合入或晋级；必需验收项未执行不能记为通过。

### 2. 各分支测试职责与通过条件

| 分支 | 必须验证 | 通过条件 |
| --- | --- | --- |
| `feature/*`、`bugfix/*` | 在独立工作树运行相关单元及回归测试；UI 改动执行静态检查和相关 Widget 测试；合并前运行受影响子系统的完整测试，涉及共享核心或协议时验证 Python、Flutter 两端及本机 gRPC | 改动行为、关键边界和失败路径通过，完成独立审查后才可合入目标分支 |
| `dev` | 在主目录对合并后的代码运行 Python、Flutter 和 Windows 本机 gRPC 集成验证，检查 CLI／GUI 业务语义及输出一致性 | 完整集成门禁通过，记录合并后版本证据，可以申请晋级 `test` |
| `test` | 固定候选范围，使用最终 ZIP 解压后的 EXE 和冻结后端执行企业回归，覆盖批量添加、采集／停止、历史、导出、备份恢复、托盘和异常路径 | 必需用例通过，无阻断缺陷，已知限制明确，可以申请晋级 `preview` |
| `preview` | 对同一候选制品执行业务验收和交付验证，包括干净 Windows、标准用户、完整解压运行及代表性业务流程；真实 BMC 测试单独授权 | 业务验收与交付检查通过，已知限制得到确认，可以申请正式发布 |
| `master` | 发布前核对已验收制品、版本、校验和及发布记录；发布后执行启动和关键只读流程冒烟 | 发布内容与验收候选一致，启动及关键流程正常；失败时按回滚规则处理 |

`docs/*`、`refactor/*`、`chore/*` 按实际改动选择验证范围；重构和依赖变化不能按纯文档豁免测试。`release/*`、`support/*` 按目标版本及晋级阶段执行对应门禁。

### 3. 命令与关键场景

在仓库根目录运行 Python 测试和 `dev` 完整集成门禁：

```powershell
.\.venv\Scripts\python.exe -m pytest -q
pwsh -File scripts/verify-desktop.ps1 -WindowsIntegration
```

在 `desktop/` 运行 `flutter analyze`、`flutter test`；只执行相关测试适用于开发迭代，不能代替合并前要求的子系统验证或 `dev` 集成门禁。

重点检查单台失败不影响其他设备、停止后不启动新设备且保留已完成结果、工作区重开及备份恢复、导出完整性，以及缺失值、多系统、大整数容量和资产标识映射。启停、托盘、打包或依赖变化必须验证 Windows 原生流程和最终打包 EXE；源码运行通过不能代替成品验证。

### 4. 候选冻结、失败处理与紧急修复

1. 测试针对明确版本，记录源 SHA、合并后的 SHA 和制品校验和；不能以旧测试结果证明新提交通过。
2. 失败在哪一层，就停在哪一层；修复并验证后才能继续晋级。候选分支修复必须回流 `dev`，冲突解决后重新验证。
3. `test` 开始验证后，不反复合入整个最新 `dev`；新增功能需要重新确定候选范围并重新验证。
4. `test → preview → master` 使用同一候选制品；源码、依赖、构建输入或冲突解决影响制品时，必须重新构建并重跑受影响检查。重新构建不能仅凭源码相同视为已通过。
5. `hotfix/*` 从实际生产 Tag 创建，至少完成回归测试、受影响集成测试、成品验证、独立审查及发布审批，再向受影响的活跃分支回流；紧急通道不免除这些门禁。

### 5. 测试安全与证据

- 自动化默认使用合成数据、脱敏回放和本机回环服务。真实 BMC 测试必须得到明确授权，并限定目标、并发和范围；保持只读，不执行电源控制、配置修改或固件升级。
- 检查日志、报告、截图和发布包，确保不泄露密码、令牌、真实资产标识或基础设施地址；仅将脱敏证据纳入审查记录。
- 每轮记录候选 SHA、测试环境、完整命令、通过／失败／跳过数量、制品校验和及未验证项。跳过项必须说明原因，本机通过、模拟通过和实机通过分别记录。
- 必需项失败或未执行时不得标记整轮通过；证据只证明已验证范围，不代表托管平台已配置门禁或用户已批准发布。

## Commit & Pull Request Guidelines

Follow history: `feat(desktop): ...`, `fix(desktop): ...`; use `docs: ...` for documentation. PRs explain behavior, linked issues where applicable, validation evidence, and screenshots for UI changes; obtain independent review.

Inspect Git status first and preserve unrelated changes. Develop in independent worktrees using `feature/*`, `bugfix/*`, or `docs/*`; integrate verified features into `dev`, then remove merged temporary worktrees. Follow `dev → test → preview → master`; later promotion, remote operations, deployment, and publication require explicit authorization. Never rewrite shared history.

## Security & Configuration

Keep BMC operations read-only. Exclude credentials, real inventories, workspace databases, logs, and reports from commits and packages. Use sanitized examples; preserve authenticated loopback-only gRPC and opt-in DPAPI credential storage.

## Git 分支管理规则

本文件是本仓库人工协作与 Agent 执行 Git 操作的共同规则。执行前先检查当前分支、工作区状态和已有改动；不得覆盖、丢弃或擅自提交其他任务的改动。

### 1. 分支模型

长期分支固定为 `dev`、`test`、`preview`、`master`。预发布分支统一拼写为 `preview`，不使用 `perview`。

正常发布方向：`feature/*（工作树+单元测试） → dev（主目录集成测试） → test（企业测试集） → preview → master`。

| 分支 | 职责 | 创建来源与合并目标 | 生命周期 |
| --- | --- | --- | --- |
| `dev` | 日常开发集成 | 接收功能、缺陷修复和必要的补丁回流 | 长期 |
| `test` | 企业测试与回归验证 | 接收 `dev` 集成测试通过的候选版本 | 长期 |
| `preview` | 预发布、业务验收、部署与迁移验证 | 接收测试通过的候选版本 | 长期 |
| `master` | 正式发布基线 | 接收预发布验收通过的版本或生产紧急修复 | 长期 |
| `feature/*` | 新功能 | 从 `dev` 创建，合回 `dev` | 必备临时类型 |
| `bugfix/*` | 非生产缺陷修复 | 从问题所在分支创建，合回原分支，必要时同步至其他活跃分支 | 必备临时类型 |
| `hotfix/*` | 生产紧急修复 | 从实际运行的生产 Tag 创建，合入 `master`，同步至其他活跃分支 | 必备临时类型 |
| `release/*` | 固定单次发布范围，支持后续版本继续开发 | 从明确的 `dev` 提交创建，经 `test`、`preview` 验证后发布 | 建议采用的临时类型 |
| `support/*` | 维护仍被支持的旧版本 | 从对应版本 Tag 创建，仅接收该版本所需修复 | 按需，维护期结束后关闭 |
| `spike/*` | 技术预研与概念验证 | 通常从 `dev` 创建；产品化成果另走正常开发与审查流程 | 按需临时类型 |

`docs/*`、`refactor/*`、`chore/*` 可用于文档、重构、依赖与工具维护，采用与 `feature/*` 相同的协作流程，不增加长期环境分支。

命名示例：`feature/123-user-login`、`bugfix/456-login-timeout`、`hotfix/789-payment-error`、`release/1.5.0`、`support/1.4`、`spike/redfish-events`。

本仓库任务分支采用上述用途前缀；这些明确的项目规则优先于工具默认的分支前缀。

### 2. 日常开发

1. 对 `feature/*`、`bugfix/*`、`docs/*`、`refactor/*`、`chore/*` 等常规临时分支，从主目录当前基线（通常为最新 `dev`）创建或复制到独立工作树；存在用户未提交改动时，先识别归属，不得擅自 stash、reset、clean 或跨分支搬移。
2. 在独立工作树完成功能或修复，并必须同时补充或更新对应的单元测试；任务分支合入前，相关单元测试必须全部通过。
3. 单元测试通过后，将任务分支合并到主目录的 `dev` 分支，并保留必要的 PR/MR 审查记录。
4. 合并到主目录 `dev` 后，在主目录运行集成测试；集成测试通过后，将同一候选版本推进到 `test` 分支，并执行企业测试集。
5. 四条长期分支不直接开发、推送代码，不强制推送、不删除、不改写历史；通过 PR/MR 与受控流水线更新。`hotfix/*`、`release/*`、`support/*` 等分支继续遵循各自的专项流程。
6. 工作完成后删除已合并的临时分支，通过 Tag 与发布记录保留版本历史。
7. 未获用户授权，不自行创建远程分支、合并 PR、部署或发布。规则文档本身不构成这些操作的授权。

### 3. 发布冻结与晋级

1. 确定发布版本、变更范围、负责人及明确的源提交 SHA。
2. 建议从该提交创建 `release/<version>`，只接收本轮发布所需修复，不再加入新功能；`dev` 可继续下一版本开发。
3. 将 `dev` 集成测试通过的候选版本晋级到 `test`，执行企业测试集与关键回归测试。
4. 测试通过后晋级到 `preview`，完成业务验收、配置、部署、数据库迁移与冒烟验证。
5. 审批通过后晋级到 `master`，受控发布生产并创建正式版本 Tag。
6. 同一时间，每个测试或预发布环境只对应一个明确的候选版本。并行发布必须分配独立环境并记录版本映射，不能把多个候选混入同一轮验证。
7. 测试开始后，不反复将最新的整个 `dev` 合入候选分支；需要新增功能时明确调整范围并重新开始候选验证。
8. `release/*` 上的修复及时同步到 `dev`，并按影响范围重新推进环境验证。发布前确认修复已回流；发布后删除不再需要的发布分支。

### 4. 缺陷修复与回流

| 发现位置 | 修复起点 | 首先合入 | 回流要求 |
| --- | --- | --- | --- |
| `dev` | `dev` | `dev` | 随正常流程晋级 |
| `test` | 对应测试候选 | 对应候选与 `test` | 同步到 `dev` 及相关活跃发布分支 |
| `preview` | 对应预发布候选 | 对应候选与 `preview` | 同步到 `test`、`dev` 及相关活跃发布分支 |
| 生产 | 实际生产 Tag | `master` | 同步到 `preview`、`test`、`dev` 及受影响的发布、维护分支 |

- 只同步所需修复，不借回流夹带未发布功能。
- 优先合并独立修复分支；不适合直接合并时采用 `cherry-pick -x`，关联原提交与 PR，避免重复应用补丁。
- 冲突解决后重新审查、构建并验证受影响内容。
- 修复 PR 记录相关分支的回流结果；全部处理或明确说明不适用后才关闭。

### 5. 生产紧急修复

流程：`生产 Tag → hotfix/* → master → 补丁发布`，随后向所有受影响的活跃分支同步补丁。

- 必须核对实际生产版本，不能从包含未发布功能的分支创建生产修复。
- 紧急通道可以缩短回归范围，但保留代码审查、相关自动化测试、发布审批和生产冒烟验证。
- 测试或预发布候选受到补丁影响时，吸收修复并重新构建与验证。
- 支持旧版本时，在 `support/*` 中维护补丁，并为各受影响版本分别验证和打 Tag。

### 6. 分支保护与合并方式

| 目标分支 | 最低审查要求 | 必需检查 | 建议合并权限 |
| --- | --- | --- | --- |
| `dev` | 至少 1 人审查 | 格式、静态检查、相关单元测试、构建 | 开发维护者 |
| `test` | 开发负责人审查、测试负责人确认范围 | 企业测试集、关键回归 | 集成负责人 |
| `preview` | 测试与发布负责人确认 | 验收、部署冒烟、迁移检查 | 发布负责人 |
| `master` | 发布负责人审批 | 发布检查、候选制品校验 | 发布机器人或少数管理员 |

- 作者不能作为自己的唯一审批人；关键目录配置 `CODEOWNERS`。
- 新提交必须重新运行检查；关键审批按平台能力配置失效与重新确认。
- 功能分支合入 `dev` 推荐 Squash Merge。
- 长期分支之间晋级采用普通 Merge，保留祖先关系与发布轨迹，不使用 Squash Merge。
- 共享分支禁止 Rebase 改写历史；个人功能分支可在合并前整理提交。
- 上述保护需在托管平台实际配置；本文件不代表远端保护已经启用。

### 7. 环境、制品与回滚

- `dev` 对应开发环境，检查通过后可自动部署；`test` 按候选范围部署；`preview` 在测试通过并确认后部署；`master` 经审批部署生产。
- 同一候选制品依次经过测试、预发布和生产，使用镜像 digest 或文件校验和标识，避免各环境独立构建造成差异。
- 合并可能生成不同 SHA，必须验证最终代码树与候选源一致。任何影响制品的源码、依赖、构建输入或冲突解决变化，都应重新构建并重新验证。
- 环境差异通过环境变量、配置平台或密钥管理系统提供，不在各分支维护不同业务代码，不提交凭据。
- 回滚优先部署上一稳定制品，不通过强制重置 `master` 回滚。代码修正使用 Revert PR；数据库变更单独设计兼容与恢复方案。
- 分支记录代码状态，部署记录才是实际运行版本的依据；二者不得混为一谈。

### 8. 版本与发布记录

- 正式版本采用 `v<major>.<minor>.<patch>`，例如 `v1.5.0`；候选版本使用 `v1.5.0-rc.1`；补丁版本如 `v1.5.1`。
- 使用受保护的 Annotated Tag，发布后不移动、不覆盖。
- 每次发布记录版本号、源提交、制品校验和、变更列表、审批记录、目标环境与回滚目标。

### 9. 参考资料

本规则结合以下资料与本项目四条长期分支的要求制定，并非直接套用某一种标准工作流：

- GitHub Flow：https://docs.github.com/en/get-started/using-github/github-flow
- Microsoft Git branching guidance：https://learn.microsoft.com/en-us/azure/devops/repos/git/git-branching-guidance?view=azure-devops
- Atlassian Gitflow workflow：https://www.atlassian.com/git/tutorials/comparing-workflows/gitflow-workflow
