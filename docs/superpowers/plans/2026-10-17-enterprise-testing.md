# Lober 企业级测试与系统验收计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本次交付仅为计划和分支调查，不代表已经执行下列任务。

**Goal:** 为当前重写版建立可重复、可定位失败、可证明发布资格的测试集合，覆盖控制台、真实 PostgreSQL、ISO、引导装机、故障恢复与发布运维。

**Architecture:** 产品内测试继续随产品代码进入 dev；黑盒接口、浏览器、装机和环境演练集中在 tests/lobertest，独立记录被测产品 SHA 与测试 SHA。按 L0 基础门禁、L1 HTTP 集成、L2 装机、L3 浏览器组织，并增加安全、恢复、容量和升级套件；第七层未交付的能力明确阻塞对应验收。

**Tech Stack:** 现有 Go testing/race、真实 PostgreSQL、Vue/Vitest/Vite；复用旧 lobertest 的 Python/uv/pytest、HTTP/SSH/QEMU 思路，浏览器使用 Playwright。工具版本依据实施时仓库声明和锁文件固定，不沿用历史机器上的绝对路径。

**Spec:** [重写设计 §7](../specs/2026-09-30-lober-rewrite-design.md)、[ISO 设计](../specs/2026-10-11-layer4-iso-design.md)、[控制台设计](../specs/2026-10-11-layer5-console-design.md)、[RBAC 设计](../specs/2026-10-16-rbac-menu-design.md)。历史验证依据为 dev 基线内的 `docs/superpowers/reports/2026-10-15-layer6-acceptance.md` 与 `docs/superpowers/reports/2026-10-16-rbac-menu.md`；当前主检出较旧，不含这两份报告，本次从 `.worktree/rbac-integration` 读取核对。

日期：2026-10-17，Asia/Shanghai。状态：待执行；本文件不是测试通过报告。

## 1. 全局约束

- 产品基线：本地 dev 的 f68432a；真正执行前重新冻结完整 SHA，若 dev 已变化，以新的候选提交重新盘点，不沿用旧结果。
- 服务部署目标为 Linux；Go 版本下限以 go.mod 为准（本次基线为 1.27.1）；PostgreSQL 测试与部署环境使用同一个大版本，并记录准确版本。
- 数据层只有 PostgreSQL 一份实现；必需数据库不可连接属于失败，不能 skip 后报告通过。
- 产品按单进程部署验收；不新增多活、跨节点调度、机器面 TLS、Secure Boot、IPAM、硬件 inspect 或磁盘 dry-run 的产品要求。预览是当前方案。
- 主机身份以 SN 为准；不宣称测试消除了 SN 错填、碰撞或伪造的残余风险，不擅自添加 MAC 双因子契约。
- C29 快照固化、C30 批次原子性、C31 单主机单活跃任务、C33 状态不回退、C34 rebooting 及之后失败不得自动重装，是发布阻断断言。
- 实测协议取自候选版本的路由、OpenAPI 和代码；历史 test 分支和旧 SPEC 的字段不得覆盖重写规格。
- 缺少第七层、合法介质或机器资源时，相关条目记录 BLOCKED/NOT_RUN；不得制造 ready ISO、直接写 done 或伪造安装成功来替代真实验收。
- 只使用独立测试库/schema、测试账号、专用服务实例和允许的实验网络；目标磁盘使用本次创建的 QEMU overlay。实体机用例必须有明确的目标资产及可清空磁盘清单。
- 凭据、DSN、token、完整应答文件和目标磁盘可能含秘密；只提交脱敏证据，不提交运行配置、私钥、ISO、overlay 或 node_modules。

## 2. 分支与测试资产调查

本次只检查本地引用，没有 fetch、合并、切换分支、清理工作区或创建测试分支。

| 对象 | 事实与用途 |
| --- | --- |
| dev | f68432a；位于 .worktree/rbac-integration，本次检查工作区干净；当前重写产品基线 |
| test | 1c8185f，最后提交时间 2026-09-27；确实有 tests/lobertest，适合保留历史测试资产 |
| 分叉 | git rev-list --left-right --count dev...test 为 149 / 28；不是仅落后几个提交，不能直接快进 |
| test 工作区 | Git 登记 .worktree/test，但目录不存在，worktree list 标记 prunable；当前不可进入运行，不应将其称为可用测试环境 |
| feat/vmlab-real-install | 历史实机相关分支，也登记在失效工作区；作为调查资料，不作为当前验收基线 |
| 当前主工作区 | codex/layer4-render-integration，存在既有暂存、未暂存及未跟踪内容；不得为实施本计划覆盖它们 |

旧 test 的 tests/lobertest 已包含 api/config/deploy/envelope/qemu/ssh 模块、L1 生命周期/准入/RBAC 测试和 L2 装机测试。README 仍声称 L1/L2 未实现，与实际代码不一致；必须以源码和新执行结果判定。

确认的迁移缺口：客户端仍使用 /artifact-sets、旧主机 id/mac/disks 和 accessToken 等约定；L2 主要针对旧 RHEL BIOS；默认 pytest 排除 needs_vm 和 slow，直接运行 pytest 可能只跑框架单测。旧依赖版本、硬编码 D: 路径、服务配置、账号播种和历史 skip 均需复核。

**代码归属决定：**

1. 保留现有 test 引用和历史，不 reset，不把旧产品源码整棵合入 dev。
2. 实施时从冻结的 dev 建 codex/enterprise-tests 工作分支，在其中仅迁移 tests/lobertest 所需文件并按当前契约改写；不整批 cherry-pick 夹带产品改动的历史提交。
3. Go 单测、存储契约、render golden 和前端组件回归随相应产品修复合入 dev，保证产品分支自己的门禁完整。
4. test 继续承担总规格指定的 L1/L2 集成角色；先在工作分支完成迁移，再计划性整合最新 dev 与测试改动，逐项解决旧产品冲突。只有产品部分核对一致、L0/L1 通过后，才称 test 已迁移。该整合是后续实施工作，本次不执行。
5. 测试分支发现产品缺陷时，在基于 dev 的修复分支提交最小回归和修复，再更新测试候选；不让 test 长期承载未回流的产品修复。

## 3. 验收关注点

1. 默认过滤掉 VM/慢测导致假绿：Task 1 的测试汇总检查选中集非空、数量完整、必测无 skip。
2. 程序、前端、配置或测试库不是同一候选版本：Task 1 固定身份并检查部署摘要，Task 4 检查实际嵌入资源。
3. DB 提交与文件发布/删除之间崩溃：Task 5/6 逐阶段杀进程，验证可恢复且不误删原件。
4. UI 切换账号或撤销权限时旧请求回写：Task 3/4 覆盖独立会话、迟到响应与真实 API 拒绝。
5. 控制台报成功但目标机未完成，或失败后再次写盘：Task 7/8 同时检查数据库终态、目标机结果和磁盘哨兵。

## 4. 套件与执行阶段

| ID | 套件 | 主要断言 | 任务 / 入口条件 |
| --- | --- | --- | --- |
| Q01 | 基础与架构 | 格式、vet、race、Linux 构建、依赖守卫、生成漂移 | Task 2；现在可运行 |
| Q02 | 数据一致性与并发 | 约束、CAS、审计事务、单主机唯一任务、批次全成全败 | Task 2/3；真实 PostgreSQL |
| Q03 | API 与业务集成 | 当前契约、错误码、分页、CSV、审批、预览、批量建任务、事件 | Task 3；现在可迁移执行 |
| Q04 | 权限与安全 | 五角色矩阵、会话撤销、越权、注入、文件/来源边界、资源限制 | Task 3/5；机器凭据测试待第七层 |
| Q05 | ISO 与制品资格 | 来源生命周期、完整性、真实介质、必需角色与版本架构 | Task 5；介质可分批提供 |
| Q06 | 渲染及目标脚本 | 七格式、固件、转义、目标解释器、hook 和 cleanup | Task 2/5/8；目标执行依赖第七层 |
| Q07 | PXE 与外部服务 | DHCP、TFTP、iPXE、Nginx、SMB、Range/大文件 | Task 7；第七层部署契约就绪 |
| Q08 | 装机安全与状态机 | 身份/兼容性、token 隔离、信号、超时、不自动重装 | Task 7/8；第七层实现并合入 |
| Q09 | 真实装机 | 七格式成功、批量成功、必需 hook 失败、结果核实 | Task 8；合法介质及目标资源 |
| Q10 | 故障恢复 | 进程终止、DB/网络/磁盘故障，恢复后状态与字节一致 | Task 6；执行中故障待第七层 |
| Q11 | 性能与稳定性 | 容量、尾延迟、下载吞吐、资源回收、持续运行 | Task 9；控制面先测，装机负载后补 |
| Q12 | 发布升级与灾备 | 空库部署、允许的旧版本升级、制品兼容、离线运行、备份恢复 | Task 10；指定升级起点及完整备份 |

阶段 A：Task 1–6 可先实施，不把建出 pending 任务称为装机成功。阶段 B：Task 7–8 随第七层交付。阶段 C：Task 9–10 完成发布资格，控制面容量与恢复可提前开展。

## 5. 文件与接口约定（以下均为实施目标）

| 路径 | 职责 |
| --- | --- |
| tests/lobertest/README.md、pyproject.toml、uv.lock | 安装、依赖锁定、各套件显式命令；替换过期说明 |
| tests/lobertest/src/lobertest/{config,api,envelope,deploy,ssh,qemu}.py | 迁移已有基础设施；按当前契约对接，删除历史路径与默认秘密 |
| tests/lobertest/src/lobertest/{evidence,gate}.py | 结果、身份、日志脱敏；必测数量和阻塞状态汇总 |
| tests/lobertest/profiles/{control,release}.json | 套件必测列表、运行规模、预算、介质与固件矩阵；不含凭据 |
| tests/lobertest/tests/l1_api/ | API、权限、并发、审计、ISO 生命周期黑盒测试 |
| tests/lobertest/tests/l2_install/ | 第七层网关、目标启动、装机、安全及失败场景 |
| tests/lobertest/tests/l3_ui/ | 真实构建前端的浏览器流程，独立账号上下文 |
| tests/lobertest/tests/{security,recovery,performance,upgrade}/ | 对应专项；每个目录独立收集和执行 |
| tests/lobertest/tests/unit/ | 测试框架自身的客户端、配置、汇总、脱敏与部署判断测试 |
| docs/superpowers/reports/enterprise/ | 脱敏汇总、能力矩阵、缺陷和发布结论；原始大日志外置 |

框架共用协议：

- Settings 从显式环境/本地秘密文件加载 base URL、SSH、测试根目录、schema、允许目标网段；缺少所选套件必需配置时 preflight 失败。
- 每轮有 run_id；创建的资源使用该标识，清理只处理已登记属于该轮的对象。日志默认脱敏，失败保留证据及目标 overlay 的受限路径。
- run manifest 保存 product_sha、test_sha、binary_sha256、web_asset_hash、脱敏配置摘要、数据库/系统/工具版本和介质 SHA256。配置摘要排除秘密，私密配置不发布。
- 每条结果包含 case_id、suite、requirement、status、duration、evidence、reason；status 为 PASS/FAIL/BLOCKED/NOT_RUN/NOT_APPLICABLE。pytest 的 skipped 必须映射原因，不能直接算 PASS。
- 汇总入口为 `python -m lobertest.gate --profile <文件> --results <目录>`；返回码 0 仅表示所选配置的全部必测项通过且身份一致，其他返回码表示失败或资格不完整。
- 人面 HTTP envelope 和机器面纯文本/文件分别处理；不得为适配旧客户端同时接受多套不相容字段而隐藏协议错误。

## 6. 实施任务

### Task 1：迁移测试框架并建立防假绿门禁

**Files:** 上述框架模块、profiles、tests/unit/test_gate.py、test_evidence.py、test_config.py 和 README。

**Consumes / Produces:** 冻结的 dev SHA 与历史 tests/lobertest；产出可离线测试的配置/结果协议及 gate 入口，供所有后续任务复用。

- [ ] 核对分支与未提交内容，在隔离工作区创建 codex/enterprise-tests；记录 test 原 SHA，不修剪或覆盖原分支及其他工作区。
- [ ] 仅提取必要 Python、配置模板和框架测试；排除旧产品源代码、__pycache__、二进制与真实秘密；将过期测试移入迁移清单，避免被误收集。
- [ ] 先写 `test_required_suite_with_zero_cases_fails`、`test_required_skip_blocks_release`、`test_product_hash_mismatch_fails`、`test_unavailable_test_database_is_error` 和 `test_secrets_are_redacted`，观察失败，再实现对应门禁。
- [ ] 取消旧配置对 VM/slow 的隐式默认排除，使用显式目录和 profile 选择；单测不依赖 SSH/数据库/部署，测试收集也不得停服或起目标机。
- [ ] 部署只作用专用实例；部署复用必须同时核对二进制、前端、配置和健康状态，禁止默认操作旧 /opt/lober 或覆盖现有 DHCP/Nginx 配置。
- [ ] 用空结果、缺必测、含 skip、摘要不匹配各执行一次 gate，均须非零；完整合成结果须为 0。先通过框架单测，再锁定依赖、记录提交。

**Verification（新框架实现后）：** 在 tests/lobertest 执行 `uv sync --locked` 和 `uv run pytest tests/unit -q`；退出 0。仅这一结果不能称 L1/L2/L3 已通过。

### Task 2：冻结并复验产品内门禁

**Files:** 复用 Makefile、internal/archtest、internal/store/pgstore/*_test.go、internal/render/*_test.go、internal/builtin/*_test.go、web/apps/web-ele 现有测试；仅在发现缺口时新增最低层回归。记录新增 reports/enterprise/<run_id>/baseline.md。

**Consumes / Produces:** Task 1 候选身份；产出 Q01/Q02/Q06 的基线记录，不重复编写已有用例。

- [ ] 盘点 C1–C35 与现有用例，列出正例、反例及证据位置；特别核对 C29–C34、审计回滚、批准版本不可变和引用删除保护。
- [ ] 在真实 PostgreSQL 上跑 `make check`，记录 schema 与数据库版本；检查数据库缺失不会被跳过。使用未缓存全量普通测试补充候选结果。
- [ ] 在锁文件安装依赖、完成现有 workspace stub 后运行 `make webcheck`，保留类型、lint、Vitest、构建结果。不要仅运行 Go check 就宣称前后端全绿。
- [ ] 前端构建与架构夹具检查顺序执行；核对受控 embed 占位文件和真实发布制品，避免并行清理导致假失败或占位页被发布。
- [ ] 发现缺陷先增加能复现的最低层回归，再由产品修复分支解决；基线失败时停止后续依赖验收并记录，不能修改断言接受错误结果。

**Verification（现有入口，Linux 验证）：** `make check`、`go test -count=1 ./...`、`make webcheck` 均退出 0；独立保存每个命令退出码，日志脱敏。

### Task 3：按当前 API 重建 L1 业务与权限矩阵

**Files:** api.py、envelope.py、tests/unit/test_api.py，tests/l1_api/test_{lifecycle,admission,rbac,concurrency,audit,events}.py。

**Consumes / Produces:** Task 1 部署和证据；读取候选 OpenAPI 与 internal/console 的实际 DTO/路由；产出不用目标机即可运行的黑盒 L1。

- [ ] 先以当前登录响应和 SN 主机路由编写客户端测试，证明旧 /artifact-sets、旧 id 主机模型与 accessToken 假设不能通过，再改为候选实际契约。
- [ ] 五角色加匿名逐项验证 API 操作权限；覆盖本人/他人、改密、禁用、角色变更、登出、强制改密、最后管理员保护。空菜单只限制导航，不额外授予或撤销 API 操作权限。
- [ ] `test_batch_failure_creates_no_tasks`：一台合法、一台非法，同批失败后两台均无新任务；`test_concurrent_create_keeps_one_active_task`：同步竞争同 SN，仅一个成功，其余有明确冲突。
- [ ] `test_snapshot_survives_source_changes`：建任务后修改允许修改的源对象、发布新版本，任务快照与预览不变；引用保护及批准版本禁止原地编辑。
- [ ] 验证主机分页/筛选/规范化、CSV 预演零写入及错误批次原子性；ISO 失败原因、来源进度、重试；作者提交与不同账号审批；任务和审计分页、CSV 导出。
- [ ] SSE 断线后从已确认事件位置补读，验证无丢失且按事件 ID 去重；当前阶段不伪造机器安装信号来证明执行链已实现。
- [ ] 每次业务变更检查审计操作者、目标和结果；用独立数据库连接核对竞争后的持久化不变量，非必要不直写业务表。

**Verification（迁移后）：** `uv run pytest tests/l1_api -q --junitxml=<run_dir>/l1.xml`；非零失败必须保留，随后用 control profile 汇总。当前取消/重试未实现时验证真实诊断，执行成功资格留待第七层。

### Task 4：真实前端浏览器回归

**Files:** tests/l3_ui/conftest.py、test_{business_flows,access_refresh,events}.py；必要的前端组件回归仍放原 web/apps/web-ele 对应目录。

**Consumes / Produces:** Task 3 API 正常、生产前端嵌入的候选二进制；产出 Q03/Q04 浏览器证据。

- [ ] 用真实登录建立独立浏览器上下文，不注入 token/localStorage，不用 mock 响应代替验收后端；记录实际静态资源摘要。
- [ ] 完整执行四条既定流程：主机及 CSV；ISO 来源登记与进度；派生模板、更换 hook、非作者审批；向导逐台预览并批量建任务。
- [ ] admin/viewer 与其他角色入口矩阵；另一会话撤销角色/菜单后，当前页面能退出失权页面；空菜单恢复能回首页，直接请求 API 仍按操作权限拒绝。
- [ ] 切换账号、关闭详情和切换资源时制造延迟响应，确认旧请求不覆盖当前界面；重载后状态来自服务器。
- [ ] 验证深链接刷新、生产同源资源、SSE 断线恢复、错误提示和重试恢复；失败保留脱敏截图及 trace。

**Verification（新增入口）：** `uv sync --locked --extra ui` 后安装锁定版本浏览器；`uv run pytest tests/l3_ui -q --junitxml=<run_dir>/ui.xml`。须实际提交表单并读取结果；工具不能执行的步骤记未验证。

### Task 5：真实介质、渲染与输入安全

**Files:** tests/l1_api/test_iso_sources.py、tests/security/test_{source_boundaries,input_limits}.py、profiles 的 media 矩阵；复用 internal/iso、iso9660、render、builtin 现有测试和 fuzz 入口。

**Consumes / Produces:** Task 3 API 客户端、合法介质清单；产出 Q04/Q05/Q06 的内容资格，不代表实际装机资格。

- [ ] 清点介质实际路径、SHA256、版本、series、arch、firmware、所需附加来源及工具 ABI；总规格中的历史“本机有”必须重新查证，不复用旧 D: 假设。
- [ ] 首先资格验证 OpenCloudOS 9.6 和 Debian 13.6 netinst 加同版本官方 netboot；不足则 BLOCKED。其余每个对外支持的 series/version/arch 逐行登记，不能以同格式另一发行版代替。
- [ ] 覆盖 URL、本地、浏览器上传；重复/乱序/重叠/截断分片、内容改变、错误 Range、SHA 不符、取消及重试；核对字节计数、状态、错误原因和审计。
- [ ] 覆盖 SSRF 的重定向、地址重解析及混合地址，按项目已允许内网镜像的契约断言；覆盖路径穿越、符号链接逃逸、损坏 ISO/WIM、超大请求和资源耗尽边界。
- [ ] 为 CSV/HTML/脚本参数设置引号、换行、控制字符、非 ASCII 等输入，逐层验证转义与拒绝；禁止通过审计/日志暴露凭据；授权 hook 作者可写脚本的设计与非授权输入注入分开判断。
- [ ] 七格式 golden 与目标解释器检查继续运行；逐必需角色缺失时拒绝且无半成品输出。Windows 工具资格、WinPE/SMB 与首次开机只在真实目标验证后标 PASS。
- [ ] 对现有解析器 fuzz 分别分配 10 分钟夜间预算、1 小时发布预算，保留崩溃语料；先从源码确认实际 Fuzz 名称，再登记完整执行命令，不把普通测试当 fuzz。

**Verification：** `go test -count=1 ./internal/iso/... ./internal/render ./internal/builtin`；真实介质开关按 isotest 的实际定义设置，逐项核对日志无意外 skip；黑盒运行 ISO 与 security 目录并汇总对应 case_id。

### Task 6：进程级故障与恢复

**Files:** tests/recovery/test_{download_restart,publication_restart,deletion_restart,dependency_failure}.py；必要产品接缝测试随修复提交。

**Consumes / Produces:** 专用实例进程控制、Task 5 介质与证据；产出 Q10 恢复记录。

- [ ] 下载完成前强制终止服务，重启后按实际契约续传并验证最终 SHA；另做 DB 进度超过落盘字节的恢复场景，以及浏览器上传刷新补传。
- [ ] 在导入发布文件完成、数据库确认前后分别终止进程；重启确认既不误删 ready 内容，也不把缺文件发布成 ready；结果不确定时保留并核实。
- [ ] 删除数据库记录前后、清理失败及重启时，确认持久化清理日志继续工作，重复恢复幂等，本地登记原件不删除。
- [ ] 通过专用进程、网络代理或小型独立挂载文件系统制造 DB 断连、网络超时、磁盘满和只读目录；不得填满宿主盘或中断其他服务。
- [ ] 服务恢复后重复读取和业务操作，核对约束、状态、摘要、审计及临时文件归属；记录恢复时间和人工步骤。无可控断点的场景记未执行，不将单元 mock 冒充进程级故障。

**Verification（新增入口）：** `uv run pytest tests/recovery -q --junitxml=<run_dir>/recovery.xml`；每个用例必须包含“故障已发生”和“恢复后不变量”两组证据。

### Task 7：第七层网关、引导和状态机资格

**Files:** tests/l2_install/test_{boot_chain,machine_auth,state_safety}.py、provision/ 适配部署文件；新增产品测试随实际 gateway/状态机模块提交，不预设尚不存在的文件名或路由。

**Consumes / Produces:** 第七层实现、接口及部署契约合入 dev 后重新冻结候选；产出 Q07/Q08。当前为 BLOCKED，不在 control profile 中假装通过。

- [ ] 根据实际交付接口补齐路由、token 与阶段预算，不照搬旧网关端点；先跑其产品单测和真实 HTTP 负例，再启目标机。
- [ ] 无任务、未登记 SN、架构/固件不匹配、重复引导和同 SN 竞争均验证结果；不得下发与任务不符的安装配置或凭据。
- [ ] 不同任务 token、过期/失效 token、finalize 独立凭据和脚本摘要白名单；记录拒绝结果及状态不变，不只检查字符串。
- [ ] 重复、乱序、迟到信号以及超时扫描竞争；确认状态不回退、事件去重、终态不被旧请求覆盖。确定性时间测试放产品层，至少一条真实时钟流程放系统层。
- [ ] 启动 DHCP→TFTP/iPXE→网关→安装源全链路，核对 BIOS iPXE 构建架构与目标架构映射、实际 UEFI 上报、ESXi UEFI 限制。
- [ ] 测试 Nginx 大文件/Range/并发传输不误套引导限流，Windows SMB 可用性，systemd 停止/重启和依赖故障诊断。

**Verification：** 新机器面用例及部署配置语法检查全部通过，再放行 Task 8；没有第七层不得通过直接改 DB 状态绕过。

### Task 8：七格式真实装机与防重装

**Files:** tests/l2_install/test_{install,batch_install,hook_failure,no_reinstall}.py；profiles/release.json 支持矩阵。

**Consumes / Produces:** Task 5 真实介质资格、Task 7 引导与安全通过；产出 Q06/Q08/Q09 的系统资格。

- [ ] 七格式至少各一条成功线：kickstart/OpenCloudOS 9.6、preseed/Debian 13.6、autoinstall/Ubuntu 24.04 server、AutoYaST/Leap 15.6、Agama/Leap 16.0、unattend/Windows Server 2025、ESXi ks/选定的合法版本；版本均以实际介质复核。
- [ ] ESXi 当前版本识别限制未解决前保持 BLOCKED；不得人工猜版本置 ready。安装完成检查使用其实际可用管理/SSH 能力，不机械套用 Windows/Linux 的 OpenSSH 注入断言。
- [ ] 每条同时验收：服务端 done、真实阶段事件、目标系统可登录、版本/架构/固件、分区/引导、主机名/网络、必需 hook 结果及目标凭据清理。收集失败证据不能掩盖原始失败。
- [ ] 为支持矩阵补齐 BIOS/UEFI 与架构组合；不支持的组合验证明确拒绝。至少一组生产拟用实体机/网卡/存储组合补充资格，不能用 QEMU 覆盖所有硬件声明。
- [ ] 一批两台以上全部完成；低资源环境允许顺序上电，真正并发装机能力由 Task 9 单独验证。
- [ ] 必需 hook 返回非零时失败且含 hook、退出码和日志尾部；rebooting/initializing/完成后重新引导、重复回调和失败重试时不得自动再次写盘。先在可清空目标磁盘写入哨兵，再核对其保留及状态。
- [ ] 首先运行故意失败但证据采集完整的短用例，再跑成功安装；目标机失败保留 overlay、串口与服务日志，材料需受限存储。

**Verification：** `uv run pytest tests/l2_install -q --junitxml=<run_dir>/l2.xml` 加 release gate；默认单条装机墙钟预算 90 分钟，依据介质实测在 profile 中固定，不能在失败后临时放宽换取通过。资源准备和下载时间另记。

### Task 9：容量基线与持续运行

**Files:** tests/performance/test_{control_load,artifact_load,soak}.py、profiles 下容量参数及 reports/enterprise/<run_id>/capacity.md。

**Consumes / Produces:** Task 3 控制面、Task 7/8 装机链分别就绪后分阶段执行；产出有适用环境的 Q11 容量结论。

- [ ] 建立初始实验档：10,000 主机、100,000 历史任务、20 并发控制台会话、50 SSE 连接、4 路镜像传输；服务资源和链路带宽必须记录。该档是本计划初始验证目标，不是已承诺的产品容量。
- [ ] 正常负载持续 30 分钟，随后 2 倍突发 5 分钟；测列表/详情 p50/p95/p99、错误率、连接数、CPU/RSS/文件句柄、DB 锁与磁盘 IO。大文件和长导入单独测吞吐，不混入普通 API 延迟。
- [ ] 初始通过预算：普通读 API p95 ≤1 秒、非预期服务端错误率 <0.1%，无数据不变量破坏/OOM/崩溃；突发后 10 分钟内队列排空且连接/句柄接近空闲基线。资源不够该档时报告容量未验证，不事后降低规模仍称原档通过。
- [ ] 单路与四路安装源传输对比，报告实际带宽及争用；第七层就绪后至少两台真正并发装机并与串行耗时对照，不让模拟 API 请求代替装机并发。
- [ ] 24 小时持续混合负载，周期创建/读取、上传/导入和事件重连，包含空闲观察；采样间隔 60 秒。RSS 首尾 30 分钟中位数增长 ≤20%，连接和句柄不持续累积；超标保留趋势并调查，不用重启掩盖。
- [ ] 正常、突发、持续运行分别生成结果；固定种子与数据生成规则，测试结束校验任务唯一性、审计与文件归属。

**Verification：** 显式运行 performance 目录，所有预算按预先冻结 profile 评估；不能以 24 小时进程未退出作为唯一通过依据。

### Task 10：部署、升级、备份恢复及发布汇总

**Files:** tests/upgrade/test_{fresh_install,supported_upgrade,restore,airgap,release_package}.py；reports/enterprise/<run_id>/{summary,support-matrix,defects}.md。

**Consumes / Produces:** Task 1 身份/汇总、允许升级的源版本和备份集；产出 Q12 与最终发布结论。

- [ ] 空库启动、显式 seedAdmin、首次改密、重启后不覆盖已有用户；发布包包含实际前端而不是占位页，运行用户权限及必要目录可用。
- [ ] 选定真实允许升级的源 SHA；准备用户/权限菜单、主机、ISO 来源、版本引用、任务快照与审计，升级后逐项核对。旧 main 产品不能默认原地升级到重写版；没有兼容设计时报告不支持并采用导出/重建方案。
- [ ] 制定并演练一致的备份窗口：停止写入及后台工作，备份 PostgreSQL、制品/SMB/来源及删除恢复日志、配置和发布包；记录恢复前后清单摘要。仅数据库备份不算完整系统备份。
- [ ] 在新环境恢复，核对摘要、权限、引用、旧快照渲染、未完成来源恢复和清理；实测 RPO/RTO。初始目标为成功备份点数据零丢失、实验数据集 RTO ≤60 分钟；额外生产 SLA 须另行确定。
- [ ] 升级失败时演练应用与数据库/制品成套恢复；没有向后兼容证据时禁止只替换旧二进制后称已回滚。
- [ ] 断开外网后执行安装包部署、控制台、已备齐来源的 ISO 导入和安装流程；必要离线包、前端资源、Windows 辅助工具均来自发布清单，不临时联网下载。
- [ ] 在可联网的构建环境固定 Go/前端依赖及漏洞库时间，执行 govulncheck 和锁文件依赖审查，记录可达性、修复或接受理由；漏洞库不可达不能写成零漏洞。
- [ ] 汇总各 Q01–Q12 必测、失败、阻塞与不适用项；交付支持矩阵、运行命令、证据链接、缺陷处置和发布范围，并记录提交。

**Verification：** 显式运行 upgrade 目录，在恢复环境跑 L1 和代表性装机冒烟；release gate 必测全过且无 BLOCKED/NOT_RUN 才返回 0。

## 7. 执行命令与结果保存

本节包含“现有命令”和“待实现入口”，不能把命令列出视为已执行。

现有产品门禁在 Linux 中运行，预先配置测试 PostgreSQL 和目标 shell 工具；所有秘密通过受限文件/环境注入，不打印 DSN：

```bash
make check
go test -count=1 ./...
make webcheck
```

迁移后，在 tests/lobertest 按目录显式选择；下面 `<run_dir>` 是本轮受限证据目录，先替换成真实路径；每个 pytest 的退出码须单独保存，不能被后续成功命令覆盖：

```bash
uv sync --locked
uv run pytest tests/unit -q
uv run pytest tests/l1_api -q --junitxml=<run_dir>/l1.xml
uv run pytest tests/security tests/recovery -q --junitxml=<run_dir>/special.xml
uv run python -m lobertest.gate --profile profiles/control.json --results <run_dir>
```

UI、L2、性能和升级按各任务单独运行，profile 验证应有的所有结果及适用矩阵。依赖阶段失败则后续结果为 BLOCKED，整体 gate 非零；不把下游 skip 数量隐藏。框架需要将外部 make/前端命令结果纳入同一结果协议，保留原始退出码与日志路径。

每次保存：manifest、命令与退出码、JUnit/结构化用例结果、介质矩阵、故障注入时间线、性能原始采样、失败串口/服务日志和脱敏摘要。大文件存实验环境证据目录；仓库报告记录可定位路径及摘要，不提交秘密和目标磁盘。

## 8. 完成标准与触发频率

| 场景 | 必须执行 | 可给出的结论 |
| --- | --- | --- |
| 日常产品合并 | Q01、相关 Q02–Q06 的产品回归；API/权限变更加 L1，UI 变更加对应 L3 | 合并门禁通过，不等于装机资格 |
| 周期测试 | 完整 L1/L3、安全、恢复、fuzz；代表性 L2 和容量短测随能力交付 | 候选稳定性记录；本计划不自动创建定时任务 |
| 发布候选 | release profile 所有适用 Q01–Q12、七格式/对外支持组合资格、恢复及容量记录 | 仅在所声明范围内可发布 |

发布阻断：误发安装配置或越权写入；违反 C29–C34；数据/审计不一致；假 ready/done；秘密泄漏；必需安装流程失败；必测结果缺失、skip 或候选身份不一致。其他缺陷记录影响、回避方式和接受人；覆盖率百分比不能替代关键断言。

七格式是总规格当前的完整系统目标；ESXi 或其他格式仍被阻塞时，整体发布资格不通过。若后续明确缩小发布支持范围，须先修改并确认规格和矩阵，再据新范围评价，不能直接删除失败行。

文档完成自查：Q01–Q12 都有任务归属；五项验收关注点均有明确用例；已有证据与未来执行分开；旧 test 迁移风险、失效工作区与第七层依赖均已标注。后续执行记录逐任务勾选并关联实际命令和证据。
