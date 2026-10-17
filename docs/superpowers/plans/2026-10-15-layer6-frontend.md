# 第 6 层：改造现有 web 前端 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:dispatching-parallel-agents for independent file groups; use superpowers:test-driven-development and superpowers:verification-before-completion throughout. Steps use checkbox syntax for tracking.

**Goal:** 在现有 Vue Vben Element Plus 控制台上对接第 5 层真实接口，完成前端验收并合入 dev。

**Architecture:** 保留应用骨架、布局和 web/packages 上游代码。改造现有业务页面、路由、请求与状态；契约从真实控制台 OpenAPI 生成，兼容性与正文解析由服务端决定。各资源按文件划分责任，最终统一审查、验收。

**Tech Stack:** Vue 3、TypeScript、Element Plus、Vben form/VxeTable、Pinia、Vite、vitest、pnpm。

**Spec:** ../specs/2026-09-30-lober-rewrite-design.md §4.6、§5.1–5.3、§6.2、§7.2、§7.4；../specs/2026-10-11-layer5-console-design.md。

## Global Constraints

- 用户于 2026-10-15 明确要求现在执行第 6 阶段、基于现有 web 修改并在验收后合入 dev；本计划落实已确认总设计，继续执行，不再次请求相同授权。
- 工作基线 dev 的 467314f；使用 .tmp/wf/layer6-frontend 本地克隆，分支 codex/layer6-frontend，保留主工作区正在进行的渲染修改。
- 非测试代码不修改 web/packages 上游与现有应用骨架；业务页面沿用 .vue、Page 与 VxeTable 适配。
- 所有接口使用 /console/v1；不调用旧 artifact-sets、disk-dryrun、reports、auth/codes 与 auth/refresh 端点。
- 请求/响应类型从真实 OpenAPI 生成；不可手写第二套领域契约，不做客户端兼容性判断或正文扫描。
- 所有列表支持服务端 cursor/next；错误显示服务端 problems、common、byHost、subject 与 next，不能吞掉逐条原因。
- 内置内容只读且已审批；自写内容审批者不能是作者。客户端依据权限码控制按钮，服务端仍是权限判定者。
- 先记录失败测试，再实现并跑绿；验证输出留在本克隆的 .tmp/layer6-evidence 下。

## Review Focus

- 旧响应解包和权限码端点导致登录看似成功后立即失败：以真实信封、用户权限与强制改密覆盖认证测试。
- 列表超过一页、搜索后残留游标和旧请求晚到：测试游标遍历与上游选择变化时丢弃旧结果。
- CSV、上传与特殊 SN/ID：测试编码路径、确认前不写入、分片原始字节和失败保持可重试状态。
- 模板/任务表单保留旧模型或前端自行兼容性推断：测试服务端插槽与参数字段为唯一数据源、锁定值不被提交覆盖。
- 批量任务部分成功、422 只显示一句错误、SSE 丢事件或卸载后泄漏：测试逐台错误定位、原子结果与重连游标/关闭。

## Shared Interfaces

- web/apps/web-ele/src/api/console/generated.ts 导出 interface Schemas，键名等于 OpenAPI components.schemas，例如 HostResponse、TemplateResponse、WizardParamsResult；导出 Paths 描述实际路由操作。类型只从生成器产出。
- 各域 api/console/<domain>.ts 使用 requestClient，类型通过 Schemas['Name'] 引用；路径段统一 encodeURIComponent。
- api/console/problem.ts 导出 problemsOf(error): Problem[] 与 errorMessage(error): string；不重新生成服务端文案。
- src/components/console/ProblemList.vue 接受 problems，显示 subject、message、next；权限读取 useAccessStore().accessCodes。
- 列表按 next 翻页；选择表单切换上游必须清空下游值与异步结果。

## Task 1：契约、认证、权限与公共基础

**Files:** api/request.ts、api/response-interceptor.ts、api/core/*、api/console/generated.ts、api/console/problem.ts、store/auth.ts、router/routes/modules/*、views/users/*、views/audit/*、views/account/*；web/scripts/generate-console-types.mjs 与真实 OpenAPI 导出入口。

**Interfaces:** 产出 Shared Interfaces 中生成类型、问题展示及认证；权限从 /user/info 的 permissions 取，不请求 /auth/codes。

- [x] 红：真实成功/失败信封、逐台问题、字段定位、无刷新端点、用户角色转换、强制改密和类型生成漂移测试失败。
- [x] 绿：实现确定性 OpenAPI→TS 生成与 check 模式，生成真实契约；修认证和请求，支持改密、用户管理与审计查询/完整导出。
- [x] 验证：pnpm exec vitest run --config apps/web-ele/vitest.config.ts；pnpm --filter @vben/web-ele typecheck。
- [x] commit：feat(web): 真实契约、认证权限与用户审计。

## Task 2：主机与 ISO 页面

**Files:** views/hosts/index.vue、views/artifact-sets/index.vue、api/console/host.ts、api/console/artifact.ts 及对应测试。

**Interfaces:** 消费 Schemas、公共问题；路由 /isos 保持该现有页面文件的复用。

- [x] 红：主机属性字典、可选架构固件、按 SN 更新与 rev、CSV dry-run 再确认、ISO sources 数组、按角色上传/续传、游标翻页的行为测试失败。
- [x] 绿：改造列表和表单；ISO 支持 URL、upload、local，附加来源，状态/进度、废弃、取消、获取重试、重新导入；移除旧制品集模型。
- [x] 验证：该域 vitest 与 vue-tsc 无错误，浏览器走通登记主机、CSV 和新建 ISO。
- [x] commit：feat(web): 主机维护、CSV 与 ISO 来源管理。

## Task 3：hook 与模板编辑审批

**Files:** views/hooks/*、views/templates/*、api/console/hook.ts、api/console/template.ts 与 authoring 测试；可创建专用编辑子组件。

**Interfaces:** 消费 Schemas；模板编辑的插槽来自 /slots；候选 hook、来源差异使用已有服务端端点；预览输出全部文件和示例标识。

- [x] 红：内置只读、按版本保存 rev、派生而不覆盖原本、替换 hook 原位继承同名绑定、审批非作者与驳回理由、诊断定位和预览测试失败。
- [x] 绿：保留现有列表风格，补参数与实现编辑、正文、装配、版本历史、提交/批准/驳回/废弃/派生/预览；不以原始 JSON 文本框代替主要业务流程。
- [x] 验证：域 vitest、vue-tsc；浏览器派生内置模板、替换 hook 并以不同用户审批。
- [x] commit：feat(web): hook 与模板的版本编辑和审批。

## Task 4：建任务向导与任务详情

**Files:** views/tasks/new.vue、index.vue、detail.vue、api/console/task.ts、wizard.ts、任务状态与向导测试。

**Interfaces:** 消费 Schemas；向导只用 /wizard/* 候选与字段；服务端重跑全部准入与参数求值。

- [x] 红：禁选主机/ISO 的原因、换 ISO 清模板、逐台字段/批量填写/锁定值、迟到响应、整批失败、事件游标重连与卸载清理测试失败。
- [x] 绿：主机→ISO→模板→参数→确认与逐台预览→一次提交；任务列表详情、事件、取消重试与服务端限制原因；不冒充第 7 层尚未实现的执行能力。
- [x] 验证：vitest、vue-tsc；浏览器多选主机批量创建，详情显示返回任务及快照。
- [x] commit：feat(web): 新版批量任务向导与任务详情。

## Task 5：集成、审查、验收与合并

**Files:** web/apps/web-ele/vite.config.*、web/package.json、独立 vitest 配置、docs/superpowers/reports/2026-10-15-layer6-acceptance.md；必要的端到端验收工具。

- [x] 工具链：本地 Node 24、pnpm；为前端提供同源 API 代理；实际后端和隔离测试 PostgreSQL，不动现有用户/业务数据。
- [x] 类型生成 check、vue-tsc、ESLint、vitest 和 vite production build 全部通过；后端 make check 验证未回归。
- [x] 浏览器实际走通四条流程并保存截图/请求证据：登记主机含 CSV、新建 ISO、派生内置模板换 hook 再审批、向导批量建任务。
- [x] 独立代码审查，修复影响交付的发现并补回归测试；证据和限制如实写入验收报告。
- [x] 更新本计划复选框、交接第 7 层；提交全部第 6 层文件。
- [x] 在干净本地克隆中以 dev 当前头为基线创建合并提交，确认目标未被其他会话推进后将结果合入主仓库 dev；不擅自推送远端。

## 执行记录

- 2026-10-15：用户明确选择现在执行、验收后合入 dev。基于现有总设计直接落实此计划。
- 预检：主工作区有其他渲染层未提交修改，因此使用被忽略的本地克隆；不切换或清理主工作区。

- 收尾：各域由并行工作者分工实现，主执行者集中提交集成结果，未按每个任务单独提交；独立审查 3 个 P2 已修复并经定点独立复核关闭。前端最终 60 项测试、类型、ESLint、生产构建通过，四条浏览器流程与生产 SSE 实测完成。

- 最终后端门禁：Linux Go 1.27.1 的 make check 全绿，含真实控制台完整竞态测试（595.586 秒）、Linux 构建与 Wire diff。监听退出测试使用独立 Transport 并完整释放客户端连接，超时标准未放宽。

- 已合并：功能提交 fdedea8；合并提交 a4bbb15（父提交 467314f、fdedea8），合并树与已验收分支完全一致。合并后未缓存架构守卫通过（26.680 秒），SPA/API 边界测试通过。主仓库 dev 已通过旧值校验原子更新，原工作区仍处于 codex/layer4-render-integration，未修改其并行渲染工作。未推送远端。第 7 层交接见验收报告。
