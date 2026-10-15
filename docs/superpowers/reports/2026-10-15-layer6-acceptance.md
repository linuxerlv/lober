# 第 6 层：现有前端改造验收

日期：2026-10-15。基线：`dev` 的 `467314f`。按用户确认，复用现有 `web/apps/web-ele`、Vben 布局、Element Plus 与 VxeTable，修改业务页面和接口；`web/packages/` 无修改。

## 交付内容

- 从真实 OpenAPI 生成 TypeScript DTO 与路由契约，提供漂移检查；认证、角色权限、强制改密、用户与审计对接真实控制台。
- 主机按 SN 维护、属性字典和 CSV 预览确认；ISO 多种来源、上传续传、获取和导入状态、失败与重试操作。
- hook 和模板的版本编辑、派生、装配、来源差异、非作者审批；候选与插槽由服务端提供。预览字段复用服务端参数求值，支持列表、枚举、预填、锁定与示例系选择。
- 多主机向导、逐台参数和完整文件预览、原子创建批次；任务快照、事件去重、SSE 和补读、取消重试限制。
- 同源开发代理、Go 内嵌资源与 SPA 回退。发布步骤见 [web/README.lober.md](../../../web/README.lober.md)。只提交占位页，交付二进制前先构建前端，再编译 Go。

## 浏览器验收

使用实际后端、真实 PostgreSQL store 和隔离 schema；所有账号、主机与介质均为本次验收夹具，不修改现有业务数据。通过普通登录操作切换账号，不读取浏览器令牌。

| 流程 | 实际结果 | 证据 |
| --- | --- | --- |
| 主机与 CSV | 单台 `QA-SINGLE` 支持架构/固件留空；CSV 预览并确认后登记 `QA-BATCH-1`、`QA-BATCH-2`，属性 `rack=QA-B` | [主机列表](assets/layer6/hosts-csv.jpg) |
| ISO 登记 | local 来源登记成功；真实获取达到 100%，73 字节无效介质返回 `iso_media_unrecognized`，详情显示服务端错误 | [ISO 状态](assets/layer6/iso-created.jpg) |
| 模板装配审批 | 从内置 `lober:kickstart v2` 派生 `qa-kickstart v1`，将 packages 插槽替换为 `qa-packages v1`，保留绑定 `curl`；作者 `qaadmin` 提交，另一账号 `qaapprover` 批准 | [审批结果](assets/layer6/template-approved.jpg) |
| 批量建任务 | 选择两台 CSV 主机、Ready Rocky 9 夹具与刚批准的模板；按服务端字段填写，逐台预览 `menu.ipxe`、`ks.cfg`，核实不同 hostname，一次创建 2 个任务 | [批次结果](assets/layer6/batch-created.jpg) |
| 生产资源与事件 | 使用含真实 Vite 资源的 Go 二进制登录、查看任务和固化快照；深链接刷新可用，SSE 显示“实时连接已建立”，收到 seq 1 的 `qa.acceptance` 夹具事件 | [任务详情](assets/layer6/task-detail.jpg) |

任务 ID：`evowpej3p67cntuzdhrzgihwoa`、`muap3vji7xt6ywejptcdrdrcpa`。ISO 登记 ID：`76awaicq7qqchegr34tlepsyvm`。批次用的 Ready ISO 是明确标注“不用于真装”的合成夹具，补齐 stage2 制品角色后由实际渲染器生成文件。本次没有实际导入完整安装介质或启动目标机装机。

## 审查与回归

[独立代码审查](2026-10-15-layer6-code-review.md) 的 3 个 P2 发现均已修复：

1. 参数校验期间禁用字段和整列填写，防止迟到响应覆盖新编辑；真实挂载组件的延迟响应测试先失败后通过。
2. hook/模板保存、派生和审批等 mutation 捕获 editor token，关闭或更换资源后不回写旧结果；两种编辑器共 6 个延迟请求回归先失败后通过。
3. 新增服务端 preview-params 端点，与渲染共享事实解析及参数求值；前端不再自行推导字段。服务端与挂载界面测试覆盖列表、options、锁定、prefill、系选择及迟到响应。

收尾干运行确认 Turbo 会对输出到仓库外层 embed 目录的应用构建命中缓存，却只记录应用内 dist；已关闭该应用构建任务的结果缓存，实际构建验证为 cache bypass，防止资源目录被还原为占位页后错误跳过生成。`web/packages/` 保持不变。浏览器额外发现的预览重试后旧错误残留已修复，新增真实挂载回归复现并验证恢复。独立审查者随后定点复核了这 3 处修复，并独立重跑 11 项挂载回归，确认原发现全部关闭。原始发现保留作为历史证据。

## 自动检查

| 检查 | 结果 |
| --- | --- |
| `pnpm install --frozen-lockfile --ignore-scripts` | 通过；此前已执行工作区 stub，锁文件无需重新解析 |
| OpenAPI 生成漂移、Vue 类型、应用 ESLint | 最后一次源码改动后全部通过；`frontend-postqa.log` |
| 前端单测 | 11 个文件、60 项通过；含浏览器发现的预览恢复回归 |
| Vite 生产构建 | 最后一次源码改动后通过；`frontend-postqa-build.log`，11/11 build tasks；修正外部资源缓存后 `frontend-embed-build.log` 再次通过，应用明确 cache bypass、依赖包 10 项复用缓存 |
| 含真实资源的 Go 二进制 | 构建通过；浏览器生产同源登录、任务、刷新、SSE 验证通过 |
| 监听退出竞态回归 | 修正测试客户端响应体和空闲连接清理后，`-race -count=3` 通过；未放宽超时或改服务端退出逻辑 |
| 后端 `make check` | 通过，退出码 0；格式、vet、未缓存全模块架构守卫、全量测试、全量 race、Linux 构建及 Wire 生成漂移均通过；`make-check-linux-green.log`。控制台 race 实跑 595.586 秒 |

后端使用 Go 1.27.1 在 Linux/WSL 执行完整门禁，前端在 Windows 执行。原生 Windows 全量后端测试含既有 Linux 文件系统/工具环境要求，未通过删测试或跳过门禁来规避。初次并行构建清理了架构测试依赖的受控占位文件，现改为前端构建完成后恢复占位，再运行后端门禁。

详细运行日志保留于隔离克隆 `.tmp/layer6-evidence/`；不提交含 DSN、测试凭据或会话信息的运行配置。验收 schema 与临时 Web 服务已清理。

## 第 7 层交接与边界

前端消费当前控制台接口，未实现或模拟网关、装机状态机与实际执行。任务取消/重试继续展示服务端尚未提供执行控制的诊断。生产发布必须先构建资源；仓库占位首页只用于保证未构建前端时 Go 源码可编译。开发代理下也覆盖事件补读回退，生产同源 SSE 已实测连接及接收事件。

主工作区有并行渲染层修改，本阶段在独立本地克隆实现；合并只更新本地 dev，不切换或清理主工作区，不推送远端。
